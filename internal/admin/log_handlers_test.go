package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestGetLogDetail_NullSafeUpstreamError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "COALESCE(upstream_error, '')") {
				t.Fatalf("expected upstream_error to be null-safe, got query: %s", sql)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				now := time.Date(2026, 7, 2, 1, 0, 0, 0, time.UTC)
				*dest[0].(*int64) = 42
				*dest[1].(*string) = "batch16-request"
				*dest[2].(*string) = "batch16-smoke-gpt-4o"
				*dest[3].(*string) = "batch16-mock"
				*dest[4].(*string) = "prd2-batch16-openai-compatible-mock"
				*dest[5].(*int) = 10
				*dest[6].(*int) = 5
				*dest[7].(*int) = 15
				*dest[8].(*int) = 1
				*dest[9].(*int) = http.StatusOK
				*dest[10].(*int) = 12
				*dest[11].(*int) = 0
				*dest[12].(**int) = nil
				*dest[13].(*string) = ""
				*dest[14].(*string) = "upstream"
				*dest[15].(*bool) = false
				*dest[16].(*string) = "127.0.0.1"
				*dest[17].(*string) = "company-chat"
				*dest[18].(*time.Time) = now
				return nil
			}}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/logs/7", nil)
	req.SetPathValue("id", "7")
	rec := httptest.NewRecorder()

	h.GetLogDetail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"upstream_error":""`) {
		t.Fatalf("expected empty upstream_error in response, got %s", rec.Body.String())
	}
}

func TestGetRequestTrace_ProjectScopedAccess(t *testing.T) {
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if strings.Contains(sql, "FROM project_members") {
					*dest[0].(*int) = 1
					return nil
				}
				if !strings.Contains(sql, "FROM call_logs") {
					t.Fatalf("unexpected QueryRow SQL: %s", sql)
				}
				if args[0] != int64(31) {
					t.Fatalf("expected log id arg 31, got %#v", args)
				}
				*dest[0].(*int64) = 7
				*dest[1].(*int64) = 1
				*dest[2].(*string) = "req-trace-001"
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "FROM request_trace_events") {
				t.Fatalf("expected trace event query, got %s", sql)
			}
			if !strings.Contains(sql, "COALESCE(project_id, 0) = $3") {
				t.Fatalf("expected project-scoped trace filter, got %s", sql)
			}
			if args[0] != "req-trace-001" || args[1] != int64(1) || args[2] != int64(7) {
				t.Fatalf("unexpected trace query args: %#v", args)
			}
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						traceID := "req-trace-001"
						spanID := "span-001"
						evidenceRef := "unirouter:request_trace_events:request_id:req-trace-001:span:span-001"
						decisionKind := "admission"
						decisionResult := "allowed"
						decisionReason := "gateway.request.accepted"
						actorType := "human"
						actorRef := "user:10"
						sourceEventHash := strings.Repeat("a", 64)
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "gateway.request.accepted"
						*dest[2].(*string) = "gateway"
						*dest[3].(*string) = "info"
						*dest[4].(*string) = "gpt-4o"
						*dest[5].(*string) = "company-chat"
						*dest[6].(*string) = ""
						*dest[7].(*string) = ""
						*dest[8].(**int) = nil
						*dest[9].(*int) = 3
						*dest[10].(*[]byte) = []byte(`{"estimated_tokens":100,"usage_source":"upstream","api_key":"sk-secret","request_body":"{\"messages\":[{\"content\":\"secret prompt\"}]}","access_token":"idp-token"}`)
						*dest[11].(*time.Time) = now
						*dest[12].(**string) = &traceID
						*dest[13].(**string) = &spanID
						*dest[14].(**string) = nil
						*dest[15].(*string) = "unirouter.evidence.v1"
						*dest[16].(*string) = "gateway"
						*dest[17].(*string) = "native"
						*dest[18].(**string) = nil
						*dest[19].(**string) = &evidenceRef
						*dest[20].(**string) = nil
						*dest[21].(**string) = &decisionKind
						*dest[22].(**string) = &decisionResult
						*dest[23].(**string) = &decisionReason
						*dest[24].(**string) = &actorType
						*dest[25].(**string) = &actorRef
						*dest[26].(**string) = nil
						*dest[27].(**string) = nil
						*dest[28].(*string) = "metadata_only"
						*dest[29].(**string) = nil
						*dest[30].(**string) = &sourceEventHash
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/logs/31/trace", nil)
	req.SetPathValue("id", "31")
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleDeveloper)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(10))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetRequestTrace(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		RequestID string                   `json:"request_id"`
		Events    []map[string]interface{} `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.RequestID != "req-trace-001" || len(resp.Events) != 1 {
		t.Fatalf("unexpected trace response: %#v", resp)
	}
	if resp.Events[0]["event_type"] != "gateway.request.accepted" {
		t.Fatalf("expected event_type in response, got %#v", resp.Events[0])
	}
	if resp.Events[0]["trace_id"] != "req-trace-001" || resp.Events[0]["span_id"] != "span-001" {
		t.Fatalf("expected trace/span ids in response, got %#v", resp.Events[0])
	}
	if resp.Events[0]["schema_version"] != "unirouter.evidence.v1" || resp.Events[0]["capture_mode"] != "native" {
		t.Fatalf("expected schema/capture fields in response, got %#v", resp.Events[0])
	}
	if resp.Events[0]["decision_kind"] != "admission" || resp.Events[0]["decision_result"] != "allowed" {
		t.Fatalf("expected decision fields in response, got %#v", resp.Events[0])
	}
	if resp.Events[0]["actor_type"] != "human" || resp.Events[0]["actor_ref"] != "user:10" {
		t.Fatalf("expected actor fields in response, got %#v", resp.Events[0])
	}
	if resp.Events[0]["payload_capture_mode"] != "metadata_only" || resp.Events[0]["source_event_hash"] != strings.Repeat("a", 64) {
		t.Fatalf("expected payload/hash fields in response, got %#v", resp.Events[0])
	}
	metadata, ok := resp.Events[0]["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected metadata object, got %#v", resp.Events[0]["metadata"])
	}
	if metadata["estimated_tokens"] != float64(100) || metadata["usage_source"] != "upstream" {
		t.Fatalf("expected safe trace metadata, got %#v", metadata)
	}
	for _, key := range []string{"api_key", "request_body", "access_token"} {
		if _, ok := metadata[key]; ok {
			t.Fatalf("metadata leaked %s: %#v", key, metadata)
		}
	}
	for _, secret := range []string{"sk-secret", "secret prompt", "idp-token"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("trace response leaked secret %q: %s", secret, rec.Body.String())
		}
	}
}

func TestSanitizeTraceResponseMetadata(t *testing.T) {
	longUsageSource := strings.Repeat("x", 300)

	metadata := sanitizeTraceResponseMetadata(map[string]interface{}{
		"estimated_tokens": float64(100),
		"route_count":      2,
		"is_stream":        true,
		"usage_source":     " " + longUsageSource + " ",
		"api_key":          "sk-secret",
		"authorization":    "Bearer secret",
		"prompt":           "secret prompt",
		"request_body":     map[string]interface{}{"messages": []interface{}{"secret"}},
		"mapped_model":     map[string]interface{}{"nested": "dropped"},
	})

	if metadata["estimated_tokens"] != float64(100) || metadata["route_count"] != 2 || metadata["is_stream"] != true {
		t.Fatalf("expected safe scalar metadata to remain, got %#v", metadata)
	}
	if usageSource, ok := metadata["usage_source"].(string); !ok || len(usageSource) != 256 || strings.Contains(usageSource, " ") {
		t.Fatalf("expected usage_source to be trimmed and truncated, got %#v", metadata["usage_source"])
	}
	for _, key := range []string{"api_key", "authorization", "prompt", "request_body", "mapped_model"} {
		if _, ok := metadata[key]; ok {
			t.Fatalf("metadata leaked or kept unsupported field %s: %#v", key, metadata)
		}
	}
}
