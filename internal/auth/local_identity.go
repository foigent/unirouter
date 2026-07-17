package auth

import (
	"errors"
	"strings"
)

// ErrLocalLoginIDRequired is returned when a local username contains no
// visible characters after outer Unicode whitespace is removed.
var ErrLocalLoginIDRequired = errors.New("local login ID is required")

// NormalizeLocalUsername prepares a local username for storage and lookup.
// It removes outer Unicode whitespace while preserving the user's display
// casing. PostgreSQL owns case folding through normalize_local_login_id(), so
// application callers must not perform a second, potentially different fold.
func NormalizeLocalUsername(value string) (string, error) {
	username := strings.TrimSpace(value)
	if username == "" {
		return "", ErrLocalLoginIDRequired
	}
	return username, nil
}
