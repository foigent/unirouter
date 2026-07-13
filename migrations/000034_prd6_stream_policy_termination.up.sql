BEGIN;

-- D6-16 adds an independently selectable stream-output stage. Existing input
-- and non-stream policies are not copied or enabled automatically.
ALTER TABLE guardrail_policies
    DROP CONSTRAINT guardrail_policies_stage_check;
ALTER TABLE guardrail_policies
    ADD CONSTRAINT guardrail_policies_stage_check
        CHECK (stage IN ('input', 'nonstream_output', 'stream_output'));

-- A long stream grows one immutable-price reservation in monotonic,
-- idempotent steps. The source split remains part of the parent reservation.
ALTER TABLE freeze_idempotency
    ADD COLUMN reserved_completion_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN reservation_extension_sequence INTEGER NOT NULL DEFAULT 0;

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_reserved_completion_nonnegative
        CHECK (reserved_completion_tokens >= 0),
    ADD CONSTRAINT freeze_idempotency_extension_sequence_nonnegative
        CHECK (reservation_extension_sequence >= 0);

CREATE TABLE billing_reservation_extensions (
    org_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    sequence INTEGER NOT NULL,
    additional_completion_tokens INTEGER NOT NULL,
    target_completion_tokens INTEGER NOT NULL,
    reserved_amount INTEGER NOT NULL,
    reserved_balance_amount INTEGER NOT NULL,
    reserved_granted_amount INTEGER NOT NULL,
    pricing_version VARCHAR(80) NOT NULL,
    status VARCHAR(16) NOT NULL,
    error_code VARCHAR(80) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, request_id, sequence),
    CONSTRAINT billing_reservation_extensions_reservation_fk
        FOREIGN KEY (org_id, request_id)
        REFERENCES freeze_idempotency(org_id, request_id)
        ON DELETE RESTRICT,
    CONSTRAINT billing_reservation_extensions_sequence_positive CHECK (sequence > 0),
    CONSTRAINT billing_reservation_extensions_tokens_positive CHECK (
        additional_completion_tokens > 0
        AND target_completion_tokens >= additional_completion_tokens
    ),
    CONSTRAINT billing_reservation_extensions_amounts_valid CHECK (
        reserved_amount >= 0
        AND reserved_balance_amount >= 0
        AND reserved_granted_amount >= 0
        AND reserved_balance_amount + reserved_granted_amount = reserved_amount
    ),
    CONSTRAINT billing_reservation_extensions_status_check CHECK (
        (status = 'reserved' AND error_code = '')
        OR (status = 'rejected' AND reserved_amount = 0
            AND reserved_balance_amount = 0 AND reserved_granted_amount = 0
            AND length(btrim(error_code)) > 0)
    )
);

CREATE FUNCTION enforce_billing_reservation_extension_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    parent_reserved_completion INTEGER;
    parent_sequence INTEGER;
    parent_pricing_version VARCHAR(80);
    previous_sequence INTEGER;
BEGIN
    SELECT reserved_completion_tokens, reservation_extension_sequence, pricing_version
    INTO parent_reserved_completion, parent_sequence, parent_pricing_version
    FROM freeze_idempotency
    WHERE org_id = NEW.org_id AND request_id = NEW.request_id
    FOR UPDATE;

    SELECT COALESCE(MAX(sequence), 0)
    INTO previous_sequence
    FROM billing_reservation_extensions
    WHERE org_id = NEW.org_id AND request_id = NEW.request_id;

    IF NEW.sequence <> previous_sequence + 1 OR NEW.pricing_version <> parent_pricing_version THEN
        RAISE EXCEPTION 'reservation extension sequence or pricing version is not monotonic';
    END IF;
    IF NEW.status = 'reserved'
       AND (parent_sequence <> NEW.sequence OR parent_reserved_completion <> NEW.target_completion_tokens) THEN
        RAISE EXCEPTION 'reserved extension does not match its parent reservation';
    END IF;
    IF NEW.status = 'rejected'
       AND (parent_sequence <> NEW.sequence - 1
            OR parent_reserved_completion + NEW.additional_completion_tokens <> NEW.target_completion_tokens) THEN
        RAISE EXCEPTION 'rejected extension does not match its parent reservation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER billing_reservation_extension_transition
BEFORE INSERT ON billing_reservation_extensions
FOR EACH ROW EXECUTE FUNCTION enforce_billing_reservation_extension_transition();

CREATE FUNCTION reject_billing_reservation_extension_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'billing reservation extension receipts are append-only';
END;
$$;

CREATE TRIGGER billing_reservation_extension_append_only
BEFORE UPDATE OR DELETE ON billing_reservation_extensions
FOR EACH ROW EXECUTE FUNCTION reject_billing_reservation_extension_mutation();

-- One mutable lease says whether a process still owns a live stream. The
-- checkpoint rows below are append-only and contain counters only, never model
-- content or provider credentials.
CREATE TABLE stream_delivery_leases (
    org_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    attempt_id VARCHAR(64) NOT NULL,
    owner_id VARCHAR(80) NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'active',
    lease_expires_at TIMESTAMPTZ NOT NULL,
    terminal_reason VARCHAR(80) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, request_id),
    CONSTRAINT stream_delivery_leases_reservation_fk
        FOREIGN KEY (org_id, request_id)
        REFERENCES freeze_idempotency(org_id, request_id)
        ON DELETE RESTRICT,
    CONSTRAINT stream_delivery_leases_identity_nonempty CHECK (
        length(btrim(attempt_id)) > 0 AND length(btrim(owner_id)) > 0
    ),
    CONSTRAINT stream_delivery_leases_state_check CHECK (
        state IN ('active', 'completed', 'interrupted')
    ),
    CONSTRAINT stream_delivery_leases_terminal_reason_check CHECK (
        (state = 'active' AND terminal_reason = '')
        OR (state <> 'active' AND length(btrim(terminal_reason)) > 0)
    )
);

CREATE INDEX idx_stream_delivery_leases_recovery
    ON stream_delivery_leases(lease_expires_at, org_id, request_id)
    WHERE state = 'active';

CREATE TABLE stream_delivery_checkpoints (
    org_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    sequence INTEGER NOT NULL,
    attempt_id VARCHAR(64) NOT NULL,
    delivered_bytes BIGINT NOT NULL,
    delivered_chunks INTEGER NOT NULL,
    prompt_tokens INTEGER NOT NULL,
    completion_tokens INTEGER NOT NULL,
    policy_set_version VARCHAR(80) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, request_id, sequence),
    CONSTRAINT stream_delivery_checkpoints_lease_fk
        FOREIGN KEY (org_id, request_id)
        REFERENCES stream_delivery_leases(org_id, request_id)
        ON DELETE RESTRICT,
    CONSTRAINT stream_delivery_checkpoints_sequence_positive CHECK (sequence > 0),
    CONSTRAINT stream_delivery_checkpoints_counts_positive CHECK (
        delivered_bytes > 0 AND delivered_chunks > 0
        AND prompt_tokens >= 0 AND completion_tokens >= 0
    ),
    CONSTRAINT stream_delivery_checkpoints_identity_nonempty CHECK (
        length(btrim(attempt_id)) > 0 AND length(btrim(policy_set_version)) > 0
    )
);

CREATE INDEX idx_stream_delivery_checkpoints_latest
    ON stream_delivery_checkpoints(org_id, request_id, sequence DESC);

CREATE FUNCTION enforce_stream_delivery_checkpoint_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    previous stream_delivery_checkpoints%ROWTYPE;
BEGIN
    SELECT * INTO previous
    FROM stream_delivery_checkpoints
    WHERE org_id = NEW.org_id AND request_id = NEW.request_id
    ORDER BY sequence DESC
    LIMIT 1;

    IF NOT FOUND THEN
        IF NEW.sequence <> 1 THEN
            RAISE EXCEPTION 'stream checkpoint sequence must begin at one';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.sequence <> previous.sequence + 1
       OR NEW.attempt_id <> previous.attempt_id
       OR NEW.policy_set_version <> previous.policy_set_version
       OR NEW.delivered_bytes < previous.delivered_bytes
       OR NEW.delivered_chunks < previous.delivered_chunks
       OR NEW.prompt_tokens < previous.prompt_tokens
       OR NEW.completion_tokens < previous.completion_tokens THEN
        RAISE EXCEPTION 'stream checkpoint identity or counters are not monotonic';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER stream_delivery_checkpoint_transition
BEFORE INSERT ON stream_delivery_checkpoints
FOR EACH ROW EXECUTE FUNCTION enforce_stream_delivery_checkpoint_transition();

CREATE FUNCTION reject_stream_delivery_checkpoint_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'stream delivery checkpoints are append-only';
END;
$$;

CREATE TRIGGER stream_delivery_checkpoint_append_only
BEFORE UPDATE OR DELETE ON stream_delivery_checkpoints
FOR EACH ROW EXECUTE FUNCTION reject_stream_delivery_checkpoint_mutation();

-- An expired process lease can prove no positive customer delivery at all. It
-- is still an explicit interruption receipt, but it is not chargeable usage.
ALTER TABLE request_delivery_receipts
    DROP CONSTRAINT request_delivery_receipts_outcome_counts_check;
ALTER TABLE request_delivery_receipts
    ADD CONSTRAINT request_delivery_receipts_outcome_counts_check CHECK (
        outcome NOT IN ('partial', 'completed', 'interrupted')
        OR (delivered_bytes > 0 AND delivered_chunks > 0)
        OR (outcome = 'interrupted' AND delivered_bytes = 0 AND delivered_chunks = 0)
    );

COMMIT;
