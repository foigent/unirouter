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

func TestP6F17ConcurrentAdminFinancialClientIDsHaveOneEffect(t *testing.T) {
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
		`INSERT INTO wallets (org_id, balance, granted_balance, frozen, total_spent)
		 VALUES ($1, 1000, 0, 0, 0)`, orgID)
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
		fmt.Sprintf(`{"org_id":%d,"amount_cents":100,"client_request_id":"same-topup-client"}`, orgID),
		h.TopUp,
	)
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusConflict}, []int{topUpResults[0].Code, topUpResults[1].Code})

	grantResults := runPair(
		"/api/v1/grants",
		fmt.Sprintf(`{"org_id":%d,"amount_cents":50,"reason":"p6f17","client_request_id":"same-grant-client"}`, orgID),
		h.CreateGrant,
	)
	require.ElementsMatch(t, []int{http.StatusCreated, http.StatusOK}, []int{grantResults[0].Code, grantResults[1].Code})

	var balance, grantedBalance, topUpTransactions, grants, grantTransactions int
	require.NoError(t, pool.QueryRow(ctx, `SELECT balance, granted_balance FROM wallets WHERE org_id = $1`, orgID).Scan(&balance, &grantedBalance))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND type = 'topup' AND client_request_id = 'same-topup-client'`, orgID,
	).Scan(&topUpTransactions))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM grants
		 WHERE org_id = $1 AND client_request_id = 'same-grant-client'`, orgID,
	).Scan(&grants))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND type = 'grant' AND client_request_id = 'same-grant-client'`, orgID,
	).Scan(&grantTransactions))
	require.Equal(t, 1100, balance)
	require.Equal(t, 50, grantedBalance)
	require.Equal(t, 1, topUpTransactions)
	require.Equal(t, 1, grants)
	require.Equal(t, 1, grantTransactions)
}
