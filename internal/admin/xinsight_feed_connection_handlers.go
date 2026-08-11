package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

const (
	xInsightFeedConnectionActive = "active"
	xInsightFeedConnectionPaused = "paused"
	xInsightFeedKeyPurpose       = middleware.APIKeyPurposeXInsightFeedRead
)

var (
	errXInsightFeedConflict      = errors.New("x-insight feed control state conflicts with the requested operation")
	errXInsightFeedNotFound      = errors.New("x-insight feed project or credential was not found")
	errXInsightFeedInvalidScope  = errors.New("x-insight feed network binding must be one exact IPv4 /32 or IPv6 /128")
	errXInsightFeedRegistration  = errors.New("invalid receiver-generated feed credential registration")
	xInsightFeedPrefixPattern    = regexp.MustCompile(`^ur-xf1-[0-9a-f]{13}$`)
	xInsightFeedOperationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type xInsightConnectorInvitationIntake interface {
	Accept(context.Context, xinsightconnector.InvitationIntakeInput) (xinsightconnector.InvitationIntakeResult, error)
}

type xInsightFeedCredential struct {
	ID             int64      `json:"id"`
	KeyPrefix      string     `json:"key_prefix"`
	Lifecycle      string     `json:"lifecycle"`
	VerifierScheme string     `json:"verifier_scheme"`
	OperationID    string     `json:"operation_id"`
	CreatedAt      time.Time  `json:"created_at"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	Status         string     `json:"-"`
}

type xInsightConnectorInstallationSummary struct {
	SchemaVersion          string     `json:"schema_version"`
	InstallationID         string     `json:"installation_id"`
	XDeploymentID          string     `json:"x_deployment_id"`
	EventScope             string     `json:"event_scope"`
	SourceInstance         string     `json:"source_instance"`
	SourceBindingSHA256    string     `json:"source_binding_sha256"`
	DesiredState           string     `json:"desired_state"`
	ObservedState          string     `json:"observed_state"`
	StateRevision          int64      `json:"state_revision"`
	ControlFence           int64      `json:"control_fence"`
	BootstrapReceiptSHA256 string     `json:"bootstrap_receipt_sha256,omitempty"`
	LastSuccessfulSyncAt   *time.Time `json:"last_successful_sync_at,omitempty"`
	LastErrorCode          string     `json:"last_error_code,omitempty"`
	LastErrorAt            *time.Time `json:"last_error_at,omitempty"`
	ConsentedAt            time.Time  `json:"consented_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type xInsightConnectorOperationSummary struct {
	SchemaVersion     string     `json:"schema_version"`
	OperationRef      string     `json:"operation_ref"`
	InstallationID    string     `json:"installation_id"`
	Action            string     `json:"action"`
	DesiredState      string     `json:"desired_state"`
	State             string     `json:"state"`
	RequestedRevision int64      `json:"requested_revision"`
	ControlFence      int64      `json:"control_fence"`
	AttemptCount      int        `json:"attempt_count"`
	ResultCode        string     `json:"result_code,omitempty"`
	ResultSHA256      string     `json:"result_sha256,omitempty"`
	ErrorCode         string     `json:"error_code,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type xInsightFeedConnectionStatus struct {
	ProjectID                   int64                                 `json:"project_id"`
	ProjectName                 string                                `json:"project_name"`
	ProjectStatus               string                                `json:"project_status"`
	GlobalConfigured            bool                                  `json:"global_configured"`
	GlobalEnabled               bool                                  `json:"global_enabled"`
	SourceInstance              string                                `json:"source_instance"`
	ConnectionStatus            string                                `json:"connection_status"`
	ActiveCredential            *xInsightFeedCredential               `json:"active_credential"`
	StagedCredential            *xInsightFeedCredential               `json:"staged_credential"`
	LastRevokedCredential       *xInsightFeedCredential               `json:"last_revoked_credential"`
	LastAuthenticatedPullAt     *time.Time                            `json:"last_authenticated_pull_at"`
	LastAuthenticatedCredential *xInsightFeedCredential               `json:"last_authenticated_credential"`
	SourceHighWatermark         string                                `json:"source_high_watermark"`
	ReceiverAcknowledgedCursor  string                                `json:"receiver_acknowledged_cursor"`
	ReceiverAcknowledgement     string                                `json:"receiver_acknowledgement_semantics"`
	PendingItemCount            int64                                 `json:"pending_item_count"`
	PendingItemCountCapped      bool                                  `json:"pending_item_count_capped"`
	OldestPendingAt             *time.Time                            `json:"oldest_pending_at,omitempty"`
	OldestPendingAgeSeconds     int64                                 `json:"oldest_pending_age_seconds"`
	CursorOutsideRetention      bool                                  `json:"cursor_outside_retention"`
	ConnectorProductStatus      string                                `json:"connector_product_status"`
	ConnectorInstallation       *xInsightConnectorInstallationSummary `json:"connector_installation,omitempty"`
	ConnectorLatestOperation    *xInsightConnectorOperationSummary    `json:"connector_latest_operation,omitempty"`
}

type createXInsightFeedCredentialRequest struct {
	AllowedIP    string                               `json:"allowed_ip"`
	Registration xInsightFeedCredentialRegistrationV1 `json:"registration"`
}

type prepareXInsightFeedRotationRequest struct {
	Registration xInsightFeedCredentialRegistrationV1 `json:"registration"`
}

type installXInsightConnectorRequest struct {
	Invitation xinsightconnector.FrozenInvitationV1 `json:"invitation"`
}

type installXInsightConnectorResponse struct {
	ConnectorProductStatus string                                `json:"connector_product_status"`
	Installation           *xInsightConnectorInstallationSummary `json:"connector_installation"`
	Operation              *xInsightConnectorOperationSummary    `json:"connector_latest_operation"`
}

type xInsightFeedCredentialRegistrationV1 struct {
	SchemaVersion  string `json:"schema_version"`
	TokenFormat    string `json:"token_format"`
	VerifierScheme string `json:"verifier_scheme"`
	KeyPrefix      string `json:"key_prefix"`
	VerifierSHA256 string `json:"verifier_sha256"`
	OperationID    string `json:"operation_id"`
}

type stagedXInsightFeedCredentialResponse struct {
	ID             int64  `json:"id"`
	KeyPrefix      string `json:"key_prefix"`
	Lifecycle      string `json:"lifecycle"`
	ProjectID      int64  `json:"project_id"`
	Purpose        string `json:"purpose"`
	KeyType        string `json:"key_type"`
	VerifierScheme string `json:"verifier_scheme"`
	OperationID    string `json:"operation_id"`
}

type xInsightFeedCredentialIDRequest struct {
	StagedCredentialID int64 `json:"staged_credential_id"`
}

func (h *Handler) GetXInsightFeedConnection(w http.ResponseWriter, r *http.Request) {
	projectID, err := xInsightFeedProjectID(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project ID"})
		return
	}
	status, err := h.readXInsightFeedConnectionStatus(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, errXInsightFeedNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Project not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to read X-Insight connection"})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) PauseXInsightFeedConnection(w http.ResponseWriter, r *http.Request) {
	h.setXInsightFeedConnectionState(w, r, xInsightFeedConnectionPaused)
}

func (h *Handler) ResumeXInsightFeedConnection(w http.ResponseWriter, r *http.Request) {
	h.setXInsightFeedConnectionState(w, r, xInsightFeedConnectionActive)
}

func (h *Handler) InstallXInsightConnector(w http.ResponseWriter, r *http.Request) {
	var request installXInsightConnectorRequest
	if !decodeXInsightFeedControlJSON(w, r, &request) {
		return
	}
	projectID, err := xInsightFeedProjectID(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project ID"})
		return
	}
	orgID, err := h.xInsightFeedExistingProjectOrg(r.Context(), projectID)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	actorID := middleware.GetUserIDFromContext(r.Context())
	if actorID <= 0 {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Forbidden"})
		return
	}
	if h.xInsightConnectorIntake == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "X-Insight Connector invitation intake is unavailable"})
		return
	}
	result, err := h.xInsightConnectorIntake.Accept(r.Context(), xinsightconnector.InvitationIntakeInput{
		Invitation: request.Invitation, OrgID: orgID, ProjectID: projectID,
		ConsentedByUserID: actorID,
	})
	if err != nil {
		writeXInsightConnectorInvitationError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, installXInsightConnectorResponse{
		ConnectorProductStatus: xinsightconnector.ProductConnecting,
		Installation:           summarizeXInsightConnectorInstallation(result.Installation),
		Operation:              summarizeXInsightConnectorOperation(result.Operation),
	})
}

func (h *Handler) setXInsightFeedConnectionState(w http.ResponseWriter, r *http.Request, target string) {
	projectID, err := xInsightFeedProjectID(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project ID"})
		return
	}
	orgID, err := h.xInsightFeedExistingProjectOrg(r.Context(), projectID)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	if target == xInsightFeedConnectionActive &&
		(h.completedFeedController == nil || !h.completedFeedController.Enabled()) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Completed-execution feed is not globally enabled"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to update X-Insight connection"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizeXInsightFeedControlTx(
		r.Context(), tx, orgID, projectID, apiKeyMutationUpdate,
		target == xInsightFeedConnectionActive,
	); err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	current, err := lockXInsightFeedConnection(r.Context(), tx, orgID, projectID)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	if target == xInsightFeedConnectionActive {
		var activeCount, stagedCount int
		if err := tx.QueryRow(r.Context(),
			`SELECT COUNT(*) FILTER (WHERE status = 'feed_active'),
			        COUNT(*) FILTER (WHERE status = 'feed_staged')
			 FROM api_keys
			 WHERE org_id = $1 AND project_id = $2
			   AND purpose = 'xinsight.feed.read'`,
			orgID, projectID,
		).Scan(&activeCount, &stagedCount); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to validate X-Insight connection"})
			return
		}
		if activeCount != 1 || stagedCount != 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Resume requires exactly one active credential and no staged replacement"})
			return
		}
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE completed_execution_feed_connections
		 SET status = $3, updated_at = NOW()
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID, target,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to update X-Insight connection"})
		return
	}
	action := "data_exchange.xinsight.connection.pause"
	if target == xInsightFeedConnectionActive {
		action = "data_exchange.xinsight.connection.resume"
	}
	if err := insertAuditEntry(r.Context(), tx, action, "xinsight_feed_connection", strconv.FormatInt(projectID, 10),
		map[string]any{
			"before": map[string]any{"status": current},
			"after":  map[string]any{"status": target},
		},
		requestAuditMeta(r, orgID, projectID),
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to update X-Insight connection"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection_status": target})
}

func (h *Handler) CreateXInsightFeedCredential(w http.ResponseWriter, r *http.Request) {
	var request createXInsightFeedCredentialRequest
	if !decodeXInsightFeedControlJSON(w, r, &request) {
		return
	}
	if !exactHostNetworkBinding(request.AllowedIP) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": errXInsightFeedInvalidScope.Error()})
		return
	}
	if _, err := validateXInsightFeedRegistration(request.Registration); err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	projectID, err := xInsightFeedProjectID(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project ID"})
		return
	}
	orgID, err := h.xInsightFeedExistingProjectOrg(r.Context(), projectID)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	response, err := h.createXInsightFeedStagedCredential(
		r, orgID, projectID, strings.TrimSpace(request.AllowedIP), request.Registration, false,
	)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, response)
}

func (h *Handler) PrepareXInsightFeedRotation(w http.ResponseWriter, r *http.Request) {
	var request prepareXInsightFeedRotationRequest
	if !decodeXInsightFeedControlJSON(w, r, &request) {
		return
	}
	if _, err := validateXInsightFeedRegistration(request.Registration); err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	projectID, err := xInsightFeedProjectID(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project ID"})
		return
	}
	orgID, err := h.xInsightFeedExistingProjectOrg(r.Context(), projectID)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	response, err := h.createXInsightFeedStagedCredential(
		r, orgID, projectID, "", request.Registration, true,
	)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, response)
}

func (h *Handler) createXInsightFeedStagedCredential(
	r *http.Request,
	orgID, projectID int64,
	requestedAllowedIP string,
	registration xInsightFeedCredentialRegistrationV1,
	rotation bool,
) (stagedXInsightFeedCredentialResponse, error) {
	decodedVerifier, err := validateXInsightFeedRegistration(registration)
	if err != nil {
		return stagedXInsightFeedCredentialResponse{}, err
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		return stagedXInsightFeedCredentialResponse{}, err
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizeXInsightFeedControlTx(
		r.Context(), tx, orgID, projectID, apiKeyMutationCreate, true,
	); err != nil {
		return stagedXInsightFeedCredentialResponse{}, err
	}
	connectionStatus, err := lockXInsightFeedConnection(r.Context(), tx, orgID, projectID)
	if err != nil {
		return stagedXInsightFeedCredentialResponse{}, err
	}
	var activeCount, stagedCount int
	var allowedIP string
	var rpmLimit, qpsLimit int
	if err := tx.QueryRow(r.Context(),
		`SELECT COUNT(*) FILTER (WHERE status = 'feed_active'),
		        COUNT(*) FILTER (WHERE status = 'feed_staged')
		 FROM api_keys
		 WHERE org_id = $1 AND project_id = $2
		   AND purpose = 'xinsight.feed.read'`,
		orgID, projectID,
	).Scan(&activeCount, &stagedCount); err != nil {
		return stagedXInsightFeedCredentialResponse{}, err
	}
	if connectionStatus != xInsightFeedConnectionPaused || stagedCount != 0 {
		return stagedXInsightFeedCredentialResponse{}, errXInsightFeedConflict
	}
	if rotation {
		if activeCount != 1 {
			return stagedXInsightFeedCredentialResponse{}, errXInsightFeedConflict
		}
		if err := tx.QueryRow(r.Context(),
			`SELECT allowed_ips, rpm_limit, qps_limit
			 FROM api_keys
			 WHERE org_id = $1 AND project_id = $2
			   AND purpose = 'xinsight.feed.read'
			   AND status = 'feed_active'
			 FOR UPDATE`,
			orgID, projectID,
		).Scan(&allowedIP, &rpmLimit, &qpsLimit); err != nil {
			return stagedXInsightFeedCredentialResponse{}, err
		}
	} else {
		if activeCount != 0 {
			return stagedXInsightFeedCredentialResponse{}, errXInsightFeedConflict
		}
		allowedIP, rpmLimit, qpsLimit = requestedAllowedIP, 60, 1
	}
	if !exactHostNetworkBinding(allowedIP) {
		return stagedXInsightFeedCredentialResponse{}, errXInsightFeedInvalidScope
	}
	keyHash := auth.HashReceiverFeedVerifier(decodedVerifier, h.apiKeyHashSecret)
	keyName := fmt.Sprintf("X-Insight feed %d %s", projectID, registration.KeyPrefix)
	var keyID int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO api_keys (
		    org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix,
		    status, rpm_limit, qps_limit, budget_limit, allowed_ips, allowed_models,
		    policy_labels, purpose, verifier_scheme, registration_operation_id,
		    created_at, updated_at
		 ) VALUES (
		    $1, $2, NULL, 'service', $3, $4, $5,
		    'feed_staged', $6, $7, 0, $8, '', '',
		    'xinsight.feed.read', $9, $10, NOW(), NOW()
		 ) RETURNING id`,
		orgID, projectID, keyName, keyHash, registration.KeyPrefix,
		rpmLimit, qpsLimit, allowedIP, registration.VerifierScheme, registration.OperationID,
	).Scan(&keyID)
	if err != nil {
		if isXInsightFeedRegistrationCollision(err) {
			return stagedXInsightFeedCredentialResponse{}, errXInsightFeedConflict
		}
		return stagedXInsightFeedCredentialResponse{}, err
	}
	action := "data_exchange.xinsight.credential.create"
	if rotation {
		action = "data_exchange.xinsight.rotation.prepare"
	}
	if err := insertAuditEntry(r.Context(), tx, action, "xinsight_feed_connection", strconv.FormatInt(projectID, 10),
		map[string]any{
			"credential_id": keyID,
			"after": map[string]any{
				"credential_id":   keyID,
				"lifecycle":       "staged",
				"purpose":         xInsightFeedKeyPurpose,
				"key_type":        "service",
				"network_bound":   true,
				"models":          0,
				"policy_labels":   0,
				"verifier_scheme": registration.VerifierScheme,
				"operation_id":    registration.OperationID,
			},
		},
		requestAuditMeta(r, orgID, projectID),
	); err != nil {
		return stagedXInsightFeedCredentialResponse{}, err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return stagedXInsightFeedCredentialResponse{}, err
	}
	return stagedXInsightFeedCredentialResponse{
		ID: keyID, KeyPrefix: registration.KeyPrefix, Lifecycle: "staged",
		ProjectID: projectID, Purpose: xInsightFeedKeyPurpose, KeyType: "service",
		VerifierScheme: registration.VerifierScheme, OperationID: registration.OperationID,
	}, nil
}

func (h *Handler) ActivateInitialXInsightFeedCredential(w http.ResponseWriter, r *http.Request) {
	h.activateXInsightFeedCredential(w, r, false)
}

func (h *Handler) ActivateXInsightFeedRotation(w http.ResponseWriter, r *http.Request) {
	h.activateXInsightFeedCredential(w, r, true)
}

func (h *Handler) activateXInsightFeedCredential(w http.ResponseWriter, r *http.Request, rotation bool) {
	projectID, orgID, request, ok := h.xInsightFeedCredentialMutationRequest(w, r)
	if !ok {
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to activate feed credential"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizeXInsightFeedControlTx(
		r.Context(), tx, orgID, projectID, apiKeyMutationUpdate, true,
	); err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	connectionStatus, err := lockXInsightFeedConnection(r.Context(), tx, orgID, projectID)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	if connectionStatus != xInsightFeedConnectionPaused {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Connection must remain paused during credential activation"})
		return
	}
	var stagedStatus string
	if err := tx.QueryRow(r.Context(),
		`SELECT status FROM api_keys
		 WHERE id = $1 AND org_id = $2 AND project_id = $3
		   AND purpose = 'xinsight.feed.read'
		 FOR UPDATE`,
		request.StagedCredentialID, orgID, projectID,
	).Scan(&stagedStatus); err != nil {
		writeXInsightFeedControlError(w, errXInsightFeedNotFound)
		return
	}
	if stagedStatus != middleware.APIKeyStatusFeedStaged {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Credential is not a staged replacement"})
		return
	}
	var activeID sql.NullInt64
	if err := tx.QueryRow(r.Context(),
		`SELECT MAX(id) FILTER (WHERE status = 'feed_active')
		 FROM api_keys
		 WHERE org_id = $1 AND project_id = $2
		   AND purpose = 'xinsight.feed.read'`,
		orgID, projectID,
	).Scan(&activeID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to validate feed credential"})
		return
	}
	if rotation != activeID.Valid {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Credential activation mode does not match current connection state"})
		return
	}
	if rotation {
		if _, err := tx.Exec(r.Context(),
			`UPDATE api_keys
			 SET status = 'feed_revoked', updated_at = NOW()
			 WHERE id = $1 AND status = 'feed_active'`,
			activeID.Int64,
		); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to retire previous feed credential"})
			return
		}
	}
	tag, err := tx.Exec(r.Context(),
		`UPDATE api_keys SET status = 'feed_active', updated_at = NOW()
		 WHERE id = $1 AND status = 'feed_staged'`,
		request.StagedCredentialID,
	)
	if err != nil || tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to activate feed credential"})
		return
	}
	action := "data_exchange.xinsight.credential.activate"
	if rotation {
		action = "data_exchange.xinsight.rotation.activate"
	}
	detail := map[string]any{
		"after": map[string]any{
			"credential_id": request.StagedCredentialID,
			"lifecycle":     "active",
		},
	}
	if activeID.Valid {
		detail["before"] = map[string]any{
			"credential_id": activeID.Int64,
			"lifecycle":     "active",
		}
		detail["retired_credential_id"] = activeID.Int64
	}
	if err := insertAuditEntry(r.Context(), tx, action, "xinsight_feed_connection", strconv.FormatInt(projectID, 10),
		detail, requestAuditMeta(r, orgID, projectID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to activate feed credential"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connection_status": xInsightFeedConnectionPaused,
		"credential_id":     request.StagedCredentialID,
		"lifecycle":         "active",
	})
}

func (h *Handler) CancelXInsightFeedRotation(w http.ResponseWriter, r *http.Request) {
	projectID, orgID, request, ok := h.xInsightFeedCredentialMutationRequest(w, r)
	if !ok {
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to cancel feed rotation"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizeXInsightFeedControlTx(
		r.Context(), tx, orgID, projectID, apiKeyMutationUpdate, false,
	); err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	connectionStatus, err := lockXInsightFeedConnection(r.Context(), tx, orgID, projectID)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	if connectionStatus != xInsightFeedConnectionPaused {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Connection must remain paused during rotation cancellation"})
		return
	}
	tag, err := tx.Exec(r.Context(),
		`UPDATE api_keys SET status = 'feed_revoked', updated_at = NOW()
		 WHERE id = $1 AND org_id = $2 AND project_id = $3
		   AND purpose = 'xinsight.feed.read' AND status = 'feed_staged'`,
		request.StagedCredentialID, orgID, projectID,
	)
	if err != nil || tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Staged replacement is not cancellable"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "data_exchange.xinsight.rotation.cancel", "xinsight_feed_connection", strconv.FormatInt(projectID, 10),
		map[string]any{
			"credential_id": request.StagedCredentialID,
			"before":        map[string]any{"lifecycle": "staged"},
			"after":         map[string]any{"lifecycle": "revoked"},
		},
		requestAuditMeta(r, orgID, projectID),
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to cancel feed rotation"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credential_id": request.StagedCredentialID, "lifecycle": "revoked"})
}

func (h *Handler) RevokeXInsightFeedCredential(w http.ResponseWriter, r *http.Request) {
	projectID, err := xInsightFeedProjectID(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project ID"})
		return
	}
	credentialID, err := strconv.ParseInt(r.PathValue("credential_id"), 10, 64)
	if err != nil || credentialID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid credential ID"})
		return
	}
	orgID, err := h.xInsightFeedExistingProjectOrg(r.Context(), projectID)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to revoke feed credential"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizeXInsightFeedControlTx(
		r.Context(), tx, orgID, projectID, apiKeyMutationUpdate, false,
	); err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	if _, err := lockXInsightFeedConnection(r.Context(), tx, orgID, projectID); err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	var before string
	if err := tx.QueryRow(r.Context(),
		`SELECT status FROM api_keys
		 WHERE id = $1 AND org_id = $2 AND project_id = $3
		   AND purpose = 'xinsight.feed.read'
		 FOR UPDATE`,
		credentialID, orgID, projectID,
	).Scan(&before); err != nil {
		writeXInsightFeedControlError(w, errXInsightFeedNotFound)
		return
	}
	if before != middleware.APIKeyStatusFeedActive && before != middleware.APIKeyStatusFeedStaged {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Feed credential is already terminal"})
		return
	}
	if before == middleware.APIKeyStatusFeedActive {
		var stagedCount int
		if err := tx.QueryRow(r.Context(),
			`SELECT COUNT(*)
			 FROM api_keys
			 WHERE org_id = $1 AND project_id = $2
			   AND purpose = 'xinsight.feed.read'
			   AND status = 'feed_staged'`,
			orgID, projectID,
		).Scan(&stagedCount); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to validate feed rotation"})
			return
		}
		if stagedCount != 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Cancel or activate the staged rotation before revoking the active credential"})
			return
		}
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE api_keys SET status = 'feed_revoked', updated_at = NOW() WHERE id = $1`,
		credentialID,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to revoke feed credential"})
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE completed_execution_feed_connections
		 SET status = 'paused', updated_at = NOW()
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to pause X-Insight connection"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "data_exchange.xinsight.credential.revoke", "xinsight_feed_connection", strconv.FormatInt(projectID, 10),
		map[string]any{
			"credential_id": credentialID,
			"before":        map[string]any{"lifecycle": xInsightFeedLifecycle(before)},
			"after":         map[string]any{"lifecycle": "revoked", "connection_status": "paused"},
		},
		requestAuditMeta(r, orgID, projectID),
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to revoke feed credential"})
		return
	}
	h.emitAPIKeyDisabledEvent(r.Context(), orgID, projectID, credentialID, "X-Insight feed credential", "")
	writeJSON(w, http.StatusOK, map[string]any{
		"credential_id": credentialID, "lifecycle": "revoked", "connection_status": "paused",
	})
}

func (h *Handler) xInsightFeedCredentialMutationRequest(
	w http.ResponseWriter,
	r *http.Request,
) (int64, int64, xInsightFeedCredentialIDRequest, bool) {
	projectID, err := xInsightFeedProjectID(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project ID"})
		return 0, 0, xInsightFeedCredentialIDRequest{}, false
	}
	var request xInsightFeedCredentialIDRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.StagedCredentialID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "staged_credential_id is required"})
		return 0, 0, xInsightFeedCredentialIDRequest{}, false
	}
	orgID, err := h.xInsightFeedExistingProjectOrg(r.Context(), projectID)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return 0, 0, xInsightFeedCredentialIDRequest{}, false
	}
	return projectID, orgID, request, true
}

func (h *Handler) readXInsightFeedConnectionStatus(ctx context.Context, projectID int64) (xInsightFeedConnectionStatus, error) {
	var result xInsightFeedConnectionStatus
	var orgID int64
	var connectionStatus sql.NullString
	var acknowledgedSequence int64
	var lastAuthenticatedPull sql.NullTime
	var lastAuthenticatedKeyID sql.NullInt64
	if err := h.db.QueryRow(ctx,
		`SELECT project.org_id, project.name, project.status,
		        connection.status,
		        COALESCE(connection.acknowledged_sequence, 0),
		        connection.last_authenticated_pull_at,
		        connection.last_authenticated_key_id
		 FROM projects AS project
		 LEFT JOIN completed_execution_feed_connections AS connection
		   ON connection.org_id = project.org_id
		  AND connection.project_id = project.id
		 WHERE project.id = $1`,
		projectID,
	).Scan(
		&orgID,
		&result.ProjectName,
		&result.ProjectStatus,
		&connectionStatus,
		&acknowledgedSequence,
		&lastAuthenticatedPull,
		&lastAuthenticatedKeyID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return xInsightFeedConnectionStatus{}, errXInsightFeedNotFound
		}
		return xInsightFeedConnectionStatus{}, err
	}
	result.ProjectID = projectID
	result.GlobalConfigured = h.completedFeedController != nil &&
		strings.TrimSpace(h.completedFeedController.SourceInstance()) != ""
	result.GlobalEnabled = h.completedFeedController != nil && h.completedFeedController.Enabled()
	if h.completedFeedController != nil {
		result.SourceInstance = h.completedFeedController.SourceInstance()
	}
	result.ConnectionStatus = "not_configured"
	if connectionStatus.Valid {
		result.ConnectionStatus = connectionStatus.String
	}
	if lastAuthenticatedPull.Valid {
		pullAt := lastAuthenticatedPull.Time.UTC()
		result.LastAuthenticatedPullAt = &pullAt
	}
	result.ReceiverAcknowledgement = "next_requested_cursor_after_receiver_persistence"

	var err error
	result.ActiveCredential, err = h.readXInsightFeedCredential(ctx, orgID, projectID, middleware.APIKeyStatusFeedActive, 0)
	if err != nil {
		return xInsightFeedConnectionStatus{}, err
	}
	result.StagedCredential, err = h.readXInsightFeedCredential(ctx, orgID, projectID, middleware.APIKeyStatusFeedStaged, 0)
	if err != nil {
		return xInsightFeedConnectionStatus{}, err
	}
	result.LastRevokedCredential, err = h.readXInsightFeedCredential(ctx, orgID, projectID, middleware.APIKeyStatusFeedRevoked, 0)
	if err != nil {
		return xInsightFeedConnectionStatus{}, err
	}
	if lastAuthenticatedKeyID.Valid {
		result.LastAuthenticatedCredential, err = h.readXInsightFeedCredential(ctx, orgID, projectID, "", lastAuthenticatedKeyID.Int64)
		if err != nil {
			return xInsightFeedConnectionStatus{}, err
		}
	}

	var highWatermark, retainedAfter int64
	if h.xInsightConnectorManager != nil {
		connectorState, connectorErr := h.xInsightConnectorManager.ReadProjectState(ctx, orgID, projectID)
		if connectorErr != nil {
			return xInsightFeedConnectionStatus{}, connectorErr
		}
		if connectorState.Installation != nil {
			result.ConnectorInstallation = summarizeXInsightConnectorInstallation(*connectorState.Installation)
		}
		if connectorState.LatestOperation != nil {
			result.ConnectorLatestOperation = summarizeXInsightConnectorOperation(*connectorState.LatestOperation)
		}
		result.ConnectorProductStatus = connectorState.Health.ProductStatus
		result.PendingItemCount = connectorState.Health.PendingItemCount
		result.PendingItemCountCapped = connectorState.Health.PendingItemCountCapped
		result.OldestPendingAt = connectorState.Health.OldestPendingAt
		result.OldestPendingAgeSeconds = connectorState.Health.OldestPendingAgeSeconds
		result.CursorOutsideRetention = connectorState.Health.CursorOutsideRetention
		highWatermark = connectorState.HighWatermark
		retainedAfter = connectorState.RetainedAfter
		acknowledgedSequence = connectorState.AcknowledgedSequence
	} else if err := h.db.QueryRow(ctx,
		`SELECT COALESCE(MAX(item.id), 0),
		        COALESCE(state.retained_after_id, 0),
		        COUNT(item.id) FILTER (WHERE item.id > $3)
		 FROM completed_execution_feed_states AS state
		 FULL JOIN completed_execution_feed_items AS item
		   ON item.org_id = state.org_id AND item.project_id = state.project_id
		 WHERE COALESCE(item.org_id, state.org_id) = $1
		   AND COALESCE(item.project_id, state.project_id) = $2
		 GROUP BY state.retained_after_id`,
		orgID, projectID, acknowledgedSequence,
	).Scan(&highWatermark, &retainedAfter, &result.PendingItemCount); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return xInsightFeedConnectionStatus{}, err
	} else {
		result.ConnectorProductStatus = xinsightconnector.ProductNotConnected
		result.CursorOutsideRetention = connectionStatus.Valid && acknowledgedSequence < retainedAfter
	}
	if highWatermark < retainedAfter {
		highWatermark = retainedAfter
	}
	if result.GlobalConfigured && h.completedFeedController != nil {
		if cursor, err := h.completedFeedController.EncodeSequence(projectID, highWatermark); err == nil {
			result.SourceHighWatermark = cursor
		} else {
			return xInsightFeedConnectionStatus{}, err
		}
		if connectionStatus.Valid {
			cursor, err := h.completedFeedController.EncodeSequence(projectID, acknowledgedSequence)
			if err != nil {
				return xInsightFeedConnectionStatus{}, err
			}
			result.ReceiverAcknowledgedCursor = cursor
		}
	}
	return result, nil
}

func summarizeXInsightConnectorInstallation(
	value xinsightconnector.Installation,
) *xInsightConnectorInstallationSummary {
	return &xInsightConnectorInstallationSummary{
		SchemaVersion: value.SchemaVersion, InstallationID: value.InstallationID,
		XDeploymentID: value.XDeploymentID, EventScope: value.EventScope,
		SourceInstance: value.SourceInstance, SourceBindingSHA256: value.SourceBindingSHA256,
		DesiredState: value.DesiredState, ObservedState: value.ObservedState,
		StateRevision: value.StateRevision, ControlFence: value.ControlFence,
		BootstrapReceiptSHA256: value.BootstrapReceiptSHA256,
		LastSuccessfulSyncAt:   value.LastSuccessfulSyncAt, LastErrorCode: value.LastErrorCode,
		LastErrorAt: value.LastErrorAt, ConsentedAt: value.ConsentedAt, UpdatedAt: value.UpdatedAt,
	}
}

func summarizeXInsightConnectorOperation(
	value xinsightconnector.Operation,
) *xInsightConnectorOperationSummary {
	return &xInsightConnectorOperationSummary{
		SchemaVersion: value.SchemaVersion, OperationRef: value.OperationRef,
		InstallationID: value.InstallationID, Action: value.Action,
		DesiredState: value.DesiredState, State: value.State,
		RequestedRevision: value.RequestedRevision, ControlFence: value.ControlFence,
		AttemptCount: value.AttemptCount, ResultCode: value.ResultCode,
		ResultSHA256: value.ResultSHA256, ErrorCode: value.ErrorCode,
		CompletedAt: value.CompletedAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

// AuthorizeXInsightConnectorControl is the product-local commit-time seam used
// by the durable Connector manager. It delegates to the existing feed/API-key
// authorization policy; it does not grant the Connector agent new authority.
func (h *Handler) AuthorizeXInsightConnectorControl(
	ctx context.Context,
	tx pgx.Tx,
	actorUserID, orgID, projectID int64,
	action string,
) error {
	if actorUserID <= 0 || middleware.GetUserIDFromContext(ctx) != actorUserID {
		return xinsightconnector.ErrUnauthorized
	}
	operation := apiKeyMutationUpdate
	targetProjectMustActive := action != xinsightconnector.ActionPause
	if action == xinsightconnector.ActionConnect {
		operation = apiKeyMutationCreate
	}
	if action != xinsightconnector.ActionConnect &&
		action != xinsightconnector.ActionPause &&
		action != xinsightconnector.ActionResume &&
		action != xinsightconnector.ActionRotate &&
		action != xinsightconnector.ActionReplay {
		return xinsightconnector.ErrInvalid
	}
	err := h.reauthorizeXInsightFeedControlTx(
		ctx, tx, orgID, projectID, operation, targetProjectMustActive,
	)
	if err == nil {
		return nil
	}
	if errors.Is(err, errAPIKeyAuthorizationChanged) {
		return xinsightconnector.ErrUnauthorized
	}
	return fmt.Errorf("%w: connector commit authorization: %v", xinsightconnector.ErrUnavailable, err)
}

func (h *Handler) readXInsightFeedCredential(
	ctx context.Context,
	orgID, projectID int64,
	status string,
	keyID int64,
) (*xInsightFeedCredential, error) {
	conditions := `org_id = $1 AND project_id = $2 AND purpose = 'xinsight.feed.read'`
	args := []any{orgID, projectID}
	if keyID > 0 {
		conditions += " AND id = $3"
		args = append(args, keyID)
	} else {
		if status == middleware.APIKeyStatusFeedRevoked {
			conditions += " AND status IN ($3, 'disabled')"
		} else {
			conditions += " AND status = $3"
		}
		args = append(args, status)
	}
	var credential xInsightFeedCredential
	var updatedAt time.Time
	err := h.db.QueryRow(ctx,
		`SELECT id, key_prefix, status, verifier_scheme, registration_operation_id,
		        created_at, updated_at
		 FROM api_keys
		 WHERE `+conditions+`
		 ORDER BY updated_at DESC, id DESC
		 LIMIT 1`,
		args...,
	).Scan(
		&credential.ID,
		&credential.KeyPrefix,
		&credential.Status,
		&credential.VerifierScheme,
		&credential.OperationID,
		&credential.CreatedAt,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	credential.Lifecycle = xInsightFeedLifecycle(credential.Status)
	if credential.Status == middleware.APIKeyStatusFeedRevoked || credential.Status == "disabled" {
		revokedAt := updatedAt.UTC()
		credential.RevokedAt = &revokedAt
	}
	return &credential, nil
}

func (h *Handler) xInsightFeedExistingProjectOrg(ctx context.Context, projectID int64) (int64, error) {
	var orgID int64
	if err := h.db.QueryRow(ctx, `SELECT org_id FROM projects WHERE id = $1`, projectID).Scan(&orgID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, errXInsightFeedNotFound
		}
		return 0, err
	}
	return orgID, nil
}

func lockXInsightFeedConnection(ctx context.Context, tx pgx.Tx, orgID, projectID int64) (string, error) {
	if _, err := tx.Exec(ctx,
		`INSERT INTO completed_execution_feed_connections (org_id, project_id, status)
		 VALUES ($1, $2, 'paused')
		 ON CONFLICT (org_id, project_id) DO NOTHING`,
		orgID, projectID,
	); err != nil {
		return "", err
	}
	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM completed_execution_feed_connections
		 WHERE org_id = $1 AND project_id = $2
		 FOR UPDATE`,
		orgID, projectID,
	).Scan(&status); err != nil {
		return "", err
	}
	return status, nil
}

func (h *Handler) reauthorizeXInsightFeedControlTx(
	ctx context.Context,
	tx pgx.Tx,
	orgID, projectID int64,
	operation apiKeyMutationOperation,
	targetProjectMustActive bool,
) error {
	intent := apiKeyAuthorizationIntent{
		operation:               operation,
		targetOrgID:             orgID,
		targetProjectID:         projectID,
		targetProjectMustActive: targetProjectMustActive,
	}
	if operation == apiKeyMutationCreate {
		intent.validateOwnership = true
		intent.ownerUserID = 0
		intent.keyType = "service"
	} else {
		intent.sourceProjectID = projectID
	}
	return h.reauthorizeAPIKeyMutationTx(ctx, tx, intent)
}

func xInsightFeedProjectID(r *http.Request) (int64, error) {
	projectID, err := strconv.ParseInt(r.PathValue("project_id"), 10, 64)
	if err != nil || projectID <= 0 {
		return 0, errXInsightFeedNotFound
	}
	return projectID, nil
}

func exactHostNetworkBinding(raw string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil || prefix.Addr().Zone() != "" || prefix != prefix.Masked() {
		return false
	}
	return (prefix.Addr().Is4() && prefix.Bits() == 32) ||
		(prefix.Addr().Is6() && prefix.Bits() == 128)
}

func decodeXInsightFeedControlJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid JSON"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Only one JSON object is allowed"})
		return false
	}
	return true
}

func validateXInsightFeedRegistration(
	registration xInsightFeedCredentialRegistrationV1,
) ([]byte, error) {
	if registration.SchemaVersion != auth.ReceiverFeedRegistrationSchema ||
		registration.TokenFormat != auth.ReceiverFeedTokenFormat ||
		registration.VerifierScheme != auth.ReceiverFeedVerifierScheme ||
		!xInsightFeedPrefixPattern.MatchString(registration.KeyPrefix) ||
		!xInsightFeedOperationPattern.MatchString(registration.OperationID) {
		return nil, errXInsightFeedRegistration
	}
	verifier, err := auth.DecodeReceiverFeedVerifier(registration.VerifierSHA256)
	if err != nil {
		return nil, errXInsightFeedRegistration
	}
	return verifier, nil
}

func isXInsightFeedRegistrationCollision(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		(pgErr.ConstraintName == "idx_api_keys_prefix" ||
			pgErr.ConstraintName == "api_keys_receiver_feed_operation" ||
			pgErr.ConstraintName == "api_keys_one_staged_xinsight_feed_per_project")
}

func xInsightFeedLifecycle(status string) string {
	switch status {
	case middleware.APIKeyStatusFeedActive:
		return "active"
	case middleware.APIKeyStatusFeedStaged:
		return "staged"
	default:
		return "revoked"
	}
}

func writeXInsightFeedControlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errAPIKeyAuthorizationChanged):
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Forbidden"})
	case errors.Is(err, errXInsightFeedNotFound), errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "X-Insight feed project or credential not found"})
	case errors.Is(err, errXInsightFeedConflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "X-Insight feed state conflicts with the requested operation"})
	case errors.Is(err, errXInsightFeedInvalidScope):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
	case errors.Is(err, errXInsightFeedRegistration):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "X-Insight feed control operation failed"})
	}
}

func writeXInsightConnectorInvitationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, xinsightconnector.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid frozen X-Insight Connector invitation"})
	case errors.Is(err, xinsightconnector.ErrUnauthorized):
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Forbidden"})
	case errors.Is(err, xinsightconnector.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "X-Insight Connector installation not found"})
	case errors.Is(err, xinsightconnector.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "X-Insight Connector invitation conflicts with existing state"})
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "X-Insight Connector invitation intake is unavailable"})
	}
}
