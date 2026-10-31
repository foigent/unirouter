package runtimegeneration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestBackgroundStartNativeAuthorityIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	ledger, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	identity := Identity{"uri_" + strings.Repeat("a", 32), "sha256:" + strings.Repeat("a", 64)}
	standby := Identity{"uri_" + strings.Repeat("b", 32), identity.Artifact}
	for _, id := range []Identity{identity, standby} {
		_, _, err = Renew(ctx, pool, id, CurrentManifest(schemaauthority.SupportedSchemaHeads()), ledger, false)
		require.NoError(t, err)
	}
	allowed := func(want bool) {
		t.Helper()
		actual, err := BackgroundStartAllowed(ctx, pool, standby, ledger)
		require.NoError(t, err)
		require.Equal(t, want, actual)
	}
	allowed(true) // No ingress history is the supported fresh path.
	plan := func(id, previous, expected string, before, after []Identity) IngressPlan {
		wire := []byte(id)
		return IngressPlan{ChangeID: id, PredecessorID: previous, ExpectedConfigDigest: expected,
			DesiredConfigBytes: wire, DesiredConfigDigest: fmt.Sprintf("%x", sha256.Sum256(wire)), Before: before, After: after}
	}
	apply := func(p IngressPlan) {
		t.Helper()
		_, err := PrepareIngress(ctx, pool, p)
		require.NoError(t, err)
		allowed(false)
		_, err = CompleteIngress(ctx, pool, p.ChangeID, p.DesiredConfigDigest, true, json.RawMessage(`{"synthetic":true}`))
		require.NoError(t, err)
	}
	first := plan("first", "", strings.Repeat("0", 64), []Identity{}, []Identity{identity})
	apply(first)
	allowed(true) // A live standby is allowed without granting it ingress.
	withdraw := plan("empty", first.ChangeID, first.DesiredConfigDigest, []Identity{identity}, []Identity{})
	apply(withdraw)
	allowed(false)
	join := plan("join", withdraw.ChangeID, withdraw.DesiredConfigDigest, []Identity{}, []Identity{identity})
	_, err = PrepareIngress(ctx, pool, join)
	require.NoError(t, err)
	allowed(false)
	_, err = CompleteIngress(ctx, pool, join.ChangeID, strings.Repeat("0", 64), false, json.RawMessage(`{"synthetic":"ambiguous"}`))
	require.NoError(t, err)
	allowed(false)
	_, err = CompleteIngress(ctx, pool, join.ChangeID, join.DesiredConfigDigest, true, json.RawMessage(`{"synthetic":true}`))
	require.NoError(t, err)
	allowed(true)
	// Corrupt only the exclusively owned synthetic database, inside rollback
	// transactions or reversible fixture updates. Never infer fresh on errors.
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE instance_id=$1`, standby.Instance)
	require.NoError(t, err)
	allowed(false)
	_, _, err = Renew(ctx, pool, standby, CurrentManifest(schemaauthority.SupportedSchemaHeads()), ledger, false)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET schema_head='unknown' WHERE instance_id=$1`, standby.Instance)
	require.NoError(t, err)
	allowed(false)
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET schema_head=$2,state='stopped' WHERE instance_id=$1`, standby.Instance, ledger[len(ledger)-1])
	require.NoError(t, err)
	allowed(false)
	missing := Identity{"uri_" + strings.Repeat("c", 32), identity.Artifact}
	actual, err := BackgroundStartAllowed(ctx, pool, missing, ledger)
	require.Error(t, err)
	require.False(t, actual)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	actual, err = BackgroundStartAllowed(canceled, pool, standby, ledger)
	require.Error(t, err)
	require.False(t, actual)
	// Native guards correctly forbid corrupting real receipts or resurrecting a
	// stopped identity. Use a guardless shadow only for impossible/corrupt-head
	// defensive reads, never disable native guards or modify their contract.
	_, err = pool.Exec(ctx, `CREATE SCHEMA background_corrupt;
CREATE TABLE background_corrupt.runtime_ingress_changes AS TABLE public.runtime_ingress_changes`)
	require.NoError(t, err)
	config := pool.Config().Copy()
	config.ConnConfig.RuntimeParams["search_path"] = "background_corrupt,public"
	shadow, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	defer shadow.Close()
	_, err = shadow.Exec(ctx, `UPDATE runtime_ingress_changes SET desired_config_bytes=decode('00','hex') WHERE change_id=$1`, join.ChangeID)
	require.NoError(t, err)
	actual, err = BackgroundStartAllowed(ctx, shadow, identity, ledger)
	require.Error(t, err)
	require.False(t, actual)
	_, err = shadow.Exec(ctx, `UPDATE runtime_ingress_changes SET desired_config_bytes=$2 WHERE change_id=$1`, join.ChangeID, join.DesiredConfigBytes)
	require.NoError(t, err)
	_, err = shadow.Exec(ctx, `INSERT INTO runtime_ingress_changes(change_id,expected_config_digest,desired_config_digest,desired_config_bytes,membership_before,membership_after,status)
SELECT 'fork',expected_config_digest,desired_config_digest,desired_config_bytes,membership_before,membership_after,status FROM runtime_ingress_changes WHERE change_id=$1`, join.ChangeID)
	require.NoError(t, err)
	actual, err = BackgroundStartAllowed(ctx, shadow, identity, ledger)
	require.Error(t, err)
	require.False(t, actual)
	_, err = shadow.Exec(ctx, `DELETE FROM runtime_ingress_changes WHERE change_id='fork';
UPDATE runtime_ingress_changes SET predecessor_id='join' WHERE change_id='first'`)
	require.NoError(t, err)
	actual, err = BackgroundStartAllowed(ctx, shadow, identity, ledger)
	require.NoError(t, err)
	require.False(t, actual, "a cycle with no head is not a fresh install")
}
