BEGIN;

-- V1-FIN-001 is a non-rolling cutover. Old binaries must be drained before
-- this migration because all canonical ledger columns are renamed to carry an
-- explicit micro-CNY unit. Refuse to cross the boundary while an old process
-- could still own a financial operation.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM freeze_idempotency
        WHERE status IN ('frozen', 'settlement_pending')
    ) THEN
        RAISE EXCEPTION 'V1-FIN-001 cannot activate: active billing reservations exist';
    END IF;
    IF EXISTS (
        SELECT 1 FROM billing_provider_attempts
        WHERE state NOT IN ('rejected', 'settled', 'reconciled')
    ) THEN
        RAISE EXCEPTION 'V1-FIN-001 cannot activate: unresolved provider attempts exist';
    END IF;
    IF EXISTS (
        SELECT 1 FROM runtime_terminal_hooks WHERE completed_at IS NULL
    ) THEN
        RAISE EXCEPTION 'V1-FIN-001 cannot activate: pending runtime terminal hooks exist';
    END IF;

    IF EXISTS (
        SELECT 1 FROM model_prices
        WHERE input_price_cents < 0 OR output_price_cents < 0
           OR cache_read_ratio NOT BETWEEN 0 AND 100
           OR cache_creation_ratio NOT BETWEEN 0 AND 100
    ) OR EXISTS (
        SELECT 1 FROM org_model_prices
        WHERE multiplier NOT BETWEEN 0.001 AND 99.999
    ) THEN
        RAISE EXCEPTION 'V1-FIN-001 cannot activate: model price authority is outside the exact supported range';
    END IF;

	IF EXISTS (
		SELECT 1
		FROM model_prices mp
		CROSS JOIN LATERAL (
			SELECT 1::NUMERIC AS multiplier
			UNION ALL
			SELECT omp.multiplier
			FROM org_model_prices omp
			WHERE omp.model_name = mp.model_name
		) effective
		WHERE round(mp.input_price_cents * effective.multiplier * 10000000) > 9223372036854775807
		   OR round(mp.input_price_cents * mp.cache_read_ratio * effective.multiplier * 10000000) > 9223372036854775807
		   OR round(mp.input_price_cents * mp.cache_creation_ratio * effective.multiplier * 10000000) > 9223372036854775807
		   OR round(mp.output_price_cents * effective.multiplier * 10000000) > 9223372036854775807
	) THEN
		RAISE EXCEPTION 'V1-FIN-001 cannot activate: effective model price exceeds micro-CNY int64 range';
	END IF;

    IF EXISTS (
        SELECT 1 FROM project_budget_usage
        WHERE spent_cents > 922337203685477
    ) OR EXISTS (
        SELECT 1 FROM runtime_terminal_hooks
        WHERE actual_cost_cents > 922337203685477
    ) THEN
        RAISE EXCEPTION 'V1-FIN-001 cannot activate: cent amount exceeds micro-CNY int64 range';
    END IF;

    IF EXISTS (
        SELECT 1 FROM freeze_idempotency
        WHERE round(effective_input_price_cents * 10000000) > 9223372036854775807
           OR round(effective_cache_read_price_cents * 10000000) > 9223372036854775807
           OR round(effective_cache_creation_price_cents * 10000000) > 9223372036854775807
           OR round(effective_output_price_cents * 10000000) > 9223372036854775807
           OR round(uncached_input_cost_cents * 10000) > 9223372036854775807
           OR round(cache_read_cost_cents * 10000) > 9223372036854775807
           OR round(cache_creation_cost_cents * 10000) > 9223372036854775807
           OR round(output_cost_cents * 10000) > 9223372036854775807
    ) OR EXISTS (
        SELECT 1 FROM call_logs
        WHERE round(effective_input_price_cents * 10000000) > 9223372036854775807
           OR round(effective_cache_read_price_cents * 10000000) > 9223372036854775807
           OR round(effective_cache_creation_price_cents * 10000000) > 9223372036854775807
           OR round(effective_output_price_cents * 10000000) > 9223372036854775807
           OR round(cache_read_cost_cents * 10000) > 9223372036854775807
           OR round(cache_creation_cost_cents * 10000) > 9223372036854775807
    ) THEN
        RAISE EXCEPTION 'V1-FIN-001 cannot activate: price or component evidence exceeds micro-CNY int64 range';
    END IF;
END;
$$;

-- 000001 inserted the platform organization with an explicit id. Keep the
-- serial authority ahead of every existing organization so the first org
-- created after a fresh V1 install cannot collide with id=1. Never move an
-- already-ahead sequence backwards on upgraded databases.
SELECT setval(
    pg_get_serial_sequence('orgs', 'id'),
    GREATEST(
        COALESCE((SELECT MAX(id) FROM orgs), 1),
        (SELECT last_value FROM orgs_id_seq)
    ),
    TRUE
);

ALTER TABLE model_prices
    ADD CONSTRAINT model_prices_input_price_nonnegative CHECK (input_price_cents >= 0),
    ADD CONSTRAINT model_prices_output_price_nonnegative CHECK (output_price_cents >= 0);
ALTER TABLE org_model_prices
    ADD CONSTRAINT org_model_prices_multiplier_bounds CHECK (multiplier BETWEEN 0.001 AND 99.999);

ALTER TABLE wallets
    RENAME COLUMN balance TO balance_micros;
ALTER TABLE wallets
    RENAME COLUMN granted_balance TO granted_balance_micros;
ALTER TABLE wallets
    RENAME COLUMN frozen TO frozen_micros;
ALTER TABLE wallets
    RENAME COLUMN total_spent TO total_spent_micros;
ALTER TABLE wallets
    ALTER COLUMN balance_micros TYPE BIGINT USING balance_micros::BIGINT * 10000,
    ALTER COLUMN granted_balance_micros TYPE BIGINT USING granted_balance_micros::BIGINT * 10000,
    ALTER COLUMN frozen_micros TYPE BIGINT USING frozen_micros::BIGINT * 10000,
    ALTER COLUMN total_spent_micros TYPE BIGINT USING total_spent_micros::BIGINT * 10000;

ALTER TABLE transactions
    RENAME COLUMN amount TO amount_micros;
ALTER TABLE transactions
    RENAME COLUMN balance_after TO balance_after_micros;
ALTER TABLE transactions
    ALTER COLUMN amount_micros TYPE BIGINT USING amount_micros::BIGINT * 10000,
    ALTER COLUMN balance_after_micros TYPE BIGINT USING balance_after_micros::BIGINT * 10000;

ALTER TABLE grants
    RENAME COLUMN amount TO amount_micros;
ALTER TABLE grants
    RENAME COLUMN balance TO balance_micros;
ALTER TABLE grants
    ALTER COLUMN amount_micros TYPE BIGINT USING amount_micros::BIGINT * 10000,
    ALTER COLUMN balance_micros TYPE BIGINT USING balance_micros::BIGINT * 10000;

ALTER TABLE call_logs
    RENAME COLUMN cost_cents TO cost_micros;
ALTER TABLE call_logs
    RENAME COLUMN effective_input_price_cents TO effective_input_price_micros_per_million;
ALTER TABLE call_logs
    RENAME COLUMN effective_cache_read_price_cents TO effective_cache_read_price_micros_per_million;
ALTER TABLE call_logs
    RENAME COLUMN effective_cache_creation_price_cents TO effective_cache_creation_price_micros_per_million;
ALTER TABLE call_logs
    RENAME COLUMN effective_output_price_cents TO effective_output_price_micros_per_million;
ALTER TABLE call_logs
    RENAME COLUMN cache_read_cost_cents TO cache_read_cost_micros;
ALTER TABLE call_logs
    RENAME COLUMN cache_creation_cost_cents TO cache_creation_cost_micros;
ALTER TABLE call_logs
    ALTER COLUMN cost_micros TYPE BIGINT USING cost_micros::BIGINT * 10000,
    ALTER COLUMN effective_input_price_micros_per_million TYPE BIGINT
        USING round(effective_input_price_micros_per_million * 10000000)::BIGINT,
    ALTER COLUMN effective_cache_read_price_micros_per_million TYPE BIGINT
        USING round(effective_cache_read_price_micros_per_million * 10000000)::BIGINT,
    ALTER COLUMN effective_cache_creation_price_micros_per_million TYPE BIGINT
        USING round(effective_cache_creation_price_micros_per_million * 10000000)::BIGINT,
    ALTER COLUMN effective_output_price_micros_per_million TYPE BIGINT
        USING round(effective_output_price_micros_per_million * 10000000)::BIGINT,
    ALTER COLUMN cache_read_cost_micros TYPE BIGINT
        USING round(cache_read_cost_micros * 10000)::BIGINT,
    ALTER COLUMN cache_creation_cost_micros TYPE BIGINT
        USING round(cache_creation_cost_micros * 10000)::BIGINT;

ALTER TABLE freeze_idempotency
    RENAME COLUMN frozen_amount TO frozen_amount_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN reserved_balance_amount TO reserved_balance_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN reserved_granted_amount TO reserved_granted_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN min_freeze_amount TO min_freeze_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN effective_input_price_cents TO effective_input_price_micros_per_million;
ALTER TABLE freeze_idempotency
    RENAME COLUMN effective_cache_read_price_cents TO effective_cache_read_price_micros_per_million;
ALTER TABLE freeze_idempotency
    RENAME COLUMN effective_cache_creation_price_cents TO effective_cache_creation_price_micros_per_million;
ALTER TABLE freeze_idempotency
    RENAME COLUMN effective_output_price_cents TO effective_output_price_micros_per_million;
ALTER TABLE freeze_idempotency
    RENAME COLUMN calculated_cost TO calculated_cost_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN charged_cost TO charged_cost_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN uncovered_cost TO uncovered_cost_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN topup_amount TO topup_amount_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN refund_amount TO refund_amount_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN forfeited_grant_amount TO forfeited_grant_amount_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN uncached_input_cost_cents TO uncached_input_cost_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN cache_read_cost_cents TO cache_read_cost_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN cache_creation_cost_cents TO cache_creation_cost_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN output_cost_cents TO output_cost_micros;
ALTER TABLE freeze_idempotency
    RENAME COLUMN balance_after TO balance_after_micros;
ALTER TABLE freeze_idempotency
    ALTER COLUMN frozen_amount_micros TYPE BIGINT USING frozen_amount_micros::BIGINT * 10000,
    ALTER COLUMN reserved_balance_micros TYPE BIGINT USING reserved_balance_micros::BIGINT * 10000,
    ALTER COLUMN reserved_granted_micros TYPE BIGINT USING reserved_granted_micros::BIGINT * 10000,
    ALTER COLUMN min_freeze_micros TYPE BIGINT USING min_freeze_micros::BIGINT * 10000,
    ALTER COLUMN effective_input_price_micros_per_million TYPE BIGINT
        USING round(effective_input_price_micros_per_million * 10000000)::BIGINT,
    ALTER COLUMN effective_cache_read_price_micros_per_million TYPE BIGINT
        USING round(effective_cache_read_price_micros_per_million * 10000000)::BIGINT,
    ALTER COLUMN effective_cache_creation_price_micros_per_million TYPE BIGINT
        USING round(effective_cache_creation_price_micros_per_million * 10000000)::BIGINT,
    ALTER COLUMN effective_output_price_micros_per_million TYPE BIGINT
        USING round(effective_output_price_micros_per_million * 10000000)::BIGINT,
    ALTER COLUMN calculated_cost_micros TYPE BIGINT USING calculated_cost_micros::BIGINT * 10000,
    ALTER COLUMN charged_cost_micros TYPE BIGINT USING charged_cost_micros::BIGINT * 10000,
    ALTER COLUMN uncovered_cost_micros TYPE BIGINT USING uncovered_cost_micros::BIGINT * 10000,
    ALTER COLUMN topup_amount_micros TYPE BIGINT USING topup_amount_micros::BIGINT * 10000,
    ALTER COLUMN refund_amount_micros TYPE BIGINT USING refund_amount_micros::BIGINT * 10000,
    ALTER COLUMN forfeited_grant_amount_micros TYPE BIGINT USING forfeited_grant_amount_micros::BIGINT * 10000,
    ALTER COLUMN uncached_input_cost_micros TYPE BIGINT USING round(uncached_input_cost_micros * 10000)::BIGINT,
    ALTER COLUMN cache_read_cost_micros TYPE BIGINT USING round(cache_read_cost_micros * 10000)::BIGINT,
    ALTER COLUMN cache_creation_cost_micros TYPE BIGINT USING round(cache_creation_cost_micros * 10000)::BIGINT,
    ALTER COLUMN output_cost_micros TYPE BIGINT USING round(output_cost_micros * 10000)::BIGINT,
    ALTER COLUMN balance_after_micros TYPE BIGINT USING balance_after_micros::BIGINT * 10000;

ALTER TABLE billing_reservation_grant_allocations
    RENAME COLUMN reserved_amount TO reserved_micros;
ALTER TABLE billing_reservation_grant_allocations
    RENAME COLUMN consumed_amount TO consumed_micros;
ALTER TABLE billing_reservation_grant_allocations
    RENAME COLUMN refunded_amount TO refunded_micros;
ALTER TABLE billing_reservation_grant_allocations
    ALTER COLUMN reserved_micros TYPE BIGINT USING reserved_micros::BIGINT * 10000,
    ALTER COLUMN consumed_micros TYPE BIGINT USING consumed_micros::BIGINT * 10000,
    ALTER COLUMN refunded_micros TYPE BIGINT USING refunded_micros::BIGINT * 10000;

ALTER TABLE usage_finalization_journal
    RENAME COLUMN calculated_cost TO calculated_cost_micros;
ALTER TABLE usage_finalization_journal
    RENAME COLUMN charged_cost TO charged_cost_micros;
ALTER TABLE usage_finalization_journal
    ALTER COLUMN calculated_cost_micros TYPE BIGINT USING calculated_cost_micros::BIGINT * 10000,
    ALTER COLUMN charged_cost_micros TYPE BIGINT USING charged_cost_micros::BIGINT * 10000;

ALTER TABLE project_budget_usage
    RENAME COLUMN spent_cents TO spent_micros;
ALTER TABLE project_budget_usage
    ALTER COLUMN spent_micros TYPE BIGINT USING spent_micros * 10000;

ALTER TABLE runtime_terminal_hooks
    RENAME COLUMN actual_cost_cents TO actual_cost_micros;
ALTER TABLE runtime_terminal_hooks
    ALTER COLUMN actual_cost_micros TYPE BIGINT USING actual_cost_micros * 10000;

ALTER TABLE billing_provider_attempts
    RENAME COLUMN calculated_cost TO calculated_cost_micros;
ALTER TABLE billing_provider_attempts
    RENAME COLUMN charged_cost TO charged_cost_micros;
ALTER TABLE billing_provider_attempts
    ALTER COLUMN calculated_cost_micros TYPE BIGINT USING calculated_cost_micros::BIGINT * 10000,
    ALTER COLUMN charged_cost_micros TYPE BIGINT USING charged_cost_micros::BIGINT * 10000;

ALTER TABLE billing_reservation_extensions
    RENAME COLUMN reserved_amount TO reserved_micros;
ALTER TABLE billing_reservation_extensions
    RENAME COLUMN reserved_balance_amount TO reserved_balance_micros;
ALTER TABLE billing_reservation_extensions
    RENAME COLUMN reserved_granted_amount TO reserved_granted_micros;
ALTER TABLE billing_reservation_extensions
    ALTER COLUMN reserved_micros TYPE BIGINT USING reserved_micros::BIGINT * 10000,
    ALTER COLUMN reserved_balance_micros TYPE BIGINT USING reserved_balance_micros::BIGINT * 10000,
    ALTER COLUMN reserved_granted_micros TYPE BIGINT USING reserved_granted_micros::BIGINT * 10000;

INSERT INTO settings (key, value, description, updated_at) VALUES
    ('ledger.version', '"micro-cny.v1"', 'Canonical customer ledger contract', NOW()),
    ('ledger.amount_scale', '1000000', 'Canonical micro-CNY units per CNY', NOW())
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    description = EXCLUDED.description,
    updated_at = NOW();

COMMENT ON COLUMN wallets.balance_micros IS 'Authoritative available cash balance in micro-CNY';
COMMENT ON COLUMN call_logs.cost_micros IS 'Authoritative terminal customer charge in micro-CNY';
COMMENT ON COLUMN freeze_idempotency.effective_input_price_micros_per_million IS
    'Immutable effective input price in micro-CNY per 1,000,000 tokens';

-- A database-owned marker makes the rollback boundary independent of outbox
-- retention. Triggers are installed only after the conversion, so historical
-- rows do not count as new micro-ledger evidence; any later ledger mutation
-- makes downgrade refuse and forces a roll-forward recovery.
CREATE TABLE micro_ledger_runtime_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    micro_evidence_written BOOLEAN NOT NULL DEFAULT FALSE,
    first_evidence_at TIMESTAMPTZ
);
INSERT INTO micro_ledger_runtime_state (singleton) VALUES (TRUE);

CREATE FUNCTION mark_micro_ledger_runtime_evidence()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE micro_ledger_runtime_state
    SET micro_evidence_written = TRUE,
        first_evidence_at = COALESCE(first_evidence_at, NOW())
    WHERE singleton = TRUE;
    RETURN NULL;
END;
$$;

CREATE TRIGGER wallets_micro_ledger_evidence
AFTER INSERT OR UPDATE OR DELETE ON wallets
FOR EACH STATEMENT EXECUTE FUNCTION mark_micro_ledger_runtime_evidence();
CREATE TRIGGER transactions_micro_ledger_evidence
AFTER INSERT OR UPDATE OR DELETE ON transactions
FOR EACH STATEMENT EXECUTE FUNCTION mark_micro_ledger_runtime_evidence();
CREATE TRIGGER grants_micro_ledger_evidence
AFTER INSERT OR UPDATE OR DELETE ON grants
FOR EACH STATEMENT EXECUTE FUNCTION mark_micro_ledger_runtime_evidence();
CREATE TRIGGER call_logs_micro_ledger_evidence
AFTER INSERT OR UPDATE OR DELETE ON call_logs
FOR EACH STATEMENT EXECUTE FUNCTION mark_micro_ledger_runtime_evidence();
CREATE TRIGGER freeze_idempotency_micro_ledger_evidence
AFTER INSERT OR UPDATE OR DELETE ON freeze_idempotency
FOR EACH STATEMENT EXECUTE FUNCTION mark_micro_ledger_runtime_evidence();
CREATE TRIGGER billing_reservation_grants_micro_ledger_evidence
AFTER INSERT OR UPDATE OR DELETE ON billing_reservation_grant_allocations
FOR EACH STATEMENT EXECUTE FUNCTION mark_micro_ledger_runtime_evidence();
CREATE TRIGGER usage_journal_micro_ledger_evidence
AFTER INSERT OR UPDATE OR DELETE ON usage_finalization_journal
FOR EACH STATEMENT EXECUTE FUNCTION mark_micro_ledger_runtime_evidence();
CREATE TRIGGER project_budget_usage_micro_ledger_evidence
AFTER INSERT OR UPDATE OR DELETE ON project_budget_usage
FOR EACH STATEMENT EXECUTE FUNCTION mark_micro_ledger_runtime_evidence();
CREATE TRIGGER runtime_terminal_hooks_micro_ledger_evidence
AFTER INSERT OR UPDATE OR DELETE ON runtime_terminal_hooks
FOR EACH STATEMENT EXECUTE FUNCTION mark_micro_ledger_runtime_evidence();
CREATE TRIGGER billing_provider_attempts_micro_ledger_evidence
AFTER INSERT OR UPDATE OR DELETE ON billing_provider_attempts
FOR EACH STATEMENT EXECUTE FUNCTION mark_micro_ledger_runtime_evidence();
CREATE TRIGGER billing_reservation_extensions_micro_ledger_evidence
AFTER INSERT OR UPDATE OR DELETE ON billing_reservation_extensions
FOR EACH STATEMENT EXECUTE FUNCTION mark_micro_ledger_runtime_evidence();

COMMIT;
