BEGIN;

CREATE TABLE enterprise_finance_rate_card_versions (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    rate_card_ref VARCHAR(160) NOT NULL,
    version BIGINT NOT NULL,
    provider_ref VARCHAR(160) NOT NULL,
    model_ref VARCHAR(255) NOT NULL,
    layer VARCHAR(40) NOT NULL,
    state VARCHAR(24) NOT NULL,
    effective_from TIMESTAMPTZ(6) NOT NULL,
    effective_to TIMESTAMPTZ(6),
    source_evidence_ref VARCHAR(255) NOT NULL DEFAULT '',
    created_by VARCHAR(160) NOT NULL,
    approved_by VARCHAR(160) NOT NULL DEFAULT '',
    activated_by VARCHAR(160) NOT NULL DEFAULT '',
    supersedes_version BIGINT,
    activated_at TIMESTAMPTZ(6),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT enterprise_finance_rate_card_versions_scope_unique UNIQUE (id, org_id),
    CONSTRAINT enterprise_finance_rate_card_versions_unique UNIQUE (org_id, rate_card_ref, version),
    CONSTRAINT enterprise_finance_rate_card_versions_layer_check CHECK (
        layer IN ('provider_list', 'supplier_contracted', 'supplier_billed_effective', 'internal_showback', 'internal_chargeback')
    ),
    CONSTRAINT enterprise_finance_rate_card_versions_state_check
        CHECK (state IN ('draft', 'approved', 'active', 'retired')),
    CONSTRAINT enterprise_finance_rate_card_versions_window_check
        CHECK (effective_to IS NULL OR effective_to > effective_from),
    CONSTRAINT enterprise_finance_rate_card_versions_identity_check CHECK (
        rate_card_ref <> '' AND version > 0 AND provider_ref <> '' AND model_ref <> '' AND created_by <> ''
    ),
    CONSTRAINT enterprise_finance_rate_card_versions_approval_check CHECK (
        (state = 'draft' AND approved_by = '' AND activated_by = '' AND activated_at IS NULL)
        OR (state = 'approved' AND approved_by <> '' AND approved_by <> created_by AND activated_by = '' AND activated_at IS NULL)
        OR (state IN ('active', 'retired') AND approved_by <> '' AND approved_by <> created_by AND activated_by <> '' AND activated_at IS NOT NULL)
    ),
    CONSTRAINT enterprise_finance_rate_card_versions_supplier_evidence_check
        CHECK (layer <> 'supplier_billed_effective' OR source_evidence_ref <> ''),
    CONSTRAINT enterprise_finance_rate_card_versions_supersedes_check CHECK (
        (version = 1 AND supersedes_version IS NULL)
        OR (version > 1 AND supersedes_version = version - 1)
    )
);

CREATE TABLE enterprise_finance_rate_components (
    rate_card_version_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    meter VARCHAR(64) NOT NULL,
    unit_quantity NUMERIC(78,0) NOT NULL,
    amount_minor NUMERIC(78,0) NOT NULL,
    currency CHAR(3) NOT NULL,
    amount_scale BIGINT NOT NULL,
    component_hash VARCHAR(71) NOT NULL,
    PRIMARY KEY (rate_card_version_id, meter),
    CONSTRAINT enterprise_finance_rate_components_card_scope
        FOREIGN KEY (rate_card_version_id, org_id)
        REFERENCES enterprise_finance_rate_card_versions(id, org_id) ON DELETE CASCADE,
    CONSTRAINT enterprise_finance_rate_components_quantity_check CHECK (unit_quantity > 0),
    CONSTRAINT enterprise_finance_rate_components_amount_check
        CHECK (amount_minor >= 0 AND amount_scale > 0 AND amount_scale <= 1000000000),
    CONSTRAINT enterprise_finance_rate_components_currency_check CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT enterprise_finance_rate_components_hash_check
        CHECK (component_hash ~ '^sha256:[a-f0-9]{64}$')
);

CREATE TABLE enterprise_finance_pricing_snapshots (
    id BIGSERIAL PRIMARY KEY,
    snapshot_hash VARCHAR(71) NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    request_ref VARCHAR(160) NOT NULL,
    project_ref VARCHAR(160) NOT NULL,
    priced_at TIMESTAMPTZ(6) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT enterprise_finance_pricing_snapshots_request_unique UNIQUE (org_id, request_ref),
    CONSTRAINT enterprise_finance_pricing_snapshots_scope_unique UNIQUE (id, org_id),
    CONSTRAINT enterprise_finance_pricing_snapshots_hash_check
        CHECK (snapshot_hash ~ '^sha256:[a-f0-9]{64}$'),
    CONSTRAINT enterprise_finance_pricing_snapshots_identity_check
        CHECK (request_ref <> '' AND project_ref <> '')
);

CREATE TABLE enterprise_finance_pricing_bindings (
    pricing_snapshot_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    meter VARCHAR(64) NOT NULL,
    quantity NUMERIC(78,0) NOT NULL,
    rate_card_ref VARCHAR(160) NOT NULL,
    rate_card_version BIGINT NOT NULL,
    rate_component_hash VARCHAR(71) NOT NULL,
    charge_minor NUMERIC(78,0) NOT NULL,
    currency CHAR(3) NOT NULL,
    amount_scale BIGINT NOT NULL,
    PRIMARY KEY (pricing_snapshot_id, meter, rate_card_ref, rate_card_version),
    CONSTRAINT enterprise_finance_pricing_bindings_snapshot_scope
        FOREIGN KEY (pricing_snapshot_id, org_id)
        REFERENCES enterprise_finance_pricing_snapshots(id, org_id) ON DELETE CASCADE,
    CONSTRAINT enterprise_finance_pricing_bindings_quantity_check CHECK (quantity >= 0),
    CONSTRAINT enterprise_finance_pricing_bindings_rate_check
        CHECK (rate_card_ref <> '' AND rate_card_version > 0 AND rate_component_hash ~ '^sha256:[a-f0-9]{64}$'),
    CONSTRAINT enterprise_finance_pricing_bindings_charge_check
        CHECK (charge_minor >= 0 AND amount_scale > 0 AND amount_scale <= 1000000000),
    CONSTRAINT enterprise_finance_pricing_bindings_currency_check CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE INDEX enterprise_finance_rate_cards_latest_idx
    ON enterprise_finance_rate_card_versions (org_id, rate_card_ref, version DESC);
CREATE INDEX enterprise_finance_rate_cards_activation_idx
    ON enterprise_finance_rate_card_versions (org_id, provider_ref, model_ref, layer, state, effective_from, effective_to);
CREATE INDEX enterprise_finance_rate_components_meter_idx
    ON enterprise_finance_rate_components (org_id, meter, component_hash);
CREATE INDEX enterprise_finance_pricing_snapshots_time_idx
    ON enterprise_finance_pricing_snapshots (org_id, priced_at DESC, id DESC);

COMMIT;
