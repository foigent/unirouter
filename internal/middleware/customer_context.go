package middleware

import (
	"context"
	"net/http"

	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/db"
)

const (
	CtxCustomerContext      contextKey = "customer_context"
	CtxCustomerOrgID        contextKey = "customer_org_id"
	CtxCustomerContextState contextKey = "customer_context_state"
)

// CustomerContext resolves and injects deployment customer state without
// rejecting the request. Customer-domain handlers enforce readiness; purely
// deployment-scoped routes remain available for repair and bootstrap.
func CustomerContext(q db.DBQuerier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			snapshot, err := customercontext.Resolve(r.Context(), q)
			if err != nil {
				snapshot = customercontext.Snapshot{State: customercontext.StateUnavailable}
			}
			ctx := context.WithValue(r.Context(), CtxCustomerContext, snapshot)
			ctx = context.WithValue(ctx, CtxCustomerContextState, snapshot.State)
			if snapshot.OrganizationID > 0 {
				ctx = context.WithValue(ctx, CtxCustomerOrgID, snapshot.OrganizationID)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetCustomerContextFromContext(ctx context.Context) (customercontext.Snapshot, bool) {
	snapshot, ok := ctx.Value(CtxCustomerContext).(customercontext.Snapshot)
	return snapshot, ok
}

func GetCustomerOrgIDFromContext(ctx context.Context) int64 {
	organizationID, _ := ctx.Value(CtxCustomerOrgID).(int64)
	return organizationID
}

func GetCustomerContextStateFromContext(ctx context.Context) customercontext.State {
	state, _ := ctx.Value(CtxCustomerContextState).(customercontext.State)
	return state
}
