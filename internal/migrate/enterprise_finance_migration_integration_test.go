//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEnterpriseFinanceMigrationsAndRollbackGuards(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var tables, constraints, credentialColumns, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name IN (
		  'enterprise_finance_fiscal_periods','enterprise_finance_period_events',
		  'enterprise_finance_cost_center_versions','enterprise_finance_budget_versions',
		  'enterprise_finance_rate_card_versions','enterprise_finance_rate_components',
		  'enterprise_finance_pricing_snapshots','enterprise_finance_pricing_bindings',
		  'enterprise_finance_allocation_runs','enterprise_finance_allocation_results',
		  'enterprise_finance_adjustment_journal_versions'
		)`).Scan(&tables))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint WHERE conname IN (
		  'enterprise_finance_budget_versions_effect_check',
		  'enterprise_finance_rate_card_versions_supplier_evidence_check',
		  'enterprise_finance_pricing_snapshots_hash_check',
		  'enterprise_finance_allocation_runs_amount_check',
		  'enterprise_finance_allocation_results_authority_check',
		  'enterprise_finance_adjustment_journal_versions_approval_check',
		  'enterprise_finance_adjustment_journal_versions_authority_check'
		)`).Scan(&constraints))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name LIKE 'enterprise_finance_%'
		  AND column_name ~ '(credential|secret|password|token|api_key)'`).Scan(&credentialColumns))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations WHERE version IN (
		  '000049_prd8_enterprise_finance_budgeting',
		  '000050_prd8_enterprise_finance_rate_cards',
		  '000051_prd8_enterprise_finance_allocation_close'
		)`).Scan(&ledger))
	require.Equal(t, 11, tables)
	require.Equal(t, 7, constraints)
	require.Zero(t, credentialColumns)
	require.Equal(t, 3, ledger)

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('enterprise-finance-migration','active','standard') RETURNING id`).Scan(&orgID))
	_, err := pool.Exec(ctx, `
		INSERT INTO enterprise_finance_fiscal_periods (
		  org_id,period_ref,start_at,end_at,state,created_by,state_changed_by
		) VALUES ($1,'FY26-P08','2026-08-01T00:00:00Z','2026-09-01T00:00:00Z','open','test','test')`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO enterprise_finance_allocation_runs (
		  org_id,allocation_run_ref,fiscal_period_ref,source_revision,rule_set_ref,rule_set_revision,
		  input_hash,state,source_record_count,result_count,direct_count,rule_based_count,
		  unallocated_count,conflict_count,source_total_minor,allocated_minor,unallocated_minor,
		  conflict_minor,currency,amount_scale,created_by
		) VALUES ($1,'ALLOC-1','FY26-P08','source-1','rules','1',
		  'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','completed',
		  0,0,0,0,0,0,0,0,0,0,'CNY',1000000,'test')`, orgID)
	require.NoError(t, err)
	rollbackEmptyPRD8RuntimeControlMigrations(t, pool)

	allocationDown, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), enterpriseFinanceDownMigrations[0],
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(allocationDown))
	require.ErrorContains(t, err, "allocation or close evidence exists")
	_, err = pool.Exec(ctx, `DELETE FROM enterprise_finance_allocation_runs WHERE org_id=$1`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(allocationDown))
	require.NoError(t, err)

	rateDown, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), enterpriseFinanceDownMigrations[1],
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(rateDown))
	require.NoError(t, err)
	budgetDown, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), enterpriseFinanceDownMigrations[2],
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(budgetDown))
	require.ErrorContains(t, err, "finance planning data exists")
	_, err = pool.Exec(ctx, `DELETE FROM enterprise_finance_fiscal_periods WHERE org_id=$1`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(budgetDown))
	require.NoError(t, err)
}
