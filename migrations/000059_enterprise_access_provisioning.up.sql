BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

-- Stabilize every identity surface that participates in an access request,
-- grant, or credential binding while the additive contract is installed.
LOCK TABLE users, projects, project_members, workload_applications, api_keys,
    async_media_jobs, request_idempotency, freeze_idempotency,
    billing_provider_attempts, model_call_authorizations,
    distributed_limit_policies, guardrail_policies
    IN SHARE ROW EXCLUSIVE MODE;

-- DELETE /users becomes an auditable offboarding operation in schema 59.
-- The row remains the immutable actor/subject anchor for access evidence;
-- physical purge is deliberately a separate future lifecycle.
ALTER TABLE users
    ADD COLUMN offboarded_at TIMESTAMPTZ(6),
    ADD COLUMN offboarded_by BIGINT,
    ADD CONSTRAINT users_offboarded_by_fk
        FOREIGN KEY (offboarded_by) REFERENCES users(id) ON DELETE RESTRICT,
    ADD CONSTRAINT users_offboarding_check CHECK (
        (offboarded_at IS NULL AND offboarded_by IS NULL)
        OR (offboarded_at IS NOT NULL AND offboarded_by IS NOT NULL AND status='disabled')
    );

CREATE UNIQUE INDEX users_id_org_unique_idx
    ON users (id, org_id);

-- Workload identity v2 separates a logical non-human identity from client
-- compatibility metadata. Existing v1 rows remain readable and are not
-- guessed into the new identity model.
ALTER TABLE workload_applications
    DROP CONSTRAINT workload_applications_contract_check,
    ALTER COLUMN client_profile DROP NOT NULL,
    ADD COLUMN identity_contract_version VARCHAR(64)
        NOT NULL DEFAULT 'unirouter.workload-identity.v1',
    ADD COLUMN logical_ref VARCHAR(64),
    ADD COLUMN environment_locked_at TIMESTAMPTZ(6),
    ADD COLUMN production_use BOOLEAN NOT NULL DEFAULT FALSE,
    ADD CONSTRAINT workload_applications_contract_check CHECK (
        contract_version IN (
            'unirouter.workload-application.v1',
            'unirouter.workload-application.v2'
        )
    ),
    ADD CONSTRAINT workload_applications_identity_contract_check CHECK (
        identity_contract_version IN (
            'unirouter.workload-identity.v1',
            'unirouter.workload-identity.v2'
        )
    ),
    ADD CONSTRAINT workload_applications_v2_identity_check CHECK (
        (identity_contract_version = 'unirouter.workload-identity.v1'
            AND contract_version = 'unirouter.workload-application.v1'
            AND client_profile IS NOT NULL
            AND logical_ref IS NULL
            AND environment_locked_at IS NULL)
        OR (
            identity_contract_version = 'unirouter.workload-identity.v2'
            AND
            contract_version = 'unirouter.workload-application.v2'
            AND logical_ref ~ '^workload_[a-z0-9][a-z0-9._-]{2,54}$'
            AND kind IN ('service', 'agent', 'workflow', 'integration_adapter')
            AND identity_mode = 'service_credential'
            AND environment_locked_at IS NOT NULL
        )
    );

CREATE UNIQUE INDEX workload_applications_logical_identity_unique
    ON workload_applications (org_id, project_id, logical_ref, environment)
    WHERE logical_ref IS NOT NULL;

ALTER TABLE workload_applications
    ADD CONSTRAINT workload_applications_id_org_project_unique
        UNIQUE (id, org_id, project_id);

CREATE TABLE workload_application_client_profiles (
    workload_application_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL,
    client_profile VARCHAR(120) NOT NULL,
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT workload_application_client_profiles_pkey
        PRIMARY KEY (workload_application_id, client_profile),
    CONSTRAINT workload_application_client_profiles_scope_fk
        FOREIGN KEY (workload_application_id, org_id)
        REFERENCES workload_applications(id, org_id) ON DELETE CASCADE,
    CONSTRAINT workload_application_client_profiles_value_check CHECK (
        length(btrim(client_profile)) BETWEEN 1 AND 120
    )
);

CREATE UNIQUE INDEX workload_application_client_profiles_primary_unique
    ON workload_application_client_profiles (workload_application_id)
    WHERE is_primary;

INSERT INTO workload_application_client_profiles (
    workload_application_id, org_id, client_profile, is_primary
)
SELECT id, org_id, client_profile, TRUE
FROM workload_applications
WHERE client_profile IS NOT NULL;

CREATE FUNCTION preserve_workload_identity_tuple()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.identity_contract_version = 'unirouter.workload-identity.v2'
       AND (
           NEW.identity_contract_version IS DISTINCT FROM OLD.identity_contract_version
           OR NEW.contract_version IS DISTINCT FROM OLD.contract_version
           OR
           NEW.org_id IS DISTINCT FROM OLD.org_id
           OR NEW.project_id IS DISTINCT FROM OLD.project_id
           OR NEW.logical_ref IS DISTINCT FROM OLD.logical_ref
           OR NEW.environment IS DISTINCT FROM OLD.environment
           OR NEW.production_use IS DISTINCT FROM OLD.production_use
           OR NEW.kind IS DISTINCT FROM OLD.kind
           OR NEW.identity_mode IS DISTINCT FROM OLD.identity_mode
       ) THEN
        RAISE EXCEPTION 'workload identity tuple is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER workload_applications_identity_tuple_immutable
BEFORE UPDATE OF identity_contract_version, contract_version, org_id, project_id, logical_ref, environment, production_use, kind, identity_mode
ON workload_applications
FOR EACH ROW
EXECUTE FUNCTION preserve_workload_identity_tuple();

-- One release of old writers still updates only client_profile. Keep the
-- primary compatibility mapping synchronized without giving profiles any
-- authorization meaning. Current writers update both representations.
CREATE FUNCTION sync_workload_primary_client_profile_from_legacy_write()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' OR NEW.client_profile IS DISTINCT FROM OLD.client_profile THEN
        IF NEW.client_profile IS NULL THEN
            DELETE FROM workload_application_client_profiles
             WHERE workload_application_id = NEW.id AND is_primary;
        ELSE
            UPDATE workload_application_client_profiles
               SET is_primary = FALSE, updated_at = NOW()
             WHERE workload_application_id = NEW.id AND is_primary;
            INSERT INTO workload_application_client_profiles (
                workload_application_id, org_id, client_profile, is_primary
            ) VALUES (NEW.id, NEW.org_id, NEW.client_profile, TRUE)
            ON CONFLICT (workload_application_id, client_profile)
            DO UPDATE SET is_primary = TRUE, org_id = EXCLUDED.org_id, updated_at = NOW();
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER workload_applications_legacy_client_profile_compatibility
AFTER INSERT OR UPDATE OF client_profile ON workload_applications
FOR EACH ROW
EXECUTE FUNCTION sync_workload_primary_client_profile_from_legacy_write();

CREATE TABLE access_requests (
    id BIGSERIAL PRIMARY KEY,
    request_ref VARCHAR(72) NOT NULL,
    client_request_id VARCHAR(128) NOT NULL,
    semantic_hash CHAR(64) NOT NULL,
    semantic_revision BIGINT NOT NULL DEFAULT 1,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    project_id BIGINT NOT NULL,
    subject_type VARCHAR(32) NOT NULL,
    subject_user_id BIGINT,
    subject_workload_application_id BIGINT,
    request_kind VARCHAR(32) NOT NULL,
    requested_project_role VARCHAR(32) NOT NULL,
    requested_scope_mode VARCHAR(24) NOT NULL,
    requested_models JSONB NOT NULL DEFAULT '[]'::jsonb,
    requested_endpoints JSONB NOT NULL DEFAULT '[]'::jsonb,
    requested_rpm_limit INTEGER,
    requested_rps_limit INTEGER,
    requested_concurrency_limit INTEGER,
    environment VARCHAR(32) NOT NULL,
    production_use BOOLEAN NOT NULL DEFAULT FALSE,
    requested_valid_until TIMESTAMPTZ(6),
    submission_mode VARCHAR(24) NOT NULL,
    sponsor_user_id BIGINT,
    target_access_grant_id BIGINT,
    expected_grant_revision BIGINT,
    status VARCHAR(20) NOT NULL,
    decision_mode VARCHAR(24) NOT NULL,
    policy_reason_codes TEXT[] NOT NULL DEFAULT '{}',
    decision_reason_codes TEXT[] NOT NULL DEFAULT '{}',
    policy_version VARCHAR(64) NOT NULL,
    submitted_by BIGINT NOT NULL,
    decided_by BIGINT,
    submitted_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    decided_at TIMESTAMPTZ(6),
    cancelled_at TIMESTAMPTZ(6),
    cancelled_by BIGINT,
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT access_requests_ref_unique UNIQUE (request_ref),
    CONSTRAINT access_requests_idempotency_unique UNIQUE (org_id, submitted_by, client_request_id),
    CONSTRAINT access_requests_scope_unique UNIQUE (id, org_id, project_id),
    CONSTRAINT access_requests_project_scope_fk
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT access_requests_subject_user_scope_fk
        FOREIGN KEY (subject_user_id, org_id) REFERENCES users(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT access_requests_subject_workload_scope_fk
        FOREIGN KEY (subject_workload_application_id, org_id, project_id)
        REFERENCES workload_applications(id, org_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT access_requests_sponsor_scope_fk
        FOREIGN KEY (sponsor_user_id, org_id) REFERENCES users(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT access_requests_submitter_scope_fk
        FOREIGN KEY (submitted_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT access_requests_decider_scope_fk
        FOREIGN KEY (decided_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT access_requests_canceller_scope_fk
        FOREIGN KEY (cancelled_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT access_requests_ref_check CHECK (
        request_ref ~ '^req_[a-z0-9][a-z0-9_-]{7,67}$'
        AND client_request_id = btrim(client_request_id)
        AND length(client_request_id) BETWEEN 1 AND 128
        AND semantic_hash ~ '^[0-9a-f]{64}$'
        AND semantic_revision > 0
        AND revision > 0
        AND (
            (target_access_grant_id IS NULL AND expected_grant_revision IS NULL)
            OR (target_access_grant_id IS NOT NULL AND expected_grant_revision > 0)
        )
    ),
    CONSTRAINT access_requests_subject_check CHECK (
        (subject_type = 'user' AND subject_user_id IS NOT NULL
            AND subject_workload_application_id IS NULL
            AND request_kind = 'human_personal'
            AND sponsor_user_id IS NULL
            AND environment = 'personal' AND NOT production_use)
        OR
        (subject_type = 'workload_application' AND subject_user_id IS NULL
            AND subject_workload_application_id IS NOT NULL
            AND request_kind = 'workload_service'
            AND requested_project_role = 'project_user'
            AND sponsor_user_id IS NOT NULL)
    ),
    CONSTRAINT access_requests_scope_check CHECK (
        requested_project_role IN ('project_admin', 'project_user', 'project_viewer')
        AND requested_scope_mode IN ('project_default', 'explicit')
        AND jsonb_typeof(requested_models) = 'array'
        AND jsonb_typeof(requested_endpoints) = 'array'
        AND requested_endpoints <@ '["/v1/chat/completions","/v1/embeddings","/v1/models","/v1/media/jobs"]'::jsonb
        AND (
            (requested_scope_mode = 'project_default'
                AND jsonb_array_length(requested_models) = 0
                AND jsonb_array_length(requested_endpoints) = 0)
            OR
            (requested_scope_mode = 'explicit'
                AND jsonb_array_length(requested_models) > 0
                AND jsonb_array_length(requested_endpoints) > 0)
        )
        AND environment ~ '^[a-z][a-z0-9_-]{0,31}$'
        AND (requested_rpm_limit IS NULL OR requested_rpm_limit BETWEEN 0 AND 2147483647)
        AND (requested_rps_limit IS NULL OR requested_rps_limit BETWEEN 0 AND 2147483647)
        AND (requested_concurrency_limit IS NULL OR requested_concurrency_limit BETWEEN 0 AND 2147483647)
        AND (requested_valid_until IS NULL OR requested_valid_until > submitted_at)
    ),
    CONSTRAINT access_requests_lifecycle_check CHECK (
        submission_mode IN ('self', 'delegated', 'direct_grant', 'migration', 'bootstrap', 'recovery')
        AND status IN ('approved', 'needs_review', 'rejected', 'cancelled')
        AND decision_mode IN ('auto_policy', 'manual', 'direct', 'pending', 'requester', 'subject_lifecycle', 'policy_rejection')
        AND policy_version = 'unirouter.enterprise-access-policy.v1'
        AND (
          (status = 'needs_review' AND decision_mode='pending'
             AND decided_at IS NULL AND decided_by IS NULL AND cancelled_at IS NULL AND cancelled_by IS NULL)
          OR (status = 'approved' AND decision_mode IN ('auto_policy','manual','direct')
             AND decided_at IS NOT NULL AND decided_by IS NOT NULL AND cancelled_at IS NULL AND cancelled_by IS NULL)
          OR (status = 'rejected' AND decision_mode IN ('policy_rejection','manual')
             AND decided_at IS NOT NULL AND decided_by IS NOT NULL AND cancelled_at IS NULL AND cancelled_by IS NULL)
          OR (status = 'cancelled' AND decision_mode IN ('requester','subject_lifecycle')
             AND cancelled_at IS NOT NULL AND cancelled_by IS NOT NULL
             AND decided_at IS NULL AND decided_by IS NULL)
        )
    ),
    CONSTRAINT access_requests_reason_evidence_check CHECK (
        cardinality(policy_reason_codes) > 0
        AND policy_reason_codes <@ ARRAY[
          'auto_approved_current_project_default','auto_approved_workload_project_default','project_default_scope',
          'first_cross_project','role_elevation','special_model','special_endpoint','scope_expansion',
          'extra_rate_or_concurrency','validity_extension','production_workload',
          'ambiguous_migration','workload_requires_review','read_only_subject_cannot_invoke'
        ]::TEXT[]
        AND decision_reason_codes <@ ARRAY[
          'direct_grant_authorized_nonproduction','bootstrap_authorized',
          'recovery_authorized','manual_approved','manual_rejected','requester_cancelled',
          'subject_offboarded','workload_archived'
        ]::TEXT[]
        AND (
          (decision_mode='pending' AND cardinality(decision_reason_codes)=0)
          OR (decision_mode='auto_policy' AND cardinality(decision_reason_codes)=0)
          OR (decision_mode='policy_rejection' AND cardinality(decision_reason_codes)=0)
          OR (decision_mode='direct' AND (
               (submission_mode='direct_grant' AND decision_reason_codes=ARRAY['direct_grant_authorized_nonproduction']::TEXT[])
               OR (submission_mode='bootstrap' AND decision_reason_codes=ARRAY['bootstrap_authorized']::TEXT[])
               OR (submission_mode='recovery' AND decision_reason_codes=ARRAY['recovery_authorized']::TEXT[])
             ))
          OR (decision_mode='manual' AND status='approved' AND decision_reason_codes=ARRAY['manual_approved']::TEXT[])
          OR (decision_mode='manual' AND status='rejected' AND decision_reason_codes=ARRAY['manual_rejected']::TEXT[])
          OR (decision_mode='requester' AND status='cancelled' AND decision_reason_codes=ARRAY['requester_cancelled']::TEXT[])
          OR (decision_mode='subject_lifecycle' AND status='cancelled'
              AND decision_reason_codes IN (
                ARRAY['subject_offboarded']::TEXT[], ARRAY['workload_archived']::TEXT[]
              ))
        )
    )
);

CREATE INDEX access_requests_project_subject_idx
    ON access_requests (org_id, project_id, subject_type, status, id DESC);
CREATE INDEX access_requests_status_idx
    ON access_requests (org_id, status, submitted_at DESC, id DESC);

CREATE TABLE access_grants (
    id BIGSERIAL PRIMARY KEY,
    grant_ref VARCHAR(72) NOT NULL,
    access_request_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    project_id BIGINT NOT NULL,
    subject_type VARCHAR(32) NOT NULL,
    subject_user_id BIGINT,
    subject_workload_application_id BIGINT,
    sponsor_user_id BIGINT,
    project_role_snapshot VARCHAR(32) NOT NULL,
    scope_mode VARCHAR(24) NOT NULL,
    allowed_models JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_endpoints JSONB NOT NULL DEFAULT '[]'::jsonb,
    rpm_limit INTEGER,
    rps_limit INTEGER,
    concurrency_limit INTEGER,
    environment VARCHAR(32) NOT NULL,
    production_use BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    revision BIGINT NOT NULL DEFAULT 1,
    valid_from TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    valid_until TIMESTAMPTZ(6),
    decision_mode VARCHAR(24) NOT NULL,
    policy_reason_codes TEXT[] NOT NULL DEFAULT '{}',
    decision_reason_codes TEXT[] NOT NULL DEFAULT '{}',
    policy_version VARCHAR(64) NOT NULL,
    semantic_hash CHAR(64) NOT NULL,
    created_by BIGINT NOT NULL,
    updated_by BIGINT NOT NULL,
    suspended_at TIMESTAMPTZ(6),
    revoked_at TIMESTAMPTZ(6),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT access_grants_ref_unique UNIQUE (grant_ref),
    CONSTRAINT access_grants_request_unique UNIQUE (access_request_id),
    CONSTRAINT access_grants_scope_unique UNIQUE (id, org_id, project_id),
    CONSTRAINT access_grants_request_scope_fk
        FOREIGN KEY (access_request_id, org_id, project_id)
        REFERENCES access_requests(id, org_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT access_grants_project_scope_fk
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT access_grants_subject_user_scope_fk
        FOREIGN KEY (subject_user_id, org_id) REFERENCES users(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT access_grants_subject_workload_scope_fk
        FOREIGN KEY (subject_workload_application_id, org_id, project_id)
        REFERENCES workload_applications(id, org_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT access_grants_sponsor_scope_fk
        FOREIGN KEY (sponsor_user_id, org_id) REFERENCES users(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT access_grants_creator_scope_fk
        FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT access_grants_updater_scope_fk
        FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT access_grants_identity_check CHECK (
        grant_ref ~ '^grant_[a-z0-9][a-z0-9_-]{7,65}$'
        AND semantic_hash ~ '^[0-9a-f]{64}$'
        AND revision > 0
        AND environment ~ '^[a-z][a-z0-9_-]{0,31}$'
        AND project_role_snapshot IN ('project_admin', 'project_user', 'project_viewer')
        AND scope_mode IN ('project_default', 'explicit')
        AND jsonb_typeof(allowed_models) = 'array'
        AND jsonb_typeof(allowed_endpoints) = 'array'
        AND allowed_endpoints <@ '["/v1/chat/completions","/v1/embeddings","/v1/models","/v1/media/jobs"]'::jsonb
        AND (
            (scope_mode = 'project_default'
                AND jsonb_array_length(allowed_models) = 0
                AND jsonb_array_length(allowed_endpoints) = 0)
            OR
            (scope_mode = 'explicit'
                AND jsonb_array_length(allowed_models) > 0
                AND jsonb_array_length(allowed_endpoints) > 0)
        )
        AND (rpm_limit IS NULL OR rpm_limit BETWEEN 0 AND 2147483647)
        AND (rps_limit IS NULL OR rps_limit BETWEEN 0 AND 2147483647)
        AND (concurrency_limit IS NULL OR concurrency_limit BETWEEN 0 AND 2147483647)
        AND (valid_until IS NULL OR valid_until > valid_from)
    ),
    CONSTRAINT access_grants_subject_check CHECK (
        (subject_type = 'user' AND subject_user_id IS NOT NULL
            AND subject_workload_application_id IS NULL
            AND sponsor_user_id IS NULL
            AND environment = 'personal' AND NOT production_use)
        OR
        (subject_type = 'workload_application' AND subject_user_id IS NULL
            AND subject_workload_application_id IS NOT NULL
            AND project_role_snapshot = 'project_user'
            AND sponsor_user_id IS NOT NULL)
    ),
    CONSTRAINT access_grants_lifecycle_check CHECK (
        status IN ('active', 'suspended', 'revoked')
        AND decision_mode IN ('auto_policy', 'manual', 'direct')
        AND policy_version = 'unirouter.enterprise-access-policy.v1'
        AND (status <> 'active' OR (suspended_at IS NULL AND revoked_at IS NULL))
        AND (status <> 'suspended' OR (suspended_at IS NOT NULL AND revoked_at IS NULL))
        AND (status <> 'revoked' OR revoked_at IS NOT NULL)
    ),
    CONSTRAINT access_grants_reason_evidence_check CHECK (
        cardinality(policy_reason_codes) > 0
        AND policy_reason_codes <@ ARRAY[
          'auto_approved_current_project_default','auto_approved_workload_project_default','project_default_scope',
          'first_cross_project','role_elevation','special_model','special_endpoint','scope_expansion',
          'extra_rate_or_concurrency','validity_extension','production_workload',
          'ambiguous_migration','workload_requires_review','read_only_subject_cannot_invoke'
        ]::TEXT[]
        AND decision_reason_codes <@ ARRAY[
          'direct_grant_authorized_nonproduction','bootstrap_authorized',
          'recovery_authorized','manual_approved'
        ]::TEXT[]
        AND (
          (decision_mode='auto_policy' AND cardinality(decision_reason_codes)=0)
          OR (decision_mode='direct' AND decision_reason_codes IN (
               ARRAY['direct_grant_authorized_nonproduction']::TEXT[],
               ARRAY['bootstrap_authorized']::TEXT[],
               ARRAY['recovery_authorized']::TEXT[]
             ))
          OR (decision_mode='manual' AND decision_reason_codes=ARRAY['manual_approved']::TEXT[])
        )
    )
);

CREATE UNIQUE INDEX access_grants_nonrevoked_user_unique
    ON access_grants (org_id, project_id, subject_user_id)
    WHERE status IN ('active', 'suspended') AND subject_user_id IS NOT NULL;
CREATE UNIQUE INDEX access_grants_nonrevoked_workload_unique
    ON access_grants (org_id, project_id, subject_workload_application_id, environment)
    WHERE status IN ('active', 'suspended') AND subject_workload_application_id IS NOT NULL;
CREATE INDEX access_grants_project_status_idx
    ON access_grants (org_id, project_id, status, id DESC);

ALTER TABLE access_requests
    ADD CONSTRAINT access_requests_target_grant_scope_fk
        FOREIGN KEY (target_access_grant_id, org_id, project_id)
        REFERENCES access_grants(id, org_id, project_id)
        DEFERRABLE INITIALLY IMMEDIATE;

CREATE TABLE access_grant_revisions (
    id BIGSERIAL PRIMARY KEY,
    access_grant_id BIGINT NOT NULL,
    grant_ref VARCHAR(72) NOT NULL,
    revision BIGINT NOT NULL,
    access_request_id BIGINT,
    org_id BIGINT NOT NULL,
    project_id BIGINT NOT NULL,
    subject_type VARCHAR(32) NOT NULL,
    subject_user_id BIGINT,
    subject_workload_application_id BIGINT,
    sponsor_user_id BIGINT,
    project_role_snapshot VARCHAR(32) NOT NULL,
    scope_mode VARCHAR(24) NOT NULL,
    allowed_models JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_endpoints JSONB NOT NULL DEFAULT '[]'::jsonb,
    rpm_limit INTEGER,
    rps_limit INTEGER,
    concurrency_limit INTEGER,
    environment VARCHAR(32) NOT NULL,
    production_use BOOLEAN NOT NULL,
    status VARCHAR(20) NOT NULL,
    valid_from TIMESTAMPTZ(6) NOT NULL,
    valid_until TIMESTAMPTZ(6),
    decision_mode VARCHAR(24) NOT NULL,
    policy_reason_codes TEXT[] NOT NULL,
    decision_reason_codes TEXT[] NOT NULL,
    policy_version VARCHAR(64) NOT NULL,
    semantic_hash CHAR(64) NOT NULL,
    revision_kind VARCHAR(24) NOT NULL DEFAULT 'scope_decision',
    lifecycle_action VARCHAR(16),
    lifecycle_reason_code VARCHAR(40),
    changed_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT access_grant_revisions_grant_revision_unique
        UNIQUE (access_grant_id, revision),
    CONSTRAINT access_grant_revisions_grant_scope_fk
        FOREIGN KEY (access_grant_id, org_id, project_id)
        REFERENCES access_grants(id, org_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT access_grant_revisions_request_scope_fk
        FOREIGN KEY (access_request_id, org_id, project_id)
        REFERENCES access_requests(id, org_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT access_grant_revisions_subject_user_scope_fk
        FOREIGN KEY (subject_user_id, org_id) REFERENCES users(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT access_grant_revisions_subject_workload_scope_fk
        FOREIGN KEY (subject_workload_application_id, org_id, project_id)
        REFERENCES workload_applications(id, org_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT access_grant_revisions_sponsor_scope_fk
        FOREIGN KEY (sponsor_user_id, org_id) REFERENCES users(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT access_grant_revisions_scope_check CHECK (
        revision > 0
        AND grant_ref ~ '^grant_[a-z0-9][a-z0-9_-]{7,65}$'
        AND environment ~ '^[a-z][a-z0-9_-]{0,31}$'
        AND status IN ('active', 'suspended', 'revoked')
        AND (
            (subject_type = 'user' AND subject_user_id IS NOT NULL
                AND subject_workload_application_id IS NULL
                AND sponsor_user_id IS NULL
                AND environment = 'personal' AND NOT production_use)
            OR
            (subject_type = 'workload_application' AND subject_user_id IS NULL
                AND subject_workload_application_id IS NOT NULL
                AND project_role_snapshot = 'project_user'
                AND sponsor_user_id IS NOT NULL)
        )
        AND project_role_snapshot IN ('project_admin', 'project_user', 'project_viewer')
        AND scope_mode IN ('project_default', 'explicit')
        AND jsonb_typeof(allowed_models) = 'array'
        AND jsonb_typeof(allowed_endpoints) = 'array'
        AND allowed_endpoints <@ '["/v1/chat/completions","/v1/embeddings","/v1/models","/v1/media/jobs"]'::jsonb
        AND (
            (scope_mode = 'project_default'
                AND jsonb_array_length(allowed_models) = 0
                AND jsonb_array_length(allowed_endpoints) = 0)
            OR
            (scope_mode = 'explicit'
                AND jsonb_array_length(allowed_models) > 0
                AND jsonb_array_length(allowed_endpoints) > 0)
        )
        AND (rpm_limit IS NULL OR rpm_limit BETWEEN 0 AND 2147483647)
        AND (rps_limit IS NULL OR rps_limit BETWEEN 0 AND 2147483647)
        AND (concurrency_limit IS NULL OR concurrency_limit BETWEEN 0 AND 2147483647)
        AND (valid_until IS NULL OR valid_until > valid_from)
        AND semantic_hash ~ '^[0-9a-f]{64}$'
        AND policy_version = 'unirouter.enterprise-access-policy.v1'
        AND (
          (revision_kind='scope_decision' AND access_request_id IS NOT NULL
            AND lifecycle_action IS NULL AND lifecycle_reason_code IS NULL)
          OR
          (revision_kind='lifecycle' AND access_request_id IS NULL
            AND lifecycle_action IN ('suspend','resume','revoke')
            AND lifecycle_reason_code IN (
              'explicit_suspend','explicit_resume','explicit_revoke','membership_removed',
              'membership_noncalling_downgrade','user_offboard','workload_archive'
            ))
        )
    ),
    CONSTRAINT access_grant_revisions_reason_evidence_check CHECK (
        cardinality(policy_reason_codes) > 0
        AND policy_reason_codes <@ ARRAY[
          'auto_approved_current_project_default','auto_approved_workload_project_default','project_default_scope',
          'first_cross_project','role_elevation','special_model','special_endpoint','scope_expansion',
          'extra_rate_or_concurrency','validity_extension','production_workload',
          'ambiguous_migration','workload_requires_review','read_only_subject_cannot_invoke'
        ]::TEXT[]
        AND decision_reason_codes <@ ARRAY[
          'direct_grant_authorized_nonproduction','bootstrap_authorized',
          'recovery_authorized','manual_approved'
        ]::TEXT[]
        AND (
          (decision_mode='auto_policy' AND cardinality(decision_reason_codes)=0)
          OR (decision_mode='direct' AND decision_reason_codes IN (
               ARRAY['direct_grant_authorized_nonproduction']::TEXT[],
               ARRAY['bootstrap_authorized']::TEXT[],
               ARRAY['recovery_authorized']::TEXT[]
             ))
          OR (decision_mode='manual' AND decision_reason_codes=ARRAY['manual_approved']::TEXT[])
        )
    )
);

CREATE INDEX access_grant_revisions_grant_created_idx
    ON access_grant_revisions (access_grant_id, revision DESC);
CREATE UNIQUE INDEX access_grant_revisions_request_unique
    ON access_grant_revisions (access_request_id)
    WHERE access_request_id IS NOT NULL;

CREATE FUNCTION preserve_access_decision_evidence()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(
        NEW.request_ref, NEW.client_request_id, NEW.semantic_hash, NEW.semantic_revision,
        NEW.org_id, NEW.project_id, NEW.subject_type, NEW.subject_user_id,
        NEW.subject_workload_application_id, NEW.request_kind, NEW.requested_project_role,
        NEW.requested_scope_mode, NEW.requested_models, NEW.requested_endpoints,
        NEW.requested_rpm_limit, NEW.requested_rps_limit, NEW.requested_concurrency_limit,
        NEW.environment, NEW.production_use,
        NEW.requested_valid_until, NEW.submission_mode, NEW.sponsor_user_id,
        NEW.target_access_grant_id, NEW.expected_grant_revision, NEW.policy_reason_codes,
        NEW.policy_version, NEW.submitted_by, NEW.submitted_at, NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.request_ref, OLD.client_request_id, OLD.semantic_hash, OLD.semantic_revision,
        OLD.org_id, OLD.project_id, OLD.subject_type, OLD.subject_user_id,
        OLD.subject_workload_application_id, OLD.request_kind, OLD.requested_project_role,
        OLD.requested_scope_mode, OLD.requested_models, OLD.requested_endpoints,
        OLD.requested_rpm_limit, OLD.requested_rps_limit, OLD.requested_concurrency_limit,
        OLD.environment, OLD.production_use,
        OLD.requested_valid_until, OLD.submission_mode, OLD.sponsor_user_id,
        OLD.target_access_grant_id, OLD.expected_grant_revision, OLD.policy_reason_codes,
        OLD.policy_version, OLD.submitted_by, OLD.submitted_at, OLD.created_at
    ) THEN
        RAISE EXCEPTION 'access request intent and policy evidence is immutable';
    END IF;
    IF OLD.status <> 'needs_review'
       AND ROW(
           NEW.status,NEW.decision_mode,NEW.decision_reason_codes,
           NEW.decided_by,NEW.decided_at,NEW.cancelled_by,NEW.cancelled_at,NEW.revision
       ) IS DISTINCT FROM ROW(
           OLD.status,OLD.decision_mode,OLD.decision_reason_codes,
           OLD.decided_by,OLD.decided_at,OLD.cancelled_by,OLD.cancelled_at,OLD.revision
       ) THEN
        RAISE EXCEPTION 'terminal access request decision evidence is immutable';
    END IF;
    IF OLD.status = 'needs_review' AND NEW.status = 'needs_review'
       AND ROW(
           NEW.decision_mode,NEW.decision_reason_codes,NEW.decided_by,
           NEW.decided_at,NEW.cancelled_by,NEW.cancelled_at,NEW.revision
       ) IS DISTINCT FROM ROW(
           OLD.decision_mode,OLD.decision_reason_codes,OLD.decided_by,
           OLD.decided_at,OLD.cancelled_by,OLD.cancelled_at,OLD.revision
       ) THEN
        RAISE EXCEPTION 'pending access request decision evidence changes only at terminal transition';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER access_requests_policy_evidence_immutable
BEFORE UPDATE ON access_requests
FOR EACH ROW EXECUTE FUNCTION preserve_access_decision_evidence();

-- A Grant is the materialized authorization for exactly one approved Request.
-- The same-org/project FK is necessary but not sufficient: this trigger makes
-- subject, sponsor, scope, limits, environment and decision evidence an exact
-- database-level binding. Lifecycle-only status/revision updates do not alter
-- these columns and remain valid.
CREATE FUNCTION enforce_access_grant_request_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    reviewed access_requests%ROWTYPE;
BEGIN
    SELECT * INTO reviewed
      FROM access_requests
     WHERE id=NEW.access_request_id
       AND org_id=NEW.org_id
       AND project_id=NEW.project_id
     FOR KEY SHARE;
    IF NOT FOUND OR reviewed.status <> 'approved'
       OR ROW(
           NEW.subject_type,NEW.subject_user_id,NEW.subject_workload_application_id,
           NEW.sponsor_user_id,NEW.project_role_snapshot,NEW.scope_mode,
           NEW.allowed_models,NEW.allowed_endpoints,NEW.rpm_limit,NEW.rps_limit,
           NEW.concurrency_limit,NEW.environment,NEW.production_use,
           NEW.valid_until,NEW.decision_mode,NEW.policy_reason_codes,
           NEW.decision_reason_codes,NEW.policy_version,NEW.semantic_hash
       ) IS DISTINCT FROM ROW(
           reviewed.subject_type,reviewed.subject_user_id,reviewed.subject_workload_application_id,
           reviewed.sponsor_user_id,reviewed.requested_project_role,reviewed.requested_scope_mode,
           reviewed.requested_models,reviewed.requested_endpoints,reviewed.requested_rpm_limit,
           reviewed.requested_rps_limit,reviewed.requested_concurrency_limit,
           reviewed.environment,reviewed.production_use,
           reviewed.requested_valid_until,reviewed.decision_mode,reviewed.policy_reason_codes,
           reviewed.decision_reason_codes,reviewed.policy_version,reviewed.semantic_hash
       ) THEN
        RAISE EXCEPTION 'access grant does not exactly match its approved request';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER access_grants_request_binding_exact
BEFORE INSERT OR UPDATE OF access_request_id,org_id,project_id,subject_type,subject_user_id,
    subject_workload_application_id,sponsor_user_id,project_role_snapshot,scope_mode,
    allowed_models,allowed_endpoints,rpm_limit,rps_limit,concurrency_limit,
    environment,production_use,valid_until,decision_mode,policy_reason_codes,
    decision_reason_codes,policy_version,semantic_hash
ON access_grants
FOR EACH ROW EXECUTE FUNCTION enforce_access_grant_request_binding();

CREATE FUNCTION prevent_access_grant_revision_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'access grant revisions are append-only';
END;
$$;

CREATE TRIGGER access_grant_revisions_append_only
BEFORE UPDATE OR DELETE ON access_grant_revisions
FOR EACH ROW EXECUTE FUNCTION prevent_access_grant_revision_mutation();

-- A logical credential is the stable authorization and quota identity. Secret
-- rotation creates another api_keys version under this row; it must never
-- create a new scope or a fresh budget/rate bucket.
CREATE TABLE access_credentials (
    id BIGSERIAL PRIMARY KEY,
    credential_ref VARCHAR(72) NOT NULL,
    access_grant_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL,
    project_id BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    allowed_ips TEXT NOT NULL DEFAULT '',
    allowed_models TEXT NOT NULL DEFAULT '',
    allowed_endpoints TEXT NOT NULL,
    -- One-release compatibility shadow for administrator-created legacy key
    -- labels. Canonical Access Credential APIs never accept caller labels;
    -- new policies target credential/grant selectors instead.
    policy_labels TEXT NOT NULL DEFAULT '',
    rpm_limit INTEGER NOT NULL,
    rps_limit INTEGER NOT NULL,
    concurrency_limit INTEGER,
    -- Compatibility-only shadow for the pre-existing api_keys budget field.
    -- It is not an Access Grant finance ledger and is never used by Gateway
    -- billing/freeze adjudication; Agent capability attenuation retains the
    -- older narrow-only behavior.
    legacy_budget_limit_cents INTEGER,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    revoked_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    revoked_at TIMESTAMPTZ(6),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT access_credentials_ref_unique UNIQUE (credential_ref),
    CONSTRAINT access_credentials_project_name_unique UNIQUE (org_id, project_id, name),
    CONSTRAINT access_credentials_scope_unique
        UNIQUE (id, credential_ref, access_grant_id, org_id, project_id),
    CONSTRAINT access_credentials_id_org_project_unique
        UNIQUE (id, org_id, project_id),
    CONSTRAINT access_credentials_grant_scope_fk
        FOREIGN KEY (access_grant_id, org_id, project_id)
        REFERENCES access_grants(id, org_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT access_credentials_identity_check CHECK (
        credential_ref ~ '^cred_[a-z0-9][a-z0-9_-]{7,66}$'
        AND name=btrim(name) AND length(name) BETWEEN 1 AND 255
        AND allowed_endpoints <> ''
        AND rpm_limit BETWEEN 0 AND 2147483647
        AND rps_limit BETWEEN 0 AND 2147483647
        AND (concurrency_limit IS NULL OR concurrency_limit BETWEEN 0 AND 2147483647)
        AND (legacy_budget_limit_cents IS NULL OR legacy_budget_limit_cents BETWEEN 0 AND 2147483647)
        AND status IN ('active','revoked')
        AND ((status='active' AND revoked_at IS NULL AND revoked_by IS NULL)
             OR (status='revoked' AND revoked_at IS NOT NULL AND revoked_by IS NOT NULL))
    )
);

CREATE INDEX access_credentials_grant_status_idx
    ON access_credentials (access_grant_id, status, id);

CREATE FUNCTION preserve_access_credential_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(
        NEW.credential_ref,NEW.access_grant_id,NEW.org_id,NEW.project_id,NEW.name,
        NEW.allowed_ips,NEW.allowed_models,NEW.allowed_endpoints,NEW.policy_labels,
        NEW.rpm_limit,NEW.rps_limit,NEW.concurrency_limit,NEW.legacy_budget_limit_cents,
        NEW.created_by,NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.credential_ref,OLD.access_grant_id,OLD.org_id,OLD.project_id,OLD.name,
        OLD.allowed_ips,OLD.allowed_models,OLD.allowed_endpoints,OLD.policy_labels,
        OLD.rpm_limit,OLD.rps_limit,OLD.concurrency_limit,OLD.legacy_budget_limit_cents,
        OLD.created_by,OLD.created_at
    ) THEN
        RAISE EXCEPTION 'access credential identity and scope are immutable';
    END IF;
	IF OLD.status='revoked' AND ROW(NEW.status,NEW.revoked_by,NEW.revoked_at)
	    IS DISTINCT FROM ROW(OLD.status,OLD.revoked_by,OLD.revoked_at) THEN
		RAISE EXCEPTION 'revoked access credential lifecycle evidence is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER access_credentials_identity_immutable
BEFORE UPDATE ON access_credentials
FOR EACH ROW EXECUTE FUNCTION preserve_access_credential_identity();

ALTER TABLE api_keys
    ADD COLUMN access_grant_id BIGINT,
    ADD COLUMN access_credential_id BIGINT,
    ADD COLUMN credential_ref VARCHAR(72),
    ADD COLUMN allowed_endpoints TEXT NOT NULL DEFAULT '',
    ADD COLUMN concurrency_limit INTEGER,
    ADD COLUMN rotated_from_key_id BIGINT,
    ADD CONSTRAINT api_keys_access_grant_scope_fk
        FOREIGN KEY (access_grant_id, org_id, project_id)
        REFERENCES access_grants(id, org_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT api_keys_rotated_from_fk
        FOREIGN KEY (rotated_from_key_id) REFERENCES api_keys(id) ON DELETE RESTRICT,
    ADD CONSTRAINT api_keys_access_credential_scope_fk
        FOREIGN KEY (access_credential_id, credential_ref, access_grant_id, org_id, project_id)
        REFERENCES access_credentials(id, credential_ref, access_grant_id, org_id, project_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT api_keys_access_grant_purpose_check CHECK (
        (access_grant_id IS NULL AND access_credential_id IS NULL AND credential_ref IS NULL)
        OR (access_grant_id IS NOT NULL AND access_credential_id IS NOT NULL
            AND credential_ref IS NOT NULL AND project_id IS NOT NULL AND COALESCE(purpose, '') = '')
    ),
    ADD CONSTRAINT api_keys_rotation_check CHECK (
        rotated_from_key_id IS NULL
        OR (access_grant_id IS NOT NULL AND COALESCE(purpose, '') = '')
    ),
    ADD CONSTRAINT api_keys_access_binding_unique
        UNIQUE (id, access_credential_id, credential_ref, access_grant_id, org_id, project_id);

CREATE FUNCTION enforce_access_credential_version_scope()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    logical access_credentials%ROWTYPE;
BEGIN
    IF NEW.access_credential_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT * INTO logical
      FROM access_credentials
     WHERE id=NEW.access_credential_id
       AND credential_ref=NEW.credential_ref
       AND access_grant_id=NEW.access_grant_id
       AND org_id=NEW.org_id
       AND project_id=NEW.project_id
       AND status='active';
    IF NOT FOUND OR ROW(
        NEW.name,COALESCE(NEW.allowed_ips,''),COALESCE(NEW.allowed_models,''),
        COALESCE(NEW.allowed_endpoints,''),COALESCE(NEW.policy_labels,''),
        NEW.rpm_limit,NEW.qps_limit,NEW.concurrency_limit,NEW.budget_limit
    ) IS DISTINCT FROM ROW(
        logical.name,logical.allowed_ips,logical.allowed_models,
        logical.allowed_endpoints,logical.policy_labels,
        logical.rpm_limit,logical.rps_limit,logical.concurrency_limit,logical.legacy_budget_limit_cents
    ) THEN
        RAISE EXCEPTION 'API key version does not match logical access credential';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER api_keys_access_credential_scope_exact
BEFORE INSERT OR UPDATE OF access_credential_id,credential_ref,access_grant_id,org_id,project_id,
    name,allowed_ips,allowed_models,allowed_endpoints,policy_labels,rpm_limit,qps_limit,
    concurrency_limit,budget_limit
ON api_keys
FOR EACH ROW EXECUTE FUNCTION enforce_access_credential_version_scope();

CREATE INDEX api_keys_access_grant_idx
    ON api_keys (access_grant_id, status, id)
    WHERE access_grant_id IS NOT NULL;
CREATE UNIQUE INDEX api_keys_rotation_successor_unique
    ON api_keys (rotated_from_key_id)
    WHERE rotated_from_key_id IS NOT NULL;
-- One logical Credential family has exactly one currently usable physical
-- secret. Atomic replacement disables the locked predecessor before inserting
-- its successor; transaction rollback restores the predecessor on any insert
-- or License failure.
CREATE UNIQUE INDEX api_keys_access_credential_one_active_version
    ON api_keys (access_credential_id)
    WHERE access_credential_id IS NOT NULL AND status='active';

-- Async media keeps the physical secret version that admitted the job as
-- durable evidence, while idempotency for a Grant-bound credential is owned
-- by the stable logical credential. This lets an atomic secret replacement
-- replay the already accepted job instead of creating a duplicate. Legacy
-- unbound keys retain their physical-key lane.
ALTER TABLE async_media_jobs
    DROP CONSTRAINT async_media_jobs_identity_unique,
    ADD COLUMN access_grant_id BIGINT,
    ADD COLUMN access_credential_id BIGINT,
    ADD COLUMN credential_ref VARCHAR(72),
    ADD COLUMN idempotency_subject_type VARCHAR(24),
    ADD COLUMN idempotency_subject_id BIGINT;

CREATE FUNCTION canonicalize_async_media_access_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    key_grant_id BIGINT;
    key_credential_id BIGINT;
    key_credential_ref VARCHAR(72);
BEGIN
    SELECT access_grant_id,access_credential_id,credential_ref
      INTO key_grant_id,key_credential_id,key_credential_ref
      FROM api_keys
     WHERE id=NEW.api_key_id
       AND org_id=NEW.org_id
       AND project_id=NEW.project_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'async media API key is outside the job scope';
    END IF;
    IF (NEW.access_grant_id IS NOT NULL
        AND NEW.access_grant_id IS DISTINCT FROM key_grant_id)
       OR (NEW.access_credential_id IS NOT NULL
        AND NEW.access_credential_id IS DISTINCT FROM key_credential_id)
       OR (NEW.credential_ref IS NOT NULL
        AND NEW.credential_ref IS DISTINCT FROM key_credential_ref) THEN
        RAISE EXCEPTION 'async media access binding does not match API key';
    END IF;
    NEW.access_grant_id := key_grant_id;
    NEW.access_credential_id := key_credential_id;
    NEW.credential_ref := key_credential_ref;
    IF key_credential_id IS NULL THEN
        NEW.idempotency_subject_type := 'api_key';
        NEW.idempotency_subject_id := NEW.api_key_id;
    ELSE
        NEW.idempotency_subject_type := 'access_credential';
        NEW.idempotency_subject_id := key_credential_id;
    END IF;
    RETURN NEW;
END;
$$;

UPDATE async_media_jobs job
   SET access_grant_id=key.access_grant_id,
       access_credential_id=key.access_credential_id,
       credential_ref=key.credential_ref,
       idempotency_subject_type=CASE WHEN key.access_credential_id IS NULL
           THEN 'api_key' ELSE 'access_credential' END,
       idempotency_subject_id=COALESCE(key.access_credential_id,key.id)
  FROM api_keys key
 WHERE key.id=job.api_key_id
   AND key.org_id=job.org_id
   AND key.project_id=job.project_id;

ALTER TABLE async_media_jobs
    ALTER COLUMN idempotency_subject_type SET NOT NULL,
    ALTER COLUMN idempotency_subject_id SET NOT NULL,
    ADD CONSTRAINT async_media_jobs_api_key_fk
        FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE RESTRICT,
    ADD CONSTRAINT async_media_jobs_key_access_binding_fk
        FOREIGN KEY (api_key_id,access_credential_id,credential_ref,access_grant_id,org_id,project_id)
        REFERENCES api_keys(id,access_credential_id,credential_ref,access_grant_id,org_id,project_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT async_media_jobs_access_credential_scope_fk
        FOREIGN KEY (access_credential_id,credential_ref,access_grant_id,org_id,project_id)
        REFERENCES access_credentials(id,credential_ref,access_grant_id,org_id,project_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT async_media_jobs_access_grant_scope_fk
        FOREIGN KEY (access_grant_id,org_id,project_id)
        REFERENCES access_grants(id,org_id,project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT async_media_jobs_access_binding_check CHECK (
        (idempotency_subject_type='api_key'
            AND idempotency_subject_id=api_key_id
            AND access_grant_id IS NULL AND access_credential_id IS NULL
            AND credential_ref IS NULL)
        OR
        (idempotency_subject_type='access_credential'
            AND idempotency_subject_id=access_credential_id
            AND access_grant_id IS NOT NULL AND access_credential_id IS NOT NULL
            AND credential_ref IS NOT NULL)
    ),
    ADD CONSTRAINT async_media_jobs_id_scope_unique UNIQUE (job_id,org_id,project_id),
    ADD CONSTRAINT async_media_jobs_identity_unique UNIQUE (
        org_id,project_id,idempotency_subject_type,idempotency_subject_id,idempotency_hash
    );

CREATE TRIGGER async_media_jobs_access_binding_canonical
BEFORE INSERT OR UPDATE OF org_id,project_id,api_key_id,access_grant_id,
    access_credential_id,credential_ref,idempotency_subject_type,idempotency_subject_id
ON async_media_jobs
FOR EACH ROW EXECUTE FUNCTION canonicalize_async_media_access_binding();

-- Media queue depth is governed by the durable project/organization job caps,
-- while execution concurrency is reserved only immediately before Provider
-- dispatch. The durable snapshot below lets any worker reconcile the shared
-- limit-policy operation after success, failure, cancellation, retry, or a
-- process crash. It contains no bearer secret or new admission authority.
CREATE TABLE async_media_limit_reservations (
    job_id VARCHAR(48) NOT NULL,
    org_id BIGINT NOT NULL,
    project_id BIGINT NOT NULL,
    lease_fence BIGINT NOT NULL,
    operation_id VARCHAR(64) NOT NULL,
    reservation JSONB NOT NULL,
    reservation_hash CHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ(6) NOT NULL,
    terminal_kind VARCHAR(20) NOT NULL DEFAULT '',
    terminalized_at TIMESTAMPTZ(6),
    terminal_outcome VARCHAR(40) NOT NULL DEFAULT '',
    terminal_owner VARCHAR(128) NOT NULL DEFAULT '',
    terminal_fence BIGINT NOT NULL DEFAULT 0,
    terminal_claim_expires_at TIMESTAMPTZ(6),
    last_error VARCHAR(512) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ(6) NOT NULL,
    updated_at TIMESTAMPTZ(6) NOT NULL,
    PRIMARY KEY (job_id,lease_fence),
    CONSTRAINT async_media_limit_reservations_operation_unique UNIQUE (operation_id),
    CONSTRAINT async_media_limit_reservations_job_scope_fk
        FOREIGN KEY (job_id,org_id,project_id)
        REFERENCES async_media_jobs(job_id,org_id,project_id) ON DELETE RESTRICT,
    CONSTRAINT async_media_limit_reservations_identity_check CHECK (
        org_id > 0 AND project_id > 0 AND lease_fence > 0
        AND operation_id <> ''
        AND reservation_hash ~ '^[a-f0-9]{64}$'
    ),
    CONSTRAINT async_media_limit_reservations_payload_check CHECK (
        jsonb_typeof(reservation)='object'
        AND octet_length(reservation::text) <= 65536
    ),
    CONSTRAINT async_media_limit_reservations_terminal_check CHECK (
        terminal_kind IN ('','settled','cancelled')
        AND terminal_outcome IN ('','settled','cancelled','operation_state_expired')
        AND ((terminalized_at IS NULL AND terminal_outcome='')
             OR (terminalized_at IS NOT NULL AND terminal_kind<>'' AND terminal_outcome<>''))
        AND ((terminal_owner='' AND terminal_claim_expires_at IS NULL)
             OR (terminal_owner<>'' AND terminal_claim_expires_at IS NOT NULL))
        AND terminal_fence >= 0
    ),
    CONSTRAINT async_media_limit_reservations_time_check CHECK (
        expires_at > created_at AND updated_at >= created_at
        AND (terminalized_at IS NULL OR terminalized_at >= created_at)
    )
);

CREATE INDEX async_media_limit_reservations_pending_idx
    ON async_media_limit_reservations (updated_at,job_id,lease_fence)
    WHERE terminal_kind<>'' AND terminalized_at IS NULL;

CREATE FUNCTION mark_async_media_execution_limit_terminal()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    terminal VARCHAR(20);
BEGIN
    terminal := '';
    IF OLD.provider_outcome='pending' AND NEW.provider_outcome='succeeded' THEN
        terminal := 'settled';
    ELSIF NEW.execution_state='terminal'
       OR (OLD.execution_state='running' AND NEW.queue_state<>'leased') THEN
        terminal := 'cancelled';
    END IF;
    IF terminal<>'' THEN
        UPDATE async_media_limit_reservations
           SET terminal_kind=terminal,
               updated_at=GREATEST(updated_at,NEW.updated_at)
         WHERE job_id=NEW.job_id AND lease_fence=OLD.lease_fence
           AND terminal_kind='';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER async_media_execution_limit_terminal
AFTER UPDATE OF queue_state,execution_state,provider_outcome
ON async_media_jobs
FOR EACH ROW EXECUTE FUNCTION mark_async_media_execution_limit_terminal();

-- Request idempotency remains attributable to the physical secret version
-- that first claimed an operation, while the uniqueness lane for a
-- grant-bound credential is the stable logical family. The trigger also
-- normalizes an old writer that only supplies api_key_id during the one-release
-- compatibility window; a supplied family identity must match the exact key.
ALTER TABLE request_idempotency
    DROP CONSTRAINT request_idempotency_pkey,
    ADD COLUMN access_credential_id BIGINT,
    ADD COLUMN idempotency_subject_type VARCHAR(24),
    ADD COLUMN idempotency_subject_id BIGINT;

CREATE FUNCTION canonicalize_request_idempotency_subject()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    bound_credential_id BIGINT;
BEGIN
    SELECT access_credential_id INTO bound_credential_id
      FROM api_keys
     WHERE id=NEW.api_key_id AND org_id=NEW.org_id;
    IF NEW.access_credential_id IS NOT NULL
       AND NEW.access_credential_id IS DISTINCT FROM bound_credential_id THEN
        RAISE EXCEPTION 'request idempotency credential does not match API key';
    END IF;
    NEW.access_credential_id := COALESCE(NEW.access_credential_id, bound_credential_id);
    IF NEW.access_credential_id IS NULL THEN
        NEW.idempotency_subject_type := 'api_key';
        NEW.idempotency_subject_id := NEW.api_key_id;
    ELSE
        NEW.idempotency_subject_type := 'access_credential';
        NEW.idempotency_subject_id := NEW.access_credential_id;
    END IF;
    RETURN NEW;
END;
$$;

UPDATE request_idempotency receipt
   SET access_credential_id=key.access_credential_id,
       idempotency_subject_type=CASE WHEN key.access_credential_id IS NULL
           THEN 'api_key' ELSE 'access_credential' END,
       idempotency_subject_id=COALESCE(key.access_credential_id,key.id)
  FROM api_keys key
 WHERE key.id=receipt.api_key_id AND key.org_id=receipt.org_id;
UPDATE request_idempotency
   SET idempotency_subject_type='api_key',idempotency_subject_id=api_key_id
 WHERE idempotency_subject_id IS NULL;

ALTER TABLE request_idempotency
    ALTER COLUMN idempotency_subject_type SET NOT NULL,
    ALTER COLUMN idempotency_subject_id SET NOT NULL,
    ADD CONSTRAINT request_idempotency_subject_check CHECK (
        (idempotency_subject_type='api_key'
            AND access_credential_id IS NULL AND idempotency_subject_id=api_key_id)
        OR (idempotency_subject_type='access_credential'
            AND access_credential_id IS NOT NULL
            AND idempotency_subject_id=access_credential_id)
    ),
    ADD CONSTRAINT request_idempotency_pkey PRIMARY KEY (
        org_id,idempotency_subject_type,idempotency_subject_id,key_hash
    );

CREATE TRIGGER request_idempotency_subject_canonical
BEFORE INSERT OR UPDATE OF org_id,api_key_id,access_credential_id,
    idempotency_subject_type,idempotency_subject_id
ON request_idempotency
FOR EACH ROW EXECUTE FUNCTION canonicalize_request_idempotency_subject();

-- Replace the commercial active-key counter with a logical-seat counter for
-- grant-bound credentials. Legacy and purpose-scoped keys remain physical
-- seats. Rotation under an existing logical credential checks License expiry
-- but consumes no additional seat.
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
            resource_name := 'active_api_keys'; limit_column := 'active_api_keys';
        WHEN 'access_credentials' THEN
            resource_name := 'active_api_keys'; limit_column := 'active_api_keys';
        WHEN 'users' THEN
            resource_name := 'active_users'; limit_column := 'active_users';
        WHEN 'projects' THEN
            resource_name := 'active_projects'; limit_column := 'active_projects';
        WHEN 'channels' THEN
            resource_name := 'active_channels'; limit_column := 'active_channels';
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

    IF TG_TABLE_NAME='api_keys' THEN
        -- NEW is a polymorphic trigger record. Keep the api_keys-only field
        -- reference behind a procedural branch so the access_credentials
        -- trigger never attempts to resolve a field that table does not have.
        IF NEW.access_credential_id IS NOT NULL THEN
            RETURN NEW;
        END IF;
    END IF;
    IF resource_name='active_api_keys' THEN
        SELECT COUNT(*) INTO active_count FROM (
            SELECT ac.id::TEXT AS seat
              FROM access_credentials ac
             WHERE ac.status='active'
            UNION ALL
            SELECT ('legacy:' || legacy_key.id)::TEXT
              FROM api_keys legacy_key
             WHERE legacy_key.status='active' AND legacy_key.access_credential_id IS NULL
        ) seats;
    ELSE
        EXECUTE format('SELECT COUNT(*) FROM %I WHERE status = $1', TG_TABLE_NAME)
            INTO active_count USING 'active';
    END IF;
    IF active_count >= signed_limit THEN
        RAISE EXCEPTION 'commercial License limit exceeded for %: % active, limit %',
            resource_name, active_count, signed_limit
            USING ERRCODE = '23514', CONSTRAINT = constraint_name;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER access_credentials_commercial_limit
    BEFORE INSERT OR UPDATE OF status ON access_credentials
    FOR EACH ROW EXECUTE FUNCTION enforce_commercial_resource_limit();

-- Freeze evidence remains attributable to the physical secret version and,
-- for Grant-bound credentials, to the stable logical family and Grant. These
-- columns do not introduce a Grant budget ledger or a new financial decision
-- surface; wallet/project/finance remains authoritative.
ALTER TABLE freeze_idempotency
    ADD COLUMN api_key_id BIGINT,
    ADD COLUMN access_credential_id BIGINT,
    ADD COLUMN credential_ref VARCHAR(72),
    ADD COLUMN access_grant_id BIGINT,
    ADD CONSTRAINT freeze_idempotency_api_key_fk
        FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE RESTRICT,
    ADD CONSTRAINT freeze_idempotency_key_grant_binding_fk
        FOREIGN KEY (api_key_id, access_credential_id, credential_ref, access_grant_id, org_id, project_id)
        REFERENCES api_keys(id, access_credential_id, credential_ref, access_grant_id, org_id, project_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT freeze_idempotency_access_credential_scope_fk
        FOREIGN KEY (access_credential_id, credential_ref, access_grant_id, org_id, project_id)
        REFERENCES access_credentials(id, credential_ref, access_grant_id, org_id, project_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT freeze_idempotency_access_grant_scope_fk
        FOREIGN KEY (access_grant_id, org_id, project_id)
        REFERENCES access_grants(id, org_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT freeze_idempotency_access_binding_check CHECK (
        (access_grant_id IS NULL AND access_credential_id IS NULL AND credential_ref IS NULL)
        OR (access_grant_id IS NOT NULL AND access_credential_id IS NOT NULL
            AND credential_ref IS NOT NULL AND api_key_id IS NOT NULL)
    );

-- Provider liability is likewise version-attributed but family-contained.
-- The binding is copied from the already locked freeze journal, so a caller
-- cannot claim an unrelated family and a rotated successor cannot bypass an
-- unresolved predecessor attempt.
ALTER TABLE billing_provider_attempts
    ADD COLUMN project_id BIGINT,
    ADD COLUMN access_credential_id BIGINT,
    ADD COLUMN credential_ref VARCHAR(72),
    ADD COLUMN access_grant_id BIGINT;

CREATE FUNCTION canonicalize_provider_attempt_access_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    frozen freeze_idempotency%ROWTYPE;
BEGIN
    SELECT * INTO frozen
      FROM freeze_idempotency
     WHERE org_id=NEW.org_id AND request_id=NEW.request_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'provider attempt reservation is missing';
    END IF;
    IF frozen.api_key_id IS NOT NULL AND frozen.api_key_id <> NEW.api_key_id THEN
        RAISE EXCEPTION 'provider attempt API key does not match reservation';
    END IF;
    NEW.project_id := NULLIF(frozen.project_id,0);
    NEW.access_credential_id := frozen.access_credential_id;
    NEW.credential_ref := frozen.credential_ref;
    NEW.access_grant_id := frozen.access_grant_id;
    RETURN NEW;
END;
$$;

UPDATE billing_provider_attempts attempt
   SET project_id=NULLIF(frozen.project_id,0),
       access_credential_id=frozen.access_credential_id,
       credential_ref=frozen.credential_ref,
       access_grant_id=frozen.access_grant_id
  FROM freeze_idempotency frozen
 WHERE frozen.org_id=attempt.org_id AND frozen.request_id=attempt.request_id;

ALTER TABLE billing_provider_attempts
    ADD CONSTRAINT billing_provider_attempts_access_binding_check CHECK (
        (access_grant_id IS NULL AND access_credential_id IS NULL AND credential_ref IS NULL)
        OR (access_grant_id IS NOT NULL AND access_credential_id IS NOT NULL
            AND credential_ref IS NOT NULL AND project_id IS NOT NULL)
    ),
    ADD CONSTRAINT billing_provider_attempts_access_credential_scope_fk
        FOREIGN KEY (access_credential_id,credential_ref,access_grant_id,org_id,project_id)
        REFERENCES access_credentials(id,credential_ref,access_grant_id,org_id,project_id)
        ON DELETE RESTRICT;

CREATE TRIGGER billing_provider_attempts_access_binding_canonical
BEFORE INSERT OR UPDATE OF org_id,request_id,api_key_id,project_id,
    access_credential_id,credential_ref,access_grant_id
ON billing_provider_attempts
FOR EACH ROW EXECUTE FUNCTION canonicalize_provider_attempt_access_binding();

CREATE INDEX billing_provider_attempts_unresolved_credential_idx
    ON billing_provider_attempts(org_id,access_credential_id,updated_at,request_id)
    WHERE access_credential_id IS NOT NULL
      AND state IN (
          'outcome_unknown','response_unusable','funding_pending',
          'delivery_pending','delivery_failed','output_withheld'
      );

-- Key-specific model authorization for a new grant-bound key is persisted at
-- the stable logical credential subject. Legacy-unbound policies remain
-- physical api_key subjects.
ALTER TABLE model_call_authorizations
    DROP CONSTRAINT model_call_authorizations_subject_check,
    ADD CONSTRAINT model_call_authorizations_subject_check CHECK (
        subject_type IN ('api_key','access_credential','project','user','agent')
        AND (subject_type <> 'access_credential' OR subject_ref ~ '^[1-9][0-9]*$')
    );

ALTER TABLE distributed_limit_policies
    DROP CONSTRAINT distributed_limit_policy_selector_scope,
    ADD COLUMN access_credential_id BIGINT,
    ADD CONSTRAINT distributed_limit_policy_selector_scope CHECK (
        (project_id IS NULL OR org_id IS NOT NULL)
        AND (api_key_id IS NULL OR org_id IS NOT NULL)
        AND (access_credential_id IS NULL OR (org_id IS NOT NULL AND project_id IS NOT NULL))
        AND NOT (api_key_id IS NOT NULL AND access_credential_id IS NOT NULL)
    ),
    ADD CONSTRAINT distributed_limit_policy_access_credential_scope_fk
        FOREIGN KEY (access_credential_id,org_id,project_id)
        REFERENCES access_credentials(id,org_id,project_id) ON DELETE CASCADE;
CREATE INDEX distributed_limit_policy_access_credential_idx
    ON distributed_limit_policies(access_credential_id)
    WHERE access_credential_id IS NOT NULL;

ALTER TABLE guardrail_policies
    DROP CONSTRAINT guardrail_policy_selector_scope,
    DROP CONSTRAINT guardrail_policy_mandatory_authority,
    ADD COLUMN access_credential_id BIGINT,
    ADD CONSTRAINT guardrail_policy_selector_scope CHECK (
        (project_id IS NULL OR org_id IS NOT NULL)
        AND (api_key_id IS NULL OR org_id IS NOT NULL)
        AND (access_credential_id IS NULL OR (org_id IS NOT NULL AND project_id IS NOT NULL))
        AND NOT (api_key_id IS NOT NULL AND access_credential_id IS NOT NULL)
    ),
    ADD CONSTRAINT guardrail_policy_access_credential_scope_fk
        FOREIGN KEY (access_credential_id,org_id,project_id)
        REFERENCES access_credentials(id,org_id,project_id) ON DELETE CASCADE,
    ADD CONSTRAINT guardrail_policy_mandatory_authority CHECK (
        NOT mandatory
        OR (org_id IS NULL AND project_id IS NULL AND api_key_id IS NULL
            AND access_credential_id IS NULL
            AND mode='enforce' AND failure_mode='fail_closed')
    );
CREATE INDEX guardrail_policy_access_credential_idx
    ON guardrail_policies(access_credential_id)
    WHERE access_credential_id IS NOT NULL;

CREATE TABLE access_operations (
    id BIGSERIAL PRIMARY KEY,
    operation_ref VARCHAR(72) NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    project_id BIGINT NOT NULL,
    access_request_id BIGINT,
    access_grant_id BIGINT,
    actor_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    client_request_id VARCHAR(128) NOT NULL,
    operation_kind VARCHAR(48) NOT NULL,
    semantic_hash CHAR(64) NOT NULL,
    predecessor_key_id BIGINT REFERENCES api_keys(id) ON DELETE RESTRICT,
    successor_key_id BIGINT REFERENCES api_keys(id) ON DELETE RESTRICT,
    result_payload JSONB NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT access_operations_ref_unique UNIQUE (operation_ref),
    CONSTRAINT access_operations_idempotency_unique UNIQUE (org_id, actor_user_id, client_request_id),
    CONSTRAINT access_operations_project_scope_fk
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT access_operations_request_scope_fk
        FOREIGN KEY (access_request_id, org_id, project_id)
        REFERENCES access_requests(id, org_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT access_operations_grant_scope_fk
        FOREIGN KEY (access_grant_id, org_id, project_id)
        REFERENCES access_grants(id, org_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT access_operations_identity_check CHECK (
        operation_ref ~ '^op_[a-z0-9][a-z0-9_-]{7,68}$'
        AND length(operation_ref) BETWEEN 11 AND 72
        AND client_request_id=btrim(client_request_id)
        AND length(client_request_id) BETWEEN 1 AND 128
        AND semantic_hash ~ '^[0-9a-f]{64}$'
        AND operation_kind IN (
            'access_request_decide','access_request_cancel',
            'grant_suspend','grant_resume','grant_revoke',
            'credential_issue','credential_rotate','credential_revoke'
        )
        AND jsonb_typeof(result_payload)='object'
    )
);

CREATE INDEX access_operations_grant_idx
    ON access_operations (access_grant_id, created_at DESC, id DESC);

CREATE FUNCTION prevent_access_operation_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'access operations are append-only';
END;
$$;

CREATE TRIGGER access_operations_append_only
BEFORE UPDATE OR DELETE ON access_operations
FOR EACH ROW EXECUTE FUNCTION prevent_access_operation_mutation();

COMMIT;
