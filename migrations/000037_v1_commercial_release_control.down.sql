BEGIN;

-- Removing either an accepted signed release or an active entitlement snapshot
-- would silently disable a security boundary. Such recovery must roll forward
-- or be performed by an explicitly reviewed data migration.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM release_history)
       OR EXISTS (SELECT 1 FROM release_state WHERE release_sequence > 0) THEN
        RAISE EXCEPTION 'V1 commercial release-control downgrade refused: signed release history exists';
    END IF;
    IF EXISTS (SELECT 1 FROM commercial_license_entitlements) THEN
        RAISE EXCEPTION 'V1 commercial release-control downgrade refused: commercial entitlement snapshot exists';
    END IF;
END;
$$;

DROP TRIGGER channels_commercial_limit ON channels;
DROP TRIGGER projects_commercial_limit ON projects;
DROP TRIGGER users_commercial_limit ON users;
DROP TRIGGER api_keys_commercial_limit ON api_keys;
DROP FUNCTION enforce_commercial_resource_limit();
DROP TABLE release_history;
DROP TABLE release_state;
DROP TABLE commercial_license_entitlements;

COMMIT;
