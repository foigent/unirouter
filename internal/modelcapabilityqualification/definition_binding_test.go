package modelcapabilityqualification

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestDataProbeRevisionUsesExistingImplementationAndRejectsChangedPins(t *testing.T) {
	glm, err := Resolve("openai.chat_completions.v1", "function_tools", "glm.5.3.api", "1.9.0", "protocol.glm-openai-chat.v7", "1.0.0")
	require.NoError(t, err)
	for _, fixture := range []struct {
		binding Binding
		path    string
	}{
		{glm, "../adapters/testdata/glm_stream_tools.sse"},
		{m27StreamTestBinding(), "../adapters/testdata/minimax_m27_stream_tools.sse"},
	} {
		t.Run(fixture.binding.ExpectedProvider, func(t *testing.T) {
			b := fixture.binding
			p, ok := modelprofile.Lookup(b.ExecutionProfileID, b.ExecutionProfileVersion)
			require.True(t, ok)
			legacy, err := modelprofile.NewDefinition(p, p.ID, "")
			require.NoError(t, err)
			p.Version = "99.0.0"
			d, err := modelprofile.NewDefinition(p, p.ID, legacy.Digest())
			require.NoError(t, err)
			b.ExecutionProfileVersion = p.Version
			_, compiled := modelprofile.Lookup(p.ID, p.Version)
			require.False(t, compiled)
			_, err = BuildOpenAIChatFunctionToolsProbeRequest(b)
			require.Error(t, err, "a new address alone is never an implementation")
			b, err = BindDefinition(b, d)
			require.NoError(t, err)
			body, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
			require.NoError(t, err)
			oldBody, err := BuildOpenAIChatFunctionToolsProbeRequest(fixture.binding)
			require.NoError(t, err)
			require.JSONEq(t, string(oldBody), string(body))
			wire, err := os.ReadFile(fixture.path)
			require.NoError(t, err)
			old, err := ValidateOpenAIChatStreamFunctionToolsForBinding(fixture.binding, "text/event-stream", strings.NewReader(string(wire)))
			require.NoError(t, err)
			actual, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(string(wire)))
			require.NoError(t, err)
			require.Equal(t, old, actual)
			for _, mutate := range []func(*Binding){
				func(b *Binding) { b.ExpectedUpstreamModel += "-other" },
				func(b *Binding) { b.ProbeContractVersion = "99.0.0" },
				func(b *Binding) { b.CapabilityID = "agent_ready" },
				func(b *Binding) { b.ExecutionProfileVersion = "98.0.0" },
			} {
				bad := b
				mutate(&bad)
				_, err := BuildOpenAIChatFunctionToolsProbeRequest(bad)
				require.Error(t, err)
			}
		})
	}
}
