package adapters

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

// Synthetic fixtures exercise transport facts; they are not Provider evidence.
func kimiToolEnvelope(choices, usage string) []byte {
	if usage != "" {
		usage = `,"usage":` + usage
	}
	return []byte(`{"id":"x","object":"chat.completion.chunk","created":1,"model":"kimi-k3","system_fingerprint":null,"choices":` + choices + usage + `}`)
}

func TestKimiToolEnvelopeReusesUsageAgreementWithoutDoubleDelivery(t *testing.T) {
	usage := `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"cached_tokens":4}`
	for _, topAlso := range []bool{false, true} {
		t.Run(fmt.Sprint(topAlso), func(t *testing.T) {
			e, err := NewToolStreamEnvelope("protocol.kimi-openai-chat.k3.v6", "kimi", "kimi-k3", kimiChatCacheUsageProfile)
			require.NoError(t, err)
			state := NewFunctionToolStreamState("kimi-k3")
			contract, _ := modelprofile.ResolveChatProtocolContract("protocol.kimi-openai-chat.k3.v6")
			accept := func(wire []byte) bool {
				body, skip, err := e.Prepare(wire)
				require.NoError(t, err)
				if skip {
					return true
				}
				_, present, err := ParseOpenAICompatibleStreamUsageWithCacheProfile(body, EndpointChatCompletions, metering.UsageSourceUpstream, kimiChatCacheUsageProfile)
				require.NoError(t, err)
				facts, err := ParseFunctionToolStreamFactsForContract(body, contract.StreamContract)
				require.NoError(t, err)
				require.NoError(t, state.Accept(facts, present))
				return false
			}
			require.False(t, accept(kimiToolEnvelope(`[{"index":0,"logprobs":null,"delta":{"reasoning_content":"check","tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read","arguments":"{"}}]}}]`, "")))
			require.False(t, accept(kimiToolEnvelope(`[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]},"finish_reason":"tool_calls","usage":`+usage+`}]`, map[bool]string{true: usage}[topAlso])))
			require.True(t, accept(kimiToolEnvelope(`[]`, usage)), "consistent repeat is validated but not delivered twice")
			require.NoError(t, e.AcceptDone())
			require.NoError(t, state.Finish())
			require.Equal(t, []StreamFunctionCall{{ID: "call_a", Name: "read", Arguments: "{}"}}, state.Calls())
			_, _, err = e.Prepare(kimiToolEnvelope(`[]`, usage))
			require.Error(t, err, "data after DONE is rejected")
		})
	}
}

func TestKimiToolEnvelopeRejectsConflictingUsageAndPostTerminalData(t *testing.T) {
	usage := `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"cached_tokens":4}`
	for name, next := range map[string][]byte{
		"cache conflict":      kimiToolEnvelope(`[]`, strings.Replace(usage, `"cached_tokens":4`, `"cached_tokens":3`, 1)),
		"post terminal tools": kimiToolEnvelope(`[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"x"}}]}}]`, ""),
		"unknown metadata":    []byte(strings.Replace(string(kimiToolEnvelope(`[]`, usage)), `"created":1`, `"created":1,"semantic_extension":true`, 1)),
		"wrong model":         []byte(strings.Replace(string(kimiToolEnvelope(`[]`, usage)), `"kimi-k3"`, `"kimi-k2.6"`, 1)),
	} {
		t.Run(name, func(t *testing.T) {
			e, err := NewToolStreamEnvelope("protocol.kimi-openai-chat.k3.v6", "kimi", "kimi-k3", kimiChatCacheUsageProfile)
			require.NoError(t, err)
			_, _, err = e.Prepare(kimiToolEnvelope(`[{"index":0,"delta":{},"finish_reason":"stop","usage":`+usage+`}]`, ""))
			require.NoError(t, err)
			_, _, err = e.Prepare(next)
			require.Error(t, err)
		})
	}
	e, _ := NewToolStreamEnvelope("protocol.kimi-openai-chat.k3.v6", "kimi", "kimi-k3", kimiChatCacheUsageProfile)
	_, _, err := e.Prepare(kimiToolEnvelope(`[{"index":0,"delta":{},"finish_reason":"stop","usage":`+usage+`}]`, strings.Replace(usage, `"cached_tokens":4`, `"cached_tokens":3`, 1)))
	require.Error(t, err, "same-frame full-vector conflict")
}

func TestExactToolEnvelopeMetadataDoesNotWidenHistoricalParsers(t *testing.T) {
	for _, protocol := range []string{"protocol.deepseek-openai-chat.v5", "protocol.kimi-openai-chat.v6"} {
		provider, cache := "deepseek", "cache-usage.deepseek-v4.v1"
		if strings.Contains(protocol, "kimi") {
			provider, cache = "kimi", kimiChatCacheUsageProfile
		}
		for _, mutation := range []string{"valid", "duplicate", "fingerprint type", "logprobs", "unknown delta"} {
			t.Run(protocol+mutation, func(t *testing.T) {
				e, err := NewToolStreamEnvelope(protocol, provider, "model", cache)
				require.NoError(t, err)
				wire := `{"id":"x","object":"chat.completion.chunk","created":1,"model":"model","system_fingerprint":"fp","choices":[{"index":0,"logprobs":null,"delta":{"reasoning_content":"retained","content":""}}]}`
				switch mutation {
				case "duplicate":
					wire = strings.Replace(wire, `"id":"x"`, `"id":"x","id":"y"`, 1)
				case "fingerprint type":
					wire = strings.Replace(wire, `"system_fingerprint":"fp"`, `"system_fingerprint":1`, 1)
				case "logprobs":
					wire = strings.Replace(wire, `"logprobs":null`, `"logprobs":{}`, 1)
				case "unknown delta":
					wire = strings.Replace(wire, `"content":""`, `"new_field":true`, 1)
				}
				body, skip, err := e.Prepare([]byte(wire))
				if mutation != "valid" {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				require.False(t, skip)
				require.Contains(t, string(body), `"reasoning_content":"retained"`)
				old, _ := modelprofile.ResolveChatProtocolContract("protocol.glm-openai-chat.v7")
				_, err = ParseFunctionToolStreamFactsForContract([]byte(wire), old.StreamContract)
				require.Error(t, err, "M113 retains its own exact envelope")
			})
		}
	}
	e, err := NewToolStreamEnvelope("protocol.glm-openai-chat.v7", "glm", "glm-5.3", "")
	require.NoError(t, err)
	require.Nil(t, e)
	_, err = NewToolStreamEnvelope("protocol.kimi-openai-chat.v6", "deepseek", "x", kimiChatCacheUsageProfile)
	require.Error(t, err)
}
