package endpointprotocol

import (
	"context"
	"encoding/json"
	"net/http"
)

// openAIResponsesProfileV2 is the append-only exact Responses text contract
// used by the dual-mode public executor. It keeps the v1 request grammar and
// stateless store=false policy while allowing the caller to select the exact
// synchronous or SSE transport with a boolean stream field.
type openAIResponsesProfileV2 struct{}

func (openAIResponsesProfileV2) Descriptor() Descriptor {
	descriptor := (openAIResponsesProfile{}).Descriptor()
	descriptor.ProfileID = ProfileOpenAIResponsesTextV2
	descriptor.ProfileVersion = "1.0.0"
	descriptor.ProviderFinality = "response.completed_with_provider_usage"
	unsupported := make([]string, 0, len(descriptor.UnsupportedSemantics)-1)
	for _, semantic := range descriptor.UnsupportedSemantics {
		if semantic != "streaming" {
			unsupported = append(unsupported, semantic)
		}
	}
	descriptor.UnsupportedSemantics = unsupported
	return descriptor
}

func (profile openAIResponsesProfileV2) BuildRequest(
	ctx context.Context,
	input BuildInput,
) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	envelope, err := parseOpenAIResponsesEnvelope(input.Body)
	if err != nil {
		return nil, nil, err
	}
	streaming, err := exactResponsesStreamValue(envelope)
	if err != nil {
		return nil, nil, err
	}
	validation := cloneOpenAIResponsesEnvelope(envelope)
	validation["stream"] = json.RawMessage("false")
	if err := validateOpenAIResponsesRequest(validation, input.RequestedModel); err != nil {
		return nil, nil, err
	}
	mappedModel, _ := json.Marshal(input.MappedModel)
	envelope["model"] = mappedModel
	envelope["store"] = json.RawMessage("false")
	if streaming {
		envelope["stream"] = json.RawMessage("true")
	} else {
		envelope["stream"] = json.RawMessage("false")
	}
	wireBody, err := json.Marshal(envelope)
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	request, err := buildPOSTRequest(ctx, baseURL, "/v1/responses", input.APIKey, input.RequestID, wireBody)
	if err != nil {
		return nil, nil, err
	}
	return request, &Binding{
		profileID: profile.Descriptor().ProfileID, profileVersion: profile.Descriptor().ProfileVersion,
		requestedModel: input.RequestedModel, mappedModel: input.MappedModel, streaming: streaming,
	}, nil
}

func (profile openAIResponsesProfileV2) ParseResponse(
	ctx context.Context,
	binding *Binding,
	statusCode int,
	body []byte,
) (Outcome, error) {
	if err := requireBinding(binding, profile.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if binding.streaming {
		return Outcome{}, protocolError(ErrInvalidConfiguration, "streaming_response_requires_event_parser")
	}
	historical := *binding
	historical.profileID = ProfileOpenAIResponsesText
	historical.profileVersion = "1.0.0"
	outcome, err := (openAIResponsesProfile{}).ParseResponse(ctx, &historical, statusCode, body)
	if err != nil {
		return Outcome{}, err
	}
	outcome.ProfileID = profile.Descriptor().ProfileID
	return outcome, nil
}

func (openAIResponsesProfileV2) ClassifyHTTPStatus(statusCode int) (string, bool) {
	return (openAIResponsesProfile{}).ClassifyHTTPStatus(statusCode)
}

func exactResponsesStreamValue(envelope openAIResponsesEnvelope) (bool, error) {
	raw, exists := envelope["stream"]
	if !exists {
		return false, nil
	}
	var value bool
	if isExplicitJSONNull(raw) || json.Unmarshal(raw, &value) != nil {
		return false, protocolError(ErrInvalidRequest, "stream_invalid")
	}
	return value, nil
}

func cloneOpenAIResponsesEnvelope(envelope openAIResponsesEnvelope) openAIResponsesEnvelope {
	clone := make(openAIResponsesEnvelope, len(envelope))
	for field, value := range envelope {
		clone[field] = append(json.RawMessage(nil), value...)
	}
	return clone
}
