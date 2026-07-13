package guardrail

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	RefreshInterval          time.Duration
	SnapshotMaxAge           time.Duration
	ExternalDeadline         time.Duration
	ExternalConcurrency      int
	MaxMatchingPolicies      int
	MaxDetectorTexts         int
	LocalDetectorMaxBytes    int
	ExternalDetectorMaxBytes int
}

func DefaultOptions() Options {
	return Options{
		RefreshInterval: time.Second, SnapshotMaxAge: 30 * time.Second,
		ExternalDeadline: time.Second, ExternalConcurrency: 32, MaxMatchingPolicies: 64,
		MaxDetectorTexts: 256, LocalDetectorMaxBytes: 128 * 1024, ExternalDetectorMaxBytes: 256 * 1024,
	}
}

func (o Options) Validate() error {
	if o.RefreshInterval <= 0 || o.SnapshotMaxAge < o.RefreshInterval || o.ExternalDeadline <= 0 ||
		o.ExternalDeadline > time.Second || o.ExternalConcurrency <= 0 || o.ExternalConcurrency > 1024 ||
		o.MaxMatchingPolicies <= 0 || o.MaxMatchingPolicies > 256 ||
		o.MaxDetectorTexts <= 0 || o.MaxDetectorTexts > 1024 ||
		o.LocalDetectorMaxBytes <= 0 || o.LocalDetectorMaxBytes > 1024*1024 ||
		o.ExternalDetectorMaxBytes <= 0 || o.ExternalDetectorMaxBytes > 1024*1024 {
		return fmt.Errorf("invalid guardrail service options")
	}
	return nil
}

type snapshot struct {
	policies []Policy
	loadedAt time.Time
	version  string
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
	service.snapshot.Store(&snapshot{loadedAt: time.Now().UTC(), version: policySetVersion(nil)})
	return service, nil
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
		s.snapshot.Store(&snapshot{loadedAt: time.Now().UTC(), version: policySetVersion(nil)})
		return nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, org_id, project_id, api_key_id, name, model, stage,
		       detector_ref, action, mode, failure_mode, mandatory, config,
		       version, created_at, updated_at
		FROM guardrail_policies
		ORDER BY mandatory DESC, id ASC`)
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
		if err := s.ValidatePolicy(policy); err != nil {
			return fmt.Errorf("validate guardrail policy %d: %w", policy.ID, err)
		}
		policies = append(policies, policy)
		if len(policies) > 4096 {
			return fmt.Errorf("guardrail policy snapshot exceeds 4096 rows")
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate guardrail policies: %w", err)
	}
	s.snapshot.Store(&snapshot{policies: policies, loadedAt: time.Now().UTC(), version: policySetVersion(policies)})
	return nil
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
	service   *Service
	subject   Subject
	policies  []Policy
	version   string
	stale     bool
	detectors map[string]Detector
}

func (s *Service) Pin(subject Subject) *Pinned {
	if s == nil {
		return &Pinned{stale: true, version: "unavailable"}
	}
	current := s.snapshot.Load()
	if current == nil {
		return &Pinned{service: s, subject: subject, stale: true, version: "unavailable"}
	}
	policies := make([]Policy, 0)
	for _, policy := range current.policies {
		if policy.Matches(subject) {
			policies = append(policies, policy)
		}
	}
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
	version := current.version
	if sensitiveVersion != "" {
		sum := sha256.Sum256([]byte(current.version + "|" + sensitiveVersion))
		version = hex.EncodeToString(sum[:8])
	}
	return &Pinned{
		service: s, subject: subject, policies: policies, version: version,
		stale:     current.loadedAt.IsZero() || time.Since(current.loadedAt) > s.options.SnapshotMaxAge || len(policies) > s.options.MaxMatchingPolicies,
		detectors: detectors,
	}
}

func (p *Pinned) Evaluate(ctx context.Context, stage string, texts []Text) Result {
	return p.evaluateSelected(ctx, stage, texts, func(Policy) bool { return true }, true)
}

func (p *Pinned) EvaluateStreamEnforced(ctx context.Context, texts []Text) Result {
	return p.evaluateSelected(ctx, StageStreamOutput, texts, policyRequiresPreflush, true)
}

func (p *Pinned) EvaluateStreamAdvisory(ctx context.Context, texts []Text) Result {
	return p.evaluateSelected(ctx, StageStreamOutput, texts, func(policy Policy) bool {
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

func (p *Pinned) evaluateSelected(ctx context.Context, stage string, texts []Text, selected func(Policy) bool, failClosedUnavailable bool) Result {
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
			result.BlockedBy = "guardrail:snapshot"
		}
		result.Receipts = append(result.Receipts, Receipt{
			PolicyRef: "guardrail:snapshot", PolicyVersion: 1, Stage: stage, DetectorRef: "policy_snapshot",
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
		receipt, transformed, blocked := p.service.evaluatePolicy(ctx, externalCtx, policy, result.Texts, detector, detectionCache)
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

func (s *Service) evaluatePolicy(ctx, externalCtx context.Context, policy Policy, texts []Text, pinnedDetector Detector, cache map[detectionCacheKey]detectionCacheValue) (Receipt, []Text, bool) {
	receipt := Receipt{
		PolicyRef: policy.Ref, PolicyVersion: policy.Version, Stage: policy.Stage,
		DetectorRef: policy.DetectorRef, Mode: policy.Mode, Action: policy.Action,
		FailureMode: policy.FailureMode, Mandatory: policy.Mandatory, Outcome: OutcomePass,
		Categories: make(map[string]int),
	}
	detector := pinnedDetector
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
