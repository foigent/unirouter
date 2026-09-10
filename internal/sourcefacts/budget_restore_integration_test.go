//go:build integration

package sourcefacts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestSourceBudgetActualBackupRestoreCannotReuseCoverageBeforeLostCharge(t *testing.T) {
	toolsDir := os.Getenv("SOURCE_BUDGET_BACKUP_TOOLS_DIR")
	if toolsDir == "" {
		t.Skip("explicit actual restore run requires the matching PostgreSQL client directory")
	}
	require.True(t, filepath.IsAbs(toolsDir))
	dumpTool, err := exec.LookPath(filepath.Join(toolsDir, "pg_dump"))
	require.NoError(t, err)
	psqlTool, err := exec.LookPath(filepath.Join(toolsDir, "psql"))
	require.NoError(t, err)
	pool, source, org, project := setupSourceDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	events, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	financial := billing.NewBilling(pool, nil)
	financial.SetOutbox(events)
	_, err = pool.Exec(ctx, `UPDATE projects SET budget_cents=100000,budget_period='monthly' WHERE id=$1`, project)
	require.NoError(t, err)
	snapshot := testSnapshot()
	snapshot.Core.OrgID = executioncontext.ID(org)
	snapshot.Core.ProjectID = executioncontext.ID(project)
	require.NoError(t, source.CaptureAuthenticated(ctx, snapshot))
	before, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	var originalFacts, originalDefinitions string
	require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(f) ORDER BY profile,sequence)::text FROM execution_source_facts f`).Scan(&originalFacts))
	require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(d) ORDER BY org_id,project_id,revision)::text FROM execution_source_budget_definitions d`).Scan(&originalDefinitions))
	dump := filepath.Join(t.TempDir(), "synthetic-budget-before-charge.sql")
	command := exec.CommandContext(ctx, dumpTool, "--no-owner", "--no-privileges", "--dbname", pool.Config().ConnString(), "--file", dump)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "pg_dump: %s", output)
	// The original writer commits 600 CNY after the backup. The older backup
	// contains neither that charge nor a pending event that could reveal it.
	_, err = pool.Exec(ctx, `INSERT INTO wallets(org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros) VALUES($1,2000000000,0,0,0)`, org)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO model_prices(model_name,provider,input_price_cents,output_price_cents) VALUES('synthetic-restore-charge','test',60000,0)`)
	require.NoError(t, err)
	frozen, err := financial.Freeze(ctx, billing.FreezeRequest{RequestID: "post-backup-600", OrgID: org, ProjectID: project, Model: "synthetic-restore-charge", EstimatedTokens: 1000, SafetyFactor: "1"})
	require.NoError(t, err)
	require.True(t, frozen.Success)
	require.NoError(t, financial.MarkBillableOutput(ctx, "post-backup-600", org))
	usage := billing.SettleRequest{RequestID: "post-backup-600", OrgID: org, PromptTokens: 1000, UncachedPromptTokens: 1000, UsageSource: "upstream", CacheUsageStatus: "not_reported"}
	require.NoError(t, financial.PersistSettlementUsage(ctx, usage))
	settled, err := financial.Settle(ctx, usage)
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.EqualValues(t, 600000000, settled.ChargedCostMicros)
	// Restore to a different task-owned empty database, never over the source.
	restored := testutil.SetupIsolatedTestDB(t)
	command = exec.CommandContext(ctx, psqlTool, "--no-psqlrc", "--set", "ON_ERROR_STOP=1", "--dbname", restored.Config().ConnString(), "--file", dump)
	output, err = command.CombinedOutput()
	require.NoError(t, err, "restore: %s", output)
	old, err := runtimegeneration.ReadBudgetCoverage(ctx, restored)
	require.NoError(t, err)
	require.Equal(t, before, old)
	helper, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "..", "scripts", "source-budget-restore-rebase.sql"))
	require.NoError(t, err)
	_, err = restored.Exec(ctx, string(helper))
	require.NoError(t, err)
	after, err := runtimegeneration.ReadBudgetCoverage(ctx, restored)
	require.NoError(t, err)
	require.Greater(t, after.Epoch, before.Epoch)
	require.True(t, after.CertifiedFrom.After(before.CertifiedFrom))
	require.Equal(t, "restore_rebase", after.Reason)
	var count int
	require.NoError(t, restored.QueryRow(ctx, `SELECT count(*) FROM internal_outbox_events WHERE event_type=ANY($1)`, budgetFinancialEventTypes).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, restored.QueryRow(ctx, `SELECT count(*) FROM freeze_idempotency WHERE request_id='post-backup-600'`).Scan(&count))
	require.Zero(t, count)
	var facts, definitions string
	require.NoError(t, restored.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(f) ORDER BY profile,sequence)::text FROM execution_source_facts f`).Scan(&facts))
	require.Equal(t, originalFacts, facts)
	require.NoError(t, restored.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(d) ORDER BY org_id,project_id,revision)::text FROM execution_source_budget_definitions d`).Scan(&definitions))
	require.Equal(t, originalDefinitions, definitions)
	restoredEvents, err := outbox.New(restored, outbox.DefaultOptions())
	require.NoError(t, err)
	restoredFinancial := billing.NewBilling(restored, nil)
	restoredFinancial.SetOutbox(restoredEvents)
	reader, err := New(restored, Options{Enabled: true, BudgetSnapshotsEnabled: true, SourceInstance: source.SourceInstance(), CursorSecret: source.options.CursorSecret, FinancialJournalReady: restoredFinancial.FinancialJournalReady})
	require.NoError(t, err)
	require.NoError(t, reader.CheckReady(ctx))
	tx, err := restored.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`)
	require.NoError(t, err)
	var cutoff time.Time
	require.NoError(t, tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&cutoff))
	access := budgetAccess{access: access{org: org, project: project, instance: source.SourceInstance()}}
	record, err := readBudgetRecordTx(ctx, tx, access, gatewayBudgetPrefix+strconv.FormatInt(project, 10))
	require.NoError(t, err)
	summary, err := reader.gatewayBudgetSummaryTx(ctx, tx, access, record.definition, cutoff)
	require.NoError(t, err)
	require.Equal(t, "0", summary.KnownSubtotal.ValueMinor)
	require.Equal(t, "0", summary.Coverage.MaterializationPending)
	require.Equal(t, "known_subtotal_with_gap", summary.Coverage.State)
	require.Contains(t, summary.Coverage.Reasons, "restore_gap")
	require.Nil(t, summary.Comparison.Remaining)
}
