// Package governancesim defines UniRouter's zero-side-effect route/policy
// simulator, progressive-release evidence and Provider Change Firewall.
package governancesim

import (
	"errors"
	"sync"

	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const (
	ContractVersion = "unirouter.governance-simulation.v1"
	ReportVersion   = "unirouter.governance-simulation-report.v1"
	ReleaseVersion  = "unirouter.progressive-release.v1"

	MaxInputBytes = 2 << 20

	ModeShadow   = "shadow"
	ModeCanary   = "canary"
	ModeEnforced = "enforced"

	StageDraft        = "draft"
	StageValidated    = "validated"
	StageShadow       = "shadow"
	StageCanary       = "canary"
	StageExternalGate = "approved_for_external_promotion"

	ActionAdvance  = "advance"
	ActionRollback = "rollback"

	RiskUnchanged      = "unchanged"
	RiskCompatible     = "compatible"
	RiskReviewRequired = "review_required"
	RiskBreaking       = "breaking"

	ReplayAccepted      = "accepted"
	ReplayDuplicateNoop = "duplicate_noop"

	RuntimeMutationNone = "none"
)

var (
	ErrInvalid  = errors.New("governancesim: invalid contract")
	ErrConflict = errors.New("governancesim: identity or revision conflict")
)

type ContractError struct {
	Code string `json:"code"`
	kind error
}

func (e *ContractError) Error() string {
	if e == nil {
		return "governancesim: unknown error"
	}
	return "governancesim: " + e.Code
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

type SimulationRequest struct {
	ContractVersion string        `json:"contract_version"`
	SimulationID    string        `json:"simulation_id"`
	CreatedAt       string        `json:"created_at"`
	SelectionKey    string        `json:"selection_key"`
	EvaluationMode  string        `json:"evaluation_mode"`
	Baseline        Bundle        `json:"baseline"`
	Candidate       Bundle        `json:"candidate"`
	Release         ReleaseIntent `json:"release"`
}

type Bundle struct {
	Policy        routingpolicy.Config   `json:"policy"`
	RouteSnapshot RouteSnapshot          `json:"route_snapshot"`
	ProviderPacks []ProviderPackSnapshot `json:"provider_packs"`
}

type RouteSnapshot struct {
	SnapshotID string            `json:"snapshot_id"`
	Model      string            `json:"model"`
	DBNow      string            `json:"db_now"`
	Source     string            `json:"source"`
	Degraded   bool              `json:"degraded"`
	Channels   []ChannelSnapshot `json:"channels"`
}

type ChannelSnapshot struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Provider      string        `json:"provider"`
	Priority      int           `json:"priority"`
	Weight        int           `json:"weight"`
	Status        string        `json:"status"`
	CircuitState  string        `json:"circuit_state"`
	HalfOpenUntil string        `json:"half_open_until"`
	Keys          []KeySnapshot `json:"keys"`
}

type KeySnapshot struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	CooldownUntil string `json:"cooldown_until"`
	AllowedModels string `json:"allowed_models"`
}

type ProviderPackSnapshot struct {
	SchemaVersion   string            `json:"schema_version"`
	SchemaSHA256    string            `json:"schema_sha256"`
	PackID          string            `json:"pack_id"`
	PackVersion     string            `json:"pack_version"`
	ManifestSHA256  string            `json:"manifest_sha256"`
	Provider        string            `json:"provider"`
	AdapterType     string            `json:"adapter_type"`
	NetworkVerified bool              `json:"network_verified"`
	Endpoints       []string          `json:"endpoints"`
	Modalities      []string          `json:"modalities"`
	AuthProfiles    []string          `json:"auth_profiles"`
	UsageSignature  string            `json:"usage_signature"`
	ErrorSignature  string            `json:"error_signature"`
	Fixtures        []FixtureSnapshot `json:"fixtures"`
}

type FixtureSnapshot struct {
	FixtureID string `json:"fixture_id"`
	SHA256    string `json:"sha256"`
}

type ReleaseIntent struct {
	ContractVersion    string `json:"contract_version"`
	ReleaseID          string `json:"release_id"`
	Revision           string `json:"revision"`
	ParentRevision     string `json:"parent_revision"`
	Action             string `json:"action"`
	FromStage          string `json:"from_stage"`
	ToStage            string `json:"to_stage"`
	RollbackToRevision string `json:"rollback_to_revision"`
	ActorRef           string `json:"actor_ref"`
	OccurredAt         string `json:"occurred_at"`
	Reason             string `json:"reason"`
}

type RouteCandidate struct {
	ChannelID        string `json:"channel_id"`
	ChannelName      string `json:"channel_name"`
	Provider         string `json:"provider"`
	KeyID            string `json:"key_id,omitempty"`
	Priority         int    `json:"priority"`
	Weight           int    `json:"weight"`
	Order            int    `json:"order,omitempty"`
	CircuitState     string `json:"circuit_state"`
	EligibleKeyCount int    `json:"eligible_key_count"`
	Reason           string `json:"reason"`
	RequiresProbe    bool   `json:"requires_probe"`
}

type RouteOutcome struct {
	SnapshotID       string           `json:"snapshot_id"`
	SnapshotHash     string           `json:"snapshot_hash"`
	PolicyVersion    string           `json:"policy_version"`
	SelectionVersion string           `json:"selection_version"`
	Candidates       []RouteCandidate `json:"candidates"`
	Exclusions       []RouteCandidate `json:"exclusions"`
	ExcludedCount    int              `json:"excluded_count"`
	ExclusionReasons string           `json:"exclusion_reasons"`
}

type RouteChange struct {
	Field     string `json:"field"`
	Baseline  string `json:"baseline"`
	Candidate string `json:"candidate"`
	Risk      string `json:"risk"`
	Reason    string `json:"reason"`
}

type ProviderPackChange struct {
	PackID           string   `json:"pack_id"`
	BaselineVersion  string   `json:"baseline_version"`
	CandidateVersion string   `json:"candidate_version"`
	Risk             string   `json:"risk"`
	Reasons          []string `json:"reasons"`
}

type DiffSummary struct {
	Unchanged      int `json:"unchanged"`
	Compatible     int `json:"compatible"`
	ReviewRequired int `json:"review_required"`
	Breaking       int `json:"breaking"`
}

type ReleaseReceipt struct {
	ContractVersion            string `json:"contract_version"`
	ReleaseID                  string `json:"release_id"`
	Revision                   string `json:"revision"`
	Action                     string `json:"action"`
	FromStage                  string `json:"from_stage"`
	SimulatedStage             string `json:"simulated_stage"`
	RollbackToRevision         string `json:"rollback_to_revision"`
	AuditEventID               string `json:"audit_event_id"`
	Decision                   string `json:"decision"`
	PromotionGate              string `json:"promotion_gate"`
	RuntimePromotionAuthorized bool   `json:"runtime_promotion_authorized"`
	RuntimeMutation            string `json:"runtime_mutation"`
}

type Report struct {
	ReportVersion       string               `json:"report_version"`
	ContractVersion     string               `json:"contract_version"`
	SimulationID        string               `json:"simulation_id"`
	InputPayloadHash    string               `json:"input_payload_hash"`
	InputSemanticHash   string               `json:"input_semantic_hash"`
	EvaluationMode      string               `json:"evaluation_mode"`
	Baseline            RouteOutcome         `json:"baseline"`
	Candidate           RouteOutcome         `json:"candidate"`
	RouteChanges        []RouteChange        `json:"route_changes"`
	ProviderPackChanges []ProviderPackChange `json:"provider_pack_changes"`
	Summary             DiffSummary          `json:"summary"`
	OverallRisk         string               `json:"overall_risk"`
	Release             ReleaseReceipt       `json:"release"`
	ReplayStatus        string               `json:"replay_status"`
	RuntimeMutation     string               `json:"runtime_mutation"`
}

type storedSimulation struct {
	semanticHash string
	report       Report
}

type releaseState struct {
	revision int64
	stage    string
	history  map[int64]string
}

// MemoryStore exists only for deterministic offline replay and lifecycle
// evidence. It is not production release state.
type MemoryStore struct {
	mu          sync.Mutex
	simulations map[string]storedSimulation
	releases    map[string]releaseState
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		simulations: make(map[string]storedSimulation),
		releases:    make(map[string]releaseState),
	}
}

type Service struct {
	store *MemoryStore
}

func NewService(store *MemoryStore) *Service {
	return &Service{store: store}
}
