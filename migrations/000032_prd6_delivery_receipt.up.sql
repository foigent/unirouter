BEGIN;

-- P6-F41B stores bounded local write outcomes as append-only metadata. It
-- never stores prompts, provider credentials, response bodies, or chunks.
CREATE TABLE request_delivery_receipts (
    org_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    sequence SMALLINT NOT NULL,
    mode VARCHAR(16) NOT NULL,
    outcome VARCHAR(24) NOT NULL,
    delivered_bytes BIGINT NOT NULL DEFAULT 0,
    delivered_chunks INTEGER NOT NULL DEFAULT 0,
    http_status INTEGER,
    error_code VARCHAR(80) NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, request_id, sequence),
    CONSTRAINT request_delivery_receipts_reservation_fk
        FOREIGN KEY (org_id, request_id)
        REFERENCES freeze_idempotency(org_id, request_id)
        ON DELETE RESTRICT,
    CONSTRAINT request_delivery_receipts_sequence_check CHECK (sequence BETWEEN 1 AND 3),
    CONSTRAINT request_delivery_receipts_mode_check CHECK (mode IN ('nonstream', 'stream')),
    CONSTRAINT request_delivery_receipts_outcome_check CHECK (
        outcome IN ('write_started', 'partial', 'completed', 'write_failed', 'interrupted')
    ),
    CONSTRAINT request_delivery_receipts_counts_check CHECK (
        delivered_bytes >= 0 AND delivered_chunks >= 0
    ),
    CONSTRAINT request_delivery_receipts_outcome_counts_check CHECK (
        outcome IN ('write_started', 'write_failed')
        OR (delivered_bytes > 0 AND delivered_chunks > 0)
    ),
    CONSTRAINT request_delivery_receipts_status_check CHECK (
        http_status IS NULL OR http_status BETWEEN 100 AND 599
    ),
    CONSTRAINT request_delivery_receipts_error_check CHECK (
        length(btrim(error_code)) <= 80
    )
);

CREATE INDEX idx_request_delivery_receipts_latest
    ON request_delivery_receipts(org_id, request_id, sequence DESC);

CREATE OR REPLACE FUNCTION enforce_request_delivery_receipt_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    previous request_delivery_receipts%ROWTYPE;
BEGIN
    SELECT * INTO previous
    FROM request_delivery_receipts
    WHERE org_id = NEW.org_id AND request_id = NEW.request_id
    ORDER BY sequence DESC
    LIMIT 1;

    IF NOT FOUND THEN
        IF NEW.sequence <> 1 OR NEW.outcome <> 'write_started'
           OR NEW.delivered_bytes <> 0 OR NEW.delivered_chunks <> 0 THEN
            RAISE EXCEPTION 'delivery receipt must begin with zero-byte write_started';
        END IF;
        RETURN NEW;
    END IF;

    IF previous.outcome IN ('completed', 'write_failed', 'interrupted') THEN
        RAISE EXCEPTION 'delivery receipt is already terminal';
    END IF;
    IF NEW.sequence <> previous.sequence + 1 OR NEW.mode <> previous.mode THEN
        RAISE EXCEPTION 'delivery receipt sequence or mode is not monotonic';
    END IF;
    IF NEW.delivered_bytes < previous.delivered_bytes
       OR NEW.delivered_chunks < previous.delivered_chunks THEN
        RAISE EXCEPTION 'delivery receipt counters cannot decrease';
    END IF;
    IF previous.outcome = 'write_started'
       AND NEW.outcome NOT IN ('partial', 'completed', 'write_failed', 'interrupted') THEN
        RAISE EXCEPTION 'invalid delivery transition after write_started';
    END IF;
    IF previous.outcome = 'partial'
       AND NEW.outcome NOT IN ('completed', 'interrupted') THEN
        RAISE EXCEPTION 'invalid delivery transition after partial';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER request_delivery_receipt_transition
BEFORE INSERT ON request_delivery_receipts
FOR EACH ROW EXECUTE FUNCTION enforce_request_delivery_receipt_transition();

CREATE OR REPLACE FUNCTION reject_request_delivery_receipt_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'delivery receipts are append-only';
END;
$$;

CREATE TRIGGER request_delivery_receipt_append_only
BEFORE UPDATE OR DELETE ON request_delivery_receipts
FOR EACH ROW EXECUTE FUNCTION reject_request_delivery_receipt_mutation();

ALTER TABLE request_idempotency
    ADD COLUMN IF NOT EXISTS usage_authority VARCHAR(40) NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS usage_purpose VARCHAR(32) NOT NULL DEFAULT 'provider_evidence',
    ADD COLUMN IF NOT EXISTS usage_finality VARCHAR(24) NOT NULL DEFAULT 'missing',
    ADD COLUMN IF NOT EXISTS usage_contract_version VARCHAR(80) NOT NULL DEFAULT 'unirouter.usage.v1',
    ADD COLUMN IF NOT EXISTS usage_estimate_version VARCHAR(80) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delivery_mode VARCHAR(16) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delivery_outcome VARCHAR(24) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delivered_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS delivered_chunks INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS delivery_updated_at TIMESTAMPTZ;

ALTER TABLE request_idempotency
    ADD CONSTRAINT request_idempotency_usage_contract_check CHECK (
        usage_contract_version = 'unirouter.usage.v1'
        AND usage_purpose IN ('provider_evidence', 'customer_charge')
        AND (
            (usage_authority = 'provider_reported' AND usage_finality = 'final' AND usage_estimate_version = '')
            OR (usage_authority = 'gateway_counter' AND usage_finality = 'estimated' AND usage_estimate_version = 'unirouter.stream-counter.v2')
            OR (usage_authority = 'conservative_estimate' AND usage_finality = 'estimated' AND length(btrim(usage_estimate_version)) > 0)
            OR (usage_authority = 'none' AND usage_purpose = 'provider_evidence' AND usage_finality = 'missing' AND usage_estimate_version = '')
        )
    ),
    ADD CONSTRAINT request_idempotency_delivery_check CHECK (
        (delivery_mode = '' AND delivery_outcome = '' AND delivered_bytes = 0 AND delivered_chunks = 0 AND delivery_updated_at IS NULL)
        OR (
            delivery_mode IN ('nonstream', 'stream')
            AND delivery_outcome IN ('write_started', 'partial', 'completed', 'write_failed', 'interrupted')
            AND delivered_bytes >= 0
            AND delivered_chunks >= 0
            AND delivery_updated_at IS NOT NULL
        )
    );

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

DROP INDEX idx_billing_provider_attempts_unresolved_key;
CREATE INDEX idx_billing_provider_attempts_unresolved_key
    ON billing_provider_attempts(org_id, api_key_id, updated_at, request_id)
    WHERE state IN (
        'dispatch_started', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending', 'delivery_pending', 'delivery_failed'
    );

DROP INDEX idx_billing_provider_attempts_unresolved_channel;
CREATE INDEX idx_billing_provider_attempts_unresolved_channel
    ON billing_provider_attempts(channel_id, channel_key_id, updated_at, org_id, request_id)
    WHERE state IN (
        'dispatch_started', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending', 'delivery_pending', 'delivery_failed'
    );

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_pending_reason_valid,
    DROP CONSTRAINT freeze_idempotency_pending_has_financial_marker,
    DROP CONSTRAINT freeze_idempotency_payload_requires_financial_marker;

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_pending_reason_valid CHECK (
        settlement_pending_reason IN (
            '', 'billable_output', 'insufficient_topup', 'ambiguous_attempt',
            'provider_response_unusable', 'provider_attempt_unresolved',
            'delivery_prepared', 'delivery_completed', 'delivery_failed'
        )
    ),
    ADD CONSTRAINT freeze_idempotency_pending_has_financial_marker CHECK (
        status <> 'settlement_pending'
        OR output_started_at IS NOT NULL
        OR provider_cost_pending_at IS NOT NULL
        OR settlement_pending_reason IN ('delivery_prepared', 'delivery_completed', 'delivery_failed')
    ),
    ADD CONSTRAINT freeze_idempotency_payload_requires_financial_marker CHECK (
        settlement_payload_ready_at IS NULL
        OR status IN ('settled', 'cancelled')
        OR output_started_at IS NOT NULL
        OR provider_cost_pending_at IS NOT NULL
        OR settlement_pending_reason IN ('delivery_prepared', 'delivery_completed', 'delivery_failed')
    );

DROP INDEX idx_freeze_idempotency_settlement_recovery;
CREATE INDEX idx_freeze_idempotency_settlement_recovery
    ON freeze_idempotency(settlement_next_attempt_at, settlement_payload_ready_at, created_at)
    WHERE status = 'settlement_pending'
      AND settlement_payload_ready_at IS NOT NULL
      AND settlement_pending_reason IN ('billable_output', 'delivery_completed');

COMMIT;
