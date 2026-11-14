package runtimegeneration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRecoverExactUpgradeAdmissionIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	ledger, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	manifest := CurrentManifest(schemaauthority.SupportedSchemaHeads())
	digest, err := manifest.Digest()
	require.NoError(t, err)
	raw := []byte("isolated installed proxy")
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	oldID := "ucu_" + strings.Repeat("a", 32)
	empty := IngressPlan{ChangeID: oldID + "-withdraw", ExpectedConfigDigest: hash, DesiredConfigDigest: hash, DesiredConfigBytes: raw, Before: []Identity{}, After: []Identity{}}
	_, err = PrepareIngress(ctx, pool, empty)
	require.NoError(t, err)
	observed := json.RawMessage(`{"fixture":"observed proxy teardown"}`)
	_, err = CompleteIngress(ctx, pool, empty.ChangeID, hash, true, observed)
	require.NoError(t, err)
	artifact := "sha256:" + strings.Repeat("b", 64)
	member := Identity{"uri_" + strings.Repeat("1", 32), artifact}
	_, _, err = Renew(ctx, pool, member, manifest, ledger, false)
	require.NoError(t, err)
	admission := IngressPlan{ChangeID: oldID + "-admit", PredecessorID: empty.ChangeID, ExpectedConfigDigest: hash, DesiredConfigDigest: hash, DesiredConfigBytes: raw, Before: []Identity{}, After: []Identity{member}}
	_, err = PrepareIngress(ctx, pool, admission)
	require.NoError(t, err)
	status, err := CompleteIngress(ctx, pool, admission.ChangeID, hash, false, observed)
	require.NoError(t, err)
	require.Equal(t, "ambiguous", status)

	var system, beforeObservation string
	var revision int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT system_identifier::text FROM pg_control_system()`).Scan(&system))
	require.NoError(t, pool.QueryRow(ctx, `SELECT observation::text FROM runtime_ingress_changes WHERE change_id=$1`, admission.ChangeID).Scan(&beforeObservation))
	require.NoError(t, pool.QueryRow(ctx, `SELECT revision FROM runtime_generations WHERE instance_id=$1`, member.Instance).Scan(&revision))
	org := testutil.NewTestOrg(t, pool)
	_, err = pool.Exec(ctx, `INSERT INTO freeze_idempotency(request_id,org_id,status,pricing_version)
	 VALUES('admission-retirement',$1,'frozen','test')`, org)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO billing_provider_attempts
	 (org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,route_sequence,route_total,endpoint,provider,channel_name,runtime_instance_id)
	 VALUES($1,'admission-retirement','attempt-1',101,201,301,1,1,'/v1/chat/completions','test-provider','test-channel',$2)`, org, member.Instance)
	require.NoError(t, err)
	in := AdmissionRecovery{ChangeID: admission.ChangeID, ConfigDigest: hash, HostProof: strings.Repeat("5", 64), Departed: []RetirementInput{{
		InstanceID: member.Instance, ArtifactDigest: artifact, ManifestDigest: digest, CompatibilityEpoch: CompatibilityEpoch,
		ExpectedSchemaHead: schemaauthority.LatestSupportedMigration, ExpectedRevision: revision, ExpectedIngressHead: admission.ChangeID + "-withdrawn",
		ExpectedEnvironment: "test", ExpectedDatabaseSystemID: system, HostProofSHA256: "sha256:" + strings.Repeat("6", 64),
	}}}
	run := func(ctx context.Context, in AdmissionRecovery, apply, commit bool) (AdmissionWithdrawal, error) {
		options := pgx.TxOptions{AccessMode: pgx.ReadOnly}
		if apply {
			options = pgx.TxOptions{}
		}
		tx, err := pool.BeginTx(ctx, options)
		if err != nil {
			return AdmissionWithdrawal{}, err
		}
		defer tx.Rollback(ctx)
		if apply {
			if err := schemaauthority.LockExecutionCompatibility(ctx, tx); err != nil {
				return AdmissionWithdrawal{}, err
			}
		}
		out, err := RecoverAdmissionTx(ctx, tx, in, apply)
		if err == nil && commit {
			err = tx.Commit(ctx)
		}
		return out, err
	}
	_, err = run(ctx, in, false, true)
	require.ErrorContains(t, err, "lease-expired")
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()+interval '100 milliseconds' WHERE instance_id=$1`, member.Instance)
	require.NoError(t, err)
	time.Sleep(150 * time.Millisecond)
	// A withdrawal must not detach real unresolved financial ownership.
	_, err = run(ctx, in, false, true)
	require.ErrorContains(t, err, "unresolved financial attempts")
	_, err = run(ctx, in, true, true)
	require.ErrorContains(t, err, "unresolved financial attempts")
	_, err = pool.Exec(ctx, `UPDATE billing_provider_attempts SET state='settled',outcome_certainty='known_usage',
	 financial_resolved_at=clock_timestamp() WHERE org_id=$1 AND request_id='admission-retirement'`, org)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE freeze_idempotency SET status='settled' WHERE org_id=$1 AND request_id='admission-retirement'`, org)
	require.NoError(t, err)
	// A wrong native revision must fail before any withdrawal is authorized.
	wrong := in
	wrong.Departed = append([]RetirementInput(nil), in.Departed...)
	wrong.Departed[0].ExpectedRevision++
	_, err = run(ctx, wrong, true, true)
	require.Error(t, err)
	readStatus := func(want string) {
		plan, have, err := ReadIngress(ctx, pool, admission.ChangeID)
		require.NoError(t, err)
		require.Equal(t, admission, plan)
		require.Equal(t, want, have)
	}
	out, err := run(ctx, in, false, true)
	require.NoError(t, err)
	require.Equal(t, "eligible", out.Status)
	readStatus("ambiguous")
	// No application transaction may commit a cancelled row on its own.
	orphan, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = orphan.Exec(ctx, `UPDATE runtime_ingress_changes SET status='cancelled', completed_at=clock_timestamp() WHERE change_id=$1`, admission.ChangeID)
	require.NoError(t, err)
	require.ErrorContains(t, orphan.Commit(ctx), "must commit together")
	readStatus("ambiguous")
	// A crash after SQL effects but before commit leaves both original states.
	out, err = run(ctx, in, true, false)
	require.NoError(t, err)
	require.Equal(t, "applied", out.Status)
	readStatus("ambiguous")

	type response struct {
		result AdmissionWithdrawal
		err    error
	}
	done := make(chan response, 2)
	for range 2 {
		go func() { r, e := run(ctx, in, true, true); done <- response{r, e} }()
	}
	for range 2 {
		got := <-done
		require.NoError(t, got.err)
		require.Equal(t, AdmissionWithdrawal{admission.ChangeID + "-withdrawn", "applied"}, got.result)
	}
	out, err = run(ctx, in, false, true) // lost response after commit
	require.NoError(t, err)
	require.Equal(t, "applied", out.Status)
	readStatus("cancelled")
	var state, membership, afterObservation string
	require.NoError(t, pool.QueryRow(ctx, `SELECT state,ingress_state FROM runtime_generations WHERE instance_id=$1`, member.Instance).Scan(&state, &membership))
	require.Equal(t, "stopped", state)
	require.Equal(t, "absent", membership)
	require.NoError(t, pool.QueryRow(ctx, `SELECT observation::text FROM runtime_ingress_changes WHERE change_id=$1`, admission.ChangeID).Scan(&afterObservation))
	require.Equal(t, beforeObservation, afterObservation)
	var retainedReceipt string
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM runtime_ingress_changes c
	 WHERE change_id=$1`, admission.ChangeID+"-withdrawn").Scan(&retainedReceipt))
	wrong = in
	wrong.ConfigDigest = strings.Repeat("8", 64)
	_, err = run(ctx, wrong, false, true)
	require.ErrorContains(t, err, "configuration changed")
	_, err = run(ctx, wrong, true, true)
	require.ErrorContains(t, err, "configuration changed")
	var afterWrongDigest string
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM runtime_ingress_changes c
	 WHERE change_id=$1`, admission.ChangeID+"-withdrawn").Scan(&afterWrongDigest))
	require.Equal(t, retainedReceipt, afterWrongDigest)
	var settled int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM billing_provider_attempts
	 WHERE org_id=$1 AND request_id='admission-retirement' AND state='settled' AND runtime_instance_id=$2`,
		org, member.Instance).Scan(&settled))
	require.Equal(t, 1, settled)
	fake := in
	fake.ChangeID = empty.ChangeID
	_, err = run(ctx, fake, true, true)
	require.Error(t, err, "no whole-fleet cancellation API")

	_, err = pool.Exec(ctx, `UPDATE runtime_ingress_changes SET observation='{}' WHERE change_id=$1`, admission.ChangeID)
	require.ErrorContains(t, err, "immutable")
	wrong = in
	wrong.HostProof = strings.Repeat("7", 64)
	_, err = run(ctx, wrong, false, true)
	require.ErrorContains(t, err, "conflicts")

	replacement := Identity{"uri_" + strings.Repeat("2", 32), artifact}
	_, _, err = Renew(ctx, pool, replacement, manifest, ledger, false)
	require.NoError(t, err)
	fresh := admission
	fresh.ChangeID += "-" + strings.Repeat("3", 32)
	fresh.PredecessorID = out.ChangeID
	fresh.After = []Identity{replacement}
	_, err = PrepareIngress(ctx, pool, fresh)
	require.NoError(t, err)
	status, err = CompleteIngress(ctx, pool, fresh.ChangeID, hash, true, observed)
	require.NoError(t, err)
	require.Equal(t, "applied", status)
	allowed, err := BackgroundStartAllowed(ctx, pool, replacement, ledger)
	require.NoError(t, err)
	require.True(t, allowed)

	// The old M129 guard cannot read cancelled history. Exercise the actual
	// down SQL with drained writers, so the refusal is bound to history.
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET state='stopped',ingress_state='absent' WHERE instance_id=$1`, replacement.Instance)
	require.NoError(t, err)
	down, err := os.ReadFile("../../migrations/000130_upgrade_admission_recovery.down.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "cancelled ingress history")
}
