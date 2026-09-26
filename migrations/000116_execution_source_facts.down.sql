BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
    IF NOT pg_try_advisory_xact_lock(86420517831702492) THEN
        RAISE EXCEPTION 'source facts expansion owner is busy' USING ERRCODE='55P03';
    END IF;
END $$;
-- Acquire the existing foreign-key parent lock without waiting on its owner.
LOCK TABLE projects IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE api_keys, freeze_idempotency, async_media_jobs,
    execution_source_streams, execution_source_facts, execution_source_connections,
    execution_source_credentials, execution_source_acknowledgements
    IN ACCESS EXCLUSIVE MODE NOWAIT;
-- Data-bearing candidates cannot be rolled back by erasing the evidence or
-- silently widening an existing key. Recovery uses the bridge/newer binary.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM runtime_generations WHERE state<>'stopped' AND (ingress_state<>'absent' OR lease_expires_at>clock_timestamp())) OR
       EXISTS (SELECT 1 FROM execution_source_streams) OR
       EXISTS (SELECT 1 FROM execution_source_connections) OR
       EXISTS (SELECT 1 FROM api_keys WHERE purpose='xinsight.source.read') OR
       EXISTS (SELECT 1 FROM freeze_idempotency WHERE source_context IS NOT NULL OR source_cost_basis IS NOT NULL) OR
       EXISTS (SELECT 1 FROM async_media_jobs WHERE source_context IS NOT NULL OR source_dispatch_context IS NOT NULL)
    THEN RAISE EXCEPTION 'M116 source evidence or credentials exist; unsafe down refused'; END IF;
END $$;
DROP TABLE execution_source_acknowledgements;
DROP TABLE execution_source_credentials;
DROP TABLE execution_source_connections;
DROP TABLE execution_source_facts;
DROP FUNCTION execution_source_facts_immutable();
DROP TABLE execution_source_streams;
ALTER TABLE freeze_idempotency DROP COLUMN source_context, DROP COLUMN source_cost_basis;
ALTER TABLE async_media_jobs DROP COLUMN source_context, DROP COLUMN source_dispatch_context;
DROP INDEX api_keys_one_active_xinsight_source;
DROP INDEX api_keys_one_staged_xinsight_source;
DROP INDEX api_keys_receiver_source_operation;
ALTER TABLE api_keys DROP CONSTRAINT api_keys_completed_feed_purpose_check,
    ADD CONSTRAINT api_keys_completed_feed_purpose_check CHECK (purpose IN ('','xinsight.feed.read'));
ALTER TABLE api_keys DROP CONSTRAINT api_keys_completed_feed_status_check,
    ADD CONSTRAINT api_keys_completed_feed_status_check CHECK (
        (purpose='' AND status NOT IN ('feed_active','feed_staged','feed_revoked')) OR
        (purpose='xinsight.feed.read' AND status IN ('feed_active','feed_staged','feed_revoked','disabled')));
ALTER TABLE api_keys DROP CONSTRAINT api_keys_verifier_scheme_purpose_check,
    ADD CONSTRAINT api_keys_verifier_scheme_purpose_check CHECK (
        (purpose='' AND verifier_scheme='legacy_hmac_sha256_v1' AND registration_operation_id='') OR
        (purpose='xinsight.feed.read' AND (
            (verifier_scheme='legacy_hmac_sha256_v1' AND registration_operation_id='') OR
            (verifier_scheme='receiver_sha256_v1' AND registration_operation_id ~ '^[0-9a-f]{32}$'))));

-- Restore the predecessor function only after guarded down and its DDL.
-- Keep every M36 trigger and the original first-evidence rollback boundary.
LOCK TABLE micro_ledger_runtime_state IN ACCESS EXCLUSIVE MODE NOWAIT;
CREATE OR REPLACE FUNCTION mark_micro_ledger_runtime_evidence()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    UPDATE micro_ledger_runtime_state
    SET micro_evidence_written = TRUE,
        first_evidence_at = COALESCE(first_evidence_at, NOW())
    WHERE singleton = TRUE;
    RETURN NULL;
END;
$$;

DELETE FROM schema_migrations WHERE version='000116_execution_source_facts';
COMMIT;
