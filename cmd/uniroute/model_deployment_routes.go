package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

type modelDeploymentRouteGuard func(http.HandlerFunc) http.Handler

type modelDeploymentRouteGuards struct {
	read    modelDeploymentRouteGuard
	write   modelDeploymentRouteGuard
	approve modelDeploymentRouteGuard
	safety  modelDeploymentRouteGuard
}

// registerModelDeploymentRoutes exposes only the canonical persistent
// deployment identity. Read truth is safe without a Gateway entitlement;
// expansion remains paid while explicit safety contractions stay available.
func registerModelDeploymentRoutes(mux *http.ServeMux, handler *admin.Handler, guards modelDeploymentRouteGuards) {
	mux.Handle("GET /api/v1/model-deployments", guards.read(handler.ListCanonicalModelApplications))
	mux.Handle("GET /api/v1/model-deployments/{deploymentID}", guards.read(handler.GetCanonicalModelApplication))
	mux.Handle("GET /api/v1/model-deployments/{deploymentID}/upstream-release-observation", guards.read(handler.GetModelUpstreamReleaseObservation))
	mux.Handle("PUT /api/v1/model-deployments/{deploymentID}/upstream-release-observation", guards.write(handler.PutModelUpstreamReleaseObservation))
	mux.Handle("GET /api/v1/model-deployments/{deploymentID}/model-discovery", guards.write(handler.DiscoverCanonicalModels))
	mux.Handle("POST /api/v1/model-deployments", guards.write(handler.CreateCanonicalModelApplication))
	mux.Handle("POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/probes", guards.write(handler.ProbeCanonicalModelOperation))
	mux.Handle("POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/production-approvals", guards.approve(handler.ApproveCanonicalModelOperationProduction))
	mux.Handle("GET /api/v1/model-deployments/{deploymentID}/operations/{operationID}/capabilities/{capabilityID}", guards.read(handler.GetCanonicalModelCapabilityQualification))
	mux.Handle("POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/capabilities/{capabilityID}/probes", guards.write(handler.ProbeCanonicalModelCapability))
	mux.Handle("GET /api/v1/model-deployments/{deploymentID}/operations/{operationID}/capability-probe-plan", guards.read(handler.GetCanonicalCapabilityProbePlan))
	mux.Handle("POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/capability-probe-groups", guards.write(handler.ProbeCanonicalCapabilityGroup))
	mux.Handle("GET /api/v1/model-deployments/{deploymentID}/operations/{operationID}/capability-probe-groups/{sourceRef}", guards.read(handler.GetCanonicalCapabilityProbeGroup))
	mux.Handle("POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/capabilities/{capabilityID}/routing-approvals", guards.approve(handler.ApproveCanonicalModelCapabilityRouting))
	mux.Handle("POST /api/v1/provider-connections/{connectionID}/pause", guards.safety(handler.PauseProviderConnection))
	mux.Handle("POST /api/v1/provider-credential-bindings/{bindingID}/revoke", guards.safety(handler.RevokeProviderCredentialBinding))
	mux.Handle("GET /api/v1/model-deployment-releases", guards.read(handler.ListCanonicalDeploymentReleases))
	mux.Handle("POST /api/v1/model-deployment-releases", guards.write(handler.AuthorCanonicalDeploymentRelease))
	mux.Handle("GET /api/v1/model-deployment-releases/{releaseRef}", guards.read(handler.GetCanonicalDeploymentLifecycle))
	mux.Handle("POST /api/v1/model-deployment-releases/{releaseRef}/activate-initial", guards.write(handler.ActivateInitialCanonicalDeploymentLifecycle))
	mux.Handle("POST /api/v1/model-deployment-releases/{releaseRef}/activate-successor", guards.write(handler.ActivateModelSuccessor))
	mux.Handle("POST /api/v1/model-deployment-releases/{releaseRef}/activate-staging-successor", guards.write(handler.ActivateStagingModelSuccessor))
	mux.Handle("POST /api/v1/model-deployment-releases/{releaseRef}/start-canary", guards.write(handler.StartCanonicalDeploymentCanary))
	mux.Handle("POST /api/v1/model-deployment-releases/{releaseRef}/promote", guards.write(handler.PromoteCanonicalDeploymentCanary))
	mux.Handle("POST /api/v1/model-deployment-releases/{releaseRef}/cancel-canary", guards.safety(handler.CancelCanonicalDeploymentCanary))
	mux.Handle("POST /api/v1/model-deployment-releases/{releaseRef}/rollback-stable", guards.write(handler.RollbackCanonicalDeploymentStable))
	mux.Handle("POST /api/v1/model-deployment-releases/{releaseRef}/cutover-legacy", guards.write(handler.CutoverLegacyCanonicalDeploymentLifecycle))
	mux.Handle("POST /api/v1/model-deployment-releases/{releaseRef}/rollback-legacy", guards.safety(handler.RollbackLegacyCanonicalDeploymentLifecycle))
}
