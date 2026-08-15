BEGIN;

-- Latest real-provider connectivity evidence for one public model on one
-- provider connection. This is an operational projection cache, not an audit
-- log: channel.test audit events remain the immutable history.
CREATE TABLE channel_model_probes (
    channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    public_model VARCHAR(160) NOT NULL,
    upstream_model VARCHAR(200) NOT NULL,
    routing_epoch BIGINT NOT NULL,
    credential_id BIGINT REFERENCES channel_keys(id) ON DELETE SET NULL,
    credential_state_epoch BIGINT NOT NULL,
    success BOOLEAN NOT NULL,
    status_code INTEGER NOT NULL DEFAULT 0,
    latency_ms BIGINT NOT NULL DEFAULT 0,
    error_summary TEXT NOT NULL DEFAULT '',
    tested_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    tested_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, public_model),
    CONSTRAINT channel_model_probes_identity_check CHECK (
        BTRIM(public_model) <> ''
        AND BTRIM(upstream_model) <> ''
        AND routing_epoch > 0
        AND credential_state_epoch > 0
    ),
    CONSTRAINT channel_model_probes_result_check CHECK (
        status_code BETWEEN 0 AND 599
        AND latency_ms >= 0
        AND LENGTH(error_summary) <= 512
    )
);

CREATE INDEX channel_model_probes_tested_at_idx
    ON channel_model_probes (tested_at DESC);

COMMENT ON TABLE channel_model_probes IS
    'Latest real upstream probe per provider connection and public model; immutable history remains in audit_logs.';

COMMIT;
