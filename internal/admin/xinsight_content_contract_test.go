package admin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

func TestContentRegistrationTargetMismatchBeforeSQL(t *testing.T) {
	controller, err := sourcefacts.New(nil, sourcefacts.Options{SourceInstance: "synthetic-u1"})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{sourceFactsController: controller}
	r := sourcefacts.Registration{SchemaVersion: sourcefacts.RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat, VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: "ur-xf1-0000000000001", VerifierSHA256: strings.Repeat("1", 64), OperationID: strings.Repeat("2", 32), SourceInstance: "synthetic-u1", OrgID: "1", ProjectID: "2", Profiles: []string{"unirouter.interaction-content.v1"}}
	for _, field := range []string{"org", "project", "source", "operation"} {
		t.Run(field, func(t *testing.T) {
			copy := r
			switch field {
			case "org":
				copy.OrgID = "9"
			case "project":
				copy.ProjectID = "9"
			case "source":
				copy.SourceInstance = "other-source"
			case "operation":
				copy.OperationID = strings.Repeat("3", 32)
			}
			request := contentControlRequest{OperationID: r.OperationID, ExpectedStateVersion: "0", Registration: &copy, AllowedIP: "127.0.0.1/32"}
			// A nil transaction makes any attempted SQL a failing test; the helper
			// must reject the mismatched target before attempting a read or write.
			if _, err := h.insertContentStagedTx(context.Background(), nil, 1, 2, "prepare", request, contentControlState{}); err == nil {
				t.Fatal("target mismatch accepted")
			}
		})
	}
}

func TestContentControlStrictFields(t *testing.T) {
	valid := `{"operation_id":"11111111111111111111111111111111","expected_state_version":"2","credential_id":"42","registration_hash":"sha256:` + strings.Repeat("a", 64) + `"}`
	if _, err := decodeContentControl([]byte(valid), "activate"); err != nil {
		t.Fatal(err)
	}
	for name, wire := range map[string]string{
		"case_alias":      strings.Replace(valid, "operation_id", "Operation_ID", 1),
		"duplicate":       strings.Replace(valid, `"expected_state_version":"2"`, `"expected_state_version":"2","expected_state_version":"2"`, 1),
		"null":            strings.Replace(valid, `"credential_id":"42"`, `"credential_id":null`, 1),
		"noncanonical_id": strings.Replace(valid, `"42"`, `"042"`, 1),
		"bare_hash":       strings.Replace(valid, "sha256:", "", 1),
		"trailing":        valid + `{}`,
		"missing_hash":    `{"operation_id":"11111111111111111111111111111111","expected_state_version":"2","credential_id":"42"}`,
		"unexpected_ip":   strings.TrimSuffix(valid, "}") + `,"allowed_ip":"127.0.0.1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeContentControl([]byte(wire), "activate"); err == nil {
				t.Fatal("accepted invalid control request")
			}
		})
	}
}

func TestContentRegistrationDoesNotBroadenLegacy(t *testing.T) {
	r := sourcefacts.Registration{SchemaVersion: sourcefacts.RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat, VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: "ur-xf1-0000000000001", VerifierSHA256: strings.Repeat("1", 64), OperationID: strings.Repeat("2", 32), SourceInstance: "synthetic-u1", OrgID: "1", ProjectID: "2", Profiles: []string{"unirouter.interaction-content.v1"}}
	if r.Validate() == nil {
		t.Fatal("legacy registration accepted C")
	}
	if _, err := sourcefacts.HashContentRegistration(r); err != nil {
		t.Fatal(err)
	}
	request := contentControlRequest{OperationID: r.OperationID, ExpectedStateVersion: "0", Registration: &r, AllowedIP: "127.0.0.1/32"}
	wire, _ := json.Marshal(request)
	if _, err := decodeContentControl(wire, "prepare"); err != nil {
		t.Fatal(err)
	}
	r.Profiles = append(r.Profiles, "unirouter.execution-identity.v1")
	wire, _ = json.Marshal(request)
	if _, err := decodeContentControl(wire, "prepare"); err == nil {
		t.Fatal("mixed-scope registration accepted")
	}
}
