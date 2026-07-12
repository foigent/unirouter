package runtimeconfig

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/streampolicy"
)

func TestNewBuildsTypedEffectiveConfiguration(t *testing.T) {
	routing := routingpolicy.Default()
	routing.MaxRetryCount = 4
	stream := streampolicy.Default()
	stream.MaxSSELineBytes = 4 * 1024 * 1024

	snapshot, err := New(routing, stream)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if snapshot.Version != Version || snapshot.RoutingPolicy.Version != routingpolicy.Version || snapshot.RoutingPolicy.MaxRetryCount != 4 {
		t.Fatalf("unexpected routing registry: %#v", snapshot)
	}
	if snapshot.RoutingPolicy.SelectionVersion != routingpolicy.SelectionVersion ||
		snapshot.RoutingPolicy.SnapshotFailureMode != "bounded_last_known_good_then_fail_closed" ||
		snapshot.RoutingPolicy.ProbeOwnership != "postgresql_epoch_cas_single_lease" {
		t.Fatalf("unexpected routing selection contract: %#v", snapshot.RoutingPolicy)
	}
	if snapshot.StreamTransport.Version != streampolicy.Version || snapshot.StreamTransport.Source != "startup_environment" ||
		snapshot.StreamTransport.MaxSSELineBytes != 4*1024*1024 || snapshot.StreamTransport.WriteIdleTimeoutSeconds != 30 {
		t.Fatalf("unexpected stream transport registry: %#v", snapshot.StreamTransport)
	}
	if snapshot.AgentCapability.Version != "unirouter.agent-capability.v1" || snapshot.AgentCapability.TokenTTLSeconds != 300 ||
		snapshot.AgentCapability.RefreshBaseSeconds != 180 || snapshot.AgentCapability.ActiveRevocationSLOSeconds > 30 ||
		snapshot.AgentCapability.AuthorityScope != "model_gateway_only" || snapshot.AgentCapability.RedisOutageMode != "fail_closed" ||
		snapshot.AgentCapability.RuntimeBudgetMode != "reserve_estimate_on_every_ancestor_settle_actual" ||
		snapshot.AgentCapability.RuntimeRateMode != "exact_redis_sliding_window_on_every_ancestor" ||
		snapshot.AgentCapability.RuntimeConcurrencyMode != "expiring_redis_lease_on_every_ancestor" ||
		snapshot.AgentCapability.MaxInvocationLeaseSeconds != 86400 || snapshot.AgentCapability.FinalizationGraceSeconds != 300 {
		t.Fatalf("unexpected Agent capability registry: %#v", snapshot.AgentCapability)
	}
	if snapshot.BudgetAlerts.Version != BudgetAlertPolicyVersion || snapshot.BudgetAlerts.Scope != "project" ||
		snapshot.BudgetAlerts.BudgetSource != "projects.budget_cents" || snapshot.BudgetAlerts.PeriodSource != "projects.budget_period" ||
		snapshot.BudgetAlerts.EventLedger != "notification_events" || snapshot.BudgetAlerts.DeliveryRouteSource != "notification_webhooks" ||
		snapshot.BudgetAlerts.LegacyGlobalSettingsActive {
		t.Fatalf("unexpected budget-alert registry: %#v", snapshot.BudgetAlerts)
	}
	if snapshot.InternalOutbox.Version != "internal-outbox.v1" || snapshot.InternalOutbox.Backend != "postgresql_internal" ||
		snapshot.InternalOutbox.WorkerCount != 2 || snapshot.InternalOutbox.BatchSize != 25 ||
		snapshot.InternalOutbox.TraceLagSLOSeconds != 30 || snapshot.InternalOutbox.ExternalExporterEnabled {
		t.Fatalf("unexpected internal-outbox registry: %#v", snapshot.InternalOutbox)
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
	if _, err := New(routing, streampolicy.Default()); err == nil {
		t.Fatal("expected invalid routing policy to fail")
	}
}

func TestNewRejectsInvalidStreamTransportPolicy(t *testing.T) {
	stream := streampolicy.Default()
	stream.HeartbeatSeconds = stream.WriteIdleTimeoutSeconds
	if _, err := New(routingpolicy.Default(), stream); err == nil {
		t.Fatal("expected invalid stream transport policy to fail")
	}
}
