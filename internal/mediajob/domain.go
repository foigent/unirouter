// Package mediajob implements the UniRouter-owned asynchronous image job
// plane. It is deliberately separate from the synchronous model relay and from
// the shared Event/Connector protocol authority.
package mediajob

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/limitpolicy"
)

const (
	ContractVersion = "unirouter.async-media-runtime.v1"

	OperationTextToImage = "text_to_image"

	AdmissionAccepted = "accepted"

	QueueQueued   = "queued"
	QueueLeased   = "leased"
	QueueTerminal = "terminal"

	LeaseUnleased = "unleased"
	LeaseLeased   = "leased"
	LeaseReleased = "released"

	ExecutionPending  = "pending"
	ExecutionRunning  = "running"
	ExecutionTerminal = "terminal"

	OutcomePending    = "pending"
	OutcomeSucceeded  = "succeeded"
	OutcomeFailed     = "failed"
	OutcomeNotStarted = "not_started"
	OutcomeUnknown    = "unknown"

	CancellationNotRequested  = "not_requested"
	CancellationRequested     = "requested"
	CancellationAccepted      = "accepted"
	CancellationTooLate       = "too_late"
	CancellationIndeterminate = "indeterminate"

	ObjectPending      = "pending"
	ObjectPersisted    = "persisted"
	ObjectPurgePending = "purge_pending"
	ObjectPurged       = "purged"
	ObjectNotCreated   = "not_created"

	SettlementNotEligible        = "not_eligible"
	SettlementChargeFactRecorded = "charge_fact_recorded"

	ChargeAmountPricingPending = "pricing_pending_media_rate_card"

	DefaultRetention = 7 * 24 * time.Hour
)

var (
	ErrInvalid       = errors.New("mediajob: invalid input")
	ErrNotFound      = errors.New("mediajob: not found")
	ErrConflict      = errors.New("mediajob: conflict")
	ErrForbidden     = errors.New("mediajob: forbidden")
	ErrRateLimited   = errors.New("mediajob: rate limited")
	ErrUnavailable   = errors.New("mediajob: unavailable")
	ErrLeaseLost     = errors.New("mediajob: lease lost")
	ErrCancelled     = errors.New("mediajob: cancellation accepted")
	ErrOutputExpired = errors.New("mediajob: output expired")
)

type Error struct {
	Code string `json:"code"`
	kind error
}

func (e *Error) Error() string {
	if e == nil {
		return "mediajob: unknown error"
	}
	return "mediajob: " + e.Code
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.kind
}

func domainError(kind error, code string) error {
	return &Error{Code: code, kind: kind}
}

type Scope struct {
	OrgID     int64 `json:"org_id"`
	ProjectID int64 `json:"project_id"`
}

func (s Scope) Valid() bool {
	return s.OrgID > 0 && s.ProjectID > 0
}

type SubmitRequest struct {
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
	N            *int64 `json:"n,omitempty"`
	Size         string `json:"size,omitempty"`
	Quality      string `json:"quality,omitempty"`
	Background   string `json:"background,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
	Stream       *bool  `json:"stream,omitempty"`
}

type NormalizedRequest struct {
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
	N            int64  `json:"n"`
	Size         string `json:"size,omitempty"`
	Quality      string `json:"quality,omitempty"`
	Background   string `json:"background,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
	Stream       bool   `json:"stream"`
}

type Lifecycle struct {
	Admission    string `json:"admission"`
	Queue        string `json:"queue"`
	Lease        string `json:"lease"`
	Execution    string `json:"execution"`
	Outcome      string `json:"provider_outcome"`
	Cancellation string `json:"cancellation"`
	Object       string `json:"object"`
	Settlement   string `json:"settlement"`
}

type Output struct {
	ContentSHA256 string     `json:"content_sha256"`
	MediaType     string     `json:"media_type"`
	SizeBytes     int64      `json:"size_bytes"`
	PersistedAt   *time.Time `json:"persisted_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	PurgedAt      *time.Time `json:"purged_at,omitempty"`
}

type ChargeFact struct {
	FactID       string                             `json:"fact_id"`
	Trigger      string                             `json:"trigger"`
	AmountState  string                             `json:"amount_state"`
	AmountMicros *int64                             `json:"amount_micros,omitempty"`
	Units        []endpointprotocol.UnitObservation `json:"units"`
	RecordedAt   time.Time                          `json:"recorded_at"`
}

type Job struct {
	ContractVersion        string     `json:"contract_version"`
	JobID                  string     `json:"job_id"`
	Scope                  Scope      `json:"scope"`
	APIKeyID               int64      `json:"-"`
	AccessGrantID          *int64     `json:"-"`
	AccessCredentialID     *int64     `json:"-"`
	CredentialRef          string     `json:"-"`
	IdempotencySubjectType string     `json:"-"`
	IdempotencySubjectID   int64      `json:"-"`
	RequestID              string     `json:"request_id"`
	ClientRequestID        string     `json:"client_request_id,omitempty"`
	Operation              string     `json:"operation"`
	ProfileID              string     `json:"profile_id"`
	RequestedModel         string     `json:"requested_model"`
	RoutingModel           string     `json:"routing_model"`
	MappedModel            string     `json:"mapped_model,omitempty"`
	RequestSHA256          string     `json:"request_sha256"`
	RequestExpiresAt       *time.Time `json:"request_expires_at,omitempty"`
	RequestClearedAt       *time.Time `json:"request_cleared_at,omitempty"`
	ExecutionStorageMode   string     `json:"execution_storage_mode,omitempty"`
	ContentPolicyRef       string     `json:"content_policy_ref,omitempty"`
	ContentPolicyVersion   string     `json:"content_policy_version,omitempty"`
	ContentPolicyVector    string     `json:"content_policy_vector,omitempty"`

	IdempotencyHash   string     `json:"-"`
	RequestCiphertext string     `json:"-"`
	ObjectKey         string     `json:"-"`
	ReceiptKey        string     `json:"-"`
	PurgeOwner        string     `json:"-"`
	PurgeFence        int64      `json:"-"`
	PurgeExpiresAt    *time.Time `json:"-"`
	PurgeAttempt      int        `json:"-"`

	Lifecycle Lifecycle `json:"lifecycle"`

	Attempt        int        `json:"attempt"`
	MaxAttempts    int        `json:"max_attempts"`
	LeaseOwner     string     `json:"-"`
	LeaseFence     int64      `json:"lease_fence"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`

	Output     *Output     `json:"output,omitempty"`
	ChargeFact *ChargeFact `json:"charge_fact,omitempty"`
	ErrorCode  string      `json:"error_code,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	TerminalAt *time.Time `json:"terminal_at,omitempty"`
}

func (j Job) Terminal() bool {
	return j.Lifecycle.Execution == ExecutionTerminal
}

func (j Job) ContentReadable(now time.Time) bool {
	return j.Lifecycle.Object == ObjectPersisted &&
		j.Output != nil &&
		j.Output.ExpiresAt != nil &&
		now.UTC().Before(j.Output.ExpiresAt.UTC())
}

type CreateRecord struct {
	Job                  Job
	AllowCreate          bool
	ExpectedLimitSubject limitpolicy.Subject
	RequestTTL           time.Duration
}

type IdempotencyLookup struct {
	Scope       Scope
	SubjectType string
	SubjectID   int64
	Hash        string
}

type ExecutionReservation struct {
	JobID           string
	Scope           Scope
	LeaseFence      int64
	OperationID     string
	Reservation     limitpolicy.DurableReservation
	ExpiresAt       time.Time
	TerminalKind    string
	TerminalizedAt  *time.Time
	TerminalOwner   string
	TerminalFence   int64
	ProviderAttempt int
}

type LimitPolicyRuntime interface {
	Reserve(context.Context, limitpolicy.AdmissionRequest) (*limitpolicy.Reservation, error)
	Finalize(context.Context, *limitpolicy.Reservation, int64) error
	Cancel(context.Context, *limitpolicy.Reservation) error
	CancelBeforeDispatch(context.Context, *limitpolicy.Reservation) error
	TerminalStateTTL(time.Duration) time.Duration
}

type CreateResult struct {
	Job    Job  `json:"job"`
	Reused bool `json:"reused"`
}

type Lease struct {
	JobID     string
	Scope     Scope
	Owner     string
	Fence     int64
	Attempt   int
	ExpiresAt time.Time
	Job       Job
	Request   NormalizedRequest
}

type ProviderResult struct {
	MappedModel string
	Bytes       []byte
	MediaType   string
	Outcome     endpointprotocol.Outcome
}

type PersistedResult struct {
	ObjectKey     string
	ReceiptKey    string
	ContentSHA256 string
	MediaType     string
	SizeBytes     int64
	PersistedAt   time.Time
	Outcome       endpointprotocol.Outcome
	MappedModel   string
}

type ListFilter struct {
	Scope       Scope
	ProjectID   int64
	Limit       int
	Before      time.Time
	BeforeJobID string
}

type ListResult struct {
	Data       []Job  `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type CancellationResult struct {
	Job     Job    `json:"job"`
	Outcome string `json:"outcome"`
}

type OutputContent struct {
	MediaType string
	Bytes     []byte
}

type PayloadCipher interface {
	Seal([]byte) (string, error)
	Open(string) ([]byte, error)
}

type ModelAuthorization struct {
	RoutingModel string
}

type ModelAuthorizer interface {
	AuthorizeModel(context.Context, Scope, string) (ModelAuthorization, error)
}

type ExecutionAuthorization struct {
	Scope              Scope
	APIKeyID           int64
	AccessCredentialID int64
	OwnerUserID        int64
	RequestedModel     string
	RoutingModel       string
}

// ExecutionModelAuthorizer is the explicit-identity counterpart of the HTTP
// submit authorizer. The supplied querier is the caller's current pool or
// commit transaction, so worker dispatch never depends on middleware context.
type ExecutionModelAuthorizer interface {
	AuthorizeExecution(context.Context, db.DBQuerier, ExecutionAuthorization) error
}

type ExecutionModelAuthorizerFunc func(context.Context, db.DBQuerier, ExecutionAuthorization) error

func (f ExecutionModelAuthorizerFunc) AuthorizeExecution(
	ctx context.Context,
	q db.DBQuerier,
	input ExecutionAuthorization,
) error {
	return f(ctx, q, input)
}

type ModelAuthorizerFunc func(context.Context, Scope, string) (ModelAuthorization, error)

func (f ModelAuthorizerFunc) AuthorizeModel(
	ctx context.Context,
	scope Scope,
	requestedModel string,
) (ModelAuthorization, error) {
	return f(ctx, scope, requestedModel)
}

type ServiceStore interface {
	FindByIdempotency(context.Context, IdempotencyLookup) (Job, error)
	CreateOrGet(context.Context, CreateRecord) (Job, bool, error)
	Get(context.Context, Scope, string) (Job, error)
	List(context.Context, ListFilter) (ListResult, error)
	RequestCancellation(context.Context, Scope, string, time.Time) (CancellationResult, error)
}

type WorkerStore interface {
	Get(context.Context, Scope, string) (Job, error)
	Claim(context.Context, string, time.Time, time.Duration) (*Job, error)
	MarkRunning(context.Context, Lease, time.Time) error
	MarkProviderSucceeded(context.Context, Lease, string, time.Time) error
	CompleteSuccess(context.Context, Lease, PersistedResult, time.Duration, time.Time) (Job, error)
	CompleteFailure(context.Context, Lease, string, time.Time) (Job, error)
	CompleteUnknown(context.Context, Lease, string, time.Time) (Job, error)
	CompleteOutputFailure(context.Context, Lease, string, time.Time) (Job, error)
	Requeue(context.Context, Lease, string, time.Time, time.Time) (Job, error)
	RequeueRecovery(context.Context, Lease, string, time.Time, time.Time) (Job, error)
	ClaimExpired(context.Context, string, time.Time, time.Duration) (*PurgeLease, error)
	MarkPurgeRetry(context.Context, PurgeLease, string, time.Time, time.Time) error
	MarkPurged(context.Context, PurgeLease, time.Time) (Job, error)
	ExecutionLimitSubject(context.Context, Lease) (limitpolicy.Subject, error)
	AttachExecutionReservation(context.Context, Lease, limitpolicy.Subject, *limitpolicy.DurableReservation, string, time.Time, time.Time) (ExecutionReservation, bool, error)
	PendingExecutionTerminal(context.Context, string, int64, string, time.Time, time.Duration) (*ExecutionReservation, error)
	MarkExecutionTerminalized(context.Context, ExecutionReservation, time.Time, string) error
	ReleaseExecutionTerminalClaim(context.Context, ExecutionReservation, time.Time, string) error
}

type Store interface {
	ServiceStore
	WorkerStore
}

type ObjectStore interface {
	Persist(context.Context, Job, ProviderResult) (PersistedResult, error)
	Recover(context.Context, Job) (*PersistedResult, error)
	Read(context.Context, Job) (OutputContent, error)
	Delete(context.Context, Job) error
}

type Provider interface {
	Generate(context.Context, Job, NormalizedRequest) (ProviderResult, error)
}

type PurgeLease struct {
	Job       Job
	Owner     string
	Fence     int64
	ExpiresAt time.Time
}

type ProviderError struct {
	Code           string
	Retryable      bool
	OutcomeUnknown bool
}

func (e *ProviderError) Error() string {
	if e == nil {
		return "mediajob: provider error"
	}
	return "mediajob: provider " + e.Code
}

type RuntimeEvidence struct {
	ContractVersion  string `json:"contract_version"`
	ProfileID        string `json:"profile_id"`
	Retention        string `json:"retention"`
	VideoEnabled     bool   `json:"video_enabled"`
	RuntimeEnabled   bool   `json:"runtime_enabled"`
	AdmissionEnabled bool   `json:"admission_enabled"`
	ChargeTrigger    string `json:"charge_trigger"`
	AmountState      string `json:"amount_state"`
}

func Evidence(runtimeEnabled bool, admissionEnabled bool) RuntimeEvidence {
	return RuntimeEvidence{
		ContractVersion:  ContractVersion,
		ProfileID:        endpointprotocol.ProfileOpenAIImageGPT,
		Retention:        "P7D",
		VideoEnabled:     false,
		RuntimeEnabled:   runtimeEnabled,
		AdmissionEnabled: admissionEnabled,
		ChargeTrigger:    "provider_terminal_success_and_output_persisted",
		AmountState:      ChargeAmountPricingPending,
	}
}

func cloneJob(job Job) Job {
	body, _ := json.Marshal(job)
	var clone Job
	_ = json.Unmarshal(body, &clone)
	clone.APIKeyID = job.APIKeyID
	clone.AccessGrantID = job.AccessGrantID
	clone.AccessCredentialID = job.AccessCredentialID
	clone.CredentialRef = job.CredentialRef
	clone.IdempotencySubjectType = job.IdempotencySubjectType
	clone.IdempotencySubjectID = job.IdempotencySubjectID
	clone.IdempotencyHash = job.IdempotencyHash
	clone.RequestCiphertext = job.RequestCiphertext
	clone.ObjectKey = job.ObjectKey
	clone.ReceiptKey = job.ReceiptKey
	clone.LeaseOwner = job.LeaseOwner
	clone.PurgeOwner = job.PurgeOwner
	clone.PurgeFence = job.PurgeFence
	clone.PurgeExpiresAt = job.PurgeExpiresAt
	clone.PurgeAttempt = job.PurgeAttempt
	return clone
}
