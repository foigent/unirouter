BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM provider_cost_reconciliations) THEN
        RAISE EXCEPTION 'P6-F40C down refused: provider cost reconciliation evidence exists; roll forward instead';
    END IF;
    IF EXISTS (SELECT 1 FROM billing_provider_attempts WHERE state = 'reconciled') THEN
        RAISE EXCEPTION 'P6-F40C down refused: reconciled provider attempts exist; roll forward instead';
    END IF;
    IF EXISTS (
        SELECT 1 FROM freeze_idempotency
        WHERE settlement_payload_ready_at IS NOT NULL
          AND output_started_at IS NULL
          AND provider_cost_pending_at IS NULL
    ) THEN
        RAISE EXCEPTION 'P6-F40C down refused: a terminal payload depends on the P6-F40C financial marker rule; roll forward instead';
    END IF;
END $$;

DROP INDEX IF EXISTS idx_provider_cost_reconciliations_org_created;
DROP INDEX IF EXISTS idx_provider_cost_reconciliations_one_request;
DROP TABLE provider_cost_reconciliations;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_payload_requires_financial_marker;

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_payload_requires_financial_marker
        CHECK (
            settlement_payload_ready_at IS NULL
            OR output_started_at IS NOT NULL
            OR provider_cost_pending_at IS NOT NULL
        );

ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_state_check,
    DROP CONSTRAINT billing_provider_attempts_state_certainty_check;

ALTER TABLE billing_provider_attempts
    ADD CONSTRAINT billing_provider_attempts_state_check
        CHECK (state IN (
            'dispatch_started', 'rejected', 'response_accepted', 'outcome_unknown',
            'response_unusable', 'funding_pending', 'settled'
        )),
    ADD CONSTRAINT billing_provider_attempts_state_certainty_check
        CHECK (
            (state IN ('dispatch_started', 'response_accepted') AND outcome_certainty = 'pending')
            OR (state = 'rejected' AND outcome_certainty = 'known_rejected')
            OR (state IN ('outcome_unknown', 'response_unusable') AND outcome_certainty = 'unknown')
            OR (state IN ('funding_pending', 'settled') AND outcome_certainty = 'known_usage')
        );

DELETE FROM schema_migrations
WHERE version = '000030_prd6_provider_cost_reconciliation';

COMMIT;
