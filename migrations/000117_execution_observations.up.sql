-- unirouter:explicit-compatible-expand-v1
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
-- Cover Renew's fast UPDATE and concurrent bootstrap from fleet inspection
-- until the support trigger becomes visible. Release/slot locks alone do not.
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
    IF NOT pg_try_advisory_xact_lock(86420517831702492) THEN
        RAISE EXCEPTION 'source observation expansion owner is busy' USING ERRCODE='55P03';
    END IF;
END $$;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest)
       WHERE g.state <> 'stopped'
       AND (m.artifact_digest IS NULL OR NOT (convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000117_execution_observations')
        OR NOT (convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @> '[{"kind":"source_facts","id":"unirouter.source-fact.v1"},{"kind":"source_observations","id":"unirouter.source-observation.v1"},{"kind":"source_dispatch","id":"unirouter.source-dispatch-outbox.v1"},{"kind":"stream_counter","id":"unirouter.stream-counter.v2"},{"kind":"stream_counter","id":"unirouter.stream-counter.v3"}]'::jsonb)))
    THEN RAISE EXCEPTION 'all serving and recovery generations must support M117 observations'; END IF;
END $$;
CREATE FUNCTION require_source_dispatch_runtime_support() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.state <> 'stopped' AND NOT EXISTS (
        SELECT 1 FROM runtime_support_manifests m WHERE m.artifact_digest=NEW.artifact_digest
        AND convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000117_execution_observations'
        AND convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
            '[{"kind":"source_dispatch","id":"unirouter.source-dispatch-outbox.v1"}]'::jsonb
    ) THEN RAISE EXCEPTION 'serving and recovery generations require the source dispatch handler'; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER runtime_generations_source_dispatch_support BEFORE INSERT OR UPDATE ON runtime_generations
FOR EACH ROW EXECUTE FUNCTION require_source_dispatch_runtime_support();

LOCK TABLE internal_outbox_events IN SHARE ROW EXCLUSIVE MODE NOWAIT;
CREATE FUNCTION preserve_source_dispatch_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE missing_record BOOLEAN;
BEGIN
    IF TG_OP='UPDATE' AND OLD.event_type='evidence.source_dispatch.v1' THEN
        IF ROW(NEW.id,NEW.event_type,NEW.event_key,NEW.priority,NEW.org_id,NEW.request_id,NEW.occurred_at)
            IS DISTINCT FROM ROW(OLD.id,OLD.event_type,OLD.event_key,OLD.priority,OLD.org_id,OLD.request_id,OLD.occurred_at)
            OR (NEW.payload-ARRAY['materialized','published_at']) IS DISTINCT FROM (OLD.payload-ARRAY['materialized','published_at'])
        THEN RAISE EXCEPTION 'frozen source dispatch identity and records are immutable'; END IF;
        -- A later lease update or replay after source retention does not need
        -- the already-published rows. Only the first marker transition does.
        IF OLD.payload->>'materialized'='true' THEN
            IF NEW.payload IS DISTINCT FROM OLD.payload THEN
                RAISE EXCEPTION 'source dispatch materialization marker cannot change';
            END IF;
            RETURN NEW;
        END IF;
        IF NEW.payload IS NOT DISTINCT FROM OLD.payload THEN RETURN NEW; END IF;
        IF NEW.payload->'materialized' IS DISTINCT FROM 'true'::jsonb OR
            jsonb_typeof(NEW.payload->'published_at') IS DISTINCT FROM 'string' OR NEW.payload->>'published_at'=''
        THEN RAISE EXCEPTION 'invalid source dispatch materialization transition'; END IF;
        SELECT EXISTS (
            SELECT 1 FROM jsonb_array_elements(OLD.payload->'records') r
            WHERE NOT EXISTS (SELECT 1 FROM execution_source_facts f
                WHERE f.event_id=r->>'event_id' AND f.source_fact_id=r->>'source_fact_id'
                AND f.source_hash=r->>'source_hash' AND f.revision=1
                AND f.source_instance=OLD.payload->>'source_instance'
                AND f.org_id=OLD.org_id AND f.project_id=(OLD.payload->>'project_id')::bigint
                AND f.request_id=OLD.request_id)
        ) INTO missing_record;
        IF missing_record THEN RAISE EXCEPTION 'source dispatch marker requires all three published records'; END IF;
        RETURN NEW;
    END IF;
    IF NEW.event_type='evidence.source_dispatch.v1' THEN
        IF TG_OP <> 'INSERT' THEN RAISE EXCEPTION 'another event cannot become a source dispatch'; END IF;
        IF NEW.priority<>300 OR NEW.payload->>'schema_version' IS DISTINCT FROM 'unirouter.source-dispatch-outbox.v1' OR
            NEW.payload->'materialized' IS DISTINCT FROM 'false'::jsonb OR NEW.payload->>'published_at' IS DISTINCT FROM '' OR
            jsonb_typeof(NEW.payload->'records') IS DISTINCT FROM 'array' OR
            NEW.payload->>'org_id' IS DISTINCT FROM NEW.org_id::text OR NEW.payload->>'request_id' IS DISTINCT FROM NEW.request_id OR
            NEW.event_key IS DISTINCT FROM 'source-dispatch:'||substring(NEW.payload->>'digest' FROM 8) OR
            octet_length(NEW.payload::text)>261120
        THEN RAISE EXCEPTION 'invalid initial frozen source dispatch'; END IF;
        IF jsonb_array_length(NEW.payload->'records')<>3 THEN RAISE EXCEPTION 'source dispatch requires three frozen records'; END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER internal_outbox_source_dispatch_immutable BEFORE INSERT OR UPDATE ON internal_outbox_events
FOR EACH ROW EXECUTE FUNCTION preserve_source_dispatch_event();
CREATE INDEX internal_outbox_source_dispatch_request ON internal_outbox_events(org_id,request_id,(payload->>'attempt_id'))
WHERE event_type='evidence.source_dispatch.v1';
LOCK TABLE execution_source_streams, execution_source_connections, execution_source_credentials IN ACCESS EXCLUSIVE MODE NOWAIT;
-- Expand the finite set only. Retained A grants, bytes, epochs and sequences do
-- not change, and no existing connection or credential gains a B profile.
ALTER TABLE execution_source_streams DROP CONSTRAINT execution_source_streams_profile_check,
    ADD CONSTRAINT execution_source_streams_profile_check CHECK (profile IN ('unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1','unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1'));
ALTER TABLE execution_source_connections DROP CONSTRAINT execution_source_connections_profiles_check,
    ADD CONSTRAINT execution_source_connections_profiles_check CHECK (cardinality(profiles) BETWEEN 1 AND 8 AND profiles <@ ARRAY['unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1','unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1']::text[]);
ALTER TABLE execution_source_credentials DROP CONSTRAINT execution_source_credentials_profiles_check,
    ADD CONSTRAINT execution_source_credentials_profiles_check CHECK (cardinality(profiles) BETWEEN 1 AND 8 AND profiles <@ ARRAY['unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1','unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1']::text[]);
COMMIT;
