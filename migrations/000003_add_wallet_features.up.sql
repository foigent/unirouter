-- Migration 000003: Ensure wallet feature columns exist
-- Columns may already exist if 000001 was applied with the latest schema.
-- This migration is idempotent.

ALTER TABLE wallets ADD COLUMN IF NOT EXISTS currency VARCHAR(10) NOT NULL DEFAULT 'CNY';
ALTER TABLE wallets ADD COLUMN IF NOT EXISTS auto_freeze BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE wallets ADD COLUMN IF NOT EXISTS low_balance_threshold_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE wallets ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
