package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/signing"
	"github.com/uniroute/unirouter/internal/upgradebundle"
)

type maintenancePackageFixture struct {
	input      communityMaintenanceInput
	cfg        *config.Config
	state      installedReleaseState
	source     upgradebundle.Manifest
	candidate  upgradebundle.Manifest
	recordedAt time.Time
	now        time.Time
	key        ed25519.PrivateKey
	sources    []upgradebundle.Source
}

func packMaintenanceFixture(t *testing.T, manifest upgradebundle.Manifest, key ed25519.PrivateKey, sources []upgradebundle.Source) (string, upgradebundle.Verification) {
	t.Helper()
	var raw bytes.Buffer
	require.NoError(t, upgradebundle.Pack(&raw, manifest, key, sources))
	p := filepath.Join(t.TempDir(), "release.tar.gz")
	require.NoError(t, os.WriteFile(p, raw.Bytes(), 0600))
	return p, upgradebundle.Verification{Manifest: manifest, BundleSHA256: installDigest(raw.Bytes())}
}

func installDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func newMaintenancePackageFixture(t *testing.T) maintenancePackageFixture {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	encoded, err := signing.EncodePublicKey(pub)
	require.NoError(t, err)
	ring, err := json.Marshal(map[string]string{"installed-fixture": encoded})
	require.NoError(t, err)
	dir := t.TempDir()
	payload := filepath.Join(dir, "uniroute")
	require.NoError(t, os.WriteFile(payload, []byte("synthetic-maintenance-payload"), 0600))
	sources := []upgradebundle.Source{{BundlePath: "payload/uniroute", FilePath: payload}}
	now := time.Now().UTC().Truncate(time.Second)
	// Historical finite source metadata has expired NOW, but was valid when its
	// immutable native release history recorded admission. Never relax target time.
	source, err := upgradebundle.CreateManifest(upgradebundle.ManifestOptions{KeyID: "installed-fixture", ReleaseSequence: 10, Version: "1.14.2", Commit: strings.Repeat("a", 40), CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-24 * time.Hour), TargetOS: runtime.GOOS, TargetArch: runtime.GOARCH, MinCurrentSequence: 9, MaxCurrentSequence: 9, MinCurrentSchema: schemaauthority.XInsightCommunicationHead, TargetSchema: schemaauthority.XInsightCommunicationHead}, sources)
	require.NoError(t, err)
	// One full signed package supports both fresh sequence 0 and the exact
	// installed M127 source. Native maintenance separately binds that source;
	// narrowing the manifest window would needlessly split the delivery path.
	candidate, err := upgradebundle.CreateManifest(upgradebundle.ManifestOptions{KeyID: "installed-fixture", ReleaseSequence: 11, Version: "1.15.0", Commit: strings.Repeat("b", 40), CreatedAt: now.Add(-time.Minute), NoExpiry: true, TargetOS: runtime.GOOS, TargetArch: runtime.GOARCH, MinCurrentSequence: 0, MaxCurrentSequence: 10, MinCurrentSchema: schemaauthority.XInsightCommunicationHead, TargetSchema: schemaauthority.UpstreamReleaseObservationHead}, sources)
	require.NoError(t, err)
	sourcePath, s := packMaintenanceFixture(t, source, key, sources)
	targetPath, c := packMaintenanceFixture(t, candidate, key, sources)
	cfg := &config.Config{RuntimeEnvironment: "production", RuntimeVersion: candidate.Version, RuntimeCommit: candidate.Commit, UpgradePublicKeys: string(ring)}
	ringSHA, err := communityTrustRingSHA256(cfg.UpgradePublicKeys)
	require.NoError(t, err)
	input := communityMaintenanceInput{SchemaVersion: communityMaintenanceSchema, InstallationID: "ufi_" + strings.Repeat("a", 32), InstallationPlanDigest: strings.Repeat("a", 64), SourcePackage: sourcePath, CandidatePackage: targetPath, ExpectedSequence: 10, ExpectedSourcePackageSHA256: s.BundleSHA256, ExpectedSourceManifestSHA256: communityManifestSHA256(source), ExpectedCandidatePackageSHA256: c.BundleSHA256, ExpectedCandidateManifestSHA256: communityManifestSHA256(candidate), ExpectedTrustedRingSHA256: ringSHA, ExpectedEnvironment: "production", ExpectedDatabaseSystemID: "123456789", ExpectedDatabase: "synthetic-installation", ExpectedDatabaseOID: 123, ExpectedIngressHead: "withdrawal-fixture"}
	return maintenancePackageFixture{input, cfg, installedReleaseState{ReleaseSequence: 10, Version: source.Version, Commit: source.Commit, PackageSHA256: s.BundleSHA256, TrustedKeyID: source.KeyID, TargetSchema: source.TargetSchema}, source, candidate, now.Add(-36 * time.Hour), now, key, sources}
}

func TestCommunityMaintenancePackageTrustAndFinitePredecessor(t *testing.T) {
	f := newMaintenancePackageFixture(t)
	require.NoError(t, f.input.validate(f.cfg))
	v, ring, err := verifyCommunityMaintenancePackages(f.input, f.cfg, f.state, f.source, f.recordedAt, f.now)
	require.NoError(t, err)
	require.Equal(t, int64(11), v.Manifest.ReleaseSequence)
	require.Equal(t, f.input.ExpectedTrustedRingSHA256, ring)
	for _, fault := range []string{"source_digest", "source_manifest", "source_sequence", "source_key", "ring", "candidate_digest", "candidate_manifest", "runtime_commit"} {
		t.Run(fault, func(t *testing.T) {
			in, cfg, state := f.input, *f.cfg, f.state
			switch fault {
			case "source_digest":
				in.ExpectedSourcePackageSHA256 = strings.Repeat("f", 64)
			case "source_manifest":
				in.ExpectedSourceManifestSHA256 = strings.Repeat("f", 64)
			case "source_sequence":
				state.ReleaseSequence++
			case "source_key":
				state.TrustedKeyID = "unrelated"
			case "ring":
				in.ExpectedTrustedRingSHA256 = strings.Repeat("f", 64)
			case "candidate_digest":
				in.ExpectedCandidatePackageSHA256 = strings.Repeat("f", 64)
			case "candidate_manifest":
				in.ExpectedCandidateManifestSHA256 = strings.Repeat("f", 64)
			case "runtime_commit":
				cfg.RuntimeCommit = strings.Repeat("f", 40)
			}
			_, _, err := verifyCommunityMaintenancePackages(in, &cfg, state, f.source, f.recordedAt, f.now)
			require.Error(t, err)
		})
	}
	for _, fault := range []string{"target_schema", "outside_sequence_window", "minimum_schema", "expired_target", "different_installed_key"} {
		t.Run(fault, func(t *testing.T) {
			in, cfg, m, key := f.input, *f.cfg, f.candidate, f.key
			switch fault {
			case "target_schema":
				m.TargetSchema = schemaauthority.XInsightCommunicationHead
			case "outside_sequence_window":
				m.MaxCurrentSequence = 9
			case "minimum_schema":
				m.MinCurrentSchema = schemaauthority.CommercialLicenseRetirementHead
			case "expired_target":
				m.SchemaVersion = upgradebundle.ManifestSchemaVersion
				m.CreatedAt = f.now.Add(-2 * time.Hour).Format(time.RFC3339)
				m.ExpiresAt = f.now.Add(-time.Hour).Format(time.RFC3339)
			case "different_installed_key":
				pub, newKey, e := ed25519.GenerateKey(rand.Reader)
				require.NoError(t, e)
				key = newKey
				m.KeyID = "also-valid-unrelated-key"
				var ring map[string]string
				require.NoError(t, json.Unmarshal([]byte(cfg.UpgradePublicKeys), &ring))
				ring[m.KeyID], e = signing.EncodePublicKey(pub)
				require.NoError(t, e)
				raw, e := json.Marshal(ring)
				require.NoError(t, e)
				cfg.UpgradePublicKeys = string(raw)
				in.ExpectedTrustedRingSHA256, e = communityTrustRingSHA256(cfg.UpgradePublicKeys)
				require.NoError(t, e)
			}
			path, v := packMaintenanceFixture(t, m, key, f.sources)
			in.CandidatePackage = path
			in.ExpectedCandidatePackageSHA256 = v.BundleSHA256
			in.ExpectedCandidateManifestSHA256 = communityManifestSHA256(m)
			_, _, err := verifyCommunityMaintenancePackages(in, &cfg, f.state, f.source, f.recordedAt, f.now)
			require.Error(t, err, "even a valid signature does not extend this maintenance authority")
		})
	}
}

func TestCommunityMaintenanceRejectsUnboundConfigurationBeforeDatabase(t *testing.T) {
	_, err := runCommunityMaintenance(context.Background(), nil, nil, communityMaintenanceInput{}, true)
	require.ErrorContains(t, err, "requires a nonempty exact writer batch")
	f := newMaintenancePackageFixture(t)
	cfg := *f.cfg
	cfg.RuntimeEnvironment = "test"
	require.Error(t, f.input.validate(&cfg))
	require.Error(t, readBoundedCLIJSON(strings.NewReader(`{"public_keys":{"attacker":"key"}}`), 64<<10, &communityMaintenanceInput{}))
	for _, raw := range []string{"", `{}`, `{"installed-fixture":"x","installed-fixture":"y"}`} {
		_, err := communityTrustRingSHA256(raw)
		require.Error(t, err)
	}
	in := f.input
	in.CandidatePackage = filepath.Join(t.TempDir(), "linked-package")
	require.NoError(t, os.Symlink(f.input.CandidatePackage, in.CandidatePackage))
	require.Error(t, in.validate(f.cfg))
}
