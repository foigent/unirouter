package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/signing"
)

func TestPackAndVerifyCLI(t *testing.T) {
	for _, noExpiry := range []bool{false, true} {
		t.Run(fmt.Sprintf("noExpiry=%v", noExpiry), func(t *testing.T) { testPackAndVerifyCLI(t, noExpiry) })
	}
}

func testPackAndVerifyCLI(t *testing.T, noExpiry bool) {
	dir := t.TempDir()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateText, err := signing.EncodePrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	publicText, err := signing.EncodePublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(dir, "private.key")
	publicRingPath := filepath.Join(dir, "public-keys.json")
	payloadPath := filepath.Join(dir, "uniroute")
	packagePath := filepath.Join(dir, "uniroute-1.0.0.tar.gz")
	if err := os.WriteFile(privatePath, []byte(privateText+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ringJSON, err := json.Marshal(map[string]string{"upgrade-test": publicText})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicRingPath, ringJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payloadPath, []byte("test-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	var packOutput bytes.Buffer
	args := []string{
		"pack",
		"--key-id", "upgrade-test",
		"--private-key", privatePath,
		"--release-sequence", "1",
		"--version", "1.0.0",
		"--commit", strings.Repeat("a", 40),
		"--created-at", now.Add(-time.Minute).Format(time.RFC3339),
		"--expires-at", now.Add(time.Hour).Format(time.RFC3339),
		"--target-os", runtime.GOOS,
		"--target-arch", runtime.GOARCH,
		"--min-current-sequence", "0",
		"--max-current-sequence", "0",
		"--min-current-schema", "000036_v1_micro_precision_ledger",
		"--target-schema", "000037_v1_commercial_release_control",
		"--artifact", "payload/uniroute=" + payloadPath,
		"--out", packagePath,
	}
	if noExpiry {
		for i, v := range args {
			if v == "--expires-at" {
				args = append(append(args[:i:i], "--no-expiry"), args[i+2:]...)
				break
			}
		}
	}
	err = run(args, &packOutput)
	if err != nil {
		t.Fatalf("pack error = %v", err)
	}
	if !strings.Contains(packOutput.String(), "release_sequence=1") {
		t.Fatalf("pack output = %q", packOutput.String())
	}
	if info, err := os.Stat(packagePath); err != nil || info.Size() == 0 {
		t.Fatalf("package stat = %v, info = %#v", err, info)
	}

	file, err := os.Open(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	if _, err := tr.Next(); err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SchemaVersion string `json:"schema_version"`
		ExpiresAt     string `json:"expires_at"`
	}
	if err := json.NewDecoder(tr).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if noExpiry {
		if manifest.SchemaVersion != "unirouter.upgrade-manifest.v2" || manifest.ExpiresAt != "never" {
			t.Fatalf("non-expiring wire: %#v", manifest)
		}
	} else if manifest.SchemaVersion != "unirouter.upgrade-manifest.v1" {
		t.Fatal("finite wire changed")
	}
	var verifyOutput bytes.Buffer
	err = run([]string{
		"verify",
		"--public-keys", publicRingPath,
		"--package", packagePath,
		"--target-os", runtime.GOOS,
		"--target-arch", runtime.GOARCH,
		"--current-sequence", "0",
		"--current-schema", "000036_v1_micro_precision_ledger",
		"--at", now.Format(time.RFC3339),
	}, &verifyOutput)
	if err != nil {
		t.Fatalf("verify error = %v", err)
	}
	if !strings.Contains(verifyOutput.String(), "verified=true") || !strings.Contains(verifyOutput.String(), "package_sha256=") {
		t.Fatalf("verify output = %q", verifyOutput.String())
	}

	if err := run([]string{
		"pack", "--key-id", "upgrade-test", "--private-key", privatePath,
		"--release-sequence", "1", "--version", "1.0.0", "--commit", strings.Repeat("a", 40),
		"--created-at", now.Add(-time.Minute).Format(time.RFC3339), "--expires-at", now.Add(time.Hour).Format(time.RFC3339),
		"--target-os", runtime.GOOS, "--target-arch", runtime.GOARCH,
		"--min-current-sequence", "0", "--max-current-sequence", "0",
		"--min-current-schema", "000036_v1_micro_precision_ledger", "--target-schema", "000037_v1_commercial_release_control",
		"--artifact", "payload/uniroute=" + payloadPath, "--out", packagePath,
	}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "file exists") {
		t.Fatalf("overwrite error = %v, want file exists", err)
	}
}

func TestVerifyCLIRejectsTamperedPackage(t *testing.T) {
	dir := t.TempDir()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateText, _ := signing.EncodePrivateKey(privateKey)
	publicText, _ := signing.EncodePublicKey(publicKey)
	privatePath := filepath.Join(dir, "private.key")
	publicRingPath := filepath.Join(dir, "public-keys.json")
	payloadPath := filepath.Join(dir, "payload")
	packagePath := filepath.Join(dir, "package.tar.gz")
	if err := os.WriteFile(privatePath, []byte(privateText), 0o600); err != nil {
		t.Fatal(err)
	}
	ringJSON, _ := json.Marshal(map[string]string{"upgrade-test": publicText})
	if err := os.WriteFile(publicRingPath, ringJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payloadPath, []byte(strings.Repeat("payload", 100)), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	if err := run([]string{
		"pack", "--key-id", "upgrade-test", "--private-key", privatePath,
		"--release-sequence", "1", "--version", "1.0.0", "--commit", strings.Repeat("a", 40),
		"--created-at", now.Add(-time.Minute).Format(time.RFC3339), "--expires-at", now.Add(time.Hour).Format(time.RFC3339),
		"--target-os", runtime.GOOS, "--target-arch", runtime.GOARCH,
		"--min-current-sequence", "0", "--max-current-sequence", "0",
		"--min-current-schema", "000036_v1_micro_precision_ledger", "--target-schema", "000037_v1_commercial_release_control",
		"--artifact", "payload/uniroute=" + payloadPath, "--out", packagePath,
	}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	tampered, err := os.ReadFile(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	tampered[len(tampered)/2] ^= 0xff
	tamperedPath := filepath.Join(dir, "tampered.tar.gz")
	if err := os.WriteFile(tamperedPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	err = run([]string{
		"verify", "--public-keys", publicRingPath, "--package", tamperedPath,
		"--target-os", runtime.GOOS, "--target-arch", runtime.GOARCH,
		"--current-sequence", "0", "--current-schema", "000036_v1_micro_precision_ledger",
		"--at", now.Format(time.RFC3339),
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("tampered package unexpectedly verified")
	}
}

func TestPackLifetimeSelectionBeforePrivateKeyOrOutput(t *testing.T) {
	dir := t.TempDir()
	base := []string{"pack", "--key-id", "synthetic", "--private-key", filepath.Join(dir, "MUST-NOT-READ"),
		"--release-sequence", "1", "--version", "1.14.0", "--commit", strings.Repeat("a", 40),
		"--created-at", "2026-10-20T00:00:00Z", "--target-os", "linux", "--target-arch", "amd64",
		"--min-current-sequence", "0", "--max-current-sequence", "0", "--min-current-schema", "000127", "--target-schema", "000127",
		"--artifact", "payload/example=" + filepath.Join(dir, "MUST-NOT-HASH"), "--out", filepath.Join(dir, "MUST-NOT-CREATE")}
	for _, tc := range []struct {
		name  string
		flags []string
		want  string
	}{
		{"absent", nil, "explicit --no-expiry"},
		{"false", []string{"--no-expiry=false"}, "explicit --no-expiry"},
		{"conflict", []string{"--no-expiry", "--expires-at", "2026-10-21T00:00:00Z"}, "conflicts"},
		{"empty conflict", []string{"--no-expiry", "--expires-at="}, "conflicts"},
		{"empty finite", []string{"--expires-at="}, "explicit --no-expiry"},
		{"bad finite", []string{"--expires-at=never"}, "must be RFC3339"},
		{"bad creation", []string{"--no-expiry", "--created-at=never"}, "--created-at must be RFC3339"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := run(append(append([]string{}, base...), tc.flags...), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v, want %s", err, tc.want)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatal("invalid lifetime created output")
			}
		})
	}
}
