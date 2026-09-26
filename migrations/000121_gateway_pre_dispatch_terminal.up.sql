-- unirouter:explicit-compatible-expand-v1
-- M121 adds one independent pre-dispatch observation profile; no grant is added.
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE execution_source_streams, execution_source_connections, execution_source_credentials IN ACCESS EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF (SELECT MAX(version) FROM schema_migrations) IS DISTINCT FROM '000120_unpublished_content_reclamation' THEN
  RAISE EXCEPTION 'M121 requires exact M120 predecessor'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest)
  WHERE g.state <> 'stopped' AND (m.artifact_digest IS NULL OR NOT COALESCE(
   convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000121_gateway_pre_dispatch_terminal',false) OR NOT COALESCE(
   convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"source_observations","id":"unirouter.source-observation.v2"}]'::jsonb,false))) THEN
  RAISE EXCEPTION 'serving or recovery generation lacks M121 support'; END IF;
END $$;
ALTER TABLE execution_source_streams DROP CONSTRAINT execution_source_streams_profile_check,
 ADD CONSTRAINT execution_source_streams_profile_check CHECK(profile=ANY(ARRAY['unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1','unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1','unirouter.runtime-observation.v2']::text[]));
ALTER TABLE execution_source_connections DROP CONSTRAINT execution_source_connections_profiles_check,
 ADD CONSTRAINT execution_source_connections_profiles_check CHECK(cardinality(profiles) BETWEEN 0 AND 12 AND profiles <@ ARRAY['unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1','unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1','unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1','unirouter.runtime-observation.v2']::text[]);
ALTER TABLE execution_source_credentials DROP CONSTRAINT execution_source_credentials_profiles_check,
 ADD CONSTRAINT execution_source_credentials_profiles_check CHECK(
 (source_lifecycle_scope='content' AND profiles=ARRAY['unirouter.interaction-content.v1']::text[]) OR
 (source_lifecycle_scope='legacy' AND cardinality(profiles) BETWEEN 1 AND 12 AND profiles <@ ARRAY['unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1','unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1','unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1','unirouter.runtime-observation.v2']::text[]));
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
