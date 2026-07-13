BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM guardrail_policies)
       OR EXISTS (SELECT 1 FROM billing_provider_attempts WHERE state = 'output_withheld')
       OR EXISTS (SELECT 1 FROM freeze_idempotency WHERE settlement_pending_reason IN ('guardrail_withheld', 'response_too_large')) THEN
        RAISE EXCEPTION 'P6-F10 rollback refused: guardrail policy or financial hold evidence exists';
    END IF;
END;
$$;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_pending_reason_valid;

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_pending_reason_valid CHECK (
        settlement_pending_reason IN (
            '', 'billable_output', 'insufficient_topup', 'ambiguous_attempt',
            'provider_response_unusable', 'provider_attempt_unresolved',
            'delivery_prepared', 'delivery_completed', 'delivery_failed'
        )
    );

DROP INDEX idx_billing_provider_attempts_unresolved_key;
DROP INDEX idx_billing_provider_attempts_unresolved_channel;

ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_state_check,
    DROP CONSTRAINT billing_provider_attempts_state_certainty_check;

ALTER TABLE billing_provider_attempts
    ADD CONSTRAINT billing_provider_attempts_state_check CHECK (state IN (
        'dispatch_started', 'rejected', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending', 'delivery_pending',
        'delivery_failed', 'settled', 'reconciled'
    )),
    ADD CONSTRAINT billing_provider_attempts_state_certainty_check CHECK (
        (state IN ('dispatch_started', 'response_accepted') AND outcome_certainty = 'pending')
        OR (state = 'rejected' AND outcome_certainty = 'known_rejected')
        OR (state IN ('outcome_unknown', 'response_unusable') AND outcome_certainty = 'unknown')
        OR (state IN ('funding_pending', 'delivery_pending', 'delivery_failed', 'settled') AND outcome_certainty = 'known_usage')
        OR (state = 'reconciled' AND outcome_certainty IN ('unknown', 'known_usage'))
    );

CREATE INDEX idx_billing_provider_attempts_unresolved_key
    ON billing_provider_attempts(org_id, api_key_id, updated_at, request_id)
    WHERE state IN (
        'dispatch_started', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending', 'delivery_pending', 'delivery_failed'
    );

CREATE INDEX idx_billing_provider_attempts_unresolved_channel
    ON billing_provider_attempts(channel_id, channel_key_id, updated_at, org_id, request_id)
    WHERE state IN (
        'dispatch_started', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending', 'delivery_pending', 'delivery_failed'
    );

DROP TABLE guardrail_policies;
DROP INDEX idx_api_keys_guardrail_project_scope;
DROP INDEX idx_api_keys_guardrail_org_scope;
DROP INDEX idx_projects_guardrail_scope;

DELETE FROM schema_migrations
WHERE version = '000033_prd6_guardrail_kernel';

COMMIT;
