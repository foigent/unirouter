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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func reclaimMigrationDown(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	for _, version := range []string{schemaauthority.XInsightCredentialExpandHead, schemaauthority.XInsightAdoptionExpandHead, schemaauthority.XInsightPolicyExpandHead, schemaauthority.ScopedContentExpandHead, schemaauthority.PreDispatchExpandHead} {
		var applied bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&applied); err != nil {
			return err
		}
		if !applied {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(dir, version+".down.sql"))
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(wire)); err != nil {
			return err
		}
	}
	wire, err := os.ReadFile(filepath.Join(dir, schemaauthority.ContentReclaimExpandHead+".down.sql"))
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	_, err = conn.Exec(ctx, string(wire))
	if err != nil {
		_, rollback := conn.Exec(context.Background(), "ROLLBACK")
		return errors.Join(err, rollback)
	}
	return nil
}
func reclaimMigrationHooks(t *testing.T) migrate.CompatibleExpandHooks {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.ContentReclaimExpandHead+".up.sql"))
	require.NoError(t, err)
	return migrate.CompatibleExpandHooks{Version: schemaauthority.ContentReclaimExpandHead, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(wire)), Before: func(context.Context, *pgxpool.Conn) error { return nil }, After: func(context.Context, pgx.Tx) error { return nil }}
}
func TestUnpublishedContentMigrationPGExactM119PreservesProofAndRejectsMissingPredecessor(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	dir := testutil.MigrationsDir(t)
	proof := func() string {
		var s string
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_jsonb(c)::text FROM execution_source_budget_coverage c WHERE ended_at IS NULL`).Scan(&s))
		return s
	}
	before := proof()
	require.NoError(t, reclaimMigrationDown(t.Context(), pool, dir))
	ledger := contentMigrationLedger(t, pool)
	require.Equal(t, schemaauthority.ContentCaptureExpandHead, ledger[len(ledger)-1])
	var exists bool
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='execution_content_objects'::regclass AND attname='reclaimed_at' AND NOT attisdropped)`).Scan(&exists))
	require.False(t, exists)
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, reclaimMigrationHooks(t)))
	require.Equal(t, before, proof())
	_, err := runtimegeneration.ReadBudgetCoverage(t.Context(), pool)
	require.NoError(t, err)
	require.NoError(t, reclaimMigrationDown(t.Context(), pool, dir))
	require.Equal(t, before, proof())
	_, err = pool.Exec(t.Context(), `DELETE FROM schema_migrations WHERE version=$1`, schemaauthority.ContentCaptureExpandHead)
	require.NoError(t, err)
	require.Error(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, reclaimMigrationHooks(t)))
	require.Equal(t, before, proof())
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='execution_content_objects'::regclass AND attname='reclaimed_at' AND NOT attisdropped)`).Scan(&exists))
	require.False(t, exists)
}

func TestUnpublishedContentMigrationPGRejectsOldRuntimeAdvertisement(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	dir := testutil.MigrationsDir(t)
	require.NoError(t, reclaimMigrationDown(t.Context(), pool, dir))
	ledger, err := schemaauthority.ExpectedLedger(dir)
	require.NoError(t, err)
	identity := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("c", 32), Artifact: "sha256:" + strings.Repeat("d", 64)}
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	manifest.SchemaHeads = slices.DeleteFunc(slices.Clone(manifest.SchemaHeads), func(head string) bool { return head == schemaauthority.ContentReclaimExpandHead })
	_, _, err = runtimegeneration.Renew(t.Context(), pool, identity, manifest, ledger, true)
	require.NoError(t, err)
	require.ErrorContains(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, reclaimMigrationHooks(t)), "must support M120")
	_, err = pool.Exec(t.Context(), `UPDATE runtime_generations SET state='stopped' WHERE instance_id=$1`, identity.Instance)
	require.NoError(t, err)
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, reclaimMigrationHooks(t)))
	_, _, err = runtimegeneration.Renew(t.Context(), pool, identity, manifest, ledger, true)
	require.Error(t, err)
}
