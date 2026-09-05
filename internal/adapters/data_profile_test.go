package adapters

import (
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
