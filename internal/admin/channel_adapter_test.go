package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCreateChannel_DefaultsAdapterType(t *testing.T) {
	var capturedAdapterType string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "adapter_type") {
				t.Fatalf("expected insert to include adapter_type, got %s", sql)
			}
			capturedAdapterType = args[4].(string)
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 42
				return nil
			}}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPost, "/channels", strings.NewReader(`{"name":"test","provider":"openai","base_url":"https://api.openai.com","models":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if capturedAdapterType != "openai_compatible" {
		t.Fatalf("expected default adapter openai_compatible, got %q", capturedAdapterType)
	}
}

func TestCreateChannel_InvalidAdapterType(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPost, "/channels", strings.NewReader(`{"name":"test","provider":"openai","base_url":"https://api.openai.com","models":"gpt-4o","adapter_type":"unknown_native"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateChannel_AcceptsAnthropicNativeAdapterType(t *testing.T) {
	var capturedAdapterType string
	var capturedAdapterConfig string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			capturedAdapterType = args[4].(string)
			capturedAdapterConfig = args[5].(string)
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 42
				return nil
			}}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPost, "/channels", strings.NewReader(`{"name":"anthropic","provider":"anthropic","base_url":"https://api.anthropic.com","models":"company-chat","adapter_type":"anthropic_native","adapter_config":{"anthropic_version":"2023-06-01"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if capturedAdapterType != "anthropic_native" {
		t.Fatalf("expected anthropic_native, got %q", capturedAdapterType)
	}
	if !strings.Contains(capturedAdapterConfig, "anthropic_version") {
		t.Fatalf("expected adapter_config to be persisted, got %q", capturedAdapterConfig)
	}
}

func TestCreateChannelAcceptsStrictAzureOpenAIConfig(t *testing.T) {
	var capturedAdapterType string
	var capturedAdapterConfig string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			capturedAdapterType = args[4].(string)
			capturedAdapterConfig = args[5].(string)
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 43
				return nil
			}}
		},
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(
		http.MethodPost,
		"/channels",
		strings.NewReader(`{"name":"azure","provider":"azure","base_url":"https://contoso.openai.azure.com","models":"company-chat","adapter_type":"azure_openai","adapter_config":{"api_version":"2024-10-21","deployment":"gpt-4o-prod"}}`),
	)
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if capturedAdapterType != "azure_openai" || !strings.Contains(capturedAdapterConfig, `"deployment":"gpt-4o-prod"`) {
		t.Fatalf("adapter/config=%q/%q", capturedAdapterType, capturedAdapterConfig)
	}
}

func TestCreateChannelRejectsAmbiguousAzureOpenAIConfig(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	req := httptest.NewRequest(
		http.MethodPost,
		"/channels",
		strings.NewReader(`{"name":"azure","provider":"azure","base_url":"https://contoso.openai.azure.com","models":"company-chat","adapter_type":"azure_openai","adapter_config":{"api_version":"2024-10-21-preview"}}`),
	)
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Invalid adapter_config") {
		t.Fatalf("expected strict config 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateChannel_InvalidAdapterType(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPut, "/channels/1", strings.NewReader(`{"adapter_type":"unknown_native"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateChannel(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateChannelValidatesConfigAgainstPersistedAdapterType(t *testing.T) {
	execCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "COALESCE(adapter_config") {
				t.Fatalf("unexpected query before validation: %s", sql)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*string) = "azure_openai"
				*dest[1].(*json.RawMessage) = json.RawMessage(`{"api_version":"2024-10-21","deployment":"existing"}`)
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execCalled = true
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(
		http.MethodPut,
		"/channels/1",
		strings.NewReader(`{"adapter_config":{"api_version":"2024-10-21-preview","deployment":"existing"}}`),
	)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateChannel(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Invalid adapter_config") {
		t.Fatalf("expected strict config 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if execCalled {
		t.Fatal("invalid adapter config must fail before persistence")
	}
}
