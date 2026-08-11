//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEnterpriseAccessProvisioningMigrationIdentityAndTenantInvariants(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var platformOrgID, platformUserID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ('access-platform-org','active','partner') RETURNING id`,
	).Scan(&platformOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'access-platform-admin','hash','platform_admin','platform_admin','active') RETURNING id`, platformOrgID,
	).Scan(&platformUserID))

	var orgID, projectA, projectB, targetUserID, secondSubmitterID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ('access-target-org','active','customer') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Access A','access-a','active') RETURNING id`, orgID,
	).Scan(&projectA))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Access B','access-b','active') RETURNING id`, orgID,
	).Scan(&projectB))
	for _, target := range []struct {
		name string
		id   *int64
	}{{"access-target-user", &targetUserID}, {"access-second-submitter", &secondSubmitterID}} {
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,$2,'hash','member','developer','active') RETURNING id`, orgID, target.name,
		).Scan(target.id))
	}
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectA, targetUserID)
		return err
	}())

	var appID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO workload_applications (
		  contract_version,identity_contract_version,org_id,project_id,application_ref,
		  display_name,kind,environment,identity_mode,client_profile,owner_team,status,
		  logical_ref,environment_locked_at,created_by,updated_by
		) VALUES (
		  'unirouter.workload-application.v1','unirouter.workload-identity.v1',$1,$2,
		  'app_access_identity','Access identity','agent','development','service_credential',
		  'client.openai-sdk.chat.v1','platform','active',NULL,NULL,$3,$3
		) RETURNING id`, orgID, projectA, targetUserID,
	).Scan(&appID))

	// The supported service may classify a v1 row once by atomically supplying
	// the complete v2 identity tuple.
	_, err := pool.Exec(ctx, `
		UPDATE workload_applications
		SET contract_version='unirouter.workload-application.v2',
		    identity_contract_version='unirouter.workload-identity.v2',
		    logical_ref='workload_access_identity',environment_locked_at=NOW()
		WHERE id=$1`, appID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE workload_applications SET identity_contract_version='unirouter.workload-identity.v1',contract_version='unirouter.workload-application.v1' WHERE id=$1`, appID)
	require.ErrorContains(t, err, "workload identity tuple is immutable")
	_, err = pool.Exec(ctx, `UPDATE workload_applications SET identity_contract_version='unirouter.workload-identity.v1',contract_version='unirouter.workload-application.v1',project_id=$2 WHERE id=$1`, appID, projectB)
	require.ErrorContains(t, err, "workload identity tuple is immutable")
	_, err = pool.Exec(ctx, `UPDATE workload_applications SET project_id=$2 WHERE id=$1`, appID, projectB)
	require.ErrorContains(t, err, "workload identity tuple is immutable")
	_, err = pool.Exec(ctx, `UPDATE workload_applications SET production_use=TRUE WHERE id=$1`, appID)
	require.ErrorContains(t, err, "workload identity tuple is immutable")
	_, err = pool.Exec(ctx, `UPDATE workload_applications SET kind='service' WHERE id=$1`, appID)
	require.ErrorContains(t, err, "workload identity tuple is immutable")
	_, err = pool.Exec(ctx, `UPDATE workload_applications SET identity_mode='personal_development' WHERE id=$1`, appID)
	require.ErrorContains(t, err, "workload identity tuple is immutable")
	_, err = pool.Exec(ctx, `UPDATE workload_applications SET display_name='Renamed identity' WHERE id=$1`, appID)
	require.NoError(t, err, "non-identity metadata must remain editable")
	_, err = pool.Exec(ctx, `UPDATE workload_applications SET client_profile=NULL WHERE id=$1`, appID)
	require.NoError(t, err, "v2 client profiles are optional")
	var profileCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM workload_application_client_profiles WHERE workload_application_id=$1`, appID).Scan(&profileCount))
	require.Zero(t, profileCount)

	insertRequest := func(ref, clientRequestID string, submitter, projectID int64, subjectType string, subjectUserID, subjectAppID int64) error {
		environment := "development"
		if subjectType == "user" {
			environment = "personal"
		}
		_, insertErr := pool.Exec(ctx, `
			INSERT INTO access_requests (
			  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
			  subject_user_id,subject_workload_application_id,request_kind,requested_project_role,
			  requested_scope_mode,requested_models,requested_endpoints,environment,submission_mode,
			  status,decision_mode,policy_reason_codes,decision_reason_codes,policy_version,
			  submitted_by,decided_by,decided_at
			) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,0),NULLIF($8,0),$9,
			  'project_user','project_default','[]','[]',$11,'direct_grant','approved','direct',
			  ARRAY['project_default_scope'],ARRAY['direct_grant_authorized_nonproduction'],
			  'unirouter.enterprise-access-policy.v1',$10,$10,NOW())`,
			ref, clientRequestID, strings.Repeat("a", 64), orgID, projectID, subjectType, subjectUserID, subjectAppID,
			map[bool]string{true: "human_personal", false: "workload_service"}[subjectType == "user"], submitter, environment)
		return insertErr
	}

	// Cross-organization platform actor evidence is globally truthful and does
	// not falsely assert that the actor belongs to the target organization.
	require.NoError(t, insertRequest("req_platform_actor_000001", "same-natural-client-id", platformUserID, projectA, "user", targetUserID, 0))
	// The same natural request ID is isolated per submitting actor.
	require.NoError(t, insertRequest("req_second_actor_0000001", "same-natural-client-id", secondSubmitterID, projectA, "user", targetUserID, 0))
	// A workload from project A cannot be used as the subject of project B.
	require.Error(t, insertRequest("req_cross_project_0000001", "cross-project-workload", platformUserID, projectB, "workload_application", 0, appID))
}

func TestEnterpriseAccessProvisioningFamilyPolicySelectorsRequireExactProjectScope(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('access-selector-contract','active','customer') RETURNING id`).Scan(&orgID))

	_, err := pool.Exec(ctx, `
		INSERT INTO distributed_limit_policies (
		  org_id,access_credential_id,name,mode,failure_mode,rpm_limit
		) VALUES ($1,9223372036854775000,'dangling-family-limit','enforce','fail_closed',1)`, orgID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "distributed_limit_policy_selector_scope")

	_, err = pool.Exec(ctx, `
		INSERT INTO guardrail_policies (
		  org_id,access_credential_id,name,stage,detector_ref,action,mode,failure_mode,config
		) VALUES ($1,9223372036854775000,'dangling-family-guardrail','input',
		  'builtin:pii_secrets','block','enforce','fail_closed','{}')`, orgID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "guardrail_policy_selector_scope")

	// Existing organization-level physical-key selector semantics remain
	// unchanged. The missing physical key is rejected by its FK, not by the
	// family/project CHECK.
	_, err = pool.Exec(ctx, `
		INSERT INTO distributed_limit_policies (
		  org_id,api_key_id,name,mode,failure_mode,rpm_limit
		) VALUES ($1,9223372036854775000,'missing-legacy-key','enforce','fail_closed',1)`, orgID)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "distributed_limit_policy_selector_scope")
}

func TestEnterpriseAccessProvisioningDownGuardAndEmptyRollback(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	rollbackEmptyMigrations(t, pool, []string{"000062_xinsight_connector_redeem_lifecycle.down.sql", "000061_xinsight_connector_cp2a_phase_contract.down.sql", "000060_xinsight_connector_cp1.down.sql"})
	var ownedIndexDefinition string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT pg_get_indexdef('users_id_org_unique_idx'::regclass)`).Scan(&ownedIndexDefinition))
	require.Contains(t, ownedIndexDefinition, "CREATE UNIQUE INDEX users_id_org_unique_idx ON public.users USING btree (id, org_id)")
	var activeVersionIndexDefinition string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT pg_get_indexdef('api_keys_access_credential_one_active_version'::regclass)`).Scan(&activeVersionIndexDefinition))
	require.Contains(t, activeVersionIndexDefinition, "UNIQUE INDEX api_keys_access_credential_one_active_version")
	require.Contains(t, activeVersionIndexDefinition, "WHERE ((access_credential_id IS NOT NULL) AND ((status)::text = 'active'::text))")
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000059_enterprise_access_provisioning.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	var ledger int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000059_enterprise_access_provisioning'`).Scan(&ledger))
	require.Zero(t, ledger)
	var indexAfterRollback string
	require.NoError(t, pool.QueryRow(ctx, `SELECT COALESCE(to_regclass('users_id_org_unique_idx')::text,'')`).Scan(&indexAfterRollback))
	require.Empty(t, indexAfterRollback)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COALESCE(to_regclass('api_keys_access_credential_one_active_version')::text,'')`).Scan(&indexAfterRollback))
	require.Empty(t, indexAfterRollback)
}

func TestEnterpriseAccessProvisioningAsyncMediaLegacyBackfillAndRollbackIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000058_enterprise_project_role_separation.up.sql")
	ctx := context.Background()
	var orgID, projectID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('access-media-rollback','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status,budget_period)
		VALUES ($1,'Access Media Rollback','access-media-rollback','active','none') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (
		  org_id,project_id,key_type,name,key_hash,key_prefix,status,allowed_models,purpose
		) VALUES ($1,$2,'shared','legacy media',repeat('1',64),'ur-media-rollback',
		  'active','company-image','') RETURNING id`, orgID, projectID).Scan(&keyID))
	_, err := pool.Exec(ctx, `
		INSERT INTO async_media_jobs (
		  job_id,contract_version,org_id,project_id,api_key_id,request_id,
		  client_request_id,operation,profile_id,requested_model,routing_model,
		  request_sha256,idempotency_hash,request_ciphertext,admission_state,
		  queue_state,lease_state,execution_state,provider_outcome,cancellation_state,
		  object_state,settlement_state,next_attempt_at
		) VALUES (
		  'mediajob_11111111111111111111111111111111','unirouter.async-media-runtime.v1',
		  $1,$2,$3,'legacy-media-request','legacy-media-client','text_to_image',
		  'unirouter.openai.images.gpt-image.v1','company-image','company-image',
		  'sha256:'||repeat('1',64),'hmac-sha256:'||repeat('2',64),'ciphertext',
		  'accepted','queued','unleased','pending','pending','not_requested',
		  'pending','not_eligible',NOW())`, orgID, projectID, keyID)
	require.NoError(t, err)

	up, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000059_enterprise_access_provisioning.up.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(up))
	require.NoError(t, err)
	var subjectType string
	var subjectID int64
	var accessCredentialID *int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT idempotency_subject_type,idempotency_subject_id,access_credential_id
		FROM async_media_jobs WHERE job_id='mediajob_11111111111111111111111111111111'`).Scan(
		&subjectType, &subjectID, &accessCredentialID,
	))
	require.Equal(t, "api_key", subjectType)
	require.Equal(t, keyID, subjectID)
	require.Nil(t, accessCredentialID)

	_, err = pool.Exec(ctx, readEnterpriseAccessDownMigration(t))
	require.NoError(t, err)
	var jobCount, accessColumnCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM async_media_jobs
		WHERE job_id='mediajob_11111111111111111111111111111111' AND api_key_id=$1`, keyID).Scan(&jobCount))
	require.Equal(t, 1, jobCount)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='async_media_jobs'
		  AND column_name IN ('access_grant_id','access_credential_id','credential_ref',
		                      'idempotency_subject_type','idempotency_subject_id')`).Scan(&accessColumnCount))
	require.Zero(t, accessColumnCount)
	var identityConstraint string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid='async_media_jobs'::regclass AND conname='async_media_jobs_identity_unique'`).Scan(&identityConstraint))
	require.Contains(t, identityConstraint, "UNIQUE (org_id, project_id, api_key_id, idempotency_hash)")
}

func TestEnterpriseAccessProvisioningRefusesPreexistingOwnedIndexName(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000058_enterprise_project_role_separation.up.sql")
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		CREATE UNIQUE INDEX users_id_org_unique_idx ON users (id, org_id)
		WHERE status='active'`)
	require.NoError(t, err)
	var before string
	require.NoError(t, pool.QueryRow(ctx, `SELECT pg_get_indexdef('users_id_org_unique_idx'::regclass)`).Scan(&before))
	require.Contains(t, before, "WHERE ((status)::text = 'active'::text)")

	up, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000059_enterprise_access_provisioning.up.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(up))
	require.Error(t, err)
	require.Contains(t, err.Error(), "users_id_org_unique_idx")

	var after string
	require.NoError(t, pool.QueryRow(ctx, `SELECT pg_get_indexdef('users_id_org_unique_idx'::regclass)`).Scan(&after))
	require.Equal(t, before, after, "a failed 000059 must not take ownership of or rewrite a preexisting index")
	var ledger int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000059_enterprise_access_provisioning'`).Scan(&ledger))
	require.Zero(t, ledger)
	var identityColumn int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='workload_applications'
		  AND column_name='identity_contract_version'`).Scan(&identityColumn))
	require.Zero(t, identityColumn)
}

func TestEnterpriseAccessRequestIntentEvidenceIsImmutableAndInt4BoundsFailClosed(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID, applicantID, adminID, requestID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('access-intent-org','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Intent Project','intent-project','active') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'intent-applicant','hash','member','member','active') RETURNING id`, orgID).Scan(&applicantID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'intent-admin','hash','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&adminID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_requests (
		  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
		  subject_user_id,request_kind,requested_project_role,requested_scope_mode,
		  requested_models,requested_endpoints,requested_rpm_limit,requested_rps_limit,
		  requested_concurrency_limit,environment,submission_mode,status,decision_mode,
		  policy_reason_codes,policy_version,submitted_by
		) VALUES (
		  'req_intent_immutable_000001','intent-request',$1,$2,$3,'user',$4,
		  'human_personal','project_user','project_default','[]','[]',2147483647,
		  2147483647,2147483647,'personal','self','needs_review','pending',
		  ARRAY['first_cross_project'],'unirouter.enterprise-access-policy.v1',$4
		) RETURNING id`, strings.Repeat("b", 64), orgID, projectID, applicantID).Scan(&requestID))

	for name, statement := range map[string]string{
		"semantic hash":   `UPDATE access_requests SET semantic_hash=repeat('c',64) WHERE id=$1`,
		"requested scope": `UPDATE access_requests SET requested_models='["model-a"]'::jsonb WHERE id=$1`,
		"submission mode": `UPDATE access_requests SET submission_mode='delegated' WHERE id=$1`,
		"sponsor":         `UPDATE access_requests SET sponsor_user_id=$2 WHERE id=$1`,
		"policy evidence": `UPDATE access_requests SET policy_reason_codes=ARRAY['role_elevation'] WHERE id=$1`,
		"submitter":       `UPDATE access_requests SET submitted_by=$2 WHERE id=$1`,
	} {
		t.Run(name, func(t *testing.T) {
			args := []any{requestID}
			if strings.Contains(statement, "$2") {
				args = append(args, adminID)
			}
			_, err := pool.Exec(ctx, statement, args...)
			require.ErrorContains(t, err, "access request intent and policy evidence is immutable")
		})
	}

	// Decision/lifecycle fields are the deliberately mutable surface.
	_, err := pool.Exec(ctx, `
		UPDATE access_requests
		SET status='approved',decision_mode='manual',decision_reason_codes=ARRAY['manual_approved'],
		    decided_by=$2,decided_at=NOW(),revision=revision+1,updated_at=NOW()
		WHERE id=$1`, requestID, adminID)
	require.NoError(t, err)

	// PostgreSQL INTEGER itself rejects max+1; the explicit CHECK documents and
	// guards the same canonical product boundary for every persisted snapshot.
	_, err = pool.Exec(ctx, `UPDATE access_requests SET requested_rpm_limit=2147483648 WHERE id=$1`, requestID)
	require.Error(t, err)
	var constraintDefinition string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid='access_requests'::regclass AND conname='access_requests_scope_check'`).Scan(&constraintDefinition))
	require.Contains(t, constraintDefinition, "2147483647")
}

func TestEnterpriseAccessProvisioningSponsorEvidenceMatchesSubjectType(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID, actorID, humanID, sponsorID, workloadID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('access-sponsor-evidence','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Sponsor Evidence','access-sponsor-evidence','active') RETURNING id`, orgID).Scan(&projectID))
	for _, target := range []struct {
		username string
		role     string
		id       *int64
	}{
		{username: "access-sponsor-actor", role: "org_admin", id: &actorID},
		{username: "access-sponsor-human", role: "member", id: &humanID},
		{username: "access-sponsor-owner", role: "member", id: &sponsorID},
	} {
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,$2,'hash',$3,$3,'active') RETURNING id`, orgID, target.username, target.role).Scan(target.id))
	}
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO workload_applications (
		  contract_version,identity_contract_version,org_id,project_id,application_ref,logical_ref,
		  display_name,kind,environment,identity_mode,owner_team,status,environment_locked_at,created_by,updated_by
		) VALUES (
		  'unirouter.workload-application.v2','unirouter.workload-identity.v2',$1,$2,
		  'app_sponsor_evidence','workload_sponsor_evidence','Sponsor Evidence','service',
		  'development','service_credential','platform','active',NOW(),$3,$3
		) RETURNING id`, orgID, projectID, actorID).Scan(&workloadID))

	insertRequest := func(t *testing.T, suffix, subjectType string, subjectUserID, subjectWorkloadID, sponsorUserID int64) int64 {
		t.Helper()
		requestKind := "human_personal"
		if subjectType == "workload_application" {
			requestKind = "workload_service"
		}
		environment := "personal"
		if subjectType == "workload_application" {
			environment = "development"
		}
		var requestID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO access_requests (
			  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
			  subject_user_id,subject_workload_application_id,request_kind,requested_project_role,
			  requested_scope_mode,requested_models,requested_endpoints,environment,submission_mode,
			  sponsor_user_id,status,decision_mode,policy_reason_codes,decision_reason_codes,
			  policy_version,submitted_by,decided_by,decided_at
			) VALUES (
			  $1,$2,repeat('a',64),$3,$4,$5,NULLIF($6,0),NULLIF($7,0),$8,'project_user',
			  'project_default','[]','[]',$11,'direct_grant',NULLIF($9,0),
			  'approved','direct',ARRAY['project_default_scope'],ARRAY['direct_grant_authorized_nonproduction'],
			  'unirouter.enterprise-access-policy.v1',$10,$10,NOW()
			) RETURNING id`, "req_sponsor_"+suffix, "sponsor-"+suffix, orgID, projectID,
			subjectType, subjectUserID, subjectWorkloadID, requestKind, sponsorUserID, actorID, environment).Scan(&requestID)
		require.NoError(t, err)
		return requestID
	}

	assertRequestRejected := func(t *testing.T, suffix, subjectType string, subjectUserID, subjectWorkloadID, sponsorUserID int64) {
		t.Helper()
		requestKind := "human_personal"
		if subjectType == "workload_application" {
			requestKind = "workload_service"
		}
		environment := "personal"
		if subjectType == "workload_application" {
			environment = "development"
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO access_requests (
			  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
			  subject_user_id,subject_workload_application_id,request_kind,requested_project_role,
			  requested_scope_mode,requested_models,requested_endpoints,environment,submission_mode,
			  sponsor_user_id,status,decision_mode,policy_reason_codes,policy_version,submitted_by
			) VALUES (
			  $1,$2,repeat('b',64),$3,$4,$5,NULLIF($6,0),NULLIF($7,0),$8,'project_user',
			  'project_default','[]','[]',$11,'delegated',NULLIF($9,0),
			  'needs_review','pending',ARRAY['project_default_scope'],
			  'unirouter.enterprise-access-policy.v1',$10
			)`, "req_sponsor_bad_"+suffix, "sponsor-bad-"+suffix, orgID, projectID,
			subjectType, subjectUserID, subjectWorkloadID, requestKind, sponsorUserID, actorID, environment)
		require.Error(t, err)
		require.Contains(t, err.Error(), "access_requests_subject_check")
	}

	assertRequestRejected(t, "human", "user", humanID, 0, sponsorID)
	assertRequestRejected(t, "workload", "workload_application", 0, workloadID, 0)

	insertGrant := func(t *testing.T, suffix, subjectType string, subjectUserID, subjectWorkloadID, sponsorUserID int64) (int64, int64) {
		t.Helper()
		requestID := insertRequest(t, suffix, subjectType, subjectUserID, subjectWorkloadID, sponsorUserID)
		environment := "personal"
		if subjectType == "workload_application" {
			environment = "development"
		}
		var grantID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO access_grants (
			  grant_ref,access_request_id,org_id,project_id,subject_type,subject_user_id,
			  subject_workload_application_id,sponsor_user_id,project_role_snapshot,scope_mode,
			  allowed_models,allowed_endpoints,environment,status,valid_from,decision_mode,
			  policy_reason_codes,decision_reason_codes,policy_version,semantic_hash,created_by,updated_by
			) VALUES (
			  $1,$2,$3,$4,$5,NULLIF($6,0),NULLIF($7,0),NULLIF($8,0),'project_user','project_default',
			  '[]','[]',$10,'active',NOW(),'direct',ARRAY['project_default_scope'],
			  ARRAY['direct_grant_authorized_nonproduction'],'unirouter.enterprise-access-policy.v1',repeat('a',64),$9,$9
			) RETURNING id`, "grant_sponsor_"+suffix, requestID, orgID, projectID,
			subjectType, subjectUserID, subjectWorkloadID, sponsorUserID, actorID, environment).Scan(&grantID)
		require.NoError(t, err)
		return requestID, grantID
	}

	assertGrantRejected := func(t *testing.T, suffix, subjectType string, subjectUserID, subjectWorkloadID, sponsorUserID int64) {
		t.Helper()
		requestID := insertRequest(t, "grant_bad_"+suffix, subjectType, subjectUserID, subjectWorkloadID,
			map[bool]int64{true: sponsorID, false: 0}[subjectType == "workload_application"])
		environment := "personal"
		if subjectType == "workload_application" {
			environment = "development"
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO access_grants (
			  grant_ref,access_request_id,org_id,project_id,subject_type,subject_user_id,
			  subject_workload_application_id,sponsor_user_id,project_role_snapshot,scope_mode,
			  allowed_models,allowed_endpoints,environment,status,valid_from,decision_mode,
			  policy_reason_codes,decision_reason_codes,policy_version,semantic_hash,created_by,updated_by
			) VALUES (
			  $1,$2,$3,$4,$5,NULLIF($6,0),NULLIF($7,0),NULLIF($8,0),'project_user','project_default',
			  '[]','[]',$10,'active',NOW(),'direct',ARRAY['project_default_scope'],
			  ARRAY['direct_grant_authorized_nonproduction'],'unirouter.enterprise-access-policy.v1',repeat('d',64),$9,$9
			)`, "grant_sponsor_bad_"+suffix, requestID, orgID, projectID,
			subjectType, subjectUserID, subjectWorkloadID, sponsorUserID, actorID, environment)
		require.Error(t, err)
		require.Contains(t, err.Error(), "access grant does not exactly match its approved request")
	}

	assertGrantRejected(t, "human", "user", humanID, 0, sponsorID)
	assertGrantRejected(t, "workload", "workload_application", 0, workloadID, 0)

	humanRequestID, humanGrantID := insertGrant(t, "human_valid", "user", humanID, 0, 0)
	workloadRequestID, workloadGrantID := insertGrant(t, "workload_valid", "workload_application", 0, workloadID, sponsorID)
	assertRevisionRejected := func(t *testing.T, suffix string, requestID, grantID int64, subjectType string, subjectUserID, subjectWorkloadID, sponsorUserID int64) {
		t.Helper()
		environment := "personal"
		if subjectType == "workload_application" {
			environment = "development"
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO access_grant_revisions (
			  access_grant_id,grant_ref,revision,access_request_id,org_id,project_id,subject_type,
			  subject_user_id,subject_workload_application_id,sponsor_user_id,project_role_snapshot,
			  scope_mode,allowed_models,allowed_endpoints,environment,production_use,status,valid_from,decision_mode,
			  policy_reason_codes,decision_reason_codes,policy_version,semantic_hash,changed_by
			) VALUES (
			  $1,$2,1,$3,$4,$5,$6,NULLIF($7,0),NULLIF($8,0),NULLIF($9,0),'project_user',
			  'project_default','[]','[]',$11,FALSE,'active',NOW(),'direct',
			  ARRAY['project_default_scope'],ARRAY['direct_grant_authorized_nonproduction'],
			  'unirouter.enterprise-access-policy.v1',repeat('e',64),$10
			)`, grantID, "grant_sponsor_"+suffix, requestID, orgID, projectID, subjectType,
			subjectUserID, subjectWorkloadID, sponsorUserID, actorID, environment)
		require.Error(t, err)
		require.Contains(t, err.Error(), "access_grant_revisions_scope_check")
	}
	assertRevisionRejected(t, "human_valid", humanRequestID, humanGrantID, "user", humanID, 0, sponsorID)
	assertRevisionRejected(t, "workload_valid", workloadRequestID, workloadGrantID, "workload_application", 0, workloadID, 0)
}

func TestEnterpriseAccessProvisioningDownRejectsNonemptyEvidenceWithoutChangingSchemaDataOrLedger(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	rollbackEmptyMigrations(t, pool, []string{"000062_xinsight_connector_redeem_lifecycle.down.sql", "000061_xinsight_connector_cp2a_phase_contract.down.sql", "000060_xinsight_connector_cp1.down.sql"})
	var orgID, projectID, userID, requestID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('access-down-guard','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Down Guard','access-down-guard','active') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'access-down-guard','hash','member','member','active') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_requests (
		  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,subject_user_id,
		  request_kind,requested_project_role,requested_scope_mode,requested_models,requested_endpoints,
		  environment,submission_mode,status,decision_mode,policy_reason_codes,policy_version,submitted_by
		) VALUES (
		  'req_down_guard_00000001','down-guard',repeat('d',64),$1,$2,'user',$3,
		  'human_personal','project_user','project_default','[]','[]','personal','self',
		  'needs_review','pending',ARRAY['first_cross_project'],'unirouter.enterprise-access-policy.v1',$3
		) RETURNING id`, orgID, projectID, userID).Scan(&requestID))

	down := readEnterpriseAccessDownMigration(t)
	_, err := pool.Exec(ctx, down)
	require.ErrorContains(t, err, "access request, grant, or credential evidence exists")
	var requestCount, ledgerCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM access_requests WHERE id=$1`, requestID).Scan(&requestCount))
	require.Equal(t, 1, requestCount)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000059_enterprise_access_provisioning'`).Scan(&ledgerCount))
	require.Equal(t, 1, ledgerCount)
	var requestsTable string
	require.NoError(t, pool.QueryRow(ctx, `SELECT COALESCE(to_regclass('access_requests')::text,'')`).Scan(&requestsTable))
	require.Equal(t, "access_requests", requestsTable)
}

func TestEnterpriseAccessProvisioningDownRejectsAsyncMediaExecutionReservationEvidence(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	rollbackEmptyMigrations(t, pool, []string{"000062_xinsight_connector_redeem_lifecycle.down.sql", "000061_xinsight_connector_cp2a_phase_contract.down.sql", "000060_xinsight_connector_cp1.down.sql"})
	var orgID, projectID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('access-down-media-reservation','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status,budget_period)
		VALUES ($1,'Down Media Reservation','access-down-media-reservation','active','none') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (org_id,project_id,key_type,name,key_hash,key_prefix,status,allowed_models,purpose)
		VALUES ($1,$2,'shared','legacy media reservation',repeat('6',64),'ur-media-down-rsv',
		  'active','company-image','') RETURNING id`, orgID, projectID).Scan(&keyID))
	_, err := pool.Exec(ctx, `
		INSERT INTO async_media_jobs (
		  job_id,contract_version,org_id,project_id,api_key_id,request_id,client_request_id,
		  operation,profile_id,requested_model,routing_model,request_sha256,idempotency_hash,
		  request_ciphertext,admission_state,queue_state,lease_state,execution_state,
		  provider_outcome,cancellation_state,object_state,settlement_state,max_attempts
		) VALUES (
		  'mediajob_66666666666666666666666666666666','unirouter.async-media-runtime.v1',$1,$2,$3,
		  'down-media-reservation','down-media-reservation','text_to_image',
		  'unirouter.openai.images.gpt-image.v1','company-image','company-image',
		  'sha256:'||repeat('6',64),'hmac-sha256:'||repeat('7',64),'ciphertext','accepted',
		  'queued','unleased','pending','pending','not_requested','pending','not_eligible',3
		)`, orgID, projectID, keyID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO async_media_limit_reservations (
		  job_id,org_id,project_id,lease_fence,operation_id,reservation,reservation_hash,
		  expires_at,created_at,updated_at
		) VALUES (
		  'mediajob_66666666666666666666666666666666',$1,$2,1,'media-down-reservation',
		  '{"operation_id":"media-down-reservation","items":[],"idempotent":false,"extension_sequence":0,"last_extension_tokens":0}'::jsonb,
		  repeat('8',64),NOW()+INTERVAL '1 hour',NOW(),NOW()
		)`, orgID, projectID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, readEnterpriseAccessDownMigration(t))
	require.ErrorContains(t, err, "async media execution reservation evidence exists")
	var reservationCount, ledgerCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM async_media_limit_reservations`).Scan(&reservationCount))
	require.Equal(t, 1, reservationCount)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations WHERE version='000059_enterprise_access_provisioning'`).Scan(&ledgerCount))
	require.Equal(t, 1, ledgerCount)
	var reservationTable string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COALESCE(to_regclass('async_media_limit_reservations')::text,'')`).Scan(&reservationTable))
	require.Equal(t, "async_media_limit_reservations", reservationTable)
}

func TestEnterpriseAccessProvisioningFutureFirstSerializesDownAndPreservesLaterLedger(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rollbackEmptyMigrations(t, pool, []string{"000062_xinsight_connector_redeem_lifecycle.down.sql", "000061_xinsight_connector_cp2a_phase_contract.down.sql", "000060_xinsight_connector_cp1.down.sql"})

	holder, err := pool.Acquire(ctx)
	require.NoError(t, err)
	var holderPID int
	require.NoError(t, holder.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID))
	_, err = holder.Exec(ctx, `BEGIN; LOCK TABLE access_requests IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	holderOpen := true
	t.Cleanup(func() {
		if holderOpen {
			_, _ = holder.Exec(context.Background(), `ROLLBACK`)
		}
		holder.Release()
	})

	runnerApplicationName := "enterprise-access-future-first"
	runnerPool := enterpriseRoleRunnerPool(t, ctx, pool, runnerApplicationName)
	runnerResult := make(chan error, 1)
	go func() {
		runnerResult <- migrate.Run(ctx, runnerPool, enterpriseAccessFutureMigrationDir(t))
	}()
	runnerPID := waitForApplicationBlockedBy(t, pool, runnerApplicationName, holderPID)
	require.True(t, backendHoldsMigrationAdvisoryLock(t, pool, runnerPID))

	downConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer downConn.Release()
	var downPID int
	require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
	downResult := make(chan error, 1)
	go func() {
		_, execErr := downConn.Exec(ctx, readEnterpriseAccessDownMigration(t))
		downResult <- execErr
	}()
	waitForBackendBlockedBy(t, pool, downPID, runnerPID)

	_, err = holder.Exec(ctx, `ROLLBACK`)
	require.NoError(t, err)
	holderOpen = false
	select {
	case runnerErr := <-runnerResult:
		require.NoError(t, runnerErr)
	case <-ctx.Done():
		t.Fatal("future access migration did not complete")
	}
	select {
	case downErr := <-downResult:
		require.ErrorContains(t, downErr, "later migrations must be reversed first")
	case <-ctx.Done():
		t.Fatal("access down did not recheck later ledger")
	}
	assertEnterpriseAccessProvisioningState(t, pool)
}

func TestEnterpriseAccessProvisioningDownFirstSerializesFutureRunnerAndForcesRediscovery(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rollbackEmptyMigrations(t, pool, []string{"000062_xinsight_connector_redeem_lifecycle.down.sql", "000061_xinsight_connector_cp2a_phase_contract.down.sql", "000060_xinsight_connector_cp1.down.sql"})

	holder, err := pool.Acquire(ctx)
	require.NoError(t, err)
	var holderPID int
	require.NoError(t, holder.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID))
	_, err = holder.Exec(ctx, `BEGIN; LOCK TABLE access_requests IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	holderOpen := true
	t.Cleanup(func() {
		if holderOpen {
			_, _ = holder.Exec(context.Background(), `ROLLBACK`)
		}
		holder.Release()
	})

	downConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer downConn.Release()
	var downPID int
	require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
	downResult := make(chan error, 1)
	go func() {
		_, execErr := downConn.Exec(ctx, readEnterpriseAccessDownMigration(t))
		downResult <- execErr
	}()
	waitForBackendBlockedBy(t, pool, downPID, holderPID)
	require.True(t, backendHoldsMigrationAdvisoryLock(t, pool, downPID))

	runnerApplicationName := "enterprise-access-down-first"
	runnerPool := enterpriseRoleRunnerPool(t, ctx, pool, runnerApplicationName)
	runnerResult := make(chan error, 1)
	go func() {
		runnerResult <- migrate.Run(ctx, runnerPool, enterpriseAccessFutureMigrationDir(t))
	}()
	waitForApplicationBlockedBy(t, pool, runnerApplicationName, downPID)

	_, err = holder.Exec(ctx, `ROLLBACK`)
	require.NoError(t, err)
	holderOpen = false
	select {
	case downErr := <-downResult:
		require.NoError(t, downErr)
	case <-ctx.Done():
		t.Fatal("access down did not finish")
	}
	select {
	case runnerErr := <-runnerResult:
		require.NoError(t, runnerErr)
	case <-ctx.Done():
		t.Fatal("future runner did not rediscover access migration")
	}
	assertEnterpriseAccessProvisioningState(t, pool)
}

func readEnterpriseAccessDownMigration(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000059_enterprise_access_provisioning.down.sql"))
	require.NoError(t, err)
	return string(body)
}

func enterpriseAccessFutureMigrationDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	up, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000059_enterprise_access_provisioning.up.sql"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "000059_enterprise_access_provisioning.up.sql"), up, 0o600))
	future := `BEGIN;
SELECT pg_advisory_xact_lock(86420517831702491);
LOCK TABLE access_requests IN SHARE ROW EXCLUSIVE MODE;
SELECT 1;
COMMIT;
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "000060_test_future_access.up.sql"), []byte(future), 0o600))
	return dir
}
