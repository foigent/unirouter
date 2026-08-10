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
	"github.com/uniroute/unirouter/internal/money"
)

const maxWalletIntegerCents = int64(1<<31 - 1)

type walletTransactionRecord struct {
	ID                 int64
	AmountMicros       int64
	BalanceAfterMicros int64
	Note               string
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
		`SELECT id, amount_micros, balance_after_micros, note
		 FROM transactions
		 WHERE org_id = $1 AND type = $2 AND client_request_id = $3`,
		orgID, transactionType, clientRequestID,
	).Scan(&record.ID, &record.AmountMicros, &record.BalanceAfterMicros, &record.Note)
	return record, err
}

func walletTransactionMatches(record walletTransactionRecord, amountMicros int64, description string) bool {
	return record.AmountMicros == amountMicros && record.Note == description
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

	var balanceMicros, grantedMicros, frozenMicros, totalSpentMicros int64
	var lowThresholdCents int64
	var currency, orgName string
	var walletExists, autoFreeze bool
	var createdAt, updatedAt time.Time
	rows, err := h.db.Query(r.Context(),
		`SELECT o.id, o.name, w.org_id IS NOT NULL,
		        COALESCE(w.balance_micros, 0), COALESCE(w.granted_balance_micros, 0),
		        COALESCE(w.frozen_micros, 0), COALESCE(w.total_spent_micros, 0),
		        COALESCE(w.currency, 'CNY'), COALESCE(w.auto_freeze, false),
		        COALESCE(w.low_balance_threshold_cents, 0),
		        COALESCE(w.created_at, TIMESTAMPTZ '1970-01-01 00:00:00+00'),
		        COALESCE(w.updated_at, TIMESTAMPTZ '1970-01-01 00:00:00+00')
		 FROM orgs o LEFT JOIN wallets w ON w.org_id = o.id WHERE o.id = $1`, orgID)
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
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Organization not found"})
		return
	}
	if err := rows.Scan(&orgID, &orgName, &walletExists, &balanceMicros, &grantedMicros, &frozenMicros, &totalSpentMicros, &currency, &autoFreeze, &lowThresholdCents, &createdAt, &updatedAt); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
		return
	}

	role := actorOrganizationRole(r.Context())
	canManage := role == auth.RolePlatformAdmin || role == auth.RoleBillingAdmin
	canMutateWallet := canManage && walletExists

	thresholdMicros, err := money.CentsToMicros(lowThresholdCents)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Wallet threshold is out of range"})
		return
	}
	var createdAtValue, updatedAtValue interface{}
	if walletExists {
		createdAtValue = createdAt
		updatedAtValue = updatedAt
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":                          orgID,
		"org_id":                      orgID,
		"org_name":                    orgName,
		"exists":                      walletExists,
		"balance_micros":              strconv.FormatInt(balanceMicros, 10),
		"granted_balance_micros":      strconv.FormatInt(grantedMicros, 10),
		"frozen_micros":               strconv.FormatInt(frozenMicros, 10),
		"threshold_micros":            strconv.FormatInt(thresholdMicros, 10),
		"amount_scale":                money.MicrosPerCNY,
		"currency":                    currency,
		"auto_freeze":                 autoFreeze,
		"low_balance_threshold_cents": lowThresholdCents,
		"total_spent_micros":          strconv.FormatInt(totalSpentMicros, 10),
		"is_admin":                    canManage,
		"can_top_up":                  canMutateWallet,
		"can_adjust":                  canMutateWallet,
		"can_export_transactions":     canManage,
		"can_select_org":              role == auth.RolePlatformAdmin,
		"created_at":                  createdAtValue,
		"updated_at":                  updatedAtValue,
	})
}

func (h *Handler) TopUp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID           int64  `json:"org_id"`
		AmountCents     int64  `json:"amount_cents"`
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
	if req.AmountCents > maxWalletIntegerCents {
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
	amountMicros, err := money.CentsToMicros(req.AmountCents)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "amount_cents is out of range"})
		return
	}

	existing, err := h.findWalletTransaction(r.Context(), walletOrgID, "topup", req.ClientRequestID)
	if err == nil {
		if !walletTransactionMatches(existing, amountMicros, req.Description) {
			writeWalletIdempotencyConflict(w, req.ClientRequestID)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message":        "Top-up successful",
			"org_id":         walletOrgID,
			"balance_micros": strconv.FormatInt(existing.BalanceAfterMicros, 10),
			"currency":       "CNY", "amount_scale": money.MicrosPerCNY,
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

	var balanceAfterMicros int64
	err = tx.QueryRow(r.Context(),
		`UPDATE wallets SET balance_micros = balance_micros + $1, updated_at = NOW() WHERE org_id = $2 RETURNING balance_micros`,
		amountMicros, walletOrgID,
	).Scan(&balanceAfterMicros)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Wallet not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update wallet"})
		return
	}

	_, err = tx.Exec(r.Context(),
		`INSERT INTO transactions (request_id, client_request_id, org_id, type, status, amount_micros, balance_after_micros, note, created_at)
		 VALUES ($1, $2, $3, 'topup', 'completed', $4, $5, $6, NOW())`,
		operationID, req.ClientRequestID, walletOrgID, amountMicros, balanceAfterMicros, req.Description)
	if err != nil {
		if isPostgresUniqueViolation(err) {
			_ = tx.Rollback(r.Context())
			existing, lookupErr := h.findWalletTransaction(r.Context(), walletOrgID, "topup", req.ClientRequestID)
			if lookupErr == nil {
				if !walletTransactionMatches(existing, amountMicros, req.Description) {
					writeWalletIdempotencyConflict(w, req.ClientRequestID)
					return
				}
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"message":        "Top-up successful",
					"org_id":         walletOrgID,
					"balance_micros": strconv.FormatInt(existing.BalanceAfterMicros, 10),
					"currency":       "CNY", "amount_scale": money.MicrosPerCNY,
					"client_request_id": req.ClientRequestID,
					"idempotent_replay": true,
				})
				return
			}
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to record transaction"})
		return
	}

	auditDetail := map[string]interface{}{
		"amount_cents":      req.AmountCents,
		"amount_micros":     strconv.FormatInt(amountMicros, 10),
		"description":       req.Description,
		"client_request_id": req.ClientRequestID,
		"after": map[string]interface{}{
			"amount_cents": req.AmountCents,
			"description":  req.Description,
		},
	}
	if err := insertAuditEntry(r.Context(), tx, "wallet.topup", "wallet", fmt.Sprintf("%d", walletOrgID), auditDetail,
		requestAuditMeta(r, walletOrgID, 0)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction commit failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":        "Top-up successful",
		"org_id":         walletOrgID,
		"balance_micros": strconv.FormatInt(balanceAfterMicros, 10),
		"currency":       "CNY", "amount_scale": money.MicrosPerCNY,
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
		DeltaCents      int64  `json:"delta_cents"`
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
	if req.DeltaCents > maxWalletIntegerCents || req.DeltaCents < -maxWalletIntegerCents {
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
	deltaMicros, err := money.CentsToMicros(req.DeltaCents)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "delta_cents is out of range"})
		return
	}

	existing, err := h.findWalletTransaction(r.Context(), walletOrgID, "adjust", req.ClientRequestID)
	if err == nil {
		if !walletTransactionMatches(existing, deltaMicros, req.Description) {
			writeWalletIdempotencyConflict(w, req.ClientRequestID)
			return
		}
		previousBalance, subtractErr := money.Sub(existing.BalanceAfterMicros, existing.AmountMicros)
		if subtractErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Stored transaction is out of range"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":                 true,
			"org_id":                  walletOrgID,
			"previous_balance_micros": strconv.FormatInt(previousBalance, 10),
			"balance_micros":          strconv.FormatInt(existing.BalanceAfterMicros, 10),
			"delta_micros":            strconv.FormatInt(existing.AmountMicros, 10),
			"currency":                "CNY", "amount_scale": money.MicrosPerCNY,
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

	var currentBalanceMicros int64
	err = tx.QueryRow(r.Context(),
		`SELECT balance_micros FROM wallets WHERE org_id = $1 FOR UPDATE`, walletOrgID,
	).Scan(&currentBalanceMicros)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Wallet not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read wallet"})
		return
	}
	newBalanceMicros, addErr := money.Add(currentBalanceMicros, deltaMicros)
	if addErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "resulting balance is out of range"})
		return
	}
	if newBalanceMicros < 0 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":                  "insufficient_balance",
			"current_balance_micros": strconv.FormatInt(currentBalanceMicros, 10),
		})
		return
	}

	_, err = tx.Exec(r.Context(),
		`UPDATE wallets SET balance_micros = $1, updated_at = NOW() WHERE org_id = $2`,
		newBalanceMicros, walletOrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update wallet"})
		return
	}

	_, err = tx.Exec(r.Context(),
		`INSERT INTO transactions (request_id, client_request_id, org_id, type, status, amount_micros, balance_after_micros, note, created_at)
		 VALUES ($1, $2, $3, 'adjust', 'completed', $4, $5, $6, NOW())`,
		operationID, req.ClientRequestID, walletOrgID, deltaMicros, newBalanceMicros, req.Description)
	if err != nil {
		if isPostgresUniqueViolation(err) {
			_ = tx.Rollback(r.Context())
			existing, lookupErr := h.findWalletTransaction(r.Context(), walletOrgID, "adjust", req.ClientRequestID)
			if lookupErr == nil {
				if !walletTransactionMatches(existing, deltaMicros, req.Description) {
					writeWalletIdempotencyConflict(w, req.ClientRequestID)
					return
				}
				previousBalance, subtractErr := money.Sub(existing.BalanceAfterMicros, existing.AmountMicros)
				if subtractErr != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Stored transaction is out of range"})
					return
				}
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"success":                 true,
					"org_id":                  walletOrgID,
					"previous_balance_micros": strconv.FormatInt(previousBalance, 10),
					"balance_micros":          strconv.FormatInt(existing.BalanceAfterMicros, 10),
					"delta_micros":            strconv.FormatInt(existing.AmountMicros, 10),
					"currency":                "CNY", "amount_scale": money.MicrosPerCNY,
					"client_request_id": req.ClientRequestID,
					"idempotent_replay": true,
				})
				return
			}
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to record transaction"})
		return
	}

	auditDetail := map[string]interface{}{
		"from_micros":       strconv.FormatInt(currentBalanceMicros, 10),
		"to_micros":         strconv.FormatInt(newBalanceMicros, 10),
		"delta_cents":       req.DeltaCents,
		"delta_micros":      strconv.FormatInt(deltaMicros, 10),
		"description":       req.Description,
		"client_request_id": req.ClientRequestID,
		"before": map[string]interface{}{
			"balance_micros": strconv.FormatInt(currentBalanceMicros, 10),
		},
		"after": map[string]interface{}{
			"balance_micros": strconv.FormatInt(newBalanceMicros, 10),
		},
	}
	if err := insertAuditEntry(r.Context(), tx, "wallet.adjust", "wallet", fmt.Sprintf("%d", walletOrgID), auditDetail,
		requestAuditMeta(r, walletOrgID, 0)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction commit failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":                 true,
		"org_id":                  walletOrgID,
		"previous_balance_micros": strconv.FormatInt(currentBalanceMicros, 10),
		"balance_micros":          strconv.FormatInt(newBalanceMicros, 10),
		"delta_micros":            strconv.FormatInt(deltaMicros, 10),
		"currency":                "CNY", "amount_scale": money.MicrosPerCNY,
		"client_request_id": req.ClientRequestID,
		"idempotent_replay": false,
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
		`SELECT id, type, amount_micros, note, created_at
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
		var amountMicros int64
		var createdAt time.Time
		if err := rows.Scan(&id, &txnType, &amountMicros, &note, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		txns = append(txns, map[string]interface{}{
			"id": id, "type": txnType, "amount_micros": strconv.FormatInt(amountMicros, 10),
			"currency": "CNY", "amount_scale": money.MicrosPerCNY,
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
		`SELECT t.id, t.type, t.amount_micros, t.balance_after_micros, t.note, t.request_id, t.created_at
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
	if err := csvWriter.Write([]string{"ID", "类型", "金额(微元)", "余额(微元)", "币种", "精度", "描述", "引用ID", "时间"}); err != nil {
		return
	}

	for rows.Next() {
		var id int64
		var txType, description, referenceID string
		var amount int64
		var balanceAfter *int64
		var createdAt time.Time
		if err := rows.Scan(&id, &txType, &amount, &balanceAfter, &description, &referenceID, &createdAt); err != nil {
			return
		}
		balance := ""
		if balanceAfter != nil {
			balance = strconv.FormatInt(*balanceAfter, 10)
		}
		if err := csvWriter.Write([]string{
			strconv.FormatInt(id, 10),
			neutralizeSpreadsheetFormula(txType),
			strconv.FormatInt(amount, 10),
			balance,
			"CNY",
			strconv.FormatInt(money.MicrosPerCNY, 10),
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
