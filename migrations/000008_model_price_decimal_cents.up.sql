-- Allow sub-cent model prices such as 0.2 cents per 1K tokens.
ALTER TABLE model_prices
    ALTER COLUMN input_price_cents TYPE NUMERIC(18,6) USING input_price_cents::numeric,
    ALTER COLUMN output_price_cents TYPE NUMERIC(18,6) USING output_price_cents::numeric;
