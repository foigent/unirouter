BEGIN;

-- A schema-only rollback is allowed only before a new writer has created a
-- reservation. Dropping source allocations or price receipts after use would
-- make a legacy binary refund grant credit as cash or reprice accepted work.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM freeze_idempotency WHERE pricing_version <> 'legacy'
    ) OR EXISTS (
        SELECT 1 FROM billing_reservation_grant_allocations
    ) THEN
        RAISE EXCEPTION 'P6-F18A down refused: new reservation evidence exists; drain/finalize and roll forward instead';
    END IF;
END;
$$;

DROP INDEX IF EXISTS idx_billing_reservation_grant_allocations_grant;
DROP TABLE IF EXISTS billing_reservation_grant_allocations;
DROP INDEX IF EXISTS idx_transactions_one_billing_terminal;

ALTER TABLE freeze_idempotency
    DROP COLUMN IF EXISTS terminal_at,
    DROP COLUMN IF EXISTS balance_after,
    DROP COLUMN IF EXISTS terminal_transaction_id,
    DROP COLUMN IF EXISTS forfeited_grant_amount,
    DROP COLUMN IF EXISTS refund_amount,
    DROP COLUMN IF EXISTS uncovered_cost,
    DROP COLUMN IF EXISTS charged_cost,
    DROP COLUMN IF EXISTS calculated_cost,
    DROP COLUMN IF EXISTS usage_source,
    DROP COLUMN IF EXISTS completion_tokens,
    DROP COLUMN IF EXISTS prompt_tokens,
    DROP COLUMN IF EXISTS pricing_version,
    DROP COLUMN IF EXISTS price_multiplier,
    DROP COLUMN IF EXISTS effective_output_price_cents,
    DROP COLUMN IF EXISTS effective_input_price_cents,
    DROP COLUMN IF EXISTS min_freeze_amount,
    DROP COLUMN IF EXISTS safety_factor,
    DROP COLUMN IF EXISTS estimated_tokens,
    DROP COLUMN IF EXISTS model_name,
    DROP COLUMN IF EXISTS reserved_granted_amount,
    DROP COLUMN IF EXISTS reserved_balance_amount;

DELETE FROM schema_migrations
WHERE version = '000022_prd6_billing_reservation_invariants';

COMMIT;
