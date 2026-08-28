BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000084_official_rate_source_currency_drafts'
    ) THEN
        RAISE EXCEPTION 'Official rate-source currency rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (
        SELECT 1 FROM enterprise_finance_rate_source_evidence
        WHERE currency <> 'CNY'
    ) THEN
        RAISE EXCEPTION 'Official rate-source currency rollback refused: non-CNY source evidence exists';
    END IF;
END;
$$;

ALTER TABLE enterprise_finance_rate_source_evidence
    DROP CONSTRAINT enterprise_finance_rate_source_currency_check,
    ADD CONSTRAINT enterprise_finance_rate_source_currency_check
        CHECK (currency = 'CNY');

COMMENT ON COLUMN enterprise_finance_rate_source_evidence.currency IS NULL;

DELETE FROM schema_migrations
WHERE version = '000084_official_rate_source_currency_drafts';

COMMIT;
