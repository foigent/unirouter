package admin

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func sharedM27Snapshot(capability string) modelCapabilitySnapshot {
	return modelCapabilitySnapshot{CustomerOrgID: 1, PublicModelID: 2, DeploymentID: 3, ConnectionID: 4, CredentialBindingID: 5,
		Provider: "minimax", BaseURL: "https://api.minimax.cn/v1", AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`), UpstreamModel: "MiniMax-M2.7",
		OperationID: modelprofile.OperationOpenAIChat, CapabilityID: capability, ExecutionProfileID: "minimax.m2.7.payg", ExecutionProfileVersion: "1.3.0", ProtocolProfileID: "protocol.minimax-m27-openai-chat.v1", ProtocolProfileVersion: "1.0.0", ExpectedProvider: "minimax", ExpectedUpstreamModel: "MiniMax-M2.7", PublicPath: "/v1/chat/completions", ProbeContractID: modelcapabilityqualification.MiniMaxM27StreamFunctionToolsProbeContractID, ProbeContractVersion: "1.0.0", ConnectionRevision: 1, DeploymentRevision: 1, OperationRevision: 1, CapabilitySupportRevision: 1, CredentialRevision: 1, CredentialLinkCreatedAt: time.Unix(1, 0), Actor: canonicalOperationProbeActorSnapshot{UserID: 6, OrgID: 1, OrganizationRole: "org_admin", AuthEpoch: 1}}
}
func TestSharedProbeGroupExactIdentityAndSupportPins(t *testing.T) {
	a, b := sharedM27Snapshot("function_tools"), sharedM27Snapshot("streaming")
	groups, separate, err := groupCapabilitySnapshots([]modelCapabilitySnapshot{b, a})
	require.NoError(t, err)
	require.Empty(t, separate)
	require.Len(t, groups, 1)
	require.Equal(t, []string{"function_tools", "streaming"}, groups[0].CapabilityIDs)
	original := groups[0].PlanDigest
	for name, change := range map[string]func(*modelCapabilitySnapshot){
		"endpoint":            func(s *modelCapabilitySnapshot) { s.BaseURL = "https://other.example/v1" },
		"connection":          func(s *modelCapabilitySnapshot) { s.ConnectionID++ },
		"connection revision": func(s *modelCapabilitySnapshot) { s.ConnectionRevision++ },
		"deployment":          func(s *modelCapabilitySnapshot) { s.DeploymentID++ },
		"deployment revision": func(s *modelCapabilitySnapshot) { s.DeploymentRevision++ },
		"operation revision":  func(s *modelCapabilitySnapshot) { s.OperationRevision++ },
		"key":                 func(s *modelCapabilitySnapshot) { s.CredentialBindingID++ },
		"key revision":        func(s *modelCapabilitySnapshot) { s.CredentialRevision++ },
		"key link": func(s *modelCapabilitySnapshot) {
			s.CredentialLinkCreatedAt = s.CredentialLinkCreatedAt.Add(time.Second)
		},
		"actor": func(s *modelCapabilitySnapshot) { s.Actor.AuthEpoch++ },
	} {
		t.Run(name, func(t *testing.T) {
			changed := b
			change(&changed)
			g, _, e := groupCapabilitySnapshots([]modelCapabilitySnapshot{a, changed})
			require.NoError(t, e)
			require.Len(t, g, 2)
			require.NotEqual(t, original, g[0].PlanDigest)
		})
	}
	b.CapabilitySupportRevision++
	groups, _, err = groupCapabilitySnapshots([]modelCapabilitySnapshot{a, b})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.NotEqual(t, original, groups[0].PlanDigest)
	_, _, err = groupCapabilitySnapshots([]modelCapabilitySnapshot{a, a})
	require.Error(t, err)
}
func TestSharedProbeRejectsUnreviewedBindingAndIncompleteReadback(t *testing.T) {
	a := sharedM27Snapshot("function_tools")
	for name, change := range map[string]func(*modelCapabilitySnapshot){
		"unknown protocol":    func(s *modelCapabilitySnapshot) { s.ProtocolProfileID = "unknown" },
		"unknown profile":     func(s *modelCapabilitySnapshot) { s.ExecutionProfileVersion = "99.0.0" },
		"unknown probe":       func(s *modelCapabilitySnapshot) { s.ProbeContractVersion = "99.0.0" },
		"upstream identity":   func(s *modelCapabilitySnapshot) { s.ExpectedUpstreamModel = "other" },
		"unproved capability": func(s *modelCapabilitySnapshot) { s.CapabilityID = "tool_choice_required" },
	} {
		t.Run(name, func(t *testing.T) {
			s := a
			change(&s)
			g, separate, e := groupCapabilitySnapshots([]modelCapabilitySnapshot{s})
			require.NoError(t, e)
			require.Empty(t, g)
			require.Equal(t, []string{s.CapabilityID}, separate)
		})
	}
	m := sharedProbeMetadata{Version: sharedProbeVersion, SourceRef: "op_00000000000000000000000000000001", PlanDigest: probeDigest("plan"), RequestDigest: probeDigest("request"), CapabilityIDs: []string{"function_tools", "streaming"}, AnchorCapabilityID: "function_tools", DispatchState: "response_received"}
	require.NoError(t, validateSharedProbeRows([]sharedProbeMetadata{m, m}, m.CapabilityIDs))
	require.Error(t, validateSharedProbeRows([]sharedProbeMetadata{m}, []string{"function_tools"}))
	other := m
	other.SourceRef = "op_00000000000000000000000000000002"
	require.Error(t, validateSharedProbeRows([]sharedProbeMetadata{m, other}, m.CapabilityIDs))
	require.Error(t, validateSharedProbeRows([]sharedProbeMetadata{m, m}, []string{"function_tools", "function_tools"}))
}
