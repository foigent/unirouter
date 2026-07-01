-- Roll back to integer cents, rounding any fractional configured prices.
ALTER TABLE model_prices
    ALTER COLUMN input_price_cents TYPE INTEGER USING ROUND(input_price_cents)::integer,
    ALTER COLUMN output_price_cents TYPE INTEGER USING ROUND(output_price_cents)::integer;
