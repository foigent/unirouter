BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE projects,execution_source_connections,execution_source_credentials,enterprise_finance_budget_versions,
 execution_source_budget_scopes,execution_source_budget_definitions,execution_source_budget_bindings,
 execution_source_budget_binding_history,execution_source_budget_coverage IN ACCESS EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF NOT pg_try_advisory_xact_lock(86420517831702492) THEN RAISE EXCEPTION 'budget expansion owner busy'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations WHERE state<>'stopped')
 OR EXISTS(SELECT 1 FROM execution_source_budget_definitions WHERE change_kind<>'observed_baseline')
 OR EXISTS(SELECT 1 FROM execution_source_budget_bindings)
 OR EXISTS(SELECT 1 FROM execution_source_budget_scopes WHERE d_access_generation<>1 OR catalog_revision<>1)
 OR EXISTS(SELECT 1 FROM execution_source_connections WHERE profiles && ARRAY['unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1']::text[])
 OR EXISTS(SELECT 1 FROM execution_source_credentials WHERE profiles && ARRAY['unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1']::text[])
 OR (SELECT count(*) FROM execution_source_budget_coverage)<>1
 THEN RAISE EXCEPTION 'M118 budget history, grants, bindings or running generations exist; unsafe down refused'; END IF;
END $$;
DROP TRIGGER execution_source_budget_grants ON execution_source_connections;
DROP TRIGGER execution_source_budget_scope_init ON execution_source_connections;
DROP TRIGGER execution_source_budget_definition_revision ON projects;
DROP TRIGGER execution_source_budget_binding_revision ON execution_source_budget_bindings;
DROP TRIGGER execution_source_enterprise_budget_revision ON enterprise_finance_budget_versions;
DROP TABLE execution_source_budget_binding_history,execution_source_budget_bindings,execution_source_budget_definitions,
 execution_source_budget_scopes,execution_source_budget_coverage;
DROP FUNCTION invalidate_source_budget_grants(),initialize_source_budget_scope(),record_source_budget_definition(),record_source_budget_binding(),
 invalidate_source_enterprise_budget_catalog(),source_budget_metadata_immutable(),verify_source_budget_coverage_commit(),preserve_source_budget_coverage_history();
DROP INDEX usage_finalization_journal_budget_settled,internal_outbox_events_budget_financial,internal_outbox_events_budget_payload_org;
ALTER TABLE execution_source_connections DROP CONSTRAINT execution_source_connections_profiles_check,
 ADD CONSTRAINT execution_source_connections_profiles_check CHECK(cardinality(profiles) BETWEEN 1 AND 8 AND profiles <@ ARRAY[
 'unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1','unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1']::text[]);
ALTER TABLE execution_source_credentials DROP CONSTRAINT execution_source_credentials_profiles_check,
 ADD CONSTRAINT execution_source_credentials_profiles_check CHECK(cardinality(profiles) BETWEEN 1 AND 8 AND profiles <@ ARRAY[
 'unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1','unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1']::text[]);
DELETE FROM schema_migrations WHERE version='000118_source_budget_snapshots';
COMMIT;
