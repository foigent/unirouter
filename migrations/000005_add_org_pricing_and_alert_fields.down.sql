BEGIN;
ALTER TABLE channels DROP COLUMN IF EXISTS degradation_threshold;
ALTER TABLE channel_keys DROP COLUMN IF EXISTS allowed_models;
COMMIT;
