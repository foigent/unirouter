package main

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
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

func TestCompatibleExpandDormantManifestRequiresCompleteTargetContract(t *testing.T) {
	for _, target := range []string{schemaauthority.ObservationExpandHead, schemaauthority.BudgetSnapshotExpandHead, schemaauthority.ContentCaptureExpandHead, schemaauthority.ContentReclaimExpandHead, schemaauthority.PreDispatchExpandHead, schemaauthority.ScopedContentExpandHead, schemaauthority.XInsightPolicyExpandHead, schemaauthority.XInsightAdoptionExpandHead} {
		t.Run(target, func(t *testing.T) {
			full := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
			wire, err := full.CanonicalBytes()
			if err != nil {
				t.Fatal(err)
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(wire))
			if err := verifyDormantExpansionManifest(wire, digest, target); err != nil {
				t.Fatal(err)
			}
			invalid := []struct {
				name   string
				wire   []byte
				digest string
			}{
				{"missing", nil, ""}, {"malformed", []byte("{"), fmt.Sprintf("%x", sha256.Sum256([]byte("{")))},
				{"digest_mismatch", wire, strings.Repeat("0", 64)}, {"noncanonical", append([]byte(" "), wire...), fmt.Sprintf("%x", sha256.Sum256(append([]byte(" "), wire...)))},
			}
			for _, test := range invalid {
				t.Run(test.name, func(t *testing.T) {
					if verifyDormantExpansionManifest(test.wire, test.digest, target) == nil {
						t.Fatal("accepted invalid immutable manifest")
					}
				})
			}
			refs := []modelprofile.ImplementationReference{{Kind: "stream_counter", ID: metering.GatewayCounterEstimateVersionV2}, {Kind: "stream_counter", ID: metering.GatewayCounterEstimateVersionV3}, {Kind: "source_facts", ID: "unirouter.source-fact.v1"}, {Kind: "source_observations", ID: "unirouter.source-observation.v1"}, {Kind: "source_dispatch", ID: "unirouter.source-dispatch-outbox.v1"}}
			if target == schemaauthority.BudgetSnapshotExpandHead || target == schemaauthority.ContentCaptureExpandHead || target == schemaauthority.ContentReclaimExpandHead || target == schemaauthority.PreDispatchExpandHead || target == schemaauthority.ScopedContentExpandHead || (target == schemaauthority.XInsightPolicyExpandHead || target == schemaauthority.XInsightAdoptionExpandHead) {
				refs = append(refs, runtimegeneration.BudgetCoverageRequirements()...)
			}
			if target == schemaauthority.ContentCaptureExpandHead || target == schemaauthority.ContentReclaimExpandHead || target == schemaauthority.PreDispatchExpandHead || target == schemaauthority.ScopedContentExpandHead || (target == schemaauthority.XInsightPolicyExpandHead || target == schemaauthority.XInsightAdoptionExpandHead) {
				refs = append(refs, runtimegeneration.ContentLifecycleRequirements()...)
			}
			if target == schemaauthority.PreDispatchExpandHead || target == schemaauthority.ScopedContentExpandHead || (target == schemaauthority.XInsightPolicyExpandHead || target == schemaauthority.XInsightAdoptionExpandHead) {
				refs = append(refs, modelprofile.ImplementationReference{Kind: "source_observations", ID: "unirouter.source-observation.v2"})
			}
			if target == schemaauthority.ScopedContentExpandHead || (target == schemaauthority.XInsightPolicyExpandHead || target == schemaauthority.XInsightAdoptionExpandHead) {
				refs = append(refs, runtimegeneration.ScopedContentRequirements()...)
			}
			if target == schemaauthority.XInsightPolicyExpandHead || target == schemaauthority.XInsightAdoptionExpandHead {
				refs = append(refs, runtimegeneration.XInsightPolicyRequirements()...)
			}
			if target == schemaauthority.XInsightAdoptionExpandHead {
				refs = append(refs, runtimegeneration.XInsightAdoptionRequirements()...)
			}
			for _, missing := range refs {
				t.Run("missing_"+missing.Kind+"_"+missing.ID, func(t *testing.T) {
					old := full
					old.Implementations = slices.DeleteFunc(slices.Clone(full.Implementations), func(ref modelprofile.ImplementationReference) bool { return ref == missing })
					wire, err := old.CanonicalBytes()
					if err != nil {
						t.Fatal(err)
					}
					if verifyDormantExpansionManifest(wire, fmt.Sprintf("%x", sha256.Sum256(wire)), target) == nil {
						t.Fatal("accepted missing target implementation")
					}
				})
			}
			old := full
			old.SchemaHeads = slices.DeleteFunc(slices.Clone(full.SchemaHeads), func(head string) bool { return head == target })
			wire, err = old.CanonicalBytes()
			if err != nil {
				t.Fatal(err)
			}
			if verifyDormantExpansionManifest(wire, fmt.Sprintf("%x", sha256.Sum256(wire)), target) == nil {
				t.Fatal("accepted unsupported schema head")
			}
		})
	}
}
