package releasetrust

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uniroute/unirouter/internal/signing"
)

const (
	ModeDevelopment = "development"
	ModeRelease     = "release"
)

// These values are intentionally populated only by release builds through
// -ldflags. Public verification keys are not secrets, but making them part of
// the signed binary prevents a customer-controlled runtime environment from
// replacing the trust root.
var (
	buildMode                    = ModeDevelopment
	embeddedLicenseKeyRingBase64 string
	embeddedUpgradeKeyRingBase64 string
)

type Bundle struct {
	Mode              string
	LicensePublicKeys string
	UpgradePublicKeys string
	LicenseKeyCount   int
	UpgradeKeyCount   int
}

// Resolve returns the effective public trust bundle. Release binaries accept
// only the key rings embedded at build time. Development binaries retain the
// explicit environment-key path so local tests and source development do not
// require production signing material.
func Resolve(_ string, runtimeUpgradeKeys string) (Bundle, error) {
	mode := strings.TrimSpace(buildMode)
	switch mode {
	case ModeRelease:
		if strings.TrimSpace(runtimeUpgradeKeys) != "" {
			return Bundle{}, fmt.Errorf("release binaries forbid runtime upgrade trust-root overrides")
		}
		upgradeJSON, upgradeCount, err := decodeEmbeddedRing(embeddedUpgradeKeyRingBase64, "embedded upgrade public-key ring")
		if err != nil {
			return Bundle{}, err
		}
		return Bundle{
			Mode:              mode,
			UpgradePublicKeys: upgradeJSON,
			UpgradeKeyCount:   upgradeCount,
		}, nil
	case ModeDevelopment:
		upgradeJSON, upgradeCount, err := normalizeRuntimeRing(runtimeUpgradeKeys, "UPGRADE_PUBLIC_KEYS")
		if err != nil {
			return Bundle{}, err
		}
		return Bundle{
			Mode:              mode,
			UpgradePublicKeys: upgradeJSON,
			UpgradeKeyCount:   upgradeCount,
		}, nil
	default:
		return Bundle{}, fmt.Errorf("unsupported embedded release trust mode %q", mode)
	}
}

func ensureDisjointSigningKeys(licenseJSON, upgradeJSON string) error {
	licenseKeys, err := signing.ParsePublicKeyRing(licenseJSON, "embedded License public-key ring")
	if err != nil {
		return err
	}
	upgradeKeys, err := signing.ParsePublicKeyRing(upgradeJSON, "embedded upgrade public-key ring")
	if err != nil {
		return err
	}
	for licenseKeyID, licenseKey := range licenseKeys {
		for upgradeKeyID, upgradeKey := range upgradeKeys {
			if bytes.Equal(licenseKey, upgradeKey) {
				return fmt.Errorf("release License key %q and upgrade key %q must use distinct Ed25519 public keys", licenseKeyID, upgradeKeyID)
			}
		}
	}
	return nil
}

func decodeEmbeddedRing(encoded, fieldName string) (string, int, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return "", 0, fmt.Errorf("%s is missing from this release binary", fieldName)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", 0, fmt.Errorf("%s is not valid base64url: %w", fieldName, err)
	}
	return normalizeRing(string(decoded), fieldName)
}

func normalizeRuntimeRing(value, fieldName string) (string, int, error) {
	if strings.TrimSpace(value) == "" {
		return "", 0, nil
	}
	return normalizeRing(value, fieldName)
}

func normalizeRing(value, fieldName string) (string, int, error) {
	ring, err := signing.ParsePublicKeyRing(value, fieldName)
	if err != nil {
		return "", 0, err
	}
	if len(ring) == 0 {
		return "", 0, fmt.Errorf("%s must contain at least one key", fieldName)
	}
	encoded := make(map[string]string, len(ring))
	for keyID, publicKey := range ring {
		value, err := signing.EncodePublicKey(publicKey)
		if err != nil {
			return "", 0, fmt.Errorf("encode %s[%s]: %w", fieldName, keyID, err)
		}
		encoded[keyID] = value
	}
	canonical, err := json.Marshal(encoded)
	if err != nil {
		return "", 0, fmt.Errorf("encode %s: %w", fieldName, err)
	}
	return string(canonical), len(ring), nil
}
