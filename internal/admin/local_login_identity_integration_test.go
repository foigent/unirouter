//go:build integration

package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestLocalLoginIdentityRequiresUniqueSchemaContractIntegration(t *testing.T) {
	t.Run("duplicate normalized legacy rows never select an implicit winner", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		ctx := t.Context()
		_, err := pool.Exec(ctx, `DROP INDEX IF EXISTS users_local_login_id_unique`)
		require.NoError(t, err)

		orgIDs := newLocalLoginTestOrgs(t, pool, 2)
		correctHash := localLoginTestHash(t, "correct-password-123")
		wrongHash := localLoginTestHash(t, "different-password-123")
		_, err = pool.Exec(ctx, `
			INSERT INTO users (org_id, username, password_hash, role, status, auth_provider)
			VALUES
				($1, 'CollisionAdmin', $3, 'org_admin', 'active', 'local'),
				($2, 'collisionadmin', $4, 'org_admin', 'active', 'local')`,
			orgIDs[0], orgIDs[1], correctHash, wrongHash)
		require.NoError(t, err)

		rec := serveLocalLogin(t, pool, `{"username":"collisionadmin","password":"correct-password-123"}`)
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		require.NotContains(t, rec.Body.String(), "token")
	})
}

func TestLocalLoginIdentityNormalizesInputAndIgnoresExternalUsernameIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	orgIDs := newLocalLoginTestOrgs(t, pool, 1)
	localHash := localLoginTestHash(t, "correct-password-123")
	externalHash := localLoginTestHash(t, "correct-password-123")
	_, err := pool.Exec(ctx, `
		INSERT INTO users (
			org_id, username, password_hash, role, status, email, auth_provider,
			oidc_issuer, oidc_subject
		) VALUES
			($1, 'CasePreservedAdmin', $2, 'org_admin', 'active', 'local@example.test', 'local', NULL, NULL),
			($1, 'CasePreservedAdmin', $3, 'org_viewer', 'active', 'oidc@example.test', 'oidc',
			 'https://issuer.example.test', 'external-subject')`,
		orgIDs[0], localHash, externalHash)
	require.NoError(t, err)

	rec := serveLocalLogin(t, pool, `{"username":"\t\u00a0CASEPRESERVEDADMIN\u3000","password":"correct-password-123"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "CasePreservedAdmin", body["username"])
	require.EqualValues(t, orgIDs[0], body["org_id"])
	require.Equal(t, "local", body["auth_provider"])
}

func TestCreateUserEnforcesGlobalLocalLoginIdentityIntegration(t *testing.T) {
	t.Run("trims display username and returns typed global conflict", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		orgIDs := newLocalLoginTestOrgs(t, pool, 2)

		first := serveCreateLocalUser(t, pool, fmt.Sprintf(
			`{"org_id":%d,"username":" \tCasePreservedUser\u3000 ","password":"valid-password-123"}`,
			orgIDs[0],
		))
		require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
		var stored string
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT username FROM users WHERE org_id=$1`, orgIDs[0]).Scan(&stored))
		require.Equal(t, "CasePreservedUser", stored)

		duplicate := serveCreateLocalUser(t, pool, fmt.Sprintf(
			`{"org_id":%d,"username":"casepreserveduser","password":"valid-password-456"}`,
			orgIDs[1],
		))
		require.Equal(t, http.StatusConflict, duplicate.Code, duplicate.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(duplicate.Body.Bytes(), &body))
		require.Equal(t, "local_login_id_conflict", body["code"])
		require.Equal(t, "Local login ID is unavailable", body["error"])
	})

	t.Run("rejects unicode-whitespace-only username before database work", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		orgID := newLocalLoginTestOrgs(t, pool, 1)[0]
		rec := serveCreateLocalUser(t, pool, fmt.Sprintf(
			`{"org_id":%d,"username":"\t\u00a0\u3000","password":"valid-password-123"}`,
			orgID,
		))
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		var count int
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM users`).Scan(&count))
		require.Zero(t, count)
	})

	t.Run("same-organization external display name does not reserve the local login ID", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		orgID := newLocalLoginTestOrgs(t, pool, 1)[0]
		_, err := pool.Exec(t.Context(), `
			INSERT INTO users (
				org_id,username,password_hash,role,status,auth_provider,oidc_issuer,oidc_subject
			) VALUES ($1,'SharedIdentity','oidc:placeholder','org_viewer','active','oidc',$2,$3)`,
			orgID, "https://issuer.example.test", "same-org-external-subject")
		require.NoError(t, err)

		created := serveCreateLocalUser(t, pool, fmt.Sprintf(
			`{"org_id":%d,"username":"SharedIdentity","password":"valid-password-123"}`,
			orgID,
		))
		require.Equal(t, http.StatusCreated, created.Code, created.Body.String())

		var localCount, externalCount int
		require.NoError(t, pool.QueryRow(t.Context(), `
			SELECT COUNT(*) FILTER (WHERE COALESCE(NULLIF(auth_provider,''),'local')='local'),
			       COUNT(*) FILTER (WHERE auth_provider='oidc')
			  FROM users WHERE org_id=$1 AND username='SharedIdentity'`, orgID).Scan(&localCount, &externalCount))
		require.Equal(t, 1, localCount)
		require.Equal(t, 1, externalCount)
	})

	t.Run("concurrent cross-org creates have exactly one winner", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		orgIDs := newLocalLoginTestOrgs(t, pool, 2)
		bodies := []string{
			fmt.Sprintf(`{"org_id":%d,"username":"ConcurrentIdentity","password":"valid-password-123"}`, orgIDs[0]),
			fmt.Sprintf(`{"org_id":%d,"username":"\u00a0concurrentidentity\u3000","password":"valid-password-456"}`, orgIDs[1]),
		}
		statuses := make(chan int, len(bodies))
		var wg sync.WaitGroup
		for _, body := range bodies {
			body := body
			wg.Add(1)
			go func() {
				defer wg.Done()
				statuses <- serveCreateLocalUser(t, pool, body).Code
			}()
		}
		wg.Wait()
		close(statuses)
		created, conflicts := 0, 0
		for status := range statuses {
			switch status {
			case http.StatusCreated:
				created++
			case http.StatusConflict:
				conflicts++
			default:
				t.Fatalf("unexpected concurrent create status %d", status)
			}
		}
		require.Equal(t, 1, created)
		require.Equal(t, 1, conflicts)
		var count int
		require.NoError(t, pool.QueryRow(t.Context(), `
			SELECT COUNT(*) FROM users
			WHERE public.normalize_local_login_id(username)='concurrentidentity'
			  AND COALESCE(NULLIF(auth_provider,''),'local')='local'`).Scan(&count))
		require.Equal(t, 1, count)
	})
}

func localLoginTestHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	return string(hash)
}

func serveLocalLogin(t *testing.T, pool db.DBQuerier, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(pool, "local-login-test-secret", []byte("0123456789abcdef01234567"), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	return rec
}

func serveCreateLocalUser(t *testing.T, pool db.DBQuerier, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(pool, "local-login-test-secret", []byte("0123456789abcdef01234567"), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CreateUser(rec, req)
	return rec
}

func newLocalLoginTestOrgs(t *testing.T, pool *pgxpool.Pool, count int) []int64 {
	t.Helper()
	orgIDs := make([]int64, 0, count)
	for i := 0; i < count; i++ {
		var orgID int64
		require.NoError(t, pool.QueryRow(t.Context(), `
			INSERT INTO orgs (name,status,org_type)
			VALUES ($1,'active','standard') RETURNING id`,
			fmt.Sprintf("local-login-%d-%d", time.Now().UnixNano(), i),
		).Scan(&orgID))
		orgIDs = append(orgIDs, orgID)
	}
	return orgIDs
}
