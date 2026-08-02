BEGIN;

-- A composite key lets every new workload reference a project from the same
-- organization without relying on an application-layer tenant check.
CREATE UNIQUE INDEX IF NOT EXISTS projects_id_org_unique_idx
    ON projects (id, org_id);

CREATE TABLE workload_applications (
    id BIGSERIAL PRIMARY KEY,
    contract_version VARCHAR(64) NOT NULL DEFAULT 'unirouter.workload-application.v1',
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL,
    application_ref VARCHAR(64) NOT NULL,
    display_name VARCHAR(160) NOT NULL,
    kind VARCHAR(32) NOT NULL,
    environment VARCHAR(32) NOT NULL,
    identity_mode VARCHAR(40) NOT NULL,
    client_profile VARCHAR(120) NOT NULL,
    owner_team VARCHAR(160) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    revision BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    updated_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ(6),
    CONSTRAINT workload_applications_identity_unique UNIQUE (org_id, application_ref),
    CONSTRAINT workload_applications_id_org_unique UNIQUE (id, org_id),
    CONSTRAINT workload_applications_project_scope_fk
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT workload_applications_contract_check CHECK (
        contract_version = 'unirouter.workload-application.v1'
    ),
    CONSTRAINT workload_applications_identity_check CHECK (
        application_ref ~ '^app_[a-z0-9][a-z0-9._-]{2,59}$'
        AND length(btrim(display_name)) BETWEEN 1 AND 160
        AND environment ~ '^[a-z][a-z0-9_-]{0,31}$'
        AND length(btrim(client_profile)) BETWEEN 1 AND 120
        AND length(owner_team) <= 160
        AND length(description) <= 1000
        AND revision > 0
    ),
    CONSTRAINT workload_applications_kind_check CHECK (
        kind IN ('service', 'agent', 'workflow', 'interactive_client', 'integration_adapter')
    ),
    CONSTRAINT workload_applications_identity_mode_check CHECK (
        identity_mode IN ('service_credential', 'personal_development')
        AND (kind <> 'integration_adapter' OR identity_mode = 'service_credential')
    ),
    CONSTRAINT workload_applications_status_check CHECK (
        status IN ('active', 'disabled', 'archived')
    ),
    CONSTRAINT workload_applications_lifecycle_check CHECK (
        (status IN ('active', 'disabled') AND archived_at IS NULL)
        OR (status = 'archived' AND archived_at IS NOT NULL)
    )
);

CREATE INDEX workload_applications_list_idx
    ON workload_applications (org_id, updated_at DESC, id DESC);
CREATE INDEX workload_applications_project_idx
    ON workload_applications (project_id, status, id);
CREATE INDEX workload_applications_client_profile_idx
    ON workload_applications (client_profile, status, id);

ALTER TABLE api_keys
    ADD COLUMN workload_application_id BIGINT;

ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_workload_application_scope_fk
        FOREIGN KEY (workload_application_id, org_id)
        REFERENCES workload_applications(id, org_id) ON DELETE RESTRICT;

CREATE INDEX api_keys_workload_application_idx
    ON api_keys (workload_application_id, status, id)
    WHERE workload_application_id IS NOT NULL;

COMMIT;
