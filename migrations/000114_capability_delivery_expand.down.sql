BEGIN;
SELECT pg_advisory_xact_lock(86420517831702491);
LOCK TABLE capability_publications,model_profile_definitions,runtime_support_manifests,runtime_generations,
    billing_provider_attempts,runtime_ingress_changes,runtime_change_releases,model_deployment_operations,model_deployments,model_registry IN SHARE ROW EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM capability_publications) OR
       EXISTS (SELECT 1 FROM model_profile_definitions) OR
       EXISTS (SELECT 1 FROM runtime_support_manifests) OR
       EXISTS (SELECT 1 FROM runtime_generations) OR
       EXISTS (SELECT 1 FROM runtime_ingress_changes) OR
       EXISTS (SELECT 1 FROM billing_provider_attempts WHERE runtime_instance_id IS NOT NULL) OR
       EXISTS (SELECT 1 FROM runtime_change_releases WHERE execution_snapshot_digest IS NOT NULL) OR
       EXISTS (SELECT 1 FROM model_deployment_operations WHERE profile_definition_digest IS NOT NULL) OR
       EXISTS (SELECT 1 FROM model_deployments WHERE profile_definition_digest IS NOT NULL) OR
       EXISTS (SELECT 1 FROM model_registry WHERE product_lineage_id IS NOT NULL) THEN
       RAISE EXCEPTION 'capability expand rollback refused: immutable or runtime evidence exists';
    END IF;
    IF EXISTS (SELECT 1 FROM schema_migrations WHERE version>'000114_capability_delivery_expand') THEN
       RAISE EXCEPTION 'capability expand rollback refused: schema head advanced';
    END IF;
END;
$$;
DROP TRIGGER runtime_generation_membership ON runtime_generations;
DROP FUNCTION guard_runtime_generation_membership();
DROP TRIGGER runtime_slot_compatibility ON runtime_change_slots;
DROP FUNCTION guard_runtime_slot_compatibility();
DROP TRIGGER provider_attempt_runtime_instance_guard ON billing_provider_attempts;
DROP FUNCTION guard_provider_attempt_runtime_instance();
ALTER TABLE billing_provider_attempts DROP COLUMN runtime_instance_id;
DROP FUNCTION require_runtime_snapshot_support(BYTEA,BYTEA);
DROP TRIGGER capability_binding_no_truncate ON model_capability_probe_contract_bindings;
DROP TRIGGER capability_selector_no_truncate ON model_capability_current_probe_contracts;
CREATE OR REPLACE FUNCTION reject_model_capability_probe_contract_binding_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model capability probe contract bindings are immutable' USING ERRCODE='55000';
END;
$$;
CREATE OR REPLACE FUNCTION reject_model_capability_current_probe_contract_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'current model capability probe contracts are migration-owned' USING ERRCODE='55000';
END;
$$;
DROP TRIGGER model_product_lineage_immutable ON model_registry;
DROP FUNCTION guard_model_product_lineage();
CREATE OR REPLACE FUNCTION enforce_runtime_release_execution_contract_v1()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP='INSERT'
       AND NEW.candidate_version='unirouter.runtime-change-candidate.v2' THEN
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

DROP TRIGGER runtime_release_snapshot_immutable ON runtime_change_releases;
DROP FUNCTION guard_release_snapshot_immutability();
DROP TRIGGER capability_selector_writer_epoch ON model_capability_current_probe_contracts;
DROP FUNCTION guard_capability_epoch_selector();
DROP TRIGGER capability_binding_writer_epoch ON model_capability_probe_contract_bindings;
DROP FUNCTION guard_capability_epoch_binding();
DROP TABLE runtime_ingress_changes,runtime_generations,runtime_support_manifests;
DROP FUNCTION guard_runtime_ingress_receipt();
ALTER TABLE runtime_change_releases
    DROP CONSTRAINT runtime_release_snapshot_complete,
    DROP CONSTRAINT runtime_release_definition_address,
    DROP COLUMN profile_definition_digest,
    DROP COLUMN execution_snapshot_format,
    DROP COLUMN execution_snapshot_digest,
    DROP COLUMN execution_snapshot_bytes;
ALTER TABLE model_capability_probe_contract_bindings
    DROP CONSTRAINT capability_binding_publication_origin,
    DROP CONSTRAINT capability_binding_definition_address,
    DROP COLUMN introduced_publication_id,
    DROP COLUMN profile_definition_digest,
    ALTER COLUMN introduced_migration SET NOT NULL;
ALTER TABLE model_registry DROP CONSTRAINT model_registry_product_lineage_identity,DROP COLUMN product_lineage_id;
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
           model.execution_profile_id AS current_execution_profile_id,
           model.execution_profile_version AS current_execution_profile_version,
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
           model.execution_profile_id AS current_execution_profile_id,
           model.execution_profile_version AS current_execution_profile_version,
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

    SELECT model.execution_profile_id,model.execution_profile_version,
           operation.protocol_profile_id,operation.protocol_profile_version,
           current_contract.probe_contract_id,current_contract.probe_contract_version
      INTO current_model
      FROM model_deployments deployment
      JOIN model_registry model ON model.id=deployment.public_model_id
      JOIN model_deployment_operations operation
        ON operation.deployment_id=NEW.deployment_id
       AND operation.operation_id=NEW.operation_id
      LEFT JOIN model_capability_current_probe_contracts current_contract
        ON current_contract.operation_id=NEW.operation_id
       AND current_contract.capability_id=NEW.capability_id
       AND current_contract.execution_profile_id=model.execution_profile_id
       AND current_contract.execution_profile_version=model.execution_profile_version
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

DROP TRIGGER model_operation_profile_definition_guard ON model_deployment_operations;
DROP FUNCTION guard_operation_profile_definition();
ALTER TABLE model_deployment_operations DROP COLUMN profile_definition_digest;
DROP TRIGGER model_deployment_profile_definition_guard ON model_deployments;
DROP FUNCTION guard_deployment_profile_definition();
DROP INDEX model_deployments_active_connection_model_upstream_unique;
ALTER TABLE model_deployments DROP COLUMN profile_definition_digest;
CREATE UNIQUE INDEX model_deployments_active_connection_model_upstream_unique
    ON model_deployments(customer_org_id,connection_id,public_model_id,upstream_model) WHERE admin_state<>'retired';
DROP TABLE model_profile_definitions;
DROP FUNCTION guard_capability_definition_insert();
DROP FUNCTION require_capability_publication_writer();
DROP TABLE capability_publications;
DROP FUNCTION reject_capability_authority_mutation();
DELETE FROM schema_migrations WHERE version='000114_capability_delivery_expand';
COMMIT;
