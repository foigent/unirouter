-- Migration 000012: PRD2 token metering V2 foundation
-- Adds explicit estimate/confidence fields for reconciliation without changing billing invariants.

BEGIN;

ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS estimated_tokens INTEGER NOT NULL DEFAULT 0;

ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS usage_confidence NUMERIC(4,3) NOT NULL DEFAULT 1.000;

UPDATE call_logs
SET usage_source = 'upstream'
WHERE usage_source = 'api';

UPDATE call_logs
SET usage_source = 'counter_v2'
WHERE usage_source = 'stream_estimate';

UPDATE call_logs
SET usage_confidence = CASE
    WHEN usage_source = 'upstream' THEN 1.000
    WHEN usage_source = 'adapter_estimated' THEN 0.750
    WHEN usage_source = 'counter_v2' THEN 0.650
    WHEN usage_source = 'estimated' THEN 0.500
    ELSE 0.000
END;

CREATE INDEX IF NOT EXISTS idx_call_logs_usage_source_created ON call_logs(usage_source, created_at);

INSERT INTO settings (key, value, description, updated_at)
VALUES ('token_metering.estimated_ratio_alert_threshold', '0.2', 'Estimated-token ratio threshold for reconciliation alert signal', NOW())
ON CONFLICT (key) DO NOTHING;

COMMIT;
