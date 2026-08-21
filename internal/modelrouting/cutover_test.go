package modelrouting

import (
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestCutoverVerifierProvesFullPlanAndReturnsRuntimeCompatibleHashes(t *testing.T) {
	value := cutoverFixture()
	legacyHash, canonicalHash, err := verifyCutoverSnapshot(value, routingpolicy.Default())
	if err != nil {
		t.Fatalf("verifyCutoverSnapshot() error = %v", err)
	}
	pattern := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	if legacyHash != canonicalHash || !pattern.MatchString(legacyHash) {
		t.Fatalf("invalid cutover proof hashes: legacy=%q canonical=%q", legacyHash, canonicalHash)
	}
}

func TestLegacyCutoverOperationSetRejectsMissingAndExtraOperations(t *testing.T) {
	for _, operations := range [][]string{
		{"openai.chat_completions.v1"},
		{"openai.chat_completions.v1", "openai.embeddings.v1", "openai.responses.v1"},
	} {
		if err := validateLegacyCutoverOperationSet(operations); !errors.Is(err, ErrParity) {
			t.Fatalf("validateLegacyCutoverOperationSet(%v) error = %v, want parity error", operations, err)
		}
	}
	if err := validateLegacyCutoverOperationSet([]string{
		"openai.chat_completions.v1", "openai.embeddings.v1",
	}); err != nil {
		t.Fatalf("exact legacy operation set error = %v", err)
	}
}

func TestCutoverVerifierRejectsEveryExecutableDriftClass(t *testing.T) {
	tests := []struct {
		name string
		edit func(*cutoverSnapshot)
	}{
		{name: "candidate_set", edit: func(value *cutoverSnapshot) {
			value.Operations[0].Canonical = value.Operations[0].Canonical[:1]
		}},
		{name: "upstream", edit: func(value *cutoverSnapshot) {
			value.Operations[0].Canonical[0].UpstreamModel = "other-upstream"
		}},
		{name: "adapter", edit: func(value *cutoverSnapshot) {
			value.Operations[0].Canonical[0].AdapterConfigDigest = adapterDigest([]byte(`{"different":true}`))
		}},
		{name: "revision", edit: func(value *cutoverSnapshot) {
			value.Operations[0].Canonical[0].ConnectionRevision++
		}},
		{name: "breaker", edit: func(value *cutoverSnapshot) {
			value.Operations[0].Canonical[0].CircuitRevision++
		}},
		{name: "credential", edit: func(value *cutoverSnapshot) {
			value.Operations[0].Canonical[0].Bindings[0].LegacyKeyID++
		}},
		{name: "probe", edit: func(value *cutoverSnapshot) {
			value.Operations[0].Canonical[0].Bindings[0].Probe.Present = false
		}},
		{name: "link_epoch", edit: func(value *cutoverSnapshot) {
			binding := &value.Operations[0].Canonical[0].Bindings[0]
			binding.LinkedAt = binding.Probe.TestedAt.Add(1)
		}},
		{name: "profile", edit: func(value *cutoverSnapshot) {
			value.Operations[0].Canonical[0].ProtocolProfileVersion = "different"
		}},
		{name: "limit", edit: func(value *cutoverSnapshot) {
			value.Operations[0].Canonical[0].DefaultMaxOutput++
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := cutoverFixture()
			test.edit(&value)
			_, _, err := verifyCutoverSnapshot(value, routingpolicy.Default())
			if !errors.Is(err, ErrParity) {
				t.Fatalf("verifyCutoverSnapshot() error = %v, want parity error", err)
			}
		})
	}
}

func TestCutoverProofHashNeverContainsSecretMaterial(t *testing.T) {
	value := cutoverFixture()
	first, _, err := verifyCutoverSnapshot(value, routingpolicy.Default())
	if err != nil {
		t.Fatal(err)
	}
	for index := range value.Legacy {
		value.Legacy[index].Bindings[0].Encrypted = "cipher:rotated"
		value.Operations[0].Canonical[index].Bindings[0].Encrypted = "cipher:rotated"
	}
	second, _, err := verifyCutoverSnapshot(value, routingpolicy.Default())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("secret material changed cutover proof hash: %q != %q", first, second)
	}
}

func TestCutoverVerifierSupportsForwardAndReverseSlotStates(t *testing.T) {
	value := cutoverFixture()
	if _, _, err := verifyCutoverSnapshot(value, routingpolicy.Default()); err != nil {
		t.Fatalf("forward proof error = %v", err)
	}
	value.SlotStableReleaseID = value.DeploymentRelease.ID
	value.SlotPublicModelID = value.PublicModelID
	value.LegacyRelease.Stage = "rolled_back"
	value.DeploymentRelease.Stage = "active"
	if _, _, err := verifyCutoverSnapshot(value, routingpolicy.Default()); err != nil {
		t.Fatalf("reverse proof error = %v", err)
	}
}

func TestCutoverVerifierRejectsMatchedButCurrentlyUnexecutablePlans(t *testing.T) {
	value := cutoverFixture()
	for index := range value.Legacy {
		value.Legacy[index].CircuitState = "open"
		value.Operations[0].Canonical[index].CircuitState = "open"
	}
	_, _, err := verifyCutoverSnapshot(value, routingpolicy.Default())
	if !errors.Is(err, ErrParity) {
		t.Fatalf("all-open cutover error = %v, want parity failure", err)
	}
}

func cutoverFixture() cutoverSnapshot {
	fixture := canonicalFixture()
	legacyRelease := fixture.LegacyRelease
	legacyRelease.Stage = "active"
	deploymentRelease := fixture.Slot.Selected
	deploymentRelease.Stage = "validated"
	document := struct {
		ContractVersion  string                    `json:"contract_version"`
		PublicModelID    int64                     `json:"public_model_id"`
		ModelName        string                    `json:"model_name"`
		DeploymentRoutes []deploymentRouteDocument `json:"deployment_routes"`
	}{ContractVersion: canonicalCandidateVersion, PublicModelID: fixture.PublicModelID, ModelName: fixture.PublicModel}
	for _, item := range fixture.Manifest {
		document.DeploymentRoutes = append(document.DeploymentRoutes, deploymentRouteDocument{
			DeploymentID: item.DeploymentID, ModelOperationID: item.ModelOperationID,
			ExpectedConnectionRevision: item.ExpectedConnectionRevision,
			ExpectedDeploymentRevision: item.ExpectedDeploymentRevision,
			ExpectedOperationRevision:  item.ExpectedOperationRevision,
			ProtocolProfileID:          item.ProtocolProfileID, ProtocolProfileVersion: item.ProtocolProfileVersion,
			Admission: item.Admission, Priority: item.Priority, Weight: item.Weight,
		})
	}
	deploymentRelease.CandidateJSON, _ = json.Marshal(document)
	deploymentRelease.CandidateHash = digestBytes(deploymentRelease.CandidateJSON)
	return cutoverSnapshot{
		DBNow: fixture.DBNow, CustomerOrgID: fixture.CustomerOrgID,
		PublicModelID: fixture.PublicModelID, PublicModel: fixture.PublicModel,
		PublicModelLifecycle: fixture.PublicModelLifecycle,
		PublicContextLimit:   fixture.PublicContextLimit, PublicOutputLimit: fixture.PublicOutputLimit,
		SlotStableReleaseID: legacyRelease.ID, SlotVersion: fixture.Slot.Version,
		LegacyRelease: legacyRelease, LegacyManifest: fixture.LegacyManifest,
		LegacyOverrides: fixture.LegacyOverrides, Legacy: fixture.Legacy,
		DeploymentRelease: deploymentRelease, DeploymentManifest: fixture.Manifest,
		Operations: []cutoverOperationSnapshot{{
			OperationID: fixture.RequestedOperationID, Canonical: fixture.Canonical,
		}},
	}
}
