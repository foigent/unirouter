package runtimegeneration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestIngressIntentRetainsPotentialReceiversThroughAmbiguity(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	ledger, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	manifest := CurrentManifest([]string{schemaauthority.LegacyCapabilityHead, schemaauthority.CapabilityExpandHead})
	first := Identity{"uri_" + strings.Repeat("a", 32), "sha256:" + strings.Repeat("a", 64)}
	second := Identity{"uri_" + strings.Repeat("b", 32), "sha256:" + strings.Repeat("b", 64)}
	for _, i := range []Identity{first, second} {
		until, head, err := Renew(ctx, pool, i, manifest, ledger, false)
		require.NoError(t, err)
		require.Equal(t, schemaauthority.CapabilityExpandHead, head)
		require.True(t, until.After(time.Now()))
	}
	wire := []byte("isolated config both")
	plan := IngressPlan{ChangeID: "bootstrap-fixture", ExpectedConfigDigest: strings.Repeat("0", 64), DesiredConfigBytes: wire, DesiredConfigDigest: fmt.Sprintf("%x", sha256.Sum256(wire)), Before: []Identity{}, After: []Identity{first, second}}
	status, err := PrepareIngress(ctx, pool, plan)
	require.NoError(t, err)
	require.Equal(t, "pending", status)
	status, err = PrepareIngress(ctx, pool, plan)
	require.NoError(t, err)
	require.Equal(t, "pending", status)
	status, err = CompleteIngress(ctx, pool, plan.ChangeID, plan.DesiredConfigDigest, false, json.RawMessage(`{"fixture":"old proxy still running"}`))
	require.NoError(t, err)
	require.Equal(t, "ambiguous", status)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM runtime_generations WHERE ingress_state<>'absent'`).Scan(&count))
	require.Equal(t, 2, count)
	status, err = CompleteIngress(ctx, pool, plan.ChangeID, plan.DesiredConfigDigest, true, json.RawMessage(`{"fixture":"old proxy exited"}`))
	require.NoError(t, err)
	require.Equal(t, "applied", status)
	_, err = pool.Exec(ctx, `UPDATE runtime_ingress_changes SET desired_config_digest=$2 WHERE change_id=$1`, plan.ChangeID, strings.Repeat("0", 64))
	require.Error(t, err)
	next := plan
	next.ChangeID = "withdraw-fixture"
	next.PredecessorID = plan.ChangeID
	next.ExpectedConfigDigest = plan.DesiredConfigDigest
	next.Before = plan.After
	next.After = []Identity{second}
	next.DesiredConfigBytes = []byte("isolated config second")
	next.DesiredConfigDigest = fmt.Sprintf("%x", sha256.Sum256(next.DesiredConfigBytes))
	_, err = PrepareIngress(ctx, pool, next)
	require.NoError(t, err)
	var state string
	require.NoError(t, pool.QueryRow(ctx, `SELECT ingress_state FROM runtime_generations WHERE instance_id=$1`, first.Instance).Scan(&state))
	require.Equal(t, "withdrawing", state)
	_, _, err = Renew(ctx, pool, first, manifest, ledger, true)
	require.NoError(t, err)
	// A third operation cannot overtake an unconfirmed external reload.
	third := next
	third.ChangeID = "overtaking-fixture"
	third.PredecessorID = next.ChangeID
	_, err = PrepareIngress(ctx, pool, third)
	require.Error(t, err)
	_, err = CompleteIngress(ctx, pool, next.ChangeID, next.DesiredConfigDigest, true, json.RawMessage(`{"fixture":"withdrawn"}`))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT ingress_state FROM runtime_generations WHERE instance_id=$1`, first.Instance).Scan(&state))
	require.Equal(t, "absent", state)
	// A waiting RR owner must see changes committed while it waited. This is
	// the actual domain lock used by both ingress and lifecycle, not a mock.
	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer blocker.Rollback(ctx)
	require.NoError(t, schemaauthority.LockExecutionCompatibility(ctx, blocker))
	waiting, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer waiting.Rollback(ctx)
	_, err = waiting.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`)
	require.NoError(t, err)
	acquired := make(chan error, 1)
	go func() { acquired <- schemaauthority.LockExecutionCompatibility(ctx, waiting) }()
	_, err = blocker.Exec(ctx, `UPDATE runtime_generations SET state='stopped' WHERE instance_id=$1`, first.Instance)
	require.NoError(t, err)
	require.NoError(t, blocker.Commit(ctx))
	require.NoError(t, <-acquired)
	require.NoError(t, waiting.QueryRow(ctx, `SELECT state FROM runtime_generations WHERE instance_id=$1`, first.Instance).Scan(&state))
	require.Equal(t, "stopped", state)
	require.NoError(t, waiting.Rollback(ctx))
	_, _, err = Renew(context.Background(), pool, first, manifest, ledger, false)
	require.Error(t, err, "stopped instance cannot resurrect")
}

func TestHeartbeatPreservesLeaseWithoutGrantingAuthorityWhilePublicationLockedIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	ledger, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	manifest := CurrentManifest([]string{schemaauthority.LegacyCapabilityHead, schemaauthority.CapabilityExpandHead})
	identity := Identity{"uri_" + strings.Repeat("c", 32), "sha256:" + strings.Repeat("c", 64)}
	_, _, err = Renew(ctx, pool, identity, manifest, ledger, false)
	require.NoError(t, err)
	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer blocker.Rollback(ctx)
	require.NoError(t, schemaauthority.LockExecutionCompatibility(ctx, blocker))
	// A publication may keep this lock while validating its full projection.
	// An unchanged heartbeat must still renew its own bounded lease.
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	until, head, err := Renew(bounded, pool, identity, manifest, ledger, false)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.CapabilityExpandHead, head)
	require.True(t, until.After(time.Now()))
	var membership string
	require.NoError(t, pool.QueryRow(ctx, `SELECT ingress_state FROM runtime_generations WHERE instance_id=$1`, identity.Instance).Scan(&membership))
	require.Equal(t, "absent", membership)
	// Registration grants new authority and must retain the same serialization.
	joining, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	other := Identity{"uri_" + strings.Repeat("d", 32), identity.Artifact}
	_, _, err = Renew(joining, pool, other, manifest, ledger, false)
	require.Error(t, err)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM runtime_generations WHERE instance_id=$1`, other.Instance).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, blocker.Rollback(ctx))
	changed := manifest
	changed.SchemaHeads = []string{schemaauthority.CapabilityExpandHead}
	_, _, err = Renew(ctx, pool, identity, changed, ledger, false)
	require.ErrorIs(t, err, ErrIncompatible, "heartbeat must not replace retained support")
}
