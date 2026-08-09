package admin

import (
	"errors"
	"net/http"

	"github.com/uniroute/unirouter/internal/accesssponsor"
)

func writeSponsorTransitionConflict(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, accesssponsor.ErrPostTransitionOrphaned) {
		return false
	}
	writeJSON(w, http.StatusConflict, map[string]interface{}{
		"error": "Workload sponsorship must be reassigned or revoked, and pending sponsored requests must be closed, before this account or role transition",
		"code":  "active_workload_sponsorship",
	})
	return true
}
