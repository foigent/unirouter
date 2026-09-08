BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE;
SELECT pg_advisory_xact_lock(86420517831702492);
LOCK TABLE stream_delivery_leases, stream_delivery_checkpoints, freeze_idempotency, call_logs, usage_finalization_journal, request_idempotency IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF (SELECT max(version) FROM schema_migrations) <> '000115_stream_counter_version' THEN
  RAISE EXCEPTION 'stream counter rollback requires the exact M115 head';
 END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations WHERE state <> 'stopped' AND (ingress_state <> 'absent' OR lease_expires_at > clock_timestamp())) THEN
  RAISE EXCEPTION 'stream counter rollback requires no serving or recovery generation';
 END IF;
 IF EXISTS(SELECT 1 FROM stream_delivery_leases WHERE counter_version='unirouter.stream-counter.v3') OR EXISTS(SELECT 1 FROM freeze_idempotency WHERE usage_source='counter_v3' OR usage_estimate_version='unirouter.stream-counter.v3') OR EXISTS(SELECT 1 FROM call_logs WHERE usage_source='counter_v3' OR usage_estimate_version='unirouter.stream-counter.v3') OR EXISTS(SELECT 1 FROM usage_finalization_journal WHERE usage_source='counter_v3' OR usage_estimate_version='unirouter.stream-counter.v3') OR EXISTS(SELECT 1 FROM request_idempotency WHERE usage_estimate_version='unirouter.stream-counter.v3') THEN RAISE EXCEPTION 'v3 history prevents downgrade; keep forward recovery'; END IF;
END $$;
ALTER TABLE freeze_idempotency DROP CONSTRAINT freeze_idempotency_usage_contract_check;
ALTER TABLE call_logs DROP CONSTRAINT call_logs_usage_contract_check;
ALTER TABLE usage_finalization_journal DROP CONSTRAINT usage_finalization_contract_check;
ALTER TABLE request_idempotency DROP CONSTRAINT request_idempotency_usage_contract_check;
ALTER TABLE freeze_idempotency ADD CONSTRAINT freeze_idempotency_usage_contract_check CHECK (
    usage_purpose IN ('provider_evidence','customer_charge') AND (
        (usage_contract_version='unirouter.usage.v1' AND (
            (usage_authority='provider_reported' AND usage_finality='final' AND usage_estimate_version='')
            OR (usage_authority='gateway_counter' AND usage_finality='estimated' AND usage_estimate_version='unirouter.stream-counter.v2')
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
            OR (usage_authority='gateway_counter' AND usage_finality='estimated' AND usage_estimate_version='unirouter.stream-counter.v2')
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
            OR (usage_authority='gateway_counter' AND usage_finality='estimated' AND usage_estimate_version='unirouter.stream-counter.v2')
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
            OR (usage_authority='gateway_counter' AND usage_finality='estimated' AND usage_estimate_version='unirouter.stream-counter.v2')
            OR (usage_authority='conservative_estimate' AND usage_finality='estimated' AND length(btrim(usage_estimate_version))>0)
            OR (usage_authority='none' AND usage_purpose='provider_evidence' AND usage_finality='missing' AND usage_estimate_version='')
        )) OR
        (usage_contract_version='unirouter.usage.v2' AND usage_authority='local_cache'
         AND usage_purpose='customer_charge' AND usage_finality='final'
         AND usage_estimate_version='')
    )
);

DROP TRIGGER stream_counter_version_immutable ON stream_delivery_leases;
DROP FUNCTION preserve_stream_counter_version();
ALTER TABLE stream_delivery_leases DROP COLUMN counter_version;
DELETE FROM schema_migrations WHERE version='000115_stream_counter_version';
COMMIT;
