BEGIN;

COMMENT ON COLUMN model_prices.input_price_cents IS NULL;
COMMENT ON COLUMN model_prices.output_price_cents IS NULL;

DELETE FROM schema_migrations
WHERE version = '000035_v1_pricing_unit_contract';

COMMIT;
