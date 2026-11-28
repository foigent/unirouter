package endpointprotocol

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/modeloperation"
)

const (
	ProfileOpenAIResponsesToolsV3  = "unirouter.openai.responses.text-tools.v3"
	PublicResponsesToolsExecutorV1 = "unirouter.endpoint-profile-responses-tools-executor.v1"
	PublicResponsesToolsDeliveryV1 = "unirouter.public-responses-tools-delivery.v1"
)

var responsesClientFunctionName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func responsesToolsIdentity() PublicContractIdentity {
	return PublicContractIdentity{
		ModelOperationID: string(modeloperation.IDResponses), PublicPath: modeloperation.PathResponses,
		ProfileID: ProfileOpenAIResponsesToolsV3, ProfileVersion: "1.0.0",
		ExecutorContractVersion: PublicResponsesToolsExecutorV1, DeliveryContractVersion: PublicResponsesToolsDeliveryV1,
		ChargeContractVersion: PublicCustomerChargeContractV1, IdempotencyContractVersion: PublicIdempotencyContractV1,
	}
}

type openAIResponsesToolsProfileV3 struct{}

func (openAIResponsesToolsProfileV3) Descriptor() Descriptor {
	d := (openAIResponsesProfile{}).Descriptor()
	d.ProfileID = ProfileOpenAIResponsesToolsV3
	d.ProviderFinality = "response.completed_with_provider_usage"
	d.UnsupportedSemantics = []string{
		"background", "multimodal_or_structured_input", "parallel_client_tools",
		"server_tools", "stored_response_state", "streaming",
	}
	return d
}

func (openAIResponsesToolsProfileV3) publicProfile() publicOpenAIResponsesProfile {
	return publicOpenAIResponsesProfile{base: openAIResponsesToolsProfileV3{}, identity: responsesToolsIdentity()}
}

// Registry callers exercise the same codec as customer delivery. The
// application-owned Profile narrows the controls on normal runtime requests.
func (p openAIResponsesToolsProfileV3) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	public := p.publicProfile()
	contract := PublicRequestContract{
		ExecutionProfileID: "endpointprotocol.openai-responses-tools-fixture", ExecutionProfileVersion: "1.0.0",
		Reasoning: &PublicReasoningRequestContract{
			RequestObjectField: "reasoning", EffortField: "effort", AllowedEfforts: []string{"low", "medium", "high", "xhigh", "max"},
		},
	}
	prepared, err := public.PrepareRequest(input.Body, input.RequestedModel, contract)
	if err != nil {
		return nil, nil, err
	}
	upstream, err := public.BuildUpstream(ctx, prepared, PublicUpstreamInput{
		BaseURL: input.BaseURL, APIKey: input.APIKey, RequestID: input.RequestID, MappedModel: input.MappedModel,
	})
	if err != nil {
		return nil, nil, err
	}
	binding := upstream.binding.base
	binding.publicContract = &upstream.binding.requestContract
	binding.publicSemantics = upstream.binding.invocationSemantics
	return upstream.request, binding, nil
}

func (p openAIResponsesToolsProfileV3) ParseResponse(ctx context.Context, b *Binding, status int, body []byte) (Outcome, error) {
	if err := requireBinding(b, p.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if b.publicContract == nil {
		return Outcome{}, protocolError(ErrInvalidConfiguration, "public_request_contract_missing")
	}
	delivery, err := p.publicProfile().parseDelivery(ctx, &publicResponseBinding{
		identity: responsesToolsIdentity(), base: b,
		requestedModel: b.requestedModel, mappedModel: b.mappedModel, requestContract: *b.publicContract, invocationSemantics: b.publicSemantics,
	}, status, body)
	if err != nil {
		return Outcome{}, err
	}
	u := delivery.CustomerUsage()
	return Outcome{
		ContractVersion: ContractVersion, ProfileID: ProfileOpenAIResponsesToolsV3, ProviderState: "completed",
		ProviderObject: delivery.ProviderObject(), MappedModel: b.mappedModel, OutputCount: 1,
		Units: []UnitObservation{
			observedUnit(UnitInputTokens, strconv.FormatInt(u.InputTokens-u.CacheReadTokens-u.CacheWriteTokens, 10)),
			observedUnit(UnitCacheReadTokens, strconv.FormatInt(u.CacheReadTokens, 10)),
			observedUnit(UnitCacheWriteTokens, strconv.FormatInt(u.CacheWriteTokens, 10)),
			observedUnit(UnitOutputTokens, strconv.FormatInt(u.OutputTokens, 10)),
		},
	}, nil
}

func (openAIResponsesToolsProfileV3) ClassifyHTTPStatus(status int) (string, bool) {
	return (openAIResponsesProfile{}).ClassifyHTTPStatus(status)
}

type responsesToolsRequestDocument struct {
	envelope openAIResponsesEnvelope
	refs     []PublicText
}

func (d responsesToolsRequestDocument) canonicalBody() ([]byte, error) {
	return json.Marshal(d.envelope)
}
func (d responsesToolsRequestDocument) texts() []PublicText { return clonePublicTexts(d.refs) }
func (d responsesToolsRequestDocument) withTexts(replacements []string) (publicRequestDocument, error) {
	if !responsesToolTextIdentity(d.refs, replacements) {
		return nil, protocolError(ErrUnsupportedSemantics, "responses_tool_history_requires_identity")
	}
	return d, nil
}

func responsesToolTextIdentity(refs []PublicText, texts []string) bool {
	if len(refs) != len(texts) {
		return false
	}
	for index, ref := range refs {
		if ref.Text != texts[index] {
			return false
		}
	}
	return true
}

func prepareResponsesClientTools(envelope openAIResponsesEnvelope, model string, contract PublicRequestContract) (*PublicPreparedRequest, error) {
	if envelope["temperature"] != nil || envelope["top_p"] != nil {
		return nil, protocolError(ErrUnsupportedSemantics, "unsupported_sampling_parameters")
	}
	core := cloneOpenAIResponsesEnvelope(envelope)
	for _, key := range []string{"tools", "tool_choice", "parallel_tool_calls", "include"} {
		delete(core, key)
	}
	semantics := PublicInvocationSemantics{FunctionToolsRequested: true, ReasoningEffortExplicit: envelope["reasoning"] != nil, ToolChoiceEffective: "auto"}
	if raw := envelope["tool_choice"]; raw != nil {
		choice, ok := decodeRequiredString(raw)
		if !ok || (choice != "auto" && choice != "none") {
			return nil, protocolError(ErrUnsupportedSemantics, "unsupported_tool_choice")
		}
		semantics.ToolChoiceEffective, semantics.ToolChoiceExplicit = choice, true
	}
	if raw := envelope["parallel_tool_calls"]; raw != nil && string(raw) != "false" {
		return nil, protocolError(ErrUnsupportedSemantics, "unsupported_parallel_client_tools")
	}
	if raw := envelope["include"]; raw != nil {
		var fields []string
		if json.Unmarshal(raw, &fields) != nil || !slices.Equal(fields, []string{"reasoning.encrypted_content"}) {
			return nil, protocolError(ErrUnsupportedSemantics, "unsupported_include_expansions")
		}
	}
	var tools []json.RawMessage
	if json.Unmarshal(envelope["tools"], &tools) != nil || len(tools) == 0 || len(tools) > 64 {
		return nil, protocolError(ErrInvalidRequest, "responses_tools_invalid")
	}
	var refs []PublicText
	declared := map[string]bool{}
	for index, tool := range tools {
		const code = "responses_client_function_invalid"
		fields, err := objectKeys(tool)
		if err != nil {
			return nil, err
		}
		name := mustJSONString(fields["name"])
		if mustJSONString(fields["type"]) != "function" || !responsesClientFunctionName.MatchString(name) || declared[name] {
			return nil, protocolError(ErrInvalidRequest, code)
		}
		parameters, err := canonicalPublicJSONObject(fields["parameters"], code)
		if err != nil {
			return nil, err
		}
		parameterFields, err := objectKeys(parameters)
		if err != nil || mustJSONString(parameterFields["type"]) != "object" {
			return nil, protocolError(ErrInvalidRequest, code)
		}
		for key, raw := range fields {
			switch key {
			case "type", "name", "parameters":
			case "description":
				if _, ok := decodeJSONString(raw); !ok {
					return nil, protocolError(ErrInvalidRequest, code)
				}
			case "strict":
				if string(raw) != "true" && string(raw) != "false" {
					return nil, protocolError(ErrInvalidRequest, code)
				}
			default:
				return nil, protocolError(ErrUnsupportedSemantics, code)
			}
		}
		declared[name] = true
		semantics.DeclaredToolNames = append(semantics.DeclaredToolNames, name)
		path := "tools[" + strconv.Itoa(index) + "]"
		if value := mustJSONString(fields["description"]); value != "" {
			refs = append(refs, PublicText{Path: path + ".description", Text: value})
		}
		refs = append(refs, publicJSONStringLeaves(parameters, path+".parameters")...)
	}
	slices.Sort(semantics.DeclaredToolNames)
	if input, ok := decodeRequiredString(envelope["input"]); ok {
		refs = append(refs, PublicText{Path: "input", Text: input})
	} else {
		texts, err := validateResponsesToolHistory(envelope["input"], declared)
		if err != nil {
			return nil, err
		}
		refs = append(refs, texts...)
		core["input"] = json.RawMessage(`"validated typed function history"`)
	}
	if err := validatePublicOpenAIResponsesRequestForMode(core, model, contract, false); err != nil {
		return nil, err
	}
	if value := mustJSONString(envelope["instructions"]); value != "" {
		refs = append(refs, PublicText{Path: "instructions", Text: value})
	}
	maxOutput, err := parseRequestInt64(json.Number(string(envelope["max_output_tokens"])))
	if err != nil || maxOutput < 16 {
		return nil, protocolError(ErrInvalidRequest, "max_output_tokens_invalid")
	}
	envelope["store"], envelope["stream"], envelope["parallel_tool_calls"] = json.RawMessage("false"), json.RawMessage("false"), json.RawMessage("false")
	envelope["include"] = json.RawMessage(`["reasoning.encrypted_content"]`)
	document := responsesToolsRequestDocument{envelope: envelope, refs: refs}
	if err := validatePublicRequestDocument(document); err != nil {
		return nil, err
	}
	return &PublicPreparedRequest{
		identity: responsesToolsIdentity(), endpointKind: EndpointResponses, requestedModel: model, maxOutput: maxOutput,
		requestContract: contract, document: document, semantics: semantics,
	}, nil
}

func validateResponsesToolHistory(raw json.RawMessage, declared map[string]bool) ([]PublicText, error) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || len(items) < 3 || len(items) > 32 {
		return nil, protocolError(ErrInvalidRequest, "responses_function_history_invalid")
	}
	var texts []PublicText
	seen := map[string]bool{}
	callID := ""
	for index, item := range items {
		fields, err := objectKeys(item)
		if err != nil {
			return nil, err
		}
		path := "input[" + strconv.Itoa(index) + "]"
		kind := mustJSONString(fields["type"])
		if index == 0 {
			text, ok := decodeRequiredString(fields["content"])
			n := 2
			if fields["type"] != nil {
				if kind != "message" {
					return nil, protocolError(ErrInvalidRequest, "responses_function_history_user_invalid")
				}
				n++
			}
			if mustJSONString(fields["role"]) != "user" || !ok || len(fields) != n {
				return nil, protocolError(ErrInvalidRequest, "responses_function_history_user_invalid")
			}
			texts = append(texts, PublicText{Path: path + ".content", Text: text})
			continue
		}
		if index == len(items)-1 {
			out, ok := decodeRequiredString(fields["output"])
			if kind != "function_call_output" || callID == "" || mustJSONString(fields["call_id"]) != callID || !ok || len(fields) != 3 {
				return nil, protocolError(ErrInvalidRequest, "responses_function_output_mismatch")
			}
			texts = append(texts, PublicText{Path: path + ".output", Text: out})
			continue
		}
		if callID != "" {
			return nil, protocolError(ErrInvalidRequest, "responses_function_history_order")
		}
		id, ok := decodeRequiredString(fields["id"])
		if !ok || len(id) > 256 || seen[id] {
			return nil, protocolError(ErrInvalidRequest, "responses_function_item_identity")
		}
		seen[id] = true
		switch kind {
		case "reasoning":
			if err := validateResponsesReplayReasoning(item, fields); err != nil {
				return nil, err
			}
		case "message":
			messages, _, err := projectPublicOpenAIResponsesMessages([]json.RawMessage{item}, true)
			if err != nil || len(messages) != 1 {
				return nil, protocolError(ErrInvalidRequest, "responses_function_history_message_invalid")
			}
			for part, content := range messages[0].Content {
				texts = append(texts, PublicText{Path: path + ".content[" + strconv.Itoa(part) + "].text", Text: content.Text})
			}
		case "function_call":
			call, arguments, err := validateResponsesFunctionCall(item, fields, declared)
			if err != nil {
				return nil, err
			}
			callID = call
			texts = append(texts, publicJSONStringLeaves(arguments, path+".arguments")...)
		default:
			return nil, protocolError(ErrUnsupportedSemantics, "unsupported_responses_function_history_item")
		}
	}
	return texts, nil
}

func validateResponsesReplayReasoning(raw json.RawMessage, fields map[string]json.RawMessage) error {
	if err := validateExactJSONObjectMembers(raw, maxResponseBytes, ErrInvalidResponse, "public_response_reasoning", publicOpenAIResponsesReasoningSchema); err != nil {
		return err
	}
	if err := validatePublicOpenAIResponsesReasoningShape(fields); err != nil {
		return err
	}
	if err := validatePublicOpenAIReasoningOutput(fields, true); err != nil {
		return err
	}
	if value, ok := decodeRequiredString(fields["encrypted_content"]); !ok || len(value) > maxRequestBytes/2 {
		return protocolError(ErrInvalidResponse, "reasoning_replay_content_missing")
	}
	return nil
}

func validateResponsesFunctionCall(raw json.RawMessage, fields map[string]json.RawMessage, declared map[string]bool) (string, json.RawMessage, error) {
	if err := validateExactJSONObjectMembers(raw, maxResponseBytes, ErrInvalidResponse, "responses_function_call", exactJSONObjectSchema{
		"id": {}, "type": {}, "call_id": {}, "name": {}, "arguments": {}, "status": {},
	}); err != nil {
		return "", nil, err
	}
	id, idOK := decodeRequiredString(fields["id"])
	call, callOK := decodeRequiredString(fields["call_id"])
	name, nameOK := decodeRequiredString(fields["name"])
	args, argsOK := decodeRequiredString(fields["arguments"])
	if !idOK || len(id) > 256 || !callOK || len(call) > 256 || !nameOK || !declared[name] ||
		!argsOK || len(args) > maxRequestBytes/2 || (fields["status"] != nil && mustJSONString(fields["status"]) != "completed") {
		return "", nil, protocolError(ErrInvalidResponse, "responses_function_call_invalid")
	}
	arguments, err := canonicalPublicJSONObject(json.RawMessage(args), "responses_function_arguments_invalid")
	return call, arguments, err
}

type responsesToolsDeliveryDocument struct {
	response publicOpenAIResponsesProviderResponse
	output   []json.RawMessage
	refs     []PublicText
}

func (d responsesToolsDeliveryDocument) canonicalBody() ([]byte, error) {
	return json.Marshal(struct {
		publicOpenAIResponsesProviderResponse
		Output []json.RawMessage `json:"output"`
	}{d.response, d.output})
}
func (d responsesToolsDeliveryDocument) texts() []PublicText { return clonePublicTexts(d.refs) }
func (d responsesToolsDeliveryDocument) withTexts(replacements []string) (publicDeliveryDocument, error) {
	if !responsesToolTextIdentity(d.refs, replacements) {
		return nil, protocolError(ErrUnsupportedSemantics, "responses_tool_delivery_requires_identity")
	}
	return d, nil
}

func deliverResponsesClientTools(wire publicOpenAIResponsesWireResponse, binding *publicResponseBinding) (PublicDelivery, error) {
	declared := map[string]bool{}
	for _, name := range binding.invocationSemantics.DeclaredToolNames {
		declared[name] = true
	}
	seen := map[string]bool{}
	var refs []PublicText
	var text strings.Builder
	hasCall := false
	for index, raw := range wire.Output {
		fields, err := objectKeys(raw)
		if err != nil {
			return PublicDelivery{}, protocolError(ErrInvalidResponse, "responses_function_output_invalid")
		}
		id, ok := decodeRequiredString(fields["id"])
		if !ok || len(id) > 256 || seen[id] || hasCall {
			return PublicDelivery{}, protocolError(ErrInvalidResponse, "responses_function_output_identity")
		}
		seen[id] = true
		path := "output[" + strconv.Itoa(index) + "]"
		switch mustJSONString(fields["type"]) {
		case "reasoning":
			if err := validateResponsesReplayReasoning(raw, fields); err != nil {
				return PublicDelivery{}, err
			}
		case "function_call":
			_, args, err := validateResponsesFunctionCall(raw, fields, declared)
			if err != nil || binding.invocationSemantics.ToolChoiceEffective == "none" {
				return PublicDelivery{}, protocolError(ErrInvalidResponse, "responses_function_call_invalid")
			}
			hasCall = true
			refs = append(refs, publicJSONStringLeaves(args, path+".arguments")...)
		case "message":
			messages, value, err := projectPublicOpenAIResponsesMessages([]json.RawMessage{raw}, true)
			if err != nil {
				return PublicDelivery{}, err
			}
			text.WriteString(value)
			for part, content := range messages[0].Content {
				refs = append(refs, PublicText{Path: path + ".content[" + strconv.Itoa(part) + "].text", Text: content.Text})
			}
		default:
			return PublicDelivery{}, protocolError(ErrUnsupportedSemantics, "unsupported_public_response_output")
		}
	}
	if (!hasCall && strings.TrimSpace(text.String()) == "") || (wire.OutputText != nil && *wire.OutputText != text.String()) {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "responses_function_terminal_evidence_missing")
	}
	usage, err := publicOpenAIUsage(wire.Usage)
	if err != nil {
		return PublicDelivery{}, err
	}
	if err := usage.validate(); err != nil {
		return PublicDelivery{}, err
	}
	output := text.String()
	document := responsesToolsDeliveryDocument{
		response: publicOpenAIResponsesProviderResponse{
			ID: wire.ID, Object: wire.Object, Status: wire.Status, Model: binding.requestedModel,
			Error: wire.Error, IncompleteDetails: wire.IncompleteDetails, OutputText: &output, Usage: wire.Usage,
		},
		output: wire.Output, refs: refs,
	}
	if err := validatePublicDeliveryDocument(document); err != nil {
		return PublicDelivery{}, err
	}
	return PublicDelivery{endpointKind: EndpointResponses, providerObject: wire.ID, usage: usage, document: document}, nil
}

var responsesToolsExactResponseSchema = func() exactJSONObjectSchema {
	schema := maps.Clone(publicOpenAIResponsesExactResponseSchema)
	schema["prompt_cache_options"] = exactJSONMemberSchema{}
	return schema
}()

func validateResponsesDefaultCacheOptions(raw json.RawMessage) error {
	if len(raw) == 0 || isExplicitJSONNull(raw) {
		return nil
	}
	fields, err := objectKeys(raw)
	if err != nil || mustJSONString(fields["mode"]) != "implicit" || mustJSONString(fields["ttl"]) != "30m" {
		return protocolError(ErrUnsupportedSemantics, "unsupported_responses_prompt_cache_options")
	}
	for key, value := range fields {
		if key != "mode" && key != "ttl" && (key != "comparison_response_id" || !isExplicitJSONNull(value)) {
			return protocolError(ErrUnsupportedSemantics, "unsupported_responses_prompt_cache_options")
		}
	}
	return nil
}
