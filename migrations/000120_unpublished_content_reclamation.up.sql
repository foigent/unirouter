-- unirouter:explicit-compatible-expand-v1
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF NOT pg_try_advisory_xact_lock(86420517831702492) THEN
  RAISE EXCEPTION 'content expansion owner is busy' USING ERRCODE='55P03';
 END IF;
 -- A surviving old writer can still run a same-purpose bulk revoke. A future
 -- lease renewal failure cannot make that window safe: reject the migration.
 IF EXISTS(SELECT 1 FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest)
  WHERE g.state <> 'stopped' AND (m.artifact_digest IS NULL
   OR NOT (convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000120_unpublished_content_reclamation')
   OR NOT (convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"source_content","id":"unirouter.source-content-lifecycle.v1"}]'::jsonb)))
 THEN RAISE EXCEPTION 'all serving and recovery generations must support M120 content lifecycle'; END IF;
END $$;
-- Nullable, no backfill: unknown historical orphan rows remain ineligible.
ALTER TABLE execution_content_objects ADD COLUMN reclaimed_at TIMESTAMPTZ,
 ADD CONSTRAINT execution_content_objects_reclaimed_state CHECK(
 reclaimed_at IS NULL OR (state='orphan' AND saved_bytes=0 AND digest IS NULL));
CREATE OR REPLACE FUNCTION require_source_content_runtime_support() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state <> 'stopped' AND NOT EXISTS(SELECT 1 FROM runtime_support_manifests m
  WHERE m.artifact_digest=NEW.artifact_digest
   AND convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000120_unpublished_content_reclamation'
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
    IF ledger[cardinality(ledger)]='000120_unpublished_content_reclamation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires verified support proof in the migration transaction: content reclamation predecessor'; END IF;
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
