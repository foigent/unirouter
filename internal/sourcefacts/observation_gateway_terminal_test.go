package sourcefacts

import (
	"bytes"
	"encoding/json"
	"testing"

	contract "github.com/uniroute/unirouter/contracts/source-facts/pre-dispatch-v2"
	v1contract "github.com/uniroute/unirouter/contracts/source-facts/v2"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

func gatewayTerminalFixture() GatewayPreDispatchTerminal {
	at := "2026-09-25T08:20:41.939743Z"
	d := GatewayPreDispatchTerminal{SchemaVersion: contract.SchemaVersion, SourceInstance: "synthetic-ur", OrgID: "2", ProjectID: "2", RequestID: "op_synthetic1", Profile: GatewayPreDispatchProfile, SubjectKind: "request", SubjectID: "op_synthetic1", Kind: GatewayPreDispatchKind, Revision: "1", OccurredAt: at, RecordedAt: at, Coverage: "observed", ContextRef: sourceID(Version, "synthetic-ur", "2", "2", ContextProfile, "op_synthetic1", "authenticated"), CaptureExpected: []string{GatewayPreDispatchKind}, Runtime: GatewayPreDispatchRuntime{Operation: "openai.chat_completions.v1", StreamMode: "not_observed", Admission: "authenticated", Provider: "not_started", LocalDelivery: "not_started", Financial: "not_created", Capture: "observed", TerminalOrigin: "gateway_pre_dispatch", FailureCode: "routing.no_route", HTTPStatus: "503"}}
	d.SourceFactID, d.EventID = d.factID(), d.eventID()
	return d
}

func TestGatewayPreDispatchTerminalV2IndependentFromV1(t *testing.T) {
	d := gatewayTerminalFixture()
	wire, err := EncodeGatewayPreDispatchTerminal(d)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeGatewayPreDispatchTerminal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.EventID != d.EventID || decoded.SourceFactID != d.SourceFactID {
		t.Fatal("identity drift")
	}
	if _, err := DecodeObservation(wire); err == nil {
		t.Fatal("v1 decoder accepted v2")
	}
	v1Descriptors, err := observationDescriptors()
	if err != nil {
		t.Fatal(err)
	}
	if len(v1Descriptors) != 5 {
		t.Fatal("v1 descriptor registry changed")
	}
	if _, exists := v1Descriptors[GatewayPreDispatchProfile]; exists {
		t.Fatal("v1 descriptor registry admitted v2")
	}
	if eventconnector.SHA256Reference(v1contract.Manifest()) != "sha256:b32c57bd868a109865eb96f3cc38f84400cda0e9513ae6fc2a498e38db808254" ||
		eventconnector.SHA256Reference(v1contract.Schema()) != "sha256:ca0aabfb0c7b40c00cfb910e6bd255691326883b98b38b174afc86f2c4df2260" ||
		eventconnector.SHA256Reference(v1contract.Vectors()) != "sha256:e63eaeab3ba4fb8145d3dfb502f71393d1e603a256d505540645b5949dd48b51" {
		t.Fatal("v1 contract bytes changed")
	}
	if _, err := GatewayPreDispatchEvent(d, wire); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(wire, []byte(`"financial":"not_created"`)) {
		t.Fatal("missing nonfinancial state")
	}
	for _, mutate := range []func(*GatewayPreDispatchTerminal){
		func(x *GatewayPreDispatchTerminal) { x.Runtime.Provider = "rejected" },
		func(x *GatewayPreDispatchTerminal) { x.Runtime.LocalDelivery = "write_failed" },
		func(x *GatewayPreDispatchTerminal) { x.Runtime.Financial = "settled" },
		func(x *GatewayPreDispatchTerminal) { x.Runtime.FailureCode = "provider_http_error" },
		func(x *GatewayPreDispatchTerminal) { x.ContextRef = "uf1_" + string(bytes.Repeat([]byte("0"), 64)) },
	} {
		x := d
		mutate(&x)
		if _, err := EncodeGatewayPreDispatchTerminal(x); err == nil {
			t.Fatalf("accepted mutated terminal: %#v", x.Runtime)
		}
	}
	if eventconnector.HashSourceBytes(wire) == "" {
		t.Fatal("missing source hash")
	}
	var vectors struct {
		Valid []struct {
			Canonical string `json:"canonical_utf8"`
			Hash      string `json:"source_sha256"`
		} `json:"valid"`
		Invalid []struct {
			Canonical string `json:"canonical_utf8"`
		} `json:"invalid"`
	}
	if err := json.Unmarshal(contract.Vectors(), &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors.Valid) != 1 || len(vectors.Invalid) != 4 || string(wire) != vectors.Valid[0].Canonical || eventconnector.HashSourceBytes(wire) != vectors.Valid[0].Hash {
		t.Fatal("encoder differs from independent golden source bytes or identity")
	}
	for _, invalid := range vectors.Invalid {
		if _, err := DecodeGatewayPreDispatchTerminal([]byte(invalid.Canonical)); err == nil {
			t.Fatal("accepted independent negative vector")
		}
	}
}
