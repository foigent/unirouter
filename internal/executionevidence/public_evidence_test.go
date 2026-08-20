package executionevidence

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

func TestEvidenceV2IsExplicitAndLeavesV1DigestStable(t *testing.T) {
	historical := canonicalFixture()
	v1Digest, err := Digest(historical)
	if err != nil {
		t.Fatal(err)
	}
	base := historical
	base.ModelOperationID = "openai.responses.v1"
	base.ProtocolProfileID = "unirouter.openai.responses.text.v1"
	value := EvidenceV2{
		EvidenceV1:                 base,
		PublicExecutionBindingID:   77,
		DeliveryContractVersion:    PublicDeliveryContractV1,
		ChargeContractVersion:      PublicChargeContractV1,
		IdempotencyContractVersion: PublicIdempotencyContractV1,
	}
	value.ContractVersion = ContractVersionV2
	v2Digest, err := DigestV2(value)
	if err != nil {
		t.Fatal(err)
	}
	if v1Digest == v2Digest {
		t.Fatal("V2 public authority must have an independent digest")
	}
	if got, err := Digest(historical); err != nil || got != v1Digest {
		t.Fatalf("V1 digest drifted: got=%q err=%v want=%q", got, err, v1Digest)
	}
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		`"contract_version":"unirouter.execution-route.v2"`,
		`"public_execution_binding_id":77`,
		`"charge_contract_version":"unirouter.provider-final-token-charge.v1"`,
	} {
		if !json.Valid(body) || !containsJSONText(body, field) {
			t.Fatalf("V2 JSON %s is missing %s", body, field)
		}
	}
	route, err := value.RouteEvidenceV1()
	if err != nil {
		t.Fatal(err)
	}
	if route.ContractVersion != ContractVersionV1 {
		t.Fatalf("underlying route version=%q", route.ContractVersion)
	}
	compact, err := CompactV2(value)
	if err != nil {
		t.Fatal(err)
	}
	if compact.PublicExecutionBindingID != value.PublicExecutionBindingID ||
		compact.DeliveryContractVersion != value.DeliveryContractVersion ||
		compact.ChargeContractVersion != value.ChargeContractVersion ||
		compact.IdempotencyContractVersion != value.IdempotencyContractVersion {
		t.Fatalf("V2 compact projection lost public authority: %#v", compact)
	}
}

func TestEvidenceV2ContractConstantsMatchEndpointProtocolRegistry(t *testing.T) {
	if PublicDeliveryContractV1 != endpointprotocol.PublicDeliveryContractV1 ||
		PublicChargeContractV1 != endpointprotocol.PublicCustomerChargeContractV1 ||
		PublicIdempotencyContractV1 != endpointprotocol.PublicIdempotencyContractV1 {
		t.Fatalf(
			"execution evidence public contracts drifted from endpoint protocol registry: delivery=%q charge=%q idempotency=%q",
			PublicDeliveryContractV1, PublicChargeContractV1, PublicIdempotencyContractV1,
		)
	}
}

func TestEvidenceV2RejectsPartialOrNonPublicAuthority(t *testing.T) {
	base := canonicalFixture()
	base.ContractVersion = ContractVersionV2
	value := EvidenceV2{
		EvidenceV1:                 base,
		PublicExecutionBindingID:   77,
		DeliveryContractVersion:    PublicDeliveryContractV1,
		ChargeContractVersion:      PublicChargeContractV1,
		IdempotencyContractVersion: PublicIdempotencyContractV1,
	}
	if _, err := NormalizeV2(value); err == nil {
		t.Fatal("non-public operation was accepted by EvidenceV2")
	}
	value.ModelOperationID = "anthropic.messages.v1"
	value.ProtocolProfileID = "unirouter.anthropic.messages.text.v1"
	for _, mutation := range []struct {
		name  string
		apply func(*EvidenceV2)
	}{
		{name: "delivery mismatch", apply: func(candidate *EvidenceV2) { candidate.DeliveryContractVersion = "delivery" }},
		{name: "charge missing", apply: func(candidate *EvidenceV2) { candidate.ChargeContractVersion = "" }},
		{name: "idempotency mismatch", apply: func(candidate *EvidenceV2) { candidate.IdempotencyContractVersion = "idempotency" }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := value
			mutation.apply(&candidate)
			if _, err := NormalizeV2(candidate); err == nil {
				t.Fatal("non-exact public contract was accepted")
			}
		})
	}
	value.AuthorityMode = ModeVerify
	value.LegacyChannelID = 1
	value.LegacyChannelKeyID = 2
	value.LegacyRoutingRevision = 1
	value.LegacyCircuitRevision = 1
	value.LegacyCredentialStateRevision = 1
	if _, err := NormalizeV2(value); err == nil {
		t.Fatal("verify-mode public authority was accepted")
	}
}

func TestNormalizeCompactKeepsV1AndV2PublicBundlesDisjoint(t *testing.T) {
	v1, err := Compact(canonicalFixture())
	if err != nil {
		t.Fatal(err)
	}
	v1.PublicExecutionBindingID = 77
	v1.DeliveryContractVersion = PublicDeliveryContractV1
	v1.ChargeContractVersion = PublicChargeContractV1
	v1.IdempotencyContractVersion = PublicIdempotencyContractV1
	if _, err := NormalizeCompact(v1); err == nil {
		t.Fatal("V1 compact accepted V2 public authority")
	}

	base := canonicalFixture()
	base.ContractVersion = ContractVersionV2
	base.ModelOperationID = "openai.responses.v1"
	base.ProtocolProfileID = "unirouter.openai.responses.text.v1"
	v2 := CompactProjection{
		ContractVersion: ContractVersionV2, AuthorityMode: ModeCanonical,
		EvidenceHash: "sha256:" + strings.Repeat("a", 64), PublicModelID: base.PublicModelID,
		DeploymentID: base.DeploymentID, ConnectionID: base.ConnectionID,
		CredentialBindingID: base.CredentialBindingID, ModelOperationID: base.ModelOperationID,
		RuntimeReleaseID: base.RuntimeReleaseID, SnapshotHash: base.SnapshotHash,
		PublicExecutionBindingID: 77, DeliveryContractVersion: PublicDeliveryContractV1,
		ChargeContractVersion: PublicChargeContractV1, IdempotencyContractVersion: PublicIdempotencyContractV1,
	}
	if _, err := NormalizeCompact(v2); err != nil {
		t.Fatalf("valid V2 compact rejected: %v", err)
	}
	v2.ChargeContractVersion = ""
	if _, err := NormalizeCompact(v2); err == nil {
		t.Fatal("partial V2 compact public authority was accepted")
	}
	v2.ChargeContractVersion = PublicChargeContractV1
	v2.DeliveryContractVersion = "delivery"
	if _, err := NormalizeCompact(v2); err == nil {
		t.Fatal("mismatched V2 compact public authority was accepted")
	}
}

func TestEvidenceV1RejectsPublicOperations(t *testing.T) {
	for _, operationID := range []string{"openai.responses.v1", "anthropic.messages.v1"} {
		t.Run(operationID, func(t *testing.T) {
			value := canonicalFixture()
			value.ModelOperationID = operationID
			if _, err := Normalize(value); err == nil {
				t.Fatal("EvidenceV1 accepted a public operation reserved for EvidenceV2")
			}

			compact := CompactProjection{
				ContractVersion: ContractVersionV1, AuthorityMode: ModeCanonical,
				EvidenceHash:  "sha256:" + strings.Repeat("a", 64),
				PublicModelID: value.PublicModelID, DeploymentID: value.DeploymentID,
				ConnectionID: value.ConnectionID, CredentialBindingID: value.CredentialBindingID,
				ModelOperationID: operationID, RuntimeReleaseID: value.RuntimeReleaseID,
				SnapshotHash: value.SnapshotHash,
			}
			if _, err := NormalizeCompact(compact); err == nil {
				t.Fatal("V1 compact projection accepted a public operation")
			}
		})
	}
}

func containsJSONText(body []byte, value string) bool {
	return string(body) != "" && strings.Contains(string(body), value)
}
