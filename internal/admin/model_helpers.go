package admin

import "strings"

func joinStrings(strs []string, sep string) string {
	return strings.Join(strs, sep)
}

// normalizeModels normalizes the models string:
// 1. Replace full-width commas with ASCII commas.
// 2. Split by comma, trim whitespace, and remove empty entries.
// 3. Deduplicate while preserving order.
func normalizeModels(models string) string {
	models = strings.ReplaceAll(models, "，", ",")
	parts := strings.Split(models, ",")
	seen := make(map[string]bool)
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	return strings.Join(result, ",")
}
