BEGIN;

-- P6-F18A makes each reservation self-describing. The spendable wallet
-- buckets are debited at freeze time; these columns retain the source split so
-- a later refund never converts expiring grant credit into cash.
ALTER TABLE freeze_idempotency
    ADD COLUMN IF NOT EXISTS reserved_balance_amount INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS reserved_granted_amount INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS model_name VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS estimated_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS safety_factor NUMERIC(10,4) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS min_freeze_amount INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS effective_input_price_cents NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS effective_output_price_cents NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS price_multiplier NUMERIC(10,6) NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS pricing_version VARCHAR(80) NOT NULL DEFAULT 'legacy',
    ADD COLUMN IF NOT EXISTS prompt_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS completion_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS usage_source VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS calculated_cost INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS charged_cost INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS uncovered_cost INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS refund_amount INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS forfeited_grant_amount INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS terminal_transaction_id BIGINT,
    ADD COLUMN IF NOT EXISTS balance_after INTEGER,
    ADD COLUMN IF NOT EXISTS terminal_at TIMESTAMPTZ;

-- Historical reservations did not record their funding source. Preserve the
-- historical runtime behavior by treating them as cash-backed; new writers
-- always persist the exact split.
UPDATE freeze_idempotency
SET reserved_balance_amount = frozen_amount
WHERE frozen_amount > 0
  AND reserved_balance_amount = 0
  AND reserved_granted_amount = 0;

-- Backfill the latest durable settlement receipt where one exists. The
-- duplicate guard below fails closed before enforcing the new unique index.
UPDATE freeze_idempotency f
SET calculated_cost = t.amount,
    charged_cost = t.amount,
    refund_amount = GREATEST(f.frozen_amount - t.amount, 0),
    terminal_transaction_id = t.id,
    balance_after = t.balance_after,
    terminal_at = t.created_at
FROM (
    SELECT DISTINCT ON (org_id, request_id)
           id, org_id, request_id, amount, balance_after, created_at
    FROM transactions
    WHERE type = 'settle'
    ORDER BY org_id, request_id, id DESC
) t
WHERE f.org_id = t.org_id
  AND f.request_id = t.request_id
  AND f.status = 'settled';

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM transactions
        WHERE type IN ('settle', 'cancel')
        GROUP BY org_id, request_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'P6-F18A cannot continue: duplicate terminal billing transactions exist';
    END IF;
END;
$$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_transactions_one_billing_terminal
    ON transactions(org_id, request_id)
    WHERE type IN ('settle', 'cancel');

CREATE TABLE IF NOT EXISTS billing_reservation_grant_allocations (
    org_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    grant_id BIGINT NOT NULL REFERENCES grants(id),
    reserved_amount INTEGER NOT NULL,
    consumed_amount INTEGER NOT NULL DEFAULT 0,
    refunded_amount INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, request_id, grant_id),
    CONSTRAINT billing_reservation_grant_allocation_reservation_fk
        FOREIGN KEY (org_id, request_id)
        REFERENCES freeze_idempotency(org_id, request_id)
        ON DELETE CASCADE,
    CONSTRAINT billing_reservation_grant_reserved_positive
        CHECK (reserved_amount > 0),
    CONSTRAINT billing_reservation_grant_terminal_amounts_valid
        CHECK (
            consumed_amount >= 0
            AND refunded_amount >= 0
            AND consumed_amount + refunded_amount <= reserved_amount
        )
);

CREATE INDEX IF NOT EXISTS idx_billing_reservation_grant_allocations_grant
    ON billing_reservation_grant_allocations(grant_id);

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_reserved_balance_nonnegative
        CHECK (reserved_balance_amount >= 0),
    ADD CONSTRAINT freeze_idempotency_reserved_granted_nonnegative
        CHECK (reserved_granted_amount >= 0),
    ADD CONSTRAINT freeze_idempotency_reservation_source_sum
        CHECK (reserved_balance_amount + reserved_granted_amount = frozen_amount),
    ADD CONSTRAINT freeze_idempotency_usage_nonnegative
        CHECK (estimated_tokens >= 0 AND prompt_tokens >= 0 AND completion_tokens >= 0),
    ADD CONSTRAINT freeze_idempotency_costs_nonnegative
        CHECK (
            min_freeze_amount >= 0
            AND calculated_cost >= 0
            AND charged_cost >= 0
            AND uncovered_cost >= 0
            AND refund_amount >= 0
            AND forfeited_grant_amount >= 0
        );

COMMIT;
