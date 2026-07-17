package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

const projectMembershipMutationLockBase int64 = 4120000000000000000

var (
	errProjectIDsInvalid       = errors.New("project_ids must contain positive project IDs")
	errProjectIDsRequired      = errors.New("an active project role must belong to at least one project")
	errProjectIDsNotAllowed    = errors.New("project_ids are only valid for project roles")
	errProjectOrganization     = errors.New("all projects must belong to the user's organization")
	errProjectMembershipUpdate = errors.New("failed to update project memberships")
)

// normalizeProjectIDs gives every membership mutation a stable lock and write
// order. Duplicate IDs are accepted from UI multi-select controls and reduced
// to one membership; non-positive IDs fail closed.
func normalizeProjectIDs(projectIDs []int64) ([]int64, error) {
	seen := make(map[int64]struct{}, len(projectIDs))
	normalized := make([]int64, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		if projectID <= 0 {
			return nil, errProjectIDsInvalid
		}
		if _, exists := seen[projectID]; exists {
			continue
		}
		seen[projectID] = struct{}{}
		normalized = append(normalized, projectID)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	return normalized, nil
}

// lockProjectMembershipMutation serializes every path that can change a
// user's role, status, or project memberships. The lock is transaction-scoped,
// so an active project user cannot pass a last-membership check in two
// concurrent requests and commit with no projects.
func lockProjectMembershipMutation(ctx context.Context, tx pgx.Tx, userID int64) error {
	if userID <= 0 || userID > (1<<63-1)-projectMembershipMutationLockBase {
		return errProjectMembershipUpdate
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, projectMembershipMutationLockBase+userID)
	return err
}

func canManageProjectMembersTx(ctx context.Context, tx pgx.Tx, projectID, orgID int64) (bool, error) {
	role := actorRole(ctx)
	switch role {
	case auth.RolePlatformAdmin:
		return true, nil
	case auth.RoleOrgAdmin:
		return actorCanAccessOrg(ctx, orgID), nil
	case auth.RoleProjectAdmin:
		if !actorCanAccessOrg(ctx, orgID) {
			return false, nil
		}
		var membershipRole string
		err := tx.QueryRow(ctx,
			`SELECT pm.role
			 FROM project_members pm
			 JOIN projects p ON p.id = pm.project_id
			 JOIN users u ON u.id = pm.user_id AND u.org_id = p.org_id
			 WHERE pm.project_id = $1 AND pm.user_id = $2 AND p.org_id = $3
			   AND u.role = $4 AND u.status = 'active' AND pm.role = $5
			 FOR SHARE OF pm, u`,
			projectID, middleware.GetUserIDFromContext(ctx), orgID,
			auth.RoleProjectAdmin, projectMemberAdmin,
		).Scan(&membershipRole)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return err == nil && membershipRole == projectMemberAdmin, err
	default:
		return false, nil
	}
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

func validateProjectIDsForOrgTx(ctx context.Context, tx pgx.Tx, orgID int64, projectIDs []int64) error {
	if len(projectIDs) == 0 {
		return nil
	}
	var count int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM projects
		 WHERE org_id = $1 AND id = ANY($2::bigint[])`,
		orgID, projectIDs,
	).Scan(&count); err != nil {
		return err
	}
	if count != len(projectIDs) {
		return errProjectOrganization
	}
	return nil
}

func validateProjectRoleMembership(role, status string, projectIDs []int64) error {
	if !auth.IsProjectScopedRole(role) {
		if len(projectIDs) > 0 {
			return errProjectIDsNotAllowed
		}
		return nil
	}
	if status == "active" && len(projectIDs) == 0 {
		return errProjectIDsRequired
	}
	return nil
}

func projectMembershipErrorResponse(err error) (int, string) {
	switch {
	case errors.Is(err, errProjectIDsInvalid), errors.Is(err, errProjectIDsNotAllowed):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, errProjectIDsRequired):
		return http.StatusConflict, err.Error()
	case errors.Is(err, errProjectOrganization):
		return http.StatusForbidden, err.Error()
	default:
		return http.StatusInternalServerError, "Failed to update project memberships"
	}
}

func replaceUserProjectMembershipsTx(ctx context.Context, tx pgx.Tx, userID, orgID int64, userRole string, projectIDs []int64) error {
	membershipRole, projectRole := projectMemberRoleForUserRole(userRole)
	if !projectRole {
		if _, err := tx.Exec(ctx, `DELETE FROM project_members WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("%w: %v", errProjectMembershipUpdate, err)
		}
		return nil
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM project_members
		 WHERE user_id = $1
		   AND NOT (project_id = ANY($2::bigint[]))`,
		userID, projectIDs,
	); err != nil {
		return fmt.Errorf("%w: %v", errProjectMembershipUpdate, err)
	}
	if len(projectIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role, created_at, updated_at)
		 SELECT project_id, $2, $3, NOW(), NOW()
		 FROM unnest($1::bigint[]) AS project_id
		 ON CONFLICT (project_id, user_id) DO UPDATE
		 SET role = EXCLUDED.role, updated_at = NOW()`,
		projectIDs, userID, membershipRole,
	); err != nil {
		return fmt.Errorf("%w: %v", errProjectMembershipUpdate, err)
	}
	return nil
}
