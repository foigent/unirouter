package telemetryexport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 32 * 1024

type HTTPSink struct {
	endpoint string
	client   *http.Client
}

func NewHTTPSink(endpoint string, timeout time.Duration) (*HTTPSink, error) {
	normalized, err := normalizeEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &HTTPSink{
		endpoint: normalized,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func normalizeEndpoint(endpoint string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT must be a valid http or https URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT must use http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT must not contain userinfo, query string or fragment")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/v1/traces"
	}
	return parsed.String(), nil
}

func (s *HTTPSink) Export(ctx context.Context, payload []byte) (ExportResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(payload))
	if err != nil {
		return ExportResult{}, &transportError{err: err}
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return ExportResult{}, &transportError{err: err}
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if readErr != nil {
		return ExportResult{}, &transportError{err: readErr}
	}
	if len(body) > maxResponseBytes {
		return ExportResult{}, &httpStatusError{
			statusCode: response.StatusCode,
			retryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()),
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ExportResult{}, &httpStatusError{
			statusCode: response.StatusCode,
			retryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()),
		}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return ExportResult{}, nil
	}
	var decoded struct {
		PartialSuccess *struct {
			RejectedSpans json.RawMessage `json:"rejectedSpans"`
		} `json:"partialSuccess"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return ExportResult{}, &responseError{err: err}
	}
	if decoded.PartialSuccess == nil {
		return ExportResult{}, nil
	}
	rejected, err := parseRejectedSpans(decoded.PartialSuccess.RejectedSpans)
	if err != nil {
		return ExportResult{}, &responseError{err: err}
	}
	return ExportResult{
		RejectedSpans: rejected,
	}, nil
}

func parseRejectedSpans(raw json.RawMessage) (uint64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	var text string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, err
		}
	} else {
		text = string(raw)
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid OTLP partial-success rejectedSpans")
	}
	return value, nil
}

type transportError struct {
	err error
}

func (e *transportError) Error() string {
	return "OTLP transport failed"
}

func (e *transportError) Unwrap() error {
	return e.err
}

func (e *transportError) Retryable() bool {
	return true
}

type httpStatusError struct {
	statusCode int
	retryAfter time.Duration
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("OTLP HTTP status %d", e.statusCode)
}

func (e *httpStatusError) Retryable() bool {
	switch e.statusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func (e *httpStatusError) RetryAfter() time.Duration {
	return e.retryAfter
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}

type responseError struct {
	err error
}

func (e *responseError) Error() string {
	return "OTLP response was invalid"
}

func (e *responseError) Unwrap() error {
	return e.err
}

func (e *responseError) Retryable() bool {
	return false
}
