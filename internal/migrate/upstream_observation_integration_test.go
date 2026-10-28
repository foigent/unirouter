//go:build integration

package migrate_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUpstreamObservationM127MaintenanceAndEmptyDownIntegration(t *testing.T) {
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightCommunicationHead)
	ctx := t.Context()
	dir := testutil.MigrationsDir(t)
	before, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	var ledgerBefore, ledgerAfter []string
	require.NoError(t, pool.QueryRow(ctx, `SELECT array_agg(version ORDER BY version) FROM schema_migrations`).Scan(&ledgerBefore))
	// Exact artifact-ledger admission rejects M127 before pending migrations are
	// considered. Ordinary startup must never cross this maintenance boundary.
	require.ErrorContains(t, migrate.RunRuntime(ctx, pool, dir), "retired-license schema must have its complete artifact ledger")
	require.NoError(t, pool.QueryRow(ctx, `SELECT array_agg(version ORDER BY version) FROM schema_migrations`).Scan(&ledgerAfter))
	require.Equal(t, ledgerBefore, ledgerAfter)
	var exists bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='model_deployments' AND column_name='upstream_release_observation')`).Scan(&exists))
	require.False(t, exists)
	require.NoError(t, migrate.RunExisting(ctx, pool, dir))
	require.NoError(t, migrate.RunRuntime(ctx, pool, dir))
	after, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, before, after, "retain original financial coverage proof")
	var head string
	require.NoError(t, pool.QueryRow(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.UpstreamReleaseObservationHead, head)
	down, err := os.ReadFile(dir + "/" + schemaauthority.UpstreamReleaseObservationHead + ".down.sql")
	require.NoError(t, err)
	// The migration owns BEGIN/COMMIT; do not wrap it in another transaction.
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='model_deployments' AND column_name='upstream_release_observation')`).Scan(&exists))
	require.False(t, exists)
}

func TestUpstreamObservationM128FreshSchemaIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var head string
	require.NoError(t, pool.QueryRow(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.UpstreamReleaseObservationHead, head)
	_, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	var valid bool
	for _, wire := range []string{`{}`, `[]`, `null`, `{"schema_version":3}`, `{"identity":"uor_x"}`} {
		require.NoError(t, pool.QueryRow(ctx, `SELECT valid_upstream_release_observation($1::jsonb)`, wire).Scan(&valid))
		require.False(t, valid, wire)
	}
}

func TestUpstreamObservationM128RefusesLiveWriterOrUnresolvedIngressIntegration(t *testing.T) {
	for _, kind := range []string{"live_writer", "pending_ingress"} {
		t.Run(kind, func(t *testing.T) {
			pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightCommunicationHead)
			ctx := t.Context()
			dir := testutil.MigrationsDir(t)
			if kind == "live_writer" {
				manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
				wire, err := manifest.CanonicalBytes()
				require.NoError(t, err)
				digest, err := manifest.Digest()
				require.NoError(t, err)
				artifact := "sha256:" + strings.Repeat("a", 64)
				_, err = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, artifact, digest, wire)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at) VALUES($1,$2,$3,$4,'serving','absent',clock_timestamp()+interval '1 hour')`, "uri_"+strings.Repeat("a", 32), artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.XInsightCommunicationHead)
				require.NoError(t, err)
			} else {
				wire := []byte("synthetic unresolved ingress")
				_, err := runtimegeneration.PrepareIngress(ctx, pool, runtimegeneration.IngressPlan{ChangeID: "upstream-observation-pending", ExpectedConfigDigest: strings.Repeat("0", 64), DesiredConfigDigest: fmt.Sprintf("%x", sha256.Sum256(wire)), DesiredConfigBytes: wire, Before: []runtimegeneration.Identity{}, After: []runtimegeneration.Identity{}})
				require.NoError(t, err)
			}
			snapshot := func() string {
				var value string
				require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_build_object('ledger',(SELECT jsonb_agg(version ORDER BY version) FROM schema_migrations),'writers',(SELECT jsonb_agg(to_jsonb(g) ORDER BY instance_id) FROM runtime_generations g),'ingress',(SELECT jsonb_agg(to_jsonb(c) ORDER BY change_id) FROM runtime_ingress_changes c))::text`).Scan(&value))
				return value
			}
			before := snapshot()
			require.ErrorContains(t, migrate.RunExisting(ctx, pool, dir), "stopped retired writers")
			require.Equal(t, before, snapshot())
			var exists bool
			require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='model_deployments' AND column_name='upstream_release_observation')`).Scan(&exists))
			require.False(t, exists)
		})
	}
}

func seedUpstreamObservationDownFixture(t *testing.T, pool *pgxpool.Pool) (int64, int64, *upstreamrelease.Observation) {
	t.Helper()
	ctx := t.Context()
	var org, actor, model, connection, deployment int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('upstream-down','active','customer') RETURNING id`).Scan(&org))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'upstream-down','fixture','org_admin','org_admin','active') RETURNING id`, org).Scan(&actor))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_registry(org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,context_length,output_limit) VALUES($1,'upstream-down','fixture','generic','staging',FALSE,4096,1024) RETURNING id`, org).Scan(&model))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO provider_connections(name,provider,base_url,adapter_type,adapter_config,admin_state) VALUES('upstream-down','generic','https://api.example.test','openai_compatible','{}','enabled') RETURNING id`).Scan(&connection))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_deployments(customer_org_id,public_model_id,connection_id,upstream_model,admin_state,priority,routing_weight) VALUES($1,$2,$3,'alias','enabled',100,100) RETURNING id`, org, model, connection).Scan(&deployment))
	next, err := upstreamrelease.Next(upstreamrelease.Scope{CustomerOrgID: org, DeploymentID: deployment, ConnectionID: connection, Provider: "generic", BaseURL: "https://api.example.test", APIModel: "alias"}, upstreamrelease.State{}, upstreamrelease.Input{ReleaseLabel: "A", SourceURL: "https://provider.example.test/release"}, actor, time.Now())
	require.NoError(t, err)
	return deployment, connection, next.Observation
}

func TestUpstreamObservationM128DownRetainsDeclarationsAndEvidenceIntegration(t *testing.T) {
	for _, kind := range []string{"declaration", "retained_probe"} {
		t.Run(kind, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			deployment, connection, observation := seedUpstreamObservationDownFixture(t, pool)
			wire, err := json.Marshal(observation)
			require.NoError(t, err)
			if kind == "declaration" {
				_, err = pool.Exec(ctx, `UPDATE model_deployments SET upstream_release_observation=$2,upstream_observation_revision=1 WHERE id=$1`, deployment, wire)
				require.NoError(t, err)
			} else {
				// Defensive restoration fixture: retained evidence survives even if its
				// current declaration is unknown. It is not new executable qualification.
				_, err = pool.Exec(ctx, `INSERT INTO model_deployment_operations(deployment_id,operation_id,support_state,evidence_state,revision,protocol_profile_id,protocol_profile_version) VALUES($1,'openai.chat_completions.v1','enabled','declared',1,'openai.chat_completions','1.0.0')`, deployment)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `INSERT INTO model_deployment_probes(deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,upstream_model,connection_revision,deployment_revision,success,source_kind,source_ref,source_fingerprint,request_summary) VALUES($1,$2,'openai.chat_completions.v1','openai.chat_completions','1.0.0','alias',1,1,FALSE,'synthetic.restored-probe','retained-observation','sha256:'||repeat('a',64),jsonb_build_object('upstream_release_observation',$3::jsonb))`, deployment, connection, wire)
				require.NoError(t, err)
			}
			snapshot := func() string {
				var value string
				require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_build_object('deployment',(SELECT to_jsonb(d) FROM model_deployments d WHERE id=$1),'probes',(SELECT jsonb_agg(to_jsonb(p) ORDER BY probe_id) FROM model_deployment_probes p WHERE deployment_id=$1),'head',(SELECT max(version) FROM schema_migrations))::text`, deployment).Scan(&value))
				return value
			}
			before := snapshot()
			down, err := os.ReadFile(testutil.MigrationsDir(t) + "/" + schemaauthority.UpstreamReleaseObservationHead + ".down.sql")
			require.NoError(t, err)
			_, err = pool.Exec(ctx, string(down))
			require.ErrorContains(t, err, "retained upstream declarations/evidence")
			require.Equal(t, before, snapshot())
			var exists bool
			require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='model_deployments' AND column_name='upstream_release_observation')`).Scan(&exists))
			require.True(t, exists)
		})
	}
}
