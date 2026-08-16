package gateway

import (
	"errors"
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

func TestApplyRuntimeAssignmentClassifiesMissingPinnedChannelAsStale(t *testing.T) {
	_, err := applyRuntimeAssignment(snapshotLoad{Snapshot: &routeSnapshot{}}, routingpolicy.RuntimeAssignment{Overrides: []routingpolicy.ChannelOverride{{
		ChannelID: 9, ExpectedRoutingEpoch: 4, Admission: routingpolicy.AdmissionAllow,
	}}})
	if !errors.Is(err, ErrRuntimeAssignmentStale) {
		t.Fatalf("missing pinned channel error = %v, want ErrRuntimeAssignmentStale", err)
	}
}

func TestApplyRuntimeAssignmentTreatsMissingDenyAsFailClosedNoOp(t *testing.T) {
	original := &routeSnapshot{Model: "company-fast"}
	load, err := applyRuntimeAssignment(snapshotLoad{Snapshot: original}, routingpolicy.RuntimeAssignment{Overrides: []routingpolicy.ChannelOverride{{
		ChannelID: 9, ExpectedRoutingEpoch: 4, Admission: routingpolicy.AdmissionDeny,
	}}})
	if err != nil {
		t.Fatalf("missing deny override must remain a safe no-op: %v", err)
	}
	if len(load.Snapshot.Channels) != 0 {
		t.Fatalf("missing deny override invented a route channel: %#v", load.Snapshot.Channels)
	}
}

func TestGovernedSnapshotOverridesAllowsOnlyExplicitDriftRecovery(t *testing.T) {
	load := snapshotLoad{Snapshot: &routeSnapshot{Channels: []snapshotChannel{{
		Channel: models.Channel{ID: 2, Status: "staged", RoutingEpoch: 7},
	}}}}
	baseline, candidate := governedSnapshotOverrides(load,
		[]routingpolicy.ChannelOverride{{ChannelID: 1, ExpectedRoutingEpoch: 3, Admission: routingpolicy.AdmissionAllow}},
		[]routingpolicy.ChannelOverride{
			{ChannelID: 1, ExpectedRoutingEpoch: 4, Admission: routingpolicy.AdmissionDeny},
			{ChannelID: 2, ExpectedRoutingEpoch: 7, Admission: routingpolicy.AdmissionAllow},
		},
	)
	if len(baseline) != 0 {
		t.Fatalf("drifted baseline override was not removed: %#v", baseline)
	}
	if len(candidate) != 1 || candidate[0].ChannelID != 2 || candidate[0].Admission != routingpolicy.AdmissionAllow {
		t.Fatalf("recovery candidate = %#v, want only the available allow override", candidate)
	}

	strictBaseline, strictCandidate := governedSnapshotOverrides(load,
		[]routingpolicy.ChannelOverride{{ChannelID: 1, ExpectedRoutingEpoch: 3, Admission: routingpolicy.AdmissionAllow}},
		[]routingpolicy.ChannelOverride{{ChannelID: 2, ExpectedRoutingEpoch: 7, Admission: routingpolicy.AdmissionAllow}},
	)
	if len(strictBaseline) != 1 || strictBaseline[0].ChannelID != 1 {
		t.Fatalf("baseline drift without an explicit deny must stay strict: %#v", strictBaseline)
	}
	if len(strictCandidate) != 1 || strictCandidate[0].ChannelID != 2 {
		t.Fatalf("ordinary candidate changed unexpectedly: %#v", strictCandidate)
	}
}

func TestGovernedSnapshotOverridesKeepsValidBaselineForOrdinaryChange(t *testing.T) {
	load := snapshotLoad{Snapshot: &routeSnapshot{Channels: []snapshotChannel{{
		Channel: models.Channel{ID: 1, Status: "staged", RoutingEpoch: 3},
	}}}}
	baseline, candidate := governedSnapshotOverrides(load,
		[]routingpolicy.ChannelOverride{{ChannelID: 1, ExpectedRoutingEpoch: 3, Admission: routingpolicy.AdmissionAllow}},
		[]routingpolicy.ChannelOverride{{ChannelID: 1, ExpectedRoutingEpoch: 3, Admission: routingpolicy.AdmissionDeny}},
	)
	if len(baseline) != 1 || len(candidate) != 1 {
		t.Fatalf("valid baseline and candidate must both remain simulated: baseline=%#v candidate=%#v", baseline, candidate)
	}
}
