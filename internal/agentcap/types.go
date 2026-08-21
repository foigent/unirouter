package agentcap

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/models"
)

const (
	SchemaVersion = "unirouter.agent-capability.v2"
	TokenPrefix   = "urc_"
	Issuer        = "unirouter"
	Audience      = "unirouter-model-gateway"
	SigningKeyID  = "agent-capability-hs256-v2"

	TokenTTL             = 5 * time.Minute
	RefreshBase          = 3 * time.Minute
	RefreshJitter        = 15 * time.Second
	AuthorityCacheTTL    = 15 * time.Second
	MinWorkflowTTL       = 5 * time.Minute
	MaxWorkflowTTL       = 24 * time.Hour
	MaxAgentDepth        = 16
	MaxWorkflowAgents    = 10_000
	MaxAuthorityCacheKey = 10_000
	// Redis Lua represents numbers as IEEE-754 doubles. Keeping runtime budget
	// values at or below 2^53-1 makes every comparison and HINCRBY delta exact.
	MaxExactRuntimeBudget int64 = 1<<53 - 1

	DefaultInvocationLeaseTTL   = 5 * time.Minute
	MaxInvocationLeaseTTL       = 24 * time.Hour
	InvocationFinalizationGrace = 5 * time.Minute
)

var (
	ErrInvalidRequest      = errors.New("invalid agent capability request")
	ErrInvalidToken        = errors.New("invalid agent capability token")
	ErrInactiveRoot        = errors.New("agent capability root authority is inactive")
	ErrAuthorityChanged    = errors.New("agent capability root authority changed")
	ErrRevoked             = errors.New("agent capability revoked")
	ErrSuperseded          = errors.New("agent capability token superseded")
	ErrConflict            = errors.New("agent capability conflict")
	ErrLimitExceeded       = errors.New("agent capability limit exceeded")
	ErrManagementForbidden = errors.New("agent capability management is not allowed for this root")
	ErrRefreshTooEarly     = errors.New("agent capability refresh requested too early")
	ErrUnavailable         = errors.New("agent capability state unavailable")
	ErrCostBudgetExceeded  = errors.New("agent capability cost budget exceeded")
	ErrTokenBudgetExceeded = errors.New("agent capability token budget exceeded")
	ErrRPSLimitExceeded    = errors.New("agent capability RPS limit exceeded")
	ErrRPMLimitExceeded    = errors.New("agent capability RPM limit exceeded")
	ErrConcurrencyExceeded = errors.New("agent capability concurrency limit exceeded")
)

// Limits are absolute ceilings carried by a capability. Delegation may only
// keep or reduce these values; it can never increase them.
type Limits struct {
	MaxDepth         int      `json:"max_depth"`
	MaxChildren      int      `json:"max_children"`
	MaxDescendants   int      `json:"max_descendants"`
	CostBudgetCents  int64    `json:"cost_budget_cents"`
	TokenBudget      int64    `json:"token_budget"`
	RPMLimit         int      `json:"rpm_limit"`
	RPSLimit         int      `json:"rps_limit"`
	ConcurrencyLimit int      `json:"concurrency_limit"`
	AllowedModels    []string `json:"allowed_models"`
	AllowedEndpoints []string `json:"allowed_endpoints"`
}

// ExchangeRequest explicitly registers the root Agent and its workflow. The
// workflow identifier is server-generated so caller labels cannot collide
// with another tenant's authority state.
type ExchangeRequest struct {
	RootAgentID        string `json:"root_agent_id"`
	WorkflowTTLSeconds int64  `json:"workflow_ttl_seconds"`
	Limits             Limits `json:"limits"`
}

// DelegateRequest explicitly registers one child Agent under the authenticated
// parent capability.
type DelegateRequest struct {
	ChildAgentID string `json:"child_agent_id"`
	Limits       Limits `json:"limits"`
}

type RevokeRequest struct {
	WorkflowID string `json:"workflow_id"`
}

// Claims are signed, audience-bound model-gateway authority. They deliberately
// contain no Tool, IAM, database, write or export permission.
type Claims struct {
	SchemaVersion        string   `json:"schema_version"`
	WorkflowID           string   `json:"workflow_id"`
	WorkflowGeneration   string   `json:"workflow_generation"`
	WorkflowExpiresAt    int64    `json:"workflow_expires_at"`
	RootAgentID          string   `json:"root_agent_id"`
	AgentID              string   `json:"agent_id"`
	ParentAgentID        string   `json:"parent_agent_id,omitempty"`
	Lineage              []string `json:"lineage"`
	Depth                int      `json:"depth"`
	RootKeyID            int64    `json:"root_key_id"`
	OrgID                int64    `json:"org_id"`
	ProjectID            int64    `json:"project_id,omitempty"`
	OwnerUserID          int64    `json:"owner_user_id,omitempty"`
	RootAuthorityVersion string   `json:"root_authority_version"`
	RootRPMLimit         int      `json:"root_rpm_limit"`
	RootRPSLimit         int      `json:"root_rps_limit"`
	Limits               Limits   `json:"limits"`
	jwt.RegisteredClaims
}

type Authentication struct {
	APIKey *models.ApiKey
	Claims *Claims
}

type TokenResponse struct {
	SchemaVersion     string    `json:"schema_version"`
	CapabilityToken   string    `json:"capability_token"`
	TokenType         string    `json:"token_type"`
	WorkflowID        string    `json:"workflow_id"`
	RootAgentID       string    `json:"root_agent_id"`
	AgentID           string    `json:"agent_id"`
	ParentAgentID     string    `json:"parent_agent_id,omitempty"`
	Depth             int       `json:"depth"`
	ExpiresAt         time.Time `json:"expires_at"`
	RefreshAfter      time.Time `json:"refresh_after"`
	WorkflowExpiresAt time.Time `json:"workflow_expires_at"`
	Limits            Limits    `json:"limits"`
}

type RevokeResponse struct {
	SchemaVersion string    `json:"schema_version"`
	WorkflowID    string    `json:"workflow_id"`
	Status        string    `json:"status"`
	RevokedAt     time.Time `json:"revoked_at"`
}

func (c *Claims) AllowsEndpoint(path string) bool {
	if c == nil {
		return false
	}
	return slices.Contains(c.Limits.AllowedEndpoints, accessscope.CanonicalExecutionEndpoint(path))
}

func IsControlEndpoint(path string) bool {
	return accessscope.IsCapabilityManagementEndpoint(path)
}

func (l Limits) ValidateShape() error {
	if l.MaxDepth < 0 || l.MaxDepth > MaxAgentDepth {
		return fmt.Errorf("%w: max_depth must be between 0 and %d", ErrInvalidRequest, MaxAgentDepth)
	}
	if l.MaxChildren < 0 || l.MaxChildren > MaxWorkflowAgents {
		return fmt.Errorf("%w: max_children must be between 0 and %d", ErrInvalidRequest, MaxWorkflowAgents)
	}
	if l.MaxDescendants < 0 || l.MaxDescendants > MaxWorkflowAgents {
		return fmt.Errorf("%w: max_descendants must be between 0 and %d", ErrInvalidRequest, MaxWorkflowAgents)
	}
	if l.MaxChildren > l.MaxDescendants {
		return fmt.Errorf("%w: max_children cannot exceed max_descendants", ErrInvalidRequest)
	}
	if l.CostBudgetCents <= 0 || l.TokenBudget <= 0 || l.RPMLimit <= 0 || l.RPSLimit <= 0 || l.ConcurrencyLimit <= 0 {
		return fmt.Errorf("%w: cost, token, RPM, RPS and concurrency limits must be positive", ErrInvalidRequest)
	}
	if l.CostBudgetCents > MaxExactRuntimeBudget/10_000 || l.TokenBudget > MaxExactRuntimeBudget ||
		int64(l.RPMLimit) > MaxExactRuntimeBudget || int64(l.RPSLimit) > MaxExactRuntimeBudget ||
		int64(l.ConcurrencyLimit) > MaxExactRuntimeBudget {
		return fmt.Errorf("%w: runtime limits exceed the exact shared-counter range", ErrInvalidRequest)
	}
	if len(l.AllowedModels) == 0 || len(l.AllowedEndpoints) == 0 {
		return fmt.Errorf("%w: allowed_models and allowed_endpoints are required", ErrInvalidRequest)
	}
	if len(l.AllowedModels) > 128 || len(l.AllowedEndpoints) > len(modeloperation.AgentCapabilityPaths()) {
		return fmt.Errorf("%w: too many models or endpoints", ErrInvalidRequest)
	}
	for _, value := range append(slices.Clone(l.AllowedModels), l.AllowedEndpoints...) {
		if len(value) > 128 {
			return fmt.Errorf("%w: model and endpoint names must be <= 128 characters", ErrInvalidRequest)
		}
	}
	return nil
}

func (l Limits) Normalize() Limits {
	l.AllowedModels = normalizedSet(l.AllowedModels)
	l.AllowedEndpoints = normalizedSet(l.AllowedEndpoints)
	return l
}

func (l Limits) IsAttenuationOf(parent Limits) bool {
	if l.MaxDepth > parent.MaxDepth || l.MaxChildren > parent.MaxChildren ||
		l.MaxDescendants > parent.MaxDescendants || l.CostBudgetCents > parent.CostBudgetCents ||
		l.TokenBudget > parent.TokenBudget || l.RPMLimit > parent.RPMLimit ||
		l.RPSLimit > parent.RPSLimit || l.ConcurrencyLimit > parent.ConcurrencyLimit {
		return false
	}
	return setSubset(l.AllowedModels, parent.AllowedModels) &&
		setSubset(l.AllowedEndpoints, parent.AllowedEndpoints)
}

func normalizedSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
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

func setSubset(child, parent []string) bool {
	if slices.Contains(parent, "*") {
		return true
	}
	for _, value := range child {
		if value == "*" || !slices.Contains(parent, value) {
			return false
		}
	}
	return true
}
