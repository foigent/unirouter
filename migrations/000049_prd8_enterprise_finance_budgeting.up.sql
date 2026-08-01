BEGIN;

CREATE TABLE enterprise_finance_fiscal_periods (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    period_ref VARCHAR(80) NOT NULL,
    start_at TIMESTAMPTZ(6) NOT NULL,
    end_at TIMESTAMPTZ(6) NOT NULL,
    state VARCHAR(24) NOT NULL DEFAULT 'open',
    version BIGINT NOT NULL DEFAULT 1,
    created_by VARCHAR(160) NOT NULL,
    state_changed_by VARCHAR(160) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT enterprise_finance_fiscal_periods_identity_unique UNIQUE (org_id, period_ref),
    CONSTRAINT enterprise_finance_fiscal_periods_scope_unique UNIQUE (id, org_id),
    CONSTRAINT enterprise_finance_fiscal_periods_window_check CHECK (end_at > start_at),
    CONSTRAINT enterprise_finance_fiscal_periods_state_check
        CHECK (state IN ('open', 'preliminary', 'reconciled', 'closed')),
    CONSTRAINT enterprise_finance_fiscal_periods_version_check CHECK (version > 0),
    CONSTRAINT enterprise_finance_fiscal_periods_actor_check
        CHECK (period_ref <> '' AND created_by <> '' AND state_changed_by <> '')
);

CREATE TABLE enterprise_finance_period_events (
    id BIGSERIAL PRIMARY KEY,
    period_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    from_state VARCHAR(24) NOT NULL,
    to_state VARCHAR(24) NOT NULL,
    version BIGINT NOT NULL,
    actor_ref VARCHAR(160) NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT enterprise_finance_period_events_period_scope
        FOREIGN KEY (period_id, org_id)
        REFERENCES enterprise_finance_fiscal_periods(id, org_id) ON DELETE CASCADE,
    CONSTRAINT enterprise_finance_period_events_unique UNIQUE (period_id, version),
    CONSTRAINT enterprise_finance_period_events_state_check CHECK (
        from_state IN ('none', 'open', 'preliminary', 'reconciled', 'closed')
        AND to_state IN ('open', 'preliminary', 'reconciled', 'closed')
    ),
    CONSTRAINT enterprise_finance_period_events_identity_check
        CHECK (version > 0 AND actor_ref <> '')
);

CREATE TABLE enterprise_finance_cost_center_versions (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    cost_center_ref VARCHAR(160) NOT NULL,
    version BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    legal_entity_ref VARCHAR(160) NOT NULL,
    owner_unit_ref VARCHAR(160) NOT NULL DEFAULT '',
    source_ref VARCHAR(160) NOT NULL,
    source_revision VARCHAR(255) NOT NULL,
    valid_from TIMESTAMPTZ(6) NOT NULL,
    valid_to TIMESTAMPTZ(6),
    created_by VARCHAR(160) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT enterprise_finance_cost_center_versions_unique
        UNIQUE (org_id, cost_center_ref, version),
    CONSTRAINT enterprise_finance_cost_center_versions_identity_check CHECK (
        cost_center_ref <> '' AND version > 0 AND name <> '' AND legal_entity_ref <> ''
        AND source_ref <> '' AND source_revision <> '' AND created_by <> ''
    ),
    CONSTRAINT enterprise_finance_cost_center_versions_window_check
        CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE TABLE enterprise_finance_budget_versions (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    budget_ref VARCHAR(160) NOT NULL,
    version BIGINT NOT NULL,
    scope_kind VARCHAR(32) NOT NULL,
    scope_ref VARCHAR(160) NOT NULL,
    fiscal_period_ref VARCHAR(80) NOT NULL,
    amount_minor NUMERIC(78,0) NOT NULL,
    currency CHAR(3) NOT NULL,
    amount_scale BIGINT NOT NULL,
    state VARCHAR(24) NOT NULL,
    runtime_effect VARCHAR(24) NOT NULL DEFAULT 'report_only',
    created_by VARCHAR(160) NOT NULL,
    approved_by VARCHAR(160) NOT NULL DEFAULT '',
    supersedes_version BIGINT,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT enterprise_finance_budget_versions_period
        FOREIGN KEY (org_id, fiscal_period_ref)
        REFERENCES enterprise_finance_fiscal_periods(org_id, period_ref) ON DELETE RESTRICT,
    CONSTRAINT enterprise_finance_budget_versions_unique UNIQUE (org_id, budget_ref, version),
    CONSTRAINT enterprise_finance_budget_versions_scope_period_unique
        UNIQUE (org_id, scope_kind, scope_ref, fiscal_period_ref, version),
    CONSTRAINT enterprise_finance_budget_versions_scope_check
        CHECK (scope_kind IN ('legal_entity', 'business_unit', 'department', 'cost_center', 'project')),
    CONSTRAINT enterprise_finance_budget_versions_amount_check
        CHECK (amount_minor >= 0 AND amount_scale > 0 AND amount_scale <= 1000000000),
    CONSTRAINT enterprise_finance_budget_versions_currency_check
        CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT enterprise_finance_budget_versions_state_check
        CHECK (state IN ('draft', 'approved', 'closed')),
    CONSTRAINT enterprise_finance_budget_versions_effect_check
        CHECK (runtime_effect = 'report_only'),
    CONSTRAINT enterprise_finance_budget_versions_identity_check CHECK (
        budget_ref <> '' AND version > 0 AND scope_ref <> '' AND fiscal_period_ref <> '' AND created_by <> ''
    ),
    CONSTRAINT enterprise_finance_budget_versions_approval_check CHECK (
        (state = 'draft' AND approved_by = '')
        OR (state IN ('approved', 'closed') AND approved_by <> '' AND approved_by <> created_by)
    ),
    CONSTRAINT enterprise_finance_budget_versions_supersedes_check CHECK (
        (version = 1 AND supersedes_version IS NULL)
        OR (version > 1 AND supersedes_version = version - 1)
    )
);

CREATE INDEX enterprise_finance_fiscal_periods_org_time_idx
    ON enterprise_finance_fiscal_periods (org_id, start_at, end_at);
CREATE INDEX enterprise_finance_period_events_org_time_idx
    ON enterprise_finance_period_events (org_id, created_at DESC, id DESC);
CREATE INDEX enterprise_finance_cost_centers_effective_idx
    ON enterprise_finance_cost_center_versions (org_id, valid_from, valid_to, cost_center_ref, version DESC);
CREATE INDEX enterprise_finance_budgets_latest_idx
    ON enterprise_finance_budget_versions (org_id, budget_ref, version DESC);
CREATE INDEX enterprise_finance_budgets_scope_idx
    ON enterprise_finance_budget_versions (org_id, fiscal_period_ref, scope_kind, scope_ref, version DESC);

COMMIT;
