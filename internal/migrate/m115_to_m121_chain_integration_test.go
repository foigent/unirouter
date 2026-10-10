//go:build integration

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
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

// This exercises the candidate binary's complete real-PG schema chain with an
// existing M115 key. The Target's actual backup, process fleet and host controller
// are separate deployment gates; this test never treats a synthetic DB as those.
func TestM115ToM125CompatibleExpandPreservesExistingKey(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	dir := testutil.PreRetirementMigrationsDir(t)
	ctx := t.Context()
	artifact, err := testutil.PreRetirementLedger(t)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.XInsightCredentialExpandHead, artifact[len(artifact)-1])
	require.NoError(t, testutil.RewindEmptySourceFactsForLegacyTest(ctx, pool, dir))
	ledger := contentMigrationLedger(t, pool)
	require.Equal(t, schemaauthority.StreamCounterExpandHead, ledger[len(ledger)-1])
	require.NoError(t, schemaauthority.VerifyRuntimeLedger(artifact, ledger))

	org := testutil.NewTestOrg(t, pool)
	var project, key int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_period)
		VALUES($1,'Existing M115 project','existing-m115','active','none') RETURNING id`, org).Scan(&project))
	keyHash := strings.Repeat("a", 64)
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,
		allowed_ips,allowed_models) VALUES($1,$2,'service','Existing M115 key',$3,'m115-existing','active','','') RETURNING id`,
		org, project, keyHash).Scan(&key))
	readKey := func() string {
		var hash string
		require.NoError(t, pool.QueryRow(ctx, `SELECT key_hash FROM api_keys WHERE id=$1 AND org_id=$2 AND project_id=$3`,
			key, org, project).Scan(&hash))
		return hash
	}
	require.Equal(t, keyHash, readKey())

	for _, version := range []string{schemaauthority.SourceFactsExpandHead, schemaauthority.ObservationExpandHead,
		schemaauthority.BudgetSnapshotExpandHead, schemaauthority.ContentCaptureExpandHead,
		schemaauthority.ContentReclaimExpandHead, schemaauthority.PreDispatchExpandHead,
		schemaauthority.ScopedContentExpandHead, schemaauthority.XInsightPolicyExpandHead, schemaauthority.XInsightAdoptionExpandHead, schemaauthority.XInsightCredentialExpandHead} {
		t.Run(version, func(t *testing.T) {
			before := contentMigrationLedger(t, pool)
			require.NoError(t, migrate.RunRuntime(ctx, pool, dir), "candidate startup must preserve pending explicit expansion")
			require.Equal(t, before, contentMigrationLedger(t, pool))
			wire, err := os.ReadFile(filepath.Join(dir, version+".up.sql"))
			require.NoError(t, err)
			digest := fmt.Sprintf("sha256:%x", sha256.Sum256(wire))
			hooks := migrate.CompatibleExpandHooks{Version: version, ExpectedSHA256: "sha256:" + strings.Repeat("0", 64),
				Before: func(context.Context, *pgxpool.Conn) error { return nil },
				After:  func(context.Context, pgx.Tx) error { return nil }}
			require.Error(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks), "wrong immutable bytes must fail")
			require.Equal(t, before, contentMigrationLedger(t, pool))
			hooks.ExpectedSHA256 = digest
			if version == schemaauthority.ScopedContentExpandHead {
				// An exact M123 file cannot skip M122 when the real ledger is at M121.
				future, readErr := os.ReadFile(filepath.Join(dir, schemaauthority.XInsightPolicyExpandHead+".up.sql"))
				require.NoError(t, readErr)
				futureHooks := hooks
				futureHooks.Version = schemaauthority.XInsightPolicyExpandHead
				futureHooks.ExpectedSHA256 = fmt.Sprintf("sha256:%x", sha256.Sum256(future))
				require.ErrorContains(t, migrate.RunCompatibleExpand(ctx, pool, dir, futureHooks), "pending compatible expansion 000122")
				require.Equal(t, before, contentMigrationLedger(t, pool))
			}
			if version == schemaauthority.BudgetSnapshotExpandHead {
				hooks.After = func(ctx context.Context, tx pgx.Tx) error {
					return runtimegeneration.CertifyBudgetCoverageTx(ctx, tx,
						runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()), digest)
				}
			}
			require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks))
			after := contentMigrationLedger(t, pool)
			require.Equal(t, append(before, version), after)
			require.NoError(t, schemaauthority.VerifyRuntimeLedger(artifact, after))
			require.Equal(t, keyHash, readKey())
			if version >= schemaauthority.BudgetSnapshotExpandHead {
				_, err = runtimegeneration.ReadBudgetCoverage(ctx, pool)
				require.NoError(t, err)
			}
		})
	}
	require.Equal(t, artifact, contentMigrationLedger(t, pool))
	require.Equal(t, keyHash, readKey())
	down125, err := os.ReadFile(filepath.Join(dir, schemaauthority.XInsightCredentialExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down125))
	require.NoError(t, err)
	artifact = artifact[:len(artifact)-1]
	down124, err := os.ReadFile(filepath.Join(dir, schemaauthority.XInsightAdoptionExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down124))
	require.NoError(t, err)
	artifact = artifact[:len(artifact)-1]
	_, err = pool.Exec(ctx, `INSERT INTO audit_logs(org_id,user_id,source,action,resource,actor_kind,actor_ref)
		VALUES($1,NULL,'system','m123.chain.down.refusal','migration','system','system')`, org)
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(dir, schemaauthority.XInsightPolicyExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "unsafe down refused")
	require.Equal(t, artifact, contentMigrationLedger(t, pool))
	require.Equal(t, keyHash, readKey())
}
