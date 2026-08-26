package endpointprotocol

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
)

type publicAnthropicToolsWireRequest struct {
	Model        string                        `json:"model"`
	MaxTokens    json.Number                   `json:"max_tokens"`
	Messages     []publicAnthropicToolsMessage `json:"messages"`
	System       string                        `json:"system,omitempty"`
	Thinking     *anthropicThinkingRequest     `json:"thinking,omitempty"`
	OutputConfig *anthropicOutputConfigRequest `json:"output_config,omitempty"`
	Tools        []publicAnthropicClientTool   `json:"tools"`
	ToolChoice   *publicAnthropicToolChoice    `json:"tool_choice,omitempty"`
	Stream       bool                          `json:"stream"`
}

type publicAnthropicClientTool struct {
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type publicAnthropicToolChoice struct {
	Type                   string `json:"type"`
	Name                   string `json:"name,omitempty"`
	DisableParallelToolUse *bool  `json:"disable_parallel_tool_use,omitempty"`
}

type publicAnthropicToolsMessage struct {
	Role          string
	ContentString *string
	Blocks        []publicAnthropicToolsContentBlock
}

func (message publicAnthropicToolsMessage) MarshalJSON() ([]byte, error) {
	content := any(message.Blocks)
	if message.ContentString != nil {
		content = *message.ContentString
	}
	return json.Marshal(struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}{Role: message.Role, Content: content})
}

type publicAnthropicToolsContentBlock struct {
	Type      string          `json:"type"`
	Text      *string         `json:"text,omitempty"`
	Thinking  *string         `json:"thinking,omitempty"`
	Signature *string         `json:"signature,omitempty"`
	Data      *string         `json:"data,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   *string         `json:"content,omitempty"`
	IsError   *bool           `json:"is_error,omitempty"`
}

type publicAnthropicToolsTextRef struct {
	path         string
	text         string
	identityOnly bool
	kind         string
	message      int
	block        int
	tool         int
}

type publicAnthropicToolsRequestDocument struct {
	request publicAnthropicToolsWireRequest
	refs    []publicAnthropicToolsTextRef
}

func (document publicAnthropicToolsRequestDocument) canonicalBody() ([]byte, error) {
	return json.Marshal(document.request)
}

func (document publicAnthropicToolsRequestDocument) texts() []PublicText {
	result := make([]PublicText, len(document.refs))
	for index, ref := range document.refs {
		result[index] = PublicText{Path: ref.path, Text: ref.text}
	}
	return result
}

func (document publicAnthropicToolsRequestDocument) withTexts(replacements []string) (publicRequestDocument, error) {
	if len(replacements) != len(document.refs) {
		return nil, replacementError(len(document.refs), len(replacements))
	}
	cloned := clonePublicAnthropicToolsDocument(document)
	for index, replacement := range replacements {
		ref := document.refs[index]
		if replacement == "" {
			return nil, protocolError(ErrInvalidRequest, "guardrail_text_empty")
		}
		if ref.identityOnly {
			if replacement != ref.text {
				return nil, protocolError(ErrPublicTextReplacement, "structured_tool_rewrite_forbidden")
			}
			continue
		}
		switch ref.kind {
		case "system":
			cloned.request.System = replacement
		case "message":
			cloned.request.Messages[ref.message].ContentString = &replacement
		case "text":
			cloned.request.Messages[ref.message].Blocks[ref.block].Text = &replacement
		case "tool_result":
			cloned.request.Messages[ref.message].Blocks[ref.block].Content = &replacement
		default:
			return nil, protocolError(ErrPublicTextReplacement, "tool_text_reference_invalid")
		}
		cloned.refs[index].text = replacement
	}
	return cloned, nil
}

func clonePublicAnthropicToolsDocument(document publicAnthropicToolsRequestDocument) publicAnthropicToolsRequestDocument {
	result := document
	result.refs = slices.Clone(document.refs)
	result.request.Messages = make([]publicAnthropicToolsMessage, len(document.request.Messages))
	for index, message := range document.request.Messages {
		result.request.Messages[index] = message
		if message.ContentString != nil {
			value := *message.ContentString
			result.request.Messages[index].ContentString = &value
		}
		result.request.Messages[index].Blocks = make([]publicAnthropicToolsContentBlock, len(message.Blocks))
		for blockIndex, block := range message.Blocks {
			result.request.Messages[index].Blocks[blockIndex] = clonePublicAnthropicToolsBlock(block)
		}
	}
	result.request.Tools = make([]publicAnthropicClientTool, len(document.request.Tools))
	for index, tool := range document.request.Tools {
		result.request.Tools[index] = tool
		result.request.Tools[index].InputSchema = slices.Clone(tool.InputSchema)
		if tool.Description != nil {
			value := *tool.Description
			result.request.Tools[index].Description = &value
		}
	}
	if document.request.ToolChoice != nil {
		choice := *document.request.ToolChoice
		if choice.DisableParallelToolUse != nil {
			value := *choice.DisableParallelToolUse
			choice.DisableParallelToolUse = &value
		}
		result.request.ToolChoice = &choice
	}
	return result
}

func clonePublicAnthropicToolsBlock(block publicAnthropicToolsContentBlock) publicAnthropicToolsContentBlock {
	result := block
	result.Text = cloneStringPointer(block.Text)
	result.Thinking = cloneStringPointer(block.Thinking)
	result.Signature = cloneStringPointer(block.Signature)
	result.Data = cloneStringPointer(block.Data)
	result.Content = cloneStringPointer(block.Content)
	if block.IsError != nil {
		value := *block.IsError
		result.IsError = &value
	}
	result.Input = slices.Clone(block.Input)
	return result
}

func publicAnthropicToolsShapeRequested(envelope map[string]json.RawMessage) bool {
	if _, present := envelope["tools"]; present {
		return true
	}
	if _, present := envelope["tool_choice"]; present {
		return true
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(envelope["messages"], &messages) != nil {
		return false
	}
	for _, message := range messages {
		if bytes.HasPrefix(bytes.TrimSpace(message["content"]), []byte("[")) {
			return true
		}
	}
	return false
}

func (profile publicAnthropicMessagesProfile) prepareToolsRequest(
	body []byte,
	requestedModel string,
	contract PublicRequestContract,
	envelope map[string]json.RawMessage,
	streaming bool,
) (*PublicPreparedRequest, error) {
	messagesContract := contract.AnthropicMessages
	toolsContract := messagesContract.FunctionTools
	if toolsContract == nil || (toolsContract.RequiresNonStream && streaming) {
		return nil, protocolError(ErrUnsupportedSemantics, "function_tools_require_non_stream")
	}
	model, ok := decodeRequiredString(envelope["model"])
	if !ok || model != requestedModel {
		return nil, protocolError(ErrInvalidRequest, "requested_model_mismatch")
	}
	maxNumber := json.Number(string(envelope["max_tokens"]))
	maxOutput, err := parseRequestInt64(maxNumber)
	if err != nil || maxOutput <= 0 {
		return nil, protocolError(ErrInvalidRequest, "max_tokens_invalid")
	}
	system := ""
	if raw, present := envelope["system"]; present {
		if value, valueOK := decodeJSONString(raw); !valueOK || value == "" || len(value) > maxRequestBytes/2 {
			return nil, protocolError(ErrInvalidRequest, "system_text_invalid")
		} else {
			system = value
		}
	}
	tools, declared, refs, err := decodePublicAnthropicClientTools(envelope["tools"], toolsContract)
	if err != nil {
		return nil, err
	}
	choice, semantics, err := decodePublicAnthropicToolChoice(envelope["tool_choice"], declared, toolsContract)
	if err != nil {
		return nil, err
	}
	thinking, outputConfig, reasoningSemantics, err := normalizePublicAnthropicReasoning(
		envelope, messagesContract, maxOutput, streaming,
	)
	if err != nil {
		return nil, err
	}
	semantics.ReasoningControlExplicit = reasoningSemantics.ReasoningControlExplicit
	semantics.ReasoningControlEffective = reasoningSemantics.ReasoningControlEffective
	semantics.ReasoningEffortExplicit = reasoningSemantics.ReasoningEffortExplicit
	semantics.ReasoningEffortEffective = reasoningSemantics.ReasoningEffortEffective
	semantics.ReasoningBudgetExplicit = reasoningSemantics.ReasoningBudgetExplicit
	semantics.ReasoningBudgetEffective = reasoningSemantics.ReasoningBudgetEffective
	semantics.Streaming = streaming

	var rawMessages []json.RawMessage
	if json.Unmarshal(envelope["messages"], &rawMessages) != nil || len(rawMessages) == 0 {
		return nil, protocolError(ErrInvalidRequest, "messages_missing")
	}
	messages, messageRefs, err := decodePublicAnthropicToolHistory(
		rawMessages, declared, messagesContract, semantics.ReasoningControlEffective,
	)
	if err != nil {
		return nil, err
	}
	refs = append(refs, messageRefs...)
	if system != "" {
		refs = append([]publicAnthropicToolsTextRef{{path: "system", text: system, kind: "system"}}, refs...)
	}
	request := publicAnthropicToolsWireRequest{
		Model: requestedModel, MaxTokens: maxNumber, Messages: messages, System: system,
		Thinking: thinking, OutputConfig: outputConfig, Tools: tools, ToolChoice: choice, Stream: false,
	}
	document := publicAnthropicToolsRequestDocument{request: request, refs: refs}
	if err := validatePublicRequestDocument(document); err != nil {
		return nil, err
	}
	return &PublicPreparedRequest{
		identity: profile.Descriptor().Identity, endpointKind: EndpointMessages,
		requestedModel: requestedModel, maxOutput: maxOutput, requestContract: contract,
		document: document, semantics: semantics,
	}, nil
}

func decodePublicAnthropicClientTools(
	raw json.RawMessage,
	contract *PublicAnthropicFunctionToolsContract,
) ([]publicAnthropicClientTool, map[string]struct{}, []publicAnthropicToolsTextRef, error) {
	if len(raw) == 0 || isExplicitJSONNull(raw) {
		return nil, nil, nil, protocolError(ErrInvalidRequest, "tools_required")
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) == 0 || len(values) > contract.MaxTools {
		return nil, nil, nil, protocolError(ErrInvalidRequest, "tools_invalid")
	}
	namePattern, err := regexp.Compile(contract.ToolNamePattern)
	if err != nil {
		return nil, nil, nil, protocolError(ErrInvalidConfiguration, "public_anthropic_tools_contract_invalid")
	}
	tools := make([]publicAnthropicClientTool, 0, len(values))
	declared := make(map[string]struct{}, len(values))
	refs := make([]publicAnthropicToolsTextRef, 0)
	for index, value := range values {
		var fields map[string]json.RawMessage
		if json.Unmarshal(value, &fields) != nil || fields == nil || len(fields) < 2 || len(fields) > 3 {
			return nil, nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_client_tool_definition")
		}
		for field := range fields {
			if field != "name" && field != "description" && field != "input_schema" {
				return nil, nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_client_tool_definition")
			}
		}
		name, nameOK := decodeRequiredString(fields["name"])
		if !nameOK || !namePattern.MatchString(name) {
			return nil, nil, nil, protocolError(ErrInvalidRequest, "tool_name_invalid")
		}
		if _, duplicate := declared[name]; duplicate {
			return nil, nil, nil, protocolError(ErrInvalidRequest, "tool_name_duplicate")
		}
		declared[name] = struct{}{}
		canonicalSchema, schemaErr := canonicalPublicJSONObject(fields["input_schema"], "tool_input_schema_invalid")
		if schemaErr != nil {
			return nil, nil, nil, schemaErr
		}
		var schemaFields map[string]json.RawMessage
		if json.Unmarshal(canonicalSchema, &schemaFields) != nil {
			return nil, nil, nil, protocolError(ErrInvalidRequest, "tool_input_schema_invalid")
		}
		if schemaType, schemaTypeOK := decodeRequiredString(schemaFields["type"]); !schemaTypeOK || schemaType != "object" {
			return nil, nil, nil, protocolError(ErrInvalidRequest, "tool_input_schema_object_required")
		}
		tool := publicAnthropicClientTool{Name: name, InputSchema: canonicalSchema}
		if rawDescription, present := fields["description"]; present {
			description, descriptionOK := decodeJSONString(rawDescription)
			if !descriptionOK {
				return nil, nil, nil, protocolError(ErrInvalidRequest, "tool_description_invalid")
			}
			tool.Description = &description
			if description != "" {
				refs = append(refs, publicAnthropicToolsTextRef{
					path: "tools[" + strconv.Itoa(index) + "].description", text: description,
					identityOnly: true, kind: "tool_description", tool: index,
				})
			}
		}
		for _, leaf := range publicJSONStringLeaves(canonicalSchema, "tools["+strconv.Itoa(index)+"].input_schema") {
			refs = append(refs, publicAnthropicToolsTextRef{
				path: leaf.Path, text: leaf.Text, identityOnly: true, kind: "tool_schema", tool: index,
			})
		}
		tools = append(tools, tool)
	}
	return tools, declared, refs, nil
}

func decodePublicAnthropicToolChoice(
	raw json.RawMessage,
	declared map[string]struct{},
	contract *PublicAnthropicFunctionToolsContract,
) (*publicAnthropicToolChoice, PublicInvocationSemantics, error) {
	semantics := PublicInvocationSemantics{FunctionToolsRequested: true}
	for name := range declared {
		semantics.DeclaredToolNames = append(semantics.DeclaredToolNames, name)
	}
	slices.Sort(semantics.DeclaredToolNames)
	choice := &publicAnthropicToolChoice{Type: contract.ProviderDefaultChoice}
	if len(raw) != 0 {
		if isExplicitJSONNull(raw) {
			return nil, PublicInvocationSemantics{}, protocolError(ErrInvalidRequest, "tool_choice_invalid")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return nil, PublicInvocationSemantics{}, protocolError(ErrInvalidRequest, "tool_choice_invalid")
		}
		value, ok := decodeRequiredString(fields["type"])
		if !ok || !slices.Contains(contract.ToolChoices, value) {
			return nil, PublicInvocationSemantics{}, protocolError(ErrInvalidRequest, "tool_choice_invalid")
		}
		choice.Type = value
		semantics.ToolChoiceExplicit = true
		if value == "tool" {
			name, nameOK := decodeRequiredString(fields["name"])
			if !nameOK || len(fields) < 2 {
				return nil, PublicInvocationSemantics{}, protocolError(ErrInvalidRequest, "named_tool_choice_invalid")
			}
			if _, exists := declared[name]; !exists {
				return nil, PublicInvocationSemantics{}, protocolError(ErrInvalidRequest, "named_tool_choice_undeclared")
			}
			choice.Name = name
			semantics.NamedToolChoice = name
		} else if _, present := fields["name"]; present {
			return nil, PublicInvocationSemantics{}, protocolError(ErrInvalidRequest, "tool_choice_name_forbidden")
		}
		if rawParallel, present := fields["disable_parallel_tool_use"]; present {
			if value == "none" {
				return nil, PublicInvocationSemantics{}, protocolError(ErrInvalidRequest, "tool_choice_parallel_forbidden")
			}
			var disabled bool
			if json.Unmarshal(rawParallel, &disabled) != nil || string(rawParallel) != strconv.FormatBool(disabled) {
				return nil, PublicInvocationSemantics{}, protocolError(ErrInvalidRequest, "disable_parallel_tool_use_invalid")
			}
			choice.DisableParallelToolUse = &disabled
			semantics.ParallelToolUseExplicit = true
		}
		wantMembers := 1
		if choice.Name != "" {
			wantMembers++
		}
		if choice.DisableParallelToolUse != nil {
			wantMembers++
		}
		if len(fields) != wantMembers {
			return nil, PublicInvocationSemantics{}, protocolError(ErrInvalidRequest, "tool_choice_invalid")
		}
	}
	semantics.ToolChoiceEffective = map[string]string{
		"auto": "auto", "none": "none", "any": "required", "tool": "named",
	}[choice.Type]
	disabled := choice.DisableParallelToolUse != nil && *choice.DisableParallelToolUse
	semantics.ParallelToolUseEffective = choice.Type != "none" && contract.ParallelProviderDefault && !disabled
	if !semantics.ToolChoiceExplicit {
		return nil, semantics, nil
	}
	return choice, semantics, nil
}

func decodePublicAnthropicToolHistory(
	rawMessages []json.RawMessage,
	declared map[string]struct{},
	contract *PublicAnthropicMessagesContract,
	reasoningEffective string,
) ([]publicAnthropicToolsMessage, []publicAnthropicToolsTextRef, error) {
	messages := make([]publicAnthropicToolsMessage, 0, len(rawMessages))
	refs := make([]publicAnthropicToolsTextRef, 0)
	pending := map[string]struct{}{}
	allIDs := map[string]struct{}{}
	reasoningDisabled := contract.Reasoning != nil && slices.Contains(contract.Reasoning.DisabledThinkingTypes, reasoningEffective)
	for messageIndex, rawMessage := range rawMessages {
		var fields map[string]json.RawMessage
		if json.Unmarshal(rawMessage, &fields) != nil || fields == nil || len(fields) != 2 {
			return nil, nil, protocolError(ErrInvalidRequest, "message_shape_invalid")
		}
		role, ok := decodeRequiredString(fields["role"])
		if !ok || (role != "user" && role != "assistant") {
			return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_message_role")
		}
		message := publicAnthropicToolsMessage{Role: role}
		if content, contentOK := decodeJSONString(fields["content"]); contentOK {
			if content == "" || len(pending) != 0 {
				return nil, nil, protocolError(ErrInvalidRequest, "tool_history_result_missing")
			}
			message.ContentString = &content
			refs = append(refs, publicAnthropicToolsTextRef{
				path: "messages[" + strconv.Itoa(messageIndex) + "].content", text: content,
				kind: "message", message: messageIndex,
			})
			messages = append(messages, message)
			continue
		}
		var rawBlocks []json.RawMessage
		if json.Unmarshal(fields["content"], &rawBlocks) != nil || len(rawBlocks) == 0 {
			return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_non_text_content")
		}
		if role == "assistant" {
			if len(pending) != 0 {
				return nil, nil, protocolError(ErrInvalidRequest, "tool_history_result_missing")
			}
			blocks, blockRefs, toolIDs, err := decodePublicAnthropicAssistantToolBlocks(
				rawBlocks, messageIndex, declared, allIDs, reasoningDisabled,
			)
			if err != nil {
				return nil, nil, err
			}
			message.Blocks = blocks
			refs = append(refs, blockRefs...)
			pending = toolIDs
		} else {
			if len(pending) == 0 {
				return nil, nil, protocolError(ErrInvalidRequest, "tool_result_without_tool_use")
			}
			blocks, blockRefs, err := decodePublicAnthropicUserToolResultBlocks(rawBlocks, messageIndex, pending)
			if err != nil {
				return nil, nil, err
			}
			message.Blocks = blocks
			refs = append(refs, blockRefs...)
			pending = map[string]struct{}{}
		}
		messages = append(messages, message)
	}
	if len(pending) != 0 || len(messages) == 0 || messages[len(messages)-1].Role != "user" {
		return nil, nil, protocolError(ErrInvalidRequest, "tool_history_incomplete")
	}
	return messages, refs, nil
}

func decodePublicAnthropicAssistantToolBlocks(
	rawBlocks []json.RawMessage,
	messageIndex int,
	declared map[string]struct{},
	allIDs map[string]struct{},
	reasoningDisabled bool,
) ([]publicAnthropicToolsContentBlock, []publicAnthropicToolsTextRef, map[string]struct{}, error) {
	blocks := make([]publicAnthropicToolsContentBlock, 0, len(rawBlocks))
	refs := make([]publicAnthropicToolsTextRef, 0)
	toolIDs := make(map[string]struct{})
	phase := "reasoning"
	for blockIndex, raw := range rawBlocks {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return nil, nil, nil, protocolError(ErrInvalidRequest, "assistant_tool_block_invalid")
		}
		kind, ok := decodeRequiredString(fields["type"])
		if !ok {
			return nil, nil, nil, protocolError(ErrInvalidRequest, "assistant_tool_block_invalid")
		}
		block := publicAnthropicToolsContentBlock{Type: kind}
		switch kind {
		case "thinking":
			thinking, thinkingOK := decodeJSONString(fields["thinking"])
			signature, signatureOK := decodeRequiredString(fields["signature"])
			if reasoningDisabled || phase != "reasoning" || !thinkingOK || !signatureOK || len(fields) != 3 {
				return nil, nil, nil, protocolError(ErrInvalidRequest, "thinking_history_invalid")
			}
			block.Thinking, block.Signature = &thinking, &signature
			if thinking != "" {
				refs = append(refs, publicAnthropicToolsTextRef{
					path: "messages[" + strconv.Itoa(messageIndex) + "].content[" + strconv.Itoa(blockIndex) + "].thinking",
					text: thinking, identityOnly: true, kind: "thinking",
					message: messageIndex, block: blockIndex,
				})
			}
		case "redacted_thinking":
			data, dataOK := decodeRequiredString(fields["data"])
			if reasoningDisabled || phase != "reasoning" || !dataOK || len(fields) != 2 {
				return nil, nil, nil, protocolError(ErrInvalidRequest, "thinking_history_invalid")
			}
			block.Data = &data
		case "text":
			text, textOK := decodeRequiredString(fields["text"])
			if phase == "tools" || !textOK || len(fields) != 2 {
				return nil, nil, nil, protocolError(ErrInvalidRequest, "assistant_tool_text_invalid")
			}
			phase = "text"
			block.Text = &text
			refs = append(refs, publicAnthropicToolsTextRef{
				path: "messages[" + strconv.Itoa(messageIndex) + "].content[" + strconv.Itoa(blockIndex) + "].text",
				text: text, kind: "text", message: messageIndex, block: blockIndex,
			})
		case "tool_use":
			id, idOK := decodeRequiredString(fields["id"])
			name, nameOK := decodeRequiredString(fields["name"])
			input, inputErr := canonicalPublicJSONObject(fields["input"], "tool_use_input_invalid")
			if !idOK || len(id) > 256 || !nameOK || inputErr != nil || len(fields) != 4 {
				return nil, nil, nil, protocolError(ErrInvalidRequest, "assistant_tool_use_invalid")
			}
			if _, exists := declared[name]; !exists {
				return nil, nil, nil, protocolError(ErrInvalidRequest, "assistant_tool_use_undeclared")
			}
			if _, duplicate := allIDs[id]; duplicate {
				return nil, nil, nil, protocolError(ErrInvalidRequest, "tool_use_id_duplicate")
			}
			allIDs[id] = struct{}{}
			toolIDs[id] = struct{}{}
			phase = "tools"
			block.ID, block.Name, block.Input = id, name, input
			for _, leaf := range publicJSONStringLeaves(input,
				"messages["+strconv.Itoa(messageIndex)+"].content["+strconv.Itoa(blockIndex)+"].input") {
				refs = append(refs, publicAnthropicToolsTextRef{
					path: leaf.Path, text: leaf.Text, identityOnly: true, kind: "tool_input",
					message: messageIndex, block: blockIndex,
				})
			}
		default:
			return nil, nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_assistant_tool_block")
		}
		blocks = append(blocks, block)
	}
	if len(toolIDs) == 0 {
		return nil, nil, nil, protocolError(ErrInvalidRequest, "assistant_tool_use_missing")
	}
	return blocks, refs, toolIDs, nil
}

func decodePublicAnthropicUserToolResultBlocks(
	rawBlocks []json.RawMessage,
	messageIndex int,
	pending map[string]struct{},
) ([]publicAnthropicToolsContentBlock, []publicAnthropicToolsTextRef, error) {
	blocks := make([]publicAnthropicToolsContentBlock, 0, len(rawBlocks))
	refs := make([]publicAnthropicToolsTextRef, 0)
	matched := make(map[string]struct{}, len(pending))
	textPhase := false
	for blockIndex, raw := range rawBlocks {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return nil, nil, protocolError(ErrInvalidRequest, "tool_result_block_invalid")
		}
		kind, ok := decodeRequiredString(fields["type"])
		if !ok {
			return nil, nil, protocolError(ErrInvalidRequest, "tool_result_block_invalid")
		}
		block := publicAnthropicToolsContentBlock{Type: kind}
		switch kind {
		case "tool_result":
			id, idOK := decodeRequiredString(fields["tool_use_id"])
			if textPhase || !idOK || len(fields) < 2 || len(fields) > 4 {
				return nil, nil, protocolError(ErrInvalidRequest, "tool_result_block_invalid")
			}
			if _, expected := pending[id]; !expected {
				return nil, nil, protocolError(ErrInvalidRequest, "tool_result_id_unmatched")
			}
			if _, duplicate := matched[id]; duplicate {
				return nil, nil, protocolError(ErrInvalidRequest, "tool_result_id_duplicate")
			}
			matched[id] = struct{}{}
			block.ToolUseID = id
			if rawContent, present := fields["content"]; present {
				content, contentOK := decodeJSONString(rawContent)
				if !contentOK {
					return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_tool_result_content")
				}
				block.Content = &content
				if content != "" {
					refs = append(refs, publicAnthropicToolsTextRef{
						path: "messages[" + strconv.Itoa(messageIndex) + "].content[" + strconv.Itoa(blockIndex) + "].content",
						text: content, kind: "tool_result", message: messageIndex, block: blockIndex,
					})
				}
			}
			if rawError, present := fields["is_error"]; present {
				var isError bool
				if json.Unmarshal(rawError, &isError) != nil || string(rawError) != strconv.FormatBool(isError) {
					return nil, nil, protocolError(ErrInvalidRequest, "tool_result_is_error_invalid")
				}
				block.IsError = &isError
			}
			wantMembers := 2
			if block.Content != nil {
				wantMembers++
			}
			if block.IsError != nil {
				wantMembers++
			}
			if len(fields) != wantMembers {
				return nil, nil, protocolError(ErrInvalidRequest, "tool_result_block_invalid")
			}
		case "text":
			text, textOK := decodeRequiredString(fields["text"])
			if len(matched) != len(pending) || !textOK || len(fields) != 2 {
				return nil, nil, protocolError(ErrInvalidRequest, "tool_result_order_invalid")
			}
			textPhase = true
			block.Text = &text
			refs = append(refs, publicAnthropicToolsTextRef{
				path: "messages[" + strconv.Itoa(messageIndex) + "].content[" + strconv.Itoa(blockIndex) + "].text",
				text: text, kind: "text", message: messageIndex, block: blockIndex,
			})
		default:
			return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_tool_result_block")
		}
		blocks = append(blocks, block)
	}
	if len(matched) != len(pending) {
		return nil, nil, protocolError(ErrInvalidRequest, "tool_history_result_missing")
	}
	return blocks, refs, nil
}

func canonicalPublicJSONObject(raw json.RawMessage, code string) (json.RawMessage, error) {
	if len(raw) == 0 || isExplicitJSONNull(raw) {
		return nil, protocolError(ErrInvalidRequest, code)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil || requireDecoderEOF(decoder) != nil {
		return nil, protocolError(ErrInvalidRequest, code)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, protocolError(ErrInvalidRequest, code)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, protocolError(ErrInvalidRequest, code)
	}
	return canonical, nil
}

func publicJSONStringLeaves(raw json.RawMessage, root string) []PublicText {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil || requireDecoderEOF(decoder) != nil {
		return nil
	}
	var result []PublicText
	appendPublicJSONStringLeaves(&result, value, root)
	return result
}

func appendPublicJSONStringLeaves(result *[]PublicText, value any, path string) {
	switch typed := value.(type) {
	case string:
		if typed != "" {
			*result = append(*result, PublicText{Path: path, Text: typed})
		}
	case []any:
		for index, child := range typed {
			appendPublicJSONStringLeaves(result, child, path+"["+strconv.Itoa(index)+"]")
		}
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			appendPublicJSONStringLeaves(result, typed[key], path+"."+key)
		}
	}
}
