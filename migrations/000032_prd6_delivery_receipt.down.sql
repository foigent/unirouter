BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM request_delivery_receipts)
       OR EXISTS (
           SELECT 1 FROM request_idempotency
           WHERE usage_authority <> 'none'
              OR usage_purpose <> 'provider_evidence'
              OR usage_finality <> 'missing'
              OR usage_estimate_version <> ''
              OR delivery_mode <> ''
              OR delivery_outcome <> ''
              OR delivered_bytes <> 0
              OR delivered_chunks <> 0
              OR delivery_updated_at IS NOT NULL
       ) THEN
        RAISE EXCEPTION 'P6-F41B rollback refused: delivery or idempotency receipt evidence exists';
    END IF;
END;
$$;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_pending_reason_valid,
    DROP CONSTRAINT freeze_idempotency_pending_has_financial_marker,
    DROP CONSTRAINT freeze_idempotency_payload_requires_financial_marker;

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_pending_reason_valid CHECK (
        settlement_pending_reason IN (
            '', 'billable_output', 'insufficient_topup', 'ambiguous_attempt',
            'provider_response_unusable', 'provider_attempt_unresolved'
        )
    ),
    ADD CONSTRAINT freeze_idempotency_pending_has_financial_marker CHECK (
        status <> 'settlement_pending'
        OR output_started_at IS NOT NULL
        OR provider_cost_pending_at IS NOT NULL
    ),
    ADD CONSTRAINT freeze_idempotency_payload_requires_financial_marker CHECK (
        settlement_payload_ready_at IS NULL
        OR status IN ('settled', 'cancelled')
        OR output_started_at IS NOT NULL
        OR provider_cost_pending_at IS NOT NULL
    );

DROP INDEX idx_freeze_idempotency_settlement_recovery;
CREATE INDEX idx_freeze_idempotency_settlement_recovery
    ON freeze_idempotency(settlement_next_attempt_at, settlement_payload_ready_at, created_at)
    WHERE status = 'settlement_pending'
      AND settlement_payload_ready_at IS NOT NULL
      AND settlement_pending_reason = 'billable_output';

DROP INDEX idx_billing_provider_attempts_unresolved_key;
DROP INDEX idx_billing_provider_attempts_unresolved_channel;

ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_state_check,
    DROP CONSTRAINT billing_provider_attempts_state_certainty_check;

ALTER TABLE billing_provider_attempts
    ADD CONSTRAINT billing_provider_attempts_state_check CHECK (state IN (
        'dispatch_started', 'rejected', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending', 'settled', 'reconciled'
    )),
    ADD CONSTRAINT billing_provider_attempts_state_certainty_check CHECK (
        (state IN ('dispatch_started', 'response_accepted') AND outcome_certainty = 'pending')
        OR (state = 'rejected' AND outcome_certainty = 'known_rejected')
        OR (state IN ('outcome_unknown', 'response_unusable') AND outcome_certainty = 'unknown')
        OR (state IN ('funding_pending', 'settled') AND outcome_certainty = 'known_usage')
        OR (state = 'reconciled' AND outcome_certainty IN ('unknown', 'known_usage'))
    );

CREATE INDEX idx_billing_provider_attempts_unresolved_key
    ON billing_provider_attempts(org_id, api_key_id, updated_at, request_id)
    WHERE state IN ('dispatch_started', 'response_accepted', 'outcome_unknown', 'response_unusable', 'funding_pending');

CREATE INDEX idx_billing_provider_attempts_unresolved_channel
    ON billing_provider_attempts(channel_id, channel_key_id, updated_at, org_id, request_id)
    WHERE state IN ('dispatch_started', 'response_accepted', 'outcome_unknown', 'response_unusable', 'funding_pending');

ALTER TABLE request_idempotency
    DROP CONSTRAINT request_idempotency_usage_contract_check,
    DROP CONSTRAINT request_idempotency_delivery_check,
    DROP COLUMN delivery_updated_at,
    DROP COLUMN delivered_chunks,
    DROP COLUMN delivered_bytes,
    DROP COLUMN delivery_outcome,
    DROP COLUMN delivery_mode,
    DROP COLUMN usage_estimate_version,
    DROP COLUMN usage_contract_version,
    DROP COLUMN usage_finality,
    DROP COLUMN usage_purpose,
    DROP COLUMN usage_authority;

DROP TRIGGER request_delivery_receipt_append_only ON request_delivery_receipts;
DROP TRIGGER request_delivery_receipt_transition ON request_delivery_receipts;
DROP TABLE request_delivery_receipts;
DROP FUNCTION reject_request_delivery_receipt_mutation();
DROP FUNCTION enforce_request_delivery_receipt_transition();

DELETE FROM schema_migrations
WHERE version = '000032_prd6_delivery_receipt';

COMMIT;
