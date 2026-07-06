package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (h *Handler) ListGrants(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	var total int
	h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM grants`).Scan(&total)

	rows, err := h.db.Query(r.Context(),
		`SELECT g.id, g.org_id, o.name AS org_name, g.amount AS amount_cents, g.reason,
		        g.client_request_id, g.created_at
		 FROM grants g LEFT JOIN orgs o ON o.id = g.org_id
		 ORDER BY g.id DESC LIMIT $1 OFFSET $2`, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	grants := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgId, amountCents int64
		var reason, clientReqId string
		var orgName *string
		var createdAt time.Time
		rows.Scan(&id, &orgId, &orgName, &amountCents, &reason, &clientReqId, &createdAt)
		oName := ""
		if orgName != nil {
			oName = *orgName
		}
		grants = append(grants, map[string]interface{}{
			"id": id, "org_id": orgId, "org_name": oName,
			"amount_cents": amountCents, "reason": reason,
			"client_request_id": clientReqId, "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": grants, "total": total})
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
	if req.AmountCents == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "amount_cents required"})
		return
	}
	if req.OrgId == 0 {
		h.db.QueryRow(r.Context(), `SELECT id FROM orgs LIMIT 1`).Scan(&req.OrgId)
	}
	if req.ClientRequestID == "" {
		req.ClientRequestID = fmt.Sprintf("grant_%d", time.Now().UnixNano())
	}

	// Idempotency check
	var existingId int64
	err := h.db.QueryRow(r.Context(),
		`SELECT id FROM grants WHERE client_request_id = $1`, req.ClientRequestID).Scan(&existingId)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"id": existingId, "duplicate": true})
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())

	var grantId int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO grants (org_id, amount, reason, client_request_id)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		req.OrgId, req.AmountCents, req.Reason, req.ClientRequestID).Scan(&grantId)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Insert grant failed"})
		return
	}

	_, err = tx.Exec(r.Context(),
		`UPDATE wallets SET balance = balance + $1, updated_at = NOW() WHERE org_id = $2`,
		req.AmountCents, req.OrgId)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update wallet failed"})
		return
	}

	_, err = tx.Exec(r.Context(),
		`INSERT INTO transactions (org_id, type, amount, balance_after, note, request_id)
		 SELECT $1, 'grant', $2, balance, $3, $4 FROM wallets WHERE org_id = $1`,
		req.OrgId, req.AmountCents, req.Reason, req.ClientRequestID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Insert transaction failed"})
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": grantId})
}
