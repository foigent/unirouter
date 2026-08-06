package oidc

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/uniroute/unirouter/internal/auth"
)

type Config struct {
	Enabled            bool
	IssuerURL          string
	ClientID           string
	ClientSecret       string
	RedirectURL        string
	ProviderName       string
	Scopes             []string
	AllowedEmailDomain string
	AutoProvision      bool
	DefaultOrgID       int64
	DefaultRole        string
	DefaultProjectSlug string
	UsernameClaim      string
	EmailClaim         string
	RoleClaim          string
	OrgIDClaim         string
	HTTPClient         *http.Client
	Now                func() time.Time
}

type Service struct {
	cfg    Config
	client *http.Client
	mu     sync.Mutex
	doc    *discoveryDoc
	keys   *jwksDoc
}

type discoveryDoc struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

type jwksDoc struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	KeyType string `json:"kty"`
	KeyID   string `json:"kid"`
	Use     string `json:"use"`
	Alg     string `json:"alg"`
	N       string `json:"n"`
	E       string `json:"e"`
}

type tokenResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

type Identity struct {
	Issuer           string
	Subject          string
	Email            string
	Username         string
	Name             string
	Role             string // users.role compatibility shadow
	OrganizationRole string
	ProjectRole      string
	OrgID            int64
	RawClaims        map[string]interface{}
}

func NewService(cfg Config) (*Service, error) {
	cfg = normalizeConfig(cfg)
	if cfg.Enabled {
		if err := cfg.validate(); err != nil {
			return nil, err
		}
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Service{cfg: cfg, client: client}, nil
}

func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

func (s *Service) Config() Config {
	if s == nil {
		return Config{}
	}
	return s.cfg
}

func (s *Service) PublicConfig() map[string]interface{} {
	if !s.Enabled() {
		return map[string]interface{}{"enabled": false}
	}
	return map[string]interface{}{
		"enabled":       true,
		"provider_name": s.cfg.ProviderName,
		"login_url":     "/api/v1/auth/oidc/start",
		"scopes":        s.cfg.Scopes,
	}
}

func (s *Service) AuthCodeURL(ctx context.Context, state, nonce string) (string, error) {
	if !s.Enabled() {
		return "", errors.New("oidc is disabled")
	}
	doc, err := s.discovery(ctx)
	if err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", s.cfg.ClientID)
	values.Set("redirect_uri", s.cfg.RedirectURL)
	values.Set("scope", strings.Join(s.cfg.Scopes, " "))
	values.Set("state", state)
	values.Set("nonce", nonce)

	authURL, err := url.Parse(doc.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("parse authorization endpoint: %w", err)
	}
	authURL.RawQuery = values.Encode()
	return authURL.String(), nil
}

func (s *Service) Exchange(ctx context.Context, code, expectedNonce string) (*Identity, error) {
	if !s.Enabled() {
		return nil, errors.New("oidc is disabled")
	}
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("authorization code is required")
	}
	doc, err := s.discovery(ctx)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", s.cfg.RedirectURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, doc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(s.cfg.ClientID, s.cfg.ClientSecret)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}
	defer resp.Body.Close()

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if tr.Error != "" {
			return nil, fmt.Errorf("token endpoint returned %s: %s", tr.Error, tr.Description)
		}
		return nil, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("token endpoint returned %s: %s", tr.Error, tr.Description)
	}
	if tr.IDToken == "" {
		return nil, errors.New("token response did not include id_token")
	}
	return s.VerifyIDToken(ctx, tr.IDToken, expectedNonce)
}

func (s *Service) VerifyIDToken(ctx context.Context, idToken, expectedNonce string) (*Identity, error) {
	if !s.Enabled() {
		return nil, errors.New("oidc is disabled")
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(idToken, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, fmt.Errorf("unsupported id_token alg %q", token.Method.Alg())
		}
		kid, _ := token.Header["kid"].(string)
		return s.rsaKey(ctx, kid)
	}, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}), jwt.WithIssuer(s.cfg.IssuerURL), jwt.WithAudience(s.cfg.ClientID), jwt.WithExpirationRequired(), jwt.WithLeeway(60*time.Second), jwt.WithJSONNumber())
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}
	if !token.Valid {
		return nil, errors.New("invalid id_token")
	}

	nonce := claimString(claims, "nonce")
	if expectedNonce != "" && nonce != expectedNonce {
		return nil, errors.New("id_token nonce mismatch")
	}
	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return nil, errors.New("id_token subject is required")
	}

	email := claimString(claims, s.cfg.EmailClaim)
	if email == "" && s.cfg.EmailClaim != "email" {
		email = claimString(claims, "email")
	}
	if s.cfg.AllowedEmailDomain != "" {
		domain := strings.TrimPrefix(strings.ToLower(s.cfg.AllowedEmailDomain), "@")
		if email == "" {
			return nil, fmt.Errorf("email is required for allowed domain %q", domain)
		}
		if !strings.HasSuffix(strings.ToLower(email), "@"+domain) {
			return nil, fmt.Errorf("email domain %q is not allowed", domain)
		}
	}

	role := claimString(claims, s.cfg.RoleClaim)
	if role == "" {
		role = s.cfg.DefaultRole
	}
	organizationRole, compatibilityRole, projectRole, ok := classifyOIDCRole(role)
	if !ok {
		return nil, fmt.Errorf("invalid oidc role %q", role)
	}

	orgID := claimInt64(claims, s.cfg.OrgIDClaim)
	if orgID == 0 {
		orgID = s.cfg.DefaultOrgID
	}
	if orgID <= 0 && s.cfg.AutoProvision {
		return nil, errors.New("oidc org_id is required")
	}

	username := claimString(claims, s.cfg.UsernameClaim)
	if username == "" {
		username = claimString(claims, "preferred_username")
	}
	if username == "" {
		username = email
	}
	if username == "" {
		username = sub
	}

	return &Identity{
		Issuer:           s.cfg.IssuerURL,
		Subject:          sub,
		Email:            email,
		Username:         username,
		Name:             claimString(claims, "name"),
		Role:             compatibilityRole,
		OrganizationRole: organizationRole,
		ProjectRole:      projectRole,
		OrgID:            orgID,
		RawClaims:        map[string]interface{}(claims),
	}, nil
}

func classifyOIDCRole(role string) (organizationRole, compatibilityRole, projectRole string, ok bool) {
	if canonicalProjectRole, projectOK := auth.NormalizeProjectMembershipRole(role); projectOK {
		shadow, shadowOK := auth.CompatibilityRoleForMembership(canonicalProjectRole)
		return auth.RoleMember, shadow, canonicalProjectRole, shadowOK
	}
	canonicalOrganizationRole, organizationOK := auth.NormalizeOrganizationRole(role)
	if !organizationOK || !auth.IsOrganizationRole(role) {
		return "", "", "", false
	}
	return canonicalOrganizationRole, canonicalOrganizationRole, "", true
}

func (s *Service) discovery(ctx context.Context) (*discoveryDoc, error) {
	s.mu.Lock()
	if s.doc != nil {
		doc := s.doc
		s.mu.Unlock()
		return doc, nil
	}
	s.mu.Unlock()

	endpoint := strings.TrimRight(s.cfg.IssuerURL, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create discovery request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("load oidc discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oidc discovery returned status %d", resp.StatusCode)
	}
	var doc discoveryDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode oidc discovery: %w", err)
	}
	if doc.Issuer != "" && strings.TrimRight(doc.Issuer, "/") != s.cfg.IssuerURL {
		return nil, fmt.Errorf("discovery issuer mismatch: %s", doc.Issuer)
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" || doc.JWKSURI == "" {
		return nil, errors.New("oidc discovery missing required endpoints")
	}

	s.mu.Lock()
	s.doc = &doc
	s.mu.Unlock()
	return &doc, nil
}

func (s *Service) jwks(ctx context.Context) (*jwksDoc, error) {
	s.mu.Lock()
	if s.keys != nil {
		keys := s.keys
		s.mu.Unlock()
		return keys, nil
	}
	s.mu.Unlock()

	doc, err := s.discovery(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, doc.JWKSURI, nil)
	if err != nil {
		return nil, fmt.Errorf("create jwks request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("load jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("jwks returned status %d", resp.StatusCode)
	}
	var keys jwksDoc
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}
	if len(keys.Keys) == 0 {
		return nil, errors.New("jwks contains no keys")
	}
	s.mu.Lock()
	s.keys = &keys
	s.mu.Unlock()
	return &keys, nil
}

func (s *Service) rsaKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	keys, err := s.jwks(ctx)
	if err != nil {
		return nil, err
	}
	for _, key := range keys.Keys {
		if key.KeyType != "RSA" {
			continue
		}
		if kid != "" && key.KeyID != "" && key.KeyID != kid {
			continue
		}
		return key.rsaPublicKey()
	}
	return nil, fmt.Errorf("jwks rsa key not found for kid %q", kid)
}

func (k jwkKey) rsaPublicKey() (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode jwk modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode jwk exponent: %w", err)
	}
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}
	if e < 3 {
		return nil, errors.New("invalid jwk exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}

func normalizeConfig(cfg Config) Config {
	cfg.IssuerURL = strings.TrimRight(strings.TrimSpace(cfg.IssuerURL), "/")
	cfg.RedirectURL = strings.TrimSpace(cfg.RedirectURL)
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
	cfg.ProviderName = strings.TrimSpace(cfg.ProviderName)
	if cfg.ProviderName == "" {
		cfg.ProviderName = "OIDC"
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "email", "profile"}
	}
	if !containsScope(cfg.Scopes, "openid") {
		cfg.Scopes = append([]string{"openid"}, cfg.Scopes...)
	}
	if cfg.DefaultRole == "" {
		cfg.DefaultRole = auth.RoleViewer
	}
	if cfg.DefaultRole == auth.RoleLegacyAdmin {
		cfg.DefaultRole = auth.RolePlatformAdmin
	}
	if cfg.DefaultProjectSlug == "" {
		cfg.DefaultProjectSlug = "default"
	}
	if cfg.UsernameClaim == "" {
		cfg.UsernameClaim = "preferred_username"
	}
	if cfg.EmailClaim == "" {
		cfg.EmailClaim = "email"
	}
	if cfg.RoleClaim == "" {
		cfg.RoleClaim = "uniroute_role"
	}
	if cfg.OrgIDClaim == "" {
		cfg.OrgIDClaim = "uniroute_org_id"
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return cfg
}

func (cfg Config) validate() error {
	if cfg.IssuerURL == "" {
		return errors.New("OIDC_ISSUER_URL is required when OIDC is enabled")
	}
	if cfg.ClientID == "" {
		return errors.New("OIDC_CLIENT_ID is required when OIDC is enabled")
	}
	if cfg.ClientSecret == "" {
		return errors.New("OIDC_CLIENT_SECRET is required when OIDC is enabled")
	}
	if cfg.RedirectURL == "" {
		return errors.New("OIDC_REDIRECT_URL is required when OIDC is enabled")
	}
	if _, _, _, ok := classifyOIDCRole(cfg.DefaultRole); !ok {
		return fmt.Errorf("invalid OIDC_DEFAULT_ROLE %q", cfg.DefaultRole)
	}
	if cfg.AutoProvision && cfg.DefaultOrgID <= 0 {
		return errors.New("OIDC_DEFAULT_ORG_ID must be set when OIDC auto provisioning is enabled")
	}
	return nil
}

func containsScope(scopes []string, needle string) bool {
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == needle {
			return true
		}
	}
	return false
}

func claimString(claims jwt.MapClaims, name string) string {
	if name == "" {
		return ""
	}
	v, ok := claims[name]
	if !ok || v == nil {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []interface{}:
		for _, item := range typed {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func claimInt64(claims jwt.MapClaims, name string) int64 {
	if name == "" {
		return 0
	}
	v, ok := claims[name]
	if !ok || v == nil {
		return 0
	}
	switch typed := v.(type) {
	case json.Number:
		n, _ := typed.Int64()
		return n
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return n
	default:
		return 0
	}
}
