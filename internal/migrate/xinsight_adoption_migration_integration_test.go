//go:build integration

package migrate_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestXInsightAdoptionM124RuntimeGateAndEmptyDownIntegration(t *testing.T) {
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightPolicyExpandHead)
	ctx := t.Context()
	dir := testutil.PreRetirementMigrationsDir(t)
	up, e := os.ReadFile(filepath.Join(dir, schemaauthority.XInsightAdoptionExpandHead+".up.sql"))
	require.NoError(t, e)
	down, e := os.ReadFile(filepath.Join(dir, schemaauthority.XInsightAdoptionExpandHead+".down.sql"))
	require.NoError(t, e)
	full := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	old := full
	old.Implementations = slices.DeleteFunc(slices.Clone(full.Implementations), func(r modelprofile.ImplementationReference) bool { return r.Kind == "xinsight_connector" })
	wire, e := old.CanonicalBytes()
	require.NoError(t, e)
	digest, e := old.Digest()
	require.NoError(t, e)
	artifact := "sha256:" + strings.Repeat("a", 64)
	instance := "uri_" + strings.Repeat("b", 32)
	_, e = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, artifact, digest, wire)
	require.NoError(t, e)
	_, e = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at) VALUES($1,$2,$3,$4,'serving','absent',clock_timestamp()+interval '1 minute')`, instance, artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.XInsightPolicyExpandHead)
	require.NoError(t, e)
	hooks := migrate.CompatibleExpandHooks{Version: schemaauthority.XInsightAdoptionExpandHead, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(up)), Before: func(context.Context, *pgxpool.Conn) error { return nil }, After: func(context.Context, pgx.Tx) error { return nil }}
	require.ErrorContains(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks), "lacks M124 receiver adoption support")
	require.Equal(t, schemaauthority.XInsightPolicyExpandHead, contentMigrationLedger(t, pool)[len(contentMigrationLedger(t, pool))-1])
	_, e = pool.Exec(ctx, `UPDATE runtime_generations SET state='stopped' WHERE instance_id=$1`, instance)
	require.NoError(t, e)
	require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks))
	_, e = runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, e)
	_, e = pool.Exec(ctx, `UPDATE runtime_generations SET state='serving' WHERE instance_id=$1`, instance)
	require.ErrorContains(t, e, "lacks M124 receiver adoption support")
	// Empty guarded down returns the exact M123 ledger and original verifier.
	_, e = pool.Exec(ctx, string(down))
	require.NoError(t, e)
	head := contentMigrationLedger(t, pool)
	require.Equal(t, schemaauthority.XInsightPolicyExpandHead, head[len(head)-1])
	_, e = runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, e)
	require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks))
	// A retained adoption intent alone is already sufficient to forbid downgrade.
	org := testutil.NewTestOrg(t, pool)
	var project, actor int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,role,status) VALUES($1,'adoption-down-owner','test','platform_admin','active') RETURNING id`, org).Scan(&actor))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,created_by) VALUES($1,'adoption-down','adoption-down','active',$2) RETURNING id`, org, actor).Scan(&project))
	_, e = pool.Exec(ctx, `INSERT INTO xinsight_connector_installations(installation_id,org_id,project_id,x_deployment_id,invitation_sha256,event_scope,source_instance,source_binding_sha256,desired_state,observed_state,consented_by,receiver_mode) VALUES('xci_AAAAAAAAAAAAAAAAAAAAAA',$1,$2,'xid_ZZZZZZZZZZZZZZZZZZZZZZ',$3,'model.request.completed','synthetic-source',$3,'active','provisioning',$4,'adopt_existing')`, org, project, "sha256:"+strings.Repeat("a", 64), actor)
	require.NoError(t, e)
	conn, e := pool.Acquire(ctx)
	require.NoError(t, e)
	defer conn.Release()
	_, e = conn.Exec(ctx, string(down))
	require.ErrorContains(t, e, "unsafe down refused")
	_, e = conn.Exec(ctx, "ROLLBACK")
	require.NoError(t, e)
	require.Equal(t, schemaauthority.XInsightAdoptionExpandHead, contentMigrationLedger(t, pool)[len(contentMigrationLedger(t, pool))-1])
}
func TestM124RestoreRebaseKeepsM118ProofAndRejectsMissingPredecessor(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	raw, e := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "..", "scripts", "source-budget-restore-rebase.sql"))
	require.NoError(t, e)
	var before string
	require.NoError(t, pool.QueryRow(ctx, `SELECT concat_ws('|',schema_head,migration_sha256,schema_ledger_sha256,support_manifest_sha256) FROM execution_source_budget_coverage WHERE ended_at IS NULL`).Scan(&before))
	_, e = pool.Exec(ctx, string(raw))
	require.NoError(t, e)
	var after string
	require.NoError(t, pool.QueryRow(ctx, `SELECT concat_ws('|',schema_head,migration_sha256,schema_ledger_sha256,support_manifest_sha256) FROM execution_source_budget_coverage WHERE ended_at IS NULL`).Scan(&after))
	require.Equal(t, before, after)
	_, e = runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, e)
	var history int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_budget_coverage WHERE ended_at IS NOT NULL`).Scan(&history))
	require.Equal(t, 1, history)
	conn, e := pool.Acquire(ctx)
	require.NoError(t, e)
	defer conn.Release()
	_, e = conn.Exec(ctx, `BEGIN; DELETE FROM schema_migrations WHERE version='000123_xinsight_policy_delegation'`)
	require.NoError(t, e)
	// Keep the altered synthetic ledger confined to this transaction.
	body := strings.TrimSuffix(strings.TrimPrefix(string(raw[strings.Index(string(raw), "BEGIN;"):]), "BEGIN;"), "COMMIT;\n")
	_, e = conn.Exec(ctx, body)
	require.ErrorContains(t, e, "M124 restore requires exact adoption schema and M123 predecessor")
	_, e = conn.Exec(ctx, "ROLLBACK")
	require.NoError(t, e)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_budget_coverage WHERE ended_at IS NOT NULL`).Scan(&history))
	require.Equal(t, 1, history)
}
