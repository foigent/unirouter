-- unirouter:explicit-compatible-expand-v1
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE;
SELECT pg_advisory_xact_lock(86420517831702492);
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest)
   WHERE g.state <> 'stopped' AND (g.ingress_state <> 'absent' OR g.lease_expires_at > clock_timestamp()) AND (m.artifact_digest IS NULL OR NOT (
    convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000115_stream_counter_version'
    AND convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @> '[{"kind":"stream_counter","id":"unirouter.stream-counter.v2"},{"kind":"stream_counter","id":"unirouter.stream-counter.v3"}]'::jsonb))) THEN
  RAISE EXCEPTION 'all serving and recovery generations must support stream counter v3';
 END IF;
END $$;
ALTER TABLE stream_delivery_leases ADD COLUMN counter_version VARCHAR(80) NOT NULL DEFAULT 'unirouter.stream-counter.v2',
 ADD CONSTRAINT stream_delivery_counter_version_check CHECK (counter_version IN ('unirouter.stream-counter.v2','unirouter.stream-counter.v3'));
CREATE FUNCTION preserve_stream_counter_version() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.counter_version IS DISTINCT FROM OLD.counter_version THEN RAISE EXCEPTION 'stream counter version is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER stream_counter_version_immutable BEFORE UPDATE OF counter_version ON stream_delivery_leases FOR EACH ROW EXECUTE FUNCTION preserve_stream_counter_version();
ALTER TABLE freeze_idempotency DROP CONSTRAINT freeze_idempotency_usage_contract_check;
ALTER TABLE call_logs DROP CONSTRAINT call_logs_usage_contract_check;
ALTER TABLE usage_finalization_journal DROP CONSTRAINT usage_finalization_contract_check;
ALTER TABLE request_idempotency DROP CONSTRAINT request_idempotency_usage_contract_check;
ALTER TABLE freeze_idempotency ADD CONSTRAINT freeze_idempotency_usage_contract_check CHECK (
    usage_purpose IN ('provider_evidence','customer_charge') AND (
        (usage_contract_version='unirouter.usage.v1' AND (
            (usage_authority='provider_reported' AND usage_finality='final' AND usage_estimate_version='')
            OR (usage_authority='gateway_counter' AND usage_finality='estimated' AND ((usage_source='counter_v2' AND usage_estimate_version='unirouter.stream-counter.v2') OR (usage_source='counter_v3' AND usage_estimate_version='unirouter.stream-counter.v3')))
            OR (usage_authority='conservative_estimate' AND usage_finality='estimated' AND length(btrim(usage_estimate_version))>0)
            OR (usage_authority='none' AND usage_purpose='provider_evidence' AND usage_finality='missing' AND usage_estimate_version='')
        )) OR
        (usage_contract_version='unirouter.usage.v2' AND usage_source='response_cache'
         AND usage_authority='local_cache' AND usage_purpose='customer_charge'
         AND usage_finality='final' AND usage_estimate_version=''
         AND prompt_tokens=0 AND uncached_prompt_tokens=0
         AND cache_read_tokens=0 AND cache_creation_tokens=0 AND completion_tokens=0)
    )
);

ALTER TABLE call_logs ADD CONSTRAINT call_logs_usage_contract_check CHECK (
    usage_purpose IN ('provider_evidence','customer_charge') AND (
        (usage_contract_version='unirouter.usage.v1' AND (
            (usage_authority='provider_reported' AND usage_finality='final' AND usage_estimate_version='')
            OR (usage_authority='gateway_counter' AND usage_finality='estimated' AND ((usage_source='counter_v2' AND usage_estimate_version='unirouter.stream-counter.v2') OR (usage_source='counter_v3' AND usage_estimate_version='unirouter.stream-counter.v3')))
            OR (usage_authority='conservative_estimate' AND usage_finality='estimated' AND length(btrim(usage_estimate_version))>0)
            OR (usage_authority='none' AND usage_purpose='provider_evidence' AND usage_finality='missing' AND usage_estimate_version='')
        )) OR
        (usage_contract_version='unirouter.usage.v2' AND usage_source='response_cache'
         AND usage_authority='local_cache' AND usage_purpose='customer_charge'
         AND usage_finality='final' AND usage_estimate_version=''
         AND prompt_tokens=0 AND uncached_prompt_tokens=0
         AND cache_read_tokens=0 AND cache_creation_tokens=0 AND completion_tokens=0)
    )
);

ALTER TABLE usage_finalization_journal ADD CONSTRAINT usage_finalization_contract_check CHECK (
    usage_purpose IN ('provider_evidence','customer_charge') AND (
        (usage_contract_version='unirouter.usage.v1' AND (
            (usage_authority='provider_reported' AND usage_finality='final' AND usage_estimate_version='')
            OR (usage_authority='gateway_counter' AND usage_finality='estimated' AND ((usage_source='counter_v2' AND usage_estimate_version='unirouter.stream-counter.v2') OR (usage_source='counter_v3' AND usage_estimate_version='unirouter.stream-counter.v3')))
            OR (usage_authority='conservative_estimate' AND usage_finality='estimated' AND length(btrim(usage_estimate_version))>0)
            OR (usage_authority='none' AND usage_purpose='provider_evidence' AND usage_finality='missing' AND usage_estimate_version='')
        )) OR
        (usage_contract_version='unirouter.usage.v2' AND usage_source='response_cache'
         AND usage_authority='local_cache' AND usage_purpose='customer_charge'
         AND usage_finality='final' AND usage_estimate_version=''
         AND prompt_tokens=0 AND uncached_prompt_tokens=0
         AND cache_read_tokens=0 AND cache_creation_tokens=0 AND completion_tokens=0)
    )
);

ALTER TABLE request_idempotency ADD CONSTRAINT request_idempotency_usage_contract_check CHECK (
    usage_purpose IN ('provider_evidence','customer_charge') AND (
        (usage_contract_version='unirouter.usage.v1' AND (
            (usage_authority='provider_reported' AND usage_finality='final' AND usage_estimate_version='')
            OR (usage_authority='gateway_counter' AND usage_finality='estimated' AND usage_estimate_version IN ('unirouter.stream-counter.v2','unirouter.stream-counter.v3'))
            OR (usage_authority='conservative_estimate' AND usage_finality='estimated' AND length(btrim(usage_estimate_version))>0)
            OR (usage_authority='none' AND usage_purpose='provider_evidence' AND usage_finality='missing' AND usage_estimate_version='')
        )) OR
        (usage_contract_version='unirouter.usage.v2' AND usage_authority='local_cache'
         AND usage_purpose='customer_charge' AND usage_finality='final'
         AND usage_estimate_version='')
    )
);


COMMIT;
