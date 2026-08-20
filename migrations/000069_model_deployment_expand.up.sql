BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE channels, channel_keys, model_registry, runtime_change_releases,
    call_logs, billing_provider_attempts, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- Expand-only foundation for the canonical provider-connection and
-- model-deployment read model. Legacy channels/channel_keys remain the runtime
-- authority until a later, separately verified cutover.
CREATE TABLE provider_connections (
    id BIGSERIAL PRIMARY KEY,
    -- Immutable numeric provenance, deliberately not an FK: removing the
    -- legacy source must not erase or cascade-delete canonical evidence.
    legacy_channel_id BIGINT UNIQUE,
    name VARCHAR(255) NOT NULL,
    provider VARCHAR(50) NOT NULL,
    base_url VARCHAR(512) NOT NULL,
    adapter_type VARCHAR(50) NOT NULL DEFAULT 'openai_compatible',
    adapter_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    admin_state VARCHAR(20) NOT NULL DEFAULT 'enabled',
    retired_at TIMESTAMPTZ(6),
    config_revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT provider_connections_identity_check CHECK (
        BTRIM(name) <> '' AND BTRIM(provider) <> '' AND BTRIM(base_url) <> ''
        AND BTRIM(adapter_type) <> '' AND config_revision > 0
        AND (legacy_channel_id IS NULL OR legacy_channel_id > 0)
        AND jsonb_typeof(adapter_config) = 'object'
        AND pg_column_size(adapter_config) <= 65536
    ),
    CONSTRAINT provider_connections_admin_state_check CHECK (
        admin_state IN ('enabled', 'disabled', 'retired')
        AND (admin_state = 'retired') = (retired_at IS NOT NULL)
    )
);

CREATE INDEX provider_connections_provider_idx
    ON provider_connections(provider, id);
CREATE INDEX provider_connections_admin_state_idx
    ON provider_connections(admin_state, id);

-- Only a channel's connection-shaped fields can be copied without selecting a
-- customer organization. Model ownership is reconciled later by an explicit
-- customer-org-aware application service.
INSERT INTO provider_connections (
    legacy_channel_id,name,provider,base_url,adapter_type,adapter_config,
    admin_state,config_revision,created_at,updated_at)
SELECT c.id,c.name,c.provider,c.base_url,
       COALESCE(NULLIF(BTRIM(c.adapter_type),''),'openai_compatible'),
       COALESCE(c.adapter_config,'{}'::jsonb),
       CASE WHEN c.status='disabled' THEN 'disabled' ELSE 'enabled' END,
       c.routing_epoch,c.created_at,c.updated_at
  FROM channels c
ON CONFLICT (legacy_channel_id) DO NOTHING;

-- A provider connection is deployment-operator state; a model deployment is
-- the explicit customer-model-to-upstream binding. The composite registry FK
-- prevents a public model row from being attributed to another organization.
ALTER TABLE model_registry
    ADD CONSTRAINT model_registry_id_org_unique UNIQUE (id, org_id);

CREATE TABLE model_deployments (
    id BIGSERIAL PRIMARY KEY,
    customer_org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    public_model_id BIGINT NOT NULL,
    connection_id BIGINT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
    upstream_model VARCHAR(200) NOT NULL,
    admin_state VARCHAR(20) NOT NULL DEFAULT 'enabled',
    retired_at TIMESTAMPTZ(6),
    config_revision BIGINT NOT NULL DEFAULT 1,
    priority INTEGER NOT NULL DEFAULT 100,
    routing_weight INTEGER NOT NULL DEFAULT 100,
    max_context INTEGER NOT NULL DEFAULT 0,
    default_max_output INTEGER NOT NULL DEFAULT 0,
    degradation_threshold INTEGER NOT NULL DEFAULT 10,
    circuit_state VARCHAR(20) NOT NULL DEFAULT 'healthy',
    circuit_revision BIGINT NOT NULL DEFAULT 1,
    fail_count INTEGER NOT NULL DEFAULT 0,
    half_open_until TIMESTAMPTZ(6),
    half_open_probe_until TIMESTAMPTZ(6),
    half_open_probe_token VARCHAR(80),
    legacy_channel_id BIGINT,
    legacy_public_model VARCHAR(160),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT model_deployments_public_model_fk
        FOREIGN KEY (public_model_id, customer_org_id)
        REFERENCES model_registry(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT model_deployments_identity_check CHECK (
        BTRIM(upstream_model) <> '' AND upstream_model = BTRIM(upstream_model)
        AND upstream_model !~ E'[\\x00\\r\\n]'
        AND config_revision > 0 AND circuit_revision > 0
    ),
    CONSTRAINT model_deployments_admin_state_check CHECK (
        admin_state IN ('enabled', 'disabled', 'retired')
        AND (admin_state = 'retired') = (retired_at IS NOT NULL)
    ),
    CONSTRAINT model_deployments_routing_check CHECK (
        priority BETWEEN 1 AND 10000
        AND routing_weight BETWEEN 1 AND 1000
        AND max_context >= 0 AND default_max_output >= 0
        AND degradation_threshold BETWEEN 1 AND 100
        AND fail_count >= 0
        AND circuit_state IN ('healthy', 'half_open', 'open')
    ),
    CONSTRAINT model_deployments_legacy_identity_check CHECK (
        (legacy_channel_id IS NULL AND legacy_public_model IS NULL)
        OR (legacy_channel_id IS NOT NULL AND legacy_channel_id > 0
            AND legacy_public_model IS NOT NULL
            AND BTRIM(legacy_public_model) <> ''
            AND legacy_public_model = BTRIM(legacy_public_model))
    ),
    CONSTRAINT model_deployments_id_connection_unique UNIQUE (id, connection_id),
    CONSTRAINT model_deployments_customer_org_id_id_unique
        UNIQUE (customer_org_id, id)
);

CREATE UNIQUE INDEX model_deployments_active_connection_model_upstream_unique
    ON model_deployments(customer_org_id, connection_id, public_model_id, upstream_model)
    WHERE admin_state <> 'retired';
CREATE UNIQUE INDEX model_deployments_legacy_identity_unique
    ON model_deployments(customer_org_id, legacy_channel_id, legacy_public_model)
    WHERE legacy_channel_id IS NOT NULL AND admin_state <> 'retired';
CREATE INDEX model_deployments_public_model_route_idx
    ON model_deployments(customer_org_id, public_model_id, admin_state, priority, id);
CREATE INDEX model_deployments_connection_idx
    ON model_deployments(connection_id, id);

CREATE TABLE provider_credential_bindings (
    id BIGSERIAL PRIMARY KEY,
    connection_id BIGINT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
    key_enc TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    retired_at TIMESTAMPTZ(6),
    state_revision BIGINT NOT NULL DEFAULT 1,
    fail_count INTEGER NOT NULL DEFAULT 0,
    last_used_at TIMESTAMPTZ(6),
    cooldown_until TIMESTAMPTZ(6),
    -- Immutable numeric provenance, deliberately not an FK. A deleted legacy
    -- key is reconciled to retired state and its canonical secret is scrubbed.
    legacy_channel_key_id BIGINT UNIQUE,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT provider_credential_bindings_state_check CHECK (
        status IN ('active', 'disabled', 'invalid', 'retired')
        AND state_revision > 0 AND fail_count >= 0
        AND (legacy_channel_key_id IS NULL OR legacy_channel_key_id > 0)
        AND (status = 'retired') = (retired_at IS NOT NULL)
        AND (status = 'retired') = (key_enc = '')
    ),
    CONSTRAINT provider_credential_bindings_id_connection_unique
        UNIQUE (id, connection_id)
);

CREATE INDEX provider_credential_bindings_eligible_idx
    ON provider_credential_bindings(connection_id, status, cooldown_until, id);

INSERT INTO provider_credential_bindings (
    connection_id,key_enc,status,retired_at,state_revision,fail_count,last_used_at,
    cooldown_until,legacy_channel_key_id,created_at,updated_at)
SELECT pc.id,CASE WHEN ck.status='retired' THEN '' ELSE ck.key_enc END,
       CASE WHEN ck.status IN ('active','disabled','invalid','retired')
            THEN ck.status ELSE 'invalid' END,
       CASE WHEN ck.status='retired' THEN ck.updated_at ELSE NULL END,
       ck.state_epoch,GREATEST(ck.fail_count,0),ck.last_used_at,
       ck.cooldown_until,ck.id,ck.created_at,ck.updated_at
  FROM channel_keys ck
  JOIN provider_connections pc ON pc.legacy_channel_id=ck.channel_id
ON CONFLICT (legacy_channel_key_id) DO NOTHING;

CREATE TABLE credential_binding_deployments (
    binding_id BIGINT NOT NULL,
    deployment_id BIGINT NOT NULL,
    connection_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    PRIMARY KEY (binding_id, deployment_id),
    CONSTRAINT credential_binding_deployments_binding_fk
        FOREIGN KEY (binding_id, connection_id)
        REFERENCES provider_credential_bindings(id, connection_id) ON DELETE RESTRICT,
    CONSTRAINT credential_binding_deployments_deployment_fk
        FOREIGN KEY (deployment_id, connection_id)
        REFERENCES model_deployments(id, connection_id) ON DELETE RESTRICT
);

CREATE INDEX credential_binding_deployments_deployment_idx
    ON credential_binding_deployments(deployment_id, binding_id);

-- Canonical identities are evidence references, not mutable aliases. Retirement
-- is terminal for a row: a later legacy source with the same semantic identity
-- receives a new deployment row instead of rewriting history.
CREATE FUNCTION preserve_provider_connection_identity_and_retirement()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'provider connection must be retired, not deleted'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.legacy_channel_id IS DISTINCT FROM OLD.legacy_channel_id THEN
        RAISE EXCEPTION 'provider connection legacy provenance is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id THEN
        RAISE EXCEPTION 'provider connection surrogate identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.admin_state = 'retired' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'retired provider connection is immutable'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER provider_connections_identity_immutable
BEFORE UPDATE OR DELETE ON provider_connections
FOR EACH ROW EXECUTE FUNCTION preserve_provider_connection_identity_and_retirement();

CREATE FUNCTION preserve_model_deployment_identity_and_retirement()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'model deployment must be retired, not deleted'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.customer_org_id IS DISTINCT FROM OLD.customer_org_id
       OR NEW.public_model_id IS DISTINCT FROM OLD.public_model_id
       OR NEW.connection_id IS DISTINCT FROM OLD.connection_id
       OR NEW.upstream_model IS DISTINCT FROM OLD.upstream_model
       OR NEW.legacy_channel_id IS DISTINCT FROM OLD.legacy_channel_id
       OR NEW.legacy_public_model IS DISTINCT FROM OLD.legacy_public_model THEN
        RAISE EXCEPTION 'model deployment identity tuple is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.admin_state = 'retired' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'retired model deployment is immutable'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_deployments_identity_immutable
BEFORE UPDATE OR DELETE ON model_deployments
FOR EACH ROW EXECUTE FUNCTION preserve_model_deployment_identity_and_retirement();

CREATE FUNCTION preserve_provider_credential_binding_identity_and_retirement()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'provider credential binding must be retired, not deleted'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.connection_id IS DISTINCT FROM OLD.connection_id
       OR NEW.legacy_channel_key_id IS DISTINCT FROM OLD.legacy_channel_key_id THEN
        RAISE EXCEPTION 'provider credential binding identity tuple is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'retired' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'retired provider credential binding is immutable'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER provider_credential_bindings_identity_immutable
BEFORE UPDATE OR DELETE ON provider_credential_bindings
FOR EACH ROW EXECUTE FUNCTION preserve_provider_credential_binding_identity_and_retirement();

CREATE TABLE model_deployment_operations (
    deployment_id BIGINT NOT NULL REFERENCES model_deployments(id) ON DELETE CASCADE,
    operation_id VARCHAR(80) NOT NULL,
    support_state VARCHAR(20) NOT NULL DEFAULT 'enabled',
    evidence_state VARCHAR(32) NOT NULL DEFAULT 'declared',
    evidence_ref VARCHAR(512) NOT NULL DEFAULT '',
    verified_at TIMESTAMPTZ(6),
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    PRIMARY KEY (deployment_id, operation_id),
    CONSTRAINT model_deployment_operations_identity_check CHECK (
        operation_id = BTRIM(operation_id) AND BTRIM(operation_id) <> ''
        AND operation_id !~ E'[\\x00\\r\\n]'
        AND evidence_ref = BTRIM(evidence_ref)
        AND evidence_ref !~ E'[\\x00\\r\\n]'
        AND revision > 0
    ),
    CONSTRAINT model_deployment_operations_state_check CHECK (
        support_state IN ('enabled', 'unsupported')
        AND evidence_state IN (
            'declared', 'provider_documented', 'live_probed',
            'conformance_verified', 'production_approved'
        )
    )
);

CREATE TABLE model_deployment_capabilities (
    deployment_id BIGINT NOT NULL,
    operation_id VARCHAR(80) NOT NULL,
    capability_id VARCHAR(80) NOT NULL,
    support_state VARCHAR(20) NOT NULL DEFAULT 'enabled',
    evidence_state VARCHAR(32) NOT NULL DEFAULT 'declared',
    evidence_ref VARCHAR(512) NOT NULL DEFAULT '',
    verified_at TIMESTAMPTZ(6),
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    PRIMARY KEY (deployment_id, operation_id, capability_id),
    CONSTRAINT model_deployment_capabilities_operation_fk
        FOREIGN KEY (deployment_id, operation_id)
        REFERENCES model_deployment_operations(deployment_id, operation_id) ON DELETE RESTRICT,
    CONSTRAINT model_deployment_capabilities_identity_check CHECK (
        capability_id = BTRIM(capability_id) AND BTRIM(capability_id) <> ''
        AND capability_id !~ E'[\\x00\\r\\n]'
        AND evidence_ref = BTRIM(evidence_ref)
        AND evidence_ref !~ E'[\\x00\\r\\n]'
        AND revision > 0
    ),
    CONSTRAINT model_deployment_capabilities_state_check CHECK (
        support_state IN ('enabled', 'unsupported')
        AND evidence_state IN (
            'declared', 'provider_documented', 'live_probed',
            'conformance_verified', 'production_approved'
        )
    )
);

CREATE TABLE model_deployment_probes (
    probe_id BIGSERIAL PRIMARY KEY,
    deployment_id BIGINT NOT NULL,
    connection_id BIGINT NOT NULL,
    operation_id VARCHAR(80) NOT NULL,
    probe_profile_id VARCHAR(80) NOT NULL,
    probe_profile_version VARCHAR(40) NOT NULL,
    upstream_model VARCHAR(200) NOT NULL,
    reported_model VARCHAR(200) NOT NULL DEFAULT '',
    connection_revision BIGINT NOT NULL,
    deployment_revision BIGINT NOT NULL,
    credential_binding_id BIGINT,
    credential_state_revision BIGINT NOT NULL DEFAULT 0,
    success BOOLEAN NOT NULL,
    status_code INTEGER NOT NULL DEFAULT 0,
    latency_ms BIGINT NOT NULL DEFAULT 0,
    error_summary TEXT NOT NULL DEFAULT '',
    capabilities_verified JSONB NOT NULL DEFAULT '[]'::jsonb,
    request_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    response_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_kind VARCHAR(50) NOT NULL,
    source_ref VARCHAR(255) NOT NULL,
    source_fingerprint VARCHAR(71) NOT NULL,
    tested_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    tested_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT model_deployment_probes_source_unique
        UNIQUE (source_kind, source_ref, source_fingerprint),
    CONSTRAINT model_deployment_probes_deployment_fk
        FOREIGN KEY (deployment_id, connection_id)
        REFERENCES model_deployments(id, connection_id) ON DELETE RESTRICT,
    CONSTRAINT model_deployment_probes_operation_fk
        FOREIGN KEY (deployment_id, operation_id)
        REFERENCES model_deployment_operations(deployment_id, operation_id) ON DELETE RESTRICT,
    CONSTRAINT model_deployment_probes_credential_fk
        FOREIGN KEY (credential_binding_id, connection_id)
        REFERENCES provider_credential_bindings(id, connection_id) ON DELETE RESTRICT,
    CONSTRAINT model_deployment_probes_identity_check CHECK (
        operation_id = BTRIM(operation_id) AND BTRIM(operation_id) <> ''
        AND probe_profile_id = BTRIM(probe_profile_id) AND BTRIM(probe_profile_id) <> ''
        AND probe_profile_version = BTRIM(probe_profile_version) AND BTRIM(probe_profile_version) <> ''
        AND BTRIM(upstream_model) <> '' AND upstream_model = BTRIM(upstream_model)
        AND reported_model = BTRIM(reported_model)
        AND upstream_model !~ E'[\\x00\\r\\n]'
        AND reported_model !~ E'[\\x00\\r\\n]'
        AND probe_profile_id !~ E'[\\x00\\r\\n]'
        AND probe_profile_version !~ E'[\\x00\\r\\n]'
        AND source_kind = BTRIM(source_kind) AND BTRIM(source_kind) <> ''
        AND source_ref = BTRIM(source_ref) AND BTRIM(source_ref) <> ''
        AND source_kind !~ E'[\\x00\\r\\n]'
        AND source_ref !~ E'[\\x00\\r\\n]'
        AND source_fingerprint ~ '^sha256:[0-9a-f]{64}$'
        AND jsonb_typeof(capabilities_verified) = 'array'
        AND jsonb_typeof(request_summary) = 'object'
        AND jsonb_typeof(response_summary) = 'object'
        AND pg_column_size(capabilities_verified) <= 4096
        AND pg_column_size(request_summary) <= 8192
        AND pg_column_size(response_summary) <= 8192
        AND connection_revision > 0 AND deployment_revision > 0
        AND ((credential_binding_id IS NULL AND credential_state_revision = 0)
             OR (credential_binding_id IS NOT NULL AND credential_state_revision > 0))
    ),
    CONSTRAINT model_deployment_probes_result_check CHECK (
        status_code BETWEEN 0 AND 599 AND latency_ms >= 0
        AND LENGTH(error_summary) <= 512
    )
);

CREATE INDEX model_deployment_probes_tested_at_idx
    ON model_deployment_probes(deployment_id, operation_id, tested_at DESC, probe_id DESC);

CREATE VIEW model_deployment_latest_probes AS
SELECT DISTINCT ON (deployment_id, operation_id) *
  FROM model_deployment_probes
 ORDER BY deployment_id, operation_id, tested_at DESC, probe_id DESC;

CREATE FUNCTION reject_model_deployment_probe_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model deployment probe attempts are append-only'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER model_deployment_probes_append_only
BEFORE UPDATE OR DELETE ON model_deployment_probes
FOR EACH ROW EXECUTE FUNCTION reject_model_deployment_probe_mutation();

CREATE TABLE runtime_route_revision_items (
    release_id BIGINT NOT NULL REFERENCES runtime_change_releases(id) ON DELETE RESTRICT,
    ordinal INTEGER NOT NULL,
    deployment_id BIGINT NOT NULL REFERENCES model_deployments(id) ON DELETE RESTRICT,
    operation_id VARCHAR(80) NOT NULL,
    expected_connection_revision BIGINT NOT NULL,
    expected_deployment_revision BIGINT NOT NULL,
    admission VARCHAR(16) NOT NULL,
    priority INTEGER,
    weight INTEGER,
    source_candidate_version VARCHAR(80) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    PRIMARY KEY (release_id, ordinal),
    CONSTRAINT runtime_route_revision_items_deployment_unique
        UNIQUE (release_id, deployment_id, operation_id),
    CONSTRAINT runtime_route_revision_items_operation_fk
        FOREIGN KEY (deployment_id, operation_id)
        REFERENCES model_deployment_operations(deployment_id, operation_id) ON DELETE RESTRICT,
    CONSTRAINT runtime_route_revision_items_identity_check CHECK (
        ordinal > 0
        AND operation_id = BTRIM(operation_id) AND BTRIM(operation_id) <> ''
        AND operation_id !~ E'[\\x00\\r\\n]'
        AND expected_connection_revision > 0
        AND expected_deployment_revision > 0
        AND BTRIM(source_candidate_version) <> ''
    ),
    CONSTRAINT runtime_route_revision_items_override_check CHECK (
        admission IN ('inherit', 'allow', 'deny')
        AND (priority IS NULL OR priority BETWEEN 1 AND 10000)
        AND (weight IS NULL OR weight BETWEEN 1 AND 1000)
    )
);

CREATE INDEX runtime_route_revision_items_deployment_idx
    ON runtime_route_revision_items(deployment_id, release_id);

ALTER TABLE call_logs
    ADD COLUMN route_deployment_id BIGINT,
    ADD CONSTRAINT call_logs_route_deployment_fk
        FOREIGN KEY (org_id, route_deployment_id)
        REFERENCES model_deployments(customer_org_id, id) ON DELETE RESTRICT;

CREATE INDEX idx_call_logs_org_route_deployment_created
    ON call_logs(org_id, route_deployment_id, created_at)
    WHERE route_deployment_id IS NOT NULL;

ALTER TABLE billing_provider_attempts
    ADD COLUMN deployment_id BIGINT,
    ADD CONSTRAINT billing_provider_attempts_deployment_fk
        FOREIGN KEY (org_id, deployment_id)
        REFERENCES model_deployments(customer_org_id, id) ON DELETE RESTRICT;

CREATE INDEX billing_provider_attempts_deployment_idx
    ON billing_provider_attempts(org_id, deployment_id, updated_at, request_id)
    WHERE deployment_id IS NOT NULL;

COMMENT ON TABLE provider_connections IS
    'Canonical provider protocol/base-URL connection; legacy channels remain runtime authority during expand rollout.';
COMMENT ON TABLE model_deployments IS
    'Explicit customer public-model to provider-connection/upstream-model deployment identity.';
COMMENT ON TABLE model_deployment_capabilities IS
    'Per-operation capability evidence. An absent capability row is unknown and must fail closed; capability support is never inferred from a model name.';
COMMENT ON TABLE model_deployment_probes IS
    'Append-only operation-specific probe attempts. Request and response summaries are bounded structural metadata, never raw prompt or secret content.';
COMMENT ON VIEW model_deployment_latest_probes IS
    'Latest probe projection per deployment and operation; the append-only probe table remains evidence authority.';
COMMENT ON TABLE runtime_route_revision_items IS
    'Parity-only normalized deployment members derived from immutable runtime-change release candidates; releases remain history authority and these rows are not P1 routing authority.';
COMMENT ON COLUMN call_logs.route_deployment_id IS
    'Immutable model-deployment identity selected by a canonical router; NULL means legacy or not provable.';
COMMENT ON COLUMN billing_provider_attempts.deployment_id IS
    'Immutable model-deployment identity for a canonical provider attempt; NULL means legacy or not provable.';

COMMIT;
