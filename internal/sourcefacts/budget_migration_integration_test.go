//go:build integration

package sourcefacts

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestSourceBudgetExpansionRequiresServingAndExpiredRecoveryWriterSupport(t *testing.T) {
	for _, missing := range []string{"schema_head", "source_budgets", "financial_journal"} {
		t.Run(missing, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			require.NoError(t, budgetDown(t, pool))
			dir := testutil.MigrationsDir(t)
			ledger, err := schemaauthority.ExpectedLedger(dir)
			require.NoError(t, err)
			full := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
			old := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
			if missing == "schema_head" {
				old.SchemaHeads = slices.DeleteFunc(old.SchemaHeads, func(head string) bool { return head == schemaauthority.BudgetSnapshotExpandHead })
			} else {
				old.Implementations = slices.DeleteFunc(old.Implementations, func(ref modelprofile.ImplementationReference) bool { return ref.Kind == missing })
			}
			identity := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("a", 32), Artifact: "sha256:" + strings.Repeat("b", 64)}
			_, _, err = runtimegeneration.Renew(ctx, pool, identity, old, ledger, missing != "schema_head")
			require.NoError(t, err)
			if missing != "schema_head" {
				_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 hour' WHERE instance_id=$1`, identity.Instance)
				require.NoError(t, err)
			}
			wire, err := os.ReadFile(filepath.Join(dir, schemaauthority.BudgetSnapshotExpandHead+".up.sql"))
			require.NoError(t, err)
			digest := fmt.Sprintf("sha256:%x", sha256.Sum256(wire))
			hooks := migrate.CompatibleExpandHooks{Version: schemaauthority.BudgetSnapshotExpandHead, ExpectedSHA256: digest, Before: func(context.Context, *pgxpool.Conn) error { return nil }, After: func(ctx context.Context, tx pgx.Tx) error {
				return runtimegeneration.CertifyBudgetCoverageTx(ctx, tx, full, digest)
			}}
			require.ErrorContains(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks), "all serving and recovery generations")
			var applied bool
			require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, schemaauthority.BudgetSnapshotExpandHead).Scan(&applied))
			require.False(t, applied)
			// Only a stopped old actor ceases to be a supported recovery writer.
			_, err = pool.Exec(ctx, `UPDATE runtime_generations SET state='stopped' WHERE instance_id=$1`, identity.Instance)
			require.NoError(t, err)
			next := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("c", 32), Artifact: "sha256:" + strings.Repeat("d", 64)}
			_, _, err = runtimegeneration.Renew(ctx, pool, next, full, ledger, false)
			require.NoError(t, err)
			require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks))
			_, err = runtimegeneration.ReadBudgetCoverage(ctx, pool)
			require.NoError(t, err)
		})
	}
}

func budgetDown(t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()
	if err := contentDownForBudgetFixture(t, pool); err != nil {
		return err
	}
	wire, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.BudgetSnapshotExpandHead+".down.sql"))
	require.NoError(t, err)
	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(t.Context(), string(wire))
	if err != nil {
		_, _ = conn.Exec(context.Background(), "ROLLBACK")
	} else {
		assertSourceFixtureLedger(t, pool, schemaauthority.ObservationExpandHead)
	}
	return err
}

func assertSourceFixtureLedger(t *testing.T, pool *pgxpool.Pool, head string) []string {
	t.Helper()
	artifact, err := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
	require.NoError(t, err)
	index := slices.Index(artifact, head)
	require.NotEqual(t, -1, index)
	expected := artifact[:index+1]
	var actual []string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT array_agg(version::text ORDER BY version) FROM schema_migrations`).Scan(&actual))
	require.Equal(t, expected, actual)
	return actual
}

func contentDownForBudgetFixture(t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()
	// Later additive migrations must be reversed in actual schema order before
	// establishing the named historical fixture, including repeated M118 calls.
	for _, version := range []string{schemaauthority.XInsightCredentialExpandHead, schemaauthority.XInsightAdoptionExpandHead, schemaauthority.XInsightPolicyExpandHead, schemaauthority.ScopedContentExpandHead, schemaauthority.PreDispatchExpandHead, schemaauthority.ContentReclaimExpandHead, schemaauthority.ContentCaptureExpandHead} {
		if err := sourceBudgetFixtureDownIfPresent(t, pool, version); err != nil {
			return err
		}
	}
	assertSourceFixtureLedger(t, pool, schemaauthority.BudgetSnapshotExpandHead)
	return nil
}

func sourceBudgetFixtureDownIfPresent(t *testing.T, pool *pgxpool.Pool, version string) error {
	t.Helper()
	if version == schemaauthority.XInsightPolicyExpandHead {
		if err := sourceBudgetFixtureDownIfPresent(t, pool, schemaauthority.XInsightAdoptionExpandHead); err != nil {
			return err
		}
	}
	if version == schemaauthority.XInsightAdoptionExpandHead {
		if err := sourceBudgetFixtureDownIfPresent(t, pool, schemaauthority.XInsightCredentialExpandHead); err != nil {
			return err
		}
	}
	var present bool
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&present))
	if present {
		wire, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), version+".down.sql"))
		require.NoError(t, err)
		conn, err := pool.Acquire(t.Context())
		require.NoError(t, err)
		defer conn.Release()
		if _, err = conn.Exec(t.Context(), string(wire)); err != nil {
			_, _ = conn.Exec(context.Background(), "ROLLBACK")
			return err
		}
	}
	return nil
}

func TestSourceBudgetMigrationRequiresCertificateAndPreservesHistory(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	certificate, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, "initial_certification", certificate.Reason)
	require.NoError(t, budgetDown(t, pool))
	dir := testutil.MigrationsDir(t)
	wire, err := os.ReadFile(filepath.Join(dir, schemaauthority.BudgetSnapshotExpandHead+".up.sql"))
	require.NoError(t, err)
	hooks := migrate.CompatibleExpandHooks{Version: schemaauthority.BudgetSnapshotExpandHead, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(wire)), Before: func(context.Context, *pgxpool.Conn) error { return nil }, After: func(context.Context, pgx.Tx) error { return nil }}
	require.ErrorContains(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks), "requires verified support proof")
	var absent bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('execution_source_budget_coverage') IS NULL`).Scan(&absent))
	require.True(t, absent)
	hooks.After = func(ctx context.Context, tx pgx.Tx) error {
		return runtimegeneration.CertifyBudgetCoverageTx(ctx, tx, runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()), hooks.ExpectedSHA256)
	}
	require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks))
	require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks))
	assertSourceFixtureLedger(t, pool, schemaauthority.BudgetSnapshotExpandHead)
	next, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	require.True(t, next.CertifiedFrom.After(certificate.CertifiedFrom))
	var org, project int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status) VALUES('synthetic-D-migration','active') RETURNING id`).Scan(&org))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_cents,budget_period) VALUES($1,'synthetic-D','synthetic-d','active',NULL,'monthly') RETURNING id`, org).Scan(&project))
	var count int
	readCount := func() {
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_budget_definitions WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&count))
	}
	readCount()
	require.Equal(t, 1, count)
	_, err = pool.Exec(ctx, `UPDATE projects SET name='renamed',budget_cents=budget_cents WHERE id=$1`, project)
	require.NoError(t, err)
	readCount()
	require.Equal(t, 1, count)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE projects SET budget_cents=1000 WHERE id=$1`, project)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))
	readCount()
	require.Equal(t, 1, count)
	_, err = pool.Exec(ctx, `UPDATE projects SET budget_cents=1000 WHERE id=$1`, project)
	require.NoError(t, err)
	readCount()
	require.Equal(t, 2, count)
	var kind string
	var cents *int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT change_kind,budget_cents FROM execution_source_budget_definitions WHERE org_id=$1 AND project_id=$2 AND revision=1`, org, project).Scan(&kind, &cents))
	require.Equal(t, "created", kind)
	require.Nil(t, cents)
	_, err = pool.Exec(ctx, `UPDATE execution_source_budget_definitions SET budget_cents=5 WHERE org_id=$1 AND project_id=$2`, org, project)
	require.ErrorContains(t, err, "immutable")
	require.ErrorContains(t, budgetDown(t, pool), "unsafe down refused")
	readCount()
	require.Equal(t, 2, count)
	_, err = pool.Exec(ctx, `UPDATE execution_source_budget_coverage SET certified_from=certified_from-interval '1 day'`)
	require.ErrorContains(t, err, "immutable")
}

func TestSourceBudgetRestoreAlwaysRebasesAndReadinessDoesNotWrite(t *testing.T) {
	for _, head := range []string{schemaauthority.BudgetSnapshotExpandHead, schemaauthority.ContentCaptureExpandHead, schemaauthority.ContentReclaimExpandHead, schemaauthority.PreDispatchExpandHead, schemaauthority.ScopedContentExpandHead} {
		t.Run(head, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			require.NoError(t, sourceBudgetFixtureDownIfPresent(t, pool, schemaauthority.XInsightPolicyExpandHead))
			if head == schemaauthority.BudgetSnapshotExpandHead {
				require.NoError(t, contentDownForBudgetFixture(t, pool))
			} else if head == schemaauthority.ContentCaptureExpandHead {
				require.NoError(t, sourceBudgetFixtureDownIfPresent(t, pool, schemaauthority.ScopedContentExpandHead))
				require.NoError(t, sourceBudgetFixtureDownIfPresent(t, pool, schemaauthority.PreDispatchExpandHead))
				require.NoError(t, sourceBudgetFixtureDownIfPresent(t, pool, schemaauthority.ContentReclaimExpandHead))
			} else if head == schemaauthority.ContentReclaimExpandHead {
				require.NoError(t, sourceBudgetFixtureDownIfPresent(t, pool, schemaauthority.ScopedContentExpandHead))
				require.NoError(t, sourceBudgetFixtureDownIfPresent(t, pool, schemaauthority.PreDispatchExpandHead))
			} else if head == schemaauthority.PreDispatchExpandHead {
				require.NoError(t, sourceBudgetFixtureDownIfPresent(t, pool, schemaauthority.ScopedContentExpandHead))
			}
			assertSourceFixtureLedger(t, pool, head)
			var contentObjects, reclaimedColumn bool
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_regclass('execution_content_objects') IS NOT NULL,
EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('execution_content_objects') AND attname='reclaimed_at' AND NOT attisdropped)`).Scan(&contentObjects, &reclaimedColumn))
			require.Equal(t, head != schemaauthority.BudgetSnapshotExpandHead, contentObjects)
			require.Equal(t, head == schemaauthority.ContentReclaimExpandHead || head == schemaauthority.PreDispatchExpandHead || head == schemaauthority.ScopedContentExpandHead, reclaimedColumn)
			pool, s, org, project := setupSourceDBWithPool(t, pool)
			s.options.ObservationsEnabled = true
			ctx := t.Context()
			snapshot := testSnapshot()
			snapshot.Core.OrgID = fmt.Sprint(org)
			snapshot.Core.ProjectID = fmt.Sprint(project)
			require.NoError(t, s.CaptureAuthenticated(ctx, snapshot))
			sourceBytes := func() string {
				var value string
				require.NoError(t, pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(f) ORDER BY profile,sequence),'[]'::jsonb)::text FROM execution_source_facts f`).Scan(&value))
				return value
			}
			originalSource := sourceBytes()
			var observations int
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE profile=$1`, RuntimeProfile).Scan(&observations))
			require.Positive(t, observations)
			proofBytes := func() string {
				var proof string
				require.NoError(t, pool.QueryRow(ctx, `SELECT (to_jsonb(c)-ARRAY['coverage_epoch','certified_from','ended_at','reason','predecessor_epoch','restore_boundary'])::text FROM execution_source_budget_coverage c WHERE ended_at IS NULL`).Scan(&proof))
				return proof
			}
			originalProof := proofBytes()
			original, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
			require.NoError(t, err)
			helper, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "..", "scripts", "source-budget-restore-rebase.sql"))
			require.NoError(t, err)
			for i := 0; i < 2; i++ {
				_, err = pool.Exec(ctx, string(helper))
				require.NoError(t, err)
				current, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
				require.NoError(t, err)
				require.Greater(t, current.Epoch, original.Epoch)
				require.Equal(t, "restore_rebase", current.Reason)
				require.NotNil(t, current.RestoreBoundary)
				require.Equal(t, current.CertifiedFrom, *current.RestoreBoundary)
				require.True(t, current.CertifiedFrom.After(original.CertifiedFrom))
				var oldFrom, ended time.Time
				require.NoError(t, pool.QueryRow(ctx, `SELECT certified_from,ended_at FROM execution_source_budget_coverage WHERE coverage_epoch=$1`, original.Epoch).Scan(&oldFrom, &ended))
				require.Equal(t, original.CertifiedFrom, oldFrom)
				require.Equal(t, current.CertifiedFrom, ended)
				require.Equal(t, originalProof, proofBytes(), "all original proof fields and manifest bytes survive each committed rebase")
				original = current
			}
			require.Equal(t, originalSource, sourceBytes())
			var before, after string
			require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(c) ORDER BY coverage_epoch)::text FROM execution_source_budget_coverage c`).Scan(&before))
			require.NoError(t, s.CheckReady(ctx))
			_, err = runtimegeneration.ReadBudgetCoverage(ctx, pool)
			require.NoError(t, err)
			require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(c) ORDER BY coverage_epoch)::text FROM execution_source_budget_coverage c`).Scan(&after))
			require.Equal(t, before, after)
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `DELETE FROM schema_migrations WHERE version=$1`, schemaauthority.BudgetSnapshotExpandHead)
			require.NoError(t, err)
			_, err = runtimegeneration.ReadBudgetCoverage(ctx, tx)
			if head == schemaauthority.BudgetSnapshotExpandHead {
				require.ErrorContains(t, err, "schema head mismatch")
			} else {
				require.ErrorContains(t, err, "budget coverage schema ledger: artifact does not contain a known exact compatibility bridge")
			}
			require.NoError(t, tx.Rollback(ctx))
			require.Equal(t, originalSource, sourceBytes())
		})
	}
}

func TestSourceBudgetRestoreHelperPreservesPreDSchema(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	require.NoError(t, budgetDown(t, pool))
	helper, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "..", "scripts", "source-budget-restore-rebase.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(helper))
	require.NoError(t, err)
	var absent bool
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_regclass('execution_source_budget_coverage') IS NULL`).Scan(&absent))
	require.True(t, absent)
}
