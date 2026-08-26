package endpointprotocol

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// anthropicMessagesProfileV2 is the append-only fixed wire identity used by
// the dual synchronous/SSE public Messages executor. The richer reasoning
// request and response semantics remain in the versioned public model contract;
// this profile only fixes the transport mode without rewriting v1.
type anthropicMessagesProfileV2 struct{}

func (anthropicMessagesProfileV2) Descriptor() Descriptor {
	descriptor := (anthropicMessagesProfile{}).Descriptor()
	descriptor.ProfileID = ProfileAnthropicMessagesTextV2
	descriptor.ProfileVersion = "1.0.0"
	descriptor.ProviderFinality = "terminal_message_or_message_stop_with_provider_usage"
	unsupported := make([]string, 0, len(descriptor.UnsupportedSemantics)-1)
	for _, semantic := range descriptor.UnsupportedSemantics {
		if semantic != "streaming" {
			unsupported = append(unsupported, semantic)
		}
	}
	descriptor.UnsupportedSemantics = unsupported
	return descriptor
}

func (profile anthropicMessagesProfileV2) BuildRequest(
	ctx context.Context,
	input BuildInput,
) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, false)
	if err != nil {
		return nil, nil, err
	}
	envelope, err := objectKeys(input.Body)
	if err != nil {
		return nil, nil, err
	}
	streaming, err := exactAnthropicMessagesStreamValue(envelope)
	if err != nil {
		return nil, nil, err
	}
	validation := make(map[string]json.RawMessage, len(envelope)+1)
	for name, raw := range envelope {
		validation[name] = append(json.RawMessage(nil), raw...)
	}
	validation["stream"] = json.RawMessage("false")
	validationBody, err := json.Marshal(validation)
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	validationInput := input
	validationInput.Body = validationBody
	historicalRequest, _, err := (anthropicMessagesProfile{}).BuildRequest(ctx, validationInput)
	if err != nil {
		return nil, nil, err
	}
	defer historicalRequest.Body.Close()
	wireBody, err := io.ReadAll(historicalRequest.Body)
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	var wire map[string]json.RawMessage
	if json.Unmarshal(wireBody, &wire) != nil || wire == nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	wire["stream"] = json.RawMessage("false")
	if streaming {
		wire["stream"] = json.RawMessage("true")
	}
	wireBody, err = json.Marshal(wire)
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	request, binding, err := buildAnthropicMessagesHTTPRequest(
		ctx, *baseURL, input, wireBody, profile.Descriptor(),
	)
	if err != nil {
		return nil, nil, err
	}
	binding.streaming = streaming
	return request, binding, nil
}

func (profile anthropicMessagesProfileV2) ParseResponse(
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
	historical.profileID = ProfileAnthropicMessagesText
	historical.profileVersion = "1.0.0"
	outcome, err := (anthropicMessagesProfile{}).ParseResponse(ctx, &historical, statusCode, body)
	if err != nil {
		return Outcome{}, err
	}
	outcome.ProfileID = profile.Descriptor().ProfileID
	return outcome, nil
}

func (anthropicMessagesProfileV2) ClassifyHTTPStatus(statusCode int) (string, bool) {
	return (anthropicMessagesProfile{}).ClassifyHTTPStatus(statusCode)
}
