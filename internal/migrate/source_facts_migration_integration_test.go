//go:build integration

package migrate_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func sourceExpandHooks(t *testing.T) migrate.CompatibleExpandHooks {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.SourceFactsExpandHead+".up.sql"))
	require.NoError(t, err)
	return migrate.CompatibleExpandHooks{Version: schemaauthority.SourceFactsExpandHead,
		ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(wire)),
		Before:         func(context.Context, *pgxpool.Conn) error { return nil },
		After:          func(context.Context, pgx.Tx) error { return nil }}
}

func sourceExpandPresent(t *testing.T, pool *pgxpool.Pool, want bool) {
	t.Helper()
	var schema, ledger bool
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_regclass('execution_source_facts') IS NOT NULL,
	 EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, schemaauthority.SourceFactsExpandHead).Scan(&schema, &ledger))
	require.Equal(t, want, schema)
	require.Equal(t, want, ledger)
}

func TestSourceFactsExpandAtomicityAndFiniteBridge(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	dir := testutil.MigrationsDir(t)
	ctx := t.Context()
	artifact, err := schemaauthority.ExpectedLedger(dir)
	require.NoError(t, err)
	require.NoError(t, schemaauthority.VerifyRuntimeLedger(artifact, artifact))
	require.Error(t, schemaauthority.VerifyRuntimeLedger(artifact[:len(artifact)-1], artifact), "M115 binary cannot resume on M116")
	require.Error(t, schemaauthority.VerifyRuntimeLedger(artifact, append(slices.Clone(artifact), "000117_unapproved")))
	require.NoError(t, testutil.RewindEmptySourceFactsForLegacyTest(ctx, pool, dir))
	sourceExpandPresent(t, pool, false)
	require.NoError(t, schemaauthority.VerifyRuntimeLedger(artifact, artifact[:len(artifact)-1]))
	require.NoError(t, migrate.RunRuntime(ctx, pool, dir), "bridge startup must preserve pending expansion")
	sourceExpandPresent(t, pool, false)
	require.ErrorContains(t, migrate.Run(ctx, pool, dir), "requires explicit fleet preflight")
	hooks := sourceExpandHooks(t)
	hooks.After = func(context.Context, pgx.Tx) error { return errors.New("injected bootstrap rollback") }
	require.ErrorContains(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks), "injected bootstrap rollback")
	sourceExpandPresent(t, pool, false)
	hooks = sourceExpandHooks(t)
	require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks))
	require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks), "same immutable bytes retry is idempotent")
	sourceExpandPresent(t, pool, true)
	require.NoError(t, testutil.RewindEmptySourceFactsForLegacyTest(ctx, pool, dir))
	sourceExpandPresent(t, pool, false)
	require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks))
}

func TestSourceFactsExpandBusyOwnersPreserveTheirTransactions(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	dir := testutil.MigrationsDir(t)
	require.NoError(t, testutil.RewindEmptySourceFactsForLegacyTest(t.Context(), pool, dir))
	for name, lock := range map[string]string{
		"release":       `LOCK TABLE runtime_change_releases IN ACCESS EXCLUSIVE MODE`,
		"slot":          `LOCK TABLE runtime_change_slots IN ACCESS EXCLUSIVE MODE`,
		"compatibility": `SELECT pg_advisory_xact_lock(86420517831702492)`,
		"keys":          `LOCK TABLE api_keys IN ROW EXCLUSIVE MODE`,
		"reservation":   `LOCK TABLE freeze_idempotency IN ROW EXCLUSIVE MODE`,
		"media":         `LOCK TABLE async_media_jobs IN ROW EXCLUSIVE MODE`,
		"projects":      `LOCK TABLE projects IN ROW EXCLUSIVE MODE`,
		"micro_marker":  `LOCK TABLE micro_ledger_runtime_state IN ROW EXCLUSIVE MODE`,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer tx.Rollback(context.Background())
			_, err = tx.Exec(ctx, lock)
			require.NoError(t, err)
			definitionBefore := sourceMarkerDefinition(t, pool)
			markerBefore := sourceMarkerState(t, pool)
			err = migrate.RunCompatibleExpand(ctx, pool, dir, sourceExpandHooks(t))
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "55P03", pgErr.Code)
			require.Equal(t, definitionBefore, sourceMarkerDefinition(t, pool))
			require.Equal(t, markerBefore, sourceMarkerState(t, pool))
			_, err = tx.Exec(ctx, `SELECT 1`)
			require.NoError(t, err)
			// The earlier projects owner can still enter the original financial
			// trigger after migration yields; it is never a deadlock victim.
			_, err = tx.Exec(ctx, `UPDATE wallets SET balance_micros=balance_micros WHERE FALSE`)
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))
			sourceExpandPresent(t, pool, false)
		})
	}
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, sourceExpandHooks(t)))
}

func TestSourceFactsExpandRejectsIncompatibleServingAndRecovery(t *testing.T) {
	for _, missing := range []string{"head", "unirouter.source-fact.v1", "unirouter.stream-counter.v2", "unirouter.stream-counter.v3"} {
		t.Run(missing, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			dir := testutil.MigrationsDir(t)
			require.NoError(t, testutil.RewindEmptySourceFactsForLegacyTest(t.Context(), pool, dir))
			artifact, err := schemaauthority.ExpectedLedger(dir)
			require.NoError(t, err)
			manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
			if missing == "head" {
				manifest.SchemaHeads = manifest.SchemaHeads[:3]
			} else {
				manifest.Implementations = slices.DeleteFunc(manifest.Implementations, func(ref modelprofile.ImplementationReference) bool { return ref.ID == missing })
			}
			member := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("d", 32), Artifact: "sha256:" + strings.Repeat("d", 64)}
			_, head, err := runtimegeneration.Renew(t.Context(), pool, member, manifest, artifact, true)
			require.NoError(t, err)
			require.Equal(t, schemaauthority.StreamCounterExpandHead, head)
			require.ErrorContains(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, sourceExpandHooks(t)), "all serving and recovery generations")
			sourceExpandPresent(t, pool, false)
		})
	}
}

func TestSourceFactsDownRefusesLiveGenerationAndDurableSeeds(t *testing.T) {
	for _, evidence := range []string{"generation", "stream", "reservation"} {
		t.Run(evidence, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			dir := testutil.MigrationsDir(t)
			if evidence == "generation" {
				artifact, err := schemaauthority.ExpectedLedger(dir)
				require.NoError(t, err)
				_, _, err = runtimegeneration.Renew(ctx, pool, runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("f", 32), Artifact: "sha256:" + strings.Repeat("f", 64)}, runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()), artifact, true)
				require.NoError(t, err)
			} else {
				var org, project int64
				require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('down-test','active','standard') RETURNING id`).Scan(&org))
				require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'Down','down','active','none') RETURNING id`, org).Scan(&project))
				var err error
				if evidence == "stream" {
					_, err = pool.Exec(ctx, `INSERT INTO execution_source_streams(source_instance,org_id,project_id,profile) VALUES('synthetic',$1,$2,'unirouter.execution-context.v1')`, org, project)
				} else {
					_, err = pool.Exec(ctx, `INSERT INTO freeze_idempotency(request_id,org_id,project_id,status,billing_mode,model_name,pricing_version,source_context) VALUES('down-test',$1,$2,'frozen','cost_observation','synthetic','test',convert_to('{}','UTF8'))`, org, project)
				}
				require.NoError(t, err)
			}
			require.ErrorContains(t, testutil.RewindEmptySourceFactsForLegacyTest(ctx, pool, dir), "unsafe down refused")
			sourceExpandPresent(t, pool, true)
		})
	}
}
