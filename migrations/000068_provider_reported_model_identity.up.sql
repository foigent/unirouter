BEGIN;

ALTER TABLE channel_model_probes
    ADD COLUMN reported_model VARCHAR(200) NOT NULL DEFAULT '';

ALTER TABLE channel_model_probes
    ADD CONSTRAINT channel_model_probes_reported_model_check CHECK (
        reported_model = BTRIM(reported_model)
        AND reported_model !~ E'[\\x00\\r\\n]'
    );

COMMENT ON COLUMN channel_model_probes.reported_model IS
    'Exact top-level model identity returned by the latest provider response; empty means the provider did not report one.';

COMMIT;
