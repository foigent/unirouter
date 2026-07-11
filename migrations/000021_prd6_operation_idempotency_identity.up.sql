BEGIN;

-- Scheme A idempotency stores only bounded identity metadata and keyed
-- digests. It never stores the prompt or response body.
CREATE TABLE IF NOT EXISTS request_idempotency (
    org_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    key_hash VARCHAR(64) NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    operation_id VARCHAR(64) NOT NULL,
    client_request_id VARCHAR(128),
    state VARCHAR(20) NOT NULL DEFAULT 'in_progress',
    original_http_status INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (org_id, api_key_id, key_hash),
    CONSTRAINT request_idempotency_operation_unique UNIQUE (operation_id),
    CONSTRAINT request_idempotency_key_hash_check CHECK (length(key_hash) = 64),
    CONSTRAINT request_idempotency_fingerprint_check CHECK (length(request_fingerprint) = 64),
    CONSTRAINT request_idempotency_state_check CHECK (state IN ('in_progress', 'completed', 'failed')),
    CONSTRAINT request_idempotency_http_status_check CHECK (
        original_http_status IS NULL OR original_http_status BETWEEN 100 AND 599
    ),
    CONSTRAINT request_idempotency_expiry_check CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS idx_request_idempotency_expires
    ON request_idempotency(expires_at);

-- Financial operations use server operation IDs, but the database still
-- scopes every lookup and uniqueness rule by tenant as defense in depth.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'freeze_idempotency'::regclass
          AND conname = 'freeze_idempotency_pkey'
    ) THEN
        ALTER TABLE freeze_idempotency DROP CONSTRAINT freeze_idempotency_pkey;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'freeze_idempotency'::regclass
          AND conname = 'freeze_idempotency_org_request_pkey'
    ) THEN
        ALTER TABLE freeze_idempotency
            ADD CONSTRAINT freeze_idempotency_org_request_pkey PRIMARY KEY (org_id, request_id);
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_freeze_idempotency_request_id
    ON freeze_idempotency(request_id);

ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS client_request_id VARCHAR(128);

ALTER TABLE audit_logs
    ADD COLUMN IF NOT EXISTS client_request_id VARCHAR(128);

CREATE UNIQUE INDEX IF NOT EXISTS idx_transactions_org_type_client_request
    ON transactions(org_id, type, client_request_id)
    WHERE client_request_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_audit_logs_client_request
    ON audit_logs(org_id, client_request_id, created_at)
    WHERE client_request_id IS NOT NULL;

-- Migration 000006 used a global client_request_id uniqueness rule. Client
-- idempotency belongs to a tenant scope, so identical values in different
-- organizations must not collide.
DROP INDEX IF EXISTS idx_grants_client_request_id;
ALTER TABLE grants
    ALTER COLUMN client_request_id TYPE VARCHAR(128);
CREATE UNIQUE INDEX IF NOT EXISTS idx_grants_org_client_request
    ON grants(org_id, client_request_id)
    WHERE client_request_id IS NOT NULL;

ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS client_request_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS idempotency_key_hash VARCHAR(64),
    ADD COLUMN IF NOT EXISTS workflow_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS agent_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS attempt_id VARCHAR(64);

ALTER TABLE request_trace_events
    ADD COLUMN IF NOT EXISTS client_request_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS idempotency_key_hash VARCHAR(64),
    ADD COLUMN IF NOT EXISTS workflow_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS agent_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS attempt_id VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_call_logs_client_request
    ON call_logs(org_id, client_request_id, created_at)
    WHERE client_request_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_call_logs_idempotency
    ON call_logs(org_id, api_key_id, idempotency_key_hash)
    WHERE idempotency_key_hash IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_call_logs_workflow
    ON call_logs(org_id, workflow_id, created_at)
    WHERE workflow_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_request_trace_events_workflow
    ON request_trace_events(org_id, workflow_id, created_at, id)
    WHERE workflow_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_request_trace_events_agent
    ON request_trace_events(org_id, agent_id, created_at, id)
    WHERE agent_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_request_trace_events_attempt
    ON request_trace_events(attempt_id, created_at, id)
    WHERE attempt_id IS NOT NULL;

COMMIT;
