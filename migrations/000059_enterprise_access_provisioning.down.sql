BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE users, projects, project_members, workload_applications,
    workload_application_client_profiles, access_requests, access_grants, access_grant_revisions,
    access_operations, access_credentials, api_keys, freeze_idempotency,
    async_media_limit_reservations, async_media_jobs, request_idempotency, billing_provider_attempts,
    model_call_authorizations, distributed_limit_policies, guardrail_policies,
    schema_migrations IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
	IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000059_enterprise_access_provisioning'
	) THEN
        RAISE EXCEPTION 'enterprise access provisioning rollback refused: later migrations must be reversed first';
    END IF;

	IF EXISTS (SELECT 1 FROM users WHERE offboarded_at IS NOT NULL OR offboarded_by IS NOT NULL) THEN
		RAISE EXCEPTION 'enterprise access provisioning rollback refused: offboarded account evidence is not representable';
	END IF;
	IF EXISTS (
        SELECT 1 FROM model_call_authorizations
		WHERE subject_type='access_credential'
	) THEN
		RAISE EXCEPTION 'enterprise access provisioning rollback refused: access credential model authorization exists';
	END IF;
	IF EXISTS (
        SELECT 1 FROM async_media_jobs
		WHERE access_grant_id IS NOT NULL
		   OR access_credential_id IS NOT NULL
		   OR credential_ref IS NOT NULL
		   OR idempotency_subject_type <> 'api_key'
		   OR idempotency_subject_id <> api_key_id
	) THEN
		RAISE EXCEPTION 'enterprise access provisioning rollback refused: Grant-bound async media evidence exists';
	END IF;
	IF EXISTS (SELECT 1 FROM async_media_limit_reservations) THEN
		RAISE EXCEPTION 'enterprise access provisioning rollback refused: async media execution reservation evidence exists';
	END IF;

    IF EXISTS (SELECT 1 FROM access_requests)
       OR EXISTS (SELECT 1 FROM access_grants)
       OR EXISTS (SELECT 1 FROM access_grant_revisions)
       OR EXISTS (SELECT 1 FROM access_operations)
       OR EXISTS (SELECT 1 FROM access_credentials)
       OR EXISTS (SELECT 1 FROM api_keys WHERE access_grant_id IS NOT NULL)
       OR EXISTS (
            SELECT 1 FROM freeze_idempotency
            WHERE access_grant_id IS NOT NULL OR api_key_id IS NOT NULL
       ) THEN
        RAISE EXCEPTION 'enterprise access provisioning rollback refused: access request, grant, or credential evidence exists';
    END IF;

    IF EXISTS (
        SELECT 1 FROM workload_applications
        WHERE identity_contract_version <> 'unirouter.workload-identity.v1'
           OR logical_ref IS NOT NULL
           OR environment_locked_at IS NOT NULL
           OR production_use
           OR contract_version <> 'unirouter.workload-application.v1'
    ) THEN
        RAISE EXCEPTION 'enterprise access provisioning rollback refused: workload v2 identity exists';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM workload_applications wa
        LEFT JOIN workload_application_client_profiles cp
          ON cp.workload_application_id = wa.id
        GROUP BY wa.id
        HAVING COUNT(cp.client_profile) <> 1
            OR BOOL_OR(NOT cp.is_primary)
            OR MIN(cp.client_profile) <> MIN(wa.client_profile)
    ) THEN
        RAISE EXCEPTION 'enterprise access provisioning rollback refused: multi-profile workload state is not representable';
    END IF;
END;
$$;

DROP TRIGGER access_operations_append_only ON access_operations;
DROP FUNCTION prevent_access_operation_mutation();
DROP TABLE access_operations;
ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_access_binding_check,
    DROP CONSTRAINT freeze_idempotency_access_grant_scope_fk,
    DROP CONSTRAINT freeze_idempotency_access_credential_scope_fk,
    DROP CONSTRAINT freeze_idempotency_key_grant_binding_fk,
    DROP CONSTRAINT freeze_idempotency_api_key_fk,
    DROP COLUMN access_grant_id,
    DROP COLUMN credential_ref,
    DROP COLUMN access_credential_id,
    DROP COLUMN api_key_id;

DROP INDEX api_keys_access_credential_one_active_version;
DROP INDEX api_keys_rotation_successor_unique;
DROP INDEX api_keys_access_grant_idx;

DROP INDEX billing_provider_attempts_unresolved_credential_idx;
DROP TRIGGER billing_provider_attempts_access_binding_canonical ON billing_provider_attempts;
DROP FUNCTION canonicalize_provider_attempt_access_binding();
ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_access_credential_scope_fk,
    DROP CONSTRAINT billing_provider_attempts_access_binding_check,
    DROP COLUMN access_grant_id,
    DROP COLUMN credential_ref,
    DROP COLUMN access_credential_id,
    DROP COLUMN project_id;

DROP TRIGGER async_media_execution_limit_terminal ON async_media_jobs;
DROP FUNCTION mark_async_media_execution_limit_terminal();
DROP TABLE async_media_limit_reservations;

DROP TRIGGER async_media_jobs_access_binding_canonical ON async_media_jobs;
DROP FUNCTION canonicalize_async_media_access_binding();
ALTER TABLE async_media_jobs
    DROP CONSTRAINT async_media_jobs_identity_unique,
    DROP CONSTRAINT async_media_jobs_access_binding_check,
    DROP CONSTRAINT async_media_jobs_access_grant_scope_fk,
    DROP CONSTRAINT async_media_jobs_access_credential_scope_fk,
    DROP CONSTRAINT async_media_jobs_key_access_binding_fk,
    DROP CONSTRAINT async_media_jobs_api_key_fk,
    DROP CONSTRAINT async_media_jobs_id_scope_unique,
    DROP COLUMN idempotency_subject_id,
    DROP COLUMN idempotency_subject_type,
    DROP COLUMN credential_ref,
    DROP COLUMN access_credential_id,
    DROP COLUMN access_grant_id,
    ADD CONSTRAINT async_media_jobs_identity_unique
        UNIQUE (org_id,project_id,api_key_id,idempotency_hash);

ALTER TABLE model_call_authorizations
    DROP CONSTRAINT model_call_authorizations_subject_check,
    ADD CONSTRAINT model_call_authorizations_subject_check CHECK (
        subject_type IN ('api_key','project','user','agent')
    );

DROP INDEX guardrail_policy_access_credential_idx;
ALTER TABLE guardrail_policies
    DROP CONSTRAINT guardrail_policy_mandatory_authority,
    DROP CONSTRAINT guardrail_policy_access_credential_scope_fk,
    DROP CONSTRAINT guardrail_policy_selector_scope,
    DROP COLUMN access_credential_id,
    ADD CONSTRAINT guardrail_policy_selector_scope CHECK (
        (project_id IS NULL OR org_id IS NOT NULL)
        AND (api_key_id IS NULL OR org_id IS NOT NULL)
    ),
    ADD CONSTRAINT guardrail_policy_mandatory_authority CHECK (
        NOT mandatory
        OR (org_id IS NULL AND project_id IS NULL AND api_key_id IS NULL
            AND mode='enforce' AND failure_mode='fail_closed')
    );

DROP INDEX distributed_limit_policy_access_credential_idx;
ALTER TABLE distributed_limit_policies
    DROP CONSTRAINT distributed_limit_policy_access_credential_scope_fk,
    DROP CONSTRAINT distributed_limit_policy_selector_scope,
    DROP COLUMN access_credential_id,
    ADD CONSTRAINT distributed_limit_policy_selector_scope CHECK (
        (project_id IS NULL OR org_id IS NOT NULL)
        AND (api_key_id IS NULL OR org_id IS NOT NULL)
    );

DROP TRIGGER request_idempotency_subject_canonical ON request_idempotency;
DROP FUNCTION canonicalize_request_idempotency_subject();
ALTER TABLE request_idempotency
    DROP CONSTRAINT request_idempotency_pkey,
    DROP CONSTRAINT request_idempotency_subject_check,
    DROP COLUMN idempotency_subject_id,
    DROP COLUMN idempotency_subject_type,
    DROP COLUMN access_credential_id,
    ADD CONSTRAINT request_idempotency_pkey PRIMARY KEY (org_id,api_key_id,key_hash);

DROP TRIGGER api_keys_access_credential_scope_exact ON api_keys;
DROP FUNCTION enforce_access_credential_version_scope();
ALTER TABLE api_keys
    DROP CONSTRAINT api_keys_access_binding_unique,
    DROP CONSTRAINT api_keys_rotation_check,
    DROP CONSTRAINT api_keys_access_credential_scope_fk,
    DROP CONSTRAINT api_keys_rotated_from_fk,
    DROP CONSTRAINT api_keys_access_grant_purpose_check,
    DROP CONSTRAINT api_keys_access_grant_scope_fk,
    DROP COLUMN rotated_from_key_id,
    DROP COLUMN concurrency_limit,
    DROP COLUMN allowed_endpoints,
    DROP COLUMN credential_ref,
    DROP COLUMN access_credential_id,
    DROP COLUMN access_grant_id;

DROP TRIGGER access_credentials_commercial_limit ON access_credentials;

-- Restore the pre-000059 physical api_keys commercial seat contract before
-- removing the logical credential table and columns it referenced.
CREATE OR REPLACE FUNCTION enforce_commercial_resource_limit()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    resource_name TEXT;
    limit_column TEXT;
    constraint_name TEXT;
    signed_limit BIGINT;
    entitlement_expiry TIMESTAMPTZ;
    active_count BIGINT;
BEGIN
    IF NEW.status <> 'active' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.status = 'active' THEN
        RETURN NEW;
    END IF;

    CASE TG_TABLE_NAME
        WHEN 'api_keys' THEN
            resource_name := 'active_api_keys';
            limit_column := 'active_api_keys';
        WHEN 'users' THEN
            resource_name := 'active_users';
            limit_column := 'active_users';
        WHEN 'projects' THEN
            resource_name := 'active_projects';
            limit_column := 'active_projects';
        WHEN 'channels' THEN
            resource_name := 'active_channels';
            limit_column := 'active_channels';
        ELSE
            RAISE EXCEPTION 'unsupported commercial License resource table %', TG_TABLE_NAME;
    END CASE;
    constraint_name := 'commercial_license_' || resource_name;

    PERFORM pg_advisory_xact_lock(hashtextextended('unirouter.commercial_limit.' || resource_name, 0));
    EXECUTE format(
        'SELECT %I, expires_at FROM commercial_license_entitlements WHERE singleton = TRUE',
        limit_column
    ) INTO signed_limit, entitlement_expiry;
    IF signed_limit IS NULL THEN
        RETURN NEW;
    END IF;
    IF entitlement_expiry <= NOW() THEN
        RAISE EXCEPTION 'commercial License is expired; cannot activate %', resource_name
            USING ERRCODE = '23514', CONSTRAINT = constraint_name;
    END IF;

    EXECUTE format('SELECT COUNT(*) FROM %I WHERE status = $1', TG_TABLE_NAME)
        INTO active_count USING 'active';
    IF active_count >= signed_limit THEN
        RAISE EXCEPTION 'commercial License limit exceeded for %: % active, limit %',
            resource_name, active_count, signed_limit
            USING ERRCODE = '23514', CONSTRAINT = constraint_name;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER access_credentials_identity_immutable ON access_credentials;
DROP FUNCTION preserve_access_credential_identity();
DROP INDEX access_credentials_grant_status_idx;
DROP TABLE access_credentials;

DROP TRIGGER access_grant_revisions_append_only ON access_grant_revisions;
DROP FUNCTION prevent_access_grant_revision_mutation();
DROP INDEX access_grant_revisions_request_unique;
DROP TABLE access_grant_revisions;
ALTER TABLE access_requests DROP CONSTRAINT access_requests_target_grant_scope_fk;
DROP TRIGGER access_requests_policy_evidence_immutable ON access_requests;
DROP TRIGGER access_grants_request_binding_exact ON access_grants;
DROP FUNCTION enforce_access_grant_request_binding();
DROP TABLE access_grants;
DROP TABLE access_requests;
DROP FUNCTION preserve_access_decision_evidence();

DROP TRIGGER workload_applications_legacy_client_profile_compatibility
    ON workload_applications;
DROP FUNCTION sync_workload_primary_client_profile_from_legacy_write();
DROP TRIGGER workload_applications_identity_tuple_immutable
    ON workload_applications;
DROP FUNCTION preserve_workload_identity_tuple();

DROP TABLE workload_application_client_profiles;
DROP INDEX workload_applications_logical_identity_unique;

ALTER TABLE workload_applications
    DROP CONSTRAINT workload_applications_id_org_project_unique,
    DROP CONSTRAINT workload_applications_v2_identity_check,
    DROP CONSTRAINT workload_applications_identity_contract_check,
    DROP CONSTRAINT workload_applications_contract_check,
    DROP COLUMN production_use,
    DROP COLUMN environment_locked_at,
    DROP COLUMN logical_ref,
    DROP COLUMN identity_contract_version,
    ALTER COLUMN client_profile SET NOT NULL,
    ADD CONSTRAINT workload_applications_contract_check CHECK (
        contract_version = 'unirouter.workload-application.v1'
    );

ALTER TABLE users
    DROP CONSTRAINT users_offboarding_check,
    DROP CONSTRAINT users_offboarded_by_fk,
    DROP COLUMN offboarded_by,
    DROP COLUMN offboarded_at;

DROP INDEX users_id_org_unique_idx;

DELETE FROM schema_migrations
WHERE version = '000059_enterprise_access_provisioning';

COMMIT;
