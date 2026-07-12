BEGIN;

-- Dropping a configured policy or a server-owned API-key label would silently
-- widen traffic after an older binary starts. Permit schema rollback only
-- before the new control plane has stored any configuration.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM distributed_limit_policies)
       OR EXISTS (SELECT 1 FROM api_keys WHERE policy_labels <> '') THEN
        RAISE EXCEPTION 'P6-F07 down refused: distributed policy configuration exists; retain the schema and roll forward';
    END IF;
END;
$$;

DROP TABLE IF EXISTS distributed_limit_policies;
ALTER TABLE api_keys DROP COLUMN IF EXISTS policy_labels;

DELETE FROM schema_migrations
WHERE version = '000024_prd6_distributed_limit_policy';

COMMIT;
