//go:build integration

package migrate_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

// Current and historical fixtures may share template machinery, but must never
// share schema state or mutable data with one another or with later clones.
func TestCommercialRetirementFixtureTemplatesIntegration(t *testing.T) {
	ctx := t.Context()
	seen := make(map[string]bool)
	check := func(pool *pgxpool.Pool, expectedHead string, expectedTriggers int) {
		var name, head string
		var triggers, marker int
		require.NoError(t, pool.QueryRow(ctx, `SELECT current_database(), max(version) FROM schema_migrations`).Scan(&name, &head))
		require.False(t, seen[name], "every fixture must have a separate database")
		seen[name] = true
		require.Equal(t, expectedHead, head)
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE tgname IN
			('api_keys_commercial_limit','users_commercial_limit','projects_commercial_limit','access_credentials_commercial_limit','provider_connections_commercial_limit')`).Scan(&triggers))
		require.Equal(t, expectedTriggers, triggers)
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM settings WHERE key = 'fixture.isolation'`).Scan(&marker))
		require.Zero(t, marker, "clone must not inherit another test's data")
		_, err := pool.Exec(ctx, `INSERT INTO settings(key,value) VALUES ('fixture.isolation','true')`)
		require.NoError(t, err)
	}
	historical := testutil.SetupPreRetirementTestDB(t)
	check(historical, schemaauthority.XInsightCredentialExpandHead, 5)
	check(testutil.SetupMigratedTestDB(t), schemaauthority.CommercialLicenseRetirementHead, 0)
	// Mutate one clone's schema as a migration/backfill test would do.
	_, err := historical.Exec(ctx, `DELETE FROM schema_migrations WHERE version = $1`, schemaauthority.XInsightCredentialExpandHead)
	require.NoError(t, err)
	check(testutil.SetupPreRetirementTestDB(t), schemaauthority.XInsightCredentialExpandHead, 5)
	check(testutil.SetupMigratedTestDB(t), schemaauthority.CommercialLicenseRetirementHead, 0)
}
