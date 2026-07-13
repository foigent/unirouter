package admin

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
)

const maxWalletIntegerCents = int64(1<<31 - 1)

type walletTransactionRecord struct {
	ID           int64
	Amount       int
	BalanceAfter int
	Note         string
}

func decodeWalletMutation(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return false
	}
	return true
}

func normalizeWalletMutationIdentity(clientRequestID, description string) (string, string, error) {
	clientRequestID = strings.TrimSpace(clientRequestID)
	description = strings.TrimSpace(description)
	if clientRequestID == "" {
		return "", "", errors.New("client_request_id is required")
	}
	if len(clientRequestID) > 128 {
		return "", "", errors.New("client_request_id must not exceed 128 characters")
	}
	if description == "" {
		return "", "", errors.New("description is required")
	}
	return clientRequestID, description, nil
}

func (h *Handler) findWalletTransaction(ctx context.Context, orgID int64, transactionType, clientRequestID string) (walletTransactionRecord, error) {
	var record walletTransactionRecord
	err := h.db.QueryRow(ctx,
		`SELECT id, amount, balance_after, note
		 FROM transactions
		 WHERE org_id = $1 AND type = $2 AND client_request_id = $3`,
		orgID, transactionType, clientRequestID,
	).Scan(&record.ID, &record.Amount, &record.BalanceAfter, &record.Note)
	return record, err
}

func walletTransactionMatches(record walletTransactionRecord, amount int, description string) bool {
	return record.Amount == amount && record.Note == description
}

func writeWalletIdempotencyConflict(w http.ResponseWriter, clientRequestID string) {
	writeJSON(w, http.StatusConflict, map[string]interface{}{
		"error":             "idempotency_key_reused",
		"client_request_id": clientRequestID,
	})
}

func (h *Handler) GetWallet(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}

	var balanceCents, grantedCents, frozenCents, totalSpentCents, lowThreshold int
	var currency string
	var autoFreeze bool
	var createdAt, updatedAt time.Time
	rows, err := h.db.Query(r.Context(),
		`SELECT org_id, balance, granted_balance, frozen, total_spent, currency, auto_freeze, low_balance_threshold_cents, created_at, updated_at
		 FROM wallets WHERE org_id = $1`, orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Wallet not found"})
		return
	}
	if err := rows.Scan(&orgID, &balanceCents, &grantedCents, &frozenCents, &totalSpentCents, &currency, &autoFreeze, &lowThreshold, &createdAt, &updatedAt); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
		return
	}

	role := actorRole(r.Context())
	canManage := role == auth.RolePlatformAdmin || role == auth.RoleBillingAdmin

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
		"is_admin":                    canManage,
		"can_top_up":                  canManage,
		"can_adjust":                  canManage,
		"can_export_transactions":     canManage,
		"can_select_org":              role == auth.RolePlatformAdmin,
		"created_at":                  createdAt,
		"updated_at":                  updatedAt,
	})
}

func (h *Handler) TopUp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID           int64  `json:"org_id"`
		AmountCents     int    `json:"amount_cents"`
		Description     string `json:"description"`
		ClientRequestID string `json:"client_request_id"`
	}
	if !decodeWalletMutation(w, r, &req) {
		return
	}

	if req.AmountCents <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "amount_cents must be positive"})
		return
	}
	if int64(req.AmountCents) > maxWalletIntegerCents {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "amount_cents is out of range"})
		return
	}

	walletOrgID, err := resolveActorOrganization(r.Context(), req.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	req.ClientRequestID, req.Description, err = normalizeWalletMutationIdentity(req.ClientRequestID, req.Description)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	r, operationID, ok := ensureOperationIdentityForHandler(w, r)
	if !ok {
		return
	}

	existing, err := h.findWalletTransaction(r.Context(), walletOrgID, "topup", req.ClientRequestID)
	if err == nil {
		if !walletTransactionMatches(existing, req.AmountCents, req.Description) {
			writeWalletIdempotencyConflict(w, req.ClientRequestID)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message":           "Top-up successful",
			"org_id":            walletOrgID,
			"balance_cents":     existing.BalanceAfter,
			"client_request_id": req.ClientRequestID,
			"idempotent_replay": true,
		})
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

	var balanceAfter int
	err = tx.QueryRow(r.Context(),
		`UPDATE wallets SET balance = balance + $1, updated_at = NOW() WHERE org_id = $2 RETURNING balance`,
		req.AmountCents, walletOrgID,
	).Scan(&balanceAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Wallet not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update wallet"})
		return
	}

	_, err = tx.Exec(r.Context(),
		`INSERT INTO transactions (request_id, client_request_id, org_id, type, status, amount, balance_after, note, created_at)
		 VALUES ($1, $2, $3, 'topup', 'completed', $4, $5, $6, NOW())`,
		operationID, req.ClientRequestID, walletOrgID, req.AmountCents, balanceAfter, req.Description)
	if err != nil {
		if isPostgresUniqueViolation(err) {
			_ = tx.Rollback(r.Context())
			existing, lookupErr := h.findWalletTransaction(r.Context(), walletOrgID, "topup", req.ClientRequestID)
			if lookupErr == nil {
				if !walletTransactionMatches(existing, req.AmountCents, req.Description) {
					writeWalletIdempotencyConflict(w, req.ClientRequestID)
					return
				}
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"message":           "Top-up successful",
					"org_id":            walletOrgID,
					"balance_cents":     existing.BalanceAfter,
					"client_request_id": req.ClientRequestID,
					"idempotent_replay": true,
				})
				return
			}
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to record transaction"})
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction commit failed"})
		return
	}

	h.recordAuditForOrgProjectFromRequest(r, walletOrgID, 0, "wallet.topup", "wallet", fmt.Sprintf("%d", walletOrgID), map[string]interface{}{
		"amount_cents":      req.AmountCents,
		"description":       req.Description,
		"client_request_id": req.ClientRequestID,
		"after": map[string]interface{}{
			"amount_cents": req.AmountCents,
			"description":  req.Description,
		},
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":           "Top-up successful",
		"org_id":            walletOrgID,
		"balance_cents":     balanceAfter,
		"client_request_id": req.ClientRequestID,
		"idempotent_replay": false,
	})
}

// BalanceAdjust applies a signed delta to the latest locked wallet balance.
func (h *Handler) BalanceAdjust(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}

	var req struct {
		OrgID           int64  `json:"org_id"`
		DeltaCents      int    `json:"delta_cents"`
		Description     string `json:"description"`
		ClientRequestID string `json:"client_request_id"`
	}
	if !decodeWalletMutation(w, r, &req) {
		return
	}

	if req.DeltaCents == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "delta_cents must not be zero"})
		return
	}
	if int64(req.DeltaCents) > maxWalletIntegerCents || int64(req.DeltaCents) < -maxWalletIntegerCents {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "delta_cents is out of range"})
		return
	}

	walletOrgID, err := resolveActorOrganization(r.Context(), req.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	req.ClientRequestID, req.Description, err = normalizeWalletMutationIdentity(req.ClientRequestID, req.Description)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	r, operationID, ok := ensureOperationIdentityForHandler(w, r)
	if !ok {
		return
	}

	existing, err := h.findWalletTransaction(r.Context(), walletOrgID, "adjust", req.ClientRequestID)
	if err == nil {
		if !walletTransactionMatches(existing, req.DeltaCents, req.Description) {
			writeWalletIdempotencyConflict(w, req.ClientRequestID)
			return
		}
		previousBalance := existing.BalanceAfter - existing.Amount
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":                true,
			"org_id":                 walletOrgID,
			"previous":               previousBalance,
			"previous_balance_cents": previousBalance,
			"new_balance":            existing.BalanceAfter,
			"balance_cents":          existing.BalanceAfter,
			"adjust_amount":          existing.Amount,
			"delta_cents":            existing.Amount,
			"client_request_id":      req.ClientRequestID,
			"idempotent_replay":      true,
		})
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

	var currentBalance int
	err = tx.QueryRow(r.Context(),
		`SELECT balance FROM wallets WHERE org_id = $1 FOR UPDATE`, walletOrgID,
	).Scan(&currentBalance)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Wallet not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read wallet"})
		return
	}
	newBalance64 := int64(currentBalance) + int64(req.DeltaCents)
	if newBalance64 < 0 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":                 "insufficient_balance",
			"current_balance_cents": currentBalance,
		})
		return
	}
	if newBalance64 > maxWalletIntegerCents {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "resulting balance is out of range"})
		return
	}
	newBalance := int(newBalance64)

	_, err = tx.Exec(r.Context(),
		`UPDATE wallets SET balance = $1, updated_at = NOW() WHERE org_id = $2`,
		newBalance, walletOrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update wallet"})
		return
	}

	_, err = tx.Exec(r.Context(),
		`INSERT INTO transactions (request_id, client_request_id, org_id, type, status, amount, balance_after, note, created_at)
		 VALUES ($1, $2, $3, 'adjust', 'completed', $4, $5, $6, NOW())`,
		operationID, req.ClientRequestID, walletOrgID, req.DeltaCents, newBalance, req.Description)
	if err != nil {
		if isPostgresUniqueViolation(err) {
			_ = tx.Rollback(r.Context())
			existing, lookupErr := h.findWalletTransaction(r.Context(), walletOrgID, "adjust", req.ClientRequestID)
			if lookupErr == nil {
				if !walletTransactionMatches(existing, req.DeltaCents, req.Description) {
					writeWalletIdempotencyConflict(w, req.ClientRequestID)
					return
				}
				previousBalance := existing.BalanceAfter - existing.Amount
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"success":                true,
					"org_id":                 walletOrgID,
					"previous":               previousBalance,
					"previous_balance_cents": previousBalance,
					"new_balance":            existing.BalanceAfter,
					"balance_cents":          existing.BalanceAfter,
					"adjust_amount":          existing.Amount,
					"delta_cents":            existing.Amount,
					"client_request_id":      req.ClientRequestID,
					"idempotent_replay":      true,
				})
				return
			}
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to record transaction"})
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction commit failed"})
		return
	}

	h.recordAuditForOrgProjectFromRequest(r, walletOrgID, 0, "wallet.adjust", "wallet", fmt.Sprintf("%d", walletOrgID), map[string]interface{}{
		"from":              currentBalance,
		"to":                newBalance,
		"delta_cents":       req.DeltaCents,
		"description":       req.Description,
		"client_request_id": req.ClientRequestID,
		"before": map[string]interface{}{
			"balance_cents": currentBalance,
		},
		"after": map[string]interface{}{
			"balance_cents": newBalance,
		},
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":                true,
		"org_id":                 walletOrgID,
		"previous":               currentBalance,
		"previous_balance_cents": currentBalance,
		"new_balance":            newBalance,
		"balance_cents":          newBalance,
		"adjust_amount":          req.DeltaCents,
		"delta_cents":            req.DeltaCents,
		"client_request_id":      req.ClientRequestID,
		"idempotent_replay":      false,
	})
}
func (h *Handler) ListWalletTransactions(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM transactions WHERE org_id = $1`, orgID).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT id, type, amount, note, created_at
		 FROM transactions WHERE org_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		orgID, pageSize, offset)
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
		if err := rows.Scan(&id, &txnType, &amountCents, &note, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		txns = append(txns, map[string]interface{}{
			"id": id, "type": txnType, "amount_cents": amountCents,
			"description": note, "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": txns, "total": total, "org_id": orgID})
}

func (h *Handler) ExportWalletTransactions(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT t.id, t.type, t.amount, t.balance_after, t.note, t.request_id, t.created_at
		 FROM transactions t
		 WHERE t.org_id = $1
		 ORDER BY t.created_at DESC LIMIT 10000`, orgID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=wallet_transactions.csv")
	fmt.Fprint(w, "\xEF\xBB\xBF") // UTF-8 BOM for Excel
	csvWriter := csv.NewWriter(w)
	if err := csvWriter.Write([]string{"ID", "类型", "金额(分)", "余额(分)", "描述", "引用ID", "时间"}); err != nil {
		return
	}

	for rows.Next() {
		var id int64
		var txType, description, referenceID string
		var amount int
		var balanceAfter *int
		var createdAt time.Time
		if err := rows.Scan(&id, &txType, &amount, &balanceAfter, &description, &referenceID, &createdAt); err != nil {
			return
		}
		balance := ""
		if balanceAfter != nil {
			balance = strconv.Itoa(*balanceAfter)
		}
		if err := csvWriter.Write([]string{
			strconv.FormatInt(id, 10),
			neutralizeSpreadsheetFormula(txType),
			strconv.Itoa(amount),
			balance,
			neutralizeSpreadsheetFormula(description),
			neutralizeSpreadsheetFormula(referenceID),
			createdAt.Format(time.RFC3339),
		}); err != nil {
			return
		}
	}
	if rows.Err() != nil {
		return
	}
	csvWriter.Flush()
	if csvWriter.Error() != nil {
		return
	}
}

func neutralizeSpreadsheetFormula(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed == "" || !strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return value
	}
	return "'" + value
}
