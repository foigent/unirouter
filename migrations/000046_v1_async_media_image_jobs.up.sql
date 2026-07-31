BEGIN;

CREATE TABLE async_media_jobs (
    job_id VARCHAR(48) PRIMARY KEY,
    contract_version VARCHAR(64) NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    client_request_id VARCHAR(255) NOT NULL DEFAULT '',
    operation VARCHAR(32) NOT NULL,
    profile_id VARCHAR(128) NOT NULL,
    requested_model VARCHAR(200) NOT NULL,
    routing_model VARCHAR(200) NOT NULL,
    mapped_model VARCHAR(200) NOT NULL DEFAULT '',
    request_sha256 VARCHAR(71) NOT NULL,
    idempotency_hash VARCHAR(80) NOT NULL,
    request_ciphertext TEXT,

    admission_state VARCHAR(24) NOT NULL,
    queue_state VARCHAR(24) NOT NULL,
    lease_state VARCHAR(24) NOT NULL,
    execution_state VARCHAR(24) NOT NULL,
    provider_outcome VARCHAR(24) NOT NULL,
    cancellation_state VARCHAR(24) NOT NULL,
    object_state VARCHAR(24) NOT NULL,
    settlement_state VARCHAR(32) NOT NULL,

    attempt INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    lease_owner VARCHAR(128) NOT NULL DEFAULT '',
    lease_fence BIGINT NOT NULL DEFAULT 0,
    lease_expires_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    object_key TEXT NOT NULL DEFAULT '',
    receipt_key TEXT NOT NULL DEFAULT '',
    output_sha256 VARCHAR(71) NOT NULL DEFAULT '',
    output_media_type VARCHAR(64) NOT NULL DEFAULT '',
    output_size_bytes BIGINT NOT NULL DEFAULT 0,
    output_persisted_at TIMESTAMPTZ,
    output_expires_at TIMESTAMPTZ,
    output_purged_at TIMESTAMPTZ,

    purge_owner VARCHAR(128) NOT NULL DEFAULT '',
    purge_fence BIGINT NOT NULL DEFAULT 0,
    purge_expires_at TIMESTAMPTZ,
    purge_attempt INTEGER NOT NULL DEFAULT 0,

    error_code VARCHAR(96) NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    terminal_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT async_media_jobs_project_scope
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE CASCADE,
    CONSTRAINT async_media_jobs_identity_unique
        UNIQUE (org_id, project_id, api_key_id, idempotency_hash),
    CONSTRAINT async_media_jobs_contract_check
        CHECK (contract_version = 'unirouter.async-media-runtime.v1'),
    CONSTRAINT async_media_jobs_operation_check
        CHECK (operation = 'text_to_image'),
    CONSTRAINT async_media_jobs_profile_check
        CHECK (profile_id = 'unirouter.openai.images.gpt-image.v1'),
    CONSTRAINT async_media_jobs_request_hash_check
        CHECK (request_sha256 ~ '^sha256:[a-f0-9]{64}$'),
    CONSTRAINT async_media_jobs_idempotency_hash_check
        CHECK (idempotency_hash ~ '^hmac-sha256:[a-f0-9]{64}$'),
    CONSTRAINT async_media_jobs_admission_check
        CHECK (admission_state = 'accepted'),
    CONSTRAINT async_media_jobs_queue_check
        CHECK (queue_state IN ('queued', 'leased', 'terminal')),
    CONSTRAINT async_media_jobs_lease_check
        CHECK (lease_state IN ('unleased', 'leased', 'released')),
    CONSTRAINT async_media_jobs_execution_check
        CHECK (execution_state IN ('pending', 'running', 'terminal')),
    CONSTRAINT async_media_jobs_outcome_check
        CHECK (provider_outcome IN ('pending', 'succeeded', 'failed', 'not_started', 'unknown')),
    CONSTRAINT async_media_jobs_cancellation_check
        CHECK (cancellation_state IN ('not_requested', 'requested', 'accepted', 'too_late', 'indeterminate')),
    CONSTRAINT async_media_jobs_object_check
        CHECK (object_state IN ('pending', 'persisted', 'purge_pending', 'purged', 'not_created')),
    CONSTRAINT async_media_jobs_settlement_check
        CHECK (settlement_state IN ('not_eligible', 'charge_fact_recorded')),
    CONSTRAINT async_media_jobs_attempt_check
        CHECK (attempt >= 0 AND max_attempts BETWEEN 1 AND 10 AND attempt <= max_attempts),
    CONSTRAINT async_media_jobs_fence_check
        CHECK (lease_fence >= 0 AND purge_fence >= 0 AND purge_attempt >= 0),
    CONSTRAINT async_media_jobs_lease_shape_check CHECK (
        (
            queue_state = 'leased'
            AND lease_state = 'leased'
            AND lease_owner <> ''
            AND lease_expires_at IS NOT NULL
            AND execution_state IN ('pending', 'running')
        )
        OR (
            queue_state <> 'leased'
            AND lease_state <> 'leased'
            AND lease_owner = ''
            AND lease_expires_at IS NULL
        )
    ),
    CONSTRAINT async_media_jobs_terminal_queue_shape_check
        CHECK ((execution_state = 'terminal') = (queue_state = 'terminal')),
    CONSTRAINT async_media_jobs_provider_shape_check CHECK (
        (
            provider_outcome = 'pending'
            AND execution_state <> 'terminal'
            AND mapped_model = ''
        )
        OR (
            provider_outcome = 'succeeded'
            AND mapped_model <> ''
            AND request_ciphertext IS NULL
        )
        OR (
            provider_outcome IN ('failed', 'not_started', 'unknown')
            AND execution_state = 'terminal'
            AND mapped_model = ''
        )
    ),
    CONSTRAINT async_media_jobs_cancellation_shape_check CHECK (
        cancellation_state NOT IN ('requested', 'accepted', 'indeterminate')
        OR (
            cancellation_state = 'requested'
            AND execution_state <> 'terminal'
            AND provider_outcome = 'pending'
        )
        OR (
            cancellation_state = 'accepted'
            AND execution_state = 'terminal'
        )
        OR (
            cancellation_state = 'indeterminate'
            AND execution_state = 'terminal'
            AND provider_outcome = 'unknown'
        )
    ),
    CONSTRAINT async_media_jobs_settlement_shape_check CHECK (
        (
            settlement_state = 'not_eligible'
            AND object_state IN ('pending', 'not_created')
        )
        OR (
            settlement_state = 'charge_fact_recorded'
            AND provider_outcome = 'succeeded'
            AND execution_state = 'terminal'
            AND object_state IN ('persisted', 'purge_pending', 'purged')
        )
    ),
    CONSTRAINT async_media_jobs_output_shape_check CHECK (
        (
            object_state IN ('pending', 'not_created')
            AND object_key = ''
            AND receipt_key = ''
            AND output_sha256 = ''
            AND output_media_type = ''
            AND output_size_bytes = 0
            AND output_persisted_at IS NULL
            AND output_expires_at IS NULL
            AND output_purged_at IS NULL
        )
        OR (
            object_state IN ('persisted', 'purge_pending')
            AND object_key <> ''
            AND receipt_key <> ''
            AND output_sha256 ~ '^sha256:[a-f0-9]{64}$'
            AND output_media_type IN ('image/png', 'image/jpeg', 'image/webp')
            AND output_size_bytes > 0
            AND output_persisted_at IS NOT NULL
            AND output_expires_at = output_persisted_at + INTERVAL '7 days'
            AND output_purged_at IS NULL
        )
        OR (
            object_state = 'purged'
            AND object_key = ''
            AND receipt_key = ''
            AND output_sha256 ~ '^sha256:[a-f0-9]{64}$'
            AND output_media_type IN ('image/png', 'image/jpeg', 'image/webp')
            AND output_size_bytes > 0
            AND output_persisted_at IS NOT NULL
            AND output_expires_at = output_persisted_at + INTERVAL '7 days'
            AND output_purged_at IS NOT NULL
        )
    ),
    CONSTRAINT async_media_jobs_terminal_shape_check CHECK (
        (
            execution_state <> 'terminal'
            AND terminal_at IS NULL
            AND (
                request_ciphertext IS NOT NULL
                OR (
                    provider_outcome = 'succeeded'
                    AND request_ciphertext IS NULL
                    AND object_state IN ('pending', 'not_created')
                    AND settlement_state = 'not_eligible'
                )
            )
        )
        OR (
            execution_state = 'terminal'
            AND terminal_at IS NOT NULL
            AND request_ciphertext IS NULL
            AND queue_state = 'terminal'
            AND lease_state = 'released'
        )
    ),
    CONSTRAINT async_media_jobs_success_shape_check CHECK (
        provider_outcome <> 'succeeded'
        OR (
            (
                execution_state <> 'terminal'
                AND object_state IN ('pending', 'not_created')
                AND settlement_state = 'not_eligible'
                AND request_ciphertext IS NULL
            )
            OR (
                execution_state = 'terminal'
                AND (
                    (
                        object_state IN ('persisted', 'purge_pending', 'purged')
                        AND settlement_state = 'charge_fact_recorded'
                    )
                    OR (
                        object_state = 'not_created'
                        AND settlement_state = 'not_eligible'
                        AND error_code <> ''
                    )
                )
            )
        )
    )
);

CREATE INDEX async_media_jobs_claim_idx
    ON async_media_jobs (next_attempt_at, created_at, job_id)
    WHERE execution_state <> 'terminal';
CREATE INDEX async_media_jobs_tenant_list_idx
    ON async_media_jobs (org_id, project_id, created_at DESC, job_id DESC);
CREATE INDEX async_media_jobs_purge_idx
    ON async_media_jobs (output_expires_at, purge_expires_at, job_id)
    WHERE object_state IN ('persisted', 'purge_pending');
CREATE INDEX async_media_jobs_request_idx
    ON async_media_jobs (org_id, project_id, request_id);

CREATE TABLE async_media_charge_facts (
    fact_id VARCHAR(56) PRIMARY KEY,
    job_id VARCHAR(48) NOT NULL UNIQUE REFERENCES async_media_jobs(job_id) ON DELETE CASCADE,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL,
    trigger VARCHAR(96) NOT NULL,
    amount_state VARCHAR(64) NOT NULL,
    amount_micros BIGINT,
    units JSONB NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT async_media_charge_project_scope
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE CASCADE,
    CONSTRAINT async_media_charge_trigger_check
        CHECK (trigger = 'provider_terminal_success_and_output_persisted'),
    CONSTRAINT async_media_charge_amount_check
        CHECK (
            amount_state = 'pricing_pending_media_rate_card'
            AND amount_micros IS NULL
        ),
    CONSTRAINT async_media_charge_units_check
        CHECK (
            jsonb_typeof(units) = 'array'
            AND jsonb_array_length(units) = 4
            AND units @> '[{"class":"input_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
            AND units @> '[{"class":"input_text_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
            AND units @> '[{"class":"input_image_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
            AND units @> '[{"class":"output_image_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
        )
);

CREATE INDEX async_media_charge_tenant_idx
    ON async_media_charge_facts (org_id, project_id, recorded_at DESC);

COMMIT;
