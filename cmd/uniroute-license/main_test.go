package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/licensing"
)

func TestLicenseCLIKeygenIssueVerify(t *testing.T) {
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "license-private.key")
	publicPath := filepath.Join(dir, "license-public-keys.json")
	var out bytes.Buffer
	if err := run([]string{"keygen", "--key-id", "license-test-a", "--private-out", privatePath, "--public-out", publicPath}, &out); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(privatePath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("private key mode=%o", info.Mode().Perm())
		}
	}
	publicKeyRing, err := os.ReadFile(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	parsedRing, err := licensing.ParsePublicKeyRing(string(publicKeyRing))
	if err != nil || len(parsedRing["license-test-a"]) == 0 {
		t.Fatalf("generated public-key ring is not directly consumable: ring=%v err=%v", parsedRing, err)
	}
	payloadPath := filepath.Join(dir, "payload.json")
	payload, _ := json.Marshal(licensing.Payload{
		LicenseID:    "lic-test",
		Customer:     "Test Customer",
		DeploymentID: "test-deployment",
		Tier:         "enterprise",
		IssuedAt:     "2026-07-14T00:00:00Z",
		NotBefore:    "2026-07-14T00:00:00Z",
		ExpiresAt:    "2027-07-14T00:00:00Z",
		Features:     []string{licensing.FeatureAll},
		Limits:       map[string]int64{"active_projects": 20},
	})
	if err := os.WriteFile(payloadPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	licensePath := filepath.Join(dir, "license.json")
	out.Reset()
	if err := run([]string{"issue", "--key-id", "license-test-a", "--private-key", privatePath, "--payload", payloadPath, "--out", licensePath}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"verify", "--public-keys", publicPath, "--deployment-id", "test-deployment", "--license", licensePath, "--at", "2026-07-15T00:00:00Z"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "status=active") || !strings.Contains(out.String(), "license_id=lic-test") {
		t.Fatalf("unexpected verify output: %s", out.String())
	}
	if err := run([]string{"verify", "--public-keys", publicPath, "--deployment-id", "other", "--license", licensePath, "--at", "2026-07-15T00:00:00Z"}, &out); err == nil {
		t.Fatal("expected deployment mismatch")
	}
}

func TestLicenseCLIRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "private.key")
	publicPath := filepath.Join(dir, "public.key")
	if err := os.WriteFile(privatePath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"keygen", "--key-id", "license-test-a", "--private-out", privatePath, "--public-out", publicPath}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected existing private-key refusal")
	}
}
