//go:build integration

package sourcefacts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

var contentRestoreTables = []string{
	"execution_content_streams", "execution_source_content_scopes",
	"execution_source_content_bindings", "execution_source_content_operations",
	"execution_content_policies", "execution_content_manifests",
	"execution_content_objects", "execution_content_health",
}

func contentRestoreHelper(t *testing.T) string {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "..", "scripts", "source-budget-restore-rebase.sql"))
	require.NoError(t, err)
	return string(wire)
}

// Compare exact retained rows without printing credential or storage bindings.
func contentRestoreSnapshot(t *testing.T, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, table := range append(append([]string{}, contentRestoreTables...), "api_keys", "execution_source_credentials", "execution_source_connections") {
		value := "to_jsonb(r)"
		if table == "execution_content_streams" {
			value += "-'recovery_epoch'"
		}
		var digest string
		require.NoError(t, pool.QueryRow(t.Context(), "SELECT encode(sha256(convert_to(COALESCE(jsonb_agg(v ORDER BY v)::text,'[]'),'UTF8')),'hex') FROM (SELECT "+value+" AS v FROM "+table+" r) s").Scan(&digest))
		result[table] = digest
	}
	return result
}

func TestContentActualBackupRestoreEpochAndBodiesPG(t *testing.T) {
	toolsDir := os.Getenv("SOURCE_BUDGET_BACKUP_TOOLS_DIR")
	if toolsDir == "" {
		t.Skip("actual SQL/body restore requires the matching PostgreSQL client directory")
	}
	require.True(t, filepath.IsAbs(toolsDir))
	dumpTool, err := exec.LookPath(filepath.Join(toolsDir, "pg_dump"))
	require.NoError(t, err)
	psqlTool, err := exec.LookPath(filepath.Join(toolsDir, "psql"))
	require.NoError(t, err)
	p := setupCaptureServicePG(t, contentcapture.ModeFullContent, 1024)
	body := []byte{0, 255, 'r', 'e', 's', 't', 'o', 'r', 'e', '\n'}
	capture, err := p.service.Begin(t.Context(), p.identity)
	require.NoError(t, err)
	require.NoError(t, capture.Offer(t.Context(), body))
	require.NoError(t, capture.Finish("none"))
	completed := waitCaptureService(t, capture)
	require.NoError(t, completed.FinalizeError)
	require.NotNil(t, completed.Outcome.Receipt)
	// The actual capture owner drains before the paired snapshot is taken.
	require.NoError(t, p.service.Close(t.Context()))
	m := p.manifests(t)[1]
	require.Equal(t, "complete", m.Capture.Completeness)
	f := &contentBodyPGFixture{pool: p.pool, source: p.service.source, org: p.identity.OrgID, project: p.identity.ProjectID, store: p.service.files, receipt: *completed.Outcome.Receipt}
	_, err = p.pool.Exec(t.Context(), `INSERT INTO execution_source_connections(org_id,project_id,source_instance,status,profiles) VALUES($1,$2,$3,'active',ARRAY[]::text[])`, f.org, f.project, f.source.SourceInstance())
	require.NoError(t, err)
	_, err = p.pool.Exec(t.Context(), `INSERT INTO execution_source_content_scopes(org_id,project_id,status,state_version,access_generation) VALUES($1,$2,'active',1,1)`, f.org, f.project)
	require.NoError(t, err)
	_, raw := insertContentBodyPGKey(t, f, "a", "content", "feed_active", 0)
	f.request = httptest.NewRequest(http.MethodGet, "http://example.invalid/content", nil)
	f.request.RemoteAddr = "127.0.0.1:1234"
	f.request.Header.Set("Authorization", "Bearer "+raw)
	f.authority, err = f.source.NewContentBodyAuthority(middleware.ClientIPResolver{})
	require.NoError(t, err)
	readConfig := contentReadTestConfig(f.authority)
	readConfig.Store = f.store
	reads, err := NewContentReadHandlers(readConfig)
	require.NoError(t, err)
	beforePage := readTestPage(t, contentReadPGRequest(f, reads.Manifests, "purpose=human_review&limit=10"))
	require.Len(t, beforePage.Items, 2)
	require.Equal(t, "available", beforePage.Items[1].Availability.State)
	snapshot := contentRestoreSnapshot(t, p.pool)
	dump := filepath.Join(t.TempDir(), "synthetic-content.sql")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := testutil.PostgresToolCommand(t, ctx, dumpTool, p.pool.Config(), "--no-owner", "--no-privileges", "--file", dump)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "pg_dump: %s", output)
	seen := map[int64]bool{1: true}
	for _, paired := range []bool{false, true} {
		t.Run(map[bool]string{false: "pg_only", true: "paired"}[paired], func(t *testing.T) {
			restored := testutil.SetupIsolatedTestDB(t)
			command := testutil.PostgresToolCommand(t, ctx, psqlTool, restored.Config(), "--no-psqlrc", "--set", "ON_ERROR_STOP=1", "--file", dump)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "psql restore: %s", output)
			require.Equal(t, snapshot, contentRestoreSnapshot(t, restored))
			root, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			// Copy the original marker; initializing a different binding would
			// test corrupt/unconfigured storage, not a PG-only missing object.
			copyNames := []string{".unirouter-content-store"}
			if paired {
				copyNames = append(copyNames, f.receipt.ObjectName)
			}
			for _, name := range copyNames {
				wire, err := os.ReadFile(filepath.Join(p.root, name))
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(root, name), wire, 0600))
			}
			files, err := contentcapture.OpenFileStore(root, f.receipt.Binding)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, files.Close()) })
			source, err := New(restored, p.service.source.options)
			require.NoError(t, err)
			authority, err := source.NewContentBodyAuthority(middleware.ClientIPResolver{})
			require.NoError(t, err)
			rf := &contentBodyPGFixture{pool: restored, source: source, authority: authority, store: files, request: f.request.Clone(t.Context())}
			config := contentReadTestConfig(authority)
			config.Store = files
			rh, err := NewContentReadHandlers(config)
			require.NoError(t, err)
			selector := ContentBodySelector{RequestID: m.RequestID, SourceFactID: m.SourceFactID, Revision: m.Revision, ContentRef: m.ContentRef, Purpose: ContentHumanReview}
			backupGrant, failure := authority.Authenticate(t.Context(), rf.request)
			require.Nil(t, failure)
			oldGrants := []ContentBodyGrant{backupGrant}
			oldCursors := []string{beforePage.NextCursor}
			for pass := 0; pass < 2; pass++ {
				_, err = restored.Exec(t.Context(), contentRestoreHelper(t))
				require.NoError(t, err)
				var epoch int64
				require.NoError(t, restored.QueryRow(t.Context(), `SELECT recovery_epoch FROM execution_content_streams WHERE org_id=$1 AND project_id=$2`, f.org, f.project).Scan(&epoch))
				require.Positive(t, epoch)
				require.False(t, seen[epoch], "restore reused an observed recovery epoch")
				seen[epoch] = true
				for _, grant := range oldGrants {
					_, failure = authority.Reauthorize(t.Context(), rf.request, grant, selector)
					requireAuthorityFailure(t, failure, 409, "source_epoch_changed")
				}
				for _, cursor := range oldCursors {
					w := contentReadPGRequest(rf, rh.Manifests, "purpose=human_review&limit=10&cursor="+url.QueryEscape(cursor))
					require.Equal(t, 409, w.Code)
					require.Contains(t, w.Body.String(), "source_epoch_changed")
				}
				page := readTestPage(t, contentReadPGRequest(rf, rh.Manifests, "purpose=human_review&limit=10"))
				require.Equal(t, strconv.FormatInt(epoch, 10), page.RecoveryEpoch)
				require.Equal(t, m, page.Items[1].Document)
				require.Equal(t, map[bool]string{false: "missing", true: "available"}[paired], page.Items[1].Availability.State)
				grant, failure := authority.Authenticate(t.Context(), rf.request)
				require.Nil(t, failure)
				oldGrants = append(oldGrants, grant)
				oldCursors = append(oldCursors, page.NextCursor)
				require.Equal(t, snapshot, contentRestoreSnapshot(t, restored))
			}
			bh := mustBodyHandler(t, ContentBodyHandlerConfig{Authority: authority, Store: files, MaxReadBytes: 1024, MaxBufferBytes: 3, MaxConcurrentReads: 1, RequestTimeout: time.Second, WriteTimeout: time.Second})
			query := url.Values{"request_id": {m.RequestID}, "source_fact_id": {m.SourceFactID}, "revision": {m.Revision}, "purpose": {ContentHumanReview}}
			rf.request.URL.Path = ContentBodyPathPrefix + m.ContentRef
			w := contentReadPGRequest(rf, bh, query.Encode())
			if !paired {
				require.Equal(t, 503, w.Code)
				require.Equal(t, "content_missing", bodyErrorCode(t, w.ResponseRecorder))
			} else {
				require.Equal(t, 200, w.Code)
				require.Equal(t, body, w.Body.Bytes())
				metadata, err := DecodeContentReadMetadataHeader(w.Header().Get("X-UR-Content-Metadata"))
				require.NoError(t, err)
				wantMetadata, err := m.ReadMetadata()
				require.NoError(t, err)
				require.Equal(t, wantMetadata, metadata)
				// Damage only this task-created restored copy, never the source.
				require.NoError(t, os.WriteFile(filepath.Join(root, f.receipt.ObjectName), []byte("damaged"), 0600))
				w = contentReadPGRequest(rf, bh, query.Encode())
				require.Equal(t, 503, w.Code)
				require.Equal(t, "content_corrupt", bodyErrorCode(t, w.ResponseRecorder))
				page := readTestPage(t, contentReadPGRequest(rf, rh.Manifests, "purpose=human_review&limit=10"))
				require.Equal(t, "corrupt", page.Items[1].Availability.State)
				require.Equal(t, m, page.Items[1].Document)
			}
			require.Equal(t, snapshot, contentRestoreSnapshot(t, restored))
		})
	}
	require.Equal(t, snapshot, contentRestoreSnapshot(t, p.pool))
	var originalEpoch int64
	require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT recovery_epoch FROM execution_content_streams WHERE org_id=$1 AND project_id=$2`, f.org, f.project).Scan(&originalEpoch))
	require.EqualValues(t, 1, originalEpoch)
	file, err := p.service.files.OpenVerified(t.Context(), f.receipt, contentcapture.StoreLimits{MaxSavedBytes: 1024, MaxReadBytes: 1024, MaxBufferBytes: 3})
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

func runContentRestoreReject(t *testing.T, pool *pgxpool.Pool, want string) {
	t.Helper()
	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(t.Context(), contentRestoreHelper(t))
	require.ErrorContains(t, err, want)
	_, err = conn.Exec(t.Context(), "ROLLBACK")
	require.NoError(t, err)
}

func TestContentRestoreRejectsPartialSchemaAtomicallyPG(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	for _, table := range contentRestoreTables {
		t.Run(table, func(t *testing.T) {
			before := budgetCoverageRows(t, pool)
			_, err := pool.Exec(t.Context(), "ALTER TABLE "+table+" RENAME TO synthetic_missing_content")
			require.NoError(t, err)
			runContentRestoreReject(t, pool, "restored C schema is incomplete")
			require.Equal(t, before, budgetCoverageRows(t, pool))
			_, err = pool.Exec(t.Context(), "ALTER TABLE synthetic_missing_content RENAME TO "+table)
			require.NoError(t, err)
		})
	}
}

func TestContentRestoreRejectsPreDContaminationPG(t *testing.T) {
	for _, fault := range []string{"content_table", "future_ledger"} {
		t.Run(fault, func(t *testing.T) {
			pool := testutil.SetupPreRetirementTestDB(t)
			require.NoError(t, budgetDown(t, pool))
			if fault == "content_table" {
				_, err := pool.Exec(t.Context(), "CREATE TABLE execution_content_health(synthetic integer)")
				require.NoError(t, err)
				runContentRestoreReject(t, pool, "restored C tables require the exact M119 ledger")
			} else {
				_, err := pool.Exec(t.Context(), "INSERT INTO schema_migrations(version) VALUES('000120_unknown_future')")
				require.NoError(t, err)
				runContentRestoreReject(t, pool, "restored D schema is incomplete")
			}
		})
	}
}

func TestContentRestoreRollsBackAfterEpochWritePG(t *testing.T) {
	pool, _, org, project := setupContentJournal(t)
	before := budgetCoverageRows(t, pool)
	_, err := pool.Exec(t.Context(), `CREATE FUNCTION synthetic_reject_content_restore() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
IF NEW.recovery_epoch=OLD.recovery_epoch OR NOT EXISTS(SELECT 1 FROM execution_source_budget_coverage WHERE reason='restore_rebase' AND ended_at IS NULL) THEN
RAISE EXCEPTION 'restore writes did not occur'; END IF;
RAISE EXCEPTION 'synthetic failure after D and C writes'; END $$;
CREATE TRIGGER synthetic_reject_content_restore AFTER UPDATE ON execution_content_streams FOR EACH ROW EXECUTE FUNCTION synthetic_reject_content_restore()`)
	require.NoError(t, err)
	runContentRestoreReject(t, pool, "synthetic failure after D and C writes")
	require.Equal(t, before, budgetCoverageRows(t, pool))
	var epoch int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT recovery_epoch FROM execution_content_streams WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&epoch))
	require.EqualValues(t, 1, epoch)
}
