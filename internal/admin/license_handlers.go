package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/uniroute/unirouter/internal/licensing"
)

const licenseSettingKey = "license.offline"
const maxLicenseImportBytes = 256 << 10

// ReconcileStoredCommercialLicense loads the signed document and rebuilds its
// derived resource-limit projection under the same lock used by imports. This
// is the startup path after migrations and makes a restored database safe even
// if the derived singleton was absent.
func (h *Handler) ReconcileStoredCommercialLicense(ctx context.Context) (licensing.Status, error) {
	return h.syncStoredCommercialLicense(ctx, true)
}

// RefreshStoredCommercialLicense accepts a stored commercial document only
// when its derived resource-limit projection matches. Imports update both in
// one transaction, so a mismatch is an unsafe restored or manually modified
// state rather than a normal replica race.
func (h *Handler) RefreshStoredCommercialLicense(ctx context.Context) (licensing.Status, error) {
	return h.syncStoredCommercialLicense(ctx, false)
}

func (h *Handler) syncStoredCommercialLicense(ctx context.Context, reconcile bool) (licensing.Status, error) {
	verifier := h.licenseVerifierOrDefault()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return licensing.Status{}, fmt.Errorf("begin commercial License sync: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := lockGlobalControlKey(ctx, tx, licenseSettingKey); err != nil {
		return licensing.Status{}, fmt.Errorf("lock commercial License sync: %w", err)
	}
	raw, _, err := readGlobalSetting(ctx, tx, licenseSettingKey, true)
	if err != nil {
		return licensing.Status{}, fmt.Errorf("read stored commercial License: %w", err)
	}
	status := verifier.VerifyBytes(raw)
	if status.Enforceable {
		if reconcile {
			if err := replaceCommercialEntitlements(ctx, tx, status); err != nil {
				return status, fmt.Errorf("reconcile commercial entitlement projection: %w", err)
			}
		} else {
			matches, err := commercialEntitlementsMatch(ctx, tx, status)
			if err != nil {
				return status, err
			}
			if !matches {
				if h.licenseRuntime != nil {
					h.licenseRuntime.Replace(nil)
				}
				return status, fmt.Errorf("commercial entitlement projection does not match the verified stored License")
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return status, fmt.Errorf("commit commercial License sync: %w", err)
	}
	if h.licenseRuntime != nil {
		status = h.licenseRuntime.Replace(raw)
	}
	return status, nil
}

func (h *Handler) GetLicense(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformGlobalControl(w, r) {
		return
	}
	raw, err := h.readStoredLicense(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read license"})
		return
	}
	writeJSON(w, http.StatusOK, h.evaluateLicense(raw))
}

func (h *Handler) UpdateLicense(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformGlobalControl(w, r) {
		return
	}
	var req struct {
		License json.RawMessage `json:"license"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLicenseImportBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{"error": "License import exceeds 256 KiB"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Only one License import object is allowed"})
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
	if h.licenseRuntime != nil && !status.Enforceable {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "An enforceable unirouter.license.v1 document is required", "status": status})
		return
	}

	normalized, err := licensing.NormalizeDocument(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := lockGlobalControlKey(r.Context(), tx, licenseSettingKey); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lock failed"})
		return
	}
	beforeRaw, _, err := readGlobalSetting(r.Context(), tx, licenseSettingKey, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read existing license"})
		return
	}
	beforeStatus := verifier.VerifyBytes(beforeRaw)
	if status.Enforceable {
		if err := replaceCommercialEntitlements(r.Context(), tx, status); err != nil {
			var conflict *commercialEntitlementConflict
			if errors.As(err, &conflict) {
				writeJSON(w, http.StatusConflict, map[string]interface{}{
					"error":    conflict.Error(),
					"code":     "license_error.resource_limit_below_current_usage",
					"resource": conflict.Resource,
					"current":  conflict.Current,
					"limit":    conflict.Limit,
				})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to persist commercial entitlements"})
			return
		}
	}

	_, err = tx.Exec(r.Context(),
		`INSERT INTO settings (key, value, description, updated_at)
		 VALUES ($1, $2::jsonb, 'Offline UniRouter license document', NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $2::jsonb, description = EXCLUDED.description, updated_at = NOW()`,
		licenseSettingKey, string(normalized))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to save license"})
		return
	}

	auditResourceID, resourceIDHashed := globalControlAuditResourceID(licenseResourceID(status))
	if err := insertAuditEntry(r.Context(), tx, "license.update", "license", auditResourceID, map[string]interface{}{
		"scope":              "platform_global",
		"resource_id_hashed": resourceIDHashed,
		"before":             licenseAuditSummary(beforeStatus),
		"after":              licenseAuditSummary(status),
	}, globalControlAuditMeta(r)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	if h.licenseRuntime != nil {
		status = h.licenseRuntime.Replace(normalized)
	}
	h.emitLicenseWarningEvent(r.Context(), status)

	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) evaluateLicense(raw []byte) licensing.Status {
	if h.licenseRuntime != nil {
		return h.licenseRuntime.Evaluate(raw)
	}
	return h.licenseVerifierOrDefault().VerifyBytes(raw)
}

func (h *Handler) readStoredLicense(r *http.Request) ([]byte, error) {
	raw, _, err := readGlobalSetting(r.Context(), h.db, licenseSettingKey, false)
	return raw, err
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
		"enforceable":         status.Enforceable,
		"commercial":          status.Commercial,
		"signature_valid":     status.SignatureValid,
		"deployment_match":    status.DeploymentMatch,
		"warning":             status.Warning,
		"days_until_expiry":   status.DaysUntilExpiry,
		"verifier_configured": status.VerifierConfigured,
		"schema_version":      status.SchemaVersion,
		"key_id":              status.KeyID,
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
