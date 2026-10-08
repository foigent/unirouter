package modelcapabilityqualification

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestDeepSeekFlashDefaultEffortBundleAndProbeV2(t *testing.T) {
	oldWire, err := os.ReadFile("../../capability-bundles/deepseek-streamed-tools.json")
	require.NoError(t, err)
	old, err := publication.DecodeBundleV2(oldWire)
	require.NoError(t, err)
	wire, err := os.ReadFile("../../capability-bundles/deepseek-default-effort.json")
	require.NoError(t, err)
	require.NoError(t, ValidateBundleSource(wire))
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	require.Empty(t, bundle.Definitions)
	require.Len(t, bundle.BindingDelta, 1)
	require.Len(t, bundle.SelectorDelta, 1)
	require.Nil(t, bundle.SelectorDelta[0].ExpectedPredecessor)
	var d modelprofile.Definition
	for _, raw := range old.Definitions {
		candidate, e := modelprofile.DecodeDefinition(raw)
		require.NoError(t, e)
		if candidate.Profile().ID == "deepseek.v4-flash.api" {
			d = candidate
		}
	}
	row := bundle.BindingDelta[0]
	require.Equal(t, "c45101819ae28376289df7876b515d42d00bf934e0721d3220257e04fc1152b6", d.Digest())
	require.Equal(t, d.Digest(), row.ProfileDefinitionDigest)
	require.Equal(t, modelprofile.CapabilityReasoningEffort, row.CapabilityID)
	b := Binding{OperationID: row.OperationID, CapabilityID: row.CapabilityID, ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion,
		ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion, ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion,
		ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath}
	b, err = BindDefinition(b, d)
	require.NoError(t, err)
	key, err := SharedStreamProbeContract(b)
	require.NoError(t, err)
	require.Equal(t, DeepSeekStreamFunctionToolsProbeContractID+"@2.0.0", key)
	request, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(request, &payload))
	require.Equal(t, "high", payload["reasoning_effort"])
	require.Equal(t, map[string]any{"type": "enabled"}, payload["thinking"])
	require.Equal(t, true, payload["stream"])
	require.Equal(t, map[string]any{"include_usage": true}, payload["stream_options"])
	require.NotContains(t, payload, "tool_choice")
	v1 := b
	v1.CapabilityID, v1.ProbeContractVersion = modelprofile.CapabilityFunctionTools, "1.0.0"
	v1Request, err := BuildOpenAIChatFunctionToolsProbeRequest(v1)
	require.NoError(t, err)
	delete(payload, "reasoning_effort")
	wantV1, err := json.Marshal(payload)
	require.NoError(t, err)
	require.Equal(t, string(wantV1), string(v1Request))
	for _, mutate := range []func(*Binding){
		func(x *Binding) { x.CapabilityID = modelprofile.CapabilityTools },
		func(x *Binding) { x.ProbeContractVersion = "1.0.0" },
		func(x *Binding) { x.ProbeContractVersion = "3.0.0" },
		func(x *Binding) { x.ExpectedProvider = "other" },
		func(x *Binding) { x.ExecutionProfileVersion = "1.5.0" },
		func(x *Binding) { x.ProtocolProfileID = "protocol.deepseek-openai-chat.v4" },
	} {
		bad := b
		mutate(&bad)
		_, err := BuildOpenAIChatFunctionToolsProbeRequest(bad)
		require.Error(t, err)
	}
	// Constructed SSE fixture validates the full v2 observation, not a paid
	// Provider call or a claim about every effort level.
	fixture := "data: " + `{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"deepseek-v4-flash","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"brief","tool_calls":[{"index":0,"id":"call_echo","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}]}` + "\n\n" +
		"data: " + `{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"deepseek-v4-flash","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}` + "\n\ndata: [DONE]\n\n"
	observation, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(fixture))
	require.NoError(t, err)
	require.True(t, observation.ReasoningObserved)
	require.True(t, observation.ToolCallObserved)
	require.EqualValues(t, 120, observation.TotalTokens)
	require.Equal(t, 3, observation.DataEventCount) // Two frames and [DONE].
	require.Equal(t, 2, observation.DeltaCount)
	legacy, err := ValidateOpenAIChatStreamFunctionToolsForBinding(v1, "text/event-stream", strings.NewReader(fixture))
	require.NoError(t, err)
	require.Equal(t, observation.DataEventCount, legacy.DataEventCount)
	require.Equal(t, observation.DeltaCount, legacy.DeltaCount)

	// Two distinct first-event failures used to collapse into the same bare
	// `stream` code and misleading 0/0 counts in the persisted Probe receipt.
	for _, tc := range []struct {
		name, wire, stage, member string
		events, deltas            int
	}{
		{"model alias", strings.Replace(fixture, `"model":"deepseek-v4-flash"`, `"model":"deepseek-flash"`, 1), "model_identity", "mismatch", 1, 0},
		{"SSE field before data", "event: private_probe_marker\n" + fixture, "sse", "unsupported_field", 0, 0},
		{"SSE field after data", strings.SplitN(fixture, "\n\n", 2)[0] + "\n\nevent: private_probe_marker\n\n", "sse", "unsupported_field", 1, 1},
		{"malformed first frame", "data: {private_probe_marker}\n\n", "frame_json", "invalid_json", 1, 0},
		{"invalid usage", strings.Replace(fixture, `"total_tokens":120`, `"total_tokens":"private_probe_marker"`, 1), "normalization", "usage", 2, 1},
	} {
		t.Run("diagnostic/"+tc.name, func(t *testing.T) {
			observed, validationErr := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(tc.wire))
			require.Equal(t, "stream", OpenAIChatFunctionToolsErrorCode(validationErr))
			require.Equal(t, OpenAIChatFunctionToolsDiagnostic{Stage: tc.stage, Member: tc.member}, DescribeOpenAIChatFunctionToolsDiagnostic(validationErr))
			require.Equal(t, tc.events, observed.DataEventCount)
			require.Equal(t, tc.deltas, observed.DeltaCount)
			diagnosticWire, err := json.Marshal(DescribeOpenAIChatFunctionToolsDiagnostic(validationErr))
			require.NoError(t, err)
			require.NotContains(t, string(diagnosticWire), "private_probe_marker")
		})
	}
	require.Equal(t, OpenAIChatFunctionToolsDiagnostic{}, CanonicalizeOpenAIChatFunctionToolsDiagnostic("private_probe_marker", "unsupported_field"))
	require.Equal(t, OpenAIChatFunctionToolsDiagnostic{}, CanonicalizeOpenAIChatFunctionToolsDiagnostic("sse", "private_probe_marker"))
	for name, invalid := range map[string]string{
		"reasoning": strings.Replace(fixture, `"reasoning_content":"brief",`, "", 1),
		"usage":     strings.Replace(fixture, `,"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}`, "", 1),
		"terminal":  strings.Replace(fixture, "data: [DONE]\n\n", "", 1),
	} {
		_, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(invalid))
		require.Error(t, err, name)
	}
}
