BEGIN;

ALTER TABLE users
    ADD COLUMN auth_epoch BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT users_auth_epoch_nonnegative CHECK (auth_epoch >= 0);

CREATE OR REPLACE FUNCTION enforce_user_auth_epoch()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.auth_epoch < OLD.auth_epoch THEN
        RAISE EXCEPTION 'users.auth_epoch cannot decrease'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.password_hash IS DISTINCT FROM OLD.password_hash
       AND NEW.auth_epoch = OLD.auth_epoch THEN
        NEW.auth_epoch := OLD.auth_epoch + 1;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_auth_epoch_monotonic
BEFORE UPDATE OF auth_epoch, password_hash ON users
FOR EACH ROW EXECUTE FUNCTION enforce_user_auth_epoch();

CREATE TABLE admin_sessions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sid_hash BYTEA NOT NULL UNIQUE,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT admin_sessions_sid_hash_sha256 CHECK (octet_length(sid_hash) = 32),
    CONSTRAINT admin_sessions_expiry_after_issue CHECK (expires_at > issued_at),
    CONSTRAINT admin_sessions_revocation_after_issue CHECK (revoked_at IS NULL OR revoked_at >= issued_at)
);

CREATE INDEX idx_admin_sessions_user_live
    ON admin_sessions (user_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE INDEX idx_admin_sessions_expired
    ON admin_sessions (expires_at);

COMMIT;
