BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM guardrail_advisory_gap_receipts)
       OR EXISTS (SELECT 1 FROM guardrail_advisory_gap_batches) THEN
        RAISE EXCEPTION 'V1 guardrail tenant-isolation rollback refused: durable advisory gap evidence exists; retain history and roll forward';
    END IF;
END;
$$;

DROP TABLE guardrail_advisory_gap_batches;
DROP TABLE guardrail_advisory_gap_receipts;
DROP TRIGGER guardrail_active_policy_quota ON guardrail_policies;
DROP FUNCTION enforce_guardrail_active_policy_quota();

DELETE FROM schema_migrations
WHERE version = '000039_v1_guardrail_tenant_isolation';

COMMIT;
