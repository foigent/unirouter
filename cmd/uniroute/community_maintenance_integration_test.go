//go:build integration

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
)

// Keep the actual M127 completed origin, expand once, and bind a separately
// signed synthetic M128 application17 plus ordinary candidate20. The source
// signature is expired today but valid at its original recorded admission.
func maintenanceM128DatabaseFixture(t *testing.T) (*pgxpool.Pool, maintenancePackageFixture) {
	t.Helper()
	pool, f := maintenanceDatabaseFixture(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `UPDATE runtime_generations SET state='stopped',revision=revision+1`)
	require.NoError(t, err)
	require.NoError(t, migrate.RunExistingThrough(ctx, pool, f.cfg.MigrationsDir, schemaauthority.UpstreamReleaseObservationHead))
	f.source.ReleaseSequence, f.source.Version, f.source.TargetSchema = 17, "1.15.0", schemaauthority.UpstreamReleaseObservationHead
	f.source.MinCurrentSequence, f.source.MaxCurrentSequence = 0, 16
	f.input.SourcePackage, _ = packMaintenanceFixture(t, f.source, f.key, f.sources)
	f.input.ExpectedSequence, f.input.ExpectedSourcePackageSHA256 = 17, installDigest(mustReadCommunityFile(t, f.input.SourcePackage))
	f.input.ExpectedSourceManifestSHA256 = communityManifestSHA256(f.source)
	f.state.ReleaseSequence, f.state.Version, f.state.TargetSchema = 17, f.source.Version, f.source.TargetSchema
	f.state.PackageSHA256 = f.input.ExpectedSourcePackageSHA256
	f.candidate.ReleaseSequence, f.candidate.Version, f.candidate.Commit = 20, "0.0.20", strings.Repeat("c", 40)
	f.candidate.MinCurrentSequence, f.candidate.MaxCurrentSequence = 0, 19
	f.candidate.MinCurrentSchema = schemaauthority.UpstreamReleaseObservationHead
	f.input.CandidatePackage, _ = packMaintenanceFixture(t, f.candidate, f.key, f.sources)
	f.input.ExpectedCandidatePackageSHA256 = installDigest(mustReadCommunityFile(t, f.input.CandidatePackage))
	f.input.ExpectedCandidateManifestSHA256 = communityManifestSHA256(f.candidate)
	f.cfg.RuntimeVersion, f.cfg.RuntimeCommit = f.candidate.Version, f.candidate.Commit
	raw, err := json.Marshal(f.source)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO release_history(release_sequence,version,commit,package_sha256,trusted_key_id,target_schema,manifest,recorded_by,recorded_at)
VALUES($1,$2,$3,$4,$5,$6,$7,'synthetic-same-schema',$8)`, f.state.ReleaseSequence, f.state.Version, f.state.Commit, f.state.PackageSHA256, f.state.TrustedKeyID, f.state.TargetSchema, raw, f.recordedAt)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE release_state SET release_sequence=$1,version=$2,commit=$3,package_sha256=$4,trusted_key_id=$5,target_schema=$6,manifest=$7`, f.state.ReleaseSequence, f.state.Version, f.state.Commit, f.state.PackageSHA256, f.state.TrustedKeyID, f.state.TargetSchema, raw)
	require.NoError(t, err)
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	wire, err := manifest.CanonicalBytes()
	require.NoError(t, err)
	digest, err := manifest.Digest()
	require.NoError(t, err)
	id := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("c", 32), Artifact: "sha256:" + strings.Repeat("c", 64)}
	_, err = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, id.Artifact, digest, wire)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at)
VALUES($1,$2,$3,$4,'serving','absent',clock_timestamp()-interval '1 hour')`, id.Instance, id.Artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.UpstreamReleaseObservationHead)
	require.NoError(t, err)
	r := f.input.Retirements[0]
	r.InstanceID, r.ArtifactDigest, r.ManifestDigest, r.ExpectedSchemaHead = id.Instance, id.Artifact, digest, schemaauthority.UpstreamReleaseObservationHead
	f.input.Retirements = []runtimegeneration.RetirementInput{r}
	return pool, f
}

func mustReadCommunityFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return raw
}

func seedCommunityM128Observation(t *testing.T, pool *pgxpool.Pool) (int64, upstreamrelease.State) {
	t.Helper()
	var org, actor int64
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO orgs(name,status,org_type) VALUES('ordinary same-schema customer','active','customer') RETURNING id`).Scan(&org))
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'same-schema-admin','synthetic-unusable','org_admin','org_admin','active') RETURNING id`, org).Scan(&actor))
	return seedCommunityM128ObservationForCustomer(t, pool, org, actor)
}

func seedCommunityM128ObservationForCustomer(t *testing.T, pool *pgxpool.Pool, org, actor int64) (int64, upstreamrelease.State) {
	t.Helper()
	var model, connection, deployment int64
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO model_registry(org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,context_length,output_limit) VALUES($1,'same-schema-model','synthetic','generic','staging',FALSE,4096,1024) RETURNING id`, org).Scan(&model))
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO provider_connections(name,provider,base_url,adapter_type,adapter_config,admin_state) VALUES('same-schema','generic','https://api.example.test','openai_compatible','{}','enabled') RETURNING id`).Scan(&connection))
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO model_deployments(customer_org_id,public_model_id,connection_id,upstream_model,admin_state,priority,routing_weight) VALUES($1,$2,$3,'alias','enabled',100,100) RETURNING id`, org, model, connection).Scan(&deployment))
	observation, err := upstreamrelease.Next(upstreamrelease.Scope{CustomerOrgID: org, DeploymentID: deployment, ConnectionID: connection, Provider: "generic", BaseURL: "https://api.example.test", APIModel: "alias"}, upstreamrelease.State{}, upstreamrelease.Input{ReleaseLabel: "existing M128 declaration", SourceURL: "https://provider.example.test/release"}, actor, time.Now())
	require.NoError(t, err)
	raw, err := json.Marshal(observation.Observation)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `UPDATE model_deployments SET upstream_release_observation=$2,upstream_observation_revision=$3 WHERE id=$1`, deployment, raw, observation.Revision)
	require.NoError(t, err)
	return deployment, observation
}

func TestCommunityMaintenanceM128SameSchemaAndHistoricalOriginIntegration(t *testing.T) {
	pool, f := maintenanceM128DatabaseFixture(t)
	deployment, observation := seedCommunityM128Observation(t, pool)
	require.Positive(t, observation.Revision)
	before := communityAllTableFingerprints(t, pool, false)
	checked, err := runCommunityMaintenance(t.Context(), pool, f.cfg, f.input, false)
	require.NoError(t, err)
	require.Equal(t, "eligible", checked.Status)
	require.Equal(t, before, communityAllTableFingerprints(t, pool, false), "native check is entirely read-only")
	for _, fault := range []string{"source_schema", "missing_fleet", "live_lease", "revision"} {
		t.Run(fault, func(t *testing.T) {
			in := f.input
			in.Retirements = append([]runtimegeneration.RetirementInput(nil), in.Retirements...)
			if fault == "source_schema" {
				in.Retirements[0].ExpectedSchemaHead = schemaauthority.XInsightCommunicationHead
			}
			if fault == "missing_fleet" {
				in.Retirements = nil
			}
			if fault == "revision" {
				in.Retirements[0].ExpectedRevision++
			}
			if fault == "live_lease" {
				_, err = pool.Exec(t.Context(), `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()+interval '1 hour' WHERE instance_id=$1`, in.Retirements[0].InstanceID)
				require.NoError(t, err)
			}
			_, err = runCommunityMaintenance(t.Context(), pool, f.cfg, in, true)
			require.Error(t, err)
			if fault == "live_lease" {
				_, err = pool.Exec(t.Context(), `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 hour' WHERE instance_id=$1`, in.Retirements[0].InstanceID)
				require.NoError(t, err)
			}
		})
	}
	_, err = runCommunityMaintenance(t.Context(), pool, f.cfg, f.input, true)
	require.NoError(t, err)
	actual := communityAllTableFingerprints(t, pool, false)
	for name, row := range before {
		if name != "runtime_generations" {
			require.Equal(t, row, actual[name], "retirement preserved %s", name)
		}
	}
	snapshot, err := installstate.Read(t.Context(), pool)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.XInsightCommunicationHead, snapshot.Record.Plan.Artifact.Ledger[len(snapshot.Record.Plan.Artifact.Ledger)-1])
	root, err := os.MkdirTemp(os.Getenv("UR_FRESH_EVIDENCE_PARENT"), "ur-m128-same-schema-restore-")
	require.NoError(t, err)
	t.Logf("retained synthetic M128 restore evidence: %s", root)
	restored := restoreM128CompatibleBackup(t, pool, f, root)
	ledger := mustCurrentCommunityLedger(t)
	restoredBefore := communityAllTableFingerprints(t, restored, false)
	_, err = checkFreshInstall(t.Context(), restored, snapshot.Record.Plan, ledger)
	require.Error(t, err, "default corrected binary must not admit a legacy schema as ordinary runtime")
	completed, err := checkFreshInstallAtSource(t.Context(), restored, snapshot.Record.Plan, ledger, schemaauthority.UpstreamReleaseObservationHead)
	require.NoError(t, err)
	require.Equal(t, snapshot.Record.Plan.Digest, completed.Record.Plan.Digest)
	require.Equal(t, restoredBefore, communityAllTableFingerprints(t, restored, false), "clone completed check does not rebase, certify or write")
	_, err = verifyRuntimeAuthority(t.Context(), restored, ledger)
	require.Error(t, err, "bounded clone check never grants ordinary runtime admission")
	var raw []byte
	var revision int64
	require.NoError(t, restored.QueryRow(t.Context(), `SELECT upstream_release_observation,upstream_observation_revision FROM model_deployments WHERE id=$1`, deployment).Scan(&raw, &revision))
	want, err := json.Marshal(observation.Observation)
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(raw))
	require.Equal(t, observation.Revision, revision)
	originalRows := communityAllTableFingerprints(t, pool, false)
	require.NoError(t, migrate.RunExisting(t.Context(), pool, f.cfg.MigrationsDir))
	correctedRows := communityAllTableFingerprints(t, pool, false)
	for name, row := range originalRows {
		if name != "schema_migrations" {
			require.Equal(t, row, correctedRows[name], "correction preserves %s", name)
		}
	}
	require.NoError(t, upstreamrelease.CheckNamespace(t.Context(), pool))
	verifyCorrectedObservationInputs(t, pool, observation)
	// Re-entry executes no additional migration or function rewrite.
	require.NoError(t, migrate.RunExisting(t.Context(), pool, f.cfg.MigrationsDir))
	require.Equal(t, correctedRows, communityAllTableFingerprints(t, pool, false))
	// Native recording advances the exact signed candidate after the genuine
	// correction; original M127 installation and M128 history remain intact.
	args := []string{"--package", f.input.CandidatePackage, "--recorded-by", "synthetic-same-schema", "--expected-source-sequence", "17", "--expected-source-package-sha256", f.state.PackageSHA256}
	require.NoError(t, runUpgradeRecordApplied(t.Context(), args, f.cfg, pool))
	require.Error(t, runUpgradeRecordApplied(t.Context(), args, f.cfg, pool), "stale predecessor cannot record again")
	var head string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT max(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.UpstreamObservationRestoreHead, head)
	var sourceCount int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM release_history WHERE release_sequence IN (10,17,20)`).Scan(&sourceCount))
	require.Equal(t, 3, sourceCount)
	// A corrected M129 archive uses ordinary PostgreSQL restore, without the
	// legacy pre-data correction, and keeps the historical M127 origin.
	correctedRoot, err := os.MkdirTemp(os.Getenv("UR_FRESH_EVIDENCE_PARENT"), "ur-m129-standard-restore-")
	require.NoError(t, err)
	t.Logf("corrected standard restore evidence: %s", correctedRoot)
	correctedClone := restoreFreshBackup(t, pool, correctedRoot)
	_, err = checkFreshInstall(t.Context(), correctedClone, snapshot.Record.Plan, ledger)
	require.NoError(t, err)
	_, err = verifyRuntimeAuthority(t.Context(), correctedClone, ledger)
	require.NoError(t, err)
	var correctedRaw []byte
	var correctedRevision int64
	require.NoError(t, correctedClone.QueryRow(t.Context(), `SELECT upstream_release_observation,upstream_observation_revision FROM model_deployments WHERE id=$1`, deployment).Scan(&correctedRaw, &correctedRevision))
	require.JSONEq(t, string(want), string(correctedRaw))
	require.Equal(t, observation.Revision, correctedRevision)
	_, err = checkFreshInstallAtSource(t.Context(), correctedClone, snapshot.Record.Plan, ledger, schemaauthority.UpstreamReleaseObservationHead)
	require.Error(t, err, "legacy source check cannot reinterpret the full corrected ledger")
	// A following normal application can bind the corrected schema without
	// a fabricated migration. Real signatures, full ledger and original Plan
	// are still verified by the native maintenance participant.
	next := f.candidate
	next.ReleaseSequence = 21
	next.Version = "0.0.21"
	next.Commit = strings.Repeat("d", 40)
	next.MinCurrentSchema = schemaauthority.UpstreamObservationRestoreHead
	next.MaxCurrentSequence = 20
	nextPackage, _ := packMaintenanceFixture(t, next, f.key, f.sources)
	follow := f.input
	follow.SourcePackage = f.input.CandidatePackage
	follow.ExpectedSequence = 20
	follow.ExpectedSourcePackageSHA256 = f.input.ExpectedCandidatePackageSHA256
	follow.ExpectedSourceManifestSHA256 = f.input.ExpectedCandidateManifestSHA256
	follow.CandidatePackage = nextPackage
	follow.ExpectedCandidatePackageSHA256 = installDigest(mustReadCommunityFile(t, nextPackage))
	follow.ExpectedCandidateManifestSHA256 = communityManifestSHA256(next)
	follow.Retirements = nil
	cfg := *f.cfg
	cfg.RuntimeVersion = next.Version
	cfg.RuntimeCommit = next.Commit
	unchangedRows := communityAllTableFingerprints(t, pool, false)
	checked, err = runCommunityMaintenance(t.Context(), pool, &cfg, follow, false)
	require.NoError(t, err)
	require.Equal(t, "eligible", checked.Status)
	require.NoError(t, migrate.RunExisting(t.Context(), pool, f.cfg.MigrationsDir))
	require.Equal(t, unchangedRows, communityAllTableFingerprints(t, pool, false), "same-schema application executes zero SQL migrations and native check writes nothing")
}

func TestCommunityM128ObservationRestoreSearchPathDiagnosticIntegration(t *testing.T) {
	pool, _ := maintenanceM128DatabaseFixture(t)
	_, observation := seedCommunityM128Observation(t, pool)
	raw, err := json.Marshal(observation.Observation)
	require.NoError(t, err)
	var ordinary, restricted, restored bool
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT public.valid_upstream_release_observation($1::jsonb)`, raw).Scan(&ordinary))
	tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	_, err = tx.Exec(t.Context(), `SET LOCAL search_path=''`)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT public.valid_upstream_release_observation($1::jsonb)`, raw).Scan(&restricted))
	_, err = tx.Exec(t.Context(), `SET LOCAL search_path=public,pg_catalog`)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT public.valid_upstream_release_observation($1::jsonb)`, raw).Scan(&restored))
	t.Logf("same valid nonzero observation: normal_path=%t dump_restore_empty_path=%t normal_path_again=%t; read-only diagnosis only", ordinary, restricted, restored)
	require.True(t, ordinary)
	require.False(t, restricted, "diagnostic reproduces the existing M128 restore blocker; it is not acceptance of this behavior")
	require.True(t, restored)
}

func maintenanceDatabaseFixture(t *testing.T) (*pgxpool.Pool, maintenancePackageFixture) {
	t.Helper()
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightCommunicationHead)
	f := newMaintenancePackageFixture(t)
	f.cfg.MigrationsDir = testutil.MigrationsDir(t)
	ctx := t.Context()
	ledger, err := schemaauthority.ExpectedLedger(f.cfg.MigrationsDir)
	require.NoError(t, err)
	ledger = ledger[:len(ledger)-2]
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
if sys.argv[2]=="true": projections={"model_deployments":("to_jsonb(t)-'upstream_release_observation'-'upstream_observation_revision'",""),"schema_migrations":("to_jsonb(t)"," WHERE version NOT IN ('000128_upstream_release_observation','000129_upstream_observation_restore_namespace')")}
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
	_, err = checkFreshInstall(ctx, restored, snapshot.Record.Plan, mustCurrentCommunityLedger(t))
	require.ErrorContains(t, err, "ledger", "historical restore verification never grants M128 fresh-start authority on M127")
	allOriginalTables := communityAllTableFingerprints(t, pool, false)
	require.NoError(t, migrate.RunExisting(ctx, pool, f.cfg.MigrationsDir))
	require.Equal(t, allOriginalTables, communityAllTableFingerprints(t, pool, true), "all original public rows survive the exact M128 additions")
	var head string
	require.NoError(t, pool.QueryRow(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.UpstreamObservationRestoreHead, head)
	after, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, coverage, after, "maintenance migration preserves the existing financial proof")
	_, err = runCommunityMaintenance(ctx, pool, f.cfg, f.input, true)
	require.ErrorContains(t, err, "installed source release and exact ledger disagree", "old source authority cannot record the corrected head")
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

func mustCurrentCommunityLedger(t *testing.T) []string {
	t.Helper()
	ledger, err := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
	require.NoError(t, err)
	return ledger
}

// Exercise the same native pre-data participant used by the host owner with
// real PostgreSQL custom archives. The original source/dump are immutable.
func restoreM128CompatibleBackup(t *testing.T, source *pgxpool.Pool, f maintenancePackageFixture, root string) *pgxpool.Pool {
	t.Helper()
	backup := filepath.Join(root, "backup")
	require.NoError(t, os.Mkdir(backup, 0700))
	log, err := os.OpenFile(filepath.Join(root, "restore.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	require.NoError(t, err)
	defer log.Close()
	pgEnv := func(pool *pgxpool.Pool) []string {
		c := pool.Config().ConnConfig
		return append(os.Environ(), "PGHOST="+c.Host, fmt.Sprintf("PGPORT=%d", c.Port), "PGUSER="+c.User, "PGPASSWORD="+c.Password, "PGDATABASE="+c.Database, "PGSSLMODE=disable")
	}
	run := func(pool *pgxpool.Pool, tool string, args ...string) {
		t.Helper()
		if tool == "pg_restore" {
			args = append([]string{"--dbname", pool.Config().ConnConfig.Database}, args...)
		}
		cmd := exec.CommandContext(t.Context(), tool, args...)
		cmd.Env = pgEnv(pool)
		cmd.Stdout = log
		cmd.Stderr = log
		require.NoError(t, cmd.Run(), "native phase %s %v; see %s", tool, args, root)
	}
	dump := filepath.Join(backup, "database.dump")
	run(source, "pg_dump", "-Fc", "--no-owner", "--no-privileges", "--file", dump)
	require.NoError(t, os.Chmod(dump, 0600))
	raw := mustReadCommunityFile(t, dump)
	dumpSHA := installDigest(raw)
	random := make([]byte, 12)
	_, err = rand.Read(random)
	require.NoError(t, err)
	name := "ur_upgrade_verify_" + hex.EncodeToString(random)
	_, err = source.Exec(t.Context(), `CREATE DATABASE `+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	cfg := source.Config().Copy()
	cfg.ConnConfig.Database = name
	clone, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		clone.Close()
		_, e := source.Exec(context.Background(), `DROP DATABASE `+pgx.Identifier{name}.Sanitize())
		if e != nil {
			t.Errorf("remove exact owned restore clone %s: %v", name, e)
		}
	})
	var oid int64
	require.NoError(t, clone.QueryRow(t.Context(), `SELECT oid::bigint FROM pg_database WHERE datname=current_database()`).Scan(&oid))
	input := f.input
	input.Retirements = nil
	in := restoreCompatibilityInput{SchemaVersion: restoreCompatibilitySchema, Source: input, CloneDatabase: name, CloneDatabaseOID: oid, DumpPath: dump, DumpSHA256: dumpSHA}
	run(clone, "pg_restore", "--exit-on-error", "--single-transaction", "--no-owner", "--no-privileges", "--section=pre-data", dump)
	beforeSource := communityAllTableFingerprints(t, source, false)
	namespace, err := upstreamrelease.ReadNamespace(t.Context(), clone)
	require.NoError(t, err)
	require.Empty(t, namespace.Config)
	for _, fault := range []string{"clone_oid", "source_database", "dump_hash", "unknown_helper", "nonempty_clone"} {
		t.Run(fault, func(t *testing.T) {
			bad := in
			var restore string
			switch fault {
			case "clone_oid":
				bad.CloneDatabaseOID++
			case "source_database":
				bad.Source.ExpectedDatabase = "other"
			case "dump_hash":
				bad.DumpSHA256 = strings.Repeat("0", 64)
			case "unknown_helper":
				require.NoError(t, clone.QueryRow(t.Context(), `SELECT pg_get_functiondef('public.valid_upstream_release_text(text,integer,boolean)'::regprocedure)`).Scan(&restore))
				_, err = clone.Exec(t.Context(), `CREATE OR REPLACE FUNCTION public.valid_upstream_release_text(v text,n integer,required boolean) RETURNS boolean LANGUAGE sql IMMUTABLE AS 'SELECT true'`)
				require.NoError(t, err)
			case "nonempty_clone":
				_, err = clone.Exec(t.Context(), `INSERT INTO schema_migrations(version) VALUES('synthetic-unknown')`)
				require.NoError(t, err)
			}
			_, e := runRestoreCompatibility(t.Context(), source, clone, f.cfg, bad, true)
			require.Error(t, e)
			if fault == "unknown_helper" {
				_, err = clone.Exec(t.Context(), restore)
				require.NoError(t, err)
			}
			if fault == "nonempty_clone" {
				_, err = clone.Exec(t.Context(), `DELETE FROM schema_migrations WHERE version='synthetic-unknown'`)
				require.NoError(t, err)
			}
			require.Equal(t, beforeSource, communityAllTableFingerprints(t, source, false))
		})
	}
	// Exercise the real existing upgrade CLI dispatch and bounded stdin too,
	// deriving the source only from the exact explicit clone connection.
	inputPath := filepath.Join(backup, "restore-compatibility-input.json")
	inputWire, err := json.Marshal(in)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(inputPath, inputWire, 0600))
	func() {
		input, err := os.Open(inputPath)
		require.NoError(t, err)
		defer input.Close()
		previous := os.Stdin
		os.Stdin = input
		defer func() { os.Stdin = previous }()
		cfg := *f.cfg
		connectionURL, err := url.Parse(source.Config().ConnConfig.ConnString())
		require.NoError(t, err)
		require.Contains(t, []string{"postgres", "postgresql"}, connectionURL.Scheme)
		connectionURL.Path = "/" + name
		cfg.DB_DSN = connectionURL.String()
		cfg.DBMaxConnections = 1
		cfg.DBMaxIdleConns = 0
		require.NoError(t, runUpgradeCLI(t.Context(), []string{"restore-compat-check"}, &cfg))
		_, err = input.Seek(0, 0)
		require.NoError(t, err)
		cfg.DB_DSN = ""
		require.ErrorContains(t, runRestoreCompatibilityCLI(t.Context(), nil, &cfg, clone, false), "explicit restore clone connection")
	}()
	checked, err := runRestoreCompatibility(t.Context(), source, clone, f.cfg, in, false)
	require.NoError(t, err)
	require.Equal(t, "eligible", checked.Status)
	unchanged, err := upstreamrelease.ReadNamespace(t.Context(), clone)
	require.NoError(t, err)
	require.Equal(t, namespace, unchanged, "check must not repair")
	applied, err := runRestoreCompatibility(t.Context(), source, clone, f.cfg, in, true)
	require.NoError(t, err)
	require.Equal(t, "applied", applied.Status)
	readback, err := runRestoreCompatibility(t.Context(), source, clone, f.cfg, in, false)
	require.NoError(t, err)
	require.Equal(t, applied, readback)
	replay, err := runRestoreCompatibility(t.Context(), source, clone, f.cfg, in, true)
	require.NoError(t, err)
	require.Equal(t, applied, replay, "known exact canonical pre-data state is idempotent")
	for _, section := range []string{"data", "post-data"} {
		run(clone, "pg_restore", "--exit-on-error", "--single-transaction", "--no-owner", "--no-privileges", "--section="+section, dump)
	}
	require.Equal(t, beforeSource, communityAllTableFingerprints(t, clone, false), "all raw rows, including the M128 ledger/observation, restore exactly before rebase")
	require.Equal(t, raw, mustReadCommunityFile(t, dump), "compatibility does not rewrite the raw archive")
	require.Equal(t, beforeSource, communityAllTableFingerprints(t, source, false), "source remains unchanged")
	_, err = runRestoreCompatibility(t.Context(), source, clone, f.cfg, in, true)
	require.ErrorContains(t, err, "empty pre-data tables", "participant cannot re-enter a populated clone")
	receipt, err := json.MarshalIndent(applied, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(backup, "restore-compatibility-receipt.json"), receipt, 0600))
	run(clone, "psql", "-X", "--set", "ON_ERROR_STOP=1", "--file", filepath.Join(filepath.Dir(testutil.MigrationsDir(t)), "scripts", "source-budget-restore-rebase.sql"))
	return clone
}

func verifyCorrectedObservationInputs(t *testing.T, pool *pgxpool.Pool, observation upstreamrelease.State) {
	t.Helper()
	raw, err := json.Marshal(observation.Observation)
	require.NoError(t, err)
	tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	_, err = tx.Exec(t.Context(), `SET LOCAL search_path=''`)
	require.NoError(t, err)
	for _, value := range []struct {
		wire  []byte
		valid bool
	}{{raw, true}, {[]byte(`{}`), false}, {[]byte(`null`), false}, {[]byte(`{"schema_version":"unknown"}`), false}} {
		var valid bool
		require.NoError(t, tx.QueryRow(t.Context(), `SELECT public.valid_upstream_release_observation($1::jsonb)`, value.wire).Scan(&valid))
		require.Equal(t, value.valid, valid)
	}
}

func TestM129MigrationRejectsUnknownValidatorIntegration(t *testing.T) {
	for _, fault := range []string{"helper_body", "helper_config", "validator_config", "validator_argument", "budget_body", "budget_config", "live_writer"} {
		t.Run(fault, func(t *testing.T) {
			pool, f := maintenanceM128DatabaseFixture(t)
			var err error
			if fault != "live_writer" {
				_, err = runCommunityMaintenance(t.Context(), pool, f.cfg, f.input, true)
				require.NoError(t, err)
			}
			_, observation := seedCommunityM128Observation(t, pool)
			switch fault {
			case "helper_body":
				_, err = pool.Exec(t.Context(), `CREATE OR REPLACE FUNCTION public.valid_upstream_release_text(v text,n integer,required boolean) RETURNS boolean LANGUAGE sql IMMUTABLE AS 'SELECT true'`)
			case "helper_config":
				_, err = pool.Exec(t.Context(), `ALTER FUNCTION public.valid_upstream_release_text(text,integer,boolean) SET search_path=public`)
			case "validator_config":
				_, err = pool.Exec(t.Context(), `ALTER FUNCTION public.valid_upstream_release_observation(jsonb) SET search_path=public`)
			case "validator_argument":
				_, err = pool.Exec(t.Context(), `ALTER FUNCTION public.valid_upstream_release_observation(jsonb) STRICT`)
			case "budget_body":
				_, err = pool.Exec(t.Context(), `CREATE OR REPLACE FUNCTION public.verify_source_budget_coverage_commit() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RETURN NEW; END'`)
			case "budget_config":
				_, err = pool.Exec(t.Context(), `ALTER FUNCTION public.verify_source_budget_coverage_commit() SET search_path=public`)
			case "live_writer": // retain the actual unretired predecessor
			}
			require.NoError(t, err)
			before := communityAllTableFingerprints(t, pool, false)
			err = migrate.RunExisting(t.Context(), pool, f.cfg.MigrationsDir)
			require.Error(t, err)
			require.Equal(t, before, communityAllTableFingerprints(t, pool, false), "rejected migration is atomic")
			var revision int64
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT max(upstream_observation_revision) FROM model_deployments`).Scan(&revision))
			require.Equal(t, observation.Revision, revision)
		})
	}
}

// Opt-in only on the disposable native Linux installer. No Provider is called.
func TestCommunityInstallerSeedM128ObservationIntegration(t *testing.T) {
	if os.Getenv("UR_COMMUNITY_INSTALLER_SEED") != "observation-m128" {
		t.Skip("only the exact disposable Linux target seeds this fixture")
	}
	require.Equal(t, "linux", runtime.GOOS)
	require.Equal(t, "true", os.Getenv("GITHUB_ACTIONS"))
	require.Equal(t, "1", os.Getenv("REQUIRE_TEST_DATABASE"))
	var target struct {
		System   string `json:"system_identifier"`
		Database string `json:"database"`
		OID      int64  `json:"database_oid"`
		Owner    string `json:"owner"`
		Role     string `json:"role"`
	}
	require.NoError(t, json.Unmarshal([]byte(os.Getenv("UR_COMMUNITY_INSTALLER_SEED_TARGET")), &target))
	require.NotEmpty(t, target.System)
	require.Positive(t, target.OID)
	require.Equal(t, "uniroute", target.Database)
	dsn := os.Getenv("TEST_DATABASE_URL")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	var system, database, role, head string
	var oid int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT (SELECT system_identifier::text FROM pg_control_system()),current_database(),(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),current_user,(SELECT max(version) FROM schema_migrations)`).Scan(&system, &database, &oid, &role, &head))
	require.Equal(t, target.System, system)
	require.Equal(t, target.Database, database)
	require.Equal(t, target.OID, oid)
	require.Equal(t, target.Owner, role)
	require.Equal(t, target.Role, role)
	require.Equal(t, schemaauthority.UpstreamReleaseObservationHead, head)
	customer, err := customercontext.Resolve(t.Context(), pool)
	require.NoError(t, err)
	require.True(t, customer.Ready())
	var actor int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT id FROM users WHERE org_id=1 AND username='admin' AND organization_role='platform_admin' AND status='active'`).Scan(&actor))
	_, observation := seedCommunityM128ObservationForCustomer(t, pool, customer.OrganizationID, actor)
	require.Positive(t, observation.Revision)
}
