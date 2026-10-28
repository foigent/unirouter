//go:build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func maintenanceDatabaseFixture(t *testing.T) (*pgxpool.Pool, maintenancePackageFixture) {
	t.Helper()
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightCommunicationHead)
	f := newMaintenancePackageFixture(t)
	f.cfg.MigrationsDir = testutil.MigrationsDir(t)
	ctx := t.Context()
	ledger, err := schemaauthority.ExpectedLedger(f.cfg.MigrationsDir)
	require.NoError(t, err)
	ledger = ledger[:len(ledger)-1]
	target, err := installstate.ReadTarget(ctx, pool, "production")
	require.NoError(t, err)
	// This is an explicitly synthetic retained completion envelope for the
	// maintenance owner's DB boundary. Real fresh-owner recovery is tested by
	// TestFreshInstallProcessRecoveryIntegration, not inferred from this fixture.
	a := installstate.Artifact{Version: f.source.Version, Commit: f.source.Commit, Tree: strings.Repeat("a", 40), ImageDigest: "sha256:" + strings.Repeat("b", 64), ExecutableDigest: "sha256:" + strings.Repeat("c", 64), ManifestDigest: strings.Repeat("d", 64), Ledger: ledger, Files: map[string]string{}}
	refs := []installstate.Reference{}
	for _, id := range ledger {
		raw, e := os.ReadFile(filepath.Join(f.cfg.MigrationsDir, id+".up.sql"))
		require.NoError(t, e)
		digest := installstate.SHA(raw)
		a.Files[id+".up.sql"] = digest
		if strings.Contains(string(raw), "-- unirouter:capability-contract-publication-v1") {
			a.Publications = append(a.Publications, id)
			refs = append(refs, installstate.Reference{Publication: id, PlanDigest: installstate.SHA([]byte("plan " + id)), FileSHA256: digest, Committed: true})
		}
	}
	p := installstate.Plan{SchemaVersion: installstate.Schema, InstallationID: f.input.InstallationID, Target: target, Artifact: a, FreshShape: "empty_database"}
	p.Digest = p.CalculateDigest()
	record := installstate.Record{Plan: p, State: "completed", References: refs, Completion: &installstate.Completion{LedgerSHA256: installstate.LedgerSHA(ledger), MigrationSHA256: "sha256:" + a.Files[schemaauthority.XInsightCommunicationHead+".up.sql"], ManifestSHA256: a.ManifestDigest}}
	wire, err := record.CommentBytes()
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `COMMENT ON TABLE schema_migrations IS `+quoteSQLLiteral(string(wire)))
	require.NoError(t, err)
	f.input.InstallationPlanDigest = p.Digest
	f.input.ExpectedDatabaseSystemID = target.SystemIdentifier
	f.input.ExpectedDatabase = target.Database
	f.input.ExpectedDatabaseOID = target.DatabaseOID
	manifest, err := json.Marshal(f.source)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO release_history(release_sequence,version,commit,package_sha256,trusted_key_id,target_schema,manifest,recorded_by,recorded_at) VALUES($1,$2,$3,$4,$5,$6,$7,'synthetic-maintenance',$8)`, f.state.ReleaseSequence, f.state.Version, f.state.Commit, f.state.PackageSHA256, f.state.TrustedKeyID, f.state.TargetSchema, manifest, f.recordedAt)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE release_state SET release_sequence=$1,version=$2,commit=$3,package_sha256=$4,trusted_key_id=$5,target_schema=$6,manifest=$7`, f.state.ReleaseSequence, f.state.Version, f.state.Commit, f.state.PackageSHA256, f.state.TrustedKeyID, f.state.TargetSchema, manifest)
	require.NoError(t, err)
	support := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()[:len(schemaauthority.SupportedSchemaHeads())-1])
	supportWire, err := support.CanonicalBytes()
	require.NoError(t, err)
	supportDigest, err := support.Digest()
	require.NoError(t, err)
	artifact := "sha256:" + strings.Repeat("b", 64)
	instance := "uri_" + strings.Repeat("b", 32)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, artifact, supportDigest, supportWire)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at) VALUES($1,$2,$3,$4,'serving','absent',clock_timestamp()-interval '1 hour')`, instance, artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.XInsightCommunicationHead)
	require.NoError(t, err)
	config := []byte("synthetic completed withdrawal")
	_, err = pool.Exec(ctx, `INSERT INTO runtime_ingress_changes(change_id,expected_config_digest,desired_config_digest,desired_config_bytes,membership_before,membership_after,status,completed_at) VALUES($1,$2,$3,$4,'[]','[]','applied',clock_timestamp())`, f.input.ExpectedIngressHead, strings.Repeat("0", 64), installstate.SHA(config), config)
	require.NoError(t, err)
	f.input.Retirements = []runtimegeneration.RetirementInput{{InstanceID: instance, ArtifactDigest: artifact, ManifestDigest: supportDigest, CompatibilityEpoch: runtimegeneration.CompatibilityEpoch, ExpectedSchemaHead: schemaauthority.XInsightCommunicationHead, ExpectedRevision: 1, ExpectedIngressHead: f.input.ExpectedIngressHead, ExpectedEnvironment: "production", ExpectedDatabaseSystemID: target.SystemIdentifier, HostProofSHA256: "sha256:" + strings.Repeat("f", 64)}}
	return pool, f
}

func communityProtectedRows(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var raw string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_build_object('state',(SELECT to_jsonb(s) FROM release_state s),'history',(SELECT jsonb_agg(to_jsonb(h) ORDER BY release_sequence) FROM release_history h),'ledger',(SELECT jsonb_agg(version ORDER BY version) FROM schema_migrations),'installation',obj_description('schema_migrations'::regclass),'manifests',(SELECT jsonb_agg(to_jsonb(m) ORDER BY artifact_digest) FROM runtime_support_manifests m),'ingress',(SELECT jsonb_agg(to_jsonb(c) ORDER BY change_id) FROM runtime_ingress_changes c))::text`).Scan(&raw))
	return raw
}
func communityGenerationRows(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var raw string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(g) ORDER BY instance_id),'[]')::text FROM runtime_generations g`).Scan(&raw))
	return raw
}

// Execute the shipped Python hash mechanic against actual PostgreSQL, including
// its exact M128 projection. The host fixture alone cannot prove SQL hashes.
func communityAllTableFingerprints(t *testing.T, pool *pgxpool.Pool, successor bool) map[string]any {
	t.Helper()
	script := `import importlib.util,json,subprocess,sys
spec=importlib.util.spec_from_file_location("recovery",sys.argv[1]); m=importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
def sql(db,query):
    result=subprocess.run(["psql","-X","-qAt","--set","ON_ERROR_STOP=1","-d",db],input=query.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    if result.returncode: raise RuntimeError("private native SQL fingerprint command failed")
    return result.stdout.decode().strip()
projections=None
if sys.argv[2]=="true": projections={"model_deployments":("to_jsonb(t)-'upstream_release_observation'-'upstream_observation_revision'",""),"schema_migrations":("to_jsonb(t)"," WHERE version<>'000128_upstream_release_observation'")}
import os
print(json.dumps(m.table_fingerprints(sql,os.environ["PGDATABASE"],projections=projections),sort_keys=True))
`
	command := exec.CommandContext(t.Context(), "python3", "-I", "-B", "-c", script,
		filepath.Join(filepath.Dir(testutil.MigrationsDir(t)), "deploy", "customer", "recovery.py"), fmt.Sprint(successor))
	c := pool.Config().ConnConfig
	command.Env = append(os.Environ(), "PGHOST="+c.Host, fmt.Sprintf("PGPORT=%d", c.Port), "PGUSER="+c.User,
		"PGPASSWORD="+c.Password, "PGDATABASE="+c.Database, "PGSSLMODE=disable")
	raw, err := command.Output()
	require.NoError(t, err, "shipped recovery SQL hash mechanic must execute against real isolated PG")
	var result map[string]any
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Greater(t, len(result), 100, "full original public table inventory, not a four-table substitute")
	return result
}

func TestCommunityMaintenanceExactProductionSourceAndAtomicRetirementIntegration(t *testing.T) {
	pool, f := maintenanceDatabaseFixture(t)
	ctx := t.Context()
	protected := communityProtectedRows(t, pool)
	generations := communityGenerationRows(t, pool)
	checked, err := runCommunityMaintenance(ctx, pool, f.cfg, f.input, false)
	require.NoError(t, err)
	require.Equal(t, "eligible", checked.Status)
	require.Equal(t, protected, communityProtectedRows(t, pool))
	require.Equal(t, generations, communityGenerationRows(t, pool))
	for _, kind := range []string{"installation", "system", "database", "sequence", "trust", "incomplete_fleet", "wrong_revision"} {
		t.Run(kind, func(t *testing.T) {
			in := f.input
			in.Retirements = append([]runtimegeneration.RetirementInput(nil), in.Retirements...)
			switch kind {
			case "installation":
				in.InstallationID = "ufi_" + strings.Repeat("f", 32)
			case "system":
				in.ExpectedDatabaseSystemID = "987654321"
				in.Retirements[0].ExpectedDatabaseSystemID = in.ExpectedDatabaseSystemID
			case "database":
				in.ExpectedDatabase = "other-database"
			case "sequence":
				in.ExpectedSequence++
			case "trust":
				in.ExpectedTrustedRingSHA256 = strings.Repeat("f", 64)
			case "incomplete_fleet":
				in.Retirements = nil
			case "wrong_revision":
				in.Retirements[0].ExpectedRevision++
			}
			_, err := runCommunityMaintenance(ctx, pool, f.cfg, in, true)
			require.Error(t, err)
			require.Equal(t, protected, communityProtectedRows(t, pool))
			require.Equal(t, generations, communityGenerationRows(t, pool))
		})
	}
	applied, err := runCommunityMaintenance(ctx, pool, f.cfg, f.input, true)
	require.NoError(t, err)
	require.Equal(t, "retired", applied.Status)
	require.Equal(t, "stopped", applied.Retirements[0].Status)
	require.EqualValues(t, 2, applied.Retirements[0].Revision)
	require.Equal(t, protected, communityProtectedRows(t, pool), "retirement does not migrate, advance release, rewrite ingress or alter original installation")
	_, err = runCommunityMaintenance(ctx, pool, f.cfg, f.input, true)
	require.Error(t, err, "unknown response must be reconciled from exact native state, never blind replay")
	readback := f.input
	readback.Retirements = nil
	checked, err = runCommunityMaintenance(ctx, pool, f.cfg, readback, false)
	require.NoError(t, err)
	require.Equal(t, "eligible", checked.Status)
	require.Empty(t, checked.Retirements, "readback is not a new writer retirement")
	_, err = runCommunityMaintenance(ctx, pool, f.cfg, readback, true)
	require.ErrorContains(t, err, "requires a nonempty exact writer batch")
	coverage, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	snapshot, err := installstate.Read(ctx, pool)
	require.NoError(t, err)
	backupRoot, err := os.MkdirTemp(os.Getenv("UR_FRESH_EVIDENCE_PARENT"), "ur-m127-maintenance-restore-")
	require.NoError(t, err)
	t.Logf("M127 maintenance native backup/restore evidence: %s", backupRoot)
	restored := restoreFreshBackup(t, pool, backupRoot)
	_, err = verifyRuntimeAuthority(ctx, restored, snapshot.Record.Plan.Artifact.Ledger)
	require.NoError(t, err, "restored M127 retains the exact source artifact projection and financial authority")
	restoredInstallation, err := installstate.RequireOrdinary(ctx, restored)
	require.NoError(t, err)
	require.Equal(t, snapshot.Record.Plan.Digest, restoredInstallation.Record.Plan.Digest)
	restoredCoverage, err := runtimegeneration.ReadBudgetCoverage(ctx, restored)
	require.NoError(t, err)
	require.Equal(t, "restore_rebase", restoredCoverage.Reason)
	_, err = checkFreshInstall(ctx, restored, snapshot.Record.Plan)
	require.ErrorContains(t, err, "binary authority", "historical restore verification never grants M128 fresh-start authority on M127")
	allOriginalTables := communityAllTableFingerprints(t, pool, false)
	require.NoError(t, migrate.RunExisting(ctx, pool, f.cfg.MigrationsDir))
	require.Equal(t, allOriginalTables, communityAllTableFingerprints(t, pool, true), "all original public rows survive the exact M128 additions")
	var head string
	require.NoError(t, pool.QueryRow(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.UpstreamReleaseObservationHead, head)
	after, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, coverage, after, "maintenance migration preserves the existing financial proof")
	_, err = runCommunityMaintenance(ctx, pool, f.cfg, f.input, true)
	require.ErrorContains(t, err, "exact complete M127 installation", "M128 cannot reuse predecessor retirement authority")
	// This checks the record owner's transaction, not real application readiness.
	// A broad fresh+upgrade signature window must not override the host's exact
	// installed predecessor binding at this final append boundary.
	beforeRecord := communityProtectedRows(t, pool)
	args := []string{"--package", f.input.CandidatePackage, "--recorded-by", "synthetic-community-maintenance",
		"--expected-source-sequence", "10", "--expected-source-package-sha256", strings.Repeat("f", 64)}
	require.ErrorContains(t, runUpgradeRecordApplied(ctx, args, f.cfg, pool), "installed release predecessor changed")
	require.Equal(t, beforeRecord, communityProtectedRows(t, pool))
	args[len(args)-1] = f.state.PackageSHA256
	require.NoError(t, runUpgradeRecordApplied(ctx, args, f.cfg, pool))
	recorded, err := readInstalledReleaseState(ctx, pool, false)
	require.NoError(t, err)
	require.EqualValues(t, 11, recorded.ReleaseSequence)
	require.Equal(t, f.input.ExpectedCandidatePackageSHA256, recorded.PackageSHA256)
}

func TestCommunityMaintenanceRevalidatesInstalledReleaseAfterLockIntegration(t *testing.T) {
	pool, f := maintenanceDatabaseFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	holder, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer holder.Release()
	_, err = holder.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended('unirouter.release_state',0))`)
	require.NoError(t, err)
	locked := true
	defer func() {
		if locked {
			_, _ = holder.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended('unirouter.release_state',0))`)
		}
	}()
	done := make(chan error, 1)
	go func() { _, e := runCommunityMaintenance(ctx, pool, f.cfg, f.input, true); done <- e }()
	require.Eventually(t, func() bool {
		var waiting bool
		e := holder.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	_, err = holder.Exec(ctx, `UPDATE release_state SET version='synthetic-concurrent-drift' WHERE singleton=true`)
	require.NoError(t, err)
	before := communityGenerationRows(t, pool)
	_, err = holder.Exec(ctx, `SELECT pg_advisory_unlock(hashtextextended('unirouter.release_state',0))`)
	require.NoError(t, err)
	locked = false
	select {
	case err = <-done:
		require.Error(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Equal(t, before, communityGenerationRows(t, pool), "concurrent installed identity drift cannot partially retire writers")
}

// quoteSQLLiteral is used only for this synthetic database comment fixture.
func quoteSQLLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func TestCommunityMaintenanceDualWriterBatchIsAtomicIntegration(t *testing.T) {
	for _, kind := range []string{"second_revision", "second_live_lease", "missing_second", "complete_pair"} {
		t.Run(kind, func(t *testing.T) {
			pool, f := maintenanceDatabaseFixture(t)
			ctx := t.Context()
			second := f.input.Retirements[0]
			second.InstanceID = "uri_" + strings.Repeat("c", 32)
			_, err := pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at) VALUES($1,$2,$3,$4,'serving','absent',clock_timestamp()-interval '1 hour')`, second.InstanceID, second.ArtifactDigest, second.CompatibilityEpoch, second.ExpectedSchemaHead)
			require.NoError(t, err)
			f.input.Retirements = append(f.input.Retirements, second)
			switch kind {
			case "second_revision":
				f.input.Retirements[1].ExpectedRevision++
			case "second_live_lease":
				_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()+interval '1 hour' WHERE instance_id=$1`, second.InstanceID)
				require.NoError(t, err)
			case "missing_second":
				f.input.Retirements = f.input.Retirements[:1]
			}
			before := communityGenerationRows(t, pool)
			protected := communityProtectedRows(t, pool)
			result, err := runCommunityMaintenance(ctx, pool, f.cfg, f.input, true)
			if kind != "complete_pair" {
				require.Error(t, err)
				require.Equal(t, before, communityGenerationRows(t, pool), "neither writer may be partially retired")
			} else {
				require.NoError(t, err)
				require.Len(t, result.Retirements, 2)
				for _, row := range result.Retirements {
					require.Equal(t, "stopped", row.Status)
					require.EqualValues(t, 2, row.Revision)
				}
				var stopped int
				require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM runtime_generations WHERE state='stopped' AND ingress_state='absent'`).Scan(&stopped))
				require.Equal(t, 2, stopped)
			}
			require.Equal(t, protected, communityProtectedRows(t, pool))
		})
	}
}

func TestCommunityMaintenanceFiveGenerationMultiArtifactHistoryIntegration(t *testing.T) {
	for _, kind := range []string{"partial_history", "crossed_identity", "crossed_support", "live_lease", "unresolved_financial", "complete_union"} {
		t.Run(kind, func(t *testing.T) {
			pool, f := maintenanceDatabaseFixture(t)
			ctx := t.Context()
			original := f.input.Retirements[0]
			for i := 0; i < 4; i++ {
				retirement := original
				retirement.InstanceID = fmt.Sprintf("uri_%032x", i+16)
				retirement.ArtifactDigest = "sha256:" + strings.Repeat(string(rune('c'+i)), 64)
				_, err := pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes)
SELECT $1,manifest_digest,canonical_bytes FROM runtime_support_manifests WHERE artifact_digest=$2`, retirement.ArtifactDigest, original.ArtifactDigest)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at)
VALUES($1,$2,$3,$4,'serving','absent',clock_timestamp()-interval '1 hour')`, retirement.InstanceID, retirement.ArtifactDigest, retirement.CompatibilityEpoch, retirement.ExpectedSchemaHead)
				require.NoError(t, err)
				f.input.Retirements = append(f.input.Retirements, retirement)
			}
			org := testutil.NewTestOrg(t, pool)
			testutil.NewTestWalletCents(t, pool, org, 123)
			_, err := pool.Exec(ctx, `INSERT INTO transactions(request_id,org_id,type,amount_micros,balance_after_micros,note)
VALUES('synthetic-history-preservation',$1,'topup',1230000,1230000,'Synthetic five-generation maintenance fixture')`, org)
			require.NoError(t, err)
			last := f.input.Retirements[4]
			switch kind {
			case "partial_history":
				f.input.Retirements = f.input.Retirements[:4]
			case "crossed_identity":
				f.input.Retirements[4].ExpectedDatabaseSystemID = "999"
			case "crossed_support":
				f.input.Retirements[4].ManifestDigest = strings.Repeat("0", 64)
			case "live_lease", "unresolved_financial":
				_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()+interval '1 hour' WHERE instance_id=$1`, last.InstanceID)
				require.NoError(t, err)
				if kind == "unresolved_financial" {
					_, err = pool.Exec(ctx, `INSERT INTO freeze_idempotency(request_id,org_id,status,pricing_version)
VALUES('five-history-unresolved',$1,'frozen','synthetic-maintenance')`, org)
					require.NoError(t, err)
					_, err = pool.Exec(ctx, `INSERT INTO billing_provider_attempts
(org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,route_sequence,route_total,endpoint,provider,channel_name,runtime_instance_id)
VALUES($1,'five-history-unresolved','synthetic-attempt',101,201,301,1,1,'/v1/chat/completions','synthetic-provider','synthetic-channel',$2)`, org, last.InstanceID)
					require.NoError(t, err)
					_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 hour' WHERE instance_id=$1`, last.InstanceID)
					require.NoError(t, err)
				}
			}
			before := communityGenerationRows(t, pool)
			protected := communityProtectedRows(t, pool)
			fullBefore := communityAllTableFingerprints(t, pool, false)
			checked, checkErr := runCommunityMaintenance(ctx, pool, f.cfg, f.input, false)
			require.Equal(t, before, communityGenerationRows(t, pool), "check must preserve every original generation")
			result, err := runCommunityMaintenance(ctx, pool, f.cfg, f.input, true)
			if kind != "complete_union" {
				require.Error(t, checkErr)
				require.Error(t, err)
				if kind == "unresolved_financial" {
					require.ErrorContains(t, err, "unresolved financial attempts")
				}
				require.Equal(t, before, communityGenerationRows(t, pool), "no member may be partially retired")
			} else {
				require.NoError(t, checkErr)
				require.Equal(t, "eligible", checked.Status)
				require.NoError(t, err)
				require.Len(t, result.Retirements, 5)
				ledger, e := schemaauthority.ExpectedLedger(f.cfg.MigrationsDir)
				require.NoError(t, e)
				ledger = ledger[:len(ledger)-1]
				support := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()[:len(schemaauthority.SupportedSchemaHeads())-1])
				for _, r := range f.input.Retirements {
					identity := runtimegeneration.Identity{Instance: r.InstanceID, Artifact: r.ArtifactDigest}
					_, _, e = runtimegeneration.Renew(ctx, pool, identity, support, ledger, false)
					require.Error(t, e, "retired original identity cannot renew its execution lease")
				}
				config := []byte("synthetic completed withdrawal")
				_, e = runtimegeneration.PrepareIngress(ctx, pool, runtimegeneration.IngressPlan{ChangeID: "retired-history-cannot-rejoin", PredecessorID: f.input.ExpectedIngressHead,
					ExpectedConfigDigest: installstate.SHA(config), DesiredConfigDigest: installstate.SHA(config), DesiredConfigBytes: config,
					Before: []runtimegeneration.Identity{}, After: []runtimegeneration.Identity{{Instance: original.InstanceID, Artifact: original.ArtifactDigest}}})
				require.Error(t, e, "retired original identity cannot be admitted to ingress")
				for _, row := range result.Retirements {
					require.Equal(t, "stopped", row.Status)
				}
			}
			require.Equal(t, protected, communityProtectedRows(t, pool))
			fullAfter := communityAllTableFingerprints(t, pool, false)
			if kind == "complete_union" {
				delete(fullBefore, "runtime_generations")
				delete(fullAfter, "runtime_generations")
			}
			require.Equal(t, fullBefore, fullAfter, "all original account, financial, auth, history and other tables remain identical")
		})
	}
}
