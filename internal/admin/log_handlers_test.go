package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
