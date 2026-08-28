package modelrouting

import "testing"

func TestPairedSnapshotCacheKeyVersionsBothAuthoritiesAsOneUnit(t *testing.T) {
	value := canonicalFixture()
	value.Mode = ModeVerify
	baseline := newPairedSnapshotCacheKey(value)
	changes := []func(*snapshot){
		func(item *snapshot) { item.Slot.Version++ },
		func(item *snapshot) { item.Slot.Selected.RouteItemsHash += "x" },
		func(item *snapshot) { item.LegacyRelease.RouteItemsHash += "x" },
		func(item *snapshot) { item.RequestedOperationID = "openai.responses.v1" },
		func(item *snapshot) {
			item.CapabilityRequirements = append(item.CapabilityRequirements, CapabilityRequirement{
				CapabilityID: "vision", Origins: []string{CapabilityOriginExplicitRequest},
			})
		},
	}
	for index, change := range changes {
		changed := canonicalFixture()
		changed.Mode = ModeVerify
		change(&changed)
		if newPairedSnapshotCacheKey(changed) == baseline {
			t.Fatalf("authority change %d was omitted from paired cache key", index)
		}
	}
	value.Legacy[0].Bindings[0].Encrypted = "different-legacy-secret"
	value.Canonical[0].Bindings[0].Encrypted = "different-canonical-secret"
	if newPairedSnapshotCacheKey(value) != baseline {
		t.Fatal("secret material changed paired cache key")
	}
}

func TestPairedSnapshotEntryHashCoversDatabaseClock(t *testing.T) {
	value := canonicalFixture()
	value.Mode = ModeVerify
	first, err := snapshotDigest(value)
	if err != nil {
		t.Fatal(err)
	}
	value.DBNow = value.DBNow.Add(1)
	second, err := snapshotDigest(value)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("paired snapshot hash omitted the database clock")
	}
}
