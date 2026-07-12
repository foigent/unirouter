BEGIN;

-- P6-F09 keeps provider prompt-cache accounting separate from response
-- caching. Platform-owned per-model ratios produce immutable effective prices
-- at Freeze; provider-reported token classes become the only cache billing
-- truth carried through settlement, logs and reconciliation.
ALTER TABLE model_prices
    ADD COLUMN IF NOT EXISTS cache_read_ratio NUMERIC(10,6) NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS cache_creation_ratio NUMERIC(10,6) NOT NULL DEFAULT 1;

ALTER TABLE model_prices
    DROP CONSTRAINT IF EXISTS model_prices_cache_read_ratio_bounds,
    DROP CONSTRAINT IF EXISTS model_prices_cache_creation_ratio_bounds,
    ADD CONSTRAINT model_prices_cache_read_ratio_bounds
        CHECK (cache_read_ratio BETWEEN 0 AND 100),
    ADD CONSTRAINT model_prices_cache_creation_ratio_bounds
        CHECK (cache_creation_ratio BETWEEN 0 AND 100);

ALTER TABLE freeze_idempotency
    ADD COLUMN IF NOT EXISTS effective_cache_read_price_cents NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS effective_cache_creation_price_cents NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_read_ratio NUMERIC(10,6) NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS cache_creation_ratio NUMERIC(10,6) NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS uncached_prompt_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_usage_schema VARCHAR(80) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cache_usage_status VARCHAR(40) NOT NULL DEFAULT 'not_reported',
    ADD COLUMN IF NOT EXISTS uncached_input_cost_cents NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_read_cost_cents NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_creation_cost_cents NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS output_cost_cents NUMERIC(18,6) NOT NULL DEFAULT 0;

UPDATE freeze_idempotency
SET uncached_prompt_tokens = prompt_tokens
WHERE uncached_prompt_tokens = 0
  AND prompt_tokens > 0
  AND cache_read_tokens = 0
  AND cache_creation_tokens = 0
  AND cache_usage_schema = '';

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT IF EXISTS freeze_idempotency_cache_usage_nonnegative,
    DROP CONSTRAINT IF EXISTS freeze_idempotency_prompt_partition,
    DROP CONSTRAINT IF EXISTS freeze_idempotency_cache_ratios_nonnegative,
    DROP CONSTRAINT IF EXISTS freeze_idempotency_cache_costs_nonnegative,
    ADD CONSTRAINT freeze_idempotency_cache_usage_nonnegative
        CHECK (uncached_prompt_tokens >= 0 AND cache_read_tokens >= 0 AND cache_creation_tokens >= 0),
    ADD CONSTRAINT freeze_idempotency_prompt_partition
        CHECK (
            uncached_prompt_tokens + cache_read_tokens + cache_creation_tokens = prompt_tokens
            OR (
                cache_usage_schema = ''
                AND cache_read_tokens = 0
                AND cache_creation_tokens = 0
                AND uncached_prompt_tokens = 0
            )
        ),
    ADD CONSTRAINT freeze_idempotency_cache_ratios_nonnegative
        CHECK (cache_read_ratio BETWEEN 0 AND 100 AND cache_creation_ratio BETWEEN 0 AND 100),
    ADD CONSTRAINT freeze_idempotency_cache_costs_nonnegative
        CHECK (
            effective_cache_read_price_cents >= 0
            AND effective_cache_creation_price_cents >= 0
            AND uncached_input_cost_cents >= 0
            AND cache_read_cost_cents >= 0
            AND cache_creation_cost_cents >= 0
            AND output_cost_cents >= 0
        );

ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS uncached_prompt_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_usage_schema VARCHAR(80) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cache_usage_status VARCHAR(40) NOT NULL DEFAULT 'not_reported',
    ADD COLUMN IF NOT EXISTS pricing_version VARCHAR(80) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS effective_input_price_cents NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS effective_cache_read_price_cents NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS effective_cache_creation_price_cents NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS effective_output_price_cents NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_read_cost_cents NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_creation_cost_cents NUMERIC(18,6) NOT NULL DEFAULT 0;

UPDATE call_logs
SET uncached_prompt_tokens = prompt_tokens
WHERE uncached_prompt_tokens = 0
  AND prompt_tokens > 0
  AND cache_read_tokens = 0
  AND cache_creation_tokens = 0
  AND cache_usage_schema = '';

ALTER TABLE call_logs
    DROP CONSTRAINT IF EXISTS call_logs_cache_usage_nonnegative,
    DROP CONSTRAINT IF EXISTS call_logs_prompt_partition,
    DROP CONSTRAINT IF EXISTS call_logs_cache_prices_nonnegative,
    ADD CONSTRAINT call_logs_cache_usage_nonnegative
        CHECK (uncached_prompt_tokens >= 0 AND cache_read_tokens >= 0 AND cache_creation_tokens >= 0),
    ADD CONSTRAINT call_logs_prompt_partition
        CHECK (
            uncached_prompt_tokens + cache_read_tokens + cache_creation_tokens = prompt_tokens
            OR (
                cache_usage_schema = ''
                AND cache_read_tokens = 0
                AND cache_creation_tokens = 0
                AND uncached_prompt_tokens = 0
            )
        ),
    ADD CONSTRAINT call_logs_cache_prices_nonnegative
        CHECK (
            effective_input_price_cents >= 0
            AND effective_cache_read_price_cents >= 0
            AND effective_cache_creation_price_cents >= 0
            AND effective_output_price_cents >= 0
            AND cache_read_cost_cents >= 0
            AND cache_creation_cost_cents >= 0
        );

CREATE INDEX IF NOT EXISTS idx_call_logs_cache_usage_created
    ON call_logs (org_id, cache_usage_status, created_at DESC)
    WHERE cache_usage_schema <> '';

COMMIT;
