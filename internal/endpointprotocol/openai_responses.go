package endpointprotocol

import (
	"context"
	"encoding/json"
	"math"
	"math/big"
	"net/http"
	"strings"
)

type openAIResponsesProfile struct{}

type openAIResponsesRequest struct {
	Model           string       `json:"model"`
	Input           string       `json:"input"`
	Instructions    string       `json:"instructions,omitempty"`
	MaxOutputTokens *json.Number `json:"max_output_tokens,omitempty"`
	Temperature     *json.Number `json:"temperature,omitempty"`
	TopP            *json.Number `json:"top_p,omitempty"`
	Store           *bool        `json:"store,omitempty"`
	Stream          *bool        `json:"stream,omitempty"`
}

type openAIResponsesWireRequest struct {
	Model           string       `json:"model"`
	Input           string       `json:"input"`
	Instructions    string       `json:"instructions,omitempty"`
	MaxOutputTokens *json.Number `json:"max_output_tokens,omitempty"`
	Temperature     *json.Number `json:"temperature,omitempty"`
	TopP            *json.Number `json:"top_p,omitempty"`
	Store           bool         `json:"store"`
	Stream          bool         `json:"stream"`
}

type openAIResponsesResponse struct {
	ID                string                  `json:"id"`
	Object            string                  `json:"object"`
	Status            string                  `json:"status"`
	Model             string                  `json:"model"`
	Error             json.RawMessage         `json:"error"`
	IncompleteDetails json.RawMessage         `json:"incomplete_details"`
	Output            []openAIResponsesOutput `json:"output"`
	Usage             *openAIResponsesUsage   `json:"usage"`
}

type openAIResponsesOutput struct {
	ID      string                   `json:"id"`
	Type    string                   `json:"type"`
	Status  string                   `json:"status"`
	Role    string                   `json:"role"`
	Content []openAIResponsesContent `json:"content"`
}

type openAIResponsesContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type openAIResponsesUsage struct {
	InputTokens       json.Number                 `json:"input_tokens"`
	InputTokenDetails *openAIResponsesInputDetail `json:"input_tokens_details"`
	OutputTokens      json.Number                 `json:"output_tokens"`
	TotalTokens       json.Number                 `json:"total_tokens"`
}

type openAIResponsesInputDetail struct {
	CachedTokens     *json.Number `json:"cached_tokens"`
	CacheWriteTokens *json.Number `json:"cache_write_tokens"`
}

func (openAIResponsesProfile) Descriptor() Descriptor {
	return Descriptor{
		ContractVersion:     ContractVersion,
		ProfileID:           ProfileOpenAIResponsesText,
		ProfileVersion:      "1.0.0",
		Provider:            "openai",
		EndpointKind:        EndpointResponses,
		DownstreamEndpoint:  "/v1/responses",
		UpstreamEndpoint:    "/v1/responses",
		Authentication:      "bearer",
		RuntimeState:        RuntimeContractOnly,
		ProviderFinality:    "status_completed_with_provider_usage",
		CustomerChargeState: ChargeObservedOnly,
		SupportedModalities: []string{"text"},
		ObservedUnitClasses: []UnitClass{
			UnitInputTokens,
			UnitCacheReadTokens,
			UnitCacheWriteTokens,
			UnitOutputTokens,
		},
		UnsupportedSemantics: []string{
			"background",
			"conversation_state",
			"function_or_hosted_tools",
			"multimodal_or_structured_input",
			"streaming",
			"stored_response_state",
			"structured_output",
		},
	}
}

func (profile openAIResponsesProfile) BuildRequest(
	ctx context.Context,
	input BuildInput,
) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if err := rejectKnownUnsupported(
		input.Body,
		"background",
		"conversation",
		"include",
		"parallel_tool_calls",
		"previous_response_id",
		"reasoning",
		"text",
		"tool_choice",
		"tools",
		"truncation",
	); err != nil {
		return nil, nil, err
	}

	object, err := objectKeys(input.Body)
	if err != nil {
		return nil, nil, err
	}
	if rawInput, exists := object["input"]; exists {
		var text string
		if json.Unmarshal(rawInput, &text) != nil {
			return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_multimodal_or_structured_input")
		}
	}

	var downstream openAIResponsesRequest
	if err := decodeStrictRequest(input.Body, &downstream); err != nil {
		return nil, nil, err
	}
	if downstream.Model == "" || downstream.Model != input.RequestedModel {
		return nil, nil, protocolError(ErrInvalidRequest, "requested_model_mismatch")
	}
	if downstream.Input == "" || len(downstream.Input) > maxRequestBytes/2 {
		return nil, nil, protocolError(ErrInvalidRequest, "input_text_invalid")
	}
	if downstream.Store != nil && *downstream.Store {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_stored_response_state")
	}
	if downstream.Stream != nil && *downstream.Stream {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_streaming")
	}
	if downstream.MaxOutputTokens != nil {
		limit, parseErr := parseNonnegativeInt64(*downstream.MaxOutputTokens, "max_output_tokens")
		if parseErr != nil || limit == 0 {
			return nil, nil, protocolError(ErrInvalidRequest, "max_output_tokens_invalid")
		}
	}
	if downstream.Temperature != nil {
		if _, parseErr := parseBoundedDecimal(*downstream.Temperature, "temperature", big.NewRat(0, 1), big.NewRat(2, 1)); parseErr != nil {
			return nil, nil, protocolError(ErrInvalidRequest, "temperature_invalid")
		}
	}
	if downstream.TopP != nil {
		if _, parseErr := parseBoundedDecimal(*downstream.TopP, "top_p", big.NewRat(0, 1), big.NewRat(1, 1)); parseErr != nil {
			return nil, nil, protocolError(ErrInvalidRequest, "top_p_invalid")
		}
	}

	wireBody, marshalErr := json.Marshal(openAIResponsesWireRequest{
		Model:           input.MappedModel,
		Input:           downstream.Input,
		Instructions:    downstream.Instructions,
		MaxOutputTokens: downstream.MaxOutputTokens,
		Temperature:     downstream.Temperature,
		TopP:            downstream.TopP,
		Store:           false,
		Stream:          false,
	})
	if marshalErr != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	request, err := buildPOSTRequest(ctx, baseURL, "/v1/responses", input.APIKey, input.RequestID, wireBody)
	if err != nil {
		return nil, nil, err
	}
	return request, &Binding{
		profileID:      profile.Descriptor().ProfileID,
		requestedModel: input.RequestedModel,
		mappedModel:    input.MappedModel,
	}, nil
}

func (profile openAIResponsesProfile) ParseResponse(
	_ context.Context,
	binding *Binding,
	statusCode int,
	body []byte,
) (Outcome, error) {
	if err := requireBinding(binding, profile.Descriptor().ProfileID); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(statusCode); err != nil {
		return Outcome{}, err
	}
	var response openAIResponsesResponse
	if err := decodeResponse(body, &response); err != nil {
		return Outcome{}, err
	}
	if response.ID == "" || response.Object != "response" || response.Model != binding.mappedModel {
		return Outcome{}, protocolError(ErrInvalidResponse, "response_identity_invalid")
	}
	if response.Status != "completed" || !isExplicitJSONNull(response.Error) || !isExplicitJSONNull(response.IncompleteDetails) {
		return Outcome{}, protocolError(ErrNonFinalResponse, "response_not_completed")
	}
	if len(response.Output) == 0 {
		return Outcome{}, protocolError(ErrInvalidResponse, "response_output_missing")
	}
	outputCount := 0
	for _, output := range response.Output {
		if output.Type != "message" || output.Status != "completed" || output.Role != "assistant" || len(output.Content) == 0 {
			return Outcome{}, protocolError(ErrUnsupportedSemantics, "unsupported_response_output")
		}
		for _, content := range output.Content {
			if content.Type != "output_text" {
				return Outcome{}, protocolError(ErrUnsupportedSemantics, "unsupported_response_content")
			}
			outputCount++
		}
	}
	if response.Usage == nil {
		return Outcome{}, protocolError(ErrMissingUsage, "provider_usage_missing")
	}
	inputTokens, err := parseNonnegativeInt64(response.Usage.InputTokens, string(UnitInputTokens))
	if err != nil {
		return Outcome{}, err
	}
	outputTokens, err := parseNonnegativeInt64(response.Usage.OutputTokens, string(UnitOutputTokens))
	if err != nil {
		return Outcome{}, err
	}
	totalTokens, err := parseNonnegativeInt64(response.Usage.TotalTokens, "total_tokens")
	if err != nil || inputTokens > math.MaxInt64-outputTokens || inputTokens+outputTokens != totalTokens {
		return Outcome{}, protocolError(ErrInvalidResponse, "usage_total_mismatch")
	}
	units := []UnitObservation{
		observedUnit(UnitInputTokens, response.Usage.InputTokens.String()),
		observedUnit(UnitOutputTokens, response.Usage.OutputTokens.String()),
	}
	if details := response.Usage.InputTokenDetails; details != nil {
		if details.CachedTokens != nil {
			cached, parseErr := parseNonnegativeInt64(*details.CachedTokens, string(UnitCacheReadTokens))
			if parseErr != nil || cached > inputTokens {
				return Outcome{}, protocolError(ErrInvalidResponse, "cache_read_tokens_invalid")
			}
			units = append(units, observedUnit(UnitCacheReadTokens, details.CachedTokens.String()))
		}
		if details.CacheWriteTokens != nil {
			written, parseErr := parseNonnegativeInt64(*details.CacheWriteTokens, string(UnitCacheWriteTokens))
			if parseErr != nil || written > inputTokens {
				return Outcome{}, protocolError(ErrInvalidResponse, "cache_write_tokens_invalid")
			}
			units = append(units, observedUnit(UnitCacheWriteTokens, details.CacheWriteTokens.String()))
		}
	}
	return Outcome{
		ContractVersion: ContractVersion,
		ProfileID:       profile.Descriptor().ProfileID,
		ProviderState:   "completed",
		ProviderObject:  response.ID,
		MappedModel:     binding.mappedModel,
		OutputCount:     outputCount,
		Units:           units,
	}, nil
}

func (openAIResponsesProfile) ClassifyHTTPStatus(statusCode int) (string, bool) {
	return classifyHTTPStatus(statusCode)
}

func isExplicitJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}
