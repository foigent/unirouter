package modelrouting

import "testing"

func TestCapabilityApprovalPendingRequiresExactTargetShape(t *testing.T) {
	provider, upstream, publicPath := "minimax", "MiniMax-M3", "/v1/chat/completions"
	candidate := canonicalCandidate{
		DeploymentID: 19, Provider: provider, UpstreamModel: upstream,
		ConnectionRevision: 1, DeploymentRevision: 1, OperationRevision: 2,
		OperationID:       "openai.chat_completions.v1",
		ProtocolProfileID: "protocol.minimax-m3-openai-chat.v9", ProtocolProfileVersion: "1.0.0",
		Manifest: &manifestItem{
			DeploymentID: 19, ModelOperationID: "openai.chat_completions.v1",
			ExpectedConnectionRevision: 1, ExpectedDeploymentRevision: 1, ExpectedOperationRevision: 2,
			ProtocolProfileID: "protocol.minimax-m3-openai-chat.v9", ProtocolProfileVersion: "1.0.0",
		},
	}
	capability := capabilitySnapshot{ID: "cache_read_usage", SupportState: "enabled", EvidenceState: "production_approved"}
	if !capabilityApprovalPending(
		candidate, candidate.OperationID, capability, &provider, &upstream, &publicPath, nil, nil,
	) {
		t.Fatal("exact Target shape was not admitted as startup-only pending approval")
	}

	assertRejected := func(label string, edit func(*canonicalCandidate, *capabilitySnapshot, *string, *string, *string)) {
		t.Helper()
		copyCandidate, copyCapability := candidate, capability
		copyProvider, copyUpstream, copyPath := provider, upstream, publicPath
		edit(&copyCandidate, &copyCapability, &copyProvider, &copyUpstream, &copyPath)
		if capabilityApprovalPending(
			copyCandidate, copyCandidate.OperationID, copyCapability,
			&copyProvider, &copyUpstream, &copyPath, nil, nil,
		) {
			t.Fatalf("%s was admitted as startup-only pending approval", label)
		}
	}
	assertRejected("manifest missing", func(candidate *canonicalCandidate, _ *capabilitySnapshot, _, _, _ *string) {
		candidate.Manifest = nil
	})
	assertRejected("manifest revision drift", func(candidate *canonicalCandidate, _ *capabilitySnapshot, _, _, _ *string) {
		candidate.Manifest.ExpectedOperationRevision++
	})
	assertRejected("projection disabled", func(_ *canonicalCandidate, capability *capabilitySnapshot, _, _, _ *string) {
		capability.SupportState = "disabled"
	})
	assertRejected("provider mismatch", func(_ *canonicalCandidate, _ *capabilitySnapshot, provider, _, _ *string) {
		*provider = "other"
	})
	assertRejected("upstream mismatch", func(_ *canonicalCandidate, _ *capabilitySnapshot, _, upstream, _ *string) {
		*upstream = "other"
	})
	assertRejected("catalog path mismatch", func(_ *canonicalCandidate, _ *capabilitySnapshot, _, _, path *string) {
		*path = "/v1/responses"
	})
	assertRejected("unknown operation", func(candidate *canonicalCandidate, _ *capabilitySnapshot, _, _, _ *string) {
		candidate.OperationID = "unknown.operation.v1"
	})

	approvalID := int64(1)
	if capabilityApprovalPending(
		candidate, candidate.OperationID, capability, &provider, &upstream, &publicPath, &approvalID, nil,
	) {
		t.Fatal("partial current approval was admitted as startup-only pending approval")
	}
}
