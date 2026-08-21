package gateway

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const (
	embeddingsDeliveryWhitelistVersion = "openai-embeddings-delivery.v1"
	maxEmbeddingResponseItems          = 65_536
	maxEmbeddingVectorDimensions       = 65_536
	maxEmbeddingUsageTokens            = 1<<31 - 1
	embeddingsEncodingFloat            = "float"
	embeddingsEncodingBase64           = "base64"
)

type embeddingsResponseExpectation struct {
	encodingFormat string
	itemCount      int
	dimensions     int
}

type embeddingsResponseInspection struct {
	failurePath string
}

func (inspection embeddingsResponseInspection) complete() bool {
	return inspection.failurePath == ""
}

func (inspection *embeddingsResponseInspection) markIncomplete(path string) {
	if inspection.failurePath == "" {
		inspection.failurePath = path
	}
}

// embeddingsResponseExpectationFromRequest freezes the response shape promised
// by the final admitted request. The request has already passed the embeddings
// admission validator; this function narrows the response-only fields that the
// delivery inspector must bind to.
func embeddingsResponseExpectationFromRequest(body []byte) (embeddingsResponseExpectation, error) {
	var request map[string]interface{}
	if err := decodeJSONUseNumber(body, &request); err != nil || request == nil {
		return embeddingsResponseExpectation{}, fmt.Errorf("embeddings response expectation requires an object request")
	}

	expectation := embeddingsResponseExpectation{encodingFormat: embeddingsEncodingFloat}
	if value, exists := request["encoding_format"]; exists && value != nil {
		format, ok := value.(string)
		if !ok || (format != embeddingsEncodingFloat && format != embeddingsEncodingBase64) {
			return embeddingsResponseExpectation{}, fmt.Errorf("unsupported embeddings encoding_format")
		}
		expectation.encodingFormat = format
	}
	if value, exists := request["dimensions"]; exists && value != nil {
		number, ok := value.(json.Number)
		if !ok {
			return embeddingsResponseExpectation{}, fmt.Errorf("embeddings dimensions must be an integer")
		}
		dimensions, err := number.Int64()
		if err != nil || dimensions <= 0 || dimensions > maxEmbeddingVectorDimensions {
			return embeddingsResponseExpectation{}, fmt.Errorf("embeddings dimensions are outside the supported range")
		}
		expectation.dimensions = int(dimensions)
	}

	itemCount, ok := embeddingsInputItemCount(request["input"])
	if !ok || itemCount <= 0 || itemCount > maxEmbeddingResponseItems {
		return embeddingsResponseExpectation{}, fmt.Errorf("embeddings input item count is outside the supported range")
	}
	expectation.itemCount = itemCount
	return expectation, nil
}

func embeddingsInputItemCount(input interface{}) (int, bool) {
	switch value := input.(type) {
	case string:
		return 1, true
	case []interface{}:
		if len(value) == 0 {
			return 0, false
		}
		switch value[0].(type) {
		case string:
			for _, item := range value {
				if _, ok := item.(string); !ok {
					return 0, false
				}
			}
			return len(value), true
		case json.Number:
			for _, item := range value {
				if _, ok := item.(json.Number); !ok {
					return 0, false
				}
			}
			return 1, true
		case []interface{}:
			for _, item := range value {
				if _, ok := item.([]interface{}); !ok {
					return 0, false
				}
			}
			return len(value), true
		}
	}
	return 0, false
}

// inspectEmbeddingsResponseEnvelope validates the bounded OpenAI-compatible
// embeddings response that UniRouter is prepared to deliver. Provider usage is
// optional because the existing contract can establish a conservative estimate;
// every delivered field is nevertheless fixed and unknown fields fail closed.
func inspectEmbeddingsResponseEnvelope(response map[string]interface{}, expectation embeddingsResponseExpectation) embeddingsResponseInspection {
	inspection := embeddingsResponseInspection{}
	if response == nil || !streamMapKeysSupported(response, "object", "data", "model", "usage") {
		inspection.markIncomplete(embeddingsDeliveryWhitelistVersion + ".root")
	}

	object, ok := response["object"].(string)
	if !ok || object != "list" {
		inspection.markIncomplete(embeddingsDeliveryWhitelistVersion + ".object")
	}
	model, ok := response["model"].(string)
	if !ok || strings.TrimSpace(model) == "" {
		inspection.markIncomplete(embeddingsDeliveryWhitelistVersion + ".model")
	}
	if value, exists := response["usage"]; exists {
		if !embeddingsUsageShapeSupported(value) {
			inspection.markIncomplete(embeddingsDeliveryWhitelistVersion + ".usage")
		}
	}

	rawData, exists := response["data"]
	data, ok := rawData.([]interface{})
	if !exists || !ok || len(data) != expectation.itemCount || len(data) == 0 || len(data) > maxEmbeddingResponseItems {
		inspection.markIncomplete(embeddingsDeliveryWhitelistVersion + ".data")
		return inspection
	}

	expectedDimensions := -1
	for itemIndex, rawItem := range data {
		path := embeddingsDeliveryWhitelistVersion + ".data"
		item, ok := rawItem.(map[string]interface{})
		if !ok || !streamMapKeysSupported(item, "object", "index", "embedding") {
			inspection.markIncomplete(path)
			continue
		}

		object, ok := item["object"].(string)
		if !ok || object != "embedding" {
			inspection.markIncomplete(path + ".object")
		}
		index, ok := exactEmbeddingResponseIndex(item["index"])
		if !ok || index != itemIndex {
			inspection.markIncomplete(path + ".index")
		}
		dimensions, valid := inspectEmbeddingVector(item["embedding"], expectation.encodingFormat)
		if !valid {
			inspection.markIncomplete(path + ".embedding")
			continue
		}
		if expectation.dimensions > 0 && dimensions != expectation.dimensions {
			inspection.markIncomplete(path + ".embedding")
		}
		if expectedDimensions == -1 {
			expectedDimensions = dimensions
		} else if dimensions != expectedDimensions {
			inspection.markIncomplete(path + ".embedding")
		}
	}
	return inspection
}

func inspectEmbeddingVector(value interface{}, encodingFormat string) (int, bool) {
	switch encodingFormat {
	case embeddingsEncodingFloat:
		vector, ok := value.([]interface{})
		if !ok || len(vector) == 0 || len(vector) > maxEmbeddingVectorDimensions {
			return 0, false
		}
		for _, value := range vector {
			number, ok := value.(json.Number)
			if !ok {
				return 0, false
			}
			parsed, err := number.Float64()
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
				return 0, false
			}
		}
		return len(vector), true
	case embeddingsEncodingBase64:
		encoded, ok := value.(string)
		if !ok || encoded == "" || strings.TrimSpace(encoded) != encoded || strings.ContainsAny(encoded, "\r\n\t ") {
			return 0, false
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(decoded) == 0 || len(decoded)%4 != 0 {
			return 0, false
		}
		dimensions := len(decoded) / 4
		if dimensions > maxEmbeddingVectorDimensions {
			return 0, false
		}
		for offset := 0; offset < len(decoded); offset += 4 {
			value := math.Float32frombits(binary.LittleEndian.Uint32(decoded[offset : offset+4]))
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return 0, false
			}
		}
		return dimensions, true
	default:
		return 0, false
	}
}

func embeddingsUsageShapeSupported(value interface{}) bool {
	usage, ok := value.(map[string]interface{})
	if !ok || !streamMapKeysSupported(usage,
		"prompt_tokens", "completion_tokens", "total_tokens", "prompt_tokens_details",
		"prompt_cache_hit_tokens", "prompt_cache_miss_tokens",
	) {
		return false
	}
	for _, key := range []string{"prompt_tokens", "completion_tokens", "total_tokens", "prompt_cache_hit_tokens", "prompt_cache_miss_tokens"} {
		if counter, exists := usage[key]; exists && counter != nil && !embeddingUsageCounterSupported(counter) {
			return false
		}
	}
	if rawDetails, exists := usage["prompt_tokens_details"]; exists && rawDetails != nil {
		details, ok := rawDetails.(map[string]interface{})
		if !ok || !streamMapKeysSupported(details, "cached_tokens", "cache_write_tokens") {
			return false
		}
		for _, counter := range details {
			if counter != nil && !embeddingUsageCounterSupported(counter) {
				return false
			}
		}
	}
	return true
}

func embeddingUsageCounterSupported(value interface{}) bool {
	number, ok := value.(json.Number)
	if !ok {
		return false
	}
	counter, err := number.Int64()
	return err == nil && counter >= 0 && counter <= maxEmbeddingUsageTokens
}

func exactEmbeddingResponseIndex(value interface{}) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := number.Int64()
	if err != nil || parsed < 0 || parsed >= maxEmbeddingResponseItems {
		return 0, false
	}
	return int(parsed), true
}
