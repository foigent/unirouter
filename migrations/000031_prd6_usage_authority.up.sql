BEGIN;

-- P6-F41A makes zero, missing and estimated usage distinguishable financial
-- evidence. These bounded fields never contain prompts or model responses.
ALTER TABLE freeze_idempotency
    ADD COLUMN IF NOT EXISTS usage_authority VARCHAR(40) NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS usage_purpose VARCHAR(32) NOT NULL DEFAULT 'provider_evidence',
    ADD COLUMN IF NOT EXISTS usage_finality VARCHAR(24) NOT NULL DEFAULT 'missing',
    ADD COLUMN IF NOT EXISTS usage_contract_version VARCHAR(80) NOT NULL DEFAULT 'unirouter.usage.v1',
    ADD COLUMN IF NOT EXISTS usage_estimate_version VARCHAR(80) NOT NULL DEFAULT '';

ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS usage_authority VARCHAR(40) NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS usage_purpose VARCHAR(32) NOT NULL DEFAULT 'provider_evidence',
    ADD COLUMN IF NOT EXISTS usage_finality VARCHAR(24) NOT NULL DEFAULT 'missing',
    ADD COLUMN IF NOT EXISTS usage_contract_version VARCHAR(80) NOT NULL DEFAULT 'unirouter.usage.v1',
    ADD COLUMN IF NOT EXISTS usage_estimate_version VARCHAR(80) NOT NULL DEFAULT '';

ALTER TABLE usage_finalization_journal
    ADD COLUMN IF NOT EXISTS usage_authority VARCHAR(40) NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS usage_purpose VARCHAR(32) NOT NULL DEFAULT 'provider_evidence',
    ADD COLUMN IF NOT EXISTS usage_finality VARCHAR(24) NOT NULL DEFAULT 'missing',
    ADD COLUMN IF NOT EXISTS usage_contract_version VARCHAR(80) NOT NULL DEFAULT 'unirouter.usage.v1',
    ADD COLUMN IF NOT EXISTS usage_estimate_version VARCHAR(80) NOT NULL DEFAULT '';

UPDATE freeze_idempotency
SET usage_authority = CASE usage_source
        WHEN 'upstream' THEN 'provider_reported'
        WHEN 'counter_v2' THEN 'gateway_counter'
        WHEN 'estimated' THEN 'conservative_estimate'
        WHEN 'adapter_estimated' THEN 'conservative_estimate'
        ELSE 'none'
    END,
    usage_purpose = CASE
        WHEN usage_source IN ('upstream', 'counter_v2', 'estimated', 'adapter_estimated')
          THEN 'customer_charge'
        ELSE 'provider_evidence'
    END,
    usage_finality = CASE usage_source
        WHEN 'upstream' THEN 'final'
        WHEN 'counter_v2' THEN 'estimated'
        WHEN 'estimated' THEN 'estimated'
        WHEN 'adapter_estimated' THEN 'estimated'
        ELSE 'missing'
    END,
    usage_estimate_version = CASE usage_source
        WHEN 'counter_v2' THEN 'unirouter.stream-counter.v2'
        WHEN 'estimated' THEN 'unirouter.token-ceiling.v1'
        WHEN 'adapter_estimated' THEN 'unirouter.token-ceiling.v1'
        ELSE ''
    END;

UPDATE call_logs
SET usage_authority = CASE usage_source
        WHEN 'upstream' THEN 'provider_reported'
        WHEN 'counter_v2' THEN 'gateway_counter'
        WHEN 'estimated' THEN 'conservative_estimate'
        WHEN 'adapter_estimated' THEN 'conservative_estimate'
        ELSE 'none'
    END,
    usage_purpose = CASE
        WHEN usage_source IN ('upstream', 'counter_v2', 'estimated', 'adapter_estimated')
          THEN 'customer_charge'
        ELSE 'provider_evidence'
    END,
    usage_finality = CASE usage_source
        WHEN 'upstream' THEN 'final'
        WHEN 'counter_v2' THEN 'estimated'
        WHEN 'estimated' THEN 'estimated'
        WHEN 'adapter_estimated' THEN 'estimated'
        ELSE 'missing'
    END,
    usage_estimate_version = CASE usage_source
        WHEN 'counter_v2' THEN 'unirouter.stream-counter.v2'
        WHEN 'estimated' THEN 'unirouter.token-ceiling.v1'
        WHEN 'adapter_estimated' THEN 'unirouter.token-ceiling.v1'
        ELSE ''
    END;

UPDATE usage_finalization_journal
SET usage_authority = CASE usage_source
        WHEN 'upstream' THEN 'provider_reported'
        WHEN 'counter_v2' THEN 'gateway_counter'
        WHEN 'estimated' THEN 'conservative_estimate'
        WHEN 'adapter_estimated' THEN 'conservative_estimate'
        ELSE 'none'
    END,
    usage_purpose = CASE
        WHEN usage_source IN ('upstream', 'counter_v2', 'estimated', 'adapter_estimated')
          THEN 'customer_charge'
        ELSE 'provider_evidence'
    END,
    usage_finality = CASE usage_source
        WHEN 'upstream' THEN 'final'
        WHEN 'counter_v2' THEN 'estimated'
        WHEN 'estimated' THEN 'estimated'
        WHEN 'adapter_estimated' THEN 'estimated'
        ELSE 'missing'
    END,
    usage_estimate_version = CASE usage_source
        WHEN 'counter_v2' THEN 'unirouter.stream-counter.v2'
        WHEN 'estimated' THEN 'unirouter.token-ceiling.v1'
        WHEN 'adapter_estimated' THEN 'unirouter.token-ceiling.v1'
        ELSE ''
    END;

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_usage_contract_check CHECK (
        usage_contract_version = 'unirouter.usage.v1'
        AND usage_purpose IN ('provider_evidence', 'customer_charge')
        AND (
            (usage_authority = 'provider_reported' AND usage_finality = 'final' AND usage_estimate_version = '')
            OR (usage_authority = 'gateway_counter' AND usage_finality = 'estimated' AND usage_estimate_version = 'unirouter.stream-counter.v2')
            OR (usage_authority = 'conservative_estimate' AND usage_finality = 'estimated' AND length(btrim(usage_estimate_version)) > 0)
            OR (usage_authority = 'none' AND usage_purpose = 'provider_evidence' AND usage_finality = 'missing' AND usage_estimate_version = '')
        )
    );

ALTER TABLE call_logs
    ADD CONSTRAINT call_logs_usage_contract_check CHECK (
        usage_contract_version = 'unirouter.usage.v1'
        AND usage_purpose IN ('provider_evidence', 'customer_charge')
        AND (
            (usage_authority = 'provider_reported' AND usage_finality = 'final' AND usage_estimate_version = '')
            OR (usage_authority = 'gateway_counter' AND usage_finality = 'estimated' AND usage_estimate_version = 'unirouter.stream-counter.v2')
            OR (usage_authority = 'conservative_estimate' AND usage_finality = 'estimated' AND length(btrim(usage_estimate_version)) > 0)
            OR (usage_authority = 'none' AND usage_purpose = 'provider_evidence' AND usage_finality = 'missing' AND usage_estimate_version = '')
        )
    );

ALTER TABLE usage_finalization_journal
    ADD CONSTRAINT usage_finalization_contract_check CHECK (
        usage_contract_version = 'unirouter.usage.v1'
        AND usage_purpose IN ('provider_evidence', 'customer_charge')
        AND (
            (usage_authority = 'provider_reported' AND usage_finality = 'final' AND usage_estimate_version = '')
            OR (usage_authority = 'gateway_counter' AND usage_finality = 'estimated' AND usage_estimate_version = 'unirouter.stream-counter.v2')
            OR (usage_authority = 'conservative_estimate' AND usage_finality = 'estimated' AND length(btrim(usage_estimate_version)) > 0)
            OR (usage_authority = 'none' AND usage_purpose = 'provider_evidence' AND usage_finality = 'missing' AND usage_estimate_version = '')
        )
    );

COMMIT;
