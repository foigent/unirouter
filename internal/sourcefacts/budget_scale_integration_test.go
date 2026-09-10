//go:build integration

package sourcefacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/outbox"
)

// Synthetic query-scale input only. These direct SQL rows are not original
// billing-writer or correctness evidence; those paths have separate tests.
func TestSourceBudgetQueryScaleEvidence(t *testing.T) {
	dir := os.Getenv("SOURCE_BUDGET_SCALE_EXPORT_DIR")
	if dir == "" {
		t.Skip("explicit query-scale evidence run only")
	}
	require.True(t, filepath.IsAbs(dir))
	pool, _, org, project := setupSourceDB(t)
	ctx := t.Context()
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	_, err := pool.Exec(ctx, `INSERT INTO usage_finalization_journal
 (event_key,event_type,request_id,org_id,project_id,occurred_at,calculated_cost_micros,charged_cost_micros)
 SELECT 'synthetic-scale-settled:'||i,'settled','synthetic-scale-'||i,
 $1::bigint,$2::bigint+(i%100),$3,1,1
 FROM generate_series(1,100000) i`, org, project, start.Add(time.Hour))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO internal_outbox_events
 (event_key,event_type,priority,org_id,request_id,payload,occurred_at)
 SELECT 'synthetic-scale-settled:'||i,$3,100,
 $1::bigint+1000,'synthetic-scale-'||i,jsonb_build_object('org_id',$1::bigint+1000),$2
 FROM generate_series(1,100000) i`, org, start.Add(time.Hour), outbox.EventBillingSettled)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `ANALYZE usage_finalization_journal; ANALYZE internal_outbox_events`)
	require.NoError(t, err)
	var subtotal string
	require.NoError(t, pool.QueryRow(ctx, budgetSettledSubtotalSQL, org, project, start, end).Scan(&subtotal))
	require.Equal(t, "1000", subtotal)
	result := map[string]any{"fixture": "synthetic_query_scale_only", "journal_rows": 100000, "outbox_rows": 100000, "matching_journal_rows": 1000, "matching_outbox_rows": 0, "scope": "one_of_100_projects_in_the_same_organization"}
	for _, q := range []struct {
		name, sql string
		args      []any
	}{
		{"settled", budgetSettledSubtotalSQL, []any{org, project, start, end}},
		{"materialization", budgetMaterializationSQL, []any{budgetFinancialEventTypes, org, strconv.FormatInt(org, 10)}},
	} {
		var plan json.RawMessage
		require.NoError(t, pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+q.sql, q.args...).Scan(&plan))
		result[q.name] = plan
	}
	wire, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "budget-query-scale.json"), wire, 0600))
}
