// Package agenttrust defines UniRouter's zero-side-effect Agent Trust
// Firewall review, signed decision receipt, and Governance Digital Twin
// evidence model.
package agenttrust

import (
	"errors"
	"sync"

	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/governancesim"
)

const (
	ContractVersion          = "unirouter.agent-trust-review.v1"
	ReportVersion            = "unirouter.agent-trust-report.v1"
	ReceiptVersion           = "unirouter.signed-decision-receipt.v1"
	ConformanceReportVersion = "unirouter.agent-trust-conformance-report.v1"
	FixtureSchemaVersion     = "unirouter.agent-trust-fixture.v1"

	ReviewAudience = "unirouter-governance-review"
	MaxInputBytes  = 3 << 20

	DecisionAllow      = "allow"
	DecisionShadowOnly = "shadow_only"
	DecisionDeny       = "deny"

	NodeActive          = "active"
	NodeExpired         = "expired"
	NodeRevoked         = "revoked"
	NodeAncestorRevoked = "ancestor_revoked"

	ReplayAccepted      = "accepted"
	ReplayDuplicateNoop = "duplicate_noop"

	RuntimeMutationNone = "none"
)

var (
	ErrInvalid  = errors.New("agenttrust: invalid contract")
	ErrConflict = errors.New("agenttrust: identity or semantic conflict")
)

type ContractError struct {
	Code string `json:"code"`
	kind error
}

func (e *ContractError) Error() string {
	if e == nil {
		return "agenttrust: unknown error"
	}
	return "agenttrust: " + e.Code
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
	ContractVersion string                          `json:"contract_version"`
	ReviewID        string                          `json:"review_id"`
	EvaluatedAt     string                          `json:"evaluated_at"`
	TenantRef       string                          `json:"tenant_ref"`
	Audience        string                          `json:"audience"`
	SubjectAgentRef string                          `json:"subject_agent_ref"`
	ProvenanceRef   string                          `json:"provenance_ref"`
	Authority       AuthoritySnapshot               `json:"authority"`
	DigitalTwin     governancesim.SimulationRequest `json:"digital_twin"`
	SignedReceipt   SignedDecisionReceipt           `json:"signed_receipt"`
}

type AuthoritySnapshot struct {
	SnapshotRef  string             `json:"snapshot_ref"`
	RootAgentRef string             `json:"root_agent_ref"`
	Nodes        []AuthorityNode    `json:"nodes"`
	Revocations  []RevocationRecord `json:"revocations"`
}

type AuthorityNode struct {
	AgentRef         string          `json:"agent_ref"`
	ParentAgentRef   string          `json:"parent_agent_ref"`
	Depth            int             `json:"depth"`
	AuthorityVersion string          `json:"authority_version"`
	IssuedAt         string          `json:"issued_at"`
	ExpiresAt        string          `json:"expires_at"`
	RevokedAt        string          `json:"revoked_at"`
	Limits           agentcap.Limits `json:"limits"`
}

type RevocationRecord struct {
	EventRef  string `json:"event_ref"`
	AgentRef  string `json:"agent_ref"`
	RevokedAt string `json:"revoked_at"`
	Reason    string `json:"reason"`
}

type DecisionReceiptPayload struct {
	ReceiptVersion          string `json:"receipt_version"`
	ReceiptID               string `json:"receipt_id"`
	ReviewID                string `json:"review_id"`
	TenantRef               string `json:"tenant_ref"`
	Audience                string `json:"audience"`
	Decision                string `json:"decision"`
	DecidedAt               string `json:"decided_at"`
	SubjectRef              string `json:"subject_ref"`
	AuthoritySnapshotHash   string `json:"authority_snapshot_hash"`
	GovernanceRequestHash   string `json:"governance_request_hash"`
	BaselinePolicyHash      string `json:"baseline_policy_hash"`
	CandidatePolicyHash     string `json:"candidate_policy_hash"`
	BaselineRouteHash       string `json:"baseline_route_hash"`
	CandidateRouteHash      string `json:"candidate_route_hash"`
	ProviderPackSetHash     string `json:"provider_pack_set_hash"`
	ProvenanceRef           string `json:"provenance_ref"`
	SynchronousDependency   bool   `json:"synchronous_dependency"`
	RuntimeActionAuthorized bool   `json:"runtime_action_authorized"`
}

type SignedDecisionReceipt struct {
	Payload      DecisionReceiptPayload `json:"payload"`
	Algorithm    string                 `json:"algorithm"`
	KeyID        string                 `json:"key_id"`
	KeyVersion   string                 `json:"key_version"`
	PublicKey    string                 `json:"public_key"`
	Signature    string                 `json:"signature"`
	TrustProfile string                 `json:"trust_profile"`
}

type AttenuationChange struct {
	AgentRef         string   `json:"agent_ref"`
	ParentAgentRef   string   `json:"parent_agent_ref"`
	ReducedFields    []string `json:"reduced_fields"`
	RemovedModels    []string `json:"removed_models"`
	RemovedEndpoints []string `json:"removed_endpoints"`
}

type BudgetProof struct {
	AgentRef            string          `json:"agent_ref"`
	Lineage             []string        `json:"lineage"`
	EffectiveLimits     agentcap.Limits `json:"effective_limits"`
	AncestorBudgetRefs  []string        `json:"ancestor_budget_refs"`
	RemainingTTLSeconds string          `json:"remaining_ttl_seconds"`
}

type AuthorityNodeReport struct {
	AgentRef            string          `json:"agent_ref"`
	ParentAgentRef      string          `json:"parent_agent_ref"`
	Depth               int             `json:"depth"`
	State               string          `json:"state"`
	Lineage             []string        `json:"lineage"`
	EffectiveLimits     agentcap.Limits `json:"effective_limits"`
	RemainingTTLSeconds string          `json:"remaining_ttl_seconds"`
	RevocationEventRef  string          `json:"revocation_event_ref"`
}

type DigitalTwinProof struct {
	ContractVersion       string                             `json:"contract_version"`
	InputSemanticHash     string                             `json:"input_semantic_hash"`
	EvaluationMode        string                             `json:"evaluation_mode"`
	OverallRisk           string                             `json:"overall_risk"`
	RouteChanges          []governancesim.RouteChange        `json:"route_changes"`
	ProviderPackChanges   []governancesim.ProviderPackChange `json:"provider_pack_changes"`
	ReleaseDecision       string                             `json:"release_decision"`
	RuntimeMutation       string                             `json:"runtime_mutation"`
	SynchronousDependency bool                               `json:"synchronous_dependency"`
}

type ReceiptVerification struct {
	ReceiptVersion         string `json:"receipt_version"`
	ReceiptID              string `json:"receipt_id"`
	Algorithm              string `json:"algorithm"`
	KeyID                  string `json:"key_id"`
	KeyVersion             string `json:"key_version"`
	PayloadHash            string `json:"payload_hash"`
	SignatureValid         bool   `json:"signature_valid"`
	TenantBound            bool   `json:"tenant_bound"`
	AudienceBound          bool   `json:"audience_bound"`
	DecisionBound          bool   `json:"decision_bound"`
	CryptographicallyValid bool   `json:"cryptographically_valid"`
	TrustProfile           string `json:"trust_profile"`
	RuntimeTrusted         bool   `json:"runtime_trusted"`
}

type Report struct {
	ReportVersion                string                `json:"report_version"`
	ContractVersion              string                `json:"contract_version"`
	ReviewID                     string                `json:"review_id"`
	TenantRef                    string                `json:"tenant_ref"`
	InputPayloadHash             string                `json:"input_payload_hash"`
	InputSemanticHash            string                `json:"input_semantic_hash"`
	AuthoritySnapshotHash        string                `json:"authority_snapshot_hash"`
	EffectiveAuthorityTree       []AuthorityNodeReport `json:"effective_authority_tree"`
	AttenuationDiffs             []AttenuationChange   `json:"attenuation_diffs"`
	AncestorBudgets              []BudgetProof         `json:"ancestor_budgets"`
	RevocationTimeline           []RevocationRecord    `json:"revocation_timeline"`
	DigitalTwin                  DigitalTwinProof      `json:"digital_twin"`
	SignedReceipt                SignedDecisionReceipt `json:"signed_receipt"`
	ReceiptVerification          ReceiptVerification   `json:"receipt_verification"`
	Decision                     string                `json:"decision"`
	ReplayStatus                 string                `json:"replay_status"`
	SynchronousRuntimeDependency bool                  `json:"synchronous_runtime_dependency"`
	RuntimeEnforcementAuthorized bool                  `json:"runtime_enforcement_authorized"`
	RuntimeMutation              string                `json:"runtime_mutation"`
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
