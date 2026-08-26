//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

type m77PublicScopeFixture struct {
	orgID, projectID, userID int64
	requestID, grantID       int64
}

type m77GatewayEndpointFixture struct {
	userID, deploymentProfileID int64
}

func downgradeEmptyM78ForM77DownTest(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	execMigrationDown(t, t.Context(), pool, "000078_atomic_model_release_authoring.down.sql")
}

func seedM77GatewayEndpointFixture(t *testing.T, pool *pgxpool.Pool) m77GatewayEndpointFixture {
	t.Helper()
	ctx := t.Context()
	var fixture m77GatewayEndpointFixture
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('m77-gateway-endpoint-profile','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'m77-gateway-endpoint-owner','hash','org_admin','org_admin','active')
RETURNING id`, orgID).Scan(&fixture.userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO deployment_profiles (
  profile_ref,display_name,kind,environment,ingress_mode,dns_mode,
  outbound_mode,notes,status,created_by,updated_by)
VALUES (
  'dep_m77_gateway_profiles','M77 Gateway Profiles','onprem_compose','test',
  'bundled_reverse_proxy','customer_managed','direct','','active',$1,$1)
RETURNING id`, fixture.userID).Scan(&fixture.deploymentProfileID))
	return fixture
}

func insertM77GatewayEndpoint(
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture m77GatewayEndpointFixture,
	endpointRef, baseURL, profileID string,
) error {
	_, err := pool.Exec(ctx, `
INSERT INTO gateway_endpoints (
  deployment_profile_id,endpoint_ref,display_name,base_url,visibility,tls_mode,
  certificate_ref,endpoint_profile,status,created_by,updated_by)
VALUES ($1,$2,$2,$3,'public','external_termination','',$4,'active',$5,$5)`,
		fixture.deploymentProfileID, endpointRef, baseURL, profileID, fixture.userID)
	return err
}

func seedM77PublicEndpointScope(t *testing.T, pool *pgxpool.Pool) m77PublicScopeFixture {
	t.Helper()
	ctx := t.Context()
	var fixture m77PublicScopeFixture
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('m77-public-endpoint-scope','active','customer') RETURNING id`).Scan(&fixture.orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO projects (org_id,name,slug,status)
VALUES ($1,'M77 Public Endpoint Scope','m77-public-endpoint-scope','active') RETURNING id`, fixture.orgID).Scan(&fixture.projectID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'m77-public-endpoint-owner','hash','org_admin','org_admin','active') RETURNING id`, fixture.orgID).Scan(&fixture.userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO access_requests (
  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
  subject_user_id,request_kind,requested_project_role,requested_scope_mode,
  requested_models,requested_endpoints,environment,submission_mode,status,
  decision_mode,policy_reason_codes,decision_reason_codes,policy_version,
  submitted_by,decided_by,decided_at)
VALUES (
  'req_m77_public_scope_000001','m77-public-scope',repeat('a',64),$1,$2,'user',$3,
  'human_personal','project_user','explicit','["catalog-public"]',
  '["/v1/responses","/v1/messages"]','personal','direct_grant','approved','direct',
  ARRAY['special_endpoint'],ARRAY['direct_grant_authorized_nonproduction'],
  'unirouter.enterprise-access-policy.v1',$3,$3,NOW())
RETURNING id`, fixture.orgID, fixture.projectID, fixture.userID).Scan(&fixture.requestID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO access_grants (
  grant_ref,access_request_id,org_id,project_id,subject_type,subject_user_id,
  project_role_snapshot,scope_mode,allowed_models,allowed_endpoints,environment,
  status,valid_from,decision_mode,policy_reason_codes,decision_reason_codes,
  policy_version,semantic_hash,created_by,updated_by)
VALUES (
  'grant_m77_public_scope_000001',$1,$2,$3,'user',$4,'project_user','explicit',
  '["catalog-public"]','["/v1/responses","/v1/messages"]','personal','active',NOW(),
  'direct',ARRAY['special_endpoint'],ARRAY['direct_grant_authorized_nonproduction'],
  'unirouter.enterprise-access-policy.v1',repeat('a',64),$4,$4)
RETURNING id`, fixture.requestID, fixture.orgID, fixture.projectID, fixture.userID).Scan(&fixture.grantID))
	_, err := pool.Exec(ctx, `
INSERT INTO access_grant_revisions (
  access_grant_id,grant_ref,revision,access_request_id,org_id,project_id,
  subject_type,subject_user_id,project_role_snapshot,scope_mode,allowed_models,
  allowed_endpoints,environment,production_use,status,valid_from,decision_mode,
  policy_reason_codes,decision_reason_codes,policy_version,semantic_hash,changed_by)
VALUES (
  $1,'grant_m77_public_scope_000001',1,$2,$3,$4,'user',$5,'project_user','explicit',
  '["catalog-public"]','["/v1/responses","/v1/messages"]','personal',FALSE,'active',NOW(),
  'direct',ARRAY['special_endpoint'],ARRAY['direct_grant_authorized_nonproduction'],
  'unirouter.enterprise-access-policy.v1',repeat('a',64),$5)`,
		fixture.grantID, fixture.requestID, fixture.orgID, fixture.projectID, fixture.userID)
	require.NoError(t, err)
	return fixture
}

func TestPublicModelExecutionAuthorityExtendsExactGrantEndpointScopeIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	fixture := seedM77PublicEndpointScope(t, pool)
	ctx := t.Context()

	for _, target := range []struct{ table, constraint string }{
		{table: "access_requests", constraint: "access_requests_scope_check"},
		{table: "access_grants", constraint: "access_grants_identity_check"},
		{table: "access_grant_revisions", constraint: "access_grant_revisions_scope_check"},
	} {
		var definition string
		require.NoError(t, pool.QueryRow(ctx, `
SELECT pg_get_constraintdef(oid) FROM pg_constraint
 WHERE conrelid=$1::regclass AND conname=$2`, target.table, target.constraint).Scan(&definition))
		require.Contains(t, definition, "/v1/responses")
		require.Contains(t, definition, "/v1/messages")
		require.NotContains(t, definition, "/v1/unknown")
	}

	_, err := pool.Exec(ctx, `
INSERT INTO access_requests (
  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
  subject_user_id,request_kind,requested_project_role,requested_scope_mode,
  requested_models,requested_endpoints,environment,submission_mode,status,
  decision_mode,policy_reason_codes,policy_version,submitted_by)
VALUES (
  'req_m77_unknown_scope_00001','m77-unknown-scope',repeat('b',64),$1,$2,'user',$3,
  'human_personal','project_user','explicit','["catalog-public"]','["/v1/unknown"]',
  'personal','self','needs_review','pending',ARRAY['special_endpoint'],
  'unirouter.enterprise-access-policy.v1',$3)`, fixture.orgID, fixture.projectID, fixture.userID)
	require.ErrorContains(t, err, "access_requests_scope_check")

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `ALTER TABLE access_grants DISABLE TRIGGER access_grants_request_binding_exact`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE access_grants SET allowed_endpoints='["/v1/unknown"]' WHERE id=$1`, fixture.grantID)
	require.ErrorContains(t, err, "access_grants_identity_check")
	require.NoError(t, tx.Rollback(ctx))

	tx, err = pool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `ALTER TABLE access_grant_revisions DISABLE TRIGGER access_grant_revisions_append_only`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE access_grant_revisions SET allowed_endpoints='["/v1/unknown"]' WHERE access_grant_id=$1`, fixture.grantID)
	require.ErrorContains(t, err, "access_grant_revisions_scope_check")
	require.NoError(t, tx.Rollback(ctx))
}

func TestPublicModelExecutionAuthorityGatewayEndpointProfileConstraintIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var definition string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT pg_get_constraintdef(oid) FROM pg_constraint
 WHERE conrelid='gateway_endpoints'::regclass
   AND conname='gateway_endpoints_profile_check'`).Scan(&definition))
	for _, profileID := range []string{
		"endpoint.openai-chat.v1",
		"endpoint.openai-responses.contract.v1",
		"endpoint.anthropic-messages.v1",
	} {
		require.Contains(t, definition, profileID)
	}

	fixture := seedM77GatewayEndpointFixture(t, pool)
	for _, endpoint := range []struct {
		ref, baseURL, profileID string
	}{
		{ref: "gwe_m77_chat", baseURL: "https://chat.m77.example", profileID: "endpoint.openai-chat.v1"},
		{ref: "gwe_m77_responses", baseURL: "https://responses.m77.example", profileID: "endpoint.openai-responses.contract.v1"},
		{ref: "gwe_m77_messages", baseURL: "https://messages.m77.example", profileID: "endpoint.anthropic-messages.v1"},
	} {
		require.NoError(t, insertM77GatewayEndpoint(ctx, pool, fixture, endpoint.ref, endpoint.baseURL, endpoint.profileID))
	}
	err := insertM77GatewayEndpoint(ctx, pool, fixture, "gwe_m77_unknown", "https://unknown.m77.example", "endpoint.unknown.v1")
	require.ErrorContains(t, err, "gateway_endpoints_profile_check")
}

func TestPublicModelExecutionAuthorityResponseUnusableHealthFenceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('m77-response-unusable-health','active','customer') RETURNING id`).Scan(&orgID))
	for index, failureKind := range []string{"channel_failure", "invalid_response"} {
		requestID := "m77-unusable-" + string(rune('a'+index))
		attemptID := "attempt-" + string(rune('a'+index))
		_, err := pool.Exec(ctx, `
INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version)
VALUES ($1,$2,'frozen','migration-test')`, requestID, orgID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
  org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
  route_sequence,route_total,endpoint,provider,channel_name,
  route_evidence_version,route_authority_mode,route_execution_contract_source,
  route_evidence_hash,route_model_operation_id,
  route_legacy_routing_revision,route_legacy_circuit_revision,
  route_legacy_credential_state_revision,route_requires_probe,dispatch_owner_hash)
VALUES ($1,$2,$3,101,201,301,1,1,'/v1/chat/completions','provider','channel',
  'unirouter.execution-route.v1','legacy','legacy_adapter',$4,
  'openai.chat_completions.v1',1,1,1,FALSE,$5)`,
			orgID, requestID, attemptID, "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64))
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `
UPDATE billing_provider_attempts
   SET state='response_unusable',outcome_certainty='unknown',http_status=202,
       failure_kind=$4,response_received_at=NOW(),updated_at=NOW()
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, attemptID, failureKind)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `
UPDATE billing_provider_attempts
   SET health_outcome='provider_failure',health_applied_at=NOW(),updated_at=NOW()
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, attemptID)
		if failureKind == "channel_failure" {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "provider attempt health outcome is out of sequence")
		}
	}
}

func TestPublicModelExecutionAuthorityDatabaseGuardsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var extensions, currentContracts int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT (SELECT COUNT(*) FROM model_operation_public_execution_contract_extensions),
       (SELECT COUNT(*) FROM model_operation_current_public_execution_contracts)`).Scan(
		&extensions, &currentContracts,
	))
	require.Equal(t, 5, extensions, "M77 v1, M86 v2 and M87 tools tuples coexist")
	require.Equal(t, extensions, currentContracts)
	var legacyExtensions int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_operation_public_execution_contract_extensions
WHERE introduced_migration='000077_public_model_execution_authority'`).Scan(&legacyExtensions))
	require.Equal(t, 2, legacyExtensions, "new tuples must not replace historical v1 authority")

	_, err := pool.Exec(ctx, `
UPDATE model_operation_public_execution_contract_extensions
   SET charge_contract_version='other'
 WHERE operation_id='openai.responses.v1'`)
	require.ErrorContains(t, err, "migration-owned and immutable")
	_, err = pool.Exec(ctx, `TRUNCATE model_operation_public_execution_contract_extensions CASCADE`)
	require.ErrorContains(t, err, "migration-owned and immutable")

	_, err = pool.Exec(ctx, `
INSERT INTO model_operation_public_execution_bindings (
    customer_org_id,public_model_id,release_id,ordinal,deployment_id,
    connection_id,operation_id,production_approval_id,https_origin,
    approved_connection_revision,approved_deployment_revision,
    approved_operation_revision,public_path,protocol_profile_id,
    protocol_profile_version,executor_contract_version,
    delivery_contract_version,charge_contract_version,
    idempotency_contract_version,created_by,created_by_org_id,
    source_kind,source_ref)
VALUES (1,1,1,1,1,1,'openai.responses.v1',1,'https://provider.invalid',
        1,1,1,'/v1/responses','unirouter.openai.responses.text.v1','1.0.0',
        'unirouter.endpoint-profile-nonstream-executor.v1',
        'unirouter.public-nonstream-delivery.v1',
        'unirouter.provider-final-token-charge.v1',
        'unirouter.public-at-most-once.v1',1,1,
        'runtime.public_execution_binding.v1','release:1/item:1')`)
	require.Error(t, err, "an unproven public execution binding must fail closed")
}

func TestPublicModelExecutionAuthorityRejectsDirectSealedReleaseIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID, publicModelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('m77-direct-sealed','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,'m77-direct-sealed-owner','fixture','org_admin','active','org_admin') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
    org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'m77-direct-sealed-model','M77 Direct Sealed','openai','ga',TRUE)
RETURNING id`, orgID).Scan(&publicModelID))

	_, err := pool.Exec(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
    candidate,provider_pack_pins,simulation_report,overall_risk,stage,version,
    reason,created_by,public_model_id,route_items_hash,route_items_count,
    route_items_sealed_at)
VALUES (
    'rcr_m77_direct_sealed','m77-direct-sealed-model',
    'unirouter.runtime-change-candidate.v2',
    'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    jsonb_build_object(
        'contract_version','unirouter.runtime-change-candidate.v2',
        'public_model_id',$2::bigint,
        'model_name','m77-direct-sealed-model',
        'deployment_routes',jsonb_build_array(jsonb_build_object(
            'model_operation_id','openai.responses.v1'))),
    '[]'::jsonb,'{}'::jsonb,'review_required','draft',1,
    'must start unsealed',$1,$2,
    'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
    1,NOW())`, userID, publicModelID)
	require.ErrorContains(t, err, "new canonical v2 create and seal require an in-progress authoring receipt")

	var count int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM runtime_change_releases
 WHERE release_ref='rcr_m77_direct_sealed'`).Scan(&count))
	require.Zero(t, count, "failed direct seal must not leave a release with zero normalized items")
}

func TestPublicModelExecutionAuthorityDownRejectsFutureMigrationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	downgradeEmptyM78ForM77DownTest(t, pool)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000079_future_contract')`)
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000077_public_model_execution_authority.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "later migrations must be reversed first")
	var present bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
               WHERE version='000077_public_model_execution_authority')`).Scan(&present))
	require.True(t, present)
	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version='000079_future_contract'`)
	require.NoError(t, err)
}

func TestPublicModelExecutionAuthorityDownRejectsPublicEndpointScopeEvidenceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	downgradeEmptyM78ForM77DownTest(t, pool)
	fixture := seedM77PublicEndpointScope(t, pool)
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000077_public_model_execution_authority.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(down))
	require.ErrorContains(t, err, "durable public endpoint scope evidence exists")

	var requestCount, grantCount, revisionCount, ledgerCount int
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT (SELECT COUNT(*) FROM access_requests WHERE id=$1),
       (SELECT COUNT(*) FROM access_grants WHERE id=$2),
       (SELECT COUNT(*) FROM access_grant_revisions WHERE access_grant_id=$2),
       (SELECT COUNT(*) FROM schema_migrations
         WHERE version='000077_public_model_execution_authority')`,
		fixture.requestID, fixture.grantID).Scan(&requestCount, &grantCount, &revisionCount, &ledgerCount))
	require.Equal(t, 1, requestCount)
	require.Equal(t, 1, grantCount)
	require.Equal(t, 1, revisionCount)
	require.Equal(t, 1, ledgerCount)
}

func TestPublicModelExecutionAuthorityDownRejectsGatewayEndpointProfileEvidenceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	downgradeEmptyM78ForM77DownTest(t, pool)
	fixture := seedM77GatewayEndpointFixture(t, pool)
	require.NoError(t, insertM77GatewayEndpoint(
		t.Context(), pool, fixture,
		"gwe_m77_messages_evidence", "https://messages-evidence.m77.example",
		"endpoint.anthropic-messages.v1",
	))
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000077_public_model_execution_authority.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(down))
	require.ErrorContains(t, err, "Anthropic Messages Gateway Endpoint profile evidence exists")

	var endpointCount, migrationCount int
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT (SELECT COUNT(*) FROM gateway_endpoints
         WHERE endpoint_profile='endpoint.anthropic-messages.v1'),
       (SELECT COUNT(*) FROM schema_migrations
         WHERE version='000077_public_model_execution_authority')`).Scan(
		&endpointCount, &migrationCount,
	))
	require.Equal(t, 1, endpointCount)
	require.Equal(t, 1, migrationCount)
}

func TestPublicModelExecutionAuthorityDownRestoresV1EvidenceSchemaIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	downgradeEmptyM78ForM77DownTest(t, pool)
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000077_public_model_execution_authority.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(down))
	require.NoError(t, err)

	var migrated, attemptColumn, logColumn, traceColumn bool
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT EXISTS(SELECT 1 FROM schema_migrations
               WHERE version='000077_public_model_execution_authority'),
       EXISTS(SELECT 1 FROM information_schema.columns
               WHERE table_schema='public' AND table_name='billing_provider_attempts'
                 AND column_name='route_public_execution_binding_id'),
       EXISTS(SELECT 1 FROM information_schema.columns
               WHERE table_schema='public' AND table_name='call_logs'
                 AND column_name='route_public_execution_binding_id'),
       EXISTS(SELECT 1 FROM information_schema.columns
               WHERE table_schema='public' AND table_name='request_trace_events'
                 AND column_name='route_public_execution_binding_id')`).Scan(
		&migrated, &attemptColumn, &logColumn, &traceColumn,
	))
	require.False(t, migrated)
	require.False(t, attemptColumn)
	require.False(t, logColumn)
	require.False(t, traceColumn)
	for _, target := range []struct{ table, constraint string }{
		{table: "access_requests", constraint: "access_requests_scope_check"},
		{table: "access_grants", constraint: "access_grants_identity_check"},
		{table: "access_grant_revisions", constraint: "access_grant_revisions_scope_check"},
	} {
		var definition string
		require.NoError(t, pool.QueryRow(t.Context(), `
SELECT pg_get_constraintdef(oid) FROM pg_constraint
 WHERE conrelid=$1::regclass AND conname=$2`, target.table, target.constraint).Scan(&definition))
		require.NotContains(t, definition, "/v1/responses")
		require.NotContains(t, definition, "/v1/messages")
		require.Contains(t, definition, "/v1/chat/completions")
		require.Contains(t, definition, "/v1/media/jobs")
	}
	var gatewayProfileDefinition string
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT pg_get_constraintdef(oid) FROM pg_constraint
 WHERE conrelid='gateway_endpoints'::regclass
   AND conname='gateway_endpoints_profile_check'`).Scan(&gatewayProfileDefinition))
	require.Contains(t, gatewayProfileDefinition, "endpoint.openai-chat.v1")
	require.Contains(t, gatewayProfileDefinition, "endpoint.openai-responses.contract.v1")
	require.NotContains(t, gatewayProfileDefinition, "endpoint.anthropic-messages.v1")

	fixture := seedM77GatewayEndpointFixture(t, pool)
	require.NoError(t, insertM77GatewayEndpoint(
		t.Context(), pool, fixture,
		"gwe_m76_chat", "https://chat.m76.example", "endpoint.openai-chat.v1",
	))
	require.NoError(t, insertM77GatewayEndpoint(
		t.Context(), pool, fixture,
		"gwe_m76_responses", "https://responses.m76.example", "endpoint.openai-responses.contract.v1",
	))
	err = insertM77GatewayEndpoint(
		t.Context(), pool, fixture,
		"gwe_m76_messages", "https://messages.m76.example", "endpoint.anthropic-messages.v1",
	)
	require.ErrorContains(t, err, "gateway_endpoints_profile_check")
	var dispatchFence string
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT pg_get_functiondef('preserve_provider_attempt_dispatch_fence()'::regprocedure)`).Scan(&dispatchFence))
	require.NotContains(t, dispatchFence, "'rejected', 'outcome_unknown', 'response_unusable'")
	require.Contains(t, dispatchFence, "'rejected','outcome_unknown'")
}
