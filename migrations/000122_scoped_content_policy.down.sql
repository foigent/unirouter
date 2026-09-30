-- M122 rollback is forbidden while scoped policy or deadline-bearing media state exists.
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE execution_content_enterprise_policies,execution_content_user_policies,
 execution_content_policies,execution_content_objects,async_media_jobs IN ACCESS EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM runtime_generations WHERE state <> 'stopped')
 OR EXISTS(SELECT 1 FROM execution_content_enterprise_policies)
 OR EXISTS(SELECT 1 FROM execution_content_user_policies)
 OR EXISTS(SELECT 1 FROM execution_content_policies WHERE NOT explicit_override OR
  desired_execution_storage_mode <> 'temporary_allowed' OR effective_execution_storage_mode <> 'temporary_allowed'
  OR project_quota_bytes IS NOT NULL)
 OR EXISTS(SELECT 1 FROM execution_content_objects WHERE policy_ref IS NOT NULL OR policy_scope IS NOT NULL OR policy_vector IS NOT NULL)
 OR EXISTS(SELECT 1 FROM async_media_jobs WHERE request_expires_at IS NOT NULL OR request_cleared_at IS NOT NULL
  OR execution_storage_mode IS NOT NULL OR content_policy_ref IS NOT NULL OR content_policy_version IS NOT NULL
  OR content_policy_vector IS NOT NULL) THEN
  RAISE EXCEPTION 'M122 scoped policy, deadline job, or active generation exists; unsafe down refused'; END IF;
END $$;
DROP TRIGGER async_media_request_cleared_at ON async_media_jobs;
DROP FUNCTION set_async_media_request_cleared_at();
DROP INDEX async_media_jobs_request_expiry;
ALTER TABLE async_media_jobs DROP CONSTRAINT async_media_request_deadline_consistency,
 DROP COLUMN request_expires_at, DROP COLUMN request_cleared_at, DROP COLUMN execution_storage_mode,
 DROP COLUMN content_policy_ref, DROP COLUMN content_policy_version, DROP COLUMN content_policy_vector;
ALTER TABLE execution_content_objects DROP CONSTRAINT execution_content_objects_scoped_policy_pair,
 DROP COLUMN policy_ref,DROP COLUMN policy_scope,DROP COLUMN policy_vector;
ALTER TABLE execution_content_policies DROP COLUMN explicit_override,
 DROP COLUMN desired_execution_storage_mode,DROP COLUMN effective_execution_storage_mode,DROP COLUMN project_quota_bytes;
DROP TABLE execution_content_user_policies,execution_content_enterprise_policies;
DROP TRIGGER runtime_generations_scoped_content_support ON runtime_generations;
DROP FUNCTION require_scoped_content_runtime_support();
DELETE FROM schema_migrations WHERE version='000122_scoped_content_policy';
CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    certified_ledger := ledger;
    IF ledger[cardinality(ledger)]='000121_gateway_pre_dispatch_terminal' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M121 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-3];
    ELSIF ledger[cardinality(ledger)]='000120_unpublished_content_reclamation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M120 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-2];
    ELSIF ledger[cardinality(ledger)]='000119_execution_content_capture' THEN
      certified_ledger := ledger[:cardinality(ledger)-1];
    END IF;
    IF c.state IS DISTINCT FROM 'certified' OR c.support_manifest_sha256 IS DISTINCT FROM encode(sha256(c.support_manifest_bytes),'hex')
       OR certified_ledger[cardinality(certified_ledger)] IS DISTINCT FROM '000118_source_budget_snapshots'
       OR c.schema_ledger_sha256 IS DISTINCT FROM encode(sha256(convert_to(array_to_string(certified_ledger,E'\n'),'UTF8')),'hex') THEN
      RAISE EXCEPTION 'budget coverage requires verified support proof in the migration transaction'; END IF;
    m := convert_from(c.support_manifest_bytes,'UTF8')::jsonb;
    IF COALESCE((m->'schema_heads' ? '000118_source_budget_snapshots'),false)=false OR COALESCE((m->'implementations' @>
       '[{"kind":"source_budgets","id":"unirouter.budget-snapshot.v1"},{"kind":"financial_journal","id":"unirouter.gateway-financial-journal.v2"}]'::jsonb),false)=false THEN
      RAISE EXCEPTION 'budget coverage support proof lacks the writer or snapshot contract'; END IF;
    RETURN NEW;
END $$;
COMMIT;
