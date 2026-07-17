BEGIN;

CREATE TABLE migration_concurrency_probe (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton)
);

INSERT INTO migration_concurrency_probe (singleton) VALUES (TRUE);
SELECT pg_sleep(0.2);

COMMIT;
