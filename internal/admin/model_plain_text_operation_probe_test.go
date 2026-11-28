package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	probe "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestOfficialTextOperationProbeExactModelUsageAndEndpoint(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/official-plain-text-chat.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	require.Len(t, bundle.BindingDelta, 7)
	require.Len(t, bundle.SelectorDelta, 7)
	require.Len(t, bundle.Definitions, 10)

	key := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("fixture-only"), key)
	require.NoError(t, err)

	for _, raw := range bundle.Definitions {
		d, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		p := d.Profile()
		model := p.UpstreamModelIDs[0]
		t.Run(model, func(t *testing.T) {
			if p.Provider == "gemini" {
				require.Empty(t, p.Capabilities)
			} else {
				require.Equal(t, []string{modelprofile.CapabilityReasoning}, p.Capabilities)
			}
			base, pathMode, outputField := "", modelprofile.EndpointPathModeCanonicalV1, "max_tokens"
			switch p.Provider {
			case "gemini":
				base = "https://generativelanguage.googleapis.com/v1beta/openai"
				pathMode = modelprofile.EndpointPathModeBaseRelative
			case "hy3":
				base = "https://tokenhub.tencentmaas.com/v1"
			case "xai":
				base = "https://api.x.ai/v1"
			case "doubao":
				base = "https://ark.cn-beijing.volces.com/api/v3"
				pathMode, outputField = modelprofile.EndpointPathModeBaseRelative, "max_completion_tokens"
			case "openai":
				base = "https://api.openai.com/v1"
				outputField = "max_completion_tokens"
			default:
				t.Fatalf("unexpected provider %s", p.Provider)
			}
			snapshot := canonicalOperationProbeSnapshot{
				Definition: d, ProfileDefinitionDigest: d.Digest(),
				Provider: p.Provider, BaseURL: base, AdapterType: adapters.TypeOpenAICompatible,
				AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"` + pathMode + `"}`),
				OperationID:   modelprofile.OperationOpenAIChat, UpstreamModel: model, EncryptedSecret: encrypted,
				ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version,
				ProfileID: p.ProtocolProfile, ProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
			}
			valid := strings.ReplaceAll(`{"id":"fixture","object":"chat.completion","model":"[MODEL]","choices":[{"index":0,"message":{"role":"assistant","content":"Ready."},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20,"prompt_tokens_details":{"cached_tokens":0}}}`, "[MODEL]", model)
			if p.Provider == "xai" {
				valid = strings.Replace(valid, `"total_tokens":20`, `"total_tokens":20,"completion_tokens_details":{"reasoning_tokens":0}`, 1)
			}

			for _, tc := range []struct {
				name, body string
				pass       bool
			}{
				{"exact assistant text", valid, true},
				{"wrong model", strings.ReplaceAll(valid, model, "sibling-model"), false},
				{"duplicate model", strings.Replace(valid, `"model":"`+model+`"`, `"model":"wrong","model":"`+model+`"`, 1), false},
				{"missing usage", strings.Replace(valid, `"usage":`, `"no_usage":`, 1), false},
				{"estimated total", strings.Replace(valid, `"total_tokens":20`, `"unreported_total":20`, 1), false},
				{"usage beyond bound", strings.ReplaceAll(strings.ReplaceAll(valid, `"completion_tokens":8`, `"completion_tokens":257`), `"total_tokens":20`, `"total_tokens":269`), false},
				{"unfinished answer", strings.Replace(valid, `"finish_reason":"stop"`, `"finish_reason":"length"`, 1), false},
				{"no assistant content", strings.Replace(valid, "Ready.", "", 1), false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					h := NewHandler(nil, "fixture-jwt", key, nil)
					calls := 0
					h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(req *http.Request) (*http.Response, error) {
						calls++
						require.Equal(t, http.MethodPost, req.Method)
						require.Equal(t, base+"/chat/completions", req.URL.String())
						var payload map[string]any
						require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
						require.Equal(t, model, payload["model"])
						require.EqualValues(t, exactTextOperationProbeOutputLimit, payload[outputField])
						require.Equal(t, false, payload["stream"])
						require.NotContains(t, payload, "tools")
						if p.Provider == "doubao" || p.Provider == "hy3" {
							require.Equal(t, map[string]any{"type": "disabled"}, payload["thinking"])
						} else {
							require.Equal(t, "low", payload["reasoning_effort"])
						}
						return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
					}))
					result := h.executeCanonicalOperationProbe(t.Context(), snapshot, "Say Ready.", "fixture")
					require.Equal(t, 1, calls)
					require.Equal(t, tc.pass, result.ProviderSuccess, result.ReasonCode)
					if tc.pass {
						require.Equal(t, model, result.ReportedModel)
					}
				})
			}
			wrongEndpoint := snapshot
			wrongEndpoint.BaseURL = "https://example.invalid/v1"
			_, err = canonicalOperationProbeSpecForSnapshot(wrongEndpoint)
			require.ErrorIs(t, err, errCanonicalOperationProbeUnsupported)
			rows := 0
			for _, row := range bundle.BindingDelta {
				if row.ProfileDefinitionDigest != d.Digest() {
					continue
				}
				rows++
				t.Run("reasoning qualification", func(t *testing.T) {
					assertPlainChatReasoningProbe(t, d, row, snapshot, outputField, valid, key)
				})
			}
			require.Len(t, p.Capabilities, rows)
		})
	}
}

func assertPlainChatReasoningProbe(t *testing.T, d modelprofile.Definition, row publication.DefinitionBindingV2, operation canonicalOperationProbeSnapshot,
	outputField, valid string, key []byte) {
	t.Helper()
	binding, err := probe.BindDefinition(probe.Binding{
		OperationID: row.OperationID, CapabilityID: row.CapabilityID,
		ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion,
		ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion,
		ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion,
		ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath,
	}, d)
	require.NoError(t, err)
	require.Equal(t, modelprofile.CapabilityReasoning, binding.CapabilityID)
	snapshot := modelCapabilitySnapshot{
		Definition: d, DataBinding: binding,
		Provider: operation.Provider, BaseURL: operation.BaseURL, UpstreamModel: operation.UpstreamModel,
		AdapterType: operation.AdapterType, AdapterConfig: operation.AdapterConfig, EncryptedSecret: operation.EncryptedSecret,
		OperationID: row.OperationID, ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion,
		ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion,
	}
	xai := operation.Provider == "xai"
	wantTotal := 20
	if xai {
		wantTotal = 498
	}
	withTokens := func(n string) string {
		if xai {
			value, _ := strconv.Atoi(n)
			answer := strings.Replace(valid, `"reasoning_tokens":0`, `"reasoning_tokens":`+n, 1)
			return strings.Replace(answer, `"total_tokens":20`, `"total_tokens":`+strconv.Itoa(20+value), 1)
		}
		return strings.Replace(valid, `"total_tokens":20`, `"total_tokens":20,"completion_tokens_details":{"reasoning_tokens":`+n+`}`, 1)
	}
	withText := strings.Replace(valid, `"content":"Ready."`, `"content":"Ready.","reasoning_content":"A provider-supplied reasoning fixture."`, 1)
	positive := withTokens("3")
	if xai {
		positive = strings.ReplaceAll(valid, `"prompt_tokens":12,"completion_tokens":8,"total_tokens":20`, `"prompt_tokens":26,"completion_tokens":168,"total_tokens":498`)
		positive = strings.Replace(positive, `"reasoning_tokens":0`, `"reasoning_tokens":304`, 1)
		positive = strings.Replace(positive, `"cached_tokens":0`, `"cached_tokens":4`, 1)
	}
	for _, tc := range []struct {
		name, body string
		pass       bool
	}{
		{"positive provider partition", positive, true},
		{"no evidence", valid, false},
		{"provider-specific reasoning text", withText, operation.Provider == "hy3"},
		{"zero", withTokens("0"), false},
		{"negative", withTokens("-1"), false},
		{"null", withTokens("null"), false},
		{"more than inclusive output", withTokens("1030"), false},
		{"wrong model", strings.Replace(withTokens("3"), operation.UpstreamModel, "other-model", 1), false},
		{"wrong total", strings.Replace(positive, `"total_tokens":`+strconv.Itoa(wantTotal), `"total_tokens":`+strconv.Itoa(wantTotal+1), 1), false},
		{"truncated reasoning", strings.Replace(withTokens("3"), `"finish_reason":"stop"`, `"finish_reason":"length"`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(nil, "fixture-jwt", key, nil)
			calls := 0
			h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, operation.BaseURL+"/chat/completions", req.URL.String())
				var body map[string]any
				require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
				require.Equal(t, operation.UpstreamModel, body["model"])
				require.EqualValues(t, probe.PlainChatReasoningOutputLimit, body[outputField])
				require.Equal(t, false, body["stream"])
				require.NotContains(t, body, "thinking")
				require.NotContains(t, body, "reasoning_effort")
				require.NotContains(t, body, "tools")
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}))
			wrongEndpoint := snapshot
			wrongEndpoint.BaseURL = "https://example.invalid/v1"
			require.False(t, h.executeModelCapabilityProbe(t.Context(), wrongEndpoint, "wrong").ContractSuccess)
			require.Zero(t, calls)
			result := h.executeModelCapabilityProbe(t.Context(), snapshot, "fixture")
			require.Equal(t, 1, calls)
			require.Equal(t, tc.pass, result.ContractSuccess, result.ReasonCode)
			require.Equal(t, tc.pass, result.ReasoningObserved)
			if tc.pass {
				require.Equal(t, operation.UpstreamModel, result.ReportedModel)
				require.EqualValues(t, wantTotal, result.TotalTokens)
			}
		})
	}
}
