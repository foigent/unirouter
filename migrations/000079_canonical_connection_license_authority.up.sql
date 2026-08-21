BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

-- Activations acquire their relation RowExclusive lock before this advisory
-- lock, while License renewal acquires the advisory before it writes the
-- entitlement row. Split the table locks around the resource lock to match
-- both orders without a cycle.
LOCK TABLE provider_connections, channels, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

SELECT pg_advisory_xact_lock(
    hashtextextended('unirouter.commercial_limit.active_channels', 0)
);

LOCK TABLE commercial_license_entitlements
    IN SHARE ROW EXCLUSIVE MODE;

-- Legacy/verify process drain remains an orchestration cutover precondition.
-- This migration switches only the signed active_channels count authority; it
-- does not install a legacy channels/channel_keys DML seal or test bypass.
DO $$
DECLARE
    signed_limit BIGINT;
    enabled_connections BIGINT;
BEGIN
    SELECT active_channels
      INTO signed_limit
      FROM commercial_license_entitlements
     WHERE singleton=TRUE;

    IF signed_limit IS NOT NULL THEN
        SELECT COUNT(*) INTO enabled_connections
          FROM provider_connections
         WHERE admin_state='enabled';
        IF enabled_connections > signed_limit THEN
            RAISE EXCEPTION 'Canonical connection License authority migration refused: % enabled provider connections exceed signed active_channels limit %',
                enabled_connections, signed_limit
                USING ERRCODE='23514',
                      CONSTRAINT='commercial_license_active_channels';
        END IF;
    END IF;
END;
$$;

DROP TRIGGER channels_commercial_limit ON channels;

-- active_channels now means canonical enabled provider connections. The
-- other commercial resources retain their exact M59 logical-seat contracts.
CREATE OR REPLACE FUNCTION enforce_commercial_resource_limit()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    resource_name TEXT;
    limit_column TEXT;
    constraint_name TEXT;
    signed_limit BIGINT;
    entitlement_expiry TIMESTAMPTZ;
    active_count BIGINT;
BEGIN
    CASE TG_TABLE_NAME
        WHEN 'provider_connections' THEN
            IF NEW.admin_state <> 'enabled' THEN
                RETURN NEW;
            END IF;
            IF TG_OP='UPDATE' AND OLD.admin_state='enabled' THEN
                RETURN NEW;
            END IF;
            resource_name := 'active_channels'; limit_column := 'active_channels';
        WHEN 'api_keys' THEN
            IF NEW.status <> 'active' THEN
                RETURN NEW;
            END IF;
            IF TG_OP='UPDATE' AND OLD.status='active' THEN
                RETURN NEW;
            END IF;
            resource_name := 'active_api_keys'; limit_column := 'active_api_keys';
        WHEN 'access_credentials' THEN
            IF NEW.status <> 'active' THEN
                RETURN NEW;
            END IF;
            IF TG_OP='UPDATE' AND OLD.status='active' THEN
                RETURN NEW;
            END IF;
            resource_name := 'active_api_keys'; limit_column := 'active_api_keys';
        WHEN 'users' THEN
            IF NEW.status <> 'active' THEN
                RETURN NEW;
            END IF;
            IF TG_OP='UPDATE' AND OLD.status='active' THEN
                RETURN NEW;
            END IF;
            resource_name := 'active_users'; limit_column := 'active_users';
        WHEN 'projects' THEN
            IF NEW.status <> 'active' THEN
                RETURN NEW;
            END IF;
            IF TG_OP='UPDATE' AND OLD.status='active' THEN
                RETURN NEW;
            END IF;
            resource_name := 'active_projects'; limit_column := 'active_projects';
        ELSE
            RAISE EXCEPTION 'unsupported commercial License resource table %', TG_TABLE_NAME;
    END CASE;
    constraint_name := 'commercial_license_' || resource_name;

    PERFORM pg_advisory_xact_lock(hashtextextended('unirouter.commercial_limit.' || resource_name, 0));
    EXECUTE format(
        'SELECT %I, expires_at FROM commercial_license_entitlements WHERE singleton = TRUE',
        limit_column
    ) INTO signed_limit, entitlement_expiry;
    IF signed_limit IS NULL THEN
        RETURN NEW;
    END IF;
    IF entitlement_expiry <= NOW() THEN
        RAISE EXCEPTION 'commercial License is expired; cannot activate %', resource_name
            USING ERRCODE='23514', CONSTRAINT=constraint_name;
    END IF;

    IF TG_TABLE_NAME='api_keys' THEN
        IF NEW.access_credential_id IS NOT NULL THEN
            RETURN NEW;
        END IF;
    END IF;
    IF resource_name='active_api_keys' THEN
        SELECT COUNT(*) INTO active_count FROM (
            SELECT ac.id::TEXT AS seat
              FROM access_credentials ac
             WHERE ac.status='active'
            UNION ALL
            SELECT ('legacy:' || legacy_key.id)::TEXT
              FROM api_keys legacy_key
             WHERE legacy_key.status='active'
               AND legacy_key.access_credential_id IS NULL
        ) seats;
    ELSIF resource_name='active_channels' THEN
        SELECT COUNT(*) INTO active_count
          FROM provider_connections
         WHERE admin_state='enabled';
    ELSE
        EXECUTE format('SELECT COUNT(*) FROM %I WHERE status = $1', TG_TABLE_NAME)
            INTO active_count USING 'active';
    END IF;
    IF active_count >= signed_limit THEN
        RAISE EXCEPTION 'commercial License limit exceeded for %: % active, limit %',
            resource_name, active_count, signed_limit
            USING ERRCODE='23514', CONSTRAINT=constraint_name;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER provider_connections_commercial_limit
    BEFORE INSERT OR UPDATE OF admin_state ON provider_connections
    FOR EACH ROW EXECUTE FUNCTION enforce_commercial_resource_limit();

COMMENT ON TRIGGER provider_connections_commercial_limit ON provider_connections IS
    'Canonical active_channels License authority; counts enabled provider_connections under the shared resource advisory lock.';

COMMIT;
