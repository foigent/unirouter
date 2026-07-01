-- Migration 000010: PRD2 model governance foundation
-- Adds internal model registry, aliases, model sets, and project assignments.

BEGIN;

CREATE TABLE IF NOT EXISTS model_registry (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    model_name VARCHAR(100) NOT NULL,
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    provider_family VARCHAR(50) NOT NULL DEFAULT '',
    lifecycle VARCHAR(20) NOT NULL DEFAULT 'staging'
        CHECK (lifecycle IN ('staging', 'ga', 'deprecated', 'retired')),
    context_length INTEGER NOT NULL DEFAULT 0 CHECK (context_length >= 0),
    output_limit INTEGER NOT NULL DEFAULT 0 CHECK (output_limit >= 0),
    capability_tags TEXT NOT NULL DEFAULT '',
    data_residency VARCHAR(100) NOT NULL DEFAULT '',
    pricing_model_name VARCHAR(100) NOT NULL DEFAULT '',
    approved_catalog BOOLEAN NOT NULL DEFAULT FALSE,
    migration_recommendation TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, model_name)
);

CREATE INDEX IF NOT EXISTS idx_model_registry_org_lifecycle ON model_registry(org_id, lifecycle);
CREATE INDEX IF NOT EXISTS idx_model_registry_org_approved ON model_registry(org_id, approved_catalog);

CREATE TABLE IF NOT EXISTS model_aliases (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    alias VARCHAR(100) NOT NULL,
    target_model VARCHAR(100) NOT NULL,
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, alias)
);

CREATE INDEX IF NOT EXISTS idx_model_aliases_org_target ON model_aliases(org_id, target_model);
CREATE INDEX IF NOT EXISTS idx_model_aliases_org_status ON model_aliases(org_id, status);

CREATE TABLE IF NOT EXISTS model_sets (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    slug VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, slug)
);

CREATE INDEX IF NOT EXISTS idx_model_sets_org_status ON model_sets(org_id, status);

CREATE TABLE IF NOT EXISTS model_set_items (
    model_set_id BIGINT NOT NULL REFERENCES model_sets(id) ON DELETE CASCADE,
    item_type VARCHAR(20) NOT NULL CHECK (item_type IN ('model', 'alias')),
    item_name VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (model_set_id, item_type, item_name)
);

CREATE INDEX IF NOT EXISTS idx_model_set_items_name ON model_set_items(item_type, item_name);

CREATE TABLE IF NOT EXISTS project_model_sets (
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    model_set_id BIGINT NOT NULL REFERENCES model_sets(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, model_set_id)
);

CREATE INDEX IF NOT EXISTS idx_project_model_sets_model_set_id ON project_model_sets(model_set_id);

INSERT INTO model_registry (
    org_id, model_name, display_name, provider_family, lifecycle,
    context_length, output_limit, pricing_model_name, approved_catalog
)
SELECT o.id,
       mp.model_name,
       mp.model_name,
       COALESCE(NULLIF(mp.provider, ''), 'openai_compatible'),
       'ga',
       mp.max_context,
       mp.default_max_output,
       mp.model_name,
       TRUE
FROM orgs o
CROSS JOIN model_prices mp
ON CONFLICT (org_id, model_name) DO NOTHING;

COMMIT;
