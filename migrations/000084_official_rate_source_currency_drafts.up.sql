BEGIN;

SELECT pg_advisory_xact_lock(86420517841702491);

LOCK TABLE enterprise_finance_rate_source_evidence, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- This expands only review-source evidence. Request pricing approval,
-- activation, quote selection, wallet settlement and outbox contracts remain
-- CNY-only and therefore cannot execute a USD draft.
ALTER TABLE enterprise_finance_rate_source_evidence
    DROP CONSTRAINT enterprise_finance_rate_source_currency_check,
    ADD CONSTRAINT enterprise_finance_rate_source_currency_check
        CHECK (currency IN ('CNY', 'USD'));

COMMENT ON COLUMN enterprise_finance_rate_source_evidence.currency IS
    'Original official-source currency; USD evidence remains review-only while request settlement is CNY-only';

COMMIT;
