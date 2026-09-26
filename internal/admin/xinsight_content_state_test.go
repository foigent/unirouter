package admin

import (
	"strconv"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/sourcefacts"
)

func TestContentLifecycleIsolationAndReplays(t *testing.T) {
	a := &contentControlKey{CredentialID: "42", RegistrationHash: "sha256:" + strings.Repeat("a", 64), CoverageFrom: sourcefacts.ContentBoundary{Time: "2026-09-15T00:00:00.000000Z", AfterSequence: "100"}, AllowedPurposes: []string{"human_review"}, ips: "127.0.0.1/32"}
	b := &contentControlKey{CredentialID: "43", RegistrationHash: "sha256:" + strings.Repeat("b", 64)}
	s := contentControlState{global: "active", local: "active", version: 5, generation: 3, safetyVersion: 2, active: a, staged: b}
	req := contentControlRequest{ExpectedStateVersion: "5", CredentialID: b.CredentialID, RegistrationHash: b.RegistrationHash}
	next, err := nextContentControlState(s, "cancel", req)
	if err != nil || next.active != a || next.staged != nil || next.generation != s.generation || next.version != 6 {
		t.Fatalf("cancel affected active authority: %+v %v", next, err)
	}
	if !contentReplayApplicable("same", "same", 6, 2, next) {
		t.Fatal("consumed original CAS must allow exact applicable replay")
	}
	if _, err = nextContentControlState(next, "cancel", req); err == nil {
		t.Fatal("adapter must not redo CAS for a replay")
	}
	if contentReplayApplicable("other", "same", 6, 2, next) || contentReplayApplicable("same", "same", 5, 2, next) || contentReplayApplicable("same", "same", 6, 1, next) {
		t.Fatal("stale replay accepted")
	}
	key := contentRegistrationBinding("rotate", s, sourcefacts.ContentBoundary{Time: "2026-09-15T01:00:00Z", AfterSequence: "0"}, "192.0.2.1/32")
	if key.CoverageFrom != a.CoverageFrom || key.ips != a.ips || len(key.AllowedPurposes) != 1 || key.AllowedPurposes[0] != "human_review" {
		t.Fatal("rotate expanded or changed existing authority")
	}
	key.AllowedPurposes[0] = "changed"
	if a.AllowedPurposes[0] != "human_review" {
		t.Fatal("rotation aliases previous grant")
	}
	for _, action := range []string{"pause", "resume", "revoke"} {
		r := contentControlRequest{ExpectedStateVersion: strconv.FormatInt(s.version, 10), CredentialID: a.CredentialID, RegistrationHash: b.RegistrationHash}
		if _, err = nextContentControlState(s, action, r); err == nil {
			t.Fatalf("%s accepted wrong exact binding", action)
		}
	}
}

func TestContentGlobalAndLocalStops(t *testing.T) {
	a := &contentControlKey{CredentialID: "42", RegistrationHash: "sha256:" + strings.Repeat("a", 64), CoverageFrom: sourcefacts.ContentBoundary{Time: "2026-09-15T00:00:00.000000Z", AfterSequence: "0"}, AllowedPurposes: []string{"human_review"}}
	s := contentControlState{global: "paused", local: "paused", version: 4, generation: 2, active: a}
	r := contentControlRequest{ExpectedStateVersion: "4", CredentialID: a.CredentialID, RegistrationHash: a.RegistrationHash}
	next, err := nextContentControlState(s, "resume", r)
	if err != nil {
		t.Fatal(err)
	}
	if next.global != "paused" || contentAccessProjection(next).Enabled {
		t.Fatal("local resume bypassed global pause")
	}
	if got := contentAccessProjection(next); got.AllowedPurposes == nil || len(got.AllowedPurposes) != 0 || got.Validate() != nil {
		t.Fatal("disabled projection is not exact C0")
	}
	s.local = "revoked"
	if _, err = nextContentControlState(s, "resume", r); err == nil {
		t.Fatal("resume resurrected revoked authority")
	}
	s.global = "revoked"
	s.active = nil
	r.CredentialID = ""
	r.RegistrationHash = ""
	if _, err = nextContentControlState(s, "prepare", r); err == nil {
		t.Fatal("prepare cleared terminal global revoke")
	}
	s.global = "active"
	next, err = nextContentControlState(s, "prepare", r)
	if err != nil || next.local != "paused" {
		t.Fatal("explicit local re-registration unavailable", err)
	}
}
