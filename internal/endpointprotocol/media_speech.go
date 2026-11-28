package endpointprotocol

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
)

func mediaToolJobDescriptor(d Descriptor) Descriptor {
	d.ContractVersion = ContractVersion
	d.ProfileVersion = "1.0.0"
	d.DownstreamEndpoint = "/v1/media/jobs"
	d.Authentication = "bearer"
	d.RuntimeState = RuntimeRepositoryAsyncMediaV1
	d.CustomerChargeState = ChargeObservedOnly
	return d
}

func decodeMediaToolInput(input BuildInput, operation string) (mediaToolInput, error) {
	var request mediaToolInput
	if err := decodeStrictRequest(input.Body, &request); err != nil {
		return mediaToolInput{}, err
	}
	if request.Model != input.RequestedModel || request.Operation != operation ||
		(request.N != 0 && request.N != 1) || request.Stream {
		return mediaToolInput{}, protocolError(ErrInvalidRequest, "speech_request_invalid")
	}
	return request, nil
}

func TranscriptionRequestBody(model string, audio []byte) ([]byte, error) {
	return json.Marshal(map[string]string{"model": model, "operation": "speech_to_text", "input_media": "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(audio)})
}

type mediaToolInput struct {
	Model      string `json:"model"`
	Operation  string `json:"operation"`
	Prompt     string `json:"prompt,omitempty"`
	InputMedia string `json:"input_media,omitempty"`
	N          int    `json:"n,omitempty"`
	Stream     bool   `json:"stream,omitempty"`
}

func reportedMediaTokenPair(input, output, total *int64) ([]UnitObservation, error) {
	if input == nil || output == nil || total == nil {
		return nil, protocolError(ErrInvalidResponse, "media_usage_partition_invalid")
	}
	return reportedMediaTokenDirections(input, output, total)
}

func reportedMediaTokenDirections(input, output, total *int64) ([]UnitObservation, error) {
	if (input != nil && *input < 0) || (output != nil && *output < 0) || (total != nil && *total < 0) ||
		(input != nil && output != nil && total != nil && (*total < *input || *total-*input != *output)) {
		return nil, protocolError(ErrInvalidResponse, "media_usage_partition_invalid")
	}
	units := make([]UnitObservation, 0, 2)
	if input != nil {
		units = append(units, observedUnit(UnitInputTokens, strconv.FormatInt(*input, 10)))
	}
	if output != nil {
		units = append(units, observedUnit(UnitOutputTokens, strconv.FormatInt(*output, 10)))
	}
	return units, nil
}

func inlineSpeechParts(data string) []map[string]any {
	return []map[string]any{{"type": "input_audio", "input_audio": map[string]string{"data": data}}}
}
