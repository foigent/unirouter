package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/middleware"
)

func (h *Handler) GetWallet(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT org_id, balance, granted_balance, frozen, total_spent, currency, auto_freeze, low_balance_threshold_cents, created_at, updated_at
		 FROM wallets ORDER BY org_id ASC`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	if rows.Next() {
		var orgID int64
		var balanceCents, grantedCents, frozenCents, totalSpentCents, lowThreshold int
		var currency string
		var autoFreeze bool
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&orgID, &balanceCents, &grantedCents, &frozenCents, &totalSpentCents, &currency, &autoFreeze, &lowThreshold, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}

		role := middleware.GetUserRoleFromContext(r.Context())
		isAdmin := role == "admin" || role == "platform_admin"

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"id":                          orgID,
			"org_id":                      orgID,
			"balance_cents":               balanceCents,
			"granted_cents":               grantedCents,
			"granted_balance_cents":       grantedCents,
			"frozen_cents":                frozenCents,
			"threshold_cents":             lowThreshold,
			"currency":                    currency,
			"auto_freeze":                 autoFreeze,
			"low_balance_threshold_cents": lowThreshold,
			"total_spent_cents":           totalSpentCents,
			"is_admin":                    isAdmin,
			"created_at":                  createdAt,
			"updated_at":                  updatedAt,
		})
		return
	}

	writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Wallet not found"})
}

func (h *Handler) TopUp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AmountCents     int    `json:"amount_cents"`
		Description     string `json:"description"`
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
	if req.Description == "" {
		req.Description = "Admin top-up"
	}

	// Get first wallet (simplified - single org)
	var walletOrgID int64
	err := h.db.QueryRow(r.Context(), `SELECT org_id FROM wallets ORDER BY org_id ASC LIMIT 1`).Scan(&walletOrgID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Wallet not found"})
		return
	}

	// Idempotency check: if client_request_id is provided, check for existing transaction
	if req.ClientRequestID != "" {
		var existingID int64
		err := h.db.QueryRow(r.Context(),
			`SELECT id FROM transactions WHERE request_id = $1 AND org_id = $2`,
			req.ClientRequestID, walletOrgID,
		).Scan(&existingID)
		if err == nil {
			// Transaction already exists - return conflict
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":             "duplicate_request",
				"existing_id":       existingID,
				"client_request_id": req.ClientRequestID,
			})
			return
		}
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())

	_, err = tx.Exec(r.Context(),
		`UPDATE wallets SET balance = balance + $1, updated_at = NOW() WHERE org_id = $2`,
		req.AmountCents, walletOrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update wallet"})
		return
	}

	requestID := req.ClientRequestID
	if requestID == "" {
		requestID = fmt.Sprintf("topup-%d", time.Now().UnixNano())
	}
	_, err = tx.Exec(r.Context(),
		`INSERT INTO transactions (request_id, org_id, type, status, amount, note, created_at)
		 VALUES ($1, $2, 'topup', 'completed', $3, $4, NOW())`,
		requestID, walletOrgID, req.AmountCents, req.Description)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to record transaction"})
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction commit failed"})
		return
	}

	r = r.WithContext(context.WithValue(r.Context(), middleware.CtxRequestID, requestID))
	h.recordAuditForOrgProjectFromRequest(r, walletOrgID, 0, "wallet.topup", "wallet", fmt.Sprintf("%d", walletOrgID), map[string]interface{}{
		"amount_cents": req.AmountCents,
		"description":  req.Description,
		"after": map[string]interface{}{
			"amount_cents": req.AmountCents,
			"description":  req.Description,
		},
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Top-up successful"})
}

// BalanceAdjust sets the wallet balance to a specific amount (admin operation).
func (h *Handler) BalanceAdjust(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}

	var req struct {
		TargetBalance int    `json:"target_balance"`
		Description   string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	if req.TargetBalance < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "target_balance must be >= 0"})
		return
	}
	if req.Description == "" {
		req.Description = "Admin balance adjustment"
	}

	var walletOrgID int64
	var currentBalance int
	err := h.db.QueryRow(r.Context(),
		`SELECT org_id, balance FROM wallets ORDER BY org_id ASC LIMIT 1`,
	).Scan(&walletOrgID, &currentBalance)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Wallet not found"})
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())

	_, err = tx.Exec(r.Context(),
		`UPDATE wallets SET balance = $1, updated_at = NOW() WHERE org_id = $2`,
		req.TargetBalance, walletOrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update wallet"})
		return
	}

	adjustAmount := req.TargetBalance - currentBalance
	requestID := fmt.Sprintf("adjust-%d", time.Now().UnixNano())
	_, err = tx.Exec(r.Context(),
		`INSERT INTO transactions (request_id, org_id, type, status, amount, note, created_at)
		 VALUES ($1, $2, 'adjust', 'completed', $3, $4, NOW())`,
		requestID, walletOrgID, adjustAmount, req.Description)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to record transaction"})
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction commit failed"})
		return
	}

	r = r.WithContext(context.WithValue(r.Context(), middleware.CtxRequestID, requestID))
	h.recordAuditForOrgProjectFromRequest(r, walletOrgID, 0, "wallet.adjust", "wallet", fmt.Sprintf("%d", walletOrgID), map[string]interface{}{
		"from":        currentBalance,
		"to":          req.TargetBalance,
		"description": req.Description,
		"before": map[string]interface{}{
			"balance_cents": currentBalance,
		},
		"after": map[string]interface{}{
			"balance_cents": req.TargetBalance,
		},
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"previous":      currentBalance,
		"new_balance":   req.TargetBalance,
		"adjust_amount": adjustAmount,
	})
}
func (h *Handler) ListWalletTransactions(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var total int
	h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM transactions`).Scan(&total)

	rows, err := h.db.Query(r.Context(),
		`SELECT id, type, amount, note, created_at
		 FROM transactions ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	txns := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var txnType, note string
		var amountCents int
		var createdAt time.Time
		rows.Scan(&id, &txnType, &amountCents, &note, &createdAt)
		txns = append(txns, map[string]interface{}{
			"id": id, "type": txnType, "amount_cents": amountCents,
			"description": note, "created_at": createdAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": txns, "total": total})
}

func (h *Handler) ExportWalletTransactions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT t.id, t.type, t.amount, t.balance_after, t.note, t.request_id, t.created_at
		 FROM transactions t
		 ORDER BY t.created_at DESC LIMIT 10000`,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=wallet_transactions.csv")
	fmt.Fprint(w, "\xEF\xBB\xBF") // UTF-8 BOM for Excel
	fmt.Fprintln(w, "ID,类型,金额(分),余额(分),描述,引用ID,时间")

	for rows.Next() {
		var id int64
		var txType, description, referenceID string
		var amount, balanceAfter int
		var createdAt time.Time
		rows.Scan(&id, &txType, &amount, &balanceAfter, &description, &referenceID, &createdAt)
		fmt.Fprintf(w, "%d,%s,%d,%d,%s,%s,%s\n",
			id, txType, amount, balanceAfter, description, referenceID, createdAt.Format(time.RFC3339))
	}
}
