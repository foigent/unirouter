package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/guardrail"
)

const chatDeliveryWhitelistVersion = "chat-completion-delivery.v1"

var (
	errStreamGuardrailUnsupportedText = errors.New("stream frame contains unsupported text shape")
	errStreamGuardrailCrossBoundary   = errors.New("stream redaction crosses already delivered context")
	errStreamGuardrailArguments       = errors.New("stream redaction cannot safely rewrite structured arguments")
	errStreamGuardrailBufferExceeded  = errors.New("stream frame exceeds synchronous guardrail buffer")
	errStreamDuplicateJSONMember      = errors.New("stream frame contains a duplicate JSON member")
)

type streamTextRef struct {
	container map[string]interface{}
	key       string
	path      string
	original  string
}

type guardedStreamFrame struct {
	raw         []byte
	envelope    map[string]interface{}
	refs        []*streamTextRef
	unsupported bool
	choiceFrame bool
	terminal    bool
	transformed bool
}

func parseGuardedStreamFrame(data []byte) (*guardedStreamFrame, error) {
	if err := rejectDuplicateJSONMembers(data); err != nil {
		return nil, fmt.Errorf("stream frame has ambiguous JSON members: %w", err)
	}
	var envelope map[string]interface{}
	if err := decodeJSONUseNumber(data, &envelope); err != nil {
		return nil, err
	}
	frame := &guardedStreamFrame{raw: append([]byte(nil), data...), envelope: envelope}
	if !streamTopLevelShapeSupported(envelope) {
		frame.unsupported = true
	}
	for _, key := range []string{"id", "object", "model", "system_fingerprint", "service_tier"} {
		if value, exists := envelope[key]; exists && value != nil {
			if _, ok := value.(string); !ok {
				frame.unsupported = true
			}
		}
	}
	if value, exists := envelope["created"]; exists && value != nil {
		number, ok := value.(json.Number)
		if !ok {
			frame.unsupported = true
		} else if created, err := number.Int64(); err != nil || created < 0 {
			frame.unsupported = true
		}
	}
	if usage, exists := envelope["usage"]; exists && usage != nil && !streamUsageShapeSupported(usage) {
		frame.unsupported = true
	}
	rawChoices, exists := envelope["choices"]
	if !exists {
		// OpenAI-compatible providers may emit a final usage-only frame. It is
		// measurable metadata and contains no model text to disclose.
		if _, hasUsage := envelope["usage"]; !hasUsage {
			frame.unsupported = true
		}
		return frame, nil
	}
	if rawChoices == nil {
		frame.unsupported = true
		return frame, nil
	}
	choices, ok := rawChoices.([]interface{})
	if !ok {
		frame.unsupported = true
		return frame, nil
	}
	frame.choiceFrame = len(choices) > 0
	allChoicesTerminal := len(choices) > 0
	for choiceIndex, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]interface{})
		if !ok {
			frame.unsupported = true
			allChoicesTerminal = false
			continue
		}
		if !streamMapKeysSupported(choice, "index", "delta", "text", "finish_reason", "logprobs", "usage") {
			frame.unsupported = true
		}
		// OpenAI-compatible providers can include a nullable logprobs member on
		// the terminal choice even when log probabilities were not requested.
		// A non-null value can contain provider-generated token text and remains
		// unsupported until it is modeled by the output guardrail.
		if logprobs, exists := choice["logprobs"]; exists && logprobs != nil {
			frame.unsupported = true
		}
		// Kimi can repeat bounded numeric usage metadata on the terminal choice
		// before emitting the standard top-level usage-only frame. It is safe to
		// disclose but does not become charging authority here; the existing
		// top-level usage parser remains the only settlement source.
		if usage, exists := choice["usage"]; exists && usage != nil && !streamUsageShapeSupported(usage) {
			frame.unsupported = true
		}
		choiceTerminal := false
		if finish, exists := choice["finish_reason"]; exists && finish != nil {
			value, ok := finish.(string)
			if !ok || !streamFinishReasonSupported(value) {
				frame.unsupported = true
			} else {
				choiceTerminal = true
			}
		}
		if !choiceTerminal {
			allChoicesTerminal = false
		}
		choiceIndex, ok = streamEnvelopeIndex(choice, choiceIndex)
		if !ok {
			frame.unsupported = true
		}
		if delta, ok := choice["delta"].(map[string]interface{}); ok {
			if !streamMapKeysSupported(delta, "role", "content", "refusal", "reasoning_content", "reasoning", "function_call", "tool_calls") {
				frame.unsupported = true
			}
			if role, exists := delta["role"]; exists && role != nil {
				value, ok := role.(string)
				if !ok || (value != "assistant" && value != "tool" && value != "function") {
					frame.unsupported = true
				}
			}
			frame.collectTextValue(delta, "content", fmt.Sprintf("choices[%d].content", choiceIndex))
			frame.collectTextValue(delta, "refusal", fmt.Sprintf("choices[%d].refusal", choiceIndex))
			frame.collectTextValue(delta, "reasoning_content", fmt.Sprintf("choices[%d].reasoning_content", choiceIndex))
			frame.collectTextValue(delta, "reasoning", fmt.Sprintf("choices[%d].reasoning", choiceIndex))
			if functionCall, exists := delta["function_call"]; exists && functionCall != nil {
				if function, ok := functionCall.(map[string]interface{}); ok {
					if !streamMapKeysSupported(function, "name", "arguments") {
						frame.unsupported = true
					}
					frame.collectString(function, "name", fmt.Sprintf("choices[%d].function_call.name", choiceIndex))
					frame.collectString(function, "arguments", fmt.Sprintf("choices[%d].function_call.arguments", choiceIndex))
				} else {
					frame.unsupported = true
				}
			}
			if toolCalls, exists := delta["tool_calls"]; exists && toolCalls != nil {
				calls, ok := toolCalls.([]interface{})
				if !ok {
					frame.unsupported = true
				} else {
					for toolIndex, rawTool := range calls {
						tool, ok := rawTool.(map[string]interface{})
						if !ok {
							frame.unsupported = true
							continue
						}
						if !streamMapKeysSupported(tool, "index", "id", "type", "function") {
							frame.unsupported = true
						}
						toolIndex, ok = streamEnvelopeIndex(tool, toolIndex)
						if !ok {
							frame.unsupported = true
						}
						frame.collectString(tool, "id", fmt.Sprintf("choices[%d].tool_calls[%d].id", choiceIndex, toolIndex))
						if value, exists := tool["type"]; exists && value != nil {
							typeName, ok := value.(string)
							if !ok || typeName != "function" {
								frame.unsupported = true
							}
						}
						functionValue, exists := tool["function"]
						if !exists || functionValue == nil {
							continue
						}
						function, ok := functionValue.(map[string]interface{})
						if !ok {
							frame.unsupported = true
							continue
						}
						if !streamMapKeysSupported(function, "name", "arguments") {
							frame.unsupported = true
						}
						frame.collectString(function, "name", fmt.Sprintf("choices[%d].tool_calls[%d].function.name", choiceIndex, toolIndex))
						frame.collectString(function, "arguments", fmt.Sprintf("choices[%d].tool_calls[%d].function.arguments", choiceIndex, toolIndex))
					}
				}
			}
		} else if deltaValue, exists := choice["delta"]; exists && deltaValue != nil {
			frame.unsupported = true
		}
		frame.collectTextValue(choice, "text", fmt.Sprintf("choices[%d].text", choiceIndex))
	}
	frame.terminal = allChoicesTerminal
	return frame, nil
}

// providerStreamErrorFrame recognizes a typed provider failure before the
// generic model-delta parser. Error envelopes are never forwarded as model
// output and never establish customer delivery.
func providerStreamErrorFrame(data []byte) (bool, error) {
	if err := rejectDuplicateJSONMembers(data); err != nil {
		return false, err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil || envelope == nil {
		return false, fmt.Errorf("provider stream frame must be an object")
	}
	raw, exists := envelope["error"]
	if !exists || isJSONNull(raw) {
		return false, nil
	}
	var providerError map[string]json.RawMessage
	if err := json.Unmarshal(raw, &providerError); err != nil || providerError == nil {
		return false, fmt.Errorf("provider stream error must be an object")
	}
	return true, nil
}

func streamFinishReasonSupported(value string) bool {
	switch value {
	case "stop", "length", "tool_calls", "function_call", "content_filter":
		return true
	default:
		return false
	}
}

func rejectDuplicateJSONMembers(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := walkUniqueJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("stream frame contains trailing JSON data")
		}
		return err
	}
	return nil
}

func walkUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("stream frame object member name is invalid")
			}
			if _, exists := seen[key]; exists {
				return errStreamDuplicateJSONMember
			}
			seen[key] = struct{}{}
			if err := walkUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("stream frame object is not terminated")
		}
	case '[':
		for decoder.More() {
			if err := walkUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("stream frame array is not terminated")
		}
	default:
		return fmt.Errorf("stream frame contains an unexpected JSON delimiter")
	}
	return nil
}

func streamTopLevelShapeSupported(envelope map[string]interface{}) bool {
	return streamMapKeysSupported(envelope, "id", "object", "created", "model", "system_fingerprint", "service_tier", "choices", "usage")
}

func streamUsageShapeSupported(value interface{}) bool {
	switch typed := value.(type) {
	case json.Number:
		_, err := typed.Int64()
		return err == nil
	case map[string]interface{}:
		for _, nested := range typed {
			if nested != nil && !streamUsageShapeSupported(nested) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func streamMapKeysSupported(container map[string]interface{}, allowed ...string) bool {
	for key := range container {
		matched := false
		for _, candidate := range allowed {
			if key == candidate {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func streamEnvelopeIndex(container map[string]interface{}, fallback int) (int, bool) {
	value, exists := container["index"]
	if !exists || value == nil {
		return fallback, true
	}
	number, ok := value.(json.Number)
	if !ok {
		return fallback, false
	}
	parsed, err := number.Int64()
	if err != nil || parsed < 0 || parsed > 1_000_000 {
		return fallback, false
	}
	return int(parsed), true
}

func (frame *guardedStreamFrame) collectString(container map[string]interface{}, key, path string) {
	value, exists := container[key]
	if !exists || value == nil {
		return
	}
	text, ok := value.(string)
	if !ok {
		frame.unsupported = true
		return
	}
	frame.refs = append(frame.refs, &streamTextRef{container: container, key: key, path: path, original: text})
}

func (frame *guardedStreamFrame) collectTextValue(container map[string]interface{}, key, path string) {
	value, exists := container[key]
	if !exists || value == nil {
		return
	}
	if text, ok := value.(string); ok {
		frame.refs = append(frame.refs, &streamTextRef{container: container, key: key, path: path, original: text})
		return
	}
	parts, ok := value.([]interface{})
	if !ok {
		frame.unsupported = true
		return
	}
	for partIndex, rawPart := range parts {
		part, ok := rawPart.(map[string]interface{})
		if !ok {
			frame.unsupported = true
			continue
		}
		if !streamMapKeysSupported(part, "type", "text") {
			frame.unsupported = true
		}
		if partType, exists := part["type"]; exists && partType != nil {
			typeName, ok := partType.(string)
			if !ok || (typeName != "text" && typeName != "output_text") {
				frame.unsupported = true
			}
		}
		textValue, exists := part["text"]
		if !exists || textValue == nil {
			frame.unsupported = true
			continue
		}
		text, ok := textValue.(string)
		if !ok {
			frame.unsupported = true
			continue
		}
		frame.refs = append(frame.refs, &streamTextRef{
			container: part, key: "text", path: fmt.Sprintf("%s[%d].text", path, partIndex), original: text,
		})
	}
}

func (frame *guardedStreamFrame) data() ([]byte, error) {
	if !frame.transformed {
		return append([]byte(nil), frame.raw...), nil
	}
	return json.Marshal(frame.envelope)
}

type streamGuardrailEvaluation struct {
	Result          guardrail.Result
	DeliveredByPath map[string]string
}

type guardedStreamBatchInspection struct {
	Frames        []guardedStreamInspection
	Result        guardrail.Result
	BufferedBytes int
}

func evaluateGuardedStreamBatch(ctx context.Context, pinned *guardrail.Pinned, frames []*guardedStreamFrame, history map[string]string) (streamGuardrailEvaluation, error) {
	for _, frame := range frames {
		if frame.unsupported {
			return streamGuardrailEvaluation{}, errStreamGuardrailUnsupportedText
		}
	}
	texts, currentByPath := streamGuardrailTexts(frames, history)
	result := pinned.EvaluateStreamEnforced(ctx, texts)
	evaluation := streamGuardrailEvaluation{Result: result, DeliveredByPath: currentByPath}
	if result.Decision == guardrail.DecisionBlock || !result.Transformed {
		return evaluation, nil
	}
	if len(result.Texts) != len(texts) {
		return evaluation, fmt.Errorf("stream guardrail transform shape changed")
	}
	refsByPath := make(map[string][]*streamTextRef)
	for _, frame := range frames {
		for _, ref := range frame.refs {
			refsByPath[ref.path] = append(refsByPath[ref.path], ref)
		}
	}
	delivered := make(map[string]string, len(result.Texts))
	for index, transformed := range result.Texts {
		original := texts[index]
		if transformed.Path != original.Path {
			return evaluation, fmt.Errorf("stream guardrail transform path changed")
		}
		prefix := history[original.Path]
		if !strings.HasPrefix(transformed.Content, prefix) {
			return evaluation, errStreamGuardrailCrossBoundary
		}
		current := strings.TrimPrefix(transformed.Content, prefix)
		if strings.HasSuffix(original.Path, ".arguments") && current != currentByPath[original.Path] {
			return evaluation, errStreamGuardrailArguments
		}
		refs := refsByPath[original.Path]
		if len(refs) == 0 {
			return evaluation, fmt.Errorf("stream guardrail transform reference missing")
		}
		refs[0].container[refs[0].key] = current
		for _, ref := range refs[1:] {
			ref.container[ref.key] = ""
		}
		for _, frame := range frames {
			frame.transformed = true
		}
		delivered[original.Path] = current
	}
	evaluation.DeliveredByPath = delivered
	return evaluation, nil
}

func streamGuardrailTexts(frames []*guardedStreamFrame, history map[string]string) ([]guardrail.Text, map[string]string) {
	current := make(map[string]string)
	for _, frame := range frames {
		for _, ref := range frame.refs {
			current[ref.path] += ref.original
		}
	}
	paths := make([]string, 0, len(current))
	for path := range current {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	texts := make([]guardrail.Text, 0, len(paths))
	for _, path := range paths {
		texts = append(texts, guardrail.Text{Path: path, Content: history[path] + current[path]})
	}
	return texts, current
}

func streamGuardrailCurrentTexts(frames []*guardedStreamFrame) ([]guardrail.Text, map[string]string) {
	current := make(map[string]string)
	for _, frame := range frames {
		if frame == nil {
			continue
		}
		for _, ref := range frame.refs {
			value, _ := ref.container[ref.key].(string)
			current[ref.path] += value
		}
	}
	paths := make([]string, 0, len(current))
	for path := range current {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	texts := make([]guardrail.Text, 0, len(paths))
	for _, path := range paths {
		texts = append(texts, guardrail.Text{Path: path, Content: current[path]})
	}
	return texts, current
}

func inspectGuardedStreamBatch(ctx context.Context, pinned *guardrail.Pinned, profile guardrail.StreamProfile, data [][]byte, history map[string]string) (guardedStreamBatchInspection, error) {
	batch := guardedStreamBatchInspection{Frames: make([]guardedStreamInspection, len(data))}
	parsed := make([]*guardedStreamFrame, 0, len(data))
	parsedByIndex := make([]*guardedStreamFrame, len(data))
	for index, raw := range data {
		frame, err := parseGuardedStreamFrame(raw)
		if err != nil {
			return batch, err
		}
		parsed = append(parsed, frame)
		parsedByIndex[index] = frame
		batch.BufferedBytes += len(raw)
	}
	for _, frame := range parsed {
		if frame.unsupported {
			return batch, errStreamGuardrailUnsupportedText
		}
	}
	if profile.Enforced && batch.BufferedBytes > 0 {
		evaluation, err := evaluateGuardedStreamBatch(ctx, pinned, parsed, history)
		batch.Result = evaluation.Result
		if err != nil {
			return batch, err
		}
		if evaluation.Result.Decision != guardrail.DecisionBlock {
			updateStreamGuardrailHistory(history, evaluation.DeliveredByPath, profile.LookbehindBytes)
		}
	}
	for index, frame := range parsedByIndex {
		if frame == nil {
			continue
		}
		inspection := guardedStreamInspection{Frame: frame}
		encoded, err := frame.data()
		if err != nil {
			return batch, err
		}
		inspection.Data = encoded
		texts, delivered := streamGuardrailCurrentTexts([]*guardedStreamFrame{frame})
		inspection.Delivered = delivered
		inspection.AdvisoryTexts = texts
		var text strings.Builder
		for _, item := range texts {
			text.WriteString(item.Content)
		}
		inspection.Text = text.String()
		batch.Frames[index] = inspection
	}
	return batch, nil
}

func updateStreamGuardrailHistory(history map[string]string, delivered map[string]string, lookbehind int) {
	if lookbehind <= 0 {
		for path := range delivered {
			delete(history, path)
		}
		return
	}
	for path, content := range delivered {
		history[path] = utf8SafeSuffix(history[path]+content, lookbehind)
	}
}

func utf8SafeSuffix(value string, maxBytes int) string {
	if maxBytes <= 0 || value == "" {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	start := len(value) - maxBytes
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}
