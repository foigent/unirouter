-- Rollback migration 000010: PRD2 model governance foundation

BEGIN;

DROP TABLE IF EXISTS project_model_sets;
DROP TABLE IF EXISTS model_set_items;
DROP TABLE IF EXISTS model_sets;
DROP TABLE IF EXISTS model_aliases;
DROP TABLE IF EXISTS model_registry;

COMMIT;
