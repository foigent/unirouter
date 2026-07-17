//go:build integration

package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestAdminSetPasswordCLIRevokesSessionsAndAuditsAtomicallyIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("initial-password-123"), bcrypt.DefaultCost)
	require.NoError(t, err)
	var userID int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'cli-password-user', $2, $3, 'active') RETURNING id`,
		orgID, string(oldHash), auth.RolePlatformAdmin).Scan(&userID))
	const secret = "cli-admin-session-secret"
	oldToken := testutil.MustAdminSessionJWT(t, t.Context(), pool, userID, orgID, auth.RolePlatformAdmin, secret)
	protected := middleware.AdminAuth(pool, secret)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	require.Equal(t, http.StatusNoContent, serveCLIAdminToken(protected, oldToken).Code)

	require.NoError(t, runAdminCLIWithPasswordInput(t.Context(), []string{
		"set-password",
		"--username", "  CLI-PASSWORD-USER  ",
		"--org-id", strconv.FormatInt(orgID, 10),
		"--password-stdin",
	}, adminCLIConfig(pool.Config().ConnString()), nonTerminalAdminPasswordInput("replacement-password-456\n")))
	require.Equal(t, http.StatusUnauthorized, serveCLIAdminToken(protected, oldToken).Code)
	var passwordHash string
	var epoch int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT password_hash, auth_epoch FROM users WHERE id = $1`, userID).Scan(&passwordHash, &epoch))
	require.Equal(t, int64(1), epoch)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("replacement-password-456")))
	var audits int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM audit_logs
		  WHERE user_id = $1 AND org_id = $2 AND action = 'user.password_reset' AND source = 'system'`,
		userID, orgID).Scan(&audits))
	require.Equal(t, 1, audits)
}

func TestAdminCreateCLIUsesStdinAndPreservesNormalizedIdentityAndPasswordIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	const password = "  create-password-456  "

	require.NoError(t, runAdminCLIWithPasswordInput(t.Context(), []string{
		"create",
		"--username", "  DisplayAdmin  ",
		"--role", auth.RoleOrgAdmin,
		"--org-id", strconv.FormatInt(orgID, 10),
		"--password-stdin",
	}, adminCLIConfig(pool.Config().ConnString()), nonTerminalAdminPasswordInput(password+"\r\n")))

	var username, passwordHash, role, provider string
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT username, password_hash, role, auth_provider
		  FROM users
		 WHERE org_id=$1 AND public.normalize_local_login_id(username)='displayadmin'`, orgID,
	).Scan(&username, &passwordHash, &role, &provider))
	require.Equal(t, "DisplayAdmin", username)
	require.Equal(t, auth.RoleOrgAdmin, role)
	require.Equal(t, "local", provider)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)))
}

func TestAdminCreateCLIMapsGlobalIdentityConflictWithoutDatabaseDetailIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	firstOrgID := testutil.NewTestOrg(t, pool)
	var secondOrgID int64
	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO orgs (name, status, org_type)
		VALUES ('cli-conflict-second-org', 'active', 'standard') RETURNING id`,
	).Scan(&secondOrgID))
	_, err := pool.Exec(t.Context(), `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
		VALUES ($1,'ExistingAdmin','hash',$2,'active','local')`, firstOrgID, auth.RoleOrgAdmin)
	require.NoError(t, err)

	err = runAdminCLIWithPasswordInput(t.Context(), []string{
		"create",
		"--username", "  existingadmin  ",
		"--role", auth.RoleOrgAdmin,
		"--org-id", strconv.FormatInt(secondOrgID, 10),
		"--password-stdin",
	}, adminCLIConfig(pool.Config().ConnString()), nonTerminalAdminPasswordInput("replacement-password-456\n"))
	require.EqualError(t, err, "local login ID is unavailable")
	require.NotContains(t, err.Error(), "users_local_login_id_unique")
	require.NotContains(t, err.Error(), "ExistingAdmin")
}

func TestAdminSetPasswordCLIHandlesLegacyEmptyProviderAsLocalIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("initial-password-123"), bcrypt.DefaultCost)
	require.NoError(t, err)
	var userID int64
	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
		VALUES ($1,'LegacyLocalUser',$2,$3,'active','') RETURNING id`,
		orgID, string(oldHash), auth.RoleOrgAdmin).Scan(&userID))

	require.NoError(t, setAdminPasswordWithDB(t.Context(), pool, " legacylocaluser ", "replacement-password-456", orgID))
	var passwordHash string
	var epoch int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT password_hash,auth_epoch FROM users WHERE id=$1`, userID).Scan(&passwordHash, &epoch))
	require.Equal(t, int64(1), epoch)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("replacement-password-456")))
}

func TestAdminSetPasswordCLINeverResetsOIDCIdentityIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("initial-password-123"), bcrypt.DefaultCost)
	require.NoError(t, err)
	var userID int64
	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider,oidc_issuer,oidc_subject)
		VALUES ($1,'OIDCOnlyUser',$2,$3,'active','oidc','https://idp.example','oidc-only-user') RETURNING id`,
		orgID, string(oldHash), auth.RoleOrgAdmin).Scan(&userID))

	err = setAdminPasswordWithDB(t.Context(), pool, "oidconlyuser", "replacement-password-456", orgID)
	require.ErrorContains(t, err, "not found")
	var passwordHash string
	var epoch int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT password_hash,auth_epoch FROM users WHERE id=$1`, userID).Scan(&passwordHash, &epoch))
	require.Equal(t, string(oldHash), passwordHash)
	require.Zero(t, epoch)
	var audits int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM audit_logs WHERE user_id=$1 AND action='user.password_reset'`, userID).Scan(&audits))
	require.Zero(t, audits)
}

func TestAdminSetPasswordCLIAmbiguousLocalIdentityHasZeroEffectsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	require.NoError(t, func() error {
		_, err := pool.Exec(t.Context(), `DROP INDEX users_local_login_id_unique`)
		return err
	}())
	oldHash, err := bcrypt.GenerateFromPassword([]byte("initial-password-123"), bcrypt.DefaultCost)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
		VALUES
		 ($1,'AmbiguousAdmin',$2,$3,'active','local'),
		 ($1,'AMBIGUOUSADMIN',$2,$3,'active','local')`,
		orgID, string(oldHash), auth.RoleOrgAdmin)
	require.NoError(t, err)

	err = setAdminPasswordWithDB(t.Context(), pool, "AMBIGUOUSADMIN", "replacement-password-456", orgID)
	require.ErrorContains(t, err, "not found")
	var unchanged, epochSum, audits int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FILTER (WHERE password_hash=$2), COALESCE(SUM(auth_epoch),0)
		  FROM users
		 WHERE org_id=$1 AND public.normalize_local_login_id(username)='ambiguousadmin'`,
		orgID, string(oldHash)).Scan(&unchanged, &epochSum))
	require.Equal(t, 2, unchanged)
	require.Zero(t, epochSum)
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM audit_logs WHERE org_id=$1 AND action='user.password_reset'`, orgID).Scan(&audits))
	require.Zero(t, audits)
}

func TestAdminSetPasswordCLIAuditFailureRollsBackIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("initial-password-123"), bcrypt.DefaultCost)
	require.NoError(t, err)
	var userID int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'cli-password-audit-rollback', $2, $3, 'active') RETURNING id`,
		orgID, string(oldHash), auth.RolePlatformAdmin).Scan(&userID))
	_, err = pool.Exec(t.Context(), `
		CREATE FUNCTION reject_cli_password_audit() RETURNS TRIGGER AS $$
		BEGIN
		    IF NEW.action = 'user.password_reset' THEN
		        RAISE EXCEPTION 'reject CLI password audit';
		    END IF;
		    RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_cli_password_audit_trigger
		BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_cli_password_audit();`)
	require.NoError(t, err)

	err = setAdminPasswordWithDB(t.Context(), pool, "cli-password-audit-rollback", "replacement-password-456", orgID)
	require.Error(t, err)
	var passwordHash string
	var epoch int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT password_hash, auth_epoch FROM users WHERE id = $1`, userID).Scan(&passwordHash, &epoch))
	require.Equal(t, string(oldHash), passwordHash)
	require.Zero(t, epoch)
}

func serveCLIAdminToken(handler http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func adminCLIConfig(dsn string) *config.Config {
	return &config.Config{
		DB_DSN:           dsn,
		DBMaxConnections: 2,
		DBMaxIdleConns:   0,
	}
}
