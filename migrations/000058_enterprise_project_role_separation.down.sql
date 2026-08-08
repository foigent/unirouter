BEGIN;

-- Stabilize every row that participates in representability before checking
-- it. A writer already in flight must finish before these checks take their
-- READ COMMITTED snapshot; later writers then wait until rollback completes.
LOCK TABLE users, project_members IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000058_enterprise_project_role_separation'
    ) THEN
        RAISE EXCEPTION 'enterprise role separation rollback refused: later migrations must be reversed first';
    END IF;

    -- The previous binary can express only one project role per account and
    -- cannot safely represent a member account with no project. Never invent
    -- organization read authority or silently collapse heterogeneous roles.
    IF EXISTS (
        SELECT 1
        FROM users u
        LEFT JOIN project_members pm ON pm.user_id = u.id
        WHERE u.organization_role = 'member'
        GROUP BY u.id
        HAVING COUNT(pm.project_id) = 0 OR COUNT(DISTINCT pm.role) <> 1
    ) THEN
        RAISE EXCEPTION 'enterprise role separation rollback refused: member project roles are not representable by users.role';
    END IF;

    -- Organization and project roles are orthogonal in the new model. Any
    -- explicit membership held by a non-member organization role cannot be
    -- represented by the old single users.role column without dropping one
    -- axis of authority.
    IF EXISTS (
        SELECT 1
        FROM users u
        JOIN project_members pm ON pm.user_id = u.id
        WHERE u.organization_role <> 'member'
    ) THEN
        RAISE EXCEPTION 'enterprise role separation rollback refused: organization role with project membership is not representable by users.role';
    END IF;
END;
$$;

DROP TRIGGER users_legacy_role_organization_role_compatibility ON users;
DROP FUNCTION sync_users_organization_role_from_legacy_role_write();
DROP TRIGGER project_members_legacy_role_compatibility ON project_members;
DROP FUNCTION normalize_project_members_role_from_legacy_write();

UPDATE users u
SET role = CASE member_roles.role
    WHEN 'project_admin' THEN 'project_admin'
    WHEN 'project_user' THEN 'developer'
    WHEN 'project_viewer' THEN 'viewer'
END
FROM (
    SELECT user_id, MIN(role) AS role
    FROM project_members
    GROUP BY user_id
) member_roles
WHERE u.id = member_roles.user_id
  AND u.organization_role = 'member';

ALTER TABLE project_members
    DROP CONSTRAINT project_members_role_check;

UPDATE project_members
SET role = CASE role
    WHEN 'project_user' THEN 'project_developer'
    ELSE role
END;

ALTER TABLE project_members
    ADD CONSTRAINT project_members_role_check CHECK (
        role IN ('project_admin', 'project_developer', 'project_viewer')
    );

DROP INDEX idx_users_org_organization_role;

ALTER TABLE users
	DROP CONSTRAINT users_organization_role_compatibility_check,
    DROP CONSTRAINT users_organization_role_check,
    DROP COLUMN organization_role;

DELETE FROM schema_migrations
WHERE version = '000058_enterprise_project_role_separation';

COMMIT;
