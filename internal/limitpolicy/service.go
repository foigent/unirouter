package limitpolicy

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/workloadadmission"
)

const maxLeaseTTL = 24 * time.Hour

type Options struct {
	// These compatibility names now define weighted workload capacity units.
	// Explicit database policy concurrency limits continue to count requests.
	GlobalConcurrency   int64
	OrgConcurrency      int64
	RefreshInterval     time.Duration
	SnapshotMaxAge      time.Duration
	TerminalGrace       time.Duration
	FallbackTTL         time.Duration
	FallbackGlobalCap   int
	MaxMatchingPolicies int
	Workload            workloadadmission.Options
}

func DefaultOptions() Options {
	workload := workloadadmission.DefaultOptions()
	return Options{
		GlobalConcurrency:   100,
		OrgConcurrency:      10,
		RefreshInterval:     5 * time.Second,
		SnapshotMaxAge:      30 * time.Second,
		TerminalGrace:       5 * time.Minute,
		FallbackTTL:         2 * time.Second,
		FallbackGlobalCap:   5,
		MaxMatchingPolicies: 32,
		Workload:            workload,
	}
}

type policySnapshot struct {
	policies            []Policy
	labels              map[int64][]string
	labelsAuthoritative bool
	index               policyIndex
	loadedAt            time.Time
}

type policyIndex struct {
	globals   []Policy
	byOrg     map[int64][]Policy
	byProject map[int64][]Policy
	byAPIKey  map[int64][]Policy
	byModel   map[string][]Policy
	byLabel   map[string][]Policy
}

type fallbackLease struct {
	expiresAt atomic.Int64
	remaining atomic.Int32
}

type Service struct {
	db       db.DBQuerier
	rdb      *redis.Client
	options  Options
	nodeID   string
	snapshot atomic.Pointer[policySnapshot]

	fallback sync.Map
	workload *workloadadmission.Service
}

func NewService(q db.DBQuerier, rdb *redis.Client, options Options) (*Service, error) {
	if rdb == nil {
		return nil, fmt.Errorf("%w: Redis client is required", ErrInvalidPolicy)
	}
	if options.GlobalConcurrency <= 0 || options.OrgConcurrency <= 0 || options.RefreshInterval <= 0 || options.SnapshotMaxAge <= 0 ||
		options.TerminalGrace <= 0 || options.FallbackTTL <= 0 || options.FallbackTTL > 5*time.Second ||
		options.FallbackGlobalCap <= 0 || options.FallbackGlobalCap > 10 ||
		options.MaxMatchingPolicies <= 0 || options.MaxMatchingPolicies > 128 {
		return nil, ErrInvalidPolicy
	}
	workloadOptions := options.Workload
	workloadOptions.GlobalCapacity = options.GlobalConcurrency
	workloadOptions.OrgCapacity = options.OrgConcurrency
	if workloadOptions.GlobalShortReserve >= workloadOptions.GlobalCapacity {
		workloadOptions.GlobalShortReserve = workloadOptions.GlobalCapacity / 10
	}
	if workloadOptions.OrgShortReserve >= workloadOptions.OrgCapacity {
		workloadOptions.OrgShortReserve = workloadOptions.OrgCapacity / 5
	}
	workloadService, err := workloadadmission.NewService(rdb, workloadOptions)
	if err != nil {
		return nil, fmt.Errorf("%w: workload admission: %v", ErrInvalidPolicy, err)
	}
	nodeBytes := make([]byte, 12)
	if _, err := rand.Read(nodeBytes); err != nil {
		return nil, fmt.Errorf("generate policy node identity: %w", err)
	}
	service := &Service{
		db: q, rdb: rdb, options: options, nodeID: hex.EncodeToString(nodeBytes), workload: workloadService,
	}
	service.snapshot.Store(buildPolicySnapshot([]Policy{}, map[int64][]string{}, false, time.Now()))
	return service, nil
}

// TerminalStateTTL returns the exact lifetime of the Redis operation state
// that a durable terminal hook may need to finalize. It exposes duration only;
// callers never receive Redis keys or admission authority.
func (s *Service) TerminalStateTTL(leaseTTL time.Duration) time.Duration {
	if leaseTTL <= 0 {
		leaseTTL = 5 * time.Minute
	}
	if leaseTTL > maxLeaseTTL {
		leaseTTL = maxLeaseTTL
	}
	if s == nil || s.options.TerminalGrace <= 0 {
		return leaseTTL + 10*time.Minute
	}
	return leaseTTL + s.options.TerminalGrace*2
}

func (s *Service) Refresh(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, org_id, project_id, api_key_id, name, model, policy_label,
		       mode, failure_mode, rps_limit, rpm_limit, tpm_limit,
		       concurrency_limit, version, updated_at
		FROM distributed_limit_policies
		ORDER BY id ASC`)
	if err != nil {
		return fmt.Errorf("load distributed limit policies: %w", err)
	}
	defer rows.Close()

	policies := make([]Policy, 0)
	for rows.Next() {
		var policy Policy
		var orgID, projectID, apiKeyID sql.NullInt64
		var rps, rpm, tpm, concurrency sql.NullInt64
		if err := rows.Scan(
			&policy.ID, &orgID, &projectID, &apiKeyID, &policy.Name, &policy.Model,
			&policy.PolicyLabel, &policy.Mode, &policy.FailureMode, &rps, &rpm,
			&tpm, &concurrency, &policy.Version, &policy.UpdatedAt,
		); err != nil {
			return fmt.Errorf("scan distributed limit policy: %w", err)
		}
		policy.Ref = fmt.Sprintf("policy:%d", policy.ID)
		policy.OrgID = nullableInt64(orgID)
		policy.ProjectID = nullableInt64(projectID)
		policy.APIKeyID = nullableInt64(apiKeyID)
		policy.RPSLimit = nullValue(rps)
		policy.RPMLimit = nullValue(rpm)
		policy.TPMLimit = nullValue(tpm)
		policy.ConcurrencyLimit = nullValue(concurrency)
		policy.RuntimeBucket = policyRuntimeBucket(policy)
		if err := policy.Validate(); err != nil {
			return fmt.Errorf("policy %d: %w", policy.ID, err)
		}
		policies = append(policies, policy)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate distributed limit policies: %w", err)
	}
	rows.Close()

	labelRows, err := s.db.Query(ctx, `SELECT id, policy_labels FROM api_keys WHERE policy_labels <> '' ORDER BY id ASC`)
	if err != nil {
		return fmt.Errorf("load API-key policy labels: %w", err)
	}
	defer labelRows.Close()
	labels := make(map[int64][]string)
	for labelRows.Next() {
		var keyID int64
		var raw string
		if err := labelRows.Scan(&keyID, &raw); err != nil {
			return fmt.Errorf("scan API-key policy labels: %w", err)
		}
		parsed, err := parseStoredLabels(raw)
		if err != nil {
			return fmt.Errorf("API key %d policy labels: %w", keyID, err)
		}
		labels[keyID] = parsed
	}
	if err := labelRows.Err(); err != nil {
		return fmt.Errorf("iterate API-key policy labels: %w", err)
	}
	s.snapshot.Store(buildPolicySnapshot(policies, labels, true, time.Now()))
	return nil
}

func (s *Service) RunRefreshLoop(ctx context.Context) {
	if s == nil {
		return
	}
	ticker := time.NewTicker(s.options.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pruneFallback(time.Now())
			if s.db == nil {
				continue
			}
			refreshCtx, cancel := context.WithTimeout(ctx, s.options.RefreshInterval)
			if err := s.Refresh(refreshCtx); err != nil {
				log.Printf("[limit-policy] snapshot refresh failed; retaining last-known database snapshot: %v", err)
			}
			cancel()
		}
	}
}

func (s *Service) Policies() []Policy {
	if s == nil {
		return nil
	}
	snapshot := s.snapshot.Load()
	if snapshot == nil {
		return nil
	}
	return append([]Policy(nil), snapshot.policies...)
}

func (s *Service) BuiltinLimits() map[string]int64 {
	if s == nil {
		return map[string]int64{}
	}
	result := map[string]int64{
		"global_concurrency":       s.options.GlobalConcurrency,
		"organization_concurrency": s.options.OrgConcurrency,
	}
	if s.workload != nil {
		options := s.workload.Options()
		result["workload_global_capacity"] = options.GlobalCapacity
		result["workload_organization_capacity"] = options.OrgCapacity
		result["workload_global_short_reserve"] = options.GlobalShortReserve
		result["workload_organization_short_reserve"] = options.OrgShortReserve
		result["workload_interactive_weight"] = options.Interactive.Weight
		result["workload_stream_weight"] = options.Stream.Weight
		result["workload_embedding_weight"] = options.Embedding.Weight
		result["workload_interactive_max_wait_ms"] = int64(options.Interactive.MaxWait / time.Millisecond)
		result["workload_stream_max_wait_ms"] = int64(options.Stream.MaxWait / time.Millisecond)
		result["workload_embedding_max_wait_ms"] = int64(options.Embedding.MaxWait / time.Millisecond)
	}
	return result
}

func (s *Service) Reserve(ctx context.Context, request AdmissionRequest) (*Reservation, error) {
	if s == nil || s.rdb == nil || request.OperationID == "" || len(request.OperationID) > 64 ||
		request.Subject.OrgID <= 0 || request.Subject.APIKeyID <= 0 || request.Subject.Model == "" ||
		request.EstimatedInput < 0 || request.ReservedOutput < 0 ||
		request.EstimatedInput+request.ReservedOutput <= 0 {
		return nil, ErrInvalidRequest
	}
	if request.LeaseTTL <= 0 {
		request.LeaseTTL = 5 * time.Minute
	}
	if request.LeaseTTL > maxLeaseTTL {
		return nil, ErrInvalidRequest
	}
	if s.db != nil {
		snapshot := s.snapshot.Load()
		if snapshot == nil || snapshot.loadedAt.IsZero() || time.Since(snapshot.loadedAt) > s.options.SnapshotMaxAge {
			decision := Decision{
				PolicyRef: "policy-set", PolicyName: "Distributed policy snapshot", PolicyVersion: 1,
				SelectorHash: "snapshot", Mode: ModeEnforce, FailureMode: FailureFailClosed,
				Decision: DecisionDeny, Reason: ReasonSnapshotStale,
			}
			recordDecision(ctx, decision, "stale")
			recordDecisionMetric(decision)
			return nil, ErrStateUnavailable
		}
	}
	request.Subject = s.authoritativeSubject(request.Subject)
	if request.WorkloadClass == "" {
		request.WorkloadClass = workloadadmission.ClassInteractive
	}

	policies := s.matchingPolicies(request.Subject)
	version := runtimePolicySetVersion(policies, s.workload.Version())
	if len(policies)+1 > s.options.MaxMatchingPolicies {
		decision := Decision{
			PolicyRef: "policy-set", PolicyName: "Distributed policy chain", PolicyVersion: 1,
			SelectorHash: version, Mode: ModeEnforce, FailureMode: FailureFailClosed,
			Decision: DecisionDeny, Reason: ReasonPolicyChainLarge,
		}
		recordDecision(ctx, decision, version)
		recordDecisionMetric(decision)
		return nil, ErrStateUnavailable
	}
	reservation := &Reservation{OperationID: request.OperationID, Items: make([]policyReservation, 0, len(policies)), Idempotent: true}
	for _, policy := range policies {
		runtimeRef := policy.RuntimeBucket
		if runtimeRef == "" {
			runtimeRef = policy.Ref
		}
		fingerprint := operationFingerprint(request, runtimeRef)
		store, storeErr := s.reserveStoreWithOperation(ctx, policy, request, runtimeRef, fingerprint)
		if storeErr != nil {
			decision, allowFallback := s.decisionForStoreError(policy, runtimeRef)
			recordDecision(ctx, decision, version)
			recordDecisionMetric(decision)
			if allowFallback {
				reservation.Idempotent = false
				reservation.Items = append(reservation.Items, policyReservation{
					OperationID: request.OperationID,
					PolicyRef:   policy.Ref, RuntimeRef: runtimeRef, Fingerprint: fingerprint,
					InputTokens:    request.EstimatedInput,
					ReservedTokens: request.EstimatedInput + request.ReservedOutput,
					TPMLimit:       policy.TPMLimit,
					Fallback:       true, Mode: policy.Mode,
				})
				continue
			}
			if policy.Mode == ModeShadow {
				continue
			}
			s.rollback(ctx, reservation)
			return nil, ErrStateUnavailable
		}

		if store.FallbackWindow > 0 && policy.FailureMode == FailureLastKnownSafe {
			s.updateFallback(runtimeRef, store.FallbackWindow)
		}
		if store.Code != storeIdempotent {
			reservation.Idempotent = false
		}
		decision := decisionFromStore(policy, store)
		recordDecision(ctx, decision, version)
		recordDecisionMetric(decision)
		if store.Code == storeConflict {
			s.rollback(ctx, reservation)
			return nil, ErrConflict
		}
		if store.Code >= storeRPSExceeded && store.Code <= storeConcurrencyExceeded && policy.Mode == ModeEnforce {
			s.rollback(ctx, reservation)
			return nil, errorForStoreCode(store.Code)
		}
		reservation.Items = append(reservation.Items, policyReservation{
			OperationID: request.OperationID,
			PolicyRef:   policy.Ref, RuntimeRef: runtimeRef, Fingerprint: fingerprint,
			InputTokens:    request.EstimatedInput,
			ReservedTokens: request.EstimatedInput + request.ReservedOutput,
			TPMLimit:       policy.TPMLimit,
			Fallback:       false, Mode: policy.Mode,
		})
	}
	workloadReservation, workloadDecision, workloadErr := s.workload.Reserve(ctx, workloadadmission.Request{
		OperationID: request.OperationID, OrgID: request.Subject.OrgID,
		Class: request.WorkloadClass, LeaseTTL: request.LeaseTTL,
	})
	mappedWorkloadDecision := workloadLimitDecision(workloadDecision)
	RecordSupplementalDecision(ctx, mappedWorkloadDecision)
	recordDecisionMetric(mappedWorkloadDecision)
	if workloadErr != nil {
		s.rollback(ctx, reservation)
		switch {
		case errors.Is(workloadErr, workloadadmission.ErrCapacityExceeded):
			return nil, ErrWorkloadCapacity
		case errors.Is(workloadErr, workloadadmission.ErrConflict):
			return nil, ErrConflict
		case errors.Is(workloadErr, workloadadmission.ErrConfigConflict):
			return nil, ErrWorkloadConfig
		default:
			return nil, ErrStateUnavailable
		}
	}
	reservation.workload = workloadReservation
	if workloadReservation != nil && !workloadReservation.Idempotent {
		reservation.Idempotent = false
	}
	return reservation, nil
}

func (s *Service) Finalize(ctx context.Context, reservation *Reservation, actualTokens int64) error {
	if !validReservation(reservation) || actualTokens < 0 {
		return ErrInvalidRequest
	}
	var terminalErrors []error
	for _, item := range reservation.Items {
		if item.Fallback {
			recordTerminalMetric("settled", "last_known_safe")
			continue
		}
		overrun, err := s.terminalStore(ctx, item, "settled", actualTokens, item.TPMLimit)
		if err != nil {
			recordTerminalMetric("settled", "retained")
			terminalErrors = append(terminalErrors, err)
			continue
		}
		if overrun {
			recordTerminalMetric("settled", "overrun")
		} else {
			recordTerminalMetric("settled", "success")
		}
	}
	if reservation.workload != nil {
		if err := s.workload.Release(ctx, reservation.workload); err != nil {
			terminalErrors = append(terminalErrors, err)
		}
	}
	return errors.Join(terminalErrors...)
}

func (s *Service) Cancel(ctx context.Context, reservation *Reservation) error {
	return s.cancel(ctx, reservation, false)
}

func (s *Service) CancelBeforeDispatch(ctx context.Context, reservation *Reservation) error {
	return s.cancel(ctx, reservation, true)
}

func (s *Service) cancel(ctx context.Context, reservation *Reservation, beforeDispatch bool) error {
	if !validReservation(reservation) {
		return ErrInvalidRequest
	}
	var terminalErrors []error
	for _, item := range reservation.Items {
		if item.Fallback {
			recordTerminalMetric("cancelled", "last_known_safe")
			continue
		}
		terminalTokens := item.InputTokens
		if beforeDispatch {
			terminalTokens = 0
		}
		if _, err := s.terminalStore(ctx, item, "cancelled", terminalTokens, item.TPMLimit); err != nil {
			recordTerminalMetric("cancelled", "retained")
			terminalErrors = append(terminalErrors, err)
			continue
		}
		recordTerminalMetric("cancelled", "success")
	}
	if reservation.workload != nil {
		if err := s.workload.Release(ctx, reservation.workload); err != nil {
			terminalErrors = append(terminalErrors, err)
		}
	}
	return errors.Join(terminalErrors...)
}

func (s *Service) reserveStoreWithOperation(ctx context.Context, policy Policy, request AdmissionRequest, runtimeRef, fingerprint string) (storeResult, error) {
	// reserveStore builds the static key family; substitute the operation key
	// for this server operation before executing the script.
	keys := operationRuntimeKeys(runtimeRef, request.OperationID)
	member := operationMember(request.OperationID)
	fallbackCap := int64(0)
	if policy.FailureMode == FailureLastKnownSafe {
		fallbackCap = int64(s.options.FallbackGlobalCap)
	}
	result, err := reserveScript.Run(ctx, s.rdb, keys,
		member, fingerprint, policy.RPSLimit, policy.RPMLimit, policy.TPMLimit,
		policy.ConcurrencyLimit, request.EstimatedInput+request.ReservedOutput,
		request.EstimatedInput, ceilMilliseconds(request.LeaseTTL+s.options.TerminalGrace),
		ceilSeconds(request.LeaseTTL+s.options.TerminalGrace*2), policy.Mode, s.nodeID,
		ceilMilliseconds(s.options.FallbackTTL), fallbackCap).Result()
	if err != nil {
		return storeResult{}, err
	}
	return parseStoreResult(result)
}

func parseStoreResult(result interface{}) (storeResult, error) {
	values, ok := result.([]interface{})
	if !ok || len(values) != 9 {
		return storeResult{}, fmt.Errorf("unexpected Redis result %T", result)
	}
	parsed := make([]int64, len(values))
	for index, value := range values {
		var err error
		parsed[index], err = redisInt64(value)
		if err != nil {
			return storeResult{}, err
		}
	}
	store := storeResult{
		Code: parsed[0], Reason: reasonForCode(parsed[1]),
		Observed:  Usage{RPS: parsed[2], RPM: parsed[3], TPM: parsed[4], Concurrency: parsed[5]},
		WouldDeny: parsed[8] == 1,
	}
	if parsed[7] > parsed[6] {
		store.FallbackWindow = time.Duration(parsed[7]-parsed[6]) * time.Millisecond
	}
	return store, nil
}

func (s *Service) matchingPolicies(subject Subject) []Policy {
	snapshot := s.snapshot.Load()
	baseRPS := subject.BaseRPSLimit
	if baseRPS <= 0 {
		baseRPS = 10
	}
	baseRPM := subject.BaseRPMLimit
	if baseRPM <= 0 {
		baseRPM = 60
	}
	keyID := subject.APIKeyID
	apiKeyRate := Policy{
		Ref: fmt.Sprintf("builtin:api-key-rate:rps-%d:rpm-%d", baseRPS, baseRPM), Name: "Built-in API-key rate", Version: 1,
		APIKeyID: &keyID, Mode: ModeEnforce, FailureMode: FailureLastKnownSafe,
		RPSLimit: baseRPS, RPMLimit: baseRPM, Builtin: true,
		RuntimeBucket: fmt.Sprintf("builtin:api-key-rate:%d", subject.APIKeyID),
	}
	// Count every authenticated model-call attempt against its API-key window
	// before a shared-capacity or more-specific policy can reject it. This both
	// preserves the historical middleware contract and prevents rejected-work
	// amplification from bypassing the caller's own rate boundary.
	policies := []Policy{apiKeyRate}
	if snapshot == nil {
		return policies
	}
	candidates := snapshot.index.candidates(subject)
	if candidates == nil && len(snapshot.policies) > 0 {
		candidates = snapshot.policies
	}
	for _, policy := range candidates {
		if policy.Matches(subject) {
			policies = append(policies, policy)
		}
	}
	return policies
}

func (s *Service) authoritativeSubject(subject Subject) Subject {
	snapshot := s.snapshot.Load()
	if snapshot != nil && snapshot.labelsAuthoritative {
		subject.PolicyLabels = append([]string(nil), snapshot.labels[subject.APIKeyID]...)
	} else {
		subject.PolicyLabels = append([]string(nil), subject.PolicyLabels...)
	}
	return subject
}

func buildPolicySnapshot(policies []Policy, labels map[int64][]string, authoritative bool, loadedAt time.Time) *policySnapshot {
	index := policyIndex{
		byOrg: make(map[int64][]Policy), byProject: make(map[int64][]Policy),
		byAPIKey: make(map[int64][]Policy), byModel: make(map[string][]Policy), byLabel: make(map[string][]Policy),
	}
	for _, policy := range policies {
		switch {
		case policy.APIKeyID != nil:
			index.byAPIKey[*policy.APIKeyID] = append(index.byAPIKey[*policy.APIKeyID], policy)
		case policy.ProjectID != nil:
			index.byProject[*policy.ProjectID] = append(index.byProject[*policy.ProjectID], policy)
		case policy.OrgID != nil:
			index.byOrg[*policy.OrgID] = append(index.byOrg[*policy.OrgID], policy)
		case policy.Model != "":
			index.byModel[policy.Model] = append(index.byModel[policy.Model], policy)
		case policy.PolicyLabel != "":
			index.byLabel[policy.PolicyLabel] = append(index.byLabel[policy.PolicyLabel], policy)
		default:
			index.globals = append(index.globals, policy)
		}
	}
	return &policySnapshot{
		policies: append([]Policy(nil), policies...), labels: labels,
		labelsAuthoritative: authoritative, index: index, loadedAt: loadedAt,
	}
}

func (index policyIndex) candidates(subject Subject) []Policy {
	if index.byOrg == nil {
		return nil
	}
	count := len(index.globals) + len(index.byAPIKey[subject.APIKeyID]) + len(index.byProject[subject.ProjectID]) +
		len(index.byOrg[subject.OrgID]) + len(index.byModel[subject.Model])
	for _, label := range subject.PolicyLabels {
		count += len(index.byLabel[label])
	}
	result := make([]Policy, 0, count)
	result = append(result, index.globals...)
	result = append(result, index.byAPIKey[subject.APIKeyID]...)
	result = append(result, index.byProject[subject.ProjectID]...)
	result = append(result, index.byOrg[subject.OrgID]...)
	result = append(result, index.byModel[subject.Model]...)
	for _, label := range subject.PolicyLabels {
		result = append(result, index.byLabel[label]...)
	}
	return result
}

func policyRuntimeBucket(policy Policy) string {
	return policy.Ref + ":selector:" + policy.selectorHash()
}

func parseStoredLabels(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	labels := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		label := strings.TrimSpace(part)
		if label == "" {
			continue
		}
		if len(label) > 64 || !validPolicyLabel(label) {
			return nil, ErrInvalidPolicy
		}
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	if len(labels) > 16 {
		return nil, ErrInvalidPolicy
	}
	return labels, nil
}

func validPolicyLabel(label string) bool {
	for index, r := range label {
		if index == 0 {
			if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				continue
			}
			return false
		}
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			r == '.' || r == '_' || r == ':' || r == '-' {
			continue
		}
		return false
	}
	return label != ""
}

func (s *Service) rollback(ctx context.Context, reservation *Reservation) {
	if reservation == nil || len(reservation.Items) == 0 {
		return
	}
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = s.CancelBeforeDispatch(rollbackCtx, reservation)
}

func (s *Service) decisionForStoreError(policy Policy, runtimeRef string) (Decision, bool) {
	decision := Decision{
		PolicyID: policy.ID, PolicyRef: policy.Ref, PolicyName: policy.Name,
		PolicyVersion: policy.Version, SelectorHash: policy.selectorHash(), Mode: policy.Mode,
		FailureMode: policy.FailureMode, Decision: DecisionDeny, Reason: ReasonStateUnavailable,
		Limits: policy.limits(),
	}
	if policy.Mode == ModeShadow {
		decision.Decision = DecisionAllow
		return decision, false
	}
	if policy.FailureMode == FailureLastKnownSafe && s.consumeFallback(runtimeRef) {
		decision.Decision = DecisionAllow
		decision.Reason = ReasonLastKnownSafe
		decision.Fallback = true
		return decision, true
	}
	return decision, false
}

func (s *Service) updateFallback(runtimeRef string, window time.Duration) {
	if window <= 0 || window > s.options.FallbackTTL {
		return
	}
	now := time.Now()
	expiresAt := now.Add(window).UnixNano()
	candidate := &fallbackLease{}
	candidate.expiresAt.Store(expiresAt)
	candidate.remaining.Store(1)
	value, loaded := s.fallback.LoadOrStore(runtimeRef, candidate)
	if !loaded {
		return
	}
	lease, ok := value.(*fallbackLease)
	if !ok {
		s.fallback.Store(runtimeRef, candidate)
		return
	}
	// One local lease per runtime population is sufficient. Refresh at most
	// halfway through its short lifetime so the global policy hot key does not
	// serialize every successful request on a process-wide lock.
	if lease.remaining.Load() > 0 && lease.expiresAt.Load() > now.Add(window/2).UnixNano() {
		return
	}
	lease.expiresAt.Store(expiresAt)
	lease.remaining.Store(1)
}

func (s *Service) consumeFallback(runtimeRef string) bool {
	value, ok := s.fallback.Load(runtimeRef)
	if !ok {
		return false
	}
	lease, ok := value.(*fallbackLease)
	if !ok || time.Now().UnixNano() >= lease.expiresAt.Load() {
		s.fallback.CompareAndDelete(runtimeRef, value)
		return false
	}
	return lease.remaining.CompareAndSwap(1, 0)
}

func (s *Service) pruneFallback(now time.Time) {
	cutoff := now.UnixNano()
	s.fallback.Range(func(key, value interface{}) bool {
		lease, ok := value.(*fallbackLease)
		if !ok || lease.expiresAt.Load() <= cutoff {
			s.fallback.CompareAndDelete(key, value)
		}
		return true
	})
}

func decisionFromStore(policy Policy, store storeResult) Decision {
	decision := Decision{
		PolicyID: policy.ID, PolicyRef: policy.Ref, PolicyName: policy.Name,
		PolicyVersion: policy.Version, SelectorHash: policy.selectorHash(), Mode: policy.Mode,
		FailureMode: policy.FailureMode, Decision: DecisionAllow, Reason: store.Reason,
		Limits: policy.limits(), Observed: store.Observed,
	}
	if store.Code >= storeRPSExceeded && store.Code <= storeConcurrencyExceeded {
		decision.Decision = DecisionDeny
	}
	if policy.Mode == ModeShadow && store.WouldDeny {
		decision.Decision = DecisionWouldDeny
	}
	if store.Code == storeConflict {
		decision.Decision = DecisionDeny
		decision.Reason = ReasonConflict
	}
	return decision
}

func workloadLimitDecision(source workloadadmission.Decision) Decision {
	reason := source.Reason
	if reason == workloadadmission.ReasonCapacityExceeded {
		reason = ReasonWorkloadCapacity
	} else if reason == workloadadmission.ReasonQueueFull {
		reason = ReasonWorkloadQueueFull
	} else if reason == workloadadmission.ReasonConflict {
		reason = ReasonConflict
	} else if reason == workloadadmission.ReasonLastKnownSafe {
		reason = ReasonLastKnownSafe
	} else if reason == workloadadmission.ReasonStateUnavailable {
		reason = ReasonStateUnavailable
	}
	decision := Decision{
		PolicyRef: "builtin:workload-admission-v1", PolicyName: "Built-in weighted workload admission",
		PolicyVersion: 1, SelectorHash: "global-org", Mode: ModeEnforce,
		FailureMode: FailureLastKnownSafe, Decision: DecisionAllow, Reason: reason,
		Limits:   Limits{Concurrency: source.GlobalCapacity},
		Observed: Usage{Concurrency: source.GlobalUsed}, Fallback: source.Fallback,
		WorkloadClass: source.Class, WorkloadWeight: source.Weight,
		WaitMS: int64(source.Waited / time.Millisecond), QueueDepth: source.QueueDepth,
	}
	if source.Decision == "deny" {
		decision.Decision = DecisionDeny
	}
	return decision
}

func errorForStoreCode(code int64) error {
	switch code {
	case storeRPSExceeded:
		return ErrRPSExceeded
	case storeRPMExceeded:
		return ErrRPMExceeded
	case storeTPMExceeded:
		return ErrTPMExceeded
	case storeConcurrencyExceeded:
		return ErrConcurrencyExceeded
	case storeConflict:
		return ErrConflict
	default:
		return nil
	}
}

func validReservation(reservation *Reservation) bool {
	if reservation == nil || reservation.OperationID == "" || len(reservation.OperationID) > 64 {
		return false
	}
	for _, item := range reservation.Items {
		if item.OperationID != reservation.OperationID || item.PolicyRef == "" || item.RuntimeRef == "" ||
			item.Fingerprint == "" || item.InputTokens < 0 || item.ReservedTokens <= 0 || item.TPMLimit < 0 {
			return false
		}
	}
	return true
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullValue(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}
