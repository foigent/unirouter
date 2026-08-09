// Package modelauthz implements UniRouter's product-local model-call
// authorization layer. It can only narrow existing API-key, project,
// membership and Agent capability authority; it never grants Tool or MCP
// permissions and never widens an existing credential.
package modelauthz

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	ContractVersion = "unirouter.model-call-authorization.v1"

	SubjectAPIKey           = "api_key"
	SubjectAccessCredential = "access_credential"
	SubjectProject          = "project"
	SubjectUser             = "user"
	SubjectAgent            = "agent"

	EffectAllow = "allow"
	EffectDeny  = "deny"

	StatusDraft   = "draft"
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

var (
	ErrInvalid     = errors.New("modelauthz: invalid policy")
	ErrUnavailable = errors.New("modelauthz: policy store unavailable")

	referencePattern = regexp.MustCompile(`^mca_[a-z0-9][a-z0-9._-]{2,79}$`)
	agentPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$`)
	environmentPat   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
)

var supportedEndpoints = []string{
	"/v1/chat/completions",
	"/v1/embeddings",
	"/v1/models",
	"/v1/media/jobs",
}

type Policy struct {
	ID               int64      `json:"id"`
	OrgID            int64      `json:"org_id"`
	AuthorizationRef string     `json:"authorization_ref"`
	SubjectType      string     `json:"subject_type"`
	SubjectRef       string     `json:"subject_ref"`
	Environment      string     `json:"environment"`
	Effect           string     `json:"effect"`
	AllowedModels    []string   `json:"allowed_models"`
	AllowedEndpoints []string   `json:"allowed_endpoints"`
	NotBefore        *time.Time `json:"not_before,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	Status           string     `json:"status"`
	Revision         int64      `json:"revision"`
	Reason           string     `json:"reason"`
	CreatedBy        int64      `json:"created_by"`
	ActivatedBy      *int64     `json:"activated_by,omitempty"`
	RevokedBy        *int64     `json:"revoked_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	ActivatedAt      *time.Time `json:"activated_at,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Draft struct {
	AuthorizationRef string     `json:"authorization_ref"`
	SubjectType      string     `json:"subject_type"`
	SubjectRef       string     `json:"subject_ref"`
	Environment      string     `json:"environment"`
	Effect           string     `json:"effect"`
	AllowedModels    []string   `json:"allowed_models"`
	AllowedEndpoints []string   `json:"allowed_endpoints"`
	NotBefore        *time.Time `json:"not_before,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	Reason           string     `json:"reason"`
}

type Input struct {
	OrgID              int64
	APIKeyID           int64
	AccessCredentialID int64
	ProjectID          int64
	OwnerUserID        int64
	AgentID            string
	Environment        string
	Endpoint           string
	RequestedModel     string
	ResolvedModel      string
	Now                time.Time
}

type Decision struct {
	ContractVersion string   `json:"contract_version"`
	Allowed         bool     `json:"allowed"`
	Reason          string   `json:"reason"`
	PolicyRefs      []string `json:"policy_refs"`
	Environment     string   `json:"environment"`
	Endpoint        string   `json:"endpoint"`
	RequestedModel  string   `json:"requested_model"`
	ResolvedModel   string   `json:"resolved_model"`
	RuntimeMutation string   `json:"runtime_mutation"`
}

func NormalizeDraft(draft Draft) (Draft, error) {
	draft.AuthorizationRef = strings.TrimSpace(strings.ToLower(draft.AuthorizationRef))
	draft.SubjectType = strings.TrimSpace(strings.ToLower(draft.SubjectType))
	draft.SubjectRef = strings.TrimSpace(draft.SubjectRef)
	draft.Environment = strings.TrimSpace(strings.ToLower(draft.Environment))
	draft.Effect = strings.TrimSpace(strings.ToLower(draft.Effect))
	draft.AllowedModels = normalizeSet(draft.AllowedModels)
	draft.AllowedEndpoints = normalizeSet(draft.AllowedEndpoints)
	draft.Reason = strings.TrimSpace(draft.Reason)
	if draft.Environment == "" {
		draft.Environment = "*"
	}
	if !referencePattern.MatchString(draft.AuthorizationRef) {
		return Draft{}, fmt.Errorf("%w: authorization_ref", ErrInvalid)
	}
	switch draft.SubjectType {
	case SubjectAPIKey, SubjectAccessCredential, SubjectProject, SubjectUser:
		value, err := strconv.ParseInt(draft.SubjectRef, 10, 64)
		if err != nil || value <= 0 || strconv.FormatInt(value, 10) != draft.SubjectRef {
			return Draft{}, fmt.Errorf("%w: subject_ref", ErrInvalid)
		}
	case SubjectAgent:
		if !agentPattern.MatchString(draft.SubjectRef) {
			return Draft{}, fmt.Errorf("%w: subject_ref", ErrInvalid)
		}
	default:
		return Draft{}, fmt.Errorf("%w: subject_type", ErrInvalid)
	}
	if draft.Environment != "*" && !environmentPat.MatchString(draft.Environment) {
		return Draft{}, fmt.Errorf("%w: environment", ErrInvalid)
	}
	if draft.Effect != EffectAllow && draft.Effect != EffectDeny {
		return Draft{}, fmt.Errorf("%w: effect", ErrInvalid)
	}
	if len(draft.AllowedModels) == 0 || len(draft.AllowedModels) > 128 {
		return Draft{}, fmt.Errorf("%w: allowed_models", ErrInvalid)
	}
	for _, model := range draft.AllowedModels {
		if len(model) > 160 || (model != "*" && strings.ContainsAny(model, "\x00\r\n")) {
			return Draft{}, fmt.Errorf("%w: allowed_models", ErrInvalid)
		}
	}
	if len(draft.AllowedEndpoints) == 0 || len(draft.AllowedEndpoints) > len(supportedEndpoints) {
		return Draft{}, fmt.Errorf("%w: allowed_endpoints", ErrInvalid)
	}
	for _, endpoint := range draft.AllowedEndpoints {
		if !slices.Contains(supportedEndpoints, endpoint) {
			return Draft{}, fmt.Errorf("%w: allowed_endpoints", ErrInvalid)
		}
	}
	if draft.Reason == "" || len(draft.Reason) > 1000 {
		return Draft{}, fmt.Errorf("%w: reason", ErrInvalid)
	}
	if draft.NotBefore != nil {
		value := draft.NotBefore.UTC().Truncate(time.Microsecond)
		draft.NotBefore = &value
	}
	if draft.ExpiresAt != nil {
		value := draft.ExpiresAt.UTC().Truncate(time.Microsecond)
		draft.ExpiresAt = &value
	}
	if draft.NotBefore != nil && draft.ExpiresAt != nil && !draft.ExpiresAt.After(*draft.NotBefore) {
		return Draft{}, fmt.Errorf("%w: validity_window", ErrInvalid)
	}
	return draft, nil
}

func SupportedEndpoints() []string {
	return slices.Clone(supportedEndpoints)
}

func normalizeSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}
