package admin

import (
	"encoding/json"
	"net/http"

	"github.com/uniroute/unirouter/internal/auth"
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":    token,
		"user_id":  userID,
		"username": req.Username,
		"role":     role,
	})
}
