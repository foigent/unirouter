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
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeldeployment"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const (
	canonicalOperationProbeMaxRequestBytes  = 4 << 10
	canonicalOperationProbeMaxTextBytes     = 256
	canonicalOperationProbeMaxResponseBytes = 1 << 20
	canonicalOperationProbeMaxHeaderBytes   = 64 << 10
	canonicalOperationProbeTimeout          = 15 * time.Second
	canonicalOperationProbeSourceKind       = "admin.operation_probe.v1"
)

var (
	errCanonicalOperationProbeInvalid     = errors.New("canonical operation probe request is invalid")
	errCanonicalOperationProbeNotFound    = errors.New("canonical operation probe target not found")
	errCanonicalOperationProbeConflict    = errors.New("canonical operation probe target is not eligible")
	errCanonicalOperationProbeUnsupported = errors.New("canonical operation probe is unsupported")
)

var canonicalOperationProbeDefaultHTTPClient = newCanonicalOperationProbeHTTPClient()

// modelDeploymentProbeHTTPDoer is deliberately narrower than *http.Client so
// tests can prove the exact outbound operation without using a real provider.
type modelDeploymentProbeHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// SetModelDeploymentProbeHTTPDoer replaces only the canonical administrative
// probe transport. It does not affect gateway execution traffic.
func (h *Handler) SetModelDeploymentProbeHTTPDoer(doer adapters.HTTPDoer) {
	if h != nil {
		h.modelDeploymentProbeHTTPDoer = doer
	}
}

type canonicalOperationProbeRequest struct {
	CredentialBindingID int64  `json:"credential_binding_id"`
	ProbeText           string `json:"probe_text,omitempty"`
}

type canonicalOperationProbeResponse struct {
	ProbeID             int64  `json:"probe_id"`
	DeploymentID        int64  `json:"deployment_id"`
	OperationID         string `json:"operation_id"`
	CredentialBindingID int64  `json:"credential_binding_id"`
	Success             bool   `json:"success"`
	ProviderSuccess     bool   `json:"provider_success"`
	Current             bool   `json:"current"`
	Stale               bool   `json:"stale"`
	OperationAdvanced   bool   `json:"operation_advanced"`
	ReasonCode          string `json:"reason_code"`
	ProviderReasonCode  string `json:"provider_reason_code"`
	StatusCode          int    `json:"status_code"`
	LatencyMS           int64  `json:"latency_ms"`
	ReportedModel       string `json:"reported_model,omitempty"`
	EvidenceRef         string `json:"evidence_ref,omitempty"`
}

type canonicalOperationProbeSnapshot struct {
	CustomerOrgID           int64
	DeploymentID            int64
	ConnectionID            int64
	CredentialBindingID     int64
	ConnectionName          string
	Provider                string
	BaseURL                 string
	AdapterType             string
	AdapterConfig           json.RawMessage
	ConnectionState         string
	ConnectionRevision      int64
	UpstreamModel           string
	DeploymentState         string
	DeploymentRevision      int64
	OperationID             string
	OperationSupportState   string
	OperationEvidenceState  string
	OperationRevision       int64
	ProfileID               string
	ProfileVersion          string
	ExecutionProfileID      string
	ExecutionProfileVersion string
	ProfileDefinitionDigest string
	Definition              modelprofile.Definition
	EncryptedSecret         string
	CredentialStatus        string
	CredentialRevision      int64
	CredentialCooldown      *time.Time
	CredentialEligible      bool
	LinkCreatedAt           time.Time
	LinkEligible            bool
	Actor                   canonicalOperationProbeActorSnapshot
}

type canonicalOperationProbeActorSnapshot struct {
	UserID            int64
	OrgID             int64
	OrganizationRole  string
	CompatibilityRole string
	AuthEpoch         int64
}

type canonicalOperationProbeOutcome struct {
	ProviderSuccess bool
	StatusCode      int
	LatencyMS       int64
	ReportedModel   string
	ReasonCode      string
	ResponseBytes   int
	OutputCount     int
	Units           []endpointprotocol.UnitObservation
}

type canonicalOperationProbeSpec struct {
	Endpoint         adapters.Endpoint
	ProfileID        string
	ProfileVersion   string
	SourceKind       string
	ExecutionBinding *modelqualification.Binding
}

func (h *Handler) ProbeCanonicalModelOperation(w http.ResponseWriter, r *http.Request) {
	deploymentID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("deploymentID")), 10, 64)
	if err != nil || deploymentID <= 0 {
		writeCanonicalOperationProbeError(w, errCanonicalOperationProbeInvalid)
		return
	}
	rawOperationID := r.PathValue("operationID")
	operationID, err := modeldeployment.NormalizeOperationID(rawOperationID)
	if err != nil || operationID != rawOperationID {
		writeCanonicalOperationProbeError(w, errCanonicalOperationProbeInvalid)
		return
	}
	if _, err := canonicalOperationProbeSpecFor(modeloperation.ID(operationID)); err != nil {
		writeCanonicalOperationProbeError(w, err)
		return
	}
	request, err := decodeCanonicalOperationProbeRequest(w, r)
	if err != nil {
		writeCanonicalOperationProbeError(w, err)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorID := middleware.GetUserIDFromContext(r.Context())
	if orgID <= 0 || actorID <= 0 {
		writeCanonicalOperationProbeError(w, errCanonicalOperationProbeInvalid)
		return
	}
	result, err := h.probeCanonicalModelOperation(
		r.Context(), orgID, actorID, deploymentID, operationID, request,
		requestAuditMeta(r, orgID, 0),
	)
	if err != nil {
		writeCanonicalOperationProbeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func decodeCanonicalOperationProbeRequest(w http.ResponseWriter, r *http.Request) (canonicalOperationProbeRequest, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, canonicalOperationProbeMaxRequestBytes))
	if err != nil || strictjson.Validate(body) != nil {
		return canonicalOperationProbeRequest{}, errCanonicalOperationProbeInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request canonicalOperationProbeRequest
	if err := decoder.Decode(&request); err != nil {
		return canonicalOperationProbeRequest{}, errCanonicalOperationProbeInvalid
	}
	if request.CredentialBindingID <= 0 {
		return canonicalOperationProbeRequest{}, errCanonicalOperationProbeInvalid
	}
	request.ProbeText = strings.TrimSpace(request.ProbeText)
	if len(request.ProbeText) > canonicalOperationProbeMaxTextBytes || !utf8.ValidString(request.ProbeText) || strings.ContainsRune(request.ProbeText, '\x00') {
		return canonicalOperationProbeRequest{}, errCanonicalOperationProbeInvalid
	}
	return request, nil
}

func writeCanonicalOperationProbeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errCanonicalOperationProbeInvalid), errors.Is(err, modeldeployment.ErrInvalidIdentity):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid canonical operation probe request", "code": "canonical_operation_probe_invalid",
		})
	case errors.Is(err, errCanonicalOperationProbeUnsupported):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error": "This operation does not have a production probe contract", "code": "canonical_operation_probe_unsupported",
		})
	case errors.Is(err, errCanonicalOperationProbeNotFound), errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": "Canonical operation probe target not found", "code": "canonical_operation_probe_not_found",
		})
	case errors.Is(err, errCanonicalOperationProbeConflict), errors.Is(err, modeldeployment.ErrRetiredIdentity):
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Canonical operation probe target is not currently eligible", "code": "canonical_operation_probe_ineligible",
		})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": "Canonical operation probe unavailable", "code": "canonical_operation_probe_unavailable",
		})
	}
}

func canonicalOperationProbeSpecFor(operation modeloperation.ID) (canonicalOperationProbeSpec, error) {
	switch operation {
	case modeloperation.IDChatCompletions:
		return canonicalOperationProbeSpec{
			Endpoint: adapters.EndpointChatCompletions, ProfileID: "openai.chat_completions", ProfileVersion: "1.0.0",
			SourceKind: canonicalOperationProbeSourceKind,
		}, nil
	case modeloperation.IDEmbeddings:
		return canonicalOperationProbeSpec{
			Endpoint: adapters.EndpointEmbeddings, ProfileID: "openai.embeddings", ProfileVersion: "1.0.0",
			SourceKind: canonicalOperationProbeSourceKind,
		}, nil
	case modeloperation.IDResponses, modeloperation.IDMessages:
		binding, err := modelqualification.CompiledBinding(string(operation))
		if err != nil {
			return canonicalOperationProbeSpec{}, errCanonicalOperationProbeUnsupported
		}
		if _, err := endpointprotocol.NewRegistry().Resolve(binding.ProtocolProfileID, binding.ProtocolProfileVersion); err != nil {
			return canonicalOperationProbeSpec{}, errCanonicalOperationProbeUnsupported
		}
		return canonicalOperationProbeSpec{
			ProfileID: binding.ProtocolProfileID, ProfileVersion: binding.ProtocolProfileVersion,
			SourceKind: modelqualification.LiveProbeSourceKind, ExecutionBinding: &binding,
		}, nil
	default:
		return canonicalOperationProbeSpec{}, errCanonicalOperationProbeUnsupported
	}
}

func canonicalOperationProbeSpecForProfile(
	operation modeloperation.ID,
	profileID, profileVersion string,
) (canonicalOperationProbeSpec, error) {
	if operation != modeloperation.IDResponses && operation != modeloperation.IDMessages {
		return canonicalOperationProbeSpecFor(operation)
	}
	binding, err := modelqualification.CompiledBindingFor(string(operation), profileID, profileVersion)
	if err != nil {
		return canonicalOperationProbeSpec{}, errCanonicalOperationProbeUnsupported
	}
	if _, err := endpointprotocol.NewRegistry().Resolve(binding.ProtocolProfileID, binding.ProtocolProfileVersion); err != nil {
		return canonicalOperationProbeSpec{}, errCanonicalOperationProbeUnsupported
	}
	return canonicalOperationProbeSpec{
		ProfileID: binding.ProtocolProfileID, ProfileVersion: binding.ProtocolProfileVersion,
		SourceKind: modelqualification.LiveProbeSourceKind, ExecutionBinding: &binding,
	}, nil
}

func canonicalOperationProbeSpecForExactProfile(
	operation modeloperation.ID,
	executionProfileID, executionProfileVersion,
	protocolProfileID, protocolProfileVersion string,
) (canonicalOperationProbeSpec, error) {
	identity, found, err := modelprofile.ResolveOperationProtocolIdentity(
		executionProfileID, executionProfileVersion, string(operation),
	)
	if err != nil {
		return canonicalOperationProbeSpec{}, errCanonicalOperationProbeUnsupported
	}
	if found {
		if identity.ProtocolProfileID == protocolProfileID &&
			identity.ProtocolProfileVersion == protocolProfileVersion {
			if operation == modeloperation.IDResponses || operation == modeloperation.IDMessages {
				return canonicalOperationProbeSpecForProfile(
					operation, protocolProfileID, protocolProfileVersion,
				)
			}
			return canonicalOperationProbeSpec{
				Endpoint: adapters.EndpointChatCompletions, ProfileID: protocolProfileID,
				ProfileVersion: protocolProfileVersion, SourceKind: canonicalOperationProbeSourceKind,
			}, nil
		}
		if !modelprofile.IsImmutableExistingAttachmentOperationProtocolIdentity(
			executionProfileID, executionProfileVersion, string(operation),
			protocolProfileID, protocolProfileVersion,
		) {
			return canonicalOperationProbeSpec{}, errCanonicalOperationProbeUnsupported
		}
	}
	return canonicalOperationProbeSpecFor(operation)
}

func (h *Handler) probeCanonicalModelOperation(
	ctx context.Context,
	orgID, actorID, deploymentID int64,
	operationID string,
	request canonicalOperationProbeRequest,
	audit auditMeta,
) (canonicalOperationProbeResponse, error) {
	snapshot, err := h.loadCanonicalOperationProbeSnapshot(
		ctx, orgID, actorID, deploymentID, operationID, request.CredentialBindingID,
	)
	if err != nil {
		return canonicalOperationProbeResponse{}, err
	}
	sourceRef, err := middleware.NewOperationID()
	if err != nil {
		return canonicalOperationProbeResponse{}, fmt.Errorf("generate canonical operation probe identity: %w", err)
	}
	outcome := h.executeCanonicalOperationProbe(ctx, snapshot, request.ProbeText, sourceRef)
	return h.persistCanonicalOperationProbe(ctx, actorID, snapshot, outcome, sourceRef, audit)
}

func (h *Handler) loadCanonicalOperationProbeSnapshot(
	ctx context.Context,
	orgID, actorID, deploymentID int64,
	operationID string,
	bindingID int64,
) (canonicalOperationProbeSnapshot, error) {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return canonicalOperationProbeSnapshot{}, fmt.Errorf("begin canonical operation probe snapshot: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
		return canonicalOperationProbeSnapshot{}, fmt.Errorf("configure canonical operation probe snapshot: %w", err)
	}
	if err := validateCanonicalOperationProbeCustomerOrg(ctx, tx, orgID); err != nil {
		return canonicalOperationProbeSnapshot{}, err
	}
	actor, err := lockCanonicalOperationProbeActor(ctx, tx, actorID, orgID)
	if err != nil {
		return canonicalOperationProbeSnapshot{}, err
	}
	snapshot, err := lockCanonicalOperationProbeRows(ctx, tx, orgID, deploymentID, operationID, bindingID, false)
	if err != nil {
		return canonicalOperationProbeSnapshot{}, err
	}
	if err := validateCanonicalOperationProbeSnapshot(snapshot); err != nil {
		return canonicalOperationProbeSnapshot{}, err
	}
	snapshot.Actor = actor
	if err := tx.Commit(ctx); err != nil {
		return canonicalOperationProbeSnapshot{}, fmt.Errorf("commit canonical operation probe snapshot: %w", err)
	}
	return snapshot, nil
}

func validateCanonicalOperationProbeCustomerOrg(ctx context.Context, tx pgx.Tx, orgID int64) error {
	if orgID <= 0 {
		return errCanonicalOperationProbeInvalid
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(8675309, 4)`); err != nil {
		return fmt.Errorf("lock canonical operation probe customer authority: %w", err)
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE orgs IN SHARE MODE`); err != nil {
		return fmt.Errorf("lock canonical operation probe customer rows: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT id FROM orgs
 WHERE status='active' AND org_type='customer' AND parent_id IS NULL
 ORDER BY id FOR SHARE`)
	if err != nil {
		return fmt.Errorf("read canonical operation probe customer authority: %w", err)
	}
	defer rows.Close()
	ids := make([]int64, 0, 2)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan canonical operation probe customer authority: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read canonical operation probe customer authority: %w", err)
	}
	if len(ids) == 1 && ids[0] != orgID {
		return errCanonicalOperationProbeNotFound
	}
	if len(ids) != 1 {
		return errCanonicalOperationProbeConflict
	}
	return nil
}

func lockCanonicalOperationProbeActor(
	ctx context.Context,
	tx pgx.Tx,
	actorID, targetOrgID int64,
) (canonicalOperationProbeActorSnapshot, error) {
	actor, present, active, err := lockCanonicalOperationProbeActorRow(ctx, tx, actorID)
	if err != nil {
		return canonicalOperationProbeActorSnapshot{}, err
	}
	if !present {
		return canonicalOperationProbeActorSnapshot{}, errCanonicalOperationProbeNotFound
	}
	if !active || !canonicalOperationProbeActorAllowed(actor, targetOrgID) {
		return canonicalOperationProbeActorSnapshot{}, errCanonicalOperationProbeConflict
	}
	return actor, nil
}

func lockCanonicalOperationProbeRows(
	ctx context.Context,
	tx pgx.Tx,
	orgID, deploymentID int64,
	operationID string,
	bindingID int64,
	operationForUpdate bool,
) (canonicalOperationProbeSnapshot, error) {
	lockClause := "FOR SHARE OF deployment,connection,public_model,operation,binding"
	if operationForUpdate {
		lockClause = "FOR SHARE OF deployment,connection,public_model,binding FOR UPDATE OF operation"
	}
	query := `
SELECT deployment.customer_org_id,deployment.id,deployment.connection_id,binding.id,
       connection.name,connection.provider,connection.base_url,connection.adapter_type,
       connection.adapter_config,connection.admin_state,connection.config_revision,
       deployment.upstream_model,deployment.admin_state,deployment.config_revision,
       operation.operation_id,operation.support_state,operation.evidence_state,
       operation.revision,operation.protocol_profile_id,operation.protocol_profile_version,
	   public_model.execution_profile_id,public_model.execution_profile_version,
       binding.key_enc,binding.status,binding.state_revision,binding.cooldown_until,
       (binding.cooldown_until IS NULL OR binding.cooldown_until<=transaction_timestamp()),
       COALESCE(to_jsonb(operation)->>'profile_definition_digest','')
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
	JOIN model_registry public_model
	  ON public_model.id=deployment.public_model_id
	 AND public_model.org_id=deployment.customer_org_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id AND operation.operation_id=$3
  JOIN provider_credential_bindings binding
    ON binding.id=$4 AND binding.connection_id=connection.id
 WHERE deployment.id=$2 AND deployment.customer_org_id=$1
 ` + lockClause
	var snapshot canonicalOperationProbeSnapshot
	if err := tx.QueryRow(ctx, query, orgID, deploymentID, operationID, bindingID).Scan(
		&snapshot.CustomerOrgID, &snapshot.DeploymentID, &snapshot.ConnectionID, &snapshot.CredentialBindingID,
		&snapshot.ConnectionName, &snapshot.Provider, &snapshot.BaseURL, &snapshot.AdapterType,
		&snapshot.AdapterConfig, &snapshot.ConnectionState, &snapshot.ConnectionRevision,
		&snapshot.UpstreamModel, &snapshot.DeploymentState, &snapshot.DeploymentRevision,
		&snapshot.OperationID, &snapshot.OperationSupportState, &snapshot.OperationEvidenceState,
		&snapshot.OperationRevision, &snapshot.ProfileID, &snapshot.ProfileVersion,
		&snapshot.ExecutionProfileID, &snapshot.ExecutionProfileVersion,
		&snapshot.EncryptedSecret, &snapshot.CredentialStatus, &snapshot.CredentialRevision,
		&snapshot.CredentialCooldown, &snapshot.CredentialEligible, &snapshot.ProfileDefinitionDigest,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return canonicalOperationProbeSnapshot{}, errCanonicalOperationProbeNotFound
		}
		return canonicalOperationProbeSnapshot{}, fmt.Errorf("lock canonical operation probe target: %w", err)
	}
	if snapshot.ProfileDefinitionDigest != "" {
		d, err := modelprofile.ReadDefinition(ctx, tx, snapshot.ProfileDefinitionDigest)
		if err != nil {
			return canonicalOperationProbeSnapshot{}, err
		}
		snapshot.Definition = d
		snapshot.ExecutionProfileID, snapshot.ExecutionProfileVersion = d.Profile().ID, d.Profile().Version
	}
	if err := tx.QueryRow(ctx, `
SELECT created_at,created_at<=transaction_timestamp() FROM credential_binding_deployments
 WHERE binding_id=$1 AND deployment_id=$2 AND connection_id=$3
	 FOR SHARE`, bindingID, deploymentID, snapshot.ConnectionID).Scan(&snapshot.LinkCreatedAt, &snapshot.LinkEligible); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return canonicalOperationProbeSnapshot{}, errCanonicalOperationProbeNotFound
		}
		return canonicalOperationProbeSnapshot{}, fmt.Errorf("lock canonical operation probe credential link: %w", err)
	}
	return snapshot, nil
}

func validateCanonicalOperationProbeSnapshot(snapshot canonicalOperationProbeSnapshot) error {
	spec, err := canonicalOperationProbeSpecForSnapshot(snapshot)
	if err != nil {
		return err
	}
	if snapshot.ProfileID != spec.ProfileID || snapshot.ProfileVersion != spec.ProfileVersion {
		return errCanonicalOperationProbeConflict
	}
	if spec.ExecutionBinding != nil {
		if spec.ExecutionBinding.OperationID != snapshot.OperationID ||
			spec.ExecutionBinding.ProtocolProfileID != snapshot.ProfileID ||
			spec.ExecutionBinding.ProtocolProfileVersion != snapshot.ProfileVersion {
			return errCanonicalOperationProbeConflict
		}
		if _, err := modelqualification.NormalizeHTTPSOrigin(snapshot.BaseURL); err != nil {
			return errCanonicalOperationProbeConflict
		}
	}
	if snapshot.ConnectionState != "enabled" || snapshot.DeploymentState != "enabled" ||
		snapshot.OperationSupportState != string(modeldeployment.SupportEnabled) ||
		snapshot.CredentialStatus != "active" || !snapshot.CredentialEligible ||
		!snapshot.LinkEligible || snapshot.EncryptedSecret == "" {
		return errCanonicalOperationProbeConflict
	}
	if snapshot.ConnectionRevision <= 0 || snapshot.DeploymentRevision <= 0 ||
		snapshot.OperationRevision <= 0 || snapshot.CredentialRevision <= 0 || snapshot.LinkCreatedAt.IsZero() {
		return errCanonicalOperationProbeConflict
	}
	return nil
}

func (h *Handler) executeCanonicalOperationProbe(
	ctx context.Context,
	snapshot canonicalOperationProbeSnapshot,
	probeText, requestID string,
) canonicalOperationProbeOutcome {
	spec, err := canonicalOperationProbeSpecForSnapshot(snapshot)
	if err != nil {
		return canonicalOperationProbeOutcome{ReasonCode: "operation_unsupported"}
	}
	secret, err := auth.DecryptKey(snapshot.EncryptedSecret, h.encKey)
	if err != nil || strings.TrimSpace(secret) == "" {
		return canonicalOperationProbeOutcome{ReasonCode: "credential_unavailable"}
	}
	body, err := canonicalOperationProbePayloadForSnapshot(snapshot, probeText)
	if err != nil {
		return canonicalOperationProbeOutcome{ReasonCode: "request_build_failed"}
	}
	if spec.ExecutionBinding != nil {
		return h.executeEndpointProtocolOperationProbe(ctx, snapshot, spec, secret, body, requestID)
	}
	adapter, err := adapters.DefaultRegistry().Get(snapshot.AdapterType)
	if err != nil {
		return canonicalOperationProbeOutcome{ReasonCode: "adapter_unavailable"}
	}
	probeCtx, cancel := context.WithTimeout(ctx, canonicalOperationProbeTimeout)
	defer cancel()
	channel := models.Channel{
		Name: snapshot.ConnectionName, Provider: snapshot.Provider, BaseURL: snapshot.BaseURL,
		AdapterType: snapshot.AdapterType, AdapterConfig: snapshot.AdapterConfig,
	}
	request, err := adapter.BuildRequest(probeCtx, adapters.RequestInput{
		Endpoint: spec.Endpoint, DownstreamBody: body, RequestedModel: snapshot.UpstreamModel,
		MappedModel: snapshot.UpstreamModel, Channel: channel, APIKey: secret, RequestID: requestID,
	})
	if err != nil {
		return canonicalOperationProbeOutcome{ReasonCode: "request_build_failed"}
	}
	started := time.Now()
	response, err := h.canonicalOperationProbeDoer().Do(request)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		return canonicalOperationProbeOutcome{LatencyMS: latency, ReasonCode: "provider_request_failed"}
	}
	if response == nil || response.Body == nil {
		return canonicalOperationProbeOutcome{LatencyMS: latency, ReasonCode: "provider_response_invalid"}
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, canonicalOperationProbeMaxResponseBytes+1))
	outcome := canonicalOperationProbeOutcome{
		StatusCode: response.StatusCode, LatencyMS: latency, ResponseBytes: len(responseBody),
	}
	if outcome.StatusCode < 100 || outcome.StatusCode > 599 {
		outcome.StatusCode = 0
	}
	if readErr != nil {
		outcome.ReasonCode = "provider_response_read_failed"
		return outcome
	}
	if len(responseBody) > canonicalOperationProbeMaxResponseBytes {
		outcome.ReasonCode = "provider_response_too_large"
		return outcome
	}
	normalized, parseErr := adapter.ParseResponse(probeCtx, adapters.ResponseInput{
		Endpoint: spec.Endpoint, StatusCode: response.StatusCode, Header: response.Header,
		Body: responseBody, Channel: channel, RequestedModel: snapshot.UpstreamModel,
		UpstreamModel: snapshot.UpstreamModel, TTFTMillis: int(latency),
	})
	if parseErr != nil || normalized == nil {
		outcome.ReasonCode = "provider_response_invalid"
		return outcome
	}
	outcome.ReportedModel = canonicalOperationProbeReportedModel(responseBody)
	if response.StatusCode < 200 || response.StatusCode >= 300 || normalized.Error != nil {
		outcome.ReasonCode = "provider_http_error"
		return outcome
	}
	if !canonicalOperationProbeResponseShape(modeloperation.ID(snapshot.OperationID), normalized.Body) {
		outcome.ReasonCode = "provider_response_invalid"
		return outcome
	}
	outcome.ProviderSuccess = true
	outcome.ReasonCode = "ok"
	return outcome
}

func (h *Handler) executeEndpointProtocolOperationProbe(
	ctx context.Context,
	snapshot canonicalOperationProbeSnapshot,
	spec canonicalOperationProbeSpec,
	secret string,
	body []byte,
	requestID string,
) canonicalOperationProbeOutcome {
	if spec.ExecutionBinding == nil {
		return canonicalOperationProbeOutcome{ReasonCode: "operation_unsupported"}
	}
	origin, err := modelqualification.NormalizeHTTPSOrigin(snapshot.BaseURL)
	if err != nil {
		return canonicalOperationProbeOutcome{ReasonCode: "https_origin_invalid"}
	}
	profile, err := endpointprotocol.NewRegistry().Resolve(spec.ProfileID, spec.ProfileVersion)
	if err != nil {
		return canonicalOperationProbeOutcome{ReasonCode: "protocol_profile_unavailable"}
	}
	probeCtx, cancel := context.WithTimeout(ctx, canonicalOperationProbeTimeout)
	defer cancel()
	request, binding, err := profile.BuildRequest(probeCtx, endpointprotocol.BuildInput{
		BaseURL: origin, APIKey: secret, RequestID: requestID,
		RequestedModel: snapshot.UpstreamModel, MappedModel: snapshot.UpstreamModel, Body: body,
	})
	if err != nil || request == nil || binding == nil {
		return canonicalOperationProbeOutcome{ReasonCode: "request_build_failed"}
	}
	started := time.Now()
	response, err := h.canonicalOperationProbeDoer().Do(request)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		return canonicalOperationProbeOutcome{LatencyMS: latency, ReasonCode: "provider_request_failed"}
	}
	if response == nil || response.Body == nil {
		return canonicalOperationProbeOutcome{LatencyMS: latency, ReasonCode: "provider_response_invalid"}
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, canonicalOperationProbeMaxResponseBytes+1))
	outcome := canonicalOperationProbeOutcome{
		StatusCode: response.StatusCode, LatencyMS: latency, ResponseBytes: len(responseBody),
	}
	if outcome.StatusCode < 100 || outcome.StatusCode > 599 {
		outcome.StatusCode = 0
	}
	if readErr != nil {
		outcome.ReasonCode = "provider_response_read_failed"
		return outcome
	}
	if len(responseBody) > canonicalOperationProbeMaxResponseBytes {
		outcome.ReasonCode = "provider_response_too_large"
		return outcome
	}
	parsed, parseErr := profile.ParseResponse(probeCtx, binding, response.StatusCode, responseBody)
	if parseErr != nil {
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			outcome.ReasonCode = "provider_http_error"
		} else {
			outcome.ReasonCode = "provider_response_invalid"
		}
		return outcome
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || len(parsed.Units) == 0 {
		outcome.ReasonCode = "provider_response_invalid"
		return outcome
	}
	outcome.ProviderSuccess = true
	outcome.ReasonCode = "ok"
	outcome.ReportedModel = parsed.MappedModel
	outcome.OutputCount = parsed.OutputCount
	outcome.Units = append([]endpointprotocol.UnitObservation(nil), parsed.Units...)
	return outcome
}

func (h *Handler) canonicalOperationProbeDoer() modelDeploymentProbeHTTPDoer {
	if h != nil && h.modelDeploymentProbeHTTPDoer != nil {
		return h.modelDeploymentProbeHTTPDoer
	}
	return canonicalOperationProbeDefaultHTTPClient
}

func newCanonicalOperationProbeHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxResponseHeaderBytes = canonicalOperationProbeMaxHeaderBytes
	return &http.Client{
		Transport: transport,
		Timeout:   canonicalOperationProbeTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func canonicalOperationProbePayload(
	operation modeloperation.ID,
	upstreamModel, probeText, executionProfileID, executionProfileVersion string,
) ([]byte, error) {
	if probeText == "" {
		probeText = "UniRouter connectivity probe"
	}
	switch operation {
	case modeloperation.IDChatCompletions:
		payload := map[string]interface{}{
			"model": upstreamModel, "stream": false, "max_tokens": 1,
			"messages": []map[string]string{{"role": "user", "content": probeText}},
		}
		thinking, required, err := resolveKimiRequiredThinkingContract(executionProfileID, executionProfileVersion)
		if err != nil {
			return nil, err
		}
		if required {
			delete(payload, "max_tokens")
			payload["max_completion_tokens"] = 64
			payload["thinking"] = thinking
		} else if thinking, present, err := resolveGLM5TurboOperationProbeThinking(
			executionProfileID, executionProfileVersion,
		); err != nil {
			return nil, err
		} else if present {
			payload["thinking"] = thinking
		}
		return json.Marshal(payload)
	case modeloperation.IDEmbeddings:
		return json.Marshal(map[string]interface{}{"model": upstreamModel, "input": probeText})
	case modeloperation.IDResponses:
		return json.Marshal(map[string]interface{}{
			"model": upstreamModel, "input": probeText, "max_output_tokens": 16,
			"stream": false, "store": false,
		})
	case modeloperation.IDMessages:
		return json.Marshal(map[string]interface{}{
			"model": upstreamModel, "max_tokens": 1, "stream": false,
			"messages": []map[string]string{{"role": "user", "content": probeText}},
		})
	default:
		return nil, errCanonicalOperationProbeUnsupported
	}
}

func resolveGLM5TurboOperationProbeThinking(profileID, profileVersion string) (map[string]string, bool, error) {
	const exactProfileID = "glm.5-turbo.api"
	if profileID != exactProfileID {
		return nil, false, nil
	}
	if profileVersion != "1.0.0" {
		return nil, false, fmt.Errorf("unsupported GLM-5-Turbo operation probe profile %s@%s", profileID, profileVersion)
	}
	profile, ok := modelprofile.Lookup(profileID, profileVersion)
	if !ok || profile.ID != profileID || profile.Version != profileVersion {
		return nil, false, fmt.Errorf("GLM-5-Turbo operation probe profile is unavailable")
	}
	contract, ok := modelprofile.ResolveReasoningContract(profile.ReasoningProfile)
	if !ok || contract.ProfileID != profile.ReasoningProfile ||
		contract.Mode != modelprofile.ReasoningModeToggle ||
		contract.ToggleControl.Field != "thinking" ||
		contract.ToggleControl.ProviderDefault != "enabled" ||
		!slices.Contains(contract.ToggleControl.AllowedValues, "disabled") {
		return nil, false, fmt.Errorf("GLM-5-Turbo operation probe reasoning contract is unavailable")
	}
	return map[string]string{"type": "disabled"}, true, nil
}

func canonicalOperationProbeRequestSummary(snapshot canonicalOperationProbeSnapshot) ([]byte, error) {
	summary := map[string]interface{}{
		"deployment_id": snapshot.DeploymentID, "connection_id": snapshot.ConnectionID,
		"operation_id": snapshot.OperationID, "credential_binding_id": snapshot.CredentialBindingID,
		"protocol_profile_id": snapshot.ProfileID, "protocol_profile_version": snapshot.ProfileVersion,
		"execution_profile_id": snapshot.ExecutionProfileID, "execution_profile_version": snapshot.ExecutionProfileVersion,
	}
	if snapshot.ProfileDefinitionDigest != "" {
		summary["profile_definition_digest"] = snapshot.ProfileDefinitionDigest
	}
	body, err := canonicalOperationProbePayloadForSnapshot(snapshot, "")
	if err != nil {
		return nil, err
	}
	var controls struct {
		Stream              *bool `json:"stream"`
		MaxTokens           *int  `json:"max_tokens"`
		MaxCompletionTokens *int  `json:"max_completion_tokens"`
		Thinking            *struct {
			Type string `json:"type"`
		} `json:"thinking"`
	}
	if err := json.Unmarshal(body, &controls); err != nil {
		return nil, err
	}
	if controls.Stream != nil {
		summary["stream"] = *controls.Stream
	}
	if controls.MaxTokens != nil {
		summary["max_tokens"] = *controls.MaxTokens
	}
	if controls.MaxCompletionTokens != nil {
		summary["max_completion_tokens"] = *controls.MaxCompletionTokens
	}
	if controls.Thinking != nil && controls.Thinking.Type != "" {
		summary["thinking_type"] = controls.Thinking.Type
	}
	return json.Marshal(summary)
}

func canonicalOperationProbeResponseShape(operation modeloperation.ID, body []byte) bool {
	switch operation {
	case modeloperation.IDChatCompletions:
		var response struct {
			Choices []struct {
				Message json.RawMessage `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal(body, &response) != nil || len(response.Choices) == 0 || len(response.Choices[0].Message) == 0 {
			return false
		}
		var message map[string]json.RawMessage
		return json.Unmarshal(response.Choices[0].Message, &message) == nil && message != nil
	case modeloperation.IDEmbeddings:
		var response struct {
			Data []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
		}
		return json.Unmarshal(body, &response) == nil && len(response.Data) > 0 && len(response.Data[0].Embedding) > 0
	default:
		return false
	}
}

func canonicalOperationProbeReportedModel(body []byte) string {
	var response struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &response) != nil {
		return ""
	}
	model := strings.TrimSpace(response.Model)
	if len(model) > 200 || !utf8.ValidString(model) || strings.ContainsAny(model, "\x00\r\n") {
		return ""
	}
	return model
}

func (h *Handler) persistCanonicalOperationProbe(
	ctx context.Context,
	actorID int64,
	snapshot canonicalOperationProbeSnapshot,
	outcome canonicalOperationProbeOutcome,
	sourceRef string,
	audit auditMeta,
) (canonicalOperationProbeResponse, error) {
	spec, err := canonicalOperationProbeSpecForSnapshot(snapshot)
	if err != nil {
		return canonicalOperationProbeResponse{}, err
	}
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return canonicalOperationProbeResponse{}, fmt.Errorf("begin canonical operation probe persist: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
		return canonicalOperationProbeResponse{}, fmt.Errorf("configure canonical operation probe persist: %w", err)
	}
	rootCurrent, err := lockCanonicalOperationProbeRootCurrent(ctx, tx, snapshot.CustomerOrgID)
	if err != nil {
		return canonicalOperationProbeResponse{}, err
	}
	currentActor, actorPresent, actorActive, err := lockCanonicalOperationProbeActorRow(ctx, tx, actorID)
	if err != nil {
		return canonicalOperationProbeResponse{}, err
	}
	currentSnapshot, currentRows, err := lockCurrentCanonicalOperationProbeRows(
		ctx, tx, snapshot.CustomerOrgID, snapshot.DeploymentID, snapshot.OperationID,
		snapshot.CredentialBindingID,
	)
	if err != nil {
		return canonicalOperationProbeResponse{}, err
	}
	actorCurrent := actorPresent && actorActive &&
		canonicalOperationProbeActorAllowed(currentActor, snapshot.CustomerOrgID) &&
		canonicalOperationProbeActorMatches(snapshot.Actor, currentActor)
	routeInputsCurrent := currentRows && canonicalOperationProbeRouteInputsMatch(snapshot, currentSnapshot)
	operationRevisionCurrent := currentRows && snapshot.OperationRevision == currentSnapshot.OperationRevision
	current := rootCurrent && actorCurrent && routeInputsCurrent && operationRevisionCurrent
	// Chat/Embeddings retain a truthful successful provider observation when
	// only the operation evidence revision advanced concurrently; the pinned
	// observation is stale and cannot route. Responses/Messages are stricter:
	// their migration-owned approval contract only accepts a successful probe
	// that was current at append time.
	evidenceUsable := outcome.ProviderSuccess && rootCurrent && actorCurrent && routeInputsCurrent
	if spec.ExecutionBinding != nil && !operationRevisionCurrent {
		evidenceUsable = false
	}
	reasonCode := outcome.ReasonCode
	if !current {
		if rootCurrent && actorCurrent && routeInputsCurrent && !operationRevisionCurrent {
			reasonCode = "operation_revision_changed_during_probe"
		} else {
			reasonCode = "configuration_changed_during_probe"
		}
	}
	requestSummary, err := canonicalOperationProbeRequestSummary(snapshot)
	if err != nil {
		return canonicalOperationProbeResponse{}, err
	}
	responseMetadata := map[string]interface{}{
		"provider_success": outcome.ProviderSuccess, "current": current,
		"reason_code": reasonCode, "provider_reason_code": outcome.ReasonCode,
		"response_bytes": outcome.ResponseBytes,
	}
	if spec.ExecutionBinding != nil {
		responseMetadata["output_count"] = outcome.OutputCount
		responseMetadata["units"] = outcome.Units
	}
	responseSummary, _ := json.Marshal(responseMetadata)
	fingerprint := canonicalOperationProbeFingerprint(snapshot, outcome, sourceRef)
	errorSummary := ""
	if !evidenceUsable {
		errorSummary = reasonCode
	}
	var probeID int64
	var testedAt time.Time
	var operationRevision, publicPath, executorContractVersion, httpsOrigin interface{}
	if spec.ExecutionBinding != nil {
		origin, originErr := modelqualification.NormalizeHTTPSOrigin(snapshot.BaseURL)
		if originErr != nil {
			return canonicalOperationProbeResponse{}, errCanonicalOperationProbeConflict
		}
		operationRevision = snapshot.OperationRevision
		publicPath = spec.ExecutionBinding.PublicPath
		executorContractVersion = spec.ExecutionBinding.ExecutorContractVersion
		httpsOrigin = origin
	}
	if err := tx.QueryRow(ctx, `
INSERT INTO model_deployment_probes (
  deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
  upstream_model,reported_model,connection_revision,deployment_revision,
  operation_revision,public_path,executor_contract_version,https_origin,
  credential_binding_id,credential_state_revision,success,status_code,latency_ms,
  error_summary,capabilities_verified,request_summary,response_summary,
  source_kind,source_ref,source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,
        '[]'::jsonb,$20::jsonb,$21::jsonb,$22,$23,$24,$25,NOW())
RETURNING probe_id,tested_at`,
		snapshot.DeploymentID, snapshot.ConnectionID, snapshot.OperationID,
		snapshot.ProfileID, snapshot.ProfileVersion, snapshot.UpstreamModel,
		outcome.ReportedModel, snapshot.ConnectionRevision, snapshot.DeploymentRevision,
		operationRevision, publicPath, executorContractVersion, httpsOrigin,
		snapshot.CredentialBindingID, snapshot.CredentialRevision, evidenceUsable,
		outcome.StatusCode, outcome.LatencyMS, errorSummary,
		string(requestSummary), string(responseSummary), spec.SourceKind,
		sourceRef, fingerprint, nullableCanonicalOperationProbeActor(actorID, actorPresent),
	).Scan(&probeID, &testedAt); err != nil {
		return canonicalOperationProbeResponse{}, fmt.Errorf("append canonical operation probe evidence: %w", err)
	}
	evidenceRef := "probe:" + strconv.FormatInt(probeID, 10)
	advanced := false
	advanceOperation := evidenceUsable && current && (currentSnapshot.OperationEvidenceState == string(modeldeployment.EvidenceDeclared) ||
		currentSnapshot.OperationEvidenceState == string(modeldeployment.EvidenceProviderDocumented))
	if evidenceUsable && current && spec.ExecutionBinding != nil {
		advanceOperation = true
		if currentSnapshot.OperationEvidenceState == string(modeldeployment.EvidenceProductionApproved) {
			var qualified bool
			if err := tx.QueryRow(ctx, `
SELECT qualified FROM model_operation_execution_qualifications
 WHERE deployment_id=$1 AND operation_id=$2`, snapshot.DeploymentID, snapshot.OperationID).Scan(&qualified); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return canonicalOperationProbeResponse{}, fmt.Errorf("read current production qualification: %w", err)
			} else if err == nil && qualified {
				advanceOperation = false
			}
		}
	}
	if advanceOperation {
		tag, err := tx.Exec(ctx, `
UPDATE model_deployment_operations
   SET evidence_state='live_probed',evidence_ref=$4,verified_at=$5,
       revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2 AND revision=$3
   AND support_state='enabled'
	   AND ($6 OR evidence_state IN ('declared','provider_documented'))`,
			snapshot.DeploymentID, snapshot.OperationID, currentSnapshot.OperationRevision,
			evidenceRef, testedAt, spec.ExecutionBinding != nil)
		if err != nil {
			return canonicalOperationProbeResponse{}, fmt.Errorf("advance canonical operation probe evidence: %w", err)
		}
		advanced = tag.RowsAffected() == 1
		if !advanced {
			return canonicalOperationProbeResponse{}, fmt.Errorf("advance canonical operation probe evidence: %w", errCanonicalOperationProbeConflict)
		}
	}
	result := canonicalOperationProbeResponse{
		ProbeID: probeID, DeploymentID: snapshot.DeploymentID, OperationID: snapshot.OperationID,
		CredentialBindingID: snapshot.CredentialBindingID, Success: evidenceUsable,
		ProviderSuccess: outcome.ProviderSuccess, Current: current, Stale: !current,
		OperationAdvanced: advanced, ReasonCode: reasonCode, ProviderReasonCode: outcome.ReasonCode,
		StatusCode: outcome.StatusCode,
		LatencyMS:  outcome.LatencyMS, ReportedModel: outcome.ReportedModel,
	}
	if evidenceUsable {
		result.EvidenceRef = evidenceRef
	}
	if err := insertAuditEntry(ctx, tx, "model_deployment.operation_probe", "model_deployment", strconv.FormatInt(snapshot.DeploymentID, 10), map[string]interface{}{
		"after": map[string]interface{}{
			"probe_id": probeID, "deployment_id": snapshot.DeploymentID,
			"connection_id": snapshot.ConnectionID, "operation_id": snapshot.OperationID,
			"credential_binding_id":     snapshot.CredentialBindingID,
			"connection_revision":       snapshot.ConnectionRevision,
			"deployment_revision":       snapshot.DeploymentRevision,
			"credential_state_revision": snapshot.CredentialRevision,
			"provider_success":          outcome.ProviderSuccess, "success": evidenceUsable,
			"current": current, "stale": !current, "reason_code": reasonCode,
			"provider_reason_code": outcome.ReasonCode,
			"status_code":          outcome.StatusCode, "latency_ms": outcome.LatencyMS,
			"operation_advanced": advanced,
		},
	}, audit); err != nil {
		return canonicalOperationProbeResponse{}, fmt.Errorf("audit canonical operation probe: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return canonicalOperationProbeResponse{}, fmt.Errorf("commit canonical operation probe: %w", err)
	}
	if evidenceUsable && h.routingController != nil {
		h.routingController.Invalidate()
	}
	return result, nil
}

func lockCanonicalOperationProbeRootCurrent(ctx context.Context, tx pgx.Tx, orgID int64) (bool, error) {
	if _, err := tx.Exec(ctx, `LOCK TABLE orgs IN SHARE MODE`); err != nil {
		return false, fmt.Errorf("lock current canonical operation probe customer rows: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT id FROM orgs
 WHERE status='active' AND org_type='customer' AND parent_id IS NULL
 ORDER BY id FOR SHARE`)
	if err != nil {
		return false, fmt.Errorf("lock canonical operation probe customer authority: %w", err)
	}
	defer rows.Close()
	ids := make([]int64, 0, 2)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return false, fmt.Errorf("scan canonical operation probe customer authority: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read canonical operation probe customer authority: %w", err)
	}
	return len(ids) == 1 && ids[0] == orgID, nil
}

func lockCanonicalOperationProbeActorRow(
	ctx context.Context,
	tx pgx.Tx,
	actorID int64,
) (canonicalOperationProbeActorSnapshot, bool, bool, error) {
	var actor canonicalOperationProbeActorSnapshot
	var userStatus, orgStatus string
	err := tx.QueryRow(ctx, `
SELECT u.id,u.org_id,u.organization_role,u.role,u.auth_epoch,u.status,o.status
  FROM users u
  JOIN orgs o ON o.id=u.org_id
 WHERE u.id=$1
 FOR SHARE OF u,o`, actorID).Scan(
		&actor.UserID, &actor.OrgID, &actor.OrganizationRole, &actor.CompatibilityRole,
		&actor.AuthEpoch, &userStatus, &orgStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return canonicalOperationProbeActorSnapshot{}, false, false, nil
	}
	if err != nil {
		return canonicalOperationProbeActorSnapshot{}, false, false, fmt.Errorf("lock canonical operation probe actor row: %w", err)
	}
	return actor, true, userStatus == "active" && orgStatus == "active", nil
}

func canonicalOperationProbeActorAllowed(actor canonicalOperationProbeActorSnapshot, targetOrgID int64) bool {
	switch actor.OrganizationRole {
	case auth.RolePlatformAdmin:
		return true
	case auth.RoleOrgAdmin:
		return actor.OrgID == targetOrgID
	default:
		return false
	}
}

func canonicalOperationProbeActorMatches(before, current canonicalOperationProbeActorSnapshot) bool {
	return before.UserID == current.UserID && before.OrgID == current.OrgID &&
		before.OrganizationRole == current.OrganizationRole &&
		before.CompatibilityRole == current.CompatibilityRole &&
		before.AuthEpoch == current.AuthEpoch
}

func nullableCanonicalOperationProbeActor(actorID int64, present bool) interface{} {
	if !present || actorID <= 0 {
		return nil
	}
	return actorID
}

func lockCurrentCanonicalOperationProbeRows(
	ctx context.Context,
	tx pgx.Tx,
	orgID, deploymentID int64,
	operationID string,
	bindingID int64,
) (canonicalOperationProbeSnapshot, bool, error) {
	current, err := lockCanonicalOperationProbeRows(ctx, tx, orgID, deploymentID, operationID, bindingID, true)
	if errors.Is(err, errCanonicalOperationProbeNotFound) {
		return canonicalOperationProbeSnapshot{}, false, nil
	}
	if err != nil {
		return canonicalOperationProbeSnapshot{}, false, err
	}
	return current, true, nil
}

func canonicalOperationProbeRouteInputsMatch(before, current canonicalOperationProbeSnapshot) bool {
	if validateCanonicalOperationProbeSnapshot(current) != nil {
		return false
	}
	return before.CustomerOrgID == current.CustomerOrgID &&
		before.DeploymentID == current.DeploymentID &&
		before.ConnectionID == current.ConnectionID &&
		before.CredentialBindingID == current.CredentialBindingID &&
		before.ConnectionRevision == current.ConnectionRevision &&
		before.DeploymentRevision == current.DeploymentRevision &&
		before.CredentialRevision == current.CredentialRevision &&
		before.OperationID == current.OperationID &&
		before.UpstreamModel == current.UpstreamModel &&
		before.ProfileDefinitionDigest == current.ProfileDefinitionDigest &&
		before.ExecutionProfileID == current.ExecutionProfileID &&
		before.ExecutionProfileVersion == current.ExecutionProfileVersion &&
		before.ProfileID == current.ProfileID && before.ProfileVersion == current.ProfileVersion &&
		before.LinkCreatedAt.Equal(current.LinkCreatedAt)
}

func canonicalOperationProbeFingerprint(
	snapshot canonicalOperationProbeSnapshot,
	outcome canonicalOperationProbeOutcome,
	sourceRef string,
) string {
	payload := strings.Join([]string{
		canonicalOperationProbeFingerprintSourceKind(snapshot.OperationID), sourceRef,
		strconv.FormatInt(snapshot.DeploymentID, 10), snapshot.OperationID,
		strconv.FormatInt(snapshot.ConnectionID, 10), strconv.FormatInt(snapshot.ConnectionRevision, 10),
		strconv.FormatInt(snapshot.DeploymentRevision, 10),
		strconv.FormatInt(snapshot.CredentialBindingID, 10), strconv.FormatInt(snapshot.CredentialRevision, 10),
		snapshot.ProfileID, snapshot.ProfileVersion, snapshot.UpstreamModel,
		strconv.FormatInt(snapshot.OperationRevision, 10), canonicalOperationProbeFingerprintBinding(snapshot),
		strconv.FormatBool(outcome.ProviderSuccess), strconv.Itoa(outcome.StatusCode), outcome.ReasonCode,
	}, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func canonicalOperationProbeFingerprintSourceKind(operationID string) string {
	spec, err := canonicalOperationProbeSpecFor(modeloperation.ID(operationID))
	if err != nil {
		return canonicalOperationProbeSourceKind
	}
	return spec.SourceKind
}

func canonicalOperationProbeFingerprintBinding(snapshot canonicalOperationProbeSnapshot) string {
	spec, err := canonicalOperationProbeSpecForSnapshot(snapshot)
	if err != nil || spec.ExecutionBinding == nil {
		return ""
	}
	origin, _ := modelqualification.NormalizeHTTPSOrigin(snapshot.BaseURL)
	return strings.Join([]string{
		spec.ExecutionBinding.PublicPath,
		spec.ExecutionBinding.ExecutorContractVersion,
		origin,
	}, "|")
}
