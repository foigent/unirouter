package admin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestNativeClientToolProbeForEveryExactProfile(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/native-responses-messages-client-tools.json")
	require.NoError(t, err)
	require.NoError(t, modelcapabilityqualification.ValidateBundleSource(wire))
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	require.Len(t, bundle.Definitions, 5)
	for _, raw := range bundle.Definitions {
		def, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		t.Run(def.Profile().ID, func(t *testing.T) { testNativeClientToolProbe(t, def) })
	}
}

func testNativeClientToolProbe(t *testing.T, definition modelprofile.Definition) {
	t.Helper()
	p := definition.Profile()
	base, typ, publicPath := "https://api.openai.com", adapters.TypeOpenAICompatible, "/v1/responses"
	if p.Provider == "anthropic" {
		base, typ, publicPath = "https://api.anthropic.com", adapters.TypeAnthropicNative, "/v1/messages"
	}
	key := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("fixture-only"), key)
	require.NoError(t, err)
	snap := modelCapabilitySnapshot{
		Provider: p.Provider, ExpectedProvider: p.Provider, BaseURL: base, AdapterType: typ,
		AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`), EncryptedSecret: encrypted,
		UpstreamModel: p.UpstreamModelIDs[0], ExpectedUpstreamModel: p.UpstreamModelIDs[0],
		OperationID: p.Operations[0], ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version,
		ProtocolProfileID: p.ProtocolProfile, ProtocolProfileVersion: p.ProtocolProfileVersion,
		ProbeContractID: modelcapabilityqualification.NativeToolProbeContractID, ProbeContractVersion: "1.0.0",
		PublicPath: publicPath, CapabilityID: modelprofile.CapabilityFunctionTools,
	}
	if p.Provider == "anthropic" {
		snap.AdapterConfig = json.RawMessage(`{}`)
	}
	bind := func(member modelCapabilitySnapshot) modelCapabilitySnapshot {
		binding, err := modelcapabilityqualification.BindDefinition(modelCapabilityProbeBindingFromSnapshot(member), definition)
		require.NoError(t, err)
		member.Definition, member.DataBinding = definition, binding
		return member
	}
	members := []modelCapabilitySnapshot{}
	for _, cap := range p.Capabilities {
		member := snap
		member.CapabilityID = cap
		members = append(members, bind(member))
	}
	snap = bind(snap)
	groups, separate, err := groupCapabilitySnapshots(members)
	require.NoError(t, err)
	require.Empty(t, separate)
	require.Len(t, groups, 1)
	require.Equal(t, 2, groups[0].ProviderRequests)
	require.Len(t, groups[0].CapabilityIDs, 4)
	for _, tc := range []struct {
		name    string
		calls   int
		success bool
	}{
		{"both legs", 2, true},
		{"wrong model", 1, false},
		{"wrong arguments", 1, false},
		{"no tool use", 1, false},
		{"missing reasoning evidence", 1, false},
		{"overlap", 1, false},
		{"unapproved cache retention", 1, false},
		{"ambiguous HTTP", 1, false},
		{"ambiguous second leg", 2, false},
		{"correlation round trip missing", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewHandler(nil, "fixture-jwt", key, nil)
			calls := 0
			handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodPost, req.Method)
				require.Equal(t, base+publicPath, req.URL.String())
				require.Nil(t, req.GetBody)
				require.LessOrEqual(t, req.ContentLength, int64(nativeToolProbeBodyLimit))
				var body map[string]json.RawMessage
				require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
				require.JSONEq(t, fmt.Sprintf("%q", snap.UpstreamModel), string(body["model"]))
				require.Equal(t, "false", string(body["stream"]))
				ref := ""
				if calls == 1 && tc.name == "ambiguous HTTP" || calls == 2 && tc.name == "ambiguous second leg" {
					return nil, errors.New("synthetic transport failure")
				}
				if p.Provider == "openai" {
					require.Equal(t, "1024", string(body["max_output_tokens"]))
					require.Equal(t, "false", string(body["store"]))
					require.JSONEq(t, `["reasoning.encrypted_content"]`, string(body["include"]))
					if calls == 2 {
						var items []json.RawMessage
						require.NoError(t, json.Unmarshal(body["input"], &items))
						require.Len(t, items, 4)
						require.JSONEq(t, `{"id":"rs_fixture","type":"reasoning","summary":[],"encrypted_content":"opaque-openai"}`, string(items[1]))
						require.JSONEq(t, `{"type":"function_call","id":"fc_fixture","name":"unirouter_probe_echo","call_id":"call_fixture","status":"completed","arguments":"{\"key\":\"check\"}"}`, string(items[2]))
						var output struct {
							CallID string `json:"call_id"`
							Output string `json:"output"`
						}
						require.NoError(t, json.Unmarshal(items[3], &output))
						require.Equal(t, "call_fixture", output.CallID)
						ref = output.Output
					}
				} else {
					require.Equal(t, "1024", string(body["max_tokens"]))
					require.JSONEq(t, `{"type":"auto","disable_parallel_tool_use":true}`, string(body["tool_choice"]))
					require.Empty(t, body["thinking"])
					if calls == 2 {
						var messages []struct {
							Role    string
							Content json.RawMessage
						}
						require.NoError(t, json.Unmarshal(body["messages"], &messages))
						require.Len(t, messages, 3)
						require.Equal(t, "assistant", messages[1].Role)
						require.JSONEq(t, `[{"type":"thinking","thinking":"","signature":"opaque-anthropic"},{"type":"tool_use","id":"toolu_fixture","name":"unirouter_probe_echo","input":{"key":"check"}}]`, string(messages[1].Content))
						var output []struct {
							ToolID  string `json:"tool_use_id"`
							Content string
						}
						require.NoError(t, json.Unmarshal(messages[2].Content, &output))
						require.Len(t, output, 1)
						require.Equal(t, "toolu_fixture", output[0].ToolID)
						ref = output[0].Content
					}
				}
				if calls == 2 {
					require.Regexp(t, `^UR_[a-f0-9]{16}$`, ref)
					if tc.name == "correlation round trip missing" {
						ref = "unrelated"
					}
				}
				response := nativeClientToolFixture(snap.UpstreamModel, p.Provider, calls == 1 && tc.name != "no tool use", calls, ref)
				if tc.name == "wrong model" {
					response = strings.Replace(response, snap.UpstreamModel, "wrong-model", 1)
				}
				if tc.name == "wrong arguments" {
					response = strings.Replace(response, "check", "wrong", 1)
				}
				if tc.name == "unapproved cache retention" {
					if p.Provider == "openai" {
						response = strings.Replace(response, `"ttl":"30m"`, `"ttl":"12h"`, 1)
					} else {
						response = strings.Replace(response, `"cache_read_input_tokens":4`, `"cache_read_input_tokens":-4`, 1)
					}
				}
				if tc.name == "missing reasoning evidence" {
					response = strings.Replace(response, `"reasoning_tokens":5`, `"reasoning_tokens":0`, 1)
					response = strings.Replace(response, `{"type":"thinking","thinking":"","signature":"opaque-anthropic"},`, "", 1)
				}
				if tc.name == "overlap" {
					response = strings.Replace(response, `"cache_write_tokens":2`, `"cache_write_tokens":30`, 1)
					if p.Provider == "anthropic" {
						response = strings.Replace(response, `"cache_read_input_tokens":4`, `"cache_read_input_tokens":-4`, 1)
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
			}))
			result := handler.executeModelCapabilityProbe(t.Context(), snap, "fixture-native")
			require.Equal(t, tc.calls, calls, result.ReasonCode)
			require.Equal(t, tc.success, result.ContractSuccess, result.ReasonCode)
			require.Equal(t, tc.name == "ambiguous HTTP" || tc.name == "ambiguous second leg", result.OutcomeAmbiguous)
			if tc.success {
				require.EqualValues(t, 24, result.PromptTokens)
				require.EqualValues(t, 16, result.CompletionTokens)
				require.EqualValues(t, 40, result.TotalTokens)
				require.Equal(t, snap.UpstreamModel, result.ReportedModel)
			}
			summary, err := json.Marshal(result.RequestSummary)
			require.NoError(t, err)
			require.NotContains(t, string(summary), "opaque-")
			require.NotContains(t, string(summary), "UR_")
			require.NotContains(t, string(summary), "fixture-only")
		})
	}
}

func nativeClientToolFixture(model, provider string, tool bool, leg int, text string) string {
	if provider == "openai" {
		output := fmt.Sprintf(`{"type":"message","id":"msg_fixture","status":"completed","role":"assistant","content":[{"type":"output_text","text":%q}]}`, text)
		if tool {
			output = `{"id":"rs_fixture","type":"reasoning","summary":[],"encrypted_content":"opaque-openai"},{"type":"function_call","id":"fc_fixture","name":"unirouter_probe_echo","call_id":"call_fixture","status":"completed","arguments":"{\"key\":\"check\"}"}`
		}
		return fmt.Sprintf(`{"id":"resp_%d","object":"response","status":"completed","model":%q,"error":null,"incomplete_details":null,"output":[%s],"prompt_cache_options":{"mode":"implicit","ttl":"30m","comparison_response_id":null},"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":4,"cache_write_tokens":2},"output_tokens":8,"output_tokens_details":{"reasoning_tokens":5},"total_tokens":20}}`,
			leg, model, output)
	}
	const thinking = `{"type":"thinking","thinking":"","signature":"opaque-anthropic"}`
	output, stop := thinking+fmt.Sprintf(`,{"type":"text","text":%q}`, text), "end_turn"
	if tool {
		output, stop = thinking+`,{"type":"tool_use","id":"toolu_fixture","name":"unirouter_probe_echo","input":{"key":"check"}}`, "tool_use"
	}
	return fmt.Sprintf(`{"id":"msg_%d","type":"message","role":"assistant","model":%q,"content":[%s],"stop_reason":%q,"stop_sequence":null,"usage":{"input_tokens":6,"cache_creation_input_tokens":2,"cache_read_input_tokens":4,"output_tokens":8}}`,
		leg, model, output, stop)
}

func TestNativeProbeHTTPClientHasNoAutomaticReplayPath(t *testing.T) {
	client := newSingleSendProbeHTTPClient(canonicalOperationProbeTimeout)
	transport := client.Transport.(*http.Transport)
	require.True(t, transport.Protocols.HTTP1())
	require.False(t, transport.Protocols.HTTP2())
	require.True(t, transport.DisableKeepAlives)
	require.ErrorIs(t, client.CheckRedirect(&http.Request{URL: &url.URL{}}, nil), http.ErrUseLastResponse)
}

// Native continuation dispatches through the two-leg executor before the
// ordinary one-response validator. Exercise that actual entry for each row.
func requireNativeBindingReachesProbeTransport(t *testing.T, snapshot modelCapabilitySnapshot, b modelcapabilityqualification.Binding) {
	t.Helper()
	snapshot.Provider, snapshot.UpstreamModel = b.ExpectedProvider, b.ExpectedUpstreamModel
	snapshot.OperationID, snapshot.ProtocolProfileID, snapshot.ProtocolProfileVersion = b.OperationID, b.ProtocolProfileID, b.ProtocolProfileVersion
	switch snapshot.Provider {
	case "openai":
		snapshot.AdapterType = adapters.TypeOpenAICompatible
		snapshot.BaseURL = "https://api.openai.com"
		snapshot.AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`)
	case "anthropic":
		snapshot.AdapterType = adapters.TypeAnthropicNative
		snapshot.BaseURL = "https://api.anthropic.com"
		snapshot.AdapterConfig = json.RawMessage(`{}`)
	default:
		t.Fatalf("unexpected native profile provider %q", snapshot.Provider)
	}
	key := []byte("0123456789abcdef01234567")
	var err error
	snapshot.EncryptedSecret, err = auth.EncryptKey([]byte("fixture-only"), key)
	require.NoError(t, err)
	h := NewHandler(nil, "fixture-jwt", key, nil)
	if b.ProbeContractID == modelcapabilityqualification.NativeInlineJPEGProbeContractID {
		requireNativeJPEGProbeBindsTheExpectedAnswer(t, h, snapshot, b)
		return
	}
	requests := 0
	h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, snapshot.BaseURL+b.PublicPath, request.URL.String())
		require.Equal(t, http.MethodPost, request.Method)
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}))
	outcome := h.executeModelCapabilityProbe(t.Context(), snapshot, "fixture")
	require.Equal(t, 1, requests)
	require.Equal(t, "provider_http_error", outcome.ReasonCode)
	require.Equal(t, http.StatusTooManyRequests, outcome.StatusCode)
	require.False(t, outcome.ContractSuccess)
}

func requireNativeJPEGProbeBindsTheExpectedAnswer(t *testing.T, h *Handler, snapshot modelCapabilitySnapshot, b modelcapabilityqualification.Binding) {
	t.Helper()
	for _, correct := range []bool{true, false} {
		calls := 0
		h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			require.Equal(t, snapshot.BaseURL+b.PublicPath, request.URL.String())
			require.Equal(t, http.MethodPost, request.Method)
			require.Nil(t, request.GetBody)
			var wire map[string]json.RawMessage
			require.NoError(t, json.NewDecoder(request.Body).Decode(&wire))
			answer := nativeJPEGFixtureAnswer(t, b.ExpectedProvider, wire)
			if !correct {
				answer = "0" + answer[1:]
			}
			response := nativeClientToolFixture(b.ExpectedUpstreamModel, b.ExpectedProvider, false, 1, answer)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(response)),
			}, nil
		}))
		outcome := h.executeModelCapabilityProbe(t.Context(), snapshot, "fixture")
		require.Equal(t, 1, calls)
		require.Equal(t, http.StatusOK, outcome.StatusCode)
		require.Equal(t, correct, outcome.ContractSuccess, outcome.ReasonCode)
		if !correct {
			require.Equal(t, "provider_inline_jpeg_evidence_invalid", outcome.ReasonCode)
		}
	}
}

func nativeJPEGFixtureAnswer(t *testing.T, provider string, body map[string]json.RawMessage) string {
	t.Helper()
	field := "input"
	if provider == "anthropic" {
		field = "messages"
	}
	var messages []struct {
		Content []struct {
			Text, Type string
			ImageURL   string `json:"image_url"`
			Source     struct{ Data string }
		}
	}
	require.NoError(t, json.Unmarshal(body[field], &messages))
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Content, 2)
	text, imagePart := messages[0].Content[0], messages[0].Content[1]
	data := imagePart.Source.Data
	if provider == "openai" {
		data = strings.TrimPrefix(imagePart.ImageURL, "data:image/jpeg;base64,")
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	require.NoError(t, err)
	picture, err := jpeg.Decode(bytes.NewReader(decoded))
	require.NoError(t, err)
	var answer strings.Builder
	for y := 24; y < 96; y += 48 {
		for x := 24; x < 144; x += 48 {
			r, g, b, _ := picture.At(x, y).RGBA()
			switch {
			case r > 40000 && g > 40000:
				answer.WriteByte('4')
			case r > 40000:
				answer.WriteByte('1')
			case g > 40000:
				answer.WriteByte('2')
			case b > 40000:
				answer.WriteByte('3')
			default:
				t.Fatal("unclassified fixture color")
			}
		}
	}
	require.NotContains(t, text.Text, answer.String())
	return answer.String()
}

func TestCapabilityProbePlanDoesNotInventCountForUnknownContracts(t *testing.T) {
	for _, id := range []string{"", "unirouter.capability-probe.unavailable.v9", modelcapabilityqualification.NativeToolProbeContractID, modelcapabilityqualification.NativeInlineJPEGProbeContractID} {
		binding := modelcapabilityqualification.Binding{ProbeContractID: id, ProbeContractVersion: "1.0.0"}
		require.Zero(t, plannedCapabilityProbeProviderRequests(binding))
	}
}
