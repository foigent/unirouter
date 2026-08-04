// Package invocationopt owns UniRouter's product-local invocation optimization
// policy, deterministic assignment and metadata-only evidence contracts.
package invocationopt

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/promptcompression"
)

const (
	PolicyContractVersion     = "unirouter.invocation-optimization-policy.v1"
	ReceiptContractVersion    = "unirouter.invocation-optimization-receipt.v1"
	CommercialReceiptVersion  = "unirouter.response-cache-commercial-receipt.v1"
	ResponseCachePriceVersion = "unirouter.response-cache-zero-charge.v1"

	DriverResponseCache = "response_cache"
	DriverCompression   = "compression"

	TargetDisabled = "disabled"
	TargetCanary   = "canary"

	StageDraft      = "draft"
	StageValidated  = "validated"
	StageShadow     = "shadow"
	StageCanary     = "canary"
	StageRolledBack = "rolled_back"

	EffectiveDisabled = "disabled"
	EffectiveShadow   = "shadow"
	EffectiveCanary   = "canary"
)

var (
	ErrInvalid       = errors.New("invocation optimization policy is invalid")
	ErrNotFound      = errors.New("invocation optimization policy not found")
	ErrConflict      = errors.New("invocation optimization policy changed")
	ErrUnavailable   = errors.New("invocation optimization policy store unavailable")
	policyRefPattern = regexp.MustCompile(`^opt_[a-z0-9][a-z0-9._-]{2,59}$`)
	portablePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,159}$`)
)

type Draft struct {
	OrgID                          int64  `json:"org_id"`
	ProjectID                      int64  `json:"project_id"`
	PolicyRef                      string `json:"policy_ref"`
	DisplayName                    string `json:"display_name"`
	ModelSelector                  string `json:"model_selector"`
	Region                         string `json:"region"`
	Residency                      string `json:"residency"`
	ResponseCacheTarget            string `json:"response_cache_target"`
	ResponseCacheCanaryBPS         int    `json:"response_cache_canary_basis_points"`
	ResponseCacheTTLSeconds        int    `json:"response_cache_ttl_seconds"`
	ResponseCachePersistence       bool   `json:"response_cache_persistence_allowed"`
	ResponseCacheRequireOptIn      bool   `json:"response_cache_require_request_opt_in"`
	CompressionTarget              string `json:"compression_target"`
	CompressionCanaryBPS           int    `json:"compression_canary_basis_points"`
	CompressionRequireOptIn        bool   `json:"compression_require_request_opt_in"`
	CompressionMinimumInputTokens  int    `json:"compression_minimum_input_tokens"`
	CompressionMinimumSavedTokens  int    `json:"compression_minimum_saved_tokens"`
	CompressionMaximumReductionBPS int    `json:"compression_maximum_reduction_basis_points"`
	MinimumShadowSamples           int    `json:"minimum_shadow_samples"`
	Status                         string `json:"status"`
}

type Policy struct {
	ID              int64  `json:"id"`
	ContractVersion string `json:"contract_version"`
	Draft
	Stage       string     `json:"stage"`
	Revision    int64      `json:"revision"`
	CreatedBy   int64      `json:"created_by"`
	UpdatedBy   int64      `json:"updated_by"`
	ValidatedAt *time.Time `json:"validated_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func DefaultDraft() Draft {
	compression := promptcompression.DefaultConfig()
	return Draft{
		ModelSelector: "*", Region: "global", Residency: "customer_policy",
		ResponseCacheTarget: TargetDisabled, ResponseCacheTTLSeconds: 300,
		ResponseCacheRequireOptIn: true,
		CompressionTarget:         TargetDisabled, CompressionRequireOptIn: true,
		CompressionMinimumInputTokens:  compression.MinimumInputTokens,
		CompressionMinimumSavedTokens:  compression.MinimumSavedTokens,
		CompressionMaximumReductionBPS: compression.MaximumReductionBPS,
		MinimumShadowSamples:           100, Status: "active",
	}
}

func NormalizeDraft(input Draft) (Draft, error) {
	defaults := DefaultDraft()
	input.PolicyRef = strings.ToLower(strings.TrimSpace(input.PolicyRef))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.ModelSelector = strings.TrimSpace(input.ModelSelector)
	input.Region = strings.TrimSpace(input.Region)
	input.Residency = strings.TrimSpace(input.Residency)
	input.ResponseCacheTarget = strings.ToLower(strings.TrimSpace(input.ResponseCacheTarget))
	input.CompressionTarget = strings.ToLower(strings.TrimSpace(input.CompressionTarget))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.ModelSelector == "" {
		input.ModelSelector = defaults.ModelSelector
	}
	if input.Region == "" {
		input.Region = defaults.Region
	}
	if input.Residency == "" {
		input.Residency = defaults.Residency
	}
	if input.ResponseCacheTarget == "" {
		input.ResponseCacheTarget = defaults.ResponseCacheTarget
	}
	if input.ResponseCacheTTLSeconds == 0 {
		input.ResponseCacheTTLSeconds = defaults.ResponseCacheTTLSeconds
	}
	// Request opt-in is a non-configurable V1 safety boundary. A JSON bool
	// cannot distinguish an omitted value from an explicit false value, so the
	// server owns this default and never lets a draft silently weaken it.
	input.ResponseCacheRequireOptIn = true
	if input.CompressionTarget == "" {
		input.CompressionTarget = defaults.CompressionTarget
	}
	input.CompressionRequireOptIn = true
	if input.CompressionMinimumInputTokens == 0 {
		input.CompressionMinimumInputTokens = defaults.CompressionMinimumInputTokens
	}
	if input.CompressionMinimumSavedTokens == 0 {
		input.CompressionMinimumSavedTokens = defaults.CompressionMinimumSavedTokens
	}
	if input.CompressionMaximumReductionBPS == 0 {
		input.CompressionMaximumReductionBPS = defaults.CompressionMaximumReductionBPS
	}
	if input.MinimumShadowSamples == 0 {
		input.MinimumShadowSamples = defaults.MinimumShadowSamples
	}
	if input.Status == "" {
		input.Status = defaults.Status
	}
	if err := input.Validate(); err != nil {
		return Draft{}, err
	}
	return input, nil
}

func (d Draft) Validate() error {
	if d.OrgID <= 0 || d.ProjectID < 0 || !policyRefPattern.MatchString(d.PolicyRef) ||
		len(d.DisplayName) < 1 || len(d.DisplayName) > 160 ||
		(d.ModelSelector != "*" && !portablePattern.MatchString(d.ModelSelector)) ||
		!portablePattern.MatchString(d.Region) || !portablePattern.MatchString(d.Residency) ||
		(d.Status != "active" && d.Status != "disabled") {
		return ErrInvalid
	}
	if !validTarget(d.ResponseCacheTarget) || !validTarget(d.CompressionTarget) {
		return ErrInvalid
	}
	if d.ResponseCacheCanaryBPS < 0 || d.ResponseCacheCanaryBPS > 1000 ||
		d.CompressionCanaryBPS < 0 || d.CompressionCanaryBPS > 1000 {
		return fmt.Errorf("%w: canary basis points must be between 0 and 1000", ErrInvalid)
	}
	if (d.ResponseCacheTarget == TargetCanary) != (d.ResponseCacheCanaryBPS > 0) ||
		(d.CompressionTarget == TargetCanary) != (d.CompressionCanaryBPS > 0) {
		return fmt.Errorf("%w: canary target and allocation disagree", ErrInvalid)
	}
	if d.ResponseCacheTTLSeconds < 60 || d.ResponseCacheTTLSeconds > 86400 ||
		d.MinimumShadowSamples < 10 || d.MinimumShadowSamples > 1_000_000 {
		return ErrInvalid
	}
	if d.ResponseCacheTarget == TargetCanary && (!d.ResponseCachePersistence || !d.ResponseCacheRequireOptIn) {
		return fmt.Errorf("%w: response-cache canary requires explicit persistence and request opt-in", ErrInvalid)
	}
	if d.CompressionTarget == TargetCanary && !d.CompressionRequireOptIn {
		return fmt.Errorf("%w: compression canary requires request opt-in", ErrInvalid)
	}
	if !(promptcompression.Config{
		MinimumInputTokens:  d.CompressionMinimumInputTokens,
		MinimumSavedTokens:  d.CompressionMinimumSavedTokens,
		MaximumReductionBPS: d.CompressionMaximumReductionBPS,
	}).Validate() {
		return ErrInvalid
	}
	return nil
}

func validTarget(value string) bool {
	return value == TargetDisabled || value == TargetCanary
}

func (p Policy) Validate() error {
	if p.ContractVersion != PolicyContractVersion || p.ID <= 0 || p.Revision <= 0 ||
		p.CreatedBy <= 0 || p.UpdatedBy <= 0 {
		return ErrInvalid
	}
	if err := p.Draft.Validate(); err != nil {
		return err
	}
	switch p.Stage {
	case StageDraft, StageValidated, StageShadow, StageCanary, StageRolledBack:
	default:
		return ErrInvalid
	}
	return nil
}

func (p Policy) EffectiveMode(driver string) string {
	if p.Status != "active" {
		return EffectiveDisabled
	}
	target := p.ResponseCacheTarget
	if driver == DriverCompression {
		target = p.CompressionTarget
	}
	if target == TargetDisabled {
		return EffectiveDisabled
	}
	switch p.Stage {
	case StageShadow:
		return EffectiveShadow
	case StageCanary:
		return EffectiveCanary
	default:
		return EffectiveDisabled
	}
}

func (p Policy) CompressionConfig() promptcompression.Config {
	return promptcompression.Config{
		MinimumInputTokens:  p.CompressionMinimumInputTokens,
		MinimumSavedTokens:  p.CompressionMinimumSavedTokens,
		MaximumReductionBPS: p.CompressionMaximumReductionBPS,
	}
}

// Assigned uses only stable policy/principal identity plus UniRouter's
// unoverrideable server operation ID. It neither trusts a caller-supplied
// percentage bucket nor derives persisted evidence from prompt content.
func Assigned(p Policy, driver string, apiKeyID int64, operationID string) bool {
	if p.EffectiveMode(driver) != EffectiveCanary || apiKeyID <= 0 || strings.TrimSpace(operationID) == "" {
		return false
	}
	bps := p.ResponseCacheCanaryBPS
	if driver == DriverCompression {
		bps = p.CompressionCanaryBPS
	}
	hash := sha256.New()
	writeLP(hash, PolicyContractVersion)
	writeInt64(hash, p.OrgID)
	writeInt64(hash, p.ProjectID)
	writeLP(hash, p.PolicyRef)
	writeInt64(hash, p.Revision)
	writeLP(hash, driver)
	writeInt64(hash, apiKeyID)
	writeLP(hash, operationID)
	sum := hash.Sum(nil)
	bucket := int(binary.BigEndian.Uint64(sum[:8]) % 10_000)
	return bucket < bps
}

func AssignmentDigest(p Policy, driver string, apiKeyID int64, operationID string) string {
	hash := sha256.New()
	writeLP(hash, PolicyContractVersion)
	writeInt64(hash, p.OrgID)
	writeInt64(hash, p.ProjectID)
	writeLP(hash, p.PolicyRef)
	writeInt64(hash, p.Revision)
	writeLP(hash, driver)
	writeInt64(hash, apiKeyID)
	writeLP(hash, operationID)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

type Receipt struct {
	OrgID                    int64
	ProjectID                int64
	RequestID                string
	RequestedModel           string
	ResolvedModel            string
	Provider                 string
	Driver                   string
	PolicyRef                string
	PolicyRevision           int64
	Stage                    string
	EffectiveMode            string
	Decision                 string
	Reason                   string
	Assigned                 bool
	Eligible                 bool
	MutationApplied          bool
	OriginalBytes            int
	CandidateBytes           int
	OriginalEstimatedTokens  int
	CandidateEstimatedTokens int
	EstimatedSavedTokens     int
	ReductionBasisPoints     int
	SafetyPassed             bool
	QualityPassed            bool
	LatencyMicros            int64
	RouteChannelID           int64
	RouteCredentialID        int64
	RuntimeAssignmentID      string
	IdentityDigest           string
	CreatedAt                time.Time
}

type CommercialReceipt struct {
	OrgID                     int64
	ProjectID                 int64
	RequestID                 string
	SourceRequestID           string
	PolicyRef                 string
	PolicyRevision            int64
	IdentityDigest            string
	SourceResponseSHA256      string
	RequestedModel            string
	ResolvedModel             string
	Provider                  string
	ChannelID                 int64
	CredentialID              int64
	CredentialEpoch           int64
	RuntimeAssignmentID       string
	CustomerPricingVersion    string
	ResponseCachePriceVersion string
	ProviderRequests          int64
	ProviderCostMicros        int64
	CustomerChargeMicros      int64
	AvoidedCustomerCostMicros int64
	DeliveryStatus            string
	HTTPStatus                int
	CreatedAt                 time.Time
}

func writeLP(hash interface{ Write([]byte) (int, error) }, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	hash.Write(length[:])
	hash.Write([]byte(value))
}

func writeInt64(hash interface{ Write([]byte) (int, error) }, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	hash.Write(encoded[:])
}
