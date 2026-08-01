//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestAsyncMediaImageMigrationContractAndRollbackGuard(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var tables, constraints, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema='public'
		   AND table_name IN ('async_media_jobs','async_media_charge_facts')`,
	).Scan(&tables))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		 WHERE conname IN (
		   'async_media_jobs_lease_shape_check',
		   'async_media_jobs_terminal_queue_shape_check',
		   'async_media_jobs_provider_shape_check',
		   'async_media_jobs_cancellation_shape_check',
		   'async_media_jobs_settlement_shape_check',
		   'async_media_jobs_terminal_shape_check',
		   'async_media_jobs_success_shape_check',
		   'async_media_jobs_output_shape_check',
		   'async_media_charge_amount_check'
		 )`,
	).Scan(&constraints))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		 WHERE version='000046_v1_async_media_image_jobs'`,
	).Scan(&ledger))
	require.Equal(t, 2, tables)
	require.Equal(t, 9, constraints)
	require.Equal(t, 1, ledger)

	var orgID, projectID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type)
		VALUES ('async-media-migration', 'active', 'standard')
		RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id, name, slug, status, budget_period)
		VALUES ($1, 'Async media', 'async-media-migration', 'active', 'none')
		RETURNING id`,
		orgID,
	).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (
		  org_id, project_id, name, key_hash, key_prefix, status
		) VALUES ($1,$2,'async-media',$3,'ur-media-migration','active')
		RETURNING id`,
		orgID, projectID, strings.Repeat("a", 64),
	).Scan(&keyID))
	_, err := pool.Exec(ctx, `
		INSERT INTO async_media_jobs (
		  job_id, contract_version, org_id, project_id, api_key_id,
		  request_id, operation, profile_id, requested_model, routing_model,
		  request_sha256, idempotency_hash, request_ciphertext,
		  admission_state, queue_state, lease_state, execution_state,
		  provider_outcome, cancellation_state, object_state, settlement_state,
		  max_attempts
		) VALUES (
		  'mediajob_0123456789abcdef0123456789abcdef',
		  'unirouter.async-media-runtime.v1',$1,$2,$3,'op-migration',
		  'text_to_image','unirouter.openai.images.gpt-image.v1',
		  'image-alias','image-route',$4,$5,'ciphertext',
		  'accepted','queued','unleased','pending','pending','not_requested',
		  'pending','not_eligible',3
		)`,
		orgID, projectID, keyID,
		"sha256:"+strings.Repeat("b", 64),
		"hmac-sha256:"+strings.Repeat("c", 64),
	)
	require.NoError(t, err)
	rollbackEmptyPRD8Migrations(t, pool)

	downSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000046_v1_async_media_image_jobs.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "durable jobs exist")

	_, err = pool.Exec(ctx, `DELETE FROM async_media_jobs`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema='public'
		   AND table_name IN ('async_media_jobs','async_media_charge_facts')`,
	).Scan(&tables))
	require.Zero(t, tables)
}
