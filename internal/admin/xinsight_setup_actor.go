package admin

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/principallock"
)

// A durable human consent carries a principal identity and auth epoch, never a
// Cookie, JWT or session. Each step locks and rereads the actual principal and
// then uses the same scope rules as the original human management boundary.
func (h *Handler) reauthorizeXInsightSetupActorTx(ctx context.Context, tx pgx.Tx, actorID, epoch int64, requirement committingActorRequirement) (committingActorState, error) {
	state := committingActorState{UserID: actorID}
	if actorID <= 0 {
		return state, errCommittingActorAuthorizationChanged
	}
	ids := append([]int64{actorID}, requirement.AdditionalUserIDs...)
	if err := principallock.LockUsers(ctx, tx, ids...); err != nil {
		return state, err
	}
	var userStatus, orgStatus string
	err := tx.QueryRow(ctx, `SELECT u.org_id,u.organization_role,u.role,u.auth_epoch,u.status,o.status
 FROM users u JOIN orgs o ON o.id=u.org_id WHERE u.id=$1 FOR UPDATE OF u FOR SHARE OF o`, actorID).Scan(&state.OrgID, &state.OrganizationRole, &state.CompatibilityRole, &state.AuthEpoch, &userStatus, &orgStatus)
	if err != nil || userStatus != "active" || orgStatus != "active" || state.AuthEpoch != epoch {
		return state, errCommittingActorAuthorizationChanged
	}
	return authorizeCommittingActorScopeTx(ctx, tx, state, requirement)
}
