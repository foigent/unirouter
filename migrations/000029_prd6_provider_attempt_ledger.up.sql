BEGIN;

-- P6-F40B records a durable financial attempt boundary before every network
-- dispatch that may create provider liability. It stores bounded metadata
-- only: no prompt, response body, credential, or full provider URL.
-- Rollout boundary: drain old application writers before activating this
-- migration. Old binaries do not create attempt rows and must not coexist
-- with the new cancellation authority.
CREATE TABLE billing_provider_attempts (
    org_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    attempt_id VARCHAR(64) NOT NULL,
    api_key_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    channel_key_id BIGINT NOT NULL,
    route_sequence INTEGER NOT NULL,
    route_total INTEGER NOT NULL,
    endpoint VARCHAR(64) NOT NULL,
    provider VARCHAR(50) NOT NULL,
    channel_name VARCHAR(255) NOT NULL,
    state VARCHAR(32) NOT NULL DEFAULT 'dispatch_started',
    outcome_certainty VARCHAR(24) NOT NULL DEFAULT 'pending',
    retry_authorized BOOLEAN NOT NULL DEFAULT FALSE,
    http_status INTEGER,
    failure_kind VARCHAR(40),
    upstream_request_id VARCHAR(128),
    calculated_cost INTEGER,
    charged_cost INTEGER,
    terminal_transaction_id BIGINT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    response_received_at TIMESTAMPTZ,
    financial_resolved_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, request_id, attempt_id),
    CONSTRAINT billing_provider_attempts_reservation_fk
        FOREIGN KEY (org_id, request_id)
        REFERENCES freeze_idempotency(org_id, request_id)
        ON DELETE RESTRICT,
    CONSTRAINT billing_provider_attempts_route_check
        CHECK (route_sequence > 0 AND route_total > 0 AND route_sequence <= route_total),
    CONSTRAINT billing_provider_attempts_identity_check
        CHECK (
            length(btrim(attempt_id)) > 0
            AND length(btrim(endpoint)) > 0
            AND length(btrim(provider)) > 0
            AND length(btrim(channel_name)) > 0
            AND api_key_id > 0
            AND channel_id > 0
            AND channel_key_id > 0
        ),
    CONSTRAINT billing_provider_attempts_state_check
        CHECK (state IN (
            'dispatch_started',
            'rejected',
            'response_accepted',
            'outcome_unknown',
            'response_unusable',
            'funding_pending',
            'settled'
        )),
    CONSTRAINT billing_provider_attempts_certainty_check
        CHECK (outcome_certainty IN ('pending', 'known_rejected', 'unknown', 'known_usage')),
    CONSTRAINT billing_provider_attempts_state_certainty_check
        CHECK (
            (state IN ('dispatch_started', 'response_accepted') AND outcome_certainty = 'pending')
            OR (state = 'rejected' AND outcome_certainty = 'known_rejected')
            OR (state IN ('outcome_unknown', 'response_unusable') AND outcome_certainty = 'unknown')
            OR (state IN ('funding_pending', 'settled') AND outcome_certainty = 'known_usage')
        ),
    CONSTRAINT billing_provider_attempts_retry_authority_check
        CHECK (NOT retry_authorized OR state = 'rejected'),
    CONSTRAINT billing_provider_attempts_http_status_check
        CHECK (http_status IS NULL OR http_status BETWEEN 100 AND 599),
    CONSTRAINT billing_provider_attempts_cost_check
        CHECK (
            (calculated_cost IS NULL OR calculated_cost >= 0)
            AND (charged_cost IS NULL OR charged_cost >= 0)
        )
);

CREATE INDEX idx_billing_provider_attempts_request
    ON billing_provider_attempts(org_id, request_id, started_at, attempt_id);

CREATE INDEX idx_billing_provider_attempts_unresolved_key
    ON billing_provider_attempts(org_id, api_key_id, updated_at, request_id)
    WHERE state IN (
        'dispatch_started', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending'
    );

CREATE INDEX idx_billing_provider_attempts_unresolved_channel
    ON billing_provider_attempts(channel_id, channel_key_id, updated_at, org_id, request_id)
    WHERE state IN (
        'dispatch_started', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending'
    );

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT IF EXISTS freeze_idempotency_pending_reason_valid,
    ADD CONSTRAINT freeze_idempotency_pending_reason_valid
        CHECK (settlement_pending_reason IN (
            '',
            'billable_output',
            'insufficient_topup',
            'ambiguous_attempt',
            'provider_response_unusable',
            'provider_attempt_unresolved'
        ));

-- Automatic recovery owns only rows with a durable usage payload produced by
-- the normal streaming path. Provider-attempt holds require reconciliation.
DROP INDEX IF EXISTS idx_freeze_idempotency_settlement_recovery;
CREATE INDEX idx_freeze_idempotency_settlement_recovery
    ON freeze_idempotency(settlement_next_attempt_at, settlement_payload_ready_at, created_at)
    WHERE status = 'settlement_pending'
      AND settlement_payload_ready_at IS NOT NULL
      AND settlement_pending_reason = 'billable_output';

COMMIT;
