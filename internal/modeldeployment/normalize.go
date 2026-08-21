package modeldeployment

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

var ErrInvalidIdentity = errors.New("modeldeployment: invalid identity")

type SupportState string
type EvidenceState string

const (
	SupportEnabled     SupportState = "enabled"
	SupportUnsupported SupportState = "unsupported"

	EvidenceDeclared            EvidenceState = "declared"
	EvidenceProviderDocumented  EvidenceState = "provider_documented"
	EvidenceLiveProbed          EvidenceState = "live_probed"
	EvidenceConformanceVerified EvidenceState = "conformance_verified"
	EvidenceProductionApproved  EvidenceState = "production_approved"
)

// NormalizeIdentity trims operator input and rejects identities that cannot be
// stored or safely used as protocol/model identifiers. It deliberately does
// not case-fold: public, upstream, operation and capability IDs remain exact.
func NormalizeIdentity(value string, maxBytes int) (string, error) {
	value = strings.TrimSpace(value)
	if maxBytes <= 0 || value == "" || len(value) > maxBytes || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") {
		return "", ErrInvalidIdentity
	}
	return value, nil
}

func NormalizeOperationID(value string) (string, error) {
	return normalizeTypedIdentity("operation", value, 80)
}

func NormalizeCapabilityID(value string) (string, error) {
	return normalizeTypedIdentity("capability", value, 80)
}

func normalizeTypedIdentity(kind, value string, maxBytes int) (string, error) {
	normalized, err := NormalizeIdentity(value, maxBytes)
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, kind)
	}
	return normalized, nil
}

func ValidSupportState(value SupportState) bool {
	return value == SupportEnabled || value == SupportUnsupported
}

func ValidEvidenceState(value EvidenceState) bool {
	switch value {
	case EvidenceDeclared, EvidenceProviderDocumented, EvidenceLiveProbed,
		EvidenceConformanceVerified, EvidenceProductionApproved:
		return true
	default:
		return false
	}
}
