package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/licensing"
)

const licenseSettingKey = "license.offline"

func (h *Handler) GetLicense(w http.ResponseWriter, r *http.Request) {
	raw, err := h.readStoredLicense(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read license"})
		return
	}
	writeJSON(w, http.StatusOK, h.licenseVerifierOrDefault().VerifyBytes(raw))
}

func (h *Handler) UpdateLicense(w http.ResponseWriter, r *http.Request) {
	var req struct {
		License json.RawMessage `json:"license"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	raw, err := normalizeLicenseInput(req.License)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}

	verifier := h.licenseVerifierOrDefault()
	status := verifier.VerifyBytes(raw)
	if status.Status == licensing.StatusInvalid {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": status.Error, "status": status})
		return
	}

	normalized, err := licensing.NormalizeDocument(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	beforeRaw, err := h.readStoredLicense(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read existing license"})
		return
	}
	beforeStatus := verifier.VerifyBytes(beforeRaw)

	_, err = h.db.Exec(r.Context(),
		`INSERT INTO settings (key, value, description, updated_at)
		 VALUES ($1, $2::jsonb, 'Offline UniRouter license document', NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $2::jsonb, description = EXCLUDED.description, updated_at = NOW()`,
		licenseSettingKey, string(normalized))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to save license"})
		return
	}

	h.recordAuditFromRequest(r, "license.update", "license", licenseResourceID(status), map[string]interface{}{
		"before": licenseAuditSummary(beforeStatus),
		"after":  licenseAuditSummary(status),
	})

	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) readStoredLicense(r *http.Request) ([]byte, error) {
	var raw []byte
	err := h.db.QueryRow(r.Context(), `SELECT value FROM settings WHERE key = $1`, licenseSettingKey).Scan(&raw)
	if err == nil {
		return raw, nil
	}
	if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "no rows") {
		return nil, nil
	}
	return nil, err
}

func (h *Handler) licenseVerifierOrDefault() *licensing.Verifier {
	if h.licenseVerifier != nil {
		return h.licenseVerifier
	}
	return licensing.NewVerifier(nil, 30)
}

func normalizeLicenseInput(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return nil, errors.New("license is required")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, errors.New("license is required")
		}
		return []byte(text), nil
	}
	return raw, nil
}

func licenseResourceID(status licensing.Status) string {
	if status.License != nil && status.License.DeploymentID != "" {
		return status.License.DeploymentID
	}
	return "license"
}

func licenseAuditSummary(status licensing.Status) map[string]interface{} {
	summary := map[string]interface{}{
		"status":              status.Status,
		"valid":               status.Valid,
		"signature_valid":     status.SignatureValid,
		"warning":             status.Warning,
		"days_until_expiry":   status.DaysUntilExpiry,
		"verifier_configured": status.VerifierConfigured,
	}
	if status.Error != "" {
		summary["error"] = status.Error
	}
	if status.License != nil {
		summary["customer"] = status.License.Customer
		summary["deployment_id"] = status.License.DeploymentID
		summary["tier"] = status.License.Tier
		summary["expires_at"] = status.License.ExpiresAt
		summary["features"] = status.License.Features
		summary["limits"] = status.License.Limits
	}
	return summary
}
