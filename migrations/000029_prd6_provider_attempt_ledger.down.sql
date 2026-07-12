BEGIN;

-- Provider-attempt rows and their reservation markers are financial evidence.
-- Refuse a destructive rollback once the new authority has been exercised.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM billing_provider_attempts)
       OR EXISTS (
           SELECT 1
           FROM freeze_idempotency
           WHERE settlement_pending_reason IN (
               'ambiguous_attempt',
               'provider_response_unusable',
               'provider_attempt_unresolved'
           )
       ) THEN
        RAISE EXCEPTION 'P6-F40B down refused: provider-attempt evidence exists; reconcile rows and roll forward instead';
    END IF;
END;
$$;

DROP INDEX IF EXISTS idx_billing_provider_attempts_unresolved_channel;
DROP INDEX IF EXISTS idx_billing_provider_attempts_unresolved_key;
DROP INDEX IF EXISTS idx_billing_provider_attempts_request;
DROP TABLE billing_provider_attempts;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT IF EXISTS freeze_idempotency_pending_reason_valid,
    ADD CONSTRAINT freeze_idempotency_pending_reason_valid
        CHECK (settlement_pending_reason IN ('', 'billable_output', 'insufficient_topup'));

DROP INDEX IF EXISTS idx_freeze_idempotency_settlement_recovery;
CREATE INDEX idx_freeze_idempotency_settlement_recovery
    ON freeze_idempotency(settlement_next_attempt_at, settlement_payload_ready_at, created_at)
    WHERE status = 'settlement_pending'
      AND settlement_payload_ready_at IS NOT NULL
      AND settlement_pending_reason <> 'insufficient_topup';

DELETE FROM schema_migrations
WHERE version = '000029_prd6_provider_attempt_ledger';

COMMIT;
