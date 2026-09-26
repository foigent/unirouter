package admin

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

func TestContentControlResponseWire(t *testing.T) {
	active := &contentControlKey{CredentialID: "42", RegistrationHash: "sha256:" + strings.Repeat("a", 64), CoverageFrom: sourcefacts.ContentBoundary{Time: "2026-09-15T00:00:00.000000Z", AfterSequence: "100"}, AllowedPurposes: []string{"human_review"}, ips: "127.0.0.1/32"}
	staged := &contentControlKey{CredentialID: "43", RegistrationHash: "sha256:" + strings.Repeat("b", 64), CoverageFrom: active.CoverageFrom, AllowedPurposes: []string{"human_review"}, ips: active.ips}
	cases := map[string]contentControlState{
		"not_configured": {global: "not_configured", local: "not_configured"},
		"staged":         {global: "paused", local: "paused", version: 1, generation: 1, staged: staged},
		"active":         {global: "active", local: "active", version: 2, generation: 2, active: active},
		"local_paused":   {global: "active", local: "paused", version: 3, generation: 3, active: active},
		"global_paused":  {global: "paused", local: "active", version: 2, generation: 2, active: active},
		"local_revoked":  {global: "active", local: "revoked", version: 4, generation: 4},
		"global_revoked": {global: "revoked", local: "revoked", version: 4, generation: 4},
	}
	wires := map[string]json.RawMessage{}
	for name, state := range cases {
		t.Run(name, func(t *testing.T) {
			view := contentView("synthetic-u1-local", 1, 2, state)
			wire, err := json.Marshal(view)
			if err != nil {
				t.Fatal(err)
			}
			if strictErr := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); strictErr != nil {
				t.Fatal(strictErr)
			}
			if strings.Contains(string(wire), "127.0.0.1") {
				t.Fatal("internal IP leaked into control response")
			}
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(wire, &fields); err != nil {
				t.Fatal(err)
			}
			_, hasActive := fields["active"]
			_, hasStaged := fields["staged"]
			_, hasAccess := fields["content_access"]
			if hasActive != (state.active != nil) || hasStaged != (state.staged != nil) || hasAccess != (state.active != nil || state.staged != nil) {
				t.Fatal("absent keys/access must be omitted, not null")
			}
			for _, keyName := range []string{"active", "staged"} {
				if value, ok := fields[keyName]; ok {
					var ref map[string]json.RawMessage
					if err = json.Unmarshal(value, &ref); err != nil {
						t.Fatal(err)
					}
					if len(ref) != 2 || ref["credential_id"] == nil || ref["registration_hash"] == nil {
						t.Fatal("public credential reference gained fields")
					}
				}
			}
			if view.ContentAccess != nil {
				if err = view.ContentAccess.Validate(); err != nil {
					t.Fatal(err)
				}
				if view.ContentAccess.Enabled != (name == "active") {
					t.Fatal("paused authority advertised as enabled")
				}
				if view.ContentAccess.CoverageFrom.AfterSequence != "100" {
					t.Fatal("authorization coverage changed")
				}
			}
			wires[name] = wire
		})
	}
	if t.Failed() {
		return
	}
	// Optional one-off fixture output, never a runtime credential or acceptance.
	if path := os.Getenv("U1_CONTENT_CONTROL_WIRE_OUTPUT"); path != "" {
		artifact := map[string]any{"status": "synthetic_response_shapes_not_product_acceptance", "get": wires,
			"meaning": "active/staged contain only credential_id/hash; content_access describes current active or staged-only binding, staged-only is always disabled; enabled requires feed_active plus global/local active; disabled purposes is []; revoked keys omitted",
			"post_examples": map[string]any{
				"prepare":               map[string]any{"operation_id": strings.Repeat("1", 32), "action": "prepare", "state_version": "1", "content_scope_revision": "1", "credential_id": "43", "registration_hash": staged.RegistrationHash, "coverage_from": staged.CoverageFrom, "allowed_purposes": staged.AllowedPurposes},
				"activate":              map[string]any{"operation_id": strings.Repeat("2", 32), "action": "activate", "state_version": "2", "content_scope_revision": "2", "credential_id": "43", "registration_hash": staged.RegistrationHash, "coverage_from": staged.CoverageFrom, "allowed_purposes": staged.AllowedPurposes},
				"cancel":                map[string]any{"operation_id": strings.Repeat("3", 32), "action": "cancel", "state_version": "5", "content_scope_revision": "3", "credential_id": "43", "registration_hash": staged.RegistrationHash, "coverage_from": staged.CoverageFrom, "allowed_purposes": staged.AllowedPurposes},
				"revoke_without_active": map[string]any{"operation_id": strings.Repeat("4", 32), "action": "revoke", "state_version": "6", "content_scope_revision": "4"},
			}, "post_boundary": "examples only: transaction coordinator has not been applied or tested; returned IDs must come from actual execution"}
		wire, err := json.MarshalIndent(artifact, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write(append(wire, '\n'))
		closeErr := f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
}
