//go:build integration

package migrate_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func scopedContentMigrationWire(t *testing.T, suffix string) []byte {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.ScopedContentExpandHead+suffix))
	require.NoError(t, err)
	return wire
}

func TestScopedContentMigrationPreservesOldPolicyAndRefusesNewHistoryRollback(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	credentialDown, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.XInsightCredentialExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(credentialDown))
	require.NoError(t, err)
	adoptionDown, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.XInsightAdoptionExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(adoptionDown))
	require.NoError(t, err)
	policyDown, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.XInsightPolicyExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(policyDown))
	require.NoError(t, err)
	down := scopedContentMigrationWire(t, ".down.sql")
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	org, project := contentMigrationProject(t, pool)
	const config = `{"mode":"metadata_only","supported_protocols":["openai.chat_completions.v1"],"observation_points":["client_request"],"allowed_purposes":[]}`
	_, err = pool.Exec(ctx, `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason)
      VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none')`, org, project, []byte(config))
	require.NoError(t, err)
	up := scopedContentMigrationWire(t, ".up.sql")
	require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, testutil.MigrationsDir(t), migrate.CompatibleExpandHooks{
		Version:        schemaauthority.ScopedContentExpandHead,
		ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(up)),
		Before:         func(context.Context, *pgxpool.Conn) error { return nil },
		After: func(ctx context.Context, tx pgx.Tx) error {
			_, err := runtimegeneration.ReadBudgetCoverage(ctx, tx)
			return err
		},
	}))
	var explicit bool
	var mode string
	require.NoError(t, pool.QueryRow(ctx, `SELECT explicit_override,effective_execution_storage_mode FROM execution_content_policies WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&explicit, &mode))
	require.True(t, explicit, "legacy project policy must remain an explicit override")
	require.Equal(t, "temporary_allowed", mode)
	_, err = pool.Exec(ctx, `INSERT INTO execution_content_enterprise_policies(org_id,desired_version,effective_version,desired_config,effective_config,
      desired_execution_storage_mode,effective_execution_storage_mode,desired_default_project_quota_bytes,effective_default_project_quota_bytes,
      effective_at,application_status,application_reason)
      VALUES($1,1,1,$2,$2,'strict_no_persist','strict_no_persist',1024,1024,clock_timestamp(),'applied','none')`, org, []byte(config))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.Error(t, err, "down must refuse any new scoped policy history")
	require.NoError(t, pool.QueryRow(ctx, `SELECT explicit_override FROM execution_content_policies WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&explicit))
	require.NoError(t, pool.QueryRow(ctx, `SELECT 1 FROM execution_content_enterprise_policies WHERE org_id=$1`, org).Scan(new(int)))
	_, err = pool.Exec(ctx, `DELETE FROM execution_content_enterprise_policies WHERE org_id=$1`, org)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err, "legacy project row alone must permit the guarded down")
	var head string
	require.NoError(t, pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.PreDispatchExpandHead, head)
}
