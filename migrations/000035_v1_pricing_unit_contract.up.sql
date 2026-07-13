BEGIN;

-- These legacy column names are internal implementation details. Runtime
-- calculations retain the existing decimal cents-per-1,000-token basis while
-- the V1 admin API and UI expose only CNY per 1,000,000 tokens. This migration
-- documents the contract without rewriting or asserting the provenance of any
-- existing production price row.
COMMENT ON COLUMN model_prices.input_price_cents IS
    'Internal rate: decimal CNY cents per 1,000 input tokens. Admin API/UI unit: CNY per 1,000,000 tokens.';
COMMENT ON COLUMN model_prices.output_price_cents IS
    'Internal rate: decimal CNY cents per 1,000 output tokens. Admin API/UI unit: CNY per 1,000,000 tokens.';

COMMIT;
