BEGIN;

-- P6-F08 assigns weight only within an equal-priority channel tier. Epochs
-- protect health/control transitions from stale completions, while the probe
-- lease gives half-open recovery one owner across all gateway replicas.
ALTER TABLE channels
    ADD COLUMN IF NOT EXISTS routing_weight INTEGER NOT NULL DEFAULT 100,
    ADD COLUMN IF NOT EXISTS routing_epoch BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS circuit_epoch BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS half_open_probe_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS half_open_probe_token VARCHAR(80);

ALTER TABLE channels
    DROP CONSTRAINT IF EXISTS channels_routing_weight_bounds,
    DROP CONSTRAINT IF EXISTS channels_routing_epoch_positive,
    DROP CONSTRAINT IF EXISTS channels_circuit_epoch_positive,
    ADD CONSTRAINT channels_routing_weight_bounds
        CHECK (routing_weight BETWEEN 1 AND 1000),
    ADD CONSTRAINT channels_routing_epoch_positive
        CHECK (routing_epoch > 0),
    ADD CONSTRAINT channels_circuit_epoch_positive
        CHECK (circuit_epoch > 0);

ALTER TABLE channel_keys
    ADD COLUMN IF NOT EXISTS state_epoch BIGINT NOT NULL DEFAULT 1;

ALTER TABLE channel_keys
    DROP CONSTRAINT IF EXISTS channel_keys_state_epoch_positive,
    ADD CONSTRAINT channel_keys_state_epoch_positive
        CHECK (state_epoch > 0);

CREATE INDEX IF NOT EXISTS idx_channels_routing_order
    ON channels (priority, routing_weight DESC, id);

COMMIT;
