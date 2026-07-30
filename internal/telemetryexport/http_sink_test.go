package telemetryexport

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestHTTPSinkUsesOTLPJSONContractAndParsesPartialSuccess(t *testing.T) {
	sink, err := NewHTTPSink("http://collector.internal:4318", time.Second)
	if err != nil {
		t.Fatalf("new HTTP sink: %v", err)
	}
	sink.client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "http://collector.internal:4318/v1/traces" {
			t.Fatalf("unexpected endpoint: %s", request.URL)
		}
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected request contract: method=%s content-type=%s", request.Method, request.Header.Get("Content-Type"))
		}
		if request.Header.Get("Authorization") != "" {
			t.Fatal("B3 exporter must not synthesize credential headers")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if string(body) != `{"resourceSpans":[]}` {
			t.Fatalf("unexpected request bytes: %s", body)
		}
		return &http.Response{
			StatusCode: 200,
			Body: io.NopCloser(strings.NewReader(
				`{"partialSuccess":{"rejectedSpans":"2","errorMessage":"policy"}}`,
			)),
			Header: make(http.Header),
		}, nil
	})
	result, err := sink.Export(context.Background(), []byte(`{"resourceSpans":[]}`))
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if result.RejectedSpans != 2 {
		t.Fatalf("unexpected partial success: %+v", result)
	}
}

func TestHTTPSinkRetryClassificationAndEndpointValidation(t *testing.T) {
	for _, test := range []struct {
		status    int
		retryable bool
	}{
		{400, false},
		{401, false},
		{429, true},
		{502, true},
		{503, true},
		{504, true},
	} {
		err := &httpStatusError{statusCode: test.status}
		if err.Retryable() != test.retryable {
			t.Fatalf("status %d retryable=%t, want %t", test.status, err.Retryable(), test.retryable)
		}
	}
	for _, endpoint := range []string{
		"",
		"ftp://collector/v1/traces",
		"http://user:pass@collector/v1/traces",
		"http://collector/v1/traces?token=secret",
		"http://collector/v1/traces#fragment",
	} {
		if _, err := NewHTTPSink(endpoint, time.Second); err == nil {
			t.Fatalf("expected endpoint rejection: %q", endpoint)
		}
	}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	if got := parseRetryAfter("3", now); got != 3*time.Second {
		t.Fatalf("unexpected delta Retry-After: %s", got)
	}
	if got := parseRetryAfter(now.Add(2*time.Second).Format(http.TimeFormat), now); got != 2*time.Second {
		t.Fatalf("unexpected HTTP-date Retry-After: %s", got)
	}
	if got := retryDelay(&httpStatusError{statusCode: 429, retryAfter: 30 * time.Second}, 0); got != maxRetryDelay {
		t.Fatalf("Retry-After must be bounded to %s, got %s", maxRetryDelay, got)
	}
}
