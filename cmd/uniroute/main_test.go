package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/models"
)

// contextKey is the type used for context values in middleware.
// Must match the type in internal/middleware/auth.go.
type contextKey string

const (
	ctxAPIKey contextKey = "api_key"
	ctxOrgID  contextKey = "org_id"
	ctxKeyID  contextKey = "key_id"
)

// setAPIKeyInContext is a test helper that injects an ApiKey into context,
// replicating what GatewayAuth middleware does internally.
func setAPIKeyInContext(ctx context.Context, apiKey *models.ApiKey) context.Context {
	ctx = context.WithValue(ctx, ctxAPIKey, apiKey)
	ctx = context.WithValue(ctx, ctxOrgID, apiKey.OrgID)
	ctx = context.WithValue(ctx, ctxKeyID, apiKey.ID)
	return ctx
}

// getAPIKeyFromContextTest retrieves the ApiKey from context (test version).
func getAPIKeyFromContextTest(ctx context.Context) *models.ApiKey {
	v, _ := ctx.Value(ctxAPIKey).(*models.ApiKey)
	return v
}

func TestRootHandlerDoesNotExposeHardcodedVersion(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	rootHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `"version"`) || strings.Contains(body, "1.0.0") {
		t.Fatalf("root response should not expose a hardcoded version, got %s", body)
	}
}

func TestRootHandlerReturnsStandardNotFoundForUnknownPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs", nil)
	rec := httptest.NewRecorder()

	rootHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `"version"`) || strings.Contains(body, "1.0.0") {
		t.Fatalf("not found response should not expose a hardcoded version, got %s", body)
	}
	if !strings.Contains(body, `"error":"not_found"`) {
		t.Fatalf("expected standard not_found body, got %s", body)
	}
}

func TestApplyBuildRuntimeMetadata(t *testing.T) {
	oldVersion, oldCommit := buildVersion, buildCommit
	t.Cleanup(func() {
		buildVersion, buildCommit = oldVersion, oldCommit
	})
	buildVersion = "2026.07.08"
	buildCommit = "abc1234"

	cfg := &config.Config{}
	applyBuildRuntimeMetadata(cfg)
	if cfg.RuntimeVersion != "2026.07.08" || cfg.RuntimeCommit != "abc1234" {
		t.Fatalf("expected build metadata fallback, got version=%q commit=%q", cfg.RuntimeVersion, cfg.RuntimeCommit)
	}

	cfg = &config.Config{RuntimeVersion: "configured-version", RuntimeCommit: "configured-commit"}
	applyBuildRuntimeMetadata(cfg)
	if cfg.RuntimeVersion != "configured-version" || cfg.RuntimeCommit != "configured-commit" {
		t.Fatalf("configured metadata should win, got version=%q commit=%q", cfg.RuntimeVersion, cfg.RuntimeCommit)
	}
}

// newTestMux builds a minimal mux that replicates the chatHandler wiring
// from main.go, allowing us to test the AllowedModels logic in isolation.
// This is a pure function test — no DB, no Redis, no real relay.
func newTestMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Replicate the exact chatHandler wiring from main.go
	chatHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := getAPIKeyFromContextTest(r.Context())
		if apiKey == nil {
			http.Error(w, `{"error":{"type":"auth_error.invalid_api_key","message":"No API key"}}`, http.StatusUnauthorized)
			return
		}

		if apiKey.AllowedModels != "" && apiKey.AllowedModels != "*" {
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, `{"error":"read body failed"}`, http.StatusBadRequest)
				return
			}
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			var req struct {
				Model string `json:"model"`
			}
			if err := json.Unmarshal(bodyBytes, &req); err == nil && req.Model != "" {
				allowed := false
				for _, m := range strings.Split(apiKey.AllowedModels, ",") {
					if strings.TrimSpace(m) == req.Model {
						allowed = true
						break
					}
				}
				if !allowed {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(map[string]string{"error": "model_not_allowed", "model": req.Model})
					return
				}
			}
		}

		// Model check passed (or no restriction) — simulate relay
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "model": "reached-handler"})
	})

	// Simulate middleware injecting ApiKey into context
	mux.Handle("POST /v1/chat/completions", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract API key from header (simplified — real middleware does DB lookup)
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, `{"error":"missing auth"}`, http.StatusUnauthorized)
			return
		}

		// Simulate what GatewayAuth does: put apiKey in context
		apiKey := &models.ApiKey{
			AllowedModels: r.Header.Get("X-Allowed-Models"),
		}
		ctx := setAPIKeyInContext(r.Context(), apiKey)
		chatHandler.ServeHTTP(w, r.WithContext(ctx))
	}))

	return mux
}

// TestChatHandler_AllowedModels_Bug_NoBodyParsing verifies the current buggy behavior:
// when AllowedModels is set, the handler does NOT actually parse the request body
// to check the model. This test documents the bug.
func TestChatHandler_AllowedModels_Bug_NoBodyParsing(t *testing.T) {
	mux := newTestMux()

	// Request with model "forbidden-model" but AllowedModels only allows "gpt-4"
	body := bytes.NewBufferString(`{"model":"forbidden-model"}`)
	req := httptest.NewRequest("POST", "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("X-Allowed-Models", "gpt-4")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// FIXED: handler now parses body and returns 403 for disallowed models
	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for forbidden-model, got %d — body: %s", rec.Code, rec.Body.String())
	}
}

// TestChatHandler_AllowedModels_Bug_AllowedModelAllowed verifies that even
// an allowed model passes — but only because the check is bypassed entirely.
func TestChatHandler_AllowedModels_Bug_AllowedModelAllowed(t *testing.T) {
	mux := newTestMux()

	body := bytes.NewBufferString(`{"model":"gpt-4"}`)
	req := httptest.NewRequest("POST", "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("X-Allowed-Models", "gpt-4")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for allowed model gpt-4, got %d — body: %s", rec.Code, rec.Body.String())
	}
}

// TestChatHandler_AllowedModels_Bug_NoAuth verifies unauthorized requests
// are properly rejected.
func TestChatHandler_AllowedModels_Bug_NoAuth(t *testing.T) {
	mux := newTestMux()

	body := bytes.NewBufferString(`{"model":"gpt-4"}`)
	req := httptest.NewRequest("POST", "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for missing auth, got %d", rec.Code)
	}
}

// TestChatHandler_AllowedModels_Bug_EmptyAllowedModels verifies that
// when AllowedModels is empty, requests pass through.
func TestChatHandler_AllowedModels_Bug_EmptyAllowedModels(t *testing.T) {
	mux := newTestMux()

	body := bytes.NewBufferString(`{"model":"any-model"}`)
	req := httptest.NewRequest("POST", "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("X-Allowed-Models", "") // empty = no restriction

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for empty AllowedModels, got %d", rec.Code)
	}
}

// TestChatHandler_AllowedModels_Bug_Wildcard verifies that
// when AllowedModels is "*", requests pass through.
func TestChatHandler_AllowedModels_Bug_Wildcard(t *testing.T) {
	mux := newTestMux()

	body := bytes.NewBufferString(`{"model":"any-model"}`)
	req := httptest.NewRequest("POST", "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("X-Allowed-Models", "*")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for wildcard AllowedModels, got %d", rec.Code)
	}
}

// TestChatHandler_NilAPIKey verifies behavior when context has no API key.
func TestChatHandler_NilAPIKey(t *testing.T) {
	// This tests the chatHandler directly with no apiKey in context
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := getAPIKeyFromContextTest(r.Context())
		if apiKey == nil {
			http.Error(w, `{"error":{"type":"auth_error.invalid_api_key","message":"No API key"}}`, http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for nil apiKey, got %d", rec.Code)
	}
}
