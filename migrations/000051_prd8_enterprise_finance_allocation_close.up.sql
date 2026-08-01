BEGIN;

CREATE TABLE enterprise_finance_allocation_runs (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    allocation_run_ref VARCHAR(160) NOT NULL,
    fiscal_period_ref VARCHAR(80) NOT NULL,
    source_revision VARCHAR(255) NOT NULL,
    rule_set_ref VARCHAR(160) NOT NULL,
    rule_set_revision VARCHAR(255) NOT NULL,
    input_hash VARCHAR(71) NOT NULL,
    supersedes_run_ref VARCHAR(160),
    state VARCHAR(24) NOT NULL DEFAULT 'completed',
    source_record_count BIGINT NOT NULL,
    result_count BIGINT NOT NULL,
    direct_count BIGINT NOT NULL,
    rule_based_count BIGINT NOT NULL,
    unallocated_count BIGINT NOT NULL,
    conflict_count BIGINT NOT NULL,
    source_total_minor NUMERIC(78,0) NOT NULL,
    allocated_minor NUMERIC(78,0) NOT NULL,
    unallocated_minor NUMERIC(78,0) NOT NULL,
    conflict_minor NUMERIC(78,0) NOT NULL,
    currency CHAR(3) NOT NULL,
    amount_scale BIGINT NOT NULL,
    created_by VARCHAR(160) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT enterprise_finance_allocation_runs_scope_unique UNIQUE (id, org_id),
    CONSTRAINT enterprise_finance_allocation_runs_identity_unique UNIQUE (org_id, allocation_run_ref),
    CONSTRAINT enterprise_finance_allocation_runs_period
        FOREIGN KEY (org_id, fiscal_period_ref)
        REFERENCES enterprise_finance_fiscal_periods(org_id, period_ref) ON DELETE RESTRICT,
    CONSTRAINT enterprise_finance_allocation_runs_supersedes
        FOREIGN KEY (org_id, supersedes_run_ref)
        REFERENCES enterprise_finance_allocation_runs(org_id, allocation_run_ref) ON DELETE RESTRICT,
    CONSTRAINT enterprise_finance_allocation_runs_identity_check CHECK (
        allocation_run_ref <> '' AND source_revision <> ''
        AND rule_set_ref <> '' AND rule_set_revision <> '' AND created_by <> ''
        AND input_hash ~ '^sha256:[a-f0-9]{64}$'
    ),
    CONSTRAINT enterprise_finance_allocation_runs_state_check CHECK (state = 'completed'),
    CONSTRAINT enterprise_finance_allocation_runs_count_check CHECK (
        source_record_count >= 0 AND result_count >= 0
        AND direct_count >= 0 AND rule_based_count >= 0
        AND unallocated_count >= 0 AND conflict_count >= 0
        AND result_count = direct_count + rule_based_count + unallocated_count + conflict_count
    ),
    CONSTRAINT enterprise_finance_allocation_runs_amount_check CHECK (
        source_total_minor >= 0 AND allocated_minor >= 0
        AND unallocated_minor >= 0 AND conflict_minor >= 0
        AND amount_scale > 0 AND amount_scale <= 1000000000
        AND source_total_minor = allocated_minor + unallocated_minor + conflict_minor
    ),
    CONSTRAINT enterprise_finance_allocation_runs_currency_check CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE TABLE enterprise_finance_allocation_results (
    allocation_run_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    request_ref VARCHAR(160) NOT NULL,
    source_revision VARCHAR(255) NOT NULL,
    state VARCHAR(24) NOT NULL,
    target_kind VARCHAR(32) NOT NULL DEFAULT '',
    target_ref VARCHAR(160) NOT NULL DEFAULT '',
    rule_ref VARCHAR(160) NOT NULL DEFAULT '',
    rule_revision VARCHAR(255) NOT NULL DEFAULT '',
    amount_minor NUMERIC(78,0) NOT NULL,
    currency CHAR(3) NOT NULL,
    amount_scale BIGINT NOT NULL,
    ledger_mutation VARCHAR(16) NOT NULL DEFAULT 'none',
    customer_debt_action VARCHAR(16) NOT NULL DEFAULT 'none',
    PRIMARY KEY (allocation_run_id, request_ref),
    CONSTRAINT enterprise_finance_allocation_results_run_scope
        FOREIGN KEY (allocation_run_id, org_id)
        REFERENCES enterprise_finance_allocation_runs(id, org_id) ON DELETE CASCADE,
    CONSTRAINT enterprise_finance_allocation_results_identity_check
        CHECK (request_ref <> '' AND source_revision <> ''),
    CONSTRAINT enterprise_finance_allocation_results_state_check
        CHECK (state IN ('direct', 'rule_based', 'unallocated', 'conflict')),
    CONSTRAINT enterprise_finance_allocation_results_target_check CHECK (
        (state IN ('direct', 'rule_based')
            AND target_kind IN ('legal_entity', 'business_unit', 'department', 'cost_center', 'project')
            AND target_ref <> '')
        OR (state IN ('unallocated', 'conflict') AND target_kind = '' AND target_ref = '')
    ),
    CONSTRAINT enterprise_finance_allocation_results_rule_check CHECK (
        (state = 'rule_based' AND rule_ref <> '' AND rule_revision <> '')
        OR (state <> 'rule_based' AND rule_ref = '' AND rule_revision = '')
    ),
    CONSTRAINT enterprise_finance_allocation_results_amount_check
        CHECK (amount_minor >= 0 AND amount_scale > 0 AND amount_scale <= 1000000000),
    CONSTRAINT enterprise_finance_allocation_results_currency_check CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT enterprise_finance_allocation_results_authority_check
        CHECK (ledger_mutation = 'none' AND customer_debt_action = 'none')
);

CREATE TABLE enterprise_finance_adjustment_journal_versions (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    journal_ref VARCHAR(160) NOT NULL,
    version BIGINT NOT NULL,
    fiscal_period_ref VARCHAR(80) NOT NULL,
    request_ref VARCHAR(160) NOT NULL,
    entry_type VARCHAR(24) NOT NULL,
    reversal_of_journal_ref VARCHAR(160) NOT NULL DEFAULT '',
    target_kind VARCHAR(32) NOT NULL,
    target_ref VARCHAR(160) NOT NULL,
    amount_minor NUMERIC(78,0) NOT NULL,
    currency CHAR(3) NOT NULL,
    amount_scale BIGINT NOT NULL,
    reason TEXT NOT NULL,
    source_allocation_run_ref VARCHAR(160) NOT NULL,
    state VARCHAR(24) NOT NULL,
    ledger_mutation VARCHAR(16) NOT NULL DEFAULT 'none',
    customer_debt_action VARCHAR(16) NOT NULL DEFAULT 'none',
    gl_export_state VARCHAR(24) NOT NULL DEFAULT 'not_configured',
    created_by VARCHAR(160) NOT NULL,
    approved_by VARCHAR(160) NOT NULL DEFAULT '',
    supersedes_version BIGINT,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT enterprise_finance_adjustment_journal_versions_unique
        UNIQUE (org_id, journal_ref, version),
    CONSTRAINT enterprise_finance_adjustment_journal_versions_period
        FOREIGN KEY (org_id, fiscal_period_ref)
        REFERENCES enterprise_finance_fiscal_periods(org_id, period_ref) ON DELETE RESTRICT,
    CONSTRAINT enterprise_finance_adjustment_journal_versions_source_run
        FOREIGN KEY (org_id, source_allocation_run_ref)
        REFERENCES enterprise_finance_allocation_runs(org_id, allocation_run_ref) ON DELETE RESTRICT,
    CONSTRAINT enterprise_finance_adjustment_journal_versions_identity_check CHECK (
        journal_ref <> '' AND version > 0 AND request_ref <> ''
        AND target_ref <> '' AND reason <> '' AND source_allocation_run_ref <> '' AND created_by <> ''
    ),
    CONSTRAINT enterprise_finance_adjustment_journal_versions_type_check CHECK (
        (entry_type = 'adjustment' AND reversal_of_journal_ref = '')
        OR (entry_type = 'reversal' AND reversal_of_journal_ref <> '')
    ),
    CONSTRAINT enterprise_finance_adjustment_journal_versions_target_check
        CHECK (target_kind IN ('legal_entity', 'business_unit', 'department', 'cost_center', 'project')),
    CONSTRAINT enterprise_finance_adjustment_journal_versions_amount_check
        CHECK (amount_minor >= 0 AND amount_scale > 0 AND amount_scale <= 1000000000),
    CONSTRAINT enterprise_finance_adjustment_journal_versions_currency_check CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT enterprise_finance_adjustment_journal_versions_state_check
        CHECK (state IN ('draft', 'approved')),
    CONSTRAINT enterprise_finance_adjustment_journal_versions_approval_check CHECK (
        (state = 'draft' AND approved_by = '')
        OR (state = 'approved' AND approved_by <> '' AND approved_by <> created_by)
    ),
    CONSTRAINT enterprise_finance_adjustment_journal_versions_supersedes_check CHECK (
        (version = 1 AND supersedes_version IS NULL)
        OR (version > 1 AND supersedes_version = version - 1)
    ),
    CONSTRAINT enterprise_finance_adjustment_journal_versions_authority_check CHECK (
        ledger_mutation = 'none' AND customer_debt_action = 'none' AND gl_export_state = 'not_configured'
    )
);

CREATE INDEX enterprise_finance_allocation_runs_period_idx
    ON enterprise_finance_allocation_runs (org_id, fiscal_period_ref, created_at DESC, id DESC);
CREATE INDEX enterprise_finance_allocation_results_state_idx
    ON enterprise_finance_allocation_results (org_id, state, allocation_run_id, request_ref);
CREATE INDEX enterprise_finance_adjustment_journals_latest_idx
    ON enterprise_finance_adjustment_journal_versions (org_id, fiscal_period_ref, journal_ref, version DESC);

COMMIT;
