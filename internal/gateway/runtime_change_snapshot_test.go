package gateway

import (
	"testing"

	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestApplyRuntimeAssignmentIsNarrowAndEmergencyDisableWins(t *testing.T) {
	original := &routeSnapshot{Channels: []snapshotChannel{
		{Channel: models.Channel{ID: 1, Status: "staged", Priority: 100, RoutingWeight: 100, RoutingEpoch: 3}},
		{Channel: models.Channel{ID: 2, Status: "disabled", Priority: 100, RoutingWeight: 100, RoutingEpoch: 7}},
	}}
	priority, weight := 10, 25
	load, err := applyRuntimeAssignment(snapshotLoad{Snapshot: original}, routingpolicy.RuntimeAssignment{Overrides: []routingpolicy.ChannelOverride{
		{ChannelID: 1, ExpectedRoutingEpoch: 3, Admission: routingpolicy.AdmissionAllow, Priority: &priority, Weight: &weight},
		{ChannelID: 2, ExpectedRoutingEpoch: 7, Admission: routingpolicy.AdmissionAllow},
	}})
	if err != nil {
		t.Fatalf("applyRuntimeAssignment() error = %v", err)
	}
	if load.Snapshot.Channels[0].Channel.Status != "active" || load.Snapshot.Channels[0].Channel.Priority != 10 || load.Snapshot.Channels[0].Channel.RoutingWeight != 25 {
		t.Fatalf("staged channel not activated by governed overlay: %#v", load.Snapshot.Channels[0].Channel)
	}
	if load.Snapshot.Channels[1].Channel.Status != "disabled" {
		t.Fatalf("emergency-disabled channel was resurrected: %#v", load.Snapshot.Channels[1].Channel)
	}
	if original.Channels[0].Channel.Status != "staged" || original.Channels[0].Channel.Priority != 100 {
		t.Fatal("cached source snapshot was mutated")
	}
}

func TestApplyRuntimeAssignmentFailsClosedOnEpochDrift(t *testing.T) {
	_, err := applyRuntimeAssignment(snapshotLoad{Snapshot: &routeSnapshot{Channels: []snapshotChannel{{
		Channel: models.Channel{ID: 1, Status: "staged", RoutingEpoch: 4},
	}}}}, routingpolicy.RuntimeAssignment{Overrides: []routingpolicy.ChannelOverride{{
		ChannelID: 1, ExpectedRoutingEpoch: 3, Admission: routingpolicy.AdmissionAllow,
	}}})
	if err == nil {
		t.Fatal("epoch drift must fail closed")
	}
}
