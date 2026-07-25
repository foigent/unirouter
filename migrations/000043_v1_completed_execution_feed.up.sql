BEGIN;

-- The feed purpose is an authorization capability, not a limit-policy label.
-- Existing keys remain general gateway keys because the empty purpose is the
-- only default.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS purpose VARCHAR(64) NOT NULL DEFAULT '';

ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_completed_feed_purpose_check CHECK (
        purpose IN ('', 'xinsight.feed.read')
    ),
    ADD CONSTRAINT api_keys_completed_feed_status_check CHECK (
        (purpose = '' AND status <> 'feed_active')
        OR (
            purpose = 'xinsight.feed.read'
            AND status IN ('feed_active', 'disabled')
        )
    ),
    ADD CONSTRAINT api_keys_completed_feed_shape_check CHECK (
        purpose = ''
        OR (
            key_type = 'service'
            AND project_id IS NOT NULL
            AND btrim(allowed_ips) <> ''
            AND btrim(allowed_models) = ''
        )
    );

CREATE UNIQUE INDEX api_keys_one_active_xinsight_feed_per_project
    ON api_keys (project_id)
    WHERE status = 'feed_active' AND purpose = 'xinsight.feed.read';

-- P6-F10 introduced the composite project uniqueness as a guardrail-owned
-- index and its controlled down migration drops that exact name. Transfer the
-- existing index to a feed-owned stable identity before creating the feed
-- foreign keys, then recreate the guardrail-owned name independently. This
-- keeps either migration rollback from dropping the other's uniqueness basis.
ALTER INDEX idx_projects_guardrail_scope
    RENAME TO completed_execution_feed_projects_scope_key;

-- Successful internal-outbox rows are deleted. This independent retained
-- sequence is therefore the only cursor authority exposed to X-Insight.
CREATE TABLE completed_execution_feed_items (
    id BIGSERIAL PRIMARY KEY,
    event_key VARCHAR(200) NOT NULL UNIQUE,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    terminal_status VARCHAR(16) NOT NULL,
    terminal_available_at TIMESTAMPTZ NOT NULL,
    revision VARCHAR(20) NOT NULL DEFAULT '1',
    snapshot BYTEA NOT NULL,
    snapshot_sha256 VARCHAR(71) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT completed_execution_feed_project_scope
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE CASCADE,
    CONSTRAINT completed_execution_feed_terminal_status
        CHECK (terminal_status IN ('settled', 'cancelled')),
    CONSTRAINT completed_execution_feed_revision
        CHECK (revision = '1'),
    CONSTRAINT completed_execution_feed_snapshot_size
        CHECK (octet_length(snapshot) BETWEEN 2 AND 65536),
    CONSTRAINT completed_execution_feed_snapshot_hash
        CHECK (snapshot_sha256 ~ '^sha256:[a-f0-9]{64}$'),
    CONSTRAINT completed_execution_feed_request_unique
        UNIQUE (org_id, request_id)
);

CREATE INDEX completed_execution_feed_project_cursor
    ON completed_execution_feed_items (org_id, project_id, id);
CREATE INDEX completed_execution_feed_retention
    ON completed_execution_feed_items (created_at, org_id, project_id, id);

-- Deletion advances a durable floor. It is never inferred from the minimum
-- currently retained BIGSERIAL because global sequence gaps may belong to a
-- different project.
CREATE TABLE completed_execution_feed_states (
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL,
    retained_after_id BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, project_id),
    CONSTRAINT completed_execution_feed_state_project_scope
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE CASCADE,
    CONSTRAINT completed_execution_feed_state_floor
        CHECK (retained_after_id >= 0)
);

CREATE UNIQUE INDEX idx_projects_guardrail_scope
    ON projects (id, org_id);

COMMIT;
