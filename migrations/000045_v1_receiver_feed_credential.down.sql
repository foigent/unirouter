BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000045_v1_receiver_feed_credential'
    ) THEN
        RAISE EXCEPTION 'receiver feed credential rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (
        SELECT 1 FROM api_keys
        WHERE purpose = 'xinsight.feed.read'
          AND verifier_scheme = 'receiver_sha256_v1'
          AND status IN ('feed_active', 'feed_staged')
    ) THEN
        RAISE EXCEPTION 'receiver feed credential rollback refused: serving or staged receiver credentials exist';
    END IF;
END;
$$;

DROP INDEX api_keys_receiver_feed_operation;

ALTER TABLE api_keys
    DROP CONSTRAINT api_keys_verifier_scheme_purpose_check,
    DROP COLUMN registration_operation_id,
    DROP COLUMN verifier_scheme;

DELETE FROM schema_migrations
WHERE version = '000045_v1_receiver_feed_credential';

COMMIT;
