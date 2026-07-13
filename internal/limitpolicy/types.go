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
	ID               int64     `json:"id"`
	Ref              string    `json:"ref"`
	Name             string    `json:"name"`
	Version          int64     `json:"version"`
	OrgID            *int64    `json:"org_id,omitempty"`
	ProjectID        *int64    `json:"project_id,omitempty"`
	APIKeyID         *int64    `json:"api_key_id,omitempty"`
	Model            string    `json:"model,omitempty"`
	PolicyLabel      string    `json:"policy_label,omitempty"`
	Mode             string    `json:"mode"`
	FailureMode      string    `json:"failure_mode"`
	RPSLimit         int64     `json:"rps_limit,omitempty"`
	RPMLimit         int64     `json:"rpm_limit,omitempty"`
	TPMLimit         int64     `json:"tpm_limit,omitempty"`
	ConcurrencyLimit int64     `json:"concurrency_limit,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
	Builtin          bool      `json:"builtin"`
	RuntimeBucket    string    `json:"-"`
}

type Subject struct {
	OrgID        int64
	ProjectID    int64
	APIKeyID     int64
	Model        string
	PolicyLabels []string
	BaseRPSLimit int64
	BaseRPMLimit int64
}

type AdmissionRequest struct {
	OperationID    string
	Subject        Subject
	EstimatedInput int64
	ReservedOutput int64
	LeaseTTL       time.Duration
	WorkloadClass  string
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
	if p.ProjectID != nil && p.OrgID == nil || p.APIKeyID != nil && p.OrgID == nil {
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
	if p.Model != "" && p.Model != subject.Model {
		return false
	}
	if p.PolicyLabel != "" && !containsLabel(subject.PolicyLabels, p.PolicyLabel) {
		return false
	}
	return true
}

func (p Policy) selectorHash() string {
	canonical := fmt.Sprintf("org=%s|project=%s|key=%s|model=%s|label=%s",
		optionalID(p.OrgID), optionalID(p.ProjectID), optionalID(p.APIKeyID), p.Model, p.PolicyLabel)
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
	canonical := fmt.Sprintf("%s|%s|%d|%d|%d|%s|%s|%d|%d",
		request.OperationID, runtimeRef, request.Subject.OrgID, request.Subject.ProjectID,
		request.Subject.APIKeyID, request.Subject.Model, strings.Join(labels, ","),
		request.EstimatedInput, request.ReservedOutput)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
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
