package runtimechange

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestListPageReturnsAuthoritativeTotalsAndUsesRequestedOffset(t *testing.T) {
	var queryArgs []any
	database := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*(dest[0].(*int)) = 105
				*(dest[1].(*int)) = 2
				*(dest[2].(*int)) = 1
				*(dest[3].(*int)) = 1
				*(dest[4].(*int)) = 1
				*(dest[5].(*int)) = 3
				*(dest[6].(*int)) = 99
				return nil
			}}
		},
		QueryFn: func(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
			queryArgs = append([]any(nil), args...)
			return &testutil.MockRows{}, nil
		},
	}
	manager := &Manager{db: database}

	page, err := manager.ListPage(context.Background(), "kimi-k3", 3, 20)
	if err != nil {
		t.Fatalf("ListPage() error = %v", err)
	}
	if page.Total != 105 || page.Page != 3 || page.PageSize != 20 || page.Summary.CurrentStable != 1 || page.Summary.RetainedActive != 1 || page.Summary.History != 99 {
		t.Fatalf("ListPage() = %+v", page)
	}
	if len(queryArgs) != 3 || queryArgs[0] != "kimi-k3" || queryArgs[1] != 20 || queryArgs[2] != 40 {
		t.Fatalf("query args = %#v, want model, limit 20, offset 40", queryArgs)
	}
}
