//go:build integration

package sourcefacts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestUpstreamObservationM128SourceReadinessIntegration(t *testing.T) {
	for _, expectedHead := range []string{schemaauthority.UpstreamReleaseObservationHead, schemaauthority.UpstreamObservationRestoreHead} {
		t.Run(expectedHead, func(t *testing.T) {
			pool, service, org, project := setupSourceDBWithPool(t, testutil.SetupMigratedThroughTestDB(t, expectedHead))
			ctx := t.Context()
			service.options.ContentEnabled = true
			_, err := pool.Exec(ctx, `INSERT INTO execution_content_streams(org_id,project_id,source_instance) VALUES($1,$2,$3)`, org, project, service.SourceInstance())
			require.NoError(t, err)
			var head string
			require.NoError(t, pool.QueryRow(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&head))
			require.Equal(t, expectedHead, head)
			service.options.ObservationsEnabled = true
			require.NoError(t, service.CheckReady(ctx))
			_, err = pool.Exec(ctx, `INSERT INTO execution_source_streams(org_id,project_id,source_instance,profile) VALUES($1,$2,$3,$4)`, org, project, service.SourceInstance(), ContextProfile)
			require.NoError(t, err)
			snapshot := func() string {
				var value string
				require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_build_object('source',(SELECT jsonb_agg(to_jsonb(s)) FROM execution_source_streams s),'content',(SELECT jsonb_agg(to_jsonb(s)) FROM execution_content_streams s))::text`).Scan(&value))
				return value
			}
			before := snapshot()
			service.options.SourceInstance = "different-source"
			require.ErrorContains(t, service.CheckReady(ctx), "source facts producer instance changed")
			service.options.SourceInstance = "synthetic-ur"
			require.NoError(t, service.CheckReady(ctx))
			require.Equal(t, before, snapshot(), "readiness checks and refusals do not rebind producer history")
		})
	}
}

func TestUpstreamObservationM128RestoreRefusesIncompleteContractIntegration(t *testing.T) {
	helper, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "..", "scripts", "source-budget-restore-rebase.sql"))
	require.NoError(t, err)
	for name, fault := range map[string]string{
		"predecessor":       `DELETE FROM schema_migrations WHERE version='000127_xinsight_communication_policy'`,
		"declaration guard": `ALTER TABLE model_deployments DISABLE TRIGGER model_deployment_upstream_observation_guard`,
		"approval guard":    `ALTER TABLE model_operation_production_approvals DISABLE TRIGGER model_operation_upstream_scope_guard`,
	} {
		t.Run(name, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			before := budgetCoverageRows(t, pool)
			_, err := pool.Exec(t.Context(), fault)
			require.NoError(t, err)
			conn, err := pool.Acquire(t.Context())
			require.NoError(t, err)
			defer conn.Release()
			_, err = conn.Exec(t.Context(), string(helper))
			require.ErrorContains(t, err, "M128 restore requires exact observation guards and M127 predecessor")
			_, err = conn.Exec(t.Context(), "ROLLBACK")
			require.NoError(t, err)
			require.Equal(t, before, budgetCoverageRows(t, pool))
		})
	}
}
