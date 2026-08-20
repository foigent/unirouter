package limitpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/workloadadmission"
)

const (
	ModeDisabled = "disabled"
	ModeShadow   = "shadow"
	ModeEnforce  = "enforce"

	FailureFailClosed    = "fail_closed"
	FailureLastKnownSafe = "last_known_safe"

	ReasonAllowed           = "allowed"
	ReasonRPSExceeded       = "rps_exceeded"
	ReasonRPMExceeded       = "rpm_exceeded"
	ReasonTPMExceeded       = "tpm_exceeded"
	ReasonConcurrency       = "concurrency_exceeded"
	ReasonStateUnavailable  = "state_unavailable"
	ReasonLastKnownSafe     = "last_known_safe"
	ReasonConflict          = "operation_conflict"
	ReasonPolicyChainLarge  = "policy_chain_too_large"
	ReasonSnapshotStale     = "policy_snapshot_stale"
	ReasonWorkloadCapacity  = "workload_capacity_exceeded"
	ReasonWorkloadQueueFull = "workload_queue_full"

	DecisionAllow     = "allow"
	DecisionDeny      = "deny"
	DecisionWouldDeny = "would_deny"

	AdmissionPhaseFull           = "full"
	AdmissionPhaseRate           = "rate"
	AdmissionPhaseUnscopedRate   = "unscoped_rate"
	AdmissionPhaseExecution      = "execution"
	AdmissionPhaseModelExecution = "model_execution"

	ReadinessReady       = "ready"
	ReadinessBlocked     = "blocked"
	ReadinessUnavailable = "unavailable"
)

var (
	ErrInvalidPolicy       = errors.New("invalid distributed limit policy")
	ErrInvalidRequest      = errors.New("invalid distributed limit request")
	ErrRPSExceeded         = errors.New("distributed policy RPS exceeded")
	ErrRPMExceeded         = errors.New("distributed policy RPM exceeded")
	ErrTPMExceeded         = errors.New("distributed policy TPM exceeded")
	ErrConcurrencyExceeded = errors.New("distributed policy concurrency exceeded")
	ErrWorkloadCapacity    = errors.New("weighted workload capacity exceeded")
	ErrWorkloadConfig      = errors.New("weighted workload configuration conflict")
	ErrStateUnavailable    = errors.New("distributed policy state unavailable")
	ErrConflict            = errors.New("distributed policy operation conflict")
)

// Policy is one versioned, monotonic constraint. Every matching policy is
// applied; a more-specific selector never overrides or widens another limit.
type Policy struct {
	ID                 int64     `json:"id"`
	Ref                string    `json:"ref"`
	Name               string    `json:"name"`
	Version            int64     `json:"version"`
	OrgID              *int64    `json:"org_id,omitempty"`
	ProjectID          *int64    `json:"project_id,omitempty"`
	APIKeyID           *int64    `json:"api_key_id,omitempty"`
	AccessCredentialID *int64    `json:"access_credential_id,omitempty"`
	AccessGrantID      *int64    `json:"access_grant_id,omitempty"`
	Model              string    `json:"model,omitempty"`
	PolicyLabel        string    `json:"policy_label,omitempty"`
	Mode               string    `json:"mode"`
	FailureMode        string    `json:"failure_mode"`
	RPSLimit           int64     `json:"rps_limit,omitempty"`
	RPMLimit           int64     `json:"rpm_limit,omitempty"`
	TPMLimit           int64     `json:"tpm_limit,omitempty"`
	ConcurrencyLimit   int64     `json:"concurrency_limit,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
	Builtin            bool      `json:"builtin"`
	RuntimeBucket      string    `json:"-"`
}

type Subject struct {
	OrgID                      int64
	ProjectID                  int64
	APIKeyID                   int64
	AccessCredentialID         int64
	CredentialRef              string
	AccessGrantID              int64
	Model                      string
	PolicyLabels               []string
	BaseRPSLimit               int64
	BaseRPMLimit               int64
	CredentialConcurrencyLimit *int64
	GrantRPSLimit              *int64
	GrantRPMLimit              *int64
	GrantConcurrencyLimit      *int64
}

type AdmissionRequest struct {
	OperationID string
	Subject     Subject
	// RequestedModel is the normalized public model identity authorized by the
	// Gateway before model_execution. During unscoped_rate it is receipt binding
	// only and must never participate in policy selection.
	RequestedModel string
	EstimatedInput int64
	ReservedOutput int64
	LeaseTTL       time.Duration
	WorkloadClass  string
	// RateReceipt is an opaque, service-authenticated receipt from the
	// preceding finalized rate phase. Model execution skips only an unchanged,
	// exactly signed RPS/RPM policy contract; alias and canonical model selectors
	// discovered after authorization still run in full.
	RateReceipt RateAdmissionReceipt
	// Phase projects the same policy population and runtime buckets onto one
	// execution boundary. Empty is the historical full model-call admission.
	// Media submission uses rate; authenticated Gateway attempts whose body has
	// no usable public model use unscoped_rate, which excludes model selectors
	// without inventing a sentinel model. The worker uses execution immediately
	// before Provider dispatch so queued durable work does not occupy concurrency.
	// Synchronous Gateway calls use unscoped_rate before governance and
	// model_execution at Freeze, retaining TPM, concurrency, and workload without
	// exposing caller-controlled model topology to the early boundary.
	Phase string
}

type Usage struct {
	RPS         int64 `json:"rps"`
	RPM         int64 `json:"rpm"`
	TPM         int64 `json:"tpm"`
	Concurrency int64 `json:"concurrency"`
}

type Limits struct {
	RPS         int64 `json:"rps,omitempty"`
	RPM         int64 `json:"rpm,omitempty"`
	TPM         int64 `json:"tpm,omitempty"`
	Concurrency int64 `json:"concurrency,omitempty"`
}

// Readiness is a safe, read-only projection of the exact authoritative
// subject and policy population used by admission. It contains no Redis key,
// policy selector value, secret, or mutation handle.
type Readiness struct {
	MatchingPolicyCount int       `json:"matching_policy_count"`
	EnforcedPolicyCount int       `json:"enforced_policy_count"`
	SnapshotLoadedAt    time.Time `json:"snapshot_loaded_at"`
	State               string    `json:"state"`
	Reason              string    `json:"reason"`
}

type Decision struct {
	PolicyID       int64  `json:"policy_id,omitempty"`
	PolicyRef      string `json:"policy_ref"`
	PolicyName     string `json:"policy_name"`
	PolicyVersion  int64  `json:"policy_version"`
	SelectorHash   string `json:"selector_hash"`
	Mode           string `json:"mode"`
	FailureMode    string `json:"failure_mode"`
	Decision       string `json:"decision"`
	Reason         string `json:"reason"`
	Limits         Limits `json:"limits"`
	Observed       Usage  `json:"observed"`
	Fallback       bool   `json:"fallback"`
	WorkloadClass  string `json:"workload_class,omitempty"`
	WorkloadWeight int64  `json:"workload_weight,omitempty"`
	WaitMS         int64  `json:"wait_ms,omitempty"`
	QueueDepth     int64  `json:"queue_depth,omitempty"`
}

type policyReservation struct {
	OperationID    string
	PolicyRef      string
	RuntimeRef     string
	Fingerprint    string
	InputTokens    int64
	ReservedTokens int64
	TPMLimit       int64
	Fallback       bool
	Mode           string
	Tokenless      bool
	rateContract   rateAdmissionContract
}

// Reservation is a server-owned terminal handle. It is not a bearer token and
// grants no model, Tool, IAM, database, or export authority.
type Reservation struct {
	OperationID         string
	Items               []policyReservation
	Idempotent          bool
	ExtensionSequence   int
	LastExtensionTokens int64
	workload            *workloadadmission.Reservation
	rateReceipt         RateAdmissionReceipt
	rateFinalized       bool
}

const (
	rateAdmissionKindRedis    = "redis"
	rateAdmissionKindFallback = "fallback"
)

// rateAdmissionContract is the exact rate authority consumed by the early
// phase. RuntimeRef alone is only rolling-population identity and deliberately
// remains stable across limit changes, so it is never sufficient skip proof.
type rateAdmissionContract struct {
	runtimeRef   string
	policyRef    string
	version      int64
	selectorHash string
	rpsLimit     int64
	rpmLimit     int64
	mode         string
	failureMode  string
	builtin      bool
	kind         string
}

// RateAdmissionReceipt is intentionally opaque outside limitpolicy. Its
// unexported binding and authenticator prevent internal callers from inventing
// rate contracts to skip during model execution.
type RateAdmissionReceipt struct {
	rateOperationID string
	mainOperationID string
	requestedModel  string
	orgID           int64
	projectID       int64
	apiKeyID        int64
	credentialID    int64
	credentialRef   string
	grantID         int64
	nodeID          string
	contracts       []rateAdmissionContract
	authenticator   []byte
}

// rateAdmissionReceiptFor returns a defensive, main-operation-bound receipt
// only after the rate reservation was finalized successfully. Receipt issuance
// remains owned by Service.FinalizeRateAdmission so callers cannot turn an
// arbitrary terminal reservation into split-phase authority.
func (r *Reservation) rateAdmissionReceiptFor(mainOperationID string) (RateAdmissionReceipt, bool) {
	if r == nil || !r.rateFinalized {
		return RateAdmissionReceipt{}, false
	}
	rateOperationID, valid := RateAdmissionOperationID(mainOperationID)
	if !valid || rateOperationID != r.rateReceipt.rateOperationID {
		return RateAdmissionReceipt{}, false
	}
	receipt := r.rateReceipt
	receipt.mainOperationID = mainOperationID
	receipt.contracts = append([]rateAdmissionContract(nil), receipt.contracts...)
	receipt.authenticator = append([]byte(nil), receipt.authenticator...)
	return receipt, true
}

// RateAdmissionOperationID derives the only supported split-phase relation.
func RateAdmissionOperationID(mainOperationID string) (string, bool) {
	const suffix = ":rate"
	if mainOperationID == "" || strings.TrimSpace(mainOperationID) != mainOperationID || len(mainOperationID) > 64-len(suffix) {
		return "", false
	}
	return mainOperationID + suffix, true
}

func mainOperationIDFromRateAdmission(rateOperationID string) (string, bool) {
	const suffix = ":rate"
	if !strings.HasSuffix(rateOperationID, suffix) {
		return "", false
	}
	mainOperationID := strings.TrimSuffix(rateOperationID, suffix)
	expected, valid := RateAdmissionOperationID(mainOperationID)
	return mainOperationID, valid && expected == rateOperationID
}

func (r RateAdmissionReceipt) isZero() bool {
	return r.rateOperationID == "" && r.mainOperationID == "" && r.requestedModel == "" && r.orgID == 0 && r.projectID == 0 &&
		r.apiKeyID == 0 && r.credentialID == 0 && r.credentialRef == "" && r.grantID == 0 &&
		r.nodeID == "" && len(r.contracts) == 0 && len(r.authenticator) == 0
}

func (p Policy) Validate() error {
	if strings.TrimSpace(p.Ref) == "" || strings.TrimSpace(p.Name) == "" || p.Version <= 0 {
		return ErrInvalidPolicy
	}
	if p.Mode != ModeDisabled && p.Mode != ModeShadow && p.Mode != ModeEnforce {
		return ErrInvalidPolicy
	}
	if p.FailureMode != FailureFailClosed && p.FailureMode != FailureLastKnownSafe {
		return ErrInvalidPolicy
	}
	if p.RPSLimit < 0 || p.RPMLimit < 0 || p.TPMLimit < 0 || p.ConcurrencyLimit < 0 {
		return ErrInvalidPolicy
	}
	if p.RPSLimit == 0 && p.RPMLimit == 0 && p.TPMLimit == 0 && p.ConcurrencyLimit == 0 {
		return ErrInvalidPolicy
	}
	if p.ProjectID != nil && p.OrgID == nil || p.APIKeyID != nil && p.OrgID == nil ||
		p.AccessCredentialID != nil && p.OrgID == nil || p.AccessGrantID != nil && p.OrgID == nil {
		return ErrInvalidPolicy
	}
	return nil
}

func (p Policy) Matches(subject Subject) bool {
	if p.Mode == ModeDisabled {
		return false
	}
	if p.OrgID != nil && *p.OrgID != subject.OrgID {
		return false
	}
	if p.ProjectID != nil && *p.ProjectID != subject.ProjectID {
		return false
	}
	if p.APIKeyID != nil && *p.APIKeyID != subject.APIKeyID {
		return false
	}
	if p.AccessCredentialID != nil && *p.AccessCredentialID != subject.AccessCredentialID {
		return false
	}
	if p.AccessGrantID != nil && *p.AccessGrantID != subject.AccessGrantID {
		return false
	}
	if p.Model != "" && p.Model != subject.Model {
		return false
	}
	if p.PolicyLabel != "" && !containsLabel(subject.PolicyLabels, p.PolicyLabel) {
		return false
	}
	return true
}

func (p Policy) selectorHash() string {
	canonical := fmt.Sprintf("org=%s|project=%s|key=%s|credential=%s|grant=%s|model=%s|label=%s",
		optionalID(p.OrgID), optionalID(p.ProjectID), optionalID(p.APIKeyID), optionalID(p.AccessCredentialID),
		optionalID(p.AccessGrantID), p.Model, p.PolicyLabel)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:8])
}

func (p Policy) limits() Limits {
	return Limits{RPS: p.RPSLimit, RPM: p.RPMLimit, TPM: p.TPMLimit, Concurrency: p.ConcurrencyLimit}
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

func runtimePolicySetVersion(policies []Policy, workloadVersion string) string {
	parts := make([]string, 0, len(policies)+1)
	for _, policy := range policies {
		parts = append(parts, fmt.Sprintf("%s:%d", policy.Ref, policy.Version))
	}
	parts = append(parts, "workload:"+workloadVersion)
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:8])
}

func operationFingerprint(request AdmissionRequest, runtimeRef string) string {
	labels := append([]string(nil), request.Subject.PolicyLabels...)
	sort.Strings(labels)
	canonical := fmt.Sprintf("%s|%s|%s|%d|%d|%d|%d|%s|%d|%s|%s|%d|%d|%s|%s|%s|%s|%s",
		request.OperationID, runtimeRef, normalizedAdmissionPhase(request.Phase), request.Subject.OrgID, request.Subject.ProjectID,
		request.Subject.APIKeyID, request.Subject.AccessCredentialID, request.Subject.CredentialRef,
		request.Subject.AccessGrantID, request.Subject.Model, strings.Join(labels, ","),
		request.EstimatedInput, request.ReservedOutput,
		optionalInt64(request.Subject.CredentialConcurrencyLimit),
		optionalInt64(request.Subject.GrantRPSLimit), optionalInt64(request.Subject.GrantRPMLimit),
		optionalInt64(request.Subject.GrantConcurrencyLimit), hex.EncodeToString(request.RateReceipt.authenticator))
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func normalizedAdmissionPhase(phase string) string {
	switch strings.TrimSpace(phase) {
	case "", AdmissionPhaseFull:
		return AdmissionPhaseFull
	case AdmissionPhaseRate:
		return AdmissionPhaseRate
	case AdmissionPhaseUnscopedRate:
		return AdmissionPhaseUnscopedRate
	case AdmissionPhaseExecution:
		return AdmissionPhaseExecution
	case AdmissionPhaseModelExecution:
		return AdmissionPhaseModelExecution
	default:
		return ""
	}
}

func admissionPhaseIncludesRate(phase string) bool {
	return phase == AdmissionPhaseFull || phase == AdmissionPhaseRate || phase == AdmissionPhaseUnscopedRate
}

func admissionPhaseIncludesTokens(phase string) bool {
	return phase == AdmissionPhaseFull || phase == AdmissionPhaseModelExecution
}

func admissionPhaseIncludesConcurrency(phase string) bool {
	return phase == AdmissionPhaseFull || phase == AdmissionPhaseExecution || phase == AdmissionPhaseModelExecution
}

func validRequestedModelForPhase(phase, requestedModel string) bool {
	if requestedModel != strings.TrimSpace(requestedModel) {
		return false
	}
	switch phase {
	case AdmissionPhaseUnscopedRate:
		return true
	case AdmissionPhaseModelExecution:
		return requestedModel != ""
	default:
		return requestedModel == ""
	}
}

func optionalInt64(value *int64) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprint(*value)
}

func optionalID(value *int64) string {
	if value == nil {
		return "*"
	}
	return fmt.Sprintf("%d", *value)
}

func containsLabel(labels []string, expected string) bool {
	for _, label := range labels {
		if label == expected {
			return true
		}
	}
	return false
}
