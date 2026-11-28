package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelqualification"
)

const (
	nativeToolProbeOutputLimit = 1024
	nativeToolProbeBodyLimit   = 3584
	nativeToolProbeName        = "unirouter_probe_echo"
	nativeToolProbeInput       = "Call unirouter_probe_echo exactly once with key check. After its result, reply with that value verbatim and do not call any other tool."
)

func nativeProbeProfile(p modelprofile.Profile) bool {
	return modelprofile.NativeClientToolsProfile(p) || modelprofile.NativeInlineJPEGProfile(p)
}

func nativeProbeIdentity(p modelprofile.Profile, provider, baseURL, adapter, operation, protocolID, version, model string, config json.RawMessage) (endpointprotocol.PublicNonStreamProfile, string, error) {
	if !nativeProbeProfile(p) {
		return nil, "", errModelCapabilityUnsupported
	}
	pathMode, err := adapters.EffectiveEndpointPathMode(adapter, config)
	if err != nil || modelprofile.ValidateProductBinding(p, modelprofile.ProductBinding{
		Provider: provider, BaseURL: baseURL, AdapterType: adapter, EndpointPathMode: pathMode,
		OperationID: operation, ProtocolProfileID: protocolID, ProtocolProfileVersion: version, UpstreamModel: model,
	}) != nil {
		return nil, "", errModelCapabilityUnsupported
	}
	origin, err := modelqualification.NormalizeHTTPSOrigin(baseURL)
	if err != nil {
		return nil, "", err
	}
	binding, err := modelqualification.CompiledBindingFor(operation, protocolID, version)
	if err != nil {
		return nil, "", err
	}
	identity, err := modelqualification.CompiledPublicBindingFor(binding)
	if err != nil {
		return nil, "", err
	}
	profile, err := endpointprotocol.NewPublicNonStreamRegistry().Resolve(identity)
	return profile, origin, err
}

func (h *Handler) executeNativeInlineJPEGProbe(ctx context.Context, snapshot modelCapabilitySnapshot, requestID string) modelCapabilityProbeOutcome {
	outcome := modelCapabilityProbeOutcome{DispatchState: "not_started", ReasonCode: "probe_contract_unsupported"}
	binding := modelCapabilityProbeBindingFromSnapshot(snapshot)
	p, err := binding.ExecutionProfile()
	if err != nil || binding.CapabilityID != modelprofile.CapabilityVision || !modelprofile.NativeInlineJPEGProfile(p) {
		return outcome
	}
	profile, origin, err := nativeProbeIdentity(p, snapshot.Provider, snapshot.BaseURL, snapshot.AdapterType, snapshot.OperationID, snapshot.ProtocolProfileID, snapshot.ProtocolProfileVersion, snapshot.UpstreamModel, snapshot.AdapterConfig)
	if err != nil {
		return outcome
	}
	secret, err := auth.DecryptKey(snapshot.EncryptedSecret, h.encKey)
	if err != nil || strings.TrimSpace(secret) == "" {
		outcome.ReasonCode = "credential_unavailable"
		return outcome
	}
	image, answer, err := modelcapabilityqualification.RandomInlineJPEGChallenge()
	if err != nil {
		outcome.ReasonCode = "request_build_failed"
		return outcome
	}
	parts := []map[string]any{
		{"type": "text", "text": modelcapabilityqualification.InlineJPEGChallengeQuestion},
		{"type": "image", "source": map[string]string{"type": "base64", "media_type": "image/jpeg", "data": image}},
	}
	inputKey, limitKey := "messages", "max_tokens"
	if p.APISurface == modelprofile.APISurfaceOpenAIResponses {
		inputKey, limitKey = "input", "max_output_tokens"
		parts[0]["type"] = "input_text"
		parts[1] = map[string]any{"type": "input_image", "image_url": "data:image/jpeg;base64," + image}
	}
	body, err := json.Marshal(map[string]any{
		"model": snapshot.UpstreamModel, "stream": false, limitKey: nativeToolProbeOutputLimit,
		inputKey: []map[string]any{{"role": "user", "content": parts}},
	})
	if err != nil {
		outcome.ReasonCode = "request_build_failed"
		return outcome
	}
	ctx, cancel := context.WithTimeout(ctx, modelCapabilityProbeTimeout)
	defer cancel()
	step := nativeProbeStep{Phase: "inline_jpeg"}
	started := time.Now()
	delivery, failure, ambiguous := h.nativeProbeExchange(ctx, profile, modelprofile.NativeClientToolsRequestContract(p), origin, secret, snapshot.UpstreamModel, requestID, body, &step)
	outcome.LatencyMS = time.Since(started).Milliseconds()
	outcome.RequestSummary = map[string]any{"stream": false, "image_count": 1, "planned_provider_requests": 1, "native_inline_jpeg": step}
	outcome.ReasonCode, outcome.OutcomeAmbiguous = failure, ambiguous
	if step.Dispatched {
		outcome.DispatchState = "started"
	}
	if step.Status != 0 {
		outcome.DispatchState, outcome.StatusCode = "response_received", step.Status
	}
	outcome.ResponseBytes = step.ResponseBytes
	outcome.PromptTokens, outcome.CompletionTokens = step.InputTokens, step.OutputTokens
	outcome.TotalTokens = step.InputTokens + step.OutputTokens
	if failure != "" {
		return outcome
	}
	if step.InputTokens <= 0 || step.OutputTokens <= 0 || !nativeToolProbeAcknowledged(p, delivery.Body(), answer) {
		outcome.ReasonCode = "provider_inline_jpeg_evidence_invalid"
		return outcome
	}
	outcome.ReportedModel = snapshot.UpstreamModel
	outcome.ProviderSuccess, outcome.ContractSuccess = true, true
	return outcome
}

func nativeToolProbeRequest(p modelprofile.Profile, model string) ([]byte, error) {
	schema := json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"],"additionalProperties":false}`)
	tool := map[string]any{"name": nativeToolProbeName, "description": "Return a short verification value for key check."}
	body := map[string]any{"model": model, "stream": false, "tools": []map[string]any{tool}}
	if p.APISurface == modelprofile.APISurfaceOpenAIResponses {
		tool["type"], tool["parameters"], tool["strict"] = "function", schema, true
		body["input"], body["max_output_tokens"], body["store"] = nativeToolProbeInput, nativeToolProbeOutputLimit, false
		body["tool_choice"], body["parallel_tool_calls"] = "auto", false
		body["include"] = []string{"reasoning.encrypted_content"}
	} else {
		tool["input_schema"] = schema
		body["messages"] = []map[string]string{{"role": "user", "content": nativeToolProbeInput}}
		body["max_tokens"] = nativeToolProbeOutputLimit
		body["tool_choice"] = map[string]any{"type": "auto", "disable_parallel_tool_use": true}
	}
	return json.Marshal(body)
}

type nativeProbeStep struct {
	Phase            string `json:"phase"`
	Status           int    `json:"status,omitempty"`
	InputTokens      int64  `json:"input_tokens,omitempty"`
	OutputTokens     int64  `json:"output_tokens,omitempty"`
	CacheReadTokens  int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64  `json:"cache_write_tokens,omitempty"`
	RequestBytes     int64  `json:"request_bytes,omitempty"`
	ResponseBytes    int    `json:"response_bytes,omitempty"`
	Dispatched       bool   `json:"dispatched"`
}

func (h *Handler) nativeProbeExchange(
	ctx context.Context, profile endpointprotocol.PublicNonStreamProfile, contract endpointprotocol.PublicRequestContract,
	origin, secret, model, requestID string, body []byte, step *nativeProbeStep,
) (endpointprotocol.PublicDelivery, string, bool) {
	if len(body) > nativeToolProbeBodyLimit {
		return endpointprotocol.PublicDelivery{}, "request_build_failed", false
	}
	prepared, err := profile.PrepareRequest(body, model, contract)
	if err != nil || prepared.Admission().CanonicalBodyBytes > nativeToolProbeBodyLimit ||
		prepared.Admission().ExplicitMaxOutputTokens != nativeToolProbeOutputLimit {
		return endpointprotocol.PublicDelivery{}, "request_build_failed", false
	}
	upstream, err := profile.BuildUpstream(ctx, prepared, endpointprotocol.PublicUpstreamInput{
		BaseURL: origin, APIKey: secret, RequestID: requestID, MappedModel: model,
	})
	if err != nil {
		return endpointprotocol.PublicDelivery{}, "request_build_failed", false
	}
	req, err := upstream.NewHTTPRequest(ctx)
	if err != nil || req.ContentLength <= 0 || req.ContentLength > nativeToolProbeBodyLimit {
		return endpointprotocol.PublicDelivery{}, "request_build_failed", false
	}
	step.RequestBytes = req.ContentLength
	if err := ctx.Err(); err != nil {
		return endpointprotocol.PublicDelivery{}, "provider_request_failed", false
	}
	step.Dispatched = true
	response, err := h.sendModelProbe(req, singleOperationProbeHTTPClient)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return endpointprotocol.PublicDelivery{}, "provider_request_failed", true
	}
	if response == nil || response.Body == nil {
		return endpointprotocol.PublicDelivery{}, "provider_response_invalid", false
	}
	defer response.Body.Close()
	if response.StatusCode >= 100 && response.StatusCode <= 599 {
		step.Status = response.StatusCode
	}
	wire, err := io.ReadAll(io.LimitReader(response.Body, canonicalOperationProbeMaxResponseBytes+1))
	step.ResponseBytes = len(wire)
	if err != nil {
		return endpointprotocol.PublicDelivery{}, "provider_response_read_failed", false
	}
	if len(wire) > canonicalOperationProbeMaxResponseBytes {
		return endpointprotocol.PublicDelivery{}, "provider_response_too_large", false
	}
	if response.StatusCode != http.StatusOK {
		return endpointprotocol.PublicDelivery{}, "provider_http_error", false
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return endpointprotocol.PublicDelivery{}, "provider_response_invalid", false
	}
	delivery, err := upstream.ParseDelivery(ctx, response.StatusCode, wire)
	if err != nil {
		return endpointprotocol.PublicDelivery{}, "provider_response_invalid", false
	}
	usage := delivery.CustomerUsage()
	step.InputTokens, step.OutputTokens = usage.InputTokens, usage.OutputTokens
	step.CacheReadTokens, step.CacheWriteTokens = usage.CacheReadTokens, usage.CacheWriteTokens
	if usage.InputTokens <= 0 || usage.InputTokens > 4096 || usage.OutputTokens <= 0 || usage.OutputTokens > nativeToolProbeOutputLimit {
		return endpointprotocol.PublicDelivery{}, "provider_response_invalid", false
	}
	return delivery, "", false
}

func (h *Handler) executeNativeToolProbe(ctx context.Context, snapshot modelCapabilitySnapshot, requestID string) (outcome modelCapabilityProbeOutcome) {
	outcome = modelCapabilityProbeOutcome{DispatchState: "not_started"}
	binding := modelCapabilityProbeBindingFromSnapshot(snapshot)
	p, err := modelcapabilityqualification.NativeToolProbeProfile(binding)
	if err != nil {
		outcome.ReasonCode = "probe_contract_unsupported"
		return outcome
	}
	if snapshot.Provider != binding.ExpectedProvider || snapshot.UpstreamModel != binding.ExpectedUpstreamModel {
		outcome.ReasonCode = "probe_contract_unsupported"
		return outcome
	}
	profile, origin, err := nativeProbeIdentity(p, snapshot.Provider, snapshot.BaseURL, snapshot.AdapterType, snapshot.OperationID, snapshot.ProtocolProfileID, snapshot.ProtocolProfileVersion, snapshot.UpstreamModel, snapshot.AdapterConfig)
	if err != nil {
		outcome.ReasonCode = "probe_contract_unsupported"
		return outcome
	}
	secret, err := auth.DecryptKey(snapshot.EncryptedSecret, h.encKey)
	if err != nil || strings.TrimSpace(secret) == "" {
		outcome.ReasonCode = "credential_unavailable"
		return outcome
	}
	firstBody, err := nativeToolProbeRequest(p, snapshot.UpstreamModel)
	if err != nil {
		outcome.ReasonCode = "request_build_failed"
		return outcome
	}
	ack := make([]byte, 8)
	if _, err := rand.Read(ack); err != nil {
		outcome.ReasonCode = "request_build_failed"
		return outcome
	}
	value := "UR_" + hex.EncodeToString(ack)
	contract := modelprofile.NativeClientToolsRequestContract(p)
	probeCtx, cancel := context.WithTimeout(ctx, modelCapabilityProbeTimeout)
	defer cancel()
	started := time.Now()
	first, second := nativeProbeStep{Phase: "function_call"}, nativeProbeStep{Phase: "function_result"}
	defer func() {
		outcome.LatencyMS = time.Since(started).Milliseconds()
		steps := []nativeProbeStep{first}
		if second.Dispatched {
			steps = append(steps, second)
		}
		outcome.RequestSummary = map[string]any{"stream": false, "planned_provider_requests": 2, "max_output_tokens_per_step": nativeToolProbeOutputLimit, "native_tool_steps": steps}
		for _, step := range steps {
			if step.Dispatched {
				outcome.DispatchState = "started"
			}
			if step.Status != 0 {
				outcome.DispatchState = "response_received"
				outcome.StatusCode = step.Status
			}
			outcome.ResponseBytes += step.ResponseBytes
			outcome.PromptTokens += step.InputTokens
			outcome.CompletionTokens += step.OutputTokens
		}
		outcome.TotalTokens = outcome.PromptTokens + outcome.CompletionTokens
	}()
	firstDelivery, failure, ambiguous := h.nativeProbeExchange(probeCtx, profile, contract, origin, secret, snapshot.UpstreamModel, requestID, firstBody, &first)
	if failure != "" {
		outcome.ReasonCode, outcome.OutcomeAmbiguous = failure, ambiguous
		return outcome
	}
	firstWire := firstDelivery.Body()
	if !nativeToolProbeReasoningObserved(p, firstWire) {
		outcome.ReasonCode = "provider_function_tools_contract_invalid"
		return outcome
	}
	next, err := nativeToolProbeContinuation(p, firstBody, firstWire, value)
	if err != nil {
		outcome.ReasonCode = "provider_function_tools_contract_invalid"
		return outcome
	}
	outcome.ToolCallObserved = true
	secondDelivery, failure, ambiguous := h.nativeProbeExchange(probeCtx, profile, contract, origin, secret, snapshot.UpstreamModel, requestID+"-result", next, &second)
	if failure != "" {
		outcome.ReasonCode, outcome.OutcomeAmbiguous = failure, ambiguous
		return outcome
	}
	if firstDelivery.ProviderObject() == secondDelivery.ProviderObject() || !nativeToolProbeAcknowledged(p, secondDelivery.Body(), value) {
		outcome.ReasonCode = "provider_function_tools_contract_invalid"
		return outcome
	}
	outcome.ProviderSuccess, outcome.ContractSuccess = true, true
	outcome.ReasonCode, outcome.ReportedModel, outcome.FinishReason = "ok", snapshot.UpstreamModel, "completed"
	return outcome
}

type nativeProbeItem struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Input     json.RawMessage `json:"input"`
	Text      string          `json:"text"`
}

// This only runs on a delivery already accepted by the exact wire parser.
// Opacity is preserved: Claude need not report a distinct reasoning counter.
func nativeToolProbeReasoningObserved(p modelprofile.Profile, wire []byte) bool {
	var response struct {
		Output  []nativeProbeItem `json:"output"`
		Content []nativeProbeItem `json:"content"`
		Usage   struct {
			OutputTokensDetails struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(wire, &response) != nil {
		return false
	}
	if p.APISurface == modelprofile.APISurfaceOpenAIResponses {
		return response.Usage.OutputTokensDetails.ReasoningTokens > 0 &&
			slices.ContainsFunc(response.Output, func(item nativeProbeItem) bool { return item.Type == "reasoning" })
	}
	return slices.ContainsFunc(response.Content, func(item nativeProbeItem) bool {
		return item.Type == "thinking" || item.Type == "redacted_thinking"
	})
}

func nativeToolProbeContinuation(p modelprofile.Profile, request, delivered []byte, value string) ([]byte, error) {
	var first struct {
		StopReason string            `json:"stop_reason"`
		Output     []json.RawMessage `json:"output"`
		Content    []json.RawMessage `json:"content"`
	}
	var next map[string]any
	if json.Unmarshal(delivered, &first) != nil || json.Unmarshal(request, &next) != nil {
		return nil, fmt.Errorf("invalid validated tool response")
	}
	items := first.Output
	isMessages := p.APISurface == modelprofile.APISurfaceAnthropicMessage
	if isMessages {
		if first.StopReason != "tool_use" {
			return nil, fmt.Errorf("invalid native tool stop")
		}
		items = first.Content
	}
	callID := ""
	for _, raw := range items {
		var item nativeProbeItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		if item.Type != "tool_use" && item.Type != "function_call" {
			continue
		}
		if callID != "" || item.Name != nativeToolProbeName || (isMessages && item.Type != "tool_use") || (!isMessages && item.Type != "function_call") {
			return nil, fmt.Errorf("invalid native tool name or count")
		}
		id, args := item.CallID, []byte(item.Arguments)
		if isMessages {
			id, args = item.ID, item.Input
		}
		var fields map[string]string
		if id == "" || json.Unmarshal(args, &fields) != nil || len(fields) != 1 || fields["key"] != "check" {
			return nil, fmt.Errorf("invalid native tool call")
		}
		callID = id
	}
	if callID == "" {
		return nil, fmt.Errorf("native tool call missing")
	}
	if isMessages {
		next["messages"] = []any{
			map[string]string{"role": "user", "content": nativeToolProbeInput},
			map[string]any{"role": "assistant", "content": items},
			map[string]any{"role": "user", "content": []map[string]string{{"type": "tool_result", "tool_use_id": callID, "content": value}}},
		}
	} else {
		history := []any{map[string]string{"role": "user", "content": nativeToolProbeInput}}
		for _, item := range items {
			history = append(history, item)
		}
		next["input"] = append(history, map[string]string{"type": "function_call_output", "call_id": callID, "output": value})
	}
	return json.Marshal(next)
}

func nativeToolProbeAcknowledged(p modelprofile.Profile, body []byte, value string) bool {
	var response struct {
		StopReason string            `json:"stop_reason"`
		Output     []nativeProbeItem `json:"output"`
		OutputText string            `json:"output_text"`
		Content    []nativeProbeItem `json:"content"`
	}
	if json.Unmarshal(body, &response) != nil {
		return false
	}
	if p.APISurface == modelprofile.APISurfaceOpenAIResponses {
		for _, item := range response.Output {
			if item.Type != "reasoning" && item.Type != "message" {
				return false
			}
		}
		return strings.TrimSpace(response.OutputText) == value
	}
	if response.StopReason != "end_turn" {
		return false
	}
	var text strings.Builder
	for _, item := range response.Content {
		if item.Type == "text" {
			text.WriteString(item.Text)
		} else if item.Type != "thinking" && item.Type != "redacted_thinking" {
			return false
		}
	}
	return strings.TrimSpace(text.String()) == value
}

func (h *Handler) executeNativeToolOperationProbe(ctx context.Context, p modelprofile.Profile, s canonicalOperationProbeSnapshot, secret string, body []byte, requestID string) canonicalOperationProbeOutcome {
	profile, origin, err := nativeProbeIdentity(p, s.Provider, s.BaseURL, s.AdapterType, s.OperationID, s.ProfileID, s.ProfileVersion, s.UpstreamModel, s.AdapterConfig)
	if err != nil {
		return canonicalOperationProbeOutcome{ReasonCode: "operation_unsupported"}
	}
	ctx, cancel := context.WithTimeout(ctx, canonicalOperationProbeTimeout)
	defer cancel()
	started := time.Now()
	step := nativeProbeStep{Phase: "text"}
	delivery, failure, _ := h.nativeProbeExchange(ctx, profile, modelprofile.NativeClientToolsRequestContract(p), origin, secret, s.UpstreamModel, requestID, body, &step)
	result := canonicalOperationProbeOutcome{
		StatusCode: step.Status, LatencyMS: time.Since(started).Milliseconds(), ResponseBytes: step.ResponseBytes, ReasonCode: failure,
	}
	if failure != "" {
		return result
	}
	if !nativeToolProbeAcknowledged(p, delivery.Body(), "Ready.") && !nativeToolProbeAcknowledged(p, delivery.Body(), "Ready") {
		result.ReasonCode = "provider_response_invalid"
		return result
	}
	result.ProviderSuccess, result.ReasonCode, result.ReportedModel, result.OutputCount = true, "ok", s.UpstreamModel, 1
	for _, unit := range []struct {
		class endpointprotocol.UnitClass
		count int64
	}{
		{endpointprotocol.UnitInputTokens, step.InputTokens - step.CacheReadTokens - step.CacheWriteTokens},
		{endpointprotocol.UnitCacheReadTokens, step.CacheReadTokens},
		{endpointprotocol.UnitCacheWriteTokens, step.CacheWriteTokens},
		{endpointprotocol.UnitOutputTokens, step.OutputTokens},
	} {
		result.Units = append(result.Units, endpointprotocol.UnitObservation{
			Class: unit.class, Quantity: strconv.FormatInt(unit.count, 10),
			Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly,
		})
	}
	return result
}
