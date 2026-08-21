package modelrouting

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidMode       = errors.New("modelrouting: invalid authority mode")
	ErrInvalidRequest    = errors.New("modelrouting: invalid request")
	ErrUnavailable       = errors.New("modelrouting: snapshot unavailable")
	ErrNoRoute           = errors.New("modelrouting: no eligible route")
	ErrManifestStale     = errors.New("modelrouting: runtime manifest is stale")
	ErrManifestUnsealed  = errors.New("modelrouting: runtime manifest is not sealed")
	ErrParity            = errors.New("modelrouting: route parity mismatch")
	ErrAuthorityNotReady = errors.New("modelrouting: route authority is not ready")
)

// ParityError is safe to return to an operator: it contains identities and
// classifications, never URLs, adapter JSON, credentials, or credential hashes.
type ParityError struct {
	Drifts []Drift
}

func (e *ParityError) Error() string {
	if e == nil || len(e.Drifts) == 0 {
		return ErrParity.Error()
	}
	kinds := make([]string, 0, len(e.Drifts))
	for _, drift := range e.Drifts {
		kinds = append(kinds, drift.Kind)
	}
	return fmt.Sprintf("%s: %s", ErrParity, strings.Join(kinds, ","))
}

func (e *ParityError) Unwrap() error { return ErrParity }
