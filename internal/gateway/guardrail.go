package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/middleware"
)

type guardrailPinContextKey struct{}

func contextWithGuardrailPin(ctx context.Context, pinned *guardrail.Pinned) context.Context {
	if pinned == nil {
		return ctx
	}
	return context.WithValue(ctx, guardrailPinContextKey{}, pinned)
}

func guardrailPinFromContext(ctx context.Context) *guardrail.Pinned {
	if ctx == nil {
		return nil
	}
	pinned, _ := ctx.Value(guardrailPinContextKey{}).(*guardrail.Pinned)
	return pinned
}

func (relay *Relay) pinGuardrail(ctx context.Context, model, endpoint string) *guardrail.Pinned {
	if relay == nil || relay.guardrail == nil {
		return nil
	}
	return relay.guardrail.Pin(guardrail.Subject{
		OrgID: middleware.GetOrgIDFromContext(ctx), ProjectID: middleware.GetProjectIDFromContext(ctx),
		APIKeyID: middleware.GetKeyIDFromContext(ctx), AccessCredentialID: middleware.GetAccessCredentialIDFromContext(ctx),
		Model: model, Endpoint: endpoint,
	})
}

func chatGuardrailTexts(messages []Message) []guardrail.Text {
	texts := make([]guardrail.Text, 0, len(messages))
	for index, message := range messages {
		texts = append(texts, guardrail.Text{Path: fmt.Sprintf("messages[%d].content", index), Content: message.Content})
	}
	return texts
}

func applyChatGuardrailTransform(body []byte, req *ChatCompletionRequest, result guardrail.Result) ([]byte, error) {
	if !result.Transformed {
		return body, nil
	}
	if req == nil || len(req.Messages) != len(result.Texts) {
		return nil, fmt.Errorf("guardrail chat transform shape changed")
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) != len(result.Texts) {
		return nil, fmt.Errorf("guardrail chat content envelope is invalid")
	}
	for index := range messages {
		message, ok := messages[index].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("guardrail chat message envelope is invalid")
		}
		message["content"] = result.Texts[index].Content
		req.Messages[index].Content = result.Texts[index].Content
	}
	return json.Marshal(payload)
}

func embeddingsGuardrailTexts(input interface{}) []guardrail.Text {
	switch value := input.(type) {
	case string:
		return []guardrail.Text{{Path: "input", Content: value}}
	case []interface{}:
		texts := make([]guardrail.Text, 0, len(value))
		for index, item := range value {
			if text, ok := item.(string); ok {
				texts = append(texts, guardrail.Text{Path: fmt.Sprintf("input[%d]", index), Content: text})
			}
		}
		return texts
	default:
		return nil
	}
}

func applyEmbeddingsGuardrailTransform(body []byte, req *EmbeddingsRequest, result guardrail.Result) ([]byte, error) {
	if !result.Transformed {
		return body, nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	switch value := payload["input"].(type) {
	case string:
		if len(result.Texts) != 1 {
			return nil, fmt.Errorf("guardrail embeddings transform shape changed")
		}
		payload["input"] = result.Texts[0].Content
		req.Input = result.Texts[0].Content
	case []interface{}:
		textIndex := 0
		for index, item := range value {
			if _, ok := item.(string); !ok {
				continue
			}
			if textIndex >= len(result.Texts) {
				return nil, fmt.Errorf("guardrail embeddings transform shape changed")
			}
			value[index] = result.Texts[textIndex].Content
			textIndex++
		}
		if textIndex != len(result.Texts) {
			return nil, fmt.Errorf("guardrail embeddings transform shape changed")
		}
		req.Input = value
	default:
		return nil, fmt.Errorf("guardrail embeddings content envelope is invalid")
	}
	return json.Marshal(payload)
}

type outputTextRef struct {
	container map[string]interface{}
	key       string
	path      string
}

// nonStreamOutputInspection keeps the exact mutation references together with
// the completeness of the modeled text fields. An absent or null field carries
// no text. A present field with an unsupported shape is incomplete and must not
// be treated as successfully inspected by an enforced output policy.
type nonStreamOutputInspection struct {
	texts           []guardrail.Text
	refs            []outputTextRef
	incompletePaths []string
}

func (inspection nonStreamOutputInspection) complete() bool {
	return len(inspection.incompletePaths) == 0
}

func (inspection *nonStreamOutputInspection) appendText(container map[string]interface{}, key, path, content string) {
	inspection.texts = append(inspection.texts, guardrail.Text{Path: path, Content: content})
	inspection.refs = append(inspection.refs, outputTextRef{container: container, key: key, path: path})
}

func (inspection *nonStreamOutputInspection) markIncomplete(path string) {
	inspection.incompletePaths = append(inspection.incompletePaths, path)
}

func (inspection *nonStreamOutputInspection) collectTextValue(container map[string]interface{}, key, path string) {
	value, exists := container[key]
	if !exists || value == nil {
		return
	}
	if text, ok := value.(string); ok {
		inspection.appendText(container, key, path, text)
		return
	}
	parts, ok := value.([]interface{})
	if !ok {
		inspection.markIncomplete(path)
		return
	}
	for partIndex, rawPart := range parts {
		partPath := fmt.Sprintf("%s[%d]", path, partIndex)
		part, ok := rawPart.(map[string]interface{})
		if !ok {
			inspection.markIncomplete(partPath)
			continue
		}
		if partType, exists := part["type"]; exists && partType != nil {
			typeName, ok := partType.(string)
			if !ok || (typeName != "text" && typeName != "output_text") {
				inspection.markIncomplete(partPath + ".type")
			}
		}
		textValue, exists := part["text"]
		if !exists || textValue == nil {
			inspection.markIncomplete(partPath + ".text")
			continue
		}
		text, ok := textValue.(string)
		if !ok {
			inspection.markIncomplete(partPath + ".text")
			continue
		}
		inspection.appendText(part, "text", partPath+".text", text)
	}
}

func inspectNonStreamOutputTexts(response map[string]interface{}) nonStreamOutputInspection {
	choices, _ := response["choices"].([]interface{})
	inspection := nonStreamOutputInspection{
		texts: make([]guardrail.Text, 0, len(choices)),
		refs:  make([]outputTextRef, 0, len(choices)),
	}
	for index, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]interface{})
		if !ok {
			continue
		}
		if message, ok := choice["message"].(map[string]interface{}); ok {
			for _, key := range []string{"content", "refusal", "reasoning_content", "reasoning"} {
				inspection.collectTextValue(message, key, fmt.Sprintf("choices[%d].message.%s", index, key))
			}
			if functionCall, ok := message["function_call"].(map[string]interface{}); ok {
				if arguments, ok := functionCall["arguments"].(string); ok {
					path := fmt.Sprintf("choices[%d].message.function_call.arguments", index)
					inspection.appendText(functionCall, "arguments", path, arguments)
				}
			}
			if toolCalls, ok := message["tool_calls"].([]interface{}); ok {
				for toolIndex, rawTool := range toolCalls {
					tool, ok := rawTool.(map[string]interface{})
					if !ok {
						continue
					}
					function, ok := tool["function"].(map[string]interface{})
					if !ok {
						continue
					}
					if arguments, ok := function["arguments"].(string); ok {
						path := fmt.Sprintf("choices[%d].message.tool_calls[%d].function.arguments", index, toolIndex)
						inspection.appendText(function, "arguments", path, arguments)
					}
				}
			}
		}
		if text, ok := choice["text"].(string); ok {
			path := fmt.Sprintf("choices[%d].text", index)
			inspection.appendText(choice, "text", path, text)
		}
	}
	return inspection
}

// nonStreamOutputTexts preserves the existing extraction API for callers that
// do not yet consume completeness. Enforced delivery must use
// evaluateNonStreamOutputGuardrail so an unsupported text shape cannot be
// mistaken for a clean inspection.
func nonStreamOutputTexts(response map[string]interface{}) ([]guardrail.Text, []outputTextRef) {
	inspection := inspectNonStreamOutputTexts(response)
	return inspection.texts, inspection.refs
}

func evaluateNonStreamOutputGuardrail(ctx context.Context, pinned *guardrail.Pinned, response map[string]interface{}) (guardrail.Result, []outputTextRef) {
	inspection := inspectNonStreamOutputTexts(response)
	result := pinned.EvaluateWithCompleteness(ctx, guardrail.StageNonStreamOutput, inspection.texts, inspection.complete())
	return result, inspection.refs
}

func applyOutputGuardrailTransform(result guardrail.Result, refs []outputTextRef) error {
	if !result.Transformed {
		return nil
	}
	if len(result.Texts) != len(refs) {
		return fmt.Errorf("guardrail output transform shape changed")
	}
	for index, ref := range refs {
		if result.Texts[index].Path != ref.path {
			return fmt.Errorf("guardrail output transform path changed")
		}
		if strings.HasSuffix(ref.path, ".arguments") && !json.Valid([]byte(result.Texts[index].Content)) {
			return fmt.Errorf("guardrail output transform invalidated structured arguments")
		}
	}
	for index, ref := range refs {
		ref.container[ref.key] = result.Texts[index].Content
	}
	return nil
}

func (relay *Relay) recordGuardrailResult(ctx context.Context, requestID, model, requestedModel, stage string, result guardrail.Result, route *RouteResult, statusCode int, upstreamRequestID string) {
	for _, receipt := range result.Receipts {
		severity := "info"
		if receipt.Degraded || result.Decision == guardrail.DecisionBlock {
			severity = "warning"
		}
		fingerprints := receipt.Fingerprints
		if len(fingerprints) > 8 {
			fingerprints = fingerprints[:8]
		}
		categories := make([]string, 0, len(receipt.Categories))
		for category, count := range receipt.Categories {
			categories = append(categories, fmt.Sprintf("%s=%d", category, count))
		}
		sort.Strings(categories)
		relay.recordTraceEvent(ctx, requestID, "guardrail.decision", stage, severity, model, requestedModel, route, statusCode, int(receipt.LatencyMS), map[string]interface{}{
			"guardrail_policy_set_version": result.PolicySetVersion, "guardrail_policy_ref": receipt.PolicyRef,
			"guardrail_policy_version": receipt.PolicyVersion, "guardrail_detector_ref": receipt.DetectorRef,
			"guardrail_detector_version": receipt.DetectorVersion, "guardrail_mode": receipt.Mode,
			"guardrail_action": receipt.Action, "guardrail_outcome": receipt.Outcome, "guardrail_failure_mode": receipt.FailureMode,
			"guardrail_mandatory": receipt.Mandatory, "guardrail_match_count": receipt.MatchCount,
			"guardrail_categories": boundedGuardrailList(categories, 240), "guardrail_finding_fingerprints": strings.Join(fingerprints, ","),
			"guardrail_decision": result.Decision, "guardrail_transformed": result.Transformed, "guardrail_degraded": result.Degraded,
		}, upstreamRequestID)
	}
}

func boundedGuardrailList(values []string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	var out strings.Builder
	for _, value := range values {
		extra := len(value)
		if out.Len() > 0 {
			extra++
		}
		if out.Len()+extra > maxBytes {
			break
		}
		if out.Len() > 0 {
			out.WriteByte(',')
		}
		out.WriteString(value)
	}
	return out.String()
}

func guardrailBlockedByUnavailable(result guardrail.Result) bool {
	if result.Decision != guardrail.DecisionBlock || !result.Degraded {
		return false
	}
	for _, receipt := range result.Receipts {
		if receipt.Outcome == guardrail.OutcomeUnavailable {
			return true
		}
	}
	return false
}

func writeInputGuardrailBlock(w http.ResponseWriter, unavailable bool) {
	if unavailable {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "server_error", "guardrail_error.mandatory_detector_unavailable", "Mandatory safety inspection is temporarily unavailable")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_request_error", "guardrail_error.input_blocked", "Request content was blocked by policy")
}

func writeOutputGuardrailBlock(w http.ResponseWriter, unavailable bool) {
	code := "guardrail_error.output_blocked"
	message := "Model output was withheld by policy"
	status := http.StatusForbidden
	if unavailable {
		code = "guardrail_error.mandatory_detector_unavailable"
		message = "Model output was withheld because mandatory safety inspection was unavailable"
		status = http.StatusServiceUnavailable
		w.Header().Set("Retry-After", "1")
	}
	writeError(w, status, "permission_error", code, message)
}

func normalizedGuardrailStage(stage string) string {
	return "guardrail_" + strings.TrimSpace(stage)
}
