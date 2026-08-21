BEGIN;

-- Administrative model-deployment creation has a dedicated idempotency
-- receipt. It is intentionally separate from request_idempotency: this row is
-- a terminal control-plane mutation receipt (retention deletion is the only
-- destructive lifecycle), not a financial/model-call delivery contract.
CREATE TABLE model_deployment_create_receipts (
    id BIGSERIAL PRIMARY KEY,
    customer_org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    actor_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    actor_org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    operation_scope VARCHAR(64) NOT NULL,
    idempotency_key_hash CHAR(64) NOT NULL,
    request_fingerprint VARCHAR(76) NOT NULL,
    server_operation_id VARCHAR(80) NOT NULL,
    deployment_id BIGINT,
    model_operation_id VARCHAR(80),
    status VARCHAR(20) NOT NULL DEFAULT 'in_progress',
    original_http_status SMALLINT,
    reused_connection BOOLEAN,
    reused_public_model BOOLEAN,
    reused_deployment BOOLEAN,
    reused_credential BOOLEAN,
    reused_operation BOOLEAN,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ(6) NOT NULL DEFAULT (NOW() + INTERVAL '24 hours'),
    completed_at TIMESTAMPTZ(6),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT model_deployment_create_receipts_actor_tenant_fk
        FOREIGN KEY (actor_user_id, actor_org_id)
        REFERENCES users(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT model_deployment_create_receipts_deployment_tenant_fk
        FOREIGN KEY (customer_org_id, deployment_id)
        REFERENCES model_deployments(customer_org_id, id) ON DELETE RESTRICT,
    CONSTRAINT model_deployment_create_receipts_operation_fk
        FOREIGN KEY (deployment_id, model_operation_id)
        REFERENCES model_deployment_operations(deployment_id, operation_id)
        ON DELETE RESTRICT,
    CONSTRAINT model_deployment_create_receipts_identity_unique
        UNIQUE (customer_org_id, actor_user_id, operation_scope, idempotency_key_hash),
    CONSTRAINT model_deployment_create_receipts_identity_check CHECK (
        operation_scope = 'model_deployment.create.v1'
        AND idempotency_key_hash ~ '^[a-f0-9]{64}$'
        AND request_fingerprint ~ '^hmac-sha256:[a-f0-9]{64}$'
        AND server_operation_id = BTRIM(server_operation_id)
        AND BTRIM(server_operation_id) <> ''
        AND server_operation_id !~ E'[\\x00\\r\\n]'
    ),
    CONSTRAINT model_deployment_create_receipts_state_check CHECK (
        (status = 'in_progress'
            AND deployment_id IS NULL
            AND model_operation_id IS NULL
            AND original_http_status IS NULL
            AND reused_connection IS NULL
            AND reused_public_model IS NULL
            AND reused_deployment IS NULL
            AND reused_credential IS NULL
            AND reused_operation IS NULL
            AND completed_at IS NULL)
        OR
        (status = 'completed'
            AND deployment_id IS NOT NULL
            AND deployment_id > 0
            AND model_operation_id IS NOT NULL
            AND model_operation_id = BTRIM(model_operation_id)
            AND BTRIM(model_operation_id) <> ''
            AND model_operation_id !~ E'[\\x00\\r\\n]'
            AND original_http_status IS NOT NULL
            AND original_http_status = 201
            AND reused_connection IS NOT NULL
            AND reused_public_model IS NOT NULL
            AND reused_deployment IS NOT NULL
            AND reused_credential IS NOT NULL
            AND reused_operation IS NOT NULL
            AND completed_at IS NOT NULL
            AND updated_at = completed_at)
    ),
    CONSTRAINT model_deployment_create_receipts_time_check CHECK (
        updated_at >= created_at
        AND expires_at > created_at
        AND (completed_at IS NULL OR completed_at >= created_at)
    )
);

CREATE INDEX model_deployment_create_receipts_deployment_idx
    ON model_deployment_create_receipts(customer_org_id, deployment_id, model_operation_id)
    WHERE status='completed';
CREATE INDEX model_deployment_create_receipts_expiry_idx
    ON model_deployment_create_receipts(expires_at, id);

CREATE FUNCTION preserve_model_deployment_create_receipt()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.expires_at > clock_timestamp() THEN
            RAISE EXCEPTION 'model deployment create receipt is retained until expiry'
                USING ERRCODE = '55000';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.customer_org_id IS DISTINCT FROM OLD.customer_org_id
       OR NEW.actor_user_id IS DISTINCT FROM OLD.actor_user_id
       OR NEW.actor_org_id IS DISTINCT FROM OLD.actor_org_id
       OR NEW.operation_scope IS DISTINCT FROM OLD.operation_scope
       OR NEW.idempotency_key_hash IS DISTINCT FROM OLD.idempotency_key_hash
       OR NEW.request_fingerprint IS DISTINCT FROM OLD.request_fingerprint
       OR NEW.server_operation_id IS DISTINCT FROM OLD.server_operation_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
        RAISE EXCEPTION 'model deployment create receipt identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'completed' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'model deployment create receipt is terminal'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'in_progress' AND NEW.status <> 'completed' THEN
        RAISE EXCEPTION 'model deployment create receipt only permits completion'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_deployment_create_receipts_immutable
BEFORE UPDATE OR DELETE ON model_deployment_create_receipts
FOR EACH ROW EXECUTE FUNCTION preserve_model_deployment_create_receipt();

COMMENT ON TABLE model_deployment_create_receipts IS
    'Terminal, actor-scoped idempotency receipts for canonical model-deployment creation, retained for 24 hours then eligible for controlled deletion. Contains no raw idempotency key, request body, provider secret, or response body.';
COMMENT ON COLUMN model_deployment_create_receipts.request_fingerprint IS
    'Domain-separated HMAC of the normalized request; a provider API key contributes only through a nested HMAC.';

COMMIT;
