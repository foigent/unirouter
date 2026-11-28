package endpointprotocol

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicNativeInlineJPEGExactRequest(t *testing.T) {
	var encoded bytes.Buffer
	require.NoError(t, jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 48, 48)), nil))
	data := base64.StdEncoding.EncodeToString(encoded.Bytes())
	for _, v := range []struct {
		name, requestKey, outputKey, textType, imageType, origin, path, response string
		imagePart                                                                map[string]any
		profile, old                                                             PublicNonStreamProfile
	}{
		{"responses", "input", "max_output_tokens", "input_text", "input_image", "https://api.openai.com", "/v1/responses",
			`{"id":"resp_fixture","object":"response","status":"completed","model":"provider-model","error":null,"incomplete_details":null,"output":[{"id":"msg_fixture","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"red"}]}],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":3},"output_tokens":4,"total_tokens":16},"prompt_cache_options":{"mode":"implicit","ttl":"30m"}}`,
			map[string]any{"type": "input_image", "image_url": inlineJPEGDataURLPrefix + data},
			publicResponsesInlineJPEGProfile(), (openAIResponsesToolsProfileV3{}).publicProfile()},
		{"messages", "messages", "max_tokens", "text", "image", "https://api.anthropic.com", "/v1/messages",
			`{"id":"msg_fixture","type":"message","role":"assistant","model":"provider-model","content":[{"type":"text","text":"red"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":12,"output_tokens":4}}`,
			map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/jpeg", "data": data}},
			publicMessagesInlineJPEGProfile(), publicAnthropicMessagesProfile{base: anthropicMessagesProfile{}, identity: anthropicMessagesV3PublicIdentity()}},
	} {
		t.Run(v.name, func(t *testing.T) {
			profile, err := NewPublicNonStreamRegistry().Resolve(v.profile.Descriptor().Identity)
			require.NoError(t, err)
			contract := PublicRequestContract{ExecutionProfileID: "fixture.native-vision", ExecutionProfileVersion: "1.0.0"}
			if v.name == "responses" {
				contract.Reasoning = &PublicReasoningRequestContract{
					RequestObjectField: "reasoning", EffortField: "effort", AllowedEfforts: []string{"low", "medium"},
				}
			} else {
				contract.AnthropicMessages = &PublicAnthropicMessagesContract{
					RequireFinalUserMessage: true, AllowedResponseStops: []string{"end_turn", "max_tokens"},
					Reasoning: &PublicAnthropicReasoningRequestContract{
						ThinkingObjectField: "thinking", ThinkingTypeField: "type", AllowedThinkingTypes: []string{"adaptive"},
						ThinkingProviderDefault: "adaptive", ResponseThinkingTextMode: "omitted",
					},
				}
			}
			parts := []map[string]any{v.imagePart, {"type": v.textType, "text": "read this image"}}
			envelope := map[string]any{"model": "public-vision", v.outputKey: 64,
				v.requestKey: []map[string]any{{"role": "user", "content": parts}}}
			marshal := func() []byte {
				wire, err := json.Marshal(envelope)
				require.NoError(t, err)
				return wire
			}
			wire := marshal()
			_, err = v.old.PrepareRequest(wire, "public-vision", contract)
			require.Error(t, err)
			_, err = profile.PrepareRequest(wire, "public-vision")
			require.ErrorContains(t, err, "native_inline_jpeg_reasoning_contract_missing")
			prepared, err := profile.PrepareRequest(wire, "public-vision", contract)
			require.NoError(t, err)
			admission := prepared.Admission()
			require.EqualValues(t, 48*48, admission.InlineImagePixels)
			require.Equal(t, []PublicText{{Path: v.requestKey + "[0].content[1].text", Text: "read this image"}}, admission.Texts)
			redacted, err := prepared.WithTexts([]string{"read the blocks"})
			require.NoError(t, err)
			upstream, err := profile.BuildUpstream(t.Context(), redacted, PublicUpstreamInput{
				BaseURL: v.origin, APIKey: "only-synthetic", RequestID: "req-vision", MappedModel: "provider-model"})
			require.NoError(t, err)
			request, err := upstream.NewHTTPRequest(t.Context())
			require.NoError(t, err)
			require.Equal(t, v.path, request.URL.Path)
			content, err := io.ReadAll(request.Body)
			request.Body.Close()
			require.NoError(t, err)
			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(content, &body))
			require.Equal(t, "provider-model", mustJSONString(body["model"]))
			var turn []struct {
				Content []json.RawMessage `json:"content"`
			}
			require.NoError(t, json.Unmarshal(body[v.requestKey], &turn))
			require.Len(t, turn, 1)
			require.Len(t, turn[0].Content, 2)
			exactImage, err := json.Marshal(v.imagePart)
			require.NoError(t, err)
			require.JSONEq(t, string(exactImage), string(turn[0].Content[0]))
			require.Contains(t, string(turn[0].Content[1]), "read the blocks")
			require.NotContains(t, string(content), "read this image")
			delivery, err := upstream.ParseDelivery(t.Context(), 200, []byte(v.response))
			require.NoError(t, err)
			require.EqualValues(t, 4, delivery.CustomerUsage().OutputTokens)
			if v.name == "responses" {
				require.EqualValues(t, 2, delivery.CustomerUsage().CacheReadTokens)
				require.EqualValues(t, 3, delivery.CustomerUsage().CacheWriteTokens)
			}
			wrongModel := bytes.Replace([]byte(v.response), []byte(`"provider-model"`), []byte(`"different-model"`), 1)
			_, err = upstream.ParseDelivery(t.Context(), 200, wrongModel)
			require.Error(t, err)

			envelope["stream"] = true
			_, err = profile.PrepareRequest(marshal(), "public-vision", contract)
			require.Error(t, err)
			delete(envelope, "stream")
			envelope["tools"] = []any{}
			_, err = profile.PrepareRequest(marshal(), "public-vision", contract)
			require.ErrorContains(t, err, "tools_with_inline_jpeg_unsupported")
			delete(envelope, "tools")
			parts[0] = map[string]any{"type": v.imageType, "image_url": "https://not-fetched.invalid/a.jpg"}
			_, err = profile.PrepareRequest(marshal(), "public-vision", contract)
			require.Error(t, err)
			parts[0] = map[string]any{"type": v.textType, "text": "image missing"}
			_, err = profile.PrepareRequest(marshal(), "public-vision", contract)
			require.Error(t, err)
		})
	}
}
