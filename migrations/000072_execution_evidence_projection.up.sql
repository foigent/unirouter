BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE call_logs, request_trace_events, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- Query projections copy only the bounded CompactProjection identity from the
-- immutable provider-attempt receipt. Mutable provider/channel display names
-- are deliberately not attribution keys.
ALTER TABLE call_logs
    ADD COLUMN route_evidence_source VARCHAR(40),
    ADD COLUMN route_evidence_version VARCHAR(48),
    ADD COLUMN route_authority_mode VARCHAR(16),
    ADD COLUMN route_evidence_hash VARCHAR(71),
    ADD COLUMN route_public_model_id BIGINT,
    ADD COLUMN route_connection_id BIGINT,
    ADD COLUMN route_credential_binding_id BIGINT,
    ADD COLUMN route_model_operation_id VARCHAR(80),
    ADD COLUMN route_runtime_release_id BIGINT,
    ADD COLUMN route_snapshot_hash VARCHAR(69);

ALTER TABLE request_trace_events
    ADD COLUMN route_evidence_source VARCHAR(40),
    ADD COLUMN route_evidence_version VARCHAR(48),
    ADD COLUMN route_authority_mode VARCHAR(16),
    ADD COLUMN route_evidence_hash VARCHAR(71),
    ADD COLUMN route_public_model_id BIGINT,
    ADD COLUMN route_deployment_id BIGINT,
    ADD COLUMN route_connection_id BIGINT,
    ADD COLUMN route_credential_binding_id BIGINT,
    ADD COLUMN route_model_operation_id VARCHAR(80),
    ADD COLUMN route_runtime_release_id BIGINT,
    ADD COLUMN route_snapshot_hash VARCHAR(69),
    ADD COLUMN route_channel_id BIGINT;

ALTER TABLE call_logs
    ADD CONSTRAINT call_logs_execution_projection_check CHECK (
        (
            route_evidence_source IS NULL
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_connection_id IS NULL AND route_credential_binding_id IS NULL
            AND route_model_operation_id IS NULL AND route_runtime_release_id IS NULL
            AND route_snapshot_hash IS NULL
        )
        OR (
            route_evidence_source IN ('response_cache','no_provider_attempt')
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_connection_id IS NULL AND route_credential_binding_id IS NULL
            AND route_model_operation_id IS NULL AND route_runtime_release_id IS NULL
            AND route_snapshot_hash IS NULL
            AND source_event_hash ~ '^[a-f0-9]{64}$'
        )
        OR (
            route_evidence_source = 'precontract_provider_attempt'
            AND COALESCE(BTRIM(attempt_id),'') <> ''
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_deployment_id IS NULL AND route_connection_id IS NULL
            AND route_credential_binding_id IS NULL AND route_model_operation_id IS NULL
            AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
            AND COALESCE(route_channel_id,0) > 0
            AND source_event_hash ~ '^[a-f0-9]{64}$'
        )
        OR (
            route_evidence_source = 'provider_attempt'
            AND COALESCE(BTRIM(attempt_id),'') <> ''
            AND route_evidence_version = 'unirouter.execution-route.v1'
            AND route_authority_mode IN ('legacy','verify','canonical')
            AND route_evidence_hash ~ '^sha256:[a-f0-9]{64}$'
            AND COALESCE(BTRIM(route_model_operation_id),'') <> ''
            AND route_model_operation_id = BTRIM(route_model_operation_id)
            AND route_model_operation_id <> 'legacy.any.v1'
            AND route_model_operation_id !~ E'[\\x00\\r\\n]'
            AND source_event_hash ~ '^[a-f0-9]{64}$'
            AND (
                route_authority_mode='legacy'
                AND COALESCE(route_channel_id,0)>0
                AND route_public_model_id IS NULL AND route_deployment_id IS NULL
                AND route_connection_id IS NULL AND route_credential_binding_id IS NULL
                AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
                OR route_authority_mode='verify'
                AND COALESCE(route_channel_id,0)>0 AND COALESCE(route_public_model_id,0)>0
                AND COALESCE(route_deployment_id,0)>0 AND COALESCE(route_connection_id,0)>0
                AND COALESCE(route_credential_binding_id,0)>0 AND COALESCE(route_runtime_release_id,0)>0
                AND COALESCE(route_snapshot_hash,'') ~ '^mrs1_[a-f0-9]{64}$'
                OR route_authority_mode='canonical'
                AND route_channel_id IS NULL AND COALESCE(route_public_model_id,0)>0
                AND COALESCE(route_deployment_id,0)>0 AND COALESCE(route_connection_id,0)>0
                AND COALESCE(route_credential_binding_id,0)>0 AND COALESCE(route_runtime_release_id,0)>0
                AND COALESCE(route_snapshot_hash,'') ~ '^mrs1_[a-f0-9]{64}$'
            )
        )
    ),
    ADD CONSTRAINT call_logs_route_public_model_org_fk
        FOREIGN KEY (route_public_model_id,org_id)
        REFERENCES model_registry(id,org_id) ON DELETE RESTRICT,
    ADD CONSTRAINT call_logs_route_deployment_connection_fk
        FOREIGN KEY (route_deployment_id,route_connection_id)
        REFERENCES model_deployments(id,connection_id) ON DELETE RESTRICT,
    ADD CONSTRAINT call_logs_route_credential_connection_fk
        FOREIGN KEY (route_credential_binding_id,route_connection_id)
        REFERENCES provider_credential_bindings(id,connection_id) ON DELETE RESTRICT,
    ADD CONSTRAINT call_logs_route_runtime_release_fk
        FOREIGN KEY (route_runtime_release_id)
        REFERENCES runtime_change_releases(id) ON DELETE RESTRICT;

ALTER TABLE request_trace_events
    ADD CONSTRAINT request_trace_events_execution_projection_check CHECK (
        (
            route_evidence_source IS NULL
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_deployment_id IS NULL AND route_connection_id IS NULL
            AND route_credential_binding_id IS NULL AND route_model_operation_id IS NULL
            AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
            AND route_channel_id IS NULL
        )
        OR (
            route_evidence_source IN ('response_cache','no_provider_attempt')
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_deployment_id IS NULL AND route_connection_id IS NULL
            AND route_credential_binding_id IS NULL AND route_model_operation_id IS NULL
            AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
            AND source_event_hash ~ '^[a-f0-9]{64}$'
        )
        OR (
            route_evidence_source = 'precontract_provider_attempt'
            AND COALESCE(BTRIM(attempt_id),'') <> ''
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_deployment_id IS NULL AND route_connection_id IS NULL
            AND route_credential_binding_id IS NULL AND route_model_operation_id IS NULL
            AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
            AND COALESCE(route_channel_id,0) > 0
            AND source_event_hash ~ '^[a-f0-9]{64}$'
        )
        OR (
            route_evidence_source = 'provider_attempt'
            AND COALESCE(BTRIM(attempt_id),'') <> ''
            AND route_evidence_version = 'unirouter.execution-route.v1'
            AND route_authority_mode IN ('legacy','verify','canonical')
            AND route_evidence_hash ~ '^sha256:[a-f0-9]{64}$'
            AND COALESCE(BTRIM(route_model_operation_id),'') <> ''
            AND route_model_operation_id = BTRIM(route_model_operation_id)
            AND route_model_operation_id <> 'legacy.any.v1'
            AND route_model_operation_id !~ E'[\\x00\\r\\n]'
            AND source_event_hash ~ '^[a-f0-9]{64}$'
            AND (
                route_authority_mode='legacy'
                AND COALESCE(route_channel_id,0)>0
                AND route_public_model_id IS NULL AND route_deployment_id IS NULL
                AND route_connection_id IS NULL AND route_credential_binding_id IS NULL
                AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
                OR route_authority_mode='verify'
                AND COALESCE(route_channel_id,0)>0 AND COALESCE(route_public_model_id,0)>0
                AND COALESCE(route_deployment_id,0)>0 AND COALESCE(route_connection_id,0)>0
                AND COALESCE(route_credential_binding_id,0)>0 AND COALESCE(route_runtime_release_id,0)>0
                AND COALESCE(route_snapshot_hash,'') ~ '^mrs1_[a-f0-9]{64}$'
                OR route_authority_mode='canonical'
                AND route_channel_id IS NULL AND COALESCE(route_public_model_id,0)>0
                AND COALESCE(route_deployment_id,0)>0 AND COALESCE(route_connection_id,0)>0
                AND COALESCE(route_credential_binding_id,0)>0 AND COALESCE(route_runtime_release_id,0)>0
                AND COALESCE(route_snapshot_hash,'') ~ '^mrs1_[a-f0-9]{64}$'
            )
        )
    ),
    ADD CONSTRAINT request_trace_events_route_public_model_org_fk
        FOREIGN KEY (route_public_model_id,org_id)
        REFERENCES model_registry(id,org_id) ON DELETE RESTRICT,
    ADD CONSTRAINT request_trace_events_route_deployment_org_fk
        FOREIGN KEY (org_id,route_deployment_id)
        REFERENCES model_deployments(customer_org_id,id) ON DELETE RESTRICT,
    ADD CONSTRAINT request_trace_events_route_deployment_connection_fk
        FOREIGN KEY (route_deployment_id,route_connection_id)
        REFERENCES model_deployments(id,connection_id) ON DELETE RESTRICT,
    ADD CONSTRAINT request_trace_events_route_credential_connection_fk
        FOREIGN KEY (route_credential_binding_id,route_connection_id)
        REFERENCES provider_credential_bindings(id,connection_id) ON DELETE RESTRICT,
    ADD CONSTRAINT request_trace_events_route_runtime_release_fk
        FOREIGN KEY (route_runtime_release_id)
        REFERENCES runtime_change_releases(id) ON DELETE RESTRICT;

-- The copied receipt identity is immutable while the row exists. DELETE is
-- intentionally left to the existing log retention/reset lifecycle; the full
-- append-only dispatch authority remains billing_provider_attempts.
CREATE FUNCTION enforce_execution_projection_attempt_match()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND (
        OLD.route_evidence_source IS NOT NULL OR NEW.route_evidence_source IS NOT NULL
    ) AND ROW(
        NEW.org_id,NEW.request_id,NEW.attempt_id,NEW.source_event_hash,
        NEW.route_evidence_source,NEW.route_evidence_version,NEW.route_authority_mode,
        NEW.route_evidence_hash,NEW.route_public_model_id,NEW.route_deployment_id,
        NEW.route_connection_id,NEW.route_credential_binding_id,NEW.route_model_operation_id,
        NEW.route_runtime_release_id,NEW.route_snapshot_hash,NEW.route_channel_id
    ) IS DISTINCT FROM ROW(
        OLD.org_id,OLD.request_id,OLD.attempt_id,OLD.source_event_hash,
        OLD.route_evidence_source,OLD.route_evidence_version,OLD.route_authority_mode,
        OLD.route_evidence_hash,OLD.route_public_model_id,OLD.route_deployment_id,
        OLD.route_connection_id,OLD.route_credential_binding_id,OLD.route_model_operation_id,
        OLD.route_runtime_release_id,OLD.route_snapshot_hash,OLD.route_channel_id
    ) THEN
        RAISE EXCEPTION 'execution evidence projection is immutable'
            USING ERRCODE='55000';
    END IF;

    IF NEW.route_evidence_source='provider_attempt' THEN
        PERFORM 1
          FROM billing_provider_attempts attempt
         WHERE attempt.org_id=NEW.org_id
           AND attempt.request_id=NEW.request_id
           AND attempt.attempt_id=NEW.attempt_id
           AND ROW(
               NEW.route_evidence_version,NEW.route_authority_mode,NEW.route_evidence_hash,
               NEW.route_public_model_id,NEW.route_deployment_id,NEW.route_connection_id,
               NEW.route_credential_binding_id,NEW.route_model_operation_id,
               NEW.route_runtime_release_id,NEW.route_snapshot_hash,NEW.route_channel_id
           ) IS NOT DISTINCT FROM ROW(
               attempt.route_evidence_version,attempt.route_authority_mode,attempt.route_evidence_hash,
               attempt.route_public_model_id,attempt.deployment_id,attempt.route_connection_id,
               attempt.route_credential_binding_id,attempt.route_model_operation_id,
               attempt.route_runtime_release_id,attempt.route_snapshot_hash,attempt.channel_id
           );
        IF NOT FOUND THEN
            RAISE EXCEPTION 'execution projection does not match exact provider attempt'
                USING ERRCODE='23503';
        END IF;
    ELSIF NEW.route_evidence_source='precontract_provider_attempt' THEN
        PERFORM 1
          FROM billing_provider_attempts attempt
         WHERE attempt.org_id=NEW.org_id
           AND attempt.request_id=NEW.request_id
           AND attempt.attempt_id=NEW.attempt_id
           AND attempt.route_evidence_version IS NULL
           AND attempt.channel_id IS NOT DISTINCT FROM NEW.route_channel_id;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'pre-contract projection does not match exact provider attempt'
                USING ERRCODE='23503';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER call_logs_execution_projection_guard
BEFORE INSERT OR UPDATE ON call_logs
FOR EACH ROW EXECUTE FUNCTION enforce_execution_projection_attempt_match();

CREATE TRIGGER request_trace_events_execution_projection_guard
BEFORE INSERT OR UPDATE ON request_trace_events
FOR EACH ROW EXECUTE FUNCTION enforce_execution_projection_attempt_match();

CREATE INDEX call_logs_execution_projection_idx
    ON call_logs(org_id,route_public_model_id,route_deployment_id,created_at)
    WHERE route_evidence_source='provider_attempt';
CREATE INDEX request_trace_events_execution_projection_idx
    ON request_trace_events(org_id,route_public_model_id,route_deployment_id,created_at,id)
    WHERE route_evidence_source='provider_attempt';

COMMENT ON COLUMN call_logs.route_evidence_source IS
    'provider_attempt, precontract_provider_attempt, response_cache, or no_provider_attempt; NULL is retained historical unknown evidence.';
COMMENT ON COLUMN request_trace_events.route_evidence_source IS
    'provider_attempt, precontract_provider_attempt, response_cache, or no_provider_attempt; NULL is retained historical unknown evidence.';
COMMENT ON COLUMN call_logs.route_evidence_hash IS
    'Immutable full execution-route receipt digest copied through executionevidence.CompactProjection.';
COMMENT ON COLUMN request_trace_events.route_evidence_hash IS
    'Immutable full execution-route receipt digest copied through executionevidence.CompactProjection.';

COMMIT;
