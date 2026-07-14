package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/money"
)

func (h *Handler) ListGrants(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM grants WHERE org_id = $1`, orgID).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT g.id, g.org_id, o.name AS org_name, g.amount_micros, g.reason,
		        COALESCE(g.client_request_id, ''), g.created_at
		 FROM grants g LEFT JOIN orgs o ON o.id = g.org_id
		 WHERE g.org_id = $1
		 ORDER BY g.id DESC LIMIT $2 OFFSET $3`, orgID, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	grants := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgId, amountMicros int64
		var reason, clientReqId string
		var orgName *string
		var createdAt time.Time
		if err := rows.Scan(&id, &orgId, &orgName, &amountMicros, &reason, &clientReqId, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		oName := ""
		if orgName != nil {
			oName = *orgName
		}
		grants = append(grants, map[string]interface{}{
			"id": id, "org_id": orgId, "org_name": oName,
			"amount_micros": strconv.FormatInt(amountMicros, 10), "currency": "CNY", "amount_scale": money.MicrosPerCNY,
			"reason":            reason,
			"client_request_id": clientReqId, "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": grants, "total": total, "org_id": orgID})
}

func (h *Handler) CreateGrant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgId           int64  `json:"org_id"`
		AmountCents     int64  `json:"amount_cents"`
		Reason          string `json:"reason"`
		ClientRequestID string `json:"client_request_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.AmountCents <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "amount_cents must be positive"})
		return
	}
	amountMicros, err := money.CentsToMicros(req.AmountCents)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "amount_cents is out of range"})
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), req.OrgId)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	req.OrgId = orgID
	r, operationID, ok := ensureOperationIdentityForHandler(w, r)
	if !ok {
		return
	}
	if req.ClientRequestID == "" {
		req.ClientRequestID = fmt.Sprintf("grant_%d", time.Now().UnixNano())
	}

	// Idempotency check
	var existingId int64
	err = h.db.QueryRow(r.Context(),
		`SELECT id FROM grants WHERE org_id = $1 AND client_request_id = $2`, req.OrgId, req.ClientRequestID).Scan(&existingId)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"id": existingId, "duplicate": true})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Idempotency check failed"})
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())

	var balanceAfterMicros int64
	err = tx.QueryRow(r.Context(),
		`UPDATE wallets
		 SET granted_balance_micros = granted_balance_micros + $1, updated_at = NOW()
		 WHERE org_id = $2
		 RETURNING balance_micros + granted_balance_micros`,
		amountMicros, req.OrgId,
	).Scan(&balanceAfterMicros)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Wallet not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update wallet failed"})
		return
	}

	var grantId int64
	createdBy := middleware.GetUserIDFromContext(r.Context())
	err = tx.QueryRow(r.Context(),
		`INSERT INTO grants (org_id, amount_micros, balance_micros, reason, client_request_id, created_by)
		 VALUES ($1, $2, $2, $3, $4, $5) RETURNING id`,
		req.OrgId, amountMicros, req.Reason, req.ClientRequestID, createdBy).Scan(&grantId)
	if err != nil {
		if isPostgresUniqueViolation(err) {
			_ = tx.Rollback(r.Context())
			lookupErr := h.db.QueryRow(r.Context(),
				`SELECT id FROM grants WHERE org_id = $1 AND client_request_id = $2`,
				req.OrgId, req.ClientRequestID,
			).Scan(&existingId)
			if lookupErr == nil {
				writeJSON(w, http.StatusOK, map[string]interface{}{"id": existingId, "duplicate": true})
				return
			}
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Insert grant failed"})
		return
	}
	_, err = tx.Exec(r.Context(),
		`INSERT INTO transactions (org_id, type, amount_micros, balance_after_micros, note, request_id, client_request_id)
		 VALUES ($1, 'grant', $2, $3, $4, $5, $6)`,
		req.OrgId, amountMicros, balanceAfterMicros, req.Reason, operationID, req.ClientRequestID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Insert transaction failed"})
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	h.recordAuditForOrgProjectFromRequest(r, req.OrgId, 0, "wallet.grant", "grant", fmt.Sprintf("%d", grantId), map[string]interface{}{
		"amount_cents":      req.AmountCents,
		"amount_micros":     strconv.FormatInt(amountMicros, 10),
		"reason":            req.Reason,
		"created_by":        createdBy,
		"client_request_id": req.ClientRequestID,
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": grantId, "org_id": req.OrgId, "amount_micros": strconv.FormatInt(amountMicros, 10),
		"currency": "CNY", "amount_scale": money.MicrosPerCNY,
	})
}
