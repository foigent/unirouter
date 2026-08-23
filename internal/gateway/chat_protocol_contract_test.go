package gateway

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestKimiK26AdvancedProtocolAcceptsFinalPartialAndInlineMedia(t *testing.T) {
	contract, ok := modelprofile.ResolveChatProtocolContract("protocol.kimi-openai-chat.v2")
	require.True(t, ok)
	body := []byte(`{"model":"Kimi K2.6","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aA=="}},{"type":"video_url","video_url":"data:video/mp4;base64,aA=="}]},{"role":"assistant","content":"prefix","partial":true}]}`)
	validated, err := validateAdvancedChatProtocolRequest(body, contract)
	require.NoError(t, err)
	require.Empty(t, validated.ReservedMeterUsage)
	require.NotEmpty(t, validated.SanitizedBody)
}

func TestKimiAdvancedProtocolRejectsUnboundedRemoteMedia(t *testing.T) {
	contract, ok := modelprofile.ResolveChatProtocolContract("protocol.kimi-openai-chat.v2")
	require.True(t, ok)
	_, err := validateAdvancedChatProtocolRequest([]byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}]}]}`), contract)
	require.ErrorContains(t, err, "only inline")
}

func TestKimiK3DynamicToolsAreProfileScoped(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","tools":[{"type":"function","function":{"name":"lookup","description":"Look up a value","parameters":{"type":"object"}}}]},{"role":"user","content":"hello"}]}`)
	k3, ok := modelprofile.ResolveChatProtocolContract("protocol.kimi-openai-chat.k3.v2")
	require.True(t, ok)
	_, err := validateAdvancedChatProtocolRequest(body, k3)
	require.NoError(t, err)

	_, err = validateAdvancedChatProtocolRequest([]byte(`{"messages":[{"role":"system","content":"invalid","tools":[{"type":"function","function":{"name":"lookup","description":"Look up a value","parameters":{"type":"object"}}}]}]}`), k3)
	require.ErrorContains(t, err, "cannot be used with content")
	_, err = validateAdvancedChatProtocolRequest([]byte(`{"messages":[{"role":"system","tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}]}`), k3)
	require.ErrorContains(t, err, "description must be a string")

	k26, ok := modelprofile.ResolveChatProtocolContract("protocol.kimi-openai-chat.v2")
	require.True(t, ok)
	_, err = validateAdvancedChatProtocolRequest(body, k26)
	require.ErrorContains(t, err, "dynamic system tools are not supported")
}

func TestKimiBuiltinSearchProducesBoundedReservationAndNormalization(t *testing.T) {
	contract, ok := modelprofile.ResolveChatProtocolContract("protocol.kimi-openai-chat.v2")
	require.True(t, ok)
	body := []byte(`{"messages":[{"role":"user","content":"latest"}],"thinking":{"type":"disabled"},"tools":[{"type":"builtin_function","function":{"name":"$web_search"}}]}`)
	validated, err := validateAdvancedChatProtocolRequest(body, contract)
	require.NoError(t, err)
	require.Equal(t, []metering.MeterQuantity{{Meter: "builtin_web_search_calls", Quantity: 1}}, validated.ReservedMeterUsage)
	require.Equal(t, []string{"parallel_tool_calls=omitted->false"}, validated.ProviderRequestNormalizations)

	var sanitized map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(validated.SanitizedBody, &sanitized))
	require.JSONEq(t, `[]`, string(sanitized["tools"]))

	_, err = validateAdvancedChatProtocolRequest([]byte(`{"messages":[],"tools":[{"type":"builtin_function","function":{"name":"$web_search"}}]}`), contract)
	require.ErrorContains(t, err, "requires thinking.type=disabled")

	_, err = validateAdvancedChatProtocolRequest([]byte(`{"messages":[],"thinking":{"type":"disabled"},"stream":true,"tools":[{"type":"builtin_function","function":{"name":"$web_search"}}]}`), contract)
	require.ErrorContains(t, err, "requires stream=false")
	_, err = validateAdvancedChatProtocolRequest([]byte(`{"messages":[],"thinking":{"type":"disabled"},"parallel_tool_calls":true,"tools":[{"type":"builtin_function","function":{"name":"$web_search"}}]}`), contract)
	require.ErrorContains(t, err, "requires parallel_tool_calls=false")
}
