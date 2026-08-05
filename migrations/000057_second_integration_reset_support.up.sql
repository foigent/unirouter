BEGIN;

-- The product-owned reset deletes both sides of the one intentional FK cycle
-- in one transaction. Only these two reviewed constraints are made
-- deferrable; every other FK remains immediate and drives the deterministic
-- child-before-parent delete order emitted by reset preview.
ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_reservation_fk,
    ADD CONSTRAINT billing_provider_attempts_reservation_fk
        FOREIGN KEY (org_id, request_id)
        REFERENCES freeze_idempotency(org_id, request_id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_delivery_attempt_provider_fk,
    ADD CONSTRAINT freeze_idempotency_delivery_attempt_provider_fk
        FOREIGN KEY (org_id, request_id, delivery_attempt_id)
        REFERENCES billing_provider_attempts(org_id, request_id, attempt_id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY IMMEDIATE;

-- The supported first-customer bootstrap endpoint uses a caller-supplied
-- operation identity for replay-safe creation. Only that one action receives
-- global idempotency semantics; existing audit identities remain unchanged.
CREATE UNIQUE INDEX audit_logs_first_customer_bootstrap_idempotency
    ON audit_logs (client_request_id)
    WHERE action = 'bootstrap.first_customer' AND client_request_id IS NOT NULL;

COMMIT;
