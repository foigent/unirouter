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

-- Default model prices (price per 1M tokens in cents)
INSERT INTO model_prices (model_name, input_price_cents, output_price_cents, max_context, default_max_output) VALUES
    ('gpt-4o', 250, 1000, 128000, 4096),
    ('gpt-4o-mini', 15, 60, 128000, 4096),
    ('gpt-4-turbo', 1000, 3000, 128000, 4096),
    ('gpt-4', 3000, 6000, 8192, 4096),
    ('gpt-3.5-turbo', 50, 150, 16385, 4096),
    ('claude-3-5-sonnet-20241022', 300, 1500, 200000, 8192),
    ('claude-3-opus-20240229', 1500, 7500, 200000, 4096),
    ('claude-3-haiku-20240307', 25, 125, 200000, 4096),
    ('deepseek-chat', 14, 28, 64000, 4096),
    ('deepseek-reasoner', 55, 219, 64000, 8192)
ON CONFLICT (model_name) DO NOTHING;
