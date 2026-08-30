package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F33MigrationQuarantinesExistingWebhooksWithoutDeletingThem(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx := context.Background()
	allMigrationsDir := testutil.MigrationsDir(t)
	preP6Dir := t.TempDir()
	throughP6Dir := t.TempDir()
	const p6Migration = "000020_prd6_notification_webhook_scope_verification.up.sql"

	entries, err := os.ReadDir(allMigrationsDir)
	require.NoError(t, err)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") || name > p6Migration {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(allMigrationsDir, name))
		require.NoError(t, readErr)
		require.NoError(t, os.WriteFile(filepath.Join(throughP6Dir, name), body, 0o600))
		if name < p6Migration {
			require.NoError(t, os.WriteFile(filepath.Join(preP6Dir, name), body, 0o600))
		}
	}
	require.NoError(t, migrate.Run(ctx, pool, preP6Dir))

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM orgs ORDER BY id LIMIT 1`).Scan(&orgID))
	var previouslyEnabledID, previouslyDisabledID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO notification_webhooks (org_id, name, url, event_types, enabled)
		 VALUES ($1, 'previously-enabled', 'https://enabled.example.test/hook', '[]', TRUE) RETURNING id`,
		orgID,
	).Scan(&previouslyEnabledID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO notification_webhooks (org_id, name, url, event_types, enabled)
		 VALUES ($1, 'previously-disabled', 'https://disabled.example.test/hook', '[]', FALSE) RETURNING id`,
		orgID,
	).Scan(&previouslyDisabledID))
	require.Positive(t, previouslyDisabledID)
	var eventID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO notification_events (org_id, event_type, severity, title, dedupe_key)
		 VALUES ($1, 'p6f33.migration', 'warning', 'pre-up failed delivery', 'p6f33-migration-event') RETURNING id`,
		orgID,
	).Scan(&eventID))
	_, err = pool.Exec(ctx,
		`INSERT INTO notification_deliveries (event_id, webhook_id, status, attempts, next_retry_at)
		 VALUES ($1, $2, 'failed', 1, NOW() - INTERVAL '1 minute')`,
		eventID, previouslyEnabledID,
	)
	require.NoError(t, err)

	require.NoError(t, migrate.Run(ctx, pool, throughP6Dir))

	rows, err := pool.Query(ctx,
		`SELECT name, enabled, scope_verified, scope_quarantine_previous_enabled
		 FROM notification_webhooks ORDER BY name`,
	)
	require.NoError(t, err)
	defer rows.Close()
	states := map[string][3]bool{}
	for rows.Next() {
		var name string
		var enabled, verified, previousEnabled bool
		require.NoError(t, rows.Scan(&name, &enabled, &verified, &previousEnabled))
		states[name] = [3]bool{enabled, verified, previousEnabled}
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Equal(t, map[string][3]bool{
		"previously-disabled": {false, false, false},
		"previously-enabled":  {false, false, true},
	}, states)
	var retryQuarantined, nextRetryIsNull, previousNextRetryIsNull bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT retry_scope_quarantined, next_retry_at IS NULL,
		        scope_quarantine_previous_next_retry_at IS NULL
		 FROM notification_deliveries WHERE event_id = $1`, eventID,
	).Scan(&retryQuarantined, &nextRetryIsNull, &previousNextRetryIsNull))
	require.True(t, retryQuarantined)
	require.True(t, nextRetryIsNull)
	require.False(t, previousNextRetryIsNull)
	_, err = pool.Exec(ctx,
		`INSERT INTO notification_webhooks (org_id, name, url, event_types, enabled)
		 VALUES ($1, 'legacy-writer', 'https://legacy.example.test/hook', '[]', TRUE)`,
		orgID,
	)
	require.Error(t, err, "an old writer must fail closed instead of enabling an unverified route")
	_, err = pool.Exec(ctx,
		`UPDATE notification_webhooks SET url = 'https://legacy-update.example.test/hook' WHERE id = $1`,
		previouslyEnabledID,
	)
	require.Error(t, err, "an old writer must not mutate a quarantined route")

	var verifiedID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO notification_webhooks
		 (org_id, name, url, event_types, enabled, scope_verified, scope_write_generation)
		 VALUES ($1, 'new-verified', 'https://verified.example.test/hook', '[]', TRUE, TRUE, 1)
		 RETURNING id`,
		orgID,
	).Scan(&verifiedID))
	_, err = pool.Exec(ctx,
		`UPDATE notification_webhooks SET url = 'https://legacy-hijack.example.test/hook' WHERE id = $1`,
		verifiedID,
	)
	require.Error(t, err, "an old writer must not mutate a verified route")
	var verified, enabled bool
	var verifiedURL string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT scope_verified, enabled, url FROM notification_webhooks WHERE id = $1`, verifiedID,
	).Scan(&verified, &enabled, &verifiedURL))
	require.True(t, verified)
	require.True(t, enabled)
	require.Equal(t, "https://verified.example.test/hook", verifiedURL)

	downSQL, err := os.ReadFile(filepath.Join(allMigrationsDir, "000020_prd6_notification_webhook_scope_verification.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)

	var count, enabledCount, verificationColumnCount, migrationLedgerCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE enabled) FROM notification_webhooks`).Scan(&count, &enabledCount))
	require.Equal(t, 3, count, "rollback must not delete historical or newly verified routes")
	require.Zero(t, enabledCount, "rollback must disable every route before a vulnerable binary can run")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND ((table_name = 'notification_webhooks'
		         AND column_name IN ('scope_verified', 'scope_quarantine_previous_enabled', 'scope_write_generation'))
		        OR (table_name = 'notification_deliveries'
		         AND column_name IN ('retry_scope_quarantined', 'scope_quarantine_previous_next_retry_at')))`,
	).Scan(&verificationColumnCount))
	require.Zero(t, verificationColumnCount)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations
		 WHERE version = '000020_prd6_notification_webhook_scope_verification'`,
	).Scan(&migrationLedgerCount))
	require.Zero(t, migrationLedgerCount)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT next_retry_at IS NULL FROM notification_deliveries WHERE event_id = $1`, eventID,
	).Scan(&nextRetryIsNull))
	require.True(t, nextRetryIsNull, "rollback must not restore a pre-fix retry destination")
	_, err = pool.Exec(ctx, `UPDATE notification_webhooks SET enabled = TRUE WHERE id = $1`, verifiedID)
	require.Error(t, err, "rollback lockdown must reject old-writer re-enable")
	_, err = pool.Exec(ctx,
		`UPDATE notification_webhooks SET url = 'https://rollback-pollution.example.test/hook' WHERE id = $1`,
		verifiedID,
	)
	require.Error(t, err, "rollback lockdown must reject legacy destination mutation")
	_, err = pool.Exec(ctx,
		`INSERT INTO notification_webhooks (org_id, name, url, event_types, enabled)
		 VALUES ($1, 'rollback-pollution', 'https://rollback-pollution.example.test/hook', '[]', FALSE)`,
		orgID,
	)
	require.Error(t, err, "rollback lockdown must reject legacy disabled-row creation")
	_, err = pool.Exec(ctx, `DELETE FROM notification_webhooks WHERE id = $1`, verifiedID)
	require.Error(t, err, "rollback lockdown must retain rows for safe roll-forward review")

	require.NoError(t, migrate.Run(ctx, pool, throughP6Dir), "down ledger removal must permit a safe roll-forward")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND ((table_name = 'notification_webhooks'
		         AND column_name IN ('scope_verified', 'scope_quarantine_previous_enabled', 'scope_write_generation'))
		        OR (table_name = 'notification_deliveries'
		         AND column_name IN ('retry_scope_quarantined', 'scope_quarantine_previous_next_retry_at')))`,
	).Scan(&verificationColumnCount))
	require.Equal(t, 5, verificationColumnCount)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE enabled), COUNT(*) FILTER (WHERE scope_verified)
		 FROM notification_webhooks`,
	).Scan(&enabledCount, &migrationLedgerCount))
	require.Zero(t, enabledCount)
	require.Zero(t, migrationLedgerCount)
	_, err = pool.Exec(ctx,
		`INSERT INTO notification_webhooks (org_id, name, url, event_types, enabled)
		 VALUES ($1, 'legacy-after-roll-forward', 'https://legacy.example.test/hook', '[]', FALSE)`,
		orgID,
	)
	require.Error(t, err, "roll-forward must restore the safe-writer trigger")
}
