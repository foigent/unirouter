BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

-- Match both activation (resource table then advisory) and License renewal
-- (advisory then entitlement table) without introducing a lock-order cycle.
LOCK TABLE provider_connections, channels, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

SELECT pg_advisory_xact_lock(
    hashtextextended('unirouter.commercial_limit.active_channels', 0)
);

LOCK TABLE commercial_license_entitlements
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
DECLARE
    signed_limit BIGINT;
    active_legacy_channels BIGINT;
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version>'000079_canonical_connection_license_authority'
    ) THEN
        RAISE EXCEPTION 'Canonical connection License authority rollback refused: later migrations must be reversed first';
    END IF;

    -- Restoring the legacy counter is safe only when every currently enabled
    -- canonical connection has one exact active legacy projection. Native
    -- disabled/retired rows remain valid M78 data and do not consume authority.
    IF EXISTS (
        SELECT 1
          FROM provider_connections connection
          LEFT JOIN channels legacy
            ON legacy.id=connection.legacy_channel_id
         WHERE connection.admin_state='enabled'
           AND (
               connection.legacy_channel_id IS NULL
               OR legacy.id IS NULL
               OR legacy.status<>'active'
               OR connection.name IS DISTINCT FROM legacy.name
               OR connection.provider IS DISTINCT FROM legacy.provider
               OR connection.base_url IS DISTINCT FROM legacy.base_url
               OR connection.adapter_type IS DISTINCT FROM
                  COALESCE(NULLIF(BTRIM(legacy.adapter_type),''),'openai_compatible')
               OR connection.adapter_config IS DISTINCT FROM
                  COALESCE(legacy.adapter_config,'{}'::jsonb)
               OR connection.config_revision IS DISTINCT FROM legacy.routing_epoch
           )
    ) THEN
        RAISE EXCEPTION 'Canonical connection License authority rollback refused: enabled provider connection lacks an exact active legacy projection';
    END IF;

    SELECT active_channels
      INTO signed_limit
      FROM commercial_license_entitlements
     WHERE singleton=TRUE;
    IF signed_limit IS NOT NULL THEN
        SELECT COUNT(*) INTO active_legacy_channels
          FROM channels
         WHERE status='active';
        IF active_legacy_channels > signed_limit THEN
            RAISE EXCEPTION 'Canonical connection License authority rollback refused: % active legacy channels exceed signed active_channels limit %',
                active_legacy_channels, signed_limit
                USING ERRCODE='23514',
                      CONSTRAINT='commercial_license_active_channels';
        END IF;
    END IF;
END;
$$;

DROP TRIGGER provider_connections_commercial_limit ON provider_connections;

-- Restore the exact M59 resource function and its channels trigger. Canonical
-- tables and credentials remain intact; this down changes only License count
-- authority after the guarded projection/limit proof above.
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
    IF NEW.status <> 'active' THEN
        RETURN NEW;
    END IF;
    IF TG_OP='UPDATE' AND OLD.status='active' THEN
        RETURN NEW;
    END IF;

    CASE TG_TABLE_NAME
        WHEN 'api_keys' THEN
            resource_name := 'active_api_keys'; limit_column := 'active_api_keys';
        WHEN 'access_credentials' THEN
            resource_name := 'active_api_keys'; limit_column := 'active_api_keys';
        WHEN 'users' THEN
            resource_name := 'active_users'; limit_column := 'active_users';
        WHEN 'projects' THEN
            resource_name := 'active_projects'; limit_column := 'active_projects';
        WHEN 'channels' THEN
            resource_name := 'active_channels'; limit_column := 'active_channels';
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

CREATE TRIGGER channels_commercial_limit
    BEFORE INSERT OR UPDATE OF status ON channels
    FOR EACH ROW EXECUTE FUNCTION enforce_commercial_resource_limit();

DELETE FROM schema_migrations
WHERE version='000079_canonical_connection_license_authority';

COMMIT;
