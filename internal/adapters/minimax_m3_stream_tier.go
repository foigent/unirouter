package adapters

import (
	"encoding/json"
	"fmt"

	"github.com/uniroute/unirouter/internal/strictjson"
)

// v11 alone selects this response metadata normalization. The supplier's tier
// is never request, price or settlement authority. Unknown siblings remain for
// the existing canonical parser; older protocol implementations are unchanged.
func prepareMiniMaxM3StreamTier(data []byte, expectedModel string) ([]byte, error) {
	if err := strictjson.Validate(data); err != nil {
		return nil, err
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil || envelope == nil {
		return nil, fmt.Errorf("invalid MiniMax stream envelope")
	}
	var model string
	if json.Unmarshal(envelope["model"], &model) != nil || model != expectedModel {
		return nil, fmt.Errorf("MiniMax stream model mismatch")
	}
	if raw, exists := envelope["service_tier"]; exists {
		var tier string
		if json.Unmarshal(raw, &tier) != nil || (tier != "standard" && tier != "priority") {
			// Independently valid usage can remain supplier evidence; callers
			// must still withhold delivery on this error.
			return data, fmt.Errorf("invalid MiniMax stream service_tier")
		}
		delete(envelope, "service_tier")
		return json.Marshal(envelope)
	}
	return data, nil
}
