-- Seed data for UniRoute
-- Adds default settings and model prices
-- (Admin org, user, and wallet are created in 000001)

-- Default settings
INSERT INTO settings (key, value, description, updated_at) VALUES
    ('site_name', '"UniRoute"', 'Site display name', NOW()),
    ('default_model', '"gpt-3.5-turbo"', 'Default model for new keys', NOW()),
    ('enable_registration', 'true', 'Allow new user registration', NOW()),
    ('maintenance_mode', 'false', 'Enable maintenance mode', NOW())
ON CONFLICT (key) DO NOTHING;

-- Budget alert settings
INSERT INTO settings (key, value, description, updated_at) VALUES
    ('budget_alert_webhook_url', '""', 'Webhook URL for budget alerts', NOW()),
    ('budget_alert_threshold_cents', '1000', 'Threshold in cents to trigger alert', NOW())
ON CONFLICT (key) DO NOTHING;

-- Model prices are intentionally not seeded. Vendor prices, currencies and
-- effective dates change independently of the binary; an administrator must
-- configure an explicit CNY-per-million-token authority before a model can be
-- billed. Existing databases are not changed by this historical seed edit.
