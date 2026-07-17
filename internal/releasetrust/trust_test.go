package releasetrust

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/signing"
)

func TestReleaseTrustUsesOnlyEmbeddedRings(t *testing.T) {
	licenseRing := testRing(t, "license-2026-a")
	upgradeRing := testRing(t, "upgrade-2026-a")
	setBuildTrustForTest(t, ModeRelease, encodeRing(licenseRing), encodeRing(upgradeRing))

	bundle, err := Resolve("", "")
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Mode != ModeRelease || bundle.LicenseKeyCount != 1 || bundle.UpgradeKeyCount != 1 {
		t.Fatalf("unexpected release trust bundle: %+v", bundle)
	}
	if !strings.Contains(bundle.LicensePublicKeys, "license-2026-a") || !strings.Contains(bundle.UpgradePublicKeys, "upgrade-2026-a") {
		t.Fatalf("embedded rings were not resolved: %+v", bundle)
	}

	if _, err := Resolve(licenseRing, ""); err == nil || !strings.Contains(err.Error(), "forbid runtime") {
		t.Fatalf("expected runtime License trust override rejection, got %v", err)
	}
	if _, err := Resolve("", upgradeRing); err == nil || !strings.Contains(err.Error(), "forbid runtime") {
		t.Fatalf("expected runtime upgrade trust override rejection, got %v", err)
	}
}

func TestReleaseTrustFailsClosedWithoutEmbeddedRings(t *testing.T) {
	setBuildTrustForTest(t, ModeRelease, "", "")
	if _, err := Resolve("", ""); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing embedded trust failure, got %v", err)
	}
}

func TestReleaseTrustRejectsLicenseAndUpgradeKeyReuse(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	licenseRing := testRingForPublicKey(t, "license-2026-a", publicKey)
	upgradeRing := testRingForPublicKey(t, "upgrade-2026-a", publicKey)
	setBuildTrustForTest(t, ModeRelease, encodeRing(licenseRing), encodeRing(upgradeRing))

	if _, err := Resolve("", ""); err == nil || !strings.Contains(err.Error(), "must use distinct") {
		t.Fatalf("expected cross-purpose signing-key reuse rejection, got %v", err)
	}
}

func TestReleaseTrustAllowsSameKeyIDWithDistinctKeys(t *testing.T) {
	licensePublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	upgradePublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	licenseRing := testRingForPublicKey(t, "shared-key-id", licensePublicKey)
	upgradeRing := testRingForPublicKey(t, "shared-key-id", upgradePublicKey)
	setBuildTrustForTest(t, ModeRelease, encodeRing(licenseRing), encodeRing(upgradeRing))

	if _, err := Resolve("", ""); err != nil {
		t.Fatalf("distinct signing keys with the same ring-local ID must remain valid: %v", err)
	}
}

func TestReleaseTrustRejectsOneOverlapAcrossMultiKeyRings(t *testing.T) {
	sharedPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	licenseOnlyPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	upgradeOnlyPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	licenseRing := testRingForPublicKeys(t, map[string]ed25519.PublicKey{
		"license-only":   licenseOnlyPublicKey,
		"license-shared": sharedPublicKey,
	})
	upgradeRing := testRingForPublicKeys(t, map[string]ed25519.PublicKey{
		"upgrade-only":   upgradeOnlyPublicKey,
		"upgrade-shared": sharedPublicKey,
	})
	setBuildTrustForTest(t, ModeRelease, encodeRing(licenseRing), encodeRing(upgradeRing))

	if _, err := Resolve("", ""); err == nil || !strings.Contains(err.Error(), "must use distinct") {
		t.Fatalf("expected overlap in multi-key rings to be rejected, got %v", err)
	}
}

func TestDevelopmentTrustAcceptsExplicitRuntimeRings(t *testing.T) {
	sharedPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	licenseRing := testRingForPublicKey(t, "license-dev", sharedPublicKey)
	upgradeRing := testRingForPublicKey(t, "upgrade-dev", sharedPublicKey)
	setBuildTrustForTest(t, ModeDevelopment, "", "")

	bundle, err := Resolve(licenseRing, upgradeRing)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Mode != ModeDevelopment || bundle.LicenseKeyCount != 1 || bundle.UpgradeKeyCount != 1 {
		t.Fatalf("unexpected development trust bundle: %+v", bundle)
	}
}

func TestReleaseTrustRejectsUnknownBuildModeAndMalformedBundle(t *testing.T) {
	setBuildTrustForTest(t, "customer-controlled", "", "")
	if _, err := Resolve("", ""); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported mode error, got %v", err)
	}

	setBuildTrustForTest(t, ModeRelease, "***", "***")
	if _, err := Resolve("", ""); err == nil || !strings.Contains(err.Error(), "base64url") {
		t.Fatalf("expected malformed bundle error, got %v", err)
	}
}

func testRing(t *testing.T, keyID string) string {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return testRingForPublicKey(t, keyID, publicKey)
}

func testRingForPublicKey(t *testing.T, keyID string, publicKey ed25519.PublicKey) string {
	t.Helper()
	return testRingForPublicKeys(t, map[string]ed25519.PublicKey{keyID: publicKey})
}

func testRingForPublicKeys(t *testing.T, keys map[string]ed25519.PublicKey) string {
	t.Helper()
	encodedKeys := make(map[string]string, len(keys))
	for keyID, publicKey := range keys {
		encoded, err := signing.EncodePublicKey(publicKey)
		if err != nil {
			t.Fatal(err)
		}
		encodedKeys[keyID] = encoded
	}
	raw, err := json.Marshal(encodedKeys)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func encodeRing(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func setBuildTrustForTest(t *testing.T, mode, licenseRing, upgradeRing string) {
	t.Helper()
	oldMode := buildMode
	oldLicense := embeddedLicenseKeyRingBase64
	oldUpgrade := embeddedUpgradeKeyRingBase64
	buildMode = mode
	embeddedLicenseKeyRingBase64 = licenseRing
	embeddedUpgradeKeyRingBase64 = upgradeRing
	t.Cleanup(func() {
		buildMode = oldMode
		embeddedLicenseKeyRingBase64 = oldLicense
		embeddedUpgradeKeyRingBase64 = oldUpgrade
	})
}
