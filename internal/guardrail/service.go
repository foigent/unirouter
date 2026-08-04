package guardrail

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/filter"
)

type Options struct {
	RefreshInterval           time.Duration
	SnapshotMaxAge            time.Duration
	ExternalDeadline          time.Duration
	ExternalConcurrency       int
	MaxMatchingPolicies       int
	MaxDetectorTexts          int
	LocalDetectorMaxBytes     int
	ExternalDetectorMaxBytes  int
	MaxActivePoliciesPerOrg   int
	MaxActivePlatformPolicies int
	MaxCachedPolicies         int
	MaxCachedTenantScopes     int
}

func DefaultOptions() Options {
	return Options{
		RefreshInterval: time.Second, SnapshotMaxAge: 30 * time.Second,
		ExternalDeadline: time.Second, ExternalConcurrency: 32, MaxMatchingPolicies: 64,
		MaxDetectorTexts: 256, LocalDetectorMaxBytes: 128 * 1024, ExternalDetectorMaxBytes: 256 * 1024,
		MaxActivePoliciesPerOrg: 256, MaxActivePlatformPolicies: 256,
		MaxCachedPolicies: 4096, MaxCachedTenantScopes: 4096,
	}
}

func (o Options) Validate() error {
	if o.RefreshInterval <= 0 || o.SnapshotMaxAge < o.RefreshInterval || o.ExternalDeadline <= 0 ||
		o.ExternalDeadline > time.Second || o.ExternalConcurrency <= 0 || o.ExternalConcurrency > 1024 ||
		o.MaxMatchingPolicies <= 0 || o.MaxMatchingPolicies > 256 ||
		o.MaxDetectorTexts <= 0 || o.MaxDetectorTexts > 1024 ||
		o.LocalDetectorMaxBytes <= 0 || o.LocalDetectorMaxBytes > 1024*1024 ||
		o.ExternalDetectorMaxBytes <= 0 || o.ExternalDetectorMaxBytes > 1024*1024 ||
		o.MaxActivePoliciesPerOrg <= 0 || o.MaxActivePoliciesPerOrg > 4096 ||
		o.MaxActivePlatformPolicies <= 0 || o.MaxActivePlatformPolicies > 4096 ||
		o.MaxCachedPolicies < o.MaxActivePoliciesPerOrg || o.MaxCachedPolicies < o.MaxActivePlatformPolicies ||
		o.MaxCachedPolicies > 65536 || o.MaxCachedTenantScopes <= 0 || o.MaxCachedTenantScopes > o.MaxCachedPolicies {
		return fmt.Errorf("invalid guardrail service options")
	}
	return nil
}

type scopedSnapshot struct {
	policies []Policy
	loadedAt time.Time
	version  string
}

type snapshot struct {
	platform                scopedSnapshot
	tenants                 map[int64]scopedSnapshot
	tenantDiscoveryComplete bool
}

type PolicyLimits struct {
	MaxActivePoliciesPerOrg   int
	MaxActivePlatformPolicies int
	MaxCachedPolicies         int
	MaxCachedTenantScopes     int
}

type Service struct {
	db                db.DBQuerier
	sensitive         *filter.DFAFilter
	fingerprintSecret []byte
	options           Options
	snapshot          atomic.Pointer[snapshot]
	mu                sync.RWMutex
	detectors         map[string]Detector
	externalSlots     chan struct{}
}

func NewService(database db.DBQuerier, sensitive *filter.DFAFilter, fingerprintSecret []byte, options Options) (*Service, error) {
	if len(fingerprintSecret) < 16 {
		return nil, fmt.Errorf("guardrail evidence fingerprint secret must be at least 16 bytes")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	service := &Service{
		db: database, sensitive: sensitive, fingerprintSecret: append([]byte(nil), fingerprintSecret...), options: options,
		detectors: make(map[string]Detector), externalSlots: make(chan struct{}, options.ExternalConcurrency),
	}
	service.detectors[DetectorPII] = piiSecretsDetector{}
	service.snapshot.Store(emptySnapshot(time.Now().UTC()))
	return service, nil
}

func emptySnapshot(now time.Time) *snapshot {
	return &snapshot{
		platform: scopedSnapshot{loadedAt: now, version: policySetVersion(nil)},
		tenants:  make(map[int64]scopedSnapshot), tenantDiscoveryComplete: true,
	}
}

func (s *Service) PolicyLimits() PolicyLimits {
	options := DefaultOptions()
	if s != nil {
		options = s.options
	}
	return PolicyLimits{
		MaxActivePoliciesPerOrg:   options.MaxActivePoliciesPerOrg,
		MaxActivePlatformPolicies: options.MaxActivePlatformPolicies,
		MaxCachedPolicies:         options.MaxCachedPolicies,
		MaxCachedTenantScopes:     options.MaxCachedTenantScopes,
	}
}

func (s *Service) RegisterDetector(ref string, detector Detector) error {
	ref = strings.TrimSpace(ref)
	if !validDetectorIdentifier(ref) || detector == nil || !validDetectorVersion(detector.Version()) {
		return fmt.Errorf("external detector registration is invalid")
	}
	if streamDetector, ok := detector.(StreamCapableDetector); ok {
		capability := streamDetector.StreamCapability()
		if capability.LookbehindBytes < 0 || capability.LookbehindBytes > MaxStreamLookbehindBytes {
			return fmt.Errorf("external detector stream lookbehind is invalid")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.detectors[ref]; exists {
		return fmt.Errorf("detector %q already registered", ref)
	}
	s.detectors[ref] = detector
	return nil
}

func (s *Service) ApprovedDetectors() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	refs := make([]string, 0, len(s.detectors)+1)
	refs = append(refs, DetectorSensitiveWords)
	for ref := range s.detectors {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

// DetectorStreamCapabilities exposes only fixed-registry capability metadata.
// It contains no endpoint, credential or tenant-controlled value and lets the
// control plane explain why a detector is enforcement-capable or shadow-only.
func (s *Service) DetectorStreamCapabilities() map[string]StreamCapability {
	capabilities := make(map[string]StreamCapability)
	if s == nil {
		return capabilities
	}
	s.mu.RLock()
	for ref, detector := range s.detectors {
		capability := StreamCapability{}
		if streamDetector, ok := detector.(StreamCapableDetector); ok {
			capability = streamDetector.StreamCapability()
		}
		capabilities[ref] = capability
	}
	s.mu.RUnlock()
	sensitiveSnapshot, _ := s.sensitive.Snapshot()
	lookbehind := 0
	if sensitiveSnapshot != nil && sensitiveSnapshot.MaxWordBytes() > 0 {
		lookbehind = sensitiveSnapshot.MaxWordBytes() - 1
	}
	capabilities[DetectorSensitiveWords] = StreamCapability{
		Synchronous: lookbehind <= MaxStreamLookbehindBytes, LookbehindBytes: lookbehind,
	}
	return capabilities
}

func (s *Service) DetectorInputLimits() (localBytes, externalBytes, maxTexts int) {
	if s == nil {
		defaults := DefaultOptions()
		return defaults.LocalDetectorMaxBytes, defaults.ExternalDetectorMaxBytes, defaults.MaxDetectorTexts
	}
	return s.options.LocalDetectorMaxBytes, s.options.ExternalDetectorMaxBytes, s.options.MaxDetectorTexts
}

func (s *Service) IsDetectorApproved(ref string) bool {
	if ref == DetectorSensitiveWords {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.detectors[ref]
	return ok
}

func (s *Service) ValidatePolicy(policy Policy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if !s.IsDetectorApproved(policy.DetectorRef) {
		return fmt.Errorf("%w: detector %q is not in the platform registry", ErrInvalidPolicy, policy.DetectorRef)
	}
	if policy.Stage == StageStreamOutput && policy.Mode == ModeEnforce && policy.Action != ActionAudit {
		detector := s.detector(policy.DetectorRef)
		streamDetector, ok := detector.(StreamCapableDetector)
		if !ok {
			return fmt.Errorf("%w: detector %q is shadow-only for streaming", ErrInvalidPolicy, policy.DetectorRef)
		}
		capability := streamDetector.StreamCapability()
		if !capability.Synchronous || capability.LookbehindBytes < 0 || capability.LookbehindBytes > MaxStreamLookbehindBytes {
			return fmt.Errorf("%w: detector %q lacks bounded synchronous stream capability", ErrInvalidPolicy, policy.DetectorRef)
		}
	}
	if policy.DetectorRef == DetectorSensitiveWords && len(policy.Config) > 0 {
		var config map[string]json.RawMessage
		if err := json.Unmarshal(policy.Config, &config); err != nil || len(config) != 0 {
			return fmt.Errorf("%w: sensitive-word compatibility has no tenant config", ErrInvalidPolicy)
		}
	}
	if policy.DetectorRef == DetectorPII {
		if _, err := (piiSecretsDetector{}).Detect(context.Background(), Text{}, policy.Config); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidPolicy, err)
		}
	}
	return nil
}

func (s *Service) Refresh(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("guardrail service is unavailable")
	}
	if s.db == nil {
		s.snapshot.Store(emptySnapshot(time.Now().UTC()))
		return nil
	}
	rows, err := s.db.Query(ctx, `
		WITH active AS (
			SELECT id, org_id, project_id, api_key_id, name, model, stage,
			       detector_ref, action, mode, failure_mode, mandatory, config,
			       version, created_at, updated_at,
			       COUNT(*) OVER (PARTITION BY org_id) AS scope_count
			FROM guardrail_policies
			WHERE mode <> 'disabled'
		)
		SELECT id, org_id, project_id, api_key_id, name, model, stage,
		       detector_ref, action, mode, failure_mode, mandatory, config,
		       version, created_at, updated_at
		FROM active
		WHERE org_id IS NULL OR scope_count <= $1
		ORDER BY org_id NULLS FIRST, id ASC
		LIMIT $2`, s.options.MaxActivePoliciesPerOrg, s.options.MaxCachedPolicies+1)
	if err != nil {
		return fmt.Errorf("load guardrail policies: %w", err)
	}
	defer rows.Close()
	policies := make([]Policy, 0)
	for rows.Next() {
		var policy Policy
		if err := rows.Scan(&policy.ID, &policy.OrgID, &policy.ProjectID, &policy.APIKeyID, &policy.Name, &policy.Model,
			&policy.Stage, &policy.DetectorRef, &policy.Action, &policy.Mode, &policy.FailureMode,
			&policy.Mandatory, &policy.Config, &policy.Version, &policy.CreatedAt, &policy.UpdatedAt); err != nil {
			return fmt.Errorf("scan guardrail policy: %w", err)
		}
		policy.Ref = fmt.Sprintf("guardrail:%d", policy.ID)
		policies = append(policies, policy)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate guardrail policies: %w", err)
	}
	discoveryComplete := len(policies) <= s.options.MaxCachedPolicies
	if !discoveryComplete {
		// Never publish a partial tenant scope. Missing scopes fail closed while
		// the configured cache bound is exceeded.
		lastOrg := policies[s.options.MaxCachedPolicies].OrgID
		policies = policies[:s.options.MaxCachedPolicies]
		if lastOrg != nil {
			cut := len(policies)
			for cut > 0 && policies[cut-1].OrgID != nil && *policies[cut-1].OrgID == *lastOrg {
				cut--
			}
			policies = policies[:cut]
		}
	}

	excessRows, err := s.db.Query(ctx, `
		SELECT org_id
		FROM guardrail_policies
		WHERE org_id IS NOT NULL AND mode <> 'disabled'
		GROUP BY org_id
		HAVING COUNT(*) > $1
		ORDER BY org_id
		LIMIT $2`, s.options.MaxActivePoliciesPerOrg, s.options.MaxCachedTenantScopes+1)
	if err != nil {
		return fmt.Errorf("load over-limit guardrail tenant scopes: %w", err)
	}
	excess := make(map[int64]struct{})
	for excessRows.Next() {
		var orgID int64
		if err := excessRows.Scan(&orgID); err != nil {
			excessRows.Close()
			return fmt.Errorf("scan over-limit guardrail tenant scope: %w", err)
		}
		excess[orgID] = struct{}{}
	}
	if err := excessRows.Err(); err != nil {
		excessRows.Close()
		return fmt.Errorf("iterate over-limit guardrail tenant scopes: %w", err)
	}
	excessRows.Close()
	if len(excess) > s.options.MaxCachedTenantScopes {
		discoveryComplete = false
	}
	return s.publishPolicyRefresh(time.Now().UTC(), policies, excess, discoveryComplete)
}

func (s *Service) publishPolicyRefresh(now time.Time, policies []Policy, excessOrgs map[int64]struct{}, discoveryComplete bool) error {
	if s == nil {
		return fmt.Errorf("guardrail service is unavailable")
	}
	current := s.snapshot.Load()
	if current == nil {
		current = emptySnapshot(time.Time{})
	}
	platformPolicies := make([]Policy, 0)
	tenantPolicies := make(map[int64][]Policy)
	for _, policy := range policies {
		if policy.Mode == ModeDisabled {
			continue
		}
		if policy.OrgID == nil {
			platformPolicies = append(platformPolicies, policy)
			continue
		}
		tenantPolicies[*policy.OrgID] = append(tenantPolicies[*policy.OrgID], policy)
	}

	nextPlatform := current.platform
	var fatalErrors []error
	if len(platformPolicies) > s.options.MaxActivePlatformPolicies {
		fatalErrors = append(fatalErrors, fmt.Errorf("platform active policy limit %d exceeded", s.options.MaxActivePlatformPolicies))
	} else if err := s.validatePolicyScope(platformPolicies); err != nil {
		fatalErrors = append(fatalErrors, fmt.Errorf("validate platform guardrail scope: %w", err))
	} else {
		nextPlatform = newScopedSnapshot(platformPolicies, now)
	}

	nextTenants := make(map[int64]scopedSnapshot)
	if !discoveryComplete {
		for orgID, tenant := range current.tenants {
			nextTenants[orgID] = tenant
		}
		log.Printf("[guardrail] tenant snapshot discovery exceeded cache bounds; known tenants remain isolated and unknown tenants fail closed")
	}
	orgIDs := make([]int64, 0, len(tenantPolicies)+len(excessOrgs))
	seen := make(map[int64]struct{})
	for orgID := range tenantPolicies {
		seen[orgID] = struct{}{}
		orgIDs = append(orgIDs, orgID)
	}
	for orgID := range excessOrgs {
		if _, ok := seen[orgID]; !ok {
			orgIDs = append(orgIDs, orgID)
		}
	}
	sort.Slice(orgIDs, func(i, j int) bool { return orgIDs[i] < orgIDs[j] })
	for index, orgID := range orgIDs {
		if index >= s.options.MaxCachedTenantScopes {
			discoveryComplete = false
			log.Printf("[guardrail] tenant snapshot cache limit %d reached; omitted tenants fail closed locally", s.options.MaxCachedTenantScopes)
			break
		}
		orgPolicies := tenantPolicies[orgID]
		_, explicitlyExcess := excessOrgs[orgID]
		var scopeErr error
		switch {
		case explicitlyExcess || len(orgPolicies) > s.options.MaxActivePoliciesPerOrg:
			scopeErr = fmt.Errorf("active policy limit %d exceeded", s.options.MaxActivePoliciesPerOrg)
		case len(platformPolicies)+len(orgPolicies) > s.options.MaxCachedPolicies:
			scopeErr = fmt.Errorf("policy cache limit %d exceeded", s.options.MaxCachedPolicies)
		default:
			scopeErr = s.validatePolicyScope(orgPolicies)
		}
		if scopeErr != nil {
			if previous, ok := current.tenants[orgID]; ok {
				nextTenants[orgID] = previous
			} else {
				nextTenants[orgID] = scopedSnapshot{version: "unavailable"}
			}
			log.Printf("[guardrail] tenant %d snapshot refresh rejected; retaining only that tenant's prior scope: %v", orgID, scopeErr)
			continue
		}
		nextTenants[orgID] = newScopedSnapshot(orgPolicies, now)
	}

	s.snapshot.Store(&snapshot{
		platform: nextPlatform, tenants: nextTenants,
		tenantDiscoveryComplete: discoveryComplete,
	})
	return errors.Join(fatalErrors...)
}

func (s *Service) validatePolicyScope(policies []Policy) error {
	for _, policy := range policies {
		if err := s.ValidatePolicy(policy); err != nil {
			return fmt.Errorf("validate guardrail policy %d: %w", policy.ID, err)
		}
	}
	return nil
}

func newScopedSnapshot(policies []Policy, loadedAt time.Time) scopedSnapshot {
	cloned := make([]Policy, len(policies))
	for index, policy := range policies {
		cloned[index] = cloneSnapshotPolicy(policy)
	}
	return scopedSnapshot{policies: cloned, loadedAt: loadedAt, version: policySetVersion(cloned)}
}

func cloneSnapshotPolicy(policy Policy) Policy {
	clone := policy
	clone.OrgID = cloneInt64Pointer(policy.OrgID)
	clone.ProjectID = cloneInt64Pointer(policy.ProjectID)
	clone.APIKeyID = cloneInt64Pointer(policy.APIKeyID)
	clone.Config = append(json.RawMessage(nil), policy.Config...)
	return clone
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func (s *Service) RunRefreshLoop(ctx context.Context) {
	ticker := time.NewTicker(s.options.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(ctx, s.options.RefreshInterval)
			if err := s.Refresh(refreshCtx); err != nil {
				log.Printf("[guardrail] snapshot refresh failed; retaining pinned snapshot: %v", err)
			}
			cancel()
		}
	}
}

type Pinned struct {
	service    *Service
	subject    Subject
	policies   []Policy
	version    string
	stale      bool
	staleScope string
	detectors  map[string]Detector
}

// PolicySetVersion exposes only the immutable metadata identity used to bind
// downstream caches and evidence; it never exposes policy content.
func (p *Pinned) PolicySetVersion() string {
	if p == nil || p.version == "" {
		return "none"
	}
	return p.version
}

func (s *Service) Pin(subject Subject) *Pinned {
	if s == nil {
		return &Pinned{stale: true, version: "unavailable"}
	}
	current := s.snapshot.Load()
	if current == nil {
		return &Pinned{service: s, subject: subject, stale: true, version: "unavailable"}
	}
	platformPolicies := make([]Policy, 0)
	for _, policy := range current.platform.policies {
		if policy.Matches(subject) {
			platformPolicies = append(platformPolicies, policy)
		}
	}
	tenantSnapshot, tenantFound := current.tenants[subject.OrgID]
	tenantPolicies := make([]Policy, 0)
	if tenantFound {
		for _, policy := range tenantSnapshot.policies {
			if policy.Matches(subject) {
				tenantPolicies = append(tenantPolicies, policy)
			}
		}
	}
	policies := append(append([]Policy(nil), platformPolicies...), tenantPolicies...)
	detectors := make(map[string]Detector)
	s.mu.RLock()
	for ref, detector := range s.detectors {
		detectors[ref] = detector
	}
	s.mu.RUnlock()
	sensitiveSnapshot, sensitiveVersion := s.sensitive.Snapshot()
	if sensitiveSnapshot != nil {
		detectors[DetectorSensitiveWords] = &sensitiveWordDetector{
			filter: sensitiveSnapshot, version: sensitiveVersion, maxWordBytes: sensitiveSnapshot.MaxWordBytes(),
		}
	}
	if subject.Endpoint == "chat" && sensitiveSnapshot != nil && sensitiveSnapshot.WordCount() > 0 {
		policies = append([]Policy{{
			Ref: "builtin:sensitive-words", Name: "Sensitive-word compatibility", Version: 1,
			Stage: StageInput, DetectorRef: DetectorSensitiveWords, Action: ActionBlock,
			Mode: ModeEnforce, FailureMode: FailureFailClosed, Mandatory: true, Builtin: true,
		}}, policies...)
	}
	version := combinedPolicySetVersion(current.platform, tenantSnapshot, tenantFound)
	if sensitiveVersion != "" {
		sum := sha256.Sum256([]byte(version + "|" + sensitiveVersion))
		version = hex.EncodeToString(sum[:8])
	}
	staleScope := ""
	now := time.Now()
	if snapshotScopeStale(current.platform, now, s.options.SnapshotMaxAge) || len(platformPolicies) > s.options.MaxMatchingPolicies {
		staleScope = "platform"
	} else if (tenantFound && snapshotScopeStale(tenantSnapshot, now, s.options.SnapshotMaxAge)) ||
		(!tenantFound && !current.tenantDiscoveryComplete) || len(policies) > s.options.MaxMatchingPolicies {
		staleScope = "tenant"
	}
	return &Pinned{
		service: s, subject: subject, policies: policies, version: version,
		stale: staleScope != "", staleScope: staleScope,
		detectors: detectors,
	}
}

func snapshotScopeStale(scope scopedSnapshot, now time.Time, maxAge time.Duration) bool {
	return scope.loadedAt.IsZero() || now.Sub(scope.loadedAt) > maxAge
}

func combinedPolicySetVersion(platform, tenant scopedSnapshot, tenantFound bool) string {
	if !tenantFound {
		if platform.version != "" {
			return platform.version
		}
		return policySetVersion(nil)
	}
	sum := sha256.Sum256([]byte(platform.version + "|" + tenant.version))
	return hex.EncodeToString(sum[:8])
}

func (p *Pinned) StreamAdvisoryPolicyIdentities() []PolicyIdentity {
	if p == nil || p.service == nil || p.stale {
		return nil
	}
	identities := make([]PolicyIdentity, 0)
	for _, policy := range p.policies {
		if policy.Stage == StageStreamOutput && policy.Mode != ModeDisabled && !policyRequiresPreflush(policy) {
			identities = append(identities, PolicyIdentity{Ref: policy.Ref, Version: policy.Version})
		}
	}
	return identities
}

func (p *Pinned) Evaluate(ctx context.Context, stage string, texts []Text) Result {
	return p.evaluateSelected(ctx, stage, texts, true, func(Policy) bool { return true }, true)
}

// EvaluateWithCompleteness evaluates text extracted from an envelope whose
// known model-text fields may have an unsupported shape. Preventive enforced
// policies must not treat a partial extraction as complete inspection.
func (p *Pinned) EvaluateWithCompleteness(ctx context.Context, stage string, texts []Text, complete bool) Result {
	return p.evaluateSelected(ctx, stage, texts, complete, func(Policy) bool { return true }, true)
}

func (p *Pinned) EvaluateStreamEnforced(ctx context.Context, texts []Text) Result {
	return p.evaluateSelected(ctx, StageStreamOutput, texts, true, policyRequiresPreflush, true)
}

func (p *Pinned) EvaluateStreamAdvisory(ctx context.Context, texts []Text) Result {
	return p.evaluateSelected(ctx, StageStreamOutput, texts, true, func(policy Policy) bool {
		return !policyRequiresPreflush(policy)
	}, false)
}

func policyRequiresPreflush(policy Policy) bool {
	return policy.Stage == StageStreamOutput && policy.Mode == ModeEnforce && policy.Action != ActionAudit
}

func (p *Pinned) StreamProfile() StreamProfile {
	profile := StreamProfile{PolicySetVersion: "unavailable"}
	if p == nil {
		profile.Enforced = true
		return profile
	}
	if p.version != "" {
		profile.PolicySetVersion = p.version
	}
	if p.service == nil || p.stale {
		profile.Enforced = true
		return profile
	}
	for _, policy := range p.policies {
		if policy.Stage != StageStreamOutput {
			continue
		}
		if policy.Mode == ModeDisabled {
			continue
		}
		if !policyRequiresPreflush(policy) {
			profile.Advisory = true
			continue
		}
		profile.Enforced = true
		detector := p.detectors[policy.DetectorRef]
		streamDetector, ok := detector.(StreamCapableDetector)
		if !ok {
			profile.LookbehindBytes = MaxStreamLookbehindBytes
			continue
		}
		capability := streamDetector.StreamCapability()
		if !capability.Synchronous || capability.LookbehindBytes < 0 || capability.LookbehindBytes > MaxStreamLookbehindBytes {
			profile.LookbehindBytes = MaxStreamLookbehindBytes
			continue
		}
		if capability.LookbehindBytes > profile.LookbehindBytes {
			profile.LookbehindBytes = capability.LookbehindBytes
		}
	}
	return profile
}

func (p *Pinned) evaluateSelected(ctx context.Context, stage string, texts []Text, complete bool, selected func(Policy) bool, failClosedUnavailable bool) Result {
	version := "unavailable"
	if p != nil && p.version != "" {
		version = p.version
	}
	result := Result{Decision: DecisionAllow, PolicySetVersion: version, Texts: append([]Text(nil), texts...)}
	if p == nil || p.service == nil || p.stale {
		if failClosedUnavailable {
			result.Decision = DecisionBlock
		}
		result.Degraded = true
		if failClosedUnavailable {
			result.BlockedBy = guardrailSnapshotRef(p)
		}
		result.Receipts = append(result.Receipts, Receipt{
			PolicyRef: guardrailSnapshotRef(p), PolicyVersion: 1, Stage: stage, DetectorRef: "policy_snapshot",
			Mode:    map[bool]string{true: ModeEnforce, false: ModeShadow}[failClosedUnavailable],
			Action:  map[bool]string{true: ActionBlock, false: ActionAudit}[failClosedUnavailable],
			Outcome: OutcomeUnavailable, FailureMode: map[bool]string{true: FailureFailClosed, false: FailureDegradedAllow}[failClosedUnavailable],
			Mandatory: failClosedUnavailable, Degraded: true,
		})
		return result
	}
	if stage != StageInput && stage != StageNonStreamOutput && stage != StageStreamOutput {
		result.Decision = DecisionBlock
		result.BlockedBy = "guardrail:invalid-stage"
		return result
	}
	detectionCache := make(map[detectionCacheKey]detectionCacheValue)
	externalCtx, cancelExternal := context.WithTimeout(ctx, p.service.options.ExternalDeadline)
	defer cancelExternal()
	for _, policy := range p.policies {
		if policy.Stage != stage || (selected != nil && !selected(policy)) {
			continue
		}
		detector := p.detectors[policy.DetectorRef]
		if stage == StageStreamOutput && policyRequiresPreflush(policy) {
			streamDetector, ok := detector.(StreamCapableDetector)
			if !ok {
				detector = nil
			} else {
				capability := streamDetector.StreamCapability()
				if !capability.Synchronous || capability.LookbehindBytes < 0 || capability.LookbehindBytes > MaxStreamLookbehindBytes {
					detector = nil
				}
			}
		}
		receipt, transformed, blocked := p.service.evaluatePolicy(ctx, externalCtx, policy, result.Texts, complete, detector, detectionCache)
		result.Receipts = append(result.Receipts, receipt)
		if transformed != nil {
			result.Texts = transformed
			result.Transformed = true
		}
		if receipt.Degraded {
			result.Degraded = true
		}
		if blocked {
			result.Decision = DecisionBlock
			result.BlockedBy = policy.Ref
			return result
		}
	}
	return result
}

func guardrailSnapshotRef(p *Pinned) string {
	if p != nil && (p.staleScope == "platform" || p.staleScope == "tenant") {
		return "guardrail:snapshot:" + p.staleScope
	}
	return "guardrail:snapshot"
}

func (s *Service) evaluatePolicy(ctx, externalCtx context.Context, policy Policy, texts []Text, complete bool, pinnedDetector Detector, cache map[detectionCacheKey]detectionCacheValue) (Receipt, []Text, bool) {
	receipt := Receipt{
		PolicyRef: policy.Ref, PolicyVersion: policy.Version, Stage: policy.Stage,
		DetectorRef: policy.DetectorRef, Mode: policy.Mode, Action: policy.Action,
		FailureMode: policy.FailureMode, Mandatory: policy.Mandatory, Outcome: OutcomePass,
		Categories: make(map[string]int),
	}
	detector := pinnedDetector
	if !complete {
		if detector != nil {
			receipt.DetectorVersion = detector.Version()
		}
		receipt.Outcome = OutcomeIndeterminate
		receipt.Degraded = true
		return receipt, nil, policy.Mode == ModeEnforce && policy.Action != ActionAudit
	}
	if detector == nil {
		receipt.Outcome = OutcomeUnavailable
		receipt.Degraded = true
		return receipt, nil, policy.Mode == ModeEnforce && policy.FailureMode == FailureFailClosed
	}
	detectorVersion := detector.Version()
	receipt.DetectorVersion = detectorVersion
	configKey := string(policy.Config)
	started := time.Now()
	if len(texts) == 0 {
		receipt.LatencyMS = time.Since(started).Milliseconds()
		receipt.Outcome = OutcomeSkipped
		receipt.Degraded = true
		return receipt, nil, policy.Mode == ModeEnforce && policy.FailureMode == FailureFailClosed
	}
	if len(texts) > s.options.MaxDetectorTexts {
		receipt.LatencyMS = time.Since(started).Milliseconds()
		receipt.Outcome = OutcomeIndeterminate
		receipt.Degraded = true
		return receipt, nil, policy.Mode == ModeEnforce && policy.FailureMode == FailureFailClosed
	}
	if limit := s.detectorInputLimit(policy.DetectorRef); limit > 0 && textBytes(texts) > limit {
		receipt.LatencyMS = time.Since(started).Milliseconds()
		receipt.Outcome = OutcomeIndeterminate
		receipt.Degraded = true
		return receipt, nil, policy.Mode == ModeEnforce && policy.FailureMode == FailureFailClosed
	}
	detectorCtx := ctx
	external := strings.HasPrefix(policy.DetectorRef, "external:")
	if external && !allDetectionsCached(policy.DetectorRef, detectorVersion, texts, configKey, cache) {
		select {
		case s.externalSlots <- struct{}{}:
			defer func() { <-s.externalSlots }()
		case <-externalCtx.Done():
			receipt.LatencyMS = time.Since(started).Milliseconds()
			receipt.Outcome = OutcomeUnavailable
			receipt.Degraded = true
			return receipt, nil, policy.Mode == ModeEnforce && policy.FailureMode == FailureFailClosed
		default:
			receipt.LatencyMS = time.Since(started).Milliseconds()
			receipt.Outcome = OutcomeUnavailable
			receipt.Degraded = true
			return receipt, nil, policy.Mode == ModeEnforce && policy.FailureMode == FailureFailClosed
		}
		detectorCtx = externalCtx
	}
	allFindings := make([][]Finding, len(texts))
	for index, text := range texts {
		findings, err := s.detect(detectorCtx, policy.DetectorRef, detectorVersion, configKey, detector, text, policy.Config, cache)
		if err != nil {
			receipt.LatencyMS = time.Since(started).Milliseconds()
			if errors.Is(err, ErrDetectionIncomplete) {
				receipt.Outcome = OutcomeIndeterminate
				receipt.Degraded = true
				return receipt, nil, policy.Mode == ModeEnforce && policy.Action != ActionAudit
			}
			receipt.Outcome = OutcomeUnavailable
			receipt.Degraded = true
			return receipt, nil, policy.Mode == ModeEnforce && policy.FailureMode == FailureFailClosed
		}
		allFindings[index] = findings
		for _, finding := range findings {
			evidenceCategory := finding.Category
			if strings.HasPrefix(policy.DetectorRef, "external:") {
				// External category strings are detector-controlled and therefore
				// cannot be copied into durable evidence. The policy and detector
				// refs retain provenance without creating a raw-content side channel.
				evidenceCategory = "external_finding"
			}
			receipt.MatchCount++
			receipt.Categories[evidenceCategory]++
			if len(receipt.Fingerprints) < 16 && finding.Start >= 0 && finding.End <= len(text.Content) && finding.End > finding.Start {
				receipt.Fingerprints = append(receipt.Fingerprints, s.fingerprint(evidenceCategory, text.Content[finding.Start:finding.End]))
			}
		}
	}
	receipt.LatencyMS = time.Since(started).Milliseconds()
	if receipt.MatchCount == 0 {
		return receipt, nil, false
	}
	receipt.Outcome = OutcomeMatch
	if policy.Mode == ModeShadow || policy.Action == ActionAudit {
		return receipt, nil, false
	}
	if policy.Action == ActionBlock {
		return receipt, nil, true
	}
	transformed := append([]Text(nil), texts...)
	for index := range transformed {
		redacted, ok := redactText(transformed[index].Content, allFindings[index])
		if !ok {
			receipt.Outcome = OutcomeIndeterminate
			receipt.Degraded = true
			return receipt, nil, policy.FailureMode == FailureFailClosed
		}
		transformed[index].Content = redacted
	}
	receipt.Outcome = OutcomeTransform
	return receipt, transformed, false
}

func (s *Service) detector(ref string) Detector {
	if ref == DetectorSensitiveWords {
		version := "sensitive-words.unversioned"
		if s.sensitive != nil {
			version = s.sensitive.Version()
		}
		maxWordBytes := 0
		if s.sensitive != nil {
			maxWordBytes = s.sensitive.MaxWordBytes()
		}
		return &sensitiveWordDetector{filter: s.sensitive, version: version, maxWordBytes: maxWordBytes}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.detectors[ref]
}

type detectionCacheKey struct {
	detectorRef     string
	detectorVersion string
	path            string
	content         string
	config          string
}

type detectionCacheValue struct {
	findings []Finding
	err      error
}

func (s *Service) detectorInputLimit(ref string) int {
	switch {
	case ref == DetectorPII:
		return s.options.LocalDetectorMaxBytes
	case strings.HasPrefix(ref, "external:"):
		return s.options.ExternalDetectorMaxBytes
	default:
		// The built-in sensitive-word compatibility path intentionally preserves
		// its existing full-request behavior during this upgrade.
		return 0
	}
}

func textBytes(texts []Text) int {
	total := 0
	for _, text := range texts {
		if len(text.Content) > int(^uint(0)>>1)-total {
			return int(^uint(0) >> 1)
		}
		total += len(text.Content)
	}
	return total
}

func (s *Service) detect(ctx context.Context, ref, detectorVersion, configKey string, detector Detector, text Text, config json.RawMessage, cache map[detectionCacheKey]detectionCacheValue) ([]Finding, error) {
	key := detectorDetectionCacheKey(ref, detectorVersion, text, configKey)
	if cached, ok := cache[key]; ok {
		return append([]Finding(nil), cached.findings...), cached.err
	}
	findings, err := detector.Detect(ctx, text, config)
	findings, err = normalizeFindings(text.Content, findings, err)
	cache[key] = detectionCacheValue{findings: append([]Finding(nil), findings...), err: err}
	return findings, err
}

func detectorDetectionCacheKey(ref, detectorVersion string, text Text, configKey string) detectionCacheKey {
	return detectionCacheKey{
		detectorRef: ref, detectorVersion: detectorVersion, path: text.Path,
		content: text.Content, config: configKey,
	}
}

func allDetectionsCached(ref, detectorVersion string, texts []Text, configKey string, cache map[detectionCacheKey]detectionCacheValue) bool {
	for _, text := range texts {
		if _, ok := cache[detectorDetectionCacheKey(ref, detectorVersion, text, configKey)]; !ok {
			return false
		}
	}
	return true
}

func (s *Service) fingerprint(category, value string) string {
	mac := hmac.New(sha256.New, s.fingerprintSecret)
	_, _ = mac.Write([]byte("unirouter.guardrail-finding.v1\x00"))
	_, _ = mac.Write([]byte(category))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil)[:12])
}

func redactText(value string, findings []Finding) (string, bool) {
	if len(findings) == 0 {
		return value, true
	}
	var out strings.Builder
	last := 0
	for _, finding := range findings {
		if finding.Start < last || finding.End <= finding.Start || finding.End > len(value) {
			return "", false
		}
		out.WriteString(value[last:finding.Start])
		out.WriteString("[REDACTED]")
		last = finding.End
	}
	out.WriteString(value[last:])
	return out.String(), true
}

func policySetVersion(policies []Policy) string {
	parts := make([]string, 0, len(policies))
	for _, policy := range policies {
		parts = append(parts, fmt.Sprintf("%s:%d", policy.Ref, policy.Version))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:8])
}

func SafeReceiptMetadata(result Result) map[string]interface{} {
	receipts := make([]map[string]interface{}, 0, len(result.Receipts))
	for _, receipt := range result.Receipts {
		receipts = append(receipts, map[string]interface{}{
			"policy_ref": receipt.PolicyRef, "policy_version": receipt.PolicyVersion,
			"detector_ref": receipt.DetectorRef, "detector_version": receipt.DetectorVersion,
			"mode": receipt.Mode, "action": receipt.Action, "outcome": receipt.Outcome,
			"latency_ms": receipt.LatencyMS, "match_count": receipt.MatchCount,
			"categories": receipt.Categories, "fingerprints": receipt.Fingerprints,
			"degraded": receipt.Degraded, "mandatory": receipt.Mandatory,
		})
	}
	return map[string]interface{}{
		"decision": result.Decision, "policy_set_version": result.PolicySetVersion,
		"transformed": result.Transformed, "degraded": result.Degraded,
		"blocked_by": result.BlockedBy, "receipts": receipts,
	}
}
