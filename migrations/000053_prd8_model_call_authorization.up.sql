BEGIN;

CREATE TABLE model_call_authorizations (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    authorization_ref VARCHAR(96) NOT NULL,
    subject_type VARCHAR(24) NOT NULL,
    subject_ref VARCHAR(160) NOT NULL,
    environment VARCHAR(32) NOT NULL DEFAULT '*',
    effect VARCHAR(16) NOT NULL,
    allowed_models JSONB NOT NULL,
    allowed_endpoints JSONB NOT NULL,
    not_before TIMESTAMPTZ(6),
    expires_at TIMESTAMPTZ(6),
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    revision BIGINT NOT NULL DEFAULT 1,
    reason TEXT NOT NULL,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    activated_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    revoked_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    activated_at TIMESTAMPTZ(6),
    revoked_at TIMESTAMPTZ(6),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT model_call_authorizations_identity_unique UNIQUE (org_id, authorization_ref),
    CONSTRAINT model_call_authorizations_identity_check CHECK (
        authorization_ref ~ '^mca_[a-z0-9][a-z0-9._-]{2,79}$'
        AND subject_ref <> ''
        AND reason <> ''
        AND revision > 0
    ),
    CONSTRAINT model_call_authorizations_subject_check CHECK (
        subject_type IN ('api_key', 'project', 'user', 'agent')
    ),
    CONSTRAINT model_call_authorizations_environment_check CHECK (
        environment = '*' OR environment ~ '^[a-z][a-z0-9_-]{0,31}$'
    ),
    CONSTRAINT model_call_authorizations_effect_check CHECK (effect IN ('allow', 'deny')),
    CONSTRAINT model_call_authorizations_models_check CHECK (
        jsonb_typeof(allowed_models) = 'array' AND jsonb_array_length(allowed_models) BETWEEN 1 AND 128
    ),
    CONSTRAINT model_call_authorizations_endpoints_check CHECK (
        jsonb_typeof(allowed_endpoints) = 'array' AND jsonb_array_length(allowed_endpoints) BETWEEN 1 AND 8
    ),
    CONSTRAINT model_call_authorizations_time_check CHECK (
        expires_at IS NULL OR not_before IS NULL OR expires_at > not_before
    ),
    CONSTRAINT model_call_authorizations_status_check CHECK (status IN ('draft', 'active', 'revoked')),
    CONSTRAINT model_call_authorizations_lifecycle_check CHECK (
        (status = 'draft' AND activated_by IS NULL AND revoked_by IS NULL AND activated_at IS NULL AND revoked_at IS NULL)
        OR (status = 'active' AND activated_by IS NOT NULL AND activated_at IS NOT NULL AND revoked_by IS NULL AND revoked_at IS NULL)
        OR (status = 'revoked' AND revoked_by IS NOT NULL AND revoked_at IS NOT NULL)
    )
);

CREATE INDEX model_call_authorizations_active_lookup_idx
    ON model_call_authorizations (org_id, status, subject_type, subject_ref, environment, id)
    WHERE status = 'active';
CREATE INDEX model_call_authorizations_list_idx
    ON model_call_authorizations (org_id, updated_at DESC, id DESC);

COMMIT;
