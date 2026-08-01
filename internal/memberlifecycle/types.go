// Package memberlifecycle defines UniRouter's IdP-neutral project-member
// lifecycle contract and zero-side-effect provisioning conformance model.
package memberlifecycle

import (
	"errors"
	"sync"
)

const (
	ContractVersion          = "unirouter.member-lifecycle.v1"
	ReportVersion            = "unirouter.member-lifecycle-report.v1"
	ConformanceReportVersion = "unirouter.member-lifecycle-conformance-report.v1"
	FixtureSchemaVersion     = "unirouter.member-lifecycle-fixture.v1"

	MaxInputBytes = 1 << 20

	OperationCreate      = "create"
	OperationUpdate      = "update"
	OperationSuspend     = "suspend"
	OperationDeprovision = "deprovision"

	StatusActive        = "active"
	StatusSuspended     = "suspended"
	StatusDeprovisioned = "deprovisioned"

	ReplayAccepted      = "accepted"
	ReplayDuplicateNoop = "duplicate_noop"

	RuntimeMutationNone = "none"
)

var (
	ErrInvalid  = errors.New("memberlifecycle: invalid contract")
	ErrConflict = errors.New("memberlifecycle: identity or revision conflict")
)

type ContractError struct {
	Code string `json:"code"`
	kind error
}

func (e *ContractError) Error() string {
	if e == nil {
		return "memberlifecycle: unknown error"
	}
	return "memberlifecycle: " + e.Code
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

type Membership struct {
	ProjectRef string `json:"project_ref"`
	Role       string `json:"role"`
}

type Capability struct {
	CapabilityRef string `json:"capability_ref"`
	Kind          string `json:"kind"`
	ProjectRef    string `json:"project_ref"`
	Status        string `json:"status"`
}

type StateSnapshot struct {
	Status       string       `json:"status"`
	Role         string       `json:"role"`
	Revision     string       `json:"revision"`
	Memberships  []Membership `json:"memberships"`
	Capabilities []Capability `json:"capabilities"`
}

type Event struct {
	ContractVersion string         `json:"contract_version"`
	EventID         string         `json:"event_id"`
	SubjectRef      string         `json:"subject_ref"`
	SourceRef       string         `json:"source_ref"`
	Operation       string         `json:"operation"`
	OccurredAt      string         `json:"occurred_at"`
	ParentRevision  string         `json:"parent_revision"`
	Revision        string         `json:"revision"`
	Before          *StateSnapshot `json:"before"`
	After           StateSnapshot  `json:"after"`
}

type Revocation struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}

type EnforcementControls struct {
	DenyAdminSessions               bool   `json:"deny_admin_sessions"`
	RevokeLiveAdminSessions         bool   `json:"revoke_live_admin_sessions"`
	DisablePersonalKeysBeforeDelete bool   `json:"disable_personal_keys_before_delete"`
	AgentCapabilityRecheckSeconds   string `json:"agent_capability_recheck_seconds"`
}

type LocalProjection struct {
	UserStatus          string `json:"user_status"`
	MembershipStorage   string `json:"membership_storage"`
	PersonalKeyHandling string `json:"personal_key_handling"`
	SessionHandling     string `json:"session_handling"`
}

type Report struct {
	ReportVersion           string              `json:"report_version"`
	ContractVersion         string              `json:"contract_version"`
	EventID                 string              `json:"event_id"`
	SubjectRef              string              `json:"subject_ref"`
	Operation               string              `json:"operation"`
	Revision                string              `json:"revision"`
	InputPayloadHash        string              `json:"input_payload_hash"`
	InputSemanticHash       string              `json:"input_semantic_hash"`
	StateStatus             string              `json:"state_status"`
	EffectiveMemberships    []Membership        `json:"effective_memberships"`
	EffectiveCapabilities   []Capability        `json:"effective_capabilities"`
	Revocations             []Revocation        `json:"revocations"`
	Controls                EnforcementControls `json:"controls"`
	LocalProjection         LocalProjection     `json:"local_projection"`
	ReplayStatus            string              `json:"replay_status"`
	VendorAdapterAuthorized bool                `json:"vendor_adapter_authorized"`
	RuntimeMutation         string              `json:"runtime_mutation"`
}

type storedEvent struct {
	semanticHash string
	report       Report
}

type subjectState struct {
	revision int64
	snapshot StateSnapshot
	terminal bool
}

type MemoryStore struct {
	mu       sync.Mutex
	events   map[string]storedEvent
	subjects map[string]subjectState
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		events:   make(map[string]storedEvent),
		subjects: make(map[string]subjectState),
	}
}

type Service struct {
	store *MemoryStore
}

func NewService(store *MemoryStore) *Service {
	return &Service{store: store}
}
