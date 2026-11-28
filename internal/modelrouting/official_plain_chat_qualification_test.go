package modelrouting

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestOfficialPlainChatDefaultRequiresCurrentReasoningApproval(t *testing.T) {
	raw, err := os.ReadFile("../../capability-bundles/official-plain-text-chat.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(raw)
	require.NoError(t, err)
	urls := map[string]string{
		"hy3": "https://tokenhub.tencentmaas.com/v1", "xai": "https://api.x.ai/v1",
		"doubao": "https://ark.cn-beijing.volces.com/api/v3", "openai": "https://api.openai.com/v1",
	}
	for _, raw := range bundle.Definitions {
		definition, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		p := definition.Profile()
		if len(p.Capabilities) == 0 {
			continue
		}
		t.Run(p.ID, func(t *testing.T) {
			value := canonicalFixture()
			value.Definition = definition
			value.ExecutionProfileID, value.ExecutionProfileVersion = p.ID, p.Version
			operation := modelprofile.OperationOpenAIChat
			request := requestFor(ModeCanonical, "reasoning-fixture")
			request.InvocationCapabilities = []CapabilityRequirement{{
				CapabilityID: modelprofile.DefaultChatReasoningCapability(p),
				Origins:      []string{CapabilityOriginProviderDefault},
			}}
			value.CapabilityRequirements, err = ResolveCapabilityRequirements(p, request, definition)
			require.NoError(t, err)
			require.Len(t, value.CapabilityRequirements, 1)
			candidate := value.Canonical[0]
			candidate.Manifest = nil
			candidate.ProtocolProfileID, candidate.ProtocolProfileVersion = p.ProtocolProfile, modelprofile.ChatOperationProtocolBindingVersionV1
			candidate.Provider, candidate.BaseURL, candidate.UpstreamModel = p.Provider, urls[p.Provider], p.UpstreamModelIDs[0]
			candidate.AdapterType = adapters.TypeOpenAICompatible
			candidate.AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`)
			if p.Provider == "doubao" {
				candidate.AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)
			}
			candidate.Capabilities = map[string]capabilitySnapshot{}
			ok, reason := canonicalEligibility(value, candidate, operation, true)
			require.False(t, ok)
			require.Equal(t, "unsupported_capability:reasoning", reason)
			at := value.DBNow.Add(-time.Minute)
			candidate.Capabilities[modelprofile.CapabilityReasoning] = capabilitySnapshot{
				SupportState: "enabled", ID: modelprofile.CapabilityReasoning, VerifiedAt: &at,
				EvidenceState: "live_probed", EvidenceRef: "fixture://reasoning", Revision: 1,
			}
			ok, _ = canonicalEligibility(value, candidate, operation, true)
			require.False(t, ok, "an observation alone grants no production approval")
			approved := candidate.Capabilities[modelprofile.CapabilityReasoning]
			approved.EvidenceState = "production_approved"
			candidate.Capabilities[modelprofile.CapabilityReasoning] = approved
			ok, reason = canonicalEligibility(value, candidate, operation, true)
			require.True(t, ok, reason)
		})
	}
}

func TestDeepSeekInlineJPEGRequiresCurrentVisionApproval(t *testing.T) {
	raw, err := os.ReadFile("../../capability-bundles/deepseek-flash-inline-jpeg.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(raw)
	require.NoError(t, err)
	definition, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	p := definition.Profile()
	value := canonicalFixture()
	value.Definition = definition
	value.ExecutionProfileID, value.ExecutionProfileVersion = p.ID, p.Version
	operation := modelprofile.OperationOpenAIChat
	request := requestFor(ModeCanonical, "vision-fixture")
	request.InvocationCapabilities = []CapabilityRequirement{{CapabilityID: modelprofile.CapabilityVision, Origins: []string{CapabilityOriginExplicitRequest}}}
	value.CapabilityRequirements, err = ResolveCapabilityRequirements(p, request, definition)
	require.NoError(t, err)
	candidate := value.Canonical[0]
	candidate.Manifest = nil
	candidate.ProtocolProfileID, candidate.ProtocolProfileVersion = p.ProtocolProfile, modelprofile.ChatOperationProtocolBindingVersionV1
	candidate.Provider, candidate.BaseURL, candidate.UpstreamModel = p.Provider, "https://api.deepseek.com", p.UpstreamModelIDs[0]
	candidate.AdapterType = adapters.TypeOpenAICompatible
	candidate.AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)
	at := value.DBNow.Add(-time.Minute)
	for _, state := range []string{"", "live_probed", "production_approved"} {
		candidate.Capabilities = map[string]capabilitySnapshot{modelprofile.CapabilityReasoning: {
			SupportState: "enabled", ID: modelprofile.CapabilityReasoning, VerifiedAt: &at, EvidenceState: "production_approved", Revision: 1,
		}}
		if state != "" {
			candidate.Capabilities[modelprofile.CapabilityVision] = capabilitySnapshot{
				SupportState: "enabled", ID: modelprofile.CapabilityVision, VerifiedAt: &at, EvidenceState: state, Revision: 1,
			}
		}
		ok, reason := canonicalEligibility(value, candidate, operation, true)
		require.Equal(t, state == "production_approved", ok, reason)
		if !ok {
			require.Equal(t, "unsupported_capability:vision", reason)
		}
	}
}
