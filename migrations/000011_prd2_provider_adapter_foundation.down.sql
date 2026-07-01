-- Rollback migration 000011: PRD2 provider adapter foundation

BEGIN;

DROP INDEX IF EXISTS idx_channels_adapter_type;
ALTER TABLE channels DROP COLUMN IF EXISTS adapter_config;
ALTER TABLE channels DROP COLUMN IF EXISTS adapter_type;

COMMIT;
