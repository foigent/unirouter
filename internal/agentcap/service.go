package agentcap

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"

	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/models"
)

type ServiceOptions struct {
	Now               func() time.Time
	TokenTTL          time.Duration
	RefreshBase       time.Duration
	RefreshJitter     time.Duration
	AuthorityCacheTTL time.Duration
	AuthorityCacheMax int
}

type Service struct {
	db            db.DBQuerier
	rdb           *redis.Client
	signingKey    []byte
	now           func() time.Time
	tokenTTL      time.Duration
	refreshBase   time.Duration
	refreshJitter time.Duration
	authorities   *authorityCache
}

func NewService(q db.DBQuerier, rdb *redis.Client, rootSecret string) (*Service, error) {
	return NewServiceWithOptions(q, rdb, rootSecret, ServiceOptions{})
}

func NewServiceWithOptions(q db.DBQuerier, rdb *redis.Client, rootSecret string, options ServiceOptions) (*Service, error) {
	if q == nil || rdb == nil || strings.TrimSpace(rootSecret) == "" {
		return nil, fmt.Errorf("agent capability service requires database, Redis and signing root")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.TokenTTL == 0 {
		options.TokenTTL = TokenTTL
	}
	if options.RefreshBase == 0 {
		options.RefreshBase = RefreshBase
	}
	if options.RefreshJitter == 0 {
		options.RefreshJitter = RefreshJitter
	}
	if options.AuthorityCacheTTL == 0 {
		options.AuthorityCacheTTL = AuthorityCacheTTL
	}
	if options.AuthorityCacheMax == 0 {
		options.AuthorityCacheMax = MaxAuthorityCacheKey
	}
	if options.TokenTTL <= 0 || options.TokenTTL > TokenTTL {
		return nil, fmt.Errorf("agent capability token TTL must be in (0, %s]", TokenTTL)
	}
	if options.AuthorityCacheTTL <= 0 || options.AuthorityCacheTTL > 30*time.Second {
		return nil, fmt.Errorf("agent capability authority cache TTL must be in (0, 30s]")
	}
	if options.AuthorityCacheMax <= 0 {
		return nil, fmt.Errorf("agent capability authority cache maximum must be positive")
	}

	return &Service{
		db:            q,
		rdb:           rdb,
		signingKey:    deriveSigningKey(rootSecret),
		now:           options.Now,
		tokenTTL:      options.TokenTTL,
		refreshBase:   options.RefreshBase,
		refreshJitter: options.RefreshJitter,
		authorities:   newAuthorityCache(options.AuthorityCacheMax, options.AuthorityCacheTTL),
	}, nil
}

func deriveSigningKey(rootSecret string) []byte {
	mac := hmac.New(sha256.New, []byte(rootSecret))
	_, _ = mac.Write([]byte("unirouter.agent-capability.signing.v2\x00"))
	return mac.Sum(nil)
}

func (s *Service) Exchange(ctx context.Context, rootKey *models.ApiKey, request ExchangeRequest) (TokenResponse, error) {
	if rootKey == nil || rootKey.ID <= 0 {
		return TokenResponse{}, ErrInvalidToken
	}
	if err := validateAgentID(request.RootAgentID); err != nil {
		return TokenResponse{}, err
	}
	if request.WorkflowTTLSeconds < int64(MinWorkflowTTL/time.Second) || request.WorkflowTTLSeconds > int64(MaxWorkflowTTL/time.Second) {
		return TokenResponse{}, fmt.Errorf("%w: workflow_ttl_seconds must be between %d and %d", ErrInvalidRequest, int64(MinWorkflowTTL/time.Second), int64(MaxWorkflowTTL/time.Second))
	}
	workflowTTL := time.Duration(request.WorkflowTTLSeconds) * time.Second
	limits := request.Limits.Normalize()
	if err := limits.ValidateShape(); err != nil {
		return TokenResponse{}, err
	}
	if limits.MaxDepth < 0 || limits.MaxChildren > limits.MaxDescendants {
		return TokenResponse{}, fmt.Errorf("%w: invalid root topology limits", ErrInvalidRequest)
	}
	if err := validateEndpoints(limits.AllowedEndpoints); err != nil {
		return TokenResponse{}, err
	}

	// Exchange always performs a fresh authority read. This prevents a prior
	// cache entry from widening a newly registered workflow after an admin edit.
	authority, err := queryAuthority(ctx, s.db, rootKey.ID)
	if err != nil {
		return TokenResponse{}, mapAuthorityError(err)
	}
	s.authorities.put(rootKey.ID, authority, s.now())
	if !authority.active() || authority.APIKey.OrgID != rootKey.OrgID {
		return TokenResponse{}, ErrInactiveRoot
	}
	if !authority.Boundary.CapabilityManagementAllowed() {
		return TokenResponse{}, ErrManagementForbidden
	}
	if err := validateRootLimits(limits, authority); err != nil {
		return TokenResponse{}, err
	}

	now := s.now().UTC()
	workflowID, err := randomID("capwf_", 16)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("generate workflow identity: %w", err)
	}
	generation, err := randomID("gen_", 16)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("generate workflow generation: %w", err)
	}
	workflowExpiresAt := now.Add(workflowTTL)
	claims, response, err := s.newClaims(authority, workflowID, generation, workflowExpiresAt, request.RootAgentID, "", []string{request.RootAgentID}, 0, limits)
	if err != nil {
		return TokenResponse{}, err
	}
	if err := s.registerWorkflow(ctx, claims); err != nil {
		return TokenResponse{}, err
	}
	return response, nil
}

func (s *Service) Delegate(ctx context.Context, parent *Claims, request DelegateRequest) (TokenResponse, error) {
	if parent == nil {
		return TokenResponse{}, ErrInvalidToken
	}
	if err := validateClaims(parent, s.tokenTTL); err != nil {
		return TokenResponse{}, ErrInvalidToken
	}
	if err := s.verifyState(ctx, parent); err != nil {
		return TokenResponse{}, err
	}
	if err := validateAgentID(request.ChildAgentID); err != nil {
		return TokenResponse{}, err
	}
	if slices.Contains(parent.Lineage, request.ChildAgentID) {
		return TokenResponse{}, fmt.Errorf("%w: child_agent_id already exists in the ancestor chain", ErrConflict)
	}
	limits := request.Limits.Normalize()
	if err := limits.ValidateShape(); err != nil {
		return TokenResponse{}, err
	}
	if err := validateEndpoints(limits.AllowedEndpoints); err != nil {
		return TokenResponse{}, err
	}
	depth := parent.Depth + 1
	if depth > parent.Limits.MaxDepth || limits.MaxDepth < depth || !limits.IsAttenuationOf(parent.Limits) {
		return TokenResponse{}, fmt.Errorf("%w: child limits must only reduce parent authority", ErrLimitExceeded)
	}

	authority, err := s.loadAuthority(ctx, parent.RootKeyID)
	if err != nil {
		return TokenResponse{}, err
	}
	if !authority.active() || authority.Version != parent.RootAuthorityVersion {
		return TokenResponse{}, ErrAuthorityChanged
	}
	if err := validateRootLimits(limits, authority); err != nil {
		return TokenResponse{}, err
	}
	lineage := append(slices.Clone(parent.Lineage), request.ChildAgentID)
	workflowExpiresAt := time.Unix(parent.WorkflowExpiresAt, 0).UTC()
	claims, response, err := s.newClaims(authority, parent.WorkflowID, parent.WorkflowGeneration, workflowExpiresAt, parent.RootAgentID, parent.AgentID, lineage, depth, limits)
	if err != nil {
		return TokenResponse{}, err
	}
	if err := s.registerChild(ctx, parent, claims); err != nil {
		return TokenResponse{}, err
	}
	return response, nil
}

func (s *Service) Refresh(ctx context.Context, current *Claims) (TokenResponse, error) {
	if current == nil || current.ID == "" || current.IssuedAt == nil {
		return TokenResponse{}, ErrInvalidToken
	}
	if err := validateClaims(current, s.tokenTTL); err != nil {
		return TokenResponse{}, ErrInvalidToken
	}
	now := s.now().UTC()
	grant, grantExists, err := s.loadRefreshGrant(ctx, current.WorkflowID, current.ID)
	if err != nil {
		return TokenResponse{}, err
	}
	if !grantExists && now.Before(current.IssuedAt.Time.Add(2*time.Minute)) {
		return TokenResponse{}, ErrRefreshTooEarly
	}
	if err := s.verifyState(ctx, current); err != nil {
		if !errors.Is(err, ErrSuperseded) {
			return TokenResponse{}, err
		}
		if !grantExists {
			grant, grantExists, err = s.loadRefreshGrant(ctx, current.WorkflowID, current.ID)
			if err != nil {
				return TokenResponse{}, err
			}
		}
		if !grantExists {
			return TokenResponse{}, ErrSuperseded
		}
	}
	authority, err := s.loadAuthority(ctx, current.RootKeyID)
	if err != nil {
		return TokenResponse{}, err
	}
	if !authority.active() || authority.Version != current.RootAuthorityVersion {
		return TokenResponse{}, ErrAuthorityChanged
	}
	if err := validateRootLimits(current.Limits, authority); err != nil {
		return TokenResponse{}, err
	}
	if !grantExists {
		grant, err = s.reserveRefreshGrant(ctx, current)
		if err != nil {
			return TokenResponse{}, err
		}
	}
	workflowExpiresAt := time.Unix(current.WorkflowExpiresAt, 0).UTC()
	_, response, err := s.issueClaims(authority, current.WorkflowID, current.WorkflowGeneration, workflowExpiresAt, current.RootAgentID, current.ParentAgentID, slices.Clone(current.Lineage), current.Depth, current.Limits, grant.JTI, time.Unix(grant.IssuedAt, 0).UTC())
	if err != nil {
		return TokenResponse{}, err
	}
	return response, nil
}

func (s *Service) Revoke(ctx context.Context, rootKey *models.ApiKey, workflowID string) (RevokeResponse, error) {
	if rootKey == nil || rootKey.ID <= 0 || !validWorkflowID(workflowID) {
		return RevokeResponse{}, ErrInvalidRequest
	}
	authority, err := queryAuthority(ctx, s.db, rootKey.ID)
	if err != nil {
		return RevokeResponse{}, mapAuthorityError(err)
	}
	if authority.APIKey.OrgID != rootKey.OrgID || !authority.Boundary.CapabilityManagementAllowed() {
		return RevokeResponse{}, ErrManagementForbidden
	}
	revokedAt := s.now().UTC()
	newGeneration, err := randomID("gen_", 16)
	if err != nil {
		return RevokeResponse{}, fmt.Errorf("generate revocation generation: %w", err)
	}
	if err := s.revokeWorkflow(ctx, workflowID, rootKey.ID, newGeneration, revokedAt); err != nil {
		return RevokeResponse{}, err
	}
	return RevokeResponse{
		SchemaVersion: SchemaVersion,
		WorkflowID:    workflowID,
		Status:        "revoked",
		RevokedAt:     revokedAt,
	}, nil
}

func (s *Service) Authenticate(ctx context.Context, rawToken string) (*Authentication, error) {
	claims, err := s.parseToken(rawToken)
	if err != nil {
		return nil, err
	}
	if err := s.verifyState(ctx, claims); err != nil {
		return nil, err
	}
	authority, err := s.loadAuthority(ctx, claims.RootKeyID)
	if err != nil {
		return nil, err
	}
	if !authority.active() {
		return nil, ErrInactiveRoot
	}
	if authority.Version != claims.RootAuthorityVersion {
		return nil, ErrAuthorityChanged
	}
	if authority.APIKey.OrgID != claims.OrgID || pointerValue(authority.APIKey.ProjectID) != claims.ProjectID ||
		pointerValue(authority.APIKey.OwnerUserID) != claims.OwnerUserID {
		return nil, ErrAuthorityChanged
	}

	apiKey := authority.APIKey
	apiKey.AllowedModels = strings.Join(claims.Limits.AllowedModels, ",")
	apiKey.RPMLimit = claims.RootRPMLimit
	apiKey.QPSLimit = claims.RootRPSLimit
	return &Authentication{APIKey: &apiKey, Claims: claims}, nil
}

func (s *Service) parseToken(rawToken string) (*Claims, error) {
	if !strings.HasPrefix(rawToken, TokenPrefix) || len(rawToken) > 64<<10 {
		return nil, ErrInvalidToken
	}
	encoded := strings.TrimPrefix(rawToken, TokenPrefix)
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(encoded, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 || token.Header["kid"] != SigningKeyID {
			return nil, ErrInvalidToken
		}
		return s.signingKey, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(Issuer),
		jwt.WithAudience(Audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(s.now),
		jwt.WithLeeway(5*time.Second),
	)
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if err := validateClaims(claims, s.tokenTTL); err != nil {
		return nil, err
	}
	return claims, nil
}

func (s *Service) newClaims(authority authoritySnapshot, workflowID, generation string, workflowExpiresAt time.Time, rootAgentID, parentAgentID string, lineage []string, depth int, limits Limits) (*Claims, TokenResponse, error) {
	jti, err := randomID("capjti_", 16)
	if err != nil {
		return nil, TokenResponse{}, fmt.Errorf("generate capability identity: %w", err)
	}
	return s.issueClaims(authority, workflowID, generation, workflowExpiresAt, rootAgentID, parentAgentID, lineage, depth, limits, jti, s.now().UTC().Truncate(time.Second))
}

func (s *Service) issueClaims(authority authoritySnapshot, workflowID, generation string, workflowExpiresAt time.Time, rootAgentID, parentAgentID string, lineage []string, depth int, limits Limits, jti string, now time.Time) (*Claims, TokenResponse, error) {
	expiresAt := now.Add(s.tokenTTL)
	if workflowExpiresAt.Before(expiresAt) {
		expiresAt = workflowExpiresAt
	}
	if !expiresAt.After(now) {
		return nil, TokenResponse{}, ErrRevoked
	}
	agentID := lineage[len(lineage)-1]
	claims := &Claims{
		SchemaVersion:        SchemaVersion,
		WorkflowID:           workflowID,
		WorkflowGeneration:   generation,
		WorkflowExpiresAt:    workflowExpiresAt.Unix(),
		RootAgentID:          rootAgentID,
		AgentID:              agentID,
		ParentAgentID:        parentAgentID,
		Lineage:              slices.Clone(lineage),
		Depth:                depth,
		RootKeyID:            authority.APIKey.ID,
		OrgID:                authority.APIKey.OrgID,
		ProjectID:            pointerValue(authority.APIKey.ProjectID),
		OwnerUserID:          pointerValue(authority.APIKey.OwnerUserID),
		RootAuthorityVersion: authority.Version,
		RootRPMLimit:         effectiveRPM(authority.APIKey.RPMLimit),
		RootRPSLimit:         effectiveRPS(authority.APIKey.QPSLimit),
		Limits:               limits,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   agentID,
			Audience:  jwt.ClaimStrings{Audience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        jti,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = SigningKeyID
	token.Header["typ"] = "at+jwt"
	signed, err := token.SignedString(s.signingKey)
	if err != nil {
		return nil, TokenResponse{}, fmt.Errorf("sign capability: %w", err)
	}
	refreshAfter := now.Add(s.refreshBase + s.refreshJitterFor(jti))
	if !refreshAfter.Before(expiresAt) {
		refreshAfter = now.Add(expiresAt.Sub(now) / 2)
	}
	response := TokenResponse{
		SchemaVersion:     SchemaVersion,
		CapabilityToken:   TokenPrefix + signed,
		TokenType:         "Bearer",
		WorkflowID:        workflowID,
		RootAgentID:       rootAgentID,
		AgentID:           agentID,
		ParentAgentID:     parentAgentID,
		Depth:             depth,
		ExpiresAt:         expiresAt,
		RefreshAfter:      refreshAfter,
		WorkflowExpiresAt: workflowExpiresAt,
		Limits:            limits,
	}
	return claims, response, nil
}

func (s *Service) refreshJitterFor(jti string) time.Duration {
	if s.refreshJitter <= 0 {
		return 0
	}
	random := sha256.Sum256([]byte("unirouter.agent-capability.refresh-jitter.v1\x00" + jti))
	span := uint64(2*s.refreshJitter + 1)
	if span == 0 {
		return 0
	}
	return time.Duration(binary.BigEndian.Uint64(random[:8])%span) - s.refreshJitter
}

func validateRootLimits(limits Limits, authority authoritySnapshot) error {
	root := authority.APIKey
	rpmLimit := effectiveRPM(root.RPMLimit)
	rpsLimit := effectiveRPS(root.QPSLimit)
	if root.GrantBound {
		grantRPM := accessscope.ProjectDefaultRPMLimit
		if root.GrantRPMLimit != nil {
			grantRPM = *root.GrantRPMLimit
		}
		grantRPS := accessscope.ProjectDefaultRPSLimit
		if root.GrantRPSLimit != nil {
			grantRPS = *root.GrantRPSLimit
		}
		if grantRPM < rpmLimit {
			rpmLimit = grantRPM
		}
		if grantRPS < rpsLimit {
			rpsLimit = grantRPS
		}
	}
	if limits.RPMLimit > rpmLimit || limits.RPSLimit > rpsLimit {
		return fmt.Errorf("%w: capability rate limits exceed the root API key", ErrLimitExceeded)
	}
	if root.GrantConcurrencyLimit != nil && limits.ConcurrencyLimit > *root.GrantConcurrencyLimit {
		return fmt.Errorf("%w: capability concurrency exceeds the live access grant", ErrLimitExceeded)
	}
	if root.BudgetLimit != nil && *root.BudgetLimit > 0 && limits.CostBudgetCents > int64(*root.BudgetLimit) {
		return fmt.Errorf("%w: capability cost budget exceeds the root API key", ErrLimitExceeded)
	}
	rootModels := normalizedSet(strings.Split(root.AllowedModels, ","))
	if root.ModelScopeDenied || (len(rootModels) > 0 && !setSubset(limits.AllowedModels, rootModels)) {
		return fmt.Errorf("%w: capability models exceed the root API key", ErrLimitExceeded)
	}
	rootEndpoints := normalizedSet(strings.Split(root.AllowedEndpoints, ","))
	for _, endpoint := range limits.AllowedEndpoints {
		if !accessscope.ExecutionEndpointAllowed(
			root.GrantBound,
			root.GrantScopeMode,
			root.EndpointDenied,
			rootEndpoints,
			endpoint,
		) {
			return fmt.Errorf("%w: capability endpoints exceed the live access grant", ErrLimitExceeded)
		}
	}
	return nil
}

func validateEndpoints(endpoints []string) error {
	supported := modeloperation.AgentCapabilityPaths()
	for _, endpoint := range endpoints {
		if !slices.Contains(supported, endpoint) {
			return fmt.Errorf("%w: unsupported model-gateway endpoint %q", ErrInvalidRequest, endpoint)
		}
	}
	return nil
}

func validateAgentID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return fmt.Errorf("%w: Agent ID must contain 1-128 characters", ErrInvalidRequest)
	}
	for index, r := range value {
		allowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || strings.ContainsRune("._:@/-", r)
		if !allowed || (index == 0 && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')) {
			return fmt.Errorf("%w: Agent ID contains unsupported characters", ErrInvalidRequest)
		}
	}
	return nil
}

func validateClaims(claims *Claims, maxTTL time.Duration) error {
	if claims == nil || claims.SchemaVersion != SchemaVersion || !validWorkflowID(claims.WorkflowID) ||
		claims.WorkflowGeneration == "" || claims.RootKeyID <= 0 || claims.OrgID <= 0 ||
		claims.RootAuthorityVersion == "" || claims.ID == "" || claims.ExpiresAt == nil || claims.IssuedAt == nil ||
		claims.Subject != claims.AgentID || claims.Depth < 0 || claims.Depth >= len(claims.Lineage) ||
		len(claims.Lineage) != claims.Depth+1 || claims.Lineage[claims.Depth] != claims.AgentID ||
		claims.Lineage[0] != claims.RootAgentID || claims.WorkflowExpiresAt <= 0 {
		return ErrInvalidToken
	}
	if claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time) > maxTTL+time.Second || claims.ExpiresAt.Unix() > claims.WorkflowExpiresAt {
		return ErrInvalidToken
	}
	if time.Unix(claims.WorkflowExpiresAt, 0).Sub(claims.IssuedAt.Time) > MaxWorkflowTTL+time.Second ||
		claims.RootRPMLimit <= 0 || claims.RootRPSLimit <= 0 || claims.Depth > MaxAgentDepth {
		return ErrInvalidToken
	}
	if (claims.Depth == 0 && claims.ParentAgentID != "") ||
		(claims.Depth > 0 && claims.ParentAgentID != claims.Lineage[claims.Depth-1]) {
		return ErrInvalidToken
	}
	if err := claims.Limits.ValidateShape(); err != nil {
		return ErrInvalidToken
	}
	if err := validateEndpoints(claims.Limits.AllowedEndpoints); err != nil {
		return ErrInvalidToken
	}
	if claims.Depth > claims.Limits.MaxDepth {
		return ErrInvalidToken
	}
	seenAgents := make(map[string]struct{}, len(claims.Lineage))
	for _, agentID := range claims.Lineage {
		if err := validateAgentID(agentID); err != nil {
			return ErrInvalidToken
		}
		if _, exists := seenAgents[agentID]; exists {
			return ErrInvalidToken
		}
		seenAgents[agentID] = struct{}{}
	}
	return nil
}

func validWorkflowID(value string) bool {
	if !strings.HasPrefix(value, "capwf_") || len(value) != len("capwf_")+32 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "capwf_"))
	return err == nil
}

func pointerValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func randomID(prefix string, size int) (string, error) {
	if size <= 0 || size > 1024 {
		return "", fmt.Errorf("invalid random identity size")
	}
	random := make([]byte, size)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(random), nil
}

func isStateDenial(err error) bool {
	return errors.Is(err, ErrRevoked) || errors.Is(err, ErrSuperseded) || errors.Is(err, ErrAuthorityChanged) || errors.Is(err, ErrInactiveRoot)
}
