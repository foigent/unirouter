// Package riskcontrol composes UniRouter's immutable Governance Digital Twin,
// Agent Trust, and Provenance Passport evidence into bounded routing advice and
// a human-reviewed control proposal. It never promotes or enforces a runtime
// policy.
package riskcontrol

import (
	"errors"
	"sync"

	"github.com/uniroute/unirouter/internal/agenttrust"
	"github.com/uniroute/unirouter/internal/provenancepassport"
)

const (
	ContractVersion          = "unirouter.risk-control-review.v1"
	ReportVersion            = "unirouter.risk-control-report.v1"
	AssertionVersion         = "unirouter.bounded-risk-assertion.v1"
	ProposalVersion          = "unirouter.evidence-control-proposal.v1"
	PolicyVersion            = "unirouter.risk-advice-policy.v1"
	FixtureSchemaVersion     = "unirouter.risk-control-fixture.v1"
	ConformanceReportVersion = "unirouter.risk-control-conformance-report.v1"

	ReviewAudience         = "unirouter-risk-control-review"
	AssertionScope         = "routing_advice_only"
	AssertionTTL           = "300"
	PromotionGate          = "P7-D04"
	MaxInputBytes          = 6 << 20
	RuntimeMutationNone    = "none"
	ScopeBindingUnverified = "unverified_product_local_mapping"

	AdviceAllowBaseline = "allow_baseline"
	AdviceShadowOnly    = "shadow_only"
	AdviceDeny          = "deny_advice"

	RiskLow      = "low"
	RiskElevated = "elevated"
	RiskHigh     = "high"
	RiskKnownGap = "known_gap"

	ProposalNotCreated              = "not_created"
	ProposalSimulatedReviewRequired = "simulated_review_required"

	ReplayAccepted      = "accepted"
	ReplayDuplicateNoop = "duplicate_noop"
)

var (
	ErrInvalid  = errors.New("riskcontrol: invalid contract")
	ErrConflict = errors.New("riskcontrol: identity or semantic conflict")
)

type ContractError struct {
	Code string `json:"code"`
	kind error
}

func (e *ContractError) Error() string {
	if e == nil {
		return "riskcontrol: unknown error"
	}
	return "riskcontrol: " + e.Code
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

type ReviewRequest struct {
	ContractVersion     string                     `json:"contract_version"`
	ReviewID            string                     `json:"review_id"`
	EvaluatedAt         string                     `json:"evaluated_at"`
	TenantRef           string                     `json:"tenant_ref"`
	Audience            string                     `json:"audience"`
	PolicyVersion       string                     `json:"policy_version"`
	RiskEpoch           string                     `json:"risk_epoch"`
	AssertionTTLSeconds string                     `json:"assertion_ttl_seconds"`
	AgentTrust          agenttrust.ReviewRequest   `json:"agent_trust"`
	ProvenancePassport  provenancepassport.Request `json:"provenance_passport"`
	EvidenceBinding     EvidenceBinding            `json:"evidence_binding"`
}

type EvidenceBinding struct {
	ProvenanceRef                string `json:"provenance_ref"`
	AgentTrustInputSemanticHash  string `json:"agent_trust_input_semantic_hash"`
	AgentTrustReceiptPayloadHash string `json:"agent_trust_receipt_payload_hash"`
	PassportInputSemanticHash    string `json:"passport_input_semantic_hash"`
	PassportSourceHash           string `json:"passport_source_hash"`
	PassportEnvelopeHash         string `json:"passport_envelope_hash"`
	PassportSourceScopeHash      string `json:"passport_source_scope_hash"`
}

type RiskDimensions struct {
	Authority  string `json:"authority"`
	Change     string `json:"change"`
	Provenance string `json:"provenance"`
	Financial  string `json:"financial"`
}

type EvidenceSummary struct {
	AgentTrustDecision            string              `json:"agent_trust_decision"`
	AgentTrustReportHash          string              `json:"agent_trust_report_hash"`
	ReceiptPayloadHash            string              `json:"receipt_payload_hash"`
	ReceiptCryptographicallyValid bool                `json:"receipt_cryptographically_valid"`
	ReceiptRuntimeTrusted         bool                `json:"receipt_runtime_trusted"`
	ProvenancePassportHash        string              `json:"provenance_passport_hash"`
	SourceHash                    string              `json:"source_hash"`
	EnvelopeHash                  string              `json:"envelope_hash"`
	TerminalStatus                string              `json:"terminal_status"`
	CaptureGapCount               int                 `json:"capture_gap_count"`
	GovernanceRequestHash         string              `json:"governance_request_hash"`
	GovernanceOverallRisk         string              `json:"governance_overall_risk"`
	SourceScope                   SourceScopeEvidence `json:"source_scope"`
}

type SourceScopeEvidence struct {
	OrgRef              string `json:"org_ref"`
	ProjectRef          string `json:"project_ref"`
	SourceInstance      string `json:"source_instance"`
	ScopeHash           string `json:"scope_hash"`
	TenantBindingStatus string `json:"tenant_binding_status"`
}

type BoundedRiskAssertion struct {
	AssertionVersion                    string         `json:"assertion_version"`
	AssertionID                         string         `json:"assertion_id"`
	TenantRef                           string         `json:"tenant_ref"`
	Audience                            string         `json:"audience"`
	SubjectRef                          string         `json:"subject_ref"`
	IssuedAt                            string         `json:"issued_at"`
	ExpiresAt                           string         `json:"expires_at"`
	RiskEpoch                           string         `json:"risk_epoch"`
	Scope                               string         `json:"scope"`
	Advice                              string         `json:"advice"`
	Dimensions                          RiskDimensions `json:"dimensions"`
	EvidenceBindingHash                 string         `json:"evidence_binding_hash"`
	AgentTrustReportHash                string         `json:"agent_trust_report_hash"`
	ProvenancePassportReportHash        string         `json:"provenance_passport_report_hash"`
	GovernanceRequestHash               string         `json:"governance_request_hash"`
	AuthorityEffect                     string         `json:"authority_effect"`
	SignatureState                      string         `json:"signature_state"`
	MissingEvidenceCanIncreasePrivilege bool           `json:"missing_evidence_can_increase_privilege"`
	SynchronousDependency               bool           `json:"synchronous_dependency"`
	RuntimeEnforcementAuthorized        bool           `json:"runtime_enforcement_authorized"`
	AssertionHash                       string         `json:"assertion_hash"`
}

type ProposedControl struct {
	ControlID             string   `json:"control_id"`
	TargetOwner           string   `json:"target_owner"`
	ProposedEffect        string   `json:"proposed_effect"`
	RationaleCode         string   `json:"rationale_code"`
	EvidenceRefs          []string `json:"evidence_refs"`
	EnforcementAuthorized bool     `json:"enforcement_authorized"`
}

type ProposalLifecycleStep struct {
	State       string `json:"state"`
	OccurredAt  string `json:"occurred_at"`
	EvidenceRef string `json:"evidence_ref"`
}

type SimulationEvidence struct {
	ContractVersion   string `json:"contract_version"`
	InputSemanticHash string `json:"input_semantic_hash"`
	EvaluationMode    string `json:"evaluation_mode"`
	OverallRisk       string `json:"overall_risk"`
	ReleaseDecision   string `json:"release_decision"`
	RuntimeMutation   string `json:"runtime_mutation"`
	ReusedDirectly    bool   `json:"reused_directly"`
}

type ControlProposal struct {
	ProposalVersion            string                  `json:"proposal_version"`
	ProposalID                 string                  `json:"proposal_id"`
	State                      string                  `json:"state"`
	Trigger                    string                  `json:"trigger"`
	Controls                   []ProposedControl       `json:"controls"`
	Lifecycle                  []ProposalLifecycleStep `json:"lifecycle"`
	Simulation                 SimulationEvidence      `json:"simulation"`
	HumanReviewRequired        bool                    `json:"human_review_required"`
	SelfPromoted               bool                    `json:"self_promoted"`
	RuntimePromotionAuthorized bool                    `json:"runtime_promotion_authorized"`
	PromotionGate              string                  `json:"promotion_gate"`
	ProposalHash               string                  `json:"proposal_hash"`
}

type RuntimeBoundary struct {
	Mode                         string `json:"mode"`
	Persistent                   bool   `json:"persistent"`
	RemoteRiskDependency         bool   `json:"remote_risk_dependency"`
	ProviderOrShadowTraffic      bool   `json:"provider_or_shadow_traffic"`
	CredentialsAccepted          bool   `json:"credentials_accepted"`
	RuntimePolicyRead            bool   `json:"runtime_policy_read"`
	RuntimeEnforcementAuthorized bool   `json:"runtime_enforcement_authorized"`
	RuntimePromotionAuthorized   bool   `json:"runtime_promotion_authorized"`
	RuntimeMutation              string `json:"runtime_mutation"`
	ExternalDecisionGate         string `json:"external_decision_gate"`
}

type Report struct {
	ReportVersion       string               `json:"report_version"`
	ContractVersion     string               `json:"contract_version"`
	ReviewID            string               `json:"review_id"`
	TenantRef           string               `json:"tenant_ref"`
	InputPayloadHash    string               `json:"input_payload_hash"`
	InputSemanticHash   string               `json:"input_semantic_hash"`
	EvidenceBindingHash string               `json:"evidence_binding_hash"`
	Evidence            EvidenceSummary      `json:"evidence"`
	Advice              string               `json:"advice"`
	Dimensions          RiskDimensions       `json:"dimensions"`
	Assertion           BoundedRiskAssertion `json:"assertion"`
	Proposal            ControlProposal      `json:"proposal"`
	ReplayStatus        string               `json:"replay_status"`
	RuntimeBoundary     RuntimeBoundary      `json:"runtime_boundary"`
}

type storedReview struct {
	semanticHash string
	report       Report
}

type MemoryStore struct {
	mu      sync.Mutex
	reviews map[string]storedReview
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{reviews: make(map[string]storedReview)}
}

type Service struct {
	store *MemoryStore
}

func NewService(store *MemoryStore) *Service {
	return &Service{store: store}
}
