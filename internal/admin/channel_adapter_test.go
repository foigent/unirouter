package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
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
