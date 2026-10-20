//go:build integration

package main

import (
	"bytes"
	"context"
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

	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/signing"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/upgradebundle"
)

func TestUpgradeRecordAppliedPinsRuntimeDigestSequenceAndSchema(t *testing.T) {
	for _, noExpiry := range []bool{false, true} {
		t.Run(fmt.Sprintf("noExpiry=%v", noExpiry), func(t *testing.T) { testUpgradeRecordApplied(t, noExpiry) })
	}
}

func testUpgradeRecordApplied(t *testing.T, noExpiry bool) {
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
	expires := now.Add(time.Hour)
	if noExpiry {
		expires = time.Time{}
	}
	manifest, err := upgradebundle.CreateManifest(upgradebundle.ManifestOptions{
		KeyID: "upgrade-integration", ReleaseSequence: 1, Version: version, Commit: commit,
		CreatedAt: now.Add(-time.Minute), ExpiresAt: expires, NoExpiry: noExpiry,
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

	validCfg := &config.Config{RuntimeVersion: version, RuntimeCommit: commit, UpgradePublicKeys: string(ringJSON)}
	if err := runInstalledUpgradeVerify(ctx, []string{"--package", packagePath}, validCfg, pool); err != nil {
		t.Fatal(err)
	}
	// Each signed failure must leave both committed release tables unchanged.
	for _, kind := range []string{"expired-v1", "future", "schema", "key", "payload"} {
		m := manifest
		switch kind {
		case "expired-v1":
			m.SchemaVersion = upgradebundle.ManifestSchemaVersion
			m.CreatedAt = now.Add(-2 * time.Hour).Format(time.RFC3339)
			m.ExpiresAt = now.Add(-time.Hour).Format(time.RFC3339)
		case "future":
			m.CreatedAt = now.Add(6 * time.Minute).Format(time.RFC3339)
		case "schema":
			m.MinCurrentSchema = "999999"
			m.TargetSchema = "999999"
		}
		var candidate bytes.Buffer
		key := privateKey
		if kind == "key" {
			_, key, err = ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := upgradebundle.Pack(&candidate, m, key, sources); err != nil {
			t.Fatal(err)
		}
		candidatePath := filepath.Join(dir, kind+".tar.gz")
		if kind == "payload" {
			// A compressed-byte flip exercises corrupt-package refusal without a state write.
			raw := candidate.Bytes()
			raw[len(raw)/2] ^= 1
		}
		if err := os.WriteFile(candidatePath, candidate.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		if err := runInstalledUpgradeVerify(ctx, []string{"--package", candidatePath}, validCfg, pool); err == nil {
			t.Fatalf("installed verify accepted %s", kind)
		}
		if err := runUpgradeRecordApplied(ctx, []string{"--package", candidatePath, "--recorded-by", "integration-test"}, validCfg, pool); err == nil {
			t.Fatalf("record accepted %s", kind)
		}
		state, err := readInstalledReleaseState(ctx, pool, false)
		if err != nil || state.ReleaseSequence != 0 {
			t.Fatalf("failed %s changed state: %#v %v", kind, state, err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM release_history`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed %s changed history: %d %v", kind, count, err)
		}
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

	var recorded upgradebundle.Manifest
	var recordedRaw []byte
	if err := pool.QueryRow(ctx, `SELECT manifest FROM release_history WHERE release_sequence = 1`).Scan(&recordedRaw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(recordedRaw, &recorded); err != nil || recorded.SchemaVersion != manifest.SchemaVersion || recorded.ExpiresAt != manifest.ExpiresAt {
		t.Fatalf("stored policy: %#v %v", recorded, err)
	}
	err = runUpgradeRecordApplied(ctx, []string{"--package", packagePath, "--recorded-by", "integration-test"}, cfg, pool)
	if err == nil || !strings.Contains(err.Error(), "outside signed window") {
		t.Fatalf("replay error = %v, want rollback/window rejection", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM release_history`).Scan(&historyCount); err != nil || historyCount != 1 {
		t.Fatalf("replay changed single history row: %d %v", historyCount, err)
	}

}

func TestUpgradeLegacyRollbackPreflightRequiresFeedCredentialDisablement(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var orgID, projectID, keyID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('legacy-rollback-cli', 'active', 'standard')
		 RETURNING id`,
	).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, status, budget_period)
		 VALUES ($1, 'Legacy rollback CLI', 'legacy-rollback-cli', 'active', 'none')
		 RETURNING id`,
		orgID,
	).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO api_keys (
		    org_id, project_id, key_type, name, key_prefix, key_hash,
		    status, allowed_ips, allowed_models, purpose
		 ) VALUES (
		    $1, $2, 'service', 'X-Insight feed', 'ur-feed-cli',
		    'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
		    'feed_active', '127.0.0.1/32', '', 'xinsight.feed.read'
		 ) RETURNING id`,
		orgID, projectID,
	).Scan(&keyID); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DB_DSN:           pool.Config().ConnString(),
		DBMaxConnections: 2,
	}
	err := runUpgradeCLI(ctx, []string{"preflight-legacy-rollback"}, cfg)
	if err == nil || !strings.Contains(err.Error(), "disable the exact feed credential") {
		t.Fatalf("active purpose credential did not stop legacy rollback: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`UPDATE api_keys SET status = 'disabled' WHERE id = $1`,
		keyID,
	); err != nil {
		t.Fatal(err)
	}
	if err := runUpgradeCLI(ctx, []string{"preflight-legacy-rollback"}, cfg); err != nil {
		t.Fatalf("disabled feed credential should permit binary rollback: %v", err)
	}
}
