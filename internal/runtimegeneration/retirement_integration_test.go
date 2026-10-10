//go:build integration

package runtimegeneration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestHistoricalM115RetirementPermitsOnlyProvedSuffix(t *testing.T) {
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.StreamCounterExpandHead)
	ctx := t.Context()
	dir := testutil.PreRetirementMigrationsDir(t)
	manifestWire, err := os.ReadFile("testdata/runtime-support-1.2.77-m115.json")
	require.NoError(t, err)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(manifestWire)))))
	manifestWire = []byte(strings.TrimSpace(string(manifestWire)))
	manifest, err := VerifyManifest(manifestWire, digest)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.StreamCounterExpandHead, manifest.SchemaHeads[len(manifest.SchemaHeads)-1])
	old := Identity{Instance: "uri_" + strings.Repeat("a", 32), Artifact: "sha256:" + strings.Repeat("a", 64)}
	_, err = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, old.Artifact, digest, manifestWire)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at)
	 VALUES($1,$2,$3,$4,'serving','absent',clock_timestamp()-interval '1 hour')`, old.Instance, old.Artifact, CompatibilityEpoch, schemaauthority.StreamCounterExpandHead)
	require.NoError(t, err)
	configWire := []byte("fixture-empty-ingress")
	configDigest := fmt.Sprintf("%x", sha256.Sum256(configWire))
	_, err = pool.Exec(ctx, `INSERT INTO runtime_ingress_changes(change_id,predecessor_id,expected_config_digest,desired_config_digest,desired_config_bytes,membership_before,membership_after,status,completed_at)
	 VALUES('fixture-head',NULL,$1,$2,$3,'[]','[]','applied',clock_timestamp())`, strings.Repeat("0", 64), configDigest, configWire)
	require.NoError(t, err)
	var systemID string
	require.NoError(t, pool.QueryRow(ctx, `SELECT system_identifier::text FROM pg_control_system()`).Scan(&systemID))
	input := RetirementInput{InstanceID: old.Instance, ArtifactDigest: old.Artifact, ManifestDigest: digest,
		CompatibilityEpoch: CompatibilityEpoch, ExpectedSchemaHead: schemaauthority.StreamCounterExpandHead,
		ExpectedRevision: 1, ExpectedIngressHead: "fixture-head", ExpectedEnvironment: "test",
		ExpectedDatabaseSystemID: systemID, HostProofSHA256: "sha256:" + strings.Repeat("f", 64)}

	// The real old binary's manifest lacks M117. M116 permits an already absent,
	// expired generation; M117 still rejects it until formal stop commits.
	up := func(head string) error {
		wire, readErr := os.ReadFile(filepath.Join(dir, head+".up.sql"))
		require.NoError(t, readErr)
		_, execErr := pool.Exec(ctx, string(wire))
		if execErr != nil {
			return execErr
		}
		_, execErr = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, head)
		return execErr
	}
	require.NoError(t, up(schemaauthority.SourceFactsExpandHead))
	ledger, err := testutil.PreRetirementLedger(t)
	require.NoError(t, err)
	currentManifest := CurrentManifest(schemaauthority.SupportedSchemaHeads())
	successors := []Identity{{Instance: "uri_" + strings.Repeat("b", 32), Artifact: "sha256:" + strings.Repeat("b", 64)},
		{Instance: "uri_" + strings.Repeat("c", 32), Artifact: "sha256:" + strings.Repeat("c", 64)}}
	for _, successor := range successors {
		_, registeredHead, renewErr := Renew(ctx, pool, successor, currentManifest, ledger, false)
		require.NoError(t, renewErr)
		require.Equal(t, schemaauthority.SourceFactsExpandHead, registeredHead)
	}
	newConfig := []byte("fixture-live-successors")
	newDigest := fmt.Sprintf("%x", sha256.Sum256(newConfig))
	ingressPlan := IngressPlan{ChangeID: "fixture-current-head", PredecessorID: "fixture-head",
		ExpectedConfigDigest: configDigest, DesiredConfigDigest: newDigest, DesiredConfigBytes: newConfig,
		Before: []Identity{}, After: successors}
	status, err := PrepareIngress(ctx, pool, ingressPlan)
	require.NoError(t, err)
	require.Equal(t, "pending", status)
	status, err = CompleteIngress(ctx, pool, ingressPlan.ChangeID, newDigest, true, json.RawMessage(`{"fixture":"current successors serving"}`))
	require.NoError(t, err)
	require.Equal(t, "applied", status)
	input.ExpectedIngressHead = ingressPlan.ChangeID
	require.ErrorContains(t, up(schemaauthority.ObservationExpandHead), "all serving and recovery generations must support M117")
	var before string
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_jsonb(g)::text FROM runtime_generations g WHERE instance_id=$1`, old.Instance).Scan(&before))
	checked, err := CheckRetirement(ctx, pool, input)
	require.NoError(t, err)
	require.Equal(t, "eligible", checked.Status)
	var after string
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_jsonb(g)::text FROM runtime_generations g WHERE instance_id=$1`, old.Instance).Scan(&after))
	require.Equal(t, before, after, "check must not touch state, lease, revision or timestamps")
	active := input
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()+interval '1 hour' WHERE instance_id=$1`, old.Instance)
	require.NoError(t, err)
	_, err = ApplyRetirement(ctx, pool, active)
	require.ErrorContains(t, err, "still active")
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 hour', ingress_state='withdrawing' WHERE instance_id=$1`, old.Instance)
	require.NoError(t, err)
	_, err = ApplyRetirement(ctx, pool, active)
	require.ErrorContains(t, err, "still active")
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET ingress_state='absent' WHERE instance_id=$1`, old.Instance)
	require.NoError(t, err)
	stale := input
	stale.ExpectedRevision++
	_, err = ApplyRetirement(ctx, pool, stale)
	require.ErrorContains(t, err, "predecessor changed")
	wrongHead := input
	wrongHead.ExpectedIngressHead = "other-head"
	_, err = ApplyRetirement(ctx, pool, wrongHead)
	require.ErrorContains(t, err, "ingress head")
	org := testutil.NewTestOrg(t, pool)
	_, err = pool.Exec(ctx, `INSERT INTO freeze_idempotency(request_id,org_id,status,pricing_version)
	 VALUES('retirement-attempt',$1,'frozen','test')`, org)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()+interval '1 hour' WHERE instance_id=$1`, old.Instance)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO billing_provider_attempts
	 (org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,route_sequence,route_total,endpoint,provider,channel_name,runtime_instance_id)
	 VALUES($1,'retirement-attempt','attempt-1',101,201,301,1,1,'/v1/chat/completions','test-provider','test-channel',$2)`, org, old.Instance)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 hour' WHERE instance_id=$1`, old.Instance)
	require.NoError(t, err)
	_, err = ApplyRetirement(ctx, pool, input)
	require.ErrorContains(t, err, "unresolved financial attempts")
	_, err = pool.Exec(ctx, `UPDATE billing_provider_attempts SET state='settled',outcome_certainty='known_usage',financial_resolved_at=clock_timestamp()
	 WHERE org_id=$1 AND request_id='retirement-attempt'`, org)
	require.NoError(t, err)
	_, err = ApplyRetirement(ctx, pool, input)
	require.ErrorContains(t, err, "unresolved financial attempts", "an attempt terminal without a closed reservation is insufficient")
	_, err = pool.Exec(ctx, `UPDATE freeze_idempotency SET status='settled' WHERE org_id=$1 AND request_id='retirement-attempt'`, org)
	require.NoError(t, err)
	var predecessor string
	require.NoError(t, pool.QueryRow(ctx, `SELECT (to_jsonb(g)-'state'-'revision'-'updated_at')::text
	 FROM runtime_generations g WHERE instance_id=$1`, old.Instance).Scan(&predecessor))
	result, err := ApplyRetirement(ctx, pool, input)
	require.NoError(t, err)
	require.Equal(t, "stopped", result.Status)
	require.Equal(t, int64(2), result.Revision)
	require.Equal(t, int64(1), result.AttemptCount)
	var preserved string
	require.NoError(t, pool.QueryRow(ctx, `SELECT (to_jsonb(g)-'state'-'revision'-'updated_at')::text
	 FROM runtime_generations g WHERE instance_id=$1`, old.Instance).Scan(&preserved))
	require.Equal(t, predecessor, preserved, "stop may change only state, revision and updated_at")
	var state, retainedDigest string
	var revision int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT g.state,g.revision,m.manifest_digest FROM runtime_generations g JOIN runtime_support_manifests m USING(artifact_digest) WHERE instance_id=$1`, old.Instance).Scan(&state, &revision, &retainedDigest))
	require.Equal(t, "stopped", state)
	require.Equal(t, int64(2), revision)
	require.Equal(t, digest, retainedDigest)
	formalExpand := func(head string) error {
		wire, readErr := os.ReadFile(filepath.Join(dir, head+".up.sql"))
		require.NoError(t, readErr)
		migrationDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(wire))
		return migrate.RunCompatibleExpand(ctx, pool, dir, migrate.CompatibleExpandHooks{
			Version: head, ExpectedSHA256: migrationDigest,
			Before: func(ctx context.Context, conn *pgxpool.Conn) error {
				var count int
				err := conn.QueryRow(ctx, `SELECT count(*) FROM runtime_generations WHERE instance_id=ANY($1)
				 AND state='serving' AND ingress_state='receiving' AND lease_expires_at>clock_timestamp()`,
					[]string{successors[0].Instance, successors[1].Instance}).Scan(&count)
				if err != nil {
					return err
				}
				if count != 2 {
					return fmt.Errorf("two live exact successors required")
				}
				return nil
			},
			After: func(ctx context.Context, tx pgx.Tx) error {
				if head == schemaauthority.BudgetSnapshotExpandHead {
					return CertifyBudgetCoverageTx(ctx, tx, currentManifest, migrationDigest)
				}
				return nil
			},
		})
	}
	for _, head := range []string{schemaauthority.ObservationExpandHead, schemaauthority.BudgetSnapshotExpandHead,
		schemaauthority.ContentCaptureExpandHead, schemaauthority.ContentReclaimExpandHead,
		schemaauthority.PreDispatchExpandHead} {
		require.NoError(t, formalExpand(head), "current live successors must carry the complete M117-M121 recovery suffix")
	}
	for _, successor := range successors {
		var state, ingressState, schemaHead string
		require.NoError(t, pool.QueryRow(ctx, `SELECT state,ingress_state,schema_head FROM runtime_generations WHERE instance_id=$1`, successor.Instance).Scan(&state, &ingressState, &schemaHead))
		require.Equal(t, "serving", state)
		require.Equal(t, "receiving", ingressState)
		require.Equal(t, schemaauthority.SourceFactsExpandHead, schemaHead)
	}
	_, err = ApplyRetirement(ctx, pool, input)
	require.ErrorContains(t, err, "predecessor changed")
}

func TestRetirementRacesFastAndSlowRenewWithoutReusingStoppedIdentity(t *testing.T) {
	for _, draining := range []bool{false, true} {
		name := "fast-serving"
		if draining {
			name = "slow-draining"
		}
		t.Run(name, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			ledger, err := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
			require.NoError(t, err)
			manifest := CurrentManifest(schemaauthority.SupportedSchemaHeads())
			id := Identity{Instance: "uri_" + strings.Repeat("d", 32), Artifact: "sha256:" + strings.Repeat("d", 64)}
			_, head, err := Renew(ctx, pool, id, manifest, ledger, false)
			require.NoError(t, err)
			require.Equal(t, schemaauthority.LatestSupportedMigration, head)
			_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 hour' WHERE instance_id=$1`, id.Instance)
			require.NoError(t, err)
			config := []byte("race-empty-ingress")
			configDigest := fmt.Sprintf("%x", sha256.Sum256(config))
			_, err = pool.Exec(ctx, `INSERT INTO runtime_ingress_changes(change_id,predecessor_id,expected_config_digest,desired_config_digest,desired_config_bytes,membership_before,membership_after,status,completed_at)
			 VALUES('race-head',NULL,$1,$2,$3,'[]','[]','applied',clock_timestamp())`, strings.Repeat("0", 64), configDigest, config)
			require.NoError(t, err)
			var systemID string
			require.NoError(t, pool.QueryRow(ctx, `SELECT system_identifier::text FROM pg_control_system()`).Scan(&systemID))
			manifestDigest, err := manifest.Digest()
			require.NoError(t, err)
			input := RetirementInput{InstanceID: id.Instance, ArtifactDigest: id.Artifact, ManifestDigest: manifestDigest,
				CompatibilityEpoch: CompatibilityEpoch, ExpectedSchemaHead: head, ExpectedRevision: 1,
				ExpectedIngressHead: "race-head", ExpectedEnvironment: "test", ExpectedDatabaseSystemID: systemID,
				HostProofSHA256: "sha256:" + strings.Repeat("e", 64)}
			start := make(chan struct{})
			var wait sync.WaitGroup
			wait.Add(2)
			var stopErr, renewErr error
			go func() {
				defer wait.Done()
				<-start
				_, stopErr = ApplyRetirement(ctx, pool, input)
			}()
			go func() {
				defer wait.Done()
				<-start
				_, _, renewErr = Renew(ctx, pool, id, manifest, ledger, draining)
			}()
			close(start)
			wait.Wait()
			require.NotEqual(t, stopErr == nil, renewErr == nil, "only one concurrent owner may win")
			var state string
			var revision int64
			var live bool
			require.NoError(t, pool.QueryRow(ctx, `SELECT state,revision,lease_expires_at>clock_timestamp() FROM runtime_generations WHERE instance_id=$1`, id.Instance).Scan(&state, &revision, &live))
			if stopErr == nil {
				require.Equal(t, "stopped", state)
				require.False(t, live)
			} else {
				require.True(t, live)
				wantState := "serving"
				if draining {
					wantState = "draining"
				}
				require.Equal(t, wantState, state)
				_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 hour' WHERE instance_id=$1`, id.Instance)
				require.NoError(t, err)
				input.ExpectedRevision = revision
				_, err = ApplyRetirement(ctx, pool, input)
				require.NoError(t, err)
			}
			for _, laterDraining := range []bool{false, true} {
				_, _, err = Renew(ctx, pool, id, manifest, ledger, laterDraining)
				require.ErrorContains(t, err, "runtime instance cannot be reused")
			}
		})
	}
}
