// Package providercogs defines UniRouter's offline, append-only supplier-cost
// evidence boundary. It never mutates the customer revenue ledger.
package providercogs

import (
	"errors"
	"sync"
)

const (
	ContractVersion       = "unirouter.provider-cogs-offline.v1"
	SourceSnapshotVersion = "unirouter.provider-attempt-snapshot.v1"
	ReportVersion         = "unirouter.provider-cogs-report.v1"

	CustomerLedgerMutationNone = "none"

	CoverageProvisional = "provisional"
	CoverageClosed      = "closed"

	FinalityProvisional = "provisional"
	FinalityFinal       = "final"
	FinalityInvoice     = "invoice_reconciled"

	GranularityRequest       = "request"
	GranularityProjectBucket = "project_bucket"
	GranularityAccountBucket = "account_bucket"
	GranularityInvoiceLine   = "invoice_line"

	LineCharge     = "charge"
	LineCredit     = "credit"
	LineTax        = "tax"
	LineAdjustment = "adjustment"

	EvidenceExactRequestUsage = "exact_request_usage"
	EvidenceAggregateMatched  = "aggregate_cost_matched"
	EvidenceInvoiceReconciled = "invoice_reconciled"

	GapProviderDataDelayed           = "provider_data_delayed"
	GapRequestAttributionUnavailable = "request_attribution_unavailable"
	GapDiscountCreditUnallocated     = "discount_credit_unallocated"
	GapTaxFXUnallocated              = "tax_fx_unallocated"

	StatusExactRequestMatched   = "exact_request_matched"
	StatusSupplierOnly          = "supplier_only"
	StatusAggregateUnattributed = "aggregate_unattributed"
	StatusAmbiguous             = "ambiguous"
	StatusSourceOnly            = "source_only"
	StatusSupplierPending       = "supplier_evidence_pending"

	ReplayAccepted      = "accepted"
	ReplayDuplicateNoop = "duplicate_noop"
)

var (
	ErrInvalid  = errors.New("providercogs: invalid offline evidence")
	ErrConflict = errors.New("providercogs: conflicting replay evidence")
	ErrUnsafe   = errors.New("providercogs: unsafe financial attribution")
)

type ContractError struct {
	Code string `json:"code"`
	kind error
}

func (e *ContractError) Error() string {
	if e == nil {
		return "providercogs: unknown error"
	}
	return "providercogs: " + e.Code
}

func (e *ContractError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.kind
}

func contractError(kind error, code string) error {
	return &ContractError{Code: code, kind: kind}
}

type ImportRequest struct {
	ContractVersion string         `json:"contract_version"`
	ImportID        string         `json:"import_id"`
	Provider        string         `json:"provider"`
	SourceInstance  string         `json:"source_instance"`
	Currency        string         `json:"currency"`
	AmountScale     string         `json:"amount_scale"`
	Coverage        Coverage       `json:"coverage"`
	Pages           []Page         `json:"pages"`
	SourceSnapshot  SourceSnapshot `json:"source_snapshot"`
}

type Coverage struct {
	StartAt string `json:"start_at"`
	EndAt   string `json:"end_at"`
	State   string `json:"state"`
	Gaps    []Gap  `json:"gaps"`
}

type Gap struct {
	Code       string `json:"code"`
	Provenance string `json:"provenance"`
	Detail     string `json:"detail"`
}

type Page struct {
	PageID    string `json:"page_id"`
	PageIndex string `json:"page_index"`
	CursorIn  string `json:"cursor_in"`
	CursorOut string `json:"cursor_out"`
	IsLast    bool   `json:"is_last"`
	Lines     []Line `json:"lines"`
}

type Line struct {
	LineID            string `json:"line_id"`
	Revision          string `json:"revision"`
	LineKind          string `json:"line_kind"`
	Granularity       string `json:"granularity"`
	EvidenceLevel     string `json:"evidence_level"`
	ObservedAt        string `json:"observed_at"`
	PeriodStart       string `json:"period_start"`
	PeriodEnd         string `json:"period_end"`
	ProjectRef        string `json:"project_ref"`
	UpstreamRequestID string `json:"upstream_request_id"`
	LineItem          string `json:"line_item"`
	AmountMinor       string `json:"amount_minor"`
	Finality          string `json:"finality"`
}

type SourceSnapshot struct {
	SchemaVersion string    `json:"schema_version"`
	SnapshotID    string    `json:"snapshot_id"`
	CreatedAt     string    `json:"created_at"`
	Attempts      []Attempt `json:"attempts"`
}

type Attempt struct {
	OrgRef                string `json:"org_ref"`
	ProjectRef            string `json:"project_ref"`
	RequestID             string `json:"request_id"`
	AttemptID             string `json:"attempt_id"`
	Provider              string `json:"provider"`
	SourceInstance        string `json:"source_instance"`
	UpstreamRequestID     string `json:"upstream_request_id"`
	OccurredAt            string `json:"occurred_at"`
	ProviderEstimateMinor string `json:"provider_estimate_minor"`
	ProviderCurrency      string `json:"provider_currency"`
	ProviderAmountScale   string `json:"provider_amount_scale"`
	CustomerRevenueMicros string `json:"customer_revenue_micros"`
	CustomerCurrency      string `json:"customer_currency"`
	CustomerAmountScale   string `json:"customer_amount_scale"`
}

type Amount struct {
	ValueMinor string `json:"value_minor"`
	Currency   string `json:"currency"`
	Scale      string `json:"scale"`
	Authority  string `json:"authority"`
}

type Match struct {
	Status             string  `json:"status"`
	EvidenceLevel      string  `json:"evidence_level"`
	LineID             string  `json:"line_id"`
	RequestID          string  `json:"request_id"`
	AttemptID          string  `json:"attempt_id"`
	UpstreamRequestID  string  `json:"upstream_request_id"`
	ProjectRef         string  `json:"project_ref"`
	ProviderCost       *Amount `json:"provider_cost,omitempty"`
	ProviderEstimate   *Amount `json:"provider_estimate,omitempty"`
	ProviderVariance   string  `json:"provider_variance_minor,omitempty"`
	CustomerRevenue    *Amount `json:"customer_revenue,omitempty"`
	CustomerDebtAction string  `json:"customer_debt_action"`
}

type PageEvidence struct {
	PageID         string `json:"page_id"`
	PageIndex      string `json:"page_index"`
	NormalizedHash string `json:"normalized_hash"`
	LineCount      int    `json:"line_count"`
	IsLast         bool   `json:"is_last"`
}

type Summary struct {
	Pages                   int    `json:"pages"`
	UniqueSupplierLines     int    `json:"unique_supplier_lines"`
	DuplicateNoopLines      int    `json:"duplicate_noop_lines"`
	SourceAttempts          int    `json:"source_attempts"`
	ExactRequestMatches     int    `json:"exact_request_matches"`
	SupplierOnly            int    `json:"supplier_only"`
	AggregateUnattributed   int    `json:"aggregate_unattributed"`
	Ambiguous               int    `json:"ambiguous"`
	SourceOnly              int    `json:"source_only"`
	SupplierEvidencePending int    `json:"supplier_evidence_pending"`
	SupplierTotalMinor      string `json:"supplier_total_minor"`
	MatchedSupplierMinor    string `json:"matched_supplier_minor"`
	UnattributedMinor       string `json:"unattributed_minor"`
}

type Report struct {
	ReportVersion          string         `json:"report_version"`
	ContractVersion        string         `json:"contract_version"`
	ImportID               string         `json:"import_id"`
	ImportPayloadHash      string         `json:"import_payload_hash"`
	ImportSemanticHash     string         `json:"import_semantic_hash"`
	SourceSnapshotHash     string         `json:"source_snapshot_hash"`
	Provider               string         `json:"provider"`
	SourceInstance         string         `json:"source_instance"`
	Currency               string         `json:"currency"`
	AmountScale            string         `json:"amount_scale"`
	Coverage               Coverage       `json:"coverage"`
	ReplayStatus           string         `json:"replay_status"`
	CustomerLedgerMutation string         `json:"customer_ledger_mutation"`
	Pages                  []PageEvidence `json:"pages"`
	Matches                []Match        `json:"matches"`
	Summary                Summary        `json:"summary"`
}

type storedImport struct {
	semanticHash string
	report       Report
}

// MemoryStore exists only for deterministic local/offline replay evidence.
// Production persistence and retention remain gated by P7-D05.
type MemoryStore struct {
	mu      sync.Mutex
	imports map[string]storedImport
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{imports: make(map[string]storedImport)}
}

type Service struct {
	store *MemoryStore
}

func NewService(store *MemoryStore) *Service {
	return &Service{store: store}
}
