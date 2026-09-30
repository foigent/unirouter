BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE api_keys,audit_logs,xinsight_machine_credentials,xinsight_policy_delegations,
 xinsight_policy_operations,xinsight_source_enrollments,xinsight_source_enrollment_bindings,
 xinsight_source_enrollment_exclusions
 IN ACCESS EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF (SELECT MAX(version) FROM schema_migrations) IS DISTINCT FROM '000123_xinsight_policy_delegation' THEN
  RAISE EXCEPTION 'M123 down requires exact M123 head'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations WHERE state <> 'stopped')
 OR EXISTS(SELECT 1 FROM xinsight_machine_credentials)
 OR EXISTS(SELECT 1 FROM xinsight_policy_delegations)
 OR EXISTS(SELECT 1 FROM xinsight_policy_operations)
 OR EXISTS(SELECT 1 FROM xinsight_source_enrollments)
 OR EXISTS(SELECT 1 FROM xinsight_source_enrollment_bindings)
 OR EXISTS(SELECT 1 FROM xinsight_source_enrollment_exclusions)
 OR EXISTS(SELECT 1 FROM audit_logs WHERE actor_kind='service' OR user_id IS NULL)
 OR EXISTS(SELECT 1 FROM api_keys WHERE purpose IN ('xinsight.policy.manage','xinsight.source.enroll')) THEN
  RAISE EXCEPTION 'M123 machine authority, service audit, or active generation exists; unsafe down refused'; END IF;
END $$;
DROP TRIGGER audit_actor_identity_default ON audit_logs;
DROP FUNCTION set_audit_actor_identity();
DROP TRIGGER api_keys_xinsight_enrollment_label ON api_keys;
DROP FUNCTION protect_xinsight_enrollment_key_label();
DROP TABLE xinsight_policy_operations;
ALTER TABLE audit_logs DROP CONSTRAINT audit_actor_identity,
 DROP COLUMN delegation_id,DROP COLUMN actor_ref,DROP COLUMN actor_kind,
 ALTER COLUMN user_id SET NOT NULL;
DROP TABLE xinsight_source_enrollment_exclusions,xinsight_source_enrollment_bindings,xinsight_source_enrollments,
 xinsight_policy_delegations,xinsight_machine_credentials;
DROP INDEX xinsight_machine_one_active_purpose;
DROP INDEX xinsight_machine_registration_operation;
ALTER TABLE api_keys DROP CONSTRAINT api_keys_completed_feed_purpose_check,
 ADD CONSTRAINT api_keys_completed_feed_purpose_check CHECK (purpose IN ('','xinsight.feed.read','xinsight.source.read'));
ALTER TABLE api_keys DROP CONSTRAINT api_keys_completed_feed_status_check,
 ADD CONSTRAINT api_keys_completed_feed_status_check CHECK (
  (purpose='' AND status NOT IN ('feed_active','feed_staged','feed_revoked')) OR
  (purpose IN ('xinsight.feed.read','xinsight.source.read') AND status IN ('feed_active','feed_staged','feed_revoked','disabled')));
ALTER TABLE api_keys DROP CONSTRAINT api_keys_verifier_scheme_purpose_check,
 ADD CONSTRAINT api_keys_verifier_scheme_purpose_check CHECK (
  (purpose='' AND verifier_scheme='legacy_hmac_sha256_v1' AND registration_operation_id='') OR
  (purpose='xinsight.feed.read' AND ((verifier_scheme='legacy_hmac_sha256_v1' AND registration_operation_id='')
   OR (verifier_scheme='receiver_sha256_v1' AND registration_operation_id ~ '^[0-9a-f]{32}$'))) OR
  (purpose='xinsight.source.read' AND verifier_scheme='receiver_sha256_v1' AND registration_operation_id ~ '^[0-9a-f]{32}$'));
DROP TRIGGER runtime_generations_xinsight_policy_support ON runtime_generations;
DROP FUNCTION require_xinsight_policy_runtime_support();
CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    certified_ledger := ledger;
    IF ledger[cardinality(ledger)]='000122_scoped_content_policy' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000121_gateway_pre_dispatch_terminal'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-3] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M122 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-4];
    ELSIF ledger[cardinality(ledger)]='000121_gateway_pre_dispatch_terminal' THEN
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
DELETE FROM schema_migrations WHERE version='000123_xinsight_policy_delegation';
COMMIT;
