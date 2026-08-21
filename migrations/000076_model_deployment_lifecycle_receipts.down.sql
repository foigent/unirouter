BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE runtime_change_releases, runtime_change_slots, runtime_change_events,
    model_deployment_lifecycle_receipt_qualifications,
    model_deployment_lifecycle_receipt_releases,
    model_deployment_lifecycle_receipts, schema_migrations
    IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version>'000076_model_deployment_lifecycle_receipts'
    ) THEN
        RAISE EXCEPTION 'Model deployment lifecycle receipt rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM model_deployment_lifecycle_receipts) THEN
        RAISE EXCEPTION 'Model deployment lifecycle receipt rollback refused: durable lifecycle receipts exist';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS runtime_change_events_append_only ON runtime_change_events;
DROP FUNCTION IF EXISTS reject_runtime_change_event_mutation();
DROP TRIGGER IF EXISTS runtime_change_events_receipt_link_guard ON runtime_change_events;
DROP FUNCTION IF EXISTS preserve_runtime_change_event_receipt_link();

DROP TRIGGER IF EXISTS model_deployment_lifecycle_receipt_qualifications_truncate_guard
    ON model_deployment_lifecycle_receipt_qualifications;
DROP TRIGGER IF EXISTS model_deployment_lifecycle_receipt_releases_truncate_guard
    ON model_deployment_lifecycle_receipt_releases;
DROP TRIGGER IF EXISTS model_deployment_lifecycle_receipts_truncate_guard
    ON model_deployment_lifecycle_receipts;
DROP FUNCTION IF EXISTS reject_model_deployment_lifecycle_receipt_truncate();

DROP TRIGGER IF EXISTS model_deployment_lifecycle_receipt_qualifications_append_only
    ON model_deployment_lifecycle_receipt_qualifications;
DROP TRIGGER IF EXISTS model_deployment_lifecycle_receipt_releases_append_only
    ON model_deployment_lifecycle_receipt_releases;
DROP FUNCTION IF EXISTS preserve_model_deployment_lifecycle_receipt_child();
DROP TRIGGER IF EXISTS model_deployment_lifecycle_receipts_immutable
    ON model_deployment_lifecycle_receipts;
DROP TRIGGER IF EXISTS model_deployment_lifecycle_receipts_terminal
    ON model_deployment_lifecycle_receipts;
DROP FUNCTION IF EXISTS require_terminal_model_deployment_lifecycle_receipt();
DROP FUNCTION IF EXISTS preserve_model_deployment_lifecycle_receipt();

DROP TABLE model_deployment_lifecycle_receipt_qualifications;
DROP TABLE model_deployment_lifecycle_receipt_releases;
DROP TABLE model_deployment_lifecycle_receipts;

DELETE FROM schema_migrations
WHERE version='000076_model_deployment_lifecycle_receipts';

COMMIT;
