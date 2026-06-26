-- UniRoute Phase 1 Database Schema
-- Version: 000001
-- Date: 2026-06-18

BEGIN;

-- ==========================================
-- 1. Organizations
-- ==========================================
CREATE TABLE orgs (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    settings JSONB NOT NULL DEFAULT '{}',
    parent_id BIGINT REFERENCES orgs(id),
    org_type VARCHAR(20) NOT NULL DEFAULT 'customer',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_orgs_status ON orgs(status);
CREATE INDEX idx_orgs_parent_id ON orgs(parent_id) WHERE parent_id IS NOT NULL;

-- ==========================================
-- 2. Users
-- ==========================================
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id),
    username VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(30) NOT NULL DEFAULT 'org_viewer',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT users_org_username_unique UNIQUE (org_id, username)
);

CREATE INDEX idx_users_org_id ON users(org_id);

-- ==========================================
-- 3. Channels
-- ==========================================
CREATE TABLE channels (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    provider VARCHAR(50) NOT NULL,
    base_url VARCHAR(512) NOT NULL,
    models TEXT NOT NULL DEFAULT '',
    model_mapping JSONB NOT NULL DEFAULT '{}',
    max_context INTEGER NOT NULL DEFAULT 128000,
    default_max_output INTEGER NOT NULL DEFAULT 4096,
    priority INTEGER NOT NULL DEFAULT 100,
    rpm_limit INTEGER NOT NULL DEFAULT 600,
    tpm_limit INTEGER NOT NULL DEFAULT 100000,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    circuit_state VARCHAR(20) NOT NULL DEFAULT 'healthy',
    half_open_until TIMESTAMPTZ,
    fail_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_channels_status ON channels(status);
CREATE INDEX idx_channels_provider ON channels(provider);
CREATE INDEX idx_channels_priority ON channels(priority);

-- ==========================================
-- 4. Channel Keys
-- ==========================================
CREATE TABLE channel_keys (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    key_enc TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    fail_count INTEGER NOT NULL DEFAULT 0,
    last_used_at TIMESTAMPTZ,
    cooldown_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_channel_keys_channel_id ON channel_keys(channel_id);
CREATE INDEX idx_channel_keys_status ON channel_keys(status);

-- ==========================================
-- 5. API Keys
-- ==========================================
CREATE TABLE api_keys (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id),
    name VARCHAR(255) NOT NULL DEFAULT '',
    key_prefix VARCHAR(20) NOT NULL,
    key_hash VARCHAR(64) NOT NULL,
    allowed_ips TEXT NOT NULL DEFAULT '',
    allowed_models TEXT NOT NULL DEFAULT '',
    rpm_limit INTEGER NOT NULL DEFAULT 60,
    qps_limit INTEGER NOT NULL DEFAULT 10,
    budget_limit INTEGER,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_api_keys_prefix ON api_keys(key_prefix);
CREATE INDEX idx_api_keys_org_id ON api_keys(org_id);
CREATE INDEX idx_api_keys_hash ON api_keys(key_hash);

-- ==========================================
-- 6. Model Prices
-- ==========================================
CREATE TABLE model_prices (
    id BIGSERIAL PRIMARY KEY,
    model_name VARCHAR(100) NOT NULL UNIQUE,
    provider VARCHAR(50) NOT NULL DEFAULT '',
    input_price_cents INTEGER NOT NULL,
    output_price_cents INTEGER NOT NULL,
    max_context INTEGER NOT NULL DEFAULT 128000,
    default_max_output INTEGER NOT NULL DEFAULT 4096,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ==========================================
-- 7. Org Model Prices (overrides)
-- ==========================================
CREATE TABLE org_model_prices (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id),
    model_name VARCHAR(100) NOT NULL,
    multiplier NUMERIC(5,3) NOT NULL DEFAULT 1.000,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT org_model_prices_unique UNIQUE (org_id, model_name)
);

CREATE INDEX idx_org_model_prices_org_id ON org_model_prices(org_id);

-- ==========================================
-- 8. Wallets
-- ==========================================
CREATE TABLE wallets (
    org_id BIGINT PRIMARY KEY REFERENCES orgs(id),
    balance INTEGER NOT NULL DEFAULT 0,
    granted_balance INTEGER NOT NULL DEFAULT 0,
    frozen INTEGER NOT NULL DEFAULT 0,
    total_spent INTEGER NOT NULL DEFAULT 0,
    currency VARCHAR(10) NOT NULL DEFAULT 'CNY',
    auto_freeze BOOLEAN NOT NULL DEFAULT false,
    low_balance_threshold_cents INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT wallets_balance_check CHECK (balance >= 0),
    CONSTRAINT wallets_granted_check CHECK (granted_balance >= 0),
    CONSTRAINT wallets_frozen_check CHECK (frozen >= 0)
);

-- ==========================================
-- 9. Transactions
-- ==========================================
CREATE TABLE transactions (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(64) NOT NULL,
    org_id BIGINT NOT NULL,
    type VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'completed',
    amount INTEGER NOT NULL,
    balance_after INTEGER,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_transactions_request_id ON transactions(request_id);
CREATE INDEX idx_transactions_org_id_created ON transactions(org_id, created_at);

-- ==========================================
-- 10. Call Logs
-- ==========================================
CREATE TABLE call_logs (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(64) NOT NULL,
    org_id BIGINT NOT NULL,
    api_key_id BIGINT,
    model VARCHAR(100) NOT NULL DEFAULT '',
    provider VARCHAR(50) NOT NULL DEFAULT '',
    channel VARCHAR(255) NOT NULL DEFAULT '',
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    cost_cents INTEGER NOT NULL DEFAULT 0,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    ttft_ms INTEGER NOT NULL DEFAULT 0,
    status_code INTEGER NOT NULL DEFAULT 0,
    upstream_status_code INTEGER,
    upstream_error TEXT,
    usage_source VARCHAR(20) NOT NULL DEFAULT 'upstream',
    is_stream BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_call_logs_request_id ON call_logs(request_id);
CREATE INDEX idx_call_logs_org_id_created ON call_logs(org_id, created_at);
CREATE INDEX idx_call_logs_model ON call_logs(model);
CREATE INDEX idx_call_logs_created ON call_logs(created_at);

-- ==========================================
-- 11. Audit Logs
-- ==========================================
CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    action VARCHAR(50) NOT NULL,
    resource VARCHAR(50) NOT NULL,
    resource_id VARCHAR(64) NOT NULL DEFAULT '',
    detail JSONB NOT NULL DEFAULT '{}',
    ip VARCHAR(45) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    request_id VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_org_id_created ON audit_logs(org_id, created_at);
CREATE INDEX idx_audit_logs_user_id ON audit_logs(user_id);

-- ==========================================
-- 12. Grants
-- ==========================================
CREATE TABLE grants (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id),
    amount INTEGER NOT NULL,
    balance INTEGER NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    reason TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_grants_org_id ON grants(org_id);
CREATE INDEX idx_grants_expires ON grants(expires_at) WHERE status = 'active';

-- ==========================================
-- 13. Freeze Idempotency
-- ==========================================
CREATE TABLE freeze_idempotency (
    request_id VARCHAR(64) PRIMARY KEY,
    status VARCHAR(20) NOT NULL DEFAULT 'Processing',
    response JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_freeze_idempotency_status ON freeze_idempotency(status);
CREATE INDEX idx_freeze_idempotency_created ON freeze_idempotency(created_at);

-- ==========================================
-- 14. Settings
-- ==========================================
CREATE TABLE settings (
    key VARCHAR(100) PRIMARY KEY,
    value JSONB NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Default settings
INSERT INTO settings (key, value, description) VALUES
    ('webhook.feishu', '""', '飞书 Webhook URL'),
    ('webhook.dingtalk', '""', '钉钉 Webhook URL'),
    ('sensitive_words', '[]', '敏感词库'),
    ('backup.enabled', 'true', '自动备份开关'),
    ('backup.cron', '"0 2 * * *"', '备份定时表达式'),
    ('backup.retain_days', '7', '备份保留天数');

-- ==========================================
-- Seed data: Default admin org
-- ==========================================
INSERT INTO orgs (id, name, status, org_type) VALUES
    (1, 'Platform Admin', 'active', 'partner');

INSERT INTO wallets (org_id) VALUES (1);

-- Default admin user (password: admin123456 - BCrypt hash)
INSERT INTO users (org_id, username, password_hash, role, status)
VALUES (1, 'admin', '$2a$10$pL5GY6ZqhE2bCJitNccowuvDF6AO3i7RQwW2xKq0nyKslD1xAWNL2', 'admin', 'active');

COMMIT;
