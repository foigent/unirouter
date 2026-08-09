package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const idempotencyTestSecret = "p6f17-idempotency-test-secret"

type memoryIdempotencyStore struct {
	mu      sync.Mutex
	records map[string]IdempotencyRecord
	claims  []IdempotencyClaimInput
}

type failingIdempotencyResponseWriter struct {
	header http.Header
}

func (w *failingIdempotencyResponseWriter) Header() http.Header {
	return w.header
}

func (*failingIdempotencyResponseWriter) WriteHeader(int) {}

func (*failingIdempotencyResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("downstream write failed")
}

func newMemoryIdempotencyStore() *memoryIdempotencyStore {
	return &memoryIdempotencyStore{records: map[string]IdempotencyRecord{}}
}

func (s *memoryIdempotencyStore) Claim(_ context.Context, in IdempotencyClaimInput) (IdempotencyClaimResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claims = append(s.claims, in)
	subjectType, subjectID := idempotencySubject(in.APIKeyID, in.AccessCredentialID)
	key := fmt.Sprintf("%d/%s/%d/%s", in.OrgID, subjectType, subjectID, in.KeyHash)
	if existing, ok := s.records[key]; ok && existing.ExpiresAt.After(in.CreatedAt) {
		return IdempotencyClaimResult{Record: existing}, nil
	}
	record := IdempotencyRecord{
		OrgID:              in.OrgID,
		APIKeyID:           in.APIKeyID,
		AccessCredentialID: in.AccessCredentialID,
		SubjectType:        subjectType,
		SubjectID:          subjectID,
		KeyHash:            in.KeyHash,
		RequestFingerprint: in.RequestFingerprint,
		OperationID:        in.OperationID,
		ClientRequestID:    in.ClientRequestID,
		State:              IdempotencyStateInProgress,
		CreatedAt:          in.CreatedAt,
		UpdatedAt:          in.CreatedAt,
		ExpiresAt:          in.ExpiresAt,
	}
	s.records[key] = record
	return IdempotencyClaimResult{Claimed: true, Record: record}, nil
}

func (s *memoryIdempotencyStore) Complete(_ context.Context, orgID, apiKeyID, accessCredentialID int64, keyHash, operationID, state string, httpStatus int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	subjectType, subjectID := idempotencySubject(apiKeyID, accessCredentialID)
	key := fmt.Sprintf("%d/%s/%d/%s", orgID, subjectType, subjectID, keyHash)
	record, ok := s.records[key]
	if !ok || record.OperationID != operationID || record.State != IdempotencyStateInProgress {
		return fmt.Errorf("claim ownership lost")
	}
	record.State = state
	record.OriginalHTTPStatus = &httpStatus
	record.UpdatedAt = time.Now().UTC()
	s.records[key] = record
	return nil
}

func scopedIdempotencyHandler(store IdempotencyStore, next http.Handler) http.Handler {
	guarded := IdempotencyGuard(store, idempotencyTestSecret, next)
	scoped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := strconv.ParseInt(r.Header.Get("X-Test-Org"), 10, 64)
		if orgID == 0 {
			orgID = 1
		}
		keyID, _ := strconv.ParseInt(r.Header.Get("X-Test-Key"), 10, 64)
		if keyID == 0 {
			keyID = 11
		}
		ctx := context.WithValue(r.Context(), CtxOrgID, orgID)
		ctx = context.WithValue(ctx, CtxKeyID, keyID)
		credentialID, _ := strconv.ParseInt(r.Header.Get("X-Test-Credential"), 10, 64)
		if credentialID > 0 {
			ctx = context.WithValue(ctx, CtxAccessCredentialID, credentialID)
		}
		guarded.ServeHTTP(w, r.WithContext(ctx))
	})
	return OperationIdentityWithSecret(idempotencyTestSecret)(scoped)
}

func newIdempotentRequest(body, key string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(IdempotencyKeyHeader, key)
	return req
}

func TestIdempotencyGuardCompletedReplayReturnsReceiptWithoutSecondDispatch(t *testing.T) {
	store := newMemoryIdempotencyStore()
	dispatches := 0
	handler := scopedIdempotencyHandler(store, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dispatches++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	first := httptest.NewRecorder()
	firstReq := newIdempotentRequest(`{"model":"gpt-4o","metadata":{"b":2,"a":1}}`, "same-business-intent")
	firstReq.Header.Set(OperationIDHeader, "client-correlation")
	handler.ServeHTTP(first, firstReq)
	if first.Code != http.StatusOK {
		t.Fatalf("first request failed: %d %s", first.Code, first.Body.String())
	}
	originalOperation := first.Header().Get(OperationIDHeader)
	if !strings.HasPrefix(originalOperation, "op_") {
		t.Fatalf("expected server operation identity, got %q", originalOperation)
	}

	second := httptest.NewRecorder()
	secondReq := newIdempotentRequest(`{"metadata":{"a":1,"b":2},"model":"gpt-4o"}`, "same-business-intent")
	handler.ServeHTTP(second, secondReq)
	if second.Code != http.StatusConflict {
		t.Fatalf("expected completed receipt conflict, got %d: %s", second.Code, second.Body.String())
	}
	if dispatches != 1 {
		t.Fatalf("expected one dispatch, got %d", dispatches)
	}
	if got := second.Header().Get(OriginalOperationHeader); got != originalOperation {
		t.Fatalf("expected original operation %q, got %q", originalOperation, got)
	}
	if !strings.Contains(second.Body.String(), `"code":"idempotency_error.already_completed"`) ||
		strings.Contains(second.Body.String(), `"ok":true`) {
		t.Fatalf("expected metadata-only receipt without replayed response: %s", second.Body.String())
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.claims) != 2 || store.claims[0].KeyHash == "same-business-intent" || len(store.claims[0].KeyHash) != 64 {
		t.Fatalf("raw idempotency key reached storage: %#v", store.claims)
	}
	if store.claims[0].ClientRequestID != "client-correlation" {
		t.Fatalf("client correlation was not retained separately: %#v", store.claims[0])
	}
}

func TestP6F41BIdempotencyMarksDownstreamWriteFailureAsFailed(t *testing.T) {
	store := newMemoryIdempotencyStore()
	handler := scopedIdempotencyHandler(store, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	first := &failingIdempotencyResponseWriter{header: make(http.Header)}
	handler.ServeHTTP(first, newIdempotentRequest(`{"model":"gpt-4o"}`, "delivery-write-failure"))

	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, newIdempotentRequest(`{"model":"gpt-4o"}`, "delivery-write-failure"))
	if replay.Code != http.StatusConflict || !strings.Contains(replay.Body.String(), `"code":"idempotency_error.original_failed"`) {
		t.Fatalf("writer failure was not retained as a failed idempotency receipt: %d %s", replay.Code, replay.Body.String())
	}
}

func TestP6F11InterruptedStreamReplayReturnsBoundedDeliveryReceipt(t *testing.T) {
	status := http.StatusOK
	updatedAt := time.Date(2026, 7, 13, 8, 30, 0, 0, time.UTC)
	recorder := httptest.NewRecorder()
	writeIdempotencyConflict(recorder, "same-fingerprint", IdempotencyRecord{
		RequestFingerprint: "same-fingerprint",
		OperationID:        "op_p6f11_interrupted_replay",
		State:              IdempotencyStateFailed,
		OriginalHTTPStatus: &status,
		DeliveryMode:       "stream",
		DeliveryOutcome:    "interrupted",
		DeliveredBytes:     512,
		DeliveredChunks:    3,
		DeliveryUpdatedAt:  &updatedAt,
		CreatedAt:          updatedAt.Add(-time.Minute),
	})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected interrupted replay conflict, got %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`"code":"idempotency_error.original_failed"`,
		`"original_request_id":"op_p6f11_interrupted_replay"`,
		`"original_http_status":200`,
		`"mode":"stream"`,
		`"outcome":"interrupted"`,
		`"delivered_bytes":512`,
		`"delivered_chunks":3`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("interrupted replay omitted %s: %s", expected, body)
		}
	}
}

func TestIdempotencyGuardChangedPayloadReturnsMismatch(t *testing.T) {
	store := newMemoryIdempotencyStore()
	dispatches := 0
	handler := scopedIdempotencyHandler(store, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dispatches++
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), newIdempotentRequest(`{"model":"gpt-4o","max_tokens":10}`, "reused-key"))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, newIdempotentRequest(`{"model":"gpt-4o","max_tokens":20}`, "reused-key"))

	if second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), "key_reused_with_different_request") {
		t.Fatalf("expected changed-payload 409, got %d: %s", second.Code, second.Body.String())
	}
	if dispatches != 1 {
		t.Fatalf("changed payload dispatched unexpectedly: %d", dispatches)
	}
}

func TestIdempotencyGuardInProgressRequestDoesNotDispatchTwice(t *testing.T) {
	store := newMemoryIdempotencyStore()
	started := make(chan struct{})
	release := make(chan struct{})
	dispatches := 0
	var mu sync.Mutex
	handler := scopedIdempotencyHandler(store, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		dispatches++
		mu.Unlock()
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, newIdempotentRequest(`{"model":"gpt-4o"}`, "concurrent-key"))
		firstDone <- rec
	}()
	<-started

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, newIdempotentRequest(`{"model":"gpt-4o"}`, "concurrent-key"))
	if second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), "request_in_progress") {
		t.Fatalf("expected in-progress receipt, got %d: %s", second.Code, second.Body.String())
	}
	if second.Header().Get("Retry-After") != "1" {
		t.Fatalf("expected bounded retry hint, got %q", second.Header().Get("Retry-After"))
	}
	close(release)
	if first := <-firstDone; first.Code != http.StatusOK {
		t.Fatalf("first request failed: %d %s", first.Code, first.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if dispatches != 1 {
		t.Fatalf("expected one dispatch, got %d", dispatches)
	}
}

func TestIdempotencyGuardScopesTheSameKeyByTenant(t *testing.T) {
	store := newMemoryIdempotencyStore()
	dispatches := 0
	handler := scopedIdempotencyHandler(store, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dispatches++
		w.WriteHeader(http.StatusOK)
	}))

	for _, orgID := range []string{"1", "2"} {
		req := newIdempotentRequest(`{"model":"gpt-4o"}`, "shared-external-key")
		req.Header.Set("X-Test-Org", orgID)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("org %s was incorrectly deduplicated: %d %s", orgID, rec.Code, rec.Body.String())
		}
	}
	if dispatches != 2 {
		t.Fatalf("expected one dispatch per tenant, got %d", dispatches)
	}
}

func TestIdempotencyGuardPreservesFlusherAndRecordsFailedReceipt(t *testing.T) {
	store := newMemoryIdempotencyStore()
	handler := scopedIdempotencyHandler(store, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Fatal("idempotency wrapper removed streaming flusher support")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, newIdempotentRequest(`{"model":"gpt-4o"}`, "failed-key"))
	if first.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected original 503, got %d", first.Code)
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, newIdempotentRequest(`{"model":"gpt-4o"}`, "failed-key"))
	if second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), "original_failed") {
		t.Fatalf("expected failed-operation receipt, got %d: %s", second.Code, second.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(second.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid receipt JSON: %v", err)
	}
}

func TestIdempotencyGuardWithoutKeyDoesNotRequireStore(t *testing.T) {
	called := false
	handler := OperationIdentity(IdempotencyGuard(nil, "", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if rec.Code != http.StatusNoContent || !called {
		t.Fatalf("non-idempotent request changed: %d %s", rec.Code, rec.Body.String())
	}
}
