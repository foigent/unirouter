package admin

import (
	"context"
	stdsql "database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func dataExchangeTestRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, "platform_admin")
	return req.WithContext(ctx)
}

func TestCreateDataExchangeItem_RequiresRequestIDForExactApproval(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	req := dataExchangeTestRequest(http.MethodPost, "/data-exchange/items", `{
		"approval_mode":"exact_request",
		"scenario_label":"model denied",
		"source_instance":"dev-a",
		"data_classes":["request_trace_events"]
	}`)
	rec := httptest.NewRecorder()

	h.CreateDataExchangeItem(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "request_id") {
		t.Fatalf("expected request_id validation error, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateDataExchangeItem_AcceptsControlledScenarioWithoutRequestID(t *testing.T) {
	var insertArgs []any
	var auditAction string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			insertArgs = append([]any(nil), args...)
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 41
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				auditAction = args[2].(string)
			}
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)
	req := dataExchangeTestRequest(http.MethodPost, "/data-exchange/items", `{
		"approval_mode":"controlled_scenario",
		"scenario_label":"provider failure",
		"source_instance":"dev-a",
		"data_classes":["request_trace_events","capture_gaps"],
		"authorized_operator":"ops-a"
	}`)
	rec := httptest.NewRecorder()

	h.CreateDataExchangeItem(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d %s", rec.Code, rec.Body.String())
	}
	if len(insertArgs) < 3 || insertArgs[1] != "controlled_scenario" || insertArgs[2] != "" {
		t.Fatalf("expected controlled-scenario insert without request ID, got %#v", insertArgs)
	}
	if auditAction != "data_exchange.item.create" {
		t.Fatalf("expected create audit, got %q", auditAction)
	}
}

func TestCreateDataExchangeReceipt_RejectsMismatchedExchangeID(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*string) = "uxchg_expected_001"
				*dest[1].(*string) = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				*dest[2].(*string) = "dev-a"
				*dest[3].(*string) = "unirouter.prd5.xinsight.snapshot.v1"
				*dest[4].(*string) = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
				return nil
			}}
		},
	}
	h := newTestHandler(db)
	req := dataExchangeTestRequest(http.MethodPost, "/data-exchange/exchanges/9/receipts", `{
		"exchange_id":"uxchg_other_001",
		"schema_version":"xinsight.import-receipt.v1",
		"source_system":"unirouter",
		"source_instance":"dev-a",
		"artifact_schema_version":"unirouter.prd5.xinsight.snapshot.v1",
		"artifact_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"result_status":"persisted",
		"checked_at":"2026-07-10T08:00:00Z"
	}`)
	req.SetPathValue("id", "9")
	rec := httptest.NewRecorder()

	h.CreateDataExchangeReceipt(rec, req)

	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "exchange_id") {
		t.Fatalf("expected exchange mismatch conflict, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateDataExchangeReceipt_RejectsUnsupportedRuntimeFields(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	for _, tc := range []struct {
		name  string
		field string
	}{
		{name: "import job id", field: `"import_job_id":"job-123"`},
		{name: "deep link", field: `"deep_link":"https://x-insight.example/jobs/job-123"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := dataExchangeTestRequest(http.MethodPost, "/data-exchange/exchanges/9/receipts", `{
				"exchange_id":"uxchg_expected_001",
				"schema_version":"xinsight.import-receipt.v1",
				"source_system":"unirouter",
				"source_instance":"dev-a",
				"artifact_schema_version":"unirouter.prd5.xinsight.snapshot.v1",
				"artifact_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"result_status":"persisted",
				"checked_at":"2026-07-10T08:00:00Z",
				`+tc.field+`
			}`)
			req.SetPathValue("id", "9")
			rec := httptest.NewRecorder()

			h.CreateDataExchangeReceipt(rec, req)

			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Invalid JSON") {
				t.Fatalf("expected unsupported field rejection, got %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateDataExchangeReceipt_RejectsUnknownOrDuplicateWarningCodes(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	for _, warnings := range []string{
		`["future_warning"]`,
		`["source_capture_gaps_present","source_capture_gaps_present"]`,
		`[" source_capture_gaps_present"]`,
	} {
		t.Run(warnings, func(t *testing.T) {
			req := dataExchangeTestRequest(http.MethodPost, "/data-exchange/exchanges/9/receipts", `{
				"exchange_id":"uxchg_expected_001",
				"schema_version":"xinsight.import-receipt.v1",
				"source_system":"unirouter",
				"source_instance":"dev-a",
				"artifact_schema_version":"unirouter.prd5.xinsight.snapshot.v1",
				"artifact_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"result_status":"persisted",
				"checked_at":"2026-07-10T08:00:00Z",
				"warning_codes":`+warnings+`
			}`)
			req.SetPathValue("id", "9")
			rec := httptest.NewRecorder()

			h.CreateDataExchangeReceipt(rec, req)

			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Invalid warning code") {
				t.Fatalf("expected warning registry rejection, got %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUpdateDataExchangeItem_RejectsUnknownExactRequestID(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "FROM integration_exchange_items") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*string) = "pending"
					*dest[1].(*string) = "exact_request"
					requestID := dest[2].(*stdsql.NullString)
					requestID.String = "req-missing"
					requestID.Valid = true
					return nil
				}}
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
	}
	h := newTestHandler(db)
	req := dataExchangeTestRequest(http.MethodPut, "/data-exchange/items/7", `{"approval_status":"approved","reason":"approved metadata only"}`)
	req.SetPathValue("id", "7")
	rec := httptest.NewRecorder()

	h.UpdateDataExchangeItem(rec, req)

	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "does not exist") {
		t.Fatalf("expected unknown request conflict, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateDataExchangeReceipt_DerivesOutdatedFromArtifactHash(t *testing.T) {
	queryCount := 0
	var auditStatus string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			queryCount++
			if strings.Contains(sql, "SELECT exchange_id") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*string) = "uxchg_expected_001"
					*dest[1].(*string) = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
					*dest[2].(*string) = "dev-a"
					*dest[3].(*string) = "unirouter.prd5.xinsight.snapshot.v1"
					*dest[4].(*string) = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
					return nil
				}}
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 51
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				detail := args[5].(string)
				if strings.Contains(detail, `"summary_status":"receipt_outdated"`) {
					auditStatus = "receipt_outdated"
				}
			}
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)
	req := dataExchangeTestRequest(http.MethodPost, "/data-exchange/exchanges/9/receipts", `{
		"exchange_id":"uxchg_expected_001",
		"schema_version":"xinsight.import-receipt.v1",
		"source_system":"unirouter",
		"source_instance":"dev-a",
		"artifact_schema_version":"unirouter.prd5.xinsight.snapshot.v1",
		"artifact_sha256":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"result_status":"persisted",
		"checked_at":"2026-07-10T08:00:00Z",
		"warning_codes":["source_capture_gaps_present"]
	}`)
	req.SetPathValue("id", "9")
	rec := httptest.NewRecorder()

	h.CreateDataExchangeReceipt(rec, req)

	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "receipt_outdated") {
		t.Fatalf("expected outdated receipt summary, got %d %s", rec.Code, rec.Body.String())
	}
	if queryCount != 2 || auditStatus != "receipt_outdated" {
		t.Fatalf("expected receipt insert and outdated audit, queryCount=%d audit=%q", queryCount, auditStatus)
	}
}

func TestDeleteDataExchangeArtifact_IsLedgerOnly(t *testing.T) {
	var auditAction string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*string) = "uxchg_expected_001"
				*dest[1].(*string) = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				*dest[2].(*string) = "evidence/snapshot.json"
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				auditAction = args[2].(string)
			}
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)
	req := dataExchangeTestRequest(http.MethodPost, "/data-exchange/exchanges/9/artifact-delete", "")
	req.SetPathValue("id", "9")
	rec := httptest.NewRecorder()

	h.DeleteDataExchangeArtifact(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"filesystem_deleted":false`) {
		t.Fatalf("expected ledger-only deletion, got %d %s", rec.Code, rec.Body.String())
	}
	if auditAction != "data_exchange.artifact.mark_deleted" {
		t.Fatalf("expected artifact deletion audit, got %q", auditAction)
	}
}

func TestGetDataExchangeActivity_ReturnsAuditedHistoryShape(t *testing.T) {
	queryCalls := 0
	var auditAction string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*string) = "uxchg_expected_001"
				*dest[1].(*string) = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
			queryCalls++
			return &testutil.MockRows{}, nil
		},
		ExecFn: func(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "INSERT INTO audit_logs") {
				auditAction = args[2].(string)
			}
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)
	req := dataExchangeTestRequest(http.MethodGet, "/data-exchange/exchanges/9/activity", "")
	req.SetPathValue("id", "9")
	rec := httptest.NewRecorder()

	h.GetDataExchangeActivity(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"members":[]`) || !strings.Contains(rec.Body.String(), `"deliveries":[]`) || !strings.Contains(rec.Body.String(), `"receipts":[]`) {
		t.Fatalf("expected empty activity shape, got %d %s", rec.Code, rec.Body.String())
	}
	if queryCalls != 3 || auditAction != "data_exchange.activity.view" {
		t.Fatalf("expected three history queries and read audit, calls=%d audit=%q", queryCalls, auditAction)
	}
}
