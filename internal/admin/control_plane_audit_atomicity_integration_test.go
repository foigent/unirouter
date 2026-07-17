package admin

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestControlPlaneAuditTargetAttributionIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)
	require.NoError(t, insertAuditTestWallets(ctx, pool, fx.orgA, fx.orgB))

	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	mux := controlPlaneAuditMux(h)
	serve := controlPlaneAuditServe(t, pool, mux, fx.platformA, fx.orgA)

	userCreate := serve(http.MethodPost, "/api/v1/users",
		fmt.Sprintf(`{"org_id":%d,"username":"audit-target-user","password":"audit-password-123","role":"org_viewer"}`, fx.orgB),
		"audit-target-user-create")
	require.Equal(t, http.StatusCreated, userCreate.Code, userCreate.Body.String())
	userID := responseID(t, userCreate.Body.Bytes())

	userUpdate := serve(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", userID),
		`{"status":"disabled"}`, "audit-target-user-update")
	require.Equal(t, http.StatusOK, userUpdate.Code, userUpdate.Body.String())
	userDelete := serve(http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", userID), "", "audit-target-user-delete")
	require.Equal(t, http.StatusOK, userDelete.Code, userDelete.Body.String())

	projectCreate := serve(http.MethodPost, "/api/v1/projects",
		fmt.Sprintf(`{"org_id":%d,"name":"Audit Target Project"}`, fx.orgB), "audit-target-project-create")
	require.Equal(t, http.StatusCreated, projectCreate.Code, projectCreate.Body.String())
	createdProjectID := responseID(t, projectCreate.Body.Bytes())
	projectUpdate := serve(http.MethodPut, fmt.Sprintf("/api/v1/projects/%d", fx.projectB),
		`{"description":"audit-target-project-updated"}`, "audit-target-project-update")
	require.Equal(t, http.StatusOK, projectUpdate.Code, projectUpdate.Body.String())

	keyCreate := serve(http.MethodPost, "/api/v1/keys",
		fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"audit-target-key","key_type":"shared"}`, fx.orgB, fx.projectB),
		"audit-target-key-create")
	require.Equal(t, http.StatusCreated, keyCreate.Code, keyCreate.Body.String())
	keyID := responseID(t, keyCreate.Body.Bytes())
	keyUpdate := serve(http.MethodPut, fmt.Sprintf("/api/v1/keys/%d", keyID),
		`{"name":"audit-target-key-updated"}`, "audit-target-key-update")
	require.Equal(t, http.StatusOK, keyUpdate.Code, keyUpdate.Body.String())
	keyDisable := serve(http.MethodPost, fmt.Sprintf("/api/v1/keys/%d/disable", keyID), "", "audit-target-key-disable")
	require.Equal(t, http.StatusOK, keyDisable.Code, keyDisable.Body.String())

	topUp := serve(http.MethodPost, "/api/v1/wallet/topup",
		fmt.Sprintf(`{"org_id":%d,"amount_cents":100,"description":"audit target topup","client_request_id":"audit-target-topup"}`, fx.orgB),
		"audit-target-wallet-topup")
	require.Equal(t, http.StatusOK, topUp.Code, topUp.Body.String())
	adjust := serve(http.MethodPost, "/api/v1/wallet/adjust",
		fmt.Sprintf(`{"org_id":%d,"delta_cents":50,"description":"audit target adjust","client_request_id":"audit-target-adjust"}`, fx.orgB),
		"audit-target-wallet-adjust")
	require.Equal(t, http.StatusOK, adjust.Code, adjust.Body.String())
	grant := serve(http.MethodPost, "/api/v1/grants",
		fmt.Sprintf(`{"org_id":%d,"amount_cents":25,"reason":"audit target grant","client_request_id":"audit-target-grant"}`, fx.orgB),
		"audit-target-wallet-grant")
	require.Equal(t, http.StatusCreated, grant.Code, grant.Body.String())

	assertAuditTarget := func(requestID, action string, projectID int64) {
		t.Helper()
		var gotOrgID int64
		var gotProjectID *int64
		var gotAction string
		err := pool.QueryRow(ctx,
			`SELECT org_id, project_id, action FROM audit_logs WHERE request_id = $1`, requestID,
		).Scan(&gotOrgID, &gotProjectID, &gotAction)
		require.NoError(t, err, requestID)
		require.Equal(t, fx.orgB, gotOrgID, requestID)
		require.Equal(t, action, gotAction, requestID)
		if projectID > 0 {
			require.NotNil(t, gotProjectID, requestID)
			require.Equal(t, projectID, *gotProjectID, requestID)
		} else {
			require.Nil(t, gotProjectID, requestID)
		}
	}

	for _, check := range []struct {
		requestID string
		action    string
		projectID int64
	}{
		{"audit-target-user-create", "user.create", 0},
		{"audit-target-user-update", "user.update", 0},
		{"audit-target-user-delete", "user.delete", 0},
		{"audit-target-project-create", "project.create", createdProjectID},
		{"audit-target-project-update", "project.update", fx.projectB},
		{"audit-target-key-create", "apikey.create", fx.projectB},
		{"audit-target-key-update", "apikey.update", fx.projectB},
		{"audit-target-key-disable", "apikey.disable", fx.projectB},
		{"audit-target-wallet-topup", "wallet.topup", 0},
		{"audit-target-wallet-adjust", "wallet.adjust", 0},
		{"audit-target-wallet-grant", "wallet.grant", 0},
	} {
		assertAuditTarget(check.requestID, check.action, check.projectID)
	}
}

func TestControlPlaneMutationRollsBackWhenAuditInsertFailsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)
	require.NoError(t, insertAuditTestWallets(ctx, pool, fx.orgA, fx.orgB))

	var deleteTargetID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'audit-rollback-delete-target', 'test-hash', 'org_viewer', 'active') RETURNING id`, fx.orgB,
	).Scan(&deleteTargetID))
	require.NoError(t, installRejectAuditTrigger(ctx, pool))

	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	mux := controlPlaneAuditMux(h)
	serve := controlPlaneAuditServe(t, pool, mux, fx.platformA, fx.orgA)
	assertAuditFailure := func(rec *httptest.ResponseRecorder) {
		t.Helper()
		require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	}

	assertAuditFailure(serve(http.MethodPost, "/api/v1/users",
		fmt.Sprintf(`{"org_id":%d,"username":"audit-rollback-created-user","password":"audit-password-123","role":"org_viewer"}`, fx.orgB),
		"audit-reject-user-create"))
	assertCount(t, pool, `SELECT COUNT(*) FROM users WHERE username = 'audit-rollback-created-user'`, 0)

	assertAuditFailure(serve(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", fx.normalB),
		`{"status":"disabled"}`, "audit-reject-user-update"))
	assertStringColumn(t, pool, `SELECT status FROM users WHERE id = $1`, fx.normalB, "active")

	assertAuditFailure(serve(http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", deleteTargetID), "", "audit-reject-user-delete"))
	assertCount(t, pool, `SELECT COUNT(*) FROM users WHERE id = $1`, 1, deleteTargetID)

	assertAuditFailure(serve(http.MethodPost, "/api/v1/projects",
		fmt.Sprintf(`{"org_id":%d,"name":"Audit Rollback Project"}`, fx.orgB), "audit-reject-project-create"))
	assertCount(t, pool, `SELECT COUNT(*) FROM projects WHERE name = 'Audit Rollback Project'`, 0)

	assertAuditFailure(serve(http.MethodPut, fmt.Sprintf("/api/v1/projects/%d", fx.projectB),
		`{"description":"audit-rollback-project-mutated"}`, "audit-reject-project-update"))
	assertStringColumn(t, pool, `SELECT description FROM projects WHERE id = $1`, fx.projectB, "project-b")

	assertAuditFailure(serve(http.MethodPost, "/api/v1/keys",
		fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"audit-rollback-created-key","key_type":"shared"}`, fx.orgB, fx.projectB),
		"audit-reject-key-create"))
	assertCount(t, pool, `SELECT COUNT(*) FROM api_keys WHERE name = 'audit-rollback-created-key'`, 0)

	assertAuditFailure(serve(http.MethodPut, fmt.Sprintf("/api/v1/keys/%d", fx.keyB),
		`{"name":"audit-rollback-key-mutated"}`, "audit-reject-key-update"))
	assertStringColumn(t, pool, `SELECT name FROM api_keys WHERE id = $1`, fx.keyB, "key-b")

	assertAuditFailure(serve(http.MethodPost, fmt.Sprintf("/api/v1/keys/%d/disable", fx.keyB), "", "audit-reject-key-disable"))
	assertStringColumn(t, pool, `SELECT status FROM api_keys WHERE id = $1`, fx.keyB, "active")

	var balanceBefore, grantedBefore int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT balance_micros, granted_balance_micros FROM wallets WHERE org_id = $1`, fx.orgB,
	).Scan(&balanceBefore, &grantedBefore))
	assertAuditFailure(serve(http.MethodPost, "/api/v1/wallet/topup",
		fmt.Sprintf(`{"org_id":%d,"amount_cents":100,"description":"audit rollback topup","client_request_id":"audit-rollback-topup"}`, fx.orgB),
		"audit-reject-wallet-topup"))
	assertWalletBalances(t, pool, fx.orgB, balanceBefore, grantedBefore)
	assertCount(t, pool, `SELECT COUNT(*) FROM transactions WHERE client_request_id = 'audit-rollback-topup'`, 0)

	assertAuditFailure(serve(http.MethodPost, "/api/v1/wallet/adjust",
		fmt.Sprintf(`{"org_id":%d,"delta_cents":100,"description":"audit rollback adjust","client_request_id":"audit-rollback-adjust"}`, fx.orgB),
		"audit-reject-wallet-adjust"))
	assertWalletBalances(t, pool, fx.orgB, balanceBefore, grantedBefore)
	assertCount(t, pool, `SELECT COUNT(*) FROM transactions WHERE client_request_id = 'audit-rollback-adjust'`, 0)

	assertAuditFailure(serve(http.MethodPost, "/api/v1/grants",
		fmt.Sprintf(`{"org_id":%d,"amount_cents":100,"reason":"audit rollback grant","client_request_id":"audit-rollback-grant"}`, fx.orgB),
		"audit-reject-wallet-grant"))
	assertWalletBalances(t, pool, fx.orgB, balanceBefore, grantedBefore)
	assertCount(t, pool, `SELECT COUNT(*) FROM grants WHERE client_request_id = 'audit-rollback-grant'`, 0)
	assertCount(t, pool, `SELECT COUNT(*) FROM transactions WHERE client_request_id = 'audit-rollback-grant'`, 0)
}

type controlPlaneAuditRequest func(method, target, body, requestID string) *httptest.ResponseRecorder

func controlPlaneAuditServe(t *testing.T, pool *pgxpool.Pool, mux http.Handler, userID, orgID int64) controlPlaneAuditRequest {
	t.Helper()
	token := testutil.MustAdminSessionJWT(t, t.Context(), pool, userID, orgID, auth.RolePlatformAdmin, p6f16JWTSecret)
	return func(method, target, body, requestID string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), middleware.CtxRequestID, requestID))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
}

func controlPlaneAuditMux(h *Handler) *http.ServeMux {
	mux := p6f16AdminMux(h)
	roleRoute := func(handler http.HandlerFunc, roles ...string) http.Handler {
		return middleware.AdminAuth(h.db, p6f16JWTSecret)(middleware.RequireRoles(roles...)(handler))
	}
	billingRoles := []string{auth.RolePlatformAdmin, auth.RoleBillingAdmin}
	mux.Handle("POST /api/v1/wallet/topup", roleRoute(h.TopUp, billingRoles...))
	mux.Handle("POST /api/v1/wallet/adjust", roleRoute(h.BalanceAdjust, billingRoles...))
	mux.Handle("POST /api/v1/grants", roleRoute(h.CreateGrant, billingRoles...))
	return mux
}

func insertAuditTestWallets(ctx context.Context, pool interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
}, orgIDs ...int64) error {
	for _, orgID := range orgIDs {
		if _, err := pool.Exec(ctx,
			`INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros)
			 VALUES ($1, 10000000, 1000000, 0, 0)`, orgID); err != nil {
			return err
		}
	}
	return nil
}

func installRejectAuditTrigger(ctx context.Context, pool interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
}) error {
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION reject_control_plane_audit_for_test() RETURNS trigger AS $$
		BEGIN
			IF NEW.request_id LIKE 'audit-reject-%' THEN
				RAISE EXCEPTION 'forced audit failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `
		CREATE TRIGGER reject_control_plane_audit_for_test
		BEFORE INSERT ON audit_logs
		FOR EACH ROW EXECUTE FUNCTION reject_control_plane_audit_for_test()`)
	return err
}

func assertCount(t *testing.T, pool interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, query string, want int, args ...interface{}) {
	t.Helper()
	var got int
	require.NoError(t, pool.QueryRow(context.Background(), query, args...).Scan(&got))
	require.Equal(t, want, got)
}

func assertStringColumn(t *testing.T, pool interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, query string, arg interface{}, want string) {
	t.Helper()
	var got string
	require.NoError(t, pool.QueryRow(context.Background(), query, arg).Scan(&got))
	require.Equal(t, want, got)
}

func assertWalletBalances(t *testing.T, pool interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, orgID, wantBalance, wantGranted int64) {
	t.Helper()
	var balance, granted int64
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT balance_micros, granted_balance_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &granted))
	require.Equal(t, wantBalance, balance)
	require.Equal(t, wantGranted, granted)
}
