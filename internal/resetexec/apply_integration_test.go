//go:build integration

package resetexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/resetplan"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

// A current binary must not treat a real predecessor schema as its exact reset
// artifact. Historical reset support requires the corresponding old artifact.
func TestCurrentResetPreviewRejectsM118AndPreservesExactState(t *testing.T) {
	pool, _ := resetM118TestDB(t)
	ctx := t.Context()
	seedResetIntegrationFixture(t, pool)
	seedM78AuthoredResetEvidence(t, pool)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Set(ctx, "wallet:v2:must-remain", "synthetic", 0).Err())
	redisBefore, err := ObserveRedis(ctx, client)
	require.NoError(t, err)
	before, err := Inspect(ctx, pool, redisBefore)
	require.NoError(t, err)
	target := resetplan.TargetIdentity{RuntimeVersion: "1.4.0", RuntimeCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40)}
	_, err = Preview(ctx, pool, client, resetArtifactSchemaLedger(t), target, "synthetic-source-old", "synthetic-source-new", time.Unix(1, 0))
	require.ErrorIs(t, err, resetplan.ErrInvalid)
	require.ErrorContains(t, err, "reset_support_migration")
	require.ErrorContains(t, err, "missing=["+schemaauthority.ContentCaptureExpandHead)
	require.ErrorContains(t, err, schemaauthority.CommercialLicenseRetirementHead)
	redisAfter, err := ObserveRedis(ctx, client)
	require.NoError(t, err)
	after, err := Inspect(ctx, pool, redisAfter)
	require.NoError(t, err)
	require.Equal(t, before, after, "original inventory, protected digests, identities and sequences survive refusal")
	require.Equal(t, redisBefore, redisAfter)
	require.Equal(t, "synthetic", client.Get(ctx, "wallet:v2:must-remain").Val())
}

func TestResetInspectBuildIncludesM88ProtectedCapabilityContractsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	seedResetIntegrationFixture(t, pool)

	target := resetplan.TargetIdentity{
		RuntimeVersion: "v2.0.2",
		RuntimeCommit:  strings.Repeat("a", 40),
		SourceTree:     strings.Repeat("b", 40),
	}
	redisObservation := resetplan.RedisObservation{Prefixes: []resetplan.RedisPrefixObservation{{Prefix: "limit:{"}}}
	baseline, err := Inspect(ctx, pool, redisObservation)
	require.NoError(t, err)
	baseline.ExpectedMigrationVersions = resetArtifactSchemaLedger(t)
	baselinePlan, err := resetplan.Build(baseline, target, "source-old", "source-new", time.Unix(1, 0))
	require.NoError(t, err)
	require.Len(t, baselinePlan.Protected.Tables, 26)

	protectedByName := make(map[string]resetplan.ContentDigest, len(baselinePlan.Protected.Tables))
	for _, digest := range baselinePlan.Protected.Tables {
		protectedByName[digest.Name] = digest
	}
	require.Contains(t, protectedByName, "model_capability_probe_contract_bindings")
	require.Contains(t, protectedByName, "model_capability_current_probe_contracts")
	// The strict artifact installer includes later Publications as well as the
	// original M88 contracts. Inspect must cover every retained row, rather than
	// only the historical 20/18-row inventory.
	var bindings, current int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM model_capability_probe_contract_bindings), (SELECT count(*) FROM model_capability_current_probe_contracts)`).Scan(&bindings, &current))
	require.GreaterOrEqual(t, bindings, int64(20))
	require.GreaterOrEqual(t, current, int64(18))
	require.Equal(t, bindings, protectedByName["model_capability_probe_contract_bindings"].RowCount)
	require.Equal(t, current, protectedByName["model_capability_current_probe_contracts"].RowCount)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `ALTER TABLE model_capability_probe_contract_bindings
		DISABLE TRIGGER model_capability_probe_contract_bindings_immutable`)
	require.NoError(t, err)
	// M114 adds a second append-only guard that intentionally survives removal
	// of the historical guard. Disable both only in this rolled-back corruption
	// fixture so the test actually reaches the protected-content digest check.
	_, err = tx.Exec(ctx, `ALTER TABLE model_capability_probe_contract_bindings
		DISABLE TRIGGER capability_binding_writer_epoch`)
	require.NoError(t, err)
	changed, err := tx.Exec(ctx, `UPDATE model_capability_probe_contract_bindings
		SET expected_upstream_model='deepseek-v4-flash-drift'
		WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming'
		  AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0'
		  AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0'
		  AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='1.0.0'`)
	require.NoError(t, err)
	require.EqualValues(t, 1, changed.RowsAffected(), "drift probe must change the retained M88 contract")

	drifted, err := Inspect(ctx, tx, redisObservation)
	require.NoError(t, err)
	drifted.ExpectedMigrationVersions = resetArtifactSchemaLedger(t)
	driftedPlan, err := resetplan.Build(drifted, target, "source-old", "source-new", time.Unix(1, 0))
	require.NoError(t, err)
	require.Len(t, driftedPlan.Protected.Tables, 26)
	driftedByName := make(map[string]resetplan.ContentDigest, len(driftedPlan.Protected.Tables))
	for _, digest := range driftedPlan.Protected.Tables {
		driftedByName[digest.Name] = digest
	}
	require.NotEqual(t,
		protectedByName["model_capability_probe_contract_bindings"].SHA256,
		driftedByName["model_capability_probe_contract_bindings"].SHA256,
	)
	require.NotEqual(t, baselinePlan.ScopeSHA256, driftedPlan.ScopeSHA256)
}

type resetAuthoringSnapshotBuilder struct{}

func (resetAuthoringSnapshotBuilder) BuildRuntimeChangeSnapshots(
	context.Context,
	string,
	[]routingpolicy.ChannelOverride,
	[]routingpolicy.ChannelOverride,
) (governancesim.RouteSnapshot, governancesim.RouteSnapshot, error) {
	return governancesim.RouteSnapshot{}, governancesim.RouteSnapshot{}, nil
}

func seedM78AuthoredResetEvidence(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	ctx := t.Context()
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT users.org_id,users.id
  FROM users
 WHERE users.username='old-user'`).Scan(&orgID, &userID))
	var publicModelID, connectionID, deploymentID, credentialID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
  context_length,output_limit)
VALUES ($1,'reset-m78-model','Reset M78 Model','custom','ga',TRUE,8192,2048)
RETURNING id`, orgID).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('reset-m78-connection','custom','https://reset-m78.invalid',
        'openai_compatible','{}','enabled',1)
RETURNING id`).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
  config_revision,priority,routing_weight,max_context,default_max_output)
VALUES ($1,$2,$3,'reset-m78-upstream','enabled',1,100,100,8192,2048)
RETURNING id`, orgID, publicModelID, connectionID).Scan(&deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'reset-m78-secret','active',1)
RETURNING id`, connectionID).Scan(&credentialID))
	_, err := pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3)`, credentialID, deploymentID, connectionID)
	require.NoError(t, err)
	const operationID = "openai.chat_completions.v1"
	const profileID = "integration.reset.openai.chat.v1"
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,
  revision,protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','live_probed','reset M78 evidence',NOW(),1,$3,'1.0.0')`,
		deploymentID, operationID, profileID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
  deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
  upstream_model,reported_model,connection_revision,deployment_revision,
  credential_binding_id,credential_state_revision,success,status_code,latency_ms,
  error_summary,source_kind,source_ref,source_fingerprint,tested_by)
VALUES ($1,$2,$3,$4,'1.0.0','reset-m78-upstream','reset-m78-upstream',1,1,
        $5,1,TRUE,200,1,'','integration.reset','reset-m78-probe',
        'sha256:'||repeat('7',64),$6)`, deploymentID, connectionID, operationID,
		profileID, credentialID, userID)
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, resetAuthoringSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)
	release := integrationfixture.AuthorSealedDeploymentRelease(t, pool, manager, integrationfixture.AtomicRelease{
		PublicModelID: publicModelID,
		CustomerOrgID: orgID,
		ActorUserID:   userID,
		ActorOrgID:    orgID,
		Selections: []runtimechange.DeploymentReleaseSelection{{
			DeploymentID: deploymentID,
			OperationID:  operationID,
		}},
		Reason: "reset must remove real M78 authored evidence",
	})
	var receiptCount, eventCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM model_deployment_release_authoring_receipts
    WHERE release_id=$1 AND status='completed'),
  (SELECT COUNT(*) FROM runtime_change_events
    WHERE release_id=$1)`, release.ID).Scan(&receiptCount, &eventCount))
	require.Equal(t, 1, receiptCount)
	require.Equal(t, 2, eventCount)
	return release.ID
}

func assertM78ResetDeletionOrder(t *testing.T, plan resetplan.Plan) {
	t.Helper()
	positions := make(map[string]int, len(plan.DeleteOrder))
	for _, step := range plan.DeleteOrder {
		positions[step.Table] = step.Position
	}
	for _, dependency := range [][2]string{
		{"runtime_change_events", "runtime_change_releases"},
		{"runtime_route_revision_items", "runtime_change_releases"},
		{"runtime_change_releases", "model_deployment_release_authoring_receipts"},
	} {
		child, childOK := positions[dependency[0]]
		parent, parentOK := positions[dependency[1]]
		require.True(t, childOK && parentOK, "reset plan must include %s and %s", dependency[0], dependency[1])
		require.Less(t, child, parent, "%s must be deleted before %s", dependency[0], dependency[1])
	}
}

func TestApplyStopsBeforeRedisMutationWhenM119ScopeAlreadyRequiresReview(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	expectedSchemaLedger := resetArtifactSchemaLedger(t)
	ctx := context.Background()
	seedResetIntegrationFixture(t, pool)
	seedM78AuthoredResetEvidence(t, pool)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	require.NoError(t, redisClient.Set(ctx, "wallet:v2:must-remain", "fixture", 0).Err())
	target := resetplan.TargetIdentity{
		RuntimeVersion: "v2.0.2",
		RuntimeCommit:  strings.Repeat("c", 40),
		SourceTree:     strings.Repeat("d", 40),
	}
	plan, err := Preview(ctx, pool, redisClient, expectedSchemaLedger, target, "source-old", "source-new", time.Unix(1, 0))
	require.NoError(t, err)
	assertM78ResetDeletionOrder(t, plan)
	require.False(t, plan.ActionAuthorized)
	require.False(t, plan.MutationPerformed)
	blocked := map[string]bool{}
	for _, table := range plan.Tables {
		if table.Scope == resetplan.ScopeReviewRequired {
			blocked[table.Name] = true
			require.Equal(t, resetplan.ActionPreserveAll, table.Action)
			require.Zero(t, table.ExpectedDeletedRows)
		}
	}
	require.True(t, blocked["execution_source_budget_coverage"])
	require.True(t, blocked["execution_source_budget_definitions"])
	_, err = pool.Exec(ctx, `INSERT INTO channels (name, provider, base_url, status) VALUES ('drift', 'fixture', 'https://example.invalid', 'disabled')`)
	require.NoError(t, err)

	require.False(t, plan.ScopeComplete)
	redisBefore, err := ObserveRedis(ctx, redisClient)
	require.NoError(t, err)
	before, err := Inspect(ctx, pool, redisBefore)
	require.NoError(t, err)
	_, err = Apply(ctx, pool, redisClient, expectedSchemaLedger, plan, target, "source-new")
	require.ErrorIs(t, err, resetplan.ErrInvalid)
	require.NotErrorIs(t, err, ErrPlanDrift, "scope refusal precedes the original semantic-drift check")
	after, err := Inspect(ctx, pool, redisBefore)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, "fixture", redisClient.Get(ctx, "wallet:v2:must-remain").Val())
}

func TestSchema64ResetContractFailsClosedOnAccessTriggerOrFKDriftIntegration(t *testing.T) {
	t.Run("append-only trigger", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		seedResetIntegrationFixture(t, pool)
		_, err := pool.Exec(context.Background(), `
			ALTER TABLE access_grant_revisions DISABLE TRIGGER access_grant_revisions_append_only`)
		require.NoError(t, err)
		_, err = Preview(context.Background(), pool, redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()}),
			resetArtifactSchemaLedger(t),
			resetplan.TargetIdentity{RuntimeVersion: "v2.0.2", RuntimeCommit: strings.Repeat("e", 40), SourceTree: strings.Repeat("f", 40)},
			"source-old", "source-new", time.Unix(1, 0))
		require.ErrorContains(t, err, "guarded_delete_trigger_access_grant_revisions_access_grant_revisions_append_only")
	})

	t.Run("operation append-only trigger", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		seedResetIntegrationFixture(t, pool)
		_, err := pool.Exec(context.Background(), `
			ALTER TABLE access_operations DISABLE TRIGGER access_operations_append_only`)
		require.NoError(t, err)
		_, err = Preview(context.Background(), pool, redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()}),
			resetArtifactSchemaLedger(t),
			resetplan.TargetIdentity{RuntimeVersion: "v2.0.2", RuntimeCommit: strings.Repeat("e", 40), SourceTree: strings.Repeat("f", 40)},
			"source-old", "source-new", time.Unix(1, 0))
		require.ErrorContains(t, err, "guarded_delete_trigger_access_operations_access_operations_append_only")
	})

	t.Run("target grant cycle constraint", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		seedResetIntegrationFixture(t, pool)
		_, err := pool.Exec(context.Background(), `
			ALTER TABLE access_requests ALTER CONSTRAINT access_requests_target_grant_scope_fk NOT DEFERRABLE`)
		require.NoError(t, err)
		_, err = Preview(context.Background(), pool, redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()}),
			resetArtifactSchemaLedger(t),
			resetplan.TargetIdentity{RuntimeVersion: "v2.0.2", RuntimeCommit: strings.Repeat("e", 40), SourceTree: strings.Repeat("f", 40)},
			"source-old", "source-new", time.Unix(1, 0))
		require.ErrorContains(t, err, "deferred_cycle_constraint_access_requests_target_grant_scope_fk")
	})
}

var resetContentTables = []string{
	"xinsight_communication_policies", "xinsight_communication_runs",
	"execution_content_streams", "execution_source_content_scopes",
	"execution_source_content_bindings", "execution_source_content_operations",
	"execution_content_policies", "execution_content_manifests",
	"execution_content_objects", "execution_content_health",
}

func resetM118TestDB(t *testing.T) (*pgxpool.Pool, []string) {
	t.Helper()
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.ContentCaptureExpandHead)
	ledger := resetArtifactSchemaLedger(t)
	ledger = ledger[:slices.Index(ledger, schemaauthority.ContentCaptureExpandHead)+1]
	require.Equal(t, schemaauthority.ContentCaptureExpandHead, ledger[len(ledger)-1])
	wire, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.ContentCaptureExpandHead+".down.sql"))
	require.NoError(t, err)
	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), string(wire))
	if err != nil {
		_, _ = conn.Exec(context.Background(), "ROLLBACK")
	}
	conn.Release()
	require.NoError(t, err)
	ledger = ledger[:len(ledger)-1]
	require.Equal(t, schemaauthority.BudgetSnapshotExpandHead, ledger[len(ledger)-1])
	actual, err := inspectMigrations(t.Context(), pool)
	require.NoError(t, err)
	require.Equal(t, ledger, actual)
	var present int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_tables WHERE schemaname='public' AND tablename=ANY($1)`, resetContentTables).Scan(&present))
	require.Zero(t, present, "M118 fixture must have the real predecessor schema")
	return pool, ledger
}

func TestApplyRefusesCurrentContentAndPreservesPostgresAndRedis(t *testing.T) {
	for _, state := range []string{"empty", "retained_content"} {
		t.Run(state, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			seedResetIntegrationFixture(t, pool)
			if state == "retained_content" {
				var org, project int64
				require.NoError(t, pool.QueryRow(ctx, `SELECT org_id,id FROM projects WHERE slug='old-project'`).Scan(&org, &project))
				_, err := pool.Exec(ctx, `INSERT INTO execution_content_streams(org_id,project_id,source_instance) VALUES($1,$2,'retained-content')`, org, project)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `INSERT INTO execution_content_health(org_id,project_id,reason,detail) VALUES($1,$2,'storage_unavailable',convert_to('{"retained":true}','UTF8'))`, org, project)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `INSERT INTO xinsight_communication_policies(org_id,project_id,automatic_enabled,cadence_ms,last_code) VALUES($1,$2,FALSE,60000,'retained_manual')`, org, project)
				require.NoError(t, err)
			}
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			require.NoError(t, client.Set(ctx, "wallet:v2:must-remain", "retained", 0).Err())
			redisBefore, err := ObserveRedis(ctx, client)
			require.NoError(t, err)
			before, err := Inspect(ctx, pool, redisBefore)
			require.NoError(t, err)
			digests := func() []resetplan.ContentDigest {
				result := make([]resetplan.ContentDigest, 0, len(resetContentTables))
				for _, name := range resetContentTables {
					digest, err := tableContentDigest(ctx, pool, name, "", nil)
					require.NoError(t, err)
					result = append(result, digest)
				}
				return result
			}
			contentBefore := digests()
			target := resetplan.TargetIdentity{RuntimeVersion: "1.4.1", RuntimeCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40)}
			ledger := resetArtifactSchemaLedger(t)
			require.Equal(t, schemaauthority.LatestSupportedMigration, before.MigrationVersions[len(before.MigrationVersions)-1])
			plan, err := Preview(ctx, pool, client, ledger, target, "source-old", "source-new", time.Unix(1, 0))
			require.NoError(t, err)
			require.False(t, plan.ScopeComplete)
			for _, name := range resetContentTables {
				found := false
				for _, table := range plan.Tables {
					if table.Name != name {
						continue
					}
					found = true
					require.Equal(t, resetplan.ScopeReviewRequired, table.Scope)
					require.Equal(t, resetplan.ActionPreserveAll, table.Action)
					require.Zero(t, table.ExpectedDeletedRows)
					require.Equal(t, table.RowCount, table.ExpectedRetainedRows)
					if state == "empty" {
						require.Zero(t, table.RowCount)
					}
				}
				require.True(t, found)
				for _, step := range plan.DeleteOrder {
					require.NotEqual(t, name, step.Table)
				}
			}
			_, err = Apply(ctx, pool, client, ledger, plan, target, "source-new")
			require.ErrorIs(t, err, resetplan.ErrInvalid)
			redisAfter, err := ObserveRedis(ctx, client)
			require.NoError(t, err)
			after, err := Inspect(ctx, pool, redisAfter)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Equal(t, contentBefore, digests(), "all content bytes survive refusal")
			require.Equal(t, redisBefore, redisAfter)
			require.Equal(t, "retained", client.Get(ctx, "wallet:v2:must-remain").Val())
		})
	}
}

func resetArtifactSchemaLedger(t *testing.T) []string {
	t.Helper()
	versions, err := schemaauthority.ExpectedLedger(filepath.Join("..", "..", "migrations"))
	require.NoError(t, err)
	return versions
}

func seedResetIntegrationFixture(t *testing.T, pool *pgxpool.Pool) (int64, int64, string) {
	t.Helper()
	ctx := context.Background()
	passwordHashBytes, err := bcrypt.GenerateFromPassword([]byte("local-integration-fixture-password"), bcrypt.MinCost)
	require.NoError(t, err)
	passwordHash := string(passwordHashBytes)
	var adminOrgID, adminID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type, settings)
		VALUES ('Reset Admin Shell', 'active', 'partner', '{"campaign":"old"}'::jsonb) RETURNING id`).Scan(&adminOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id, username, password_hash, organization_role, role, status, auth_provider, email)
		VALUES ($1, 'Admin', $2, 'platform_admin', 'platform_admin', 'active', 'local', 'old-admin@example.invalid') RETURNING id`,
		adminOrgID, passwordHash).Scan(&adminID))
	_, err = pool.Exec(ctx, `
		INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at)
		VALUES ($1, decode(repeat('ab', 32), 'hex'), NOW(), NOW() + INTERVAL '1 hour')`, adminID)
	require.NoError(t, err)

	var oldOrgID, oldUserID, projectID, apiKeyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type)
		VALUES ('Old Campaign Customer', 'active', 'customer') RETURNING id`).Scan(&oldOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id, username, password_hash, organization_role, role, status, auth_provider)
		VALUES ($1, 'old-user', $2, 'org_admin', 'org_admin', 'active', 'local') RETURNING id`,
		oldOrgID, passwordHash).Scan(&oldUserID))
	_, err = pool.Exec(ctx, `
		INSERT INTO model_deployment_create_receipts (
			customer_org_id,actor_user_id,actor_org_id,operation_scope,
			idempotency_key_hash,request_fingerprint,server_operation_id,
			created_at,expires_at,updated_at)
		VALUES ($1,$2,$1,'model_deployment.create.v1',repeat('a',64),
		        'hmac-sha256:'||repeat('b',64),'op_reset_create_receipt',
		        NOW(),NOW()+INTERVAL '24 hours',NOW())`, oldOrgID, oldUserID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id, name, slug, status, created_by)
		VALUES ($1, 'Old Project', 'old-project', 'active', $2) RETURNING id`,
		oldOrgID, oldUserID).Scan(&projectID))
	_, err = pool.Exec(ctx, `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'project_admin')`, projectID, oldUserID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO wallets (org_id, balance_micros) VALUES ($1, 1000000)`, oldOrgID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (
			org_id, project_id, owner_user_id, name, key_prefix, key_hash,
			allowed_ips, allowed_models, status, key_type, purpose
		) VALUES (
			$1, $2, $3, 'old-feed', 'ur-fixture-old', $4,
			'192.0.2.0/24', '', 'disabled', 'service', 'xinsight.feed.read'
		) RETURNING id`, oldOrgID, projectID, oldUserID, strings.Repeat("e", 64)).Scan(&apiKeyID))
	_, err = pool.Exec(ctx, `
		INSERT INTO completed_execution_feed_connections (
			org_id, project_id, status, acknowledged_sequence, last_authenticated_key_id
		) VALUES ($1, $2, 'paused', 0, $3)`, oldOrgID, projectID, apiKeyID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO completed_execution_feed_states (org_id, project_id, retained_after_id)
		VALUES ($1, $2, 0)`, oldOrgID, projectID)
	require.NoError(t, err)
	fixtureSnapshot := []byte(`{}`)
	_, err = pool.Exec(ctx, `
		INSERT INTO completed_execution_feed_items (
			event_key, org_id, project_id, request_id, terminal_status,
			terminal_available_at, snapshot, snapshot_sha256
		) VALUES ('fixture-feed-event', $1, $2, 'fixture-request', 'settled', NOW(), $3, $4)`,
		oldOrgID, projectID, fixtureSnapshot, digestFixture(fixtureSnapshot))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO audit_logs (org_id, user_id, action, resource, resource_id, detail)
		VALUES ($1, $2, 'old.campaign', 'fixture', 'old', '{}'::jsonb)`, oldOrgID, oldUserID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO internal_outbox_events (
			event_key, event_type, priority, org_id, payload
		) VALUES ('fixture-outbox', 'fixture.event', 100, $1, '{}'::jsonb)`, oldOrgID)
	require.NoError(t, err)
	const cycleRequestID = "reset-cycle-request"
	const cycleAttemptID = "reset-cycle-attempt"
	_, err = pool.Exec(ctx, `
		INSERT INTO freeze_idempotency (request_id, org_id, status, pricing_version)
		VALUES ($1, $2, 'settled', 'reset-cycle-test')`, cycleRequestID, oldOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO billing_provider_attempts (
			org_id, request_id, attempt_id, api_key_id, channel_id, channel_key_id,
			route_sequence, route_total, endpoint, provider, channel_name,
			state, outcome_certainty
		) VALUES ($1,$2,$3,$4,201,301,1,1,'/test','fixture','fixture-channel','settled','known_usage')`,
		oldOrgID, cycleRequestID, cycleAttemptID, apiKeyID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE freeze_idempotency SET delivery_attempt_id=$1
		WHERE org_id=$2 AND request_id=$3`, cycleAttemptID, oldOrgID, cycleRequestID)
	require.NoError(t, err)
	const observationRequestID = "reset-observation-request"
	_, err = pool.Exec(ctx, `
		INSERT INTO freeze_idempotency (
			request_id, org_id, status, pricing_version, billing_mode,
			admission_calculated_cost_micros
		) VALUES ($1, $2, 'frozen', 'reset-observation-price', 'cost_observation', 42000)`,
		observationRequestID, oldOrgID)
	require.NoError(t, err)
	var observationReceiptID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO billing_observation_receipts (
			org_id, request_id, terminal_status, calculated_cost_micros,
			charged_cost_micros, pricing_version
		) VALUES ($1, $2, 'settled', 42000, 0, 'reset-observation-price')
		RETURNING id`, oldOrgID, observationRequestID).Scan(&observationReceiptID))
	_, err = pool.Exec(ctx, `
		UPDATE freeze_idempotency
		SET status='settled', terminal_observation_receipt_id=$1
		WHERE org_id=$2 AND request_id=$3`, observationReceiptID, oldOrgID, observationRequestID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO enterprise_finance_rate_source_evidence (
			org_id, evidence_ref, source_kind, source_url, source_revision,
			fetched_at, payload_sha256, transform_version, currency,
			unit_contract, last_success_at, stale_after, imported_by
		) VALUES (
			$1, 'reset-rate-source', 'official_import',
			'https://example.invalid/pricing', 'fixture-v1', NOW(),
			'sha256:' || repeat('a', 64), 'reset-fixture-v1', 'CNY',
			'tokens', NOW(), NOW() + INTERVAL '1 day', 'reset-fixture'
		)`, oldOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO channels (name, provider, base_url, status)
		VALUES ('Old Provider', 'fixture', 'https://example.invalid', 'disabled')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO settings (key, value, description)
		VALUES ('campaign.fixture', 'true'::jsonb, 'reset integration fixture')
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`)
	require.NoError(t, err)

	var initialAccessRequestID, revisionAccessRequestID, accessGrantID, logicalCredentialID, grantKeyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_requests (
		  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
		  subject_user_id,request_kind,requested_project_role,requested_scope_mode,
		  environment,submission_mode,status,decision_mode,policy_reason_codes,
		  policy_version,submitted_by,decided_by,decided_at
		) VALUES (
		  'req_reset_access_fixture','reset-access-fixture',repeat('f',64),$1,$2,'user',
		  $3,'human_personal','project_admin','project_default','personal','self',
		  'approved','auto_policy',ARRAY['auto_approved_current_project_default'],
		  'unirouter.enterprise-access-policy.v1',$3,$3,NOW()
		) RETURNING id`, oldOrgID, projectID, oldUserID).Scan(&initialAccessRequestID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_grants (
		  grant_ref,access_request_id,org_id,project_id,subject_type,subject_user_id,
		  project_role_snapshot,scope_mode,environment,status,revision,decision_mode,
		  policy_reason_codes,policy_version,semantic_hash,created_by,updated_by
		) VALUES (
		  'grant_reset_access_fixture',$1,$2,$3,'user',$4,'project_admin','project_default',
		  'personal','active',1,'auto_policy',ARRAY['auto_approved_current_project_default'],
		  'unirouter.enterprise-access-policy.v1',repeat('f',64),$4,$4
		) RETURNING id`, initialAccessRequestID, oldOrgID, projectID, oldUserID).Scan(&accessGrantID))
	_, err = pool.Exec(ctx, `
		INSERT INTO access_grant_revisions (
		  access_grant_id,grant_ref,revision,access_request_id,org_id,project_id,
		  subject_type,subject_user_id,project_role_snapshot,scope_mode,environment,production_use,status,
		  valid_from,decision_mode,policy_reason_codes,decision_reason_codes,policy_version,semantic_hash,changed_by
		) VALUES (
		  $1,'grant_reset_access_fixture',1,$2,$3,$4,'user',$5,'project_admin','project_default',
		  'personal',FALSE,'active',NOW(),'auto_policy',ARRAY['auto_approved_current_project_default'],ARRAY[]::TEXT[],
		  'unirouter.enterprise-access-policy.v1',repeat('f',64),$5)`,
		accessGrantID, initialAccessRequestID, oldOrgID, projectID, oldUserID)
	require.NoError(t, err)

	// A real scope revision creates the reviewed request with its target Grant
	// binding already immutable, then atomically advances the stable Grant and
	// appends revision evidence. This is the supported access_requests <->
	// access_grants cycle that reset must defer; the initial request is never
	// rewritten after submission.
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_requests (
		  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
		  subject_user_id,request_kind,requested_project_role,requested_scope_mode,
		  environment,submission_mode,target_access_grant_id,expected_grant_revision,
		  status,decision_mode,policy_reason_codes,decision_reason_codes,
		  policy_version,submitted_by,decided_by,decided_at
		) VALUES (
		  'req_reset_access_revision','reset-access-revision',repeat('a',64),$1,$2,'user',
		  $3,'human_personal','project_admin','project_default','personal','direct_grant',
		  $4,1,'approved','direct',ARRAY['project_default_scope'],
		  ARRAY['direct_grant_authorized_nonproduction'],
		  'unirouter.enterprise-access-policy.v1',$3,$3,NOW()
		) RETURNING id`, oldOrgID, projectID, oldUserID, accessGrantID).Scan(&revisionAccessRequestID))
	_, err = pool.Exec(ctx, `
		UPDATE access_grants
		SET access_request_id=$1,revision=2,decision_mode='direct',
		    policy_reason_codes=ARRAY['project_default_scope'],
		    decision_reason_codes=ARRAY['direct_grant_authorized_nonproduction'],
		    semantic_hash=repeat('a',64),updated_at=NOW()
		WHERE id=$2 AND revision=1`, revisionAccessRequestID, accessGrantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO access_grant_revisions (
		  access_grant_id,grant_ref,revision,access_request_id,org_id,project_id,
		  subject_type,subject_user_id,project_role_snapshot,scope_mode,environment,production_use,status,
		  valid_from,decision_mode,policy_reason_codes,decision_reason_codes,policy_version,semantic_hash,changed_by
		) VALUES (
		  $1,'grant_reset_access_fixture',2,$2,$3,$4,'user',$5,'project_admin','project_default',
		  'personal',FALSE,'active',NOW(),'direct',ARRAY['project_default_scope'],
		  ARRAY['direct_grant_authorized_nonproduction'],
		  'unirouter.enterprise-access-policy.v1',repeat('a',64),$5)`,
		accessGrantID, revisionAccessRequestID, oldOrgID, projectID, oldUserID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_credentials (
		  credential_ref,access_grant_id,org_id,project_id,name,allowed_ips,allowed_models,
		  allowed_endpoints,rpm_limit,rps_limit,created_by
		) VALUES (
		  'cred_reset_access_fixture',$1,$2,$3,'reset grant key','','',
		  '/v1/chat/completions,/v1/embeddings,/v1/models',60,10,$4
		) RETURNING id`, accessGrantID, oldOrgID, projectID, oldUserID).Scan(&logicalCredentialID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (
		  org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,
		  rpm_limit,qps_limit,allowed_ips,allowed_models,allowed_endpoints,purpose,
		  access_grant_id,access_credential_id,credential_ref
		) VALUES ($1,$2,$3,'personal','reset grant key',$4,'ur-reset-grant','active',
		  60,10,'','','/v1/chat/completions,/v1/embeddings,/v1/models','',$5,$6,
		  'cred_reset_access_fixture')
		RETURNING id`, oldOrgID, projectID, oldUserID, strings.Repeat("f", 64), accessGrantID, logicalCredentialID).Scan(&grantKeyID))
	_, err = pool.Exec(ctx, `
		INSERT INTO access_operations (
		  operation_ref,org_id,project_id,access_request_id,access_grant_id,actor_user_id,
		  client_request_id,operation_kind,semantic_hash,successor_key_id,result_payload
		) VALUES (
		  'op_reset_access_fixture',$1,$2,$3,$4,$5,'reset-access-operation',
		  'credential_issue',repeat('f',64),$6,'{}'::jsonb)`,
		oldOrgID, projectID, revisionAccessRequestID, accessGrantID, oldUserID, grantKeyID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO async_media_jobs (
		  job_id,contract_version,org_id,project_id,api_key_id,
		  access_grant_id,access_credential_id,credential_ref,
		  request_id,client_request_id,operation,profile_id,requested_model,routing_model,
		  request_sha256,idempotency_hash,request_ciphertext,
		  admission_state,queue_state,lease_state,execution_state,provider_outcome,
		  cancellation_state,object_state,settlement_state,max_attempts
		) VALUES (
		  'mediajob_reset_access_fixture','unirouter.async-media-runtime.v1',$1,$2,$3,
		  $4,$5,'cred_reset_access_fixture',
		  'reset-media-request','reset-media-client','text_to_image',
		  'unirouter.openai.images.gpt-image.v1','company-image','company-image',
		  'sha256:' || repeat('b',64),'hmac-sha256:' || repeat('c',64),'fixture-ciphertext',
		  'accepted','queued','unleased','pending','pending',
		  'not_requested','pending','not_eligible',3
		)`, oldOrgID, projectID, grantKeyID, accessGrantID, logicalCredentialID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO async_media_limit_reservations (
		  job_id,org_id,project_id,lease_fence,operation_id,reservation,
		  reservation_hash,expires_at,created_at,updated_at
		) VALUES (
		  'mediajob_reset_access_fixture',$1,$2,1,'media-reset-execution-fixture',
		  '{"operation_id":"media-reset-execution-fixture","items":[],"idempotent":false,"extension_sequence":0,"last_extension_tokens":0}'::jsonb,
		  repeat('d',64),NOW()+INTERVAL '1 hour',NOW(),NOW()
		)`, oldOrgID, projectID)
	require.NoError(t, err)

	return adminID, adminOrgID, passwordHash
}

func quotedTable(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func digestFixture(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}
