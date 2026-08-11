BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE xinsight_connector_control_bindings,
    xinsight_connector_installations,
    schema_migrations IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000062_xinsight_connector_redeem_lifecycle'
    ) THEN
        RAISE EXCEPTION 'X-Insight Connector redeem lifecycle rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (
        SELECT 1 FROM xinsight_connector_control_bindings
        WHERE control_token_enc IS NOT NULL
           OR control_token_expires_at IS NOT NULL
           OR redeem_request_sha256 IS NOT NULL
           OR redeem_result_sha256 IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'X-Insight Connector redeem lifecycle rollback refused: pending or active control authority exists';
    END IF;
END $$;

ALTER TABLE xinsight_connector_control_bindings
    DROP CONSTRAINT xinsight_connector_control_binding_secret_lifecycle,
    DROP COLUMN redeem_result_sha256,
    DROP COLUMN redeem_request_sha256,
    DROP COLUMN control_token_expires_at;

ALTER TABLE xinsight_connector_control_bindings
    ADD CONSTRAINT xinsight_connector_control_binding_secret_lifecycle
        CHECK (
            (invitation_token_enc IS NOT NULL
             AND octet_length(invitation_token_enc) BETWEEN 1 AND 4096
             AND control_project_id IS NULL
             AND control_token_enc IS NULL
             AND control_token_sha256 IS NULL)
            OR
            (invitation_token_enc IS NULL
             AND control_token_enc IS NOT NULL
             AND octet_length(control_token_enc) BETWEEN 1 AND 4096
             AND control_project_id IS NOT NULL
             AND octet_length(control_project_id) BETWEEN 1 AND 128
             AND control_project_id = BTRIM(control_project_id)
             AND control_project_id !~ '[[:cntrl:]]'
             AND control_token_sha256 IS NOT NULL
             AND control_token_sha256 ~ '^sha256:[a-f0-9]{64}$')
        );

DELETE FROM schema_migrations
WHERE version = '000062_xinsight_connector_redeem_lifecycle';

COMMIT;
