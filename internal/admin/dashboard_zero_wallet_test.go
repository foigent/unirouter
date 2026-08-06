package admin

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDashboardWalletInfoTreatsMissingWalletAsZero(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(...any) error { return pgx.ErrNoRows }}
		},
	})

	balance, granted, frozen, threshold, err := h.dashboardWalletInfo(context.Background(), 7)
	if err != nil {
		t.Fatalf("missing wallet must be a zero baseline: %v", err)
	}
	if balance != 0 || granted != 0 || frozen != 0 || threshold != 0 {
		t.Fatalf("expected exact zero wallet baseline, got balance=%d granted=%d frozen=%d threshold=%d", balance, granted, frozen, threshold)
	}
}

func TestClassifyDashboardQueryErrorDoesNotUseRawErrorText(t *testing.T) {
	reason, sqlState := classifyDashboardQueryError(context.DeadlineExceeded)
	if reason != dashboardReasonDeadlineExceeded || sqlState != "none" {
		t.Fatalf("unexpected deadline classification: reason=%q sqlstate=%q", reason, sqlState)
	}

	reason, sqlState = classifyDashboardQueryError(assertionError("customer-value raw SQL SELECT secret"))
	if reason != dashboardReasonDatabase || sqlState != "none" {
		t.Fatalf("unexpected generic classification: reason=%q sqlstate=%q", reason, sqlState)
	}
}

type assertionError string

func (err assertionError) Error() string { return string(err) }
