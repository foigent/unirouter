BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE xinsight_connector_installations,xinsight_connector_control_bindings,
 xinsight_receiver_adoptions,xinsight_connector_phase_operations IN ACCESS EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF (SELECT MAX(version) FROM schema_migrations) IS DISTINCT FROM '000124_xinsight_receiver_adoption' THEN
  RAISE EXCEPTION 'M124 down requires exact M124 head'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations WHERE state<>'stopped')
 OR EXISTS(SELECT 1 FROM xinsight_receiver_adoptions)
 OR EXISTS(SELECT 1 FROM xinsight_connector_installations WHERE receiver_mode='adopt_existing' OR setup_request_id IS NOT NULL)
 OR EXISTS(SELECT 1 FROM xinsight_connector_control_bindings WHERE receiver_adoption_bootstrap_canonical IS NOT NULL)
 OR EXISTS(SELECT 1 FROM xinsight_connector_control_bindings WHERE redeemed_origin IS NOT NULL)
 OR EXISTS(SELECT 1 FROM xinsight_connector_phase_operations
   WHERE convert_from(request_canonical,'UTF8')::jsonb->>'schema_version'='xinsight.connector-control-operation.v3') THEN
  RAISE EXCEPTION 'M124 adoption or v3 history or active generation exists; unsafe down refused'; END IF;
END $$;
DROP TRIGGER xinsight_setup_consent_identity ON xinsight_connector_installations;
DROP FUNCTION protect_xinsight_setup_consent();
ALTER TABLE xinsight_connector_installations DROP CONSTRAINT xinsight_setup_consent_pair,
 DROP COLUMN setup_request_id,DROP COLUMN setup_consent_canonical,DROP COLUMN setup_consent_sha256,
 DROP COLUMN setup_state,DROP COLUMN setup_progress;
DROP TABLE xinsight_receiver_adoptions;
DROP FUNCTION protect_xinsight_adoption_identity();
DROP TRIGGER runtime_generation_xinsight_adoption_support ON runtime_generations;
DROP FUNCTION require_xinsight_adoption_runtime_support();
DROP TRIGGER xinsight_adoption_bootstrap_identity ON xinsight_connector_control_bindings;
DROP FUNCTION protect_xinsight_adoption_bootstrap();
ALTER TABLE xinsight_connector_control_bindings DROP CONSTRAINT xinsight_adoption_bootstrap_pair,
 DROP COLUMN receiver_adoption_bootstrap_canonical,DROP COLUMN receiver_adoption_bootstrap_sha256;
DROP TRIGGER xinsight_redeemed_origin_identity ON xinsight_connector_control_bindings;
DROP FUNCTION protect_xinsight_redeemed_origin();
ALTER TABLE xinsight_connector_control_bindings DROP CONSTRAINT xinsight_redeemed_origin_completed,
 DROP COLUMN redeemed_origin;
DROP TRIGGER xinsight_receiver_mode_identity ON xinsight_connector_installations;
DROP FUNCTION protect_xinsight_receiver_mode();
ALTER TABLE xinsight_connector_installations DROP COLUMN receiver_mode;
CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    certified_ledger := ledger;
    IF ledger[cardinality(ledger)]='000123_xinsight_policy_delegation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000122_scoped_content_policy'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000121_gateway_pre_dispatch_terminal'
         OR ledger[cardinality(ledger)-3] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-4] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M123 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-5];
    ELSIF ledger[cardinality(ledger)]='000122_scoped_content_policy' THEN
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
DELETE FROM schema_migrations WHERE version='000124_xinsight_receiver_adoption';
COMMIT;
