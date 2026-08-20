package executionevidence

import (
	"testing"
	"time"
)

func TestEvidenceV1NormalizesAndDigestsCanonicalEvidence(t *testing.T) {
	value := canonicalFixture()
	digest, err := Digest(value)
	if err != nil {
		t.Fatal(err)
	}
	if !IsDigest(digest) {
		t.Fatalf("digest = %q", digest)
	}
	value.CapabilityEvidence = []CapabilityEntry{value.CapabilityEvidence[1], value.CapabilityEvidence[0]}
	reordered, err := Digest(value)
	if err != nil {
		t.Fatal(err)
	}
	if reordered != digest {
		t.Fatalf("normalized digest changed: %q != %q", reordered, digest)
	}
}

func TestEvidenceV1RejectsCompatibilityProfileAsExecutionContract(t *testing.T) {
	value := canonicalFixture()
	value.ProtocolProfileID = "legacy.channel_model_probe"
	if _, err := Normalize(value); err == nil {
		t.Fatal("legacy probe profile was accepted as an execution contract")
	}
	value = canonicalFixture()
	value.ModelOperationID = "legacy.any.v1"
	if _, err := Normalize(value); err == nil {
		t.Fatal("legacy compatibility operation was accepted as an execution operation")
	}
}

func TestEvidenceV1LegacyAndVerifyBundlesAreModeBound(t *testing.T) {
	legacy := legacyFixture()
	if _, err := Normalize(legacy); err != nil {
		t.Fatalf("legacy evidence: %v", err)
	}
	verify := canonicalFixture()
	verify.AuthorityMode = ModeVerify
	verify.LegacyChannelID = 31
	verify.LegacyChannelKeyID = 32
	verify.LegacyRoutingRevision = 4
	verify.LegacyCircuitRevision = 5
	verify.LegacyCredentialStateRevision = 6
	if _, err := Normalize(verify); err != nil {
		t.Fatalf("verify evidence: %v", err)
	}
	verify.LegacyChannelKeyID = 0
	if _, err := Normalize(verify); err == nil {
		t.Fatal("partial verify bundle was accepted")
	}
}

func canonicalFixture() EvidenceV1 {
	now := time.Date(2026, 8, 20, 1, 2, 3, 456789000, time.UTC)
	capabilities := []CapabilityEntry{
		{CapabilityID: "stream", SupportState: "enabled", EvidenceState: "live_probed", EvidenceRef: "probe:1", VerifiedAt: &now, Revision: 2},
		{CapabilityID: "tools", SupportState: "enabled", EvidenceState: "production_approved", EvidenceRef: "approval:1", VerifiedAt: &now, Revision: 3},
	}
	capabilityHash, _ := CapabilityDigest(capabilities)
	return EvidenceV1{
		ContractVersion: ContractVersion, AuthorityMode: ModeCanonical,
		ExecutionContractSource: ContractSourceCanonicalProfile,
		PublicModelID:           11, DeploymentID: 12, ConnectionID: 13, CredentialBindingID: 14,
		ConnectionRevision: 2, DeploymentRevision: 3, CircuitRevision: 4,
		CredentialStateRevision: 5, CredentialLinkedAt: now,
		ModelOperationID: "openai.chat_completions.v1", OperationRevision: 6,
		ProtocolProfileID: "openai.chat_completions", ProtocolProfileVersion: "1.0.0",
		OperationEvidenceState: "production_approved", OperationEvidenceRef: "approval:operation",
		OperationVerifiedAt: &now, CapabilityEvidenceHash: capabilityHash, CapabilityEvidence: capabilities,
		CredentialProbeID: 15, CredentialProbeTestedAt: now,
		RuntimeReleaseID: 16, RuntimeReleaseRef: "rcr_execution_v1", RuntimeStage: "active",
		SlotVersion: 7, SnapshotHash: "mrs1_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
}

func legacyFixture() EvidenceV1 {
	return EvidenceV1{
		ContractVersion: ContractVersion, AuthorityMode: ModeLegacy,
		ExecutionContractSource: ContractSourceLegacyAdapter,
		ModelOperationID:        "openai.chat_completions.v1",
		LegacyChannelID:         21, LegacyChannelKeyID: 22, LegacyRoutingRevision: 2,
		LegacyCircuitRevision: 3, LegacyCredentialStateRevision: 4,
	}
}
