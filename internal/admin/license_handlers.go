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
	"github.com/uniroute/unirouter/internal/strictjson"
)

const licenseSettingKey = "license.offline"
const maxLicenseImportBytes = 256 << 10

// ReconcileStoredCommercialLicense is retained for historical verification and
// pre-M126 fixtures. The open-source server never calls it during startup or
// restore, and the deprecated License HTTP endpoint does not import documents.
func (h *Handler) ReconcileStoredCommercialLicense(ctx context.Context) (licensing.Status, error) {
	return h.syncStoredCommercialLicense(ctx, true)
}

// RefreshStoredCommercialLicense retains the historical projection consistency
// check. It is not scheduled or used for admission by the open-source server.
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
	writeJSON(w, http.StatusOK, licensing.NotRequiredStatus())
}

func (h *Handler) UpdateLicense(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformGlobalControl(w, r) {
		return
	}
	var req struct {
		License json.RawMessage `json:"license"`
	}
	wire, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxLicenseImportBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		code := http.StatusBadRequest
		if errors.As(err, &tooLarge) {
			code = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, code, map[string]interface{}{"error": "Invalid or oversized License import"})
		return
	}
	if strictjson.Validate(wire) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	decoder := json.NewDecoder(strings.NewReader(string(wire)))
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

	// Compatibility only: consume a well-formed object without storing, validating
	// signatures, changing entitlements or emitting an import/expiry event.
	var object map[string]json.RawMessage
	if strictjson.Validate(raw) != nil || json.Unmarshal(raw, &object) != nil || len(object) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "license must be a nonempty JSON object"})
		return
	}
	writeJSON(w, http.StatusOK, struct {
		licensing.Status
		Imported bool   `json:"imported"`
		Message  string `json:"message"`
	}{Status: licensing.NotRequiredStatus(), Imported: false, Message: "Commercial licenses are not required; no document was imported."})

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
