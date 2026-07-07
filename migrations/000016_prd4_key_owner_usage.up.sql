BEGIN;

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS owner_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS key_type VARCHAR(20) NOT NULL DEFAULT 'shared';

UPDATE api_keys
SET key_type = 'shared'
WHERE key_type IS NULL OR key_type = '';

DO $$
BEGIN
    ALTER TABLE api_keys
        ADD CONSTRAINT api_keys_key_type_check
        CHECK (key_type IN ('shared', 'personal', 'service'));
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS owner_user_id BIGINT,
    ADD COLUMN IF NOT EXISTS key_type VARCHAR(20) NOT NULL DEFAULT 'unknown';

UPDATE call_logs cl
SET key_type = COALESCE(NULLIF(ak.key_type, ''), 'shared')
FROM api_keys ak
WHERE cl.api_key_id = ak.id
  AND (cl.key_type IS NULL OR cl.key_type = '' OR cl.key_type = 'unknown');

DO $$
BEGIN
    ALTER TABLE call_logs
        ADD CONSTRAINT call_logs_key_type_check
        CHECK (key_type IN ('unknown', 'shared', 'personal', 'service'));
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS idx_api_keys_owner_user_id ON api_keys(owner_user_id) WHERE owner_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_key_type ON api_keys(key_type);
CREATE INDEX IF NOT EXISTS idx_call_logs_owner_created ON call_logs(owner_user_id, created_at) WHERE owner_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_call_logs_key_type_created ON call_logs(key_type, created_at);

COMMIT;
