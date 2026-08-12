package admin

import (
	"strings"

	"github.com/uniroute/unirouter/internal/modellist"
)

func joinStrings(strs []string, sep string) string {
	return strings.Join(strs, sep)
}

// normalizeModels returns the canonical comma-separated storage form used by
// channels and channel credentials.
func normalizeModels(models string) string {
	return modellist.Normalize(models)
}
