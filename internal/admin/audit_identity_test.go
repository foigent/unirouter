package admin

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

// audit_logs.action is VARCHAR(50). Check the direct X-Insight action literals
// before an integration run reaches the transactional audit writer.
func TestXInsightAuditActionLiteralsFitSchema(t *testing.T) {
	paths, err := filepath.Glob("xinsight*.go")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	fileSet := token.NewFileSet()
	found := 0
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		require.NoError(t, err)
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil || !strings.HasPrefix(value, "data_exchange.xinsight.") {
				return true
			}
			found++
			if len(value) > 50 {
				t.Errorf("%s: X-Insight audit action length %d exceeds audit_logs.action VARCHAR(50)", fileSet.Position(literal.Pos()), len(value))
			}
			return true
		})
	}
	require.Positive(t, found)
}

func TestP6F17AuditSeparatesServerOperationAndClientCorrelation(t *testing.T) {
	var auditArgs []any
	db := &testutil.MockDB{ExecFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		if strings.Contains(sql, "INSERT INTO audit_logs") {
			auditArgs = append([]any(nil), args...)
		}
		return pgconn.NewCommandTag("INSERT 1"), nil
	}}
	h := newTestHandler(db)

	handler := middleware.OperationIdentityWithSecret("audit-identity-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), middleware.CtxOrgID, int64(7))
		ctx = context.WithValue(ctx, middleware.CtxUserID, int64(9))
		r = r.WithContext(ctx)
		h.recordAuditFromRequest(r, "settings.update", "setting", "routing.retry", map[string]interface{}{"after": map[string]interface{}{"value": "3"}})
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/routing.retry", nil)
	req.Header.Set(middleware.ClientRequestIDHeader, "client-control-change-001")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent || len(auditArgs) != 14 {
		t.Fatalf("expected one correlated audit insert, status=%d args=%#v", rec.Code, auditArgs)
	}
	operationID, _ := auditArgs[8].(string)
	clientRequestID, _ := auditArgs[13].(string)
	if !strings.HasPrefix(operationID, "op_") || operationID == clientRequestID || clientRequestID != "client-control-change-001" {
		t.Fatalf("audit identity collision: operation=%q client=%q", operationID, clientRequestID)
	}
	if rec.Header().Get(middleware.OperationIDHeader) != operationID {
		t.Fatalf("response operation ID mismatch: header=%q audit=%q", rec.Header().Get(middleware.OperationIDHeader), operationID)
	}
}

func TestP6F17AuditIdentityPersistsAndFiltersPostgres(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('p6f17-audit-identity', 'active', 'standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'p6f17-audit-admin', 'test-hash', $2, 'active') RETURNING id`,
		orgID, auth.RoleOrgAdmin,
	).Scan(&userID))

	h := NewHandler(pool, "audit-jwt-secret", []byte("0123456789abcdef01234567"), nil)
	handler := middleware.OperationIdentityWithSecret("audit-identity-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCtx := context.WithValue(r.Context(), middleware.CtxOrgID, orgID)
		requestCtx = context.WithValue(requestCtx, middleware.CtxUserID, userID)
		requestCtx = context.WithValue(requestCtx, middleware.CtxUserRole, auth.RoleOrgAdmin)
		r = r.WithContext(requestCtx)
		h.recordAuditFromRequest(r, "settings.update", "setting", "routing.retry", map[string]interface{}{
			"after": map[string]interface{}{"value": "3"},
		})
		w.WriteHeader(http.StatusNoContent)
	}))

	writeReq := httptest.NewRequest(http.MethodPut, "/api/v1/settings/routing.retry", nil)
	writeReq.Header.Set(middleware.ClientRequestIDHeader, "client-audit-readback-001")
	writeRec := httptest.NewRecorder()
	handler.ServeHTTP(writeRec, writeReq)
	require.Equal(t, http.StatusNoContent, writeRec.Code)
	operationID := writeRec.Header().Get(middleware.OperationIDHeader)
	require.True(t, strings.HasPrefix(operationID, "op_"))

	var storedOperationID, storedClientRequestID string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT request_id, client_request_id FROM audit_logs
		 WHERE org_id = $1 AND resource_id = 'routing.retry'`, orgID,
	).Scan(&storedOperationID, &storedClientRequestID))
	require.Equal(t, operationID, storedOperationID)
	require.Equal(t, "client-audit-readback-001", storedClientRequestID)

	for query, expectedID := range map[string]string{
		"request_id=" + operationID:                   operationID,
		"client_request_id=client-audit-readback-001": operationID,
	} {
		listReq := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?"+query, nil)
		listCtx := context.WithValue(listReq.Context(), middleware.CtxOrgID, orgID)
		listCtx = context.WithValue(listCtx, middleware.CtxUserID, userID)
		listCtx = context.WithValue(listCtx, middleware.CtxUserRole, auth.RoleOrgAdmin)
		listReq = listReq.WithContext(listCtx)
		listRec := httptest.NewRecorder()
		h.ListAuditLogs(listRec, listReq)
		require.Equal(t, http.StatusOK, listRec.Code, listRec.Body.String())

		var response struct {
			Data []struct {
				RequestID       string `json:"request_id"`
				ClientRequestID string `json:"client_request_id"`
			} `json:"data"`
			Total int `json:"total"`
		}
		require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &response))
		require.Equal(t, 1, response.Total)
		require.Len(t, response.Data, 1)
		require.Equal(t, expectedID, response.Data[0].RequestID)
		require.Equal(t, "client-audit-readback-001", response.Data[0].ClientRequestID)
	}
}
