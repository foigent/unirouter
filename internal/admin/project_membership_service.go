package admin

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessgrantlifecycle"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/principallock"
)

var (
	errProjectMembershipUpdate = errors.New("failed to update project memberships")
)

type compatibilityRoleTransition struct {
	BeforeRole      string
	AfterRole       string
	RevokedSessions int64
}

// lockProjectMembershipMutation serializes every path that can change a
// user's role, status, or project memberships. The lock is transaction-scoped,
// so an active project user cannot pass a last-membership check in two
// concurrent requests and commit with no projects.
func lockProjectMembershipMutation(ctx context.Context, tx pgx.Tx, userID int64) error {
	if err := principallock.LockUser(ctx, tx, userID); err != nil {
		return errProjectMembershipUpdate
	}
	return nil
}

// syncUserCompatibilityRoleTx keeps users.role usable by version-2 JWTs and
// older route gates without giving an old binary more authority than the
// user's least-privileged live project membership. Current binaries authorize
// organization_role and project_members independently.
func syncUserCompatibilityRoleTx(ctx context.Context, tx pgx.Tx, userID int64) (compatibilityRoleTransition, error) {
	var transition compatibilityRoleTransition
	var organizationRole, currentRole string
	if err := tx.QueryRow(ctx,
		`SELECT organization_role, role FROM users WHERE id = $1 FOR UPDATE`,
		userID,
	).Scan(&organizationRole, &currentRole); err != nil {
		return transition, err
	}
	transition.BeforeRole = currentRole
	nextRole := currentRole
	if organizationRole != auth.RoleMember {
		// Preserve the legacy admin shadow until that account is explicitly
		// edited; all other organization roles have an exact old-role value.
		if !(currentRole == auth.RoleLegacyAdmin && organizationRole == auth.RolePlatformAdmin) {
			nextRole = organizationRole
		}
	} else {
		conservativeRole, err := projectCompatibilityRoleTx(ctx, tx, userID)
		if err != nil {
			return transition, err
		}
		nextRole = conservativeRole
	}
	transition.AfterRole = nextRole
	if nextRole == currentRole {
		return transition, nil
	}
	if _, err := tx.Exec(ctx,
		`UPDATE users
		 SET role = $2, auth_epoch = auth_epoch + 1, updated_at = NOW()
		 WHERE id = $1`,
		userID, nextRole,
	); err != nil {
		return transition, err
	}
	tag, err := tx.Exec(ctx,
		`UPDATE admin_sessions
		 SET revoked_at = COALESCE(revoked_at, NOW())
		 WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW()`,
		userID,
	)
	if err != nil {
		return transition, err
	}
	transition.RevokedSessions = tag.RowsAffected()
	return transition, nil
}

func projectCompatibilityRoleTx(ctx context.Context, tx pgx.Tx, userID int64) (string, error) {
	var conservativeRole string
	err := tx.QueryRow(ctx,
		`SELECT CASE
		    WHEN COUNT(*) = 0 THEN 'member'
		    WHEN bool_or(pm.role IN ('project_viewer', 'viewer')) THEN 'viewer'
		    WHEN bool_or(pm.role IN ('project_user', 'project_developer', 'developer')) THEN 'developer'
		    WHEN bool_and(pm.role = 'project_admin') THEN 'project_admin'
		    ELSE 'member'
		 END
		 FROM project_members pm
		 WHERE pm.user_id = $1`,
		userID,
	).Scan(&conservativeRole)
	return conservativeRole, err
}

func disablePersonalCredentialsForMembershipTx(ctx context.Context, tx pgx.Tx, userID, orgID, projectID, actorUserID int64, lifecycleReason string) (int64, error) {
	result, err := accessgrantlifecycle.RevokeUser(
		ctx, tx, orgID, projectID, userID, actorUserID,
		lifecycleReason, time.Now().UTC(),
	)
	if err != nil {
		return 0, err
	}
	return result.CredentialFamilies + result.SecretVersions + result.LegacyKeys, nil
}

func listUserProjectIDsTx(ctx context.Context, tx pgx.Tx, userID, orgID int64) ([]int64, error) {
	rows, err := tx.Query(ctx,
		`SELECT pm.project_id
		 FROM project_members pm
		 JOIN projects p ON p.id = pm.project_id
		 WHERE pm.user_id = $1 AND p.org_id = $2
		 ORDER BY pm.project_id
		 FOR SHARE OF pm`,
		userID, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projectIDs := make([]int64, 0)
	for rows.Next() {
		var projectID int64
		if err := rows.Scan(&projectID); err != nil {
			return nil, err
		}
		projectIDs = append(projectIDs, projectID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return projectIDs, nil
}
