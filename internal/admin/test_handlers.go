package admin

import (
	"net/http"
	"strings"
)

func (h *Handler) TestChannel(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func auditErrorSummary(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 512 {
		value = value[:512]
	}
	return value
}
