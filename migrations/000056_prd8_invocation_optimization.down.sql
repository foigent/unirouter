BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM schema_migrations WHERE version > '000056_prd8_invocation_optimization') THEN
        RAISE EXCEPTION 'PRD8 invocation-optimization rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM invocation_optimization_policies)
       OR EXISTS (SELECT 1 FROM invocation_optimization_receipts)
       OR EXISTS (SELECT 1 FROM response_cache_delivery_receipts)
       OR EXISTS (SELECT 1 FROM response_cache_commercial_receipts)
       OR EXISTS (SELECT 1 FROM api_keys WHERE auth_epoch <> 1)
       OR EXISTS (SELECT 1 FROM freeze_idempotency WHERE usage_contract_version='unirouter.usage.v2')
       OR EXISTS (SELECT 1 FROM call_logs WHERE usage_contract_version='unirouter.usage.v2')
       OR EXISTS (SELECT 1 FROM usage_finalization_journal WHERE usage_contract_version='unirouter.usage.v2')
       OR EXISTS (SELECT 1 FROM request_idempotency WHERE usage_contract_version='unirouter.usage.v2') THEN
        RAISE EXCEPTION 'PRD8 invocation-optimization rollback refused: policy or runtime evidence exists';
    END IF;
END;
$$;

DROP INDEX idx_freeze_idempotency_settlement_recovery;
CREATE INDEX idx_freeze_idempotency_settlement_recovery
    ON freeze_idempotency(settlement_next_attempt_at, settlement_payload_ready_at, created_at)
    WHERE status='settlement_pending' AND settlement_payload_ready_at IS NOT NULL
      AND settlement_pending_reason IN ('billable_output','delivery_completed');

ALTER TABLE freeze_idempotency DROP CONSTRAINT freeze_idempotency_pending_reason_valid;
ALTER TABLE freeze_idempotency ADD CONSTRAINT freeze_idempotency_pending_reason_valid CHECK (
    settlement_pending_reason IN (
        '', 'billable_output', 'insufficient_topup', 'ambiguous_attempt',
        'provider_response_unusable', 'provider_attempt_unresolved',
        'delivery_prepared', 'delivery_completed', 'delivery_failed',
        'guardrail_withheld', 'response_too_large'
    )
);

ALTER TABLE freeze_idempotency DROP CONSTRAINT freeze_idempotency_usage_contract_check;
ALTER TABLE call_logs DROP CONSTRAINT call_logs_usage_contract_check;
ALTER TABLE usage_finalization_journal DROP CONSTRAINT usage_finalization_contract_check;
ALTER TABLE request_idempotency DROP CONSTRAINT request_idempotency_usage_contract_check;

ALTER TABLE freeze_idempotency ADD CONSTRAINT freeze_idempotency_usage_contract_check CHECK (
    usage_contract_version='unirouter.usage.v1' AND usage_purpose IN ('provider_evidence','customer_charge') AND (
        (usage_authority='provider_reported' AND usage_finality='final' AND usage_estimate_version='')
        OR (usage_authority='gateway_counter' AND usage_finality='estimated' AND usage_estimate_version='unirouter.stream-counter.v2')
        OR (usage_authority='conservative_estimate' AND usage_finality='estimated' AND length(btrim(usage_estimate_version))>0)
        OR (usage_authority='none' AND usage_purpose='provider_evidence' AND usage_finality='missing' AND usage_estimate_version='')
    )
);
ALTER TABLE call_logs ADD CONSTRAINT call_logs_usage_contract_check CHECK (
    usage_contract_version='unirouter.usage.v1' AND usage_purpose IN ('provider_evidence','customer_charge') AND (
        (usage_authority='provider_reported' AND usage_finality='final' AND usage_estimate_version='')
        OR (usage_authority='gateway_counter' AND usage_finality='estimated' AND usage_estimate_version='unirouter.stream-counter.v2')
        OR (usage_authority='conservative_estimate' AND usage_finality='estimated' AND length(btrim(usage_estimate_version))>0)
        OR (usage_authority='none' AND usage_purpose='provider_evidence' AND usage_finality='missing' AND usage_estimate_version='')
    )
);
ALTER TABLE usage_finalization_journal ADD CONSTRAINT usage_finalization_contract_check CHECK (
    usage_contract_version='unirouter.usage.v1' AND usage_purpose IN ('provider_evidence','customer_charge') AND (
        (usage_authority='provider_reported' AND usage_finality='final' AND usage_estimate_version='')
        OR (usage_authority='gateway_counter' AND usage_finality='estimated' AND usage_estimate_version='unirouter.stream-counter.v2')
        OR (usage_authority='conservative_estimate' AND usage_finality='estimated' AND length(btrim(usage_estimate_version))>0)
        OR (usage_authority='none' AND usage_purpose='provider_evidence' AND usage_finality='missing' AND usage_estimate_version='')
    )
);
ALTER TABLE request_idempotency ADD CONSTRAINT request_idempotency_usage_contract_check CHECK (
    usage_contract_version='unirouter.usage.v1' AND usage_purpose IN ('provider_evidence','customer_charge') AND (
        (usage_authority='provider_reported' AND usage_finality='final' AND usage_estimate_version='')
        OR (usage_authority='gateway_counter' AND usage_finality='estimated' AND usage_estimate_version='unirouter.stream-counter.v2')
        OR (usage_authority='conservative_estimate' AND usage_finality='estimated' AND length(btrim(usage_estimate_version))>0)
        OR (usage_authority='none' AND usage_purpose='provider_evidence' AND usage_finality='missing' AND usage_estimate_version='')
    )
);

DROP TRIGGER response_cache_commercial_receipts_append_only ON response_cache_commercial_receipts;
DROP TRIGGER response_cache_delivery_receipts_append_only ON response_cache_delivery_receipts;
DROP TRIGGER invocation_optimization_receipts_append_only ON invocation_optimization_receipts;
DROP FUNCTION reject_invocation_optimization_evidence_mutation();
DROP TABLE response_cache_commercial_receipts;
DROP TABLE response_cache_delivery_receipts;
DROP TABLE invocation_optimization_receipts;
DROP TABLE invocation_optimization_policy_revisions;
DROP TABLE invocation_optimization_policies;

DROP TRIGGER trg_api_keys_auth_epoch_monotonic ON api_keys;
DROP FUNCTION enforce_api_key_auth_epoch();
ALTER TABLE api_keys DROP CONSTRAINT api_keys_auth_epoch_positive;
ALTER TABLE api_keys DROP COLUMN auth_epoch;

DELETE FROM schema_migrations WHERE version='000056_prd8_invocation_optimization';

COMMIT;
