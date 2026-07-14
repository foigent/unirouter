//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/signing"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/upgradebundle"
)

func TestUpgradeRecordAppliedPinsRuntimeDigestSequenceAndSchema(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	schema, err := readCurrentSchema(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicText, err := signing.EncodePublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	ringJSON, err := json.Marshal(map[string]string{"upgrade-integration": publicText})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "uniroute")
	packagePath := filepath.Join(dir, "release.tar.gz")
	if err := os.WriteFile(payloadPath, []byte("runtime-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	sources := []upgradebundle.Source{{BundlePath: "payload/uniroute", FilePath: payloadPath}}
	now := time.Now().UTC().Truncate(time.Second)
	version := "1.0.0-integration"
	commit := strings.Repeat("c", 40)
	manifest, err := upgradebundle.CreateManifest(upgradebundle.ManifestOptions{
		KeyID: "upgrade-integration", ReleaseSequence: 1, Version: version, Commit: commit,
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		TargetOS: runtime.GOOS, TargetArch: runtime.GOARCH,
		MinCurrentSequence: 0, MaxCurrentSequence: 0,
		MinCurrentSchema: schema, TargetSchema: schema,
	}, sources)
	if err != nil {
		t.Fatal(err)
	}
	var bundle bytes.Buffer
	if err := upgradebundle.Pack(&bundle, manifest, privateKey, sources); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, bundle.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	wrongRuntime := &config.Config{RuntimeVersion: "wrong", RuntimeCommit: commit, UpgradePublicKeys: string(ringJSON)}
	err = runUpgradeRecordApplied(ctx, []string{"--package", packagePath, "--recorded-by", "integration-test"}, wrongRuntime, pool)
	if err == nil || !strings.Contains(err.Error(), "does not match signed package") {
		t.Fatalf("runtime mismatch error = %v", err)
	}
	state, err := readInstalledReleaseState(ctx, pool, false)
	if err != nil {
		t.Fatal(err)
	}
	if state.ReleaseSequence != 0 {
		t.Fatalf("mismatched runtime advanced state: %#v", state)
	}

	cfg := &config.Config{RuntimeVersion: version, RuntimeCommit: commit, UpgradePublicKeys: string(ringJSON)}
	if err := runUpgradeRecordApplied(ctx, []string{"--package", packagePath, "--recorded-by", "integration-test"}, cfg, pool); err != nil {
		t.Fatal(err)
	}
	state, err = readInstalledReleaseState(ctx, pool, false)
	if err != nil {
		t.Fatal(err)
	}
	if state.ReleaseSequence != 1 || state.Version != version || state.Commit != commit || state.PackageSHA256 == "" || state.TargetSchema != schema || state.TrustedKeyID != "upgrade-integration" {
		t.Fatalf("unexpected recorded release state: %#v", state)
	}
	var historyCount int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM release_history WHERE release_sequence = 1 AND package_sha256 = $1`, state.PackageSHA256).Scan(&historyCount); err != nil {
		t.Fatal(err)
	}
	if historyCount != 1 {
		t.Fatalf("history count = %d, want 1", historyCount)
	}

	err = runUpgradeRecordApplied(ctx, []string{"--package", packagePath, "--recorded-by", "integration-test"}, cfg, pool)
	if err == nil || !strings.Contains(err.Error(), "outside signed window") {
		t.Fatalf("replay error = %v, want rollback/window rejection", err)
	}
}
