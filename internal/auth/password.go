package auth

import (
	"fmt"
	"unicode/utf8"
)

const (
	MinLocalPasswordCharacters = 12
	MaxLocalPasswordBytes      = 72
)

// ValidateLocalPassword enforces the common password boundary used by the
// admin API, self-service password changes and the recovery CLI. The upper
// bound is bcrypt's input limit; measuring the minimum in Unicode code points
// avoids treating one multi-byte character as several characters.
func ValidateLocalPassword(password string) error {
	if !utf8.ValidString(password) {
		return fmt.Errorf("password must be valid UTF-8")
	}
	if utf8.RuneCountInString(password) < MinLocalPasswordCharacters {
		return fmt.Errorf("password must be at least %d characters", MinLocalPasswordCharacters)
	}
	if len([]byte(password)) > MaxLocalPasswordBytes {
		return fmt.Errorf("password must be at most %d bytes", MaxLocalPasswordBytes)
	}
	return nil
}
