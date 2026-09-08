package migrate_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testdb"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCapabilityExpandRequiresExplicitBridgeAndPreservesLegacy(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx := t.Context()
	dir := testdb.MigrationsDir(t)
	require.NoError(t, testutil.InstallExactTestArtifact(ctx, pool, dir))
	counterDown, err := os.ReadFile(filepath.Join(dir, schemaauthority.StreamCounterExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(counterDown))
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(dir, schemaauthority.CapabilityExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	ledger, err := schemaauthority.ExpectedLedger(dir)
	require.NoError(t, err)
	ledger = ledger[:len(ledger)-1] // This test owns the historical M113/M114 transition.
	legacy := ledger[:len(ledger)-1]
	require.NoError(t, publication.VerifyRuntimeStaticProjection(ctx, pool, legacy))
	// Both owner and migration-skipping runtime remain on the exact legacy DB.
	require.NoError(t, migrate.RunRuntime(ctx, pool, dir))
	require.ErrorContains(t, migrate.Run(ctx, pool, dir), "requires explicit fleet preflight")
	var expanded bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('public.capability_publications') IS NOT NULL`).Scan(&expanded))
	require.False(t, expanded)
	wire, err := os.ReadFile(filepath.Join(dir, schemaauthority.CapabilityExpandHead+".up.sql"))
	require.NoError(t, err)
	hooks := migrate.CompatibleExpandHooks{ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(wire)),
		After:  func(context.Context, pgx.Tx) error { return nil }, // no runtime in this isolated fixture
		Before: func(context.Context, *pgxpool.Conn) error { return fmt.Errorf("incompatible receiving generation") }}
	require.ErrorContains(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks), "incompatible receiving generation")
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('public.capability_publications') IS NOT NULL`).Scan(&expanded))
	require.False(t, expanded)
	hooks.Before = func(context.Context, *pgxpool.Conn) error { return nil } // isolated database; no runtime exists
	hooks.After = func(context.Context, pgx.Tx) error { return fmt.Errorf("bootstrap proof failed") }
	require.ErrorContains(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks), "bootstrap proof failed")
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('public.capability_publications') IS NOT NULL`).Scan(&expanded))
	require.False(t, expanded, "failed lease bootstrap must roll back DDL too")
	hooks.After = func(context.Context, pgx.Tx) error { return nil }
	require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks))
	require.NoError(t, publication.VerifyRuntimeStaticProjection(ctx, pool, ledger))
	var publications, definitions, snapshots int
	require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM capability_publications),
    (SELECT count(*) FROM model_profile_definitions),(SELECT count(*) FROM runtime_change_releases WHERE execution_snapshot_digest IS NOT NULL)`).Scan(&publications, &definitions, &snapshots))
	require.Zero(t, publications)
	require.Zero(t, definitions)
	require.Zero(t, snapshots)
	require.NoError(t, migrate.RunRuntime(ctx, pool, dir))
	_, err = pool.Exec(ctx, `UPDATE capability_publications SET materializer_contract='rewrite'`)
	require.Error(t, err)
	_, err = pool.Exec(ctx, `TRUNCATE model_profile_definitions`)
	require.Error(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, publication.VerifyRuntimeStaticProjection(ctx, pool, legacy))
}

func TestCapabilityExpandPublishesVerifiedLeasesAtomically(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	dir := testdb.MigrationsDir(t)
	counterDown, err := os.ReadFile(filepath.Join(dir, schemaauthority.StreamCounterExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(counterDown))
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(dir, schemaauthority.CapabilityExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	ledger, err := schemaauthority.ExpectedLedger(dir)
	require.NoError(t, err)
	ledger = ledger[:len(ledger)-1]
	wire, err := os.ReadFile(filepath.Join(dir, schemaauthority.CapabilityExpandHead+".up.sql"))
	require.NoError(t, err)
	manifest := runtimegeneration.CurrentManifest([]string{schemaauthority.LegacyCapabilityHead, schemaauthority.CapabilityExpandHead})
	identities := []runtimegeneration.Identity{{Instance: "uri_" + strings.Repeat("a", 32), Artifact: "sha256:" + strings.Repeat("b", 64)}, {Instance: "uri_" + strings.Repeat("c", 32), Artifact: "sha256:" + strings.Repeat("b", 64)}}
	hooks := migrate.CompatibleExpandHooks{ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(wire)),
		Before: func(context.Context, *pgxpool.Conn) error { return nil }, // exact isolated fixture identities
		After: func(ctx context.Context, tx pgx.Tx) error {
			for _, identity := range identities {
				if err := runtimegeneration.BootstrapVerifiedBridge(ctx, tx, identity, manifest, ledger); err != nil {
					return err
				}
			}
			// A concurrent reader cannot see the new schema before these leases.
			var head string
			if err := pool.QueryRow(ctx, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&head); err != nil {
				return err
			}
			if head != schemaauthority.LegacyCapabilityHead {
				return fmt.Errorf("expand became visible before bootstrap committed")
			}
			return nil
		}}
	require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks))
	var count, publications int
	require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM runtime_generations WHERE schema_head=$1 AND state='serving' AND ingress_state='absent' AND lease_expires_at>clock_timestamp()),(SELECT count(*) FROM capability_publications)`, schemaauthority.CapabilityExpandHead).Scan(&count, &publications))
	require.Equal(t, 2, count)
	require.Zero(t, publications, "bootstrap grants no Publication or ingress membership")
}
