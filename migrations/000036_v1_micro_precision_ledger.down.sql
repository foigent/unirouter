BEGIN;

ALTER TABLE model_prices
    DROP CONSTRAINT model_prices_input_price_nonnegative,
    DROP CONSTRAINT model_prices_output_price_nonnegative;
ALTER TABLE org_model_prices
    DROP CONSTRAINT org_model_prices_multiplier_bounds;

-- Downgrade is intentionally lossless-only. Once any micro-CNY value cannot be
-- represented in whole cents, or a v2 durable event exists, recovery must roll
-- forward with the micro ledger instead of rounding customer funds.
DO $$
DECLARE
    non_cent BOOLEAN;
BEGIN
    SELECT EXISTS (
        SELECT 1 FROM wallets
        WHERE balance_micros % 10000 <> 0
           OR granted_balance_micros % 10000 <> 0
           OR frozen_micros % 10000 <> 0
           OR total_spent_micros % 10000 <> 0
        UNION ALL
        SELECT 1 FROM transactions
        WHERE amount_micros % 10000 <> 0
           OR balance_after_micros % 10000 <> 0
        UNION ALL
        SELECT 1 FROM grants
        WHERE amount_micros % 10000 <> 0 OR balance_micros % 10000 <> 0
        UNION ALL
        SELECT 1 FROM call_logs WHERE cost_micros % 10000 <> 0
        UNION ALL
        SELECT 1 FROM freeze_idempotency
        WHERE frozen_amount_micros % 10000 <> 0
           OR reserved_balance_micros % 10000 <> 0
           OR reserved_granted_micros % 10000 <> 0
           OR min_freeze_micros % 10000 <> 0
           OR calculated_cost_micros % 10000 <> 0
           OR charged_cost_micros % 10000 <> 0
           OR uncovered_cost_micros % 10000 <> 0
           OR topup_amount_micros % 10000 <> 0
           OR refund_amount_micros % 10000 <> 0
           OR forfeited_grant_amount_micros % 10000 <> 0
           OR balance_after_micros % 10000 <> 0
        UNION ALL
        SELECT 1 FROM billing_reservation_grant_allocations
        WHERE reserved_micros % 10000 <> 0
           OR consumed_micros % 10000 <> 0
           OR refunded_micros % 10000 <> 0
        UNION ALL
        SELECT 1 FROM usage_finalization_journal
        WHERE calculated_cost_micros % 10000 <> 0 OR charged_cost_micros % 10000 <> 0
        UNION ALL
        SELECT 1 FROM project_budget_usage WHERE spent_micros % 10000 <> 0
        UNION ALL
        SELECT 1 FROM runtime_terminal_hooks WHERE actual_cost_micros % 10000 <> 0
        UNION ALL
        SELECT 1 FROM billing_provider_attempts
        WHERE calculated_cost_micros % 10000 <> 0 OR charged_cost_micros % 10000 <> 0
        UNION ALL
        SELECT 1 FROM billing_reservation_extensions
        WHERE reserved_micros % 10000 <> 0
           OR reserved_balance_micros % 10000 <> 0
           OR reserved_granted_micros % 10000 <> 0
    ) INTO non_cent;

    IF non_cent THEN
        RAISE EXCEPTION 'V1-FIN-001 downgrade refused: non-cent micro-CNY evidence exists';
    END IF;
    IF EXISTS (
        SELECT 1 FROM micro_ledger_runtime_state WHERE micro_evidence_written
    ) THEN
        RAISE EXCEPTION 'V1-FIN-001 downgrade refused: post-activation micro-ledger evidence exists';
    END IF;
    IF EXISTS (
        SELECT 1 FROM freeze_idempotency
        WHERE effective_input_price_micros_per_million % 10 <> 0
           OR effective_cache_read_price_micros_per_million % 10 <> 0
           OR effective_cache_creation_price_micros_per_million % 10 <> 0
           OR effective_output_price_micros_per_million % 10 <> 0
        UNION ALL
        SELECT 1 FROM call_logs
        WHERE effective_input_price_micros_per_million % 10 <> 0
           OR effective_cache_read_price_micros_per_million % 10 <> 0
           OR effective_cache_creation_price_micros_per_million % 10 <> 0
           OR effective_output_price_micros_per_million % 10 <> 0
    ) THEN
        RAISE EXCEPTION 'V1-FIN-001 downgrade refused: effective price is not representable in NUMERIC(18,6) cents/K';
    END IF;
    IF EXISTS (
        SELECT 1 FROM internal_outbox_events
        WHERE event_type IN (
            'billing.usage.prepared.v2', 'billing.settled.v2',
            'billing.cancelled.v2', 'evidence.call_log.v2', 'runtime.terminal.v2'
        ) OR payload->>'money_schema' = 'micro-cny.v1'
    ) THEN
        RAISE EXCEPTION 'V1-FIN-001 downgrade refused: v2 durable money event exists';
    END IF;
END;
$$;

DROP TRIGGER billing_reservation_extensions_micro_ledger_evidence ON billing_reservation_extensions;
DROP TRIGGER billing_provider_attempts_micro_ledger_evidence ON billing_provider_attempts;
DROP TRIGGER runtime_terminal_hooks_micro_ledger_evidence ON runtime_terminal_hooks;
DROP TRIGGER project_budget_usage_micro_ledger_evidence ON project_budget_usage;
DROP TRIGGER usage_journal_micro_ledger_evidence ON usage_finalization_journal;
DROP TRIGGER billing_reservation_grants_micro_ledger_evidence ON billing_reservation_grant_allocations;
DROP TRIGGER freeze_idempotency_micro_ledger_evidence ON freeze_idempotency;
DROP TRIGGER call_logs_micro_ledger_evidence ON call_logs;
DROP TRIGGER grants_micro_ledger_evidence ON grants;
DROP TRIGGER transactions_micro_ledger_evidence ON transactions;
DROP TRIGGER wallets_micro_ledger_evidence ON wallets;
DROP FUNCTION mark_micro_ledger_runtime_evidence();
DROP TABLE micro_ledger_runtime_state;

ALTER TABLE billing_reservation_extensions
    ALTER COLUMN reserved_micros TYPE INTEGER USING (reserved_micros / 10000)::INTEGER,
    ALTER COLUMN reserved_balance_micros TYPE INTEGER USING (reserved_balance_micros / 10000)::INTEGER,
    ALTER COLUMN reserved_granted_micros TYPE INTEGER USING (reserved_granted_micros / 10000)::INTEGER;
ALTER TABLE billing_reservation_extensions RENAME COLUMN reserved_micros TO reserved_amount;
ALTER TABLE billing_reservation_extensions RENAME COLUMN reserved_balance_micros TO reserved_balance_amount;
ALTER TABLE billing_reservation_extensions RENAME COLUMN reserved_granted_micros TO reserved_granted_amount;

ALTER TABLE billing_provider_attempts
    ALTER COLUMN calculated_cost_micros TYPE INTEGER USING (calculated_cost_micros / 10000)::INTEGER,
    ALTER COLUMN charged_cost_micros TYPE INTEGER USING (charged_cost_micros / 10000)::INTEGER;
ALTER TABLE billing_provider_attempts RENAME COLUMN calculated_cost_micros TO calculated_cost;
ALTER TABLE billing_provider_attempts RENAME COLUMN charged_cost_micros TO charged_cost;

ALTER TABLE runtime_terminal_hooks
    ALTER COLUMN actual_cost_micros TYPE BIGINT USING actual_cost_micros / 10000;
ALTER TABLE runtime_terminal_hooks RENAME COLUMN actual_cost_micros TO actual_cost_cents;

ALTER TABLE project_budget_usage
    ALTER COLUMN spent_micros TYPE BIGINT USING spent_micros / 10000;
ALTER TABLE project_budget_usage RENAME COLUMN spent_micros TO spent_cents;

ALTER TABLE usage_finalization_journal
    ALTER COLUMN calculated_cost_micros TYPE INTEGER USING (calculated_cost_micros / 10000)::INTEGER,
    ALTER COLUMN charged_cost_micros TYPE INTEGER USING (charged_cost_micros / 10000)::INTEGER;
ALTER TABLE usage_finalization_journal RENAME COLUMN calculated_cost_micros TO calculated_cost;
ALTER TABLE usage_finalization_journal RENAME COLUMN charged_cost_micros TO charged_cost;

ALTER TABLE billing_reservation_grant_allocations
    ALTER COLUMN reserved_micros TYPE INTEGER USING (reserved_micros / 10000)::INTEGER,
    ALTER COLUMN consumed_micros TYPE INTEGER USING (consumed_micros / 10000)::INTEGER,
    ALTER COLUMN refunded_micros TYPE INTEGER USING (refunded_micros / 10000)::INTEGER;
ALTER TABLE billing_reservation_grant_allocations RENAME COLUMN reserved_micros TO reserved_amount;
ALTER TABLE billing_reservation_grant_allocations RENAME COLUMN consumed_micros TO consumed_amount;
ALTER TABLE billing_reservation_grant_allocations RENAME COLUMN refunded_micros TO refunded_amount;

ALTER TABLE freeze_idempotency
    ALTER COLUMN frozen_amount_micros TYPE INTEGER USING (frozen_amount_micros / 10000)::INTEGER,
    ALTER COLUMN reserved_balance_micros TYPE INTEGER USING (reserved_balance_micros / 10000)::INTEGER,
    ALTER COLUMN reserved_granted_micros TYPE INTEGER USING (reserved_granted_micros / 10000)::INTEGER,
    ALTER COLUMN min_freeze_micros TYPE INTEGER USING (min_freeze_micros / 10000)::INTEGER,
    ALTER COLUMN effective_input_price_micros_per_million TYPE NUMERIC(18,6)
        USING effective_input_price_micros_per_million::NUMERIC / 10000000,
    ALTER COLUMN effective_cache_read_price_micros_per_million TYPE NUMERIC(18,6)
        USING effective_cache_read_price_micros_per_million::NUMERIC / 10000000,
    ALTER COLUMN effective_cache_creation_price_micros_per_million TYPE NUMERIC(18,6)
        USING effective_cache_creation_price_micros_per_million::NUMERIC / 10000000,
    ALTER COLUMN effective_output_price_micros_per_million TYPE NUMERIC(18,6)
        USING effective_output_price_micros_per_million::NUMERIC / 10000000,
    ALTER COLUMN calculated_cost_micros TYPE INTEGER USING (calculated_cost_micros / 10000)::INTEGER,
    ALTER COLUMN charged_cost_micros TYPE INTEGER USING (charged_cost_micros / 10000)::INTEGER,
    ALTER COLUMN uncovered_cost_micros TYPE INTEGER USING (uncovered_cost_micros / 10000)::INTEGER,
    ALTER COLUMN topup_amount_micros TYPE INTEGER USING (topup_amount_micros / 10000)::INTEGER,
    ALTER COLUMN refund_amount_micros TYPE INTEGER USING (refund_amount_micros / 10000)::INTEGER,
    ALTER COLUMN forfeited_grant_amount_micros TYPE INTEGER USING (forfeited_grant_amount_micros / 10000)::INTEGER,
    ALTER COLUMN uncached_input_cost_micros TYPE NUMERIC(18,6) USING uncached_input_cost_micros::NUMERIC / 10000,
    ALTER COLUMN cache_read_cost_micros TYPE NUMERIC(18,6) USING cache_read_cost_micros::NUMERIC / 10000,
    ALTER COLUMN cache_creation_cost_micros TYPE NUMERIC(18,6) USING cache_creation_cost_micros::NUMERIC / 10000,
    ALTER COLUMN output_cost_micros TYPE NUMERIC(18,6) USING output_cost_micros::NUMERIC / 10000,
    ALTER COLUMN balance_after_micros TYPE INTEGER USING (balance_after_micros / 10000)::INTEGER;
ALTER TABLE freeze_idempotency RENAME COLUMN frozen_amount_micros TO frozen_amount;
ALTER TABLE freeze_idempotency RENAME COLUMN reserved_balance_micros TO reserved_balance_amount;
ALTER TABLE freeze_idempotency RENAME COLUMN reserved_granted_micros TO reserved_granted_amount;
ALTER TABLE freeze_idempotency RENAME COLUMN min_freeze_micros TO min_freeze_amount;
ALTER TABLE freeze_idempotency RENAME COLUMN effective_input_price_micros_per_million TO effective_input_price_cents;
ALTER TABLE freeze_idempotency RENAME COLUMN effective_cache_read_price_micros_per_million TO effective_cache_read_price_cents;
ALTER TABLE freeze_idempotency RENAME COLUMN effective_cache_creation_price_micros_per_million TO effective_cache_creation_price_cents;
ALTER TABLE freeze_idempotency RENAME COLUMN effective_output_price_micros_per_million TO effective_output_price_cents;
ALTER TABLE freeze_idempotency RENAME COLUMN calculated_cost_micros TO calculated_cost;
ALTER TABLE freeze_idempotency RENAME COLUMN charged_cost_micros TO charged_cost;
ALTER TABLE freeze_idempotency RENAME COLUMN uncovered_cost_micros TO uncovered_cost;
ALTER TABLE freeze_idempotency RENAME COLUMN topup_amount_micros TO topup_amount;
ALTER TABLE freeze_idempotency RENAME COLUMN refund_amount_micros TO refund_amount;
ALTER TABLE freeze_idempotency RENAME COLUMN forfeited_grant_amount_micros TO forfeited_grant_amount;
ALTER TABLE freeze_idempotency RENAME COLUMN uncached_input_cost_micros TO uncached_input_cost_cents;
ALTER TABLE freeze_idempotency RENAME COLUMN cache_read_cost_micros TO cache_read_cost_cents;
ALTER TABLE freeze_idempotency RENAME COLUMN cache_creation_cost_micros TO cache_creation_cost_cents;
ALTER TABLE freeze_idempotency RENAME COLUMN output_cost_micros TO output_cost_cents;
ALTER TABLE freeze_idempotency RENAME COLUMN balance_after_micros TO balance_after;

ALTER TABLE call_logs
    ALTER COLUMN cost_micros TYPE INTEGER USING (cost_micros / 10000)::INTEGER,
    ALTER COLUMN effective_input_price_micros_per_million TYPE NUMERIC(18,6)
        USING effective_input_price_micros_per_million::NUMERIC / 10000000,
    ALTER COLUMN effective_cache_read_price_micros_per_million TYPE NUMERIC(18,6)
        USING effective_cache_read_price_micros_per_million::NUMERIC / 10000000,
    ALTER COLUMN effective_cache_creation_price_micros_per_million TYPE NUMERIC(18,6)
        USING effective_cache_creation_price_micros_per_million::NUMERIC / 10000000,
    ALTER COLUMN effective_output_price_micros_per_million TYPE NUMERIC(18,6)
        USING effective_output_price_micros_per_million::NUMERIC / 10000000,
    ALTER COLUMN cache_read_cost_micros TYPE NUMERIC(18,6) USING cache_read_cost_micros::NUMERIC / 10000,
    ALTER COLUMN cache_creation_cost_micros TYPE NUMERIC(18,6) USING cache_creation_cost_micros::NUMERIC / 10000;
ALTER TABLE call_logs RENAME COLUMN cost_micros TO cost_cents;
ALTER TABLE call_logs RENAME COLUMN effective_input_price_micros_per_million TO effective_input_price_cents;
ALTER TABLE call_logs RENAME COLUMN effective_cache_read_price_micros_per_million TO effective_cache_read_price_cents;
ALTER TABLE call_logs RENAME COLUMN effective_cache_creation_price_micros_per_million TO effective_cache_creation_price_cents;
ALTER TABLE call_logs RENAME COLUMN effective_output_price_micros_per_million TO effective_output_price_cents;
ALTER TABLE call_logs RENAME COLUMN cache_read_cost_micros TO cache_read_cost_cents;
ALTER TABLE call_logs RENAME COLUMN cache_creation_cost_micros TO cache_creation_cost_cents;

ALTER TABLE grants
    ALTER COLUMN amount_micros TYPE INTEGER USING (amount_micros / 10000)::INTEGER,
    ALTER COLUMN balance_micros TYPE INTEGER USING (balance_micros / 10000)::INTEGER;
ALTER TABLE grants RENAME COLUMN amount_micros TO amount;
ALTER TABLE grants RENAME COLUMN balance_micros TO balance;

ALTER TABLE transactions
    ALTER COLUMN amount_micros TYPE INTEGER USING (amount_micros / 10000)::INTEGER,
    ALTER COLUMN balance_after_micros TYPE INTEGER USING (balance_after_micros / 10000)::INTEGER;
ALTER TABLE transactions RENAME COLUMN amount_micros TO amount;
ALTER TABLE transactions RENAME COLUMN balance_after_micros TO balance_after;

ALTER TABLE wallets
    ALTER COLUMN balance_micros TYPE INTEGER USING (balance_micros / 10000)::INTEGER,
    ALTER COLUMN granted_balance_micros TYPE INTEGER USING (granted_balance_micros / 10000)::INTEGER,
    ALTER COLUMN frozen_micros TYPE INTEGER USING (frozen_micros / 10000)::INTEGER,
    ALTER COLUMN total_spent_micros TYPE INTEGER USING (total_spent_micros / 10000)::INTEGER;
ALTER TABLE wallets RENAME COLUMN balance_micros TO balance;
ALTER TABLE wallets RENAME COLUMN granted_balance_micros TO granted_balance;
ALTER TABLE wallets RENAME COLUMN frozen_micros TO frozen;
ALTER TABLE wallets RENAME COLUMN total_spent_micros TO total_spent;

DELETE FROM settings WHERE key IN ('ledger.version', 'ledger.amount_scale');
DELETE FROM schema_migrations WHERE version = '000036_v1_micro_precision_ledger';

COMMIT;
