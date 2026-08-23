BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE freeze_idempotency, billing_observation_receipts,
    usage_finalization_journal, call_logs,
    enterprise_finance_rate_source_evidence, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- Official imports remain review-only drafts, but their source evidence must
-- distinguish token-only payloads from payloads that also contain typed
-- per-call meters. The Rate Card components themselves remain the absolute,
-- versioned price authority.
ALTER TABLE enterprise_finance_rate_source_evidence
    DROP CONSTRAINT enterprise_finance_rate_source_unit_check,
    ADD CONSTRAINT enterprise_finance_rate_source_unit_check CHECK (
        unit_contract IN ('tokens', 'versioned_rate_card_components.v1')
    );

-- Non-token charges are frozen as exact Rate Card rationals. Token rates keep
-- their dedicated integer columns because their prompt partition and million-
-- token basis are separate contracts.
ALTER TABLE freeze_idempotency
    ADD COLUMN effective_unit_rates JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN reserved_meter_usage JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN meter_usage JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN non_token_cost_micros BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT freeze_idempotency_effective_unit_rates_array
        CHECK (jsonb_typeof(effective_unit_rates) = 'array'),
    ADD CONSTRAINT freeze_idempotency_reserved_meter_usage_array
        CHECK (jsonb_typeof(reserved_meter_usage) = 'array'),
    ADD CONSTRAINT freeze_idempotency_meter_usage_array
        CHECK (jsonb_typeof(meter_usage) = 'array'),
    ADD CONSTRAINT freeze_idempotency_non_token_cost_nonnegative
        CHECK (non_token_cost_micros >= 0);

ALTER TABLE billing_observation_receipts
    ADD COLUMN effective_unit_rates JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN meter_usage JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN non_token_cost_micros BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT billing_observation_effective_unit_rates_array
        CHECK (jsonb_typeof(effective_unit_rates) = 'array'),
    ADD CONSTRAINT billing_observation_meter_usage_array
        CHECK (jsonb_typeof(meter_usage) = 'array'),
    ADD CONSTRAINT billing_observation_non_token_cost_nonnegative
        CHECK (non_token_cost_micros >= 0);

ALTER TABLE usage_finalization_journal
    ADD COLUMN effective_unit_rates JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN meter_usage JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN non_token_cost_micros BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT usage_finalization_effective_unit_rates_array
        CHECK (jsonb_typeof(effective_unit_rates) = 'array'),
    ADD CONSTRAINT usage_finalization_meter_usage_array
        CHECK (jsonb_typeof(meter_usage) = 'array'),
    ADD CONSTRAINT usage_finalization_non_token_cost_nonnegative
        CHECK (non_token_cost_micros >= 0);

ALTER TABLE call_logs
    ADD COLUMN effective_unit_rates JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN meter_usage JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN non_token_cost_micros BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT call_logs_effective_unit_rates_array
        CHECK (jsonb_typeof(effective_unit_rates) = 'array'),
    ADD CONSTRAINT call_logs_meter_usage_array
        CHECK (jsonb_typeof(meter_usage) = 'array'),
    ADD CONSTRAINT call_logs_non_token_cost_nonnegative
        CHECK (non_token_cost_micros >= 0);

COMMENT ON COLUMN freeze_idempotency.effective_unit_rates IS
    'Exact content-addressed non-token Rate Card rationals frozen before dispatch';
COMMENT ON COLUMN freeze_idempotency.reserved_meter_usage IS
    'Conservative maximum non-token quantities admitted before dispatch';
COMMENT ON COLUMN freeze_idempotency.meter_usage IS
    'Canonical provider-evidenced non-token quantities used at settlement';
COMMENT ON COLUMN freeze_idempotency.non_token_cost_micros IS
    'Exact aggregate non-token cost included in calculated_cost_micros';

COMMIT;
