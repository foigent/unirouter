package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/principallock"
)

var (
	errCommittingActorAuthorizationChanged = errors.New("committing actor authorization changed")
	errCommittingActorScopeNotFound        = errors.New("authorized target scope not found")
)

type committingActorProjectRequirement struct {
	ProjectID    int64
	AllowedRoles []string
	MustBeActive bool
}

// committingActorRequirement is deliberately narrower than a policy engine.
// It describes the two authorization axes used by one committing mutation:
// accepted organization roles and, where applicable, accepted live roles for
// one exact project. Additional user IDs participate only in the deterministic
// user-mutation lock order; they never grant authority.
type committingActorRequirement struct {
	TargetOrgID                             int64
	OrganizationForUpdate                   bool
	AllowedOrganizationRoles                []string
	Project                                 *committingActorProjectRequirement
	AllowSelfRequestWithoutMembershipUserID int64
	AdditionalUserIDs                       []int64
	AdditionalWorkloadIDs                   []int64
}

type committingActorState struct {
	UserID            int64
	OrgID             int64
	OrganizationRole  string
	CompatibilityRole string
	AuthEpoch         int64
	TargetOrgID       int64
	ProjectRole       string
}

// reauthorizeCommittingActorTx establishes the commit order shared by every
// protected control-plane mutation. It locks the actor's user-mutation lane,
// rereads and locks the live principal, locks the exact admitted session, then
// resolves the requested organization/project scope from live state. Actor
// demotion, disable, epoch changes, exact-session logout, and membership
// revocation therefore either commit before this control and are observed, or
// wait until this already-authorized transaction finishes.
func (h *Handler) reauthorizeCommittingActorTx(
	ctx context.Context,
	tx pgx.Tx,
	requirement committingActorRequirement,
) (committingActorState, error) {
	var state committingActorState
	constrainedRequirement, constraintErr := constrainProjectCommitRequirementToCustomer(ctx, requirement)
	if constraintErr != nil {
		return state, constraintErr
	}
	requirement = constrainedRequirement
	if isUnauthenticatedUnitContext(ctx) {
		if err := principallock.LockUsers(ctx, tx, requirement.AdditionalUserIDs...); err != nil {
			return state, err
		}
		if err := principallock.LockWorkloads(ctx, tx, requirement.AdditionalWorkloadIDs...); err != nil {
			return state, err
		}
		state.TargetOrgID = requirement.TargetOrgID
		if requirement.Project != nil && state.TargetOrgID <= 0 {
			if err := tx.QueryRow(ctx, `SELECT org_id FROM projects WHERE id=$1`, requirement.Project.ProjectID).Scan(&state.TargetOrgID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return state, errCommittingActorScopeNotFound
				}
				return state, err
			}
		}
		return state, nil
	}

	actorUserID := middleware.GetUserIDFromContext(ctx)
	actorOrgID := middleware.GetOrgIDFromContext(ctx)
	expectedCompatibilityRole := middleware.GetUserRoleFromContext(ctx)
	expectedOrganizationRole := actorOrganizationRole(ctx)
	expectedAuthEpoch, hasAuthEpoch := middleware.GetAuthEpochFromContext(ctx)
	sessionHash := middleware.GetAdminSessionHashFromContext(ctx)
	if actorUserID <= 0 || actorOrgID <= 0 || expectedCompatibilityRole == "" ||
		expectedOrganizationRole == "" || !hasAuthEpoch || len(sessionHash) == 0 {
		return state, errCommittingActorAuthorizationChanged
	}
	state.UserID = actorUserID

	lockUserIDs := append([]int64{actorUserID}, requirement.AdditionalUserIDs...)
	if err := principallock.LockUsers(ctx, tx, lockUserIDs...); err != nil {
		return state, fmt.Errorf("lock committing authorization users: %w", err)
	}
	if err := principallock.LockWorkloads(ctx, tx, requirement.AdditionalWorkloadIDs...); err != nil {
		return state, fmt.Errorf("lock committing authorization workloads: %w", err)
	}

	var userStatus, organizationStatus string
	if err := tx.QueryRow(ctx, `
		SELECT u.org_id, u.organization_role, u.role, u.status, u.auth_epoch, o.status
		  FROM users u
		  JOIN orgs o ON o.id=u.org_id
		 WHERE u.id=$1
		 FOR UPDATE OF u FOR SHARE OF o`, actorUserID).Scan(
		&state.OrgID, &state.OrganizationRole, &state.CompatibilityRole,
		&userStatus, &state.AuthEpoch, &organizationStatus,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return state, errCommittingActorAuthorizationChanged
		}
		return state, fmt.Errorf("read committing actor: %w", err)
	}
	if state.OrgID != actorOrgID || state.OrganizationRole != expectedOrganizationRole ||
		state.CompatibilityRole != expectedCompatibilityRole || state.AuthEpoch != expectedAuthEpoch ||
		userStatus != "active" || organizationStatus != "active" {
		return state, errCommittingActorAuthorizationChanged
	}

	var sessionID int64
	if err := tx.QueryRow(ctx, `
		SELECT id
		  FROM admin_sessions
		 WHERE user_id=$1 AND sid_hash=$2
		   AND revoked_at IS NULL
		   AND issued_at <= NOW() AND expires_at > NOW()
		 FOR UPDATE`, actorUserID, sessionHash).Scan(&sessionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return state, errCommittingActorAuthorizationChanged
		}
		return state, fmt.Errorf("read committing actor session: %w", err)
	}

	return authorizeCommittingActorScopeTx(ctx, tx, state, requirement)
}

func authorizeCommittingActorScopeTx(ctx context.Context, tx pgx.Tx, state committingActorState, requirement committingActorRequirement) (committingActorState, error) {
	if requirement.Project == nil {
		if requirement.TargetOrgID <= 0 {
			return state, errCommittingActorScopeNotFound
		}
		if !auth.RoleIn(state.OrganizationRole, requirement.AllowedOrganizationRoles...) ||
			(state.OrganizationRole != auth.RolePlatformAdmin && state.OrgID != requirement.TargetOrgID) {
			return state, errCommittingActorScopeNotFound
		}
		var targetOrganizationStatus string
		orgLock := "FOR SHARE"
		if requirement.OrganizationForUpdate {
			orgLock = "FOR UPDATE"
		}
		if err := tx.QueryRow(ctx, `SELECT status FROM orgs WHERE id=$1 `+orgLock, requirement.TargetOrgID).Scan(&targetOrganizationStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return state, errCommittingActorScopeNotFound
			}
			return state, fmt.Errorf("read committing target organization: %w", err)
		}
		if targetOrganizationStatus != "active" {
			return state, errCommittingActorScopeNotFound
		}
		state.TargetOrgID = requirement.TargetOrgID
		return state, nil
	}

	projectRequirement := requirement.Project
	if projectRequirement.ProjectID <= 0 {
		return state, errCommittingActorScopeNotFound
	}
	organizationAuthority := auth.RoleIn(state.OrganizationRole, requirement.AllowedOrganizationRoles...)
	if organizationAuthority {
		var projectStatus string
		err := tx.QueryRow(ctx, `
			SELECT p.org_id, p.status
			  FROM projects p
			  JOIN orgs o ON o.id=p.org_id AND o.status='active'
			 WHERE p.id=$1
			   AND ($2::bigint=0 OR p.org_id=$2)
			   AND ($3::boolean OR p.org_id=$4)
			 FOR UPDATE OF p FOR SHARE OF o`, projectRequirement.ProjectID, requirement.TargetOrgID,
			state.OrganizationRole == auth.RolePlatformAdmin, state.OrgID).Scan(&state.TargetOrgID, &projectStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return state, errCommittingActorScopeNotFound
		}
		if err != nil {
			return state, fmt.Errorf("read committing project scope: %w", err)
		}
		if projectRequirement.MustBeActive && projectStatus != "active" {
			return state, errCommittingActorScopeNotFound
		}
		return state, nil
	}

	// A human access request must be able to begin before calling membership
	// exists. This exception authorizes only submission of the actor's own
	// request for one exact active project in the same organization. No
	// membership produces first_cross_project; a live viewer membership is
	// returned to policy evaluation so promotion produces role_elevation. An
	// existing caller falls through to the ordinary live-membership gate.
	if requirement.AllowSelfRequestWithoutMembershipUserID == state.UserID &&
		state.OrganizationRole == auth.RoleMember {
		var projectStatus string
		err := tx.QueryRow(ctx, `
			SELECT p.org_id,p.status
			  FROM projects p
			  JOIN orgs o ON o.id=p.org_id AND o.status='active'
			 WHERE p.id=$1 AND p.org_id=$2
			   AND ($3::bigint=0 OR p.org_id=$3)
			 FOR UPDATE OF p FOR SHARE OF o`, projectRequirement.ProjectID, state.OrgID,
			requirement.TargetOrgID).Scan(&state.TargetOrgID, &projectStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return state, errCommittingActorScopeNotFound
		}
		if err != nil {
			return state, fmt.Errorf("read self-request project scope: %w", err)
		}
		if projectRequirement.MustBeActive && projectStatus != "active" {
			return state, errCommittingActorScopeNotFound
		}
		var storedRole string
		err = tx.QueryRow(ctx, `
			SELECT role FROM project_members
			 WHERE project_id=$1 AND user_id=$2
			 FOR SHARE`, projectRequirement.ProjectID, state.UserID).Scan(&storedRole)
		if errors.Is(err, pgx.ErrNoRows) {
			return state, nil
		}
		if err != nil {
			return state, fmt.Errorf("read self-request membership: %w", err)
		}
		canonicalRole, ok := auth.NormalizeProjectMembershipRole(storedRole)
		if ok && canonicalRole == auth.ProjectRoleViewer {
			state.ProjectRole = canonicalRole
			return state, nil
		}
	}

	if len(projectRequirement.AllowedRoles) == 0 {
		return state, errCommittingActorScopeNotFound
	}
	acceptedRoles := projectMembershipStorageValues(projectRequirement.AllowedRoles)
	var projectStatus, storedProjectRole string
	err := tx.QueryRow(ctx, `
		SELECT p.org_id, p.status, pm.role
		  FROM projects p
		  JOIN orgs o ON o.id=p.org_id AND o.status='active'
		  JOIN project_members pm ON pm.project_id=p.id AND pm.user_id=$2
		 WHERE p.id=$1 AND p.org_id=$3
		   AND ($4::bigint=0 OR p.org_id=$4)
		   AND pm.role=ANY($5::text[])
		 FOR UPDATE OF p FOR SHARE OF o, pm`, projectRequirement.ProjectID, state.UserID, state.OrgID,
		requirement.TargetOrgID, acceptedRoles).Scan(&state.TargetOrgID, &projectStatus, &storedProjectRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, errCommittingActorScopeNotFound
	}
	if err != nil {
		return state, fmt.Errorf("read committing project membership: %w", err)
	}
	state.ProjectRole, _ = auth.NormalizeProjectMembershipRole(storedProjectRole)
	if !auth.RoleIn(state.ProjectRole, projectRequirement.AllowedRoles...) ||
		(projectRequirement.MustBeActive && projectStatus != "active") {
		return state, errCommittingActorScopeNotFound
	}
	return state, nil
}

// constrainProjectCommitRequirementToCustomer turns the canonical customer
// context into a transaction-time invariant for every project mutation. This
// prevents a deployment operator from bypassing handler-level scoping by using
// a globally unique project ID. Organization-only/deployment mutations are
// deliberately left to their explicit target contracts.
func constrainProjectCommitRequirementToCustomer(
	ctx context.Context,
	requirement committingActorRequirement,
) (committingActorRequirement, error) {
	if requirement.Project == nil {
		return requirement, nil
	}
	customer, present := middleware.GetCustomerContextFromContext(ctx)
	if !present {
		return requirement, nil
	}
	if !customer.Ready() || (requirement.TargetOrgID > 0 && requirement.TargetOrgID != customer.OrganizationID) {
		return committingActorRequirement{}, errCommittingActorScopeNotFound
	}
	requirement.TargetOrgID = customer.OrganizationID
	return requirement, nil
}

func projectMembershipStorageValues(canonicalRoles []string) []string {
	values := make([]string, 0, len(canonicalRoles)+3)
	seen := make(map[string]struct{}, len(canonicalRoles)+3)
	add := func(value string) {
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	for _, role := range canonicalRoles {
		switch role {
		case auth.ProjectRoleAdmin:
			add(auth.ProjectRoleAdmin)
		case auth.ProjectRoleUser:
			add(auth.ProjectRoleUser)
			add(auth.LegacyProjectRoleDeveloper)
			add(auth.RoleDeveloper)
		case auth.ProjectRoleViewer:
			add(auth.ProjectRoleViewer)
			add(auth.RoleViewer)
		}
	}
	return values
}
