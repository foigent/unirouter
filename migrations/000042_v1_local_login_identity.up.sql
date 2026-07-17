BEGIN;

-- Local login is a two-field username/password flow with no organization
-- selector. Hold all user writers while validating and activating the global
-- local identity contract so no legacy writer can race the preflight/index.
LOCK TABLE users IN ACCESS EXCLUSIVE MODE;

-- This function is the single database authority for login comparison,
-- migration validation, the CHECK constraint, and the unique index. The
-- explicit trim set is Go's unicode.IsSpace set used by strings.TrimSpace;
-- spelling it out avoids database locale-dependent POSIX class differences.
CREATE FUNCTION public.normalize_local_login_id(value TEXT)
RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
STRICT
PARALLEL SAFE
AS $$
    SELECT pg_catalog.lower(
        pg_catalog.btrim(
            value,
            U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'
        )
    )
$$;

DO $$
DECLARE
    blank_count BIGINT;
    untrimmed_count BIGINT;
    collision_groups BIGINT;
BEGIN
    SELECT COUNT(*) INTO blank_count
    FROM users
    WHERE COALESCE(NULLIF(auth_provider, ''), 'local') = 'local'
      AND public.normalize_local_login_id(username) = '';

    SELECT COUNT(*) INTO untrimmed_count
    FROM users
    WHERE COALESCE(NULLIF(auth_provider, ''), 'local') = 'local'
      AND pg_catalog.lower(username) IS DISTINCT FROM public.normalize_local_login_id(username);

    SELECT COUNT(*) INTO collision_groups
    FROM (
        SELECT public.normalize_local_login_id(username)
        FROM users
        WHERE COALESCE(NULLIF(auth_provider, ''), 'local') = 'local'
        GROUP BY public.normalize_local_login_id(username)
        HAVING COUNT(*) > 1
    ) conflicts;

    IF blank_count > 0 OR untrimmed_count > 0 OR collision_groups > 0 THEN
        RAISE EXCEPTION
            'V1 local login identity migration refused: blank_count=%, untrimmed_count=%, collision_groups=%; resolve local identities explicitly before retry',
            blank_count, untrimmed_count, collision_groups;
    END IF;
END;
$$;

ALTER TABLE users
    ADD CONSTRAINT users_local_login_id_nonblank CHECK (
        COALESCE(NULLIF(auth_provider, ''), 'local') <> 'local'
        OR public.normalize_local_login_id(username) <> ''
    ),
    ADD CONSTRAINT users_local_login_username_canonical CHECK (
        COALESCE(NULLIF(auth_provider, ''), 'local') <> 'local'
        OR pg_catalog.lower(username) = public.normalize_local_login_id(username)
    );

CREATE UNIQUE INDEX users_local_login_id_unique
    ON users (public.normalize_local_login_id(username))
    WHERE COALESCE(NULLIF(auth_provider, ''), 'local') = 'local';

-- Preserve the legacy same-provider display-name behavior for externally
-- managed identities without letting an OIDC display name reserve or shadow a
-- local login ID in the same organization. OIDC authentication remains bound
-- to issuer/subject; this index only keeps generated display names distinct
-- inside an organization.
CREATE UNIQUE INDEX users_external_org_username_unique
    ON users (org_id, username)
    WHERE COALESCE(NULLIF(auth_provider, ''), 'local') <> 'local';

ALTER TABLE users
    DROP CONSTRAINT users_org_username_unique;

COMMIT;
