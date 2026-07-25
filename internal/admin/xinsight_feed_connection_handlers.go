package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

const (
	xInsightFeedConnectionActive = "active"
	xInsightFeedConnectionPaused = "paused"
	xInsightFeedKeyPurpose       = middleware.APIKeyPurposeXInsightFeedRead
)

var (
	errXInsightFeedConflict     = errors.New("x-insight feed control state conflicts with the requested operation")
	errXInsightFeedNotFound     = errors.New("x-insight feed project or credential was not found")
	errXInsightFeedNotReady     = errors.New("x-insight feed connection is not ready")
	errXInsightFeedInvalidScope = errors.New("x-insight feed network binding must be one exact IPv4 /32 or IPv6 /128")
)

type xInsightFeedCredential struct {
	ID        int64      `json:"id"`
	KeyPrefix string     `json:"key_prefix"`
	Lifecycle string     `json:"lifecycle"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	Status    string     `json:"-"`
}

type xInsightFeedConnectionStatus struct {
	ProjectID                   int64                   `json:"project_id"`
	ProjectName                 string                  `json:"project_name"`
	GlobalConfigured            bool                    `json:"global_configured"`
	GlobalEnabled               bool                    `json:"global_enabled"`
	SourceInstance              string                  `json:"source_instance"`
	ConnectionStatus            string                  `json:"connection_status"`
	ActiveCredential            *xInsightFeedCredential `json:"active_credential"`
	StagedCredential            *xInsightFeedCredential `json:"staged_credential"`
	LastRevokedCredential       *xInsightFeedCredential `json:"last_revoked_credential"`
	LastAuthenticatedPullAt     *time.Time              `json:"last_authenticated_pull_at"`
	LastAuthenticatedCredential *xInsightFeedCredential `json:"last_authenticated_credential"`
	SourceHighWatermark         string                  `json:"source_high_watermark"`
	ReceiverAcknowledgedCursor  string                  `json:"receiver_acknowledged_cursor"`
	ReceiverAcknowledgement     string                  `json:"receiver_acknowledgement_semantics"`
	PendingItemCount            int64                   `json:"pending_item_count"`
	CursorOutsideRetention      bool                    `json:"cursor_outside_retention"`
}

type createXInsightFeedCredentialRequest struct {
	AllowedIP string `json:"allowed_ip"`
}

type stagedXInsightFeedCredentialResponse struct {
	ID        int64  `json:"id"`
	Key       string `json:"key"`
	KeyPrefix string `json:"key_prefix"`
	Lifecycle string `json:"lifecycle"`
	ProjectID int64  `json:"project_id"`
	Purpose   string `json:"purpose"`
	KeyType   string `json:"key_type"`
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

func (h *Handler) setXInsightFeedConnectionState(w http.ResponseWriter, r *http.Request, target string) {
	projectID, err := xInsightFeedProjectID(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project ID"})
		return
	}
	orgID, err := h.xInsightFeedProjectOrg(r.Context(), projectID)
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
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid JSON"})
		return
	}
	if !exactHostNetworkBinding(request.AllowedIP) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": errXInsightFeedInvalidScope.Error()})
		return
	}
	projectID, err := xInsightFeedProjectID(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project ID"})
		return
	}
	orgID, err := h.xInsightFeedProjectOrg(r.Context(), projectID)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	response, err := h.createXInsightFeedStagedCredential(r, orgID, projectID, strings.TrimSpace(request.AllowedIP), false)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, response)
}

func (h *Handler) PrepareXInsightFeedRotation(w http.ResponseWriter, r *http.Request) {
	projectID, err := xInsightFeedProjectID(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid project ID"})
		return
	}
	orgID, err := h.xInsightFeedProjectOrg(r.Context(), projectID)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	response, err := h.createXInsightFeedStagedCredential(r, orgID, projectID, "", true)
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
	rotation bool,
) (stagedXInsightFeedCredentialResponse, error) {
	for attempt := 0; attempt < apiKeyPrefixInsertMaxAttempts; attempt++ {
		rawKey, keyPrefix, err := auth.GenerateAPIKey(orgID)
		if err != nil {
			return stagedXInsightFeedCredentialResponse{}, err
		}
		tx, err := h.db.Begin(r.Context())
		if err != nil {
			return stagedXInsightFeedCredentialResponse{}, err
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback(r.Context())
			}
		}()
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
		keyHash := auth.HashAPIKey(rawKey, h.apiKeyHashSecret)
		keyName := fmt.Sprintf("X-Insight feed %d %s", projectID, keyPrefix)
		var keyID int64
		err = tx.QueryRow(r.Context(),
			`INSERT INTO api_keys (
			    org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix,
			    status, rpm_limit, qps_limit, budget_limit, allowed_ips, allowed_models,
			    policy_labels, purpose, created_at, updated_at
			 ) VALUES (
			    $1, $2, NULL, 'service', $3, $4, $5,
			    'feed_staged', $6, $7, 0, $8, '', '',
			    'xinsight.feed.read', NOW(), NOW()
			 ) RETURNING id`,
			orgID, projectID, keyName, keyHash, keyPrefix, rpmLimit, qpsLimit, allowedIP,
		).Scan(&keyID)
		if err != nil {
			_ = tx.Rollback(r.Context())
			if isAPIKeyPrefixCollision(err) {
				continue
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
					"credential_id": keyID,
					"lifecycle":     "staged",
					"purpose":       xInsightFeedKeyPurpose,
					"key_type":      "service",
					"network_bound": true,
					"models":        0,
					"policy_labels": 0,
				},
			},
			requestAuditMeta(r, orgID, projectID),
		); err != nil {
			return stagedXInsightFeedCredentialResponse{}, err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return stagedXInsightFeedCredentialResponse{}, err
		}
		committed = true
		return stagedXInsightFeedCredentialResponse{
			ID: keyID, Key: rawKey, KeyPrefix: keyPrefix, Lifecycle: "staged",
			ProjectID: projectID, Purpose: xInsightFeedKeyPurpose, KeyType: "service",
		}, nil
	}
	return stagedXInsightFeedCredentialResponse{}, errors.New("failed to allocate a unique feed credential")
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
	orgID, err := h.xInsightFeedProjectOrg(r.Context(), projectID)
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
	orgID, err := h.xInsightFeedProjectOrg(r.Context(), projectID)
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
		`SELECT project.org_id, project.name,
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
	if err := h.db.QueryRow(ctx,
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
	}
	if highWatermark < retainedAfter {
		highWatermark = retainedAfter
	}
	result.CursorOutsideRetention = connectionStatus.Valid && acknowledgedSequence < retainedAfter
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
		`SELECT id, key_prefix, status, created_at, updated_at
		 FROM api_keys
		 WHERE `+conditions+`
		 ORDER BY updated_at DESC, id DESC
		 LIMIT 1`,
		args...,
	).Scan(&credential.ID, &credential.KeyPrefix, &credential.Status, &credential.CreatedAt, &updatedAt)
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

func (h *Handler) xInsightFeedProjectOrg(ctx context.Context, projectID int64) (int64, error) {
	var orgID int64
	var status string
	if err := h.db.QueryRow(ctx, `SELECT org_id, status FROM projects WHERE id = $1`, projectID).Scan(&orgID, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, errXInsightFeedNotFound
		}
		return 0, err
	}
	if status != "active" {
		return 0, errXInsightFeedNotReady
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
	case errors.Is(err, errXInsightFeedNotFound), errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "X-Insight feed project or credential not found"})
	case errors.Is(err, errXInsightFeedConflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "X-Insight feed state conflicts with the requested operation"})
	case errors.Is(err, errXInsightFeedNotReady):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Project is not active"})
	case errors.Is(err, errXInsightFeedInvalidScope):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "X-Insight feed control operation failed"})
	}
}
