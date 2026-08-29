package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modeldeployment"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const canonicalModelApplicationContractVersion = "unirouter.model-deployment.v1"

const (
	canonicalModelApplicationMaxRequestBytes       = 128 << 10
	canonicalModelApplicationMaxAdapterConfigBytes = 64 << 10
)

var (
	errCanonicalApplicationInvalid                     = errors.New("canonical model application request is invalid")
	errCanonicalApplicationNotFound                    = errors.New("canonical model application not found")
	errCanonicalApplicationConflict                    = errors.New("canonical model application conflicts with existing state")
	errCanonicalApplicationQualificationOriginInvalid  = errors.New("canonical model application qualification requires a path-free HTTPS origin")
	errCanonicalApplicationQualificationOriginConflict = errors.New("canonical model application connection is incompatible with qualification")
)

// canonicalModelApplication is the administration contract for one persistent
// deployment identity. Its axes are deliberately independent: a successful
// probe does not imply a runtime release, and a runtime release does not imply
// that an unspecified project/caller is authorized to call it.
type canonicalModelApplication struct {
	ContractVersion string                         `json:"contract_version"`
	PublicModel     canonicalPublicModel           `json:"public_model"`
	Deployment      canonicalDeployment            `json:"deployment"`
	Connection      canonicalConnectionAxis        `json:"connection"`
	Credential      canonicalCredentialAxis        `json:"credential"`
	Operations      []canonicalOperationAxis       `json:"operations"`
	Runtime         canonicalRuntimeAxis           `json:"runtime"`
	Callability     canonicalCallabilityAxis       `json:"callability"`
	Actions         canonicalApplicationActionMeta `json:"actions"`
}

type canonicalPublicModel struct {
	ID                      int64  `json:"id"`
	ModelName               string `json:"model_name"`
	DisplayName             string `json:"display_name"`
	ProviderFamily          string `json:"provider_family"`
	Lifecycle               string `json:"lifecycle"`
	ContextLength           int    `json:"context_length"`
	OutputLimit             int    `json:"output_limit"`
	ApprovedCatalog         bool   `json:"approved_catalog"`
	ExecutionProfileID      string `json:"execution_profile_id"`
	ExecutionProfileVersion string `json:"execution_profile_version"`
}

type canonicalDeployment struct {
	ID                   int64      `json:"id"`
	UpstreamModel        string     `json:"upstream_model"`
	AdminState           string     `json:"admin_state"`
	ConfigRevision       int64      `json:"config_revision"`
	Priority             int        `json:"priority"`
	RoutingWeight        int        `json:"routing_weight"`
	MaxContext           int        `json:"max_context"`
	DefaultMaxOutput     int        `json:"default_max_output"`
	DegradationThreshold int        `json:"degradation_threshold"`
	CircuitState         string     `json:"circuit_state"`
	CircuitRevision      int64      `json:"circuit_revision"`
	FailCount            int        `json:"fail_count"`
	RetiredAt            *time.Time `json:"retired_at,omitempty"`
}

type canonicalConnectionAxis struct {
	ID             int64           `json:"id"`
	Name           string          `json:"name"`
	Provider       string          `json:"provider"`
	BaseURL        string          `json:"base_url"`
	AdapterType    string          `json:"adapter_type"`
	AdapterConfig  json.RawMessage `json:"adapter_config"`
	AdminState     string          `json:"admin_state"`
	ConfigRevision int64           `json:"config_revision"`
	RetiredAt      *time.Time      `json:"retired_at,omitempty"`
}

type canonicalCredentialAxis struct {
	State         string                          `json:"state"`
	ReasonCode    string                          `json:"reason_code"`
	BoundCount    int                             `json:"bound_count"`
	EligibleCount int                             `json:"eligible_count"`
	Bindings      []canonicalCredentialBindingRef `json:"bindings"`
}

type canonicalCredentialBindingRef struct {
	ID            int64      `json:"id"`
	Status        string     `json:"status"`
	StateRevision int64      `json:"state_revision"`
	FailCount     int        `json:"fail_count"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	CooldownUntil *time.Time `json:"cooldown_until,omitempty"`
	Eligible      bool       `json:"eligible"`
}

type canonicalOperationAxis struct {
	OperationID              string                                `json:"operation_id"`
	ProtocolProfileID        string                                `json:"protocol_profile_id"`
	ProtocolProfileVersion   string                                `json:"protocol_profile_version"`
	SupportState             string                                `json:"support_state"`
	EvidenceState            string                                `json:"evidence_state"`
	EvidenceRef              string                                `json:"evidence_ref,omitempty"`
	Revision                 int64                                 `json:"revision"`
	VerifiedAt               *time.Time                            `json:"verified_at,omitempty"`
	CircuitState             string                                `json:"circuit_state"`
	CircuitRevision          int64                                 `json:"circuit_revision"`
	CircuitFailCount         int                                   `json:"circuit_fail_count"`
	ProbedBindingCount       int                                   `json:"probed_binding_count"`
	CurrentSuccessCount      int                                   `json:"current_success_count"`
	CurrentFailureCount      int                                   `json:"current_failure_count"`
	StaleProbedBindingCount  int                                   `json:"stale_probed_binding_count"`
	ProbeState               string                                `json:"probe_state"`
	ProbeReasonCode          string                                `json:"probe_reason_code"`
	ProbeAvailable           bool                                  `json:"probe_available"`
	ProbeAvailabilityReason  string                                `json:"probe_availability_reason_code"`
	CapabilityProbeContracts []canonicalCapabilityProbeContractRef `json:"capability_probe_contracts"`
	ExecutionSupport         *canonicalExecutionSupport            `json:"execution_support,omitempty"`
	ProductionQualification  canonicalProductionQualification      `json:"production_qualification"`
	LatestProbe              *canonicalOperationProbeView          `json:"latest_probe,omitempty"`
	Runtime                  canonicalOperationRuntimeAxis         `json:"runtime"`
}

type canonicalCapabilityProbeContractRef struct {
	CapabilityID         string `json:"capability_id"`
	ProbeContractID      string `json:"probe_contract_id"`
	ProbeContractVersion string `json:"probe_contract_version"`
}

type canonicalExecutionSupport struct {
	OperationID             string `json:"operation_id"`
	PublicPath              string `json:"public_path"`
	ProtocolProfileID       string `json:"protocol_profile_id"`
	ProtocolProfileVersion  string `json:"protocol_profile_version"`
	ExecutorContractVersion string `json:"executor_contract_version"`
}

type canonicalProductionQualification struct {
	Required                   bool   `json:"required"`
	Qualified                  bool   `json:"qualified"`
	State                      string `json:"state"`
	ApprovalID                 *int64 `json:"approval_id,omitempty"`
	ProbeID                    *int64 `json:"probe_id,omitempty"`
	HTTPSOrigin                string `json:"https_origin,omitempty"`
	ApprovedConnectionRevision int64  `json:"approved_connection_revision,omitempty"`
	ApprovedDeploymentRevision int64  `json:"approved_deployment_revision,omitempty"`
	ApprovedOperationRevision  int64  `json:"approved_operation_revision,omitempty"`
	CurrentConnectionRevision  int64  `json:"current_connection_revision,omitempty"`
	CurrentDeploymentRevision  int64  `json:"current_deployment_revision,omitempty"`
	CurrentOperationRevision   int64  `json:"current_operation_revision,omitempty"`
}

type canonicalOperationRuntimeAxis struct {
	State            string                      `json:"state"`
	ReasonCode       string                      `json:"reason_code"`
	SlotVersion      int64                       `json:"slot_version,omitempty"`
	StableMembership *canonicalRuntimeMembership `json:"stable_membership,omitempty"`
	CanaryMembership *canonicalRuntimeMembership `json:"canary_membership,omitempty"`
}

type canonicalRuntimeMembership struct {
	ReleaseRef                 string `json:"release_ref"`
	ReleaseStage               string `json:"release_stage"`
	ReleaseVersion             int64  `json:"release_version"`
	RouteItemsHash             string `json:"route_items_hash"`
	Ordinal                    int    `json:"ordinal"`
	Admission                  string `json:"admission"`
	Priority                   *int   `json:"priority,omitempty"`
	Weight                     *int   `json:"weight,omitempty"`
	ExpectedConnectionRevision int64  `json:"expected_connection_revision"`
	ExpectedDeploymentRevision int64  `json:"expected_deployment_revision"`
	ExpectedOperationRevision  int64  `json:"expected_operation_revision"`
	ProtocolProfileID          string `json:"protocol_profile_id"`
	ProtocolProfileVersion     string `json:"protocol_profile_version"`
	PublicPath                 string `json:"public_path,omitempty"`
	ExecutorContractVersion    string `json:"executor_contract_version,omitempty"`
}

type canonicalOperationProbeView struct {
	ProbeID                 int64     `json:"probe_id"`
	Success                 bool      `json:"success"`
	Current                 bool      `json:"current"`
	StatusCode              int       `json:"status_code"`
	LatencyMS               int64     `json:"latency_ms"`
	ReportedModel           string    `json:"reported_model,omitempty"`
	ErrorSummary            string    `json:"error_summary,omitempty"`
	CredentialBindingID     *int64    `json:"credential_binding_id,omitempty"`
	ConnectionRevision      int64     `json:"connection_revision"`
	DeploymentRevision      int64     `json:"deployment_revision"`
	CredentialStateRevision int64     `json:"credential_state_revision"`
	ProbeProfileID          string    `json:"probe_profile_id"`
	ProbeProfileVersion     string    `json:"probe_profile_version"`
	OperationRevision       *int64    `json:"operation_revision,omitempty"`
	PublicPath              string    `json:"public_path,omitempty"`
	ExecutorContractVersion string    `json:"executor_contract_version,omitempty"`
	HTTPSOrigin             string    `json:"https_origin,omitempty"`
	SourceKind              string    `json:"source_kind"`
	TestedAt                time.Time `json:"tested_at"`
	TestedBy                *int64    `json:"tested_by,omitempty"`
}

type canonicalRuntimeAxis struct {
	State                string `json:"state"`
	ReasonCode           string `json:"reason_code"`
	SlotVersion          int64  `json:"slot_version,omitempty"`
	StableReleaseRef     string `json:"stable_release_ref,omitempty"`
	StableReleaseStage   string `json:"stable_release_stage,omitempty"`
	StableReleaseVersion int64  `json:"stable_release_version,omitempty"`
	CanaryReleaseRef     string `json:"canary_release_ref,omitempty"`
	CanaryReleaseStage   string `json:"canary_release_stage,omitempty"`
	CanaryReleaseVersion int64  `json:"canary_release_version,omitempty"`
	CanaryBasisPoints    int    `json:"canary_basis_points,omitempty"`
}

type canonicalCallabilityAxis struct {
	Evaluated  bool   `json:"evaluated"`
	State      string `json:"state"`
	ReasonCode string `json:"reason_code"`
}

type canonicalApplicationActionMeta struct {
	IdentityType      string   `json:"identity_type"`
	RequiredFields    []string `json:"required_fields"`
	LifecycleWriter   string   `json:"lifecycle_writer"`
	LicenseMiddleware string   `json:"license_middleware"`
	RBACMiddleware    string   `json:"rbac_middleware"`
}

type canonicalModelApplicationProjection struct {
	ContractVersion string                      `json:"contract_version"`
	GeneratedAt     time.Time                   `json:"generated_at"`
	Applications    []canonicalModelApplication `json:"applications"`
	Summary         canonicalApplicationSummary `json:"summary"`
}

type canonicalApplicationSummary struct {
	Deployments               int `json:"deployments"`
	ConnectionsPaused         int `json:"connections_paused"`
	CredentialsReady          int `json:"credentials_ready"`
	ProbesReady               int `json:"probes_ready"`
	RuntimeAssignedOperations int `json:"runtime_assigned_operations"`
}

type canonicalModelApplicationCreateRequest struct {
	Connection  canonicalConnectionInput  `json:"connection"`
	PublicModel canonicalPublicModelInput `json:"public_model"`
	Deployment  canonicalDeploymentInput  `json:"deployment"`
	Credential  canonicalCredentialInput  `json:"credential"`
	Operation   canonicalOperationInput   `json:"operation"`
}

type canonicalConnectionInput struct {
	ID            int64           `json:"id,omitempty"`
	Name          string          `json:"name,omitempty"`
	Provider      string          `json:"provider,omitempty"`
	BaseURL       string          `json:"base_url,omitempty"`
	AdapterType   string          `json:"adapter_type,omitempty"`
	AdapterConfig json.RawMessage `json:"adapter_config,omitempty"`
}

type canonicalPublicModelInput struct {
	ID                      int64  `json:"id,omitempty"`
	ModelName               string `json:"model_name,omitempty"`
	DisplayName             string `json:"display_name,omitempty"`
	ProviderFamily          string `json:"provider_family,omitempty"`
	Lifecycle               string `json:"lifecycle,omitempty"`
	ContextLength           *int   `json:"context_length,omitempty"`
	OutputLimit             *int   `json:"output_limit,omitempty"`
	ApprovedCatalog         *bool  `json:"approved_catalog,omitempty"`
	ExecutionProfileID      string `json:"execution_profile_id,omitempty"`
	ExecutionProfileVersion string `json:"execution_profile_version,omitempty"`
}

type canonicalDeploymentInput struct {
	UpstreamModel        string `json:"upstream_model"`
	Priority             *int   `json:"priority,omitempty"`
	RoutingWeight        *int   `json:"routing_weight,omitempty"`
	MaxContext           *int   `json:"max_context,omitempty"`
	DefaultMaxOutput     *int   `json:"default_max_output,omitempty"`
	DegradationThreshold *int   `json:"degradation_threshold,omitempty"`
}

type canonicalCredentialInput struct {
	BindingID int64  `json:"binding_id,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
}

type canonicalOperationInput struct {
	OperationID            string `json:"operation_id"`
	ProtocolProfileID      string `json:"-"`
	ProtocolProfileVersion string `json:"-"`
}

type canonicalConnectionIdentity struct {
	ID            int64
	Provider      string
	BaseURL       string
	AdapterType   string
	AdapterConfig json.RawMessage
}

type canonicalPublicModelIdentity struct {
	ID                      int64
	ExecutionProfileID      string
	ExecutionProfileVersion string
}

type canonicalApplicationReadStore interface {
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

type canonicalModelApplicationCreateResult struct {
	Application         canonicalModelApplication `json:"application"`
	IdempotencyReplay   bool                      `json:"idempotency_replay"`
	OriginalHTTPStatus  int                       `json:"-"`
	OriginalOperationID string                    `json:"-"`
	Reused              struct {
		Connection  bool `json:"connection"`
		PublicModel bool `json:"public_model"`
		Deployment  bool `json:"deployment"`
		Credential  bool `json:"credential"`
		Operation   bool `json:"operation"`
	} `json:"reused"`
}

type canonicalApplicationSnapshotLoader func(
	context.Context,
	canonicalApplicationReadStore,
	int64,
	int64,
	string,
	time.Time,
) (canonicalModelApplicationProjection, error)

func canonicalActionMeta() canonicalApplicationActionMeta {
	return canonicalApplicationActionMeta{
		IdentityType:      "deployment_id+operation_id",
		RequiredFields:    []string{"deployment_id", "operation_id", "expected_release_version", "expected_slot_version"},
		LifecycleWriter:   "runtimechange.v2_only",
		LicenseMiddleware: "route_owned",
		RBACMiddleware:    "route_owned",
	}
}

func (h *Handler) ListCanonicalModelApplications(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	operationID := strings.TrimSpace(r.URL.Query().Get("operation"))
	if operationID != "" {
		if _, err := modeldeployment.NormalizeOperationID(operationID); err != nil {
			writeCanonicalApplicationError(w, errCanonicalApplicationInvalid)
			return
		}
	}
	projection, err := h.loadCanonicalModelApplications(r.Context(), orgID, 0, operationID)
	if err != nil {
		writeCanonicalApplicationError(w, err)
		return
	}
	redactCanonicalModelApplicationViewerProjection(r.Context(), &projection)
	writeJSON(w, http.StatusOK, projection)
}

func (h *Handler) GetCanonicalModelApplication(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	deploymentID, err := canonicalDeploymentID(r)
	if err != nil {
		writeCanonicalApplicationError(w, errCanonicalApplicationInvalid)
		return
	}
	operationID, err := modeldeployment.NormalizeOperationID(r.URL.Query().Get("operation"))
	if err != nil {
		writeCanonicalApplicationError(w, errCanonicalApplicationInvalid)
		return
	}
	projection, err := h.loadCanonicalModelApplications(r.Context(), orgID, deploymentID, operationID)
	if err != nil {
		writeCanonicalApplicationError(w, err)
		return
	}
	if len(projection.Applications) != 1 || len(projection.Applications[0].Operations) != 1 {
		writeCanonicalApplicationError(w, errCanonicalApplicationNotFound)
		return
	}
	redactCanonicalModelApplicationViewerProjection(r.Context(), &projection)
	writeJSON(w, http.StatusOK, projection.Applications[0])
}

func redactCanonicalModelApplicationViewerProjection(ctx context.Context, projection *canonicalModelApplicationProjection) {
	if projection == nil || actorOrganizationRole(ctx) != auth.RoleOrgViewer {
		return
	}
	for applicationIndex := range projection.Applications {
		application := &projection.Applications[applicationIndex]
		application.Credential.Bindings = make([]canonicalCredentialBindingRef, 0)
		for operationIndex := range application.Operations {
			probe := application.Operations[operationIndex].LatestProbe
			if probe == nil {
				continue
			}
			probe.CredentialBindingID = nil
			probe.TestedBy = nil
		}
	}
}

func (h *Handler) CreateCanonicalModelApplication(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	identity := middleware.GetRequestIdentityFromContext(r.Context())
	if !identity.IdempotencyKeyPresent || identity.IdempotencyKeyHash == "" {
		writeCanonicalApplicationError(w, errCanonicalApplicationIdempotencyRequired)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, canonicalModelApplicationMaxRequestBytes))
	if err != nil {
		writeCanonicalApplicationError(w, errCanonicalApplicationInvalid)
		return
	}
	if err := strictjson.Validate(body); err != nil {
		writeCanonicalApplicationError(w, errCanonicalApplicationInvalid)
		return
	}
	var request canonicalModelApplicationCreateRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeCanonicalApplicationError(w, errCanonicalApplicationInvalid)
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeCanonicalApplicationError(w, errCanonicalApplicationInvalid)
		return
	}
	result, err := h.createCanonicalModelApplication(r.Context(), orgID, request, requestAuditMeta(r, orgID, 0))
	if err != nil {
		writeCanonicalApplicationError(w, err)
		return
	}
	if result.IdempotencyReplay {
		w.Header().Set(middleware.IdempotencyReplayHeader, "true")
		if result.OriginalOperationID != "" {
			w.Header().Set(middleware.OriginalOperationHeader, result.OriginalOperationID)
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	h.invalidateRoutingSnapshots()
	writeJSON(w, http.StatusCreated, result)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errCanonicalApplicationInvalid
		}
		return err
	}
	return nil
}

func canonicalDeploymentID(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.PathValue("deploymentID"))
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("deployment_id"))
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errCanonicalApplicationInvalid
	}
	return id, nil
}

func writeCanonicalApplicationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errCanonicalApplicationIdempotencyRequired):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Idempotency-Key is required for canonical model deployment creation", "code": "canonical_application_idempotency_required"})
	case errors.Is(err, errCanonicalApplicationIdempotencyConflict):
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "The Idempotency-Key was already used with a different canonical model deployment request", "code": "canonical_application_idempotency_conflict"})
	case errors.Is(err, errCanonicalApplicationQualificationOriginInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Responses and Messages require a path-free HTTPS connection origin", "code": "canonical_application_https_origin_required"})
	case errors.Is(err, errCanonicalApplicationQualificationOriginConflict):
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "The existing connection does not have the path-free HTTPS origin required by this operation", "code": "canonical_application_https_origin_required"})
	case errors.Is(err, errCanonicalApplicationInvalid), errors.Is(err, modeldeployment.ErrInvalidIdentity):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid canonical model application request", "code": "canonical_application_invalid"})
	case errors.Is(err, errCanonicalApplicationNotFound), errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Canonical model application not found", "code": "canonical_application_not_found"})
	case errors.Is(err, errCanonicalApplicationConflict), errors.Is(err, modeldeployment.ErrRetiredIdentity):
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Canonical model application conflicts with current state", "code": "canonical_application_conflict"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Canonical model application unavailable", "code": "canonical_application_unavailable"})
	}
}

func (h *Handler) createCanonicalModelApplication(
	ctx context.Context,
	orgID int64,
	request canonicalModelApplicationCreateRequest,
	meta auditMeta,
) (canonicalModelApplicationCreateResult, error) {
	idempotency, normalized, err := h.canonicalCreateIdempotency(ctx, orgID, request)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	var result canonicalModelApplicationCreateResult
	for attempt := 0; attempt < 2; attempt++ {
		result, err = h.createCanonicalModelApplicationWithReadbackAndIdempotency(
			ctx, orgID, normalized, meta, h.loadCanonicalModelApplicationsSnapshot, &idempotency,
		)
		if err == nil {
			return result, nil
		}
		if !isCanonicalCreateTransactionRetryable(err) || attempt == 1 {
			break
		}
	}
	var commitErr *canonicalCreateCommitError
	if errors.As(err, &commitErr) {
		if recovered, recoveryErr := h.recoverCanonicalModelApplicationCommit(ctx, idempotency); recoveryErr == nil {
			return recovered, nil
		}
	}
	return canonicalModelApplicationCreateResult{}, err
}

func (h *Handler) createCanonicalModelApplicationWithReadback(
	ctx context.Context,
	orgID int64,
	request canonicalModelApplicationCreateRequest,
	meta auditMeta,
	readback canonicalApplicationSnapshotLoader,
) (canonicalModelApplicationCreateResult, error) {
	return h.createCanonicalModelApplicationWithReadbackAndIdempotency(
		ctx, orgID, request, meta, readback, nil,
	)
}

func (h *Handler) createCanonicalModelApplicationWithReadbackAndIdempotency(
	ctx context.Context,
	orgID int64,
	request canonicalModelApplicationCreateRequest,
	meta auditMeta,
	readback canonicalApplicationSnapshotLoader,
	idempotency *canonicalModelApplicationIdempotency,
) (canonicalModelApplicationCreateResult, error) {
	request, err := normalizeCanonicalApplicationCreate(request)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	encryptedKey := ""
	if request.Credential.APIKey != "" {
		encryptedKey, err = auth.EncryptKey([]byte(request.Credential.APIKey), h.encKey)
		if err != nil {
			return canonicalModelApplicationCreateResult{}, fmt.Errorf("encrypt canonical provider credential: %w", err)
		}
	}
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, fmt.Errorf("begin canonical application create: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
		return canonicalModelApplicationCreateResult{}, fmt.Errorf("configure canonical application create: %w", err)
	}
	// M75-qualified operations own the complete provider path. Validate the
	// effective connection origin before claiming an idempotency receipt or
	// creating any canonical row, including when the connection is reused by ID.
	if err := validateCanonicalQualificationOrigin(ctx, tx, orgID, request); err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	var receiptID int64
	if idempotency != nil {
		claimed, receipt, err := claimCanonicalModelApplicationReceipt(ctx, tx, *idempotency)
		if err != nil {
			return canonicalModelApplicationCreateResult{}, err
		}
		if !claimed {
			if err := validateCanonicalModelApplicationReplay(receipt, *idempotency); err != nil {
				return canonicalModelApplicationCreateResult{}, err
			}
			result, err := h.readCanonicalModelApplicationReplay(ctx, tx, orgID, readback, receipt)
			if err != nil {
				return canonicalModelApplicationCreateResult{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return canonicalModelApplicationCreateResult{}, &canonicalCreateCommitError{cause: err}
			}
			return result, nil
		}
		receiptID = receipt.ID
	}

	if err := validateCanonicalCustomerOrg(ctx, tx, orgID); err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	connection, connectionReused, err := ensureCanonicalConnection(ctx, tx, orgID, request.Connection)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	request.Operation, err = resolveCanonicalOperationProfile(request.Operation, connection.AdapterType)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	publicModel, publicModelReused, err := ensureCanonicalPublicModel(ctx, tx, orgID, request.PublicModel, connection.Provider)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	request.Operation, err = resolveCanonicalTypedOperationProfile(publicModel, request.Operation)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	if err := validateCanonicalProfileProductBinding(publicModel, connection, request.Deployment.UpstreamModel, request.Operation); err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	deploymentID, deploymentReused, err := ensureCanonicalDeployment(ctx, tx, orgID, publicModel.ID, connection.ID, request.Deployment)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	if err := validateCanonicalNewDeploymentProfile(publicModel, deploymentReused); err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	bindingID, credentialReused, err := ensureCanonicalCredentialBinding(ctx, tx, connection.ID, deploymentID, request.Credential.BindingID, encryptedKey)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	operationReused, err := ensureCanonicalOperation(ctx, tx, deploymentID, request.Operation)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	if err := ensureCanonicalDeclaredCapabilities(
		ctx, tx, deploymentID, publicModel, connection,
		request.Deployment.UpstreamModel, request.Operation,
	); err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	if err := insertAuditEntry(ctx, tx, "model_deployment.create", "model_deployment", strconv.FormatInt(deploymentID, 10), map[string]interface{}{
		"after": map[string]interface{}{
			"customer_org_id": orgID, "connection_id": connection.ID, "public_model_id": publicModel.ID,
			"deployment_id": deploymentID, "credential_binding_id": bindingID,
			"operation_id":             request.Operation.OperationID,
			"protocol_profile_id":      request.Operation.ProtocolProfileID,
			"protocol_profile_version": request.Operation.ProtocolProfileVersion,
		},
		"reused": map[string]bool{
			"connection": connectionReused, "public_model": publicModelReused,
			"deployment": deploymentReused, "credential": credentialReused, "operation": operationReused,
		},
	}, meta); err != nil {
		return canonicalModelApplicationCreateResult{}, fmt.Errorf("audit canonical application create: %w", err)
	}
	if readback == nil {
		return canonicalModelApplicationCreateResult{}, fmt.Errorf("read canonical application after create: snapshot loader is unavailable")
	}
	var generatedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&generatedAt); err != nil {
		return canonicalModelApplicationCreateResult{}, fmt.Errorf("timestamp canonical application create: %w", err)
	}
	projection, err := readback(
		ctx, tx, orgID, deploymentID, request.Operation.OperationID, generatedAt,
	)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, fmt.Errorf("read canonical application after create: %w", err)
	}
	if len(projection.Applications) != 1 || len(projection.Applications[0].Operations) != 1 {
		return canonicalModelApplicationCreateResult{}, fmt.Errorf("read canonical application after create: %w", errCanonicalApplicationNotFound)
	}
	result := canonicalModelApplicationCreateResult{Application: projection.Applications[0]}
	result.Reused.Connection = connectionReused
	result.Reused.PublicModel = publicModelReused
	result.Reused.Deployment = deploymentReused
	result.Reused.Credential = credentialReused
	result.Reused.Operation = operationReused
	if receiptID > 0 {
		if err := completeCanonicalModelApplicationReceipt(ctx, tx, receiptID, result); err != nil {
			return canonicalModelApplicationCreateResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return canonicalModelApplicationCreateResult{}, &canonicalCreateCommitError{cause: err}
	}
	return result, nil
}

func normalizeCanonicalApplicationCreate(request canonicalModelApplicationCreateRequest) (canonicalModelApplicationCreateRequest, error) {
	request.PublicModel.ModelName = strings.TrimSpace(request.PublicModel.ModelName)
	request.PublicModel.DisplayName = strings.TrimSpace(request.PublicModel.DisplayName)
	request.PublicModel.ProviderFamily = strings.TrimSpace(request.PublicModel.ProviderFamily)
	request.PublicModel.Lifecycle = strings.TrimSpace(request.PublicModel.Lifecycle)
	request.PublicModel.ExecutionProfileID = strings.TrimSpace(request.PublicModel.ExecutionProfileID)
	request.PublicModel.ExecutionProfileVersion = strings.TrimSpace(request.PublicModel.ExecutionProfileVersion)
	request.Deployment.UpstreamModel = strings.TrimSpace(request.Deployment.UpstreamModel)
	request.Credential.APIKey = strings.TrimSpace(request.Credential.APIKey)
	request.Operation.OperationID = strings.TrimSpace(request.Operation.OperationID)
	if request.PublicModel.ID < 0 || request.Connection.ID < 0 || request.Credential.BindingID < 0 {
		return request, errCanonicalApplicationInvalid
	}
	if request.PublicModel.ID == 0 {
		if _, err := modeldeployment.NormalizeIdentity(request.PublicModel.ModelName, 100); err != nil {
			return request, errCanonicalApplicationInvalid
		}
		if request.PublicModel.DisplayName == "" {
			request.PublicModel.DisplayName = request.PublicModel.ModelName
		}
		if request.PublicModel.Lifecycle == "" {
			request.PublicModel.Lifecycle = "staging"
		}
		if request.PublicModel.ExecutionProfileID == "" && request.PublicModel.ExecutionProfileVersion == "" {
			if modelprofile.HasAmbiguousCurrentAlias(request.PublicModel.ModelName) {
				return request, errCanonicalApplicationInvalid
			}
			request.PublicModel.ExecutionProfileID = modelprofile.UnverifiedProfileID
			request.PublicModel.ExecutionProfileVersion = modelprofile.UnverifiedProfileVersion
		}
	} else if request.PublicModel.ModelName != "" {
		if _, err := modeldeployment.NormalizeIdentity(request.PublicModel.ModelName, 100); err != nil {
			return request, errCanonicalApplicationInvalid
		}
	}
	if len(request.PublicModel.DisplayName) > 255 || strings.ContainsAny(request.PublicModel.DisplayName, "\x00\r\n") {
		return request, errCanonicalApplicationInvalid
	}
	if request.PublicModel.Lifecycle != "" {
		switch request.PublicModel.Lifecycle {
		case "staging", "ga", "deprecated":
		default:
			return request, errCanonicalApplicationInvalid
		}
	}
	if (request.PublicModel.ContextLength != nil && *request.PublicModel.ContextLength < 0) ||
		(request.PublicModel.OutputLimit != nil && *request.PublicModel.OutputLimit < 0) {
		return request, errCanonicalApplicationInvalid
	}
	if (request.PublicModel.ExecutionProfileID == "") != (request.PublicModel.ExecutionProfileVersion == "") {
		return request, errCanonicalApplicationInvalid
	}
	if request.PublicModel.ExecutionProfileID != "" {
		profile, ok := modelprofile.Lookup(request.PublicModel.ExecutionProfileID, request.PublicModel.ExecutionProfileVersion)
		if !ok {
			return request, errCanonicalApplicationInvalid
		}
		if request.PublicModel.ID == 0 && profile.ID != modelprofile.UnverifiedProfileID {
			defaultPublicContextLimit := modelprofile.DefaultPublicContextLimit(profile)
			defaultPublicOutputLimit := modelprofile.DefaultPublicOutputLimit(profile)
			if request.PublicModel.ContextLength == nil && defaultPublicContextLimit > 0 {
				request.PublicModel.ContextLength = canonicalApplicationIntPointer(defaultPublicContextLimit)
			}
			if request.PublicModel.OutputLimit == nil && defaultPublicOutputLimit > 0 {
				request.PublicModel.OutputLimit = canonicalApplicationIntPointer(defaultPublicOutputLimit)
			}
			if request.PublicModel.ContextLength == nil || request.PublicModel.OutputLimit == nil ||
				modelprofile.ValidateEffectiveLimits(profile, *request.PublicModel.ContextLength, *request.PublicModel.OutputLimit) != nil {
				return request, errCanonicalApplicationInvalid
			}
			if request.Deployment.MaxContext == nil {
				request.Deployment.MaxContext = canonicalApplicationIntPointer(*request.PublicModel.ContextLength)
			}
			expectedRequestDefault := modelprofile.DefaultRequestOutputLimit(profile, *request.PublicModel.OutputLimit)
			if request.Deployment.DefaultMaxOutput == nil {
				request.Deployment.DefaultMaxOutput = canonicalApplicationIntPointer(expectedRequestDefault)
			}
			if *request.Deployment.MaxContext > *request.PublicModel.ContextLength ||
				*request.Deployment.DefaultMaxOutput > *request.PublicModel.OutputLimit ||
				// Canonical request default is model-level because one request can
				// fan out across multiple routes. Until the planner owns a distinct
				// per-deployment default-selection contract, this stored field is a
				// projection/evidence value and must not advertise an inert override.
				*request.Deployment.DefaultMaxOutput != expectedRequestDefault ||
				modelprofile.ValidateEffectiveLimits(
					profile, *request.Deployment.MaxContext, *request.Deployment.DefaultMaxOutput,
				) != nil {
				return request, errCanonicalApplicationInvalid
			}
		}
	}
	if _, err := modeldeployment.NormalizeIdentity(request.Deployment.UpstreamModel, 200); err != nil {
		return request, errCanonicalApplicationInvalid
	}
	if (request.Deployment.Priority != nil && (*request.Deployment.Priority < 1 || *request.Deployment.Priority > 10000)) ||
		(request.Deployment.RoutingWeight != nil && (*request.Deployment.RoutingWeight < 1 || *request.Deployment.RoutingWeight > 1000)) ||
		(request.Deployment.DegradationThreshold != nil && (*request.Deployment.DegradationThreshold < 1 || *request.Deployment.DegradationThreshold > 100)) ||
		(request.Deployment.MaxContext != nil && *request.Deployment.MaxContext < 0) ||
		(request.Deployment.DefaultMaxOutput != nil && *request.Deployment.DefaultMaxOutput < 0) {
		return request, errCanonicalApplicationInvalid
	}
	operationID, err := modeldeployment.NormalizeOperationID(request.Operation.OperationID)
	if err != nil {
		return request, errCanonicalApplicationInvalid
	}
	if _, ok := modeloperation.LookupID(modeloperation.ID(operationID)); !ok || operationID == string(modeloperation.IDModelsList) {
		return request, errCanonicalApplicationInvalid
	}
	request.Operation.OperationID = operationID

	if (request.Credential.BindingID > 0) == (request.Credential.APIKey != "") {
		return request, errCanonicalApplicationInvalid
	}
	request.Connection.Name = strings.TrimSpace(request.Connection.Name)
	request.Connection.Provider = strings.TrimSpace(request.Connection.Provider)
	request.Connection.BaseURL = strings.Trim(request.Connection.BaseURL, " ")
	request.Connection.AdapterType = strings.TrimSpace(request.Connection.AdapterType)
	if request.Connection.ID == 0 {
		if _, err := modeldeployment.NormalizeIdentity(request.Connection.Name, 255); err != nil {
			return request, errCanonicalApplicationInvalid
		}
		request.Connection.AdapterType = adapters.NormalizeType(request.Connection.AdapterType)
	} else if len(request.Connection.Name) > 255 || strings.ContainsAny(request.Connection.Name, "\x00\r\n") {
		return request, errCanonicalApplicationInvalid
	}
	if request.Connection.ID == 0 || request.Connection.Provider != "" {
		if _, err := modeldeployment.NormalizeIdentity(request.Connection.Provider, 50); err != nil {
			return request, errCanonicalApplicationInvalid
		}
	}
	if request.Connection.ID == 0 || request.Connection.BaseURL != "" {
		if _, err := modeldeployment.NormalizeIdentity(request.Connection.BaseURL, 512); err != nil || !validCanonicalProviderBaseURL(request.Connection.BaseURL) {
			return request, errCanonicalApplicationInvalid
		}
	}
	if request.Connection.ID == 0 || request.Connection.AdapterType != "" {
		if !adapters.DefaultRegistry().Has(request.Connection.AdapterType) {
			return request, errCanonicalApplicationInvalid
		}
	}
	if request.Connection.ID == 0 && len(request.Connection.AdapterConfig) == 0 {
		request.Connection.AdapterConfig = json.RawMessage(`{}`)
	}
	if len(request.Connection.AdapterConfig) > 0 {
		canonicalConfig, err := strictjson.Canonicalize(request.Connection.AdapterConfig)
		if err != nil || len(canonicalConfig) == 0 || canonicalConfig[0] != '{' || len(canonicalConfig) > canonicalModelApplicationMaxAdapterConfigBytes {
			return request, errCanonicalApplicationInvalid
		}
		if request.Connection.AdapterType != "" && adapters.DefaultRegistry().ValidateConfig(request.Connection.AdapterType, canonicalConfig) != nil {
			return request, errCanonicalApplicationInvalid
		}
		request.Connection.AdapterConfig = canonicalConfig
	}
	return request, nil
}

func validateCanonicalQualificationOrigin(
	ctx context.Context,
	tx pgx.Tx,
	orgID int64,
	request canonicalModelApplicationCreateRequest,
) error {
	if !modelqualification.IsQualificationRequired(request.Operation.OperationID) {
		return nil
	}
	baseURL := request.Connection.BaseURL
	if request.Connection.ID > 0 {
		if err := tx.QueryRow(ctx, `
SELECT pc.base_url
  FROM provider_connections pc
 WHERE pc.id=$1
   AND NOT EXISTS (
     SELECT 1 FROM model_deployments foreign_deployment
      WHERE foreign_deployment.connection_id=pc.id
        AND foreign_deployment.customer_org_id<>$2
   )
 FOR SHARE`, request.Connection.ID, orgID).Scan(&baseURL); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errCanonicalApplicationNotFound
			}
			return fmt.Errorf("read canonical provider connection origin: %w", err)
		}
	}
	if _, err := modelqualification.NormalizeHTTPSOrigin(baseURL); err != nil {
		if request.Connection.ID > 0 {
			return errCanonicalApplicationQualificationOriginConflict
		}
		return errCanonicalApplicationQualificationOriginInvalid
	}
	return nil
}

// resolveCanonicalOperationProfile is the server-owned provider-template
// registry. Clients select an operation; they cannot promote an arbitrary
// profile string into execution evidence. Contract-only endpointprotocol
// profiles may be declared, but remain unverified until a server-side probe
// and later evidence transition says otherwise.
func resolveCanonicalOperationProfile(input canonicalOperationInput, adapterType string) (canonicalOperationInput, error) {
	switch modeloperation.ID(input.OperationID) {
	case modeloperation.IDChatCompletions:
		if adapterType != adapters.TypeOpenAICompatible && adapterType != adapters.TypeAzureOpenAI {
			return input, errCanonicalApplicationInvalid
		}
		input.ProtocolProfileID, input.ProtocolProfileVersion = "openai.chat_completions", "1.0.0"
	case modeloperation.IDEmbeddings:
		if adapterType != adapters.TypeOpenAICompatible && adapterType != adapters.TypeAzureOpenAI {
			return input, errCanonicalApplicationInvalid
		}
		input.ProtocolProfileID, input.ProtocolProfileVersion = "openai.embeddings", "1.0.0"
	case modeloperation.IDResponses:
		if adapterType != adapters.TypeOpenAICompatible {
			return input, errCanonicalApplicationInvalid
		}
		input.ProtocolProfileID, input.ProtocolProfileVersion = endpointprotocol.ProfileOpenAIResponsesText, "1.0.0"
		if _, err := endpointprotocol.NewRegistry().Resolve(input.ProtocolProfileID, input.ProtocolProfileVersion); err != nil {
			return input, errCanonicalApplicationInvalid
		}
	case modeloperation.IDMessages:
		if adapterType != adapters.TypeAnthropicNative {
			return input, errCanonicalApplicationInvalid
		}
		input.ProtocolProfileID, input.ProtocolProfileVersion = endpointprotocol.ProfileAnthropicMessagesText, "1.0.0"
		if _, err := endpointprotocol.NewRegistry().Resolve(input.ProtocolProfileID, input.ProtocolProfileVersion); err != nil {
			return input, errCanonicalApplicationInvalid
		}
	default:
		return input, errCanonicalApplicationInvalid
	}
	return input, nil
}

// resolveCanonicalTypedOperationProfile applies only a modelprofile-owned
// exact protocol override to the independently validated canonical base
// operation. NoAuthority preserves that base; an invalid typed declaration is
// never silently downgraded to it.
func resolveCanonicalTypedOperationProfile(
	publicModel canonicalPublicModelIdentity,
	operation canonicalOperationInput,
) (canonicalOperationInput, error) {
	override, found, err := modelprofile.ResolveOperationProtocolIdentity(
		publicModel.ExecutionProfileID, publicModel.ExecutionProfileVersion,
		operation.OperationID,
	)
	if err != nil {
		return canonicalOperationInput{}, errCanonicalApplicationInvalid
	}
	if !found {
		return operation, nil
	}
	operation.ProtocolProfileID = override.ProtocolProfileID
	operation.ProtocolProfileVersion = override.ProtocolProfileVersion
	return operation, nil
}

func validCanonicalProviderBaseURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" &&
		parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validateCanonicalProfileProductBinding(
	publicModel canonicalPublicModelIdentity,
	connection canonicalConnectionIdentity,
	upstreamModel string,
	operation canonicalOperationInput,
) error {
	profile, ok := modelprofile.Lookup(publicModel.ExecutionProfileID, publicModel.ExecutionProfileVersion)
	if !ok {
		return errCanonicalApplicationInvalid
	}
	endpointPathMode := ""
	if modelprofile.RequiredEndpointPathMode(profile) != "" {
		var err error
		endpointPathMode, err = adapters.EffectiveEndpointPathMode(connection.AdapterType, connection.AdapterConfig)
		if err != nil {
			return errCanonicalApplicationInvalid
		}
	}
	if err := modelprofile.ValidateProductBinding(profile, modelprofile.ProductBinding{
		Provider: connection.Provider, BaseURL: connection.BaseURL,
		AdapterType: connection.AdapterType, EndpointPathMode: endpointPathMode, OperationID: operation.OperationID,
		ProtocolProfileID: operation.ProtocolProfileID, ProtocolProfileVersion: operation.ProtocolProfileVersion,
		UpstreamModel: upstreamModel,
	}); err != nil {
		return errCanonicalApplicationInvalid
	}
	return nil
}

func validateCanonicalNewDeploymentProfile(publicModel canonicalPublicModelIdentity, deploymentReused bool) error {
	if deploymentReused || modelprofile.ValidateNewAttachment(
		publicModel.ExecutionProfileID,
		publicModel.ExecutionProfileVersion,
	) == nil {
		return nil
	}
	// Historical Profiles remain valid for reads, routing, sealed releases,
	// and exact reuse of an existing deployment. They are not an onboarding
	// surface: the caller rolls a failed new attachment back with its transaction.
	return errCanonicalApplicationInvalid
}

func validateCanonicalCustomerOrg(ctx context.Context, tx pgx.Tx, orgID int64) error {
	if orgID <= 0 {
		return errCanonicalApplicationInvalid
	}
	// ProviderConnection is deployment-level state in the accepted one-customer
	// on-prem topology. Serialize root-customer authority checks so an empty
	// connection can only ever be claimed by that sole customer.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(8675309, 4)`); err != nil {
		return fmt.Errorf("lock canonical customer authority: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT id FROM orgs
 WHERE status='active' AND org_type='customer' AND parent_id IS NULL
 ORDER BY id FOR SHARE`)
	if err != nil {
		return fmt.Errorf("read canonical customer authority: %w", err)
	}
	defer rows.Close()
	rootIDs := make([]int64, 0, 2)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan canonical customer authority: %w", err)
		}
		rootIDs = append(rootIDs, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read canonical customer authority: %w", err)
	}
	if len(rootIDs) != 1 || rootIDs[0] != orgID {
		return errCanonicalApplicationConflict
	}
	return nil
}

// validateCanonicalPublicModelRename keeps a mutable display label separate
// from the stable public API identity. The application guard is fail-closed
// until the coordinated database-level immutability constraint is available;
// every admin writer for model_registry.model_name must call it while holding
// the registry row lock.
func validateCanonicalPublicModelRename(currentName, requestedName string) error {
	requestedName = strings.TrimSpace(requestedName)
	if requestedName == currentName {
		return nil
	}
	if _, err := modeldeployment.NormalizeIdentity(requestedName, 100); err != nil {
		return errCanonicalApplicationInvalid
	}
	// model_name is the stable public API identity from creation onward. Legacy
	// catalog, authorization, pricing, and model-set relations still reference
	// it by value, so a partial dependency scan would permit orphaning weak
	// references. Operators rename display_name or manage aliases instead.
	return errCanonicalApplicationConflict
}

func ensureCanonicalConnection(ctx context.Context, tx pgx.Tx, orgID int64, input canonicalConnectionInput) (canonicalConnectionIdentity, bool, error) {
	if input.ID > 0 {
		var state, name, provider, baseURL, adapterType string
		var adapterConfig []byte
		var identity canonicalConnectionIdentity
		if err := tx.QueryRow(ctx, `
SELECT pc.id,pc.name,pc.provider,pc.base_url,pc.adapter_type,pc.adapter_config,pc.admin_state
  FROM provider_connections pc
 WHERE pc.id=$1
   AND NOT EXISTS (
     SELECT 1 FROM model_deployments foreign_deployment
      WHERE foreign_deployment.connection_id=pc.id
        AND foreign_deployment.customer_org_id<>$2
   )
 FOR SHARE`, input.ID, orgID).Scan(
			&identity.ID, &name, &provider, &baseURL, &adapterType, &adapterConfig, &state,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return canonicalConnectionIdentity{}, false, errCanonicalApplicationNotFound
			}
			return canonicalConnectionIdentity{}, false, fmt.Errorf("read canonical provider connection: %w", err)
		}
		if state == "retired" {
			return canonicalConnectionIdentity{}, false, modeldeployment.ErrRetiredIdentity
		}
		if (input.Name != "" && input.Name != name) ||
			(input.Provider != "" && input.Provider != provider) ||
			(input.BaseURL != "" && input.BaseURL != baseURL) ||
			(input.AdapterType != "" && input.AdapterType != adapterType) ||
			(len(input.AdapterConfig) > 0 && !canonicalJSONEqual(input.AdapterConfig, adapterConfig)) {
			return canonicalConnectionIdentity{}, false, errCanonicalApplicationConflict
		}
		if len(input.AdapterConfig) > 0 && adapters.DefaultRegistry().ValidateConfig(adapterType, input.AdapterConfig) != nil {
			return canonicalConnectionIdentity{}, false, errCanonicalApplicationInvalid
		}
		identity.Provider, identity.BaseURL, identity.AdapterType = provider, baseURL, adapterType
		identity.AdapterConfig = json.RawMessage(adapterConfig)
		return identity, true, nil
	}
	// No implicit lookup: two connections may intentionally share the same
	// provider/base URL/config. Reuse always requires an explicit persistent ID.
	var id int64
	if err := tx.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config,admin_state)
VALUES ($1,$2,$3,$4,$5::jsonb,'enabled') RETURNING id`,
		input.Name, input.Provider, input.BaseURL, input.AdapterType, string(input.AdapterConfig)).Scan(&id); err != nil {
		return canonicalConnectionIdentity{}, false, fmt.Errorf("create canonical provider connection: %w", err)
	}
	return canonicalConnectionIdentity{
		ID: id, Provider: input.Provider, BaseURL: input.BaseURL,
		AdapterType: input.AdapterType, AdapterConfig: json.RawMessage(input.AdapterConfig),
	}, false, nil
}

func canonicalJSONEqual(left, right []byte) bool {
	leftCanonical, leftErr := strictjson.Canonicalize(left)
	rightCanonical, rightErr := strictjson.Canonicalize(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return bytes.Equal(leftCanonical, rightCanonical)
}

func ensureCanonicalPublicModel(ctx context.Context, tx pgx.Tx, orgID int64, input canonicalPublicModelInput, provider string) (canonicalPublicModelIdentity, bool, error) {
	if input.ID > 0 {
		var modelName, displayName, providerFamily, lifecycle, executionProfileID, executionProfileVersion string
		var contextLength, outputLimit int
		var approved bool
		if err := tx.QueryRow(ctx, `
SELECT model_name,display_name,provider_family,lifecycle,context_length,output_limit,approved_catalog,
       execution_profile_id,execution_profile_version
  FROM model_registry WHERE id=$1 AND org_id=$2 FOR UPDATE`, input.ID, orgID).Scan(
			&modelName, &displayName, &providerFamily, &lifecycle, &contextLength, &outputLimit, &approved,
			&executionProfileID, &executionProfileVersion,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return canonicalPublicModelIdentity{}, false, errCanonicalApplicationNotFound
			}
			return canonicalPublicModelIdentity{}, false, fmt.Errorf("read canonical public model: %w", err)
		}
		if (input.ModelName != "" && modelName != input.ModelName) ||
			(input.DisplayName != "" && displayName != input.DisplayName) ||
			(input.ProviderFamily != "" && providerFamily != input.ProviderFamily) ||
			(input.Lifecycle != "" && lifecycle != input.Lifecycle) ||
			(input.ContextLength != nil && contextLength != *input.ContextLength) ||
			(input.OutputLimit != nil && outputLimit != *input.OutputLimit) ||
			(input.ApprovedCatalog != nil && approved != *input.ApprovedCatalog) ||
			(input.ExecutionProfileID != "" && executionProfileID != input.ExecutionProfileID) ||
			(input.ExecutionProfileVersion != "" && executionProfileVersion != input.ExecutionProfileVersion) {
			// Never treat a mutable spelling as a selector. Once a caller supplies
			// an ID, any additional field is an assertion, not a mutation.
			return canonicalPublicModelIdentity{}, false, errCanonicalApplicationConflict
		}
		if lifecycle == "retired" {
			return canonicalPublicModelIdentity{}, false, modeldeployment.ErrRetiredIdentity
		}
		return canonicalPublicModelIdentity{
			ID: input.ID, ExecutionProfileID: executionProfileID, ExecutionProfileVersion: executionProfileVersion,
		}, true, nil
	}
	providerFamily := input.ProviderFamily
	if providerFamily == "" {
		providerFamily = provider
	}
	if len(providerFamily) > 50 || strings.ContainsAny(providerFamily, "\x00\r\n") {
		return canonicalPublicModelIdentity{}, false, errCanonicalApplicationInvalid
	}
	var id int64
	contextLength, outputLimit, approved := 0, 0, false
	if input.ContextLength != nil {
		contextLength = *input.ContextLength
	}
	if input.OutputLimit != nil {
		outputLimit = *input.OutputLimit
	}
	if input.ApprovedCatalog != nil {
		approved = *input.ApprovedCatalog
	}
	err := tx.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,context_length,
  output_limit,pricing_model_name,approved_catalog,execution_profile_id,execution_profile_version)
VALUES ($1,$2,$3,$4,$5,$6,$7,$2,$8,$9,$10)
ON CONFLICT (org_id,model_name) DO NOTHING
RETURNING id`, orgID, input.ModelName, input.DisplayName, providerFamily, input.Lifecycle,
		contextLength, outputLimit, approved, input.ExecutionProfileID, input.ExecutionProfileVersion).Scan(&id)
	if err == nil {
		return canonicalPublicModelIdentity{
			ID: id, ExecutionProfileID: input.ExecutionProfileID, ExecutionProfileVersion: input.ExecutionProfileVersion,
		}, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return canonicalPublicModelIdentity{}, false, fmt.Errorf("create canonical public model: %w", err)
	}
	var existingDisplayName, existingProviderFamily, existingLifecycle, existingExecutionProfileID, existingExecutionProfileVersion string
	var existingContextLength, existingOutputLimit int
	var existingApproved bool
	if err := tx.QueryRow(ctx, `
SELECT id,display_name,provider_family,lifecycle,context_length,output_limit,approved_catalog,
       execution_profile_id,execution_profile_version
  FROM model_registry
 WHERE org_id=$1 AND model_name=$2
 FOR UPDATE`, orgID, input.ModelName).Scan(
		&id, &existingDisplayName, &existingProviderFamily, &existingLifecycle,
		&existingContextLength, &existingOutputLimit, &existingApproved,
		&existingExecutionProfileID, &existingExecutionProfileVersion,
	); err != nil {
		return canonicalPublicModelIdentity{}, false, fmt.Errorf("reuse canonical public model: %w", err)
	}
	if existingLifecycle == "retired" {
		return canonicalPublicModelIdentity{}, false, modeldeployment.ErrRetiredIdentity
	}
	// ID=0 is a deterministic create-or-exact-retry contract, never an
	// implicit natural-key update. Normalized defaults are part of that
	// contract: a retry that omits optional fields resolves to the same defaults,
	// while any configuration drift must use the stable ID plus an explicit
	// revisioned update surface.
	if existingDisplayName != input.DisplayName ||
		existingProviderFamily != providerFamily ||
		existingLifecycle != input.Lifecycle ||
		existingContextLength != contextLength ||
		existingOutputLimit != outputLimit ||
		existingApproved != approved ||
		existingExecutionProfileID != input.ExecutionProfileID ||
		existingExecutionProfileVersion != input.ExecutionProfileVersion {
		return canonicalPublicModelIdentity{}, false, errCanonicalApplicationConflict
	}
	return canonicalPublicModelIdentity{
		ID: id, ExecutionProfileID: existingExecutionProfileID, ExecutionProfileVersion: existingExecutionProfileVersion,
	}, true, nil
}

func ensureCanonicalDeployment(ctx context.Context, tx pgx.Tx, orgID, publicModelID, connectionID int64, input canonicalDeploymentInput) (int64, bool, error) {
	priority, routingWeight, maxContext, defaultMaxOutput, threshold := 100, 100, 0, 0, 10
	if input.Priority != nil {
		priority = *input.Priority
	}
	if input.RoutingWeight != nil {
		routingWeight = *input.RoutingWeight
	}
	if input.MaxContext != nil {
		maxContext = *input.MaxContext
	}
	if input.DefaultMaxOutput != nil {
		defaultMaxOutput = *input.DefaultMaxOutput
	}
	if input.DegradationThreshold != nil {
		threshold = *input.DegradationThreshold
	}
	var id int64
	err := tx.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
  priority,routing_weight,max_context,default_max_output,degradation_threshold)
VALUES ($1,$2,$3,$4,'enabled',$5,$6,$7,$8,$9)
ON CONFLICT (customer_org_id,connection_id,public_model_id,upstream_model)
  WHERE admin_state<>'retired' DO NOTHING
	RETURNING id`, orgID, publicModelID, connectionID, input.UpstreamModel, priority,
		routingWeight, maxContext, defaultMaxOutput, threshold).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, fmt.Errorf("create canonical model deployment: %w", err)
	}
	var storedPriority, storedWeight, storedMaxContext, storedMaxOutput, storedThreshold int
	if err := tx.QueryRow(ctx, `
SELECT id,priority,routing_weight,max_context,default_max_output,degradation_threshold
  FROM model_deployments
 WHERE customer_org_id=$1 AND public_model_id=$2 AND connection_id=$3
   AND upstream_model=$4 AND admin_state<>'retired'
	 FOR SHARE`, orgID, publicModelID, connectionID, input.UpstreamModel).Scan(
		&id, &storedPriority, &storedWeight, &storedMaxContext, &storedMaxOutput, &storedThreshold,
	); err != nil {
		return 0, false, fmt.Errorf("reuse canonical model deployment: %w", err)
	}
	// A natural tuple is only an idempotent retry when its complete effective
	// configuration matches, including server defaults for omitted fields. A
	// future revisioned endpoint may reuse a non-default row by deployment ID;
	// this create contract never silently accepts drift.
	if storedPriority != priority || storedWeight != routingWeight ||
		storedMaxContext != maxContext || storedMaxOutput != defaultMaxOutput ||
		storedThreshold != threshold {
		return 0, false, errCanonicalApplicationConflict
	}
	return id, true, nil
}

func ensureCanonicalCredentialBinding(ctx context.Context, tx pgx.Tx, connectionID, deploymentID, bindingID int64, encryptedKey string) (int64, bool, error) {
	reused := bindingID > 0
	if bindingID > 0 {
		var status string
		if err := tx.QueryRow(ctx, `
SELECT status FROM provider_credential_bindings
 WHERE id=$1 AND connection_id=$2 FOR SHARE`, bindingID, connectionID).Scan(&status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, false, errCanonicalApplicationNotFound
			}
			return 0, false, fmt.Errorf("read canonical provider credential: %w", err)
		}
		if status == "retired" {
			return 0, false, modeldeployment.ErrRetiredIdentity
		}
	} else {
		if err := tx.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status)
VALUES ($1,$2,'active') RETURNING id`, connectionID, encryptedKey).Scan(&bindingID); err != nil {
			return 0, false, fmt.Errorf("create canonical provider credential: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3) ON CONFLICT (binding_id,deployment_id) DO NOTHING`, bindingID, deploymentID, connectionID); err != nil {
		return 0, false, fmt.Errorf("bind canonical provider credential: %w", err)
	}
	return bindingID, reused, nil
}

func ensureCanonicalOperation(ctx context.Context, tx pgx.Tx, deploymentID int64, input canonicalOperationInput) (bool, error) {
	tag, err := tx.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','declared','',1,$3,$4)
ON CONFLICT (deployment_id,operation_id) DO NOTHING`, deploymentID, input.OperationID, input.ProtocolProfileID, input.ProtocolProfileVersion)
	if err != nil {
		return false, fmt.Errorf("declare canonical model operation: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return false, nil
	}
	var profileID, profileVersion, supportState string
	if err := tx.QueryRow(ctx, `
SELECT protocol_profile_id,protocol_profile_version,support_state
  FROM model_deployment_operations
 WHERE deployment_id=$1 AND operation_id=$2 FOR SHARE`, deploymentID, input.OperationID).Scan(&profileID, &profileVersion, &supportState); err != nil {
		return false, fmt.Errorf("reuse canonical model operation: %w", err)
	}
	if profileID != input.ProtocolProfileID || profileVersion != input.ProtocolProfileVersion || supportState != "enabled" {
		return false, errCanonicalApplicationConflict
	}
	return true, nil
}

// ensureCanonicalDeclaredCapabilities materializes only capabilities for which
// the compiled Profile contract and the migration-owned support tuple agree
// exactly. It records supported/declared state; it never creates probe,
// approval, Release, or routing evidence and never overwrites an existing
// explicit support decision.
func ensureCanonicalDeclaredCapabilities(
	ctx context.Context,
	tx pgx.Tx,
	deploymentID int64,
	publicModel canonicalPublicModelIdentity,
	connection canonicalConnectionIdentity,
	upstreamModel string,
	operation canonicalOperationInput,
) error {
	bindings, err := modelcapabilityqualification.ResolveAll(
		operation.OperationID,
		publicModel.ExecutionProfileID, publicModel.ExecutionProfileVersion,
		operation.ProtocolProfileID, operation.ProtocolProfileVersion,
	)
	if errors.Is(err, modelcapabilityqualification.ErrUnsupported) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve canonical capability declaration: %w", err)
	}
	for _, binding := range bindings {
		if connection.Provider != binding.ExpectedProvider || upstreamModel != binding.ExpectedUpstreamModel {
			return errCanonicalApplicationConflict
		}

		var databaseBinding modelcapabilityqualification.Binding
		err = tx.QueryRow(ctx, `
SELECT current_contract.operation_id,current_contract.capability_id,
       current_contract.execution_profile_id,current_contract.execution_profile_version,
       current_contract.protocol_profile_id,current_contract.protocol_profile_version,
       current_contract.probe_contract_id,current_contract.probe_contract_version,
       contract.expected_provider,contract.expected_upstream_model,contract.public_path
  FROM model_capability_current_probe_contracts current_contract
  JOIN model_capability_probe_contract_bindings contract
    ON contract.operation_id=current_contract.operation_id
   AND contract.capability_id=current_contract.capability_id
   AND contract.execution_profile_id=current_contract.execution_profile_id
   AND contract.execution_profile_version=current_contract.execution_profile_version
   AND contract.protocol_profile_id=current_contract.protocol_profile_id
   AND contract.protocol_profile_version=current_contract.protocol_profile_version
   AND contract.probe_contract_id=current_contract.probe_contract_id
   AND contract.probe_contract_version=current_contract.probe_contract_version
 WHERE current_contract.operation_id=$1 AND current_contract.capability_id=$2
   AND current_contract.execution_profile_id=$3
   AND current_contract.execution_profile_version=$4
   AND current_contract.protocol_profile_id=$5
   AND current_contract.protocol_profile_version=$6`,
			binding.OperationID, binding.CapabilityID,
			binding.ExecutionProfileID, binding.ExecutionProfileVersion,
			binding.ProtocolProfileID, binding.ProtocolProfileVersion,
		).Scan(
			&databaseBinding.OperationID, &databaseBinding.CapabilityID,
			&databaseBinding.ExecutionProfileID, &databaseBinding.ExecutionProfileVersion,
			&databaseBinding.ProtocolProfileID, &databaseBinding.ProtocolProfileVersion,
			&databaseBinding.ProbeContractID, &databaseBinding.ProbeContractVersion,
			&databaseBinding.ExpectedProvider, &databaseBinding.ExpectedUpstreamModel,
			&databaseBinding.PublicPath,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return errCanonicalApplicationConflict
		}
		if err != nil {
			return fmt.Errorf("read canonical capability declaration contract: %w", err)
		}
		if databaseBinding != binding {
			return errCanonicalApplicationConflict
		}

		evidenceRef := "capability_contract:" + binding.ProbeContractID + "@" + binding.ProbeContractVersion
		if _, err := tx.Exec(ctx, `
INSERT INTO model_deployment_capabilities (
  deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision)
VALUES ($1,$2,$3,'enabled','declared',$4,1)
ON CONFLICT (deployment_id,operation_id,capability_id) DO NOTHING`,
			deploymentID, binding.OperationID, binding.CapabilityID, evidenceRef,
		); err != nil {
			return fmt.Errorf("declare canonical model capability: %w", err)
		}
	}
	return nil
}

func (h *Handler) loadCanonicalModelApplications(ctx context.Context, orgID, deploymentID int64, operationID string) (canonicalModelApplicationProjection, error) {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return canonicalModelApplicationProjection{}, fmt.Errorf("begin canonical application snapshot: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY`); err != nil {
		return canonicalModelApplicationProjection{}, fmt.Errorf("configure canonical application snapshot: %w", err)
	}
	var generatedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&generatedAt); err != nil {
		return canonicalModelApplicationProjection{}, fmt.Errorf("timestamp canonical application snapshot: %w", err)
	}
	projection, err := h.loadCanonicalModelApplicationsSnapshot(ctx, tx, orgID, deploymentID, operationID, generatedAt)
	if err != nil {
		return projection, err
	}
	if err := tx.Commit(ctx); err != nil {
		return canonicalModelApplicationProjection{}, fmt.Errorf("commit canonical application snapshot: %w", err)
	}
	return projection, nil
}

func (h *Handler) loadCanonicalModelApplicationsSnapshot(
	ctx context.Context,
	store canonicalApplicationReadStore,
	orgID, deploymentID int64,
	operationID string,
	generatedAt time.Time,
) (canonicalModelApplicationProjection, error) {
	projection := canonicalModelApplicationProjection{
		ContractVersion: canonicalModelApplicationContractVersion,
		GeneratedAt:     generatedAt.UTC(), Applications: make([]canonicalModelApplication, 0),
	}
	rows, err := store.Query(ctx, `
SELECT mr.id,mr.model_name,mr.display_name,mr.provider_family,mr.lifecycle,
	       mr.context_length,mr.output_limit,mr.approved_catalog,
	       mr.execution_profile_id,mr.execution_profile_version,
       md.id,md.upstream_model,md.admin_state,md.config_revision,md.priority,
       md.routing_weight,md.max_context,md.default_max_output,md.degradation_threshold,
       md.circuit_state,md.circuit_revision,md.fail_count,md.retired_at,
	       pc.id,pc.name,pc.provider,pc.base_url,pc.adapter_type,
	       COALESCE(pc.adapter_config,'{}'::jsonb),pc.admin_state,
       pc.config_revision,pc.retired_at,
       slot.slot_version,slot.canary_basis_points,
       stable.release_ref,stable.stage,stable.version,
       canary.release_ref,canary.stage,canary.version
  FROM model_deployments md
  JOIN model_registry mr ON mr.id=md.public_model_id AND mr.org_id=md.customer_org_id
  JOIN provider_connections pc ON pc.id=md.connection_id
  LEFT JOIN runtime_change_slots slot ON slot.public_model_id=mr.id
  LEFT JOIN runtime_change_releases stable ON stable.id=slot.stable_release_id
  LEFT JOIN runtime_change_releases canary ON canary.id=slot.canary_release_id
 WHERE md.customer_org_id=$1 AND ($2::bigint=0 OR md.id=$2)
   AND ($3::text='' OR EXISTS (
     SELECT 1 FROM model_deployment_operations selected_operation
      WHERE selected_operation.deployment_id=md.id
        AND selected_operation.operation_id=$3
   ))
 ORDER BY mr.model_name,md.priority,md.id`, orgID, deploymentID, operationID)
	if err != nil {
		return projection, fmt.Errorf("list canonical model applications: %w", err)
	}
	defer rows.Close()
	byDeployment := make(map[int64]int)
	for rows.Next() {
		item := canonicalModelApplication{ContractVersion: canonicalModelApplicationContractVersion, Operations: make([]canonicalOperationAxis, 0), Actions: canonicalActionMeta()}
		item.Callability = canonicalCallabilityAxis{Evaluated: false, State: "not_evaluated", ReasonCode: "caller_context_absent"}
		var deploymentRetired, connectionRetired sql.NullTime
		var slotVersion sql.NullInt64
		var canaryBPS sql.NullInt64
		var stableRef, stableStage, canaryRef, canaryStage sql.NullString
		var stableVersion, canaryVersion sql.NullInt64
		if err := rows.Scan(
			&item.PublicModel.ID, &item.PublicModel.ModelName, &item.PublicModel.DisplayName,
			&item.PublicModel.ProviderFamily, &item.PublicModel.Lifecycle,
			&item.PublicModel.ContextLength, &item.PublicModel.OutputLimit, &item.PublicModel.ApprovedCatalog,
			&item.PublicModel.ExecutionProfileID, &item.PublicModel.ExecutionProfileVersion,
			&item.Deployment.ID, &item.Deployment.UpstreamModel, &item.Deployment.AdminState,
			&item.Deployment.ConfigRevision, &item.Deployment.Priority, &item.Deployment.RoutingWeight,
			&item.Deployment.MaxContext, &item.Deployment.DefaultMaxOutput,
			&item.Deployment.DegradationThreshold, &item.Deployment.CircuitState,
			&item.Deployment.CircuitRevision, &item.Deployment.FailCount, &deploymentRetired,
			&item.Connection.ID, &item.Connection.Name, &item.Connection.Provider,
			&item.Connection.BaseURL, &item.Connection.AdapterType, &item.Connection.AdapterConfig,
			&item.Connection.AdminState,
			&item.Connection.ConfigRevision, &connectionRetired,
			&slotVersion, &canaryBPS, &stableRef, &stableStage, &stableVersion,
			&canaryRef, &canaryStage, &canaryVersion,
		); err != nil {
			return projection, fmt.Errorf("scan canonical model application: %w", err)
		}
		if deploymentRetired.Valid {
			value := deploymentRetired.Time
			item.Deployment.RetiredAt = &value
		}
		if connectionRetired.Valid {
			value := connectionRetired.Time
			item.Connection.RetiredAt = &value
		}
		item.Runtime = canonicalRuntimeAxis{State: "unassigned", ReasonCode: "no_runtime_slot"}
		if slotVersion.Valid {
			item.Runtime.SlotVersion = slotVersion.Int64
			item.Runtime.CanaryBasisPoints = int(canaryBPS.Int64)
			item.Runtime.StableReleaseRef, item.Runtime.StableReleaseStage = stableRef.String, stableStage.String
			item.Runtime.StableReleaseVersion = stableVersion.Int64
			item.Runtime.CanaryReleaseRef, item.Runtime.CanaryReleaseStage = canaryRef.String, canaryStage.String
			item.Runtime.CanaryReleaseVersion = canaryVersion.Int64
			switch {
			case canaryRef.Valid:
				item.Runtime.State, item.Runtime.ReasonCode = "slot_present", "public_model_canary_slot_present"
			case stableRef.Valid:
				item.Runtime.State, item.Runtime.ReasonCode = "slot_present", "public_model_stable_slot_present"
			default:
				item.Runtime.State, item.Runtime.ReasonCode = "empty", "runtime_slot_has_no_release"
			}
		}
		item.Credential = canonicalCredentialAxis{State: "missing", ReasonCode: "no_bound_credential", Bindings: make([]canonicalCredentialBindingRef, 0)}
		byDeployment[item.Deployment.ID] = len(projection.Applications)
		projection.Applications = append(projection.Applications, item)
	}
	if err := rows.Err(); err != nil {
		return projection, fmt.Errorf("read canonical model applications: %w", err)
	}
	if deploymentID > 0 && len(projection.Applications) == 0 {
		return projection, errCanonicalApplicationNotFound
	}
	// An operation filter may legitimately exclude every deployment for a
	// public model. Keep that as an empty canonical projection so readiness can
	// report the missing deployment/operation axes. The credential and operation
	// loaders intentionally query the wider deployment inventory and therefore
	// must not run without an owner index.
	if len(projection.Applications) == 0 {
		return projection, nil
	}

	if err := h.loadCanonicalCredentials(ctx, store, orgID, deploymentID, byDeployment, projection.Applications); err != nil {
		return projection, err
	}
	if err := h.loadCanonicalOperations(ctx, store, orgID, deploymentID, operationID, byDeployment, projection.Applications); err != nil {
		return projection, err
	}
	for index := range projection.Applications {
		item := &projection.Applications[index]
		projection.Summary.Deployments++
		if item.Connection.AdminState != "enabled" {
			projection.Summary.ConnectionsPaused++
		}
		if item.Credential.EligibleCount > 0 {
			projection.Summary.CredentialsReady++
		}
		for _, operation := range item.Operations {
			if operation.ProbeState == "ready" {
				projection.Summary.ProbesReady++
			}
		}
		for _, operation := range item.Operations {
			if operation.Runtime.State == "stable" || operation.Runtime.State == "canary" {
				projection.Summary.RuntimeAssignedOperations++
			}
		}
	}
	return projection, nil
}

func (h *Handler) loadCanonicalCredentials(ctx context.Context, store canonicalApplicationReadStore, orgID, deploymentID int64, index map[int64]int, applications []canonicalModelApplication) error {
	rows, err := store.Query(ctx, `
SELECT md.id,b.id,b.status,b.state_revision,b.fail_count,b.last_used_at,b.cooldown_until,
       (b.status='active' AND (b.cooldown_until IS NULL OR b.cooldown_until<=NOW())) AS eligible
  FROM model_deployments md
  JOIN credential_binding_deployments link ON link.deployment_id=md.id AND link.connection_id=md.connection_id
  JOIN provider_credential_bindings b ON b.id=link.binding_id AND b.connection_id=link.connection_id
 WHERE md.customer_org_id=$1 AND ($2::bigint=0 OR md.id=$2)
 ORDER BY md.id,b.id`, orgID, deploymentID)
	if err != nil {
		return fmt.Errorf("list canonical deployment credentials: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ownerID int64
		var binding canonicalCredentialBindingRef
		var lastUsed, cooldown sql.NullTime
		if err := rows.Scan(&ownerID, &binding.ID, &binding.Status, &binding.StateRevision, &binding.FailCount, &lastUsed, &cooldown, &binding.Eligible); err != nil {
			return fmt.Errorf("scan canonical deployment credential: %w", err)
		}
		if lastUsed.Valid {
			value := lastUsed.Time
			binding.LastUsedAt = &value
		}
		if cooldown.Valid {
			value := cooldown.Time
			binding.CooldownUntil = &value
		}
		position, exists := index[ownerID]
		if !exists {
			return fmt.Errorf("canonical deployment credential owner missing")
		}
		axis := &applications[position].Credential
		axis.Bindings = append(axis.Bindings, binding)
		axis.BoundCount++
		if binding.Eligible {
			axis.EligibleCount++
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read canonical deployment credentials: %w", err)
	}
	for index := range applications {
		axis := &applications[index].Credential
		switch {
		case axis.EligibleCount > 0:
			axis.State, axis.ReasonCode = "ready", "eligible_credential_bound"
		case axis.BoundCount > 0:
			axis.State, axis.ReasonCode = "blocked", "no_eligible_bound_credential"
		}
	}
	return nil
}

func canonicalCapabilityProbeContractsForExactOperation(
	operationID, executionProfileID, executionProfileVersion,
	protocolProfileID, protocolProfileVersion string,
) ([]canonicalCapabilityProbeContractRef, error) {
	bindings, err := modelcapabilityqualification.ResolveAll(
		operationID, executionProfileID, executionProfileVersion,
		protocolProfileID, protocolProfileVersion,
	)
	if errors.Is(err, modelcapabilityqualification.ErrUnsupported) {
		return make([]canonicalCapabilityProbeContractRef, 0), nil
	}
	if err != nil {
		return nil, err
	}
	refs := make([]canonicalCapabilityProbeContractRef, 0, len(bindings))
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if _, duplicate := seen[binding.CapabilityID]; duplicate {
			return nil, fmt.Errorf("duplicate capability probe contract: %s", binding.CapabilityID)
		}
		seen[binding.CapabilityID] = struct{}{}
		refs = append(refs, canonicalCapabilityProbeContractRef{
			CapabilityID: binding.CapabilityID, ProbeContractID: binding.ProbeContractID,
			ProbeContractVersion: binding.ProbeContractVersion,
		})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].CapabilityID < refs[j].CapabilityID })
	return refs, nil
}

func (h *Handler) loadCanonicalOperations(ctx context.Context, store canonicalApplicationReadStore, orgID, deploymentID int64, operationFilter string, index map[int64]int, applications []canonicalModelApplication) error {
	rows, err := store.Query(ctx, `
SELECT md.id,operation.operation_id,operation.protocol_profile_id,
       operation.protocol_profile_version,operation.support_state,
       operation.evidence_state,operation.evidence_ref,operation.revision,operation.verified_at,
       execution_support.public_path,execution_support.executor_contract_version,
       qualification.qualified,qualification.qualification_state,
       qualification.approval_id,qualification.probe_id,qualification.https_origin,
       qualification.approved_connection_revision,qualification.approved_deployment_revision,
       qualification.approved_operation_revision,qualification.current_connection_revision,
       qualification.current_deployment_revision,qualification.current_operation_revision,
       COALESCE(probe_stats.probed_binding_count,0),
       COALESCE(probe_stats.current_success_count,0),COALESCE(probe_stats.current_failure_count,0),
       COALESCE(probe_stats.stale_probed_binding_count,0),
       probe.probe_id,probe.success,probe.status_code,probe.latency_ms,probe.reported_model,
       probe.error_summary,probe.credential_binding_id,probe.connection_revision,
       probe.deployment_revision,probe.credential_state_revision,probe.probe_profile_id,
       probe.probe_profile_version,probe.operation_revision,probe.public_path,
       probe.executor_contract_version,probe.https_origin,probe.source_kind,
       probe.tested_at,probe.tested_by,
       CASE WHEN probe.probe_id IS NULL THEN FALSE ELSE
         probe.connection_revision=pc.config_revision
         AND probe.deployment_revision=md.config_revision
         AND probe.upstream_model=md.upstream_model
         AND probe.probe_profile_id=operation.protocol_profile_id
         AND probe.probe_profile_version=operation.protocol_profile_version
         AND (
           operation.operation_id NOT IN ('openai.responses.v1','anthropic.messages.v1')
           OR (
             probe.operation_revision IS NOT NULL
             AND probe.public_path=execution_support.public_path
             AND probe.executor_contract_version=execution_support.executor_contract_version
             AND probe.https_origin=model_operation_https_origin(pc.base_url)
             AND probe.source_kind='admin.endpointprotocol_live_probe.v1'
           )
         )
         AND probe.credential_binding_id IS NOT NULL
         AND EXISTS (
             SELECT 1
               FROM credential_binding_deployments link
               JOIN provider_credential_bindings binding
                 ON binding.id=link.binding_id AND binding.connection_id=link.connection_id
              WHERE link.deployment_id=md.id
                AND link.connection_id=md.connection_id
                AND link.binding_id=probe.credential_binding_id
                AND binding.state_revision=probe.credential_state_revision
                AND probe.tested_at>=link.created_at
           ) END AS probe_current
       ,slot.slot_version,
       CASE WHEN stable_item.ordinal IS NULL THEN NULL ELSE jsonb_build_object(
         'release_ref',stable.release_ref,'release_stage',stable.stage,
         'release_version',stable.version,'route_items_hash',stable.route_items_hash,
         'ordinal',stable_item.ordinal,'admission',stable_item.admission,
         'priority',stable_item.priority,'weight',stable_item.weight,
         'expected_connection_revision',stable_item.expected_connection_revision,
         'expected_deployment_revision',stable_item.expected_deployment_revision,
         'expected_operation_revision',stable_item.expected_operation_revision,
         'protocol_profile_id',stable_item.protocol_profile_id,
         'protocol_profile_version',stable_item.protocol_profile_version,
         'public_path',stable_item.public_path,
         'executor_contract_version',stable_item.executor_contract_version
       )::text END AS stable_membership,
       CASE WHEN canary_item.ordinal IS NULL THEN NULL ELSE jsonb_build_object(
         'release_ref',canary.release_ref,'release_stage',canary.stage,
         'release_version',canary.version,'route_items_hash',canary.route_items_hash,
         'ordinal',canary_item.ordinal,'admission',canary_item.admission,
         'priority',canary_item.priority,'weight',canary_item.weight,
         'expected_connection_revision',canary_item.expected_connection_revision,
         'expected_deployment_revision',canary_item.expected_deployment_revision,
         'expected_operation_revision',canary_item.expected_operation_revision,
         'protocol_profile_id',canary_item.protocol_profile_id,
         'protocol_profile_version',canary_item.protocol_profile_version,
         'public_path',canary_item.public_path,
         'executor_contract_version',canary_item.executor_contract_version
       )::text END AS canary_membership
  FROM model_deployments md
  JOIN provider_connections pc ON pc.id=md.connection_id
  JOIN model_registry public_model
    ON public_model.id=md.public_model_id AND public_model.org_id=md.customer_org_id
  JOIN model_deployment_operations operation ON operation.deployment_id=md.id
  LEFT JOIN model_operation_exact_execution_support_bindings execution_support
    ON execution_support.operation_id=operation.operation_id
   AND execution_support.protocol_profile_id=operation.protocol_profile_id
   AND execution_support.protocol_profile_version=operation.protocol_profile_version
  LEFT JOIN model_operation_execution_qualifications qualification
    ON qualification.deployment_id=operation.deployment_id
   AND qualification.operation_id=operation.operation_id
  LEFT JOIN runtime_change_slots slot ON slot.public_model_id=public_model.id
  LEFT JOIN runtime_change_releases stable ON stable.id=slot.stable_release_id
  LEFT JOIN runtime_route_revision_items stable_item
    ON stable_item.release_id=stable.id
   AND stable_item.deployment_id=md.id
   AND stable_item.operation_id=operation.operation_id
  LEFT JOIN runtime_change_releases canary ON canary.id=slot.canary_release_id
  LEFT JOIN runtime_route_revision_items canary_item
    ON canary_item.release_id=canary.id
   AND canary_item.deployment_id=md.id
   AND canary_item.operation_id=operation.operation_id
  LEFT JOIN LATERAL (
    SELECT p.* FROM model_deployment_probes p
     WHERE p.deployment_id=md.id AND p.operation_id=operation.operation_id
     ORDER BY p.tested_at DESC,p.probe_id DESC LIMIT 1
  ) probe ON TRUE
  LEFT JOIN LATERAL (
    SELECT
      COUNT(*) FILTER (WHERE sample.probe_id IS NOT NULL) AS probed_binding_count,
      COUNT(*) FILTER (WHERE sample.probe_current AND sample.success) AS current_success_count,
      COUNT(*) FILTER (WHERE sample.probe_current AND NOT sample.success) AS current_failure_count,
      COUNT(*) FILTER (WHERE sample.probe_id IS NOT NULL AND NOT sample.probe_current) AS stale_probed_binding_count
      FROM (
        SELECT binding.id,p.probe_id,COALESCE(p.success,FALSE) AS success,
               p.probe_id IS NOT NULL
                 AND p.connection_revision=pc.config_revision
                 AND p.deployment_revision=md.config_revision
                 AND p.credential_state_revision=binding.state_revision
                 AND p.upstream_model=md.upstream_model
                 AND p.probe_profile_id=operation.protocol_profile_id
                 AND p.probe_profile_version=operation.protocol_profile_version
                 AND (
                   operation.operation_id NOT IN ('openai.responses.v1','anthropic.messages.v1')
                   OR (
                     p.operation_revision IS NOT NULL
                     AND p.public_path=execution_support.public_path
                     AND p.executor_contract_version=execution_support.executor_contract_version
                     AND p.https_origin=model_operation_https_origin(pc.base_url)
                     AND p.source_kind='admin.endpointprotocol_live_probe.v1'
                   )
                 )
                 AND p.tested_at>=link.created_at AS probe_current
          FROM credential_binding_deployments link
          JOIN provider_credential_bindings binding
            ON binding.id=link.binding_id AND binding.connection_id=link.connection_id
          LEFT JOIN LATERAL (
            SELECT binding_probe.* FROM model_deployment_probes binding_probe
             WHERE binding_probe.deployment_id=md.id
               AND binding_probe.operation_id=operation.operation_id
               AND binding_probe.credential_binding_id=binding.id
             ORDER BY binding_probe.tested_at DESC,binding_probe.probe_id DESC LIMIT 1
          ) p ON TRUE
         WHERE link.deployment_id=md.id AND link.connection_id=md.connection_id
      ) sample
  ) probe_stats ON TRUE
 WHERE md.customer_org_id=$1 AND ($2::bigint=0 OR md.id=$2)
   AND ($3::text='' OR operation.operation_id=$3)
 ORDER BY md.id,operation.operation_id`, orgID, deploymentID, operationFilter)
	if err != nil {
		return fmt.Errorf("list canonical deployment operations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ownerID int64
		operation := canonicalOperationAxis{
			ProbeState: "missing", ProbeReasonCode: "no_probe_recorded",
			ProductionQualification:  canonicalProductionQualification{State: "not_required"},
			CapabilityProbeContracts: make([]canonicalCapabilityProbeContractRef, 0),
		}
		var verifiedAt sql.NullTime
		var probeID, probeStatus, probeLatency, connectionRevision, deploymentRevision, credentialRevision sql.NullInt64
		var probeOperationRevision sql.NullInt64
		var probeSuccess sql.NullBool
		var reported, probeError, profileID, profileVersion sql.NullString
		var probePublicPath, probeExecutor, probeHTTPSOrigin, probeSourceKind sql.NullString
		var supportPublicPath, supportExecutor sql.NullString
		var qualificationQualified sql.NullBool
		var qualificationState, qualificationHTTPSOrigin sql.NullString
		var approvalID, approvalProbeID sql.NullInt64
		var approvedConnectionRevision, approvedDeploymentRevision, approvedOperationRevision sql.NullInt64
		var currentQualificationConnectionRevision, currentQualificationDeploymentRevision, currentQualificationOperationRevision sql.NullInt64
		var bindingID, testedBy sql.NullInt64
		var testedAt sql.NullTime
		var current bool
		var slotVersion sql.NullInt64
		var stableMembershipJSON, canaryMembershipJSON sql.NullString
		if err := rows.Scan(
			&ownerID, &operation.OperationID, &operation.ProtocolProfileID,
			&operation.ProtocolProfileVersion, &operation.SupportState, &operation.EvidenceState,
			&operation.EvidenceRef, &operation.Revision, &verifiedAt,
			&supportPublicPath, &supportExecutor,
			&qualificationQualified, &qualificationState,
			&approvalID, &approvalProbeID, &qualificationHTTPSOrigin,
			&approvedConnectionRevision, &approvedDeploymentRevision, &approvedOperationRevision,
			&currentQualificationConnectionRevision, &currentQualificationDeploymentRevision,
			&currentQualificationOperationRevision,
			&operation.ProbedBindingCount, &operation.CurrentSuccessCount,
			&operation.CurrentFailureCount, &operation.StaleProbedBindingCount,
			&probeID, &probeSuccess, &probeStatus, &probeLatency, &reported, &probeError,
			&bindingID, &connectionRevision, &deploymentRevision, &credentialRevision,
			&profileID, &profileVersion, &probeOperationRevision, &probePublicPath,
			&probeExecutor, &probeHTTPSOrigin, &probeSourceKind, &testedAt, &testedBy, &current,
			&slotVersion, &stableMembershipJSON, &canaryMembershipJSON,
		); err != nil {
			return fmt.Errorf("scan canonical deployment operation: %w", err)
		}
		if verifiedAt.Valid {
			value := verifiedAt.Time
			operation.VerifiedAt = &value
		}
		if supportPublicPath.Valid && supportExecutor.Valid {
			operation.ExecutionSupport = &canonicalExecutionSupport{
				OperationID: operation.OperationID, PublicPath: supportPublicPath.String,
				ProtocolProfileID:       operation.ProtocolProfileID,
				ProtocolProfileVersion:  operation.ProtocolProfileVersion,
				ExecutorContractVersion: supportExecutor.String,
			}
			if err := modelqualification.ValidateCompiledBinding(modelqualification.Binding{
				OperationID:             operation.ExecutionSupport.OperationID,
				PublicPath:              operation.ExecutionSupport.PublicPath,
				ProtocolProfileID:       operation.ExecutionSupport.ProtocolProfileID,
				ProtocolProfileVersion:  operation.ExecutionSupport.ProtocolProfileVersion,
				ExecutorContractVersion: operation.ExecutionSupport.ExecutorContractVersion,
			}); err != nil {
				return fmt.Errorf("canonical execution support binding mismatch: %w", err)
			}
			operation.ProductionQualification.Required = true
			operation.ProductionQualification.State = "qualification_unavailable"
			if qualificationState.Valid {
				operation.ProductionQualification.State = qualificationState.String
			}
			operation.ProductionQualification.Qualified = qualificationQualified.Bool
			operation.ProductionQualification.HTTPSOrigin = qualificationHTTPSOrigin.String
			operation.ProductionQualification.ApprovedConnectionRevision = approvedConnectionRevision.Int64
			operation.ProductionQualification.ApprovedDeploymentRevision = approvedDeploymentRevision.Int64
			operation.ProductionQualification.ApprovedOperationRevision = approvedOperationRevision.Int64
			operation.ProductionQualification.CurrentConnectionRevision = currentQualificationConnectionRevision.Int64
			operation.ProductionQualification.CurrentDeploymentRevision = currentQualificationDeploymentRevision.Int64
			operation.ProductionQualification.CurrentOperationRevision = currentQualificationOperationRevision.Int64
			if approvalID.Valid {
				value := approvalID.Int64
				operation.ProductionQualification.ApprovalID = &value
			}
			if approvalProbeID.Valid {
				value := approvalProbeID.Int64
				operation.ProductionQualification.ProbeID = &value
			}
		}
		position, exists := index[ownerID]
		if !exists {
			return fmt.Errorf("canonical operation owner missing")
		}
		publicModel := applications[position].PublicModel
		capabilityProbeContracts, contractErr := canonicalCapabilityProbeContractsForExactOperation(
			operation.OperationID,
			publicModel.ExecutionProfileID, publicModel.ExecutionProfileVersion,
			operation.ProtocolProfileID, operation.ProtocolProfileVersion,
		)
		if contractErr != nil {
			return fmt.Errorf("resolve canonical capability probe contracts: %w", contractErr)
		}
		operation.CapabilityProbeContracts = capabilityProbeContracts
		if _, specErr := canonicalOperationProbeSpecForExactProfile(
			modeloperation.ID(operation.OperationID), publicModel.ExecutionProfileID,
			publicModel.ExecutionProfileVersion, operation.ProtocolProfileID,
			operation.ProtocolProfileVersion,
		); specErr == nil {
			operation.ProbeAvailable = true
			operation.ProbeAvailabilityReason = "exact_probe_contract_available"
		} else if _, operationErr := canonicalOperationProbeSpecFor(modeloperation.ID(operation.OperationID)); operationErr == nil {
			operation.ProbeAvailabilityReason = "operation_profile_not_probeable"
		} else {
			operation.ProbeAvailabilityReason = "operation_probe_contract_unavailable"
		}
		if probeID.Valid {
			probe := canonicalOperationProbeView{
				ProbeID: probeID.Int64, Success: probeSuccess.Bool, Current: current,
				StatusCode: int(probeStatus.Int64), LatencyMS: probeLatency.Int64,
				ReportedModel: reported.String, ErrorSummary: probeError.String,
				ConnectionRevision: connectionRevision.Int64, DeploymentRevision: deploymentRevision.Int64,
				CredentialStateRevision: credentialRevision.Int64,
				ProbeProfileID:          profileID.String, ProbeProfileVersion: profileVersion.String,
				PublicPath: probePublicPath.String, ExecutorContractVersion: probeExecutor.String,
				HTTPSOrigin: probeHTTPSOrigin.String, SourceKind: probeSourceKind.String,
				TestedAt: testedAt.Time,
			}
			if probeOperationRevision.Valid {
				value := probeOperationRevision.Int64
				probe.OperationRevision = &value
			}
			if bindingID.Valid {
				value := bindingID.Int64
				probe.CredentialBindingID = &value
			}
			if testedBy.Valid {
				value := testedBy.Int64
				probe.TestedBy = &value
			}
			operation.LatestProbe = &probe
		}
		switch {
		case operation.CurrentSuccessCount > 0:
			operation.ProbeState, operation.ProbeReasonCode = "ready", "eligible_binding_has_current_success"
		case operation.CurrentFailureCount > 0:
			operation.ProbeState, operation.ProbeReasonCode = "failed", "eligible_bindings_latest_current_failed"
		case operation.StaleProbedBindingCount > 0:
			operation.ProbeState, operation.ProbeReasonCode = "stale", "binding_probe_revision_stale"
		case operation.ProbedBindingCount > 0:
			operation.ProbeState, operation.ProbeReasonCode = "unknown", "probed_binding_has_no_boolean_outcome"
		}
		operation.CircuitState = applications[position].Deployment.CircuitState
		operation.CircuitRevision = applications[position].Deployment.CircuitRevision
		operation.CircuitFailCount = applications[position].Deployment.FailCount
		operation.Runtime = canonicalOperationRuntimeAxis{State: "unassigned", ReasonCode: "no_public_model_runtime_slot"}
		if slotVersion.Valid {
			operation.Runtime.SlotVersion = slotVersion.Int64
			operation.Runtime.State, operation.Runtime.ReasonCode = "unassigned", "public_model_slot_excludes_deployment_operation"
		}
		if stableMembershipJSON.Valid {
			var membership canonicalRuntimeMembership
			if err := json.Unmarshal([]byte(stableMembershipJSON.String), &membership); err != nil {
				return fmt.Errorf("decode stable deployment operation membership: %w", err)
			}
			operation.Runtime.StableMembership = &membership
			operation.Runtime.State, operation.Runtime.ReasonCode = "stable", "stable_release_includes_deployment_operation"
		}
		if canaryMembershipJSON.Valid {
			var membership canonicalRuntimeMembership
			if err := json.Unmarshal([]byte(canaryMembershipJSON.String), &membership); err != nil {
				return fmt.Errorf("decode canary deployment operation membership: %w", err)
			}
			operation.Runtime.CanaryMembership = &membership
			operation.Runtime.State, operation.Runtime.ReasonCode = "canary", "canary_release_includes_deployment_operation"
		}
		applications[position].Operations = append(applications[position].Operations, operation)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read canonical deployment operations: %w", err)
	}
	return nil
}
