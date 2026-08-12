// Package modellist owns the canonical parsing contract for persisted model
// identifier lists. Identifiers remain case-sensitive; separators are not part
// of a valid identifier in UniRouter's list fields.
package modellist

import "strings"

// Parse accepts every separator historically exposed by the admin console and
// returns trimmed, first-seen identifiers without duplicates.
func Parse(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '\n', '\r':
			return true
		default:
			return false
		}
	})
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		model := strings.TrimSpace(part)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	return result
}

// Normalize returns the comma-separated storage representation.
func Normalize(value string) string {
	return strings.Join(Parse(value), ",")
}

// Contains performs exact, case-sensitive membership testing.
func Contains(value, wanted string) bool {
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		return false
	}
	for _, model := range Parse(value) {
		if model == wanted {
			return true
		}
	}
	return false
}
