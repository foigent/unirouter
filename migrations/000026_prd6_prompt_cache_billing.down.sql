BEGIN;

-- A pv2 reservation has frozen cache-aware financial semantics. Refuse an
-- older schema/binary combination after activation and require roll-forward.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM model_prices
        WHERE cache_read_ratio <> 1 OR cache_creation_ratio <> 1
    ) OR EXISTS (
        SELECT 1 FROM freeze_idempotency
        WHERE pricing_version LIKE 'pv2_%'
           OR cache_read_tokens <> 0
           OR cache_creation_tokens <> 0
           OR cache_usage_schema <> ''
    ) OR EXISTS (
        SELECT 1 FROM call_logs
        WHERE cache_read_tokens <> 0
           OR cache_creation_tokens <> 0
           OR cache_usage_schema <> ''
           OR pricing_version LIKE 'pv2_%'
    ) THEN
        RAISE EXCEPTION 'P6-F09 down refused: activated cache-aware billing state exists; retain the schema and roll forward';
    END IF;
END;
$$;

DROP INDEX IF EXISTS idx_call_logs_cache_usage_created;

ALTER TABLE call_logs
    DROP CONSTRAINT IF EXISTS call_logs_cache_usage_nonnegative,
    DROP CONSTRAINT IF EXISTS call_logs_prompt_partition,
    DROP CONSTRAINT IF EXISTS call_logs_cache_prices_nonnegative,
    DROP COLUMN IF EXISTS cache_creation_cost_cents,
    DROP COLUMN IF EXISTS cache_read_cost_cents,
    DROP COLUMN IF EXISTS effective_output_price_cents,
    DROP COLUMN IF EXISTS effective_cache_creation_price_cents,
    DROP COLUMN IF EXISTS effective_cache_read_price_cents,
    DROP COLUMN IF EXISTS effective_input_price_cents,
    DROP COLUMN IF EXISTS pricing_version,
    DROP COLUMN IF EXISTS cache_usage_status,
    DROP COLUMN IF EXISTS cache_usage_schema,
    DROP COLUMN IF EXISTS cache_creation_tokens,
    DROP COLUMN IF EXISTS cache_read_tokens,
    DROP COLUMN IF EXISTS uncached_prompt_tokens;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT IF EXISTS freeze_idempotency_cache_usage_nonnegative,
    DROP CONSTRAINT IF EXISTS freeze_idempotency_prompt_partition,
    DROP CONSTRAINT IF EXISTS freeze_idempotency_cache_ratios_nonnegative,
    DROP CONSTRAINT IF EXISTS freeze_idempotency_cache_costs_nonnegative,
    DROP COLUMN IF EXISTS output_cost_cents,
    DROP COLUMN IF EXISTS cache_creation_cost_cents,
    DROP COLUMN IF EXISTS cache_read_cost_cents,
    DROP COLUMN IF EXISTS uncached_input_cost_cents,
    DROP COLUMN IF EXISTS cache_usage_status,
    DROP COLUMN IF EXISTS cache_usage_schema,
    DROP COLUMN IF EXISTS cache_creation_tokens,
    DROP COLUMN IF EXISTS cache_read_tokens,
    DROP COLUMN IF EXISTS uncached_prompt_tokens,
    DROP COLUMN IF EXISTS cache_creation_ratio,
    DROP COLUMN IF EXISTS cache_read_ratio,
    DROP COLUMN IF EXISTS effective_cache_creation_price_cents,
    DROP COLUMN IF EXISTS effective_cache_read_price_cents;

ALTER TABLE model_prices
    DROP CONSTRAINT IF EXISTS model_prices_cache_read_ratio_bounds,
    DROP CONSTRAINT IF EXISTS model_prices_cache_creation_ratio_bounds,
    DROP COLUMN IF EXISTS cache_creation_ratio,
    DROP COLUMN IF EXISTS cache_read_ratio;

DELETE FROM schema_migrations
WHERE version = '000026_prd6_prompt_cache_billing';

COMMIT;
