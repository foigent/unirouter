package licensestudio

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/licensing"
)

func TestGenerateIssueAndVerifyLicenseFiles(t *testing.T) {
	directory := t.TempDir()
	generated, err := GenerateKeyFiles(directory, KeyPurposeLicense, "license-test-a")
	if err != nil {
		t.Fatal(err)
	}
	if generated.PrivateFile != "license-test-a.private" || generated.PublicFile != "license-test-a.public-keys.json" {
		t.Fatalf("unexpected generated filenames: %+v", generated)
	}
	if generated.BuildArgument != "UNIROUTER_LICENSE_TRUST_BUNDLE_B64" || len(generated.PublicFingerprint) != 64 {
		t.Fatalf("unexpected public release metadata: %+v", generated)
	}
	decodedRing, err := base64.RawURLEncoding.DecodeString(generated.BuildValue)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := licensing.ParsePublicKeyRing(string(decodedRing)); err != nil {
		t.Fatalf("generated build trust is not directly consumable: %v", err)
	}
	privatePath := filepath.Join(directory, generated.PrivateFile)
	publicPath := filepath.Join(directory, generated.PublicFile)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(privatePath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("private key mode=%o", info.Mode().Perm())
		}
	}
	payload := licensing.Payload{
		LicenseID:    "lic-test-customer",
		Customer:     "Test Customer",
		DeploymentID: "test-deployment",
		Tier:         "enterprise",
		IssuedAt:     "2026-07-15T00:00:00Z",
		NotBefore:    "2026-07-15T00:00:00Z",
		ExpiresAt:    "2027-07-15T23:59:59Z",
		Features:     []string{licensing.FeatureAll},
		Limits:       map[string]int64{licensing.LimitActiveProjects: 10},
	}
	licensePath := filepath.Join(directory, "customer.license.json")
	issued, err := IssueLicense(privatePath, licensePath, "license-test-a", payload)
	if err != nil {
		t.Fatal(err)
	}
	if issued.LicenseFile != filepath.Base(licensePath) || len(issued.LicenseSHA256) != 64 {
		t.Fatalf("unexpected issue result: %+v", issued)
	}
	status := VerifyLicense(publicPath, licensePath, "test-deployment", time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC))
	if !status.Valid || !status.Enforceable || !status.SignatureValid || !status.DeploymentMatch {
		t.Fatalf("generated License did not verify: %+v", status)
	}
	mismatch := VerifyLicense(publicPath, licensePath, "other-deployment", time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC))
	if mismatch.Status != licensing.StatusDeploymentMismatch || mismatch.Valid {
		t.Fatalf("expected deployment mismatch, got %+v", mismatch)
	}
}

func TestGenerateKeyFilesSeparatesPurposeAndRefusesOverwrite(t *testing.T) {
	directory := t.TempDir()
	result, err := GenerateKeyFiles(directory, KeyPurposeUpgrade, "upgrade-test-a")
	if err != nil {
		t.Fatal(err)
	}
	if result.BuildArgument != "UNIROUTER_UPGRADE_TRUST_BUNDLE_B64" {
		t.Fatalf("unexpected upgrade build argument: %+v", result)
	}
	if _, err := GenerateKeyFiles(directory, KeyPurposeUpgrade, "upgrade-test-a"); err == nil || !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("expected overwrite refusal, got %v", err)
	}
	if _, err := GenerateKeyFiles(directory, "combined", "shared-key"); err == nil {
		t.Fatal("expected unsupported combined key purpose rejection")
	}
}

func TestIssueLicenseRefusesOverwriteAndInvalidPrivateKey(t *testing.T) {
	directory := t.TempDir()
	generated, err := GenerateKeyFiles(directory, KeyPurposeLicense, "license-test-a")
	if err != nil {
		t.Fatal(err)
	}
	payload := licensing.Payload{
		LicenseID: "lic-test", Customer: "Customer", DeploymentID: "deployment", Tier: "enterprise",
		IssuedAt: "2026-07-15T00:00:00Z", NotBefore: "2026-07-15T00:00:00Z", ExpiresAt: "2027-07-15T00:00:00Z",
		Features: []string{licensing.FeatureGateway},
	}
	output := filepath.Join(directory, "customer.license.json")
	if _, err := IssueLicense(filepath.Join(directory, generated.PrivateFile), output, generated.KeyID, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := IssueLicense(filepath.Join(directory, generated.PrivateFile), output, generated.KeyID, payload); err == nil || !strings.Contains(err.Error(), "exist") {
		t.Fatalf("expected output overwrite refusal, got %v", err)
	}
	invalidPrivate := filepath.Join(directory, "invalid.private")
	if err := os.WriteFile(invalidPrivate, []byte("not-a-private-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := IssueLicense(invalidPrivate, filepath.Join(directory, "invalid.license.json"), generated.KeyID, payload); err == nil || !strings.Contains(err.Error(), "parse private key") {
		t.Fatalf("expected invalid private key rejection, got %v", err)
	}
}
