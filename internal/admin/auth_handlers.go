package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

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

	auditCtx := context.WithValue(context.Background(), middleware.CtxOrgID, orgID)
	auditCtx = context.WithValue(auditCtx, middleware.CtxUserID, userID)
	h.recordAudit(auditCtx, "login", "user", fmt.Sprintf("%d", userID),
		map[string]interface{}{"username": req.Username, "role": role}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":    token,
		"user_id":  userID,
		"username": req.Username,
		"role":     role,
	})
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}

	userID := middleware.GetUserIDFromContext(r.Context())
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Not authenticated"})
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "old_password and new_password are required"})
		return
	}
	if len(req.NewPassword) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "new_password must be at least 6 characters"})
		return
	}
	if req.OldPassword == req.NewPassword {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "new_password must differ from old_password"})
		return
	}

	var passwordHash string
	err := h.db.QueryRow(r.Context(),
		`SELECT password_hash FROM users WHERE id = $1 AND status = 'active'`,
		userID,
	).Scan(&passwordHash)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.OldPassword)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid credentials"})
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to hash password"})
		return
	}

	tag, err := h.db.Exec(r.Context(),
		`UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`,
		string(newHash), userID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update password"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		return
	}

	h.recordAudit(r.Context(), "change_password", "user", fmt.Sprintf("%d", userID), nil, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
