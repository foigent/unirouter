package modelrouting

import (
	"fmt"
	"strings"

	"github.com/uniroute/unirouter/internal/executionevidence"
)

// Mode is a startup-selected routing authority. It is intentionally not a
// database flag: changing authority requires an explicit process rollout.
type Mode = executionevidence.AuthorityMode

const (
	ModeLegacy    = executionevidence.ModeLegacy
	ModeVerify    = executionevidence.ModeVerify
	ModeCanonical = executionevidence.ModeCanonical
)

// ParseMode accepts only the three supported authority modes. The caller owns
// the default so an empty environment value can never silently change traffic.
func ParseMode(value string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case ModeLegacy, ModeVerify, ModeCanonical:
		return mode, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidMode, value)
	}
}
