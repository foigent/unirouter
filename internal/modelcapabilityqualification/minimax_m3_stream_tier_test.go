package modelcapabilityqualification

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestMiniMaxM3StreamTierDataBundleAndCapturedProbe(t *testing.T) {
	fixture, err := os.ReadFile("../adapters/testdata/minimax_m3_stream_tier_20260906.sse")
	require.NoError(t, err)
	for _, name := range []string{"minimax-existing-streamed-tools.json", "minimax-m3-stream-tier.json"} {
		wire, err := os.ReadFile("../../capability-bundles/" + name)
		require.NoError(t, err)
		require.NoError(t, ValidateBundleSource(wire))
		bundle, err := publication.DecodeBundleV2(wire)
		require.NoError(t, err)
		for _, raw := range bundle.Definitions {
			d, err := modelprofile.DecodeDefinition(raw)
			require.NoError(t, err)
			p := d.Profile()
			if p.ID != "minimax.m3.payg" {
				continue
			}
			for _, row := range bundle.BindingDelta {
				if row.ExecutionProfileID != p.ID {
					continue
				}
				b, err := BindDefinition(Binding{OperationID: row.OperationID, CapabilityID: row.CapabilityID, ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion, ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion, ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion, ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath}, d)
				require.NoError(t, err)
				request, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
				require.NoError(t, err)
				if row.CapabilityID == modelprofile.CapabilityPromptCache || row.CapabilityID == modelprofile.CapabilityCacheReadUsage {
					continue
				}
				require.Equal(t, "8b4a20a55e11defca9907a097ab7cb68fb2b8b1ee4775f1240a891c956ce0011", fmt.Sprintf("%x", sha256.Sum256(request)), "no request-contract change")
				obs, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", bytes.NewReader(fixture))
				if p.Version == "1.3.0" {
					require.Error(t, err)
					require.Equal(t, "stream", OpenAIChatFunctionToolsErrorCode(err))
					continue
				}
				require.NoError(t, err)
				require.True(t, obs.ToolCallObserved)
				require.True(t, obs.ReasoningObserved)
				require.EqualValues(t, 511, obs.TotalTokens)
				require.EqualValues(t, 128, obs.CachedTokens)
				require.Equal(t, "cb3d67dec34dc7a5dcc52aa3a2748588f07d206b12da84b05fefc1405ef8da0b", d.PredecessorDigest())
				require.Contains(t, d.Requirements(), modelprofile.ImplementationReference{Kind: "chat", ID: "protocol.minimax-m3-openai-chat.v11"})
				_, compiled := modelprofile.Lookup(p.ID, p.Version)
				require.False(t, compiled, "revision is data, not Catalog code")
			}
		}
	}
}
