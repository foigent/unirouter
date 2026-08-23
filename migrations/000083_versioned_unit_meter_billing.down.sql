BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000083_versioned_unit_meter_billing'
    ) THEN
        RAISE EXCEPTION 'Unit meter billing rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (
        SELECT 1 FROM enterprise_finance_rate_source_evidence
        WHERE unit_contract = 'versioned_rate_card_components.v1'
    ) THEN
        RAISE EXCEPTION 'Unit meter billing rollback refused: versioned component source evidence exists';
    END IF;
    IF EXISTS (
        SELECT 1 FROM freeze_idempotency
        WHERE effective_unit_rates <> '[]'::jsonb
           OR reserved_meter_usage <> '[]'::jsonb
           OR meter_usage <> '[]'::jsonb
           OR non_token_cost_micros <> 0
    ) OR EXISTS (
        SELECT 1 FROM billing_observation_receipts
        WHERE effective_unit_rates <> '[]'::jsonb
           OR meter_usage <> '[]'::jsonb
           OR non_token_cost_micros <> 0
    ) OR EXISTS (
        SELECT 1 FROM usage_finalization_journal
        WHERE effective_unit_rates <> '[]'::jsonb
           OR meter_usage <> '[]'::jsonb
           OR non_token_cost_micros <> 0
    ) OR EXISTS (
        SELECT 1 FROM call_logs
        WHERE effective_unit_rates <> '[]'::jsonb
           OR meter_usage <> '[]'::jsonb
           OR non_token_cost_micros <> 0
    ) THEN
        RAISE EXCEPTION 'Unit meter billing rollback refused: durable meter evidence exists';
    END IF;
END;
$$;

ALTER TABLE enterprise_finance_rate_source_evidence
    DROP CONSTRAINT enterprise_finance_rate_source_unit_check,
    ADD CONSTRAINT enterprise_finance_rate_source_unit_check
        CHECK (unit_contract = 'tokens');

ALTER TABLE call_logs
    DROP CONSTRAINT call_logs_non_token_cost_nonnegative,
    DROP CONSTRAINT call_logs_meter_usage_array,
    DROP CONSTRAINT call_logs_effective_unit_rates_array,
    DROP COLUMN non_token_cost_micros,
    DROP COLUMN meter_usage,
    DROP COLUMN effective_unit_rates;

ALTER TABLE usage_finalization_journal
    DROP CONSTRAINT usage_finalization_non_token_cost_nonnegative,
    DROP CONSTRAINT usage_finalization_meter_usage_array,
    DROP CONSTRAINT usage_finalization_effective_unit_rates_array,
    DROP COLUMN non_token_cost_micros,
    DROP COLUMN meter_usage,
    DROP COLUMN effective_unit_rates;

ALTER TABLE billing_observation_receipts
    DROP CONSTRAINT billing_observation_non_token_cost_nonnegative,
    DROP CONSTRAINT billing_observation_meter_usage_array,
    DROP CONSTRAINT billing_observation_effective_unit_rates_array,
    DROP COLUMN non_token_cost_micros,
    DROP COLUMN meter_usage,
    DROP COLUMN effective_unit_rates;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_non_token_cost_nonnegative,
    DROP CONSTRAINT freeze_idempotency_meter_usage_array,
    DROP CONSTRAINT freeze_idempotency_reserved_meter_usage_array,
    DROP CONSTRAINT freeze_idempotency_effective_unit_rates_array,
    DROP COLUMN non_token_cost_micros,
    DROP COLUMN meter_usage,
    DROP COLUMN reserved_meter_usage,
    DROP COLUMN effective_unit_rates;

DELETE FROM schema_migrations
WHERE version = '000083_versioned_unit_meter_billing';

COMMIT;
