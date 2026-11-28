package admin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	probe "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestInlineJPEGNormalManagementProbe(t *testing.T) {
	type providerWire struct {
		baseURL, pathMode, thinking, effort, limitField, usageTail string
		limit                                                      int
	}
	wires := map[string]providerWire{
		"deepseek":  {"https://api.deepseek.com", "base_relative", "disabled", "", "max_tokens", `,"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":309`, 24},
		"kimi":      {"https://api.moonshot.cn/v1", "canonical_v1", "", "low", "max_completion_tokens", `,"prompt_tokens_details":{"cached_tokens":8,"cache_write_tokens":256}`, 512},
		"minimax":   {"https://api.minimax.cn/v1", "canonical_v1", "adaptive", "low", "max_completion_tokens", `,"prompt_tokens_details":{"cached_tokens":0}`, 512},
		"glm":       {"https://open.bigmodel.cn/api/paas/v4", "base_relative", "enabled", "", "max_tokens", "", 512},
		"dashscope": {"https://dashscope.aliyuncs.com/compatible-mode/v1", "canonical_v1", "", "", "max_completion_tokens", `,"prompt_tokens_details":{"cached_tokens":0}`, 512},
		"mimo":      {"https://api.xiaomimimo.com/v1", "canonical_v1", "", "", "max_completion_tokens", `,"prompt_tokens_details":{"cached_tokens":128,"image_tokens":280}`, 512},
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	secret, err := auth.EncryptKey([]byte("fixture-secret"), key)
	require.NoError(t, err)
	var visualBindings int
	for _, file := range []string{"deepseek-flash-inline-jpeg.json", "bounded-inline-jpeg-chat.json"} {
		wire, err := os.ReadFile("../../capability-bundles/" + file)
		require.NoError(t, err)
		bundle, err := publication.DecodeBundleV2(wire)
		require.NoError(t, err)
		require.NoError(t, probe.ValidateBundleSource(wire))
		definitions := make(map[string]modelprofile.Definition, len(bundle.Definitions))
		for _, raw := range bundle.Definitions {
			d, err := modelprofile.DecodeDefinition(raw)
			require.NoError(t, err)
			definitions[d.Digest()] = d
		}
		for _, row := range bundle.BindingDelta {
			d := definitions[row.ProfileDefinitionDigest]
			fields := probe.Binding{
				OperationID: row.OperationID, CapabilityID: row.CapabilityID, ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion,
				ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion, ProbeContractID: row.ProbeContractID,
				ProbeContractVersion: row.ProbeContractVersion, ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath,
			}
			b, err := probe.BindDefinition(fields, d)
			require.NoError(t, err)
			if row.CapabilityID != modelprofile.CapabilityVision {
				switch row.ExpectedProvider {
				case "glm":
					_, err = probe.BuildGLMStreamFunctionToolsProbeRequest(b)
				case "dashscope":
					_, err = probe.BuildQwenStreamFunctionToolsProbeRequest(b)
				case "mimo":
					_, err = probe.BuildOpenAIChatFunctionToolsProbeRequest(b)
				default:
					_, err = probe.BuildExistingFamilyStreamFunctionToolsProbeRequest(b)
				}
				require.NoError(t, err, row.CapabilityID)
				continue
			}
			visualBindings++
			wire, ok := wires[row.ExpectedProvider]
			require.True(t, ok)
			wrong := fields
			wrong.ExpectedUpstreamModel = "unrelated-image-model"
			_, err = probe.BindDefinition(wrong, d)
			require.Error(t, err)
			snapshot := modelCapabilitySnapshot{
				Definition: d, DataBinding: b, Provider: row.ExpectedProvider, UpstreamModel: row.ExpectedUpstreamModel,
				BaseURL: wire.baseURL, AdapterType: adapters.TypeOpenAICompatible,
				AdapterConfig: json.RawMessage(fmt.Sprintf(`{"endpoint_path_mode":%q}`, wire.pathMode)), EncryptedSecret: secret,
				OperationID: row.OperationID, ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion,
				CapabilityID: row.CapabilityID, ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion,
			}
			scenarios := []string{"matching pixels", "fixed wrong answer", "wrong model", "bad usage", "http failure", "wrong endpoint", "missing binding"}
			if row.ExpectedProvider == "mimo" {
				scenarios = append(scenarios, "missing image usage", "out-of-range image usage")
			}
			for _, scenario := range scenarios {
				t.Run(row.ExpectedUpstreamModel+"/"+scenario, func(t *testing.T) {
					calls := 0
					h := NewHandler(nil, "fixture-jwt", key, nil)
					h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(req *http.Request) (*http.Response, error) {
						calls++
						require.Equal(t, wire.baseURL+"/chat/completions", req.URL.String())
						require.Equal(t, http.MethodPost, req.Method)
						require.Nil(t, req.GetBody)
						var request struct {
							Model         string            `json:"model"`
							Stream        bool              `json:"stream"`
							Max           int               `json:"max_tokens"`
							MaxCompletion int               `json:"max_completion_tokens"`
							Effort        string            `json:"reasoning_effort"`
							Thinking      map[string]string `json:"thinking"`
							Messages      []struct {
								Content []struct {
									Text     string
									ImageURL map[string]string `json:"image_url"`
								}
							} `json:"messages"`
						}
						require.NoError(t, json.NewDecoder(req.Body).Decode(&request))
						require.Equal(t, row.ExpectedUpstreamModel, request.Model)
						require.False(t, request.Stream)
						if wire.limitField == "max_tokens" {
							require.Equal(t, wire.limit, request.Max)
							require.Zero(t, request.MaxCompletion)
						} else {
							require.Equal(t, wire.limit, request.MaxCompletion)
							require.Zero(t, request.Max)
						}
						require.Equal(t, wire.thinking, request.Thinking["type"])
						require.Equal(t, wire.effort, request.Effort)
						parts := request.Messages[0].Content
						url := parts[1].ImageURL["url"]
						data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, "data:image/jpeg;base64,"))
						require.NoError(t, err)
						picture, err := jpeg.Decode(strings.NewReader(string(data)))
						require.NoError(t, err)
						var code strings.Builder
						for y := 24; y < 96; y += 48 {
							for x := 24; x < 144; x += 48 {
								r, g, b, _ := picture.At(x, y).RGBA()
								switch {
								case r > 40000 && g > 40000:
									code.WriteByte('4')
								case r > 40000:
									code.WriteByte('1')
								case g > 40000:
									code.WriteByte('2')
								case b > 40000:
									code.WriteByte('3')
								default:
									t.Fatal("unclassified fixture color")
								}
							}
						}
						answer := code.String()
						require.Len(t, answer, 6)
						require.NotContains(t, parts[0].Text, answer)
						if scenario == "fixed wrong answer" {
							answer = strings.Repeat("1", 6)
							if code.String() == answer {
								answer = strings.Repeat("2", 6)
							}
						}
						model, total, status := row.ExpectedUpstreamModel, 313, 200
						if scenario == "wrong model" {
							model = "unrelated-image-model"
						}
						if scenario == "bad usage" {
							total++
						}
						if scenario == "http failure" {
							status = 429
						}
						reasoning := ""
						if wire.thinking != "disabled" {
							reasoning = `,"reasoning_content":"checked pixel order"`
						}
						usageTail := wire.usageTail
						if scenario == "missing image usage" {
							usageTail = `,"prompt_tokens_details":{"cached_tokens":128}`
						}
						if scenario == "out-of-range image usage" {
							usageTail = strings.Replace(usageTail, `"image_tokens":280`, `"image_tokens":310`, 1)
						}
						body := fmt.Sprintf(`{"model":%q,"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":%q%s}}],"usage":{"prompt_tokens":309,"completion_tokens":4,"total_tokens":%d%s}}`,
							model, answer, reasoning, total, usageTail)
						return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
					}))
					input := snapshot
					if scenario == "wrong endpoint" {
						input.BaseURL = "https://example.invalid"
					}
					if scenario == "missing binding" {
						input.DataBinding = probe.Binding{}
					}
					outcome := h.executeModelCapabilityProbe(t.Context(), input, "fixture")
					if scenario == "wrong endpoint" || scenario == "missing binding" {
						require.Zero(t, calls)
					} else {
						require.Equal(t, 1, calls)
					}
					require.Equal(t, scenario == "matching pixels", outcome.ContractSuccess, outcome)
					if scenario == "matching pixels" {
						require.EqualValues(t, 313, outcome.TotalTokens)
						require.Equal(t, row.ExpectedUpstreamModel, outcome.ReportedModel)
					}
					require.NotContains(t, fmt.Sprint(outcome.RequestSummary), "data:image")
				})
			}
		}
	}
	require.Equal(t, 10, visualBindings)
}
