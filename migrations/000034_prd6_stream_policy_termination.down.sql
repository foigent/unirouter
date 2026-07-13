BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM guardrail_policies WHERE stage = 'stream_output')
       OR EXISTS (SELECT 1 FROM billing_reservation_extensions)
       OR EXISTS (SELECT 1 FROM stream_delivery_checkpoints)
       OR EXISTS (SELECT 1 FROM stream_delivery_leases) THEN
        RAISE EXCEPTION 'P6-F11 rollback refused: stream policy or financial evidence exists';
    END IF;
END;
$$;

ALTER TABLE request_delivery_receipts
    DROP CONSTRAINT request_delivery_receipts_outcome_counts_check;
ALTER TABLE request_delivery_receipts
    ADD CONSTRAINT request_delivery_receipts_outcome_counts_check CHECK (
        outcome IN ('write_started', 'write_failed')
        OR (delivered_bytes > 0 AND delivered_chunks > 0)
    );

DROP TRIGGER stream_delivery_checkpoint_append_only ON stream_delivery_checkpoints;
DROP FUNCTION reject_stream_delivery_checkpoint_mutation();
DROP TRIGGER stream_delivery_checkpoint_transition ON stream_delivery_checkpoints;
DROP FUNCTION enforce_stream_delivery_checkpoint_transition();
DROP INDEX idx_stream_delivery_checkpoints_latest;
DROP TABLE stream_delivery_checkpoints;
DROP INDEX idx_stream_delivery_leases_recovery;
DROP TABLE stream_delivery_leases;
DROP TRIGGER billing_reservation_extension_append_only ON billing_reservation_extensions;
DROP FUNCTION reject_billing_reservation_extension_mutation();
DROP TRIGGER billing_reservation_extension_transition ON billing_reservation_extensions;
DROP FUNCTION enforce_billing_reservation_extension_transition();
DROP TABLE billing_reservation_extensions;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_extension_sequence_nonnegative,
    DROP CONSTRAINT freeze_idempotency_reserved_completion_nonnegative,
    DROP COLUMN reservation_extension_sequence,
    DROP COLUMN reserved_completion_tokens;

ALTER TABLE guardrail_policies
    DROP CONSTRAINT guardrail_policies_stage_check;
ALTER TABLE guardrail_policies
    ADD CONSTRAINT guardrail_policies_stage_check
        CHECK (stage IN ('input', 'nonstream_output'));

DELETE FROM schema_migrations
WHERE version = '000034_prd6_stream_policy_termination';

COMMIT;
