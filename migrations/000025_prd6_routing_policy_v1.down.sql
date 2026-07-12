BEGIN;

-- An older binary silently ignores weights and does not maintain epochs/probe
-- ownership. Refuse destructive rollback after any routing-policy.v1 control
-- or health state has been activated; drain old/new binaries and roll forward.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM channels
         WHERE routing_weight <> 100
            OR routing_epoch <> 1
            OR circuit_epoch <> 1
            OR half_open_probe_until IS NOT NULL
            OR half_open_probe_token IS NOT NULL
    ) OR EXISTS (SELECT 1 FROM channel_keys WHERE state_epoch <> 1) THEN
        RAISE EXCEPTION 'P6-F08 down refused: activated routing-policy state exists; retain the schema and roll forward';
    END IF;
END;
$$;

DROP INDEX IF EXISTS idx_channels_routing_order;

ALTER TABLE channel_keys
    DROP CONSTRAINT IF EXISTS channel_keys_state_epoch_positive,
    DROP COLUMN IF EXISTS state_epoch;

ALTER TABLE channels
    DROP CONSTRAINT IF EXISTS channels_routing_weight_bounds,
    DROP CONSTRAINT IF EXISTS channels_routing_epoch_positive,
    DROP CONSTRAINT IF EXISTS channels_circuit_epoch_positive,
    DROP COLUMN IF EXISTS half_open_probe_token,
    DROP COLUMN IF EXISTS half_open_probe_until,
    DROP COLUMN IF EXISTS circuit_epoch,
    DROP COLUMN IF EXISTS routing_epoch,
    DROP COLUMN IF EXISTS routing_weight;

DELETE FROM schema_migrations
WHERE version = '000025_prd6_routing_policy_v1';

COMMIT;
