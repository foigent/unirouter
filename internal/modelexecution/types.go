// Package modelexecution owns the last database boundary before provider
// traffic is allowed. It does not select routes, prepare provider payloads, or
// advance billing; those responsibilities remain with modelrouting, adapters,
// and billing respectively.
package modelexecution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

// Status is the semantic result of a pre-dispatch revalidation. Only Applied
// permits a provider network call. The other statuses are normal control-plane
// races and intentionally do not create a provider-attempt row.
type Status string

const (
	Applied   Status = "applied"
	Stale     Status = "stale"
	Disabled  Status = "disabled"
	LeaseLost Status = "lease_lost"
)

var (
	ErrInvalid          = errors.New("modelexecution: invalid request")
	ErrUnavailable      = errors.New("modelexecution: unavailable")
	ErrDispatchNotWired = errors.New("modelexecution: operation dispatch is not wired")
)

type Config struct {
	HalfOpenLease       time.Duration
	BreakerRecovery     time.Duration
	RateLimitCooldown   time.Duration
	AuthFailureCooldown time.Duration
}

func DefaultConfig() Config {
	config, _ := ConfigFromRoutingPolicy(routingpolicy.Default())
	return config
}

// ConfigFromRoutingPolicy keeps the execution CAS durations on the same
// startup contract as route evaluation. Dynamic degradation thresholds remain
// row authority and are deliberately not copied into this config.
func ConfigFromRoutingPolicy(policy routingpolicy.Config) (Config, error) {
	if err := policy.Validate(); err != nil {
		return Config{}, fmt.Errorf("%w: routing policy: %v", ErrInvalid, err)
	}
	config := Config{
		HalfOpenLease:       time.Duration(policy.HalfOpenProbeLeaseSeconds) * time.Second,
		BreakerRecovery:     time.Duration(policy.CircuitBreakerRecoverSeconds) * time.Second,
		RateLimitCooldown:   time.Duration(policy.RateLimitCooldownSeconds) * time.Second,
		AuthFailureCooldown: time.Duration(policy.AuthFailureCooldownSeconds) * time.Second,
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) validate() error {
	if c.HalfOpenLease < time.Second || c.HalfOpenLease > 5*time.Minute || c.HalfOpenLease%time.Second != 0 {
		return fmt.Errorf("%w: half-open lease must be a whole second between 1s and 5m", ErrInvalid)
	}
	if c.BreakerRecovery < time.Second || c.BreakerRecovery > 24*time.Hour || c.BreakerRecovery%time.Second != 0 {
		return fmt.Errorf("%w: breaker recovery must be a whole second between 1s and 24h", ErrInvalid)
	}
	if c.RateLimitCooldown < time.Second || c.RateLimitCooldown > 7*24*time.Hour || c.RateLimitCooldown%time.Second != 0 {
		return fmt.Errorf("%w: rate-limit cooldown must be a whole second between 1s and 7d", ErrInvalid)
	}
	if c.AuthFailureCooldown < time.Second || c.AuthFailureCooldown > 7*24*time.Hour || c.AuthFailureCooldown%time.Second != 0 {
		return fmt.Errorf("%w: auth-failure cooldown must be a whole second between 1s and 7d", ErrInvalid)
	}
	return nil
}

// AttemptIdentity contains only caller and financial identities. Route and
// credential identities are always copied from the revalidated route evidence,
// never accepted independently from a gateway caller.
type AttemptIdentity struct {
	RequestID          string
	AttemptID          string
	OrgID              int64
	APIKeyID           int64
	AccessCredentialID int64
	CredentialRef      string
	AccessGrantID      int64
	RouteSequence      int
	RouteTotal         int
	Endpoint           string
}

// Decision is a bounded dispatch receipt. ProbeLeaseToken and
// DispatchOwnerToken are ephemeral process-only capabilities; neither may be
// logged, serialized into public DTOs, or persisted in raw form.
type Decision struct {
	Status Status `json:"status"`
	// Evidence and PublicEvidence are mutually exclusive. Public execution
	// keeps its V2 digest through attempt, owner, health fence, and projection;
	// the embedded V1 route identity is used only for currentness CAS checks.
	Evidence           executionevidence.EvidenceV1  `json:"evidence"`
	PublicEvidence     *executionevidence.EvidenceV2 `json:"public_evidence,omitempty"`
	EvidenceHash       string                        `json:"evidence_hash"`
	ProbeLeaseToken    string                        `json:"-"`
	DispatchOwnerToken string                        `json:"-"`
	DatabaseTime       time.Time                     `json:"database_time"`
	ProviderAttemptKey ProviderAttemptKey            `json:"provider_attempt_key"`
}

type ProviderAttemptKey struct {
	OrgID     int64
	RequestID string
	AttemptID string
}

// HealthResult is the committed outcome of a post-dispatch compare-and-swap.
// Applied means every projection required by the authority mode committed in
// one transaction. Other statuses are expected races and never leave a partial
// verify-mode update behind.
type HealthResult struct {
	Status       Status
	DatabaseTime time.Time
}

type txStarter interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type attemptLedger interface {
	BeginProviderAttemptOwnedTx(context.Context, pgx.Tx, billing.ProviderAttemptStart, string) (bool, error)
	ValidateProviderAttemptOwnerTx(context.Context, pgx.Tx, billing.ProviderAttemptOwnerRequest) error
	FenceProviderAttemptHealthTx(context.Context, pgx.Tx, billing.ProviderAttemptHealthFenceRequest) (billing.ProviderAttemptHealthFenceResult, error)
}

type tokenSource func() (string, error)

type Coordinator struct {
	database             txStarter
	ledger               attemptLedger
	config               Config
	newToken             tokenSource
	dispatchMaterializer DispatchMaterializer
}

// DispatchMaterializer records the minimum source proof only after the existing
// attempt/authority locks and successful revalidation, before the same commit.
// It does not authorize a dispatch or alter the retry and ownership contracts.
type DispatchMaterializer interface {
	MaterializeDispatchTx(context.Context, pgx.Tx, modelrouting.Plan, modelrouting.Route, billing.ProviderAttemptStart, string, time.Time) error
}

func (c *Coordinator) SetDispatchMaterializer(materializer DispatchMaterializer) {
	if c != nil {
		c.dispatchMaterializer = materializer
	}
}

func New(database txStarter, ledger attemptLedger, config Config) (*Coordinator, error) {
	if database == nil || ledger == nil {
		return nil, fmt.Errorf("%w: database and attempt ledger are required", ErrInvalid)
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &Coordinator{database: database, ledger: ledger, config: config, newToken: randomProbeToken}, nil
}

type preparedRequest struct {
	plan                   modelrouting.Plan
	route                  modelrouting.Route
	identity               AttemptIdentity
	evidence               executionevidence.EvidenceV1
	publicEvidence         *executionevidence.EvidenceV2
	dispatchEvidence       executionevidence.EvidenceV1
	dispatchPublicEvidence *executionevidence.EvidenceV2
	dispatchEvidenceHash   string
	probeToken             string
	dispatchOwnerToken     string
	dispatchOwnerHash      string
	runtimePin             *runtimeAttemptPin
	underlying             operationIdentity
}

// One ephemeral dispatch owner keeps this choice across bounded transaction
// recovery, including an M113 commit followed by an M114 expansion.
type runtimeAttemptPin struct {
	resolved bool
	instance string
}

type operationIdentity struct {
	id             string
	profileID      string
	profileVersion string
}

type semanticResult struct {
	status       Status
	databaseTime time.Time
	probeToken   string
}
