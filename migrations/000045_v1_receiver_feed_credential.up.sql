BEGIN;

ALTER TABLE api_keys
    ADD COLUMN verifier_scheme VARCHAR(32) NOT NULL DEFAULT 'legacy_hmac_sha256_v1',
    ADD COLUMN registration_operation_id VARCHAR(32) NOT NULL DEFAULT '',
    ADD CONSTRAINT api_keys_verifier_scheme_purpose_check CHECK (
        (
            purpose = ''
            AND verifier_scheme = 'legacy_hmac_sha256_v1'
            AND registration_operation_id = ''
        )
        OR (
            purpose = 'xinsight.feed.read'
            AND (
                (
                    verifier_scheme = 'legacy_hmac_sha256_v1'
                    AND registration_operation_id = ''
                )
                OR (
                    verifier_scheme = 'receiver_sha256_v1'
                    AND registration_operation_id ~ '^[0-9a-f]{32}$'
                )
            )
        )
    );

CREATE UNIQUE INDEX api_keys_receiver_feed_operation
    ON api_keys (project_id, registration_operation_id)
    WHERE purpose = 'xinsight.feed.read'
      AND verifier_scheme = 'receiver_sha256_v1';

COMMIT;
