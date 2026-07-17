BEGIN;

LOCK TABLE users IN ACCESS EXCLUSIVE MODE;

DO $$
DECLARE
    incompatible_groups BIGINT;
BEGIN
    IF EXISTS (
        SELECT 1
        FROM schema_migrations
        WHERE version > '000042_v1_local_login_identity'
    ) THEN
        RAISE EXCEPTION 'V1 local login identity rollback refused: later migrations must be reversed first';
    END IF;

    SELECT COUNT(*) INTO incompatible_groups
    FROM (
        SELECT org_id, username
        FROM users
        GROUP BY org_id, username
        HAVING COUNT(*) > 1
    ) conflicts;

    IF incompatible_groups > 0 THEN
        RAISE EXCEPTION
            'V1 local login identity rollback refused: incompatible_org_username_groups=%; resolve same-organization cross-provider display-name overlaps explicitly before retry',
            incompatible_groups;
    END IF;
END;
$$;

ALTER TABLE users
    ADD CONSTRAINT users_org_username_unique UNIQUE (org_id, username);

DROP INDEX public.users_external_org_username_unique;
DROP INDEX public.users_local_login_id_unique;
ALTER TABLE users
    DROP CONSTRAINT users_local_login_username_canonical,
    DROP CONSTRAINT users_local_login_id_nonblank;
DROP FUNCTION public.normalize_local_login_id(TEXT);

DELETE FROM schema_migrations
WHERE version = '000042_v1_local_login_identity';

COMMIT;
