package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCreateModelPriceRollsBackWhenAuditInsertFails(t *testing.T) {
	var committed, rolledBack bool
	tx := &testutil.MockTx{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "SELECT id") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}
			return modelPriceMockRow(42, "gpt-4o", 5, 15)
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				return pgconn.CommandTag{}, fmt.Errorf("audit unavailable")
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
		CommitFn: func(ctx context.Context) error {
			committed = true
			return nil
		},
		RollbackFn: func(ctx context.Context) error {
			rolledBack = true
			return nil
		},
	}
	db := &testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil }}
	h := newTestHandler(db)
	req := pricingActorRequest(
		httptest.NewRequest(http.MethodPost, "/model-prices", strings.NewReader(`{"model_name":"gpt-4o","input_price_cny_per_million_tokens":50,"output_price_cny_per_million_tokens":150}`)),
		1,
		auth.RolePlatformAdmin,
	)
	rec := httptest.NewRecorder()

	h.CreateModelPrice(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "Audit failed")
	require.False(t, committed)
	require.True(t, rolledBack)
}

func TestPriceMutationHandlerDefenseInDepthRejectsAuthenticatedTenantRole(t *testing.T) {
	beginCalls := 0
	h := newTestHandler(&testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) {
		beginCalls++
		return &testutil.MockTx{}, nil
	}})
	req := pricingActorRequest(
		httptest.NewRequest(http.MethodPost, "/model-prices", strings.NewReader(`{"model_name":"gpt-4o","input_price_cny_per_million_tokens":50,"output_price_cny_per_million_tokens":150}`)),
		1,
		auth.RoleOrgAdmin,
	)
	rec := httptest.NewRecorder()

	h.CreateModelPrice(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Zero(t, beginCalls)
}

func TestCreateModelPriceRejectsLegacyAmbiguousUnit(t *testing.T) {
	beginCalls := 0
	h := newTestHandler(&testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) {
		beginCalls++
		return &testutil.MockTx{}, nil
	}})
	req := pricingActorRequest(
		httptest.NewRequest(http.MethodPost, "/model-prices", strings.NewReader(`{"model_name":"gpt-4o","input_price_cents":5,"output_price_cents":15}`)),
		1,
		auth.RolePlatformAdmin,
	)
	rec := httptest.NewRecorder()

	h.CreateModelPrice(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Zero(t, beginCalls)
}

func TestCreateModelPriceRejectsUnrepresentablePublicPrecision(t *testing.T) {
	beginCalls := 0
	h := newTestHandler(&testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) {
		beginCalls++
		return &testutil.MockTx{}, nil
	}})
	req := pricingActorRequest(
		httptest.NewRequest(http.MethodPost, "/model-prices", strings.NewReader(`{"model_name":"gpt-4o","input_price_cny_per_million_tokens":0.000001,"output_price_cny_per_million_tokens":1}`)),
		1,
		auth.RolePlatformAdmin,
	)
	rec := httptest.NewRecorder()

	h.CreateModelPrice(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Zero(t, beginCalls)
}

func TestOrgModelPriceMultiplierBoundsRejectValuesThatDatabaseWouldRoundOrOverflow(t *testing.T) {
	for _, multiplier := range []string{"0.0004", "100"} {
		t.Run("create_"+multiplier, func(t *testing.T) {
			beginCalls := 0
			h := newTestHandler(&testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) {
				beginCalls++
				return &testutil.MockTx{}, nil
			}})
			req := pricingActorRequest(
				httptest.NewRequest(http.MethodPost, "/org-model-prices", strings.NewReader(
					fmt.Sprintf(`{"org_id":1,"model_name":"gpt-4o","multiplier":%s}`, multiplier),
				)),
				1,
				auth.RolePlatformAdmin,
			)
			rec := httptest.NewRecorder()

			h.CreateOrgModelPrice(rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Zero(t, beginCalls)
		})

		t.Run("update_"+multiplier, func(t *testing.T) {
			beginCalls := 0
			h := newTestHandler(&testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) {
				beginCalls++
				return &testutil.MockTx{}, nil
			}})
			req := pricingActorRequest(
				httptest.NewRequest(http.MethodPut, "/org-model-prices/1", strings.NewReader(
					fmt.Sprintf(`{"multiplier":%s}`, multiplier),
				)),
				1,
				auth.RolePlatformAdmin,
			)
			req.SetPathValue("id", "1")
			rec := httptest.NewRecorder()

			h.UpdateOrgModelPrice(rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Zero(t, beginCalls)
		})
	}

	require.True(t, validOrgModelPriceMultiplier(0.001))
	require.True(t, validOrgModelPriceMultiplier(99.999))
}

func TestUpdateModelPriceRejectsEmptyRenameBeforeTransaction(t *testing.T) {
	beginCalls := 0
	h := newTestHandler(&testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) {
		beginCalls++
		return &testutil.MockTx{}, nil
	}})
	req := pricingActorRequest(
		httptest.NewRequest(http.MethodPut, "/model-prices/1", strings.NewReader(`{"model_name":"   "}`)),
		1,
		auth.RolePlatformAdmin,
	)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateModelPrice(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Zero(t, beginCalls)
}

func TestUpdateModelPriceRenameLocksTargetKeyBeforeRowMutation(t *testing.T) {
	operations := make([]string, 0, 5)
	tx := &testutil.MockTx{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			operations = append(operations, "query:"+sql)
			if strings.Contains(sql, "FOR UPDATE") {
				return modelPriceMockRow(1, "old-model", 5, 15)
			}
			return modelPriceMockRow(1, "new-model", 5, 15)
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			operations = append(operations, "exec:"+sql)
			if strings.Contains(sql, "UPDATE model_prices") {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}
			return pgconn.NewCommandTag("SELECT 1"), nil
		},
	}
	h := newTestHandler(&testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil }})
	req := pricingActorRequest(
		httptest.NewRequest(http.MethodPut, "/model-prices/1", strings.NewReader(`{"model_name":"new-model"}`)),
		1,
		auth.RolePlatformAdmin,
	)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateModelPrice(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.GreaterOrEqual(t, len(operations), 2)
	require.Contains(t, operations[0], "pg_advisory_xact_lock")
	require.Contains(t, operations[1], "FOR UPDATE")
}
