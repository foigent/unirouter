package runtimeconfig

import (
	"fmt"

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
	BudgetAlerts    BudgetAlertPolicy     `json:"budget_alerts"`
}

type RoutingPolicy struct {
	Version string `json:"version"`
	Source  string `json:"source"`
	routingpolicy.Config
}

type StreamTransportPolicy struct {
	Version string `json:"version"`
	Source  string `json:"source"`
	streampolicy.Config
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
			Version: routingpolicy.Version,
			Source:  "startup_environment",
			Config:  routing,
		},
		StreamTransport: StreamTransportPolicy{
			Version: streampolicy.Version,
			Source:  "startup_environment",
			Config:  stream,
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
