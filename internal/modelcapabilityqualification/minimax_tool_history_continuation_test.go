package modelcapabilityqualification

import (
	"github.com/stretchr/testify/require"
	pub "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"os"
	"strings"
	"testing"
)

func TestMiniMaxContinuationBundleExactComposition(t *testing.T) {
	wire, e := os.ReadFile("../../capability-bundles/minimax-tool-history-continuation.json")
	require.NoError(t, e)
	require.NoError(t, ValidateBundleSource(wire))
	bundle, e := pub.DecodeBundleV2(wire)
	require.NoError(t, e)
	require.Len(t, bundle.Definitions, 3)
	require.Len(t, bundle.BindingDelta, 14)
	require.Len(t, bundle.SelectorDelta, 14)
	defs := map[string]modelprofile.Definition{}
	for _, raw := range bundle.Definitions {
		d, e := modelprofile.DecodeDefinition(raw)
		require.NoError(t, e)
		defs[d.Digest()] = d
		_, compiled := modelprofile.Lookup(d.Profile().ID, d.Profile().Version)
		require.False(t, compiled)
	}
	for _, r := range bundle.BindingDelta {
		d := defs[r.ProfileDefinitionDigest]
		require.NotEmpty(t, d.Digest())
		p := d.Profile()
		b := Binding{OperationID: r.OperationID, CapabilityID: r.CapabilityID, ExecutionProfileID: r.ExecutionProfileID, ExecutionProfileVersion: r.ExecutionProfileVersion, ProtocolProfileID: r.ProtocolProfileID, ProtocolProfileVersion: r.ProtocolProfileVersion, ProbeContractID: r.ProbeContractID, ProbeContractVersion: r.ProbeContractVersion, ExpectedProvider: r.ExpectedProvider, ExpectedUpstreamModel: r.ExpectedUpstreamModel, PublicPath: r.PublicPath}
		bound, e := BindDefinition(b, d)
		require.NoError(t, e)
		request, e := BuildOpenAIChatFunctionToolsProbeRequest(bound)
		require.NoError(t, e)
		require.Contains(t, string(request), r.ExpectedUpstreamModel)
		parentFile := "minimax-existing-streamed-tools.json"
		if p.ID == "minimax.m3.payg" {
			parentFile = "minimax-m3-stream-tier.json"
		}
		oldWire, e := os.ReadFile("../../capability-bundles/" + parentFile)
		require.NoError(t, e)
		oldBundle, e := pub.DecodeBundleV2(oldWire)
		require.NoError(t, e)
		for _, oldRaw := range oldBundle.Definitions {
			old, e := modelprofile.DecodeDefinition(oldRaw)
			require.NoError(t, e)
			if old.Profile().ID == p.ID {
				require.Equal(t, old.Digest(), d.PredecessorDigest())
				after := p
				after.Version = old.Profile().Version
				after.ReasoningProfile = old.Profile().ReasoningProfile
				require.Equal(t, old.Profile(), after, "no limits/product/defaults drift")
			}
		}
		for _, bad := range []string{"wrong_reasoning", "wrong_cache", "wrong_provider", "wrong_protocol"} {
			t.Run(p.ID+"/"+r.CapabilityID+"/"+bad, func(t *testing.T) {
				invalid := p
				badB := b
				switch bad {
				case "wrong_reasoning":
					if p.ID == "minimax.m3.payg" {
						invalid.ReasoningProfile = "reasoning.minimax-m2-required-embedded.v2"
					} else {
						invalid.ReasoningProfile = "reasoning.minimax-m3-adaptive-embedded.v3"
					}
				case "wrong_cache":
					invalid.CacheUsageProfile = "cache-usage.minimax-openai.v1"
				case "wrong_provider":
					invalid.Provider = "glm"
					badB.ExpectedProvider = "glm"
				case "wrong_protocol":
					if p.ID == "minimax.m3.payg" {
						invalid.ProtocolProfile = "protocol.minimax-m3-openai-chat.v10"
					} else {
						invalid.ProtocolProfile = "protocol.minimax-m2-openai-chat.v7"
					}
					badB.ProtocolProfileID = invalid.ProtocolProfile
				}
				other, e := modelprofile.NewDefinition(invalid, d.ProductLineageID(), d.PredecessorDigest())
				if e == nil {
					_, e = BindDefinition(badB, other)
				}
				require.Error(t, e)
			})
		}
	}
	for _, r := range bundle.SelectorDelta {
		require.Nil(t, r.ExpectedPredecessor)
		require.True(t, strings.HasSuffix(r.ExecutionProfileVersion, ".0"))
	}
	// The bundled M3 prerequisite is byte-identical to the retained formal artifact.
	raw, e := os.ReadFile("../../capability-bundles/minimax-m3-stream-tier.json")
	require.NoError(t, e)
	old, e := pub.DecodeBundleV2(raw)
	require.NoError(t, e)
	d, e := modelprofile.DecodeDefinition(old.Definitions[0])
	require.NoError(t, e)
	require.Equal(t, d.CanonicalBytes(), defs[d.Digest()].CanonicalBytes())
}
