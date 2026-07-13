BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM freeze_idempotency
        WHERE usage_authority <> 'none'
           OR usage_finality <> 'missing'
           OR usage_purpose <> 'provider_evidence'
           OR usage_estimate_version <> ''
    ) OR EXISTS (
        SELECT 1 FROM call_logs
        WHERE usage_authority <> 'none'
           OR usage_finality <> 'missing'
           OR usage_purpose <> 'provider_evidence'
           OR usage_estimate_version <> ''
    ) OR EXISTS (
        SELECT 1 FROM usage_finalization_journal
        WHERE usage_authority <> 'none'
           OR usage_finality <> 'missing'
           OR usage_purpose <> 'provider_evidence'
           OR usage_estimate_version <> ''
    ) THEN
        RAISE EXCEPTION 'P6-F41A rollback refused: typed usage evidence exists';
    END IF;
END;
$$;

ALTER TABLE usage_finalization_journal
    DROP CONSTRAINT IF EXISTS usage_finalization_contract_check,
    DROP COLUMN IF EXISTS usage_estimate_version,
    DROP COLUMN IF EXISTS usage_contract_version,
    DROP COLUMN IF EXISTS usage_finality,
    DROP COLUMN IF EXISTS usage_purpose,
    DROP COLUMN IF EXISTS usage_authority;

ALTER TABLE call_logs
    DROP CONSTRAINT IF EXISTS call_logs_usage_contract_check,
    DROP COLUMN IF EXISTS usage_estimate_version,
    DROP COLUMN IF EXISTS usage_contract_version,
    DROP COLUMN IF EXISTS usage_finality,
    DROP COLUMN IF EXISTS usage_purpose,
    DROP COLUMN IF EXISTS usage_authority;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT IF EXISTS freeze_idempotency_usage_contract_check,
    DROP COLUMN IF EXISTS usage_estimate_version,
    DROP COLUMN IF EXISTS usage_contract_version,
    DROP COLUMN IF EXISTS usage_finality,
    DROP COLUMN IF EXISTS usage_purpose,
    DROP COLUMN IF EXISTS usage_authority;

DELETE FROM schema_migrations
WHERE version = '000031_prd6_usage_authority';

COMMIT;
