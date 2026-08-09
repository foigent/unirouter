// Package accessprovisioning owns UniRouter's project-bound enterprise access
// request, grant, and ordinary credential lifecycle. Connector/Feed purpose
// credentials and model-call authorization overlays remain separate domains.
package accessprovisioning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/ipallowlist"
)

const PolicyVersion = "unirouter.enterprise-access-policy.v1"
const PersonalEnvironment = "personal"

const (
	MaxOperationalLimit int64 = 1<<31 - 1
	DefaultPageLimit          = 50
	MaxPageLimit              = 100
	// MaxGrantLifecycleReasonBytes bounds the client-authored explanatory text
	// copied into durable audit/outbox evidence. Lifecycle reason codes remain
	// server-owned machine enums.
	MaxGrantLifecycleReasonBytes = 500
)

// PageInput is a descending-id keyset cursor. AfterID is exclusive: a page
// returns objects whose stable database id is lower than AfterID. A zero
// AfterID starts at the newest object. Service methods normalize a zero Limit
// to DefaultPageLimit and reject every other out-of-range value.
type PageInput struct {
	Limit   int   `json:"limit"`
	AfterID int64 `json:"after_id,omitempty"`
}

type PageInfo struct {
	Limit       int    `json:"limit"`
	HasMore     bool   `json:"has_more"`
	NextAfterID *int64 `json:"next_after_id,omitempty"`
}

type RequestPage struct {
	Data       []Request `json:"data"`
	Pagination PageInfo  `json:"pagination"`
	Scope      string    `json:"scope,omitempty"`
}

type GrantPage struct {
	Data       []Grant  `json:"data"`
	Pagination PageInfo `json:"pagination"`
	Scope      string   `json:"scope,omitempty"`
}

type GrantCredentialPage struct {
	Data       []GrantCredentialMetadata `json:"data"`
	Pagination PageInfo                  `json:"pagination"`
}

func NormalizePageInput(input PageInput) (PageInput, error) {
	if input.Limit == 0 {
		input.Limit = DefaultPageLimit
	}
	if input.Limit < 1 || input.Limit > MaxPageLimit || input.AfterID < 0 {
		return PageInput{}, ErrInvalid
	}
	return input, nil
}

const (
	SubjectUser                = "user"
	SubjectWorkloadApplication = "workload_application"

	RequestKindHumanPersonal   = "human_personal"
	RequestKindWorkloadService = "workload_service"

	SubmissionSelf        = "self"
	SubmissionDelegated   = "delegated"
	SubmissionDirectGrant = "direct_grant"
	SubmissionMigration   = "migration"
	SubmissionBootstrap   = "bootstrap"
	SubmissionRecovery    = "recovery"

	RequestApproved    = "approved"
	RequestNeedsReview = "needs_review"
	RequestRejected    = "rejected"
	RequestCancelled   = "cancelled"

	GrantActive    = "active"
	GrantSuspended = "suspended"
	GrantRevoked   = "revoked"

	DecisionAutoPolicy       = "auto_policy"
	DecisionManual           = "manual"
	DecisionDirect           = "direct"
	DecisionPending          = "pending"
	DecisionRequester        = "requester"
	DecisionSubjectLifecycle = "subject_lifecycle"
	DecisionPolicyRejection  = "policy_rejection"

	ReasonAutoApprovedCurrentProjectDefault  = "auto_approved_current_project_default"
	ReasonAutoApprovedWorkloadProjectDefault = "auto_approved_workload_project_default"
	ReasonDirectGrantAuthorizedNonProduction = "direct_grant_authorized_nonproduction"
	ReasonBootstrapAuthorized                = "bootstrap_authorized"
	ReasonRecoveryAuthorized                 = "recovery_authorized"
	ReasonProjectDefaultScope                = "project_default_scope"
	ReasonFirstCrossProject                  = "first_cross_project"
	ReasonRoleElevation                      = "role_elevation"
	ReasonSpecialModel                       = "special_model"
	ReasonSpecialEndpoint                    = "special_endpoint"
	ReasonScopeExpansion                     = "scope_expansion"
	ReasonExtraRateOrConcurrency             = "extra_rate_or_concurrency"
	ReasonValidityExtension                  = "validity_extension"
	ReasonProductionWorkload                 = "production_workload"
	ReasonAmbiguousMigration                 = "ambiguous_migration"
	ReasonWorkloadRequiresReview             = "workload_requires_review"
	ReasonReadOnlySubjectCannotInvoke        = "read_only_subject_cannot_invoke"
	ReasonManualApproved                     = "manual_approved"
	ReasonManualRejected                     = "manual_rejected"
	ReasonRequesterCancelled                 = "requester_cancelled"
	ReasonSubjectOffboarded                  = "subject_offboarded"
	ReasonWorkloadArchived                   = "workload_archived"
)

var (
	ErrInvalid                = errors.New("access provisioning input is invalid")
	ErrForbidden              = errors.New("access provisioning forbidden")
	ErrNotFound               = errors.New("access provisioning object not found")
	ErrConflict               = errors.New("access provisioning conflict")
	ErrCredentialNameConflict = errors.New("credential name already exists")
	ErrAuthorizationChanged   = errors.New("access provisioning actor authorization changed")

	clientRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	environmentPattern     = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	policyLabelPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
)

type Subject struct {
	Type                  string `json:"type"`
	UserID                int64  `json:"user_id,omitempty"`
	WorkloadApplicationID int64  `json:"workload_application_id,omitempty"`
}

type Envelope struct {
	ProjectRole      string   `json:"project_role"`
	ScopeMode        string   `json:"scope_mode"`
	Models           []string `json:"models"`
	Endpoints        []string `json:"endpoints"`
	RPMLimit         *int     `json:"rpm_limit,omitempty"`
	RPSLimit         *int     `json:"rps_limit,omitempty"`
	ConcurrencyLimit *int     `json:"concurrency_limit,omitempty"`
}

type ProvisionInput struct {
	ClientRequestID string     `json:"-"`
	RequestID       string     `json:"-"`
	OrgID           int64      `json:"org_id"`
	ProjectID       int64      `json:"project_id"`
	Subject         Subject    `json:"subject"`
	RequestKind     string     `json:"request_kind"`
	Envelope        Envelope   `json:"requested_scope"`
	Environment     string     `json:"environment"`
	ProductionUse   bool       `json:"production_use"`
	ValidUntil      *time.Time `json:"valid_until,omitempty"`
	SubmissionMode  string     `json:"submission_mode"`
	SponsorUserID   int64      `json:"sponsor_user_id,omitempty"`
}

type Request struct {
	ID                    int64           `json:"id"`
	RequestRef            string          `json:"request_ref"`
	ClientRequestID       string          `json:"client_request_id"`
	SemanticHash          string          `json:"semantic_hash"`
	SemanticRevision      int64           `json:"semantic_revision"`
	OrgID                 int64           `json:"org_id"`
	ProjectID             int64           `json:"project_id"`
	Subject               Subject         `json:"subject"`
	RequestKind           string          `json:"request_kind"`
	Envelope              Envelope        `json:"requested_scope"`
	Environment           string          `json:"environment"`
	ProductionUse         bool            `json:"production_use"`
	RequestedValidUntil   *time.Time      `json:"requested_valid_until,omitempty"`
	SubmissionMode        string          `json:"submission_mode"`
	SponsorUserID         int64           `json:"sponsor_user_id,omitempty"`
	TargetAccessGrantID   int64           `json:"target_access_grant_id,omitempty"`
	ExpectedGrantRevision int64           `json:"expected_grant_revision,omitempty"`
	Status                string          `json:"status"`
	DecisionMode          string          `json:"decision_mode"`
	PolicyReasonCodes     []string        `json:"policy_reason_codes"`
	DecisionReasonCodes   []string        `json:"decision_reason_codes"`
	PolicyVersion         string          `json:"policy_version"`
	SubmittedBy           int64           `json:"submitted_by"`
	DecidedBy             int64           `json:"decided_by,omitempty"`
	CancelledBy           int64           `json:"cancelled_by,omitempty"`
	SubmittedAt           time.Time       `json:"submitted_at"`
	DecidedAt             *time.Time      `json:"decided_at,omitempty"`
	CancelledAt           *time.Time      `json:"cancelled_at,omitempty"`
	Revision              int64           `json:"revision"`
	Actions               *RequestActions `json:"actions,omitempty"`
}

// RequestActions is a read-only, actor-specific projection for the exact
// admitted actor. It is deliberately advisory: Decide and Cancel still repeat
// their full commit-time authorization and optimistic-revision checks.
type RequestActions struct {
	CanApprove bool   `json:"can_approve"`
	CanReject  bool   `json:"can_reject"`
	CanCancel  bool   `json:"can_cancel"`
	ReasonCode string `json:"reason_code"`
}

type RequestListFilter struct {
	Status                       string
	SubjectWorkloadApplicationID int64
}

type GrantListFilter struct {
	SubjectWorkloadApplicationID int64
}

type Grant struct {
	ID                  int64      `json:"id"`
	GrantRef            string     `json:"grant_ref"`
	AccessRequestID     int64      `json:"access_request_id"`
	OrgID               int64      `json:"org_id"`
	ProjectID           int64      `json:"project_id"`
	Subject             Subject    `json:"subject"`
	Envelope            Envelope   `json:"scope"`
	Environment         string     `json:"environment"`
	ProductionUse       bool       `json:"production_use"`
	SponsorUserID       int64      `json:"sponsor_user_id,omitempty"`
	Status              string     `json:"status"`
	Revision            int64      `json:"revision"`
	ValidFrom           time.Time  `json:"valid_from"`
	ValidUntil          *time.Time `json:"valid_until,omitempty"`
	DecisionMode        string     `json:"decision_mode"`
	PolicyReasonCodes   []string   `json:"policy_reason_codes"`
	DecisionReasonCodes []string   `json:"decision_reason_codes"`
	PolicyVersion       string     `json:"policy_version"`
	SemanticHash        string     `json:"semantic_hash"`
	CreatedBy           int64      `json:"created_by"`
	UpdatedBy           int64      `json:"updated_by"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type ProvisionResult struct {
	Request    Request `json:"request"`
	Grant      *Grant  `json:"grant,omitempty"`
	Idempotent bool    `json:"idempotent"`
}

// ProvisionCredentialResult is the atomic compatibility result for callers
// that request access and, when policy approves it immediately, issue one
// ordinary credential in the same database transaction. Needs-review results
// deliberately contain no credential or secret.
type ProvisionCredentialResult struct {
	ProvisionResult
	Credential *Credential `json:"credential,omitempty"`
}

type DecisionInput struct {
	RequestRef       string   `json:"-"`
	ExpectedRevision int64    `json:"expected_revision"`
	Decision         string   `json:"decision"`
	ReasonCodes      []string `json:"reason_codes"`
	RequestID        string   `json:"-"`
	ClientRequestID  string   `json:"-"`
}

type CancelRequestInput struct {
	RequestRef       string `json:"-"`
	ExpectedRevision int64  `json:"expected_revision"`
	RequestID        string `json:"-"`
	ClientRequestID  string `json:"-"`
}

type CancelRequestResult struct {
	Request    Request `json:"request"`
	Idempotent bool    `json:"idempotent"`
}

type GrantLifecycleInput struct {
	GrantRef         string `json:"-"`
	ExpectedRevision int64  `json:"expected_revision"`
	Action           string `json:"-"`
	Reason           string `json:"reason"`
	RequestID        string `json:"-"`
	ClientRequestID  string `json:"-"`
}

type CredentialInput struct {
	GrantRef         string   `json:"-"`
	Name             string   `json:"name"`
	AllowedIPs       string   `json:"allowed_ips"`
	AllowedModels    []string `json:"allowed_models"`
	AllowedEndpoints []string `json:"allowed_endpoints"`
	RPMLimit         *int     `json:"rpm_limit,omitempty"`
	RPSLimit         *int     `json:"rps_limit,omitempty"`
	ConcurrencyLimit *int     `json:"concurrency_limit,omitempty"`
	// LegacyPolicyLabels and LegacyBudgetLimitCents are trusted compatibility
	// metadata populated only by the old administrator key adapter. They are
	// deliberately absent from the canonical Access Credential JSON contract
	// and never widen an Access Grant.
	LegacyPolicyLabels     []string `json:"-"`
	LegacyBudgetLimitCents *int64   `json:"-"`
	RequestID              string   `json:"-"`
	ClientRequestID        string   `json:"-"`
}

type RotateCredentialInput struct {
	GrantRef         string `json:"-"`
	PredecessorKeyID int64  `json:"-"`
	RequestID        string `json:"-"`
	ClientRequestID  string `json:"-"`
}

type Credential struct {
	ID               int64     `json:"id"`
	CredentialRef    string    `json:"credential_ref"`
	Key              string    `json:"key,omitempty"`
	KeyPrefix        string    `json:"key_prefix"`
	Name             string    `json:"name"`
	GrantRef         string    `json:"grant_ref"`
	Status           string    `json:"status"`
	AllowedIPs       string    `json:"allowed_ips"`
	AllowedModels    []string  `json:"allowed_models"`
	AllowedEndpoints []string  `json:"allowed_endpoints"`
	RPMLimit         int       `json:"rpm_limit"`
	RPSLimit         int       `json:"rps_limit"`
	ConcurrencyLimit *int      `json:"concurrency_limit,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	Classification   string    `json:"classification"`
	Idempotent       bool      `json:"idempotent"`
	SecretAvailable  bool      `json:"secret_available"`
	RecoveryAction   string    `json:"recovery_action,omitempty"`
}

type CredentialMetadata struct {
	ID            int64  `json:"id"`
	CredentialRef string `json:"credential_ref"`
	KeyPrefix     string `json:"key_prefix"`
	Name          string `json:"name"`
	Status        string `json:"status"`
}

// CredentialVersionMetadata is one physical authentication-secret version.
// Historical attribution remains version-bound while policy and quota state
// belongs to the logical Access Credential family.
type CredentialVersionMetadata struct {
	ID               int64     `json:"id"`
	KeyPrefix        string    `json:"key_prefix"`
	Status           string    `json:"status"`
	EffectiveStatus  string    `json:"effective_status"`
	CreatedAt        time.Time `json:"created_at"`
	RotatedFromKeyID *int64    `json:"rotated_from_key_id,omitempty"`
}

// GrantCredentialMetadata is the secret-free logical Access Credential read
// model. Secret versions are nested so atomic secret replacement remains one
// authorization, License-seat and quota subject.
type GrantCredentialMetadata struct {
	ID               int64                       `json:"id"`
	CredentialRef    string                      `json:"credential_ref"`
	Name             string                      `json:"name"`
	Status           string                      `json:"status"`
	EffectiveStatus  string                      `json:"effective_status"`
	AllowedIPs       string                      `json:"allowed_ips"`
	AllowedModels    []string                    `json:"allowed_models"`
	AllowedEndpoints []string                    `json:"allowed_endpoints"`
	RPMLimit         int                         `json:"rpm_limit"`
	RPSLimit         int                         `json:"rps_limit"`
	ConcurrencyLimit *int                        `json:"concurrency_limit,omitempty"`
	CreatedAt        time.Time                   `json:"created_at"`
	VersionCount     int                         `json:"version_count"`
	Versions         []CredentialVersionMetadata `json:"versions"`
}

type RotationResult struct {
	Credential  Credential         `json:"credential"`
	Predecessor CredentialMetadata `json:"predecessor"`
	Idempotent  bool               `json:"idempotent"`
}

type LegacyCredentialSummary struct {
	KeyID       int64  `json:"key_id"`
	KeyPrefix   string `json:"key_prefix"`
	KeyType     string `json:"key_type"`
	Purpose     string `json:"purpose,omitempty"`
	ProjectID   int64  `json:"project_id,omitempty"`
	OwnerUserID int64  `json:"owner_user_id,omitempty"`
}

// LegacyClassification is a read-only migration preview record. Workload
// applications are classified as one object with sanitized linked credential
// metadata so ownership and project ambiguity cannot be guessed key-by-key.
// Credential records remain present for connector separation and existing
// unbound-key inventory, but an application-linked ordinary key is never a
// second migration candidate.
type LegacyClassification struct {
	ObjectType            string                    `json:"object_type"`
	ObjectID              int64                     `json:"object_id"`
	ObjectRef             string                    `json:"object_ref"`
	Classification        string                    `json:"classification"`
	ReasonCode            string                    `json:"reason_code"`
	MigrationStatus       string                    `json:"migration_status"`
	AutomaticMigration    bool                      `json:"automatic_migration"`
	ProjectID             int64                     `json:"project_id,omitempty"`
	OwnerUserID           int64                     `json:"owner_user_id,omitempty"`
	WorkloadApplicationID int64                     `json:"workload_application_id,omitempty"`
	ApplicationRef        string                    `json:"application_ref,omitempty"`
	Kind                  string                    `json:"kind,omitempty"`
	IdentityMode          string                    `json:"identity_mode,omitempty"`
	Environment           string                    `json:"environment,omitempty"`
	Status                string                    `json:"status,omitempty"`
	ClientProfiles        []string                  `json:"client_profiles,omitempty"`
	LinkedCredentialCount int                       `json:"linked_credential_count,omitempty"`
	LinkedCredentials     []LegacyCredentialSummary `json:"linked_credentials,omitempty"`
	KeyID                 int64                     `json:"key_id,omitempty"`
	KeyPrefix             string                    `json:"key_prefix,omitempty"`
}

type Actor struct {
	UserID           int64
	OrgID            int64
	OrganizationRole string
	ProjectRole      string
}

// ActorScope is a non-authoritative snapshot from the admitted request. It is
// used only to scope the first object lookup so hidden and absent references
// are indistinguishable. Every mutation still performs CommitAuthorizer
// reauthorization under transaction locks before writing.
type ActorScope struct {
	UserID           int64
	OrgID            int64
	OrganizationRole string
}

func (scope ActorScope) valid() bool {
	return scope.UserID > 0 && scope.OrgID > 0 && slices.Contains([]string{
		"platform_admin", "org_admin", "billing_admin", "org_viewer", "member",
	}, scope.OrganizationRole)
}

type AuthorizationRequirement struct {
	OrgID                                   int64
	ProjectID                               int64
	AllowedOrganizationRoles                []string
	AllowedProjectRoles                     []string
	AllowInactiveProject                    bool
	AllowSelfRequestWithoutMembershipUserID int64
	AdditionalUserIDs                       []int64
	AdditionalWorkloadIDs                   []int64
}

type MembershipChange struct {
	Changed             bool   `json:"changed"`
	BeforeRole          string `json:"before_role,omitempty"`
	AfterRole           string `json:"after_role"`
	CompatibilityBefore string `json:"compatibility_before,omitempty"`
	CompatibilityAfter  string `json:"compatibility_after,omitempty"`
	RevokedSessions     int64  `json:"revoked_sessions"`
}

func normalizeProvisionInput(input ProvisionInput) (ProvisionInput, string, error) {
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Subject.Type = strings.ToLower(strings.TrimSpace(input.Subject.Type))
	input.RequestKind = strings.ToLower(strings.TrimSpace(input.RequestKind))
	input.Envelope.ProjectRole = strings.ToLower(strings.TrimSpace(input.Envelope.ProjectRole))
	input.Environment = strings.ToLower(strings.TrimSpace(input.Environment))
	if input.ValidUntil != nil {
		value := input.ValidUntil.UTC().Truncate(time.Microsecond)
		if value.IsZero() {
			return ProvisionInput{}, "", ErrInvalid
		}
		input.ValidUntil = &value
	}
	input.SubmissionMode = strings.ToLower(strings.TrimSpace(input.SubmissionMode))
	input.Envelope.Models = normalizeStringSet(input.Envelope.Models)
	input.Envelope.Endpoints = normalizeStringSet(input.Envelope.Endpoints)
	input.Envelope.ScopeMode = accessscope.NormalizeMode(input.Envelope.ScopeMode, input.Envelope.Models, input.Envelope.Endpoints)
	if input.Subject.Type == SubjectUser && input.Environment == "" {
		input.Environment = PersonalEnvironment
	}
	if input.OrgID <= 0 || input.ProjectID <= 0 || !clientRequestIDPattern.MatchString(input.ClientRequestID) ||
		!environmentPattern.MatchString(input.Environment) ||
		!slices.Contains([]string{"project_admin", "project_user", "project_viewer"}, input.Envelope.ProjectRole) ||
		!validOperationalLimit(input.Envelope.RPMLimit) || !validOperationalLimit(input.Envelope.RPSLimit) ||
		!validOperationalLimit(input.Envelope.ConcurrencyLimit) ||
		!validModelStrings(input.Envelope.Models, 160) || !validStrings(input.Envelope.Endpoints, 160) ||
		!accessscope.Validate(input.Envelope.ScopeMode, input.Envelope.Models, input.Envelope.Endpoints) {
		return ProvisionInput{}, "", ErrInvalid
	}
	if !slices.Contains([]string{SubmissionSelf, SubmissionDelegated, SubmissionDirectGrant, SubmissionMigration, SubmissionBootstrap, SubmissionRecovery}, input.SubmissionMode) {
		return ProvisionInput{}, "", ErrInvalid
	}
	switch input.Subject.Type {
	case SubjectUser:
		if input.Subject.UserID <= 0 || input.Subject.WorkloadApplicationID != 0 || input.RequestKind != RequestKindHumanPersonal {
			return ProvisionInput{}, "", ErrInvalid
		}
		if input.Environment != PersonalEnvironment || input.ProductionUse {
			return ProvisionInput{}, "", ErrInvalid
		}
		// Human responsibility is already represented by submitted_by. Sponsor
		// is reserved for workload ownership and must never become forgeable
		// human-request evidence in any submission mode.
		if input.SponsorUserID != 0 {
			return ProvisionInput{}, "", ErrInvalid
		}
	case SubjectWorkloadApplication:
		if input.Subject.WorkloadApplicationID <= 0 || input.Subject.UserID != 0 ||
			input.RequestKind != RequestKindWorkloadService || input.SponsorUserID <= 0 ||
			input.Envelope.ProjectRole != "project_user" {
			return ProvisionInput{}, "", ErrInvalid
		}
	default:
		return ProvisionInput{}, "", ErrInvalid
	}
	semantic := struct {
		OrgID          int64      `json:"org_id"`
		ProjectID      int64      `json:"project_id"`
		Subject        Subject    `json:"subject"`
		RequestKind    string     `json:"request_kind"`
		Envelope       Envelope   `json:"envelope"`
		Environment    string     `json:"environment"`
		ProductionUse  bool       `json:"production_use"`
		ValidUntil     *time.Time `json:"valid_until,omitempty"`
		SubmissionMode string     `json:"submission_mode"`
		SponsorUserID  int64      `json:"sponsor_user_id"`
	}{input.OrgID, input.ProjectID, input.Subject, input.RequestKind, input.Envelope, input.Environment, input.ProductionUse, input.ValidUntil, input.SubmissionMode, input.SponsorUserID}
	payload, err := json.Marshal(semantic)
	if err != nil {
		return ProvisionInput{}, "", fmt.Errorf("%w: semantic payload", ErrInvalid)
	}
	digest := sha256.Sum256(payload)
	return input, hex.EncodeToString(digest[:]), nil
}

func normalizeCredentialInput(input CredentialInput) (CredentialInput, error) {
	input.GrantRef = strings.TrimSpace(input.GrantRef)
	input.Name = strings.TrimSpace(input.Name)
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.AllowedModels = normalizeStringSet(input.AllowedModels)
	input.AllowedEndpoints = normalizeStringSet(input.AllowedEndpoints)
	input.LegacyPolicyLabels = normalizeStringSet(input.LegacyPolicyLabels)
	normalizedIPs, err := ipallowlist.Normalize(input.AllowedIPs)
	if err != nil {
		return CredentialInput{}, ErrInvalid
	}
	input.AllowedIPs = normalizedIPs
	if input.GrantRef == "" || input.Name == "" || len(input.Name) > 255 ||
		!clientRequestIDPattern.MatchString(input.ClientRequestID) ||
		!validOperationalLimit(input.RPMLimit) || !validOperationalLimit(input.RPSLimit) ||
		!validOperationalLimit(input.ConcurrencyLimit) ||
		!validModelStrings(input.AllowedModels, 160) || !validStrings(input.AllowedEndpoints, 160) ||
		len(input.LegacyPolicyLabels) > 16 || slices.ContainsFunc(input.LegacyPolicyLabels, func(value string) bool {
		return !policyLabelPattern.MatchString(value)
	}) || (input.LegacyBudgetLimitCents != nil && (*input.LegacyBudgetLimitCents < 0 || *input.LegacyBudgetLimitCents > MaxOperationalLimit)) {
		return CredentialInput{}, ErrInvalid
	}
	return input, nil
}

func normalizeGrantLifecycleReason(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > MaxGrantLifecycleReasonBytes || strings.ContainsAny(value, "\x00\r\n") {
		return "", ErrInvalid
	}
	return value, nil
}

func normalizeStringSet(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func validStrings(values []string, max int) bool {
	return len(values) <= 256 && !slices.ContainsFunc(values, func(value string) bool {
		return value == "" || len(value) > max || strings.ContainsAny(value, "\x00\r\n")
	})
}

func validModelStrings(values []string, max int) bool {
	return validStrings(values, max) && !slices.ContainsFunc(values, func(value string) bool {
		return strings.Contains(value, ",")
	})
}

func validOperationalLimit(value *int) bool {
	return value == nil || (*value >= 0 && int64(*value) <= MaxOperationalLimit)
}

func stableRef(prefix string, parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + hex.EncodeToString(digest[:16])
}

func cloneStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}
