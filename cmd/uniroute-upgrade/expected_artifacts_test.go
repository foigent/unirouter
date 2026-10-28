package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/upgradebundle"
)

func TestExpectedArtifactInventoryRejectsDriftBeforePrivateKeyOrOutput(t *testing.T) {
	for _, kind := range []string{"bytes", "path", "size", "media", "extra", "missing", "duplicate", "unknown", "ambiguous"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source")
			inventory := filepath.Join(dir, "expected.json")
			out := filepath.Join(dir, "new-package.tar.gz")
			original := []byte("synthetic selected material")
			sum := sha256.Sum256(original)
			expected := []upgradebundle.Artifact{{Path: "payload/uniroute", Size: int64(len(original)), SHA256: hex.EncodeToString(sum[:])}}
			if err := os.WriteFile(source, original, 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "bytes":
				if err := os.WriteFile(source, []byte("mutated after selection"), 0600); err != nil {
					t.Fatal(err)
				}
			case "path":
				expected[0].Path = "payload/other"
			case "size":
				expected[0].Size++
			case "media":
				expected[0].MediaType = "application/json"
			case "extra", "duplicate":
				expected = append(expected, expected[0])
			case "missing":
				expected = nil
			}
			raw, _ := json.Marshal(expected)
			if kind == "unknown" {
				raw = []byte(`[{"path":"payload/uniroute","size":27,"sha256":"` + expected[0].SHA256 + `","approved":true}]`)
			}
			if kind == "ambiguous" {
				raw = []byte(`[{"path":"payload/uniroute","path":"payload/uniroute","size":27,"sha256":"` + expected[0].SHA256 + `"}]`)
			}
			if err := os.WriteFile(inventory, raw, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"pack", "--key-id", "synthetic-materials", "--private-key", filepath.Join(dir, "NONEXISTENT-KEY"), "--release-sequence", "17", "--version", "1.15.0", "--commit", strings.Repeat("a", 40), "--created-at", "2026-10-28T00:00:00Z", "--no-expiry", "--target-os", "linux", "--target-arch", "amd64", "--min-current-sequence", "0", "--max-current-sequence", "16", "--min-current-schema", "000127_xinsight_communication_policy", "--target-schema", "000128_upstream_release_observation", "--artifact", "payload/uniroute=" + source, "--expected-artifacts", inventory, "--out", out}
			err := run(args, &bytes.Buffer{})
			if err == nil || strings.Contains(err.Error(), "private key") || !strings.Contains(err.Error(), "inventory") {
				t.Fatalf("expected pre-key inventory refusal, got %v", err)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatalf("inventory refusal created an output: %v", err)
			}
		})
	}
}
