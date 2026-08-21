package endpointprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	maxRequestBytes       = 2 << 20
	maxResponseBytes      = 32 << 20
	maxModelBytes         = 256
	maxOpenAIRequestIDLen = 512
)

var (
	canonicalUintPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	plainDecimalPattern  = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
)

func validateBuildInput(input BuildInput, openAIRequestID bool) (*url.URL, error) {
	baseURL, err := url.Parse(input.BaseURL)
	if err != nil || baseURL.Scheme != "https" || baseURL.Host == "" ||
		baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" ||
		(baseURL.Path != "" && baseURL.Path != "/") {
		return nil, protocolError(ErrInvalidConfiguration, "base_url_must_be_https_origin")
	}
	if input.APIKey == "" || input.APIKey != strings.TrimSpace(input.APIKey) ||
		strings.ContainsAny(input.APIKey, "\r\n") {
		return nil, protocolError(ErrInvalidConfiguration, "api_key_invalid")
	}
	if !isPortableIdentifier(input.RequestedModel, maxModelBytes) ||
		!isPortableIdentifier(input.MappedModel, maxModelBytes) {
		return nil, protocolError(ErrInvalidConfiguration, "model_binding_invalid")
	}
	if openAIRequestID && input.RequestID != "" {
		if len(input.RequestID) > maxOpenAIRequestIDLen {
			return nil, protocolError(ErrInvalidConfiguration, "request_id_too_long")
		}
		for _, value := range []byte(input.RequestID) {
			if value < 0x20 || value > 0x7e {
				return nil, protocolError(ErrInvalidConfiguration, "request_id_not_printable_ascii")
			}
		}
	}
	return baseURL, nil
}

func isPortableIdentifier(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || value != strings.TrimSpace(value) || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}

func buildPOSTRequest(
	ctx context.Context,
	baseURL *url.URL,
	endpoint string,
	apiKey string,
	requestID string,
	body []byte,
) (*http.Request, error) {
	target := *baseURL
	target.Path = endpoint
	target.RawPath = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, protocolError(ErrInvalidConfiguration, "request_construction_failed")
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	if requestID != "" {
		request.Header.Set("X-Client-Request-Id", requestID)
	}
	return request, nil
}

func validateStrictJSON(body []byte, maxBytes int, kind error) error {
	if len(body) == 0 || len(body) > maxBytes {
		return protocolError(kind, "json_size_invalid")
	}
	if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strictErr != nil {
		if strictErr.Code == "duplicate_json_key" {
			return protocolError(ErrInvalidJSON, "duplicate_json_key")
		}
		return protocolError(ErrInvalidJSON, "malformed_json")
	}
	return nil
}

func decodeStrictRequest(body []byte, target any) error {
	if err := validateStrictJSON(body, maxRequestBytes, ErrInvalidRequest); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return protocolError(ErrInvalidRequest, "request_shape_invalid")
	}
	if err := requireDecoderEOF(decoder); err != nil {
		return protocolError(ErrInvalidJSON, "trailing_json")
	}
	return nil
}

func decodeResponse(body []byte, target any) error {
	return decodeResponseWithLimit(body, target, maxResponseBytes)
}

func decodeResponseWithLimit(body []byte, target any, maxBytes int) error {
	if err := validateStrictJSON(body, maxBytes, ErrInvalidResponse); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return protocolError(ErrInvalidResponse, "response_shape_invalid")
	}
	if err := requireDecoderEOF(decoder); err != nil {
		return protocolError(ErrInvalidJSON, "trailing_json")
	}
	return nil
}

func requireDecoderEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("extra json value")
		}
		return err
	}
	return nil
}

func objectKeys(body []byte) (map[string]json.RawMessage, error) {
	if err := validateStrictJSON(body, maxRequestBytes, ErrInvalidRequest); err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil || object == nil {
		return nil, protocolError(ErrInvalidRequest, "request_must_be_object")
	}
	return object, nil
}

func rejectKnownUnsupported(body []byte, fields ...string) error {
	object, err := objectKeys(body)
	if err != nil {
		return err
	}
	for _, field := range fields {
		if _, exists := object[field]; exists {
			return protocolError(ErrUnsupportedSemantics, "unsupported_"+field)
		}
	}
	return nil
}

func parseNonnegativeInt64(value json.Number, field string) (int64, error) {
	text := string(value)
	if !canonicalUintPattern.MatchString(text) {
		return 0, protocolError(ErrInvalidResponse, field+"_not_canonical_uint")
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, protocolError(ErrInvalidResponse, field+"_out_of_range")
	}
	return parsed, nil
}

func parseBoundedDecimal(value json.Number, field string, minimum, maximum *big.Rat) (*big.Rat, error) {
	text := string(value)
	if !plainDecimalPattern.MatchString(text) {
		return nil, protocolError(ErrInvalidResponse, field+"_not_plain_decimal")
	}
	parsed, ok := new(big.Rat).SetString(text)
	if !ok || parsed.Cmp(minimum) < 0 || parsed.Cmp(maximum) > 0 {
		return nil, protocolError(ErrInvalidResponse, field+"_out_of_range")
	}
	return parsed, nil
}

func observedUnit(class UnitClass, quantity string) UnitObservation {
	return UnitObservation{
		Class:       class,
		Quantity:    quantity,
		Authority:   "provider_reported",
		Finality:    "final",
		ChargeState: ChargeObservedOnly,
	}
}

func requireBinding(binding *Binding, descriptor Descriptor) error {
	if binding == nil ||
		binding.profileID != descriptor.ProfileID ||
		binding.profileVersion != descriptor.ProfileVersion {
		return protocolError(ErrInvalidConfiguration, "response_binding_invalid")
	}
	return nil
}

func classifyHTTPStatus(statusCode int) (string, bool) {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "auth", false
	case http.StatusRequestTimeout:
		return "channel_failure", true
	case http.StatusTooManyRequests:
		return "rate_limit", true
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "channel_failure", true
	default:
		if statusCode >= 400 && statusCode < 500 {
			return "client_request", false
		}
		return "unknown", false
	}
}

func rejectNonSuccess(statusCode int) error {
	if statusCode >= 200 && statusCode < 300 {
		return nil
	}
	failureKind, _ := classifyHTTPStatus(statusCode)
	return protocolError(ErrUpstreamResponse, fmt.Sprintf("http_%d_%s", statusCode, failureKind))
}
