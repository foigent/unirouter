//go:build integration

package resetexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/resetplan"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestApplyDeterministicResetPreservesExactAdminPlatformStateAndSequences(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	adminID, adminOrgID, passwordHash := seedResetIntegrationFixture(t, pool)

	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	for _, key := range []string{"wallet:v2:fixture", "rl:fixture", "unrelated:preserve"} {
		require.NoError(t, redisClient.Set(ctx, key, "fixture", 0).Err())
	}

	target := resetplan.TargetIdentity{
		RuntimeVersion: "v2.0.2",
		RuntimeCommit:  strings.Repeat("a", 40),
		SourceTree:     strings.Repeat("b", 40),
	}
	plan, err := Preview(ctx, pool, redisClient, target, "source-old", "source-new", time.Unix(1, 0))
	require.NoError(t, err)
	require.True(t, plan.ScopeComplete)
	require.True(t, plan.ApplySupported)
	require.Equal(t, adminID, plan.SelectedAdmin.UserID)
	require.Equal(t, adminOrgID, plan.SelectedAdmin.OrganizationID)
	require.Positive(t, plan.Summary.ExpectedDeletedRows)

	result, err := Apply(ctx, pool, redisClient, plan, target, "source-new")
	require.NoError(t, err)
	require.True(t, result.EmptyBaseline)
	require.Equal(t, adminID, result.RetainedAdminID)
	require.Equal(t, adminOrgID, result.RetainedOrgID)
	require.Equal(t, int64(2), result.RedisKeysDeleted)

	var retainedPassword, retainedUsername, retainedOrganizationRole, retainedCompatibilityRole string
	var retainedAuthEpoch int64
	var retainedEmail *string
	var orgSettings string
	require.NoError(t, pool.QueryRow(ctx, `SELECT username, password_hash, organization_role, role, auth_epoch, email FROM users WHERE id=$1`, adminID).Scan(&retainedUsername, &retainedPassword, &retainedOrganizationRole, &retainedCompatibilityRole, &retainedAuthEpoch, &retainedEmail))
	require.NoError(t, pool.QueryRow(ctx, `SELECT settings::text FROM orgs WHERE id=$1`, adminOrgID).Scan(&orgSettings))
	require.Equal(t, "Admin", retainedUsername)
	require.Equal(t, passwordHash, retainedPassword)
	require.Equal(t, auth.RolePlatformAdmin, retainedOrganizationRole)
	require.Equal(t, auth.RolePlatformAdmin, retainedCompatibilityRole)
	require.EqualValues(t, 1, retainedAuthEpoch)
	require.Nil(t, retainedEmail)
	require.Equal(t, `{}`, orgSettings)
	for _, table := range plan.Tables {
		var count int64
		require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+quotedTable(table.Name)).Scan(&count))
		want := table.ExpectedRetainedRows
		if table.Action == resetplan.ActionPreserveAll {
			want = table.RowCount
		}
		require.Equalf(t, want, count, "post-reset count for %s", table.Name)
	}
	value, err := redisClient.Get(ctx, "unrelated:preserve").Result()
	require.NoError(t, err)
	require.Equal(t, "fixture", value)

	post, err := Preview(ctx, pool, redisClient, target, "source-old", "source-new", time.Unix(2, 0))
	require.NoError(t, err)
	require.Equal(t, plan.Schema, post.Schema, "schema, trigger, FK, and sequence evidence changed")
	require.Equal(t, plan.Protected, post.Protected, "protected platform state changed")
	for _, table := range []string{"async_media_limit_reservations", "async_media_jobs", "api_keys", "access_operations", "access_credentials", "access_grant_revisions", "access_grants", "access_requests"} {
		var count int
		require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+quotedTable(table)).Scan(&count))
		require.Zero(t, count, "%s must be empty after reset", table)
	}
	var triggerEnabled, deleteAction string
	var deferrable, initiallyDeferred bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT t.tgenabled::text
		FROM pg_trigger t
		WHERE t.tgrelid='access_grant_revisions'::regclass
		  AND t.tgname='access_grant_revisions_append_only'`).Scan(&triggerEnabled))
	require.Equal(t, "O", triggerEnabled)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT t.tgenabled::text
		FROM pg_trigger t
		WHERE t.tgrelid='access_operations'::regclass
		  AND t.tgname='access_operations_append_only'`).Scan(&triggerEnabled))
	require.Equal(t, "O", triggerEnabled)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT CASE confdeltype WHEN 'a' THEN 'NO ACTION' ELSE 'OTHER' END,
		       condeferrable,condeferred
		FROM pg_constraint WHERE conname='access_requests_target_grant_scope_fk'`).Scan(
		&deleteAction, &deferrable, &initiallyDeferred))
	require.Equal(t, "NO ACTION", deleteAction)
	require.True(t, deferrable)
	require.False(t, initiallyDeferred)
}

func TestApplyStopsBeforeRedisMutationWhenSemanticScopeDrifts(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	seedResetIntegrationFixture(t, pool)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	require.NoError(t, redisClient.Set(ctx, "wallet:v2:must-remain", "fixture", 0).Err())
	target := resetplan.TargetIdentity{
		RuntimeVersion: "v2.0.2",
		RuntimeCommit:  strings.Repeat("c", 40),
		SourceTree:     strings.Repeat("d", 40),
	}
	plan, err := Preview(ctx, pool, redisClient, target, "source-old", "source-new", time.Unix(1, 0))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO channels (name, provider, base_url, status) VALUES ('drift', 'fixture', 'https://example.invalid', 'disabled')`)
	require.NoError(t, err)

	_, err = Apply(ctx, pool, redisClient, plan, target, "source-new")
	require.ErrorIs(t, err, ErrPlanDrift)
	require.Equal(t, "fixture", redisClient.Get(ctx, "wallet:v2:must-remain").Val())
}

func TestSchema59ResetContractFailsClosedOnAccessTriggerOrFKDriftIntegration(t *testing.T) {
	t.Run("append-only trigger", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		seedResetIntegrationFixture(t, pool)
		_, err := pool.Exec(context.Background(), `
			ALTER TABLE access_grant_revisions DISABLE TRIGGER access_grant_revisions_append_only`)
		require.NoError(t, err)
		_, err = Preview(context.Background(), pool, redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()}),
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
			resetplan.TargetIdentity{RuntimeVersion: "v2.0.2", RuntimeCommit: strings.Repeat("e", 40), SourceTree: strings.Repeat("f", 40)},
			"source-old", "source-new", time.Unix(1, 0))
		require.ErrorContains(t, err, "deferred_cycle_constraint_access_requests_target_grant_scope_fk")
	})
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
