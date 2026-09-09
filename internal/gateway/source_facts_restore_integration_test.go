//go:build integration

package gateway_test

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
)

// This optional local recovery gate requires the matching PostgreSQL tools.
// Both databases come from testutil; no existing database is restored over.
// Receiver credential lifecycle and authenticated cursor recovery have their
// own acceptance cases and are not claimed by this financial/source restore.
func TestSourceFactsPGBackupRestorePreservesIdentityAndPendingJournalIntegration(t *testing.T) {
	bin := os.Getenv("SOURCE_FACTS_PG_BIN")
	if bin == "" {
		t.Skip("SOURCE_FACTS_PG_BIN must select the matching local pg_dump/pg_restore")
	}
	f := gateway.NewSourceFactsGatewayFixture(t, modeloperation.IDResponses)
	source, journal, raw := sourceGatewayAttachSource(t, f)
	handler := sourceGatewayHandler(t, f, f.Relay, source)
	invoke := func() string {
		r := httptest.NewRequest(http.MethodPost, f.Path, strings.NewReader(string(f.Body)))
		r.Header.Set("Authorization", "Bearer "+raw)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w.Header().Get(middleware.OperationIDHeader)
	}
	first := invoke()
	_, err := journal.DrainOnce(t.Context())
	require.NoError(t, err)
	removed, err := source.DeleteExpired(t.Context(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 7, removed)
	pending := invoke() // Financial settlement is durable; journal is still pending.
	var pendingCount int
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT count(*) FROM internal_outbox_events WHERE request_id=$1`, pending).Scan(&pendingCount))
	require.Positive(t, pendingCount)
	var sourceHigh int64
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT high_sequence FROM execution_source_streams WHERE profile=$1`, sourcefacts.ContextProfile).Scan(&sourceHigh))
	before := sourceRecoveryTableDigests(t, f.Pool)
	var markerDefinition string
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT pg_get_functiondef('mark_micro_ledger_runtime_evidence()'::regprocedure)`).Scan(&markerDefinition))
	require.Contains(t, markerDefinition, "NOT micro_evidence_written OR first_evidence_at IS NULL")
	archive := filepath.Join(t.TempDir(), "synthetic-source.pgdump")
	command := exec.CommandContext(t.Context(), filepath.Join(bin, "pg_dump"), "--format=custom", "--no-owner", "--no-acl", "--file", archive, "--dbname", f.Pool.Config().ConnString())
	output, err := command.CombinedOutput()
	require.NoError(t, err, "pg_dump: %s", output)

	// Advance the live temporary database beyond the backup. A restored high
	// watermark must remain behind this position, never jump silently to it.
	late := invoke()
	_, err = journal.DrainOnce(t.Context())
	require.NoError(t, err)
	var laterHigh int64
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT high_sequence FROM execution_source_streams WHERE profile=$1`, sourcefacts.ContextProfile).Scan(&laterHigh))
	require.Greater(t, laterHigh, sourceHigh)
	restored := testutil.SetupIsolatedTestDB(t)
	command = exec.CommandContext(t.Context(), filepath.Join(bin, "pg_restore"), "--exit-on-error", "--single-transaction", "--no-owner", "--no-acl", "--dbname", restored.Config().ConnString(), archive)
	output, err = command.CombinedOutput()
	require.NoError(t, err, "pg_restore: %s", output)
	require.Equal(t, before, sourceRecoveryTableDigests(t, restored), "all source/financial/recovery bytes must survive the actual archive")
	var restoredMarkerDefinition string
	require.NoError(t, restored.QueryRow(t.Context(), `SELECT pg_get_functiondef('mark_micro_ledger_runtime_evidence()'::regprocedure)`).Scan(&restoredMarkerDefinition))
	require.Equal(t, markerDefinition, restoredMarkerDefinition, "the archive must preserve the actual M116 marker function")
	var restoredHigh int64
	require.NoError(t, restored.QueryRow(t.Context(), `SELECT high_sequence FROM execution_source_streams WHERE profile=$1`, sourcefacts.ContextProfile).Scan(&restoredHigh))
	require.Equal(t, sourceHigh, restoredHigh)

	resumed, err := sourcefacts.New(restored, sourcefacts.Options{Enabled: true, SourceInstance: source.SourceInstance(), CursorSecret: sourceGatewaySyntheticSecret, I01Enabled: true})
	require.NoError(t, err)
	require.NoError(t, resumed.CheckReady(t.Context()))
	v1, err := completedfeed.NewService(restored, sourceGatewaySyntheticSecret, source.SourceInstance(), true)
	require.NoError(t, err)
	b := billing.NewBilling(restored, nil)
	b.SetTerminalMaterializer(billing.TerminalMaterializers{v1, resumed})
	worker, err := outbox.New(restored, outbox.DefaultOptions())
	require.NoError(t, err)
	b.SetOutbox(worker)
	for _, kind := range []string{outbox.EventBillingUsagePrepared, outbox.EventBillingSettled, outbox.EventBillingCancelled} {
		require.NoError(t, worker.RegisterHandler(kind, b.ProcessUsageJournalEvent))
	}
	_, err = worker.DrainOnce(t.Context())
	require.NoError(t, err)
	for _, request := range []string{first, pending} {
		var liveHash, restoredHash string
		require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT snapshot_sha256 FROM completed_execution_feed_items WHERE request_id=$1`, request).Scan(&liveHash))
		require.NoError(t, restored.QueryRow(t.Context(), `SELECT snapshot_sha256 FROM completed_execution_feed_items WHERE request_id=$1`, request).Scan(&restoredHash))
		require.Equal(t, liveHash, restoredHash)
	}
	var absent, remaining, terminals int
	require.NoError(t, restored.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM freeze_idempotency WHERE request_id=$1),(SELECT count(*) FROM internal_outbox_events),(SELECT count(*) FROM usage_finalization_journal WHERE request_id=$2 AND event_type='settled')`, late, pending).Scan(&absent, &remaining, &terminals))
	require.Zero(t, absent)
	require.Zero(t, remaining)
	require.Equal(t, 1, terminals)
	for _, pool := range []*pgxpool.Pool{f.Pool, restored} {
		var calculated, charged string
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT (convert_from(snapshot,'UTF8')::jsonb->'money'->>'calculated_cost_micros'),(convert_from(snapshot,'UTF8')::jsonb->'money'->>'charged_cost_micros') FROM completed_execution_feed_items WHERE request_id=$1`, pending).Scan(&calculated, &charged))
		require.NotEqual(t, "0", calculated)
		require.Equal(t, "0", charged)
	}
	// A retry after restoration must remain an exact no-op at the destination.
	after := sourceRecoveryTableDigests(t, restored)
	_, err = worker.DrainOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, after, sourceRecoveryTableDigests(t, restored))
	require.EqualValues(t, 3, f.ProviderCalls())
}

func sourceRecoveryTableDigests(t *testing.T, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, table := range []string{"schema_migrations", "freeze_idempotency", "billing_provider_attempts", "request_delivery_receipts", "billing_observation_receipts", "usage_finalization_journal", "completed_execution_feed_items", "completed_execution_feed_states", "execution_source_facts", "execution_source_streams", "internal_outbox_events", "transactions"} {
		rows, err := pool.Query(t.Context(), `SELECT to_jsonb(t)::text FROM `+pgx.Identifier{table}.Sanitize()+` t ORDER BY to_jsonb(t)::text`)
		require.NoError(t, err)
		hash := sha256.New()
		for rows.Next() {
			var row string
			require.NoError(t, rows.Scan(&row))
			fmt.Fprintf(hash, "%d:%s", len(row), row)
		}
		require.NoError(t, rows.Err())
		rows.Close()
		result[table] = fmt.Sprintf("%x", hash.Sum(nil))
	}
	return result
}
