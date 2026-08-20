package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelrouteauthority"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/oidc"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/runtimeconfig"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

type routingController interface {
	Preview(context.Context, string) (routingpolicy.Preview, error)
	Invalidate()
}

// modelRoutePlanner is the narrow, read-only routing authority used by admin
// projections. It deliberately excludes Router preview/cache mutation so an
// admin read cannot silently fall back to the legacy channel authority.
type modelRoutePlanner interface {
	Plan(context.Context, modelrouting.Request) (modelrouting.Plan, error)
}

type providerCostReconciler interface {
	ReconcileProviderAttempt(context.Context, billing.ProviderReconciliationRequest) (*billing.ProviderReconciliationResult, error)
}

type billingReadinessProvider interface {
	InspectAdmissionReadiness(context.Context, int64, string) (billing.AdmissionReadiness, error)
}

type guardrailController interface {
	Refresh(context.Context) error
	ApprovedDetectors() []string
	DetectorStreamCapabilities() map[string]guardrail.StreamCapability
	DetectorInputLimits() (int, int, int)
	IsDetectorApproved(string) bool
	ValidatePolicy(guardrail.Policy) error
}

type completedFeedController interface {
	Enabled() bool
	SourceInstance() string
	EncodeSequence(int64, int64) (string, error)
}

// Handler contains all admin API handlers.
type Handler struct {
	sensitiveFilter              *filter.DFAFilter
	db                           db.DBQuerier
	jwtSecret                    string
	apiKeyHashSecret             string
	licenseVerifier              *licensing.Verifier
	licenseRuntime               *licensing.Runtime
	eventService                 *billing.EventService
	oidcService                  *oidc.Service
	encKey                       []byte
	redisClient                  *redis.Client
	startedAt                    time.Time
	runtimeOptions               RuntimeStatusOptions
	runtimeHostTelemetryProvider func() runtimeHostTelemetry
	runtimeStatusCache           runtimeStatusSnapshotCache
	runtimeStatusCollector       runtimeStatusCollector
	limitPolicyReloader          interface{ Refresh(context.Context) error }
	routingController            routingController
	outbox                       *outbox.Service
	providerCostReconciler       providerCostReconciler
	billingReadiness             billingReadinessProvider
	guardrailController          guardrailController
	completedFeedController      completedFeedController
	xInsightConnectorManager     *xinsightconnector.Manager
	xInsightConnectorIntake      xInsightConnectorInvitationIntake
	modelAuthorizationEvaluator  *modelauthz.Evaluator
	runtimeChangeManager         *runtimechange.Manager
	deploymentLifecycleManager   deploymentLifecycleApplication
	invocationOptimization       *invocationopt.Manager
	responseCacheRuntimeReady    bool
	requestPricingAuthority      string
	billingMode                  billing.BillingMode
	modelDeploymentProbeHTTPDoer modelDeploymentProbeHTTPDoer
	modelRouteSafetyController   *modelrouteauthority.Service
	modelRouteAuthorityMode      modelrouting.Mode
	modelRoutePlanner            modelRoutePlanner
}

func (h *Handler) SetModelRouteSafetyController(controller *modelrouteauthority.Service, mode modelrouting.Mode) {
	if h != nil {
		h.modelRouteSafetyController = controller
		h.modelRouteAuthorityMode = mode
	}
}

func (h *Handler) SetModelRoutePlanner(planner modelRoutePlanner) {
	if h != nil {
		h.modelRoutePlanner = planner
	}
}

func (h *Handler) SetInvocationOptimization(manager *invocationopt.Manager, responseCacheReady bool) {
	if h != nil {
		h.invocationOptimization = manager
		h.responseCacheRuntimeReady = responseCacheReady
	}
}

// SetBillingMode configures how billing participates in model-readiness
// admission. New handlers default to prepaid enforcement so existing callers
// that do not provide runtime configuration retain the historical behavior.
func (h *Handler) SetBillingMode(mode billing.BillingMode) error {
	if err := billing.ValidateBillingMode(mode); err != nil {
		return err
	}
	if h != nil {
		h.billingMode = mode
	}
	return nil
}

func (h *Handler) effectiveBillingMode() billing.BillingMode {
	if h == nil || h.billingMode == "" {
		return billing.BillingModePrepaidEnforced
	}
	return h.billingMode
}

func (h *Handler) SetModelAuthorizationEvaluator(evaluator *modelauthz.Evaluator) {
	if h != nil {
		h.modelAuthorizationEvaluator = evaluator
	}
}

func (h *Handler) SetRuntimeChangeManager(manager *runtimechange.Manager) {
	if h != nil {
		h.runtimeChangeManager = manager
		h.deploymentLifecycleManager = manager
	}
}

func (h *Handler) SetLimitPolicyReloader(reloader interface{ Refresh(context.Context) error }) {
	h.limitPolicyReloader = reloader
}

func (h *Handler) SetRoutingController(controller routingController) {
	h.routingController = controller
}

func (h *Handler) SetProviderCostReconciler(reconciler providerCostReconciler) {
	h.providerCostReconciler = reconciler
}

func (h *Handler) SetBillingReadinessProvider(provider billingReadinessProvider) {
	if h != nil {
		h.billingReadiness = provider
	}
}

func (h *Handler) SetGuardrailController(controller guardrailController) {
	h.guardrailController = controller
}

func (h *Handler) SetCompletedFeedController(controller completedFeedController) {
	h.completedFeedController = controller
}

func (h *Handler) SetXInsightConnectorManager(manager *xinsightconnector.Manager) {
	if h != nil {
		h.xInsightConnectorManager = manager
	}
}

func (h *Handler) SetXInsightConnectorInvitationIntake(intake xInsightConnectorInvitationIntake) {
	if h != nil {
		h.xInsightConnectorIntake = intake
	}
}

func (h *Handler) invalidateRoutingSnapshots() {
	if h != nil && h.routingController != nil {
		h.routingController.Invalidate()
	}
}

// NewHandler creates a new admin Handler.
func NewHandler(db db.DBQuerier, jwtSecret string, encKey []byte, sf *filter.DFAFilter) *Handler {
	return NewHandlerWithAPIKeyHashSecret(db, jwtSecret, jwtSecret, encKey, sf)
}

// NewHandlerWithAPIKeyHashSecret creates a Handler with separate JWT signing
// and API-key HMAC secrets. Use this in production wiring.
func NewHandlerWithAPIKeyHashSecret(db db.DBQuerier, jwtSecret, apiKeyHashSecret string, encKey []byte, sf *filter.DFAFilter) *Handler {
	if apiKeyHashSecret == "" {
		apiKeyHashSecret = jwtSecret
	}
	return &Handler{
		db: db, jwtSecret: jwtSecret, apiKeyHashSecret: apiKeyHashSecret,
		encKey: encKey, sensitiveFilter: sf, startedAt: time.Now(),
		runtimeOptions: RuntimeStatusOptions{EffectiveConfig: runtimeconfig.Default()},
		billingMode:    billing.BillingModePrepaidEnforced,
	}
}

func (h *Handler) SetLicenseVerifier(verifier *licensing.Verifier) {
	h.licenseVerifier = verifier
}

func (h *Handler) SetLicenseRuntime(runtime *licensing.Runtime) {
	h.licenseRuntime = runtime
}

func (h *Handler) SetEventService(svc *billing.EventService) {
	h.eventService = svc
}

func (h *Handler) SetOutbox(service *outbox.Service) {
	if h != nil {
		h.outbox = service
	}
}

func (h *Handler) SetOIDCService(svc *oidc.Service) {
	h.oidcService = svc
}

func (h *Handler) SetRedisClient(client *redis.Client) {
	h.redisClient = client
}

func (h *Handler) SetRuntimeStatusOptions(options RuntimeStatusOptions) {
	if options.EffectiveConfig.Version == "" {
		options.EffectiveConfig = runtimeconfig.Default()
	}
	h.runtimeOptions = options
}

func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserIDFromContext(r.Context())
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Not authenticated"})
		return
	}

	var username, organizationRole, role, status, email, authProvider string
	var orgID int64
	var orgName string
	var createdAt time.Time
	var hasLiveProjectMembership, hasProjectAdminMembership bool
	err := h.db.QueryRow(r.Context(),
		`SELECT u.username, u.organization_role, u.role, u.status, u.org_id, u.created_at, COALESCE(o.name, '') as org_name,
		        COALESCE(u.email, ''), COALESCE(NULLIF(u.auth_provider, ''), 'local'),
		        EXISTS (
		          SELECT 1 FROM project_members pm JOIN projects p ON p.id=pm.project_id
		          WHERE pm.user_id=u.id AND p.org_id=u.org_id AND p.status='active'
		        ),
		        EXISTS (
		          SELECT 1 FROM project_members pm JOIN projects p ON p.id=pm.project_id
		          WHERE pm.user_id=u.id AND p.org_id=u.org_id AND p.status='active' AND pm.role='project_admin'
		        )
		 FROM users u LEFT JOIN orgs o ON o.id = u.org_id
		 WHERE u.id = $1`, userID).Scan(&username, &organizationRole, &role, &status, &orgID, &createdAt, &orgName, &email, &authProvider,
		&hasLiveProjectMembership, &hasProjectAdminMembership)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		return
	}

	licenseAccess := licensing.CapabilityStatus{
		Status:      licensing.StatusInvalid,
		Enforceable: false,
		Features: map[string]bool{
			licensing.FeatureGateway:           false,
			licensing.FeatureAgentCapabilities: false,
			licensing.FeatureOIDC:              false,
			licensing.FeatureGuardrails:        false,
			licensing.FeatureNotifications:     false,
			licensing.FeatureModelGovernance:   false,
			licensing.FeatureDataExchange:      false,
		},
	}
	if h.licenseRuntime != nil {
		licenseAccess = h.licenseRuntime.Capabilities()
	}
	customer, present := middleware.GetCustomerContextFromContext(r.Context())
	if !present {
		customer = customercontext.Snapshot{State: customercontext.StateUnavailable}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":                userID,
		"username":          username,
		"role":              role,
		"organization_role": organizationRole,
		"status":            status,
		"org_id":            orgID,
		"org_name":          orgName,
		"email":             email,
		"auth_provider":     authProvider,
		"identity_scope":    actorIdentityScope(r.Context(), organizationRole),
		"customer_context":  customer,
		"created_at":        createdAt,
		"license_access":    licenseAccess,
		"navigation_capabilities": map[string]interface{}{
			"has_live_project_membership":  hasLiveProjectMembership,
			"has_project_admin_membership": hasProjectAdminMembership,
			"can_discover_access_review":   organizationRole == "platform_admin" || organizationRole == "org_admin" || hasProjectAdminMembership,
			"can_discover_workloads":       organizationRole == "platform_admin" || organizationRole == "org_admin" || hasProjectAdminMembership,
		},
	})
}
