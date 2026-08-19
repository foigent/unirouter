package admin

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/uniroute/unirouter/internal/modellist"
)

var errInvalidModelMapping = errors.New("invalid model_mapping")

// normalizeModelMapping validates the one identity boundary that the gateway
// depends on: channel.models contains public model IDs, while model_mapping
// optionally maps each public ID to the exact supplier model/deployment ID.
func normalizeModelMapping(raw json.RawMessage, models string) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var input map[string]string
	if err := json.Unmarshal(raw, &input); err != nil || input == nil {
		return nil, errInvalidModelMapping
	}
	configured := make(map[string]struct{})
	for _, model := range modellist.Parse(models) {
		configured[model] = struct{}{}
	}
	normalized := make(map[string]string, len(input))
	for publicModel, upstreamModel := range input {
		publicModel = strings.TrimSpace(publicModel)
		upstreamModel = strings.TrimSpace(upstreamModel)
		if _, ok := configured[publicModel]; !ok || publicModel == "" || upstreamModel == "" ||
			len(publicModel) > 160 || len(upstreamModel) > 200 ||
			strings.ContainsAny(publicModel, ",，;；\r\n\x00") ||
			strings.ContainsAny(upstreamModel, ",，;；\r\n\x00") ||
			strings.IndexFunc(upstreamModel, unicode.IsSpace) >= 0 {
			return nil, errInvalidModelMapping
		}
		if _, duplicate := normalized[publicModel]; duplicate {
			return nil, errInvalidModelMapping
		}
		normalized[publicModel] = upstreamModel
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, errInvalidModelMapping
	}
	return encoded, nil
}
