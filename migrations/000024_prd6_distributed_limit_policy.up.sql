BEGIN;

-- P6-F07 keeps quota labels separate from provider/channel tags. Labels are
-- server-owned API-key attributes and are never accepted from gateway headers.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS policy_labels TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS distributed_limit_policies (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT REFERENCES orgs(id) ON DELETE CASCADE,
    project_id BIGINT REFERENCES projects(id) ON DELETE CASCADE,
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    model VARCHAR(200) NOT NULL DEFAULT '',
    policy_label VARCHAR(64) NOT NULL DEFAULT '',
    mode VARCHAR(16) NOT NULL DEFAULT 'shadow'
        CHECK (mode IN ('disabled', 'shadow', 'enforce')),
    failure_mode VARCHAR(24) NOT NULL DEFAULT 'fail_closed'
        CHECK (failure_mode IN ('fail_closed', 'last_known_safe')),
    rps_limit INTEGER,
    rpm_limit INTEGER,
    tpm_limit BIGINT,
    concurrency_limit INTEGER,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT distributed_limit_policy_name_nonempty
        CHECK (length(btrim(name)) > 0),
    CONSTRAINT distributed_limit_policy_selector_scope
        CHECK ((project_id IS NULL OR org_id IS NOT NULL)
           AND (api_key_id IS NULL OR org_id IS NOT NULL)),
    CONSTRAINT distributed_limit_policy_label_format
        CHECK (policy_label = '' OR policy_label ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'),
    CONSTRAINT distributed_limit_policy_has_limit
        CHECK (rps_limit IS NOT NULL OR rpm_limit IS NOT NULL OR
               tpm_limit IS NOT NULL OR concurrency_limit IS NOT NULL),
    CONSTRAINT distributed_limit_policy_positive_limits
        CHECK ((rps_limit IS NULL OR rps_limit > 0) AND
               (rpm_limit IS NULL OR rpm_limit > 0) AND
               (tpm_limit IS NULL OR tpm_limit > 0) AND
               (concurrency_limit IS NULL OR concurrency_limit > 0))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_distributed_limit_policy_scope_name
    ON distributed_limit_policies (COALESCE(org_id, 0), lower(name));
CREATE INDEX IF NOT EXISTS idx_distributed_limit_policy_org
    ON distributed_limit_policies (org_id, mode, id);
CREATE INDEX IF NOT EXISTS idx_distributed_limit_policy_project
    ON distributed_limit_policies (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_distributed_limit_policy_api_key
    ON distributed_limit_policies (api_key_id) WHERE api_key_id IS NOT NULL;

COMMIT;
