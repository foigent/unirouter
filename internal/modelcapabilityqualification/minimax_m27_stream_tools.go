package modelcapabilityqualification

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/strictjson"
)

func validateMiniMaxM27StreamBinding(b Binding) error {
	if b.OperationID != modelprofile.OperationOpenAIChat || b.ExecutionProfileID != "minimax.m2.7.payg" || b.ExecutionProfileVersion != "1.3.0" || b.ProtocolProfileID != "protocol.minimax-m27-openai-chat.v1" || b.ProtocolProfileVersion != "1.0.0" || b.ProbeContractID != MiniMaxM27StreamFunctionToolsProbeContractID || b.ProbeContractVersion != "1.0.0" || b.ExpectedProvider != "minimax" || b.ExpectedUpstreamModel != "MiniMax-M2.7" || b.PublicPath != "/v1/chat/completions" || !slices.Contains([]string{modelprofile.CapabilityStreaming, modelprofile.CapabilityReasoning, modelprofile.CapabilityTools, modelprofile.CapabilityFunctionTools, modelprofile.CapabilityToolChoiceAuto}, b.CapabilityID) {
		return fmt.Errorf("%w: exact M2.7 streamed tools binding", ErrUnsupported)
	}
	return nil
}

func BuildMiniMaxM27StreamFunctionToolsProbeRequest(b Binding) ([]byte, error) {
	if err := validateMiniMaxM27StreamBinding(b); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]interface{}{
		"model": b.ExpectedUpstreamModel, "stream": true, "stream_options": map[string]bool{"include_usage": true},
		"tool_choice": "auto", "max_completion_tokens": 1024, "tools": openAIChatProbeTools(),
		"messages": []map[string]string{{"role": "user", "content": "Call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."}},
	})
}

// ValidateMiniMaxM27StreamFunctionToolsForBinding consumes raw, bounded SSE.
// Normalization, tool assembly and usage parsing are the same mechanics as
// Runtime; only this role asserts the fixed probe tool/value and thinking.
func ValidateMiniMaxM27StreamFunctionToolsForBinding(b Binding, contentType string, reader io.Reader) (OpenAIChatFunctionToolsObservation, error) {
	var observation OpenAIChatFunctionToolsObservation
	if err := validateMiniMaxM27StreamBinding(b); err != nil {
		return observation, invalidOpenAIChatFunctionTools("probe_contract", err)
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "text/event-stream" {
		return observation, invalidOpenAIChatFunctionTools("content_type")
	}
	profile, _ := modelprofile.Lookup(b.ExecutionProfileID, b.ExecutionProfileVersion)
	limited := &io.LimitedReader{R: reader, N: OpenAIChatFunctionToolsMaxResponseBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Split(adapters.SplitSSELines)
	scanner.Buffer(make([]byte, 4096), OpenAIChatFunctionToolsMaxResponseBytes)
	assembler, _ := adapters.NewChatSSEEventAssembler(OpenAIChatFunctionToolsMaxResponseBytes)
	state := adapters.NewFunctionToolStreamState(b.ExpectedUpstreamModel)
	adapter := &adapters.OpenAICompatible{}
	var content strings.Builder
	done := false
	process := func(raw []byte) error {
		if string(raw) == "[DONE]" {
			done = true
			return state.Finish()
		}
		if err := strictjson.Validate(raw); err != nil {
			return err
		}
		body, err := adapter.NormalizeStreamFrame(context.Background(), adapters.StreamChunkInput{Data: raw, Channel: models.Channel{Provider: b.ExpectedProvider}, CacheUsageProfile: profile.CacheUsageProfile})
		if err != nil {
			return err
		}
		usage, present, err := adapters.ParseOpenAICompatibleStreamUsageWithCacheProfile(body, adapters.EndpointChatCompletions, metering.UsageSourceUpstream, profile.CacheUsageProfile)
		if err != nil {
			return err
		}
		if present {
			observation.PromptTokens = int64(usage.PromptTokens)
			observation.CompletionTokens = int64(usage.CompletionTokens)
			observation.TotalTokens = int64(usage.PromptTokens + usage.CompletionTokens)
			observation.CacheReadCounterObserved = usage.CacheUsageStatus == adapters.CacheUsageStatusReadReported
			observation.CachedTokens = int64(usage.CacheReadTokens)
		}
		facts, err := adapters.ParseFunctionToolStreamFacts(body)
		if err != nil {
			return err
		}
		observation.ReportedModel = facts.Model
		if facts.Finish != "" {
			observation.FinishReason = facts.Finish
		}
		content.WriteString(facts.Content)
		return state.Accept(facts, present)
	}
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			line = strings.TrimPrefix(line, "\uFEFF")
			first = false
		}
		event, ready, err := assembler.AcceptLine(line)
		if err == nil && ready {
			err = process(event.Data)
		}
		observation.ResponseBytes = OpenAIChatFunctionToolsMaxResponseBytes + 1 - int(limited.N)
		if err != nil {
			return observation, invalidOpenAIChatFunctionTools("stream", err)
		}
		if done {
			break
		}
	}
	if scanner.Err() != nil || limited.N <= 0 {
		return observation, invalidOpenAIChatFunctionTools("read_or_limit", scanner.Err())
	}
	if !done {
		event, ready, err := assembler.Finish()
		if err == nil && ready {
			err = process(event.Data)
		}
		if err != nil {
			return observation, invalidOpenAIChatFunctionTools("stream", err)
		}
	}
	if err := state.Finish(); err != nil {
		return observation, invalidOpenAIChatFunctionTools("terminal", err)
	}
	calls := state.Calls()
	if observation.FinishReason != "tool_calls" || len(calls) != 1 || calls[0].Name != "unirouter_probe_echo" {
		return observation, invalidOpenAIChatFunctionTools("tool_call_identity")
	}
	var args map[string]interface{}
	if json.Unmarshal([]byte(calls[0].Arguments), &args) != nil || len(args) != 1 || args["value"] != "UNIROUTER_TOOL_PROBE_OK" {
		return observation, invalidOpenAIChatFunctionTools("tool_arguments")
	}
	observation.ToolCallObserved = true
	observation.ReasoningObserved = hasExactEmbeddedThinking(content.String())
	if !observation.ReasoningObserved {
		return observation, invalidOpenAIChatFunctionTools("reasoning")
	}
	return observation, nil
}
