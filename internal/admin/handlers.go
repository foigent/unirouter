package admin

import (
	"context"
	"io"
	"strings"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/models"
	"golang.org/x/crypto/bcrypt"

	"github.com/uniroute/unirouter/internal/filter"
)

// Handler contains all admin API handlers.
type Handler struct {
	sensitiveFilter *filter.DFAFilter
	db        db.DBQuerier
	jwtSecret string
	encKey    []byte
}

// NewHandler creates a new admin Handler.
func NewHandler(db db.DBQuerier, jwtSecret string, encKey []byte, sf *filter.DFAFilter) *Handler {
	return &Handler{db: db, jwtSecret: jwtSecret, encKey: encKey, sensitiveFilter: sf}
}

// --- Auth ---

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "username and password are required"})
		return
	}

	var userID, orgID int64
	var passwordHash, role, status string
	err := h.db.QueryRow(r.Context(),
		`SELECT id, org_id, password_hash, role, status FROM users WHERE username = $1 LIMIT 1`,
		req.Username,
	).Scan(&userID, &orgID, &passwordHash, &role, &status)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid credentials"})
		return
	}

	if status != "active" {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Account is disabled"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid credentials"})
		return
	}

	token, err := auth.GenerateJWT(userID, orgID, role, h.jwtSecret, 24)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to generate token"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":    token,
		"user_id":  userID,
		"username": req.Username,
		"role":     role,
	})
}

// --- Channels ---

func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT id, name, provider, base_url, models, priority, degradation_threshold, status, circuit_state, fail_count, created_at
		 FROM channels ORDER BY priority ASC, id ASC`,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	channels := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var name, provider, baseURL, models, status, circuitState string
		var priority, failCount, degradationThreshold int
		var createdAt time.Time
		rows.Scan(&id, &name, &provider, &baseURL, &models, &priority, &degradationThreshold, &status, &circuitState, &failCount, &createdAt)
		channels = append(channels, map[string]interface{}{
			"id": id, "name": name, "provider": provider, "base_url": baseURL,
			"models": models, "priority": priority, "degradation_threshold": degradationThreshold, "status": status,
			"circuit_state": circuitState, "fail_count": failCount, "created_at": createdAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": channels, "total": len(channels)})
}

func (h *Handler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name                 string `json:"name"`
		Provider             string `json:"provider"`
		BaseURL              string `json:"base_url"`
		Models               string `json:"models"`
		Priority             int    `json:"priority"`
		DegradationThreshold int    `json:"degradation_threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	if req.Name == "" || req.Provider == "" || req.BaseURL == "" || req.Models == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "name, provider, base_url, models are required"})
		return
	}

	if req.Priority == 0 {
		req.Priority = 100
	}

	var id int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO channels (name, provider, base_url, models, priority, degradation_threshold, status, circuit_state, fail_count, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 'active', 'healthy', 0, NOW(), NOW())
		 RETURNING id`,
		req.Name, req.Provider, req.BaseURL, req.Models, req.Priority, req.DegradationThreshold,
	).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create channel"})
		return
	}

	h.recordAudit(r.Context(), "channel.create", "channel", fmt.Sprintf("%d", id),
		map[string]interface{}{"name": req.Name, "provider": req.Provider}, r.RemoteAddr)

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}

func (h *Handler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid channel ID"})
		return
	}

	var req struct {
		Name                 *string `json:"name"`
		Provider             *string `json:"provider"`
		BaseURL              *string `json:"base_url"`
		Models               *string `json:"models"`
		Priority             *int    `json:"priority"`
		Status               *string `json:"status"`
		DegradationThreshold *int    `json:"degradation_threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	// Build dynamic SET clauses
	var sets []string
	var args []interface{}
	argIdx := 1

	if req.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.Provider != nil {
		sets = append(sets, fmt.Sprintf("provider = $%d", argIdx))
		args = append(args, *req.Provider)
		argIdx++
	}
	if req.BaseURL != nil {
		sets = append(sets, fmt.Sprintf("base_url = $%d", argIdx))
		args = append(args, *req.BaseURL)
		argIdx++
	}
	if req.Models != nil {
		sets = append(sets, fmt.Sprintf("models = $%d", argIdx))
		args = append(args, *req.Models)
		argIdx++
	}
	if req.Priority != nil {
		sets = append(sets, fmt.Sprintf("priority = $%d", argIdx))
		args = append(args, *req.Priority)
		argIdx++
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}
	if req.DegradationThreshold != nil {
		sets = append(sets, fmt.Sprintf("degradation_threshold = $%d", argIdx))
		args = append(args, *req.DegradationThreshold)
		argIdx++
	}

	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE channels SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update channel"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel not found"})
		return
	}

	changedFields := []string{}
	if req.Name != nil { changedFields = append(changedFields, "name") }
	if req.Provider != nil { changedFields = append(changedFields, "provider") }
	if req.BaseURL != nil { changedFields = append(changedFields, "base_url") }
	if req.Models != nil { changedFields = append(changedFields, "models") }
	if req.Priority != nil { changedFields = append(changedFields, "priority") }
	if req.Status != nil { changedFields = append(changedFields, "status") }
	if req.DegradationThreshold != nil { changedFields = append(changedFields, "degradation_threshold") }
	h.recordAudit(r.Context(), "channel.update", "channel", idStr,
		map[string]interface{}{"fields": changedFields}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Channel updated"})
}

func (h *Handler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid channel ID"})
		return
	}

	tag, err := h.db.Exec(r.Context(), `DELETE FROM channels WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to delete channel"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel not found"})
		return
	}

	h.recordAudit(r.Context(), "channel.delete", "channel", idStr, map[string]interface{}{}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Channel deleted"})
}

// --- API Keys ---

func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT ak.id, ak.org_id, ak.name, ak.key_prefix, ak.status, ak.rpm_limit, ak.qps_limit,
		        ak.budget_limit, ak.allowed_ips, ak.allowed_models, ak.created_at,
		        o.name as org_name
		 FROM api_keys ak
		 JOIN orgs o ON o.id = ak.org_id
		 ORDER BY ak.id ASC`,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	keys := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgID int64
		var name, keyPrefix, status, orgName string
		var rpmLimit, qpsLimit, budgetLimit int
		var allowedIPs, allowedModels string
		var createdAt time.Time
		rows.Scan(&id, &orgID, &name, &keyPrefix, &status, &rpmLimit, &qpsLimit, &budgetLimit, &allowedIPs, &allowedModels, &createdAt, &orgName)
		keys = append(keys, map[string]interface{}{
			"id": id, "org_id": orgID, "name": name, "key_prefix": keyPrefix,
			"status": status, "rpm_limit": rpmLimit, "qps_limit": qpsLimit,
			"budget_limit": budgetLimit, "allowed_ips": allowedIPs, "allowed_models": allowedModels,
			"created_at": createdAt, "org_name": orgName,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": keys, "total": len(keys)})
}

func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID          int64  `json:"org_id"`
		Name           string `json:"name"`
		RPMLimit       int    `json:"rpm_limit"`
		QPSLimit       int    `json:"qps_limit"`
		AllowedIPs     string `json:"allowed_ips"`
		AllowedModels  string `json:"allowed_models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	if req.OrgID == 0 {
		req.OrgID = 1
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "name is required"})
		return
	}
	if req.RPMLimit == 0 {
		req.RPMLimit = 60
	}
	if req.QPSLimit == 0 {
		req.QPSLimit = 10
	}

	rawKey, keyPrefix, err := auth.GenerateAPIKey(req.OrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to generate key"})
		return
	}

	keyHash := auth.HashAPIKey(rawKey, h.jwtSecret)

	var id int64
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'active', $5, $6, $7, $8, NOW(), NOW())
		 RETURNING id`,
		req.OrgID, req.Name, keyHash, keyPrefix, req.RPMLimit, req.QPSLimit, req.AllowedIPs, req.AllowedModels,
	).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create key"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "key": rawKey, "key_prefix": keyPrefix})
}

func (h *Handler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}

	var req struct {
		Name          *string `json:"name"`
		AllowedIPs    *string `json:"allowed_ips"`
		AllowedModels *string `json:"allowed_models"`
		Status        *string `json:"status"`
		RPMLimit      *int    `json:"rpm_limit"`
		QPSLimit      *int    `json:"qps_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	var sets []string
	var args []interface{}
	argIdx := 1

	if req.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.AllowedIPs != nil {
		sets = append(sets, fmt.Sprintf("allowed_ips = $%d", argIdx))
		args = append(args, *req.AllowedIPs)
		argIdx++
	}
	if req.AllowedModels != nil {
		sets = append(sets, fmt.Sprintf("allowed_models = $%d", argIdx))
		args = append(args, *req.AllowedModels)
		argIdx++
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}
	if req.RPMLimit != nil {
		sets = append(sets, fmt.Sprintf("rpm_limit = $%d", argIdx))
		args = append(args, *req.RPMLimit)
		argIdx++
	}
	if req.QPSLimit != nil {
		sets = append(sets, fmt.Sprintf("qps_limit = $%d", argIdx))
		args = append(args, *req.QPSLimit)
		argIdx++
	}

	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE api_keys SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update key"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Key not found"})
		return
	}

	changedFields := []string{}
	if req.Name != nil { changedFields = append(changedFields, "name") }
	if req.AllowedIPs != nil { changedFields = append(changedFields, "allowed_ips") }
	if req.AllowedModels != nil { changedFields = append(changedFields, "allowed_models") }
	if req.Status != nil { changedFields = append(changedFields, "status") }
	if req.RPMLimit != nil { changedFields = append(changedFields, "rpm_limit") }
	if req.QPSLimit != nil { changedFields = append(changedFields, "qps_limit") }
	h.recordAudit(r.Context(), "apikey.update", "apikey", idStr,
		map[string]interface{}{"fields": changedFields}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Key updated"})
}

func (h *Handler) DisableAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}

	tag, err := h.db.Exec(r.Context(),
		`UPDATE api_keys SET status = 'disabled', updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to disable key"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Key not found"})
		return
	}

	h.recordAudit(r.Context(), "apikey.disable", "apikey", idStr, map[string]interface{}{}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Key disabled"})
}

// --- Wallet ---

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
		rows.Scan(&orgID, &balanceCents, &grantedCents, &frozenCents, &totalSpentCents, &currency, &autoFreeze, &lowThreshold, &createdAt, &updatedAt)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"id":                         orgID,
			"org_id":                     orgID,
			"balance_cents":              balanceCents,
			"granted_balance_cents":      grantedCents,
			"frozen_cents":               frozenCents,
			"currency":                   currency,
			"auto_freeze":                autoFreeze,
			"low_balance_threshold_cents": lowThreshold,
			"total_spent_cents":          totalSpentCents,
			"created_at":                 createdAt,
			"updated_at":                 updatedAt,
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
				"error":         "duplicate_request",
				"existing_id":   existingID,
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

	// Write audit log
	_, _ = h.db.Exec(r.Context(),
		`INSERT INTO audit_logs (org_id, user_id, action, resource, resource_id, detail, request_id, source, created_at)
		 VALUES ($1, $2, 'wallet.topup', 'wallet', $3, $4, $5, 'admin', NOW())`,
		walletOrgID, int64(0), fmt.Sprintf("%d", walletOrgID),
		fmt.Sprintf(`{"amount_cents":%d,"description":"%s"}`, req.AmountCents, req.Description),
		requestID)

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

	// Write audit log
	_, _ = h.db.Exec(r.Context(),
		`INSERT INTO audit_logs (org_id, user_id, action, resource, resource_id, detail, request_id, source, created_at)
		 VALUES ($1, $2, 'wallet.adjust', 'wallet', $3, $4, $5, 'admin', NOW())`,
		walletOrgID, int64(0), fmt.Sprintf("%d", walletOrgID),
		fmt.Sprintf(`{"from":%d,"to":%d,"description":"%s"}`, currentBalance, req.TargetBalance, req.Description),
		requestID)

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
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 100 { pageSize = 20 }
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

// --- Logs ---

func (h *Handler) ListLogs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 100 { pageSize = 20 }
	offset := (page - 1) * pageSize

	// Parse optional filters
	modelFilter := r.URL.Query().Get("model")
	providerFilter := r.URL.Query().Get("provider")
	statusCodeFilter := r.URL.Query().Get("status_code")
	startTime := r.URL.Query().Get("start_time")
	endTime := r.URL.Query().Get("end_time")
	requestIDFilter := r.URL.Query().Get("request_id")

	// Build WHERE clause dynamically
	var conditions []string
	var args []interface{}
	argIdx := 1

	if modelFilter != "" {
		conditions = append(conditions, fmt.Sprintf("model ILIKE $%d", argIdx))
		args = append(args, "%"+modelFilter+"%")
		argIdx++
	}
	if providerFilter != "" {
		conditions = append(conditions, fmt.Sprintf("provider = $%d", argIdx))
		args = append(args, providerFilter)
		argIdx++
	}
	if statusCodeFilter != "" {
		if sc, err := strconv.Atoi(statusCodeFilter); err == nil {
			conditions = append(conditions, fmt.Sprintf("status_code = $%d", argIdx))
			args = append(args, sc)
			argIdx++
		}
	}
	if startTime != "" {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, startTime)
		argIdx++
	}
	if endTime != "" {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, endTime)
		argIdx++
	}
	if requestIDFilter != "" {
		conditions = append(conditions, fmt.Sprintf("request_id = $%d", argIdx))
		args = append(args, requestIDFilter)
		argIdx++
	}
	latencyMin := r.URL.Query().Get("latency_min")
	latencyMax := r.URL.Query().Get("latency_max")
	if latencyMin != "" {
		if lm, err := strconv.Atoi(latencyMin); err == nil {
			conditions = append(conditions, fmt.Sprintf("latency_ms >= $%d", argIdx))
			args = append(args, lm)
			argIdx++
		}
	}
	if latencyMax != "" {
		if lx, err := strconv.Atoi(latencyMax); err == nil {
			conditions = append(conditions, fmt.Sprintf("latency_ms <= $%d", argIdx))
			args = append(args, lx)
			argIdx++
		}
	}
	// Global search: search across request_id, model, provider, upstream_error
	search := r.URL.Query().Get("search")
	if search != "" {
		conditions = append(conditions, fmt.Sprintf("(request_id ILIKE $%d OR model ILIKE $%d OR provider ILIKE $%d OR upstream_error ILIKE $%d)", argIdx, argIdx, argIdx, argIdx))
		args = append(args, "%"+search+"%")
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + conditions[0]
		for i := 1; i < len(conditions); i++ {
			whereClause += " AND " + conditions[i]
		}
	}

	var total int
	countQuery := "SELECT COUNT(*) FROM call_logs" + whereClause
	h.db.QueryRow(r.Context(), countQuery, args...).Scan(&total)

	selectQuery := fmt.Sprintf(
		`SELECT id, request_id, model, provider, prompt_tokens, completion_tokens, cost_cents, status_code, latency_ms, created_at
		 FROM call_logs%s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, argIdx, argIdx+1)
	args = append(args, pageSize, offset)

	rows, err := h.db.Query(r.Context(), selectQuery, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	logs := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var requestID, model, provider string
		var promptTokens, completionTokens, costCents, statusCode, latencyMs int
		var createdAt time.Time
		rows.Scan(&id, &requestID, &model, &provider, &promptTokens, &completionTokens, &costCents, &statusCode, &latencyMs, &createdAt)
		logs = append(logs, map[string]interface{}{
			"id": id, "request_id": requestID, "model": model, "provider": provider,
			"prompt_tokens": promptTokens, "completion_tokens": completionTokens,
			"cost_cents": costCents, "status_code": statusCode,
			"latency_ms": latencyMs, "created_at": createdAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": logs, "total": total})
}

// --- Log Detail ---

func (h *Handler) GetLogDetail(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid id"})
		return
	}
	var requestID, model, provider, channel string
	var promptTokens, completionTokens, totalTokens, costCents, statusCode, latencyMs, ttftMs int
	var upstreamStatusCode *int
	var upstreamError, usageSource, ipAddress, requestedModel string
	var isStream bool
	var createdAt time.Time
	err = h.db.QueryRow(r.Context(),
		`SELECT request_id, model, provider, channel, prompt_tokens, completion_tokens, total_tokens,
		        cost_cents, status_code, latency_ms, ttft_ms, upstream_status_code, upstream_error,
		        usage_source, is_stream, ip_address, requested_model, created_at
		 FROM call_logs WHERE id = $1`, id,
	).Scan(&requestID, &model, &provider, &channel, &promptTokens, &completionTokens, &totalTokens,
		&costCents, &statusCode, &latencyMs, &ttftMs, &upstreamStatusCode, &upstreamError,
		&usageSource, &isStream, &ipAddress, &requestedModel, &createdAt)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "log not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":                    id,
		"request_id":            requestID,
		"model":                 model,
		"provider":              provider,
		"channel":               channel,
		"prompt_tokens":         promptTokens,
		"completion_tokens":     completionTokens,
		"total_tokens":          totalTokens,
		"cost_cents":            costCents,
		"status_code":           statusCode,
		"latency_ms":            latencyMs,
		"ttft_ms":               ttftMs,
		"upstream_status_code":  upstreamStatusCode,
		"upstream_error":        upstreamError,
		"usage_source":          usageSource,
		"is_stream":             isStream,
		"ip_address":            ipAddress,
		"requested_model":       requestedModel,
		"created_at":            createdAt,
	})
}

// --- Helpers ---

func parsePagination(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 100 { pageSize = 20 }
	return page, pageSize
}

// --- Model Prices ---

// --- Reconciliation ---

func (h *Handler) ListReconciliation(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)

	modelFilter := r.URL.Query().Get("model")
	statusFilter := r.URL.Query().Get("status")

	var conditions []string
	var args []interface{}
	argIdx := 1

	if modelFilter != "" {
		conditions = append(conditions, fmt.Sprintf("model = $%d", argIdx))
		args = append(args, modelFilter)
		argIdx++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	h.db.QueryRow(r.Context(), fmt.Sprintf(`SELECT COUNT(*) FROM call_logs %s`, where), args...).Scan(&total)

	offset := (page - 1) * pageSize
	query := fmt.Sprintf(
		`SELECT id, request_id, model, total_tokens, prompt_tokens, completion_tokens, status_code, created_at
		 FROM call_logs %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		where, argIdx, argIdx+1)
	args = append(args, pageSize, offset)

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	type ReconRecord struct {
		ID          int64     `json:"id"`
		RequestID   string    `json:"request_id"`
		Model       string    `json:"model"`
		EstTokens   int       `json:"estimated_tokens"`
		ActualTokens int      `json:"actual_tokens"`
		Delta       int       `json:"delta"`
		Status      string    `json:"status"`
		CreatedAt   string    `json:"created_at"`
	}

	records := make([]ReconRecord, 0)
	for rows.Next() {
		var id int64
		var requestID, model string
		var totalTokens, promptTokens, completionTokens, statusCode int
		var createdAt string
		rows.Scan(&id, &requestID, &model, &totalTokens, &promptTokens, &completionTokens, &statusCode, &createdAt)

		// Estimated = prompt + completion (actual from upstream)
		// For reconciliation, we use the same as actual since we don't have pre-flight estimation yet
		estTokens := promptTokens + completionTokens
		status := "reconciled"
		if statusCode >= 400 {
			status = "error"
		} else if statusFilter != "" && status != statusFilter {
			continue
		}

		records = append(records, ReconRecord{
			ID:           id,
			RequestID:    requestID,
			Model:        model,
			EstTokens:    estTokens,
			ActualTokens: totalTokens,
			Delta:        totalTokens - estTokens,
			Status:       status,
			CreatedAt:    createdAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": records, "total": total, "page": page, "page_size": pageSize})
}


func (h *Handler) ListModelPrices(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)

	var total int
	err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM model_prices`).Scan(&total)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	offset := (page - 1) * pageSize
	rows, err := h.db.Query(r.Context(),
		`SELECT id, model_name, input_price_cents, output_price_cents FROM model_prices ORDER BY model_name LIMIT $1 OFFSET $2`,
		pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	prices := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var modelName string
		var inputPrice, outputPrice int
		rows.Scan(&id, &modelName, &inputPrice, &outputPrice)
		prices = append(prices, map[string]interface{}{
			"id": id, "model_name": modelName, "input_price_cents": inputPrice,
			"output_price_cents": outputPrice, "currency": "CNY",
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": prices, "total": total, "page": page, "page_size": pageSize})
}

// --- Settings ---

func (h *Handler) ListSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT key, value, description, updated_at FROM settings ORDER BY key`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	settings := make([]map[string]interface{}, 0)
	for rows.Next() {
		var key, description string
		var value []byte
		var updatedAt time.Time
		rows.Scan(&key, &value, &description, &updatedAt)
		settings = append(settings, map[string]interface{}{
			"key": key, "value": json.RawMessage(value), "description": description, "updated_at": updatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": settings})
}

func (h *Handler) UpdateSetting(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "key is required"})
		return
	}

	var req struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	_, err := h.db.Exec(r.Context(),
		`INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()`,
		key, req.Value)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update setting"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Setting updated"})
}

// --- Dashboard Stats ---

func (h *Handler) DashboardStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Optional time range params; default to last 24h
	startTime := r.URL.Query().Get("start_time")
	endTime := r.URL.Query().Get("end_time")
	if startTime == "" {
		startTime = time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	}
	if endTime == "" {
		endTime = time.Now().Format(time.RFC3339)
	}

	var totalRequests int
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM call_logs`).Scan(&totalRequests)

	var todayCostCents int
	h.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_cents), 0) FROM call_logs WHERE created_at >= $1 AND created_at <= $2`,
		startTime, endTime,
	).Scan(&todayCostCents)

	var avgLatencyMs float64
	h.db.QueryRow(ctx,
		`SELECT COALESCE(AVG(latency_ms), 0) FROM call_logs WHERE created_at >= $1 AND created_at <= $2`,
		startTime, endTime,
	).Scan(&avgLatencyMs)

	var successCount, totalCount int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM call_logs WHERE created_at >= $1 AND created_at <= $2 AND status_code >= 200 AND status_code < 300`,
		startTime, endTime,
	).Scan(&successCount)
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM call_logs WHERE created_at >= $1 AND created_at <= $2`,
		startTime, endTime,
	).Scan(&totalCount)
	successRate := 0.0
	if totalCount > 0 {
		successRate = float64(successCount) / float64(totalCount)
	}

	// Recent logs (last 10)
	recentRows, err := h.db.Query(ctx,
		`SELECT request_id, model, status_code, latency_ms, cost_cents, created_at
		 FROM call_logs ORDER BY created_at DESC LIMIT 10`)
	if err != nil {
		recentRows = nil
	}
	recentLogs := make([]map[string]interface{}, 0)
	if recentRows != nil {
		defer recentRows.Close()
		for recentRows.Next() {
			var requestID, model string
			var statusCode, costCents, latencyMs int
			var createdAt time.Time
			recentRows.Scan(&requestID, &model, &statusCode, &latencyMs, &costCents, &createdAt)
			recentLogs = append(recentLogs, map[string]interface{}{
				"request_id": requestID, "model": model, "status_code": statusCode,
				"latency_ms": latencyMs, "cost_cents": costCents, "created_at": createdAt,
			})
		}
	}

	// Model prices
	priceRows, err := h.db.Query(ctx,
		`SELECT model_name, input_price_cents, output_price_cents FROM model_prices ORDER BY model_name`)
	if err != nil {
		priceRows = nil
	}
	modelPrices := make([]map[string]interface{}, 0)
	if priceRows != nil {
		defer priceRows.Close()
		for priceRows.Next() {
			var modelName string
			var inputPrice, outputPrice int
			priceRows.Scan(&modelName, &inputPrice, &outputPrice)
			modelPrices = append(modelPrices, map[string]interface{}{
				"model_name": modelName, "input_price_cents": inputPrice, "output_price_cents": outputPrice,
			})
		}
	}

	// 7-day trend: daily request count and cost
	trendRows, err := h.db.Query(ctx,
		`SELECT DATE(created_at) AS day, COUNT(*) AS requests, COALESCE(SUM(cost_cents), 0) AS cost
		 FROM call_logs
		 WHERE created_at >= NOW() - INTERVAL '7 days'
		 GROUP BY DATE(created_at)
		 ORDER BY day ASC`)
	if err != nil {
		trendRows = nil
	}
	dailyTrend := make([]map[string]interface{}, 0)
	if trendRows != nil {
		defer trendRows.Close()
		for trendRows.Next() {
			var day time.Time
			var requests, cost int
			trendRows.Scan(&day, &requests, &cost)
			dailyTrend = append(dailyTrend, map[string]interface{}{
				"date":     day.Format("2006-01-02"),
				"requests": requests,
				"cost_cents": cost,
			})
		}
	}

	// Wallet budget info
	var walletBalance, walletGranted, walletFrozen, walletThreshold int
	h.db.QueryRow(ctx,
		`SELECT COALESCE(balance,0), COALESCE(granted_balance,0), COALESCE(frozen,0), COALESCE(low_balance_threshold_cents,0)
		 FROM wallets ORDER BY org_id LIMIT 1`,
	).Scan(&walletBalance, &walletGranted, &walletFrozen, &walletThreshold)

	// Model distribution (top 5 models by request count in time range)
	modelDistRows, err := h.db.Query(ctx,
		`SELECT model, COUNT(*) as cnt
		 FROM call_logs
		 WHERE created_at >= $1 AND created_at <= $2
		 GROUP BY model ORDER BY cnt DESC LIMIT 5`, startTime, endTime)
	if err != nil {
		modelDistRows = nil
	}
	modelDistribution := make([]map[string]interface{}, 0)
	if modelDistRows != nil {
		defer modelDistRows.Close()
		for modelDistRows.Next() {
			var model string
			var cnt int
			modelDistRows.Scan(&model, &cnt)
			modelDistribution = append(modelDistribution, map[string]interface{}{
				"model":    model,
				"requests": cnt,
			})
		}
	}

	// Channel health (enabled channels with recent stats)
	channelHealthRows, err := h.db.Query(ctx,
		`SELECT c.id, c.name, c.provider, c.enabled,
		        COALESCE(SUM(CASE WHEN cl.status_code >= 200 AND cl.status_code < 300 THEN 1 ELSE 0 END), 0) as success_count,
		        COALESCE(SUM(CASE WHEN cl.status_code >= 400 OR cl.status_code = 0 THEN 1 ELSE 0 END), 0) as fail_count
		 FROM channels c
		 LEFT JOIN call_logs cl ON cl.channel = c.name AND cl.created_at >= $1 AND cl.created_at <= $2
		 GROUP BY c.id, c.name, c.provider, c.enabled
		 ORDER BY c.id`, startTime, endTime)
	if err != nil {
		channelHealthRows = nil
	}
	channelHealth := make([]map[string]interface{}, 0)
	if channelHealthRows != nil {
		defer channelHealthRows.Close()
		for channelHealthRows.Next() {
			var id int
			var name, provider string
			var enabled bool
			var successCount, failCount int
			channelHealthRows.Scan(&id, &name, &provider, &enabled, &successCount, &failCount)
			status := "healthy"
			if !enabled {
				status = "disabled"
			} else if failCount > 0 && successCount == 0 {
				status = "error"
			} else if failCount > 0 && float64(failCount)/float64(successCount+failCount) > 0.5 {
				status = "warning"
			}
			channelHealth = append(channelHealth, map[string]interface{}{
				"id":            id,
				"name":          name,
				"provider":      provider,
				"enabled":       enabled,
				"status":        status,
				"success_count": successCount,
				"fail_count":    failCount,
			})
		}
	}

	// Yesterday KPIs for delta comparison
	yesterdayStart := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	yesterdayEnd := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	var yesterCostCents int
	h.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_cents), 0) FROM call_logs WHERE created_at >= $1 AND created_at <= $2`,
		yesterdayStart, yesterdayEnd,
	).Scan(&yesterCostCents)
	var yesterLatency float64
	h.db.QueryRow(ctx,
		`SELECT COALESCE(AVG(latency_ms), 0) FROM call_logs WHERE created_at >= $1 AND created_at <= $2`,
		yesterdayStart, yesterdayEnd,
	).Scan(&yesterLatency)
	var yesterSuccess, yesterTotal int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM call_logs WHERE created_at >= $1 AND created_at <= $2 AND status_code >= 200 AND status_code < 300`,
		yesterdayStart, yesterdayEnd,
	).Scan(&yesterSuccess)
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM call_logs WHERE created_at >= $1 AND created_at <= $2`,
		yesterdayStart, yesterdayEnd,
	).Scan(&yesterTotal)
	yesterRate := 0.0
	if yesterTotal > 0 {
		yesterRate = float64(yesterSuccess) / float64(yesterTotal)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_requests":      totalRequests,
		"today_cost_cents":    todayCostCents,
		"avg_latency_ms":      avgLatencyMs,
		"success_rate":        successRate,
		"recent_logs":         recentLogs,
		"model_prices":        modelPrices,
		"daily_trend":         dailyTrend,
		"wallet_balance":      walletBalance,
		"wallet_granted":      walletGranted,
		"wallet_frozen":       walletFrozen,
		"wallet_threshold":    walletThreshold,
		"model_distribution":  modelDistribution,
		"channel_health":      channelHealth,
		"delta_vs_yesterday": map[string]interface{}{
			"cost_cents": todayCostCents - yesterCostCents,
			"latency_ms": avgLatencyMs - yesterLatency,
			"success_rate": successRate - yesterRate,
		},
	})
}


// --- Channel Keys ---

func (h *Handler) ListChannelKeys(w http.ResponseWriter, r *http.Request) {
	// Pagination
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	// Count total
	var total int
	err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM channel_keys`).Scan(&total)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT ck.id, ck.channel_id, c.name as channel_name, ck.key_enc, ck.allowed_models, ck.status,
		        ck.fail_count, ck.last_used_at, ck.cooldown_until, ck.created_at
		 FROM channel_keys ck
		 JOIN channels c ON c.id = ck.channel_id
		 ORDER BY ck.id ASC
		 LIMIT $1 OFFSET $2`, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	keys := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, channelID int64
		var channelName, keyEnc, allowedModels, status string
		var failCount int
		var lastUsedAt, cooldownUntil *time.Time
		var createdAt time.Time
		rows.Scan(&id, &channelID, &channelName, &keyEnc, &allowedModels, &status, &failCount, &lastUsedAt, &cooldownUntil, &createdAt)
		keys = append(keys, map[string]interface{}{
			"id": id, "channel_id": channelID, "channel_name": channelName,
			"key_masked": maskKey(keyEnc), "allowed_models": allowedModels, "status": status,
			"fail_count": failCount, "last_used_at": lastUsedAt, "cooldown_until": cooldownUntil, "created_at": createdAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": keys, "total": total})
}

func (h *Handler) CreateChannelKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChannelID     int64  `json:"channel_id"`
		Key           string `json:"key"`
		AllowedModels string `json:"allowed_models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	if req.ChannelID == 0 || req.Key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "channel_id and key are required"})
		return
	}

	// Encrypt the key
	keyEnc, err := auth.EncryptKey([]byte(req.Key), h.encKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to encrypt key"})
		return
	}

	var id int64
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO channel_keys (channel_id, key_enc, allowed_models, status, fail_count, created_at, updated_at)
		 VALUES ($1, $2, $3, 'active', 0, NOW(), NOW())
		 RETURNING id`,
		req.ChannelID, keyEnc, req.AllowedModels,
	).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create channel key"})
		return
	}

	h.recordAudit(r.Context(), "channelkey.create", "channel_key", fmt.Sprintf("%d", id),
		map[string]interface{}{"channel_id": req.ChannelID}, r.RemoteAddr)

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}

func (h *Handler) UpdateChannelKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}

	var req struct {
		ChannelID     *int64  `json:"channel_id"`
		Status        *string `json:"status"`
		AllowedModels *string `json:"allowed_models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	var sets []string
	var args []interface{}
	argIdx := 1

	if req.ChannelID != nil {
		sets = append(sets, fmt.Sprintf("channel_id = $%d", argIdx))
		args = append(args, *req.ChannelID)
		argIdx++
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}

	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE channel_keys SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel key not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) DeleteChannelKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}

	tag, err := h.db.Exec(r.Context(), `DELETE FROM channel_keys WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel key not found"})
		return
	}

	h.recordAudit(r.Context(), "channelkey.delete", "channel_key", idStr, map[string]interface{}{}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}



// maskKey returns a masked version of an encrypted key, showing only the last 4 hex chars.
func maskKey(keyEnc string) string {
	if len(keyEnc) <= 8 {
		return "****"
	}
	return "****" + keyEnc[len(keyEnc)-4:]
}

// --- Users ---

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 100 { pageSize = 20 }
	offset := (page - 1) * pageSize

	var total int
	h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&total)

	rows, err := h.db.Query(r.Context(),
		`SELECT u.id, u.org_id, o.name AS org_name, u.username, u.role, u.status, u.created_at, u.updated_at
		 FROM users u LEFT JOIN orgs o ON o.id = u.org_id
		 ORDER BY u.id DESC LIMIT $1 OFFSET $2`, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	users := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgId int64
		var username, role, status string
		var orgName *string
		var createdAt, updatedAt time.Time
		rows.Scan(&id, &orgId, &orgName, &username, &role, &status, &createdAt, &updatedAt)
		oName := ""
		if orgName != nil { oName = *orgName }
		users = append(users, map[string]interface{}{
			"id": id, "org_id": orgId, "org_name": oName,
			"username": username, "role": role, "status": status,
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": users, "total": total})
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgId    int64  `json:"org_id"`
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Username and password required"})
		return
	}
	if req.Role == "" { req.Role = "org_viewer" }
	if req.OrgId == 0 {
		h.db.QueryRow(r.Context(), `SELECT id FROM orgs LIMIT 1`).Scan(&req.OrgId)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Hash failed"})
		return
	}

	var id int64
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO users (org_id, username, password_hash, role) VALUES ($1, $2, $3, $4) RETURNING id`,
		req.OrgId, req.Username, string(hash), req.Role).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Username already exists"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	var req struct {
		Role   *string `json:"role"`
		Status *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	var sets []string
	var args []interface{}
	argIdx := 1
	if req.Role != nil {
		sets = append(sets, fmt.Sprintf("role = $%d", argIdx))
		args = append(args, *req.Role)
		argIdx++
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}
	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	tag, err := h.db.Exec(r.Context(), `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// --- Model Prices CRUD ---

func (h *Handler) CreateModelPrice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModelName       string `json:"model_name"`
		InputPriceCents int    `json:"input_price_cents"`
		OutputPriceCents int   `json:"output_price_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.ModelName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "model_name required"})
		return
	}
	var newID int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO model_prices (model_name, input_price_cents, output_price_cents) VALUES ($1, $2, $3)
		 ON CONFLICT (model_name) DO UPDATE SET input_price_cents = $2, output_price_cents = $3, updated_at = NOW()
		 RETURNING id`,
		req.ModelName, req.InputPriceCents, req.OutputPriceCents).Scan(&newID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Insert failed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": newID, "model_name": req.ModelName, "input_price_cents": req.InputPriceCents, "output_price_cents": req.OutputPriceCents})
}

func (h *Handler) UpdateModelPrice(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	var req struct {
		InputPriceCents  *int    `json:"input_price_cents"`
		OutputPriceCents *int    `json:"output_price_cents"`
		ModelName        *string `json:"model_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	var sets []string
	var args []interface{}
	argIdx := 1
	if req.ModelName != nil {
		sets = append(sets, fmt.Sprintf("model_name = $%d", argIdx))
		args = append(args, *req.ModelName)
		argIdx++
	}
	if req.InputPriceCents != nil {
		sets = append(sets, fmt.Sprintf("input_price_cents = $%d", argIdx))
		args = append(args, *req.InputPriceCents)
		argIdx++
	}
	if req.OutputPriceCents != nil {
		sets = append(sets, fmt.Sprintf("output_price_cents = $%d", argIdx))
		args = append(args, *req.OutputPriceCents)
		argIdx++
	}
	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf("UPDATE model_prices SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model price not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) DeleteModelPrice(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	tag, err := h.db.Exec(r.Context(), `DELETE FROM model_prices WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model price not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// --- Org Model Prices ---

func (h *Handler) ListOrgModelPrices(w http.ResponseWriter, r *http.Request) {
	orgIDStr := r.URL.Query().Get("org_id")
	if orgIDStr == "" {
		orgIDStr = "1"
	}
	orgID, err := strconv.ParseInt(orgIDStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
		return
	}

	// Pagination
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	// Count total
	var total int
	err = h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM org_model_prices WHERE org_id = $1`, orgID).Scan(&total)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT id, org_id, model_name, multiplier, created_at, updated_at
		 FROM org_model_prices WHERE org_id = $1 ORDER BY model_name
		 LIMIT $2 OFFSET $3`, orgID, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()
	var items []models.OrgModelPrice
	for rows.Next() {
		var p models.OrgModelPrice
		if err := rows.Scan(&p.ID, &p.OrgID, &p.ModelName, &p.Multiplier, &p.CreatedAt, &p.UpdatedAt); err != nil {
			continue
		}
		items = append(items, p)
	}
	if items == nil {
		items = []models.OrgModelPrice{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items, "total": total})
}

func (h *Handler) CreateOrgModelPrice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID      int64   `json:"org_id"`
		ModelName  string  `json:"model_name"`
		Multiplier float64 `json:"multiplier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.OrgID == 0 || req.ModelName == "" || req.Multiplier <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "org_id, model_name, multiplier > 0 required"})
		return
	}
	var id int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO org_model_prices (org_id, model_name, multiplier)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (org_id, model_name) DO UPDATE SET multiplier = $3, updated_at = NOW()
		 RETURNING id`,
		req.OrgID, req.ModelName, req.Multiplier).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "success": true})
}

func (h *Handler) UpdateOrgModelPrice(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	var req struct {
		Multiplier *float64 `json:"multiplier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.Multiplier == nil || *req.Multiplier <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "multiplier > 0 required"})
		return
	}
	tag, err := h.db.Exec(r.Context(),
		`UPDATE org_model_prices SET multiplier = $1, updated_at = NOW() WHERE id = $2`,
		*req.Multiplier, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) DeleteOrgModelPrice(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	tag, err := h.db.Exec(r.Context(), `DELETE FROM org_model_prices WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// --- Grants ---

func (h *Handler) ListGrants(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 100 { pageSize = 20 }
	offset := (page - 1) * pageSize

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
		if orgName != nil { oName = *orgName }
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
		OrgId            int64  `json:"org_id"`
		AmountCents      int64  `json:"amount_cents"`
		Reason           string `json:"reason"`
		ClientRequestID  string `json:"client_request_id"`
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

// --- Audit Logs ---

func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 100 { pageSize = 20 }
	offset := (page - 1) * pageSize

	var total int
	h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM audit_logs`).Scan(&total)

	rows, err := h.db.Query(r.Context(),
		`SELECT id, user_id, action, resource, resource_id, detail, created_at
		 FROM audit_logs ORDER BY id DESC LIMIT $1 OFFSET $2`, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	logs := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, userId int64
		var action, resource, resourceId string
		var detail []byte
		var createdAt time.Time
		rows.Scan(&id, &userId, &action, &resource, &resourceId, &detail, &createdAt)
		logs = append(logs, map[string]interface{}{
			"id": id, "user_id": userId, "action": action,
			"resource": resource, "resource_id": resourceId,
			"detail": json.RawMessage(detail), "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": logs, "total": total})
}

// --- Cleanup ---


// recordAudit logs an admin action to audit_logs. Non-blocking, errors are logged.
func (h *Handler) recordAudit(ctx context.Context, action, resource, resourceID string, detail map[string]interface{}, ip string) {
	detailJSON := "{}"
	if b, err := json.Marshal(detail); err == nil {
		detailJSON = string(b)
	}
	_, err := h.db.Exec(ctx,
		`INSERT INTO audit_logs (org_id, user_id, action, resource, resource_id, detail, ip, request_id, created_at)
		 VALUES (0, 0, $1, $2, $3, $4::jsonb, $5, '', NOW())`,
		action, resource, resourceID, detailJSON, ip)
	if err != nil {
		log.Printf("audit log failed: %s %s/%s: %v", action, resource, resourceID, err)
	}
}

func (h *Handler) CleanupOldData(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		retentionDays = 30
	}

	result, err := h.db.Exec(ctx,
		`DELETE FROM call_logs WHERE created_at < NOW() - INTERVAL '1 day' * $1`,
		retentionDays)
	if err != nil {
		return fmt.Errorf("cleanup call_logs: %w", err)
	}
	log.Printf("Cleanup: deleted %d old call_logs (older than %d days)", result.RowsAffected(), retentionDays)

	result, err = h.db.Exec(ctx,
		`DELETE FROM audit_logs WHERE created_at < NOW() - INTERVAL '1 day' * $1`,
		retentionDays*2)
	if err != nil {
		return fmt.Errorf("cleanup audit_logs: %w", err)
	}
	log.Printf("Cleanup: deleted %d old audit_logs (older than %d days)", result.RowsAffected(), retentionDays*2)

	result, err = h.db.Exec(ctx,
		`DELETE FROM transactions WHERE created_at < NOW() - INTERVAL '1 day' * $1 AND type IN ('settle', 'cancel')`,
		retentionDays)
	if err != nil {
		return fmt.Errorf("cleanup transactions: %w", err)
	}
	log.Printf("Cleanup: deleted %d old transactions (older than %d days)", result.RowsAffected(), retentionDays)

	// Keep freeze_idempotency records for 30 days (needed for idempotency checks)
	result, err = h.db.Exec(ctx,
		`DELETE FROM freeze_idempotency WHERE created_at < NOW() - INTERVAL '30 days'`,
	)
	if err != nil {
		return fmt.Errorf("cleanup freeze_idempotency: %w", err)
	}
	log.Printf("Cleanup: deleted %d expired freeze_idempotency records (>30d)", result.RowsAffected())

	// First: deduct expired grants from wallets.granted_balance
	result, err = h.db.Exec(ctx,
		`UPDATE wallets w
		 SET granted_balance = granted_balance - COALESCE(sub.total, 0),
		     updated_at = NOW()
		 FROM (
		     SELECT org_id, SUM(balance) AS total
		     FROM grants
		     WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at < NOW()
		     GROUP BY org_id
		 ) sub
		 WHERE w.org_id = sub.org_id AND sub.total > 0`)
	if err != nil {
		return fmt.Errorf("deduct expired grants from wallets: %w", err)
	}
	if result.RowsAffected() > 0 {
		log.Printf("Cleanup: deducted expired grants from %d wallets", result.RowsAffected())
	}

	// Second: mark grants as expired
	result, err = h.db.Exec(ctx,
		`UPDATE grants SET status = 'expired', updated_at = NOW()
		 WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at < NOW()`)
	if err != nil {
		return fmt.Errorf("expire grants: %w", err)
	}
	if result.RowsAffected() > 0 {
		log.Printf("Cleanup: expired %d grants past their expiry date", result.RowsAffected())
	}

	return nil
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// --- Sensitive Words CRUD (PRD F13) ---

// ListSensitiveWords returns all sensitive words from settings.
func (h *Handler) ListSensitiveWords(w http.ResponseWriter, r *http.Request) {
	var valueJSON string
	err := h.db.QueryRow(r.Context(),
		`SELECT value FROM settings WHERE key = 'sensitive_words'`,
	).Scan(&valueJSON)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": []string{}})
		return
	}

	var words []string
	if err := json.Unmarshal([]byte(valueJSON), &words); err != nil {
		words = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": words})
}

// UpdateSensitiveWords replaces the entire sensitive words list.
func (h *Handler) UpdateSensitiveWords(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Words []string `json:"words"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return
	}

	valueJSON, err := json.Marshal(req.Words)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Marshal failed"})
		return
	}

	_, err = h.db.Exec(r.Context(),
		`INSERT INTO settings (key, value, description, updated_at)
		 VALUES ('sensitive_words', $1, '敏感词库', NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = NOW()`,
		string(valueJSON),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}

	// Hot-reload the DFA filter
	if h.sensitiveFilter != nil {
		h.sensitiveFilter.UpdateWords(req.Words)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": req.Words})
}

// AddSensitiveWords adds words to the existing list.
func (h *Handler) AddSensitiveWords(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Words []string `json:"words"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return
	}

	// Get current words
	var valueJSON string
	err := h.db.QueryRow(r.Context(),
		`SELECT value FROM settings WHERE key = 'sensitive_words'`,
	).Scan(&valueJSON)
	if err != nil {
		valueJSON = "[]"
	}

	var existing []string
	json.Unmarshal([]byte(valueJSON), &existing)
	if existing == nil {
		existing = []string{}
	}

	// Merge (deduplicate)
	wordSet := make(map[string]bool)
	for _, w := range existing {
		wordSet[w] = true
	}
	for _, w := range req.Words {
		wordSet[w] = true
	}
	merged := make([]string, 0, len(wordSet))
	for w := range wordSet {
		merged = append(merged, w)
	}

	newJSON, _ := json.Marshal(merged)
	_, err = h.db.Exec(r.Context(),
		`INSERT INTO settings (key, value, description, updated_at)
		 VALUES ('sensitive_words', $1, '敏感词库', NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = NOW()`,
		string(newJSON),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}

	// Hot-reload
	if h.sensitiveFilter != nil {
		h.sensitiveFilter.UpdateWords(merged)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": merged})
}

// DeleteSensitiveWords removes words from the list.
func (h *Handler) DeleteSensitiveWords(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Words []string `json:"words"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return
	}

	var valueJSON string
	err := h.db.QueryRow(r.Context(),
		`SELECT value FROM settings WHERE key = 'sensitive_words'`,
	).Scan(&valueJSON)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": []string{}})
		return
	}

	var existing []string
	json.Unmarshal([]byte(valueJSON), &existing)

	// Build removal set
	remove := make(map[string]bool)
	for _, w := range req.Words {
		remove[w] = true
	}
	var remaining []string
	for _, w := range existing {
		if !remove[w] {
			remaining = append(remaining, w)
		}
	}
	if remaining == nil {
		remaining = []string{}
	}

	newJSON, _ := json.Marshal(remaining)
	_, err = h.db.Exec(r.Context(),
		`INSERT INTO settings (key, value, description, updated_at)
		 VALUES ('sensitive_words', $1, '敏感词库', NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = NOW()`,
		string(newJSON),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}

	// Hot-reload
	if h.sensitiveFilter != nil {
		h.sensitiveFilter.UpdateWords(remaining)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": remaining})
}

// TestChannel tests connectivity to an upstream channel by calling its /v1/models endpoint.
func (h *Handler) TestChannel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid channel ID"})
		return
	}

	var baseURL, models string
	err = h.db.QueryRow(r.Context(),
		`SELECT base_url, models FROM channels WHERE id = $1`, id,
	).Scan(&baseURL, &models)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel not found"})
		return
	}

	// Get first active channel key for this channel
	var keyEnc string
	err = h.db.QueryRow(r.Context(),
		`SELECT key_enc FROM channel_keys WHERE channel_id = $1 AND status = 'active' ORDER BY id LIMIT 1`, id,
	).Scan(&keyEnc)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "No active key found for this channel",
			"status": "no_key",
		})
		return
	}

	// Decrypt the key
	apiKey, err := auth.DecryptKey(keyEnc, h.encKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to decrypt key"})
		return
	}

	// Build the test URL
	testURL := strings.TrimRight(baseURL, "/") + "/v1/models"

	start := time.Now()
	req, err := http.NewRequestWithContext(r.Context(), "GET", testURL, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "error", "error": err.Error(),
		})
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":     "error",
			"error":      err.Error(),
			"latency_ms": latency,
		})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Build curl example
	curlExample := fmt.Sprintf("curl %s -H \"Authorization: Bearer sk-***\"", testURL)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "ok",
		"status_code":  resp.StatusCode,
		"latency_ms":   latency,
		"models":       models,
		"response":     string(body),
		"curl_example": curlExample,
	})
}

// ExportWalletTransactions returns wallet transactions as CSV.
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

// GetChannel returns a single channel by ID.
func (h *Handler) GetChannel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid channel ID"})
		return
	}
	var ch struct {
		ID                   int64
		Name, Provider       string
		BaseURL, Models      string
		Priority             int
		DegradationThreshold int
		Status               string
		CircuitState         string
		FailCount            int
		CreatedAt            time.Time
	}
	err = h.db.QueryRow(r.Context(),
		`SELECT id, name, provider, base_url, models, priority, degradation_threshold, status, circuit_state, fail_count, created_at
		 FROM channels WHERE id = $1`, id).Scan(
		&ch.ID, &ch.Name, &ch.Provider, &ch.BaseURL, &ch.Models, &ch.Priority,
		&ch.DegradationThreshold, &ch.Status, &ch.CircuitState, &ch.FailCount, &ch.CreatedAt)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": ch.ID, "name": ch.Name, "provider": ch.Provider, "base_url": ch.BaseURL,
		"models": ch.Models, "priority": ch.Priority, "degradation_threshold": ch.DegradationThreshold,
		"status": ch.Status, "circuit_state": ch.CircuitState, "fail_count": ch.FailCount, "created_at": ch.CreatedAt,
	})
}

// GetAPIKey returns a single API key by ID.
func (h *Handler) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}
	var k struct {
		ID, OrgID       int64
		Name, KeyPrefix string
		Status          string
		RPMLimit, QPSLimit, BudgetLimit int
		AllowedIPs, AllowedModels string
		OrgName         string
		CreatedAt       time.Time
	}
	err = h.db.QueryRow(r.Context(),
		`SELECT ak.id, ak.org_id, ak.name, ak.key_prefix, ak.status, ak.rpm_limit, ak.qps_limit,
		        ak.budget_limit, ak.allowed_ips, ak.allowed_models, ak.created_at, o.name
		 FROM api_keys ak JOIN orgs o ON o.id = ak.org_id
		 WHERE ak.id = $1`, id).Scan(
		&k.ID, &k.OrgID, &k.Name, &k.KeyPrefix, &k.Status, &k.RPMLimit, &k.QPSLimit,
		&k.BudgetLimit, &k.AllowedIPs, &k.AllowedModels, &k.CreatedAt, &k.OrgName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "API key not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": k.ID, "org_id": k.OrgID, "name": k.Name, "key_prefix": k.KeyPrefix,
		"status": k.Status, "rpm_limit": k.RPMLimit, "qps_limit": k.QPSLimit,
		"budget_limit": k.BudgetLimit, "allowed_ips": k.AllowedIPs, "allowed_models": k.AllowedModels,
		"created_at": k.CreatedAt, "org_name": k.OrgName,
	})
}
