package guardrail

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	StageInput           = "input"
	StageNonStreamOutput = "nonstream_output"
	StageStreamOutput    = "stream_output"

	ModeDisabled = "disabled"
	ModeShadow   = "shadow"
	ModeEnforce  = "enforce"

	ActionAudit  = "audit"
	ActionRedact = "redact"
	ActionBlock  = "block"

	FailureFailClosed    = "fail_closed"
	FailureDegradedAllow = "degraded_allow"

	OutcomePass          = "pass"
	OutcomeMatch         = "match"
	OutcomeTransform     = "transform"
	OutcomeIndeterminate = "indeterminate"
	OutcomeUnavailable   = "unavailable"
	OutcomeSkipped       = "skipped"

	DecisionAllow = "allow"
	DecisionBlock = "block"

	DetectorSensitiveWords = "builtin:sensitive_words"
	DetectorPII            = "builtin:pii_secrets"

	// MaxStreamLookbehindBytes is an application safety bound, not a detector
	// tuning knob. A detector that needs more context cannot synchronously enforce
	// a streaming policy under D6-16.
	MaxStreamLookbehindBytes = 4 * 1024
)

var (
	ErrInvalidPolicy       = errors.New("invalid guardrail policy")
	ErrDetectorMissing     = errors.New("guardrail detector is not registered")
	ErrDetectionIncomplete = errors.New("guardrail detection is incomplete")
)

type Policy struct {
	ID                 int64           `json:"id"`
	Ref                string          `json:"ref"`
	Name               string          `json:"name"`
	Version            int64           `json:"version"`
	OrgID              *int64          `json:"org_id,omitempty"`
	ProjectID          *int64          `json:"project_id,omitempty"`
	APIKeyID           *int64          `json:"api_key_id,omitempty"`
	AccessCredentialID *int64          `json:"access_credential_id,omitempty"`
	Model              string          `json:"model,omitempty"`
	Stage              string          `json:"stage"`
	DetectorRef        string          `json:"detector_ref"`
	Action             string          `json:"action"`
	Mode               string          `json:"mode"`
	FailureMode        string          `json:"failure_mode"`
	Mandatory          bool            `json:"mandatory"`
	Config             json.RawMessage `json:"config,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	Builtin            bool            `json:"builtin"`
}

func (p Policy) Validate() error {
	if strings.TrimSpace(p.Ref) == "" || strings.TrimSpace(p.Name) == "" || p.Version <= 0 {
		return ErrInvalidPolicy
	}
	if p.Stage != StageInput && p.Stage != StageNonStreamOutput && p.Stage != StageStreamOutput {
		return ErrInvalidPolicy
	}
	if p.Mode != ModeDisabled && p.Mode != ModeShadow && p.Mode != ModeEnforce {
		return ErrInvalidPolicy
	}
	if p.Action != ActionAudit && p.Action != ActionRedact && p.Action != ActionBlock {
		return ErrInvalidPolicy
	}
	if p.FailureMode != FailureFailClosed && p.FailureMode != FailureDegradedAllow {
		return ErrInvalidPolicy
	}
	if p.DetectorRef != DetectorSensitiveWords && p.DetectorRef != DetectorPII && !strings.HasPrefix(p.DetectorRef, "external:") {
		return ErrInvalidPolicy
	}
	if len(p.DetectorRef) > 120 || len(p.Model) > 200 || len(p.Config) > 8*1024 || (len(p.Config) > 0 && !json.Valid(p.Config)) {
		return ErrInvalidPolicy
	}
	if len(p.Config) > 0 {
		var object map[string]interface{}
		if err := json.Unmarshal(p.Config, &object); err != nil || object == nil {
			return fmt.Errorf("%w: detector config must be a JSON object", ErrInvalidPolicy)
		}
		if configContainsCredentialKey(object) {
			return fmt.Errorf("%w: detector credentials belong to the platform registry, not policy config", ErrInvalidPolicy)
		}
	}
	if (p.ProjectID != nil || p.APIKeyID != nil || p.AccessCredentialID != nil) && p.OrgID == nil {
		return ErrInvalidPolicy
	}
	if p.APIKeyID != nil && p.AccessCredentialID != nil {
		return ErrInvalidPolicy
	}
	if p.Mandatory {
		if p.OrgID != nil || p.FailureMode != FailureFailClosed || p.Mode != ModeEnforce {
			return ErrInvalidPolicy
		}
	}
	if p.DetectorRef == DetectorSensitiveWords && p.Action == ActionRedact {
		return fmt.Errorf("%w: sensitive-word compatibility supports audit or block", ErrInvalidPolicy)
	}
	return nil
}

func configContainsCredentialKey(value interface{}) bool {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, nested := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"), " ", "_"))
			switch normalized {
			case "token", "api_key", "apikey", "secret", "password", "authorization", "credential", "credentials",
				"auth", "bearer", "access_key_id", "secret_access_key", "private_key", "client_secret":
				return true
			}
			if strings.HasSuffix(normalized, "_token") || strings.HasSuffix(normalized, "_password") ||
				strings.HasSuffix(normalized, "_secret") || strings.HasSuffix(normalized, "_api_key") ||
				strings.HasSuffix(normalized, "_private_key") || configContainsCredentialKey(nested) {
				return true
			}
		}
	case []interface{}:
		for _, nested := range typed {
			if configContainsCredentialKey(nested) {
				return true
			}
		}
	}
	return false
}

type Subject struct {
	OrgID              int64
	ProjectID          int64
	APIKeyID           int64
	AccessCredentialID int64
	Model              string
	Endpoint           string
}

type PolicyIdentity struct {
	Ref     string `json:"ref"`
	Version int64  `json:"version"`
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
	return p.Model == "" || p.Model == subject.Model
}

type Text struct {
	Path    string
	Content string
}

type Finding struct {
	Category         string
	Start            int
	End              int
	ConfidenceBucket string
}

type Detector interface {
	Version() string
	Detect(ctx Context, text Text, config json.RawMessage) ([]Finding, error)
}

// StreamCapability is declared by the fixed process registry. Tenant policy
// configuration cannot turn an asynchronous detector into a synchronous one.
type StreamCapability struct {
	Synchronous     bool `json:"synchronous"`
	LookbehindBytes int  `json:"lookbehind_bytes"`
}

// StreamCapableDetector may be used by an enforced stream-output policy when
// its capability is synchronous and fits the platform lookbehind bound.
type StreamCapableDetector interface {
	Detector
	StreamCapability() StreamCapability
}

type StreamProfile struct {
	PolicySetVersion string `json:"policy_set_version"`
	Enforced         bool   `json:"enforced"`
	Advisory         bool   `json:"advisory"`
	LookbehindBytes  int    `json:"lookbehind_bytes"`
}

// Context is the small cancellation/deadline surface detectors need. Keeping
// the interface narrow makes fixture and external adapters deterministic.
type Context interface {
	Done() <-chan struct{}
	Err() error
	Deadline() (time.Time, bool)
}

type Receipt struct {
	PolicyRef       string         `json:"policy_ref"`
	PolicyVersion   int64          `json:"policy_version"`
	Stage           string         `json:"stage"`
	DetectorRef     string         `json:"detector_ref"`
	DetectorVersion string         `json:"detector_version"`
	Mode            string         `json:"mode"`
	Action          string         `json:"action"`
	Outcome         string         `json:"outcome"`
	FailureMode     string         `json:"failure_mode"`
	Mandatory       bool           `json:"mandatory"`
	LatencyMS       int64          `json:"latency_ms"`
	MatchCount      int            `json:"match_count"`
	Categories      map[string]int `json:"categories,omitempty"`
	Fingerprints    []string       `json:"fingerprints,omitempty"`
	Degraded        bool           `json:"degraded"`
}

type Result struct {
	Decision         string    `json:"decision"`
	PolicySetVersion string    `json:"policy_set_version"`
	Texts            []Text    `json:"-"`
	Receipts         []Receipt `json:"receipts"`
	Transformed      bool      `json:"transformed"`
	Degraded         bool      `json:"degraded"`
	BlockedBy        string    `json:"blocked_by,omitempty"`
}
