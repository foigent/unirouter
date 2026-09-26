-- unirouter:explicit-compatible-expand-v1
BEGIN;
-- Preserve the existing finite expansion owner's lock order. Busy financial
-- or authorization transactions yield the migration, never the accepted call.
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
    IF NOT pg_try_advisory_xact_lock(86420517831702492) THEN
        RAISE EXCEPTION 'source facts expansion owner is busy' USING ERRCODE='55P03';
    END IF;
    IF EXISTS (SELECT 1 FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest)
       WHERE g.state <> 'stopped' AND (g.ingress_state <> 'absent' OR g.lease_expires_at > clock_timestamp())
       AND (m.artifact_digest IS NULL OR NOT (convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000116_execution_source_facts')
        OR NOT (convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @> '[{"kind":"source_facts","id":"unirouter.source-fact.v1"},{"kind":"stream_counter","id":"unirouter.stream-counter.v2"},{"kind":"stream_counter","id":"unirouter.stream-counter.v3"}]'::jsonb)))
    THEN RAISE EXCEPTION 'all serving and recovery generations must support M116'; END IF;
END $$;
-- Acquire the existing foreign-key parent lock without waiting on its owner.
LOCK TABLE projects IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE api_keys, freeze_idempotency, async_media_jobs IN ACCESS EXCLUSIVE MODE NOWAIT;

ALTER TABLE api_keys DROP CONSTRAINT api_keys_completed_feed_purpose_check,
    ADD CONSTRAINT api_keys_completed_feed_purpose_check CHECK (purpose IN ('','xinsight.feed.read','xinsight.source.read'));
ALTER TABLE api_keys DROP CONSTRAINT api_keys_completed_feed_status_check,
    ADD CONSTRAINT api_keys_completed_feed_status_check CHECK (
        (purpose='' AND status NOT IN ('feed_active','feed_staged','feed_revoked')) OR
        (purpose IN ('xinsight.feed.read','xinsight.source.read') AND status IN ('feed_active','feed_staged','feed_revoked','disabled'))
    );
ALTER TABLE api_keys DROP CONSTRAINT api_keys_verifier_scheme_purpose_check,
    ADD CONSTRAINT api_keys_verifier_scheme_purpose_check CHECK (
        (purpose='' AND verifier_scheme='legacy_hmac_sha256_v1' AND registration_operation_id='') OR
        (purpose='xinsight.feed.read' AND (
            (verifier_scheme='legacy_hmac_sha256_v1' AND registration_operation_id='') OR
            (verifier_scheme='receiver_sha256_v1' AND registration_operation_id ~ '^[0-9a-f]{32}$'))) OR
        (purpose='xinsight.source.read' AND verifier_scheme='receiver_sha256_v1' AND registration_operation_id ~ '^[0-9a-f]{32}$')
    );
CREATE UNIQUE INDEX api_keys_one_active_xinsight_source ON api_keys(project_id)
    WHERE purpose='xinsight.source.read' AND status='feed_active';
CREATE UNIQUE INDEX api_keys_one_staged_xinsight_source ON api_keys(project_id)
    WHERE purpose='xinsight.source.read' AND status='feed_staged';
CREATE UNIQUE INDEX api_keys_receiver_source_operation ON api_keys(project_id,registration_operation_id)
    WHERE purpose='xinsight.source.read';

-- These small snapshots belong to the original durable reservation/job. An
-- unresolved provider hold must outlive the independently retained source page.
ALTER TABLE freeze_idempotency ADD COLUMN source_context BYTEA,
    ADD COLUMN source_cost_basis BYTEA,
    ADD CONSTRAINT freeze_source_context_size CHECK (source_context IS NULL OR octet_length(source_context) BETWEEN 2 AND 32768),
    ADD CONSTRAINT freeze_source_basis_size CHECK (source_cost_basis IS NULL OR octet_length(source_cost_basis) BETWEEN 2 AND 32768);
ALTER TABLE async_media_jobs ADD COLUMN source_context BYTEA,
    ADD COLUMN source_dispatch_context BYTEA,
    ADD CONSTRAINT media_source_context_size CHECK (source_context IS NULL OR octet_length(source_context) BETWEEN 2 AND 32768),
    ADD CONSTRAINT media_source_dispatch_context_size CHECK (source_dispatch_context IS NULL OR octet_length(source_dispatch_context) BETWEEN 2 AND 32768);

CREATE TABLE execution_source_streams (
    source_instance VARCHAR(128) NOT NULL,
    org_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
    profile VARCHAR(96) NOT NULL CHECK (profile IN ('unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1')),
    source_epoch BIGINT NOT NULL DEFAULT 1 CHECK (source_epoch > 0),
    high_sequence BIGINT NOT NULL DEFAULT 0,
    retained_after_sequence BIGINT NOT NULL DEFAULT 0,
    capture_started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(source_instance,org_id,project_id,profile),
    FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE RESTRICT,
    CHECK (retained_after_sequence >= 0 AND high_sequence >= retained_after_sequence)
);
CREATE TABLE execution_source_facts (
    source_instance VARCHAR(128) NOT NULL,
    org_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
    profile VARCHAR(96) NOT NULL,
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    event_id VARCHAR(68) NOT NULL CHECK (event_id ~ '^uf1_[a-f0-9]{64}$'),
    source_fact_id VARCHAR(68) NOT NULL CHECK (source_fact_id ~ '^uf1_[a-f0-9]{64}$'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    request_id VARCHAR(64) NOT NULL,
    stage VARCHAR(32) NOT NULL,
    document BYTEA NOT NULL CHECK (octet_length(document) BETWEEN 2 AND 65536),
    source_hash VARCHAR(71) NOT NULL CHECK (source_hash ~ '^sha256:[a-f0-9]{64}$'),
    published_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY(source_instance,org_id,project_id,profile,sequence),
    UNIQUE(event_id), UNIQUE(source_fact_id,revision),
    FOREIGN KEY(source_instance,org_id,project_id,profile)
      REFERENCES execution_source_streams(source_instance,org_id,project_id,profile) ON DELETE RESTRICT
);
CREATE INDEX execution_source_facts_retention ON execution_source_facts(published_at);
CREATE FUNCTION execution_source_facts_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'execution source facts are immutable';
END;
$$;
CREATE TRIGGER execution_source_facts_immutable BEFORE UPDATE ON execution_source_facts
    FOR EACH ROW EXECUTE FUNCTION execution_source_facts_immutable();

CREATE TABLE execution_source_connections (
    org_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
    source_instance VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'paused' CHECK (status IN ('active','paused','revoked')),
    scope_generation BIGINT NOT NULL DEFAULT 1 CHECK (scope_generation > 0),
    profiles TEXT[] NOT NULL CHECK (cardinality(profiles) BETWEEN 1 AND 3 AND
      profiles <@ ARRAY['unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1']::text[]),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(org_id,project_id),
    FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE RESTRICT
);
CREATE TABLE execution_source_credentials (
    api_key_id BIGINT PRIMARY KEY REFERENCES api_keys(id) ON DELETE RESTRICT,
    org_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
    registration_sha256 VARCHAR(71) NOT NULL CHECK (registration_sha256 ~ '^sha256:[a-f0-9]{64}$'),
    profiles TEXT[] NOT NULL,
    FOREIGN KEY(org_id,project_id) REFERENCES execution_source_connections(org_id,project_id) ON DELETE RESTRICT,
    CHECK (cardinality(profiles) BETWEEN 1 AND 3 AND
      profiles <@ ARRAY['unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1']::text[])
);
CREATE TABLE execution_source_acknowledgements (
    org_id BIGINT NOT NULL, project_id BIGINT NOT NULL, profile VARCHAR(96) NOT NULL,
    scope_generation BIGINT NOT NULL, acknowledged_sequence BIGINT NOT NULL CHECK (acknowledged_sequence >= 0),
    last_authenticated_pull_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(org_id,project_id,profile),
    FOREIGN KEY(org_id,project_id) REFERENCES execution_source_connections(org_id,project_id) ON DELETE RESTRICT
);

-- Replace the marker only after all dependent DDL and parent locks.
-- Keep every M36 trigger and the original first-evidence rollback boundary.
LOCK TABLE micro_ledger_runtime_state IN ACCESS EXCLUSIVE MODE NOWAIT;
CREATE OR REPLACE FUNCTION mark_micro_ledger_runtime_evidence()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    UPDATE micro_ledger_runtime_state
    SET micro_evidence_written = TRUE,
        first_evidence_at = COALESCE(first_evidence_at, NOW())
    WHERE singleton = TRUE AND (NOT micro_evidence_written OR first_evidence_at IS NULL);
    RETURN NULL;
END;
$$;

COMMIT;
