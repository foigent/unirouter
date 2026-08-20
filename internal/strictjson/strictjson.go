// Package strictjson validates JSON documents without imposing a protocol
// schema. It rejects duplicate object members at every nesting level and uses
// json.Number while producing canonical bytes, so callers never round numbers
// through float64. The original document remains the protocol layer's
// authority for RawMessage retention and unknown-field handling.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrDuplicateKey = errors.New("strictjson: duplicate object member")

// Canonicalize validates one complete JSON document and returns a stable
// encoding suitable for request identity and fingerprinting. Object member
// order and insignificant whitespace are normalized; number lexemes retain
// json.Number precision.
func Canonicalize(data []byte) ([]byte, error) {
	value, err := parse(data, true)
	if err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("strictjson: canonicalize document: %w", err)
	}
	return canonical, nil
}

// Validate applies the same duplicate-member and complete-document rules as
// Canonicalize without retaining the decoded value tree. This is the normal
// ingress path when no idempotency fingerprint is requested.
func Validate(data []byte) error {
	_, err := parse(data, false)
	return err
}

func parse(data []byte, retain bool) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	value, err := decodeValue(decoder, retain)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("strictjson: multiple top-level values")
		}
		return nil, fmt.Errorf("strictjson: trailing data: %w", err)
	}

	return value, nil
}

func decodeValue(decoder *json.Decoder, retain bool) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("strictjson: decode value: %w", err)
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		if !retain {
			return nil, nil
		}
		return token, nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		var object map[string]any
		if retain {
			object = make(map[string]any)
		}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, fmt.Errorf("strictjson: decode object member: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("strictjson: object member name is not a string")
			}
			if _, exists := seen[key]; exists {
				return nil, ErrDuplicateKey
			}
			seen[key] = struct{}{}
			value, err := decodeValue(decoder, retain)
			if err != nil {
				return nil, err
			}
			if retain {
				object[key] = value
			}
		}
		if err := consumeDelimiter(decoder, '}'); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		var array []any
		if retain {
			array = make([]any, 0)
		}
		for decoder.More() {
			value, err := decodeValue(decoder, retain)
			if err != nil {
				return nil, err
			}
			if retain {
				array = append(array, value)
			}
		}
		if err := consumeDelimiter(decoder, ']'); err != nil {
			return nil, err
		}
		return array, nil
	default:
		return nil, fmt.Errorf("strictjson: unexpected delimiter %q", delimiter)
	}
}

func consumeDelimiter(decoder *json.Decoder, expected json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("strictjson: close container: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != expected {
		return fmt.Errorf("strictjson: expected delimiter %q", expected)
	}
	return nil
}
