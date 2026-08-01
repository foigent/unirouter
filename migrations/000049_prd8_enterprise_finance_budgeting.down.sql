BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000049_prd8_enterprise_finance_budgeting'
    ) THEN
        RAISE EXCEPTION 'PRD8 enterprise-finance budgeting rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM enterprise_finance_budget_versions)
       OR EXISTS (SELECT 1 FROM enterprise_finance_cost_center_versions)
       OR EXISTS (SELECT 1 FROM enterprise_finance_period_events)
       OR EXISTS (SELECT 1 FROM enterprise_finance_fiscal_periods) THEN
        RAISE EXCEPTION 'PRD8 enterprise-finance budgeting rollback refused: finance planning data exists';
    END IF;
END;
$$;

DROP TABLE enterprise_finance_budget_versions;
DROP TABLE enterprise_finance_cost_center_versions;
DROP TABLE enterprise_finance_period_events;
DROP TABLE enterprise_finance_fiscal_periods;

DELETE FROM schema_migrations
WHERE version = '000049_prd8_enterprise_finance_budgeting';

COMMIT;
