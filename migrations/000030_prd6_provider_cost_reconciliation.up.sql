BEGIN;

ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_state_check,
    DROP CONSTRAINT billing_provider_attempts_state_certainty_check;

ALTER TABLE billing_provider_attempts
    ADD CONSTRAINT billing_provider_attempts_state_check
        CHECK (state IN (
            'dispatch_started', 'rejected', 'response_accepted', 'outcome_unknown',
            'response_unusable', 'funding_pending', 'settled', 'reconciled'
        )),
    ADD CONSTRAINT billing_provider_attempts_state_certainty_check
        CHECK (
            (state IN ('dispatch_started', 'response_accepted') AND outcome_certainty = 'pending')
            OR (state = 'rejected' AND outcome_certainty = 'known_rejected')
            OR (state IN ('outcome_unknown', 'response_unusable') AND outcome_certainty = 'unknown')
            OR (state IN ('funding_pending', 'settled') AND outcome_certainty = 'known_usage')
            OR (state = 'reconciled' AND outcome_certainty IN ('unknown', 'known_usage'))
        );

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_payload_requires_financial_marker;

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_payload_requires_financial_marker
        CHECK (
            settlement_payload_ready_at IS NULL
            OR status IN ('settled', 'cancelled')
            OR output_started_at IS NOT NULL
            OR provider_cost_pending_at IS NOT NULL
        );

CREATE TABLE provider_cost_reconciliations (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    attempt_id VARCHAR(64) NOT NULL,
    api_key_id BIGINT NOT NULL,
    action VARCHAR(24) NOT NULL,
    phase VARCHAR(16) NOT NULL,
    actor_user_id BIGINT NOT NULL,
    reason VARCHAR(500) NOT NULL,
    evidence_ref VARCHAR(255) NOT NULL,
    terminal_transaction_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT provider_cost_reconciliations_attempt_fk
        FOREIGN KEY (org_id, request_id, attempt_id)
        REFERENCES billing_provider_attempts(org_id, request_id, attempt_id)
        ON DELETE RESTRICT,
    CONSTRAINT provider_cost_reconciliations_action_check
        CHECK (action IN ('collect', 'refund', 'subsidy')),
    CONSTRAINT provider_cost_reconciliations_phase_check
        CHECK (phase IN ('requested', 'completed')),
    CONSTRAINT provider_cost_reconciliations_identity_check
        CHECK (api_key_id > 0 AND actor_user_id > 0),
    CONSTRAINT provider_cost_reconciliations_reason_check
        CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
    CONSTRAINT provider_cost_reconciliations_evidence_check
        CHECK (length(btrim(evidence_ref)) BETWEEN 1 AND 255),
    CONSTRAINT provider_cost_reconciliations_terminal_check
        CHECK (
            (phase = 'requested' AND terminal_transaction_id IS NULL)
            OR (phase = 'completed' AND terminal_transaction_id IS NOT NULL)
        ),
    UNIQUE (org_id, request_id, attempt_id, action, phase)
);

CREATE UNIQUE INDEX idx_provider_cost_reconciliations_one_request
    ON provider_cost_reconciliations(org_id, request_id, attempt_id)
    WHERE phase = 'requested';

CREATE INDEX idx_provider_cost_reconciliations_org_created
    ON provider_cost_reconciliations(org_id, created_at DESC, id DESC);

COMMIT;
