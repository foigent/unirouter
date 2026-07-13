BEGIN;

-- P6-F10 stores only versioned policy configuration. Runtime findings are
-- emitted as bounded metadata; prompts and responses never enter this table.
CREATE UNIQUE INDEX idx_projects_guardrail_scope
    ON projects (id, org_id);
CREATE UNIQUE INDEX idx_api_keys_guardrail_org_scope
    ON api_keys (id, org_id);
CREATE UNIQUE INDEX idx_api_keys_guardrail_project_scope
    ON api_keys (id, org_id, project_id);

CREATE TABLE guardrail_policies (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT REFERENCES orgs(id) ON DELETE CASCADE,
    project_id BIGINT REFERENCES projects(id) ON DELETE CASCADE,
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    model VARCHAR(200) NOT NULL DEFAULT '',
    stage VARCHAR(32) NOT NULL CHECK (stage IN ('input', 'nonstream_output')),
    detector_ref VARCHAR(120) NOT NULL,
    action VARCHAR(16) NOT NULL CHECK (action IN ('audit', 'redact', 'block')),
    mode VARCHAR(16) NOT NULL DEFAULT 'shadow'
        CHECK (mode IN ('disabled', 'shadow', 'enforce')),
    failure_mode VARCHAR(24) NOT NULL DEFAULT 'degraded_allow'
        CHECK (failure_mode IN ('fail_closed', 'degraded_allow')),
    mandatory BOOLEAN NOT NULL DEFAULT FALSE,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT guardrail_policy_name_nonempty CHECK (length(btrim(name)) > 0),
    CONSTRAINT guardrail_policy_detector_nonempty CHECK (length(btrim(detector_ref)) > 0),
    CONSTRAINT guardrail_policy_config_object CHECK (jsonb_typeof(config) = 'object'),
    CONSTRAINT guardrail_policy_selector_scope CHECK (
        (project_id IS NULL OR org_id IS NOT NULL)
        AND (api_key_id IS NULL OR org_id IS NOT NULL)
    ),
    CONSTRAINT guardrail_policy_project_org_fk
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE CASCADE,
    CONSTRAINT guardrail_policy_key_org_fk
        FOREIGN KEY (api_key_id, org_id) REFERENCES api_keys(id, org_id) ON DELETE CASCADE,
    CONSTRAINT guardrail_policy_key_project_fk
        FOREIGN KEY (api_key_id, org_id, project_id) REFERENCES api_keys(id, org_id, project_id) ON DELETE CASCADE,
    CONSTRAINT guardrail_policy_mandatory_authority CHECK (
        NOT mandatory
        OR (org_id IS NULL AND project_id IS NULL AND api_key_id IS NULL
            AND mode = 'enforce' AND failure_mode = 'fail_closed')
    ),
    CONSTRAINT guardrail_policy_sensitive_redact CHECK (
        detector_ref <> 'builtin:sensitive_words' OR action <> 'redact'
    )
);

CREATE UNIQUE INDEX idx_guardrail_policy_scope_name
    ON guardrail_policies (COALESCE(org_id, 0), lower(name));
CREATE INDEX idx_guardrail_policy_runtime
    ON guardrail_policies (mode, stage, org_id, project_id, api_key_id, model, id);

-- A parseable provider response may be intentionally withheld by an output
-- guardrail. Keep authoritative usage and provider liability without creating
-- customer debt or pretending that local delivery occurred.
DROP INDEX idx_billing_provider_attempts_unresolved_key;
DROP INDEX idx_billing_provider_attempts_unresolved_channel;

ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_state_check,
    DROP CONSTRAINT billing_provider_attempts_state_certainty_check;

ALTER TABLE billing_provider_attempts
    ADD CONSTRAINT billing_provider_attempts_state_check CHECK (state IN (
        'dispatch_started', 'rejected', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending', 'delivery_pending',
        'delivery_failed', 'output_withheld', 'settled', 'reconciled'
    )),
    ADD CONSTRAINT billing_provider_attempts_state_certainty_check CHECK (
        (state IN ('dispatch_started', 'response_accepted') AND outcome_certainty = 'pending')
        OR (state = 'rejected' AND outcome_certainty = 'known_rejected')
        OR (state IN ('outcome_unknown', 'response_unusable') AND outcome_certainty = 'unknown')
        OR (state IN ('funding_pending', 'delivery_pending', 'delivery_failed', 'output_withheld', 'settled')
            AND outcome_certainty = 'known_usage')
        OR (state = 'reconciled' AND outcome_certainty IN ('unknown', 'known_usage'))
    );

CREATE INDEX idx_billing_provider_attempts_unresolved_key
    ON billing_provider_attempts(org_id, api_key_id, updated_at, request_id)
    WHERE state IN (
        'dispatch_started', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending', 'delivery_pending',
        'delivery_failed', 'output_withheld'
    );

CREATE INDEX idx_billing_provider_attempts_unresolved_channel
    ON billing_provider_attempts(channel_id, channel_key_id, updated_at, org_id, request_id)
    WHERE state IN (
        'dispatch_started', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending', 'delivery_pending',
        'delivery_failed', 'output_withheld'
    );

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_pending_reason_valid;

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_pending_reason_valid CHECK (
        settlement_pending_reason IN (
            '', 'billable_output', 'insufficient_topup', 'ambiguous_attempt',
            'provider_response_unusable', 'provider_attempt_unresolved',
            'delivery_prepared', 'delivery_completed', 'delivery_failed',
            'guardrail_withheld', 'response_too_large'
        )
    );

COMMIT;
