package runtimeconfig

import (
	"fmt"

	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/streampolicy"
)

const (
	Version                  = "effective-config.v1"
	BudgetAlertPolicyVersion = "project-budget-alert.v1"
)

// Snapshot is the typed, read-only registry of configuration that affects the
// current process. It contains effective values and stable database contracts,
// never environment dumps, webhook URLs, credentials, or other secret values.
type Snapshot struct {
	Version         string                `json:"version"`
	Source          string                `json:"source"`
	RoutingPolicy   RoutingPolicy         `json:"routing_policy"`
	StreamTransport StreamTransportPolicy `json:"stream_transport"`
	AgentCapability AgentCapabilityPolicy `json:"agent_capability"`
	BudgetAlerts    BudgetAlertPolicy     `json:"budget_alerts"`
}

type RoutingPolicy struct {
	Version             string `json:"version"`
	SelectionVersion    string `json:"selection_version"`
	Source              string `json:"source"`
	PrioritySemantics   string `json:"priority_semantics"`
	WeightSemantics     string `json:"weight_semantics"`
	SnapshotFailureMode string `json:"snapshot_failure_mode"`
	ProbeOwnership      string `json:"probe_ownership"`
	routingpolicy.Config
}

type StreamTransportPolicy struct {
	Version string `json:"version"`
	Source  string `json:"source"`
	streampolicy.Config
}

type AgentCapabilityPolicy struct {
	Version                    string `json:"version"`
	Source                     string `json:"source"`
	DiscoveryMode              string `json:"discovery_mode"`
	AuthorityScope             string `json:"authority_scope"`
	TokenTTLSeconds            int    `json:"token_ttl_seconds"`
	RefreshBaseSeconds         int    `json:"refresh_base_seconds"`
	RefreshJitterSeconds       int    `json:"refresh_jitter_seconds"`
	RootAuthorityCacheSeconds  int    `json:"root_authority_cache_seconds"`
	ActiveRevocationSLOSeconds int    `json:"active_revocation_slo_seconds"`
	MaxWorkflowTTLSeconds      int    `json:"max_workflow_ttl_seconds"`
	MaxDepth                   int    `json:"max_depth"`
	MaxWorkflowAgents          int    `json:"max_workflow_agents"`
	RedisOutageMode            string `json:"redis_outage_mode"`
	NormalExpiryMode           string `json:"normal_expiry_mode"`
	RefreshConcurrency         string `json:"refresh_concurrency"`
	RuntimeBudgetMode          string `json:"runtime_budget_mode"`
	RuntimeRateMode            string `json:"runtime_rate_mode"`
	RuntimeConcurrencyMode     string `json:"runtime_concurrency_mode"`
	RuntimeRecoveryMode        string `json:"runtime_recovery_mode"`
	MaxInvocationLeaseSeconds  int    `json:"max_invocation_lease_seconds"`
	FinalizationGraceSeconds   int    `json:"finalization_grace_seconds"`
}

// BudgetAlertPolicy describes the one active budget-alert contract. Budgets
// are scoped to a project and delivered through the tenant-scoped notification
// event/webhook ledger. The historical global settings are retained only as
// retired data so an operator cannot mistake them for effective configuration.
type BudgetAlertPolicy struct {
	Version                    string   `json:"version"`
	Scope                      string   `json:"scope"`
	BudgetSource               string   `json:"budget_source"`
	PeriodSource               string   `json:"period_source"`
	EvaluationTrigger          string   `json:"evaluation_trigger"`
	EventLedger                string   `json:"event_ledger"`
	DeliveryRouteSource        string   `json:"delivery_route_source"`
	EventType                  string   `json:"event_type"`
	ThresholdPercent           int      `json:"threshold_percent"`
	LegacyGlobalSettingsActive bool     `json:"legacy_global_settings_active"`
	RetiredGlobalSettingKeys   []string `json:"retired_global_setting_keys"`
}

func New(routing routingpolicy.Config, stream streampolicy.Config) (Snapshot, error) {
	if err := routing.Validate(); err != nil {
		return Snapshot{}, fmt.Errorf("routing policy: %w", err)
	}
	if err := stream.Validate(); err != nil {
		return Snapshot{}, fmt.Errorf("stream transport policy: %w", err)
	}
	return Snapshot{
		Version: Version,
		Source:  "startup_environment_and_schema_contract",
		RoutingPolicy: RoutingPolicy{
			Version:             routingpolicy.Version,
			SelectionVersion:    routingpolicy.SelectionVersion,
			Source:              "startup_environment_and_channel_state",
			PrioritySemantics:   "lower_first_then_failover",
			WeightSemantics:     "deterministic_weighted_order_within_equal_priority",
			SnapshotFailureMode: "bounded_last_known_good_then_fail_closed",
			ProbeOwnership:      "postgresql_epoch_cas_single_lease",
			Config:              routing,
		},
		StreamTransport: StreamTransportPolicy{
			Version: streampolicy.Version,
			Source:  "startup_environment",
			Config:  stream,
		},
		AgentCapability: AgentCapabilityPolicy{
			Version:                    agentcap.SchemaVersion,
			Source:                     "compiled_security_contract",
			DiscoveryMode:              "explicit_supervisor_exchange_and_parent_delegation",
			AuthorityScope:             "model_gateway_only",
			TokenTTLSeconds:            int(agentcap.TokenTTL.Seconds()),
			RefreshBaseSeconds:         int(agentcap.RefreshBase.Seconds()),
			RefreshJitterSeconds:       int(agentcap.RefreshJitter.Seconds()),
			RootAuthorityCacheSeconds:  int(agentcap.AuthorityCacheTTL.Seconds()),
			ActiveRevocationSLOSeconds: int(agentcap.AuthorityCacheTTL.Seconds()),
			MaxWorkflowTTLSeconds:      int(agentcap.MaxWorkflowTTL.Seconds()),
			MaxDepth:                   agentcap.MaxAgentDepth,
			MaxWorkflowAgents:          agentcap.MaxWorkflowAgents,
			RedisOutageMode:            "fail_closed",
			NormalExpiryMode:           "deny_new_admission_keep_admitted_work",
			RefreshConcurrency:         "server_convergent_and_client_singleflight",
			RuntimeBudgetMode:          "reserve_estimate_on_every_ancestor_settle_actual",
			RuntimeRateMode:            "exact_redis_sliding_window_on_every_ancestor",
			RuntimeConcurrencyMode:     "expiring_redis_lease_on_every_ancestor",
			RuntimeRecoveryMode:        "conservative_ttl_until_p6_f20_durable_terminal_hook",
			MaxInvocationLeaseSeconds:  int(agentcap.MaxInvocationLeaseTTL.Seconds()),
			FinalizationGraceSeconds:   int(agentcap.InvocationFinalizationGrace.Seconds()),
		},
		BudgetAlerts: BudgetAlertPolicy{
			Version:                    BudgetAlertPolicyVersion,
			Scope:                      "project",
			BudgetSource:               "projects.budget_cents",
			PeriodSource:               "projects.budget_period",
			EvaluationTrigger:          "post_settlement",
			EventLedger:                "notification_events",
			DeliveryRouteSource:        "notification_webhooks",
			EventType:                  "project_budget.threshold",
			ThresholdPercent:           100,
			LegacyGlobalSettingsActive: false,
			RetiredGlobalSettingKeys: []string{
				"budget_alert_webhook_url",
				"budget_alert_threshold_cents",
				"budget_cents",
			},
		},
	}, nil
}

func Default() Snapshot {
	snapshot, err := New(routingpolicy.Default(), streampolicy.Default())
	if err != nil {
		panic(err)
	}
	return snapshot
}
