package migrate_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCounterExpandRejectsIncompatibleRecoveryActorAtomically(t *testing.T) {
	for _, missing := range []string{"M115", metering.GatewayCounterEstimateVersionV2, metering.GatewayCounterEstimateVersionV3} {
		t.Run(missing, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			dir := testutil.MigrationsDir(t)
			down, err := os.ReadFile(filepath.Join(dir, schemaauthority.StreamCounterExpandHead+".down.sql"))
			require.NoError(t, err)
			_, err = pool.Exec(ctx, string(down))
			require.NoError(t, err)
			artifact, err := schemaauthority.ExpectedLedger(dir)
			require.NoError(t, err)
			manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
			if missing == "M115" {
				manifest.SchemaHeads = manifest.SchemaHeads[:2]
			} else {
				refs := manifest.Implementations
				manifest.Implementations = nil
				for _, ref := range refs {
					if ref.ID != missing {
						manifest.Implementations = append(manifest.Implementations, ref)
					}
				}
			}
			id := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("e", 32), Artifact: "sha256:" + strings.Repeat("e", 64)}
			_, head, err := runtimegeneration.Renew(ctx, pool, id, manifest, artifact, true)
			require.NoError(t, err)
			require.Equal(t, schemaauthority.CapabilityExpandHead, head)
			up, err := os.ReadFile(filepath.Join(dir, schemaauthority.StreamCounterExpandHead+".up.sql"))
			require.NoError(t, err)
			hooks := migrate.CompatibleExpandHooks{Version: schemaauthority.StreamCounterExpandHead, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(up)), Before: func(context.Context, *pgxpool.Conn) error { return nil }, After: func(context.Context, pgx.Tx) error { return nil }}
			require.NoError(t, migrate.RunRuntime(ctx, pool, dir), "ordinary startup must leave the additive migration pending")
			require.ErrorContains(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks), "all serving and recovery generations")
			var column, ledger bool
			require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='stream_delivery_leases' AND column_name='counter_version'), EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, schemaauthority.StreamCounterExpandHead).Scan(&column, &ledger))
			require.False(t, column)
			require.False(t, ledger)
			// Isolated host fixture: explicit stop preserves the old immutable identity.
			_, err = pool.Exec(ctx, `UPDATE runtime_generations SET state='stopped' WHERE instance_id=$1`, id.Instance)
			require.NoError(t, err)
			require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks))
			_, _, err = runtimegeneration.Renew(ctx, pool, id, manifest, artifact[:len(artifact)-1], true)
			require.Error(t, err, "old binary cannot resume recovery on the new schema")
		})
	}
}
