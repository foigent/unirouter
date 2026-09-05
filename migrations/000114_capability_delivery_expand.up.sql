-- unirouter:explicit-compatible-expand-v1
BEGIN;
SELECT pg_advisory_xact_lock(86420517831702491);

-- Structure only. Import records the actual Target legacy head/projection in
-- a separate command. Existing releases, bindings and receipts are not rewritten.
CREATE TABLE capability_publications (
    publication_id TEXT PRIMARY KEY,
    sequence BIGSERIAL NOT NULL UNIQUE,
    predecessor_id TEXT UNIQUE REFERENCES capability_publications(publication_id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK (kind IN ('legacy_import','delta','compensation')),
    legacy_through TEXT,
    legacy_projection_digest TEXT,
    expected_projection_digest TEXT NOT NULL CHECK (expected_projection_digest ~ '^[a-f0-9]{64}$'),
    result_projection_digest TEXT NOT NULL CHECK (result_projection_digest ~ '^[a-f0-9]{64}$'),
    plan_digest TEXT NOT NULL UNIQUE CHECK (plan_digest ~ '^[a-f0-9]{64}$'),
    plan_bytes BYTEA NOT NULL CHECK (octet_length(plan_bytes) BETWEEN 1 AND 16777216),
    bundle_digest TEXT,
    bundle_bytes BYTEA,
    materializer_contract TEXT NOT NULL,
    target_identity JSONB NOT NULL CHECK (jsonb_typeof(target_identity)='object'),
    publisher_identity JSONB NOT NULL CHECK (jsonb_typeof(publisher_identity)='object'),
    created_xid XID8 NOT NULL DEFAULT pg_current_xact_id(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CHECK (publication_id ~ '^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,199}$'),
    CHECK (encode(sha256(plan_bytes),'hex')=plan_digest),
    CHECK ((bundle_digest IS NULL AND bundle_bytes IS NULL) OR
      (bundle_digest IS NOT NULL AND bundle_bytes IS NOT NULL AND
       octet_length(bundle_bytes) BETWEEN 1 AND 16777216 AND
       encode(sha256(bundle_bytes),'hex')=bundle_digest)),
    CHECK ((kind='legacy_import' AND predecessor_id IS NULL AND legacy_through IS NOT NULL
            AND legacy_projection_digest IS NOT NULL AND legacy_projection_digest ~ '^[a-f0-9]{64}$') OR
           (kind<>'legacy_import' AND predecessor_id IS NOT NULL AND legacy_through IS NULL
            AND legacy_projection_digest IS NULL AND bundle_digest IS NOT NULL))
);
CREATE UNIQUE INDEX capability_publications_single_import ON capability_publications ((kind)) WHERE kind='legacy_import';

CREATE TABLE model_profile_definitions (
    definition_digest TEXT PRIMARY KEY CHECK (definition_digest ~ '^[a-f0-9]{64}$'),
    profile_id VARCHAR(100) NOT NULL,
    profile_version VARCHAR(40) NOT NULL,
    product_lineage_id VARCHAR(200) NOT NULL,
    predecessor_digest TEXT REFERENCES model_profile_definitions(definition_digest) ON DELETE RESTRICT,
    schema_version TEXT NOT NULL,
    canonical_bytes BYTEA NOT NULL CHECK (octet_length(canonical_bytes) BETWEEN 1 AND 65536),
    publication_id TEXT NOT NULL REFERENCES capability_publications(publication_id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (profile_id,profile_version),
    UNIQUE (profile_id,profile_version,definition_digest),
    CHECK (profile_id ~ '^[a-z0-9][a-z0-9._-]{2,99}$'),
    CHECK (profile_version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'),
    CHECK (product_lineage_id ~ '^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,199}$'),
    CHECK (encode(sha256(canonical_bytes),'hex')=definition_digest)
);

ALTER TABLE model_registry ADD COLUMN product_lineage_id VARCHAR(200);
ALTER TABLE model_registry ADD CONSTRAINT model_registry_product_lineage_identity CHECK (
    product_lineage_id IS NULL OR product_lineage_id ~ '^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,199}$');

ALTER TABLE model_capability_probe_contract_bindings
    ALTER COLUMN introduced_migration DROP NOT NULL,
    ADD COLUMN introduced_publication_id TEXT REFERENCES capability_publications(publication_id) ON DELETE RESTRICT,
    ADD COLUMN profile_definition_digest TEXT REFERENCES model_profile_definitions(definition_digest) ON DELETE RESTRICT,
    ADD CONSTRAINT capability_binding_publication_origin CHECK (
        (introduced_migration IS NOT NULL AND introduced_publication_id IS NULL AND profile_definition_digest IS NULL)
        OR (introduced_migration IS NULL AND introduced_publication_id IS NOT NULL AND profile_definition_digest IS NOT NULL)),
    ADD CONSTRAINT capability_binding_definition_address FOREIGN KEY (
        execution_profile_id,execution_profile_version,profile_definition_digest)
        REFERENCES model_profile_definitions(profile_id,profile_version,definition_digest) ON DELETE RESTRICT;

-- A stable model may keep an old execution deployment while an exact data
-- revision is qualified on the same Connection and upstream product. This is
-- a revision of existing Deployment identity, not a new public model or key.
ALTER TABLE model_deployments ADD COLUMN profile_definition_digest TEXT
    REFERENCES model_profile_definitions(definition_digest) ON DELETE RESTRICT;
DROP INDEX model_deployments_active_connection_model_upstream_unique;
CREATE UNIQUE INDEX model_deployments_active_connection_model_upstream_unique
    ON model_deployments(customer_org_id,connection_id,public_model_id,upstream_model,COALESCE(profile_definition_digest,''))
    WHERE admin_state<>'retired';
CREATE FUNCTION guard_deployment_profile_definition() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND NEW.profile_definition_digest IS DISTINCT FROM OLD.profile_definition_digest THEN
        RAISE EXCEPTION 'Deployment Profile identity is immutable; create a candidate revision' USING ERRCODE='55000';
    END IF;
    IF NEW.profile_definition_digest IS NOT NULL AND (
       NEW.legacy_channel_id IS NOT NULL OR NEW.legacy_public_model IS NOT NULL OR NOT EXISTS (
         SELECT 1 FROM model_profile_definitions d JOIN model_registry m ON m.id=NEW.public_model_id
         WHERE d.definition_digest=NEW.profile_definition_digest AND d.product_lineage_id=m.product_lineage_id)) THEN
        RAISE EXCEPTION 'data Deployment requires exact stable product lineage and no legacy reconciliation identity' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER model_deployment_profile_definition_guard BEFORE INSERT OR UPDATE ON model_deployments
    FOR EACH ROW EXECUTE FUNCTION guard_deployment_profile_definition();

-- Candidate operations can be qualified against an exact data definition
-- without mutating the stable model's bootstrap Profile or active Release.
ALTER TABLE model_deployment_operations ADD COLUMN profile_definition_digest TEXT
    REFERENCES model_profile_definitions(definition_digest) ON DELETE RESTRICT;
CREATE FUNCTION guard_operation_profile_definition() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND NEW.profile_definition_digest IS DISTINCT FROM OLD.profile_definition_digest
       AND EXISTS(SELECT 1 FROM runtime_route_revision_items i JOIN runtime_change_releases r ON r.id=i.release_id
           WHERE i.deployment_id=OLD.deployment_id AND i.operation_id=OLD.operation_id AND r.route_items_sealed_at IS NOT NULL) THEN
        RAISE EXCEPTION 'sealed operation Profile cannot change; create a candidate Deployment revision' USING ERRCODE='55000';
    END IF;
    IF EXISTS(SELECT 1 FROM model_deployments d WHERE d.id=NEW.deployment_id AND d.profile_definition_digest IS NOT NULL
        AND d.profile_definition_digest IS DISTINCT FROM NEW.profile_definition_digest) THEN
        RAISE EXCEPTION 'operation Profile must match its immutable Deployment revision' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' AND NEW.profile_definition_digest IS DISTINCT FROM OLD.profile_definition_digest
       AND NEW.revision<>OLD.revision+1 THEN
        RAISE EXCEPTION 'operation Profile definition change requires next revision' USING ERRCODE='23514';
    END IF;
    IF NEW.profile_definition_digest IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM model_profile_definitions definition
          JOIN model_deployments deployment ON deployment.id=NEW.deployment_id
          JOIN model_registry model ON model.id=deployment.public_model_id
         WHERE definition.definition_digest=NEW.profile_definition_digest
           AND definition.product_lineage_id=model.product_lineage_id
    ) THEN
        RAISE EXCEPTION 'candidate operation Profile must belong to explicit stable product lineage' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER model_operation_profile_definition_guard BEFORE INSERT OR UPDATE ON model_deployment_operations
    FOR EACH ROW EXECUTE FUNCTION guard_operation_profile_definition();

ALTER TABLE runtime_change_releases
    ADD COLUMN profile_definition_digest TEXT REFERENCES model_profile_definitions(definition_digest) ON DELETE RESTRICT,
    ADD COLUMN execution_snapshot_format TEXT,
    ADD COLUMN execution_snapshot_digest TEXT,
    ADD COLUMN execution_snapshot_bytes BYTEA,
    ADD CONSTRAINT runtime_release_snapshot_complete CHECK (
        (profile_definition_digest IS NULL AND execution_snapshot_format IS NULL
         AND execution_snapshot_digest IS NULL AND execution_snapshot_bytes IS NULL)
        OR (profile_definition_digest IS NOT NULL AND execution_snapshot_format IS NOT NULL
            AND execution_snapshot_digest IS NOT NULL AND execution_snapshot_bytes IS NOT NULL
            AND candidate_version='unirouter.runtime-change-candidate.v2'
            AND execution_snapshot_format='unirouter.release-execution.v1'
            AND octet_length(execution_snapshot_bytes) BETWEEN 1 AND 4194304
            AND encode(sha256(execution_snapshot_bytes),'hex')=execution_snapshot_digest)),
    ADD CONSTRAINT runtime_release_definition_address FOREIGN KEY (
        execution_profile_id,execution_profile_version,profile_definition_digest)
        REFERENCES model_profile_definitions(profile_id,profile_version,definition_digest) ON DELETE RESTRICT;

CREATE TABLE runtime_support_manifests (
    artifact_digest TEXT PRIMARY KEY CHECK (artifact_digest ~ '^sha256:[a-f0-9]{64}$'),
    manifest_digest TEXT NOT NULL CHECK (manifest_digest ~ '^[a-f0-9]{64}$'),
    canonical_bytes BYTEA NOT NULL CHECK (octet_length(canonical_bytes) BETWEEN 1 AND 1048576),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CHECK (encode(sha256(canonical_bytes),'hex')=manifest_digest)
);

-- A lease does not prove ingress withdrawal. Expiry must keep a potential
-- receiver in activation checks until the explicit membership owner removes it.
CREATE TABLE runtime_generations (
    instance_id TEXT PRIMARY KEY CHECK (instance_id ~ '^uri_[a-f0-9]{32}$'),
    artifact_digest TEXT NOT NULL REFERENCES runtime_support_manifests(artifact_digest) ON DELETE RESTRICT,
    compatibility_epoch TEXT NOT NULL,
    schema_head TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('registered','serving','draining','stopped')),
    ingress_state TEXT NOT NULL DEFAULT 'absent' CHECK (ingress_state IN ('absent','pending_join','receiving','withdrawing')),
    lease_expires_at TIMESTAMPTZ NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

ALTER TABLE billing_provider_attempts ADD COLUMN runtime_instance_id TEXT
    REFERENCES runtime_generations(instance_id) ON DELETE RESTRICT;

CREATE TABLE runtime_ingress_changes (
    change_id TEXT PRIMARY KEY,
    predecessor_id TEXT UNIQUE REFERENCES runtime_ingress_changes(change_id) ON DELETE RESTRICT,
    expected_config_digest TEXT NOT NULL CHECK (expected_config_digest ~ '^[a-f0-9]{64}$'),
    desired_config_digest TEXT NOT NULL CHECK (desired_config_digest ~ '^[a-f0-9]{64}$'),
    desired_config_bytes BYTEA NOT NULL CHECK (octet_length(desired_config_bytes) BETWEEN 1 AND 1048576),
    membership_before JSONB NOT NULL CHECK (jsonb_typeof(membership_before)='array'),
    membership_after JSONB NOT NULL CHECK (jsonb_typeof(membership_after)='array'),
    status TEXT NOT NULL CHECK (status IN ('pending','applied','ambiguous','cancelled')),
    observation JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(observation)='object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at TIMESTAMPTZ,
    CHECK (encode(sha256(desired_config_bytes),'hex')=desired_config_digest)
);
CREATE UNIQUE INDEX runtime_ingress_changes_single_genesis ON runtime_ingress_changes ((true)) WHERE predecessor_id IS NULL;

CREATE FUNCTION guard_runtime_ingress_receipt() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'UPDATE' THEN
        RAISE EXCEPTION 'runtime ingress history cannot be deleted' USING ERRCODE='55000';
    END IF;
    IF OLD.status='applied' OR NEW.change_id IS DISTINCT FROM OLD.change_id
       OR NEW.predecessor_id IS DISTINCT FROM OLD.predecessor_id
       OR NEW.expected_config_digest IS DISTINCT FROM OLD.expected_config_digest
       OR NEW.desired_config_digest IS DISTINCT FROM OLD.desired_config_digest
       OR NEW.desired_config_bytes IS DISTINCT FROM OLD.desired_config_bytes
       OR NEW.membership_before IS DISTINCT FROM OLD.membership_before
       OR NEW.membership_after IS DISTINCT FROM OLD.membership_after
       OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR NEW.status NOT IN ('ambiguous','applied')
       OR (NEW.status='applied') IS DISTINCT FROM (NEW.completed_at IS NOT NULL) THEN
        RAISE EXCEPTION 'runtime ingress intent is immutable and completion is monotonic' USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER runtime_ingress_receipt_immutable BEFORE UPDATE OR DELETE ON runtime_ingress_changes
    FOR EACH ROW EXECUTE FUNCTION guard_runtime_ingress_receipt();
CREATE TRIGGER runtime_ingress_no_truncate BEFORE TRUNCATE ON runtime_ingress_changes
    FOR EACH STATEMENT EXECUTE FUNCTION guard_runtime_ingress_receipt();

CREATE FUNCTION reject_capability_authority_mutation() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'capability authority history is immutable' USING ERRCODE='55000';
END;
$$;
CREATE TRIGGER capability_publications_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON capability_publications
    FOR EACH STATEMENT EXECUTE FUNCTION reject_capability_authority_mutation();
CREATE TRIGGER model_profile_definitions_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON model_profile_definitions
    FOR EACH STATEMENT EXECUTE FUNCTION reject_capability_authority_mutation();
CREATE TRIGGER runtime_support_manifests_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON runtime_support_manifests
    FOR EACH STATEMENT EXECUTE FUNCTION reject_capability_authority_mutation();

CREATE FUNCTION require_capability_publication_writer() RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM capability_publications
        WHERE publication_id=current_setting('unirouter.capability_publication_id',true)
          AND created_xid=pg_current_xact_id()) THEN
        RAISE EXCEPTION 'capability DML requires the same transaction publication owner' USING ERRCODE='55000';
    END IF;
END;
$$;

CREATE FUNCTION guard_capability_definition_insert() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM require_capability_publication_writer();
    IF NEW.publication_id IS DISTINCT FROM current_setting('unirouter.capability_publication_id',true) THEN
        RAISE EXCEPTION 'profile definition publication owner mismatch' USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER model_profile_definitions_insert_owner BEFORE INSERT ON model_profile_definitions
    FOR EACH ROW EXECUTE FUNCTION guard_capability_definition_insert();

-- Independent guard names survive an unaware V1 writer dropping its historical
-- trigger names. The durable import event makes future V1 writes ineligible.
CREATE FUNCTION guard_capability_epoch_binding() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'INSERT' THEN
        RAISE EXCEPTION 'capability bindings remain append only' USING ERRCODE='55000';
    END IF;
    IF EXISTS (SELECT 1 FROM capability_publications) THEN
        PERFORM require_capability_publication_writer();
        IF NEW.introduced_publication_id IS DISTINCT FROM current_setting('unirouter.capability_publication_id',true)
           OR NEW.introduced_migration IS NOT NULL THEN
            RAISE EXCEPTION 'legacy capability writer is retired for this Target' USING ERRCODE='55000';
        END IF;
    ELSIF NEW.introduced_publication_id IS NOT NULL THEN
        RAISE EXCEPTION 'capability legacy import is required' USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER capability_binding_writer_epoch BEFORE INSERT OR UPDATE OR DELETE ON model_capability_probe_contract_bindings
    FOR EACH ROW EXECUTE FUNCTION guard_capability_epoch_binding();

CREATE FUNCTION guard_capability_epoch_selector() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM capability_publications) THEN
        PERFORM require_capability_publication_writer();
        IF TG_OP='DELETE' THEN
            RAISE EXCEPTION 'capability selectors require explicit successor compensation' USING ERRCODE='55000';
        END IF;
        IF EXISTS (SELECT 1 FROM runtime_change_releases r
            WHERE r.execution_snapshot_digest IS NULL AND r.route_items_sealed_at IS NOT NULL
              AND r.execution_profile_id=NEW.execution_profile_id
              AND r.execution_profile_version=NEW.execution_profile_version) THEN
            RAISE EXCEPTION 'publication cannot reinterpret a sealed legacy Release' USING ERRCODE='55000';
        END IF;
    END IF;
    RETURN COALESCE(NEW,OLD);
END;
$$;
CREATE TRIGGER capability_selector_writer_epoch BEFORE INSERT OR UPDATE OR DELETE ON model_capability_current_probe_contracts
    FOR EACH ROW EXECUTE FUNCTION guard_capability_epoch_selector();

CREATE OR REPLACE FUNCTION enforce_runtime_release_execution_contract_v1()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP='INSERT'
       AND NEW.candidate_version='unirouter.runtime-change-candidate.v2' AND NEW.execution_snapshot_digest IS NULL THEN
        PERFORM 1
          FROM model_registry model
         WHERE model.id=NEW.public_model_id
           AND model.model_name=NEW.model_name
           AND model.execution_profile_id=NEW.execution_profile_id
           AND model.execution_profile_version=NEW.execution_profile_version
           AND model.context_length=NEW.public_context_limit
           AND model.output_limit=NEW.public_output_limit
         FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'canonical release execution contract must equal the locked public model profile';
        END IF;
    END IF;
    IF TG_OP='INSERT' AND NEW.execution_snapshot_digest IS NOT NULL THEN
        PERFORM 1 FROM model_registry model
          JOIN model_profile_definitions definition ON definition.definition_digest=NEW.profile_definition_digest
         WHERE model.id=NEW.public_model_id AND model.model_name=NEW.model_name
           AND model.product_lineage_id=definition.product_lineage_id
           AND definition.profile_id=NEW.execution_profile_id
           AND definition.profile_version=NEW.execution_profile_version
         FOR SHARE OF model;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'Release definition must belong to the explicit stable product lineage';
        END IF;
    END IF;
    IF TG_OP='UPDATE' AND (
        NEW.execution_profile_id IS DISTINCT FROM OLD.execution_profile_id
        OR NEW.execution_profile_version IS DISTINCT FROM OLD.execution_profile_version
        OR NEW.public_context_limit IS DISTINCT FROM OLD.public_context_limit
        OR NEW.public_output_limit IS DISTINCT FROM OLD.public_output_limit
    ) THEN
        RAISE EXCEPTION 'sealed model execution contract is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION guard_release_snapshot_immutability() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND (
        NEW.profile_definition_digest IS DISTINCT FROM OLD.profile_definition_digest OR
        NEW.execution_snapshot_format IS DISTINCT FROM OLD.execution_snapshot_format OR
        NEW.execution_snapshot_digest IS DISTINCT FROM OLD.execution_snapshot_digest OR
        NEW.execution_snapshot_bytes IS DISTINCT FROM OLD.execution_snapshot_bytes) THEN
        RAISE EXCEPTION 'Release execution snapshot cannot be rewritten or backfilled' USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER runtime_release_snapshot_immutable BEFORE UPDATE ON runtime_change_releases
    FOR EACH ROW EXECUTE FUNCTION guard_release_snapshot_immutability();

CREATE FUNCTION guard_model_product_lineage() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.product_lineage_id IS NOT NULL AND NEW.product_lineage_id IS DISTINCT FROM OLD.product_lineage_id THEN
        RAISE EXCEPTION 'stable model product lineage is immutable' USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER model_product_lineage_immutable BEFORE UPDATE OF product_lineage_id ON model_registry
    FOR EACH ROW EXECUTE FUNCTION guard_model_product_lineage();

-- The daily materializer keeps the historical statement guards installed.
-- Only its same-transaction append receipt opens authorized DML; no daily DDL.
CREATE OR REPLACE FUNCTION reject_model_capability_probe_contract_binding_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM capability_publications) THEN
        RAISE EXCEPTION 'model capability probe contract bindings are immutable' USING ERRCODE='55000';
    END IF;
    PERFORM require_capability_publication_writer();
    RETURN NULL;
END;
$$;
CREATE OR REPLACE FUNCTION reject_model_capability_current_probe_contract_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM capability_publications) THEN
        RAISE EXCEPTION 'current model capability probe contracts are migration-owned' USING ERRCODE='55000';
    END IF;
    PERFORM require_capability_publication_writer();
    RETURN NULL;
END;
$$;
CREATE TRIGGER capability_binding_no_truncate BEFORE TRUNCATE ON model_capability_probe_contract_bindings
    FOR EACH STATEMENT EXECUTE FUNCTION reject_capability_authority_mutation();
CREATE TRIGGER capability_selector_no_truncate BEFORE TRUNCATE ON model_capability_current_probe_contracts
    FOR EACH STATEMENT EXECUTE FUNCTION reject_capability_authority_mutation();

CREATE FUNCTION require_runtime_snapshot_support(snapshot_bytes BYTEA,manifest_bytes BYTEA)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE snapshot JSONB; manifest JSONB;
BEGIN
    snapshot:=convert_from(snapshot_bytes,'UTF8')::jsonb;
    manifest:=convert_from(manifest_bytes,'UTF8')::jsonb;
    IF NOT COALESCE(snapshot->>'schema_version'='unirouter.release-execution.v1'
        AND jsonb_typeof(snapshot->'requirements')='array'
        AND (snapshot->'requirements') @> '[{"kind":"runtime_release","id":"unirouter.release-execution.v1"}]'::jsonb
        AND manifest->>'schema_version'='unirouter.runtime-support.v1'
        AND (manifest->'implementations') @> (snapshot->'requirements'),false) THEN
        RAISE EXCEPTION 'runtime generation cannot interpret exact Release requirements' USING ERRCODE='55000';
    END IF;
END;
$$;

-- The existing ProviderAttempt receipt fixes the process identity. Artifact
-- bytes resolve through the immutable generation-to-manifest association.
CREATE FUNCTION guard_provider_attempt_runtime_instance() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE generation RECORD; snapshot BYTEA;
BEGIN
    IF TG_OP='UPDATE' THEN
        IF NEW.runtime_instance_id IS DISTINCT FROM OLD.runtime_instance_id THEN
            RAISE EXCEPTION 'ProviderAttempt runtime identity is immutable' USING ERRCODE='55000';
        END IF;
        RETURN NEW;
    END IF;
    SELECT execution_snapshot_bytes INTO snapshot FROM runtime_change_releases WHERE id=NEW.route_runtime_release_id;
    IF snapshot IS NOT NULL AND NEW.runtime_instance_id IS NULL THEN
        RAISE EXCEPTION 'data Release dispatch requires runtime generation identity' USING ERRCODE='55000';
    END IF;
    IF NEW.runtime_instance_id IS NOT NULL THEN
        SELECT g.state,g.lease_expires_at,m.canonical_bytes INTO STRICT generation
          FROM runtime_generations g JOIN runtime_support_manifests m USING(artifact_digest)
         WHERE g.instance_id=NEW.runtime_instance_id;
        IF generation.state NOT IN ('serving','draining') OR generation.lease_expires_at<=clock_timestamp() THEN
            RAISE EXCEPTION 'dispatch runtime lease is not live' USING ERRCODE='55000';
        END IF;
        IF snapshot IS NOT NULL THEN
            PERFORM require_runtime_snapshot_support(snapshot,generation.canonical_bytes);
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER provider_attempt_runtime_instance_guard BEFORE INSERT OR UPDATE ON billing_provider_attempts
    FOR EACH ROW EXECUTE FUNCTION guard_provider_attempt_runtime_instance();

-- Join and activation use the same domain lock. The Go owners also acquire it
-- before their business locks; these DB guards protect against unaware writers.
CREATE FUNCTION guard_runtime_slot_compatibility() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE release RECORD; generation RECORD;
BEGIN
    PERFORM pg_advisory_xact_lock(86420517831702492);
    FOR release IN SELECT execution_snapshot_bytes FROM runtime_change_releases
        WHERE id IN (NEW.stable_release_id,NEW.canary_release_id) AND execution_snapshot_digest IS NOT NULL LOOP
        IF NOT EXISTS (SELECT 1 FROM runtime_generations WHERE ingress_state<>'absent') THEN
            RAISE EXCEPTION 'Release activation requires proved potential ingress receivers' USING ERRCODE='55000';
        END IF;
        FOR generation IN SELECT g.state,g.lease_expires_at,m.canonical_bytes FROM runtime_generations g
            JOIN runtime_support_manifests m USING(artifact_digest) WHERE g.ingress_state<>'absent' LOOP
            IF generation.state NOT IN ('serving','draining') OR generation.lease_expires_at<=clock_timestamp() THEN
                RAISE EXCEPTION 'Release activation cannot ignore an unavailable potential receiver' USING ERRCODE='55000';
            END IF;
            PERFORM require_runtime_snapshot_support(release.execution_snapshot_bytes,generation.canonical_bytes);
        END LOOP;
    END LOOP;
    RETURN NEW;
END;
$$;
CREATE TRIGGER runtime_slot_compatibility BEFORE INSERT OR UPDATE ON runtime_change_slots
    FOR EACH ROW EXECUTE FUNCTION guard_runtime_slot_compatibility();

CREATE FUNCTION guard_runtime_generation_membership() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE release RECORD; manifest BYTEA;
BEGIN
    IF TG_OP='UPDATE' AND (NEW.instance_id IS DISTINCT FROM OLD.instance_id OR
       NEW.artifact_digest IS DISTINCT FROM OLD.artifact_digest OR
       NEW.compatibility_epoch IS DISTINCT FROM OLD.compatibility_epoch OR NEW.created_at IS DISTINCT FROM OLD.created_at) THEN
        RAISE EXCEPTION 'runtime generation artifact identity is immutable' USING ERRCODE='55000';
    END IF;
    -- An unchanged live identity renews without the activation/publication lock.
    -- This changes no support, schema, state or membership; otherwise a long
    -- static publication could starve serving heartbeats and manufacture 5xx.
    IF TG_OP='UPDATE' AND NEW.state=OLD.state AND NEW.state IN ('serving','draining')
       AND NEW.ingress_state=OLD.ingress_state AND NEW.schema_head=OLD.schema_head
       AND NEW.schema_head=(SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1)
       AND NEW.lease_expires_at>clock_timestamp() THEN RETURN NEW; END IF;
    PERFORM pg_advisory_xact_lock(86420517831702492);
    IF NEW.ingress_state IN ('pending_join','receiving') THEN
        IF NEW.state<>'serving' OR NEW.lease_expires_at<=clock_timestamp() THEN
            RAISE EXCEPTION 'ingress join requires a live serving generation' USING ERRCODE='55000';
        END IF;
        SELECT canonical_bytes INTO STRICT manifest FROM runtime_support_manifests WHERE artifact_digest=NEW.artifact_digest;
        IF NOT COALESCE(convert_from(manifest,'UTF8')::jsonb->'schema_heads' ? NEW.schema_head,false)
           OR NEW.compatibility_epoch IS DISTINCT FROM (convert_from(manifest,'UTF8')::jsonb->>'compatibility_epoch')
           OR NEW.schema_head IS DISTINCT FROM (SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1) THEN
            RAISE EXCEPTION 'ingress generation does not support the current schema head' USING ERRCODE='55000';
        END IF;
        FOR release IN SELECT DISTINCT r.id,r.execution_snapshot_bytes FROM runtime_change_slots s
            JOIN runtime_change_releases r ON r.id IN (s.stable_release_id,s.canary_release_id)
            WHERE r.execution_snapshot_digest IS NOT NULL LOOP
            PERFORM require_runtime_snapshot_support(release.execution_snapshot_bytes,manifest);
        END LOOP;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER runtime_generation_membership BEFORE INSERT OR UPDATE ON runtime_generations
    FOR EACH ROW EXECUTE FUNCTION guard_runtime_generation_membership();

-- Preserve legacy qualification semantics when no data definition is selected.
CREATE OR REPLACE FUNCTION validate_model_capability_routing_approval()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    proof RECORD;
    actor RECORD;
    root_customer_count BIGINT;
    root_customer_id BIGINT;
BEGIN
    LOCK TABLE orgs IN SHARE MODE;
    SELECT COUNT(*),MIN(id)
      INTO root_customer_count,root_customer_id
      FROM orgs
     WHERE status='active' AND org_type='customer' AND parent_id IS NULL;
    IF root_customer_count<>1 OR root_customer_id IS DISTINCT FROM NEW.customer_org_id THEN
        RAISE EXCEPTION 'capability approval customer root authority is stale or ambiguous'
            USING ERRCODE='23514';
    END IF;

    SELECT probe.*,connection.config_revision AS current_connection_revision,
           connection.provider AS current_provider,connection.base_url AS current_base_url,
           connection.adapter_type AS current_adapter_type,
           COALESCE(connection.adapter_config,'{}'::jsonb) AS current_adapter_config,
           deployment.config_revision AS current_deployment_revision,
           deployment.upstream_model AS current_upstream_model,
           operation.revision AS current_operation_revision,
           operation.protocol_profile_id AS current_protocol_profile_id,
           operation.protocol_profile_version AS current_protocol_profile_version,
           capability.support_state AS current_capability_support_state,
           capability.support_revision AS current_capability_support_revision,
           COALESCE(definition.profile_id,model.execution_profile_id) AS current_execution_profile_id,
           COALESCE(definition.profile_version,model.execution_profile_version) AS current_execution_profile_version,
           binding.status AS current_credential_status,
           binding.key_enc AS current_credential_key,
           binding.state_revision AS current_credential_state_revision,
           binding.cooldown_until AS current_credential_cooldown,
           link.created_at AS current_credential_link_created_at,
           current_contract.probe_contract_id AS current_probe_contract_id,
           current_contract.probe_contract_version AS current_probe_contract_version,
           contract.expected_provider AS expected_contract_provider,
           contract.expected_upstream_model AS expected_contract_upstream_model
      INTO proof
      FROM model_capability_probes probe
      JOIN model_deployments deployment ON deployment.id=probe.deployment_id
      JOIN provider_connections connection ON connection.id=probe.connection_id
      JOIN model_deployment_operations operation
        ON operation.deployment_id=probe.deployment_id
       AND operation.operation_id=probe.operation_id
      JOIN model_deployment_capabilities capability
        ON capability.deployment_id=probe.deployment_id
       AND capability.operation_id=probe.operation_id
       AND capability.capability_id=probe.capability_id
      JOIN model_registry model ON model.id=deployment.public_model_id
      JOIN provider_credential_bindings binding
        ON binding.id=probe.credential_binding_id
       AND binding.connection_id=probe.connection_id
      JOIN credential_binding_deployments link
        ON link.binding_id=probe.credential_binding_id
       AND link.deployment_id=probe.deployment_id
       AND link.connection_id=probe.connection_id
      LEFT JOIN model_profile_definitions definition ON definition.definition_digest=operation.profile_definition_digest
      JOIN model_capability_current_probe_contracts current_contract
        ON current_contract.operation_id=probe.operation_id
       AND current_contract.capability_id=probe.capability_id
       AND current_contract.execution_profile_id=probe.execution_profile_id
       AND current_contract.execution_profile_version=probe.execution_profile_version
       AND current_contract.protocol_profile_id=probe.protocol_profile_id
       AND current_contract.protocol_profile_version=probe.protocol_profile_version
      JOIN model_capability_probe_contract_bindings contract
        ON contract.operation_id=probe.operation_id
       AND contract.capability_id=probe.capability_id
       AND contract.execution_profile_id=probe.execution_profile_id
       AND contract.execution_profile_version=probe.execution_profile_version
       AND contract.protocol_profile_id=probe.protocol_profile_id
       AND contract.protocol_profile_version=probe.protocol_profile_version
       AND contract.probe_contract_id=probe.probe_contract_id
       AND contract.probe_contract_version=probe.probe_contract_version
     WHERE probe.capability_probe_id=NEW.capability_probe_id
     FOR UPDATE OF probe,deployment,connection,operation,capability,binding,link;

    SELECT user_row.id,user_row.org_id,
           COALESCE(NULLIF(user_row.organization_role,''),user_row.role) AS organization_role,
           user_row.status AS user_status,org_row.status AS org_status
      INTO actor
      FROM users user_row
      JOIN orgs org_row ON org_row.id=user_row.org_id
     WHERE user_row.id=NEW.approved_by
     FOR KEY SHARE OF user_row,org_row;

    IF NOT FOUND OR actor.org_id IS DISTINCT FROM NEW.approved_by_org_id
       OR actor.organization_role<>'platform_admin'
       OR actor.user_status<>'active' OR actor.org_status<>'active' THEN
        RAISE EXCEPTION 'capability routing approval requires a current platform administrator'
            USING ERRCODE='42501';
    END IF;

    IF proof.capability_probe_id IS NULL OR NOT proof.success
       OR proof.source_kind<>'admin.model_capability_probe.v1'
       OR proof.current_capability_support_state<>'enabled'
       OR proof.current_connection_revision IS DISTINCT FROM proof.connection_revision
       OR proof.current_deployment_revision IS DISTINCT FROM proof.deployment_revision
       OR proof.current_operation_revision IS DISTINCT FROM proof.operation_revision
       OR proof.current_capability_support_revision IS DISTINCT FROM proof.capability_support_revision
       OR proof.current_execution_profile_id IS DISTINCT FROM proof.execution_profile_id
       OR proof.current_execution_profile_version IS DISTINCT FROM proof.execution_profile_version
       OR proof.current_protocol_profile_id IS DISTINCT FROM proof.protocol_profile_id
       OR proof.current_protocol_profile_version IS DISTINCT FROM proof.protocol_profile_version
       OR proof.current_probe_contract_id IS DISTINCT FROM proof.probe_contract_id
       OR proof.current_probe_contract_version IS DISTINCT FROM proof.probe_contract_version
       OR proof.expected_contract_provider IS DISTINCT FROM proof.provider
       OR proof.expected_contract_upstream_model IS DISTINCT FROM proof.upstream_model
       OR proof.current_provider IS DISTINCT FROM proof.provider
       OR proof.current_base_url IS DISTINCT FROM proof.base_url
       OR proof.current_adapter_type IS DISTINCT FROM proof.adapter_type
       OR proof.current_adapter_config IS DISTINCT FROM proof.adapter_config
       OR proof.current_upstream_model IS DISTINCT FROM proof.upstream_model
       OR proof.current_credential_status<>'active' OR proof.current_credential_key=''
       OR proof.current_credential_state_revision IS DISTINCT FROM proof.credential_state_revision
       OR proof.current_credential_link_created_at IS DISTINCT FROM proof.credential_link_created_at
       OR (proof.current_credential_cooldown IS NOT NULL
           AND proof.current_credential_cooldown>transaction_timestamp())
       OR NEW.customer_org_id IS DISTINCT FROM proof.customer_org_id
       OR NEW.deployment_id IS DISTINCT FROM proof.deployment_id
       OR NEW.connection_id IS DISTINCT FROM proof.connection_id
       OR NEW.operation_id IS DISTINCT FROM proof.operation_id
       OR NEW.capability_id IS DISTINCT FROM proof.capability_id
       OR NEW.execution_profile_id IS DISTINCT FROM proof.execution_profile_id
       OR NEW.execution_profile_version IS DISTINCT FROM proof.execution_profile_version
       OR NEW.protocol_profile_id IS DISTINCT FROM proof.protocol_profile_id
       OR NEW.protocol_profile_version IS DISTINCT FROM proof.protocol_profile_version
       OR NEW.probe_contract_id IS DISTINCT FROM proof.probe_contract_id
       OR NEW.probe_contract_version IS DISTINCT FROM proof.probe_contract_version
       OR NEW.connection_revision IS DISTINCT FROM proof.connection_revision
       OR NEW.deployment_revision IS DISTINCT FROM proof.deployment_revision
       OR NEW.operation_revision IS DISTINCT FROM proof.operation_revision
       OR NEW.capability_support_revision IS DISTINCT FROM proof.capability_support_revision
       OR NEW.credential_binding_id IS DISTINCT FROM proof.credential_binding_id
       OR NEW.credential_state_revision IS DISTINCT FROM proof.credential_state_revision
       OR NEW.credential_link_created_at IS DISTINCT FROM proof.credential_link_created_at
       OR NEW.source_kind<>'admin.model_capability_routing_approval.v1'
       OR NEW.source_ref IS DISTINCT FROM 'capability_probe:' || NEW.capability_probe_id::TEXT THEN
        RAISE EXCEPTION 'capability routing approval proof is stale, incomplete, or mismatched'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE VIEW model_capability_approval_qualifications AS
WITH root_authority AS (
    SELECT COUNT(*) AS root_count,MIN(id) AS root_customer_id
      FROM orgs
     WHERE status='active' AND org_type='customer' AND parent_id IS NULL
), facts AS (
    SELECT approval.*,probe.provider,probe.base_url,probe.adapter_type,probe.adapter_config,
           probe.upstream_model,probe.success AS probe_success,
           probe.source_kind AS probe_source_kind,
           connection.config_revision AS current_connection_revision,
           connection.provider AS current_provider,connection.base_url AS current_base_url,
           connection.adapter_type AS current_adapter_type,
           COALESCE(connection.adapter_config,'{}'::jsonb) AS current_adapter_config,
           deployment.config_revision AS current_deployment_revision,
           deployment.upstream_model AS current_upstream_model,
           operation.revision AS current_operation_revision,
           operation.protocol_profile_id AS current_protocol_profile_id,
           operation.protocol_profile_version AS current_protocol_profile_version,
           capability.support_state AS current_capability_support_state,
           capability.support_revision AS current_capability_support_revision,
           COALESCE(definition.profile_id,model.execution_profile_id) AS current_execution_profile_id,
           COALESCE(definition.profile_version,model.execution_profile_version) AS current_execution_profile_version,
           current_contract.probe_contract_id AS current_probe_contract_id,
           current_contract.probe_contract_version AS current_probe_contract_version,
           contract.expected_provider,contract.expected_upstream_model,
           root_authority.root_count,root_authority.root_customer_id
      FROM model_capability_routing_approvals approval
      JOIN model_capability_probes probe
        ON probe.capability_probe_id=approval.capability_probe_id
      JOIN model_deployments deployment ON deployment.id=approval.deployment_id
      JOIN provider_connections connection ON connection.id=approval.connection_id
      JOIN model_deployment_operations operation
        ON operation.deployment_id=approval.deployment_id
       AND operation.operation_id=approval.operation_id
      JOIN model_deployment_capabilities capability
        ON capability.deployment_id=approval.deployment_id
       AND capability.operation_id=approval.operation_id
       AND capability.capability_id=approval.capability_id
      JOIN model_registry model ON model.id=deployment.public_model_id
      LEFT JOIN model_profile_definitions definition ON definition.definition_digest=operation.profile_definition_digest
      LEFT JOIN model_capability_current_probe_contracts current_contract
        ON current_contract.operation_id=approval.operation_id
       AND current_contract.capability_id=approval.capability_id
       AND current_contract.execution_profile_id=approval.execution_profile_id
       AND current_contract.execution_profile_version=approval.execution_profile_version
       AND current_contract.protocol_profile_id=approval.protocol_profile_id
       AND current_contract.protocol_profile_version=approval.protocol_profile_version
      LEFT JOIN model_capability_probe_contract_bindings contract
        ON contract.operation_id=approval.operation_id
       AND contract.capability_id=approval.capability_id
       AND contract.execution_profile_id=approval.execution_profile_id
       AND contract.execution_profile_version=approval.execution_profile_version
       AND contract.protocol_profile_id=approval.protocol_profile_id
       AND contract.protocol_profile_version=approval.protocol_profile_version
       AND contract.probe_contract_id=approval.probe_contract_id
       AND contract.probe_contract_version=approval.probe_contract_version
      CROSS JOIN root_authority
), evaluated AS (
    SELECT facts.*,
           CASE
             WHEN root_count<>1 OR root_customer_id IS DISTINCT FROM customer_org_id
               THEN 'customer_authority_stale'
             WHEN NOT probe_success OR probe_source_kind<>'admin.model_capability_probe.v1'
               THEN 'probe_invalid'
             WHEN current_capability_support_state<>'enabled'
               THEN 'capability_unsupported'
             WHEN current_connection_revision IS DISTINCT FROM connection_revision
               OR current_provider IS DISTINCT FROM provider
               OR current_base_url IS DISTINCT FROM base_url
               OR current_adapter_type IS DISTINCT FROM adapter_type
               OR current_adapter_config IS DISTINCT FROM adapter_config
               THEN 'connection_stale'
             WHEN current_deployment_revision IS DISTINCT FROM deployment_revision
               OR current_upstream_model IS DISTINCT FROM upstream_model
               THEN 'deployment_stale'
             WHEN current_operation_revision IS DISTINCT FROM operation_revision
               OR current_protocol_profile_id IS DISTINCT FROM protocol_profile_id
               OR current_protocol_profile_version IS DISTINCT FROM protocol_profile_version
               THEN 'operation_stale'
             WHEN current_capability_support_revision IS DISTINCT FROM capability_support_revision
               THEN 'capability_support_stale'
             WHEN current_execution_profile_id IS DISTINCT FROM execution_profile_id
               OR current_execution_profile_version IS DISTINCT FROM execution_profile_version
               THEN 'execution_profile_stale'
             WHEN current_probe_contract_id IS DISTINCT FROM probe_contract_id
               OR current_probe_contract_version IS DISTINCT FROM probe_contract_version
               OR expected_provider IS DISTINCT FROM provider
               OR expected_upstream_model IS DISTINCT FROM upstream_model
               THEN 'probe_contract_stale'
             ELSE 'qualified'
           END AS qualification_state
      FROM facts
)
SELECT evaluated.*,qualification_state='qualified' AS qualified
  FROM evaluated;

CREATE OR REPLACE FUNCTION enforce_model_capability_evidence_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    current_model RECORD;
    numeric_id BIGINT;
    valid_proof BOOLEAN;
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.evidence_state NOT IN ('live_probed','production_approved') THEN
            RETURN NEW;
        END IF;
    ELSIF NEW.evidence_state IS NOT DISTINCT FROM OLD.evidence_state
       AND NEW.evidence_ref IS NOT DISTINCT FROM OLD.evidence_ref THEN
        RETURN NEW;
    END IF;

    SELECT COALESCE(definition.profile_id,model.execution_profile_id) AS execution_profile_id,
           COALESCE(definition.profile_version,model.execution_profile_version) AS execution_profile_version,
           operation.protocol_profile_id,operation.protocol_profile_version,
           current_contract.probe_contract_id,current_contract.probe_contract_version
      INTO current_model
      FROM model_deployments deployment
      JOIN model_registry model ON model.id=deployment.public_model_id
      JOIN model_deployment_operations operation
        ON operation.deployment_id=NEW.deployment_id
       AND operation.operation_id=NEW.operation_id
      LEFT JOIN model_profile_definitions definition ON definition.definition_digest=operation.profile_definition_digest
      LEFT JOIN model_capability_current_probe_contracts current_contract
        ON current_contract.operation_id=NEW.operation_id
       AND current_contract.capability_id=NEW.capability_id
       AND current_contract.execution_profile_id=COALESCE(definition.profile_id,model.execution_profile_id)
       AND current_contract.execution_profile_version=COALESCE(definition.profile_version,model.execution_profile_version)
       AND current_contract.protocol_profile_id=operation.protocol_profile_id
       AND current_contract.protocol_profile_version=operation.protocol_profile_version
     WHERE deployment.id=NEW.deployment_id;

    IF current_model.probe_contract_id IS NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.evidence_state='live_probed'
       AND NEW.evidence_ref ~ '^capability_probe:[1-9][0-9]*$' THEN
        numeric_id := SUBSTRING(NEW.evidence_ref FROM 18)::BIGINT;
        SELECT EXISTS (
            SELECT 1 FROM model_capability_probes probe
             WHERE probe.capability_probe_id=numeric_id
               AND probe.deployment_id=NEW.deployment_id
               AND probe.operation_id=NEW.operation_id
               AND probe.capability_id=NEW.capability_id
               AND probe.execution_profile_id=current_model.execution_profile_id
               AND probe.execution_profile_version=current_model.execution_profile_version
               AND probe.protocol_profile_id=current_model.protocol_profile_id
               AND probe.protocol_profile_version=current_model.protocol_profile_version
               AND probe.probe_contract_id=current_model.probe_contract_id
               AND probe.probe_contract_version=current_model.probe_contract_version
               AND probe.capability_support_revision=NEW.support_revision
               AND probe.success
        ) INTO valid_proof;
    ELSIF NEW.evidence_state='production_approved'
       AND NEW.evidence_ref ~ '^capability_approval:[1-9][0-9]*$' THEN
        numeric_id := SUBSTRING(NEW.evidence_ref FROM 21)::BIGINT;
        SELECT EXISTS (
            SELECT 1 FROM model_capability_approval_qualifications qualification
             WHERE qualification.approval_id=numeric_id
               AND qualification.deployment_id=NEW.deployment_id
               AND qualification.operation_id=NEW.operation_id
               AND qualification.capability_id=NEW.capability_id
               AND qualification.qualified
        ) INTO valid_proof;
    ELSE
        valid_proof := FALSE;
    END IF;

    IF NOT COALESCE(valid_proof,FALSE) THEN
        RAISE EXCEPTION 'capability evidence projection lacks a current typed receipt'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

COMMIT;
