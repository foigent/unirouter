BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000064_rate_card_request_pricing_authority'
    ) THEN
        RAISE EXCEPTION 'Rate Card authority rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM enterprise_finance_rate_source_evidence) THEN
        RAISE EXCEPTION 'Rate Card authority rollback refused: source evidence exists';
    END IF;
	IF EXISTS (
		SELECT 1 FROM enterprise_finance_rate_card_versions
		WHERE daily_price_bands <> '[]'::jsonb
	) THEN
		RAISE EXCEPTION 'Rate Card authority rollback refused: daily price schedules exist';
	END IF;
END;
$$;

DROP TABLE enterprise_finance_rate_source_evidence;

ALTER TABLE enterprise_finance_rate_card_versions
    DROP CONSTRAINT enterprise_finance_rate_card_daily_price_bands_array_check,
    DROP COLUMN daily_price_bands;

DELETE FROM schema_migrations
WHERE version = '000064_rate_card_request_pricing_authority';

COMMIT;
