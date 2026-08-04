// Package responsecache implements the exact, tenant-isolated response-cache
// service boundary. It never performs semantic similarity matching.
package responsecache

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/metering"
)

const (
	Version                  = "unirouter.response-cache.v2"
	RequestSchemaChatV1      = "openai.chat.completions.request.v1"
	ResponseSchemaChatV1     = "openai.chat.completions.response.v1"
	EndpointChatCompletions  = "chat.completions"
	DeliveryCompleted        = "completed"
	SettlementSettled        = "settled"
	CustomerChargeZero       = "zero_charge_v1"
	CurrentProviderCostZero  = "0"
	defaultMaximumEntryBytes = 1024 * 1024
	maxUsageTokens           = int64(1<<31 - 1)
)

var (
	ErrInvalidConfig       = errors.New("responsecache: invalid config")
	ErrInvalidIdentity     = errors.New("responsecache: invalid identity")
	ErrInvalidRequest      = errors.New("responsecache: invalid request")
	ErrInvalidProof        = errors.New("responsecache: invalid source proof")
	ErrInvalidResponse     = errors.New("responsecache: invalid response")
	ErrCacheMiss           = errors.New("responsecache: miss")
	ErrLeaseLost           = errors.New("responsecache: lease lost")
	ErrBackendUnavailable  = errors.New("responsecache: backend unavailable")
	ErrEncryptionKey       = errors.New("responsecache: encryption key unavailable")
	ErrIntegrity           = errors.New("responsecache: integrity validation failed")
	ErrReservationMismatch = errors.New("responsecache: reservation mismatch")
)

var portableIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,159}$`)

// Scope is the minimum purge and residency boundary. ProjectID zero means an
// explicitly organization-scoped API key; it is not equivalent to an unknown
// project.
type Scope struct {
	OrgID     int64  `json:"org_id"`
	ProjectID int64  `json:"project_id"`
	Region    string `json:"region"`
	Residency string `json:"residency"`
}

func (s Scope) Validate() error {
	if s.OrgID <= 0 || s.ProjectID < 0 ||
		!portableIdentity(s.Region) || !portableIdentity(s.Residency) {
		return ErrInvalidIdentity
	}
	return nil
}

// Identity binds every authority or behavior version that may affect whether
// an exact response remains consumable. None of these fields contains prompt
// or response content.
type Identity struct {
	Scope                     Scope  `json:"scope"`
	APIKeyID                  int64  `json:"api_key_id"`
	AuthorizationEpoch        string `json:"authorization_epoch"`
	Endpoint                  string `json:"endpoint"`
	RequestedModel            string `json:"requested_model"`
	ResolvedModel             string `json:"resolved_model"`
	Provider                  string `json:"provider"`
	ChannelID                 int64  `json:"channel_id"`
	ChannelRoutingEpoch       int64  `json:"channel_routing_epoch"`
	ChannelCircuitEpoch       int64  `json:"channel_circuit_epoch"`
	CredentialID              int64  `json:"credential_id"`
	CredentialEpoch           int64  `json:"credential_epoch"`
	AdapterProfileVersion     string `json:"adapter_profile_version"`
	RoutePolicyVersion        string `json:"route_policy_version"`
	RuntimeReleaseRef         string `json:"runtime_release_ref"`
	RuntimeStage              string `json:"runtime_stage"`
	RuntimeAssignmentID       string `json:"runtime_assignment_id"`
	GuardrailPolicyVersion    string `json:"guardrail_policy_version"`
	OptimizationPolicyVersion string `json:"optimization_policy_version"`
	CustomerPricingVersion    string `json:"customer_pricing_version"`
	ResponseCachePriceVersion string `json:"response_cache_price_version"`
	RequestSchemaVersion      string `json:"request_schema_version"`
	ResponseSchemaVersion     string `json:"response_schema_version"`
}

func (i Identity) Validate() error {
	if err := i.Scope.Validate(); err != nil {
		return err
	}
	if i.APIKeyID <= 0 || i.ChannelID <= 0 || i.ChannelRoutingEpoch <= 0 ||
		i.ChannelCircuitEpoch <= 0 || i.CredentialID <= 0 || i.CredentialEpoch <= 0 ||
		i.Endpoint != EndpointChatCompletions ||
		i.RequestSchemaVersion != RequestSchemaChatV1 ||
		i.ResponseSchemaVersion != ResponseSchemaChatV1 {
		return ErrInvalidIdentity
	}
	for _, value := range []string{
		i.AuthorizationEpoch,
		i.RequestedModel,
		i.ResolvedModel,
		i.Provider,
		i.AdapterProfileVersion,
		i.RoutePolicyVersion,
		i.RuntimeReleaseRef,
		i.RuntimeStage,
		i.RuntimeAssignmentID,
		i.GuardrailPolicyVersion,
		i.OptimizationPolicyVersion,
		i.CustomerPricingVersion,
		i.ResponseCachePriceVersion,
	} {
		if !portableIdentity(value) {
			return ErrInvalidIdentity
		}
	}
	return nil
}

func portableIdentity(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) <= 160 && portableIdentityPattern.MatchString(value)
}

// Request contains the post-input-policy bytes that would be sent upstream.
// EffectiveBody is HMAC-bound but is never stored in the cache index.
type Request struct {
	Identity           Identity      `json:"identity"`
	EffectiveBody      []byte        `json:"-"`
	TTL                time.Duration `json:"-"`
	OptIn              bool          `json:"opt_in"`
	PersistenceAllowed bool          `json:"persistence_allowed"`
	NoStore            bool          `json:"no_store"`
}

type DecisionKind string

const (
	DecisionHit      DecisionKind = "hit"
	DecisionOwner    DecisionKind = "owner"
	DecisionWait     DecisionKind = "wait"
	DecisionBypass   DecisionKind = "bypass"
	DecisionReject   DecisionKind = "reject"
	DecisionEligible DecisionKind = "eligible"
)

const (
	ReasonHit                  = "exact_hit"
	ReasonMissOwner            = "miss_owner"
	ReasonMissWait             = "miss_wait"
	ReasonNotOptedIn           = "not_opted_in"
	ReasonPersistenceDenied    = "persistence_denied"
	ReasonNoStore              = "client_no_store"
	ReasonIneligibleShape      = "ineligible_request_shape"
	ReasonBackendUnavailable   = "backend_unavailable"
	ReasonWaitTimeout          = "wait_timeout"
	ReasonWaiterCapacity       = "waiter_capacity"
	ReasonIntegrityRecovery    = "integrity_recovery"
	ReasonInvalidConfiguration = "invalid_configuration"
	ReasonRequestCancelled     = "request_cancelled"
	ReasonEligible             = "eligible"
)

// UsageProof is an encrypted cache-entry field. B5 admits only provider-
// reported, final usage whose customer delivery and settlement completed.
// Estimated or merely HTTP-success responses are not cacheable.
type UsageProof struct {
	SourceRequestID      string                  `json:"source_request_id"`
	PromptTokens         int64                   `json:"prompt_tokens"`
	UncachedPromptTokens int64                   `json:"uncached_prompt_tokens"`
	CacheReadTokens      int64                   `json:"provider_prompt_cache_read_tokens"`
	CacheCreationTokens  int64                   `json:"provider_prompt_cache_creation_tokens"`
	CompletionTokens     int64                   `json:"completion_tokens"`
	UsageSource          string                  `json:"usage_source"`
	UsageAuthority       metering.UsageAuthority `json:"usage_authority"`
	UsagePurpose         metering.UsagePurpose   `json:"usage_purpose"`
	UsageFinality        metering.UsageFinality  `json:"usage_finality"`
	UsageContractVersion string                  `json:"usage_contract_version"`
	CustomerPricing      string                  `json:"customer_pricing_version"`
	DeliveryFinality     string                  `json:"delivery_finality"`
	SettlementFinality   string                  `json:"settlement_finality"`
	Provider             string                  `json:"source_provider"`
	MappedModel          string                  `json:"source_mapped_model"`
	ProviderRequests     int64                   `json:"source_provider_requests"`
	HTTPStatus           int                     `json:"source_http_status"`
}

func (p UsageProof) Validate(identity Identity) error {
	if err := identity.Validate(); err != nil {
		return ErrInvalidProof
	}
	if !portableIdentity(p.SourceRequestID) || p.PromptTokens < 0 || p.UncachedPromptTokens < 0 ||
		p.CacheReadTokens < 0 || p.CacheCreationTokens < 0 ||
		p.CompletionTokens < 0 ||
		p.PromptTokens > maxUsageTokens || p.UncachedPromptTokens > maxUsageTokens ||
		p.CacheReadTokens > maxUsageTokens || p.CacheCreationTokens > maxUsageTokens ||
		p.CompletionTokens > maxUsageTokens ||
		p.UncachedPromptTokens+p.CacheReadTokens+p.CacheCreationTokens != p.PromptTokens ||
		p.UsageSource != metering.UsageSourceUpstream ||
		p.UsageAuthority != metering.UsageAuthorityProviderReported ||
		p.UsagePurpose != metering.UsagePurposeCustomerCharge ||
		p.UsageFinality != metering.UsageFinalityFinal ||
		p.UsageContractVersion != metering.UsageContractVersion ||
		p.CustomerPricing != identity.CustomerPricingVersion ||
		p.DeliveryFinality != DeliveryCompleted ||
		p.SettlementFinality != SettlementSettled ||
		!portableIdentity(p.Provider) ||
		p.MappedModel != identity.ResolvedModel ||
		p.ProviderRequests <= 0 ||
		p.HTTPStatus != 200 {
		return ErrInvalidProof
	}
	return nil
}

type StoreEntry struct {
	SourceResponse []byte     `json:"-"`
	Proof          UsageProof `json:"proof"`
}

// Hit is not a wire response. SourceResponse and SourceProof describe the
// historical provider execution; an endpoint adapter must construct current
// request-local identity, usage and billing metadata before any future serve.
// The cache service makes no customer-price choice.
type Hit struct {
	SourceResponse            []byte     `json:"-"`
	SourceProof               UsageProof `json:"source_proof"`
	IdentityDigest            string     `json:"identity_digest"`
	SourceResponseSHA256      string     `json:"source_response_sha256"`
	CurrentProviderRequests   int64      `json:"current_provider_requests"`
	CurrentProviderCostMicros string     `json:"current_provider_cost_micros"`
	CustomerChargeState       string     `json:"customer_charge_state"`
	CreatedAt                 time.Time  `json:"created_at"`
	ExpiresAt                 time.Time  `json:"expires_at"`
}

type Decision struct {
	Kind        DecisionKind `json:"kind"`
	Reason      string       `json:"reason"`
	Hit         *Hit         `json:"hit,omitempty"`
	Reservation *Reservation `json:"-"`
	Wait        *Wait        `json:"-"`
}

// Reservation and Wait are opaque capabilities. Their fields are deliberately
// unexported so they cannot become an API or log payload.
type Reservation struct {
	serviceID       string
	entryKey        string
	leaseKey        string
	leaseToken      string
	scopeGeneration uint64
	identity        Identity
	identityDigest  string
	entryTTL        time.Duration
	acquiredAt      time.Time
}

func (r *Reservation) IdentityDigest() string {
	if r == nil || r.identityDigest == "" {
		return ""
	}
	return "hmac-sha256:" + r.identityDigest
}

type Wait struct {
	serviceID       string
	entryKey        string
	leaseKey        string
	scopeGeneration uint64
	identity        Identity
	identityDigest  string
	entryTTL        time.Duration
}

type Config struct {
	TTL               time.Duration
	LeaseTTL          time.Duration
	MaxWait           time.Duration
	PollInterval      time.Duration
	MaxWaiters        int
	MaxRequestBytes   int
	MaxEntryBytes     int
	DependencyTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		TTL: 5 * time.Minute, LeaseTTL: 45 * time.Second,
		MaxWait: 2 * time.Second, PollInterval: 20 * time.Millisecond,
		MaxWaiters: 64, MaxRequestBytes: 1024 * 1024,
		MaxEntryBytes: defaultMaximumEntryBytes, DependencyTimeout: 100 * time.Millisecond,
	}
}

func (c Config) Validate() error {
	if c.TTL < time.Second || c.TTL > 24*time.Hour ||
		c.LeaseTTL < time.Second || c.LeaseTTL > 5*time.Minute ||
		c.MaxWait <= 0 || c.MaxWait > c.LeaseTTL ||
		c.PollInterval <= 0 || c.PollInterval > c.MaxWait ||
		c.MaxWaiters <= 0 || c.MaxWaiters > 4096 ||
		c.MaxRequestBytes <= 0 || c.MaxRequestBytes > 16*1024*1024 ||
		c.MaxEntryBytes <= 0 || c.MaxEntryBytes > 16*1024*1024 ||
		c.DependencyTimeout <= 0 || c.DependencyTimeout > 2*time.Second {
		return ErrInvalidConfig
	}
	return nil
}

// Backend stores only opaque HMAC keys and encrypted values.
type Backend interface {
	Generation(ctx context.Context, scopeKey string) (uint64, error)
	Read(ctx context.Context, entryKey string) ([]byte, error)
	Acquire(ctx context.Context, leaseKey, token string, ttl time.Duration) (bool, error)
	Commit(ctx context.Context, entryKey, leaseKey, token string, encrypted []byte, ttl time.Duration) error
	Release(ctx context.Context, leaseKey, token string) error
	Invalidate(ctx context.Context, entryKey string) error
	Purge(ctx context.Context, scopeKey string) (uint64, error)
}

// EncryptionKeyring supplies versioned 32-byte AES keys. Returned bytes must
// be defensive copies. Key identifiers are metadata; keys are not.
type EncryptionKeyring interface {
	Current(ctx context.Context) (version string, key []byte, err error)
	ByVersion(ctx context.Context, version string) ([]byte, error)
}

type runtimeOptions struct {
	clock  func() time.Time
	random io.Reader
}

type Option func(*runtimeOptions)

func WithClock(clock func() time.Time) Option {
	return func(options *runtimeOptions) {
		if clock != nil {
			options.clock = clock
		}
	}
}

func WithRandom(random io.Reader) Option {
	return func(options *runtimeOptions) {
		if random != nil {
			options.random = random
		}
	}
}

func validateServiceDependency(backend Backend, indexKey []byte, keyring EncryptionKeyring) error {
	if backend == nil || keyring == nil || len(indexKey) != 32 {
		return ErrInvalidConfig
	}
	return nil
}

func safeReason(reason string) string {
	if portableIdentity(reason) {
		return reason
	}
	return ReasonInvalidConfiguration
}

func invalidDecision(reason string) Decision {
	return Decision{Kind: DecisionReject, Reason: safeReason(reason)}
}

func bypassDecision(reason string) Decision {
	return Decision{Kind: DecisionBypass, Reason: safeReason(reason)}
}
