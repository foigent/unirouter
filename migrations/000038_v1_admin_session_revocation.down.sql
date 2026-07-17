BEGIN;

-- A mixed old/new fleet or a destructive rollback after session issuance would
-- silently restore stateless JWT acceptance. Only a strict reverse-order,
-- pre-activation rollback is safe.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version > '000038_v1_admin_session_revocation'
    ) THEN
        RAISE EXCEPTION 'V1 admin session rollback refused: successor migration exists';
    END IF;
    IF EXISTS (SELECT 1 FROM admin_sessions) THEN
        RAISE EXCEPTION 'V1 admin session rollback refused: server-side session evidence exists';
    END IF;
    IF EXISTS (SELECT 1 FROM users WHERE auth_epoch <> 0) THEN
        RAISE EXCEPTION 'V1 admin session rollback refused: authentication revocation epoch evidence exists';
    END IF;
END;
$$;

DROP TABLE IF EXISTS admin_sessions;
DROP TRIGGER IF EXISTS trg_users_auth_epoch_monotonic ON users;
DROP FUNCTION IF EXISTS enforce_user_auth_epoch();
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_auth_epoch_nonnegative;
ALTER TABLE users DROP COLUMN IF EXISTS auth_epoch;

DELETE FROM schema_migrations
WHERE version = '000038_v1_admin_session_revocation';

COMMIT;
