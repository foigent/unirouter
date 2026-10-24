//go:build integration

package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/signing"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/upgradebundle"
)

func TestFirstCustomerBootstrapAfterNativePasswordHistoryAndSignedUpgradeIntegration(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	require.NoError(t, testutil.InstallExactTestArtifact(t.Context(), pool, testutil.MigrationsDir(t)))
	_, err := customercontext.EnsureBaseline(t.Context(), pool)
	require.NoError(t, err)
	ledger, err := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
	require.NoError(t, err)
	const jwt = "fresh-upgrade-local-jwt-fixture"
	h := admin.NewHandler(pool, jwt, []byte("0123456789abcdef01234567"), nil)
	h.SetRuntimeStatusOptions(admin.RuntimeStatusOptions{Environment: "production", SchemaMigrations: ledger})
	events, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	h.SetOutbox(events)
	login := func(password string) string {
		body, _ := json.Marshal(map[string]string{"username": "admin", "password": password})
		w := httptest.NewRecorder()
		middleware.OperationIdentity(http.HandlerFunc(h.Login)).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body)))
		require.Equal(t, http.StatusOK, w.Code)
		var result struct{ Token string }
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.NotEmpty(t, result.Token)
		return result.Token
	}
	bootstrap := func(token, id string, dry bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"organization_name": "Foigent Community Acceptance", "dry_run": dry})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set(middleware.ClientRequestIDHeader, id)
		w := httptest.NewRecorder()
		middleware.OperationIdentity(middleware.AdminAuth(pool, jwt)(http.HandlerFunc(h.BootstrapFirstCustomer))).ServeHTTP(w, r)
		return w
	}
	require.NoError(t, setAdminPasswordWithDB(t.Context(), pool, "admin", "initial-local-password", 1))
	old := login("initial-local-password")
	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.Header.Set("Authorization", "Bearer "+old)
	logoutResponse := httptest.NewRecorder()
	middleware.OperationIdentity(http.HandlerFunc(h.Logout)).ServeHTTP(logoutResponse, logout)
	require.Equal(t, http.StatusOK, logoutResponse.Code)
	require.NoError(t, setAdminPasswordWithDB(t.Context(), pool, "admin", "human-local-password", 1))
	current := login("human-local-password")
	var resets, sessions, live int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT "+
		"(SELECT count(*) FROM audit_logs WHERE action='user.password_reset'),"+
		"(SELECT count(*) FROM admin_sessions),"+
		"(SELECT count(*) FROM admin_sessions WHERE revoked_at IS NULL AND expires_at>now())").Scan(&resets, &sessions, &live))
	require.Equal(t, 2, resets)
	require.Equal(t, 2, sessions)
	require.Equal(t, 1, live)
	require.Equal(t, http.StatusUnauthorized, bootstrap(old, "revoked-fixture", true).Code)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pubText, err := signing.EncodePublicKey(pub)
	require.NoError(t, err)
	ring, err := json.Marshal(map[string]string{"local-fresh-upgrade-fixture": pubText})
	require.NoError(t, err)
	previous := int64(0)
	for i, seq := range []int64{13, 29} {
		dir := t.TempDir()
		payload := filepath.Join(dir, "uniroute")
		require.NoError(t, os.WriteFile(payload, []byte(fmt.Sprintf("synthetic runtime %d", seq)), 0o600))
		version := []string{"1.14.0", "1.14.1"}[i]
		commit := strings.Repeat([]string{"c", "d"}[i], 40)
		sources := []upgradebundle.Source{{BundlePath: "payload/uniroute", FilePath: payload}}
		manifest, err := upgradebundle.CreateManifest(upgradebundle.ManifestOptions{
			KeyID: "local-fresh-upgrade-fixture", ReleaseSequence: seq, Version: version, Commit: commit,
			CreatedAt: time.Now().UTC().Add(-time.Minute), NoExpiry: true,
			TargetOS: runtime.GOOS, TargetArch: runtime.GOARCH, MinCurrentSequence: previous, MaxCurrentSequence: previous,
			MinCurrentSchema: schemaauthority.LatestSupportedMigration, TargetSchema: schemaauthority.LatestSupportedMigration,
		}, sources)
		require.NoError(t, err)
		var packed bytes.Buffer
		require.NoError(t, upgradebundle.Pack(&packed, manifest, priv, sources))
		packagePath := filepath.Join(dir, "local-fixture.tar.gz")
		require.NoError(t, os.WriteFile(packagePath, packed.Bytes(), 0o600))
		cfg := &config.Config{RuntimeVersion: version, RuntimeCommit: commit, UpgradePublicKeys: string(ring)}
		require.NoError(t, runUpgradeRecordApplied(t.Context(), []string{"--package", packagePath, "--recorded-by", "local-fresh-upgrade-fixture"}, cfg, pool))
		previous = seq
	}
	var historyCount int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM release_history").Scan(&historyCount))
	require.Equal(t, 2, historyCount)

	// Reproduce the observed two-session/two-reset shape before adding another
	// live login. Synthetic password, JWT and session values never leave the test.
	preserved := func() string {
		var raw []byte
		require.NoError(t, pool.QueryRow(t.Context(), "SELECT jsonb_build_object("+
			"'sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM admin_sessions s),"+
			"'auth_audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_logs a WHERE action IN ('login','logout','user.password_reset')),"+
			"'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY event_key) FROM internal_outbox_events e),"+
			"'state',(SELECT jsonb_agg(to_jsonb(s)) FROM release_state s),"+
			"'history',(SELECT jsonb_agg(to_jsonb(h) ORDER BY release_sequence) FROM release_history h))").Scan(&raw))
		return fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	checkpointState := func() string {
		var raw []byte
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
			'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
			'orgs',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM orgs o),
			'wallets',(SELECT jsonb_agg(to_jsonb(w) ORDER BY org_id) FROM wallets w),
			'projects',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM projects p),
			'members',(SELECT jsonb_agg(to_jsonb(m) ORDER BY project_id,user_id) FROM project_members m),
			'keys',(SELECT jsonb_agg(to_jsonb(k) ORDER BY id) FROM api_keys k),
			'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_logs a),
			'settings',(SELECT jsonb_agg(to_jsonb(s) ORDER BY key) FROM settings s),
			'sequences',(SELECT jsonb_agg(jsonb_build_array(sequencename,last_value)
			 ORDER BY sequencename) FROM pg_sequences WHERE schemaname='public'))`).Scan(&raw))
		return preserved() + fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	var originalLogout []byte
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_jsonb(a) FROM audit_logs a WHERE action='logout'`).Scan(&originalLogout))
	restoreLogout := func(t *testing.T) {
		t.Helper()
		_, err := pool.Exec(t.Context(), `UPDATE audit_logs a SET
			action=s.action,resource=s.resource,resource_id=s.resource_id,
			request_id=s.request_id,source=s.source,detail=s.detail,
			before_state=s.before_state,after_state=s.after_state,
			user_id=s.user_id,org_id=s.org_id,project_id=s.project_id,
			actor_kind=s.actor_kind,actor_ref=s.actor_ref
			FROM jsonb_populate_record(NULL::audit_logs,$1::jsonb) s WHERE a.id=s.id`, string(originalLogout))
		require.NoError(t, err)
	}
	before := checkpointState()
	preview := bootstrap(current, "reviewed-bootstrap", true)
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	require.Equal(t, before, checkpointState())
	var reviewed map[string]any
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &reviewed))
	require.Equal(t, "fresh_seed", reviewed["checkpoint"])
	// A retained revoked row remains a sufficient historical witness for an
	// older logout lacking operation identity. Only an absent witness needs it.
	require.True(t, t.Run("retained_legacy_empty_operation", func(t *testing.T) {
		defer restoreLogout(t)
		_, err := pool.Exec(t.Context(), `UPDATE audit_logs SET request_id='' WHERE action='logout'`)
		require.NoError(t, err)
		before := checkpointState()
		response := bootstrap(current, "retained-legacy-preview", true)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Equal(t, before, checkpointState())
	}))
	_, err = pool.Exec(t.Context(), "UPDATE admin_sessions SET issued_at=now()-interval '3 days',expires_at=now()-interval '2 days' WHERE revoked_at IS NOT NULL")
	require.NoError(t, err)
	// Use the supported cleaner, rather than leaving an expired row as a
	// permanent witness for the longer-lived native logout audit.
	require.NoError(t, h.CleanupOldData(t.Context(), 30))
	var remaining, logoutAudits, orphanLogoutAudits int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM admin_sessions),
		(SELECT count(*) FROM audit_logs WHERE action='logout'),
		(SELECT count(*) FROM audit_logs a WHERE action='logout' AND NOT EXISTS
		 (SELECT 1 FROM admin_sessions s WHERE s.id::text=a.resource_id))`).Scan(&remaining, &logoutAudits, &orphanLogoutAudits))
	require.Equal(t, 1, remaining, "only the expired revoked session is removed")
	require.Equal(t, 1, logoutAudits, "the recent native logout audit is retained")
	require.Equal(t, 1, orphanLogoutAudits)
	require.Equal(t, http.StatusUnauthorized, bootstrap(old, "cleaned-old-caller", true).Code)
	parallel := login("human-local-password")
	before = checkpointState()
	cleanedPreview := bootstrap(parallel, "parallel-preview", true)
	require.Equal(t, http.StatusOK, cleanedPreview.Code, cleanedPreview.Body.String())
	require.Equal(t, before, checkpointState())
	for _, tc := range []struct {
		name, assignment, nullableColumn string
		accept                           bool
	}{
		{name: "canonical_minimum", assignment: "resource_id='1'", accept: true},
		{name: "canonical_maximum", assignment: "resource_id='9223372036854775807'", accept: true},
		{name: "missing_operation", assignment: "request_id=''"},
		{name: "null_operation", assignment: "request_id=NULL", nullableColumn: "request_id"},
		{name: "whitespace_operation", assignment: "request_id=E' \\t\\r\\n '"},
		{name: "null_resource_id", assignment: "resource_id=NULL", nullableColumn: "resource_id"},
		{name: "empty_resource_id", assignment: "resource_id=''"},
		{name: "zero_resource_id", assignment: "resource_id='0'"},
		{name: "negative_resource_id", assignment: "resource_id='-1'"},
		{name: "leading_zero_resource_id", assignment: "resource_id='01'"},
		{name: "malformed_resource_id", assignment: "resource_id='not-a-session'"},
		{name: "decimal_resource_id", assignment: "resource_id='1.0'"},
		{name: "prefixed_resource_id", assignment: "resource_id='+1'"},
		{name: "whitespace_resource_id", assignment: "resource_id=' 1'"},
		{name: "overflow_resource_id", assignment: "resource_id='9223372036854775808'"},
		{name: "oversized_resource_id", assignment: "resource_id='100000000000000000000'"},
		{name: "wrong_source", assignment: "source='system'"},
		{name: "wrong_resource", assignment: "resource='user'"},
		{name: "wrong_action", assignment: "action='unexpected_history'"},
		{name: "wrong_detail", assignment: `detail='{"scope":"all_sessions"}'::jsonb`},
		{name: "wrong_before", assignment: `before_state='{"changed":true}'::jsonb`},
		{name: "wrong_after", assignment: `after_state='{}'::jsonb`},
		{name: "foreign_actor", assignment: "user_id=999999,actor_ref='user:999999'"},
		{name: "foreign_org", assignment: "org_id=999999"},
		{name: "project_scoped", assignment: "project_id=1"},
		{name: "retained_live_session", assignment: `resource_id=(SELECT id::text FROM admin_sessions WHERE revoked_at IS NULL ORDER BY id LIMIT 1)`},
	} {
		require.True(t, t.Run(tc.name, func(t *testing.T) {
			defer func() {
				restoreLogout(t)
				if tc.nullableColumn != "" {
					_, err := pool.Exec(t.Context(), "ALTER TABLE audit_logs ALTER COLUMN "+tc.nullableColumn+" SET NOT NULL")
					require.NoError(t, err)
				}
			}()
			if tc.nullableColumn != "" {
				// Transactional endpoints must also fail closed on SQL NULL, even
				// if a damaged fixture has lost the normal NOT NULL constraint.
				_, err := pool.Exec(t.Context(), "ALTER TABLE audit_logs ALTER COLUMN "+tc.nullableColumn+" DROP NOT NULL")
				require.NoError(t, err)
			}
			_, err := pool.Exec(t.Context(), "UPDATE audit_logs SET "+tc.assignment+" WHERE action='logout'")
			require.NoError(t, err)
			before := checkpointState()
			response := bootstrap(current, "history-"+tc.name, true)
			if tc.accept {
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				var result struct{ Checkpoint string }
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
				require.Equal(t, "fresh_seed", result.Checkpoint)
			} else {
				require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
				commit := bootstrap(current, "history-"+tc.name, false)
				require.Equal(t, http.StatusConflict, commit.Code, commit.Body.String())
			}
			require.Equal(t, before, checkpointState(), "preview and rejected commit must not mutate state")
		}))
	}
	require.True(t, t.Run("retained_foreign_session", func(t *testing.T) {
		var foreignUser int64
		require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO users
			(org_id,username,password_hash,organization_role,role,status,auth_provider)
			SELECT org_id,'foreign-history-fixture',password_hash,'org_admin','org_admin','active','local'
			FROM users WHERE id=1 RETURNING id`).Scan(&foreignUser))
		defer func() {
			restoreLogout(t)
			_, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id=$1`, foreignUser)
			require.NoError(t, err)
		}()
		var foreignSession string
		require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO admin_sessions
			(user_id,sid_hash,issued_at,expires_at,revoked_at)
			VALUES ($1,decode(repeat('ef',32),'hex'),NOW()-INTERVAL '1 hour',NOW()+INTERVAL '1 hour',NOW())
			RETURNING id::text`, foreignUser).Scan(&foreignSession))
		_, err := pool.Exec(t.Context(), `UPDATE audit_logs SET resource_id=$1 WHERE action='logout'`, foreignSession)
		require.NoError(t, err)
		before := checkpointState()
		for _, dry := range []bool{true, false} {
			response := bootstrap(current, "foreign-retained-session", dry)
			require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
			require.Equal(t, before, checkpointState())
		}
	}))
	before = checkpointState()
	finalPreview := bootstrap(current, "reviewed-bootstrap", true)
	require.Equal(t, http.StatusOK, finalPreview.Code, finalPreview.Body.String())
	require.Equal(t, before, checkpointState())
	before = preserved()
	applied := bootstrap(current, "reviewed-bootstrap", false)
	require.Equal(t, http.StatusCreated, applied.Code, applied.Body.String())
	require.Equal(t, before, preserved())
	replay := bootstrap(current, "reviewed-bootstrap", false)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.Equal(t, before, preserved())
	var replayed struct{ Idempotent bool }
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &replayed))
	require.True(t, replayed.Idempotent)
	var orgs, wallets, projects int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM orgs),(SELECT count(*) FROM wallets),(SELECT count(*) FROM projects)").Scan(&orgs, &wallets, &projects))
	require.Equal(t, 2, orgs)
	require.Equal(t, 1, wallets)
	require.Zero(t, projects)
	require.Equal(t, http.StatusConflict, bootstrap(current, "must-not-create-second", false).Code)
}
