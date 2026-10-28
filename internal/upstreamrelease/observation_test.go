package upstreamrelease

import (
	"encoding/json"
	"testing"
	"time"
)

func TestObservationEpochRecheckAndScope(t *testing.T) {
	for _, provider := range []string{"provider-a", "provider-b"} {
		scope := Scope{CustomerOrgID: 2, DeploymentID: 3, ConnectionID: 4, Provider: provider, BaseURL: "https://api.example.test/v1", APIModel: "rolling-alias"}
		now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
		in := Input{ReleaseLabel: "release-A", AnnouncedOn: "2026-10-17", SourceURL: "https://example.test/releases/a"}
		a, err := Next(scope, State{}, in, 1, now)
		if err != nil {
			t.Fatal(err)
		}
		in.ExpectedRevision, in.ExpectedIdentity = a.Revision, a.Observation.Identity
		in.SourceRevision = "checked-again"
		a2, err := Next(scope, a, in, 1, now.Add(time.Hour))
		if err != nil || a2.Observation.Identity != a.Observation.Identity || a2.Revision != 2 || !Matches(a.Observation, a2.Observation) {
			t.Fatalf("recheck invalidated epoch: %v", err)
		}
		in.ExpectedRevision = a2.Revision
		in.ReleaseLabel = "release-B"
		b, err := Next(scope, a2, in, 1, now.Add(2*time.Hour))
		if err != nil || b.Observation.Identity == a.Observation.Identity {
			t.Fatalf("changed release retained epoch: %v", err)
		}
		in.ExpectedRevision, in.ExpectedIdentity, in.ReleaseLabel = b.Revision, b.Observation.Identity, "release-A"
		a3, err := Next(scope, b, in, 1, now.Add(3*time.Hour))
		if err != nil || a3.Observation.Identity == a.Observation.Identity {
			t.Fatalf("ABA revived old proof: %v", err)
		}
		if Require(a2.Observation, a.Observation, a.Observation.Identity) != nil {
			t.Fatal("recheck should preserve proof")
		}
		if Require(b.Observation, a.Observation, b.Observation.Identity) == nil {
			t.Fatal("old proof accepted for new release")
		}
		if Assess(b.Observation, nil).State != "stale" || Assess(nil, nil).State != "unknown" {
			t.Fatal("unknown was misrepresented")
		}
		wire, _ := json.Marshal(a.Observation)
		decoded, err := Decode(wire)
		if err != nil || !Matches(a.Observation, decoded) {
			t.Fatal(err)
		}
	}
}

func TestObservationRejectsInvalidAndStaleWrites(t *testing.T) {
	scope := Scope{2, 3, 4, "generic", "https://api.example.test", "alias"}
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	valid := Input{ReleaseLabel: "release-A", SourceURL: "https://example.test/release"}
	for _, mutate := range []func(*Input){
		func(i *Input) { i.ReleaseLabel = "" }, func(i *Input) { i.ReleaseLabel = " x" },
		func(i *Input) { i.AnnouncedOn = "2026-02-31" }, func(i *Input) { i.SourceURL = "http://example.test" },
		func(i *Input) { i.SourceURL = "https://user:secret@example.test" }, func(i *Input) { i.SourceURL = "https://example.test/?token=x" },
		func(i *Input) { i.SourceSHA256 = "not-a-digest" }, func(i *Input) { i.ExpectedRevision = 1 },
	} {
		in := valid
		mutate(&in)
		if _, err := Next(scope, State{}, in, 1, now); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	state, err := Next(scope, State{}, valid, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Next(scope, state, valid, 1, now); err != ErrConflict {
		t.Fatal("stale CAS accepted")
	}
	valid.ExpectedRevision = state.Revision
	valid.ExpectedIdentity = state.Observation.Identity
	valid.ReleaseLabel = ""
	if _, err := Next(scope, state, valid, 1, now); err == nil {
		t.Fatal("known observation cleared")
	}
	for _, raw := range []string{`{"identity":"one","identity":"two"}`, `{"unknown":true}`, `[]`} {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Fatal("invalid stored object accepted")
		}
	}
}

func TestObservationRejectsCaseAliasesAndChecksLiveEndpoint(t *testing.T) {
	scope := Scope{2, 3, 4, "generic", "https://api.example.test", "alias"}
	state, err := Next(scope, State{}, Input{ReleaseLabel: "A", SourceURL: "https://provider.example.test/release"}, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(state.Observation)
	var value map[string]any
	if err := json.Unmarshal(wire, &value); err != nil {
		t.Fatal(err)
	}
	value["Identity"] = value["identity"]
	changed, _ := json.Marshal(value)
	if _, err := Decode(changed); err == nil {
		t.Fatal("case alias accepted")
	}
	delete(value, "Identity")
	claim := value["claim"].(map[string]any)
	claim["Release_Label"] = "B"
	changed, _ = json.Marshal(value)
	if _, err := Decode(changed); err == nil {
		t.Fatal("nested case alias accepted")
	}
	other := scope
	other.BaseURL = "https://replacement.example.test"
	if result := AssessInScope(other, state.Observation, state.Observation); result.State != "stale" || result.Reason != "upstream_endpoint_scope_changed" {
		t.Fatalf("stale endpoint relabeled: %+v", result)
	}
}
