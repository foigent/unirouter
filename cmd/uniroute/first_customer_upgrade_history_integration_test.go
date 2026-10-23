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

func TestFreshCustomerAfterNativePasswordHistoryAndSignedUpgradeIntegration(t *testing.T) {
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
	h.Logout(logoutResponse, logout)
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
	before := preserved()
	preview := bootstrap(current, "reviewed-bootstrap", true)
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	require.Equal(t, before, preserved())
	var reviewed map[string]any
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &reviewed))
	require.Equal(t, "fresh_seed", reviewed["checkpoint"])
	_, err = pool.Exec(t.Context(), "UPDATE admin_sessions SET issued_at=now()-interval '3 days',expires_at=now()-interval '2 days' WHERE revoked_at IS NOT NULL")
	require.NoError(t, err)
	parallel := login("human-local-password")
	before = preserved()
	require.Equal(t, http.StatusOK, bootstrap(parallel, "parallel-preview", true).Code)
	require.Equal(t, before, preserved())
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
