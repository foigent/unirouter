package runtimeconfig

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestNewBuildsTypedEffectiveConfiguration(t *testing.T) {
	routing := routingpolicy.Default()
	routing.MaxRetryCount = 4

	snapshot, err := New(routing)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if snapshot.Version != Version || snapshot.RoutingPolicy.Version != routingpolicy.Version || snapshot.RoutingPolicy.MaxRetryCount != 4 {
		t.Fatalf("unexpected routing registry: %#v", snapshot)
	}
	if snapshot.BudgetAlerts.Version != BudgetAlertPolicyVersion || snapshot.BudgetAlerts.Scope != "project" ||
		snapshot.BudgetAlerts.BudgetSource != "projects.budget_cents" || snapshot.BudgetAlerts.PeriodSource != "projects.budget_period" ||
		snapshot.BudgetAlerts.EventLedger != "notification_events" || snapshot.BudgetAlerts.DeliveryRouteSource != "notification_webhooks" ||
		snapshot.BudgetAlerts.LegacyGlobalSettingsActive {
		t.Fatalf("unexpected budget-alert registry: %#v", snapshot.BudgetAlerts)
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, forbidden := range []string{"https://", "secret", "credential", "password"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("effective configuration exposed forbidden material %q: %s", forbidden, encoded)
		}
	}
}

func TestNewRejectsInvalidRoutingPolicy(t *testing.T) {
	routing := routingpolicy.Default()
	routing.MaxRetryCount = 11
	if _, err := New(routing); err == nil {
		t.Fatal("expected invalid routing policy to fail")
	}
}
