package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/metering"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestDeepSeekInlineJPEGPublicChat(t *testing.T) {
	image := testJPEGDataURL(t, 32, 32)
	body := `{"model":"public-model","stream":false,"max_tokens":16,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":[{"type":"text","text":"What color is this?"},{"type":"image_url","image_url":{"url":"` + image + `"}}]}]}`
	successor := deepSeekBundleDefinition(t, "deepseek-flash-inline-jpeg.json")
	predecessor := deepSeekCompletionDefinition(t)
	require.Equal(t, predecessor.Digest(), successor.PredecessorDigest())
	for _, tc := range []struct {
		name, body, want string
		d                modelprofile.Definition
	}{
		{"image", body, "", successor},
		{"old flash", body, "image input is not supported", predecessor},
		{"external image URL", strings.Replace(body, image, "https://example.invalid/image.jpg", 1), "only inline", successor},
		{"invalid JPEG", strings.Replace(body, image, "data:image/jpeg;base64,bm90IGEganBlZw==", 1), "valid JPEG", successor},
		{"unsupported PNG", strings.Replace(body, "image/jpeg", "image/png", 1), "MIME type image/png", successor},
		{"thinking", strings.Replace(body, `"type":"disabled"`, `"type":"enabled"`, 1), "requires thinking.type=disabled", successor},
		{"provider default thinking", strings.Replace(body, `"thinking":{"type":"disabled"},`, ``, 1), "requires thinking.type=disabled", successor},
		{"stream", strings.Replace(body, `"stream":false`, `"stream":true,"stream_options":{"include_usage":true}`, 1), "requires stream=false", successor},
		{"tools", strings.Replace(body, `"messages":`, `"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"messages":`, 1), "incompatible with tools", successor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.d.Profile()
			key := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.deepseek.com", key, nil)
			plan.Definition, plan.ExecutionProfileID, plan.ExecutionProfileVersion = tc.d, p.ID, p.Version
			plan.ModelProtocolProfile, plan.ReasoningProfile, plan.CacheUsageProfile = p.ProtocolProfile, p.ReasoningProfile, p.CacheUsageProfile
			plan.Routes[0].Provider, plan.Routes[0].UpstreamModel = p.Provider, p.UpstreamModelIDs[0]
			plan.Routes[0].AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)
			plan.Routes[0].ProtocolProfileID, plan.Routes[0].ProtocolProfileVersion = p.ProtocolProfile, modelprofile.ChatOperationProtocolBindingVersionV1
			var calls, settles int
			billingStub := &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					return &billing.FreezeResult{Success: true, AmountMicros: 1, PricingVersion: "price-v1"}, nil
				},
				settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
					settles++
					require.Equal(t, metering.UsageSourceUpstream, req.UsageSource)
					require.Equal(t, 4, req.CompletionTokens)
					return &billing.SettleResult{Success: true}, nil
				},
			}
			relay := NewRelay(executionTestDB(), nil, nil, billingStub, nil, key)
			planner := &executionPlannerStub{plan: plan}
			coordinator := &executionCoordinatorStub{authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
				digest, err := executionevidence.Digest(route.Evidence)
				return modelexecution.Decision{Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
					ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID}}, err
			}}
			require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()))
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "https://api.deepseek.com/chat/completions", req.URL.String())
				var request map[string]any
				require.NoError(t, json.NewDecoder(req.Body).Decode(&request))
				require.Equal(t, "deepseek-flash", request["model"])
				require.EqualValues(t, 16, request["max_tokens"])
				message := request["messages"].([]any)[0].(map[string]any)
				require.Equal(t, map[string]any{"url": image}, message["content"].([]any)[1].(map[string]any)["image_url"])
				wire := `{"id":"fixture","object":"chat.completion","model":"deepseek-flash","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Green"}}],"usage":{"prompt_tokens":110,"completion_tokens":4,"total_tokens":114,"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":110}}`
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
			})}
			if tc.want != "" {
				_, err := validateChatExecutionProfileRequest([]byte(tc.body), plan)
				require.ErrorContains(t, err, tc.want)
			}
			rec := httptest.NewRecorder()
			relay.HandleChatCompletion(rec, executionRequest(http.MethodPost, "/v1/chat/completions", "fixture-inline-jpeg", tc.body))
			if tc.want != "" {
				require.Equal(t, 400, rec.Code, rec.Body.String())
				require.Zero(t, calls)
				return
			}
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "Green")
			require.Equal(t, 1, calls)
			require.Equal(t, 1, settles)
			require.Contains(t, planner.requests[0].InvocationCapabilities, modelrouting.CapabilityRequirement{CapabilityID: modelprofile.CapabilityVision, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}})
		})
	}
}

func TestBoundedInlineJPEGThroughCanonicalProjectChat(t *testing.T) {
	bundleWire, err := os.ReadFile("../../capability-bundles/bounded-inline-jpeg-chat.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(bundleWire)
	require.NoError(t, err)
	require.Len(t, bundle.Definitions, 9)
	type providerWire struct {
		origin, pathMode, limitField, thinking, usageTail string
	}
	policies := map[string]providerWire{
		"kimi":      {"https://api.moonshot.cn/v1", "canonical_v1", "max_completion_tokens", "", `,"prompt_tokens_details":{"cache_write_tokens":256}`},
		"minimax":   {"https://api.minimax.cn/v1", "canonical_v1", "max_completion_tokens", "adaptive", ""},
		"glm":       {"https://open.bigmodel.cn/api/paas/v4", "base_relative", "max_tokens", "enabled", ""},
		"dashscope": {"https://dashscope.aliyuncs.com/compatible-mode/v1", "canonical_v1", "max_completion_tokens", "", ""},
		"mimo":      {"https://api.xiaomimimo.com/v1", "canonical_v1", "max_completion_tokens", "", `,"prompt_tokens_details":{"cached_tokens":128,"image_tokens":280}`},
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	imageURL := testJPEGDataURL(t, 32, 32)
	for _, raw := range bundle.Definitions {
		d, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		p := d.Profile()
		model := p.UpstreamModelIDs[0]
		t.Run(model, func(t *testing.T) {
			wire, ok := policies[p.Provider]
			require.True(t, ok)
			payload := map[string]any{
				"model": "public-model", "stream": false, wire.limitField: 512,
				"messages": []map[string]any{{"role": "user", "content": []map[string]any{
					{"type": "text", "text": "What is this synthetic image color?"},
					{"type": "image_url", "image_url": map[string]string{"url": imageURL}},
				}}},
			}
			if wire.thinking != "" {
				payload["thinking"] = map[string]string{"type": wire.thinking}
			}
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, wire.origin, key, nil)
			plan.Definition, plan.ExecutionProfileID, plan.ExecutionProfileVersion = d, p.ID, p.Version
			plan.ModelProtocolProfile, plan.ReasoningProfile, plan.CacheUsageProfile = p.ProtocolProfile, p.ReasoningProfile, p.CacheUsageProfile
			plan.Routes[0].Provider, plan.Routes[0].UpstreamModel = p.Provider, model
			plan.Routes[0].AdapterConfig = json.RawMessage(fmt.Sprintf(`{"endpoint_path_mode":%q}`, wire.pathMode))
			plan.Routes[0].ProtocolProfileID, plan.Routes[0].ProtocolProfileVersion = p.ProtocolProfile, modelprofile.ChatOperationProtocolBindingVersionV1
			for _, invalid := range []struct {
				old, replacement, want string
			}{
				{imageURL, "https://example.invalid/image.jpg", "only inline"},
				{"image/jpeg", "image/png", "MIME type image/png"},
				{`"stream":false`, `"stream":true,"stream_options":{"include_usage":true}`, "requires stream=false"},
				{`"messages":`, `"tools":[{"type":"function","function":{"name":"color","description":"Color","parameters":{"type":"object"}}}],"messages":`, "incompatible with tools"},
			} {
				_, err := validateChatExecutionProfileRequest([]byte(strings.Replace(string(body), invalid.old, invalid.replacement, 1)), plan)
				require.ErrorContains(t, err, invalid.want)
			}
			var calls, settlements int
			account := &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					return &billing.FreezeResult{Success: true, AmountMicros: 1, PricingVersion: "price-v1"}, nil
				},
				settleFn: func(_ context.Context, r billing.SettleRequest) (*billing.SettleResult, error) {
					settlements++
					require.Equal(t, metering.UsageSourceUpstream, r.UsageSource)
					require.Equal(t, 309, r.PromptTokens)
					require.Equal(t, 4, r.CompletionTokens)
					return &billing.SettleResult{Success: true}, nil
				},
			}
			relay := NewRelay(executionTestDB(), nil, nil, account, nil, key)
			planner := &executionPlannerStub{plan: plan}
			coordinator := &executionCoordinatorStub{authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
				digest, err := executionevidence.Digest(route.Evidence)
				return modelexecution.Decision{Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
					ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID}}, err
			}}
			require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()))
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, wire.origin+"/chat/completions", req.URL.String())
				var outgoing map[string]any
				require.NoError(t, json.NewDecoder(req.Body).Decode(&outgoing))
				require.Equal(t, model, outgoing["model"])
				require.EqualValues(t, 512, outgoing[wire.limitField])
				parts := outgoing["messages"].([]any)[0].(map[string]any)["content"].([]any)
				require.Equal(t, map[string]any{"url": imageURL}, parts[1].(map[string]any)["image_url"])
				raw := fmt.Sprintf(`{"id":"fixture","object":"chat.completion","model":%q,"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Green","reasoning_content":"synthetic"}}],"usage":{"prompt_tokens":309,"completion_tokens":4,"total_tokens":313%s}}`, model, wire.usageTail)
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(raw))}, nil
			})}
			rec := httptest.NewRecorder()
			relay.HandleChatCompletion(rec, executionRequest(http.MethodPost, "/v1/chat/completions", "fixture-inline-jpeg", string(body)))
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "Green")
			require.Equal(t, 1, calls)
			require.Equal(t, 1, settlements)
			require.Contains(t, planner.requests[0].InvocationCapabilities, modelrouting.CapabilityRequirement{
				CapabilityID: modelprofile.CapabilityVision, Origins: []string{modelrouting.CapabilityOriginExplicitRequest},
			})
		})
	}
}
