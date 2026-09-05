package modelcapabilityqualification

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"mime"
	"strings"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/strictjson"
)

// Shared bounded wire consumption. Exact wrappers validate immutable binding
// identity; this role asserts echo/arguments/reasoning, not Runtime policy.
func validateChatStreamFunctionTools(b Binding, contentType string, reader io.Reader, reasoningInContent bool) (OpenAIChatFunctionToolsObservation, error) {
	var observation OpenAIChatFunctionToolsObservation
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "text/event-stream" {
		return observation, invalidOpenAIChatFunctionTools("content_type")
	}
	profile, err := b.ExecutionProfile()
	if err != nil {
		return observation, invalidOpenAIChatFunctionTools("probe_contract", err)
	}
	contract, ok := modelprofile.ResolveChatProtocolContract(b.ProtocolProfileID)
	if !ok || !contract.StreamContract.FunctionToolDeltas {
		return observation, invalidOpenAIChatFunctionTools("probe_contract")
	}
	limited := &io.LimitedReader{R: reader, N: OpenAIChatFunctionToolsMaxResponseBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Split(adapters.SplitSSELines)
	scanner.Buffer(make([]byte, 4096), OpenAIChatFunctionToolsMaxResponseBytes)
	assembler, _ := adapters.NewChatSSEEventAssembler(OpenAIChatFunctionToolsMaxResponseBytes)
	state := adapters.NewFunctionToolStreamState(b.ExpectedUpstreamModel)
	toolEnvelope, err := adapters.NewToolStreamEnvelope(b.ProtocolProfileID, b.ExpectedProvider, b.ExpectedUpstreamModel, profile.CacheUsageProfile)
	if err != nil {
		return observation, invalidOpenAIChatFunctionTools("probe_contract", err)
	}
	adapter := &adapters.OpenAICompatible{}
	var content strings.Builder
	done := false
	process := func(raw []byte) error {
		if string(raw) == "[DONE]" {
			done = true
			if err := toolEnvelope.AcceptDone(); err != nil {
				return err
			}
			return state.Finish()
		}
		if err := strictjson.Validate(raw); err != nil {
			return err
		}
		body, err := adapter.NormalizeStreamFrame(context.Background(), adapters.StreamChunkInput{Data: raw, Channel: models.Channel{Provider: b.ExpectedProvider}, CacheUsageProfile: profile.CacheUsageProfile})
		if err != nil {
			return err
		}
		body, skip, prepareErr := toolEnvelope.Prepare(body)
		if skip {
			return nil
		}
		if body == nil {
			return prepareErr
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
		if prepareErr != nil {
			return prepareErr
		}
		facts, err := adapters.ParseFunctionToolStreamFactsForContract(body, contract.StreamContract)
		if err != nil {
			return err
		}
		observation.ReportedModel = facts.Model
		if facts.Finish != "" {
			observation.FinishReason = facts.Finish
		}
		content.WriteString(facts.Content)
		observation.ReasoningObserved = observation.ReasoningObserved || strings.TrimSpace(facts.Reasoning) != ""
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
	if contract.StreamContract.RequireDone && !done {
		return observation, invalidOpenAIChatFunctionTools("terminal")
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
	if reasoningInContent {
		observation.ReasoningObserved = hasExactEmbeddedThinking(content.String())
	}
	if !observation.ReasoningObserved {
		return observation, invalidOpenAIChatFunctionTools("reasoning")
	}
	return observation, nil
}
