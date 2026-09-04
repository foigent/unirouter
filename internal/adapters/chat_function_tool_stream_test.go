package adapters

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/models"
)

func TestFunctionToolStreamRejectsNullMetadataAndUnboundedIdentity(t *testing.T) {
	_, err := ParseFunctionToolStreamFacts([]byte(`{"id":"x","object":"chat.completion.chunk","created":null,"model":"MiniMax-M2.7","choices":[]}`))
	require.Error(t, err)
	id, kind, name := strings.Repeat("x", 1<<20), "function", "read"
	err = NewFunctionToolStreamState("MiniMax-M2.7").Accept(FunctionToolStreamFacts{Model: "MiniMax-M2.7", Choice: true, Tools: []FunctionToolDelta{{Index: 0, ID: &id, Type: &kind, Name: &name}}}, false)
	require.ErrorContains(t, err, "identities exceed bound")
}

func TestFunctionToolStreamFixtureAndLineEndings(t *testing.T) {
	fixture, err := os.ReadFile("testdata/minimax_m27_stream_tools.sse")
	require.NoError(t, err)
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		t.Run(fmt.Sprintf("ending-%q", ending), func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(strings.ReplaceAll(string(fixture), "\n", ending)))
			scanner.Split(SplitSSELines)
			assembler, _ := NewChatSSEEventAssembler(1 << 20)
			state := NewFunctionToolStreamState("MiniMax-M2.7")
			for scanner.Scan() {
				event, ready, err := assembler.AcceptLine(scanner.Text())
				require.NoError(t, err)
				if !ready {
					continue
				}
				data, err := NewOpenAICompatible().NormalizeStreamFrame(context.Background(), StreamChunkInput{Data: event.Data, Channel: models.Channel{Provider: "minimax"}, CacheUsageProfile: "cache-usage.minimax-m27-openai.v3"})
				require.NoError(t, err)
				usage, present, err := ParseOpenAICompatibleStreamUsageWithCacheProfile(data, EndpointChatCompletions, metering.UsageSourceUpstream, "cache-usage.minimax-m27-openai.v3")
				require.NoError(t, err)
				if present {
					require.Equal(t, 5585, usage.PromptTokens)
					require.False(t, usage.HasAuthoritativeCacheUsage())
					require.Equal(t, CacheUsageStatusNotReported, usage.CacheUsageStatus)
				}
				facts, err := ParseFunctionToolStreamFacts(data)
				require.NoError(t, err)
				require.NoError(t, state.Accept(facts, present))
			}
			require.NoError(t, scanner.Err())
			require.NoError(t, state.Finish())
			require.Equal(t, []StreamFunctionCall{{ID: "call_echo", Name: "unirouter_probe_echo", Arguments: `{"value":"UNIROUTER_TOOL_PROBE_OK"}`}}, state.Calls())
		})
	}
}

func TestFunctionToolStreamStateNegativeBoundaries(t *testing.T) {
	ptr := func(s string) *string { return &s }
	start := func(index int, id string) FunctionToolStreamFacts {
		return FunctionToolStreamFacts{Model: "MiniMax-M2.7", Choice: true, Tools: []FunctionToolDelta{{Index: index, ID: ptr(id), Type: ptr("function"), Name: ptr("read"), Arguments: ptr(`{"path":`)}}}
	}
	finish := FunctionToolStreamFacts{Model: "MiniMax-M2.7", Choice: true, Finish: "tool_calls"}
	usage := FunctionToolStreamFacts{Model: "MiniMax-M2.7", HasUsage: true}
	for _, name := range []string{"id changed", "duplicate id", "missing first identity", "bad arguments", "duplicate argument key", "usage before finish", "data after finish", "duplicate usage", "missing usage", "sparse index", "tool truncated", "model mismatch", "oversized"} {
		t.Run(name, func(t *testing.T) {
			s := NewFunctionToolStreamState("MiniMax-M2.7")
			require.NoError(t, s.Accept(start(0, "a"), false))
			var err error
			switch name {
			case "id changed":
				err = s.Accept(start(0, "b"), false)
			case "duplicate id":
				err = s.Accept(start(1, "a"), false)
			case "missing first identity":
				err = s.Accept(FunctionToolStreamFacts{Model: "MiniMax-M2.7", Choice: true, Tools: []FunctionToolDelta{{Index: 1, Arguments: ptr(`{}`)}}}, false)
			case "bad arguments":
				err = s.Accept(finish, false)
			case "duplicate argument key":
				require.NoError(t, s.Accept(FunctionToolStreamFacts{Model: "MiniMax-M2.7", Choice: true, Tools: []FunctionToolDelta{{Index: 0, Arguments: ptr(`"x","path":"y"}`)}}}, false))
				err = s.Accept(finish, false)
			case "usage before finish":
				err = s.Accept(usage, true)
			case "tool truncated":
				f := finish
				f.Finish = "length"
				err = s.Accept(f, false)
			case "model mismatch":
				f := finish
				f.Model = "MiniMax-M3"
				err = s.Accept(f, false)
			case "oversized":
				err = s.Accept(FunctionToolStreamFacts{Model: "MiniMax-M2.7", Choice: true, Tools: []FunctionToolDelta{{Index: 0, Arguments: ptr(strings.Repeat("x", 1<<20))}}}, false)
			default:
				require.NoError(t, s.Accept(FunctionToolStreamFacts{Model: "MiniMax-M2.7", Choice: true, Tools: []FunctionToolDelta{{Index: 0, Arguments: ptr(`"VERSION"}`)}}}, false))
				if name == "sparse index" {
					require.NoError(t, s.Accept(start(2, "b"), false))
					err = s.Accept(finish, false)
					break
				}
				require.NoError(t, s.Accept(finish, false))
				switch name {
				case "data after finish":
					err = s.Accept(start(1, "b"), false)
				case "missing usage":
					err = s.Finish()
				case "duplicate usage":
					require.NoError(t, s.Accept(usage, true))
					err = s.Accept(usage, true)
				}
			}
			require.Error(t, err)
		})
	}
	// Two interleaved calls retain distinct identities and argument buffers.
	s := NewFunctionToolStreamState("MiniMax-M2.7")
	require.NoError(t, s.Accept(start(0, "a"), false))
	require.NoError(t, s.Accept(start(1, "b"), false))
	for _, index := range []int{1, 0} {
		require.NoError(t, s.Accept(FunctionToolStreamFacts{Model: "MiniMax-M2.7", Choice: true, Tools: []FunctionToolDelta{{Index: index, Arguments: ptr(`"VERSION"}`)}}}, false))
	}
	require.NoError(t, s.Accept(finish, false))
	require.NoError(t, s.Accept(usage, true))
	require.NoError(t, s.Finish())
	require.Len(t, s.Calls(), 2)
}

func TestMiniMaxM27StreamCacheContextDoesNotWidenHistory(t *testing.T) {
	for _, counter := range []string{"", "0", "null", "-1", "6000", "\"0\""} {
		member := ""
		if counter != "" {
			member = `,"prompt_tokens_details":{"cached_tokens":` + counter + `}`
		}
		body := []byte(`{"object":"chat.completion.chunk","usage":{"prompt_tokens":5585,"completion_tokens":149,"total_tokens":5734` + member + `}}`)
		for _, profile := range []string{"cache-usage.minimax-openai.v2", "cache-usage.minimax-m27-openai.v3"} {
			for _, stream := range []bool{false, true} {
				parse := ParseOpenAICompatibleUsageWithCacheProfile
				if stream {
					parse = ParseOpenAICompatibleStreamUsageWithCacheProfile
				}
				usage, present, err := parse(body, EndpointChatCompletions, metering.UsageSourceUpstream, profile)
				if counter == "0" || (counter == "" && stream && strings.HasSuffix(profile, ".v3")) {
					require.NoError(t, err)
					require.True(t, present)
					require.Equal(t, counter == "0", usage.HasAuthoritativeCacheUsage())
				} else {
					require.Error(t, err)
				}
			}
		}
	}
}
