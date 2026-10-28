package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeldeployment"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const modelOperationProductionApprovalMaxRequestBytes = 4 << 10

var (
	errModelOperationProductionApprovalInvalid  = errors.New("model operation production approval request is invalid")
	errModelOperationProductionApprovalNotFound = errors.New("model operation production approval proof not found")
	errModelOperationProductionApprovalConflict = errors.New("model operation production approval proof is stale or mismatched")
)

type modelOperationProductionApprovalRequest struct {
	ExpectedUpstreamObservationIdentity string `json:"expected_upstream_observation_identity,omitempty"`
	ProbeID                             int64  `json:"probe_id"`
	ExpectedConnectionRevision          int64  `json:"expected_connection_revision"`
	ExpectedDeploymentRevision          int64  `json:"expected_deployment_revision"`
	ExpectedOperationRevision           int64  `json:"expected_operation_revision"`
	ExpectedExecutorContractVersion     string `json:"expected_executor_contract_version"`
	Reason                              string `json:"reason"`
}

type modelOperationProductionApprovalResponse struct {
	UpstreamScope             upstreamrelease.Applicability `json:"upstream_scope"`
	ApprovalID                int64                         `json:"approval_id"`
	ProbeID                   int64                         `json:"probe_id"`
	CustomerOrgID             int64                         `json:"customer_org_id"`
	DeploymentID              int64                         `json:"deployment_id"`
	OperationID               string                        `json:"operation_id"`
	PublicPath                string                        `json:"public_path"`
	ProtocolProfileID         string                        `json:"protocol_profile_id"`
	ProtocolProfileVersion    string                        `json:"protocol_profile_version"`
	ExecutorContractVersion   string                        `json:"executor_contract_version"`
	HTTPSOrigin               string                        `json:"https_origin"`
	SourceConnectionRevision  int64                         `json:"source_connection_revision"`
	SourceDeploymentRevision  int64                         `json:"source_deployment_revision"`
	ProbeOperationRevision    int64                         `json:"probe_operation_revision"`
	SourceOperationRevision   int64                         `json:"source_operation_revision"`
	ApprovedOperationRevision int64                         `json:"approved_operation_revision"`
	ApprovedBy                int64                         `json:"approved_by"`
	ApprovedByOrgID           int64                         `json:"-"`
	ApprovedAt                time.Time                     `json:"approved_at"`
	Reason                    string                        `json:"reason"`
	Replayed                  bool                          `json:"replayed"`
	Qualified                 bool                          `json:"qualified"`
	QualificationState        string                        `json:"qualification_state"`
	CurrentConnectionRevision int64                         `json:"current_connection_revision"`
	CurrentDeploymentRevision int64                         `json:"current_deployment_revision"`
	CurrentOperationRevision  int64                         `json:"current_operation_revision"`
}

type modelOperationProductionApprovalProof struct {
	CustomerOrgID           int64
	DeploymentID            int64
	ConnectionID            int64
	OperationID             string
	PublicPath              string
	ProtocolProfileID       string
	ProtocolProfileVersion  string
	ExecutorContractVersion string
	HTTPSOrigin             string
	ConnectionRevision      int64
	DeploymentRevision      int64
	ProbeOperationRevision  int64
	SourceOperationRevision int64
	CredentialBindingID     int64
	CredentialStateRevision int64
	CredentialLinkCreatedAt time.Time
}

func (h *Handler) ApproveCanonicalModelOperationProduction(w http.ResponseWriter, r *http.Request) {
	deploymentID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("deploymentID")), 10, 64)
	if err != nil || deploymentID <= 0 {
		writeModelOperationProductionApprovalError(w, errModelOperationProductionApprovalInvalid)
		return
	}
	rawOperationID := r.PathValue("operationID")
	operationID, err := modeldeployment.NormalizeOperationID(rawOperationID)
	if err != nil || operationID != rawOperationID || !modelqualification.IsQualificationRequired(operationID) {
		writeModelOperationProductionApprovalError(w, errModelOperationProductionApprovalInvalid)
		return
	}
	request, err := decodeModelOperationProductionApprovalRequest(w, r)
	if err != nil {
		writeModelOperationProductionApprovalError(w, err)
		return
	}
	actorID := middleware.GetUserIDFromContext(r.Context())
	actorOrgID := middleware.GetOrgIDFromContext(r.Context())
	targetOrgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil || actorID <= 0 || actorOrgID <= 0 || targetOrgID <= 0 {
		writeModelOperationProductionApprovalError(w, errModelOperationProductionApprovalInvalid)
		return
	}
	response, created, err := h.approveCanonicalModelOperationProduction(
		r.Context(), actorID, actorOrgID, targetOrgID, deploymentID, operationID, request,
		requestAuditMeta(r, targetOrgID, 0),
	)
	if err != nil {
		writeModelOperationProductionApprovalError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, response)
}

func decodeModelOperationProductionApprovalRequest(w http.ResponseWriter, r *http.Request) (modelOperationProductionApprovalRequest, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, modelOperationProductionApprovalMaxRequestBytes))
	if err != nil || strictjson.Validate(body) != nil {
		return modelOperationProductionApprovalRequest{}, errModelOperationProductionApprovalInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request modelOperationProductionApprovalRequest
	if err := decoder.Decode(&request); err != nil {
		return modelOperationProductionApprovalRequest{}, errModelOperationProductionApprovalInvalid
	}
	request.ExpectedExecutorContractVersion = strings.TrimSpace(request.ExpectedExecutorContractVersion)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.ProbeID <= 0 || request.ExpectedConnectionRevision <= 0 || request.ExpectedDeploymentRevision <= 0 ||
		request.ExpectedOperationRevision <= 0 || request.ExpectedExecutorContractVersion == "" ||
		request.ExpectedExecutorContractVersion != modelqualification.ExecutorContractVersion ||
		request.Reason == "" || len(request.Reason) > 512 || !utf8.ValidString(request.Reason) ||
		strings.ContainsAny(request.Reason, "\x00\r\n") {
		return modelOperationProductionApprovalRequest{}, errModelOperationProductionApprovalInvalid
	}
	return request, nil
}

func writeModelOperationProductionApprovalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errModelOperationProductionApprovalInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid production approval request", "code": "model_operation_production_approval_invalid",
		})
	case errors.Is(err, errModelOperationProductionApprovalNotFound), errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": "Production approval proof not found", "code": "model_operation_production_approval_not_found",
		})
	case errors.Is(err, errModelOperationProductionApprovalConflict):
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Production approval proof is stale or mismatched", "code": "model_operation_production_approval_conflict",
		})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": "Production approval unavailable", "code": "model_operation_production_approval_unavailable",
		})
	}
}

func (h *Handler) approveCanonicalModelOperationProduction(
	ctx context.Context,
	actorID, actorOrgID, targetOrgID, deploymentID int64,
	operationID string,
	request modelOperationProductionApprovalRequest,
	audit auditMeta,
) (modelOperationProductionApprovalResponse, bool, error) {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return modelOperationProductionApprovalResponse{}, false, fmt.Errorf("begin production approval: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(8675310, ($1 % 2147483647)::INTEGER)`, request.ProbeID); err != nil {
		return modelOperationProductionApprovalResponse{}, false, fmt.Errorf("lock production approval receipt: %w", err)
	}
	if existing, found, err := loadModelOperationProductionApproval(ctx, tx, request.ProbeID); err != nil {
		return modelOperationProductionApprovalResponse{}, false, err
	} else if found {
		if !modelOperationProductionApprovalReplayMatches(existing, actorID, actorOrgID, targetOrgID, deploymentID, operationID, request) {
			return modelOperationProductionApprovalResponse{}, false, errModelOperationProductionApprovalConflict
		}
		captured, scopeErr := upstreamrelease.Probe(ctx, tx, false, targetOrgID, deploymentID, request.ProbeID)
		if scopeErr != nil {
			return modelOperationProductionApprovalResponse{}, false, scopeErr
		}
		if upstreamrelease.Identity(captured) != request.ExpectedUpstreamObservationIdentity {
			return modelOperationProductionApprovalResponse{}, false, errModelOperationProductionApprovalConflict
		}
		current, liveScope, scopeErr := upstreamrelease.Read(ctx, tx, targetOrgID, deploymentID, false)
		if scopeErr != nil {
			return modelOperationProductionApprovalResponse{}, false, scopeErr
		}
		existing.UpstreamScope = upstreamrelease.AssessInScope(liveScope, current.Observation, captured)
		existing.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return modelOperationProductionApprovalResponse{}, false, fmt.Errorf("commit production approval replay: %w", err)
		}
		return existing, false, nil
	}
	// Root-customer currentness is an append-time authority. Exact replay is a
	// readback of the immutable historical receipt and reports the qualification
	// view's current state; it must not be recast as a new approval attempt.
	rootCurrent, err := lockCanonicalOperationProbeRootCurrent(ctx, tx, targetOrgID)
	if err != nil {
		return modelOperationProductionApprovalResponse{}, false, fmt.Errorf("lock production approval customer authority: %w", err)
	}
	if !rootCurrent {
		return modelOperationProductionApprovalResponse{}, false, errModelOperationProductionApprovalConflict
	}

	proof, err := lockModelOperationProductionApprovalProof(ctx, tx, request.ProbeID, deploymentID, operationID)
	if err != nil {
		return modelOperationProductionApprovalResponse{}, false, err
	}
	if proof.CustomerOrgID != targetOrgID ||
		proof.ConnectionRevision != request.ExpectedConnectionRevision ||
		proof.DeploymentRevision != request.ExpectedDeploymentRevision ||
		proof.SourceOperationRevision != request.ExpectedOperationRevision ||
		proof.ExecutorContractVersion != request.ExpectedExecutorContractVersion {
		return modelOperationProductionApprovalResponse{}, false, errModelOperationProductionApprovalConflict
	}
	if err := modelqualification.ValidateCompiledBinding(modelqualification.Binding{
		OperationID: proof.OperationID, PublicPath: proof.PublicPath,
		ProtocolProfileID: proof.ProtocolProfileID, ProtocolProfileVersion: proof.ProtocolProfileVersion,
		ExecutorContractVersion: proof.ExecutorContractVersion,
	}); err != nil {
		return modelOperationProductionApprovalResponse{}, false, errModelOperationProductionApprovalConflict
	}

	currentObservation, currentObservationScope, err := upstreamrelease.Read(ctx, tx, targetOrgID, deploymentID, true)
	if err != nil {
		return modelOperationProductionApprovalResponse{}, false, err
	}
	if currentObservation.Observation.Validate(currentObservationScope) != nil {
		return modelOperationProductionApprovalResponse{}, false, errModelOperationProductionApprovalConflict
	}
	capturedObservation, err := upstreamrelease.Probe(ctx, tx, false, targetOrgID, deploymentID, request.ProbeID)
	if err != nil {
		return modelOperationProductionApprovalResponse{}, false, err
	}
	if err := upstreamrelease.Require(currentObservation.Observation, capturedObservation, request.ExpectedUpstreamObservationIdentity); err != nil {
		return modelOperationProductionApprovalResponse{}, false, errModelOperationProductionApprovalConflict
	}
	var approvalID int64
	err = tx.QueryRow(ctx, `
INSERT INTO model_operation_production_approvals (
  customer_org_id,deployment_id,connection_id,operation_id,probe_id,
  credential_binding_id,credential_state_revision,credential_link_created_at,public_path,
  protocol_profile_id,protocol_profile_version,executor_contract_version,https_origin,
  connection_revision,deployment_revision,probe_operation_revision,
  source_operation_revision,approved_operation_revision,approved_by,approved_by_org_id,
  reason,source_kind,source_ref,approved_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17::BIGINT+1,$18,$19,$20,$21,$22,NOW())
RETURNING approval_id`,
		proof.CustomerOrgID, proof.DeploymentID, proof.ConnectionID, proof.OperationID, request.ProbeID,
		proof.CredentialBindingID, proof.CredentialStateRevision, proof.CredentialLinkCreatedAt, proof.PublicPath,
		proof.ProtocolProfileID, proof.ProtocolProfileVersion, proof.ExecutorContractVersion, proof.HTTPSOrigin,
		proof.ConnectionRevision, proof.DeploymentRevision, proof.ProbeOperationRevision,
		proof.SourceOperationRevision, actorID, actorOrgID, request.Reason,
		modelqualification.ApprovalSourceKind, "probe:"+strconv.FormatInt(request.ProbeID, 10),
	).Scan(&approvalID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.Code == "55000" || pgErr.Code == "23514" || pgErr.Code == "40001" || pgErr.Code == "42501" || pgErr.Code == "23505") {
			return modelOperationProductionApprovalResponse{}, false, errModelOperationProductionApprovalConflict
		}
		return modelOperationProductionApprovalResponse{}, false, fmt.Errorf("append production approval: %w", err)
	}
	response, found, err := loadModelOperationProductionApproval(ctx, tx, request.ProbeID)
	if err != nil || !found || response.ApprovalID != approvalID {
		if err == nil {
			err = errModelOperationProductionApprovalConflict
		}
		return modelOperationProductionApprovalResponse{}, false, err
	}
	response.UpstreamScope = upstreamrelease.Assess(currentObservation.Observation, capturedObservation)
	if err := insertAuditEntry(ctx, tx, "model_deployment.operation_production_approve", "model_deployment", strconv.FormatInt(deploymentID, 10), map[string]interface{}{
		"after": map[string]interface{}{
			"approval_id": approvalID, "probe_id": request.ProbeID,
			"customer_org_id": proof.CustomerOrgID, "deployment_id": deploymentID, "operation_id": operationID,
			"connection_revision":         proof.ConnectionRevision,
			"deployment_revision":         proof.DeploymentRevision,
			"credential_binding_id":       proof.CredentialBindingID,
			"credential_state_revision":   proof.CredentialStateRevision,
			"credential_link_created_at":  proof.CredentialLinkCreatedAt,
			"source_operation_revision":   proof.SourceOperationRevision,
			"approved_operation_revision": proof.SourceOperationRevision + 1,
			"public_path":                 proof.PublicPath,
			"protocol_profile_id":         proof.ProtocolProfileID,
			"protocol_profile_version":    proof.ProtocolProfileVersion,
			"executor_contract_version":   proof.ExecutorContractVersion,
			"qualified":                   response.Qualified,
		},
	}, audit); err != nil {
		return modelOperationProductionApprovalResponse{}, false, fmt.Errorf("audit production approval: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return modelOperationProductionApprovalResponse{}, false, fmt.Errorf("commit production approval: %w", err)
	}
	if h.routingController != nil {
		h.routingController.Invalidate()
	}
	return response, true, nil
}

func lockModelOperationProductionApprovalProof(
	ctx context.Context,
	tx pgx.Tx,
	probeID, deploymentID int64,
	operationID string,
) (modelOperationProductionApprovalProof, error) {
	var proof modelOperationProductionApprovalProof
	var success bool
	var probeSourceKind, supportState, evidenceState, evidenceRef string
	var currentConnectionRevision, currentDeploymentRevision int64
	var credentialStatus, encryptedSecret string
	var credentialCurrentRevision int64
	var credentialCooldownEligible, credentialLinkEligible bool
	err := tx.QueryRow(ctx, `
SELECT deployment.customer_org_id,probe.deployment_id,probe.connection_id,probe.operation_id,
       support.public_path,operation.protocol_profile_id,operation.protocol_profile_version,
       support.executor_contract_version,probe.https_origin,
       probe.connection_revision,probe.deployment_revision,probe.operation_revision,
       probe.credential_binding_id,probe.credential_state_revision,credential_link.created_at,
       operation.revision,probe.success,probe.source_kind,
       operation.support_state,operation.evidence_state,operation.evidence_ref,
       connection.config_revision,deployment.config_revision,
       binding.status,binding.key_enc,binding.state_revision,
       (binding.cooldown_until IS NULL OR binding.cooldown_until<=transaction_timestamp()),
       credential_link.created_at<=probe.tested_at
  FROM model_deployment_probes probe
  JOIN model_deployment_operations operation
    ON operation.deployment_id=probe.deployment_id AND operation.operation_id=probe.operation_id
  JOIN model_deployments deployment ON deployment.id=probe.deployment_id
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN provider_credential_bindings binding
    ON binding.id=probe.credential_binding_id AND binding.connection_id=probe.connection_id
  JOIN credential_binding_deployments credential_link
    ON credential_link.binding_id=probe.credential_binding_id
   AND credential_link.deployment_id=probe.deployment_id
   AND credential_link.connection_id=probe.connection_id
  JOIN model_operation_exact_execution_support_bindings support
    ON support.operation_id=operation.operation_id
   AND support.protocol_profile_id=operation.protocol_profile_id
   AND support.protocol_profile_version=operation.protocol_profile_version
 WHERE probe.probe_id=$1 AND probe.deployment_id=$2 AND probe.operation_id=$3
 FOR UPDATE OF probe,operation,deployment,connection,binding,credential_link`, probeID, deploymentID, operationID).Scan(
		&proof.CustomerOrgID, &proof.DeploymentID, &proof.ConnectionID, &proof.OperationID,
		&proof.PublicPath, &proof.ProtocolProfileID, &proof.ProtocolProfileVersion,
		&proof.ExecutorContractVersion, &proof.HTTPSOrigin,
		&proof.ConnectionRevision, &proof.DeploymentRevision, &proof.ProbeOperationRevision,
		&proof.CredentialBindingID, &proof.CredentialStateRevision, &proof.CredentialLinkCreatedAt,
		&proof.SourceOperationRevision, &success, &probeSourceKind,
		&supportState, &evidenceState, &evidenceRef,
		&currentConnectionRevision, &currentDeploymentRevision,
		&credentialStatus, &encryptedSecret, &credentialCurrentRevision,
		&credentialCooldownEligible, &credentialLinkEligible,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return modelOperationProductionApprovalProof{}, errModelOperationProductionApprovalNotFound
	}
	if err != nil {
		return modelOperationProductionApprovalProof{}, fmt.Errorf("lock production approval proof: %w", err)
	}
	if !success || probeSourceKind != modelqualification.LiveProbeSourceKind || supportState != "enabled" ||
		evidenceState != "live_probed" || evidenceRef != "probe:"+strconv.FormatInt(probeID, 10) ||
		proof.ProbeOperationRevision+1 != proof.SourceOperationRevision ||
		proof.ConnectionRevision != currentConnectionRevision || proof.DeploymentRevision != currentDeploymentRevision ||
		proof.CredentialBindingID <= 0 || proof.CredentialStateRevision <= 0 ||
		credentialStatus != "active" || encryptedSecret == "" ||
		credentialCurrentRevision != proof.CredentialStateRevision ||
		!credentialCooldownEligible || !credentialLinkEligible || proof.CredentialLinkCreatedAt.IsZero() ||
		proof.HTTPSOrigin == "" {
		return modelOperationProductionApprovalProof{}, errModelOperationProductionApprovalConflict
	}
	return proof, nil
}

func loadModelOperationProductionApproval(
	ctx context.Context,
	tx pgx.Tx,
	probeID int64,
) (modelOperationProductionApprovalResponse, bool, error) {
	var response modelOperationProductionApprovalResponse
	err := tx.QueryRow(ctx, `
SELECT approval.approval_id,approval.probe_id,approval.customer_org_id,
       approval.deployment_id,approval.operation_id,
       approval.public_path,approval.protocol_profile_id,approval.protocol_profile_version,
       approval.executor_contract_version,approval.https_origin,
       approval.connection_revision,approval.deployment_revision,
       approval.probe_operation_revision,approval.source_operation_revision,
       approval.approved_operation_revision,approval.approved_by,approval.approved_by_org_id,
       approval.approved_at,approval.reason,
       qualification.qualified,qualification.qualification_state,
       qualification.current_connection_revision,qualification.current_deployment_revision,
       qualification.current_operation_revision
  FROM model_operation_production_approvals approval
  JOIN model_operation_execution_qualifications qualification
    ON qualification.deployment_id=approval.deployment_id
   AND qualification.operation_id=approval.operation_id
 WHERE approval.probe_id=$1`, probeID).Scan(
		&response.ApprovalID, &response.ProbeID, &response.CustomerOrgID,
		&response.DeploymentID, &response.OperationID,
		&response.PublicPath, &response.ProtocolProfileID, &response.ProtocolProfileVersion,
		&response.ExecutorContractVersion, &response.HTTPSOrigin,
		&response.SourceConnectionRevision, &response.SourceDeploymentRevision,
		&response.ProbeOperationRevision, &response.SourceOperationRevision,
		&response.ApprovedOperationRevision, &response.ApprovedBy, &response.ApprovedByOrgID,
		&response.ApprovedAt, &response.Reason,
		&response.Qualified, &response.QualificationState,
		&response.CurrentConnectionRevision, &response.CurrentDeploymentRevision,
		&response.CurrentOperationRevision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return modelOperationProductionApprovalResponse{}, false, nil
	}
	if err != nil {
		return modelOperationProductionApprovalResponse{}, false, fmt.Errorf("read production approval receipt: %w", err)
	}
	return response, true, nil
}

func modelOperationProductionApprovalReplayMatches(
	existing modelOperationProductionApprovalResponse,
	actorID, actorOrgID, targetOrgID, deploymentID int64,
	operationID string,
	request modelOperationProductionApprovalRequest,
) bool {
	return existing.ProbeID == request.ProbeID && existing.DeploymentID == deploymentID &&
		existing.OperationID == operationID && existing.CustomerOrgID == targetOrgID &&
		existing.ApprovedBy == actorID && existing.ApprovedByOrgID == actorOrgID &&
		existing.SourceConnectionRevision == request.ExpectedConnectionRevision &&
		existing.SourceDeploymentRevision == request.ExpectedDeploymentRevision &&
		existing.SourceOperationRevision == request.ExpectedOperationRevision &&
		existing.ExecutorContractVersion == request.ExpectedExecutorContractVersion &&
		existing.Reason == request.Reason
}
