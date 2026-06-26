-- Migration 000005: Add missing fields for PRD F09/F10
-- org_model_prices table is already created in 000001, but degradation_threshold and allowed_models are missing

BEGIN;

-- F07: Channel degradation threshold (for degraded status auto-switch)
ALTER TABLE channels ADD COLUMN IF NOT EXISTS degradation_threshold INTEGER NOT NULL DEFAULT 10;

-- F11: Channel key allowed models (comma-separated, empty = all models)
ALTER TABLE channel_keys ADD COLUMN IF NOT EXISTS allowed_models TEXT NOT NULL DEFAULT '';

-- Index for org_model_prices (if not exists)
CREATE INDEX IF NOT EXISTS idx_org_model_prices_org_id ON org_model_prices(org_id);

COMMIT;
