package gateway

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestCurrentKimiChatStreamingRequiresTerminalUsageRequest(t *testing.T) {
	profiles := []struct {
		id, version string
	}{
		{"kimi.k2.6.api", "1.7.0"},
		{"kimi.k2.7-code.api", "1.3.0"},
		{"kimi.k3.api", "1.9.0"},
	}
	for _, profile := range profiles {
		plan := executionProfilePlan(t, profile.id, profile.version)
		plan.ModelOperationID = modelprofile.OperationOpenAIChat
		for _, test := range []struct {
			name, body, normalization, wantErr string
		}{
			{name: "omitted", body: `{"model":"kimi","messages":[],"stream":true}`, normalization: "stream_options.include_usage=omitted->true"},
			{name: "null", body: `{"model":"kimi","messages":[],"stream":true,"stream_options":null}`, normalization: "stream_options.include_usage=omitted->true"},
			{name: "explicit true", body: `{"model":"kimi","messages":[],"stream":true,"stream_options":{"include_usage":true}}`},
			{name: "explicit false", body: `{"model":"kimi","messages":[],"stream":true,"stream_options":{"include_usage":false}}`, wantErr: "include_usage=false is not supported"},
			{name: "wrong type", body: `{"model":"kimi","messages":[],"stream":true,"stream_options":{"include_usage":"true"}}`, wantErr: "include_usage must be a boolean"},
			{name: "non-stream", body: `{"model":"kimi","messages":[]}`},
		} {
			t.Run(profile.id+"/"+test.name, func(t *testing.T) {
				evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
				if test.wantErr != "" {
					require.ErrorContains(t, err, test.wantErr)
					return
				}
				require.NoError(t, err)
				require.Equal(t, test.normalization, strings.Join(evidence.ProviderRequestNormalizations, ","))
				if test.normalization != "" {
					normalized, normalizeErr := evidence.normalizeProviderRequest([]byte(test.body))
					require.NoError(t, normalizeErr)
					require.JSONEq(t, `{"model":"kimi","messages":[],"stream":true,"stream_options":{"include_usage":true}}`, string(normalized))
				}
			})
		}
	}
}

func TestHistoricalKimiChatProfileDoesNotGainCurrentStreamingAuthority(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k3.api", "1.8.0")
	plan.ModelOperationID = modelprofile.OperationOpenAIChat
	evidence, err := validateChatExecutionProfileRequest(
		[]byte(`{"model":"kimi-k3","messages":[],"stream":true}`), plan,
	)
	require.NoError(t, err)
	require.Empty(t, evidence.ProviderRequestNormalizations)
}

func TestCurrentKimiChatStreamingNormalizesProviderPayloadThroughPlanEvidence(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.moonshot.cn/v1", key, nil)
	setExecutionTestProfile(t, &plan, "kimi.k2.6.api", "1.7.0")
	plan.Routes[0].Provider = "kimi"
	plan.Routes[0].UpstreamModel = "kimi-k2.6"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, key)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
	))

	explicitOutput := 64
	body := []byte(`{"model":"public-kimi","messages":[{"role":"user","content":"hello"}],"stream":true,"max_completion_tokens":64}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-kimi", "public-kimi", "req-kimi-stream-normalization",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"stream_options.include_usage=omitted->true"}, batch.profileEvidence.ProviderRequestNormalizations)
	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"kimi-k2.6","messages":[{"role":"user","content":"hello"}],"stream":true,"stream_options":{"include_usage":true},"max_completion_tokens":64}`, string(actual))
}
