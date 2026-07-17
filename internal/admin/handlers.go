package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/oidc"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimeconfig"
)

type routingController interface {
	Preview(context.Context, string) (routingpolicy.Preview, error)
	Invalidate()
}

type providerCostReconciler interface {
	ReconcileProviderAttempt(context.Context, billing.ProviderReconciliationRequest) (*billing.ProviderReconciliationResult, error)
}

type guardrailController interface {
	Refresh(context.Context) error
	ApprovedDetectors() []string
	DetectorStreamCapabilities() map[string]guardrail.StreamCapability
	DetectorInputLimits() (int, int, int)
	IsDetectorApproved(string) bool
	ValidatePolicy(guardrail.Policy) error
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
	guardrailController          guardrailController
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

func (h *Handler) SetGuardrailController(controller guardrailController) {
	h.guardrailController = controller
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

	var username, role, status, email, authProvider string
	var orgID int64
	var orgName string
	var createdAt time.Time
	err := h.db.QueryRow(r.Context(),
		`SELECT u.username, u.role, u.status, u.org_id, u.created_at, COALESCE(o.name, '') as org_name,
		        COALESCE(u.email, ''), COALESCE(NULLIF(u.auth_provider, ''), 'local')
		 FROM users u LEFT JOIN orgs o ON o.id = u.org_id
		 WHERE u.id = $1`, userID).Scan(&username, &role, &status, &orgID, &createdAt, &orgName, &email, &authProvider)
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":             userID,
		"username":       username,
		"role":           role,
		"status":         status,
		"org_id":         orgID,
		"org_name":       orgName,
		"email":          email,
		"auth_provider":  authProvider,
		"created_at":     createdAt,
		"license_access": licenseAccess,
	})
}
