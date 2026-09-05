package main

import (
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

func TestCompatibleExpandRejectsStaleOrUnsupportedReceiver(t *testing.T) {
	valid := httpRuntimeStatus{Instance: "uri_" + strings.Repeat("a", 32), Artifact: "sha256:" + strings.Repeat("b", 64), State: "serving", SchemaLedgerDigest: strings.Repeat("c", 64), SupportManifestDigest: strings.Repeat("d", 64), CompatibilityEpoch: runtimegeneration.CompatibilityEpoch, SupportedSchemaHeads: []string{schemaauthority.LegacyCapabilityHead, schemaauthority.CapabilityExpandHead}}
	if err := checkBridgeRuntime(valid, valid, valid.SchemaLedgerDigest); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*httpRuntimeStatus){
		func(r *httpRuntimeStatus) { r.Instance = "uri_" + strings.Repeat("e", 32) },
		func(r *httpRuntimeStatus) { r.Artifact = "sha256:" + strings.Repeat("f", 64) },
		func(r *httpRuntimeStatus) { r.State = "draining" },
		func(r *httpRuntimeStatus) { r.SupportedSchemaHeads = []string{schemaauthority.LegacyCapabilityHead} },
		func(r *httpRuntimeStatus) { r.SchemaLedgerDigest = strings.Repeat("e", 64) },
		func(r *httpRuntimeStatus) { r.CompatibilityEpoch = "future" },
	} {
		bad := valid
		change(&bad)
		if checkBridgeRuntime(valid, bad, valid.SchemaLedgerDigest) == nil {
			t.Fatalf("accepted drifted receiver: %+v", bad)
		}
	}
}
