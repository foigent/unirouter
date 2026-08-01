BEGIN;

ALTER TABLE channels
    DROP CONSTRAINT IF EXISTS channels_runtime_status_check;

ALTER TABLE channels
    ADD CONSTRAINT channels_runtime_status_check
        CHECK (status IN ('active', 'staged', 'disabled'));

CREATE TABLE runtime_change_releases (
    id BIGSERIAL PRIMARY KEY,
    release_ref VARCHAR(80) NOT NULL UNIQUE,
    model_name VARCHAR(160) NOT NULL,
    candidate_version VARCHAR(80) NOT NULL,
    candidate_hash VARCHAR(71) NOT NULL,
    baseline_hash VARCHAR(71) NOT NULL,
    candidate JSONB NOT NULL,
    provider_pack_pins JSONB NOT NULL,
    simulation_report JSONB NOT NULL,
    overall_risk VARCHAR(24) NOT NULL,
    stage VARCHAR(24) NOT NULL DEFAULT 'draft',
    version BIGINT NOT NULL DEFAULT 1,
    parent_stable_release_id BIGINT REFERENCES runtime_change_releases(id) ON DELETE RESTRICT,
    canary_basis_points INTEGER NOT NULL DEFAULT 0,
    reason TEXT NOT NULL,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    validated_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    activated_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    validated_at TIMESTAMPTZ(6),
    activated_at TIMESTAMPTZ(6),
    rolled_back_at TIMESTAMPTZ(6),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT runtime_change_releases_identity_check CHECK (
        release_ref ~ '^rcr_[a-z0-9][a-z0-9._-]{2,63}$'
        AND model_name <> ''
        AND candidate_version = 'unirouter.runtime-change-candidate.v1'
        AND candidate_hash ~ '^sha256:[a-f0-9]{64}$'
        AND baseline_hash ~ '^sha256:[a-f0-9]{64}$'
        AND jsonb_typeof(candidate) = 'object'
        AND jsonb_typeof(provider_pack_pins) = 'array'
        AND jsonb_typeof(simulation_report) = 'object'
        AND reason <> ''
    ),
    CONSTRAINT runtime_change_releases_risk_check CHECK (
        overall_risk IN ('unchanged', 'compatible', 'review_required', 'breaking')
    ),
    CONSTRAINT runtime_change_releases_stage_check CHECK (
        stage IN ('draft', 'validated', 'shadow', 'canary', 'active', 'rolled_back', 'rejected')
    ),
    CONSTRAINT runtime_change_releases_version_check CHECK (version > 0),
    CONSTRAINT runtime_change_releases_canary_check CHECK (
        (stage = 'canary' AND canary_basis_points BETWEEN 1 AND 5000)
        OR (stage <> 'canary' AND canary_basis_points = 0)
    ),
    CONSTRAINT runtime_change_releases_lifecycle_check CHECK (
        (stage = 'draft' AND validated_by IS NULL AND activated_by IS NULL AND validated_at IS NULL AND activated_at IS NULL AND rolled_back_at IS NULL)
        OR (stage IN ('validated', 'shadow') AND validated_by IS NOT NULL AND validated_at IS NOT NULL AND activated_by IS NULL AND activated_at IS NULL AND rolled_back_at IS NULL)
        OR (stage IN ('canary', 'active') AND validated_by IS NOT NULL AND validated_at IS NOT NULL AND activated_by IS NOT NULL AND activated_at IS NOT NULL AND rolled_back_at IS NULL)
        OR (stage = 'rolled_back' AND validated_by IS NOT NULL AND validated_at IS NOT NULL AND rolled_back_at IS NOT NULL)
        OR (stage = 'rejected' AND activated_by IS NULL AND activated_at IS NULL)
    )
);

CREATE TABLE runtime_change_slots (
    model_name VARCHAR(160) PRIMARY KEY,
    stable_release_id BIGINT REFERENCES runtime_change_releases(id) ON DELETE RESTRICT,
    canary_release_id BIGINT REFERENCES runtime_change_releases(id) ON DELETE RESTRICT,
    canary_basis_points INTEGER NOT NULL DEFAULT 0,
    slot_version BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT runtime_change_slots_canary_check CHECK (
        (canary_release_id IS NULL AND canary_basis_points = 0)
        OR (canary_release_id IS NOT NULL AND canary_basis_points BETWEEN 1 AND 5000)
    ),
    CONSTRAINT runtime_change_slots_release_distinct CHECK (
        stable_release_id IS NULL OR canary_release_id IS NULL OR stable_release_id <> canary_release_id
    ),
    CONSTRAINT runtime_change_slots_version_check CHECK (slot_version > 0)
);

CREATE TABLE runtime_change_events (
    id BIGSERIAL PRIMARY KEY,
    release_id BIGINT NOT NULL REFERENCES runtime_change_releases(id) ON DELETE RESTRICT,
    sequence BIGINT NOT NULL,
    action VARCHAR(32) NOT NULL,
    from_stage VARCHAR(24) NOT NULL,
    to_stage VARCHAR(24) NOT NULL,
    actor_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    expected_version BIGINT NOT NULL,
    resulting_version BIGINT NOT NULL,
    detail JSONB NOT NULL DEFAULT '{}',
    occurred_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT runtime_change_events_unique UNIQUE (release_id, sequence),
    CONSTRAINT runtime_change_events_action_check CHECK (
        action IN ('create', 'validate', 'advance', 'promote', 'rollback', 'reject')
    ),
    CONSTRAINT runtime_change_events_stage_check CHECK (
        from_stage IN ('none', 'draft', 'validated', 'shadow', 'canary', 'active', 'rolled_back', 'rejected')
        AND to_stage IN ('draft', 'validated', 'shadow', 'canary', 'active', 'rolled_back', 'rejected')
    ),
    CONSTRAINT runtime_change_events_version_check CHECK (
        sequence > 0 AND expected_version >= 0 AND resulting_version > 0
    ),
    CONSTRAINT runtime_change_events_detail_check CHECK (jsonb_typeof(detail) = 'object')
);

CREATE INDEX runtime_change_releases_model_stage_idx
    ON runtime_change_releases (model_name, stage, updated_at DESC, id DESC);
CREATE INDEX runtime_change_events_release_idx
    ON runtime_change_events (release_id, sequence DESC);

COMMIT;
