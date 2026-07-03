-- Rollback 000015: remove PRD2 OIDC first SSO user binding fields.

BEGIN;

DROP INDEX IF EXISTS idx_users_auth_provider;
DROP INDEX IF EXISTS idx_users_email;
DROP INDEX IF EXISTS idx_users_oidc_identity;

ALTER TABLE users DROP COLUMN IF EXISTS oidc_subject;
ALTER TABLE users DROP COLUMN IF EXISTS oidc_issuer;
ALTER TABLE users DROP COLUMN IF EXISTS auth_provider;
ALTER TABLE users DROP COLUMN IF EXISTS email;

COMMIT;
