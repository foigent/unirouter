package admin

import (
	"context"
	"net/http"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

func withTestActor(req *http.Request, userID, orgID int64, role string) *http.Request {
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, orgID)
	ctx = context.WithValue(ctx, middleware.CtxUserRole, role)
	return req.WithContext(ctx)
}

func withTestBillingActor(req *http.Request) *http.Request {
	return withTestActor(req, 1, 1, auth.RoleBillingAdmin)
}
