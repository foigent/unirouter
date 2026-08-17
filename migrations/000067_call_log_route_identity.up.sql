BEGIN;

-- A channel display name is mutable and intentionally non-unique. Persist the
-- immutable route identity selected by the gateway so operational projections
-- never attribute one request to every same-named provider endpoint.
ALTER TABLE call_logs
    ADD COLUMN route_channel_id BIGINT;

COMMENT ON COLUMN call_logs.route_channel_id IS
    'Immutable channel id selected for this request; NULL means no route or legacy evidence without a provable route identity.';

-- Provider-attempt evidence is the only safe legacy backfill authority. Do not
-- infer identity from call_logs.channel because duplicate names are valid.
UPDATE call_logs call_log
   SET route_channel_id = provider_attempt.channel_id
  FROM billing_provider_attempts provider_attempt
 WHERE call_log.route_channel_id IS NULL
   AND NULLIF(BTRIM(call_log.attempt_id), '') IS NOT NULL
   AND provider_attempt.org_id = call_log.org_id
   AND provider_attempt.request_id = call_log.request_id
   AND provider_attempt.attempt_id = call_log.attempt_id;

CREATE INDEX idx_call_logs_org_route_channel_created
    ON call_logs (org_id, route_channel_id, created_at)
    WHERE route_channel_id IS NOT NULL;

COMMIT;
