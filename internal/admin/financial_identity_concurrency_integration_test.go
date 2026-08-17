//go:build integration

package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F17ConcurrentAdminFinancialClientIDsHaveOneEffectIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('p6f17-admin-finance', 'active', 'standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'p6f17-billing-admin', 'test-hash', $2, 'active') RETURNING id`,
		orgID, auth.RoleBillingAdmin,
	).Scan(&userID))
	_, err := pool.Exec(ctx,
		`INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros)
		 VALUES ($1, 10000000, 0, 0, 0)`, orgID)
	require.NoError(t, err)

	h := NewHandler(pool, "finance-jwt-secret", []byte("0123456789abcdef01234567"), nil)
	runPair := func(path, body string, target http.HandlerFunc) []*httptest.ResponseRecorder {
		t.Helper()
		handler := middleware.OperationIdentityWithSecret("finance-operation-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCtx := context.WithValue(r.Context(), middleware.CtxOrgID, orgID)
			requestCtx = context.WithValue(requestCtx, middleware.CtxUserID, userID)
			requestCtx = context.WithValue(requestCtx, middleware.CtxUserRole, auth.RoleBillingAdmin)
			target(w, r.WithContext(requestCtx))
		}))
		recorders := []*httptest.ResponseRecorder{httptest.NewRecorder(), httptest.NewRecorder()}
		var wg sync.WaitGroup
		for index := range recorders {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				handler.ServeHTTP(recorders[index], req)
			}(index)
		}
		wg.Wait()
		require.NotEqual(t,
			recorders[0].Header().Get(middleware.OperationIDHeader),
			recorders[1].Header().Get(middleware.OperationIDHeader),
			"concurrent HTTP operations must keep distinct server identities",
		)
		return recorders
	}

	topUpResults := runPair(
		"/api/v1/wallet/topup",
		fmt.Sprintf(`{"org_id":%d,"amount_cents":100,"description":"concurrent top-up","client_request_id":"same-topup-client"}`, orgID),
		h.TopUp,
	)
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusOK}, []int{topUpResults[0].Code, topUpResults[1].Code})

	adjustResults := runPair(
		"/api/v1/wallet/adjust",
		fmt.Sprintf(`{"org_id":%d,"delta_cents":25,"description":"concurrent adjustment","client_request_id":"same-adjust-client"}`, orgID),
		h.BalanceAdjust,
	)
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusOK}, []int{adjustResults[0].Code, adjustResults[1].Code})

	grantResults := runPair(
		"/api/v1/grants",
		fmt.Sprintf(`{"org_id":%d,"amount_cents":50,"reason":"p6f17","client_request_id":"same-grant-client"}`, orgID),
		h.CreateGrant,
	)
	require.ElementsMatch(t, []int{http.StatusCreated, http.StatusOK}, []int{grantResults[0].Code, grantResults[1].Code})

	var balanceMicros, grantedBalanceMicros int64
	var topUpTransactions, adjustTransactions, grants, grantTransactions int
	require.NoError(t, pool.QueryRow(ctx, `SELECT balance_micros, granted_balance_micros FROM wallets WHERE org_id = $1`, orgID).Scan(&balanceMicros, &grantedBalanceMicros))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND type = 'topup' AND client_request_id = 'same-topup-client'`, orgID,
	).Scan(&topUpTransactions))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND type = 'adjust' AND client_request_id = 'same-adjust-client'`, orgID,
	).Scan(&adjustTransactions))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM grants
		 WHERE org_id = $1 AND client_request_id = 'same-grant-client'`, orgID,
	).Scan(&grants))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND type = 'grant' AND client_request_id = 'same-grant-client'`, orgID,
	).Scan(&grantTransactions))
	require.Equal(t, int64(11_250_000), balanceMicros)
	require.Equal(t, int64(500_000), grantedBalanceMicros)
	require.Equal(t, 1, topUpTransactions)
	require.Equal(t, 1, adjustTransactions)
	require.Equal(t, 1, grants)
	require.Equal(t, 1, grantTransactions)
}

func TestWalletConcurrentDistinctDeltasPreserveAllEffectsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('wallet-distinct-deltas', 'active', 'standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'wallet-distinct-admin', 'test-hash', $2, 'active') RETURNING id`,
		orgID, auth.RoleBillingAdmin,
	).Scan(&userID))
	_, err := pool.Exec(ctx,
		`INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros)
		 VALUES ($1, 10000000, 0, 0, 0)`, orgID)
	require.NoError(t, err)

	h := NewHandler(pool, "finance-jwt-secret", []byte("0123456789abcdef01234567"), nil)
	wrap := func(target http.HandlerFunc) http.Handler {
		return middleware.OperationIdentityWithSecret("finance-operation-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCtx := context.WithValue(r.Context(), middleware.CtxOrgID, orgID)
			requestCtx = context.WithValue(requestCtx, middleware.CtxUserID, userID)
			requestCtx = context.WithValue(requestCtx, middleware.CtxUserRole, auth.RoleBillingAdmin)
			target(w, r.WithContext(requestCtx))
		}))
	}

	type operation struct {
		path    string
		body    string
		handler http.Handler
	}
	operations := []operation{
		{
			path:    "/api/v1/wallet/topup",
			body:    fmt.Sprintf(`{"org_id":%d,"amount_cents":100,"description":"parallel top-up","client_request_id":"parallel-topup"}`, orgID),
			handler: wrap(h.TopUp),
		},
		{
			path:    "/api/v1/wallet/adjust",
			body:    fmt.Sprintf(`{"org_id":%d,"delta_cents":-200,"description":"parallel correction","client_request_id":"parallel-adjust"}`, orgID),
			handler: wrap(h.BalanceAdjust),
		},
	}
	recorders := []*httptest.ResponseRecorder{httptest.NewRecorder(), httptest.NewRecorder()}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := range operations {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodPost, operations[index].path, strings.NewReader(operations[index].body))
			req.Header.Set("Content-Type", "application/json")
			operations[index].handler.ServeHTTP(recorders[index], req)
		}(index)
	}
	close(start)
	wg.Wait()

	require.Equal(t, http.StatusOK, recorders[0].Code, recorders[0].Body.String())
	require.Equal(t, http.StatusOK, recorders[1].Code, recorders[1].Body.String())
	var balanceMicros, transactionDeltaMicros int64
	var transactionCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT balance_micros FROM wallets WHERE org_id = $1`, orgID).Scan(&balanceMicros))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(SUM(amount_micros), 0)
		 FROM transactions WHERE org_id = $1 AND type IN ('topup', 'adjust')`, orgID,
	).Scan(&transactionCount, &transactionDeltaMicros))
	require.Equal(t, int64(9_000_000), balanceMicros)
	require.Equal(t, 2, transactionCount)
	require.Equal(t, int64(-1_000_000), transactionDeltaMicros)
}
