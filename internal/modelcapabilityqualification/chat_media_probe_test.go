package modelcapabilityqualification

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestInlineChatMediaBundles(t *testing.T) {
	for _, file := range []string{"qwen-beijing-omni-chat.json", "mimo-26-paygo-inline-media.json", "glm-53-bounded-inline-file.json", "minimax-m31-bounded-inline-video.json", "kimi-qwen27b-bounded-inline-video.json", "qwen-hybrid-bounded-inline-video.json", "glm-53-release-video-sample.json"} {
		wire, err := os.ReadFile("../../capability-bundles/" + file)
		require.NoError(t, err)
		require.NoError(t, ValidateBundleSource(wire))
		bundle, err := publication.DecodeBundleV2(wire)
		require.NoError(t, err)
		definitions := map[string]modelprofile.Definition{}
		for _, raw := range bundle.Definitions {
			d, err := modelprofile.DecodeDefinition(raw)
			require.NoError(t, err)
			definitions[d.Digest()] = d
		}
		for _, row := range bundle.BindingDelta {
			switch row.CapabilityID {
			case modelprofile.CapabilityAudio, modelprofile.CapabilityVideo, modelprofile.CapabilityFiles, modelprofile.CapabilityVision:
			default:
				continue
			}
			t.Run(row.ExpectedUpstreamModel+"/"+row.CapabilityID, func(t *testing.T) {
				b, err := BindDefinition(Binding{OperationID: row.OperationID, CapabilityID: row.CapabilityID, ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion, ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion, ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion, ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath}, definitions[row.ProfileDefinitionDigest])
				require.NoError(t, err)
				answer, details := "", ""
				provider := func(model, answer, details string) string {
					return fmt.Sprintf(`{"model":%q,"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":171,"completion_tokens":6,"total_tokens":177,"prompt_tokens_details":{%s}}}`, model, answer, details)
				}
				switch row.CapabilityID {
				case modelprofile.CapabilityAudio:
					wave, err := os.ReadFile("../endpointprotocol/testdata/deepseek-speech-synthetic.wav")
					require.NoError(t, err)
					req, err := BuildChatInlineWAVProbeRequest(b, wave)
					require.NoError(t, err)
					require.Contains(t, string(req), "data:audio/wav;base64,")
					if row.ExpectedProvider == "dashscope" {
						require.Contains(t, string(req), `"format":"wav"`)
						require.Contains(t, string(req), `"reasoning_effort":"none"`)
					} else {
						require.NotContains(t, string(req), `"format":"wav"`)
						require.Contains(t, string(req), `"thinking":{"type":"disabled"}`)
					}
					reply, usage, _, err := ObserveChatInlineWAV(b, "application/json", strings.NewReader(provider(row.ExpectedUpstreamModel, "27415", `"audio_tokens":19`)))
					require.NoError(t, err)
					require.Equal(t, "27415", reply)
					require.Equal(t, 177, usage.TotalTokens)
					_, _, _, err = ObserveChatInlineWAV(b, "application/json", strings.NewReader(provider(row.ExpectedUpstreamModel, "27415", `"audio_tokens":0`)))
					require.Error(t, err)
				case modelprofile.CapabilityVideo:
					challenge, err := NewChatInlineVideoChallenge(b, strings.Repeat("a", 40))
					require.NoError(t, err)
					var req struct {
						Messages []struct {
							Content []struct {
								Text  string `json:"text"`
								Video struct {
									URL string `json:"url"`
								} `json:"video_url"`
							} `json:"content"`
						} `json:"messages"`
					}
					require.NoError(t, json.Unmarshal(challenge.Body, &req))
					parts := req.Messages[0].Content
					require.NotContains(t, parts[0].Text, challenge.answer)
					if row.ProtocolProfileID == modelprofile.GLM53PublicVideoProtocol {
						sample, _ := modelprofile.ReleaseVideoSampleURL(strings.Repeat("a", 40))
						require.Equal(t, sample, parts[1].Video.URL)
						_, err := NewChatInlineVideoChallenge(b, "main")
						require.Error(t, err)
					} else {
						header, encoded, found := strings.Cut(parts[1].Video.URL, ",")
						require.True(t, found)
						if row.ExpectedUpstreamModel == modelprofile.QwenOmniModel {
							require.Equal(t, "data:;base64", header)
						} else {
							require.Equal(t, "data:video/mp4;base64", header)
						}
						clip, err := base64.StdEncoding.Strict().DecodeString(encoded)
						require.NoError(t, err)
						require.True(t, endpointprotocol.ValidateMP4(clip, endpointprotocol.MP4Requirements{Width: 160, Height: 96, Duration: 6 * time.Second}))
					}
					answer = challenge.answer
					details = `"video_tokens":100`
					_, _, err = challenge.Observe("application/json", strings.NewReader(provider(row.ExpectedUpstreamModel, answer, details)))
					require.NoError(t, err)
					_, _, err = challenge.Observe("application/json", strings.NewReader(provider(row.ExpectedUpstreamModel, "000000", details)))
					require.Error(t, err)
				case modelprofile.CapabilityFiles:
					challenge, err := NewChatInlineTextFileChallenge(b)
					require.NoError(t, err)
					require.NotContains(t, string(challenge.Body), challenge.answer)
					require.Contains(t, string(challenge.Body), "data:text/plain;base64,")
					_, _, err = challenge.Observe("application/json", strings.NewReader(provider(row.ExpectedUpstreamModel, challenge.answer, "")))
					require.NoError(t, err)
					_, _, err = challenge.Observe("application/json", strings.NewReader(provider(row.ExpectedUpstreamModel, "wrong", "")))
					require.Error(t, err)
				case modelprofile.CapabilityVision:
					challenge, err := NewChatInlineJPEGChallenge(b)
					require.NoError(t, err)
					_, _, err = challenge.Observe("application/json", strings.NewReader(provider(row.ExpectedUpstreamModel, challenge.answer, `"image_tokens":128`)))
					require.NoError(t, err)
				}
			})
		}
	}
}
