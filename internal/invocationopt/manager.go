package invocationopt

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
	"golang.org/x/sync/singleflight"
)

const policyCacheTTL = time.Second

type cachedResolution struct {
	policy Policy
	found  bool
	loaded time.Time
}

type Manager struct {
	db    db.DBQuerier
	now   func() time.Time
	mu    sync.RWMutex
	cache map[string]cachedResolution
	group singleflight.Group
}

type policyScanner interface {
	Scan(...any) error
}

const policySelect = `
SELECT id, contract_version, org_id, COALESCE(project_id,0), policy_ref, display_name,
       model_selector, region, residency, response_cache_target,
       response_cache_canary_basis_points, response_cache_ttl_seconds,
       response_cache_persistence_allowed, response_cache_require_request_opt_in,
       compression_target, compression_canary_basis_points,
       compression_require_request_opt_in, compression_minimum_input_tokens,
       compression_minimum_saved_tokens, compression_maximum_reduction_basis_points,
       minimum_shadow_samples, status, stage, revision, created_by, updated_by,
       validated_at, created_at, updated_at`

func NewManager(database db.DBQuerier) (*Manager, error) {
	if database == nil {
		return nil, ErrUnavailable
	}
	return &Manager{db: database, now: time.Now, cache: make(map[string]cachedResolution)}, nil
}

func (m *Manager) Resolve(ctx context.Context, orgID, projectID int64, model string) (Policy, bool, error) {
	if m == nil || m.db == nil || orgID <= 0 || projectID < 0 || model == "" {
		return Policy{}, false, ErrInvalid
	}
	key := fmt.Sprintf("%d/%d/%s", orgID, projectID, model)
	now := m.now().UTC()
	if cached, ok := m.cached(key); ok && now.Sub(cached.loaded) < policyCacheTTL {
		return cached.policy, cached.found, nil
	}
	value, err, _ := m.group.Do(key, func() (any, error) {
		if cached, ok := m.cached(key); ok && m.now().UTC().Sub(cached.loaded) < policyCacheTTL {
			return cached, nil
		}
		policy, found, loadErr := m.load(ctx, orgID, projectID, model)
		if loadErr != nil {
			return cachedResolution{}, loadErr
		}
		resolution := cachedResolution{policy: policy, found: found, loaded: m.now().UTC()}
		m.mu.Lock()
		m.cache[key] = resolution
		m.mu.Unlock()
		return resolution, nil
	})
	if err != nil {
		return Policy{}, false, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	resolution := value.(cachedResolution)
	return resolution.policy, resolution.found, nil
}

func (m *Manager) Invalidate() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.cache = make(map[string]cachedResolution)
	m.mu.Unlock()
}

func (m *Manager) cached(key string) (cachedResolution, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.cache[key]
	return value, ok
}

func (m *Manager) load(ctx context.Context, orgID, projectID int64, model string) (Policy, bool, error) {
	row := m.db.QueryRow(ctx, policySelect+`
  FROM invocation_optimization_policies
 WHERE org_id=$1 AND status='active'
   AND (project_id=$2 OR project_id IS NULL)
   AND (model_selector=$3 OR model_selector='*')
 ORDER BY (project_id IS NOT NULL) DESC, (model_selector <> '*') DESC, revision DESC
 LIMIT 1`, orgID, projectID, model)
	policy, err := scanPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, false, nil
	}
	if err != nil {
		return Policy{}, false, err
	}
	if err := policy.Validate(); err != nil {
		return Policy{}, false, err
	}
	return policy, true, nil
}

func scanPolicy(row policyScanner) (Policy, error) {
	var policy Policy
	err := row.Scan(
		&policy.ID, &policy.ContractVersion, &policy.OrgID, &policy.ProjectID,
		&policy.PolicyRef, &policy.DisplayName, &policy.ModelSelector, &policy.Region,
		&policy.Residency, &policy.ResponseCacheTarget, &policy.ResponseCacheCanaryBPS,
		&policy.ResponseCacheTTLSeconds, &policy.ResponseCachePersistence,
		&policy.ResponseCacheRequireOptIn, &policy.CompressionTarget,
		&policy.CompressionCanaryBPS, &policy.CompressionRequireOptIn,
		&policy.CompressionMinimumInputTokens, &policy.CompressionMinimumSavedTokens,
		&policy.CompressionMaximumReductionBPS, &policy.MinimumShadowSamples,
		&policy.Status, &policy.Stage, &policy.Revision, &policy.CreatedBy,
		&policy.UpdatedBy, &policy.ValidatedAt, &policy.CreatedAt, &policy.UpdatedAt,
	)
	return policy, err
}

func (m *Manager) Get(ctx context.Context, orgID int64, policyRef string) (Policy, error) {
	if m == nil || m.db == nil || orgID <= 0 || !policyRefPattern.MatchString(policyRef) {
		return Policy{}, ErrInvalid
	}
	policy, err := scanPolicy(m.db.QueryRow(ctx, policySelect+`
  FROM invocation_optimization_policies WHERE org_id=$1 AND policy_ref=$2`, orgID, policyRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, ErrNotFound
	}
	if err != nil {
		return Policy{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func (m *Manager) List(ctx context.Context, orgID int64, limit int) ([]Policy, error) {
	if m == nil || m.db == nil || orgID <= 0 {
		return nil, ErrInvalid
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := m.db.Query(ctx, policySelect+`
  FROM invocation_optimization_policies WHERE org_id=$1
 ORDER BY updated_at DESC, id DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	items := make([]Policy, 0)
	for rows.Next() {
		policy, scanErr := scanPolicy(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, scanErr)
		}
		if validateErr := policy.Validate(); validateErr != nil {
			return nil, validateErr
		}
		items = append(items, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return items, nil
}

func (m *Manager) Record(ctx context.Context, receipt Receipt) error {
	if m == nil || m.db == nil || receipt.OrgID <= 0 || receipt.ProjectID < 0 ||
		receipt.RequestID == "" || receipt.RequestedModel == "" || receipt.ResolvedModel == "" ||
		receipt.PolicyRef == "" || receipt.PolicyRevision <= 0 ||
		(receipt.Driver != DriverResponseCache && receipt.Driver != DriverCompression) {
		return ErrInvalid
	}
	if receipt.CreatedAt.IsZero() {
		receipt.CreatedAt = m.now().UTC()
	}
	_, err := m.db.Exec(ctx, `
INSERT INTO invocation_optimization_receipts (
    contract_version, org_id, project_id, request_id, requested_model,
    resolved_model, provider, driver, policy_ref,
    policy_revision, stage, effective_mode, decision, reason, assigned, eligible,
    mutation_applied, original_bytes, candidate_bytes, original_estimated_tokens,
    candidate_estimated_tokens, estimated_saved_tokens, reduction_basis_points,
    safety_passed, quality_passed, latency_micros, route_channel_id,
    route_credential_id, runtime_assignment_id, identity_digest, created_at)
VALUES ($1,$2,NULLIF($3,0),$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,
        $18,$19,$20,$21,$22,$23,$24,$25,$26,NULLIF($27,0),NULLIF($28,0),$29,$30,$31)
ON CONFLICT (org_id,request_id,driver) DO NOTHING`,
		ReceiptContractVersion, receipt.OrgID, receipt.ProjectID, receipt.RequestID,
		receipt.RequestedModel, receipt.ResolvedModel, receipt.Provider,
		receipt.Driver, receipt.PolicyRef, receipt.PolicyRevision, receipt.Stage,
		receipt.EffectiveMode, receipt.Decision, receipt.Reason, receipt.Assigned,
		receipt.Eligible, receipt.MutationApplied, receipt.OriginalBytes,
		receipt.CandidateBytes, receipt.OriginalEstimatedTokens,
		receipt.CandidateEstimatedTokens, receipt.EstimatedSavedTokens,
		receipt.ReductionBasisPoints, receipt.SafetyPassed, receipt.QualityPassed,
		receipt.LatencyMicros, receipt.RouteChannelID, receipt.RouteCredentialID,
		receipt.RuntimeAssignmentID, receipt.IdentityDigest, receipt.CreatedAt)
	if err != nil {
		return fmt.Errorf("record invocation optimization receipt: %w", err)
	}
	return nil
}

func (m *Manager) RecordCommercial(ctx context.Context, receipt CommercialReceipt) error {
	if m == nil || m.db == nil || receipt.OrgID <= 0 || receipt.ProjectID < 0 ||
		receipt.RequestID == "" || receipt.SourceRequestID == "" || receipt.PolicyRef == "" ||
		receipt.PolicyRevision <= 0 || receipt.ProviderRequests != 0 ||
		receipt.ProviderCostMicros != 0 || receipt.CustomerChargeMicros != 0 ||
		receipt.AvoidedCustomerCostMicros < 0 || receipt.DeliveryStatus != "completed" ||
		receipt.HTTPStatus < 200 || receipt.HTTPStatus > 299 {
		return ErrInvalid
	}
	if receipt.CreatedAt.IsZero() {
		receipt.CreatedAt = m.now().UTC()
	}
	_, err := m.db.Exec(ctx, `
INSERT INTO response_cache_commercial_receipts (
    contract_version, org_id, project_id, request_id, source_request_id,
    policy_ref, policy_revision, identity_digest, source_response_sha256,
    requested_model, resolved_model, provider, channel_id, credential_id,
    credential_epoch, runtime_assignment_id, customer_pricing_version,
    response_cache_price_version, provider_requests, provider_cost_micros,
    customer_charge_micros, avoided_customer_cost_micros, delivery_status,
    http_status, created_at)
VALUES ($1,$2,NULLIF($3,0),$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
        $17,$18,$19,$20,$21,$22,$23,$24,$25)
ON CONFLICT (org_id,request_id) DO NOTHING`,
		CommercialReceiptVersion, receipt.OrgID, receipt.ProjectID, receipt.RequestID,
		receipt.SourceRequestID, receipt.PolicyRef, receipt.PolicyRevision,
		receipt.IdentityDigest, receipt.SourceResponseSHA256, receipt.RequestedModel,
		receipt.ResolvedModel, receipt.Provider, receipt.ChannelID, receipt.CredentialID,
		receipt.CredentialEpoch, receipt.RuntimeAssignmentID,
		receipt.CustomerPricingVersion, receipt.ResponseCachePriceVersion,
		receipt.ProviderRequests, receipt.ProviderCostMicros,
		receipt.CustomerChargeMicros, receipt.AvoidedCustomerCostMicros,
		receipt.DeliveryStatus, receipt.HTTPStatus, receipt.CreatedAt)
	if err != nil {
		return fmt.Errorf("record response-cache commercial receipt: %w", err)
	}
	return nil
}
