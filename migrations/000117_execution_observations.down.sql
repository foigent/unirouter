BEGIN;
SELECT version FROM schema_migrations WHERE version='000117_execution_observations' FOR UPDATE NOWAIT;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
    IF NOT pg_try_advisory_xact_lock(86420517831702492) THEN
        RAISE EXCEPTION 'source observation expansion owner is busy' USING ERRCODE='55P03';
    END IF;
END $$;
LOCK TABLE internal_outbox_events IN SHARE MODE NOWAIT;
LOCK TABLE execution_source_streams, execution_source_facts, execution_source_connections,
    execution_source_credentials, execution_source_acknowledgements IN ACCESS EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM internal_outbox_events WHERE event_type='evidence.source_dispatch.v1')
    THEN RAISE EXCEPTION 'M117 source dispatch events remain; unsafe down refused'; END IF;
    IF EXISTS (SELECT 1 FROM runtime_generations WHERE state<>'stopped') OR
       EXISTS (SELECT 1 FROM execution_source_streams WHERE profile NOT IN ('unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1')) OR
       EXISTS (SELECT 1 FROM execution_source_connections WHERE NOT profiles <@ ARRAY['unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1']::text[]) OR
       EXISTS (SELECT 1 FROM execution_source_credentials WHERE NOT profiles <@ ARRAY['unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1']::text[]) OR
       EXISTS (SELECT 1 FROM execution_source_acknowledgements WHERE profile NOT IN ('unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1'))
    THEN RAISE EXCEPTION 'M117 observation evidence, grants or running generations exist; unsafe down refused'; END IF;
END $$;
DROP TRIGGER internal_outbox_source_dispatch_immutable ON internal_outbox_events;
DROP FUNCTION preserve_source_dispatch_event();
DROP INDEX internal_outbox_source_dispatch_request;
DROP TRIGGER runtime_generations_source_dispatch_support ON runtime_generations;
DROP FUNCTION require_source_dispatch_runtime_support();
-- A evidence and grants are retained byte-for-byte. Never erase B to downgrade.
ALTER TABLE execution_source_streams DROP CONSTRAINT execution_source_streams_profile_check,
    ADD CONSTRAINT execution_source_streams_profile_check CHECK (profile IN ('unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1'));
ALTER TABLE execution_source_connections DROP CONSTRAINT execution_source_connections_profiles_check,
    ADD CONSTRAINT execution_source_connections_profiles_check CHECK (cardinality(profiles) BETWEEN 1 AND 3 AND profiles <@ ARRAY['unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1']::text[]);
ALTER TABLE execution_source_credentials DROP CONSTRAINT execution_source_credentials_profiles_check,
    ADD CONSTRAINT execution_source_credentials_profiles_check CHECK (cardinality(profiles) BETWEEN 1 AND 3 AND profiles <@ ARRAY['unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1']::text[]);
DELETE FROM schema_migrations WHERE version='000117_execution_observations';
COMMIT;
