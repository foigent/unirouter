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

func TestDevelopmentTrustAcceptsExplicitRuntimeRings(t *testing.T) {
	licenseRing := testRing(t, "license-dev")
	upgradeRing := testRing(t, "upgrade-dev")
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
	encoded, err := signing.EncodePublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{keyID: encoded})
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
