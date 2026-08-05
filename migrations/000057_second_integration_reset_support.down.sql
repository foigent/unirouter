BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000057_second_integration_reset_support'
    ) THEN
        RAISE EXCEPTION 'second-integration reset-support rollback refused: later migrations must be reversed first';
    END IF;
END;
$$;

DROP INDEX audit_logs_first_customer_bootstrap_idempotency;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_delivery_attempt_provider_fk,
    ADD CONSTRAINT freeze_idempotency_delivery_attempt_provider_fk
        FOREIGN KEY (org_id, request_id, delivery_attempt_id)
        REFERENCES billing_provider_attempts(org_id, request_id, attempt_id)
        ON DELETE RESTRICT
        NOT DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_reservation_fk,
    ADD CONSTRAINT billing_provider_attempts_reservation_fk
        FOREIGN KEY (org_id, request_id)
        REFERENCES freeze_idempotency(org_id, request_id)
        ON DELETE RESTRICT
        NOT DEFERRABLE INITIALLY IMMEDIATE;

DELETE FROM schema_migrations
WHERE version = '000057_second_integration_reset_support';

COMMIT;
