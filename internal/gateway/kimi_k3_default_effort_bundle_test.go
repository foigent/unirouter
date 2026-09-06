package gateway

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	qualification "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

func TestKimiK3DefaultStreamToolsRequirementsHaveExactDataBindings(t *testing.T) {
	available := map[string]publication.DefinitionBindingV2{}
	var d modelprofile.Definition
	for _, name := range []string{"kimi-streamed-tools.json", "kimi-k3-default-effort.json"} {
		wire, err := os.ReadFile("../../capability-bundles/" + name)
		require.NoError(t, err)
		bundle, err := publication.DecodeBundleV2(wire)
		require.NoError(t, err)
		for _, raw := range bundle.Definitions {
			next, err := modelprofile.DecodeDefinition(raw)
			require.NoError(t, err)
			if next.Profile().ID == "kimi.k3.api" {
				d = next
			}
		}
		for _, row := range bundle.BindingDelta {
			if row.ExecutionProfileID == "kimi.k3.api" {
				require.NotContains(t, available, row.CapabilityID, "new Bundle must not replace an old binding")
				available[row.CapabilityID] = row
			}
		}
	}
	require.Len(t, available, 6)
	profile := d.Profile()
	plan := modelExecutionPlanForProfile(profile)
	plan.Definition = d
	body := []byte(`{"model":"kimi-k3","messages":[{"role":"user","content":"Use the echo tool."}],"stream":true,"stream_options":{"include_usage":true},"max_completion_tokens":8192,"tools":[{"type":"function","function":{"name":"echo","description":"Return the supplied value.","parameters":{"type":"object","properties":{}}}}],"tool_choice":"auto"}`)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	required, err := invocationCapabilityRequirementsForChat(profile, evidence, body)
	require.NoError(t, err)
	effort := false
	for _, requirement := range required {
		row, ok := available[requirement.CapabilityID]
		require.True(t, ok, "default request is missing qualification binding for %s", requirement.CapabilityID)
		if requirement.CapabilityID == modelprofile.CapabilityReasoningEffort {
			effort = true
			require.Equal(t, []string{modelrouting.CapabilityOriginProviderDefault}, requirement.Origins)
			require.Equal(t, "2.0.0", row.ProbeContractVersion)
		}
		require.Equal(t, d.Digest(), row.ProfileDefinitionDigest)
		_, err := qualification.BindDefinition(qualification.Binding{OperationID: row.OperationID, CapabilityID: row.CapabilityID, ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion, ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion, ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion, ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath}, d)
		require.NoError(t, err)
	}
	require.True(t, effort, "omitted effort must retain the default max qualification requirement")
}
