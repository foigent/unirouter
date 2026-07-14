BEGIN;

-- Only an application-verified commercial License may populate this singleton.
-- Private keys and the raw signed document never enter this table. Nullable
-- limits mean that the signed License did not constrain that resource.
CREATE TABLE commercial_license_entitlements (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    license_id VARCHAR(128) NOT NULL,
    deployment_id VARCHAR(128) NOT NULL,
    key_id VARCHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    features JSONB NOT NULL CHECK (jsonb_typeof(features) = 'array'),
    active_api_keys BIGINT CHECK (active_api_keys IS NULL OR active_api_keys > 0),
    active_users BIGINT CHECK (active_users IS NULL OR active_users > 0),
    active_projects BIGINT CHECK (active_projects IS NULL OR active_projects > 0),
    active_channels BIGINT CHECK (active_channels IS NULL OR active_channels > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Supported resource writers are serialized per resource across every replica.
-- INSERT or inactive-to-active transitions fail before the row mutation when
-- they would exceed a signed limit. Disables and deletes always remain possible.
CREATE FUNCTION enforce_commercial_resource_limit()
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
    IF TG_OP = 'UPDATE' AND OLD.status = 'active' THEN
        RETURN NEW;
    END IF;

    CASE TG_TABLE_NAME
        WHEN 'api_keys' THEN
            resource_name := 'active_api_keys';
            limit_column := 'active_api_keys';
        WHEN 'users' THEN
            resource_name := 'active_users';
            limit_column := 'active_users';
        WHEN 'projects' THEN
            resource_name := 'active_projects';
            limit_column := 'active_projects';
        WHEN 'channels' THEN
            resource_name := 'active_channels';
            limit_column := 'active_channels';
        ELSE
            RAISE EXCEPTION 'unsupported commercial License resource table %', TG_TABLE_NAME;
    END CASE;
    constraint_name := 'commercial_license_' || resource_name;

    PERFORM pg_advisory_xact_lock(hashtextextended('unirouter.commercial_limit.' || resource_name, 0));
    EXECUTE format(
        'SELECT %I, expires_at FROM commercial_license_entitlements WHERE singleton = TRUE',
        limit_column
    ) INTO signed_limit, entitlement_expiry;

    -- No verified snapshot, or no explicit signed constraint for this resource.
    IF signed_limit IS NULL THEN
        RETURN NEW;
    END IF;
    IF entitlement_expiry <= NOW() THEN
        RAISE EXCEPTION 'commercial License is expired; cannot activate %', resource_name
            USING ERRCODE = '23514', CONSTRAINT = constraint_name;
    END IF;

    EXECUTE format('SELECT COUNT(*) FROM %I WHERE status = $1', TG_TABLE_NAME)
        INTO active_count USING 'active';
    IF active_count >= signed_limit THEN
        RAISE EXCEPTION 'commercial License limit exceeded for %: % active, limit %',
            resource_name, active_count, signed_limit
            USING ERRCODE = '23514', CONSTRAINT = constraint_name;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER api_keys_commercial_limit
    BEFORE INSERT OR UPDATE OF status ON api_keys
    FOR EACH ROW EXECUTE FUNCTION enforce_commercial_resource_limit();
CREATE TRIGGER users_commercial_limit
    BEFORE INSERT OR UPDATE OF status ON users
    FOR EACH ROW EXECUTE FUNCTION enforce_commercial_resource_limit();
CREATE TRIGGER projects_commercial_limit
    BEFORE INSERT OR UPDATE OF status ON projects
    FOR EACH ROW EXECUTE FUNCTION enforce_commercial_resource_limit();
CREATE TRIGGER channels_commercial_limit
    BEFORE INSERT OR UPDATE OF status ON channels
    FOR EACH ROW EXECUTE FUNCTION enforce_commercial_resource_limit();

-- Release sequence zero is the pre-signed-bundle baseline. It advances only
-- after the exact verified runtime is healthy and explicitly recorded.
CREATE TABLE release_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    release_sequence BIGINT NOT NULL DEFAULT 0 CHECK (release_sequence >= 0),
    version VARCHAR(128) NOT NULL DEFAULT '',
    commit VARCHAR(128) NOT NULL DEFAULT '',
    package_sha256 CHAR(64) CHECK (package_sha256 IS NULL OR package_sha256 ~ '^[0-9a-f]{64}$'),
    trusted_key_id VARCHAR(64) NOT NULL DEFAULT '',
    target_schema VARCHAR(128) NOT NULL DEFAULT '',
    manifest JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO release_state (singleton) VALUES (TRUE);

CREATE TABLE release_history (
    id BIGSERIAL PRIMARY KEY,
    release_sequence BIGINT NOT NULL UNIQUE CHECK (release_sequence > 0),
    version VARCHAR(128) NOT NULL,
    commit VARCHAR(128) NOT NULL,
    package_sha256 CHAR(64) NOT NULL UNIQUE CHECK (package_sha256 ~ '^[0-9a-f]{64}$'),
    trusted_key_id VARCHAR(64) NOT NULL,
    target_schema VARCHAR(128) NOT NULL,
    manifest JSONB NOT NULL,
    recorded_by VARCHAR(128) NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMIT;
