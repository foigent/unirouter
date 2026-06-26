-- Reverse migration 000003
ALTER TABLE wallets DROP COLUMN IF EXISTS currency;
ALTER TABLE wallets DROP COLUMN IF EXISTS auto_freeze;
ALTER TABLE wallets DROP COLUMN IF EXISTS low_balance_threshold_cents;
ALTER TABLE wallets DROP COLUMN IF EXISTS created_at;
