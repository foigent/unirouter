// Package accesssponsor owns the live accountability invariant for Workload
// AccessRequests and AccessGrants. Sponsorship is evidence and visibility, not
// runtime calling authority, but supported principal transitions may not leave
// a pending or running Workload without a currently accountable sponsor.
package accesssponsor

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/principallock"
)

var (
	ErrNotFound               = errors.New("workload sponsor not found")
	ErrInactive               = errors.New("workload sponsor inactive")
	ErrNotAuthorized          = errors.New("workload sponsor not authorized")
	ErrPostTransitionOrphaned = errors.New("workload sponsorship would be orphaned")
)

type MembershipOverride struct {
	ProjectID int64
	Role      string
	Present   bool
}

// PostTransitionState describes the exact principal state that will exist if
// the surrounding transaction commits. MembershipOverride is used for one
// membership upsert/removal; all other project memberships remain live reads.
type PostTransitionState struct {
	UserID             int64
	OrgID              int64
	Status             string
	OrganizationRole   string
	MembershipOverride *MembershipOverride
}

type sponsoredLane struct {
	orgID     int64
	projectID int64
}

// ValidateCurrentAuthority locks one sponsor lane and validates the current
// account/organization-role/project-membership authority used when creating a
// Workload request or Grant revision.
func ValidateCurrentAuthority(ctx context.Context, tx pgx.Tx, userID, orgID, projectID int64) error {
	if userID <= 0 || orgID <= 0 || projectID <= 0 {
		return ErrNotFound
	}
	if err := principallock.LockUser(ctx, tx, userID); err != nil {
		return fmt.Errorf("lock workload sponsor: %w", err)
	}
	var status, organizationRole string
	if err := tx.QueryRow(ctx, `
		SELECT status,organization_role
		FROM users
		WHERE id=$1 AND org_id=$2
		FOR SHARE`, userID, orgID).Scan(&status, &organizationRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read workload sponsor: %w", err)
	}
	if status != "active" {
		return ErrInactive
	}
	if auth.RoleIn(organizationRole, auth.RolePlatformAdmin, auth.RoleOrgAdmin) {
		return nil
	}
	var role string
	if err := tx.QueryRow(ctx, `
		SELECT pm.role
		FROM project_members pm
		JOIN projects p ON p.id=pm.project_id AND p.org_id=$3
		WHERE pm.project_id=$1 AND pm.user_id=$2
		FOR SHARE OF pm`, projectID, userID, orgID).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAuthorized
		}
		return fmt.Errorf("read workload sponsor membership: %w", err)
	}
	if !callingMembership(role) {
		return ErrNotAuthorized
	}
	return nil
}

// ValidatePostTransition locks every pending/running Workload responsibility
// for this sponsor and proves that the proposed principal state remains valid.
// It performs no writes. Callers must invoke it before account, membership,
// audit, or outbox mutation.
func ValidatePostTransition(ctx context.Context, tx pgx.Tx, state PostTransitionState) error {
	if state.UserID <= 0 || state.OrgID <= 0 || state.OrganizationRole == "" {
		return fmt.Errorf("invalid sponsor post-transition state")
	}
	if err := principallock.LockUser(ctx, tx, state.UserID); err != nil {
		return fmt.Errorf("lock sponsor transition: %w", err)
	}
	lanes, err := lockSponsoredLanes(ctx, tx, state.UserID)
	if err != nil {
		return err
	}
	if len(lanes) == 0 {
		return nil
	}
	if state.Status != "active" {
		return ErrPostTransitionOrphaned
	}
	for _, lane := range lanes {
		if lane.orgID != state.OrgID {
			return ErrPostTransitionOrphaned
		}
		if auth.RoleIn(state.OrganizationRole, auth.RolePlatformAdmin, auth.RoleOrgAdmin) {
			continue
		}
		role, present, err := membershipAfterTransition(ctx, tx, state, lane.projectID)
		if err != nil {
			return err
		}
		if !present || !callingMembership(role) {
			return ErrPostTransitionOrphaned
		}
	}
	return nil
}

func lockSponsoredLanes(ctx context.Context, tx pgx.Tx, userID int64) ([]sponsoredLane, error) {
	lanes := make([]sponsoredLane, 0)
	// Every scope approval locks its access_requests row before the target
	// access_grants row. Keep sponsor transitions on that same total order after
	// the shared principal advisory lane so decision and containment cannot form
	// a request<->Grant row-lock cycle.
	rows, err := tx.Query(ctx, `
		SELECT org_id,project_id
		FROM access_requests
		WHERE subject_type='workload_application'
		  AND sponsor_user_id=$1
		  AND status='needs_review'
		ORDER BY id
		FOR UPDATE`, userID)
	if err != nil {
		return nil, fmt.Errorf("lock sponsored workload requests: %w", err)
	}
	for rows.Next() {
		var lane sponsoredLane
		if err := rows.Scan(&lane.orgID, &lane.projectID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan sponsored workload request: %w", err)
		}
		lanes = append(lanes, lane)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate sponsored workload requests: %w", err)
	}
	rows.Close()

	rows, err = tx.Query(ctx, `
		SELECT org_id,project_id
		FROM access_grants
		WHERE subject_type='workload_application'
		  AND sponsor_user_id=$1
		  AND status IN ('active','suspended')
		ORDER BY id
		FOR UPDATE`, userID)
	if err != nil {
		return nil, fmt.Errorf("lock sponsored workload grants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var lane sponsoredLane
		if err := rows.Scan(&lane.orgID, &lane.projectID); err != nil {
			return nil, fmt.Errorf("scan sponsored workload grant: %w", err)
		}
		lanes = append(lanes, lane)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sponsored workload grants: %w", err)
	}
	return lanes, nil
}

func membershipAfterTransition(ctx context.Context, tx pgx.Tx, state PostTransitionState, projectID int64) (string, bool, error) {
	if override := state.MembershipOverride; override != nil && override.ProjectID == projectID {
		return override.Role, override.Present, nil
	}
	var role string
	if err := tx.QueryRow(ctx, `
		SELECT pm.role
		FROM project_members pm
		JOIN projects p ON p.id=pm.project_id AND p.org_id=$3
		WHERE pm.project_id=$1 AND pm.user_id=$2
		FOR SHARE OF pm`, projectID, state.UserID, state.OrgID).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read sponsor post-transition membership: %w", err)
	}
	return role, true, nil
}

func callingMembership(role string) bool {
	canonical, ok := auth.NormalizeProjectMembershipRole(role)
	return ok && (canonical == auth.ProjectRoleAdmin || canonical == auth.ProjectRoleUser)
}
