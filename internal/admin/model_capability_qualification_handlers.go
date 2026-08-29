package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modeldeployment"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const (
	modelCapabilityProbeMaxRequestBytes    = 4 << 10
	modelCapabilityApprovalMaxRequestBytes = 4 << 10
	modelCapabilityProbeTimeout            = 30 * time.Second
	modelCapabilityApprovalReasonMaxBytes  = 512
)

var (
	errModelCapabilityInvalid     = errors.New("model capability qualification request is invalid")
	errModelCapabilityNotFound    = errors.New("model capability qualification target not found")
	errModelCapabilityConflict    = errors.New("model capability qualification target is stale or mismatched")
	errModelCapabilityUnsupported = errors.New("model capability probe contract is unsupported")
)

type modelCapabilityProbeRequest struct {
	CredentialBindingID int64 `json:"credential_binding_id"`
}

type modelCapabilityApprovalRequest struct {
	CapabilityProbeID int64  `json:"capability_probe_id"`
	Reason            string `json:"reason"`
}

type modelCapabilityProbeResult struct {
	CapabilityProbeID  int64  `json:"capability_probe_id"`
	DeploymentID       int64  `json:"deployment_id"`
	OperationID        string `json:"operation_id"`
	CapabilityID       string `json:"capability_id"`
	Success            bool   `json:"success"`
	ProviderSuccess    bool   `json:"provider_success"`
	Current            bool   `json:"current"`
	Stale              bool   `json:"stale"`
	ReasonCode         string `json:"reason_code"`
	ProviderReasonCode string `json:"provider_reason_code"`
	ContractErrorCode  string `json:"contract_error_code,omitempty"`
	StatusCode         int    `json:"status_code"`
	LatencyMS          int64  `json:"latency_ms"`
	ReportedModel      string `json:"reported_model,omitempty"`
	FinishReason       string `json:"finish_reason,omitempty"`
	PromptTokens       int64  `json:"prompt_tokens,omitempty"`
	CompletionTokens   int64  `json:"completion_tokens,omitempty"`
	TotalTokens        int64  `json:"total_tokens,omitempty"`
	ReasoningObserved  bool   `json:"reasoning_observed,omitempty"`
	ToolCallObserved   bool   `json:"tool_call_observed,omitempty"`
	StructuredObserved bool   `json:"structured_output_observed,omitempty"`
	EvidenceRef        string `json:"evidence_ref,omitempty"`
}

type modelCapabilityApprovalResult struct {
	ApprovalID         int64     `json:"approval_id"`
	CapabilityProbeID  int64     `json:"capability_probe_id"`
	DeploymentID       int64     `json:"deployment_id"`
	OperationID        string    `json:"operation_id"`
	CapabilityID       string    `json:"capability_id"`
	Qualified          bool      `json:"qualified"`
	QualificationState string    `json:"qualification_state"`
	EvidenceRef        string    `json:"evidence_ref"`
	ApprovedAt         time.Time `json:"approved_at"`
	IdempotencyReplay  bool      `json:"idempotency_replay"`
}

type modelCapabilityQualificationView struct {
	ContractVersion            string                    `json:"contract_version"`
	DeploymentID               int64                     `json:"deployment_id"`
	OperationID                string                    `json:"operation_id"`
	CapabilityID               string                    `json:"capability_id"`
	SupportState               string                    `json:"support_state"`
	SupportRevision            int64                     `json:"support_revision"`
	ExecutionProfileID         string                    `json:"execution_profile_id"`
	ExecutionProfileVersion    string                    `json:"execution_profile_version"`
	ProtocolProfileID          string                    `json:"protocol_profile_id"`
	ProtocolProfileVersion     string                    `json:"protocol_profile_version"`
	ProbeContractID            string                    `json:"probe_contract_id,omitempty"`
	ProbeContractVersion       string                    `json:"probe_contract_version,omitempty"`
	ProbeAvailable             bool                      `json:"probe_available"`
	ProbeAvailabilityReason    string                    `json:"probe_availability_reason_code"`
	State                      string                    `json:"state"`
	Qualified                  bool                      `json:"qualified"`
	QualificationState         string                    `json:"qualification_state"`
	PreferredApprovalID        int64                     `json:"preferred_approval_id,omitempty"`
	PreferredCapabilityProbeID int64                     `json:"preferred_capability_probe_id,omitempty"`
	LatestProbe                *modelCapabilityProbeView `json:"latest_probe,omitempty"`
}

type modelCapabilityProbeView struct {
	CapabilityProbeID   int64     `json:"capability_probe_id"`
	CredentialBindingID int64     `json:"credential_binding_id,omitempty"`
	Success             bool      `json:"success"`
	ProviderSuccess     bool      `json:"provider_success"`
	Current             bool      `json:"current"`
	StatusCode          int       `json:"status_code"`
	LatencyMS           int64     `json:"latency_ms"`
	ReportedModel       string    `json:"reported_model,omitempty"`
	ErrorSummary        string    `json:"error_summary,omitempty"`
	ContractErrorCode   string    `json:"contract_error_code,omitempty"`
	FinishReason        string    `json:"finish_reason,omitempty"`
	PromptTokens        int64     `json:"prompt_tokens,omitempty"`
	CompletionTokens    int64     `json:"completion_tokens,omitempty"`
	TotalTokens         int64     `json:"total_tokens,omitempty"`
	ReasoningObserved   bool      `json:"reasoning_observed,omitempty"`
	ToolCallObserved    bool      `json:"tool_call_observed,omitempty"`
	StructuredObserved  bool      `json:"structured_output_observed,omitempty"`
	TestedAt            time.Time `json:"tested_at"`
}

type modelCapabilitySnapshot struct {
	CustomerOrgID             int64
	DeploymentID              int64
	PublicModelID             int64
	ConnectionID              int64
	CredentialBindingID       int64
	Provider                  string
	BaseURL                   string
	AdapterType               string
	AdapterConfig             json.RawMessage
	ConnectionState           string
	ConnectionRevision        int64
	UpstreamModel             string
	DeploymentState           string
	DeploymentRevision        int64
	OperationID               string
	OperationSupportState     string
	OperationRevision         int64
	ProtocolProfileID         string
	ProtocolProfileVersion    string
	CapabilityID              string
	CapabilitySupportState    string
	CapabilitySupportRevision int64
	ExecutionProfileID        string
	ExecutionProfileVersion   string
	ProbeContractID           string
	ProbeContractVersion      string
	ExpectedProvider          string
	ExpectedUpstreamModel     string
	PublicPath                string
	EncryptedSecret           string
	CredentialStatus          string
	CredentialRevision        int64
	CredentialCooldown        *time.Time
	CredentialEligible        bool
	CredentialLinkCreatedAt   time.Time
	Actor                     canonicalOperationProbeActorSnapshot
}

type modelCapabilityProbeOutcome struct {
	ProviderSuccess    bool
	ContractSuccess    bool
	StatusCode         int
	LatencyMS          int64
	ReasonCode         string
	ContractErrorCode  string
	ReportedModel      string
	FinishReason       string
	PromptTokens       int64
	CompletionTokens   int64
	TotalTokens        int64
	ResponseBytes      int
	DataEventCount     int
	DeltaCount         int
	ReasoningObserved  bool
	ToolCallObserved   bool
	StructuredObserved bool
}

func (h *Handler) GetCanonicalModelCapabilityQualification(w http.ResponseWriter, r *http.Request) {
	deploymentID, operationID, capabilityID, err := canonicalCapabilityPath(r)
	if err != nil {
		writeModelCapabilityError(w, err)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	view, err := h.loadModelCapabilityQualificationView(r.Context(), orgID, deploymentID, operationID, capabilityID)
	if err != nil {
		writeModelCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) ProbeCanonicalModelCapability(w http.ResponseWriter, r *http.Request) {
	deploymentID, operationID, capabilityID, err := canonicalCapabilityPath(r)
	if err != nil {
		writeModelCapabilityError(w, err)
		return
	}
	request, err := decodeModelCapabilityProbeRequest(w, r)
	if err != nil {
		writeModelCapabilityError(w, err)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorID := middleware.GetUserIDFromContext(r.Context())
	if orgID <= 0 || actorID <= 0 {
		writeModelCapabilityError(w, errModelCapabilityInvalid)
		return
	}
	result, err := h.probeCanonicalModelCapability(r.Context(), orgID, actorID, deploymentID, operationID, capabilityID, request, requestAuditMeta(r, orgID, 0))
	if err != nil {
		writeModelCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ApproveCanonicalModelCapabilityRouting(w http.ResponseWriter, r *http.Request) {
	deploymentID, operationID, capabilityID, err := canonicalCapabilityPath(r)
	if err != nil {
		writeModelCapabilityError(w, err)
		return
	}
	request, err := decodeModelCapabilityApprovalRequest(w, r)
	if err != nil {
		writeModelCapabilityError(w, err)
		return
	}
	targetOrgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorID := middleware.GetUserIDFromContext(r.Context())
	actorOrgID := middleware.GetOrgIDFromContext(r.Context())
	if targetOrgID <= 0 || actorID <= 0 || actorOrgID <= 0 {
		writeModelCapabilityError(w, errModelCapabilityInvalid)
		return
	}
	result, err := h.approveCanonicalModelCapabilityRouting(
		r.Context(), targetOrgID, actorID, actorOrgID,
		deploymentID, operationID, capabilityID, request,
		requestAuditMeta(r, targetOrgID, 0),
	)
	if err != nil {
		writeModelCapabilityError(w, err)
		return
	}
	status := http.StatusCreated
	if result.IdempotencyReplay {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func canonicalCapabilityPath(r *http.Request) (int64, string, string, error) {
	deploymentID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("deploymentID")), 10, 64)
	operationID := strings.TrimSpace(r.PathValue("operationID"))
	capabilityID := strings.TrimSpace(r.PathValue("capabilityID"))
	if err != nil || deploymentID <= 0 || operationID == "" || capabilityID == "" ||
		strings.ContainsAny(operationID+capabilityID, "\x00\r\n") || len(operationID) > 80 || len(capabilityID) > 80 {
		return 0, "", "", errModelCapabilityInvalid
	}
	return deploymentID, operationID, capabilityID, nil
}

func decodeModelCapabilityProbeRequest(w http.ResponseWriter, r *http.Request) (modelCapabilityProbeRequest, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, modelCapabilityProbeMaxRequestBytes))
	if err != nil {
		return modelCapabilityProbeRequest{}, errModelCapabilityInvalid
	}
	var request modelCapabilityProbeRequest
	if err := decodeStrictModelCapabilityObject(body, &request); err != nil || request.CredentialBindingID <= 0 {
		return modelCapabilityProbeRequest{}, errModelCapabilityInvalid
	}
	return request, nil
}

func decodeModelCapabilityApprovalRequest(w http.ResponseWriter, r *http.Request) (modelCapabilityApprovalRequest, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, modelCapabilityApprovalMaxRequestBytes))
	if err != nil {
		return modelCapabilityApprovalRequest{}, errModelCapabilityInvalid
	}
	var request modelCapabilityApprovalRequest
	if err := decodeStrictModelCapabilityObject(body, &request); err != nil {
		return modelCapabilityApprovalRequest{}, errModelCapabilityInvalid
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.CapabilityProbeID <= 0 || request.Reason == "" || len(request.Reason) > modelCapabilityApprovalReasonMaxBytes || strings.ContainsAny(request.Reason, "\x00\r\n") {
		return modelCapabilityApprovalRequest{}, errModelCapabilityInvalid
	}
	return request, nil
}

func decodeStrictModelCapabilityObject(body []byte, target interface{}) error {
	if err := strictjson.Validate(body); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errModelCapabilityInvalid
	}
	return nil
}

func writeModelCapabilityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errModelCapabilityInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid model capability qualification request", "code": "model_capability_qualification_invalid"})
	case errors.Is(err, errModelCapabilityNotFound), errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model capability qualification target not found", "code": "model_capability_qualification_not_found"})
	case errors.Is(err, errModelCapabilityUnsupported):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"error": "Typed capability probe contract is unavailable", "code": "model_capability_probe_unsupported"})
	case errors.Is(err, errModelCapabilityConflict):
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Model capability qualification target is stale or mismatched", "code": "model_capability_qualification_conflict"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Model capability qualification unavailable", "code": "model_capability_qualification_unavailable"})
	}
}

func isModelCapabilityConflictError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "23514" || pgErr.Code == "40001" || pgErr.Code == "42501" || pgErr.Code == "23505")
}

func (h *Handler) loadModelCapabilitySnapshot(ctx context.Context, orgID, actorID, deploymentID int64, operationID, capabilityID string, bindingID int64) (modelCapabilitySnapshot, error) {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return modelCapabilitySnapshot{}, fmt.Errorf("begin model capability snapshot: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
		return modelCapabilitySnapshot{}, fmt.Errorf("configure model capability snapshot: %w", err)
	}
	if err := validateCanonicalOperationProbeCustomerOrg(ctx, tx, orgID); err != nil {
		return modelCapabilitySnapshot{}, err
	}
	actor, err := lockCanonicalOperationProbeActor(ctx, tx, actorID, orgID)
	if err != nil {
		return modelCapabilitySnapshot{}, err
	}
	snapshot, err := lockModelCapabilitySnapshotRows(ctx, tx, orgID, deploymentID, operationID, capabilityID, bindingID)
	if err != nil {
		return modelCapabilitySnapshot{}, err
	}
	snapshot.Actor = actor
	if err := validateModelCapabilitySnapshot(snapshot); err != nil {
		return modelCapabilitySnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modelCapabilitySnapshot{}, fmt.Errorf("commit model capability snapshot: %w", err)
	}
	return snapshot, nil
}

func lockModelCapabilitySnapshotRows(ctx context.Context, tx pgx.Tx, orgID, deploymentID int64, operationID, capabilityID string, bindingID int64) (modelCapabilitySnapshot, error) {
	var snapshot modelCapabilitySnapshot
	err := tx.QueryRow(ctx, `
SELECT deployment.customer_org_id,deployment.id,deployment.public_model_id,
       deployment.connection_id,binding.id,
       connection.provider,connection.base_url,connection.adapter_type,
       COALESCE(connection.adapter_config,'{}'::jsonb),connection.admin_state,
       connection.config_revision,deployment.upstream_model,deployment.admin_state,
       deployment.config_revision,operation.operation_id,operation.support_state,
       operation.revision,operation.protocol_profile_id,operation.protocol_profile_version,
       capability.capability_id,capability.support_state,capability.support_revision,
       model.execution_profile_id,model.execution_profile_version,
       current_contract.probe_contract_id,current_contract.probe_contract_version,
       contract.expected_provider,contract.expected_upstream_model,contract.public_path,
       binding.key_enc,binding.status,binding.state_revision,binding.cooldown_until,
       (binding.cooldown_until IS NULL OR binding.cooldown_until<=transaction_timestamp()),
       link.created_at
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_registry model ON model.id=deployment.public_model_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id AND operation.operation_id=$3
  JOIN model_deployment_capabilities capability
    ON capability.deployment_id=deployment.id
   AND capability.operation_id=operation.operation_id
   AND capability.capability_id=$4
  JOIN model_capability_current_probe_contracts current_contract
    ON current_contract.operation_id=operation.operation_id
   AND current_contract.capability_id=capability.capability_id
   AND current_contract.execution_profile_id=model.execution_profile_id
   AND current_contract.execution_profile_version=model.execution_profile_version
   AND current_contract.protocol_profile_id=operation.protocol_profile_id
   AND current_contract.protocol_profile_version=operation.protocol_profile_version
  JOIN model_capability_probe_contract_bindings contract
    ON contract.operation_id=current_contract.operation_id
   AND contract.capability_id=current_contract.capability_id
   AND contract.execution_profile_id=current_contract.execution_profile_id
   AND contract.execution_profile_version=current_contract.execution_profile_version
   AND contract.protocol_profile_id=current_contract.protocol_profile_id
   AND contract.protocol_profile_version=current_contract.protocol_profile_version
   AND contract.probe_contract_id=current_contract.probe_contract_id
   AND contract.probe_contract_version=current_contract.probe_contract_version
  JOIN provider_credential_bindings binding
    ON binding.id=$5 AND binding.connection_id=connection.id
  JOIN credential_binding_deployments link
    ON link.binding_id=binding.id AND link.deployment_id=deployment.id
   AND link.connection_id=connection.id
 WHERE deployment.id=$2 AND deployment.customer_org_id=$1
 FOR SHARE OF deployment,connection,model,operation,capability,binding,link`,
		orgID, deploymentID, operationID, capabilityID, bindingID).Scan(
		&snapshot.CustomerOrgID, &snapshot.DeploymentID, &snapshot.PublicModelID,
		&snapshot.ConnectionID, &snapshot.CredentialBindingID,
		&snapshot.Provider, &snapshot.BaseURL, &snapshot.AdapterType, &snapshot.AdapterConfig,
		&snapshot.ConnectionState, &snapshot.ConnectionRevision, &snapshot.UpstreamModel,
		&snapshot.DeploymentState, &snapshot.DeploymentRevision, &snapshot.OperationID,
		&snapshot.OperationSupportState, &snapshot.OperationRevision,
		&snapshot.ProtocolProfileID, &snapshot.ProtocolProfileVersion,
		&snapshot.CapabilityID, &snapshot.CapabilitySupportState, &snapshot.CapabilitySupportRevision,
		&snapshot.ExecutionProfileID, &snapshot.ExecutionProfileVersion,
		&snapshot.ProbeContractID, &snapshot.ProbeContractVersion,
		&snapshot.ExpectedProvider, &snapshot.ExpectedUpstreamModel, &snapshot.PublicPath,
		&snapshot.EncryptedSecret, &snapshot.CredentialStatus, &snapshot.CredentialRevision,
		&snapshot.CredentialCooldown, &snapshot.CredentialEligible, &snapshot.CredentialLinkCreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return modelCapabilitySnapshot{}, errModelCapabilityNotFound
	}
	if err != nil {
		return modelCapabilitySnapshot{}, fmt.Errorf("lock model capability target: %w", err)
	}
	return snapshot, nil
}

func validateModelCapabilitySnapshot(snapshot modelCapabilitySnapshot) error {
	binding, err := modelcapabilityqualification.Resolve(
		snapshot.OperationID, snapshot.CapabilityID,
		snapshot.ExecutionProfileID, snapshot.ExecutionProfileVersion,
		snapshot.ProtocolProfileID, snapshot.ProtocolProfileVersion,
	)
	if err != nil {
		return errModelCapabilityUnsupported
	}
	if binding.ProbeContractID != snapshot.ProbeContractID || binding.ProbeContractVersion != snapshot.ProbeContractVersion ||
		binding.ExpectedProvider != snapshot.ExpectedProvider || binding.ExpectedUpstreamModel != snapshot.ExpectedUpstreamModel ||
		binding.PublicPath != snapshot.PublicPath || snapshot.Provider != binding.ExpectedProvider || snapshot.UpstreamModel != binding.ExpectedUpstreamModel {
		return errModelCapabilityConflict
	}
	if snapshot.ConnectionState != "enabled" || snapshot.DeploymentState != "enabled" ||
		snapshot.OperationSupportState != string(modeldeployment.SupportEnabled) || snapshot.CapabilitySupportState != "enabled" ||
		snapshot.CredentialStatus != "active" || !snapshot.CredentialEligible || snapshot.EncryptedSecret == "" ||
		snapshot.ConnectionRevision <= 0 || snapshot.DeploymentRevision <= 0 || snapshot.OperationRevision <= 0 ||
		snapshot.CapabilitySupportRevision <= 0 || snapshot.CredentialRevision <= 0 || snapshot.CredentialLinkCreatedAt.IsZero() {
		return errModelCapabilityConflict
	}
	return nil
}

func (h *Handler) probeCanonicalModelCapability(ctx context.Context, orgID, actorID, deploymentID int64, operationID, capabilityID string, request modelCapabilityProbeRequest, audit auditMeta) (modelCapabilityProbeResult, error) {
	snapshot, err := h.loadModelCapabilitySnapshot(ctx, orgID, actorID, deploymentID, operationID, capabilityID, request.CredentialBindingID)
	if err != nil {
		return modelCapabilityProbeResult{}, err
	}
	sourceRef, err := middleware.NewOperationID()
	if err != nil {
		return modelCapabilityProbeResult{}, fmt.Errorf("generate capability probe identity: %w", err)
	}
	outcome := h.executeModelCapabilityProbe(ctx, snapshot, sourceRef)
	return h.persistModelCapabilityProbe(ctx, actorID, snapshot, outcome, sourceRef, audit)
}

func (h *Handler) executeModelCapabilityProbe(ctx context.Context, snapshot modelCapabilitySnapshot, requestID string) modelCapabilityProbeOutcome {
	secret, err := auth.DecryptKey(snapshot.EncryptedSecret, h.encKey)
	if err != nil || strings.TrimSpace(secret) == "" {
		return modelCapabilityProbeOutcome{ReasonCode: "credential_unavailable"}
	}
	body, err := buildModelCapabilityProbeRequestBody(snapshot)
	if err != nil {
		return modelCapabilityProbeOutcome{ReasonCode: "request_build_failed"}
	}
	adapter, err := adapters.DefaultRegistry().Get(snapshot.AdapterType)
	if err != nil {
		return modelCapabilityProbeOutcome{ReasonCode: "adapter_unavailable"}
	}
	probeCtx, cancel := context.WithTimeout(ctx, modelCapabilityProbeTimeout)
	defer cancel()
	request, err := adapter.BuildRequest(probeCtx, adapters.RequestInput{
		Endpoint:       adapters.EndpointChatCompletions,
		DownstreamBody: body,
		RequestedModel: snapshot.UpstreamModel,
		MappedModel:    snapshot.UpstreamModel,
		Channel:        models.Channel{Name: "typed-capability-probe", Provider: snapshot.Provider, BaseURL: snapshot.BaseURL, AdapterType: snapshot.AdapterType, AdapterConfig: snapshot.AdapterConfig},
		APIKey:         secret,
		RequestID:      requestID,
	})
	if err != nil {
		return modelCapabilityProbeOutcome{ReasonCode: "request_build_failed"}
	}
	started := time.Now()
	response, err := h.canonicalOperationProbeDoer().Do(request)
	if err != nil {
		return modelCapabilityProbeOutcome{LatencyMS: time.Since(started).Milliseconds(), ReasonCode: "provider_request_failed"}
	}
	if response == nil || response.Body == nil {
		return modelCapabilityProbeOutcome{LatencyMS: time.Since(started).Milliseconds(), ReasonCode: "provider_response_invalid"}
	}
	defer response.Body.Close()
	outcome := modelCapabilityProbeOutcome{StatusCode: response.StatusCode}
	if outcome.StatusCode < 100 || outcome.StatusCode > 599 {
		outcome.StatusCode = 0
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		outcome.LatencyMS = time.Since(started).Milliseconds()
		outcome.ReasonCode = "provider_http_error"
		return outcome
	}
	outcome.ProviderSuccess = true
	switch snapshot.ProbeContractID {
	case modelcapabilityqualification.DeepSeekChatSSEProbeContractID:
		observation, validationErr := modelcapabilityqualification.ValidateDeepSeekChatSSEForModel(
			snapshot.ProbeContractID, snapshot.ProbeContractVersion, snapshot.ExpectedUpstreamModel,
			response.Header.Get("Content-Type"), response.Body,
		)
		outcome.ReportedModel = observation.ReportedModel
		outcome.FinishReason = observation.FinishReason
		outcome.PromptTokens = observation.PromptTokens
		outcome.CompletionTokens = observation.CompletionTokens
		outcome.TotalTokens = observation.TotalTokens
		outcome.ResponseBytes = observation.ResponseBytes
		outcome.DataEventCount = observation.DataEventCount
		outcome.DeltaCount = observation.DeltaCount
		if validationErr != nil {
			outcome.LatencyMS = time.Since(started).Milliseconds()
			outcome.ReasonCode = "provider_stream_contract_invalid"
			outcome.ContractErrorCode = modelcapabilityqualification.DeepSeekSSEErrorCode(validationErr)
			return outcome
		}
	case modelcapabilityqualification.DeepSeekChatReasoningProbeContractID,
		modelcapabilityqualification.DeepSeekChatReasoningEffortProbeContractID:
		observation, validationErr := modelcapabilityqualification.ValidateDeepSeekChatReasoningForModel(
			snapshot.ProbeContractID, snapshot.ProbeContractVersion, snapshot.ExpectedUpstreamModel,
			response.Header.Get("Content-Type"), response.Body,
		)
		outcome.ReportedModel = observation.ReportedModel
		outcome.FinishReason = observation.FinishReason
		outcome.PromptTokens = observation.PromptTokens
		outcome.CompletionTokens = observation.CompletionTokens
		outcome.TotalTokens = observation.TotalTokens
		outcome.ResponseBytes = observation.ResponseBytes
		outcome.ReasoningObserved = observation.ReasoningSeen
		if validationErr != nil {
			outcome.LatencyMS = time.Since(started).Milliseconds()
			outcome.ReasonCode = "provider_reasoning_contract_invalid"
			outcome.ContractErrorCode = modelcapabilityqualification.DeepSeekReasoningErrorCode(validationErr)
			return outcome
		}
	case modelcapabilityqualification.DeepSeekChatFunctionToolsProbeContractID:
		observation, validationErr := modelcapabilityqualification.ValidateDeepSeekChatFunctionToolsForModel(
			snapshot.ProbeContractID, snapshot.ProbeContractVersion, snapshot.ExpectedUpstreamModel,
			response.Header.Get("Content-Type"), response.Body,
		)
		outcome.ReportedModel = observation.ReportedModel
		outcome.FinishReason = observation.FinishReason
		outcome.PromptTokens = observation.PromptTokens
		outcome.CompletionTokens = observation.CompletionTokens
		outcome.TotalTokens = observation.TotalTokens
		outcome.ResponseBytes = observation.ResponseBytes
		outcome.ToolCallObserved = observation.ToolCallObserved
		if validationErr != nil {
			outcome.LatencyMS = time.Since(started).Milliseconds()
			outcome.ReasonCode = "provider_function_tools_contract_invalid"
			outcome.ContractErrorCode = modelcapabilityqualification.DeepSeekCoreErrorCode(validationErr)
			return outcome
		}
	case modelcapabilityqualification.DeepSeekChatJSONObjectProbeContractID:
		observation, validationErr := modelcapabilityqualification.ValidateDeepSeekChatJSONObjectForModel(
			snapshot.ProbeContractID, snapshot.ProbeContractVersion, snapshot.ExpectedUpstreamModel,
			response.Header.Get("Content-Type"), response.Body,
		)
		outcome.ReportedModel = observation.ReportedModel
		outcome.FinishReason = observation.FinishReason
		outcome.PromptTokens = observation.PromptTokens
		outcome.CompletionTokens = observation.CompletionTokens
		outcome.TotalTokens = observation.TotalTokens
		outcome.ResponseBytes = observation.ResponseBytes
		outcome.StructuredObserved = observation.StructuredOutputObserved
		if validationErr != nil {
			outcome.LatencyMS = time.Since(started).Milliseconds()
			outcome.ReasonCode = "provider_json_object_contract_invalid"
			outcome.ContractErrorCode = modelcapabilityqualification.DeepSeekCoreErrorCode(validationErr)
			return outcome
		}
	case modelcapabilityqualification.KimiChatSSEProbeContractID:
		observation, validationErr := modelcapabilityqualification.ValidateKimiChatSSE(
			snapshot.ProbeContractID, snapshot.ProbeContractVersion, snapshot.ExpectedUpstreamModel,
			response.Header.Get("Content-Type"), response.Body,
		)
		outcome.ReportedModel = observation.ReportedModel
		outcome.FinishReason = observation.FinishReason
		outcome.PromptTokens = observation.PromptTokens
		outcome.CompletionTokens = observation.CompletionTokens
		outcome.TotalTokens = observation.TotalTokens
		outcome.ResponseBytes = observation.ResponseBytes
		outcome.DataEventCount = observation.DataEventCount
		outcome.DeltaCount = observation.DeltaCount
		if validationErr != nil {
			outcome.LatencyMS = time.Since(started).Milliseconds()
			outcome.ReasonCode = "provider_stream_contract_invalid"
			outcome.ContractErrorCode = modelcapabilityqualification.KimiChatErrorCode(validationErr)
			return outcome
		}
	case modelcapabilityqualification.KimiChatReasoningProbeContractID:
		observation, validationErr := modelcapabilityqualification.ValidateKimiChatReasoning(
			snapshot.ProbeContractID, snapshot.ProbeContractVersion, snapshot.ExpectedUpstreamModel,
			response.Header.Get("Content-Type"), response.Body,
		)
		outcome.ReportedModel = observation.ReportedModel
		outcome.FinishReason = observation.FinishReason
		outcome.PromptTokens = observation.PromptTokens
		outcome.CompletionTokens = observation.CompletionTokens
		outcome.TotalTokens = observation.TotalTokens
		outcome.ResponseBytes = observation.ResponseBytes
		outcome.ReasoningObserved = observation.ReasoningObserved
		if validationErr != nil {
			outcome.LatencyMS = time.Since(started).Milliseconds()
			outcome.ReasonCode = "provider_reasoning_contract_invalid"
			outcome.ContractErrorCode = modelcapabilityqualification.KimiChatErrorCode(validationErr)
			return outcome
		}
	case modelcapabilityqualification.KimiChatFunctionToolsProbeContractID:
		observation, validationErr := modelcapabilityqualification.ValidateKimiChatFunctionTools(
			snapshot.ProbeContractID, snapshot.ProbeContractVersion, snapshot.ExpectedUpstreamModel,
			response.Header.Get("Content-Type"), response.Body,
		)
		outcome.ReportedModel = observation.ReportedModel
		outcome.FinishReason = observation.FinishReason
		outcome.PromptTokens = observation.PromptTokens
		outcome.CompletionTokens = observation.CompletionTokens
		outcome.TotalTokens = observation.TotalTokens
		outcome.ResponseBytes = observation.ResponseBytes
		outcome.ToolCallObserved = observation.ToolCallObserved
		if validationErr != nil {
			outcome.LatencyMS = time.Since(started).Milliseconds()
			outcome.ReasonCode = "provider_function_tools_contract_invalid"
			outcome.ContractErrorCode = modelcapabilityqualification.KimiChatErrorCode(validationErr)
			return outcome
		}
	case modelcapabilityqualification.KimiChatJSONObjectProbeContractID:
		observation, validationErr := modelcapabilityqualification.ValidateKimiChatJSONObject(
			snapshot.ProbeContractID, snapshot.ProbeContractVersion, snapshot.ExpectedUpstreamModel,
			response.Header.Get("Content-Type"), response.Body,
		)
		outcome.ReportedModel = observation.ReportedModel
		outcome.FinishReason = observation.FinishReason
		outcome.PromptTokens = observation.PromptTokens
		outcome.CompletionTokens = observation.CompletionTokens
		outcome.TotalTokens = observation.TotalTokens
		outcome.ResponseBytes = observation.ResponseBytes
		outcome.StructuredObserved = observation.StructuredOutputObserved
		if validationErr != nil {
			outcome.LatencyMS = time.Since(started).Milliseconds()
			outcome.ReasonCode = "provider_json_object_contract_invalid"
			outcome.ContractErrorCode = modelcapabilityqualification.KimiChatErrorCode(validationErr)
			return outcome
		}
	default:
		outcome.LatencyMS = time.Since(started).Milliseconds()
		outcome.ReasonCode = "probe_contract_unsupported"
		return outcome
	}
	outcome.LatencyMS = time.Since(started).Milliseconds()
	outcome.ContractSuccess = true
	outcome.ReasonCode = "ok"
	return outcome
}

func buildModelCapabilityProbeRequestBody(snapshot modelCapabilitySnapshot) ([]byte, error) {
	payload := map[string]interface{}{
		"model":      snapshot.UpstreamModel,
		"max_tokens": 64,
		"stream":     false,
	}
	switch snapshot.ProbeContractID {
	case modelcapabilityqualification.DeepSeekChatSSEProbeContractID:
		payload["messages"] = []map[string]string{{"role": "user", "content": "Reply with exactly: UNIROUTER_STREAM_PROBE_OK"}}
		payload["stream"] = true
		payload["stream_options"] = map[string]bool{"include_usage": true}
		payload["thinking"] = map[string]string{"type": "disabled"}
	case modelcapabilityqualification.DeepSeekChatReasoningProbeContractID:
		payload["messages"] = []map[string]string{{"role": "user", "content": "Reply with exactly: UNIROUTER_REASONING_PROBE_OK"}}
		payload["max_tokens"] = 512
		payload["thinking"] = map[string]string{"type": "enabled"}
	case modelcapabilityqualification.DeepSeekChatReasoningEffortProbeContractID:
		payload["messages"] = []map[string]string{{"role": "user", "content": "Reply with exactly: UNIROUTER_REASONING_PROBE_OK"}}
		payload["max_tokens"] = 512
		payload["reasoning_effort"] = "low"
	case modelcapabilityqualification.DeepSeekChatFunctionToolsProbeContractID:
		payload["messages"] = []map[string]string{{"role": "user", "content": "Call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."}}
		payload["max_tokens"] = 256
		payload["thinking"] = map[string]string{"type": "disabled"}
		payload["tools"] = []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name": "unirouter_probe_echo", "description": "Return the supplied probe value through a function call.",
				"parameters": map[string]interface{}{
					"type": "object", "properties": map[string]interface{}{
						"value": map[string]string{"type": "string"},
					}, "required": []string{"value"}, "additionalProperties": false,
				},
			},
		}}
		switch snapshot.CapabilityID {
		case modelprofile.CapabilityToolChoiceAuto:
			payload["tool_choice"] = "auto"
		case modelprofile.CapabilityToolChoiceNamed:
			payload["tool_choice"] = map[string]interface{}{
				"type": "function", "function": map[string]string{"name": "unirouter_probe_echo"},
			}
		default:
			payload["tool_choice"] = "required"
		}
	case modelcapabilityqualification.DeepSeekChatJSONObjectProbeContractID:
		payload["messages"] = []map[string]string{
			{"role": "system", "content": "Return valid JSON only."},
			{"role": "user", "content": `Return exactly {"status":"UNIROUTER_JSON_PROBE_OK"}.`},
		}
		payload["max_tokens"] = 64
		payload["thinking"] = map[string]string{"type": "disabled"}
		payload["response_format"] = map[string]string{"type": "json_object"}
	case modelcapabilityqualification.KimiChatSSEProbeContractID:
		delete(payload, "max_tokens")
		payload["max_completion_tokens"] = 64
		payload["messages"] = []map[string]string{{"role": "user", "content": "Reply with exactly: UNIROUTER_STREAM_PROBE_OK"}}
		payload["stream"] = true
		payload["stream_options"] = map[string]bool{"include_usage": true}
		payload["thinking"] = map[string]string{"type": "disabled"}
	case modelcapabilityqualification.KimiChatReasoningProbeContractID:
		delete(payload, "max_tokens")
		payload["max_completion_tokens"] = 512
		payload["messages"] = []map[string]string{{"role": "user", "content": "Reply with exactly: UNIROUTER_REASONING_PROBE_OK"}}
		// Omission is deliberate: the probe proves K2.6's provider-default
		// thinking path, which is the path used by an ordinary invocation.
	case modelcapabilityqualification.KimiChatFunctionToolsProbeContractID:
		delete(payload, "max_tokens")
		payload["max_completion_tokens"] = 256
		payload["messages"] = []map[string]string{{"role": "user", "content": "Call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."}}
		payload["thinking"] = map[string]string{"type": "disabled"}
		payload["tools"] = []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name": "unirouter_probe_echo", "description": "Return the supplied probe value through a function call.",
				"parameters": map[string]interface{}{
					"type": "object", "properties": map[string]interface{}{
						"value": map[string]string{"type": "string"},
					}, "required": []string{"value"}, "additionalProperties": false,
				},
			},
		}}
		payload["tool_choice"] = "auto"
	case modelcapabilityqualification.KimiChatJSONObjectProbeContractID:
		delete(payload, "max_tokens")
		payload["max_completion_tokens"] = 64
		payload["messages"] = []map[string]string{
			{"role": "system", "content": "Return valid JSON only."},
			{"role": "user", "content": `Return exactly {"status":"UNIROUTER_JSON_PROBE_OK"}.`},
		}
		payload["thinking"] = map[string]string{"type": "disabled"}
		payload["response_format"] = map[string]string{"type": "json_object"}
	default:
		return nil, errModelCapabilityUnsupported
	}
	return json.Marshal(payload)
}

func (h *Handler) persistModelCapabilityProbe(ctx context.Context, actorID int64, snapshot modelCapabilitySnapshot, outcome modelCapabilityProbeOutcome, sourceRef string, audit auditMeta) (modelCapabilityProbeResult, error) {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return modelCapabilityProbeResult{}, fmt.Errorf("begin capability probe persist: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
		return modelCapabilityProbeResult{}, fmt.Errorf("configure capability probe persist: %w", err)
	}
	rootCurrent, err := lockCanonicalOperationProbeRootCurrent(ctx, tx, snapshot.CustomerOrgID)
	if err != nil {
		return modelCapabilityProbeResult{}, err
	}
	actor, present, active, err := lockCanonicalOperationProbeActorRow(ctx, tx, actorID)
	if err != nil {
		return modelCapabilityProbeResult{}, err
	}
	current, currentErr := lockModelCapabilitySnapshotRows(ctx, tx, snapshot.CustomerOrgID, snapshot.DeploymentID, snapshot.OperationID, snapshot.CapabilityID, snapshot.CredentialBindingID)
	currentRows := currentErr == nil
	if currentErr != nil && !errors.Is(currentErr, errModelCapabilityNotFound) {
		return modelCapabilityProbeResult{}, currentErr
	}
	actorCurrent := present && active && canonicalOperationProbeActorAllowed(actor, snapshot.CustomerOrgID) && canonicalOperationProbeActorMatches(snapshot.Actor, actor)
	inputsCurrent := currentRows && sameModelCapabilitySnapshot(snapshot, current) && validateModelCapabilitySnapshot(current) == nil
	currentProof := rootCurrent && actorCurrent && inputsCurrent
	success := outcome.ContractSuccess && currentProof
	reasonCode := outcome.ReasonCode
	if !currentProof {
		reasonCode = "configuration_changed_during_probe"
	}
	requestSummaryFields := map[string]interface{}{
		"deployment_id": snapshot.DeploymentID, "operation_id": snapshot.OperationID,
		"capability_id": snapshot.CapabilityID, "credential_binding_id": snapshot.CredentialBindingID,
		"execution_profile_id": snapshot.ExecutionProfileID, "execution_profile_version": snapshot.ExecutionProfileVersion,
		"protocol_profile_id": snapshot.ProtocolProfileID, "protocol_profile_version": snapshot.ProtocolProfileVersion,
		"probe_contract_id": snapshot.ProbeContractID, "probe_contract_version": snapshot.ProbeContractVersion,
	}
	for key, value := range modelCapabilityProbeRequestContractSummary(snapshot.ProbeContractID, snapshot.CapabilityID) {
		requestSummaryFields[key] = value
	}
	requestSummary, _ := json.Marshal(requestSummaryFields)
	responseSummary, _ := json.Marshal(map[string]interface{}{
		"provider_success": outcome.ProviderSuccess, "contract_success": outcome.ContractSuccess,
		"current": currentProof, "reason_code": reasonCode, "provider_reason_code": outcome.ReasonCode,
		"contract_error_code": outcome.ContractErrorCode,
		"response_bytes":      outcome.ResponseBytes, "data_event_count": outcome.DataEventCount,
		"delta_count": outcome.DeltaCount, "finish_reason": outcome.FinishReason,
		"reasoning_observed":         outcome.ReasoningObserved,
		"tool_call_observed":         outcome.ToolCallObserved,
		"structured_output_observed": outcome.StructuredObserved,
		"usage":                      map[string]int64{"prompt_tokens": outcome.PromptTokens, "completion_tokens": outcome.CompletionTokens, "total_tokens": outcome.TotalTokens},
	})
	fingerprint := modelCapabilityProbeFingerprint(snapshot, outcome, sourceRef)
	errorSummary := ""
	if !success {
		errorSummary = reasonCode
	}
	var probeID int64
	var testedAt time.Time
	err = tx.QueryRow(ctx, `
INSERT INTO model_capability_probes (
  customer_org_id,deployment_id,connection_id,operation_id,capability_id,
  execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,
  probe_contract_id,probe_contract_version,provider,base_url,adapter_type,adapter_config,
  upstream_model,connection_revision,deployment_revision,operation_revision,
  capability_support_revision,credential_binding_id,credential_state_revision,
  credential_link_created_at,provider_success,success,status_code,latency_ms,
  reported_model,error_summary,request_summary,response_summary,source_kind,source_ref,
  source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb,$16,$17,$18,$19,$20,
        $21,$22,$23,$24,$25,$26,$27,$28,$29,$30::jsonb,$31::jsonb,$32,$33,$34,$35,NOW())
RETURNING capability_probe_id,tested_at`,
		snapshot.CustomerOrgID, snapshot.DeploymentID, snapshot.ConnectionID, snapshot.OperationID, snapshot.CapabilityID,
		snapshot.ExecutionProfileID, snapshot.ExecutionProfileVersion, snapshot.ProtocolProfileID, snapshot.ProtocolProfileVersion,
		snapshot.ProbeContractID, snapshot.ProbeContractVersion, snapshot.Provider, snapshot.BaseURL, snapshot.AdapterType, string(snapshot.AdapterConfig),
		snapshot.UpstreamModel, snapshot.ConnectionRevision, snapshot.DeploymentRevision, snapshot.OperationRevision,
		snapshot.CapabilitySupportRevision, snapshot.CredentialBindingID, snapshot.CredentialRevision, snapshot.CredentialLinkCreatedAt,
		outcome.ProviderSuccess, success, outcome.StatusCode, outcome.LatencyMS, outcome.ReportedModel, errorSummary,
		string(requestSummary), string(responseSummary), modelcapabilityqualification.ProbeSourceKind, sourceRef, fingerprint,
		nullableCanonicalOperationProbeActor(actorID, present),
	).Scan(&probeID, &testedAt)
	if err != nil {
		return modelCapabilityProbeResult{}, fmt.Errorf("append model capability probe: %w", err)
	}
	evidenceRef := "capability_probe:" + strconv.FormatInt(probeID, 10)
	if success {
		var hasQualified bool
		if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM model_capability_execution_qualifications
   WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3
     AND execution_profile_id=$4 AND execution_profile_version=$5
     AND protocol_profile_id=$6 AND protocol_profile_version=$7
)`, snapshot.DeploymentID, snapshot.OperationID, snapshot.CapabilityID,
			snapshot.ExecutionProfileID, snapshot.ExecutionProfileVersion,
			snapshot.ProtocolProfileID, snapshot.ProtocolProfileVersion).Scan(&hasQualified); err != nil {
			return modelCapabilityProbeResult{}, fmt.Errorf("read current capability qualification: %w", err)
		}
		if !hasQualified {
			if _, err := tx.Exec(ctx, `
UPDATE model_deployment_capabilities
   SET evidence_state='live_probed',evidence_ref=$4,verified_at=$5,
       revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3
   AND support_state='enabled' AND support_revision=$6`,
				snapshot.DeploymentID, snapshot.OperationID, snapshot.CapabilityID,
				evidenceRef, testedAt, snapshot.CapabilitySupportRevision); err != nil {
				return modelCapabilityProbeResult{}, fmt.Errorf("project capability probe evidence: %w", err)
			}
		}
	}
	result := modelCapabilityProbeResult{
		CapabilityProbeID: probeID, DeploymentID: snapshot.DeploymentID, OperationID: snapshot.OperationID,
		CapabilityID: snapshot.CapabilityID, Success: success, ProviderSuccess: outcome.ProviderSuccess,
		Current: currentProof, Stale: !currentProof, ReasonCode: reasonCode, ProviderReasonCode: outcome.ReasonCode,
		ContractErrorCode: outcome.ContractErrorCode,
		StatusCode:        outcome.StatusCode, LatencyMS: outcome.LatencyMS, ReportedModel: outcome.ReportedModel,
		FinishReason: outcome.FinishReason, PromptTokens: outcome.PromptTokens,
		CompletionTokens: outcome.CompletionTokens, TotalTokens: outcome.TotalTokens,
		ReasoningObserved: outcome.ReasoningObserved, ToolCallObserved: outcome.ToolCallObserved,
		StructuredObserved: outcome.StructuredObserved,
	}
	if success {
		result.EvidenceRef = evidenceRef
	}
	if err := insertAuditEntry(ctx, tx, "model_deployment.capability_probe", "model_deployment", strconv.FormatInt(snapshot.DeploymentID, 10), map[string]interface{}{
		"after": map[string]interface{}{
			"capability_probe_id": probeID, "operation_id": snapshot.OperationID,
			"capability_id": snapshot.CapabilityID, "credential_binding_id": snapshot.CredentialBindingID,
			"provider_success": outcome.ProviderSuccess, "success": success, "current": currentProof,
			"status_code": outcome.StatusCode, "latency_ms": outcome.LatencyMS,
			"reason_code": reasonCode, "provider_reason_code": outcome.ReasonCode,
		},
	}, audit); err != nil {
		return modelCapabilityProbeResult{}, fmt.Errorf("audit model capability probe: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return modelCapabilityProbeResult{}, fmt.Errorf("commit model capability probe: %w", err)
	}
	if success && h.routingController != nil {
		h.routingController.Invalidate()
	}
	return result, nil
}

func modelCapabilityProbeRequestContractSummary(probeContractID, capabilityID string) map[string]interface{} {
	switch probeContractID {
	case modelcapabilityqualification.DeepSeekChatSSEProbeContractID:
		return map[string]interface{}{
			"stream": true, "stream_options_include_usage": true,
			"thinking_type": "disabled", "max_tokens": 64,
		}
	case modelcapabilityqualification.DeepSeekChatReasoningProbeContractID:
		return map[string]interface{}{
			"stream": false, "thinking_type": "enabled", "max_tokens": 512,
		}
	case modelcapabilityqualification.DeepSeekChatReasoningEffortProbeContractID:
		return map[string]interface{}{
			"stream": false, "reasoning_effort": "low", "max_tokens": 512,
		}
	case modelcapabilityqualification.DeepSeekChatFunctionToolsProbeContractID:
		choice := "required"
		switch capabilityID {
		case modelprofile.CapabilityToolChoiceAuto:
			choice = "auto"
		case modelprofile.CapabilityToolChoiceNamed:
			choice = "named"
		}
		return map[string]interface{}{
			"stream": false, "thinking_type": "disabled", "max_tokens": 256,
			"function_name": "unirouter_probe_echo", "tool_choice": choice,
		}
	case modelcapabilityqualification.DeepSeekChatJSONObjectProbeContractID:
		return map[string]interface{}{
			"stream": false, "thinking_type": "disabled", "max_tokens": 64,
			"response_format_type": "json_object",
		}
	case modelcapabilityqualification.KimiChatSSEProbeContractID:
		return map[string]interface{}{
			"stream": true, "stream_options_include_usage": true,
			"thinking_type": "disabled", "max_completion_tokens": 64,
		}
	case modelcapabilityqualification.KimiChatReasoningProbeContractID:
		return map[string]interface{}{
			"stream": false, "thinking_type": "provider_default_enabled", "max_completion_tokens": 512,
		}
	case modelcapabilityqualification.KimiChatFunctionToolsProbeContractID:
		return map[string]interface{}{
			"stream": false, "thinking_type": "disabled", "max_completion_tokens": 256,
			"function_name": "unirouter_probe_echo", "tool_choice": "auto",
		}
	case modelcapabilityqualification.KimiChatJSONObjectProbeContractID:
		return map[string]interface{}{
			"stream": false, "thinking_type": "disabled", "max_completion_tokens": 64,
			"response_format_type": "json_object",
		}
	default:
		return map[string]interface{}{}
	}
}

func sameModelCapabilitySnapshot(before, current modelCapabilitySnapshot) bool {
	return before.CustomerOrgID == current.CustomerOrgID && before.DeploymentID == current.DeploymentID &&
		before.PublicModelID == current.PublicModelID && before.ConnectionID == current.ConnectionID &&
		before.CredentialBindingID == current.CredentialBindingID && before.Provider == current.Provider &&
		before.BaseURL == current.BaseURL && before.AdapterType == current.AdapterType && bytes.Equal(before.AdapterConfig, current.AdapterConfig) &&
		before.ConnectionRevision == current.ConnectionRevision && before.UpstreamModel == current.UpstreamModel &&
		before.DeploymentRevision == current.DeploymentRevision && before.OperationID == current.OperationID &&
		before.OperationRevision == current.OperationRevision && before.ProtocolProfileID == current.ProtocolProfileID &&
		before.ProtocolProfileVersion == current.ProtocolProfileVersion && before.CapabilityID == current.CapabilityID &&
		before.CapabilitySupportRevision == current.CapabilitySupportRevision &&
		before.ExecutionProfileID == current.ExecutionProfileID && before.ExecutionProfileVersion == current.ExecutionProfileVersion &&
		before.ProbeContractID == current.ProbeContractID && before.ProbeContractVersion == current.ProbeContractVersion &&
		before.CredentialRevision == current.CredentialRevision && before.CredentialLinkCreatedAt.Equal(current.CredentialLinkCreatedAt)
}

func modelCapabilityProbeFingerprint(snapshot modelCapabilitySnapshot, outcome modelCapabilityProbeOutcome, sourceRef string) string {
	payload := strings.Join([]string{
		modelcapabilityqualification.ProbeSourceKind, sourceRef,
		strconv.FormatInt(snapshot.DeploymentID, 10), snapshot.OperationID, snapshot.CapabilityID,
		snapshot.ExecutionProfileID, snapshot.ExecutionProfileVersion,
		snapshot.ProtocolProfileID, snapshot.ProtocolProfileVersion,
		snapshot.ProbeContractID, snapshot.ProbeContractVersion,
		strconv.FormatInt(snapshot.ConnectionRevision, 10), strconv.FormatInt(snapshot.DeploymentRevision, 10),
		strconv.FormatInt(snapshot.OperationRevision, 10), strconv.FormatInt(snapshot.CapabilitySupportRevision, 10),
		strconv.FormatInt(snapshot.CredentialBindingID, 10), strconv.FormatInt(snapshot.CredentialRevision, 10),
		strconv.FormatBool(outcome.ContractSuccess), strconv.Itoa(outcome.StatusCode), outcome.ReasonCode,
	}, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (h *Handler) approveCanonicalModelCapabilityRouting(ctx context.Context, targetOrgID, actorID, actorOrgID, deploymentID int64, operationID, capabilityID string, request modelCapabilityApprovalRequest, audit auditMeta) (modelCapabilityApprovalResult, error) {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return modelCapabilityApprovalResult{}, fmt.Errorf("begin capability approval: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
		return modelCapabilityApprovalResult{}, fmt.Errorf("configure capability approval: %w", err)
	}
	if err := validateCanonicalOperationProbeCustomerOrg(ctx, tx, targetOrgID); err != nil {
		return modelCapabilityApprovalResult{}, err
	}
	var existing modelCapabilityApprovalResult
	var existingActor, existingActorOrg int64
	var existingReason string
	err = tx.QueryRow(ctx, `
SELECT approval.approval_id,approval.capability_probe_id,approval.deployment_id,
       approval.operation_id,approval.capability_id,approval.approved_at,
       approval.approved_by,approval.approved_by_org_id,approval.reason,
       qualification.qualified,qualification.qualification_state
  FROM model_capability_routing_approvals approval
  JOIN model_capability_approval_qualifications qualification
    ON qualification.approval_id=approval.approval_id
 WHERE approval.capability_probe_id=$1
 FOR SHARE OF approval`, request.CapabilityProbeID).Scan(
		&existing.ApprovalID, &existing.CapabilityProbeID, &existing.DeploymentID,
		&existing.OperationID, &existing.CapabilityID, &existing.ApprovedAt,
		&existingActor, &existingActorOrg, &existingReason,
		&existing.Qualified, &existing.QualificationState,
	)
	if err == nil {
		if existing.DeploymentID != deploymentID || existing.OperationID != operationID || existing.CapabilityID != capabilityID ||
			existingActor != actorID || existingActorOrg != actorOrgID || existingReason != request.Reason {
			return modelCapabilityApprovalResult{}, errModelCapabilityConflict
		}
		existing.EvidenceRef = "capability_approval:" + strconv.FormatInt(existing.ApprovalID, 10)
		existing.IdempotencyReplay = true
		if err := tx.Commit(ctx); err != nil {
			return modelCapabilityApprovalResult{}, fmt.Errorf("commit capability approval replay: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return modelCapabilityApprovalResult{}, fmt.Errorf("read capability approval replay: %w", err)
	}
	var approvalID int64
	var approvedAt time.Time
	err = tx.QueryRow(ctx, `
INSERT INTO model_capability_routing_approvals (
  capability_probe_id,customer_org_id,deployment_id,connection_id,operation_id,capability_id,
  execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,
  probe_contract_id,probe_contract_version,connection_revision,deployment_revision,
  operation_revision,capability_support_revision,credential_binding_id,
  credential_state_revision,credential_link_created_at,approved_by,approved_by_org_id,
  reason,source_kind,source_ref,approved_at)
SELECT probe.capability_probe_id,probe.customer_org_id,probe.deployment_id,probe.connection_id,
       probe.operation_id,probe.capability_id,probe.execution_profile_id,probe.execution_profile_version,
       probe.protocol_profile_id,probe.protocol_profile_version,probe.probe_contract_id,
       probe.probe_contract_version,probe.connection_revision,probe.deployment_revision,
       probe.operation_revision,probe.capability_support_revision,probe.credential_binding_id,
       probe.credential_state_revision,probe.credential_link_created_at,$5,$6,$7,
       $8,'capability_probe:' || probe.capability_probe_id::TEXT,NOW()
  FROM model_capability_probes probe
 WHERE probe.capability_probe_id=$1 AND probe.deployment_id=$2
   AND probe.operation_id=$3 AND probe.capability_id=$4
RETURNING approval_id,approved_at`, request.CapabilityProbeID, deploymentID, operationID, capabilityID,
		actorID, actorOrgID, request.Reason, modelcapabilityqualification.ApprovalSourceKind).Scan(&approvalID, &approvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return modelCapabilityApprovalResult{}, errModelCapabilityNotFound
	}
	if err != nil {
		if isModelCapabilityConflictError(err) {
			return modelCapabilityApprovalResult{}, errModelCapabilityConflict
		}
		return modelCapabilityApprovalResult{}, fmt.Errorf("append capability approval: %w", err)
	}
	evidenceRef := "capability_approval:" + strconv.FormatInt(approvalID, 10)
	tag, err := tx.Exec(ctx, `
UPDATE model_deployment_capabilities
   SET evidence_state='production_approved',evidence_ref=$4,verified_at=$5,
       revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3
   AND support_state='enabled'`, deploymentID, operationID, capabilityID, evidenceRef, approvedAt)
	if err != nil {
		if isModelCapabilityConflictError(err) {
			return modelCapabilityApprovalResult{}, errModelCapabilityConflict
		}
		return modelCapabilityApprovalResult{}, fmt.Errorf("project capability approval: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return modelCapabilityApprovalResult{}, errModelCapabilityConflict
	}
	result := modelCapabilityApprovalResult{
		ApprovalID: approvalID, CapabilityProbeID: request.CapabilityProbeID,
		DeploymentID: deploymentID, OperationID: operationID, CapabilityID: capabilityID,
		Qualified: true, QualificationState: "qualified", EvidenceRef: evidenceRef, ApprovedAt: approvedAt,
	}
	if err := insertAuditEntry(ctx, tx, "model_deployment.capability_routing_approval", "model_deployment", strconv.FormatInt(deploymentID, 10), map[string]interface{}{
		"after": map[string]interface{}{
			"approval_id": approvalID, "capability_probe_id": request.CapabilityProbeID,
			"operation_id": operationID, "capability_id": capabilityID,
			"qualification_state": "qualified", "reason": request.Reason,
		},
	}, audit); err != nil {
		return modelCapabilityApprovalResult{}, fmt.Errorf("audit capability approval: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return modelCapabilityApprovalResult{}, fmt.Errorf("commit capability approval: %w", err)
	}
	if h.routingController != nil {
		h.routingController.Invalidate()
	}
	return result, nil
}

func (h *Handler) loadModelCapabilityQualificationView(ctx context.Context, orgID, deploymentID int64, operationID, capabilityID string) (modelCapabilityQualificationView, error) {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return modelCapabilityQualificationView{}, fmt.Errorf("begin capability qualification view: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return modelCapabilityQualificationView{}, fmt.Errorf("configure capability qualification view: %w", err)
	}
	view := modelCapabilityQualificationView{ContractVersion: "unirouter.model-capability-qualification.v1"}
	var probeContractID, probeContractVersion *string
	err = tx.QueryRow(ctx, `
SELECT deployment.id,operation.operation_id,capability.capability_id,
       capability.support_state,capability.support_revision,
       model.execution_profile_id,model.execution_profile_version,
       operation.protocol_profile_id,operation.protocol_profile_version,
       current_contract.probe_contract_id,current_contract.probe_contract_version
  FROM model_deployments deployment
  JOIN model_registry model ON model.id=deployment.public_model_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id AND operation.operation_id=$3
  JOIN model_deployment_capabilities capability
    ON capability.deployment_id=deployment.id
   AND capability.operation_id=operation.operation_id
   AND capability.capability_id=$4
  LEFT JOIN model_capability_current_probe_contracts current_contract
    ON current_contract.operation_id=operation.operation_id
   AND current_contract.capability_id=capability.capability_id
   AND current_contract.execution_profile_id=model.execution_profile_id
   AND current_contract.execution_profile_version=model.execution_profile_version
   AND current_contract.protocol_profile_id=operation.protocol_profile_id
   AND current_contract.protocol_profile_version=operation.protocol_profile_version
 WHERE deployment.customer_org_id=$1 AND deployment.id=$2`, orgID, deploymentID, operationID, capabilityID).Scan(
		&view.DeploymentID, &view.OperationID, &view.CapabilityID,
		&view.SupportState, &view.SupportRevision,
		&view.ExecutionProfileID, &view.ExecutionProfileVersion,
		&view.ProtocolProfileID, &view.ProtocolProfileVersion,
		&probeContractID, &probeContractVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return modelCapabilityQualificationView{}, errModelCapabilityNotFound
	}
	if err != nil {
		return modelCapabilityQualificationView{}, fmt.Errorf("read capability qualification target: %w", err)
	}
	if probeContractID != nil && probeContractVersion != nil {
		view.ProbeContractID = *probeContractID
		view.ProbeContractVersion = *probeContractVersion
		_, resolveErr := modelcapabilityqualification.Resolve(view.OperationID, view.CapabilityID,
			view.ExecutionProfileID, view.ExecutionProfileVersion, view.ProtocolProfileID, view.ProtocolProfileVersion)
		view.ProbeAvailable = resolveErr == nil && view.SupportState == "enabled"
		if view.ProbeAvailable {
			view.ProbeAvailabilityReason = "typed_probe_available"
		} else if resolveErr != nil {
			view.ProbeAvailabilityReason = "compiled_probe_contract_unavailable"
		} else {
			view.ProbeAvailabilityReason = "capability_unsupported"
		}
	} else {
		view.ProbeAvailabilityReason = "probe_contract_unavailable"
	}

	var approvalState string
	err = tx.QueryRow(ctx, `
SELECT approval_id,capability_probe_id,qualification_state,qualified
  FROM model_capability_execution_qualifications
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3
   AND execution_profile_id=$4 AND execution_profile_version=$5
   AND protocol_profile_id=$6 AND protocol_profile_version=$7`,
		deploymentID, operationID, capabilityID, view.ExecutionProfileID, view.ExecutionProfileVersion,
		view.ProtocolProfileID, view.ProtocolProfileVersion).Scan(
		&view.PreferredApprovalID, &view.PreferredCapabilityProbeID, &approvalState, &view.Qualified,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return modelCapabilityQualificationView{}, fmt.Errorf("read preferred capability qualification: %w", err)
	}
	if view.Qualified {
		view.State = "approved_current"
		view.QualificationState = approvalState
	}

	latest := modelCapabilityProbeView{}
	var responseSummary json.RawMessage
	var latestCurrent bool
	err = tx.QueryRow(ctx, `
SELECT probe.capability_probe_id,probe.credential_binding_id,probe.success,
       probe.provider_success,probe.status_code,probe.latency_ms,probe.reported_model,
       probe.error_summary,probe.response_summary,probe.tested_at,
       probe.connection_revision=connection.config_revision
       AND probe.provider=connection.provider AND probe.base_url=connection.base_url
       AND probe.adapter_type=connection.adapter_type
       AND probe.adapter_config=COALESCE(connection.adapter_config,'{}'::jsonb)
       AND probe.deployment_revision=deployment.config_revision
       AND probe.upstream_model=deployment.upstream_model
       AND probe.operation_revision=operation.revision
       AND probe.protocol_profile_id=operation.protocol_profile_id
       AND probe.protocol_profile_version=operation.protocol_profile_version
       AND probe.capability_support_revision=capability.support_revision
       AND probe.execution_profile_id=model.execution_profile_id
       AND probe.execution_profile_version=model.execution_profile_version
       AND probe.probe_contract_id=current_contract.probe_contract_id
       AND probe.probe_contract_version=current_contract.probe_contract_version
       AND binding.status='active' AND binding.key_enc<>''
       AND binding.state_revision=probe.credential_state_revision
       AND (binding.cooldown_until IS NULL OR binding.cooldown_until<=transaction_timestamp())
       AND link.created_at=probe.credential_link_created_at AS current
  FROM model_capability_probes probe
  JOIN model_deployments deployment ON deployment.id=probe.deployment_id
  JOIN provider_connections connection ON connection.id=probe.connection_id
  JOIN model_registry model ON model.id=deployment.public_model_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=probe.deployment_id AND operation.operation_id=probe.operation_id
  JOIN model_deployment_capabilities capability
    ON capability.deployment_id=probe.deployment_id
   AND capability.operation_id=probe.operation_id
   AND capability.capability_id=probe.capability_id
  JOIN model_capability_current_probe_contracts current_contract
    ON current_contract.operation_id=probe.operation_id
   AND current_contract.capability_id=probe.capability_id
   AND current_contract.execution_profile_id=model.execution_profile_id
   AND current_contract.execution_profile_version=model.execution_profile_version
   AND current_contract.protocol_profile_id=operation.protocol_profile_id
   AND current_contract.protocol_profile_version=operation.protocol_profile_version
  JOIN provider_credential_bindings binding
    ON binding.id=probe.credential_binding_id AND binding.connection_id=probe.connection_id
  JOIN credential_binding_deployments link
    ON link.binding_id=probe.credential_binding_id
   AND link.deployment_id=probe.deployment_id AND link.connection_id=probe.connection_id
 WHERE probe.deployment_id=$1 AND probe.operation_id=$2 AND probe.capability_id=$3
   AND probe.execution_profile_id=$4 AND probe.execution_profile_version=$5
 ORDER BY probe.tested_at DESC,probe.capability_probe_id DESC LIMIT 1`,
		deploymentID, operationID, capabilityID, view.ExecutionProfileID, view.ExecutionProfileVersion).Scan(
		&latest.CapabilityProbeID, &latest.CredentialBindingID, &latest.Success,
		&latest.ProviderSuccess, &latest.StatusCode, &latest.LatencyMS, &latest.ReportedModel,
		&latest.ErrorSummary, &responseSummary, &latest.TestedAt, &latestCurrent,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return modelCapabilityQualificationView{}, fmt.Errorf("read latest capability probe: %w", err)
	}
	if err == nil {
		latest.Current = latestCurrent
		var summary struct {
			FinishReason       string `json:"finish_reason"`
			ContractErrorCode  string `json:"contract_error_code"`
			ReasoningObserved  bool   `json:"reasoning_observed"`
			ToolCallObserved   bool   `json:"tool_call_observed"`
			StructuredObserved bool   `json:"structured_output_observed"`
			Usage              struct {
				PromptTokens     int64 `json:"prompt_tokens"`
				CompletionTokens int64 `json:"completion_tokens"`
				TotalTokens      int64 `json:"total_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(responseSummary, &summary)
		latest.FinishReason = summary.FinishReason
		latest.ContractErrorCode = summary.ContractErrorCode
		latest.ReasoningObserved = summary.ReasoningObserved
		latest.ToolCallObserved = summary.ToolCallObserved
		latest.StructuredObserved = summary.StructuredObserved
		latest.PromptTokens = summary.Usage.PromptTokens
		latest.CompletionTokens = summary.Usage.CompletionTokens
		latest.TotalTokens = summary.Usage.TotalTokens
		view.LatestProbe = &latest
		if !view.Qualified {
			switch {
			case latest.Success && latestCurrent:
				view.State = "probed_awaiting_approval"
				view.QualificationState = "approval_missing"
			case latest.Success:
				view.State = "probe_stale"
				view.QualificationState = "probe_stale"
			default:
				view.State = "probe_failed"
				view.QualificationState = "probe_failed"
			}
		}
	}
	if view.State == "" {
		view.State = "not_probed"
		view.QualificationState = "approval_missing"
	}
	if err := tx.Commit(ctx); err != nil {
		return modelCapabilityQualificationView{}, fmt.Errorf("commit capability qualification view: %w", err)
	}
	return view, nil
}
