package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// decodeJSONUseNumber is the only decoder for JSON that will be mutated and
// re-encoded by the gateway. It prevents guardrail, response extension and
// cache-delivery paths from silently rounding provider integers above 2^53.
func decodeJSONUseNumber(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON document contains trailing data")
		}
		return err
	}
	return nil
}
