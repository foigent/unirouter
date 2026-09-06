package adapters

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/models"
)

func TestDataRevisionsRetainExactResponseMechanicsWithoutCompiledAddresses(t *testing.T) {
	for _, id := range []string{"deepseek.v4-flash.api", "deepseek.v4-pro.api", "kimi.k2.6.api", "kimi.k2.7-code.api", "kimi.k3.api", "minimax.m3.payg", "minimax.m2.7.payg", "minimax.m2.7-highspeed.payg", "glm.5.api", "glm.5-turbo.api", "glm.5.1.api", "glm.5.2.api", "glm.5.3.api", "glm.5v-turbo.api"} {
		t.Run(id, func(t *testing.T) {
			var p modelprofile.Profile
			for _, candidate := range modelprofile.All() {
				if candidate.ID == id && candidate.Current {
					p = candidate
					break
				}
			}
			require.NotEmpty(t, p.ID)
			before := ResponseInput{Endpoint: EndpointChatCompletions, Channel: models.Channel{Provider: p.Provider}, UpstreamModel: p.UpstreamModelIDs[0], ModelOperationID: modelprofile.OperationOpenAIChat, ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, CacheUsageProfile: p.CacheUsageProfile, ProtocolProfile: p.ProtocolProfile, OperationProtocolProfileID: p.ProtocolProfile, OperationProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1}
			old, oldFound, err := runtimeChatNonStreamMechanics(before)
			require.NoError(t, err)
			p.Version = "99.0.0"
			d, err := modelprofile.NewDefinition(p, p.Provider+"/"+p.UpstreamModelIDs[0], "")
			require.NoError(t, err)
			_, compiled := modelprofile.Lookup(p.ID, p.Version)
			require.False(t, compiled)
			after := before
			after.ProfileDefinition = d
			after.ExecutionProfileVersion = p.Version
			current, found, err := runtimeChatNonStreamMechanics(after)
			require.NoError(t, err)
			require.Equal(t, oldFound, found)
			// The private provenance flag intentionally replaces legacy name dispatch.
			require.Equal(t, current.NormalizeToolCallIndex, current.dataToolCallIndex)
			current.dataToolCallIndex = false
			require.Equal(t, old, current)
			bad := after
			bad.UpstreamModel = "unapproved-product"
			_, _, err = runtimeChatNonStreamMechanics(bad)
			require.Error(t, err)
			if p.Provider == "deepseek" {
				i := DeepSeekChatStreamRuntimeIdentity{OperationID: before.ModelOperationID, ExecutionProfileID: p.ID, ExecutionProfileVersion: before.ExecutionProfileVersion, Provider: p.Provider, UpstreamModel: before.UpstreamModel, ProtocolProfileID: p.ProtocolProfile, ProtocolProfileVersion: before.OperationProtocolProfileVersion, CacheUsageProfile: p.CacheUsageProfile}
				old, of, err := ResolveDeepSeekRuntimeChatStreamMechanics(i)
				require.NoError(t, err)
				i.ProfileDefinition = d
				i.ExecutionProfileVersion = p.Version
				current, cf, err := ResolveDeepSeekRuntimeChatStreamMechanics(i)
				require.NoError(t, err)
				require.Equal(t, of, cf)
				require.Equal(t, old, current)
			}
			if p.Provider == "kimi" {
				i := KimiChatStreamRuntimeIdentity{OperationID: before.ModelOperationID, ExecutionProfileID: p.ID, ExecutionProfileVersion: before.ExecutionProfileVersion, Provider: p.Provider, UpstreamModel: before.UpstreamModel, ProtocolProfileID: p.ProtocolProfile, ProtocolProfileVersion: before.OperationProtocolProfileVersion, CacheUsageProfile: p.CacheUsageProfile}
				old, of, err := ResolveKimiRuntimeChatStreamMechanics(i)
				require.NoError(t, err)
				i.ProfileDefinition = d
				i.ExecutionProfileVersion = p.Version
				current, cf, err := ResolveKimiRuntimeChatStreamMechanics(i)
				require.NoError(t, err)
				require.Equal(t, of, cf)
				require.Equal(t, old, current)
			}
		})
	}
}

func TestDataToolIndexSelectionUsesImmutableContract(t *testing.T) {
	for _, tc := range []struct{ id, version, protocol string }{
		{"minimax.m2.7-highspeed.payg", "1.2.0", "protocol.minimax-m2-openai-chat.v8"},
		{"kimi.k2.6.api", "1.7.0", "protocol.kimi-openai-chat.v6"},
		{"kimi.k3.api", "1.9.0", "protocol.kimi-openai-chat.k3.v6"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			p, ok := modelprofile.Lookup(tc.id, tc.version)
			require.True(t, ok)
			p.Version = "99.0.0"
			p.ProtocolProfile = tc.protocol
			d, err := modelprofile.NewDefinition(p, p.Provider+"/"+p.UpstreamModelIDs[0], "")
			require.NoError(t, err)
			input := ResponseInput{ProfileDefinition: d, Endpoint: EndpointChatCompletions, Channel: models.Channel{Provider: p.Provider}, UpstreamModel: p.UpstreamModelIDs[0], ModelOperationID: modelprofile.OperationOpenAIChat, ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, CacheUsageProfile: p.CacheUsageProfile, ProtocolProfile: p.ProtocolProfile, OperationProtocolProfileID: p.ProtocolProfile, OperationProtocolProfileVersion: "1.0.0"}
			mechanics, found, err := runtimeChatNonStreamMechanics(input)
			require.NoError(t, err)
			require.True(t, found)
			require.True(t, mechanics.dataToolCallIndex)
			for _, index := range []string{"0", "9999999999999999999999999999999", "null", "-1", "0.0", `"0"`} {
				raw := []byte(`{"choices":[{"index":0,"message":{"tool_calls":[{"index":` + index + `,"id":"call","type":"function","function":{"name":"echo","arguments":"{}"}}]}}]}`)
				require.NoError(t, ValidateOpenAICompatibleChatNonStreamDocument(raw, mechanics))
				prepared, err := PrepareOpenAICompatibleChatNonStreamDocument(p.Provider, p.UpstreamModelIDs[0], p.ProtocolProfile, "1.0.0", raw, mechanics)
				require.NoError(t, err)
				if index == "0" || strings.HasPrefix(index, "999") {
					require.Empty(t, prepared.DeliveryIssue)
					var envelope map[string]any
					require.NoError(t, json.Unmarshal(prepared.Body, &envelope))
					choice := envelope["choices"].([]any)[0].(map[string]any)
					require.EqualValues(t, 0, choice["index"])
					call := choice["message"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
					require.NotContains(t, call, "index")
					require.Equal(t, "call", call["id"])
				} else {
					require.Equal(t, ChatNonStreamToolCallNormalizationInvalid, prepared.DeliveryIssue)
					require.Equal(t, raw, prepared.Body)
				}
				old, changed, err := NormalizeOpenAICompatibleChatToolCallIndex(p.Provider, p.UpstreamModelIDs[0], p.ProtocolProfile, raw)
				require.NoError(t, err)
				require.False(t, changed)
				require.Equal(t, raw, old)
			}
			duplicate := []byte(`{"choices":[{"message":{"tool_calls":[{"index":0,"index":1}]}}]}`)
			require.Error(t, ValidateOpenAICompatibleChatNonStreamDocument(duplicate, mechanics))
			bad := input
			bad.ExecutionProfileVersion = "1.2.0"
			_, _, err = runtimeChatNonStreamMechanics(bad)
			require.Error(t, err)
		})
	}
	for _, m := range []ChatNonStreamMechanics{{dataToolCallIndex: true}, {dataToolCallIndex: true, NormalizeToolCallIndex: true}, {dataToolCallIndex: true, RejectDuplicateMembers: true}} {
		require.Error(t, validateChatNonStreamMechanics(m))
	}
}
