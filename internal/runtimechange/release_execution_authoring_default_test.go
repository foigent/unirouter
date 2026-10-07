package runtimechange

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/releaseexecution"
)

func TestNewDeepSeekFlashDataReleaseRequiresDefaultEffortPerRoute(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/deepseek-streamed-tools.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	var p modelprofile.Profile
	for _, raw := range bundle.Definitions {
		d, e := modelprofile.DecodeDefinition(raw)
		require.NoError(t, e)
		if d.Profile().ID == "deepseek.v4-flash.api" {
			p = d.Profile()
		}
	}
	require.Equal(t, "1.6.0", p.Version)
	require.Equal(t, []string{modelprofile.CapabilityReasoningEffort}, requiredReleaseCapabilityPins(p))
	routes := []DeploymentRoute{{DeploymentID: 1}, {DeploymentID: 2}}
	pins := []releaseexecution.CapabilityPin{{DeploymentID: 1, CapabilityID: modelprofile.CapabilityReasoningEffort}}
	require.ErrorContains(t, validateRequiredReleasePins(p, routes, nil), "reasoning_effort missing")
	require.ErrorContains(t, validateRequiredReleasePins(p, routes, pins), "reasoning_effort missing", "one route cannot authorize another")
	pins = append(pins, releaseexecution.CapabilityPin{DeploymentID: 2, CapabilityID: modelprofile.CapabilityReasoningEffort})
	require.NoError(t, validateRequiredReleasePins(p, routes, pins))
	p.AlwaysRequiredCapabilities = []string{modelprofile.CapabilityReasoningEffort}
	require.Equal(t, p.AlwaysRequiredCapabilities, requiredReleaseCapabilityPins(p), "do not require duplicate pins")
	p.LegacyProtocolAuthority = true
	p.AlwaysRequiredCapabilities = nil
	require.Empty(t, requiredReleaseCapabilityPins(p))
	p.LegacyProtocolAuthority = false
	p.ReasoningProfile = "reasoning.unknown"
	require.Empty(t, requiredReleaseCapabilityPins(p))
	p.ReasoningProfile = "reasoning.deepseek-v4-flash-thinking-toggle.v5"
	p.Version = "1.5.0"
	require.Equal(t, []string{modelprofile.CapabilityReasoningEffort}, requiredReleaseCapabilityPins(p), "a future Profile version cannot omit the same default requirement")
	other, ok := modelprofile.Lookup("glm.5.3.api", "1.9.0")
	require.True(t, ok)
	require.Equal(t, []string{modelprofile.CapabilityReasoning}, requiredReleaseCapabilityPins(other), "new data Chat authoring uses the same default rule")
	inactive, ok := modelprofile.Lookup("hy3.api", "1.2.0")
	require.True(t, ok)
	require.Empty(t, requiredReleaseCapabilityPins(inactive))
}
