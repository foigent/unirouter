BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE execution_content_objects IN ACCESS EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF NOT pg_try_advisory_xact_lock(86420517831702492) THEN RAISE EXCEPTION 'content expansion owner busy'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations WHERE state<>'stopped')
 OR EXISTS(SELECT 1 FROM execution_content_objects WHERE reclaimed_at IS NOT NULL)
 THEN RAISE EXCEPTION 'content reclamation history or running generation exists; unsafe down refused'; END IF;
END $$;
ALTER TABLE execution_content_objects DROP CONSTRAINT execution_content_objects_reclaimed_state, DROP COLUMN reclaimed_at;
DELETE FROM schema_migrations WHERE version='000120_unpublished_content_reclamation';
CREATE OR REPLACE FUNCTION require_source_content_runtime_support() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state <> 'stopped' AND NOT EXISTS(SELECT 1 FROM runtime_support_manifests m
  WHERE m.artifact_digest=NEW.artifact_digest
   AND convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000119_execution_content_capture'
   AND convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"source_content","id":"unirouter.source-content-lifecycle.v1"}]'::jsonb)
 THEN RAISE EXCEPTION 'runtime requires the content lifecycle implementation'; END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    certified_ledger := ledger;
    IF ledger[cardinality(ledger)]='000119_execution_content_capture' THEN
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
