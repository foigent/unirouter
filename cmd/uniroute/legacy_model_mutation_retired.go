package main

import (
	"encoding/json"
	"net/http"
)

const legacyModelMutationRetiredCode = "legacy_model_mutation_retired"

// legacyModelMutationRetired is registered behind the existing admin
// authentication and platform-role boundary. Legacy reads remain available,
// but no route-authority mode can re-enable these superseded writers.
func legacyModelMutationRetired(w http.ResponseWriter, _ *http.Request) {
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
