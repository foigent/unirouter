BEGIN;

-- Freeze the two legacy role surfaces while their authority is separated.
-- This migration is additive for accounts and preserves users.role as a
-- compatibility shadow for one release window.
LOCK TABLE users, project_members IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM users
        WHERE role NOT IN (
            'admin', 'platform_admin', 'org_admin', 'billing_admin',
            'org_viewer', 'member', 'project_admin', 'developer',
            'project_developer', 'project_user', 'viewer', 'project_viewer'
        )
    ) THEN
        RAISE EXCEPTION 'enterprise role separation refused: users.role contains an unknown value';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM project_members
        WHERE role NOT IN (
            'project_admin', 'project_developer', 'developer',
            'project_user', 'project_viewer', 'viewer'
        )
    ) THEN
        RAISE EXCEPTION 'enterprise role separation refused: project_members.role contains an unknown value';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM project_members pm
        JOIN projects p ON p.id = pm.project_id
        JOIN users u ON u.id = pm.user_id
        WHERE p.org_id <> u.org_id
    ) THEN
        RAISE EXCEPTION 'enterprise role separation refused: cross-organization membership exists';
    END IF;
END;
$$;

ALTER TABLE users
    ADD COLUMN organization_role VARCHAR(30) DEFAULT '__legacy_role_unset__';

UPDATE users
SET organization_role = CASE role
    WHEN 'admin' THEN 'platform_admin'
    WHEN 'platform_admin' THEN 'platform_admin'
    WHEN 'org_admin' THEN 'org_admin'
    WHEN 'billing_admin' THEN 'billing_admin'
    WHEN 'org_viewer' THEN 'org_viewer'
    ELSE 'member'
END;

ALTER TABLE users
	ALTER COLUMN organization_role SET NOT NULL,
	ADD CONSTRAINT users_organization_role_check CHECK (
        organization_role IN (
            'platform_admin', 'org_admin', 'billing_admin', 'org_viewer', 'member'
        )
    );

CREATE INDEX idx_users_org_organization_role
    ON users (org_id, organization_role, status, id);

-- A binary from the immediately preceding release omits organization_role.
-- The non-persistable sentinel distinguishes that omission from a current
-- binary explicitly writing organization_role='member'; NULL is not treated
-- as a legacy omission. The BEFORE trigger replaces the sentinel before the
-- NOT NULL and CHECK constraints run, so it can never become durable state.
-- A legacy UPDATE changes only users.role and remains recognizable when the
-- canonical organization_role is unchanged.
-- PostgreSQL row triggers cannot distinguish that legacy statement from a
-- current client redundantly assigning organization_role to its OLD value
-- while changing role. During this one-release window that exact ambiguous
-- shape is intentionally interpreted as a legacy write and normalized from
-- role. Current product writers never use that shape for organization-role
-- changes: they validate and write both canonical axes together. The trigger
-- and sentinel default must be removed in a later migration after the old
-- writer is retired; they are not a permanent substitute for product-level
-- validation.
-- Keep that rolling-upgrade path deterministic without making the shadow an
-- authorization source: organization roles map to themselves, while every
-- project-role alias maps to the least-privilege member organization role.
-- Current binaries write both columns; membership shadow updates retain
-- organization_role=member. This is a one-release compatibility mechanism;
-- removing it requires a later, explicit migration after old writers retire.
CREATE FUNCTION sync_users_organization_role_from_legacy_role_write()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
	IF (TG_OP = 'INSERT' AND NEW.organization_role = '__legacy_role_unset__') OR (
		TG_OP = 'UPDATE'
		AND
		NEW.role IS DISTINCT FROM OLD.role
		AND NEW.organization_role IS NOT DISTINCT FROM OLD.organization_role
	) THEN
        NEW.organization_role = CASE NEW.role
            WHEN 'admin' THEN 'platform_admin'
            WHEN 'platform_admin' THEN 'platform_admin'
            WHEN 'org_admin' THEN 'org_admin'
            WHEN 'billing_admin' THEN 'billing_admin'
            WHEN 'org_viewer' THEN 'org_viewer'
            ELSE 'member'
        END;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER users_legacy_role_organization_role_compatibility
BEFORE INSERT OR UPDATE OF role, organization_role ON users
FOR EACH ROW
EXECUTE FUNCTION sync_users_organization_role_from_legacy_role_write();

ALTER TABLE project_members
    DROP CONSTRAINT project_members_role_check;

UPDATE project_members
SET role = CASE role
    WHEN 'project_admin' THEN 'project_admin'
    WHEN 'project_developer' THEN 'project_user'
    WHEN 'developer' THEN 'project_user'
    WHEN 'project_user' THEN 'project_user'
    WHEN 'viewer' THEN 'project_viewer'
    ELSE 'project_viewer'
END;

ALTER TABLE project_members
    ADD CONSTRAINT project_members_role_check CHECK (
        role IN ('project_admin', 'project_user', 'project_viewer')
    );

-- A binary from the immediately preceding release still writes the legacy
-- project-role aliases. Normalize those writes before the canonical CHECK is
-- evaluated so a rolling deployment remains writable without admitting a
-- fourth authority value. Unknown values are intentionally left unchanged and
-- fail closed at project_members_role_check. This is a one-release
-- compatibility mechanism and must be removed by a later explicit migration
-- after every old writer has retired.
CREATE FUNCTION normalize_project_members_role_from_legacy_write()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.role = CASE NEW.role
        WHEN 'project_developer' THEN 'project_user'
        WHEN 'developer' THEN 'project_user'
        WHEN 'viewer' THEN 'project_viewer'
        ELSE NEW.role
    END;
    RETURN NEW;
END;
$$;

CREATE TRIGGER project_members_legacy_role_compatibility
BEFORE INSERT OR UPDATE OF role ON project_members
FOR EACH ROW
EXECUTE FUNCTION normalize_project_members_role_from_legacy_write();

-- The compatibility shadow is the conservative minimum across every live
-- membership. An immediately preceding binary still treats users.role as a
-- route gate, so choosing the strongest membership would over-authorize a
-- weaker project during a rolling upgrade. Organization-role shadows remain
-- unchanged because the two axes are orthogonal.
UPDATE users u
SET role = CASE
    WHEN EXISTS (
        SELECT 1 FROM project_members pm
        WHERE pm.user_id = u.id AND pm.role = 'project_viewer'
    ) THEN 'viewer'
    WHEN EXISTS (
        SELECT 1 FROM project_members pm
        WHERE pm.user_id = u.id AND pm.role = 'project_user'
    ) THEN 'developer'
    WHEN NOT EXISTS (
        SELECT 1 FROM project_members pm
        WHERE pm.user_id = u.id
    ) THEN 'member'
    ELSE 'project_admin'
END
WHERE u.organization_role = 'member';

-- Accepted v2 admin JWTs do not carry organization_role and must derive it
-- from the compatibility shadow. Make an inconsistent dual-column state
-- unrepresentable so that fallback can never yield more organization authority
-- than the canonical column. The member branch contains only conservative
-- legacy shadows, never canonical membership values.
ALTER TABLE users
    ADD CONSTRAINT users_organization_role_compatibility_check CHECK (
        (organization_role = 'platform_admin' AND role IN ('admin', 'platform_admin'))
        OR (organization_role = 'org_admin' AND role = 'org_admin')
        OR (organization_role = 'billing_admin' AND role = 'billing_admin')
        OR (organization_role = 'org_viewer' AND role = 'org_viewer')
        OR (
            organization_role = 'member'
            AND role IN ('member', 'project_admin', 'developer', 'viewer')
        )
    );

COMMIT;
