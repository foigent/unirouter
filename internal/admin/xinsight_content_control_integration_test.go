//go:build integration

package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

// These tests require the real, isolated M119 database installed by the existing
// fixture. Compiling them alone is not evidence that any PG behavior passed.
func newContentControlIntegrationFixture(t *testing.T) sourceControlFixture {
	t.Helper()
	f := newSourceControlFixture(t)
	var err error
	f.source, err = sourcefacts.New(f.pool, sourcefacts.Options{Enabled: true, ContentEnabled: true, SourceInstance: "synthetic-source-control", CursorSecret: syntheticSourceControlSecret})
	require.NoError(t, err)
	require.NoError(t, f.source.CheckReady(t.Context()))
	f.handler.SetSourceFactsController(f.source)
	return f
}

func contentIntegrationOperation(label string) string {
	sum := sha256.Sum256([]byte("synthetic-content-control-operation:" + label))
	return hex.EncodeToString(sum[:16])
}

func contentIntegrationPrepare(t *testing.T, f sourceControlFixture, label string) (contentControlRequest, string) {
	t.Helper()
	registration, token := f.registration(t, label, []string{sourcefacts.ContentProfile})
	return contentControlRequest{OperationID: registration.OperationID, ExpectedStateVersion: "0", Registration: &registration, AllowedIP: "127.0.0.1/32"}, token
}

func contentIntegrationTarget(label, version string, target contentControlResult) contentControlRequest {
	return contentControlRequest{OperationID: contentIntegrationOperation(label), ExpectedStateVersion: version, CredentialID: target.CredentialID, RegistrationHash: target.RegistrationHash}
}

func contentIntegrationHTTP(t *testing.T, f sourceControlFixture, action string, input contentControlRequest, status int) contentControlResult {
	t.Helper()
	r := f.request(t, action, input) // Includes the original committing actor/session context.
	r.URL.Path = "/api/v1/data-exchange/xinsight/source-connections/" + stringInt64(f.projectID) + "/content/" + action
	w := httptest.NewRecorder()
	f.handler.ContentControl(action)(w, r)
	require.Equal(t, status, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NotContains(t, w.Body.String(), "verifier_sha256")
	require.NotContains(t, w.Body.String(), syntheticSourceControlSecret)
	var result contentControlResult
	if status == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	} else {
		var failure map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &failure))
		require.NotEmpty(t, failure["error"])
	}
	return result
}

func contentIntegrationLegacy(t *testing.T, f sourceControlFixture, label string) (sourcefacts.Registration, string) {
	t.Helper()
	reg, token := f.registration(t, label, []string{sourcefacts.ContextProfile})
	staged := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}, http.StatusOK)
	f.control(t, "activate", map[string]any{"credential_id": staged["credential_id"]}, http.StatusOK)
	f.control(t, "resume", map[string]any{}, http.StatusOK)
	return reg, token
}

// Compare complete persisted rows, including audit and timestamps. PostgreSQL
// sequence allocations may have gaps after rollback and are intentionally not
// treated as committed business state.
func contentIntegrationSnapshot(t *testing.T, f sourceControlFixture, legacyOnly bool) map[string]string {
	t.Helper()
	tables := []string{"execution_source_connections", "api_keys", "execution_source_credentials"}
	if !legacyOnly {
		tables = append(tables, "execution_content_streams", "execution_source_content_scopes", "execution_source_content_bindings", "execution_source_content_operations", "audit_logs")
	}
	result := make(map[string]string, len(tables))
	for _, table := range tables {
		predicate := "org_id=$1 AND project_id=$2"
		if legacyOnly && (table == "api_keys" || table == "execution_source_credentials") {
			predicate += " AND source_lifecycle_scope='legacy'"
		}
		query := `SELECT COALESCE(jsonb_agg(row_value ORDER BY row_value::text),'[]'::jsonb)::text FROM (SELECT to_jsonb(t) AS row_value FROM ` + pgx.Identifier{table}.Sanitize() + ` t WHERE ` + predicate + `) rows`
		var value string
		require.NoError(t, f.pool.QueryRow(t.Context(), query, f.orgID, f.projectID).Scan(&value))
		result[table] = value
	}
	return result
}

func contentIntegrationState(t *testing.T, f sourceControlFixture) contentControlState {
	t.Helper()
	tx, err := f.pool.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(context.Background())
	state, err := readContentStateTx(t.Context(), tx, f.orgID, f.projectID, "synthetic-source-control", false)
	require.NoError(t, err)
	return state
}

func TestContentControlPrepareActivateReplayAndLegacyIsolationIntegration(t *testing.T) {
	f := newContentControlIntegrationFixture(t)
	_, legacyToken := contentIntegrationLegacy(t, f, "content-isolation-legacy")
	path := sourceReadPath + "?profile=" + sourcefacts.ContextProfile
	boot := sourcePage(t, f.read(t, legacyToken, path, "127.0.0.1:3000", http.StatusOK))
	require.NotEmpty(t, boot.NextCursor)
	beforeLegacy := contentIntegrationSnapshot(t, f, true)
	prepare, contentToken := contentIntegrationPrepare(t, f, "content-isolation")
	staged := contentIntegrationHTTP(t, f, "prepare", prepare, http.StatusOK)
	require.Equal(t, "1", staged.StateVersion)
	require.Equal(t, "1", staged.ContentScopeRevision)
	require.Equal(t, []string{"human_review"}, staged.AllowedPurposes)
	require.NotNil(t, staged.CoverageFrom)
	state := contentIntegrationState(t, f)
	require.Nil(t, state.active)
	require.NotNil(t, state.staged)
	require.False(t, contentAccessProjection(state).Enabled)
	afterPrepare := contentIntegrationSnapshot(t, f, false)
	// Expected version remains zero: exact replay must precede CAS.
	require.Equal(t, staged, contentIntegrationHTTP(t, f, "prepare", prepare, http.StatusOK))
	require.Equal(t, afterPrepare, contentIntegrationSnapshot(t, f, false))
	activate := contentIntegrationTarget("isolation-activate", "1", staged)
	active := contentIntegrationHTTP(t, f, "activate", activate, http.StatusOK)
	require.Equal(t, "2", active.StateVersion)
	require.Equal(t, "2", active.ContentScopeRevision)
	require.True(t, contentAccessProjection(contentIntegrationState(t, f)).Enabled)
	afterActivate := contentIntegrationSnapshot(t, f, false)
	require.Equal(t, active, contentIntegrationHTTP(t, f, "activate", activate, http.StatusOK))
	require.Equal(t, afterActivate, contentIntegrationSnapshot(t, f, false))
	require.Equal(t, beforeLegacy, contentIntegrationSnapshot(t, f, true))
	// The C credential is never admitted by the legacy source reader.
	f.read(t, contentToken, path, "127.0.0.1:3000", http.StatusUnauthorized)
	f.capture(t, "synthetic-after-content-activation")
	page := sourcePage(t, f.read(t, legacyToken, path+"&cursor="+url.QueryEscape(boot.NextCursor), "127.0.0.1:3000", http.StatusOK))
	require.Len(t, page.Events, 1)
	// Local safety controls leave the global parent and legacy credential intact.
	beforeSafety := contentIntegrationSnapshot(t, f, true)
	paused := contentIntegrationHTTP(t, f, "pause", contentIntegrationTarget("local-pause", "2", active), http.StatusOK)
	require.Equal(t, "3", paused.ContentScopeRevision)
	require.False(t, contentAccessProjection(contentIntegrationState(t, f)).Enabled)
	resumed := contentIntegrationHTTP(t, f, "resume", contentIntegrationTarget("local-resume", "3", active), http.StatusOK)
	require.Equal(t, "3", resumed.ContentScopeRevision)
	contentIntegrationHTTP(t, f, "revoke", contentIntegrationTarget("local-revoke", "4", active), http.StatusOK)
	require.Equal(t, beforeSafety, contentIntegrationSnapshot(t, f, true))
	require.Equal(t, "revoked", contentIntegrationState(t, f).local)
	f.read(t, legacyToken, path, "127.0.0.1:3000", http.StatusOK)
}

func TestContentControlRejectsWrongTargetOperationAndCASIntegration(t *testing.T) {
	f := newContentControlIntegrationFixture(t)
	legacy, _ := contentIntegrationLegacy(t, f, "wrong-target-legacy")
	prepare, _ := contentIntegrationPrepare(t, f, "wrong-target")
	staged := contentIntegrationHTTP(t, f, "prepare", prepare, http.StatusOK)
	valid := contentIntegrationTarget("wrong-target-activate", "1", staged)
	wrongID := valid
	wrongID.CredentialID = "9223372036854775807"
	wrongHash := valid
	wrongHash.RegistrationHash = "sha256:" + strings.Repeat("0", 64)
	stale := valid
	stale.ExpectedStateVersion = "0"
	wrongActionOperation := valid
	wrongActionOperation.OperationID = prepare.OperationID
	changedReplay := prepare
	changedReplay.AllowedIP = "127.0.0.2/32"
	for _, tc := range []struct {
		name, action string
		request      contentControlRequest
	}{
		{"wrong credential", "activate", wrongID}, {"wrong registration hash", "activate", wrongHash},
		{"stale CAS", "activate", stale}, {"same operation different action", "activate", wrongActionOperation},
		{"same operation different request", "prepare", changedReplay},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := contentIntegrationSnapshot(t, f, false)
			contentIntegrationHTTP(t, f, tc.action, tc.request, http.StatusConflict)
			require.Equal(t, before, contentIntegrationSnapshot(t, f, false))
		})
	}
	active := contentIntegrationHTTP(t, f, "activate", valid, http.StatusOK)
	rotateReg, _ := f.registration(t, "cross-scope-operation", []string{sourcefacts.ContentProfile})
	rotateReg.OperationID = legacy.OperationID
	collision := contentIntegrationTarget("unused", "2", active)
	collision.OperationID = legacy.OperationID
	collision.Registration = &rotateReg
	before := contentIntegrationSnapshot(t, f, false)
	contentIntegrationHTTP(t, f, "rotate", collision, http.StatusConflict)
	require.Equal(t, before, contentIntegrationSnapshot(t, f, false))
}

func TestContentControlGlobalPauseResumeInvalidatesReplayIntegration(t *testing.T) {
	f := newContentControlIntegrationFixture(t)
	contentIntegrationLegacy(t, f, "global-replay-legacy")
	prepare, _ := contentIntegrationPrepare(t, f, "global-replay")
	staged := contentIntegrationHTTP(t, f, "prepare", prepare, http.StatusOK)
	activate := contentIntegrationTarget("global-replay-activate", "1", staged)
	contentIntegrationHTTP(t, f, "activate", activate, http.StatusOK)
	original := contentIntegrationState(t, f)
	for _, action := range []string{"pause", "resume"} {
		f.control(t, action, map[string]any{}, http.StatusOK)
		state := contentIntegrationState(t, f)
		require.Greater(t, state.safetyVersion, original.safetyVersion)
		require.Equal(t, original.version, state.version)
		require.Equal(t, original.generation, state.generation)
		require.Equal(t, action == "resume", contentAccessProjection(state).Enabled)
		before := contentIntegrationSnapshot(t, f, false)
		contentIntegrationHTTP(t, f, "activate", activate, http.StatusConflict)
		require.Equal(t, before, contentIntegrationSnapshot(t, f, false))
		original = state
	}
}

func TestContentControlRotateInheritsApprovedCoverageAndIPIntegration(t *testing.T) {
	f := newContentControlIntegrationFixture(t)
	contentIntegrationLegacy(t, f, "rotation-legacy")
	approvedTime := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	_, err := f.pool.Exec(t.Context(), `INSERT INTO execution_content_streams(org_id,project_id,source_instance,high_sequence,retained_after_sequence,retained_after_time) VALUES($1,$2,'synthetic-source-control',7,3,$3)`, f.orgID, f.projectID, approvedTime)
	require.NoError(t, err)
	prepare, _ := contentIntegrationPrepare(t, f, "rotation-original")
	prepare.AllowedIP = "192.0.2.10/32"
	staged := contentIntegrationHTTP(t, f, "prepare", prepare, http.StatusOK)
	require.Equal(t, "3", staged.CoverageFrom.AfterSequence)
	active := contentIntegrationHTTP(t, f, "activate", contentIntegrationTarget("rotation-first-activate", "1", staged), http.StatusOK)
	_, err = f.pool.Exec(t.Context(), `UPDATE execution_content_streams SET high_sequence=20,retained_after_sequence=10,retained_after_time=$3 WHERE org_id=$1 AND project_id=$2`, f.orgID, f.projectID, approvedTime.Add(time.Hour))
	require.NoError(t, err)
	beforeLegacy := contentIntegrationSnapshot(t, f, true)
	nextReg, _ := f.registration(t, "rotation-next", []string{sourcefacts.ContentProfile})
	rotate := contentIntegrationTarget("unused-rotation", "2", active)
	rotate.OperationID, rotate.Registration = nextReg.OperationID, &nextReg
	replacement := contentIntegrationHTTP(t, f, "rotate", rotate, http.StatusOK)
	require.NotEqual(t, active.CredentialID, replacement.CredentialID)
	require.Equal(t, active.CoverageFrom, replacement.CoverageFrom)
	require.Equal(t, active.AllowedPurposes, replacement.AllowedPurposes)
	require.Equal(t, active.ContentScopeRevision, replacement.ContentScopeRevision)
	state := contentIntegrationState(t, f)
	require.Equal(t, active.CredentialID, state.active.CredentialID)
	require.Equal(t, "192.0.2.10/32", state.active.ips)
	require.Equal(t, state.active.ips, state.staged.ips)
	contentIntegrationHTTP(t, f, "activate", contentIntegrationTarget("rotation-next-activate", "3", replacement), http.StatusOK)
	state = contentIntegrationState(t, f)
	require.Nil(t, state.staged)
	require.Equal(t, replacement.CredentialID, state.active.CredentialID)
	var oldStatus string
	oldID, ok := contentControlNumber(active.CredentialID, false)
	require.True(t, ok)
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT status FROM api_keys WHERE id=$1`, oldID).Scan(&oldStatus))
	require.Equal(t, "feed_revoked", oldStatus)
	require.Equal(t, beforeLegacy, contentIntegrationSnapshot(t, f, true))
}

// The names below identify only resources owned by this test in its isolated DB.
func contentIntegrationFailureSQL(t *testing.T, table string) (string, string, string) {
	t.Helper()
	var suffix [8]byte
	_, err := rand.Read(suffix[:])
	require.NoError(t, err)
	name := "content_test_failure_" + hex.EncodeToString(suffix[:])
	function := pgx.Identifier{name}.Sanitize()
	target := pgx.Identifier{table}.Sanitize()
	createFunction := `CREATE FUNCTION ` + function + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic content transaction failure'; END $$`
	predicate := ""
	if table == "audit_logs" {
		predicate = " WHEN (NEW.resource='xinsight_source_content')"
	}
	createTrigger := `CREATE TRIGGER ` + function + ` BEFORE INSERT ON ` + target + ` FOR EACH ROW` + predicate + ` EXECUTE FUNCTION ` + function + `()`
	return name, createFunction, createTrigger
}

func TestContentControlOperationAndAuditFailureRollbackIntegration(t *testing.T) {
	for _, table := range []string{"execution_source_content_operations", "audit_logs"} {
		t.Run(table, func(t *testing.T) {
			f := newContentControlIntegrationFixture(t)
			input, _ := contentIntegrationPrepare(t, f, "rollback-"+table)
			r := f.request(t, "prepare", input)
			before := contentIntegrationSnapshot(t, f, false)
			tx, err := f.pool.Begin(r.Context())
			require.NoError(t, err)
			defer tx.Rollback(context.Background())
			name, createFunction, createTrigger := contentIntegrationFailureSQL(t, table)
			_, err = tx.Exec(r.Context(), createFunction)
			require.NoError(t, err)
			_, err = tx.Exec(r.Context(), createTrigger)
			require.NoError(t, err)
			_, err = f.handler.contentControlTx(r.Context(), tx, f.orgID, f.projectID, "prepare", input, r)
			require.ErrorContains(t, err, "synthetic content transaction failure")
			require.NoError(t, tx.Rollback(r.Context()))
			require.Equal(t, before, contentIntegrationSnapshot(t, f, false))
			var remains bool
			require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgname=$1)`, name).Scan(&remains))
			require.False(t, remains)
			// A rolled-back attempt may be retried with its same operation/registration.
			contentIntegrationHTTP(t, f, "prepare", input, http.StatusOK)
		})
	}
}

func TestContentControlHTTPAuditFailureRollsBackWithRealActorIntegration(t *testing.T) {
	f := newContentControlIntegrationFixture(t)
	input, _ := contentIntegrationPrepare(t, f, "http-audit-rollback")
	before := contentIntegrationSnapshot(t, f, false)
	name, createFunction, createTrigger := contentIntegrationFailureSQL(t, "audit_logs")
	quoted := pgx.Identifier{name}.Sanitize()
	_, err := f.pool.Exec(t.Context(), createFunction)
	require.NoError(t, err)
	// Registered after fixture cleanup, so these exact test resources are removed
	// before the isolated pool/database is closed, including on assertion failure.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, cleanupErr := f.pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+quoted+` ON audit_logs`)
		require.NoError(t, cleanupErr)
		_, cleanupErr = f.pool.Exec(ctx, `DROP FUNCTION IF EXISTS `+quoted+`()`)
		require.NoError(t, cleanupErr)
	})
	_, err = f.pool.Exec(t.Context(), createTrigger)
	require.NoError(t, err)
	contentIntegrationHTTP(t, f, "prepare", input, http.StatusInternalServerError)
	require.Equal(t, before, contentIntegrationSnapshot(t, f, false))
	_, err = f.pool.Exec(t.Context(), `DROP TRIGGER `+quoted+` ON audit_logs`)
	require.NoError(t, err)
	contentIntegrationHTTP(t, f, "prepare", input, http.StatusOK)
}
