package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

type contentControlResult struct {
	OperationID          string                       `json:"operation_id"`
	Action               string                       `json:"action"`
	StateVersion         string                       `json:"state_version"`
	ContentScopeRevision string                       `json:"content_scope_revision"`
	CredentialID         string                       `json:"credential_id,omitempty"`
	RegistrationHash     string                       `json:"registration_hash,omitempty"`
	CoverageFrom         *sourcefacts.ContentBoundary `json:"coverage_from,omitempty"`
	AllowedPurposes      []string                     `json:"allowed_purposes,omitempty"`
}

// This coordinator reauthorizes before its first write and never commits its
// caller's transaction. The HTTP adapter must roll back any returned error.
func (h *Handler) contentControlTx(ctx context.Context, tx pgx.Tx, org, project int64, action string, request contentControlRequest, r *http.Request) (contentControlResult, error) {
	var result contentControlResult
	if h == nil || h.sourceFactsController == nil {
		return result, errXInsightFeedInvalidScope
	}
	instance := h.sourceFactsController.SourceInstance()
	// Validate the exact C request and target before any write. Do not rely
	// on an as-yet-unwired HTTP adapter to establish transaction authority.
	input, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	if _, err = decodeContentControl(input, action); err != nil {
		return result, err
	}
	if request.Registration != nil && (request.Registration.OrgID != strconv.FormatInt(org, 10) || request.Registration.ProjectID != strconv.FormatInt(project, 10) || request.Registration.SourceInstance != instance) {
		return result, errXInsightFeedInvalidScope
	}
	safety := action == "pause" || action == "cancel" || action == "revoke"
	controller, enabled := h.sourceFactsController.(interface{ ContentEnabled() bool })
	if !safety && (!enabled || !controller.ContentEnabled()) {
		return result, errXInsightFeedConflict
	}
	operation := apiKeyMutationUpdate
	if action == "prepare" || action == "rotate" {
		operation = apiKeyMutationCreate
	}
	// Existing original actor/project/ownership locks are acquired here,
	// before the first INSERT or parent/scope/key lock, including replays.
	if err = h.reauthorizeXInsightFeedControlTx(ctx, tx, org, project, operation, !safety); err != nil {
		return result, err
	}
	if action == "prepare" {
		if _, err := tx.Exec(ctx, `INSERT INTO execution_source_connections(org_id,project_id,source_instance,profiles) VALUES($1,$2,$3,ARRAY[]::text[]) ON CONFLICT DO NOTHING`, org, project, instance); err != nil {
			return result, err
		}
	}
	s, err := readContentStateTx(ctx, tx, org, project, instance, true)
	if err != nil {
		return result, err
	}
	if s.global == "not_configured" || s.global == "revoked" {
		return result, errXInsightFeedConflict
	}
	requestWire, err := json.Marshal(struct {
		Action  string                `json:"action"`
		Request contentControlRequest `json:"request"`
	}{action, request})
	if err != nil {
		return result, err
	}
	requestHash := eventconnector.HashSourceBytes(requestWire)
	if s.local != "not_configured" {
		var oldHash string
		var oldVersion, oldSafety int64
		var oldResponse []byte
		err = tx.QueryRow(ctx, `SELECT request_sha256,result_version,safety_version,response FROM execution_source_content_operations WHERE org_id=$1 AND project_id=$2 AND operation_id=$3`, org, project, request.OperationID).Scan(&oldHash, &oldVersion, &oldSafety, &oldResponse)
		if err == nil {
			if !contentReplayApplicable(requestHash, oldHash, oldVersion, oldSafety, s) {
				return result, errXInsightFeedConflict
			}
			err = json.Unmarshal(oldResponse, &result)
			return result, err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return result, err
		}
	}
	var operationTaken bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.source.read' AND registration_operation_id=$3)`, org, project, request.OperationID).Scan(&operationTaken); err != nil {
		return result, err
	}
	if operationTaken {
		return result, errXInsightFeedConflict
	}
	next, err := nextContentControlState(s, action, request)
	if err != nil {
		return result, err
	}
	if s.local == "not_configured" {
		if _, err = tx.Exec(ctx, `INSERT INTO execution_content_streams(org_id,project_id,source_instance) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, org, project, instance); err != nil {
			return result, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO execution_source_content_scopes(org_id,project_id) VALUES($1,$2)`, org, project); err != nil {
			return result, err
		}
	}
	var target *contentControlKey
	switch action {
	case "prepare", "rotate":
		key, err := h.insertContentStagedTx(ctx, tx, org, project, action, request, s)
		if err != nil {
			return result, err
		}
		target = &key
	case "activate", "cancel":
		target = s.staged
		if action == "activate" {
			if _, err = tx.Exec(ctx, `UPDATE api_keys SET status='feed_revoked',updated_at=clock_timestamp() WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.source.read' AND source_lifecycle_scope='content' AND status='feed_active'`, org, project); err != nil {
				return result, err
			}
		}
		state := "feed_revoked"
		if action == "activate" {
			state = "feed_active"
		}
		id, _ := contentControlNumber(target.CredentialID, false)
		tag, err := tx.Exec(ctx, `UPDATE api_keys SET status=$4,updated_at=clock_timestamp() WHERE id=$1 AND org_id=$2 AND project_id=$3 AND purpose='xinsight.source.read' AND source_lifecycle_scope='content' AND status='feed_staged'`, id, org, project, state)
		if err != nil {
			return result, err
		}
		if tag.RowsAffected() != 1 {
			return result, errXInsightFeedConflict
		}
	case "pause", "resume":
		target = s.active
	case "revoke":
		target = s.active
		if _, err = tx.Exec(ctx, `UPDATE api_keys SET status='feed_revoked',updated_at=clock_timestamp() WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.source.read' AND source_lifecycle_scope='content' AND status IN ('feed_active','feed_staged')`, org, project); err != nil {
			return result, err
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE execution_source_content_scopes SET status=$3,state_version=$4,access_generation=$5,updated_at=clock_timestamp() WHERE org_id=$1 AND project_id=$2 AND state_version=$6`, org, project, next.local, next.version, next.generation, s.version)
	if err != nil {
		return result, err
	}
	if tag.RowsAffected() != 1 {
		return result, errXInsightFeedConflict
	}
	result = contentControlResult{OperationID: request.OperationID, Action: action, StateVersion: strconv.FormatInt(next.version, 10), ContentScopeRevision: strconv.FormatInt(next.generation, 10)}
	if target != nil {
		result.CredentialID = target.CredentialID
		result.RegistrationHash = target.RegistrationHash
		boundary := target.CoverageFrom
		result.CoverageFrom = &boundary
		result.AllowedPurposes = target.AllowedPurposes
	}
	response, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO execution_source_content_operations(org_id,project_id,operation_id,request_sha256,result_version,safety_version,connection_status,response) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, org, project, request.OperationID, requestHash, next.version, s.safetyVersion, s.global, response); err != nil {
		return result, err
	}
	var audit map[string]any
	if err = json.Unmarshal(response, &audit); err != nil {
		return result, err
	}
	err = insertAuditEntry(ctx, tx, "data_exchange.xinsight.source.content."+action, "xinsight_source_content", strconv.FormatInt(project, 10), audit, requestAuditMeta(r, org, project))
	return result, err
}
