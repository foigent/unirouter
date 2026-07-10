package admin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/middleware"
)

var (
	dataExchangeSHA256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	dataExchangeIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,159}$`)
)

var dataExchangeClasses = map[string]bool{
	"request_trace_events": true,
	"call_logs":            true,
	"capture_gaps":         true,
}

type createExchangeItemRequest struct {
	ApprovalMode       string     `json:"approval_mode"`
	RequestID          string     `json:"request_id"`
	ScenarioLabel      string     `json:"scenario_label"`
	SourceInstance     string     `json:"source_instance"`
	DataClasses        []string   `json:"data_classes"`
	Reason             string     `json:"reason"`
	AuthorizedOperator string     `json:"authorized_operator"`
	RetentionUntil     *time.Time `json:"retention_until"`
}

type updateExchangeItemRequest struct {
	ApprovalStatus string `json:"approval_status"`
	Reason         string `json:"reason"`
	RequestID      string `json:"request_id"`
}

type createExchangeRequest struct {
	ExchangeID             string     `json:"exchange_id"`
	SourceInstance         string     `json:"source_instance"`
	SchemaVersion          string     `json:"schema_version"`
	ApprovalManifestSHA256 string     `json:"approval_manifest_sha256"`
	ArtifactSHA256         string     `json:"artifact_sha256"`
	ArtifactRef            string     `json:"artifact_ref"`
	ItemIDs                []int64    `json:"item_ids"`
	RequestCount           int        `json:"request_count"`
	InputEventCount        int        `json:"input_event_count"`
	GeneratedAt            time.Time  `json:"generated_at"`
	RetentionUntil         *time.Time `json:"retention_until"`
}

type createDeliveryRequest struct {
	Action      string    `json:"action"`
	HandoffRef  string    `json:"handoff_ref"`
	Note        string    `json:"note"`
	DeliveredAt time.Time `json:"delivered_at"`
}

type createReceiptRequest struct {
	ExchangeID             string    `json:"exchange_id"`
	ReceiptSchemaVersion   string    `json:"schema_version"`
	SourceSystem           string    `json:"source_system"`
	SourceInstance         string    `json:"source_instance"`
	ArtifactSchemaVersion  string    `json:"artifact_schema_version"`
	ArtifactSHA256         string    `json:"artifact_sha256"`
	ApprovalManifestSHA256 string    `json:"approval_manifest_sha256"`
	ResultStatus           string    `json:"result_status"`
	RequestIDs             int       `json:"request_ids"`
	InputEvents            int       `json:"input_events"`
	Accepted               int       `json:"accepted"`
	DuplicateNoop          int       `json:"duplicate_noop"`
	RerunDuplicateNoop     int       `json:"rerun_duplicate_noop"`
	Rejected               int       `json:"rejected"`
	Quarantined            int       `json:"quarantined"`
	CaptureGapEvents       int       `json:"capture_gap_events"`
	PersistedInserted      int       `json:"persisted_inserted"`
	PersistedDuplicateNoop int       `json:"persisted_duplicate_noop"`
	PersistenceConflicts   int       `json:"persistence_conflicts"`
	ReadbackEvents         int       `json:"readback_events"`
	ReadbackCaptureGaps    int       `json:"readback_capture_gaps"`
	WarningCodes           []string  `json:"warning_codes"`
	ImportJobID            string    `json:"import_job_id"`
	DeepLink               string    `json:"deep_link"`
	CheckedAt              time.Time `json:"checked_at"`
}

func (h *Handler) ListDataExchangeItems(w http.ResponseWriter, r *http.Request) {
	orgID, ok := dataExchangeOrgID(w, r)
	if !ok {
		return
	}
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)
	args := []any{orgID}
	conditions := []string{"org_id = $1"}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		if !oneOf(status, "pending", "approved", "unavailable") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid status"})
			return
		}
		conditions = append(conditions, "approval_status = $2")
		args = append(args, status)
	}
	where := " WHERE " + strings.Join(conditions, " AND ")
	var total int
	if err := h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM integration_exchange_items"+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Count failed"})
		return
	}
	query := fmt.Sprintf(`SELECT id, approval_mode, request_id, scenario_label, source_instance,
		data_classes, safety_profile, approval_status, reason, authorized_operator,
		retention_until, approved_by, approved_at, created_by, created_at, updated_at
		FROM integration_exchange_items%s ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, where, len(args)+1, len(args)+2)
	args = append(args, pageSize, offset)
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Query failed"})
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var approvalMode, scenarioLabel, sourceInstance, safetyProfile, status, reason string
		var requestID, authorizedOperator sql.NullString
		var dataClasses []string
		var retentionUntil, approvedAt sql.NullTime
		var approvedBy, createdBy sql.NullInt64
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &approvalMode, &requestID, &scenarioLabel, &sourceInstance, &dataClasses, &safetyProfile,
			&status, &reason, &authorizedOperator, &retentionUntil, &approvedBy, &approvedAt, &createdBy, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Scan failed"})
			return
		}
		items = append(items, map[string]any{
			"id": id, "approval_mode": approvalMode, "request_id": nullableDBString(requestID), "scenario_label": scenarioLabel,
			"source_instance": sourceInstance, "data_classes": dataClasses, "safety_profile": safetyProfile,
			"approval_status": status, "reason": reason, "authorized_operator": nullableDBString(authorizedOperator),
			"retention_until": nullableTime(retentionUntil), "approved_by": nullableSQLInt64(approvedBy),
			"approved_at": nullableTime(approvedAt), "created_by": nullableSQLInt64(createdBy),
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Rows failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateDataExchangeItem(w http.ResponseWriter, r *http.Request) {
	orgID, ok := dataExchangeOrgID(w, r)
	if !ok {
		return
	}
	var req createExchangeItemRequest
	if !decodeDataExchangeJSON(w, r, &req) || !validateExchangeItemRequest(w, &req) {
		return
	}
	userID := middleware.GetUserIDFromContext(r.Context())
	var id int64
	err := h.db.QueryRow(r.Context(), `INSERT INTO integration_exchange_items
		(org_id, approval_mode, request_id, scenario_label, source_instance, data_classes,
		 safety_profile, approval_status, reason, authorized_operator, retention_until, created_by)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, 'metadata_only', 'pending', $7, NULLIF($8, ''), $9, NULLIF($10, 0))
		RETURNING id`, orgID, req.ApprovalMode, req.RequestID, req.ScenarioLabel, req.SourceInstance,
		req.DataClasses, req.Reason, req.AuthorizedOperator, req.RetentionUntil, userID).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Candidate already exists or could not be created"})
		return
	}
	h.recordAuditForOrgProjectFromRequest(r, orgID, 0, "data_exchange.item.create", "integration_exchange_item", strconv.FormatInt(id, 10), map[string]any{
		"after": map[string]any{"approval_mode": req.ApprovalMode, "request_id": req.RequestID, "scenario_label": req.ScenarioLabel, "approval_status": "pending"},
	})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) UpdateDataExchangeItem(w http.ResponseWriter, r *http.Request) {
	orgID, ok := dataExchangeOrgID(w, r)
	if !ok {
		return
	}
	id, ok := dataExchangePathID(w, r)
	if !ok {
		return
	}
	var req updateExchangeItemRequest
	if !decodeDataExchangeJSON(w, r, &req) {
		return
	}
	req.ApprovalStatus = strings.TrimSpace(req.ApprovalStatus)
	req.Reason = strings.TrimSpace(req.Reason)
	req.RequestID = strings.TrimSpace(req.RequestID)
	if !oneOf(req.ApprovalStatus, "pending", "approved", "unavailable") || len(req.Reason) > 500 || len(req.RequestID) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid approval status or reason"})
		return
	}
	if req.ApprovalStatus == "unavailable" && req.Reason == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "reason is required for unavailable items"})
		return
	}
	var previousStatus, approvalMode string
	var existingRequestID sql.NullString
	if err := h.db.QueryRow(r.Context(), `SELECT approval_status, approval_mode, request_id FROM integration_exchange_items WHERE id = $1 AND org_id = $2`, id, orgID).Scan(&previousStatus, &approvalMode, &existingRequestID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Candidate not found"})
		return
	}
	requestID := existingRequestID.String
	if req.RequestID != "" {
		if approvalMode != "controlled_scenario" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request_id can only be sealed for a controlled_scenario"})
			return
		}
		requestID = req.RequestID
	}
	if req.ApprovalStatus == "approved" && requestID != "" {
		var requestCount int
		if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM call_logs WHERE org_id = $1 AND request_id = $2`, orgID, requestID).Scan(&requestCount); err != nil || requestCount == 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "request_id does not exist in this organization"})
			return
		}
	}
	if _, err := h.db.Exec(r.Context(), `UPDATE integration_exchange_items
		SET approval_status = $1, reason = $2,
		    approved_by = CASE WHEN $1 = 'approved' THEN NULLIF($3, 0) ELSE NULL END,
		    approved_at = CASE WHEN $1 = 'approved' THEN NOW() ELSE NULL END,
		    request_id = CASE WHEN $6 <> '' THEN $6 ELSE request_id END,
		    updated_at = NOW()
		WHERE id = $4 AND org_id = $5`, req.ApprovalStatus, req.Reason, middleware.GetUserIDFromContext(r.Context()), id, orgID, req.RequestID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Candidate update failed"})
		return
	}
	h.recordAuditForOrgProjectFromRequest(r, orgID, 0, "data_exchange.item.update", "integration_exchange_item", strconv.FormatInt(id, 10), map[string]any{
		"before": map[string]any{"approval_status": previousStatus, "request_id": existingRequestID.String},
		"after":  map[string]any{"approval_status": req.ApprovalStatus, "reason": req.Reason, "request_id": requestID},
	})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "approval_status": req.ApprovalStatus})
}

func (h *Handler) ListDataExchanges(w http.ResponseWriter, r *http.Request) {
	orgID, ok := dataExchangeOrgID(w, r)
	if !ok {
		return
	}
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)
	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM integration_exchanges WHERE org_id = $1`, orgID).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Count failed"})
		return
	}
	rows, err := h.db.Query(r.Context(), `SELECT e.id, e.exchange_id, e.downstream_system, e.source_instance, e.schema_version,
		e.approval_manifest_sha256, e.artifact_sha256, e.artifact_ref, e.workflow_status,
		e.request_count, e.input_event_count, e.retention_until, e.generated_at, e.artifact_deleted_at,
		ARRAY(SELECT m.item_id FROM integration_exchange_members m WHERE m.exchange_record_id = e.id ORDER BY m.item_id),
		COALESCE(d.delivery_count, 0), d.last_delivery_at,
		r.id, r.result_status, r.artifact_sha256, r.checked_at, r.warning_codes, r.import_job_id, r.deep_link
		FROM integration_exchanges e
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS delivery_count, MAX(delivered_at) AS last_delivery_at
			FROM integration_exchange_deliveries WHERE exchange_record_id = e.id
		) d ON TRUE
		LEFT JOIN LATERAL (
			SELECT id, result_status, artifact_sha256, checked_at, warning_codes, import_job_id, deep_link
			FROM integration_exchange_receipts WHERE exchange_record_id = e.id
			ORDER BY received_at DESC, id DESC LIMIT 1
		) r ON TRUE
		WHERE e.org_id = $1 ORDER BY e.created_at DESC, e.id DESC LIMIT $2 OFFSET $3`, orgID, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Query failed"})
		return
	}
	defer rows.Close()
	exchanges := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var exchangeID, downstream, sourceInstance, schemaVersion, manifestHash, artifactHash, artifactRef, workflowStatus string
		var requestCount, inputEventCount, deliveryCount int
		var itemIDs []int64
		var retentionUntil, artifactDeletedAt, lastDeliveryAt sql.NullTime
		var generatedAt time.Time
		var receiptID sql.NullInt64
		var receiptStatus, receiptArtifactHash, importJobID, deepLink sql.NullString
		var receiptCheckedAt sql.NullTime
		var warningCodes []byte
		if err := rows.Scan(&id, &exchangeID, &downstream, &sourceInstance, &schemaVersion, &manifestHash, &artifactHash, &artifactRef,
			&workflowStatus, &requestCount, &inputEventCount, &retentionUntil, &generatedAt, &artifactDeletedAt, &itemIDs,
			&deliveryCount, &lastDeliveryAt, &receiptID, &receiptStatus, &receiptArtifactHash, &receiptCheckedAt, &warningCodes, &importJobID, &deepLink); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Scan failed"})
			return
		}
		downstreamStatus := "not_received"
		if receiptID.Valid {
			downstreamStatus = receiptStatus.String
			if receiptArtifactHash.String != artifactHash {
				downstreamStatus = "receipt_outdated"
			}
		}
		exchanges = append(exchanges, map[string]any{
			"id": id, "exchange_id": exchangeID, "downstream_system": downstream, "source_instance": sourceInstance,
			"schema_version": schemaVersion, "approval_manifest_sha256": manifestHash, "artifact_sha256": artifactHash,
			"artifact_ref": artifactRef, "workflow_status": workflowStatus, "request_count": requestCount,
			"input_event_count": inputEventCount, "item_ids": itemIDs, "retention_until": nullableTime(retentionUntil), "generated_at": generatedAt,
			"artifact_deleted_at": nullableTime(artifactDeletedAt), "delivery_count": deliveryCount,
			"last_delivery_at": nullableTime(lastDeliveryAt), "receipt_status": downstreamStatus,
			"receipt_checked_at": nullableTime(receiptCheckedAt), "warning_codes": decodeStringArray(warningCodes),
			"import_job_id": nullableDBString(importJobID), "deep_link": nullableDBString(deepLink),
			"safety": map[string]bool{"raw_prompt": false, "raw_response": false, "credentials": false, "raw_ip": false, "raw_upstream_error": false},
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Rows failed"})
		return
	}
	h.recordAuditForOrgProjectFromRequest(r, orgID, 0, "data_exchange.ledger.view", "integration_exchange", "list", map[string]any{"page": page, "page_size": pageSize})
	writeJSON(w, http.StatusOK, map[string]any{"data": exchanges, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateDataExchange(w http.ResponseWriter, r *http.Request) {
	orgID, ok := dataExchangeOrgID(w, r)
	if !ok {
		return
	}
	var req createExchangeRequest
	if !decodeDataExchangeJSON(w, r, &req) || !validateCreateExchangeRequest(w, &req) {
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())
	var approvedCount int
	if err := tx.QueryRow(r.Context(), `SELECT COUNT(*) FROM integration_exchange_items
		WHERE org_id = $1 AND id = ANY($2::bigint[]) AND approval_status = 'approved' AND request_id IS NOT NULL`, orgID, req.ItemIDs).Scan(&approvedCount); err != nil || approvedCount != len(req.ItemIDs) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "All exchange items must be approved and sealed to request IDs"})
		return
	}
	var id int64
	userID := middleware.GetUserIDFromContext(r.Context())
	if err := tx.QueryRow(r.Context(), `INSERT INTO integration_exchanges
		(org_id, exchange_id, downstream_system, source_instance, schema_version, approval_manifest_sha256,
		 artifact_sha256, artifact_ref, workflow_status, request_count, input_event_count, retention_until, generated_by, generated_at)
		VALUES ($1, $2, 'x-insight', $3, $4, $5, $6, $7, 'generated', $8, $9, $10, NULLIF($11, 0), $12)
		RETURNING id`, orgID, req.ExchangeID, req.SourceInstance, req.SchemaVersion, req.ApprovalManifestSHA256,
		req.ArtifactSHA256, req.ArtifactRef, req.RequestCount, req.InputEventCount, req.RetentionUntil, userID, req.GeneratedAt).Scan(&id); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Exchange already exists or could not be created"})
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO integration_exchange_members (exchange_record_id, item_id)
		SELECT $1, unnest($2::bigint[])`, id, req.ItemIDs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Could not attach approved items"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Commit failed"})
		return
	}
	h.recordAuditForOrgProjectFromRequest(r, orgID, 0, "data_exchange.exchange.create", "integration_exchange", strconv.FormatInt(id, 10), map[string]any{
		"after": map[string]any{"exchange_id": req.ExchangeID, "artifact_sha256": req.ArtifactSHA256, "item_ids": req.ItemIDs, "workflow_status": "generated"},
	})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "exchange_id": req.ExchangeID})
}

func (h *Handler) GetDataExchangeActivity(w http.ResponseWriter, r *http.Request) {
	orgID, ok := dataExchangeOrgID(w, r)
	if !ok {
		return
	}
	id, ok := dataExchangePathID(w, r)
	if !ok {
		return
	}
	var exchangeID, artifactHash string
	if err := h.db.QueryRow(r.Context(), `SELECT exchange_id, artifact_sha256 FROM integration_exchanges WHERE id = $1 AND org_id = $2`, id, orgID).Scan(&exchangeID, &artifactHash); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Exchange not found"})
		return
	}

	memberRows, err := h.db.Query(r.Context(), `SELECT i.id, i.request_id, i.scenario_label, i.approval_mode, i.approval_status
		FROM integration_exchange_members m JOIN integration_exchange_items i ON i.id = m.item_id
		WHERE m.exchange_record_id = $1 ORDER BY i.id`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Member query failed"})
		return
	}
	members := make([]map[string]any, 0)
	for memberRows.Next() {
		var itemID int64
		var requestID sql.NullString
		var scenario, mode, status string
		if err := memberRows.Scan(&itemID, &requestID, &scenario, &mode, &status); err != nil {
			memberRows.Close()
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Member scan failed"})
			return
		}
		members = append(members, map[string]any{"id": itemID, "request_id": nullableDBString(requestID), "scenario_label": scenario, "approval_mode": mode, "approval_status": status})
	}
	if err := memberRows.Err(); err != nil {
		memberRows.Close()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Member rows failed"})
		return
	}
	memberRows.Close()

	deliveryRows, err := h.db.Query(r.Context(), `SELECT id, action, handoff_ref, note, delivered_by, delivered_at
		FROM integration_exchange_deliveries WHERE exchange_record_id = $1 ORDER BY delivered_at DESC, id DESC`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Delivery query failed"})
		return
	}
	deliveries := make([]map[string]any, 0)
	for deliveryRows.Next() {
		var deliveryID int64
		var action, handoffRef, note string
		var deliveredBy sql.NullInt64
		var deliveredAt time.Time
		if err := deliveryRows.Scan(&deliveryID, &action, &handoffRef, &note, &deliveredBy, &deliveredAt); err != nil {
			deliveryRows.Close()
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Delivery scan failed"})
			return
		}
		deliveries = append(deliveries, map[string]any{"id": deliveryID, "action": action, "handoff_ref": handoffRef, "note": note, "delivered_by": nullableSQLInt64(deliveredBy), "delivered_at": deliveredAt})
	}
	if err := deliveryRows.Err(); err != nil {
		deliveryRows.Close()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Delivery rows failed"})
		return
	}
	deliveryRows.Close()

	receiptRows, err := h.db.Query(r.Context(), `SELECT id, receipt_schema_version, artifact_sha256, result_status,
		request_ids, input_events, accepted, duplicate_noop, rerun_duplicate_noop, rejected, quarantined,
		capture_gap_events, persisted_inserted, persisted_duplicate_noop, persistence_conflicts,
		readback_events, readback_capture_gaps, warning_codes, import_job_id, deep_link, checked_at, received_at
		FROM integration_exchange_receipts WHERE exchange_record_id = $1 ORDER BY received_at DESC, id DESC`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Receipt query failed"})
		return
	}
	receipts := make([]map[string]any, 0)
	for receiptRows.Next() {
		var receiptID int64
		var schemaVersion, receiptArtifactHash, resultStatus string
		var requestIDs, inputEvents, accepted, duplicateNoop, rerunDuplicateNoop, rejected, quarantined int
		var captureGaps, persistedInserted, persistedDuplicateNoop, conflicts, readbackEvents, readbackGaps int
		var warningCodes []byte
		var importJobID, deepLink sql.NullString
		var checkedAt, receivedAt time.Time
		if err := receiptRows.Scan(&receiptID, &schemaVersion, &receiptArtifactHash, &resultStatus,
			&requestIDs, &inputEvents, &accepted, &duplicateNoop, &rerunDuplicateNoop, &rejected, &quarantined,
			&captureGaps, &persistedInserted, &persistedDuplicateNoop, &conflicts, &readbackEvents, &readbackGaps,
			&warningCodes, &importJobID, &deepLink, &checkedAt, &receivedAt); err != nil {
			receiptRows.Close()
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Receipt scan failed"})
			return
		}
		summaryStatus := resultStatus
		if receiptArtifactHash != artifactHash {
			summaryStatus = "receipt_outdated"
		}
		receipts = append(receipts, map[string]any{
			"id": receiptID, "schema_version": schemaVersion, "artifact_sha256": receiptArtifactHash,
			"result_status": resultStatus, "receipt_status": summaryStatus, "request_ids": requestIDs,
			"input_events": inputEvents, "accepted": accepted, "duplicate_noop": duplicateNoop,
			"rerun_duplicate_noop": rerunDuplicateNoop, "rejected": rejected, "quarantined": quarantined,
			"capture_gap_events": captureGaps, "persisted_inserted": persistedInserted,
			"persisted_duplicate_noop": persistedDuplicateNoop, "persistence_conflicts": conflicts,
			"readback_events": readbackEvents, "readback_capture_gaps": readbackGaps,
			"warning_codes": decodeStringArray(warningCodes), "import_job_id": nullableDBString(importJobID),
			"deep_link": nullableDBString(deepLink), "checked_at": checkedAt, "received_at": receivedAt,
		})
	}
	if err := receiptRows.Err(); err != nil {
		receiptRows.Close()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Receipt rows failed"})
		return
	}
	receiptRows.Close()

	h.recordAuditForOrgProjectFromRequest(r, orgID, 0, "data_exchange.activity.view", "integration_exchange", strconv.FormatInt(id, 10), map[string]any{"exchange_id": exchangeID})
	writeJSON(w, http.StatusOK, map[string]any{"exchange_id": exchangeID, "members": members, "deliveries": deliveries, "receipts": receipts})
}

func (h *Handler) CreateDataExchangeDelivery(w http.ResponseWriter, r *http.Request) {
	h.mutateDataExchangeDelivery(w, r)
}

func (h *Handler) mutateDataExchangeDelivery(w http.ResponseWriter, r *http.Request) {
	orgID, ok := dataExchangeOrgID(w, r)
	if !ok {
		return
	}
	id, ok := dataExchangePathID(w, r)
	if !ok {
		return
	}
	var req createDeliveryRequest
	if !decodeDataExchangeJSON(w, r, &req) {
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	req.HandoffRef = strings.TrimSpace(req.HandoffRef)
	req.Note = strings.TrimSpace(req.Note)
	if !oneOf(req.Action, "handed_off", "redelivered") || !validDataExchangeRef(req.HandoffRef) || len(req.Note) > 500 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid delivery"})
		return
	}
	if req.DeliveredAt.IsZero() {
		req.DeliveredAt = time.Now().UTC()
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())
	var exchangeID string
	if err := tx.QueryRow(r.Context(), `SELECT exchange_id FROM integration_exchanges
		WHERE id = $1 AND org_id = $2 AND workflow_status <> 'artifact_deleted' FOR UPDATE`, id, orgID).Scan(&exchangeID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Active exchange not found"})
		return
	}
	var deliveryID int64
	if err := tx.QueryRow(r.Context(), `INSERT INTO integration_exchange_deliveries
		(exchange_record_id, action, handoff_ref, note, delivered_by, delivered_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, 0), $6) RETURNING id`, id, req.Action, req.HandoffRef, req.Note,
		middleware.GetUserIDFromContext(r.Context()), req.DeliveredAt).Scan(&deliveryID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Delivery insert failed"})
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE integration_exchanges SET workflow_status = 'handed_off', updated_at = NOW() WHERE id = $1`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Exchange update failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Commit failed"})
		return
	}
	h.recordAuditForOrgProjectFromRequest(r, orgID, 0, "data_exchange."+req.Action, "integration_exchange", strconv.FormatInt(id, 10), map[string]any{
		"exchange_id": exchangeID, "handoff_ref": req.HandoffRef, "delivery_id": deliveryID,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"id": deliveryID, "workflow_status": "handed_off"})
}

func (h *Handler) DeleteDataExchangeArtifact(w http.ResponseWriter, r *http.Request) {
	orgID, ok := dataExchangeOrgID(w, r)
	if !ok {
		return
	}
	id, ok := dataExchangePathID(w, r)
	if !ok {
		return
	}
	var exchangeID, artifactHash, artifactRef string
	err := h.db.QueryRow(r.Context(), `UPDATE integration_exchanges
		SET workflow_status = 'artifact_deleted', artifact_deleted_by = NULLIF($1, 0), artifact_deleted_at = NOW(), updated_at = NOW()
		WHERE id = $2 AND org_id = $3 AND workflow_status <> 'artifact_deleted'
		RETURNING exchange_id, artifact_sha256, artifact_ref`, middleware.GetUserIDFromContext(r.Context()), id, orgID).Scan(&exchangeID, &artifactHash, &artifactRef)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Active exchange not found"})
		return
	}
	h.recordAuditForOrgProjectFromRequest(r, orgID, 0, "data_exchange.artifact.mark_deleted", "integration_exchange", strconv.FormatInt(id, 10), map[string]any{
		"exchange_id": exchangeID, "artifact_sha256": artifactHash, "artifact_ref": artifactRef,
		"after": map[string]any{"workflow_status": "artifact_deleted"},
	})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "workflow_status": "artifact_deleted", "filesystem_deleted": false})
}

func (h *Handler) CreateDataExchangeReceipt(w http.ResponseWriter, r *http.Request) {
	orgID, ok := dataExchangeOrgID(w, r)
	if !ok {
		return
	}
	id, ok := dataExchangePathID(w, r)
	if !ok {
		return
	}
	var req createReceiptRequest
	if !decodeDataExchangeJSON(w, r, &req) || !validateReceiptRequest(w, &req) {
		return
	}
	var exchangeID, currentArtifactHash, sourceInstance, schemaVersion, manifestHash string
	if err := h.db.QueryRow(r.Context(), `SELECT exchange_id, artifact_sha256, source_instance, schema_version, approval_manifest_sha256 FROM integration_exchanges WHERE id = $1 AND org_id = $2`, id, orgID).Scan(&exchangeID, &currentArtifactHash, &sourceInstance, &schemaVersion, &manifestHash); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Exchange not found"})
		return
	}
	if req.ExchangeID != exchangeID {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Receipt exchange_id does not match the ledger exchange"})
		return
	}
	if req.SourceSystem != "unirouter" || req.SourceInstance != sourceInstance || req.ArtifactSchemaVersion != schemaVersion || (req.ApprovalManifestSHA256 != "" && req.ApprovalManifestSHA256 != manifestHash) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Receipt source, schema, or approval manifest does not match the ledger exchange"})
		return
	}
	warningsJSON, _ := json.Marshal(req.WarningCodes)
	var receiptID int64
	err := h.db.QueryRow(r.Context(), `INSERT INTO integration_exchange_receipts
		(exchange_record_id, receipt_schema_version, source_system, source_instance, artifact_schema_version,
		 artifact_sha256, approval_manifest_sha256, result_status, request_ids, input_events, accepted,
		 duplicate_noop, rerun_duplicate_noop, rejected, quarantined, capture_gap_events, persisted_inserted,
		 persisted_duplicate_noop, persistence_conflicts, readback_events, readback_capture_gaps,
		 warning_codes, import_job_id, deep_link, checked_at, received_by)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, $10, $11, $12, $13, $14, $15, $16,
		 $17, $18, $19, $20, $21, $22::jsonb, NULLIF($23, ''), NULLIF($24, ''), $25, NULLIF($26, 0))
		RETURNING id`, id, req.ReceiptSchemaVersion, req.SourceSystem, req.SourceInstance, req.ArtifactSchemaVersion,
		req.ArtifactSHA256, req.ApprovalManifestSHA256, req.ResultStatus, req.RequestIDs, req.InputEvents, req.Accepted,
		req.DuplicateNoop, req.RerunDuplicateNoop, req.Rejected, req.Quarantined, req.CaptureGapEvents, req.PersistedInserted,
		req.PersistedDuplicateNoop, req.PersistenceConflicts, req.ReadbackEvents, req.ReadbackCaptureGaps,
		string(warningsJSON), req.ImportJobID, req.DeepLink, req.CheckedAt, middleware.GetUserIDFromContext(r.Context())).Scan(&receiptID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Receipt already exists or could not be recorded"})
		return
	}
	summaryStatus := req.ResultStatus
	if req.ArtifactSHA256 != currentArtifactHash {
		summaryStatus = "receipt_outdated"
	}
	h.recordAuditForOrgProjectFromRequest(r, orgID, 0, "data_exchange.receipt.record", "integration_exchange", strconv.FormatInt(id, 10), map[string]any{
		"exchange_id": exchangeID, "receipt_id": receiptID, "result_status": req.ResultStatus,
		"summary_status": summaryStatus, "artifact_sha256": req.ArtifactSHA256, "warning_codes": req.WarningCodes,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"id": receiptID, "receipt_status": summaryStatus})
}

func dataExchangeOrgID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	orgID := middleware.GetOrgIDFromContext(r.Context())
	if orgID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Organization context is required"})
		return 0, false
	}
	return orgID, true
}

func dataExchangePathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid id"})
		return 0, false
	}
	return id, true
}

func decodeDataExchangeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
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

func validateExchangeItemRequest(w http.ResponseWriter, req *createExchangeItemRequest) bool {
	req.ApprovalMode = strings.TrimSpace(req.ApprovalMode)
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.ScenarioLabel = strings.TrimSpace(req.ScenarioLabel)
	req.SourceInstance = strings.TrimSpace(req.SourceInstance)
	req.Reason = strings.TrimSpace(req.Reason)
	req.AuthorizedOperator = strings.TrimSpace(req.AuthorizedOperator)
	if !oneOf(req.ApprovalMode, "exact_request", "controlled_scenario") || req.ScenarioLabel == "" || req.SourceInstance == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "approval_mode, scenario_label, and source_instance are required"})
		return false
	}
	if req.ApprovalMode == "exact_request" && req.RequestID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request_id is required for exact_request approval"})
		return false
	}
	if len(req.RequestID) > 128 || len(req.ScenarioLabel) > 120 || len(req.SourceInstance) > 160 || len(req.Reason) > 500 || len(req.AuthorizedOperator) > 160 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "One or more fields are too long"})
		return false
	}
	if len(req.DataClasses) == 0 || len(req.DataClasses) > 16 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "data_classes are required"})
		return false
	}
	seen := map[string]bool{}
	for i, class := range req.DataClasses {
		class = strings.TrimSpace(class)
		if !dataExchangeClasses[class] || seen[class] {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unsupported or duplicate data class"})
			return false
		}
		seen[class] = true
		req.DataClasses[i] = class
	}
	return true
}

func validateCreateExchangeRequest(w http.ResponseWriter, req *createExchangeRequest) bool {
	req.ExchangeID = strings.TrimSpace(req.ExchangeID)
	req.SourceInstance = strings.TrimSpace(req.SourceInstance)
	req.SchemaVersion = strings.TrimSpace(req.SchemaVersion)
	req.ApprovalManifestSHA256 = strings.TrimSpace(req.ApprovalManifestSHA256)
	req.ArtifactSHA256 = strings.TrimSpace(req.ArtifactSHA256)
	req.ArtifactRef = strings.TrimSpace(req.ArtifactRef)
	if !dataExchangeIDPattern.MatchString(req.ExchangeID) || req.SourceInstance == "" || req.SchemaVersion == "" || len(req.SourceInstance) > 160 || len(req.SchemaVersion) > 80 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid exchange identity"})
		return false
	}
	if !dataExchangeSHA256Pattern.MatchString(req.ApprovalManifestSHA256) || !dataExchangeSHA256Pattern.MatchString(req.ArtifactSHA256) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Manifest and artifact SHA-256 values are required"})
		return false
	}
	if !validDataExchangeRef(req.ArtifactRef) || len(req.ItemIDs) == 0 || req.RequestCount != len(req.ItemIDs) || req.InputEventCount < req.RequestCount || req.GeneratedAt.IsZero() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid artifact ledger fields"})
		return false
	}
	seen := map[int64]bool{}
	for _, id := range req.ItemIDs {
		if id <= 0 || seen[id] {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "item_ids must be unique positive integers"})
			return false
		}
		seen[id] = true
	}
	return true
}

func validateReceiptRequest(w http.ResponseWriter, req *createReceiptRequest) bool {
	req.ExchangeID = strings.TrimSpace(req.ExchangeID)
	req.ReceiptSchemaVersion = strings.TrimSpace(req.ReceiptSchemaVersion)
	req.SourceSystem = strings.TrimSpace(req.SourceSystem)
	req.SourceInstance = strings.TrimSpace(req.SourceInstance)
	req.ArtifactSchemaVersion = strings.TrimSpace(req.ArtifactSchemaVersion)
	req.ArtifactSHA256 = strings.TrimSpace(req.ArtifactSHA256)
	req.ApprovalManifestSHA256 = strings.TrimSpace(req.ApprovalManifestSHA256)
	req.ResultStatus = strings.TrimSpace(req.ResultStatus)
	req.ImportJobID = strings.TrimSpace(req.ImportJobID)
	req.DeepLink = strings.TrimSpace(req.DeepLink)
	if !dataExchangeIDPattern.MatchString(req.ExchangeID) || req.ReceiptSchemaVersion != "xinsight.import-receipt.v1" || req.SourceSystem == "" || req.SourceInstance == "" || req.ArtifactSchemaVersion == "" || !oneOf(req.ResultStatus, "validated", "persisted", "rejected") || req.CheckedAt.IsZero() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid receipt identity or status"})
		return false
	}
	if !dataExchangeSHA256Pattern.MatchString(req.ArtifactSHA256) || (req.ApprovalManifestSHA256 != "" && !dataExchangeSHA256Pattern.MatchString(req.ApprovalManifestSHA256)) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid receipt SHA-256"})
		return false
	}
	counts := []int{req.RequestIDs, req.InputEvents, req.Accepted, req.DuplicateNoop, req.RerunDuplicateNoop, req.Rejected, req.Quarantined, req.CaptureGapEvents, req.PersistedInserted, req.PersistedDuplicateNoop, req.PersistenceConflicts, req.ReadbackEvents, req.ReadbackCaptureGaps}
	for _, count := range counts {
		if count < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Receipt counts must be non-negative"})
			return false
		}
	}
	if len(req.WarningCodes) > 64 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Too many warning codes"})
		return false
	}
	for i, code := range req.WarningCodes {
		code = strings.TrimSpace(code)
		if code == "" || len(code) > 120 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid warning code"})
			return false
		}
		req.WarningCodes[i] = code
	}
	if (req.ImportJobID == "") != (req.DeepLink == "") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "import_job_id and deep_link must be supplied together"})
		return false
	}
	if req.DeepLink != "" {
		u, err := url.Parse(req.DeepLink)
		if err != nil || u.Scheme != "https" || u.Host == "" || len(req.DeepLink) > 500 || len(req.ImportJobID) > 160 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "deep_link must be a valid HTTPS URL"})
			return false
		}
	}
	return true
}

func validDataExchangeRef(value string) bool {
	return value != "" && len(value) <= 255 && !filepath.IsAbs(value) && !strings.Contains(value, "..") && !strings.Contains(value, "://") && !strings.ContainsAny(value, "\r\n\x00")
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func nullableDBString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func nullableSQLInt64(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func decodeStringArray(value []byte) []string {
	if len(value) == 0 {
		return []string{}
	}
	var result []string
	if json.Unmarshal(value, &result) != nil {
		return []string{}
	}
	return result
}
