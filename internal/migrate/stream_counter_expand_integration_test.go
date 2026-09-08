package migrate_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	"time"
)

func TestCounterExpandBusySettlementPreservesRuntimeTransaction(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	dir := testutil.MigrationsDir(t)
	down, err := os.ReadFile(filepath.Join(dir, schemaauthority.StreamCounterExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	up, err := os.ReadFile(filepath.Join(dir, schemaauthority.StreamCounterExpandHead+".up.sql"))
	require.NoError(t, err)
	// Match the real Settle owner: its reservation lock precedes the stream
	// relation/version lock. The schema owner must yield instead of deadlocking it.
	runtimeTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer runtimeTx.Rollback(context.Background())
	_, err = runtimeTx.Exec(ctx, `LOCK TABLE freeze_idempotency IN ROW EXCLUSIVE MODE`)
	require.NoError(t, err)
	pid := make(chan uint32, 1)
	done := make(chan error, 1)
	hooks := migrate.CompatibleExpandHooks{Version: schemaauthority.StreamCounterExpandHead, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(up)), Before: func(_ context.Context, conn *pgxpool.Conn) error { pid <- conn.Conn().PgConn().PID(); return nil }, After: func(context.Context, pgx.Tx) error { return nil }}
	go func() { done <- migrate.RunCompatibleExpand(ctx, pool, dir, hooks) }()
	var migrationPID uint32
	select {
	case migrationPID = <-pid:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var migrationErr error
	finished := false
	require.Eventually(t, func() bool {
		select {
		case migrationErr = <-done:
			finished = true
			return true
		default:
			var waiting bool
			err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND relation='freeze_idempotency'::regclass AND NOT granted)`, migrationPID).Scan(&waiting)
			return err == nil && waiting
		}
	}, 3*time.Second, 5*time.Millisecond)
	_, runtimeErr := runtimeTx.Exec(ctx, `LOCK TABLE stream_delivery_leases IN ROW EXCLUSIVE MODE`)
	if runtimeErr != nil {
		_ = runtimeTx.Rollback(context.Background())
	}
	if !finished {
		select {
		case migrationErr = <-done:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	t.Logf("runtime result=%v; migration result=%v", runtimeErr, migrationErr)
	require.NoError(t, runtimeErr, "online migration may not abort the accepted runtime transaction")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, migrationErr, &pgErr)
	require.Equal(t, "55P03", pgErr.Code, "busy migration must fail without a deadlock victim")
	require.NoError(t, runtimeTx.Commit(ctx))
	var column, ledger bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='stream_delivery_leases' AND column_name='counter_version'), EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, schemaauthority.StreamCounterExpandHead).Scan(&column, &ledger))
	require.False(t, column)
	require.False(t, ledger)
	hooks.Before = func(context.Context, *pgxpool.Conn) error { return nil }
	require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks), "same formal owner can apply after the runtime transaction finishes")
}

func TestCounterExpandBusyOwnersLeaveSchemaUntouched(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	dir := testutil.MigrationsDir(t)
	down, err := os.ReadFile(filepath.Join(dir, schemaauthority.StreamCounterExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(down))
	require.NoError(t, err)
	up, err := os.ReadFile(filepath.Join(dir, schemaauthority.StreamCounterExpandHead+".up.sql"))
	require.NoError(t, err)
	hooks := migrate.CompatibleExpandHooks{Version: schemaauthority.StreamCounterExpandHead, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(up)), Before: func(context.Context, *pgxpool.Conn) error { return nil }, After: func(context.Context, pgx.Tx) error { return nil }}
	cases := map[string]string{
		"release":       `LOCK TABLE runtime_change_releases IN ACCESS EXCLUSIVE MODE`,
		"slot":          `LOCK TABLE runtime_change_slots IN ACCESS EXCLUSIVE MODE`,
		"compatibility": `SELECT pg_advisory_xact_lock(86420517831702492)`,
		"reservation":   `LOCK TABLE freeze_idempotency IN ROW EXCLUSIVE MODE`,
		"stream":        `LOCK TABLE stream_delivery_leases IN ROW EXCLUSIVE MODE`,
		"call_log":      `LOCK TABLE call_logs IN ROW EXCLUSIVE MODE`,
		"usage_journal": `LOCK TABLE usage_finalization_journal IN ROW EXCLUSIVE MODE`,
		"request":       `LOCK TABLE request_idempotency IN ROW EXCLUSIVE MODE`,
	}
	for name, lock := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer tx.Rollback(context.Background())
			_, err = tx.Exec(ctx, lock)
			require.NoError(t, err)
			err = migrate.RunCompatibleExpand(ctx, pool, dir, hooks)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "55P03", pgErr.Code)
			_, err = tx.Exec(ctx, `SELECT 1`)
			require.NoError(t, err, "existing owner remains usable")
			require.NoError(t, tx.Commit(ctx))
			var column, ledger bool
			require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='stream_delivery_leases' AND column_name='counter_version'), EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, schemaauthority.StreamCounterExpandHead).Scan(&column, &ledger))
			require.False(t, column)
			require.False(t, ledger)
		})
	}
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, hooks))
}

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
