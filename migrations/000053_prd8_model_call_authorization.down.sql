BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000053_prd8_model_call_authorization'
    ) THEN
        RAISE EXCEPTION 'PRD8 model-call-authorization rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM model_call_authorizations) THEN
        RAISE EXCEPTION 'PRD8 model-call-authorization rollback refused: authorization evidence exists';
    END IF;
END;
$$;

DROP TABLE model_call_authorizations;

DELETE FROM schema_migrations
WHERE version = '000053_prd8_model_call_authorization';

COMMIT;
