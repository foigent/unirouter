//go:build integration

package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestSourceCompatibleExpandRetainsOnlySupportedDormantGenerations(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	dir := testutil.MigrationsDir(t)
	for _, head := range []string{schemaauthority.BudgetSnapshotExpandHead, schemaauthority.ObservationExpandHead} {
		wire, err := os.ReadFile(filepath.Join(dir, head+".down.sql"))
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(wire))
		require.NoError(t, err)
	}
	var head string
	require.NoError(t, pool.QueryRow(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.SourceFactsExpandHead, head)
	ledger, err := schemaauthority.ExpectedLedger(dir)
	require.NoError(t, err)
	full := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	artifact := "sha256:" + strings.Repeat("a", 64)
	old := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("a", 32), Artifact: artifact}
	_, _, err = runtimegeneration.Renew(ctx, pool, old, full, ledger, false)
	require.NoError(t, err)
	// A clean process exit leaves the historical identity and immutable manifest.
	// Expiry here represents elapsed wall time, not retirement or deleted evidence.
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 hour' WHERE instance_id=$1`, old.Instance)
	require.NoError(t, err)
	instances := map[string]bool{}
	for _, c := range []string{"b", "c"} {
		id := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat(c, 32), Artifact: artifact}
		_, _, err = runtimegeneration.Renew(ctx, pool, id, full, ledger, false)
		require.NoError(t, err)
		instances[id.Instance] = true
	}
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	snapshot := func() string {
		var data string
		require.NoError(t, conn.QueryRow(ctx, `SELECT jsonb_build_object('generations',(SELECT jsonb_agg(to_jsonb(g) ORDER BY instance_id) FROM runtime_generations g),'manifests',(SELECT jsonb_agg(to_jsonb(m) ORDER BY artifact_digest) FROM runtime_support_manifests m))::text`).Scan(&data))
		return data
	}
	verify := func(wantError string) {
		t.Helper()
		before := snapshot()
		for _, target := range []string{schemaauthority.ObservationExpandHead, schemaauthority.BudgetSnapshotExpandHead} {
			err := verifyCompatibleRecoveryFleet(ctx, conn, target, instances)
			if wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, wantError)
			}
		}
		require.Equal(t, before, snapshot(), "check must not retire or mutate any identity or manifest")
	}
	verify("")
	_, err = conn.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()+interval '1 hour' WHERE instance_id=$1`, old.Instance)
	require.NoError(t, err)
	verify("unaccounted serving or recovery generation")
	_, err = conn.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 hour',ingress_state='withdrawing' WHERE instance_id=$1`, old.Instance)
	require.NoError(t, err)
	verify("unaccounted serving or recovery generation")
	_, err = conn.Exec(ctx, `UPDATE runtime_generations SET ingress_state='absent' WHERE instance_id=$1`, old.Instance)
	require.NoError(t, err)
	verify("")
	unsupported := full
	unsupported.Implementations = slices.DeleteFunc(slices.Clone(full.Implementations), func(ref modelprofile.ImplementationReference) bool { return ref.Kind == "source_dispatch" })
	wire, err := unsupported.CanonicalBytes()
	require.NoError(t, err)
	unsupportedArtifact := "sha256:" + strings.Repeat("d", 64)
	_, err = conn.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, unsupportedArtifact, fmt.Sprintf("%x", sha256.Sum256(wire)), wire)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,lease_expires_at) VALUES($1,$2,$3,$4,'serving',clock_timestamp()-interval '1 hour')`, "uri_"+strings.Repeat("d", 32), unsupportedArtifact, runtimegeneration.CompatibilityEpoch, head)
	require.NoError(t, err)
	verify("unsupported dormant recovery generation")
}
