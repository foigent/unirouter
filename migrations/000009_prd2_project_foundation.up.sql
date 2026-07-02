-- Migration 000009: PRD2 project/workspace foundation
-- Adds project-scoped ownership without breaking existing org-scoped PRD1 data.

BEGIN;

CREATE TABLE IF NOT EXISTS projects (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    slug VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled', 'archived')),
    budget_cents INTEGER,
    budget_period VARCHAR(20) NOT NULL DEFAULT 'monthly'
        CHECK (budget_period IN ('none', 'daily', 'monthly')),
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, slug)
);

CREATE INDEX IF NOT EXISTS idx_projects_org_id ON projects(org_id);
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);

CREATE TABLE IF NOT EXISTS project_members (
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(50) NOT NULL
        CHECK (role IN ('project_admin', 'project_developer', 'project_viewer')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_project_members_user_id ON project_members(user_id);

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS project_id BIGINT REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE call_logs ADD COLUMN IF NOT EXISTS project_id BIGINT REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS project_id BIGINT REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS project_id BIGINT REFERENCES projects(id) ON DELETE SET NULL;

INSERT INTO projects (org_id, name, slug, description, status)
SELECT o.id, 'Default Project', 'default', 'Default project created for PRD2 compatibility', 'active'
FROM orgs o
WHERE NOT EXISTS (
    SELECT 1 FROM projects p WHERE p.org_id = o.id AND p.slug = 'default'
);

INSERT INTO project_members (project_id, user_id, role)
SELECT p.id, u.id, 'project_admin'
FROM projects p
JOIN users u ON u.org_id = p.org_id
WHERE p.slug = 'default'
  AND u.role IN ('admin', 'platform_admin', 'org_admin')
ON CONFLICT (project_id, user_id) DO NOTHING;

UPDATE api_keys ak
SET project_id = p.id
FROM projects p
WHERE ak.project_id IS NULL
  AND p.org_id = ak.org_id
  AND p.slug = 'default';

UPDATE call_logs cl
SET project_id = ak.project_id
FROM api_keys ak
WHERE cl.project_id IS NULL
  AND cl.api_key_id = ak.id
  AND ak.project_id IS NOT NULL;

UPDATE call_logs cl
SET project_id = p.id
FROM projects p
WHERE cl.project_id IS NULL
  AND p.org_id = cl.org_id
  AND p.slug = 'default';

UPDATE transactions tx
SET project_id = p.id
FROM projects p
WHERE tx.project_id IS NULL
  AND p.org_id = tx.org_id
  AND p.slug = 'default';

UPDATE audit_logs al
SET project_id = p.id
FROM projects p
WHERE al.project_id IS NULL
  AND p.org_id = al.org_id
  AND p.slug = 'default';

CREATE INDEX IF NOT EXISTS idx_api_keys_project_id ON api_keys(project_id);
CREATE INDEX IF NOT EXISTS idx_call_logs_project_created ON call_logs(project_id, created_at) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_transactions_project_created ON transactions(project_id, created_at) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_logs_project_created ON audit_logs(project_id, created_at) WHERE project_id IS NOT NULL;

COMMIT;
