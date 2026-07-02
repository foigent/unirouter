-- Rollback migration 000012: PRD2 token metering V2 foundation

BEGIN;

DROP INDEX IF EXISTS idx_call_logs_usage_source_created;

UPDATE call_logs
SET usage_source = 'api'
WHERE usage_source = 'upstream';

UPDATE call_logs
SET usage_source = 'stream_estimate'
WHERE usage_source = 'counter_v2';

ALTER TABLE call_logs DROP COLUMN IF EXISTS usage_confidence;
ALTER TABLE call_logs DROP COLUMN IF EXISTS estimated_tokens;
DELETE FROM settings WHERE key = 'token_metering.estimated_ratio_alert_threshold';

COMMIT;
