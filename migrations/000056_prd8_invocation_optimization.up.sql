BEGIN;

-- API-key authority is part of exact-cache identity. Every control-plane
-- mutation invalidates prior entries without exposing key material.
ALTER TABLE api_keys
    ADD COLUMN auth_epoch BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT api_keys_auth_epoch_positive CHECK (auth_epoch > 0);

CREATE OR REPLACE FUNCTION enforce_api_key_auth_epoch()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.auth_epoch < OLD.auth_epoch THEN
        RAISE EXCEPTION 'api_keys.auth_epoch cannot decrease';
    END IF;
    IF NEW.auth_epoch = OLD.auth_epoch THEN
        NEW.auth_epoch := OLD.auth_epoch + 1;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_api_keys_auth_epoch_monotonic
BEFORE UPDATE ON api_keys
FOR EACH ROW EXECUTE FUNCTION enforce_api_key_auth_epoch();

CREATE TABLE invocation_optimization_policies (
    id BIGSERIAL PRIMARY KEY,
    contract_version VARCHAR(80) NOT NULL DEFAULT 'unirouter.invocation-optimization-policy.v1',
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    project_id BIGINT,
    policy_ref VARCHAR(64) NOT NULL,
    display_name VARCHAR(160) NOT NULL,
    model_selector VARCHAR(200) NOT NULL DEFAULT '*',
    region VARCHAR(160) NOT NULL DEFAULT 'global',
    residency VARCHAR(160) NOT NULL DEFAULT 'customer_policy',
    response_cache_target VARCHAR(16) NOT NULL DEFAULT 'disabled',
    response_cache_canary_basis_points INTEGER NOT NULL DEFAULT 0,
    response_cache_ttl_seconds INTEGER NOT NULL DEFAULT 300,
    response_cache_persistence_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    response_cache_require_request_opt_in BOOLEAN NOT NULL DEFAULT TRUE,
    compression_target VARCHAR(16) NOT NULL DEFAULT 'disabled',
    compression_canary_basis_points INTEGER NOT NULL DEFAULT 0,
    compression_require_request_opt_in BOOLEAN NOT NULL DEFAULT TRUE,
    compression_minimum_input_tokens INTEGER NOT NULL DEFAULT 128,
    compression_minimum_saved_tokens INTEGER NOT NULL DEFAULT 8,
    compression_maximum_reduction_basis_points INTEGER NOT NULL DEFAULT 2500,
    minimum_shadow_samples INTEGER NOT NULL DEFAULT 100,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    stage VARCHAR(16) NOT NULL DEFAULT 'draft',
    revision BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    updated_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    validated_at TIMESTAMPTZ(6),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT invocation_optimization_policy_project_fk
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT invocation_optimization_policy_contract_check
        CHECK (contract_version = 'unirouter.invocation-optimization-policy.v1'),
    CONSTRAINT invocation_optimization_policy_identity_check CHECK (
        policy_ref ~ '^opt_[a-z0-9][a-z0-9._-]{2,59}$'
        AND length(btrim(display_name)) BETWEEN 1 AND 160
        AND (model_selector = '*' OR model_selector ~ '^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,159}$')
        AND region ~ '^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,159}$'
        AND residency ~ '^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,159}$'
        AND revision > 0
    ),
    CONSTRAINT invocation_optimization_policy_stage_check
        CHECK (stage IN ('draft','validated','shadow','canary','rolled_back')),
    CONSTRAINT invocation_optimization_policy_status_check
        CHECK (status IN ('active','disabled')),
    CONSTRAINT invocation_optimization_policy_target_check CHECK (
        response_cache_target IN ('disabled','canary')
        AND compression_target IN ('disabled','canary')
        AND response_cache_canary_basis_points BETWEEN 0 AND 1000
        AND compression_canary_basis_points BETWEEN 0 AND 1000
        AND ((response_cache_target='canary') = (response_cache_canary_basis_points>0))
        AND ((compression_target='canary') = (compression_canary_basis_points>0))
    ),
    CONSTRAINT invocation_optimization_policy_safety_check CHECK (
        response_cache_ttl_seconds BETWEEN 60 AND 86400
        AND (response_cache_target <> 'canary' OR
             (response_cache_persistence_allowed AND response_cache_require_request_opt_in))
        AND (compression_target <> 'canary' OR compression_require_request_opt_in)
        AND compression_minimum_input_tokens BETWEEN 1 AND 1000000
        AND compression_minimum_saved_tokens BETWEEN 1 AND 1000000
        AND compression_maximum_reduction_basis_points BETWEEN 1 AND 5000
        AND minimum_shadow_samples BETWEEN 10 AND 1000000
    ),
    UNIQUE (org_id, policy_ref)
);

CREATE UNIQUE INDEX invocation_optimization_policy_scope_idx
    ON invocation_optimization_policies (org_id, COALESCE(project_id,0), model_selector);

CREATE INDEX invocation_optimization_policy_resolve_idx
    ON invocation_optimization_policies (org_id, project_id, model_selector, status, revision DESC);

CREATE TABLE invocation_optimization_policy_revisions (
    org_id BIGINT NOT NULL,
    policy_ref VARCHAR(64) NOT NULL,
    revision BIGINT NOT NULL,
    stage VARCHAR(16) NOT NULL,
    snapshot JSONB NOT NULL,
    actor_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reason VARCHAR(500) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, policy_ref, revision),
    CONSTRAINT invocation_optimization_revision_policy_fk
        FOREIGN KEY (org_id, policy_ref)
        REFERENCES invocation_optimization_policies(org_id, policy_ref) ON DELETE RESTRICT,
    CONSTRAINT invocation_optimization_revision_snapshot_check
        CHECK (jsonb_typeof(snapshot)='object' AND length(reason)<=500)
);

CREATE TABLE invocation_optimization_receipts (
    id BIGSERIAL PRIMARY KEY,
    contract_version VARCHAR(80) NOT NULL,
    org_id BIGINT NOT NULL,
    project_id BIGINT,
    request_id VARCHAR(64) NOT NULL,
    requested_model VARCHAR(200) NOT NULL,
    resolved_model VARCHAR(200) NOT NULL,
    provider VARCHAR(80) NOT NULL DEFAULT '',
    driver VARCHAR(32) NOT NULL,
    policy_ref VARCHAR(64) NOT NULL,
    policy_revision BIGINT NOT NULL,
    stage VARCHAR(16) NOT NULL,
    effective_mode VARCHAR(16) NOT NULL,
    decision VARCHAR(48) NOT NULL,
    reason VARCHAR(80) NOT NULL,
    assigned BOOLEAN NOT NULL,
    eligible BOOLEAN NOT NULL,
    mutation_applied BOOLEAN NOT NULL,
    original_bytes INTEGER NOT NULL DEFAULT 0,
    candidate_bytes INTEGER NOT NULL DEFAULT 0,
    original_estimated_tokens INTEGER NOT NULL DEFAULT 0,
    candidate_estimated_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_saved_tokens INTEGER NOT NULL DEFAULT 0,
    reduction_basis_points INTEGER NOT NULL DEFAULT 0,
    safety_passed BOOLEAN NOT NULL DEFAULT FALSE,
    quality_passed BOOLEAN NOT NULL DEFAULT FALSE,
    latency_micros BIGINT NOT NULL DEFAULT 0,
    route_channel_id BIGINT REFERENCES channels(id) ON DELETE RESTRICT,
    route_credential_id BIGINT REFERENCES channel_keys(id) ON DELETE RESTRICT,
    runtime_assignment_id VARCHAR(160) NOT NULL DEFAULT '',
    identity_digest VARCHAR(80) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT invocation_optimization_receipt_request_fk
        FOREIGN KEY (org_id, request_id) REFERENCES freeze_idempotency(org_id, request_id) ON DELETE RESTRICT,
    CONSTRAINT invocation_optimization_receipt_project_fk
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT invocation_optimization_receipt_policy_fk
        FOREIGN KEY (org_id, policy_ref, policy_revision)
        REFERENCES invocation_optimization_policy_revisions(org_id, policy_ref, revision) ON DELETE RESTRICT,
    CONSTRAINT invocation_optimization_receipt_contract_check
        CHECK (contract_version='unirouter.invocation-optimization-receipt.v1'),
    CONSTRAINT invocation_optimization_receipt_values_check CHECK (
        driver IN ('response_cache','compression')
        AND stage IN ('draft','validated','shadow','canary','rolled_back')
        AND effective_mode IN ('disabled','shadow','canary')
        AND length(btrim(decision)) BETWEEN 1 AND 48
        AND length(btrim(reason)) BETWEEN 1 AND 80
        AND length(btrim(requested_model)) BETWEEN 1 AND 200
        AND length(btrim(resolved_model)) BETWEEN 1 AND 200
        AND length(provider) <= 80
        AND policy_revision > 0
        AND original_bytes >= 0 AND candidate_bytes >= 0
        AND original_estimated_tokens >= 0 AND candidate_estimated_tokens >= 0
        AND estimated_saved_tokens >= 0
        AND reduction_basis_points BETWEEN 0 AND 10000
        AND latency_micros >= 0
        AND (identity_digest='' OR identity_digest ~ '^(sha256|hmac-sha256):[0-9a-f]{64}$')
    ),
    UNIQUE (org_id, request_id, driver)
);

CREATE INDEX invocation_optimization_receipt_policy_idx
    ON invocation_optimization_receipts (org_id, policy_ref, policy_revision, driver, created_at DESC);

CREATE TABLE response_cache_delivery_receipts (
    org_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    sequence SMALLINT NOT NULL,
    outcome VARCHAR(24) NOT NULL,
    delivered_bytes BIGINT NOT NULL DEFAULT 0,
    delivered_chunks INTEGER NOT NULL DEFAULT 0,
    http_status INTEGER,
    error_code VARCHAR(80) NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, request_id, sequence),
    CONSTRAINT response_cache_delivery_reservation_fk
        FOREIGN KEY (org_id, request_id) REFERENCES freeze_idempotency(org_id, request_id) ON DELETE RESTRICT,
    CONSTRAINT response_cache_delivery_values_check CHECK (
        sequence BETWEEN 1 AND 2
        AND outcome IN ('write_started','completed','write_failed')
        AND delivered_bytes >= 0 AND delivered_chunks >= 0
        AND (http_status IS NULL OR http_status BETWEEN 100 AND 599)
        AND ((outcome='completed' AND delivered_bytes>0 AND delivered_chunks=1)
             OR (outcome<>'completed' AND delivered_chunks=0))
    )
);

CREATE TABLE response_cache_commercial_receipts (
    id BIGSERIAL PRIMARY KEY,
    contract_version VARCHAR(80) NOT NULL,
    org_id BIGINT NOT NULL,
    project_id BIGINT,
    request_id VARCHAR(64) NOT NULL,
    source_request_id VARCHAR(64) NOT NULL,
    policy_ref VARCHAR(64) NOT NULL,
    policy_revision BIGINT NOT NULL,
    identity_digest VARCHAR(80) NOT NULL,
    source_response_sha256 VARCHAR(71) NOT NULL,
    requested_model VARCHAR(200) NOT NULL,
    resolved_model VARCHAR(200) NOT NULL,
    provider VARCHAR(80) NOT NULL,
    channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE RESTRICT,
    credential_id BIGINT NOT NULL REFERENCES channel_keys(id) ON DELETE RESTRICT,
    credential_epoch BIGINT NOT NULL,
    runtime_assignment_id VARCHAR(160) NOT NULL DEFAULT '',
    customer_pricing_version VARCHAR(160) NOT NULL,
    response_cache_price_version VARCHAR(160) NOT NULL,
    provider_requests BIGINT NOT NULL,
    provider_cost_micros BIGINT NOT NULL,
    customer_charge_micros BIGINT NOT NULL,
    avoided_customer_cost_micros BIGINT NOT NULL,
    delivery_status VARCHAR(24) NOT NULL,
    http_status INTEGER NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT response_cache_commercial_request_fk
        FOREIGN KEY (org_id, request_id) REFERENCES freeze_idempotency(org_id, request_id) ON DELETE RESTRICT,
    CONSTRAINT response_cache_commercial_project_fk
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT response_cache_commercial_policy_fk
        FOREIGN KEY (org_id, policy_ref, policy_revision)
        REFERENCES invocation_optimization_policy_revisions(org_id, policy_ref, revision) ON DELETE RESTRICT,
    CONSTRAINT response_cache_commercial_contract_check CHECK (
        contract_version='unirouter.response-cache-commercial-receipt.v1'
        AND provider_requests=0 AND provider_cost_micros=0 AND customer_charge_micros=0
        AND avoided_customer_cost_micros>=0
        AND delivery_status='completed' AND http_status BETWEEN 200 AND 299
        AND identity_digest ~ '^hmac-sha256:[0-9a-f]{64}$'
        AND source_response_sha256 ~ '^sha256:[0-9a-f]{64}$'
        AND credential_epoch>0 AND policy_revision>0
        AND response_cache_price_version='unirouter.response-cache-zero-charge.v1'
    ),
    UNIQUE (org_id, request_id)
);

CREATE OR REPLACE FUNCTION reject_invocation_optimization_evidence_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'invocation optimization evidence is append-only';
END;
$$;

CREATE TRIGGER invocation_optimization_receipts_append_only
BEFORE UPDATE OR DELETE ON invocation_optimization_receipts
FOR EACH ROW EXECUTE FUNCTION reject_invocation_optimization_evidence_mutation();

CREATE TRIGGER response_cache_delivery_receipts_append_only
BEFORE UPDATE OR DELETE ON response_cache_delivery_receipts
FOR EACH ROW EXECUTE FUNCTION reject_invocation_optimization_evidence_mutation();

CREATE TRIGGER response_cache_commercial_receipts_append_only
BEFORE UPDATE OR DELETE ON response_cache_commercial_receipts
FOR EACH ROW EXECUTE FUNCTION reject_invocation_optimization_evidence_mutation();

-- Usage v2 adds only the local exact-cache zero-token authority. Existing v1
-- rows and provider usage semantics remain byte-for-byte valid.
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

ALTER TABLE freeze_idempotency DROP CONSTRAINT freeze_idempotency_pending_reason_valid;
ALTER TABLE freeze_idempotency ADD CONSTRAINT freeze_idempotency_pending_reason_valid CHECK (
    settlement_pending_reason IN (
        '', 'billable_output', 'insufficient_topup', 'ambiguous_attempt',
        'provider_response_unusable', 'provider_attempt_unresolved',
        'delivery_prepared', 'delivery_completed', 'delivery_failed',
        'guardrail_withheld', 'response_too_large',
        'response_cache_delivery_prepared', 'response_cache_delivery_completed',
        'response_cache_delivery_failed'
    )
);

DROP INDEX idx_freeze_idempotency_settlement_recovery;
CREATE INDEX idx_freeze_idempotency_settlement_recovery
    ON freeze_idempotency(settlement_next_attempt_at, settlement_payload_ready_at, created_at)
    WHERE status='settlement_pending' AND settlement_payload_ready_at IS NOT NULL
      AND settlement_pending_reason IN (
          'billable_output','delivery_completed',
          'response_cache_delivery_completed','response_cache_delivery_failed'
      );

COMMIT;
