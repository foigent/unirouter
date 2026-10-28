package modelcapabilityqualification

import (
	"bufio"
	"context"
	"errors"
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
	scenario, err := streamToolProbeScenarioForBinding(b)
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
				return invalidOpenAIChatFunctionToolsDiagnostic("stream", "terminal", "invalid_state", err)
			}
			if err := state.Finish(); err != nil {
				return invalidOpenAIChatFunctionToolsDiagnostic("stream", "terminal", "invalid_state", err)
			}
			return nil
		}
		if err := strictjson.Validate(raw); err != nil {
			return invalidOpenAIChatFunctionToolsDiagnostic("stream", "frame_json", "invalid_json", err)
		}
		body, err := adapter.NormalizeStreamFrame(context.Background(), adapters.StreamChunkInput{Data: raw, Channel: models.Channel{Provider: b.ExpectedProvider}, CacheUsageProfile: profile.CacheUsageProfile})
		if err != nil {
			return invalidOpenAIChatFunctionToolsDiagnostic("stream", "normalization", adapters.ResponseNormalizationStage(err), err)
		}
		body, skip, prepareErr := toolEnvelope.Prepare(body)
		if skip {
			return nil
		}
		if body == nil {
			if errors.Is(prepareErr, adapters.ErrToolStreamModelMismatch) {
				return invalidOpenAIChatFunctionToolsDiagnostic("stream", "model_identity", "mismatch", prepareErr)
			}
			return invalidOpenAIChatFunctionToolsDiagnostic("stream", "envelope", "invalid_shape", prepareErr)
		}
		usage, present, err := adapters.ParseOpenAICompatibleStreamUsageWithCacheProfile(body, adapters.EndpointChatCompletions, metering.UsageSourceUpstream, profile.CacheUsageProfile)
		if err != nil {
			return invalidOpenAIChatFunctionToolsDiagnostic("stream", "usage", "invalid_usage", err)
		}
		if present {
			observation.PromptTokens = int64(usage.PromptTokens)
			observation.CompletionTokens = int64(usage.CompletionTokens)
			observation.TotalTokens = int64(usage.PromptTokens + usage.CompletionTokens)
			observation.CacheReadCounterObserved = usage.CacheUsageStatus == adapters.CacheUsageStatusReadReported
			observation.CachedTokens = int64(usage.CacheReadTokens)
		}
		if prepareErr != nil {
			return invalidOpenAIChatFunctionToolsDiagnostic("stream", "envelope", "invalid_shape", prepareErr)
		}
		facts, err := adapters.ParseFunctionToolStreamFactsForContract(body, contract.StreamContract)
		if err != nil {
			return invalidOpenAIChatFunctionToolsDiagnostic("stream", "frame_shape", "invalid_shape", err)
		}
		if facts.Choice {
			observation.DeltaCount++
		}
		observation.ReportedModel = facts.Model
		if facts.Finish != "" {
			observation.FinishReason = facts.Finish
		}
		content.WriteString(facts.Content)
		observation.ReasoningObserved = observation.ReasoningObserved || strings.TrimSpace(facts.Reasoning) != ""
		if err := state.Accept(facts, present); err != nil {
			return invalidOpenAIChatFunctionToolsDiagnostic("stream", "stream_state", "invalid_transition", err)
		}
		return nil
	}
	processEvent := func(event adapters.ChatSSEEvent, ready bool, assembleErr error) error {
		if assembleErr != nil {
			member := "invalid_event"
			switch {
			case errors.Is(assembleErr, adapters.ErrChatSSEUnsupportedField):
				member = "unsupported_field"
			case errors.Is(assembleErr, adapters.ErrChatSSEEventTooLarge):
				member = "event_too_large"
			}
			return invalidOpenAIChatFunctionToolsDiagnostic("stream", "sse", member, assembleErr)
		}
		if !ready {
			return nil
		}
		observation.DataEventCount++
		return process(event.Data)
	}
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			line = strings.TrimPrefix(line, "\uFEFF")
			first = false
		}
		event, ready, assembleErr := assembler.AcceptLine(line)
		err := processEvent(event, ready, assembleErr)
		observation.ResponseBytes = OpenAIChatFunctionToolsMaxResponseBytes + 1 - int(limited.N)
		if err != nil {
			return observation, err
		}
		if done {
			break
		}
	}
	if scanner.Err() != nil || limited.N <= 0 {
		member := "read_error"
		if limited.N <= 0 {
			member = "response_too_large"
		}
		return observation, invalidOpenAIChatFunctionToolsDiagnostic("read_or_limit", "read", member, scanner.Err())
	}
	if !done {
		event, ready, assembleErr := assembler.Finish()
		err := processEvent(event, ready, assembleErr)
		if err != nil {
			return observation, err
		}
	}
	if contract.StreamContract.RequireDone && !done {
		return observation, invalidOpenAIChatFunctionToolsDiagnostic("terminal", "terminal", "missing_done", nil)
	}
	if err := state.Finish(); err != nil {
		return observation, invalidOpenAIChatFunctionToolsDiagnostic("terminal", "terminal", "invalid_state", err)
	}
	calls := state.Calls()
	if observation.FinishReason != "tool_calls" || len(calls) != 1 || calls[0].Name != scenario.functionName {
		return observation, invalidOpenAIChatFunctionTools("tool_call_identity")
	}
	if !scenario.matchesArguments(calls[0].Arguments) {
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
