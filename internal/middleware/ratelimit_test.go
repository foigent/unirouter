package middleware

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/models"
)

// --- GatewayAuth context injection boundary tests ---

func TestGatewayAuth_RequestIDFromHeader(t *testing.T) {
	// X-Request-ID is retained only as client correlation; it cannot become the
	// authoritative server operation ID.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Request-ID", "custom-req-id")
	rec := httptest.NewRecorder()
	var identity RequestIdentity
	OperationIdentity(http.HandlerFunc(func(_ http.ResponseWriter, identified *http.Request) {
		identity = GetRequestIdentityFromContext(identified.Context())
	})).ServeHTTP(rec, r)
	if identity.OperationID == "custom-req-id" || identity.ClientRequestID != "custom-req-id" {
		t.Fatalf("identity fields were not separated: %#v", identity)
	}
}

func TestGatewayAuth_RequestIDGenerated(t *testing.T) {
	// When X-Request-ID is empty, a secure server operation ID is generated.
	id := generateRequestID()
	if id == "" {
		t.Fatal("generated request ID should not be empty")
	}
	if len(id) != 35 || !strings.HasPrefix(id, "op_") {
		t.Fatalf("expected op_ plus 32 hex characters, got %q", id)
	}
}

func TestGetOrgIDFromContext_NonInt64(t *testing.T) {
	// When context value is not int64, should return 0
	ctx := context.WithValue(context.Background(), CtxOrgID, "not-an-int64")
	if GetOrgIDFromContext(ctx) != 0 {
		t.Fatal("should return 0 for non-int64 value")
	}
}

func TestGetKeyIDFromContext_Missing(t *testing.T) {
	if GetKeyIDFromContext(context.Background()) != 0 {
		t.Fatal("empty context should return 0")
	}
}

func TestGetAPIKeyFromContext_NonPointer(t *testing.T) {
	// When context value is not *ApiKey, should return nil
	ctx := context.WithValue(context.Background(), CtxAPIKey, "not-an-apikey")
	if GetAPIKeyFromContext(ctx) != nil {
		t.Fatal("should return nil for non-pointer value")
	}
}

func TestGetRequestIDFromContext_NonString(t *testing.T) {
	ctx := context.WithValue(context.Background(), CtxRequestID, 12345)
	if GetRequestIDFromContext(ctx) != "" {
		t.Fatal("should return empty for non-string value")
	}
}

// --- validateKeyAndOrgStatus additional boundary tests ---

func TestValidateKeyAndOrgStatus_BothInactive(t *testing.T) {
	// When both key and org are inactive, key error should be returned (checked first)
	apiKey := &models.ApiKey{Status: "disabled"}
	err := validateKeyAndOrgStatus(apiKey, "suspended")
	if err == nil {
		t.Fatal("expected error")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthError, got %T", err)
	}
	// Key status is checked first, so should be 401 not 403
	if authErr.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401 (key checked first), got %d", authErr.Status)
	}
}

func TestValidateKeyAndOrgStatus_EmptyStatus(t *testing.T) {
	apiKey := &models.ApiKey{Status: ""}
	err := validateKeyAndOrgStatus(apiKey, "active")
	if err == nil {
		t.Fatal("expected error for empty key status")
	}
}

func TestValidateKeyAndOrgStatus_EmptyOrgStatus(t *testing.T) {
	apiKey := &models.ApiKey{Status: "active"}
	err := validateKeyAndOrgStatus(apiKey, "")
	if err == nil {
		t.Fatal("expected error for empty org status")
	}
}

// --- RateLimit tests with miniredis ---

func setupMiniredis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.NewMiniRedis()
	if err := mr.Start(); err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return mr, rdb
}

func TestRateLimit_NoAPIKey_Passthrough(t *testing.T) {
	_, rdb := setupMiniredis(t)
	handler := RateLimit(rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No API key in context
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when no API key, got %d", rec.Code)
	}
}

func TestRateLimit_UnderLimit_Passthrough(t *testing.T) {
	mr, rdb := setupMiniredis(t)
	_ = mr

	apiKey := &models.ApiKey{ID: 1, RPMLimit: 10, QPSLimit: 5}
	handler := RateLimit(rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), CtxAPIKey, apiKey)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 under limit, got %d", rec.Code)
	}
}

func TestRateLimit_QPSExceeded(t *testing.T) {
	_, rdb := setupMiniredis(t)

	apiKey := &models.ApiKey{ID: 2, RPMLimit: 1000, QPSLimit: 2}
	handler := RateLimit(rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := context.WithValue(req.Context(), CtxAPIKey, apiKey)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if i <= apiKey.QPSLimit && rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
		if i == apiKey.QPSLimit+1 {
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("request %d: expected 429, got %d", i, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "QPS limit exceeded") {
				t.Fatalf("request %d: unexpected response body %q", i, rec.Body.String())
			}
		}
	}
}

func TestRateLimit_RPMExceeded(t *testing.T) {
	_, rdb := setupMiniredis(t)

	apiKey := &models.ApiKey{ID: 3, RPMLimit: 3, QPSLimit: 1000}
	handler := RateLimit(rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 1; i <= 4; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := context.WithValue(req.Context(), CtxAPIKey, apiKey)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if i <= apiKey.RPMLimit && rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
		if i == apiKey.RPMLimit+1 {
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("request %d: expected 429, got %d", i, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "RPM limit exceeded") {
				t.Fatalf("request %d: unexpected response body %q", i, rec.Body.String())
			}
		}
	}
}

func TestRateLimit_ConcurrentRequestsDoNotExceedLimit(t *testing.T) {
	_, rdb := setupMiniredis(t)

	const (
		requestCount = 40
		qpsLimit     = 10
	)
	apiKey := &models.ApiKey{ID: 7, RPMLimit: requestCount, QPSLimit: qpsLimit}
	handler := RateLimit(rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	start := make(chan struct{})
	var wg sync.WaitGroup
	var allowed atomic.Int64
	var rejected atomic.Int64
	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req = req.WithContext(context.WithValue(req.Context(), CtxAPIKey, apiKey))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			switch rec.Code {
			case http.StatusOK:
				allowed.Add(1)
			case http.StatusTooManyRequests:
				rejected.Add(1)
			default:
				t.Errorf("unexpected status %d", rec.Code)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := allowed.Load(); got != qpsLimit {
		t.Fatalf("expected exactly %d allowed requests, got %d", qpsLimit, got)
	}
	if got := rejected.Load(); got != requestCount-qpsLimit {
		t.Fatalf("expected exactly %d rejected requests, got %d", requestCount-qpsLimit, got)
	}
}

func TestRateLimit_RedisError_FailOpen(t *testing.T) {
	// Create a client pointing to a dead server
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1"})
	defer rdb.Close()

	apiKey := &models.ApiKey{ID: 4, RPMLimit: 10, QPSLimit: 5}
	handler := RateLimit(rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), CtxAPIKey, apiKey)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Fail-open: should pass through on Redis error
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (fail-open), got %d", rec.Code)
	}
}

func TestRateLimit_UsesRequestContext(t *testing.T) {
	_, rdb := setupMiniredis(t)

	apiKey := &models.ApiKey{ID: 8, RPMLimit: 10, QPSLimit: 5}
	handler := RateLimit(rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx, cancel := context.WithCancel(context.WithValue(req.Context(), CtxAPIKey, apiKey))
	cancel()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected canceled Redis operation to fail open, got %d", rec.Code)
	}
	if count, err := rdb.Exists(context.Background(), "rl:qps:8", "rl:rpm:8").Result(); err != nil {
		t.Fatalf("check rate-limit keys: %v", err)
	} else if count != 0 {
		t.Fatalf("expected canceled request not to mutate Redis, found %d keys", count)
	}
}

func TestRateLimit_DefaultLimits(t *testing.T) {
	mr, rdb := setupMiniredis(t)

	// RPMLimit and QPSLimit are 0 → should use defaults (60 RPM, 10 QPS)
	apiKey := &models.ApiKey{ID: 5, RPMLimit: 0, QPSLimit: 0}
	handler := RateLimit(rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Should pass (well under default limits)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), CtxAPIKey, apiKey)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with default limits, got %d", rec.Code)
	}
	_ = mr
}

func TestRateLimit_NegativeLimits_UseDefaults(t *testing.T) {
	mr, rdb := setupMiniredis(t)

	apiKey := &models.ApiKey{ID: 6, RPMLimit: -1, QPSLimit: -1}
	handler := RateLimit(rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), CtxAPIKey, apiKey)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with negative limits (defaults), got %d", rec.Code)
	}
	_ = mr
}

func TestRateLimit_DifferentKeys_Independent(t *testing.T) {
	mr, rdb := setupMiniredis(t)

	apiKey1 := &models.ApiKey{ID: 10, RPMLimit: 1000, QPSLimit: 1}
	apiKey2 := &models.ApiKey{ID: 11, RPMLimit: 1000, QPSLimit: 100}
	handler := RateLimit(rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Both keys should pass independently.
	for _, key := range []*models.ApiKey{apiKey1, apiKey2} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := context.WithValue(req.Context(), CtxAPIKey, key)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("key %d: expected 200, got %d", key.ID, rec.Code)
		}
	}
	_ = mr
}

func TestRateLimit_WindowReset(t *testing.T) {
	mr, rdb := setupMiniredis(t)

	apiKey := &models.ApiKey{ID: 20, RPMLimit: 1000, QPSLimit: 1}
	handler := RateLimit(rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Verify that Redis time advances with the window and the old request expires.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), CtxAPIKey, apiKey)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	mr.FastForward(2 * time.Second)

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req.WithContext(ctx))
	if rec2.Code != http.StatusOK {
		t.Fatalf("after fast forward: expected 200, got %d", rec2.Code)
	}
}

func TestLoginRateLimit_UserLimitExceeded(t *testing.T) {
	_, rdb := setupMiniredis(t)
	handler := LoginRateLimit(rdb, LoginRateLimitOptions{
		Enabled:      true,
		Window:       time.Minute,
		IPRequests:   10,
		UserRequests: 1,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !strings.Contains(string(body), "Admin@Example.com") {
			t.Fatalf("body was not restored for login handler: %s", string(body))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"Admin@Example.com","password":"x"}`))
	req1.RemoteAddr = "203.0.113.10:4567"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first login should pass, got %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin@example.com","password":"x"}`))
	req2.RemoteAddr = "203.0.113.10:4567"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second login should be rate limited, got %d", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestLoginRateLimit_RestoresOversizedBody(t *testing.T) {
	_, rdb := setupMiniredis(t)
	originalBody := `{"username":"` + strings.Repeat("a", 32) + `","password":"x"}`
	handler := LoginRateLimit(rdb, LoginRateLimitOptions{
		Enabled:      true,
		Window:       time.Minute,
		IPRequests:   0,
		UserRequests: 10,
		MaxBodyBytes: 8,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != originalBody {
			t.Fatalf("body was not fully restored after limiter read: %q", string(body))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(originalBody))
	req.RemoteAddr = "203.0.113.12:4567"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("oversized login body should pass through to login handler, got %d", rec.Code)
	}
}

func TestLoginRateLimit_IPLimitExceededAcrossUsers(t *testing.T) {
	_, rdb := setupMiniredis(t)
	handler := LoginRateLimit(rdb, LoginRateLimitOptions{
		Enabled:      true,
		Window:       time.Minute,
		IPRequests:   1,
		UserRequests: 10,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i, username := range []string{"alice", "bob"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"`+username+`","password":"x"}`))
		req.RemoteAddr = "203.0.113.11:4567"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if i == 0 && rec.Code != http.StatusOK {
			t.Fatalf("first login should pass, got %d", rec.Code)
		}
		if i == 1 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("second login from same IP should be rate limited, got %d", rec.Code)
		}
	}
}

func TestLoginRateLimit_DisabledPassesThrough(t *testing.T) {
	_, rdb := setupMiniredis(t)
	handler := LoginRateLimit(rdb, LoginRateLimitOptions{
		Enabled:      false,
		Window:       time.Minute,
		IPRequests:   0,
		UserRequests: 0,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"x"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disabled login limiter should pass through, got %d", rec.Code)
	}
}

func TestLoginRateLimit_RedisErrorFailOpen(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1"})
	defer rdb.Close()
	handler := LoginRateLimit(rdb, LoginRateLimitOptions{
		Enabled:      true,
		Window:       time.Minute,
		IPRequests:   1,
		UserRequests: 1,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"x"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login limiter should fail open on Redis errors, got %d", rec.Code)
	}
}

// --- CORS boundary tests ---

func TestCORS_Preflight_NoContent(t *testing.T) {
	handler := CORSWithOptions(CORSOptions{AllowedOrigins: []string{"https://console.example.com"}})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://console.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://console.example.com" {
		t.Fatal("CORS headers should be set on preflight")
	}
}

func TestCORS_NonOptions_PassesThrough(t *testing.T) {
	handler := CORSWithOptions(CORSOptions{AllowedOrigins: []string{"https://console.example.com"}})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://console.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://console.example.com" {
		t.Fatal("CORS headers should be set on normal requests")
	}
}

func TestCORS_DisallowedPreflightForbidden(t *testing.T) {
	handler := CORSWithOptions(CORSOptions{AllowedOrigins: []string{"https://console.example.com"}})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for a disallowed preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disallowed preflight, got %d", rec.Code)
	}
}

func TestMetricsCIDRAllowlist(t *testing.T) {
	handler := MetricsCIDRAllowlist([]string{"203.0.113.0/24"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	allowedReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	allowedReq.RemoteAddr = "203.0.113.10:12345"
	allowedRec := httptest.NewRecorder()
	handler.ServeHTTP(allowedRec, allowedReq)
	if allowedRec.Code != http.StatusOK {
		t.Fatalf("expected allowed metrics request, got %d", allowedRec.Code)
	}

	blockedReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	blockedReq.RemoteAddr = "198.51.100.10:12345"
	blockedRec := httptest.NewRecorder()
	handler.ServeHTTP(blockedRec, blockedReq)
	if blockedRec.Code != http.StatusNotFound {
		t.Fatalf("expected hidden metrics request to return 404, got %d", blockedRec.Code)
	}
}
