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
		for _, part := range message.textParts() {
			texts = append(texts, guardrail.Text{
				Path:    fmt.Sprintf("messages[%d].%s", index, part.Path),
				Content: part.Content,
			})
		}
	}
	return texts
}

type chatInputTextRef struct {
	object         map[string]interface{}
	array          []interface{}
	key            string
	index          int
	path           string
	original       string
	transformable  bool
	structuredJSON bool
}

func (ref chatInputTextRef) set(value string) {
	if !ref.transformable {
		return
	}
	if ref.object != nil {
		ref.object[ref.key] = value
		return
	}
	ref.array[ref.index] = value
}

type chatInputGuardrailInspection struct {
	payload         map[string]interface{}
	texts           []guardrail.Text
	refs            []chatInputTextRef
	incompletePaths []string
}

func (inspection chatInputGuardrailInspection) complete() bool {
	return len(inspection.incompletePaths) == 0
}

func (inspection *chatInputGuardrailInspection) appendObjectText(object map[string]interface{}, key, path, content string, structuredJSON bool) {
	inspection.texts = append(inspection.texts, guardrail.Text{Path: path, Content: content})
	inspection.refs = append(inspection.refs, chatInputTextRef{
		object: object, key: key, path: path, original: content, transformable: true, structuredJSON: structuredJSON,
	})
}

func (inspection *chatInputGuardrailInspection) appendArrayText(array []interface{}, index int, path, content string) {
	inspection.texts = append(inspection.texts, guardrail.Text{Path: path, Content: content})
	inspection.refs = append(inspection.refs, chatInputTextRef{array: array, index: index, path: path, original: content, transformable: true})
}

func (inspection *chatInputGuardrailInspection) appendReadOnlyText(path, content string) {
	inspection.texts = append(inspection.texts, guardrail.Text{Path: path, Content: content})
	inspection.refs = append(inspection.refs, chatInputTextRef{path: path, original: content})
}

func (inspection *chatInputGuardrailInspection) markIncomplete(path string) {
	inspection.incompletePaths = append(inspection.incompletePaths, path)
}

func inspectChatInputGuardrail(body []byte) (chatInputGuardrailInspection, error) {
	inspection := chatInputGuardrailInspection{}
	if err := decodeJSONUseNumber(body, &inspection.payload); err != nil || inspection.payload == nil {
		return inspection, fmt.Errorf("guardrail chat content envelope is invalid")
	}
	messages, ok := inspection.payload["messages"].([]interface{})
	if !ok {
		inspection.markIncomplete("messages")
	} else {
		for messageIndex, rawMessage := range messages {
			messagePath := fmt.Sprintf("messages[%d]", messageIndex)
			message, ok := rawMessage.(map[string]interface{})
			if !ok {
				inspection.markIncomplete(messagePath)
				continue
			}
			for _, field := range []string{"role", "name", "tool_call_id"} {
				inspection.collectReadOnlyOptionalString(message, field, messagePath+"."+field)
			}
			inspection.collectTextValue(message, "content", messagePath+".content")
			for _, field := range []string{"refusal", "reasoning", "reasoning_content"} {
				inspection.collectOptionalString(message, field, messagePath+"."+field, false)
			}
			if rawCall, exists := message["function_call"]; exists && rawCall != nil {
				inspection.collectFunctionCall(rawCall, messagePath+".function_call")
			}
			if rawCalls, exists := message["tool_calls"]; exists && rawCalls != nil {
				calls, ok := rawCalls.([]interface{})
				if !ok {
					inspection.markIncomplete(messagePath + ".tool_calls")
				} else {
					for callIndex, rawCall := range calls {
						callPath := fmt.Sprintf("%s.tool_calls[%d]", messagePath, callIndex)
						call, ok := rawCall.(map[string]interface{})
						if !ok {
							inspection.markIncomplete(callPath)
							continue
						}
						inspection.collectRequiredReadOnlyString(call, "id", callPath+".id")
						inspection.collectRequiredReadOnlyString(call, "type", callPath+".type")
						function, exists := call["function"]
						if !exists || function == nil {
							inspection.markIncomplete(callPath + ".function")
							continue
						}
						inspection.collectFunctionCall(function, callPath+".function")
					}
				}
			}
		}
	}
	inspection.collectFunctionCollections("tools")
	inspection.collectFunctionCollections("functions")
	inspection.collectFunctionChoice("tool_choice", true)
	inspection.collectFunctionChoice("function_call", false)
	inspection.collectResponseSchema()
	inspection.collectPrediction()
	for _, field := range []string{"guided_regex", "guided_grammar"} {
		inspection.collectOptionalString(inspection.payload, field, field, false)
	}
	if rawChoices, exists := inspection.payload["guided_choice"]; exists && rawChoices != nil {
		choices, ok := rawChoices.([]interface{})
		if !ok {
			inspection.markIncomplete("guided_choice")
		} else {
			inspection.collectStringLeaves(choices, "guided_choice")
		}
	}
	if guidedJSON, exists := inspection.payload["guided_json"]; exists && guidedJSON != nil {
		inspection.collectSchemaStrings(guidedJSON, "guided_json")
	}
	return inspection, nil
}

func (inspection *chatInputGuardrailInspection) collectOptionalString(object map[string]interface{}, key, path string, structuredJSON bool) {
	value, exists := object[key]
	if !exists || value == nil {
		return
	}
	text, ok := value.(string)
	if !ok {
		inspection.markIncomplete(path)
		return
	}
	inspection.appendObjectText(object, key, path, text, structuredJSON)
}

func (inspection *chatInputGuardrailInspection) collectReadOnlyOptionalString(object map[string]interface{}, key, path string) {
	value, exists := object[key]
	if !exists || value == nil {
		return
	}
	text, ok := value.(string)
	if !ok {
		inspection.markIncomplete(path)
		return
	}
	inspection.appendReadOnlyText(path, text)
}

func (inspection *chatInputGuardrailInspection) collectRequiredReadOnlyString(object map[string]interface{}, key, path string) {
	if value, exists := object[key]; !exists || value == nil {
		inspection.markIncomplete(path)
		return
	}
	inspection.collectReadOnlyOptionalString(object, key, path)
}

func (inspection *chatInputGuardrailInspection) collectTextValue(object map[string]interface{}, key, path string) {
	value, exists := object[key]
	if !exists || value == nil {
		return
	}
	if text, ok := value.(string); ok {
		inspection.appendObjectText(object, key, path, text, false)
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
		kind := "text"
		if rawKind, exists := part["type"]; exists {
			var ok bool
			kind, ok = rawKind.(string)
			if !ok {
				inspection.markIncomplete(partPath + ".type")
				continue
			}
		}
		field := "text"
		switch kind {
		case "text", "input_text", "output_text":
		case "refusal":
			field = "refusal"
		default:
			inspection.markIncomplete(partPath + ".type")
			continue
		}
		inspection.collectOptionalString(part, field, partPath+"."+field, false)
		if _, exists := part[field]; !exists {
			inspection.markIncomplete(partPath + "." + field)
		}
	}
}

func (inspection *chatInputGuardrailInspection) collectFunctionCall(raw interface{}, path string) {
	function, ok := raw.(map[string]interface{})
	if !ok {
		inspection.markIncomplete(path)
		return
	}
	inspection.collectRequiredReadOnlyString(function, "name", path+".name")
	arguments, exists := function["arguments"]
	if !exists || arguments == nil {
		inspection.markIncomplete(path + ".arguments")
		return
	}
	text, ok := arguments.(string)
	if !ok {
		inspection.markIncomplete(path + ".arguments")
		return
	}
	inspection.appendObjectText(function, "arguments", path+".arguments", text, true)
}

func (inspection *chatInputGuardrailInspection) collectFunctionCollections(field string) {
	rawCollection, exists := inspection.payload[field]
	if !exists || rawCollection == nil {
		return
	}
	collection, ok := rawCollection.([]interface{})
	if !ok {
		inspection.markIncomplete(field)
		return
	}
	for index, rawEntry := range collection {
		entryPath := fmt.Sprintf("%s[%d]", field, index)
		entry, ok := rawEntry.(map[string]interface{})
		if !ok {
			inspection.markIncomplete(entryPath)
			continue
		}
		function := entry
		functionPath := entryPath
		if field == "tools" {
			inspection.collectRequiredReadOnlyString(entry, "type", entryPath+".type")
			rawFunction, exists := entry["function"]
			if !exists || rawFunction == nil {
				inspection.markIncomplete(entryPath + ".function")
				continue
			}
			function, ok = rawFunction.(map[string]interface{})
			if !ok {
				inspection.markIncomplete(entryPath + ".function")
				continue
			}
			functionPath += ".function"
		}
		if _, exists := function["name"]; !exists {
			inspection.markIncomplete(functionPath + ".name")
		} else {
			inspection.collectReadOnlyOptionalString(function, "name", functionPath+".name")
		}
		inspection.collectOptionalString(function, "description", functionPath+".description", false)
		if parameters, exists := function["parameters"]; exists && parameters != nil {
			inspection.collectSchemaStrings(parameters, functionPath+".parameters")
		}
	}
}

func (inspection *chatInputGuardrailInspection) collectFunctionChoice(field string, wrapped bool) {
	rawChoice, exists := inspection.payload[field]
	if !exists || rawChoice == nil {
		return
	}
	if text, ok := rawChoice.(string); ok {
		inspection.appendReadOnlyText(field, text)
		return
	}
	choice, ok := rawChoice.(map[string]interface{})
	if !ok {
		inspection.markIncomplete(field)
		return
	}
	function := choice
	path := field
	if wrapped {
		inspection.collectRequiredReadOnlyString(choice, "type", field+".type")
		rawFunction, exists := choice["function"]
		if !exists || rawFunction == nil {
			inspection.markIncomplete(field + ".function")
			return
		}
		function, ok = rawFunction.(map[string]interface{})
		if !ok {
			inspection.markIncomplete(field + ".function")
			return
		}
		path += ".function"
	}
	inspection.collectRequiredReadOnlyString(function, "name", path+".name")
}

func (inspection *chatInputGuardrailInspection) collectResponseSchema() {
	rawFormat, exists := inspection.payload["response_format"]
	if !exists || rawFormat == nil {
		return
	}
	format, ok := rawFormat.(map[string]interface{})
	if !ok {
		inspection.markIncomplete("response_format")
		return
	}
	inspection.collectRequiredReadOnlyString(format, "type", "response_format.type")
	rawDefinition, exists := format["json_schema"]
	if !exists || rawDefinition == nil {
		return
	}
	definition, ok := rawDefinition.(map[string]interface{})
	if !ok {
		inspection.markIncomplete("response_format.json_schema")
		return
	}
	if _, exists := definition["name"]; !exists {
		inspection.markIncomplete("response_format.json_schema.name")
	} else {
		inspection.collectReadOnlyOptionalString(definition, "name", "response_format.json_schema.name")
	}
	inspection.collectOptionalString(definition, "description", "response_format.json_schema.description", false)
	if schema, exists := definition["schema"]; exists && schema != nil {
		inspection.collectSchemaStrings(schema, "response_format.json_schema.schema")
	}
}

func (inspection *chatInputGuardrailInspection) collectPrediction() {
	rawPrediction, exists := inspection.payload["prediction"]
	if !exists || rawPrediction == nil {
		return
	}
	prediction, ok := rawPrediction.(map[string]interface{})
	if !ok {
		inspection.markIncomplete("prediction")
		return
	}
	inspection.collectReadOnlyOptionalString(prediction, "type", "prediction.type")
	inspection.collectTextValue(prediction, "content", "prediction.content")
}

func (inspection *chatInputGuardrailInspection) collectSchemaStrings(value interface{}, path string) {
	switch typed := value.(type) {
	case string:
		inspection.appendReadOnlyText(path, typed)
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := typed[key]
			childPath := path + "." + key
			inspection.appendReadOnlyText(path+"{key:"+key+"}", key)
			if text, ok := child.(string); ok {
				inspection.appendReadOnlyText(childPath, text)
				continue
			}
			inspection.collectSchemaStrings(child, childPath)
		}
	case []interface{}:
		inspection.collectReadOnlyStringLeaves(typed, path)
	case json.Number, bool, nil:
	default:
		inspection.markIncomplete(path)
	}
}

func (inspection *chatInputGuardrailInspection) collectReadOnlyStringLeaves(values []interface{}, path string) {
	for index, value := range values {
		childPath := fmt.Sprintf("%s[%d]", path, index)
		switch typed := value.(type) {
		case string:
			inspection.appendReadOnlyText(childPath, typed)
		case map[string]interface{}, []interface{}:
			inspection.collectSchemaStrings(typed, childPath)
		case json.Number, bool, nil:
		default:
			inspection.markIncomplete(childPath)
		}
	}
}

func (inspection *chatInputGuardrailInspection) collectStringLeaves(values []interface{}, path string) {
	for index, value := range values {
		childPath := fmt.Sprintf("%s[%d]", path, index)
		switch typed := value.(type) {
		case string:
			inspection.appendArrayText(values, index, childPath, typed)
		case map[string]interface{}, []interface{}:
			inspection.collectSchemaStrings(typed, childPath)
		case json.Number, bool, nil:
		default:
			inspection.markIncomplete(childPath)
		}
	}
}

func applyChatGuardrailTransform(body []byte, req *ChatCompletionRequest, result guardrail.Result) ([]byte, error) {
	if !result.Transformed {
		return body, nil
	}
	if req == nil {
		return nil, fmt.Errorf("guardrail chat transform shape changed")
	}
	inspection, err := inspectChatInputGuardrail(body)
	if err != nil {
		return nil, err
	}
	if len(result.Texts) != len(inspection.refs) {
		return nil, fmt.Errorf("guardrail chat transform shape changed")
	}
	for index, ref := range inspection.refs {
		if result.Texts[index].Path != ref.path {
			return nil, fmt.Errorf("guardrail chat transform path changed")
		}
		if !ref.transformable && result.Texts[index].Content != ref.original {
			return nil, fmt.Errorf("guardrail chat transform cannot safely rewrite structured identifiers")
		}
		if ref.structuredJSON && !json.Valid([]byte(result.Texts[index].Content)) {
			return nil, fmt.Errorf("guardrail chat transform invalidated structured arguments")
		}
	}
	for index, ref := range inspection.refs {
		ref.set(result.Texts[index].Content)
	}
	transformed, err := json.Marshal(inspection.payload)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(transformed, req); err != nil {
		return nil, fmt.Errorf("guardrail chat transform validation: %w", err)
	}
	return transformed, nil
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
	if err := decodeJSONUseNumber(body, &payload); err != nil {
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
	container     map[string]interface{}
	key           string
	path          string
	original      string
	transformable bool
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
	inspection.refs = append(inspection.refs, outputTextRef{container: container, key: key, path: path, original: content, transformable: true})
}

func (inspection *nonStreamOutputInspection) appendReadOnlyText(path, content string) {
	inspection.texts = append(inspection.texts, guardrail.Text{Path: path, Content: content})
	inspection.refs = append(inspection.refs, outputTextRef{path: path, original: content})
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
	rawChoices, exists := response["choices"]
	choices, ok := rawChoices.([]interface{})
	inspection := nonStreamOutputInspection{
		texts: make([]guardrail.Text, 0, len(choices)),
		refs:  make([]outputTextRef, 0, len(choices)),
	}
	if !streamMapKeysSupported(response, "id", "object", "created", "model", "system_fingerprint", "service_tier", "choices", "usage", "metadata") {
		inspection.markIncomplete(chatDeliveryWhitelistVersion + ".root")
	}
	for _, key := range []string{"id", "object", "model", "system_fingerprint", "service_tier"} {
		if value, exists := response[key]; exists && value != nil {
			if _, ok := value.(string); !ok {
				inspection.markIncomplete(chatDeliveryWhitelistVersion + "." + key)
			}
		}
	}
	if value, exists := response["created"]; exists && value != nil {
		number, ok := value.(json.Number)
		if !ok {
			inspection.markIncomplete(chatDeliveryWhitelistVersion + ".created")
		} else if created, err := number.Int64(); err != nil || created < 0 {
			inspection.markIncomplete(chatDeliveryWhitelistVersion + ".created")
		}
	}
	if usage, exists := response["usage"]; exists && usage != nil && !streamUsageShapeSupported(usage) {
		inspection.markIncomplete(chatDeliveryWhitelistVersion + ".usage")
	}
	if metadata, exists := response["metadata"]; exists && metadata != nil {
		inspection.collectOpaqueOutputText(metadata, "metadata")
	}
	if !exists || !ok {
		inspection.markIncomplete("choices")
		return inspection
	}
	for index, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]interface{})
		if !ok {
			inspection.markIncomplete(fmt.Sprintf("choices[%d]", index))
			continue
		}
		if !streamMapKeysSupported(choice, "index", "message", "text", "finish_reason", "logprobs") {
			inspection.markIncomplete(fmt.Sprintf("choices[%d]", index))
		}
		if _, valid := streamEnvelopeIndex(choice, index); !valid {
			inspection.markIncomplete(fmt.Sprintf("choices[%d].index", index))
		}
		if finish, exists := choice["finish_reason"]; exists && finish != nil {
			value, ok := finish.(string)
			if !ok || !streamFinishReasonSupported(value) {
				inspection.markIncomplete(fmt.Sprintf("choices[%d].finish_reason", index))
			}
		}
		if logprobs, exists := choice["logprobs"]; exists && logprobs != nil {
			inspection.collectOpaqueOutputText(logprobs, fmt.Sprintf("choices[%d].logprobs", index))
		}
		if rawMessage, exists := choice["message"]; exists && rawMessage != nil {
			message, ok := rawMessage.(map[string]interface{})
			if !ok {
				inspection.markIncomplete(fmt.Sprintf("choices[%d].message", index))
				continue
			}
			if !streamMapKeysSupported(message, "role", "content", "refusal", "reasoning_content", "reasoning", "function_call", "tool_calls") {
				inspection.markIncomplete(fmt.Sprintf("choices[%d].message", index))
			}
			if role, exists := message["role"]; exists && role != nil {
				value, ok := role.(string)
				if !ok || (value != "assistant" && value != "tool" && value != "function") {
					inspection.markIncomplete(fmt.Sprintf("choices[%d].message.role", index))
				}
			}
			for _, key := range []string{"content", "refusal", "reasoning_content", "reasoning"} {
				inspection.collectTextValue(message, key, fmt.Sprintf("choices[%d].message.%s", index, key))
			}
			if rawFunctionCall, exists := message["function_call"]; exists && rawFunctionCall != nil {
				path := fmt.Sprintf("choices[%d].message.function_call", index)
				functionCall, ok := rawFunctionCall.(map[string]interface{})
				if !ok {
					inspection.markIncomplete(path)
				} else if !streamMapKeysSupported(functionCall, "name", "arguments") {
					inspection.markIncomplete(path)
				} else {
					if name, exists := functionCall["name"]; !exists || name == nil {
						inspection.markIncomplete(path + ".name")
					} else if text, ok := name.(string); ok {
						inspection.appendReadOnlyText(path+".name", text)
					} else {
						inspection.markIncomplete(path + ".name")
					}
					if arguments, exists := functionCall["arguments"]; !exists || arguments == nil {
						inspection.markIncomplete(path + ".arguments")
					} else if text, ok := arguments.(string); ok {
						inspection.appendText(functionCall, "arguments", path+".arguments", text)
					} else {
						inspection.markIncomplete(path + ".arguments")
					}
				}
			}
			if rawToolCalls, exists := message["tool_calls"]; exists && rawToolCalls != nil {
				toolCalls, ok := rawToolCalls.([]interface{})
				if !ok {
					inspection.markIncomplete(fmt.Sprintf("choices[%d].message.tool_calls", index))
					continue
				}
				for toolIndex, rawTool := range toolCalls {
					toolPath := fmt.Sprintf("choices[%d].message.tool_calls[%d]", index, toolIndex)
					tool, ok := rawTool.(map[string]interface{})
					if !ok {
						inspection.markIncomplete(toolPath)
						continue
					}
					if !streamMapKeysSupported(tool, "id", "type", "function") {
						inspection.markIncomplete(toolPath)
					}
					if value, exists := tool["id"]; !exists || value == nil {
						inspection.markIncomplete(toolPath + ".id")
					} else if text, ok := value.(string); ok {
						inspection.appendReadOnlyText(toolPath+".id", text)
					} else {
						inspection.markIncomplete(toolPath + ".id")
					}
					if value, exists := tool["type"]; !exists || value == nil {
						inspection.markIncomplete(toolPath + ".type")
					} else if text, ok := value.(string); !ok || text != "function" {
						inspection.markIncomplete(toolPath + ".type")
					} else {
						inspection.appendReadOnlyText(toolPath+".type", text)
					}
					rawFunction, exists := tool["function"]
					function, ok := rawFunction.(map[string]interface{})
					if !exists || !ok {
						inspection.markIncomplete(toolPath + ".function")
						continue
					}
					if !streamMapKeysSupported(function, "name", "arguments") {
						inspection.markIncomplete(toolPath + ".function")
					}
					if value, exists := function["name"]; !exists || value == nil {
						inspection.markIncomplete(toolPath + ".function.name")
					} else if text, ok := value.(string); ok {
						inspection.appendReadOnlyText(toolPath+".function.name", text)
					} else {
						inspection.markIncomplete(toolPath + ".function.name")
					}
					arguments, exists := function["arguments"]
					path := toolPath + ".function.arguments"
					if !exists {
						inspection.markIncomplete(path)
					} else if text, ok := arguments.(string); ok {
						inspection.appendText(function, "arguments", path, text)
					} else {
						inspection.markIncomplete(path)
					}
				}
			}
		}
		inspection.collectTextValue(choice, "text", fmt.Sprintf("choices[%d].text", index))
	}
	return inspection
}

func (inspection *nonStreamOutputInspection) collectOpaqueOutputText(value interface{}, path string) {
	switch typed := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			inspection.appendReadOnlyText(path+"{key:"+key+"}", key)
			inspection.collectOpaqueOutputText(typed[key], path+"."+key)
		}
	case []interface{}:
		for index, item := range typed {
			inspection.collectOpaqueOutputText(item, fmt.Sprintf("%s[%d]", path, index))
		}
	case string:
		inspection.appendReadOnlyText(path, typed)
	case json.Number, bool, nil:
	default:
		inspection.markIncomplete(path)
	}
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
		if !ref.transformable && result.Texts[index].Content != ref.original {
			return fmt.Errorf("guardrail output transform cannot safely rewrite structured metadata")
		}
		if strings.HasSuffix(ref.path, ".arguments") && !json.Valid([]byte(result.Texts[index].Content)) {
			return fmt.Errorf("guardrail output transform invalidated structured arguments")
		}
	}
	for index, ref := range refs {
		if ref.transformable {
			ref.container[ref.key] = result.Texts[index].Content
		}
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
