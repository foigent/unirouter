BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_capability_routing_approvals,model_capability_probes,
    model_deployment_capabilities,billing_provider_attempts,schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version>'000088_model_capability_execution_qualification'
    ) THEN
        RAISE EXCEPTION 'cannot remove M88 while later migrations exist'
            USING ERRCODE='55000';
    END IF;
    IF EXISTS (SELECT 1 FROM model_capability_routing_approvals)
       OR EXISTS (SELECT 1 FROM model_capability_probes)
       OR EXISTS (
            SELECT 1 FROM model_deployment_capabilities
             WHERE evidence_ref LIKE 'capability_probe:%'
                OR evidence_ref LIKE 'capability_approval:%'
       )
       OR EXISTS (
            SELECT 1 FROM billing_provider_attempts
             WHERE route_capability_evidence::TEXT LIKE '%capability_approval:%'
       ) THEN
        RAISE EXCEPTION 'cannot remove M88 while capability qualification evidence exists'
            USING ERRCODE='55000';
    END IF;
END;
$$;

-- Remove only rows whose exact evidence marker proves M88 (or its matching
-- application writer) created the declaration. Pre-existing declarations and
-- explicit unsupported decisions were never overwritten by M88.
DELETE FROM model_deployment_capabilities
 WHERE operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'
   AND support_state='enabled'
   AND evidence_state='declared'
   AND evidence_ref='capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@1.0.0'
   AND revision=1
   AND support_revision=1;

DROP TRIGGER model_deployment_capabilities_receipt_projection_guard
    ON model_deployment_capabilities;
DROP FUNCTION enforce_model_capability_evidence_projection();
DROP VIEW model_capability_execution_qualifications;
DROP VIEW model_capability_approval_qualifications;

DROP TRIGGER model_capability_routing_approvals_append_only
    ON model_capability_routing_approvals;
DROP TRIGGER model_capability_routing_approvals_validate
    ON model_capability_routing_approvals;
DROP FUNCTION reject_model_capability_routing_approval_mutation();
DROP FUNCTION validate_model_capability_routing_approval();
DROP TABLE model_capability_routing_approvals;

DROP TRIGGER model_capability_probes_append_only ON model_capability_probes;
DROP FUNCTION reject_model_capability_probe_mutation();
DROP TABLE model_capability_probes;

DROP TRIGGER model_capability_current_probe_contracts_immutable
    ON model_capability_current_probe_contracts;
DROP FUNCTION reject_model_capability_current_probe_contract_mutation();
DROP TABLE model_capability_current_probe_contracts;
DROP TRIGGER model_capability_probe_contract_bindings_immutable
    ON model_capability_probe_contract_bindings;
DROP FUNCTION reject_model_capability_probe_contract_binding_mutation();
DROP TABLE model_capability_probe_contract_bindings;

DROP TRIGGER a_model_deployment_capabilities_support_revision
    ON model_deployment_capabilities;
DROP FUNCTION enforce_model_deployment_capability_support_revision();
ALTER TABLE model_deployment_capabilities
    DROP CONSTRAINT model_deployment_capabilities_support_revision_check,
    DROP COLUMN support_revision;

DELETE FROM schema_migrations
 WHERE version='000088_model_capability_execution_qualification';

COMMIT;
