package admin

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/providerpack"
)

// ListProviderPacks exposes immutable, credential-free compatibility claims.
// A fixture-verified pack is never presented as live network or billing proof.
func (h *Handler) ListProviderPacks(w http.ResponseWriter, r *http.Request) {
	packs, err := providerpack.Catalog()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": "Provider pack catalog is unavailable",
		})
		return
	}
	reportDigest, err := providerpack.GoldenReportDigest()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": "Provider pack catalog is unavailable",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data":  packs,
		"total": len(packs),
		"claim_boundary": map[string]interface{}{
			"conformance_level": "fixture_verified",
			"network_verified":  false,
			"live_calls":        false,
			"credentials_used":  false,
			"report_sha256":     reportDigest,
		},
	})
}
