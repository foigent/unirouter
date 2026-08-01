BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000050_prd8_enterprise_finance_rate_cards'
    ) THEN
        RAISE EXCEPTION 'PRD8 enterprise-finance rate-card rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM enterprise_finance_pricing_bindings)
       OR EXISTS (SELECT 1 FROM enterprise_finance_pricing_snapshots)
       OR EXISTS (SELECT 1 FROM enterprise_finance_rate_components)
       OR EXISTS (SELECT 1 FROM enterprise_finance_rate_card_versions) THEN
        RAISE EXCEPTION 'PRD8 enterprise-finance rate-card rollback refused: pricing evidence exists';
    END IF;
END;
$$;

DROP TABLE enterprise_finance_pricing_bindings;
DROP TABLE enterprise_finance_pricing_snapshots;
DROP TABLE enterprise_finance_rate_components;
DROP TABLE enterprise_finance_rate_card_versions;

DELETE FROM schema_migrations
WHERE version = '000050_prd8_enterprise_finance_rate_cards';

COMMIT;
