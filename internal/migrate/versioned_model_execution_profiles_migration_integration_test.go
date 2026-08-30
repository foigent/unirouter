//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestVersionedModelExecutionProfilesMigrationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	execMigrationDown(t, ctx, pool, "000082_sealed_model_execution_contracts.down.sql")
	down, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000081_versioned_model_execution_profiles.down.sql",
	))
	require.NoError(t, err)
	require.NoError(t, func() error {
		_, execErr := pool.Exec(ctx, string(down))
		return execErr
	}())

	var orgID, modelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('m81-profile-migration','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,
  context_length,output_limit,capability_tags)
VALUES ($1,'MiniMax-M2.7','MiniMax M2.7','minimax','ga',204800,65536,'reasoning,tools')
RETURNING id`, orgID).Scan(&modelID))

	require.NoError(t, migrate.RunThrough(ctx, pool, testutil.MigrationsDir(t), "000082_sealed_model_execution_contracts"))
	var profileID, profileVersion string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT execution_profile_id,execution_profile_version
  FROM model_registry WHERE id=$1`, modelID).Scan(&profileID, &profileVersion))
	require.Equal(t, "unverified.chat", profileID,
		"migration must not infer a tokenizer or provider contract from a historical model name")
	require.Equal(t, "1.0.0", profileVersion)

	_, err = pool.Exec(ctx, `
UPDATE model_registry
   SET execution_profile_id='minimax.m2.7.token-plan',execution_profile_version='1.0.0'
 WHERE id=$1`, modelID)
	require.NoError(t, err)
	execMigrationDown(t, ctx, pool, "000082_sealed_model_execution_contracts.down.sql")
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "durable profile selections exist")

	var ledgerPresent, profileColumnsPresent bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  EXISTS(SELECT 1 FROM schema_migrations
          WHERE version='000081_versioned_model_execution_profiles'),
  (SELECT COUNT(*)=2 FROM information_schema.columns
    WHERE table_schema='public' AND table_name='model_registry'
      AND column_name IN ('execution_profile_id','execution_profile_version'))`,
	).Scan(&ledgerPresent, &profileColumnsPresent))
	require.True(t, ledgerPresent)
	require.True(t, profileColumnsPresent)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT execution_profile_id,execution_profile_version
  FROM model_registry WHERE id=$1`, modelID).Scan(&profileID, &profileVersion))
	require.Equal(t, "minimax.m2.7.token-plan", profileID)
	require.Equal(t, "1.0.0", profileVersion)

	_, err = pool.Exec(ctx, `
UPDATE model_registry
   SET execution_profile_id='Bad Identity',execution_profile_version='1.0.0'
 WHERE id=$1`, modelID)
	require.ErrorContains(t, err, "model_registry_execution_profile_identity_check")

	_, err = pool.Exec(ctx, `
UPDATE model_registry
   SET execution_profile_id='unverified.chat',execution_profile_version='1.0.0'
 WHERE id=$1`, modelID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000088_future_contract')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "later migrations must be reversed first")
	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version='000088_future_contract'`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  NOT EXISTS(SELECT 1 FROM schema_migrations
              WHERE version='000081_versioned_model_execution_profiles'),
  NOT EXISTS(SELECT 1 FROM information_schema.columns
              WHERE table_schema='public' AND table_name='model_registry'
                AND column_name='execution_profile_id')`,
	).Scan(&ledgerPresent, &profileColumnsPresent))
	require.True(t, ledgerPresent)
	require.True(t, profileColumnsPresent)

	require.NoError(t, migrate.RunThrough(ctx, pool, testutil.MigrationsDir(t), "000082_sealed_model_execution_contracts"))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT execution_profile_id,execution_profile_version
  FROM model_registry WHERE id=$1`, modelID).Scan(&profileID, &profileVersion))
	require.Equal(t, "unverified.chat", profileID)
	require.Equal(t, "1.0.0", profileVersion)
}
