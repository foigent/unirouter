BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE xinsight_connector_control_bindings,
    xinsight_connector_installations,
    schema_migrations IN SHARE ROW EXCLUSIVE MODE;

-- Migration 61 could represent only an invitation or an active control
-- bearer. It cannot safely upgrade a pre-existing active bearer because the X
-- control contract also requires its exact expiry and redeem request/result
-- identities. Refuse to invent those facts.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM xinsight_connector_control_bindings
        WHERE control_token_enc IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'X-Insight Connector redeem lifecycle migration refused: existing control bearer lacks expiry and redeem identities';
    END IF;
END $$;

ALTER TABLE xinsight_connector_control_bindings
    DROP CONSTRAINT xinsight_connector_control_binding_secret_lifecycle,
    ADD COLUMN control_token_expires_at TIMESTAMPTZ,
    ADD COLUMN redeem_request_sha256 VARCHAR(71),
    ADD COLUMN redeem_result_sha256 VARCHAR(71);

-- The three states are intentionally derived from the secret columns rather
-- than duplicated in a mutable status column:
--   invitation_staged: invitation only;
--   redeem_pending: invitation plus the exact bearer/request to replay;
--   control_active: validated result recorded and invitation retired.
ALTER TABLE xinsight_connector_control_bindings
    ADD CONSTRAINT xinsight_connector_control_binding_secret_lifecycle
        CHECK (
            (invitation_token_enc IS NOT NULL
             AND octet_length(invitation_token_enc) BETWEEN 1 AND 4096
             AND control_project_id IS NULL
             AND control_token_enc IS NULL
             AND control_token_sha256 IS NULL
             AND control_token_expires_at IS NULL
             AND redeem_request_sha256 IS NULL
             AND redeem_result_sha256 IS NULL)
            OR
            (invitation_token_enc IS NOT NULL
             AND octet_length(invitation_token_enc) BETWEEN 1 AND 4096
             AND control_project_id IS NOT NULL
             AND octet_length(control_project_id) BETWEEN 1 AND 128
             AND control_project_id = BTRIM(control_project_id)
             AND control_project_id !~ '[[:cntrl:]]'
             AND control_token_enc IS NOT NULL
             AND octet_length(control_token_enc) BETWEEN 1 AND 4096
             AND control_token_sha256 IS NOT NULL
             AND control_token_sha256 ~ '^sha256:[a-f0-9]{64}$'
             AND control_token_expires_at IS NOT NULL
             AND control_token_expires_at > created_at
             AND redeem_request_sha256 IS NOT NULL
             AND redeem_request_sha256 ~ '^sha256:[a-f0-9]{64}$'
             AND redeem_result_sha256 IS NULL)
            OR
            (invitation_token_enc IS NULL
             AND control_project_id IS NOT NULL
             AND octet_length(control_project_id) BETWEEN 1 AND 128
             AND control_project_id = BTRIM(control_project_id)
             AND control_project_id !~ '[[:cntrl:]]'
             AND control_token_enc IS NOT NULL
             AND octet_length(control_token_enc) BETWEEN 1 AND 4096
             AND control_token_sha256 IS NOT NULL
             AND control_token_sha256 ~ '^sha256:[a-f0-9]{64}$'
             AND control_token_expires_at IS NOT NULL
             AND control_token_expires_at > created_at
             AND redeem_request_sha256 IS NOT NULL
             AND redeem_request_sha256 ~ '^sha256:[a-f0-9]{64}$'
             AND redeem_result_sha256 IS NOT NULL
             AND redeem_result_sha256 ~ '^sha256:[a-f0-9]{64}$')
        );

COMMIT;
