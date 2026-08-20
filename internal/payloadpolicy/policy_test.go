package payloadpolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestDefaultLimitsMatchAcceptedProfiles(t *testing.T) {
	limits := DefaultLimits()
	if err := limits.Validate(); err != nil {
		t.Fatal(err)
	}
	if limits.ChatRequestBytes != 8*MiB || limits.EmbeddingsRequestBytes != 16*MiB || limits.NonStreamResponseBytes != 16*MiB {
		t.Fatalf("unexpected defaults: %+v", limits)
	}
	if PlatformHardMaxBytes != 64*MiB {
		t.Fatalf("unexpected platform hard max: %d", PlatformHardMaxBytes)
	}
}

func TestReadAllUsesActualBytesAndReleasesBudget(t *testing.T) {
	limits := DefaultLimits()
	limits.ChatRequestBytes = 4
	limits.EmbeddingsRequestBytes = 4
	limits.NonStreamResponseBytes = 4
	limits.NodeInFlightBytes = 8
	limits.OrgInFlightBytes = 8
	limits.ChatRequestBytes = 8
	limits.EmbeddingsRequestBytes = 8
	limits.NonStreamResponseBytes = 8
	budget := MustNewBudget(limits)

	body, lease, err := budget.ReadAll(bytes.NewBufferString("12345678"), 7, 8)
	if err != nil || string(body) != "12345678" {
		t.Fatalf("read failed: body=%q err=%v", body, err)
	}
	if _, _, err := budget.ReadAll(bytes.NewBufferString("x"), 7, 8); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected org budget denial, got %v", err)
	}
	lease.Release()
	if _, next, err := budget.ReadAll(bytes.NewBufferString("x"), 7, 8); err != nil {
		t.Fatalf("expected released budget, got %v", err)
	} else {
		next.Release()
	}
}

func TestReadAllRejectsActualOversizeWithoutTrustingLength(t *testing.T) {
	limits := DefaultLimits()
	limits.ChatRequestBytes = 4
	limits.EmbeddingsRequestBytes = 4
	limits.NonStreamResponseBytes = 4
	budget := MustNewBudget(limits)
	if _, _, err := budget.ReadAll(bytes.NewBufferString("12345"), 1, 4); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("expected payload limit, got %v", err)
	}
	if _, lease, err := budget.ReadAll(bytes.NewBufferString("1234"), 1, 4); err != nil {
		t.Fatalf("budget leaked after oversize: %v", err)
	} else {
		lease.Release()
	}
}

func TestRequestMiddlewareReadsOnceAndAccountsConcurrentOrganizations(t *testing.T) {
	limits := DefaultLimits()
	limits.ChatRequestBytes = 4
	limits.EmbeddingsRequestBytes = 4
	limits.NonStreamResponseBytes = 4
	limits.NodeInFlightBytes = 6
	limits.OrgInFlightBytes = 4
	budget := MustNewBudget(limits)

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	handler := RequestMiddleware(budget, ProfileChatRequest, func(context.Context) int64 { return 5 })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := BodyFromContext(r.Context())
		if !ok || string(body) != "1234" {
			t.Errorf("missing envelope: %q %t", body, ok)
		}
		readback, _ := io.ReadAll(r.Body)
		if string(readback) != "1234" {
			t.Errorf("body not replayable: %q", readback)
		}
		once.Do(func() { close(entered) })
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))

	first := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString("1234"))
	firstResult := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(firstResult, first)
		close(done)
	}()
	<-entered

	secondResult := httptest.NewRecorder()
	handler.ServeHTTP(secondResult, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString("12")))
	if secondResult.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected node budget denial, got %d", secondResult.Code)
	}
	close(release)
	<-done
	if firstResult.Code != http.StatusNoContent {
		t.Fatalf("first request failed: %d", firstResult.Code)
	}
}

func TestRequestMiddlewareReturns413ForChunkedOversize(t *testing.T) {
	limits := DefaultLimits()
	limits.ChatRequestBytes = 4
	limits.EmbeddingsRequestBytes = 4
	limits.NonStreamResponseBytes = 4
	budget := MustNewBudget(limits)
	handler := RequestMiddleware(budget, ProfileChatRequest, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("oversize request reached handler")
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString("12345"))
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"validation_error.payload_too_large"`)) {
		t.Fatalf("missing stable error code: %s", rec.Body.String())
	}
}

func TestRequestMiddlewareUsesMessagesProtocolErrorEnvelope(t *testing.T) {
	limits := DefaultLimits()
	limits.ChatRequestBytes = 4
	limits.EmbeddingsRequestBytes = 4
	limits.NonStreamResponseBytes = 4
	limits.NodeInFlightBytes = 8
	limits.OrgInFlightBytes = 8
	budget := MustNewBudget(limits)
	handler := RequestMiddlewareWithMax(budget, 4, func(context.Context) int64 { return 1 })(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("oversize Messages request reached handler")
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString("12345"))
	request.ContentLength = -1
	recorder := httptest.NewRecorder()
	recorder.Header().Set("X-Request-ID", "op_payload_messages")
	handler.ServeHTTP(recorder, request)

	var body struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Error     struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusRequestEntityTooLarge || body.Type != "error" ||
		body.Error.Type != "request_too_large" || body.RequestID != "op_payload_messages" {
		t.Fatalf("status=%d body=%+v raw=%s", recorder.Code, body, recorder.Body.String())
	}
}

func TestReplaceRequestBodyAdjustsLeaseAndLength(t *testing.T) {
	limits := DefaultLimits()
	limits.ChatRequestBytes = 8
	limits.EmbeddingsRequestBytes = 8
	limits.NonStreamResponseBytes = 8
	limits.NodeInFlightBytes = 16
	limits.OrgInFlightBytes = 8
	budget := MustNewBudget(limits)
	handler := RequestMiddleware(budget, ProfileChatRequest, func(context.Context) int64 { return 7 })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rewritten, err := ReplaceRequestBody(r, []byte("123456"))
		if err != nil {
			t.Fatal(err)
		}
		body, ok := BodyFromContext(rewritten.Context())
		if !ok || string(body) != "123456" || rewritten.ContentLength != 6 {
			t.Fatalf("replacement envelope is inconsistent: body=%q length=%d", body, rewritten.ContentLength)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString("1234")))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("replacement failed: %d", rec.Code)
	}
}
