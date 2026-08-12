package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

// policySelector is shared by the limit and guardrail control planes. A
// Grant-bound physical key is canonicalized to its logical credential family,
// so rotation cannot shed a policy. Legacy-unbound keys remain physical.
type policySelector struct {
	OrgID              *int64
	ProjectID          *int64
	APIKeyID           *int64
	AccessCredentialID *int64
}

// reauthorizePolicySelectorMutationTx reuses the shared committing-actor lane
// and then locks the exact canonical selector. A demotion/session revocation or
// selector move/removal therefore either commits first and is observed, or
// waits until this already-authorized policy transaction commits.
func (h *Handler) reauthorizePolicySelectorMutationTx(ctx context.Context, tx pgx.Tx, selector policySelector) error {
	if isUnauthenticatedUnitContext(ctx) {
		return nil
	}
	targetOrgID := pointerValue(selector.OrgID)
	if targetOrgID == 0 && auth.IsPlatformAdmin(actorOrganizationRole(ctx)) {
		// A deployment-global policy has no target organization selector. Use
		// the platform actor's own organization only as the live principal and
		// exact-session commit lane; it does not scope or rewrite the policy.
		targetOrgID = middleware.GetOrgIDFromContext(ctx)
	}
	requirement := committingActorRequirement{
		TargetOrgID:              targetOrgID,
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
	}
	if selector.ProjectID != nil {
		requirement.Project = &committingActorProjectRequirement{ProjectID: *selector.ProjectID, MustBeActive: true}
	}
	if _, err := h.reauthorizeCommittingActorTx(ctx, tx, requirement); err != nil {
		if errors.Is(err, errCommittingActorScopeNotFound) {
			return errPolicySelectorNotFound
		}
		if errors.Is(err, errCommittingActorAuthorizationChanged) {
			return errLimitPolicyForbidden
		}
		return err
	}

	var id int64
	switch {
	case selector.AccessCredentialID != nil:
		if selector.ProjectID == nil || selector.OrgID == nil {
			return errPolicySelectorNotFound
		}
		if err := tx.QueryRow(ctx, `
			SELECT id FROM access_credentials
			WHERE id=$1 AND org_id=$2 AND project_id=$3 AND status='active'
			FOR SHARE`, *selector.AccessCredentialID, *selector.OrgID, *selector.ProjectID).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errPolicySelectorNotFound
			}
			return err
		}
	case selector.APIKeyID != nil:
		if selector.OrgID == nil {
			return errPolicySelectorNotFound
		}
		projectID := int64(0)
		if selector.ProjectID != nil {
			projectID = *selector.ProjectID
		}
		if err := tx.QueryRow(ctx, `
			SELECT id FROM api_keys
			WHERE id=$1 AND org_id=$2
			  AND ($3::BIGINT=0 OR project_id=$3)
			  AND access_credential_id IS NULL
			FOR SHARE`, *selector.APIKeyID, *selector.OrgID, projectID).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errPolicySelectorNotFound
			}
			return err
		}
	}
	return nil
}

func pointerValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

var errPolicySelectorNotFound = errors.New("policy selector not found")

func validatePolicySelectorMutation(apiKeyID, accessCredentialID *int64) error {
	if apiKeyID != nil && accessCredentialID != nil {
		return fmt.Errorf("api_key_id and access_credential_id are mutually exclusive")
	}
	return nil
}

func (h *Handler) canonicalizePolicySelector(ctx context.Context, selector *policySelector) error {
	if selector == nil {
		return fmt.Errorf("invalid policy selector")
	}
	if selector.APIKeyID != nil && selector.AccessCredentialID != nil {
		return fmt.Errorf("api_key_id and access_credential_id are mutually exclusive")
	}
	hasObjectSelector := selector.ProjectID != nil || selector.APIKeyID != nil || selector.AccessCredentialID != nil
	role := actorOrganizationRole(ctx)
	if auth.IsPlatformAdmin(role) {
		if selector.OrgID != nil && !actorCanAccessOrg(ctx, *selector.OrgID) {
			return errLimitPolicyForbidden
		}
		if hasObjectSelector && (selector.OrgID == nil || *selector.OrgID <= 0) {
			return fmt.Errorf("org_id is required for an object selector")
		}
	} else {
		actorOrgID := middleware.GetOrgIDFromContext(ctx)
		if !auth.IsOrgAdmin(role) || actorOrgID <= 0 {
			return errLimitPolicyForbidden
		}
		if selector.OrgID == nil {
			selector.OrgID = int64Pointer(actorOrgID)
		} else if *selector.OrgID != actorOrgID {
			if hasObjectSelector {
				return errPolicySelectorNotFound
			}
			return errLimitPolicyForbidden
		}
	}
	if selector.ProjectID != nil {
		if *selector.ProjectID <= 0 {
			return fmt.Errorf("project_id must be positive")
		}
		var found int64
		if err := h.db.QueryRow(ctx, `SELECT id FROM projects WHERE id=$1 AND org_id=$2`, *selector.ProjectID, *selector.OrgID).Scan(&found); err != nil {
			return errPolicySelectorNotFound
		}
	}
	if selector.APIKeyID != nil {
		if *selector.APIKeyID <= 0 {
			return fmt.Errorf("api_key_id must be positive")
		}
		var projectID, credentialID sql.NullInt64
		expectedProjectID := int64(0)
		if selector.ProjectID != nil {
			expectedProjectID = *selector.ProjectID
		}
		if err := h.db.QueryRow(ctx, `
			SELECT project_id,access_credential_id
			FROM api_keys
			WHERE id=$1 AND org_id=$2 AND ($3::BIGINT=0 OR project_id=$3)`,
			*selector.APIKeyID, *selector.OrgID, expectedProjectID).Scan(&projectID, &credentialID); err != nil {
			return errPolicySelectorNotFound
		}
		if err := bindPolicySelectorProject(selector, projectID); err != nil {
			return errPolicySelectorNotFound
		}
		if credentialID.Valid {
			value := credentialID.Int64
			selector.AccessCredentialID = &value
			selector.APIKeyID = nil
		}
	}
	if selector.AccessCredentialID != nil {
		if *selector.AccessCredentialID <= 0 {
			return fmt.Errorf("access_credential_id must be positive")
		}
		expectedProjectID := int64(0)
		if selector.ProjectID != nil {
			expectedProjectID = *selector.ProjectID
		}
		var projectID int64
		if err := h.db.QueryRow(ctx, `
			SELECT project_id
			FROM access_credentials
			WHERE id=$1 AND org_id=$2 AND status='active'
			  AND ($3::BIGINT=0 OR project_id=$3)`,
			*selector.AccessCredentialID, *selector.OrgID, expectedProjectID).Scan(&projectID); err != nil {
			return errPolicySelectorNotFound
		}
		if err := bindPolicySelectorProject(selector, sql.NullInt64{Int64: projectID, Valid: true}); err != nil {
			return errPolicySelectorNotFound
		}
	}
	return nil
}

func int64Pointer(value int64) *int64 {
	return &value
}

func bindPolicySelectorProject(selector *policySelector, projectID sql.NullInt64) error {
	if !projectID.Valid || projectID.Int64 <= 0 {
		if selector.ProjectID != nil {
			return errPolicySelectorNotFound
		}
		return nil
	}
	if selector.ProjectID == nil {
		value := projectID.Int64
		selector.ProjectID = &value
		return nil
	}
	if *selector.ProjectID != projectID.Int64 {
		return errPolicySelectorNotFound
	}
	return nil
}
