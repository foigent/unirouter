//go:build integration

package main

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil/sourcereceiver"
	"net/http"
	"testing"
)

func sourceHTTPAuthenticatedPull(t *testing.T, pool *pgxpool.Pool, source *sourcefacts.Service, secret string, org, project int64) func() {
	observe := sourcereceiver.Observe
	if source.ObservationsEnabled() {
		observe = sourcereceiver.ObserveWithObservations
	}
	return observe(t, pool, source, secret, prd2E2EJWTSecret, org, project,
		func(mux *http.ServeMux, h *admin.Handler, guard func(http.HandlerFunc) http.Handler) {
			registerXInsightSourceControlRoutes(mux, h, xInsightFeedControlRouteGuards{safety: guard, expansion: guard})
		})
}
