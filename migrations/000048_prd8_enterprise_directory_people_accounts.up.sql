BEGIN;

CREATE TABLE enterprise_directory_person_snapshots (
    snapshot_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    source_id BIGINT NOT NULL,
    person_ref VARCHAR(160) NOT NULL,
    external_ref VARCHAR(255) NOT NULL,
    employment_status VARCHAR(24) NOT NULL,
    primary_unit_ref VARCHAR(160) NOT NULL DEFAULT '',
    manager_person_ref VARCHAR(160) NOT NULL DEFAULT '',
    cost_center_ref VARCHAR(160) NOT NULL DEFAULT '',
    source_revision VARCHAR(255) NOT NULL,
    valid_from TIMESTAMPTZ(6) NOT NULL,
    valid_to TIMESTAMPTZ(6),
    PRIMARY KEY (snapshot_id, person_ref),
    CONSTRAINT enterprise_directory_person_snapshots_scope
        FOREIGN KEY (snapshot_id, org_id, source_id)
        REFERENCES enterprise_directory_snapshots(id, org_id, source_id) ON DELETE CASCADE,
    CONSTRAINT enterprise_directory_person_snapshots_external_unique
        UNIQUE (snapshot_id, external_ref),
    CONSTRAINT enterprise_directory_person_snapshots_status_check
        CHECK (employment_status IN ('active', 'leave', 'terminated')),
    CONSTRAINT enterprise_directory_person_snapshots_identity_check
        CHECK (person_ref <> '' AND external_ref <> '' AND source_revision <> ''),
    CONSTRAINT enterprise_directory_person_snapshots_manager_check
        CHECK (manager_person_ref = '' OR manager_person_ref <> person_ref),
    CONSTRAINT enterprise_directory_person_snapshots_window_check
        CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE TABLE enterprise_directory_account_snapshots (
    snapshot_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    source_id BIGINT NOT NULL,
    account_ref VARCHAR(160) NOT NULL,
    external_subject_ref VARCHAR(255) NOT NULL,
    person_ref VARCHAR(160) NOT NULL DEFAULT '',
    local_user_id BIGINT,
    authentication_state VARCHAR(24) NOT NULL,
    source_revision VARCHAR(255) NOT NULL,
    valid_from TIMESTAMPTZ(6) NOT NULL,
    valid_to TIMESTAMPTZ(6),
    PRIMARY KEY (snapshot_id, account_ref),
    CONSTRAINT enterprise_directory_account_snapshots_scope
        FOREIGN KEY (snapshot_id, org_id, source_id)
        REFERENCES enterprise_directory_snapshots(id, org_id, source_id) ON DELETE CASCADE,
    CONSTRAINT enterprise_directory_account_snapshots_external_unique
        UNIQUE (snapshot_id, external_subject_ref),
    CONSTRAINT enterprise_directory_account_snapshots_state_check
        CHECK (authentication_state IN ('active', 'disabled')),
    CONSTRAINT enterprise_directory_account_snapshots_identity_check
        CHECK (account_ref <> '' AND external_subject_ref <> '' AND source_revision <> ''),
    CONSTRAINT enterprise_directory_account_snapshots_binding_check
        CHECK (local_user_id IS NULL OR (local_user_id > 0 AND person_ref <> '')),
    CONSTRAINT enterprise_directory_account_snapshots_window_check
        CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE TABLE enterprise_directory_iam_actions (
    id BIGSERIAL PRIMARY KEY,
    snapshot_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    source_id BIGINT NOT NULL,
    subject_kind VARCHAR(24) NOT NULL,
    subject_ref VARCHAR(160) NOT NULL,
    person_ref VARCHAR(160) NOT NULL DEFAULT '',
    account_ref VARCHAR(160) NOT NULL DEFAULT '',
    local_user_id BIGINT,
    action VARCHAR(32) NOT NULL,
    reason VARCHAR(64) NOT NULL,
    result VARCHAR(24) NOT NULL,
    before_status VARCHAR(24) NOT NULL DEFAULT '',
    after_status VARCHAR(24) NOT NULL DEFAULT '',
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    applied_at TIMESTAMPTZ(6),
    CONSTRAINT enterprise_directory_iam_actions_snapshot_scope
        FOREIGN KEY (snapshot_id, org_id, source_id)
        REFERENCES enterprise_directory_snapshots(id, org_id, source_id) ON DELETE CASCADE,
    CONSTRAINT enterprise_directory_iam_actions_unique
        UNIQUE (snapshot_id, subject_kind, subject_ref, action),
    CONSTRAINT enterprise_directory_iam_actions_subject_check
        CHECK (subject_kind IN ('person', 'account')),
    CONSTRAINT enterprise_directory_iam_actions_action_check
        CHECK (action IN ('no_change', 'review_required', 'disable_user')),
    CONSTRAINT enterprise_directory_iam_actions_result_check
        CHECK (result IN ('recorded', 'applied', 'noop', 'conflict')),
    CONSTRAINT enterprise_directory_iam_actions_identity_check
        CHECK (subject_ref <> '' AND reason <> '' AND (local_user_id IS NULL OR local_user_id > 0)),
    CONSTRAINT enterprise_directory_iam_actions_detail_check
        CHECK (jsonb_typeof(detail) = 'object')
);

CREATE INDEX enterprise_directory_people_effective_idx
    ON enterprise_directory_person_snapshots (org_id, source_id, valid_from, valid_to, person_ref);
CREATE INDEX enterprise_directory_accounts_effective_idx
    ON enterprise_directory_account_snapshots (org_id, source_id, valid_from, valid_to, account_ref);
CREATE INDEX enterprise_directory_accounts_person_idx
    ON enterprise_directory_account_snapshots (org_id, person_ref, local_user_id)
    WHERE person_ref <> '';
CREATE INDEX enterprise_directory_iam_actions_org_time_idx
    ON enterprise_directory_iam_actions (org_id, created_at DESC, id DESC);

COMMIT;
