BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_operation_production_approvals,
    model_operation_execution_support_bindings, model_deployment_operations,
    model_deployment_probes, runtime_change_releases,
    runtime_route_revision_items, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version>'000075_model_operation_production_qualification'
    ) THEN
        RAISE EXCEPTION 'Model operation production qualification rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM model_operation_production_approvals) THEN
        RAISE EXCEPTION 'Model operation production qualification rollback refused: append-only approvals exist';
    END IF;
    IF EXISTS (
        SELECT 1 FROM model_deployment_probes
         WHERE operation_revision IS NOT NULL OR public_path IS NOT NULL
            OR executor_contract_version IS NOT NULL OR https_origin IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'Model operation production qualification rollback refused: frozen network probe evidence exists';
    END IF;
    IF EXISTS (
        SELECT 1 FROM runtime_route_revision_items
         WHERE public_path IS NOT NULL OR executor_contract_version IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'Model operation production qualification rollback refused: sealed qualification membership exists';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS runtime_change_releases_p3_qualification_guard
    ON runtime_change_releases;
DROP FUNCTION IF EXISTS enforce_runtime_release_p3_qualification();
DROP TRIGGER IF EXISTS runtime_route_revision_items_p3_qualification_guard
    ON runtime_route_revision_items;
DROP FUNCTION IF EXISTS enforce_runtime_route_item_production_qualification();

ALTER TABLE runtime_route_revision_items
    DROP CONSTRAINT runtime_route_revision_items_execution_binding_shape_check,
    DROP COLUMN executor_contract_version,
    DROP COLUMN public_path;

DROP VIEW model_operation_execution_qualifications;
DROP TRIGGER IF EXISTS model_deployment_operations_p3_evidence_guard
    ON model_deployment_operations;
DROP FUNCTION IF EXISTS enforce_qualified_model_operation_evidence_transition();

DROP TRIGGER IF EXISTS model_operation_production_approvals_append_only
    ON model_operation_production_approvals;
DROP FUNCTION IF EXISTS reject_model_operation_production_approval_mutation();
DROP TRIGGER IF EXISTS model_operation_production_approvals_apply
    ON model_operation_production_approvals;
DROP FUNCTION IF EXISTS apply_model_operation_production_approval();
DROP TRIGGER IF EXISTS model_operation_production_approvals_validate
    ON model_operation_production_approvals;
DROP FUNCTION IF EXISTS validate_model_operation_production_approval();
DROP TABLE model_operation_production_approvals;

DROP TRIGGER IF EXISTS model_deployment_probes_truncate_guard
    ON model_deployment_probes;
DROP FUNCTION IF EXISTS reject_model_deployment_probe_truncate();
DROP TRIGGER IF EXISTS model_deployment_probes_p3_contract_guard
    ON model_deployment_probes;
DROP FUNCTION IF EXISTS enforce_model_operation_live_probe_contract();

ALTER TABLE model_deployment_probes
    DROP CONSTRAINT model_deployment_probes_execution_support_fk,
    DROP CONSTRAINT model_deployment_probes_execution_binding_shape_check,
    DROP CONSTRAINT model_deployment_probes_operation_revision_check,
    DROP COLUMN https_origin,
    DROP COLUMN executor_contract_version,
    DROP COLUMN public_path,
    DROP COLUMN operation_revision;

DROP TRIGGER IF EXISTS model_operation_execution_support_bindings_immutable
    ON model_operation_execution_support_bindings;
DROP FUNCTION IF EXISTS reject_model_operation_execution_support_mutation();
DROP VIEW model_operation_current_execution_support_bindings;
DROP TABLE model_operation_execution_support_bindings;
DROP FUNCTION model_operation_https_origin(TEXT);

DELETE FROM schema_migrations
WHERE version='000075_model_operation_production_qualification';

COMMIT;
