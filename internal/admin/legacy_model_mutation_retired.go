package admin

import (
	"encoding/json"
	"net/http"
)

const legacyModelMutationRetiredCode = "legacy_model_mutation_retired"

// writeLegacyModelMutationRetired is the final in-package fence for mutation
// handlers superseded by the canonical model-deployment APIs. Keeping this
// guard inside admin prevents an embedded caller from bypassing the retired
// HTTP route registrations and invoking the old writers directly.
func writeLegacyModelMutationRetired(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusGone)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"type":    "gone_error",
			"code":    legacyModelMutationRetiredCode,
			"message": "legacy model mutation ingress is retired; use the canonical model deployment APIs",
		},
	})
}
