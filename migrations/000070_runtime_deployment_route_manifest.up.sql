BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE runtime_change_releases, runtime_change_slots,
    model_deployment_operations, model_deployment_capabilities,
    provider_connections, channels, model_deployments,
    provider_credential_bindings, channel_keys,
    credential_binding_deployments, runtime_route_revision_items,
    model_deployment_probes, model_registry, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- P2 keeps the existing release/slot lifecycle, but gives a canonical release
-- an explicit public-model identity and a sealed, normalized deployment-route
-- manifest. Historical v1 channel candidates are not rewritten.
ALTER TABLE runtime_change_releases
    DROP CONSTRAINT runtime_change_releases_identity_check;

ALTER TABLE runtime_change_releases
    ADD COLUMN public_model_id BIGINT REFERENCES model_registry(id) ON DELETE RESTRICT,
    ADD COLUMN route_items_hash VARCHAR(71),
    ADD COLUMN route_items_count INTEGER,
    ADD COLUMN route_items_sealed_at TIMESTAMPTZ(6),
    ADD CONSTRAINT runtime_change_releases_identity_check CHECK (
        release_ref ~ '^rcr_[a-z0-9][a-z0-9._-]{2,63}$'
        AND model_name <> ''
        AND candidate_version IN (
            'unirouter.runtime-change-candidate.v1',
            'unirouter.runtime-change-candidate.v2'
        )
        AND candidate_hash ~ '^sha256:[a-f0-9]{64}$'
        AND baseline_hash ~ '^sha256:[a-f0-9]{64}$'
        AND jsonb_typeof(candidate) = 'object'
        AND candidate->>'contract_version' = candidate_version
        AND jsonb_typeof(provider_pack_pins) = 'array'
        AND jsonb_typeof(simulation_report) = 'object'
        AND reason <> ''
        AND (
            (
                candidate_version = 'unirouter.runtime-change-candidate.v1'
                AND (
                    (
                        public_model_id IS NULL
                        AND route_items_hash IS NULL
                        AND route_items_count IS NULL
                        AND route_items_sealed_at IS NULL
                    )
                    OR
                    (
                        public_model_id IS NOT NULL
                        AND route_items_hash ~ '^sha256:[a-f0-9]{64}$'
                        AND route_items_count > 0
                        AND route_items_sealed_at IS NOT NULL
                    )
                )
            )
            OR
            (
                candidate_version = 'unirouter.runtime-change-candidate.v2'
                AND public_model_id IS NOT NULL
                AND (candidate->>'public_model_id')::BIGINT = public_model_id
                AND candidate->>'model_name' = model_name
                AND jsonb_typeof(candidate->'deployment_routes') = 'array'
                AND route_items_hash ~ '^sha256:[a-f0-9]{64}$'
                AND route_items_count > 0
                AND jsonb_array_length(candidate->'deployment_routes') = route_items_count
            )
        )
    );

CREATE INDEX runtime_change_releases_public_model_stage_idx
    ON runtime_change_releases(public_model_id, stage, updated_at DESC, id DESC)
    WHERE public_model_id IS NOT NULL;

ALTER TABLE runtime_change_slots
    ADD COLUMN public_model_id BIGINT REFERENCES model_registry(id) ON DELETE RESTRICT;

ALTER TABLE model_deployment_operations
    ADD COLUMN protocol_profile_id VARCHAR(100),
    ADD COLUMN protocol_profile_version VARCHAR(40);

-- There was no supported non-legacy operation writer before this migration.
-- A protocol profile is an execution contract, so it must never be guessed
-- from an operation name, adapter type, or provider label during migration.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM model_deployment_operations
         WHERE operation_id <> 'legacy.any.v1'
    ) THEN
        RAISE EXCEPTION 'Runtime deployment route manifest migration refused: non-legacy operations require an explicit protocol profile';
    END IF;
END;
$$;

UPDATE model_deployment_operations
   SET protocol_profile_id='legacy.channel_model_probe',
       protocol_profile_version='v1'
 WHERE operation_id='legacy.any.v1';

ALTER TABLE model_deployment_operations
    ALTER COLUMN protocol_profile_id SET NOT NULL,
    ALTER COLUMN protocol_profile_version SET NOT NULL;

ALTER TABLE model_deployment_operations
    ADD CONSTRAINT model_deployment_operations_protocol_profile_check CHECK (
        protocol_profile_id=BTRIM(protocol_profile_id)
        AND BTRIM(protocol_profile_id)<>''
        AND protocol_profile_id !~ E'[\\x00\\r\\n]'
        AND protocol_profile_version=BTRIM(protocol_profile_version)
        AND BTRIM(protocol_profile_version)<>''
        AND protocol_profile_version !~ E'[\\x00\\r\\n]'
        AND (
            (operation_id='legacy.any.v1'
             AND protocol_profile_id='legacy.channel_model_probe'
             AND protocol_profile_version='v1')
            OR
            (operation_id<>'legacy.any.v1'
             AND protocol_profile_id<>'legacy.any.v1'
             AND protocol_profile_id<>'legacy.channel_model_probe')
        )
    );

CREATE FUNCTION enforce_model_deployment_operation_revision()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.deployment_id IS DISTINCT FROM OLD.deployment_id
       OR NEW.operation_id IS DISTINCT FROM OLD.operation_id THEN
        RAISE EXCEPTION 'model deployment operation identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.revision < OLD.revision OR NEW.revision > OLD.revision+1 THEN
        RAISE EXCEPTION 'model deployment operation revision must not decrease or skip'
            USING ERRCODE = '23514';
    END IF;
    IF (
        NEW.support_state IS DISTINCT FROM OLD.support_state
        OR NEW.evidence_state IS DISTINCT FROM OLD.evidence_state
        OR NEW.evidence_ref IS DISTINCT FROM OLD.evidence_ref
        OR NEW.verified_at IS DISTINCT FROM OLD.verified_at
        OR NEW.protocol_profile_id IS DISTINCT FROM OLD.protocol_profile_id
        OR NEW.protocol_profile_version IS DISTINCT FROM OLD.protocol_profile_version
    ) AND NEW.revision <> OLD.revision+1 THEN
        RAISE EXCEPTION 'model deployment operation semantic changes require the next revision'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_deployment_operations_revision_guard
BEFORE UPDATE ON model_deployment_operations
FOR EACH ROW EXECUTE FUNCTION enforce_model_deployment_operation_revision();

CREATE FUNCTION enforce_model_deployment_capability_revision()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.deployment_id IS DISTINCT FROM OLD.deployment_id
       OR NEW.operation_id IS DISTINCT FROM OLD.operation_id
       OR NEW.capability_id IS DISTINCT FROM OLD.capability_id THEN
        RAISE EXCEPTION 'model deployment capability identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.revision < OLD.revision OR NEW.revision > OLD.revision+1 THEN
        RAISE EXCEPTION 'model deployment capability revision must not decrease or skip'
            USING ERRCODE = '23514';
    END IF;
    IF (
        NEW.support_state IS DISTINCT FROM OLD.support_state
        OR NEW.evidence_state IS DISTINCT FROM OLD.evidence_state
        OR NEW.evidence_ref IS DISTINCT FROM OLD.evidence_ref
        OR NEW.verified_at IS DISTINCT FROM OLD.verified_at
    ) AND NEW.revision <> OLD.revision+1 THEN
        RAISE EXCEPTION 'model deployment capability semantic changes require the next revision'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_deployment_capabilities_revision_guard
BEFORE UPDATE ON model_deployment_capabilities
FOR EACH ROW EXECUTE FUNCTION enforce_model_deployment_capability_revision();

CREATE FUNCTION enforce_provider_connection_revision()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE source_revision BIGINT;
BEGIN
    IF NEW.config_revision < OLD.config_revision THEN
        RAISE EXCEPTION 'provider connection config revision must not decrease or skip'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.config_revision > OLD.config_revision+1 THEN
        IF OLD.legacy_channel_id IS NULL THEN
            RAISE EXCEPTION 'provider connection config revision must not decrease or skip'
                USING ERRCODE = '23514';
        END IF;
        SELECT routing_epoch INTO source_revision FROM channels WHERE id=OLD.legacy_channel_id;
        IF source_revision IS NULL OR NEW.config_revision <> source_revision THEN
            RAISE EXCEPTION 'provider connection config revision jump must match the legacy source epoch'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF (
        NEW.provider IS DISTINCT FROM OLD.provider
        OR NEW.base_url IS DISTINCT FROM OLD.base_url
        OR NEW.adapter_type IS DISTINCT FROM OLD.adapter_type
        OR NEW.adapter_config IS DISTINCT FROM OLD.adapter_config
        -- Disabling is an emergency safety contraction and deliberately keeps
        -- the sealed configuration identity stable. Native/canonical resume
        -- expands execution authority and invalidates the old manifest/probe
        -- pins. Before any v2 authority is current, a legacy-provenance row may
        -- still mirror the legacy status-only resume at the same routing epoch;
        -- verify parity rejects a unilateral canonical resume.
        OR (
            NEW.admin_state IS DISTINCT FROM OLD.admin_state
            AND NEW.admin_state = 'enabled'
            AND (
                OLD.legacy_channel_id IS NULL
                OR EXISTS (
                    SELECT 1
                      FROM runtime_change_slots slot
                     CROSS JOIN LATERAL (
                           VALUES (slot.stable_release_id), (slot.canary_release_id)
                     ) current_release(release_id)
                      JOIN runtime_change_releases release
                        ON release.id=current_release.release_id
                     WHERE release.candidate_version='unirouter.runtime-change-candidate.v2'
                )
            )
        )
        OR NEW.retired_at IS DISTINCT FROM OLD.retired_at
    ) AND NEW.config_revision <= OLD.config_revision THEN
        RAISE EXCEPTION 'provider connection semantic changes require the next config revision'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER provider_connections_revision_guard
BEFORE UPDATE ON provider_connections
FOR EACH ROW EXECUTE FUNCTION enforce_provider_connection_revision();

CREATE FUNCTION enforce_model_deployment_revisions()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE source_config_revision BIGINT;
DECLARE source_circuit_revision BIGINT;
BEGIN
    IF NEW.config_revision < OLD.config_revision THEN
        RAISE EXCEPTION 'model deployment config revision must not decrease or skip'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.config_revision > OLD.config_revision+1 THEN
        IF OLD.legacy_channel_id IS NULL THEN
            RAISE EXCEPTION 'model deployment config revision must not decrease or skip'
                USING ERRCODE = '23514';
        END IF;
        SELECT routing_epoch INTO source_config_revision FROM channels WHERE id=OLD.legacy_channel_id;
        IF source_config_revision IS NULL OR NEW.config_revision <> source_config_revision THEN
            RAISE EXCEPTION 'model deployment config revision jump must match the legacy source epoch'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF (
        NEW.retired_at IS DISTINCT FROM OLD.retired_at
        -- As with connections, pause is an independent safety gate while
        -- resume is an authority expansion that requires fresh evidence.
        OR (
            NEW.admin_state IS DISTINCT FROM OLD.admin_state
            AND NEW.admin_state = 'enabled'
        )
        OR NEW.priority IS DISTINCT FROM OLD.priority
        OR NEW.routing_weight IS DISTINCT FROM OLD.routing_weight
        OR NEW.max_context IS DISTINCT FROM OLD.max_context
        OR NEW.default_max_output IS DISTINCT FROM OLD.default_max_output
        OR NEW.degradation_threshold IS DISTINCT FROM OLD.degradation_threshold
    ) AND NEW.config_revision <= OLD.config_revision THEN
        RAISE EXCEPTION 'model deployment semantic changes require the next config revision'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.circuit_revision < OLD.circuit_revision THEN
        RAISE EXCEPTION 'model deployment circuit revision must not decrease or skip'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.circuit_revision > OLD.circuit_revision+1 THEN
        IF OLD.legacy_channel_id IS NULL THEN
            RAISE EXCEPTION 'model deployment circuit revision must not decrease or skip'
                USING ERRCODE = '23514';
        END IF;
        SELECT circuit_epoch INTO source_circuit_revision FROM channels WHERE id=OLD.legacy_channel_id;
        IF source_circuit_revision IS NULL OR NEW.circuit_revision <> source_circuit_revision THEN
            RAISE EXCEPTION 'model deployment circuit revision jump must match the legacy source epoch'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF (
        NEW.circuit_state IS DISTINCT FROM OLD.circuit_state
        OR NEW.half_open_until IS DISTINCT FROM OLD.half_open_until
    ) AND NEW.circuit_revision <= OLD.circuit_revision THEN
        RAISE EXCEPTION 'model deployment breaker changes require the next circuit revision'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_deployments_revision_guard
BEFORE UPDATE ON model_deployments
FOR EACH ROW EXECUTE FUNCTION enforce_model_deployment_revisions();

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM model_deployments
         WHERE (half_open_probe_token IS NULL) <> (half_open_probe_until IS NULL)
            OR (half_open_probe_token IS NOT NULL
                AND (
                    admin_state <> 'enabled'
                    OR circuit_state <> 'half_open'
                    OR half_open_until IS NULL
                    OR BTRIM(half_open_probe_token) = ''
                    OR half_open_probe_token ~ E'[\\x00\\r\\n]'
                ))
    ) THEN
        RAISE EXCEPTION 'Runtime deployment route manifest migration refused: existing probe lease state is structurally invalid; drain writers and clear stale ownership first';
    END IF;
END;
$$;

ALTER TABLE model_deployments
    ADD CONSTRAINT model_deployments_probe_lease_check CHECK (
        (half_open_probe_token IS NULL AND half_open_probe_until IS NULL)
        OR
        (half_open_probe_token IS NOT NULL AND half_open_probe_until IS NOT NULL
         AND BTRIM(half_open_probe_token) <> ''
         AND half_open_probe_token !~ E'[\\x00\\r\\n]'
         AND admin_state='enabled' AND circuit_state='half_open'
         AND half_open_until IS NOT NULL)
    );

-- Probe ownership is a short-lived cross-replica lease, not a breaker-state
-- revision. Claims and renewals preserve the circuit revision so a selected
-- half-open plan does not invalidate itself; only breaker state transitions
-- advance the revision. The request path still supplies the exact old token in
-- its UPDATE predicate, while this trigger rejects structurally unsafe writes.
CREATE FUNCTION enforce_model_deployment_probe_lease()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    observed_at TIMESTAMPTZ := statement_timestamp();
    breaker_changed BOOLEAN;
    lease_changed BOOLEAN;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF (NEW.half_open_probe_token IS NULL) <> (NEW.half_open_probe_until IS NULL) THEN
            RAISE EXCEPTION 'probe lease token and expiry must be present together'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.half_open_probe_token IS NULL THEN
            RETURN NEW;
        END IF;
        IF NEW.admin_state <> 'enabled'
           OR NEW.circuit_state <> 'half_open'
           OR NEW.half_open_until IS NULL
           OR NEW.half_open_until > observed_at THEN
            RAISE EXCEPTION 'probe lease requires a recoverable half-open deployment'
                USING ERRCODE = '23514';
        END IF;
        IF BTRIM(NEW.half_open_probe_token) = ''
           OR NEW.half_open_probe_token ~ E'[\\x00\\r\\n]'
           OR NEW.half_open_probe_until <= observed_at
           OR NEW.half_open_probe_until > observed_at + INTERVAL '300 seconds' THEN
            RAISE EXCEPTION 'probe lease identity or expiry is invalid'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    breaker_changed := NEW.circuit_state IS DISTINCT FROM OLD.circuit_state
        OR NEW.half_open_until IS DISTINCT FROM OLD.half_open_until;
    lease_changed := NEW.half_open_probe_until IS DISTINCT FROM OLD.half_open_probe_until
        OR NEW.half_open_probe_token IS DISTINCT FROM OLD.half_open_probe_token;

    IF breaker_changed THEN
        IF NEW.half_open_probe_until IS NOT NULL OR NEW.half_open_probe_token IS NOT NULL THEN
            RAISE EXCEPTION 'breaker transitions must release probe ownership'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    IF (NEW.half_open_probe_token IS NULL) <> (NEW.half_open_probe_until IS NULL) THEN
        RAISE EXCEPTION 'probe lease token and expiry must be present together'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.admin_state <> 'enabled' AND NEW.half_open_probe_token IS NOT NULL THEN
        RAISE EXCEPTION 'disabled deployment cannot retain a probe lease'
            USING ERRCODE = '23514';
    END IF;

    IF NOT lease_changed THEN
        RETURN NEW;
    END IF;

    IF NEW.circuit_revision <> OLD.circuit_revision THEN
        RAISE EXCEPTION 'probe lease changes must preserve the circuit revision'
            USING ERRCODE = '23514';
    END IF;

    -- Disabling or retiring a deployment may only clear an existing lease.
    IF NEW.admin_state <> 'enabled' THEN
        IF OLD.half_open_probe_token IS NOT NULL
           AND NEW.half_open_probe_token IS NULL
           AND NEW.half_open_probe_until IS NULL THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'disabled deployment cannot acquire or renew a probe lease'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.circuit_state <> 'half_open'
       OR NEW.half_open_until IS NULL
       OR NEW.half_open_until > observed_at THEN
        RAISE EXCEPTION 'probe lease requires a recoverable half-open deployment'
            USING ERRCODE = '23514';
    END IF;

    -- Exact-token release is enforced by the writer's compare-and-swap WHERE
    -- clause; the row transition itself must clear both lease columns.
    IF OLD.half_open_probe_token IS NOT NULL
       AND NEW.half_open_probe_token IS NULL
       AND NEW.half_open_probe_until IS NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.half_open_probe_token IS NULL
       OR NEW.half_open_probe_until IS NULL
       OR BTRIM(NEW.half_open_probe_token) = ''
       OR NEW.half_open_probe_token ~ E'[\\x00\\r\\n]'
       OR NEW.half_open_probe_until <= observed_at
       OR NEW.half_open_probe_until > observed_at + INTERVAL '300 seconds' THEN
        RAISE EXCEPTION 'probe lease identity or expiry is invalid'
            USING ERRCODE = '23514';
    END IF;

    -- Same-token renewal may only extend the lease. A new token may replace an
    -- absent or expired owner, never a live owner.
    IF OLD.half_open_probe_token = NEW.half_open_probe_token THEN
        IF OLD.half_open_probe_until IS NULL
           OR NEW.half_open_probe_until <= OLD.half_open_probe_until THEN
            RAISE EXCEPTION 'probe lease renewal must extend the same token'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.half_open_probe_token IS NULL
       OR OLD.half_open_probe_until IS NULL
       OR OLD.half_open_probe_until <= observed_at THEN
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'live probe lease token cannot be replaced'
        USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER model_deployments_probe_lease_guard
BEFORE INSERT OR UPDATE ON model_deployments
FOR EACH ROW EXECUTE FUNCTION enforce_model_deployment_probe_lease();

CREATE FUNCTION enforce_provider_credential_binding_revision()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE source_revision BIGINT;
BEGIN
    IF NEW.state_revision < OLD.state_revision THEN
        RAISE EXCEPTION 'provider credential state revision must not decrease or skip'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.state_revision > OLD.state_revision+1 THEN
        IF OLD.legacy_channel_key_id IS NULL THEN
            RAISE EXCEPTION 'provider credential state revision must not decrease or skip'
                USING ERRCODE = '23514';
        END IF;
        SELECT state_epoch INTO source_revision FROM channel_keys WHERE id=OLD.legacy_channel_key_id;
        IF source_revision IS NULL OR NEW.state_revision <> source_revision THEN
            RAISE EXCEPTION 'provider credential state revision jump must match the legacy source epoch'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF (
        NEW.key_enc IS DISTINCT FROM OLD.key_enc
        OR NEW.status IS DISTINCT FROM OLD.status
        OR NEW.retired_at IS DISTINCT FROM OLD.retired_at
        OR NEW.cooldown_until IS DISTINCT FROM OLD.cooldown_until
    ) AND NEW.state_revision <= OLD.state_revision THEN
        RAISE EXCEPTION 'provider credential semantic changes require the next state revision'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER provider_credential_bindings_revision_guard
BEFORE UPDATE ON provider_credential_bindings
FOR EACH ROW EXECUTE FUNCTION enforce_provider_credential_binding_revision();

CREATE FUNCTION reject_credential_deployment_link_update()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'credential deployment scope links are immutable; delete and insert a new scope epoch'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER credential_binding_deployments_update_guard
BEFORE UPDATE ON credential_binding_deployments
FOR EACH ROW EXECUTE FUNCTION reject_credential_deployment_link_update();

ALTER TABLE runtime_route_revision_items
    ADD COLUMN expected_operation_revision BIGINT,
    ADD COLUMN protocol_profile_id VARCHAR(100),
    ADD COLUMN protocol_profile_version VARCHAR(40),
    ADD CONSTRAINT runtime_route_revision_items_operation_revision_check
        CHECK (expected_operation_revision IS NULL OR expected_operation_revision > 0),
    ADD CONSTRAINT runtime_route_revision_items_protocol_profile_check CHECK (
        (protocol_profile_id IS NULL AND protocol_profile_version IS NULL)
        OR
        (
            protocol_profile_id=BTRIM(protocol_profile_id)
            AND BTRIM(protocol_profile_id)<>''
            AND protocol_profile_id !~ E'[\\x00\\r\\n]'
            AND protocol_profile_version=BTRIM(protocol_profile_version)
            AND BTRIM(protocol_profile_version)<>''
            AND protocol_profile_version !~ E'[\\x00\\r\\n]'
        )
    );

CREATE UNIQUE INDEX runtime_change_slots_public_model_unique
    ON runtime_change_slots(public_model_id)
    WHERE public_model_id IS NOT NULL;

CREATE INDEX model_deployment_probes_binding_latest_idx
    ON model_deployment_probes(
        deployment_id,operation_id,credential_binding_id,tested_at DESC,probe_id DESC
    )
    WHERE credential_binding_id IS NOT NULL;

CREATE FUNCTION enforce_runtime_route_item_identity()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    release_version VARCHAR(80);
    release_model_id BIGINT;
    sealed_at TIMESTAMPTZ(6);
    source_sealed_at TIMESTAMPTZ(6);
    deployment_model_id BIGINT;
BEGIN
    IF TG_OP IN ('UPDATE','DELETE') THEN
        SELECT route_items_sealed_at
          INTO source_sealed_at
          FROM runtime_change_releases
         WHERE id = OLD.release_id
         FOR SHARE;
        IF source_sealed_at IS NOT NULL THEN
            RAISE EXCEPTION 'sealed runtime route items are append-only'
                USING ERRCODE = '55000';
        END IF;
    END IF;

    IF TG_OP='UPDATE'
       AND (NEW.release_id IS DISTINCT FROM OLD.release_id
            OR NEW.ordinal IS DISTINCT FROM OLD.ordinal) THEN
        RAISE EXCEPTION 'runtime route item release and ordinal identity are immutable'
            USING ERRCODE = '55000';
    END IF;

    IF TG_OP='DELETE' THEN
        RETURN OLD;
    END IF;

    SELECT candidate_version, public_model_id, route_items_sealed_at
      INTO release_version, release_model_id, sealed_at
      FROM runtime_change_releases
     WHERE id = NEW.release_id
     FOR SHARE;
    IF sealed_at IS NOT NULL THEN
        RAISE EXCEPTION 'sealed runtime route items are append-only'
            USING ERRCODE = '55000';
    END IF;

    IF release_version = 'unirouter.runtime-change-candidate.v2' THEN
        IF NEW.source_candidate_version <> release_version THEN
            RAISE EXCEPTION 'runtime route item candidate version mismatch'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.expected_operation_revision IS NULL OR NEW.expected_operation_revision <= 0 THEN
            RAISE EXCEPTION 'canonical runtime route item must pin an operation revision'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.operation_id='legacy.any.v1'
           OR NEW.protocol_profile_id IS NULL
           OR NEW.protocol_profile_version IS NULL
           OR NEW.protocol_profile_id IN ('legacy.any.v1','legacy.channel_model_probe') THEN
            RAISE EXCEPTION 'canonical runtime route item requires an explicit non-legacy protocol profile'
                USING ERRCODE = '23514';
        END IF;
        SELECT public_model_id INTO deployment_model_id
          FROM model_deployments WHERE id = NEW.deployment_id;
        IF deployment_model_id IS DISTINCT FROM release_model_id THEN
            RAISE EXCEPTION 'runtime route item public model mismatch'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER runtime_route_revision_items_seal_guard
BEFORE INSERT OR UPDATE OR DELETE ON runtime_route_revision_items
FOR EACH ROW EXECUTE FUNCTION enforce_runtime_route_item_identity();

CREATE FUNCTION enforce_runtime_release_manifest_seal()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    item_count INTEGER;
BEGIN
    IF OLD.route_items_sealed_at IS NOT NULL AND (
        NEW.release_ref IS DISTINCT FROM OLD.release_ref
        OR NEW.model_name IS DISTINCT FROM OLD.model_name
        OR NEW.candidate_version IS DISTINCT FROM OLD.candidate_version
        OR NEW.candidate IS DISTINCT FROM OLD.candidate
        OR NEW.candidate_hash IS DISTINCT FROM OLD.candidate_hash
        OR NEW.baseline_hash IS DISTINCT FROM OLD.baseline_hash
        OR NEW.public_model_id IS DISTINCT FROM OLD.public_model_id
        OR NEW.route_items_hash IS DISTINCT FROM OLD.route_items_hash
        OR NEW.route_items_count IS DISTINCT FROM OLD.route_items_count
        OR NEW.route_items_sealed_at IS DISTINCT FROM OLD.route_items_sealed_at
        OR NEW.parent_stable_release_id IS DISTINCT FROM OLD.parent_stable_release_id
        OR NEW.provider_pack_pins IS DISTINCT FROM OLD.provider_pack_pins
        OR NEW.simulation_report IS DISTINCT FROM OLD.simulation_report
        OR NEW.overall_risk IS DISTINCT FROM OLD.overall_risk
        OR NEW.reason IS DISTINCT FROM OLD.reason
        OR NEW.created_by IS DISTINCT FROM OLD.created_by
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
    ) THEN
        RAISE EXCEPTION 'sealed runtime deployment manifest is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF OLD.route_items_sealed_at IS NULL
       AND NEW.route_items_sealed_at IS NOT NULL THEN
        IF NEW.candidate_version NOT IN (
            'unirouter.runtime-change-candidate.v1',
            'unirouter.runtime-change-candidate.v2'
        ) THEN
            RAISE EXCEPTION 'unsupported runtime route manifest seal version'
                USING ERRCODE = '23514';
        END IF;
        SELECT COUNT(*) INTO item_count
          FROM runtime_route_revision_items
         WHERE release_id = NEW.id;
        IF item_count <> NEW.route_items_count THEN
            RAISE EXCEPTION 'runtime deployment manifest item count mismatch'
                USING ERRCODE = '23514';
        END IF;
        IF EXISTS (
            SELECT 1 FROM runtime_route_revision_items
             WHERE release_id=NEW.id
               AND (
                   expected_operation_revision IS NULL
                   OR protocol_profile_id IS NULL
                   OR protocol_profile_version IS NULL
               )
        ) THEN
            RAISE EXCEPTION 'sealed runtime route items must pin operation revisions'
                USING ERRCODE = '23514';
        END IF;
        IF EXISTS (
            SELECT 1
              FROM runtime_route_revision_items item
              LEFT JOIN model_deployments deployment
                ON deployment.id=item.deployment_id
              LEFT JOIN provider_connections connection
                ON connection.id=deployment.connection_id
              LEFT JOIN model_deployment_operations operation
                ON operation.deployment_id=item.deployment_id
               AND operation.operation_id=item.operation_id
             WHERE item.release_id=NEW.id
               AND (
                   deployment.id IS NULL
                   OR connection.id IS NULL
                   OR connection.config_revision IS DISTINCT FROM item.expected_connection_revision
                   OR deployment.config_revision IS DISTINCT FROM item.expected_deployment_revision
                   OR operation.deployment_id IS NULL
                   OR operation.revision IS DISTINCT FROM item.expected_operation_revision
                   OR operation.protocol_profile_id IS DISTINCT FROM item.protocol_profile_id
                   OR operation.protocol_profile_version IS DISTINCT FROM item.protocol_profile_version
               )
        ) THEN
            RAISE EXCEPTION 'runtime route item connection/deployment/operation/profile revision mismatch'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.candidate_version='unirouter.runtime-change-candidate.v2'
           AND EXISTS (
               SELECT 1
                 FROM runtime_route_revision_items item
                 JOIN model_deployments deployment ON deployment.id=item.deployment_id
                 JOIN provider_connections connection ON connection.id=deployment.connection_id
                 JOIN model_deployment_operations operation
                   ON operation.deployment_id=item.deployment_id
                  AND operation.operation_id=item.operation_id
                WHERE item.release_id=NEW.id
                  AND (
                      connection.admin_state<>'enabled'
                      OR deployment.admin_state<>'enabled'
                      OR operation.support_state<>'enabled'
                      OR operation.evidence_state NOT IN (
                          'live_probed','conformance_verified','production_approved'
                      )
                      OR operation.verified_at IS NULL
                      OR NOT EXISTS (
                          SELECT 1
                            FROM credential_binding_deployments link
                            JOIN provider_credential_bindings binding
                              ON binding.id=link.binding_id
                             AND binding.connection_id=link.connection_id
                            JOIN LATERAL (
                                SELECT probe.*
                                  FROM model_deployment_probes probe
                                 WHERE probe.deployment_id=deployment.id
                                   AND probe.operation_id=operation.operation_id
                                   AND probe.credential_binding_id=binding.id
                                 ORDER BY probe.tested_at DESC,probe.probe_id DESC
                                 LIMIT 1
                            ) probe ON TRUE
                           WHERE link.deployment_id=deployment.id
                             AND link.connection_id=deployment.connection_id
                             AND binding.status='active'
                             AND (binding.cooldown_until IS NULL OR binding.cooldown_until<=NOW())
                             AND probe.success
                             AND probe.upstream_model=deployment.upstream_model
                             AND probe.connection_revision=connection.config_revision
                             AND probe.deployment_revision=deployment.config_revision
                             AND probe.credential_state_revision=binding.state_revision
                             AND probe.probe_profile_id=operation.protocol_profile_id
                             AND probe.probe_profile_version=operation.protocol_profile_version
                             AND probe.tested_at>=link.created_at
                      )
                  )
           ) THEN
            RAISE EXCEPTION 'canonical runtime route item lacks current executable admission evidence'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER runtime_change_releases_manifest_seal_guard
BEFORE UPDATE ON runtime_change_releases
FOR EACH ROW EXECUTE FUNCTION enforce_runtime_release_manifest_seal();

CREATE FUNCTION enforce_runtime_slot_public_model()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    release_model_id BIGINT;
    release_version VARCHAR(80);
    release_sealed_at TIMESTAMPTZ(6);
    release_stage VARCHAR(24);
    release_model_name VARCHAR(160);
BEGIN
    IF NEW.stable_release_id IS NOT NULL THEN
        SELECT public_model_id,candidate_version,route_items_sealed_at,stage,model_name
          INTO release_model_id,release_version,release_sealed_at,release_stage,release_model_name
          FROM runtime_change_releases
         WHERE id=NEW.stable_release_id
         FOR KEY SHARE;
        IF NOT FOUND OR release_stage<>'active' OR release_model_name<>NEW.model_name THEN
            RAISE EXCEPTION 'runtime stable slot requires an active release'
                USING ERRCODE = '23514';
        END IF;

        IF NEW.public_model_id IS NULL THEN
            IF release_version = 'unirouter.runtime-change-candidate.v2' THEN
                RAISE EXCEPTION 'canonical runtime stable requires a public model slot identity'
                    USING ERRCODE = '23514';
            END IF;
        ELSIF release_model_id IS DISTINCT FROM NEW.public_model_id
              OR release_version <> 'unirouter.runtime-change-candidate.v2'
              OR release_sealed_at IS NULL THEN
            RAISE EXCEPTION 'runtime stable does not match its sealed public model manifest'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    IF NEW.canary_release_id IS NOT NULL THEN
        SELECT public_model_id,candidate_version,route_items_sealed_at,stage,model_name
          INTO release_model_id,release_version,release_sealed_at,release_stage,release_model_name
          FROM runtime_change_releases
         WHERE id=NEW.canary_release_id
         FOR KEY SHARE;
        IF NOT FOUND OR release_stage<>'canary' OR release_model_name<>NEW.model_name THEN
            RAISE EXCEPTION 'runtime canary slot requires a canary release'
                USING ERRCODE = '23514';
        END IF;

        IF NEW.public_model_id IS NULL THEN
            IF release_version = 'unirouter.runtime-change-candidate.v2' THEN
                RAISE EXCEPTION 'canonical runtime canary requires a public model slot identity'
                    USING ERRCODE = '23514';
            END IF;
        ELSIF release_model_id IS DISTINCT FROM NEW.public_model_id
              OR release_version <> 'unirouter.runtime-change-candidate.v2'
              OR release_sealed_at IS NULL THEN
            RAISE EXCEPTION 'runtime canary does not match its sealed public model manifest'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER runtime_change_slots_public_model_guard
BEFORE INSERT OR UPDATE ON runtime_change_slots
FOR EACH ROW EXECUTE FUNCTION enforce_runtime_slot_public_model();

COMMENT ON COLUMN runtime_change_releases.public_model_id IS
    'Explicit public-model identity for a sealed v1 compatibility patch or a v2 deployment-route manifest; NULL for unprepared v1 history.';
COMMENT ON COLUMN runtime_change_releases.route_items_hash IS
    'SHA-256 of ordered normalized rows: an override patch for sealed v1 compatibility evidence, or a full deployment_routes manifest for v2.';
COMMENT ON COLUMN runtime_change_releases.route_items_sealed_at IS
    'Once set, candidate identity and normalized route rows are append-only release evidence.';
COMMENT ON COLUMN runtime_change_slots.public_model_id IS
    'Canonical slot identity. A non-NULL value may reference only sealed v2 releases for that same public model.';
COMMENT ON COLUMN runtime_route_revision_items.expected_operation_revision IS
    'Pinned model-deployment operation revision for sealed v1 compatibility rows and v2 manifests; NULL only for unsealed v1 parity history.';
COMMENT ON COLUMN model_deployment_operations.protocol_profile_id IS
    'Versioned execution/conversion contract selected for this operation; it is never inferred from adapter_type.';
COMMENT ON COLUMN runtime_route_revision_items.protocol_profile_id IS
    'Protocol profile pinned into sealed route evidence and returned by canonical assignment resolution.';

COMMIT;
