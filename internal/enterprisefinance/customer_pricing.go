package enterprisefinance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modeloperation"
)

const CustomerPricingTierStandard = "standard"

type CustomerPriceEntry struct {
	PublicModel string            `json:"public_model"`
	Operation   modeloperation.ID `json:"operation"`
	Components  []RateComponent   `json:"components"`
}

type CustomerPriceTable struct {
	OrgID         int64                `json:"org_id"`
	Ref           string               `json:"table_ref"`
	Version       int64                `json:"version"`
	EffectiveFrom time.Time            `json:"effective_from"`
	EffectiveTo   *time.Time           `json:"effective_to,omitempty"`
	Entries       []CustomerPriceEntry `json:"entries"`
	CreatedBy     string               `json:"created_by,omitempty"`
	ApprovedBy    *string              `json:"approved_by,omitempty"`
	ApprovedAt    *time.Time           `json:"approved_at,omitempty"`
}

type CustomerPriceBinding struct {
	OrgID        int64  `json:"org_id"`
	ProjectID    int64  `json:"project_id"`
	Revision     int64  `json:"revision"`
	TableRef     string `json:"table_ref"`
	TableVersion int64  `json:"table_version"`
}

type CustomerPriceSelection struct {
	Binding       CustomerPriceBinding `json:"binding"`
	Rate          CustomerPriceEntry   `json:"rate"`
	Tier          string               `json:"tier"`
	EffectiveFrom time.Time            `json:"effective_from"`
	EffectiveTo   *time.Time           `json:"effective_to,omitempty"`
}

func customerTokenOperation(operation modeloperation.ID) bool {
	switch operation {
	case modeloperation.IDChatCompletions, modeloperation.IDEmbeddings,
		modeloperation.IDResponses, modeloperation.IDMessages:
		return true
	default:
		return false
	}
}

func normalizeCustomerPriceTable(table CustomerPriceTable) (CustomerPriceTable, error) {
	table.Ref = strings.TrimSpace(table.Ref)
	table.EffectiveFrom = table.EffectiveFrom.UTC().Truncate(time.Microsecond)
	if table.EffectiveTo != nil {
		end := table.EffectiveTo.UTC().Truncate(time.Microsecond)
		table.EffectiveTo = &end
	}
	if table.OrgID <= 0 || table.Ref == "" || len(table.Ref) > 160 || table.Version <= 0 ||
		!validTime(table.EffectiveFrom) || (table.EffectiveTo != nil &&
		(!validTime(*table.EffectiveTo) || !table.EffectiveTo.After(table.EffectiveFrom))) ||
		len(table.Entries) == 0 || len(table.Entries) > 256 {
		return CustomerPriceTable{}, invalid("customer_price_table")
	}
	table.Entries = slices.Clone(table.Entries)
	type selector struct {
		model     string
		operation modeloperation.ID
	}
	seen := make(map[selector]struct{}, len(table.Entries))
	for i := range table.Entries {
		entry := &table.Entries[i]
		entry.PublicModel = strings.TrimSpace(entry.PublicModel)
		key := selector{entry.PublicModel, entry.Operation}
		if entry.PublicModel == "" || len(entry.PublicModel) > 255 || !customerTokenOperation(entry.Operation) {
			return CustomerPriceTable{}, invalid("customer_price_selector")
		}
		if _, exists := seen[key]; exists {
			return CustomerPriceTable{}, invalid("duplicate_customer_price_selector")
		}
		seen[key] = struct{}{}
		if len(entry.Components) != len(requiredRequestMeters) {
			return CustomerPriceTable{}, invalid("customer_price_four_token_meters")
		}
		for _, component := range entry.Components {
			if component.UnitQuantity != "1000000" {
				return CustomerPriceTable{}, invalid("customer_price_per_million")
			}
		}
		if err := validateRequestPriceComponents(entry.Components, "customer price"); err != nil {
			return CustomerPriceTable{}, err
		}
		entry.Components = slices.Clone(entry.Components)
		slices.SortFunc(entry.Components, func(a, b RateComponent) int { return strings.Compare(a.Meter, b.Meter) })
	}
	return table, nil
}

// CreateCustomerPriceTable records one exact draft version for independent approval.
func (s *PostgresStore) CreateCustomerPriceTable(ctx context.Context, table CustomerPriceTable, actor string) (CustomerPriceTable, error) {
	actor = strings.TrimSpace(actor)
	table, err := normalizeCustomerPriceTable(table)
	if err != nil {
		return CustomerPriceTable{}, err
	}
	if actor == "" {
		return CustomerPriceTable{}, invalid("customer_price_actor")
	}
	table.CreatedBy, table.ApprovedBy, table.ApprovedAt = actor, nil, nil
	entries, err := json.Marshal(table.Entries)
	if err != nil {
		return CustomerPriceTable{}, err
	}
	result, err := s.db.Exec(ctx, `INSERT INTO customer_price_table_versions
		(org_id, table_ref, version, effective_from, effective_to, entries, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
		table.OrgID, table.Ref, table.Version, table.EffectiveFrom, table.EffectiveTo, entries, actor)
	if err != nil {
		return CustomerPriceTable{}, err
	}
	if result.RowsAffected() != 1 {
		return CustomerPriceTable{}, ErrConflict
	}
	return table, nil
}

// ApproveCustomerPriceTable independently reviews an exact draft version.
func (s *PostgresStore) ApproveCustomerPriceTable(ctx context.Context, orgID int64, ref string, version int64, actor string) error {
	ref, actor = strings.TrimSpace(ref), strings.TrimSpace(actor)
	if orgID <= 0 || ref == "" || version <= 0 || actor == "" {
		return invalid("customer_price_approval")
	}
	result, err := s.db.Exec(ctx, `UPDATE customer_price_table_versions SET approved_by=$4, approved_at=NOW()
		WHERE org_id=$1 AND table_ref=$2 AND version=$3 AND approved_at IS NULL AND created_by<>$4`, orgID, ref, version, actor)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

// BindCustomerPriceTable records an approved version and advances the exact scope revision.
func (s *PostgresStore) BindCustomerPriceTable(ctx context.Context, binding CustomerPriceBinding, expectedRevision int64, actor string) (CustomerPriceBinding, error) {
	binding.TableRef = strings.TrimSpace(binding.TableRef)
	actor = strings.TrimSpace(actor)
	if binding.OrgID <= 0 || binding.ProjectID < 0 || binding.TableRef == "" || binding.TableVersion <= 0 || expectedRevision < 0 || actor == "" {
		return CustomerPriceBinding{}, invalid("customer_price_binding")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CustomerPriceBinding{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		fmt.Sprintf("customer-price-binding:%d:%d", binding.OrgID, binding.ProjectID)); err != nil {
		return CustomerPriceBinding{}, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO customer_price_binding_versions
		    (org_id, scope_project_id, revision, table_ref, table_version, bound_by)
		SELECT $1::bigint, $2::bigint, $5::bigint + 1, $3::text, $4::bigint, $6::text
		WHERE EXISTS (SELECT 1 FROM customer_price_table_versions
		              WHERE org_id=$1 AND table_ref=$3 AND version=$4 AND approved_at IS NOT NULL AND approved_by IS NOT NULL)
		  AND ($2::bigint=0 OR EXISTS (SELECT 1 FROM projects WHERE id=$2 AND org_id=$1 AND status='active'))
		  AND $5=COALESCE((SELECT MAX(revision) FROM customer_price_binding_versions
		                   WHERE org_id=$1 AND scope_project_id=$2), 0)
		RETURNING revision`, binding.OrgID, binding.ProjectID, binding.TableRef, binding.TableVersion, expectedRevision, actor).Scan(&binding.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return CustomerPriceBinding{}, ErrConflict
	}
	if err != nil {
		return CustomerPriceBinding{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CustomerPriceBinding{}, err
	}
	return binding, nil
}

// ResolveCustomerPrice resolves the authenticated scope and exact operation from one database snapshot.
func (s *PostgresStore) ResolveCustomerPrice(ctx context.Context, orgID, projectID int64, model string, operation modeloperation.ID, tier string, at time.Time) (CustomerPriceSelection, error) {
	model = strings.TrimSpace(model)
	at = at.UTC().Truncate(time.Microsecond)
	if orgID <= 0 || projectID < 0 || model == "" || !customerTokenOperation(operation) ||
		tier != CustomerPricingTierStandard || !validTime(at) {
		return CustomerPriceSelection{}, ErrRequestPricingNotReady
	}
	binding := CustomerPriceBinding{OrgID: orgID}
	table := CustomerPriceTable{OrgID: orgID}
	var entries []byte
	err := s.db.QueryRow(ctx, `
		SELECT b.scope_project_id, b.revision, b.table_ref, b.table_version,
		       t.effective_from, t.effective_to, t.entries
		FROM (
		    SELECT scope_project_id, revision, table_ref, table_version
		    FROM customer_price_binding_versions
		    WHERE org_id=$1 AND scope_project_id IN (0, $2)
		      AND ($2::bigint=0 OR EXISTS (SELECT 1 FROM projects WHERE id=$2 AND org_id=$1 AND status='active'))
		    ORDER BY scope_project_id DESC, revision DESC LIMIT 1
		) b
		JOIN customer_price_table_versions t
		  ON t.org_id=$1 AND t.table_ref=b.table_ref AND t.version=b.table_version
		WHERE t.approved_at IS NOT NULL AND t.approved_by IS NOT NULL AND t.effective_from<=$3
		  AND (t.effective_to IS NULL OR t.effective_to>$3)`, orgID, projectID, at).Scan(
		&binding.ProjectID, &binding.Revision, &binding.TableRef, &binding.TableVersion,
		&table.EffectiveFrom, &table.EffectiveTo, &entries)
	if errors.Is(err, pgx.ErrNoRows) {
		return CustomerPriceSelection{}, ErrRequestPricingNotReady
	}
	if err != nil {
		return CustomerPriceSelection{}, err
	}
	table.Ref, table.Version = binding.TableRef, binding.TableVersion
	if err := json.Unmarshal(entries, &table.Entries); err != nil {
		return CustomerPriceSelection{}, err
	}
	table, err = normalizeCustomerPriceTable(table)
	if err != nil {
		return CustomerPriceSelection{}, err
	}
	for _, rate := range table.Entries {
		if rate.PublicModel == model && rate.Operation == operation {
			return CustomerPriceSelection{Binding: binding, Rate: rate, Tier: tier,
				EffectiveFrom: table.EffectiveFrom, EffectiveTo: table.EffectiveTo}, nil
		}
	}
	return CustomerPriceSelection{}, ErrRequestPricingNotReady
}

// CustomerPriceTokenRates returns exact micro-CNY-per-million projections.
func CustomerPriceTokenRates(selection CustomerPriceSelection) (map[string]int64, error) {
	rates := make(map[string]int64, len(selection.Rate.Components))
	for _, component := range selection.Rate.Components {
		price, err := requestComponentPrice(component)
		if err != nil {
			return nil, err
		}
		rates[component.Meter] = price
	}
	return rates, nil
}

// GetCustomerPriceTable reads the exact draft or approved version for review.
func (s *PostgresStore) GetCustomerPriceTable(ctx context.Context, orgID int64, ref string, version int64) (CustomerPriceTable, error) {
	table := CustomerPriceTable{OrgID: orgID, Ref: strings.TrimSpace(ref), Version: version}
	var entries []byte
	err := s.db.QueryRow(ctx, `SELECT effective_from,effective_to,entries,created_by,approved_by,approved_at
		FROM customer_price_table_versions WHERE org_id=$1 AND table_ref=$2 AND version=$3`,
		orgID, table.Ref, version).Scan(
		&table.EffectiveFrom, &table.EffectiveTo, &entries, &table.CreatedBy, &table.ApprovedBy, &table.ApprovedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return CustomerPriceTable{}, ErrNotFound
	}
	if err != nil {
		return CustomerPriceTable{}, err
	}
	if err := json.Unmarshal(entries, &table.Entries); err != nil {
		return CustomerPriceTable{}, err
	}
	return normalizeCustomerPriceTable(table)
}

// GetCustomerPriceBinding reads the current revision of the exact scope.
func (s *PostgresStore) GetCustomerPriceBinding(ctx context.Context, orgID, projectID int64) (CustomerPriceBinding, error) {
	binding := CustomerPriceBinding{OrgID: orgID, ProjectID: projectID}
	err := s.db.QueryRow(ctx, `SELECT revision,table_ref,table_version FROM customer_price_binding_versions
		WHERE org_id=$1 AND scope_project_id=$2 ORDER BY revision DESC LIMIT 1`, orgID, projectID).Scan(
		&binding.Revision, &binding.TableRef, &binding.TableVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return CustomerPriceBinding{}, ErrNotFound
	}
	return binding, err
}

// GetCustomerChargeQuote pairs immutable authorization with current settlement facts.
func (s *PostgresStore) GetCustomerChargeQuote(ctx context.Context, orgID int64, requestID string) (json.RawMessage, error) {
	var receipt json.RawMessage
	err := s.db.QueryRow(ctx, `
		SELECT jsonb_build_object(
		    'quote', q.receipt,
		    'wallet_authorized_cost_micros', q.maximum_authorized_micros::text,
		    'charged_customer_cost_micros', f.charged_cost_micros::text,
		    'settlement_status', f.status,
		    'settlement_pending_reason', f.settlement_pending_reason
		)
		FROM customer_charge_quote_receipts q
		JOIN freeze_idempotency f USING (org_id, request_id)
		WHERE q.org_id=$1 AND q.request_id=$2 AND q.pricing_version=f.pricing_version`, orgID, requestID).Scan(&receipt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return receipt, err
}

type CustomerChargeQuoteOverrun struct {
	RequestID         string    `json:"request_id"`
	ProjectID         int64     `json:"project_id"`
	ProviderPendingAt time.Time `json:"provider_cost_pending_at"`
	AuthorizedMicros  string    `json:"wallet_authorized_cost_micros"`
	CalculatedMicros  string    `json:"calculated_customer_cost_micros"`
	ChargedMicros     string    `json:"charged_customer_cost_micros"`
	UncoveredMicros   string    `json:"uncovered_customer_cost_micros"`
	UncachedInput     int64     `json:"uncached_input_tokens"`
	CacheRead         int64     `json:"cache_read_tokens"`
	CacheCreation     int64     `json:"cache_creation_tokens"`
	Output            int64     `json:"output_tokens"`
}

type CustomerChargeQuoteOverrunCursor struct {
	ProviderPendingAt time.Time `json:"provider_cost_pending_at"`
	RequestID         string    `json:"request_id"`
}

type CustomerChargeQuoteOverrunPage struct {
	Data []CustomerChargeQuoteOverrun      `json:"data"`
	Next *CustomerChargeQuoteOverrunCursor `json:"next,omitempty"`
}

func (s *PostgresStore) ListCustomerChargeQuoteOverruns(
	ctx context.Context, orgID int64, after CustomerChargeQuoteOverrunCursor, limit int,
) (CustomerChargeQuoteOverrunPage, error) {
	if orgID <= 0 || limit <= 0 || limit > 500 || len(after.RequestID) > 64 || (after.RequestID == "") != after.ProviderPendingAt.IsZero() {
		return CustomerChargeQuoteOverrunPage{}, invalid("customer_charge_quote_overrun_page")
	}
	var afterTime *time.Time
	if !after.ProviderPendingAt.IsZero() {
		afterTime = &after.ProviderPendingAt
	}
	rows, err := s.db.Query(ctx, `
		SELECT f.request_id, (q.receipt->>'project_id')::bigint, f.provider_cost_pending_at,
		       q.maximum_authorized_micros::text, f.calculated_cost_micros::text,
		       f.charged_cost_micros::text, f.uncovered_cost_micros::text,
		       f.uncached_prompt_tokens, f.cache_read_tokens, f.cache_creation_tokens, f.completion_tokens
		FROM freeze_idempotency f
		JOIN customer_charge_quote_receipts q USING (org_id, request_id, pricing_version)
		WHERE f.org_id=$1 AND f.status='settlement_pending'
		  AND f.settlement_pending_reason='customer_authorization_exceeded'
		  AND f.usage_authority='provider_reported' AND f.usage_finality='final'
		  AND f.provider_cost_pending_at IS NOT NULL
		  AND ($2::timestamptz IS NULL OR (f.provider_cost_pending_at, f.request_id) > ($2, $3))
		ORDER BY f.provider_cost_pending_at, f.request_id
		LIMIT $4`, orgID, afterTime, after.RequestID, limit+1)
	if err != nil {
		return CustomerChargeQuoteOverrunPage{}, err
	}
	data, err := pgx.CollectRows(rows, pgx.RowToStructByPos[CustomerChargeQuoteOverrun])
	if err != nil {
		return CustomerChargeQuoteOverrunPage{}, err
	}
	page := CustomerChargeQuoteOverrunPage{Data: data}
	if len(data) > limit {
		last := data[limit-1]
		page.Data = data[:limit]
		page.Next = &CustomerChargeQuoteOverrunCursor{ProviderPendingAt: last.ProviderPendingAt, RequestID: last.RequestID}
	}
	return page, nil
}
