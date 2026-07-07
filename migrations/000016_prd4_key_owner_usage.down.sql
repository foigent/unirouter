BEGIN;

DROP INDEX IF EXISTS idx_call_logs_key_type_created;
DROP INDEX IF EXISTS idx_call_logs_owner_created;
DROP INDEX IF EXISTS idx_api_keys_key_type;
DROP INDEX IF EXISTS idx_api_keys_owner_user_id;

ALTER TABLE call_logs DROP CONSTRAINT IF EXISTS call_logs_key_type_check;
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_key_type_check;

ALTER TABLE call_logs DROP COLUMN IF EXISTS key_type;
ALTER TABLE call_logs DROP COLUMN IF EXISTS owner_user_id;
ALTER TABLE api_keys DROP COLUMN IF EXISTS key_type;
ALTER TABLE api_keys DROP COLUMN IF EXISTS owner_user_id;

COMMIT;
