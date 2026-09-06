package adapters

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/models"
)

func TestMiniMaxM3StreamTierCapturedFacts(t *testing.T) {
	raw, err := os.ReadFile("testdata/minimax_m3_stream_tier_20260906.sse")
	require.NoError(t, err)
	digest := sha256.Sum256(raw)
	require.Equal(t, "5ee0ccf35812dfa6e18eed6acd21bdfa6f804f973c706f86ca47c044dc3eb93f", hex.EncodeToString(digest[:]))
	for _, revision := range []string{"v10", "v11"} {
		t.Run(revision, func(t *testing.T) {
			protocol := "protocol.minimax-m3-openai-chat." + revision
			preparer, err := NewToolStreamEnvelope(protocol, "minimax", "MiniMax-M3", "cache-usage.minimax-openai.v2")
			require.NoError(t, err)
			contract, ok := modelprofile.ResolveChatProtocolContract(protocol)
			require.True(t, ok)
			state := NewFunctionToolStreamState("MiniMax-M3")
			scanner := bufio.NewScanner(strings.NewReader(string(raw)))
			events := 0
			for scanner.Scan() {
				if !strings.HasPrefix(scanner.Text(), "data:") {
					continue
				}
				data := []byte(strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "data:")))
				body, err := (&OpenAICompatible{}).NormalizeStreamFrame(context.Background(), StreamChunkInput{Data: data, Channel: models.Channel{Provider: "minimax"}, CacheUsageProfile: "cache-usage.minimax-openai.v2"})
				require.NoError(t, err)
				body, skip, err := preparer.Prepare(body)
				require.NoError(t, err)
				require.False(t, skip)
				facts, err := ParseFunctionToolStreamFactsForContract(body, contract.StreamContract)
				if revision == "v10" {
					require.ErrorContains(t, err, "unmodeled stream envelope field")
					return
				}
				require.NoError(t, err)
				require.NoError(t, state.Accept(facts, facts.HasUsage))
				events++
			}
			require.NoError(t, scanner.Err())
			require.Equal(t, 19, events)
			require.NoError(t, state.Finish())
			require.Len(t, state.Calls(), 1)
			require.Equal(t, "unirouter_probe_echo", state.Calls()[0].Name)
			require.JSONEq(t, `{"value":"UNIROUTER_TOOL_PROBE_OK"}`, state.Calls()[0].Arguments)
		})
	}
}

func TestMiniMaxM3StreamTierExactScopeAndInvalidFields(t *testing.T) {
	const frame = `{"id":"x","object":"chat.completion.chunk","created":1,"model":"MiniMax-M3","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	p, err := NewToolStreamEnvelope("protocol.minimax-m3-openai-chat.v11", "minimax", "MiniMax-M3", "cache-usage.minimax-openai.v2")
	require.NoError(t, err)
	for _, value := range []string{`"standard"`, `"priority"`} {
		data := []byte(strings.TrimSuffix(frame, "}") + `,"service_tier":` + value + `}`)
		body, skip, err := p.Prepare(data)
		require.NoError(t, err)
		require.False(t, skip)
		require.JSONEq(t, frame, string(body))
	}
	body, _, err := p.Prepare([]byte(frame))
	require.NoError(t, err)
	require.Equal(t, frame, string(body))
	for _, value := range []string{`null`, `1`, `true`, `{}`, `[]`, `""`, `"flex"`, `"STANDARD"`} {
		data := []byte(strings.TrimSuffix(frame, "}") + `,"service_tier":` + value + `}`)
		body, _, err := p.Prepare(data)
		require.Error(t, err)
		require.Equal(t, data, body, "safe usage remains available on metadata rejection")
	}
	for _, suffix := range []string{`,"service_tier":"standard","service_tier":"priority"}`, `,"service_tier":"standard","service\u005ftier":"priority"}`} {
		body, _, err := p.Prepare([]byte(strings.TrimSuffix(frame, "}") + suffix))
		require.Error(t, err)
		require.Nil(t, body)
	}
	body, _, err = p.Prepare([]byte(strings.Replace(frame, "MiniMax-M3", "wrong-model", 1)))
	require.Error(t, err)
	require.Nil(t, body)
	for _, field := range []string{"system_fingerprint", "unknown"} {
		data := []byte(strings.TrimSuffix(frame, "}") + `,"` + field + `":"extra","service_tier":"standard"}`)
		body, _, err := p.Prepare(data)
		require.NoError(t, err)
		var object map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &object))
		require.Contains(t, object, field)
		c, _ := modelprofile.ResolveChatProtocolContract("protocol.minimax-m3-openai-chat.v11")
		_, err = ParseFunctionToolStreamFactsForContract(body, c.StreamContract)
		require.Error(t, err)
	}
	for _, tuple := range [][2]string{{"kimi", "cache-usage.minimax-openai.v2"}, {"minimax", "cache-usage.kimi-platform.v1"}} {
		_, err := NewToolStreamEnvelope("protocol.minimax-m3-openai-chat.v11", tuple[0], "MiniMax-M3", tuple[1])
		require.Error(t, err)
	}
	for _, protocol := range []string{"protocol.minimax-m3-openai-chat.v10", "protocol.minimax-m27-openai-chat.v1", "protocol.minimax-m2-openai-chat.v8", "protocol.glm-openai-chat.v7"} {
		p, err := NewToolStreamEnvelope(protocol, "minimax", "MiniMax-M3", "cache-usage.minimax-openai.v2")
		require.NoError(t, err)
		require.Nil(t, p)
	}
}
