BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000051_prd8_enterprise_finance_allocation_close'
    ) THEN
        RAISE EXCEPTION 'PRD8 enterprise-finance allocation rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM enterprise_finance_adjustment_journal_versions)
       OR EXISTS (SELECT 1 FROM enterprise_finance_allocation_results)
       OR EXISTS (SELECT 1 FROM enterprise_finance_allocation_runs) THEN
        RAISE EXCEPTION 'PRD8 enterprise-finance allocation rollback refused: allocation or close evidence exists';
    END IF;
END;
$$;

DROP TABLE enterprise_finance_adjustment_journal_versions;
DROP TABLE enterprise_finance_allocation_results;
DROP TABLE enterprise_finance_allocation_runs;

DELETE FROM schema_migrations
WHERE version = '000051_prd8_enterprise_finance_allocation_close';

COMMIT;
