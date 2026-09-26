BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE projects,api_keys,execution_source_connections,execution_source_credentials,
 execution_source_content_scopes,execution_source_content_bindings,execution_source_content_operations,
 execution_content_streams,execution_content_policies,execution_content_manifests,
 execution_content_objects,execution_content_health IN ACCESS EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF NOT pg_try_advisory_xact_lock(86420517831702492) THEN RAISE EXCEPTION 'content expansion owner busy'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations WHERE state<>'stopped')
 OR EXISTS(SELECT 1 FROM api_keys WHERE source_lifecycle_scope='content')
 OR EXISTS(SELECT 1 FROM execution_source_connections WHERE cardinality(profiles)=0)
 OR EXISTS(SELECT 1 FROM execution_source_content_scopes)
 OR EXISTS(SELECT 1 FROM execution_source_content_bindings)
 OR EXISTS(SELECT 1 FROM execution_source_content_operations)
 OR EXISTS(SELECT 1 FROM execution_content_streams)
 OR EXISTS(SELECT 1 FROM execution_content_policies)
 OR EXISTS(SELECT 1 FROM execution_content_manifests)
 OR EXISTS(SELECT 1 FROM execution_content_objects)
 OR EXISTS(SELECT 1 FROM execution_content_health)
 THEN RAISE EXCEPTION 'M119 content history, policy, authority, storage or running generations exist; unsafe down refused'; END IF;
END $$;
DROP TABLE execution_content_health,execution_content_objects,execution_content_manifests,
 execution_source_content_operations,execution_source_content_bindings,execution_source_content_scopes,
 execution_content_streams,execution_content_policies;
DROP TRIGGER runtime_generations_source_content_support ON runtime_generations;
DROP FUNCTION require_source_content_runtime_support();
DROP TRIGGER execution_source_connections_content_safety ON execution_source_connections;
DROP FUNCTION advance_content_safety_version();
ALTER TABLE execution_source_connections DROP COLUMN content_safety_version,
 DROP CONSTRAINT execution_source_connections_profiles_check,
 ADD CONSTRAINT execution_source_connections_profiles_check CHECK(cardinality(profiles) BETWEEN 1 AND 11 AND profiles <@ ARRAY[
 'unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1',
 'unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1',
 'unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1']::text[]);
ALTER TABLE execution_source_credentials DROP CONSTRAINT execution_source_credentials_profiles_check,
 DROP CONSTRAINT execution_source_credentials_scope_key,
 DROP CONSTRAINT execution_source_credentials_scope_identity,
 DROP COLUMN source_lifecycle_scope,
 ADD CONSTRAINT execution_source_credentials_profiles_check CHECK(cardinality(profiles) BETWEEN 1 AND 11 AND profiles <@ ARRAY[
 'unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1',
 'unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1',
 'unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1']::text[]);
DROP INDEX api_keys_one_active_xinsight_source;
DROP INDEX api_keys_one_staged_xinsight_source;
DROP TRIGGER api_keys_source_lifecycle_default ON api_keys;
DROP FUNCTION default_source_lifecycle_scope();
ALTER TABLE api_keys DROP CONSTRAINT api_keys_source_lifecycle_scope_check,
 DROP CONSTRAINT api_keys_source_lifecycle_identity, DROP COLUMN source_lifecycle_scope;
CREATE UNIQUE INDEX api_keys_one_active_xinsight_source ON api_keys(project_id)
 WHERE purpose='xinsight.source.read' AND status='feed_active';
CREATE UNIQUE INDEX api_keys_one_staged_xinsight_source ON api_keys(project_id)
 WHERE purpose='xinsight.source.read' AND status='feed_staged';
DELETE FROM schema_migrations WHERE version='000119_execution_content_capture';
-- Restore the original M118 commit verifier with the original schema.
CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB;
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    IF c.state<>'certified' OR c.support_manifest_sha256<>encode(sha256(c.support_manifest_bytes),'hex')
       OR c.schema_ledger_sha256 IS DISTINCT FROM (SELECT encode(sha256(convert_to(string_agg(version,E'\n' ORDER BY version),'UTF8')),'hex') FROM schema_migrations) THEN
      RAISE EXCEPTION 'budget coverage requires verified support proof in the migration transaction'; END IF;
    m := convert_from(c.support_manifest_bytes,'UTF8')::jsonb;
    IF COALESCE((m->'schema_heads' ? '000118_source_budget_snapshots'),false)=false OR COALESCE((m->'implementations' @>
       '[{"kind":"source_budgets","id":"unirouter.budget-snapshot.v1"},{"kind":"financial_journal","id":"unirouter.gateway-financial-journal.v2"}]'::jsonb),false)=false THEN
      RAISE EXCEPTION 'budget coverage support proof lacks the writer or snapshot contract'; END IF;
    RETURN NEW;
END $$;
COMMIT;
