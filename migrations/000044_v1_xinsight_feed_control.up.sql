BEGIN;

ALTER TABLE api_keys
    DROP CONSTRAINT api_keys_completed_feed_status_check,
    ADD CONSTRAINT api_keys_completed_feed_status_check CHECK (
        (purpose = '' AND status NOT IN ('feed_active', 'feed_staged', 'feed_revoked'))
        OR (
            purpose = 'xinsight.feed.read'
            AND status IN ('feed_active', 'feed_staged', 'feed_revoked', 'disabled')
        )
    );

CREATE UNIQUE INDEX api_keys_one_staged_xinsight_feed_per_project
    ON api_keys (project_id)
    WHERE status = 'feed_staged' AND purpose = 'xinsight.feed.read';

CREATE TABLE completed_execution_feed_connections (
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'paused',
    acknowledged_sequence BIGINT NOT NULL DEFAULT 0,
    last_authenticated_pull_at TIMESTAMPTZ,
    last_authenticated_key_id BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, project_id),
    CONSTRAINT completed_execution_feed_connection_project_scope
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE CASCADE,
    CONSTRAINT completed_execution_feed_connection_status
        CHECK (status IN ('active', 'paused')),
    CONSTRAINT completed_execution_feed_connection_ack
        CHECK (acknowledged_sequence >= 0)
);

INSERT INTO completed_execution_feed_connections (org_id, project_id, status)
SELECT DISTINCT org_id, project_id, 'paused'
FROM api_keys
WHERE purpose = 'xinsight.feed.read'
  AND project_id IS NOT NULL
ON CONFLICT (org_id, project_id) DO NOTHING;

COMMIT;
