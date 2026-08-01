BEGIN;

CREATE TABLE enterprise_directory_sources (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    contract_version VARCHAR(64) NOT NULL,
    source_ref VARCHAR(160) NOT NULL,
    authority VARCHAR(16) NOT NULL,
    connection VARCHAR(24) NOT NULL,
    current_snapshot_ref VARCHAR(71) NOT NULL DEFAULT '',
    current_revision VARCHAR(255) NOT NULL DEFAULT '',
    observed_at TIMESTAMPTZ(6),
    reconciled_at TIMESTAMPTZ(6),
    has_conflict BOOLEAN NOT NULL DEFAULT FALSE,
    gaps JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT enterprise_directory_sources_identity_unique UNIQUE (org_id, source_ref),
    CONSTRAINT enterprise_directory_sources_scope_unique UNIQUE (id, org_id),
    CONSTRAINT enterprise_directory_sources_contract_check
        CHECK (contract_version = 'unirouter.enterprise-directory-projection.v1'),
    CONSTRAINT enterprise_directory_sources_authority_check
        CHECK (authority IN ('hcm', 'idp')),
    CONSTRAINT enterprise_directory_sources_connection_check
        CHECK (connection IN ('unconfigured', 'connected', 'disabled', 'error')),
    CONSTRAINT enterprise_directory_sources_snapshot_ref_check
        CHECK (current_snapshot_ref = '' OR current_snapshot_ref ~ '^sha256:[a-f0-9]{64}$'),
    CONSTRAINT enterprise_directory_sources_observation_shape_check CHECK (
        (current_revision = '' AND current_snapshot_ref = '' AND observed_at IS NULL)
        OR
        (current_revision <> '' AND current_snapshot_ref <> '' AND observed_at IS NOT NULL)
    ),
    CONSTRAINT enterprise_directory_sources_reconciliation_time_check
        CHECK (reconciled_at IS NULL OR observed_at IS NOT NULL),
    CONSTRAINT enterprise_directory_sources_gaps_check
        CHECK (jsonb_typeof(gaps) = 'array')
);

CREATE TABLE enterprise_directory_snapshots (
    id BIGSERIAL PRIMARY KEY,
    snapshot_ref VARCHAR(71) NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    source_id BIGINT NOT NULL,
    kind VARCHAR(16) NOT NULL,
    source_revision VARCHAR(255) NOT NULL,
    previous_revision VARCHAR(255) NOT NULL DEFAULT '',
    observed_at TIMESTAMPTZ(6) NOT NULL,
    gaps JSONB NOT NULL DEFAULT '[]'::jsonb,
    applied_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT enterprise_directory_snapshots_source_scope
        FOREIGN KEY (source_id, org_id)
        REFERENCES enterprise_directory_sources(id, org_id) ON DELETE CASCADE,
    CONSTRAINT enterprise_directory_snapshots_ref_unique UNIQUE (org_id, snapshot_ref),
    CONSTRAINT enterprise_directory_snapshots_revision_unique
        UNIQUE (org_id, source_id, source_revision),
    CONSTRAINT enterprise_directory_snapshots_scope_unique UNIQUE (id, org_id, source_id),
    CONSTRAINT enterprise_directory_snapshots_ref_check
        CHECK (snapshot_ref ~ '^sha256:[a-f0-9]{64}$'),
    CONSTRAINT enterprise_directory_snapshots_kind_check
        CHECK (kind IN ('full', 'delta')),
    CONSTRAINT enterprise_directory_snapshots_revision_check
        CHECK (source_revision <> '' AND source_revision <> previous_revision),
    CONSTRAINT enterprise_directory_snapshots_delta_parent_check
        CHECK (kind <> 'delta' OR previous_revision <> ''),
    CONSTRAINT enterprise_directory_snapshots_gaps_check
        CHECK (jsonb_typeof(gaps) = 'array')
);

CREATE TABLE enterprise_directory_unit_snapshots (
    snapshot_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    source_id BIGINT NOT NULL,
    unit_ref VARCHAR(160) NOT NULL,
    external_ref VARCHAR(255) NOT NULL,
    parent_unit_ref VARCHAR(160) NOT NULL DEFAULT '',
    kind VARCHAR(32) NOT NULL,
    name VARCHAR(255) NOT NULL,
    source_revision VARCHAR(255) NOT NULL,
    valid_from TIMESTAMPTZ(6) NOT NULL,
    valid_to TIMESTAMPTZ(6),
    PRIMARY KEY (snapshot_id, unit_ref),
    CONSTRAINT enterprise_directory_unit_snapshots_scope
        FOREIGN KEY (snapshot_id, org_id, source_id)
        REFERENCES enterprise_directory_snapshots(id, org_id, source_id) ON DELETE CASCADE,
    CONSTRAINT enterprise_directory_unit_snapshots_external_unique
        UNIQUE (snapshot_id, external_ref),
    CONSTRAINT enterprise_directory_unit_snapshots_kind_check
        CHECK (kind IN ('legal_entity', 'business_unit', 'department', 'team')),
    CONSTRAINT enterprise_directory_unit_snapshots_identity_check
        CHECK (unit_ref <> '' AND external_ref <> '' AND name <> '' AND source_revision <> ''),
    CONSTRAINT enterprise_directory_unit_snapshots_parent_check
        CHECK (parent_unit_ref = '' OR parent_unit_ref <> unit_ref),
    CONSTRAINT enterprise_directory_unit_snapshots_window_check
        CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE INDEX enterprise_directory_sources_org_health_idx
    ON enterprise_directory_sources (org_id, authority, connection, updated_at DESC);
CREATE INDEX enterprise_directory_snapshots_source_time_idx
    ON enterprise_directory_snapshots (org_id, source_id, observed_at DESC, id DESC);
CREATE INDEX enterprise_directory_units_effective_idx
    ON enterprise_directory_unit_snapshots (org_id, source_id, valid_from, valid_to, unit_ref);

COMMIT;
