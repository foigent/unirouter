-- Migration 000011: PRD2 provider adapter foundation
-- Adds adapter metadata to channels while preserving existing OpenAI-compatible behavior.

BEGIN;

ALTER TABLE channels
    ADD COLUMN IF NOT EXISTS adapter_type VARCHAR(50) NOT NULL DEFAULT 'openai_compatible';

ALTER TABLE channels
    ADD COLUMN IF NOT EXISTS adapter_config JSONB NOT NULL DEFAULT '{}';

UPDATE channels
SET adapter_type = 'openai_compatible'
WHERE adapter_type IS NULL OR adapter_type = '';

CREATE INDEX IF NOT EXISTS idx_channels_adapter_type ON channels(adapter_type);

COMMIT;
