package upgradebundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type bundleFixture struct {
	bundle     []byte
	manifest   Manifest
	sources    []Source
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
	now        time.Time
}

func TestPackVerifyAndDeterminism(t *testing.T) {
	fixture := newBundleFixture(t)
	verification, err := Verify(bytes.NewReader(fixture.bundle), fixture.verifyOptions())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verification.Manifest.ReleaseSequence != fixture.manifest.ReleaseSequence {
		t.Fatalf("release sequence = %d, want %d", verification.Manifest.ReleaseSequence, fixture.manifest.ReleaseSequence)
	}
	wantPayloadBytes := int64(len("unirouter-binary") + len("BEGIN; SELECT 1; COMMIT;") + len(`{"builder":"ci"}`))
	if verification.PayloadBytes != wantPayloadBytes {
		t.Fatalf("payload bytes = %d, want %d", verification.PayloadBytes, wantPayloadBytes)
	}
	digest := sha256.Sum256(fixture.bundle)
	if verification.BundleSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("bundle digest = %q, want %q", verification.BundleSHA256, hex.EncodeToString(digest[:]))
	}

	var second bytes.Buffer
	if err := Pack(&second, fixture.manifest, fixture.privateKey, fixture.sources); err != nil {
		t.Fatalf("second Pack() error = %v", err)
	}
	if !bytes.Equal(fixture.bundle, second.Bytes()) {
		t.Fatal("Pack() is not deterministic for identical inputs")
	}
}

func TestVerifyRejectsTamperingAndUntrustedMetadata(t *testing.T) {
	for _, noExpiry := range []bool{false, true} {
		t.Run(fmt.Sprintf("noExpiry=%v", noExpiry), func(t *testing.T) { VerifyRejectsTamperingAndUntrustedMetadata(t, noExpiry) })
	}
}

func VerifyRejectsTamperingAndUntrustedMetadata(t *testing.T, noExpiry bool) {
	fixture := newBundleFixture(t)
	if noExpiry {
		fixture.manifest.SchemaVersion, fixture.manifest.ExpiresAt = NonExpiringManifestSchemaVersion, NeverExpires
		var packed bytes.Buffer
		if err := Pack(&packed, fixture.manifest, fixture.privateKey, fixture.sources); err != nil {
			t.Fatal(err)
		}
		fixture.bundle = packed.Bytes()
	}

	_, wrongPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongKey := wrongPrivate.Public().(ed25519.PublicKey)
	tests := []struct {
		name    string
		bundle  []byte
		options VerifyOptions
		want    string
	}{
		{name: "wrong signing key", bundle: fixture.bundle, options: withKey(fixture.verifyOptions(), wrongKey), want: "signature verification failed"},
		{name: "wrong platform", bundle: fixture.bundle, options: withOS(fixture.verifyOptions(), "darwin"), want: "current platform"},
		{name: "expired metadata", bundle: fixture.bundle, options: withNow(fixture.verifyOptions(), fixture.now.Add(49*time.Hour)), want: "metadata has expired"},
		{name: "rollback sequence", bundle: fixture.bundle, options: withSequence(fixture.verifyOptions(), 7), want: "outside signed window"},
		{name: "schema too old", bundle: fixture.bundle, options: withSchema(fixture.verifyOptions(), "000033"), want: "incompatible"},
		{name: "payload size bound", bundle: fixture.bundle, options: withMaxPayloadBytes(fixture.verifyOptions(), 1), want: "size bound"},
		{name: "tampered product", bundle: rewriteBundle(t, fixture.bundle, func(entries []testEntry) []testEntry {
			var manifest Manifest
			if err := json.Unmarshal(entries[0].data, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest.Product = "different-product"
			entries[0].data, err = json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			return entries
		}), options: fixture.verifyOptions(), want: "signature verification failed"},
		{name: "tampered expiry is not trusted before signature", bundle: rewriteBundle(t, fixture.bundle, func(entries []testEntry) []testEntry {
			var manifest Manifest
			if err := json.Unmarshal(entries[0].data, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest.ExpiresAt = fixture.now.Add(-time.Hour).Format(time.RFC3339)
			entries[0].data, err = json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			return entries
		}), options: fixture.verifyOptions(), want: "signature verification failed"},
		{name: "payload changed", bundle: rewriteBundle(t, fixture.bundle, func(entries []testEntry) []testEntry {
			entries[2].data[0] ^= 0xff
			return entries
		}), options: fixture.verifyOptions(), want: "hash verification failed"},
		{name: "manifest changed", bundle: rewriteBundle(t, fixture.bundle, func(entries []testEntry) []testEntry {
			var manifest Manifest
			if err := json.Unmarshal(entries[0].data, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest.Version = "1.0.1-tampered"
			entries[0].data, err = json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			return entries
		}), options: fixture.verifyOptions(), want: "signature verification failed"},
	}

	for _, test := range tests {
		if noExpiry && test.name == "expired metadata" {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			_, err := Verify(bytes.NewReader(test.bundle), test.options)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Verify() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestVerifyRejectsUnsafeArchiveShape(t *testing.T) {
	for _, noExpiry := range []bool{false, true} {
		t.Run(fmt.Sprintf("noExpiry=%v", noExpiry), func(t *testing.T) { VerifyRejectsUnsafeArchiveShape(t, noExpiry) })
	}
}

func VerifyRejectsUnsafeArchiveShape(t *testing.T, noExpiry bool) {
	fixture := newBundleFixture(t)
	if noExpiry {
		fixture.manifest.SchemaVersion, fixture.manifest.ExpiresAt = NonExpiringManifestSchemaVersion, NeverExpires
		var packed bytes.Buffer
		if err := Pack(&packed, fixture.manifest, fixture.privateKey, fixture.sources); err != nil {
			t.Fatal(err)
		}
		fixture.bundle = packed.Bytes()
	}
	tests := []struct {
		name   string
		mutate func([]testEntry) []testEntry
		want   string
	}{
		{name: "path traversal", mutate: func(entries []testEntry) []testEntry {
			entries[2].header.Name = "../../outside"
			return entries
		}, want: "unsafe upgrade entry path"},
		{name: "symlink", mutate: func(entries []testEntry) []testEntry {
			entries[2].header.Typeflag = tar.TypeSymlink
			entries[2].header.Linkname = "/tmp/outside"
			entries[2].data = nil
			return entries
		}, want: "is not a regular file"},
		{name: "duplicate payload", mutate: func(entries []testEntry) []testEntry {
			duplicate := entries[2]
			duplicate.data = append([]byte(nil), duplicate.data...)
			return append(entries, duplicate)
		}, want: "duplicate upgrade entry"},
		{name: "undeclared payload", mutate: func(entries []testEntry) []testEntry {
			entries[2].header.Name = "payload/extra"
			return entries
		}, want: "undeclared upgrade payload"},
		{name: "missing payload", mutate: func(entries []testEntry) []testEntry {
			return append(entries[:2], entries[3:]...)
		}, want: "is missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bundle := rewriteBundle(t, fixture.bundle, test.mutate)
			_, err := Verify(bytes.NewReader(bundle), fixture.verifyOptions())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Verify() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestVerifyRejectsTrailingGzipStream(t *testing.T) {
	fixture := newBundleFixture(t)
	trailing := append(append([]byte(nil), fixture.bundle...), fixture.bundle...)
	_, err := Verify(bytes.NewReader(trailing), fixture.verifyOptions())
	if err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("Verify() error = %v, want trailing data rejection", err)
	}
}

func TestValidateManifestRejectsMutableOrIncompleteReleaseIdentity(t *testing.T) {
	fixture := newBundleFixture(t)
	manifest := fixture.manifest
	manifest.Commit = "abc1234"
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected abbreviated commit rejection")
	}
	manifest = fixture.manifest
	manifest.Version = "local"
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected local version rejection")
	}
	manifest = fixture.manifest
	manifest.Version = " 1.0.0"
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected unnormalized version rejection")
	}
	manifest = fixture.manifest
	manifest.TargetSchema = "../../schema"
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected invalid schema identity rejection")
	}
	manifest = fixture.manifest
	manifest.ProvenanceSHA256 = strings.Repeat("c", 64)
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected unbound provenance digest rejection")
	}
}

func newBundleFixture(t *testing.T) bundleFixture {
	t.Helper()
	dir := t.TempDir()
	binaryPath := dir + "/uniroute"
	migrationPath := dir + "/000037.up.sql"
	provenancePath := dir + "/build-provenance.json"
	if err := os.WriteFile(binaryPath, []byte("unirouter-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(migrationPath, []byte("BEGIN; SELECT 1; COMMIT;"), 0o600); err != nil {
		t.Fatal(err)
	}
	provenanceBytes := []byte(`{"builder":"ci"}`)
	if err := os.WriteFile(provenancePath, provenanceBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	provenanceDigest := sha256.Sum256(provenanceBytes)
	sources := []Source{
		{BundlePath: "payload/uniroute", FilePath: binaryPath, MediaType: "application/octet-stream"},
		{BundlePath: "payload/migrations/000037.up.sql", FilePath: migrationPath, MediaType: "application/sql"},
		{BundlePath: ProvenanceArtifactPath, FilePath: provenancePath, MediaType: "application/json"},
	}
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	manifest, err := CreateManifest(ManifestOptions{
		KeyID:              "upgrade-2026-01",
		ReleaseSequence:    7,
		Version:            "1.0.0",
		Commit:             strings.Repeat("a", 40),
		CreatedAt:          now.Add(-time.Hour),
		ExpiresAt:          now.Add(48 * time.Hour),
		TargetOS:           "linux",
		TargetArch:         "amd64",
		MinCurrentSequence: 5,
		MaxCurrentSequence: 6,
		MinCurrentSchema:   "000034",
		TargetSchema:       "000037",
		ProvenanceSHA256:   hex.EncodeToString(provenanceDigest[:]),
	}, sources)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var packed bytes.Buffer
	if err := Pack(&packed, manifest, privateKey, sources); err != nil {
		t.Fatal(err)
	}
	return bundleFixture{bundle: packed.Bytes(), manifest: manifest, sources: sources, publicKey: publicKey, privateKey: privateKey, now: now}
}

func (fixture bundleFixture) verifyOptions() VerifyOptions {
	return VerifyOptions{
		PublicKeys:      map[string]ed25519.PublicKey{fixture.manifest.KeyID: fixture.publicKey},
		Now:             fixture.now,
		TargetOS:        "linux",
		TargetArch:      "amd64",
		CurrentSequence: 6,
		CurrentSchema:   "000036",
	}
}

func withKey(options VerifyOptions, key ed25519.PublicKey) VerifyOptions {
	options.PublicKeys = map[string]ed25519.PublicKey{"upgrade-2026-01": key}
	return options
}

func withOS(options VerifyOptions, value string) VerifyOptions {
	options.TargetOS = value
	return options
}

func withNow(options VerifyOptions, value time.Time) VerifyOptions {
	options.Now = value
	return options
}

func withSequence(options VerifyOptions, value int64) VerifyOptions {
	options.CurrentSequence = value
	return options
}

func withSchema(options VerifyOptions, value string) VerifyOptions {
	options.CurrentSchema = value
	return options
}

func withMaxPayloadBytes(options VerifyOptions, value int64) VerifyOptions {
	options.MaxPayloadBytes = value
	return options
}

type testEntry struct {
	header tar.Header
	data   []byte
}

func rewriteBundle(t *testing.T, original []byte, mutate func([]testEntry) []testEntry) []byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var entries []testEntry
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		copyHeader := *header
		entries = append(entries, testEntry{header: copyHeader, data: data})
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	entries = mutate(entries)
	var output bytes.Buffer
	newGzip := gzip.NewWriter(&output)
	newGzip.ModTime = time.Unix(0, 0)
	tw := tar.NewWriter(newGzip)
	for _, entry := range entries {
		entry.header.Size = int64(len(entry.data))
		if err := tw.WriteHeader(&entry.header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := newGzip.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestExplicitLifetimeModes(t *testing.T) {
	f := newBundleFixture(t)
	for _, tc := range []struct {
		schema, expiry string
		valid          bool
	}{
		{ManifestSchemaVersion, f.manifest.ExpiresAt, true},
		{NonExpiringManifestSchemaVersion, NeverExpires, true},
		{ManifestSchemaVersion, NeverExpires, false},
		{NonExpiringManifestSchemaVersion, f.manifest.ExpiresAt, false},
		{NonExpiringManifestSchemaVersion, "", false},
		{NonExpiringManifestSchemaVersion, "Never", false},
		{NonExpiringManifestSchemaVersion, " never", false},
		{NonExpiringManifestSchemaVersion, "never ", false},
		{"unirouter.upgrade-manifest.v3", NeverExpires, false},
	} {
		t.Run(tc.schema+"/"+tc.expiry, func(t *testing.T) {
			m := f.manifest
			m.SchemaVersion, m.ExpiresAt = tc.schema, tc.expiry
			if err := ValidateManifest(m); (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
	options := ManifestOptions{KeyID: f.manifest.KeyID, ReleaseSequence: 7, Version: "1.0.0", Commit: f.manifest.Commit,
		CreatedAt: f.now.Add(-time.Hour), TargetOS: "linux", TargetArch: "amd64", MinCurrentSequence: 5,
		MaxCurrentSequence: 6, MinCurrentSchema: "000034", TargetSchema: "000037", ProvenanceSHA256: f.manifest.ProvenanceSHA256}
	if _, err := CreateManifest(options, f.sources); err == nil {
		t.Fatal("implicit lifetime accepted")
	}
	options.NoExpiry = true
	m, err := CreateManifest(options, f.sources)
	if err != nil || m.SchemaVersion != NonExpiringManifestSchemaVersion || m.ExpiresAt != NeverExpires {
		t.Fatalf("explicit no expiry: %#v %v", m, err)
	}
	options.ExpiresAt = f.now.Add(time.Hour)
	if _, err := CreateManifest(options, f.sources); err == nil {
		t.Fatal("conflicting lifetime accepted")
	}
	options.NoExpiry = false
	m, err = CreateManifest(options, f.sources)
	if err != nil || m.SchemaVersion != ManifestSchemaVersion {
		t.Fatalf("finite default changed: %v", err)
	}
}

func TestNonExpiringVerificationPreservesOtherGuards(t *testing.T) {
	f := newBundleFixture(t)
	f.manifest.SchemaVersion, f.manifest.ExpiresAt = NonExpiringManifestSchemaVersion, NeverExpires
	var b bytes.Buffer
	if err := Pack(&b, f.manifest, f.privateKey, f.sources); err != nil {
		t.Fatal(err)
	}
	v, err := Verify(bytes.NewReader(b.Bytes()), withNow(f.verifyOptions(), time.Date(2126, 7, 14, 0, 0, 0, 0, time.UTC)))
	if err != nil || v.Manifest.ExpiresAt != NeverExpires {
		t.Fatalf("never expired: %v", err)
	}
	for _, tc := range []struct {
		name string
		opt  VerifyOptions
		want string
	}{
		{"future", withNow(f.verifyOptions(), f.now.Add(-66*time.Minute)), "creation time"},
		{"five minute tolerance", withNow(f.verifyOptions(), f.now.Add(-65*time.Minute)), ""},
		{"sequence", withSequence(f.verifyOptions(), 7), "outside signed window"},
		{"schema", withSchema(f.verifyOptions(), "000033"), "incompatible"},
		{"platform", withOS(f.verifyOptions(), "darwin"), "current platform"},
		{"size", withMaxPayloadBytes(f.verifyOptions(), 1), "size bound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Verify(bytes.NewReader(b.Bytes()), tc.opt)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v, want %s", err, tc.want)
			}
		})
	}
	changed := rewriteBundle(t, b.Bytes(), func(entries []testEntry) []testEntry {
		entries[0].data = bytes.Replace(entries[0].data, []byte(`"expires_at":"never"`), []byte(`"expires_at":"Never"`), 1)
		return entries
	})
	if _, err := Verify(bytes.NewReader(changed), f.verifyOptions()); err == nil || !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("policy trusted before signature: %v", err)
	}
	for _, raw := range [][]byte{
		bytes.Replace(mustManifestJSON(t, f.manifest), []byte(`,"expires_at":"never"`), nil, 1),
		bytes.Replace(mustManifestJSON(t, f.manifest), []byte(`"expires_at":"never"`), []byte(`"expires_at":null`), 1),
	} {
		if _, err := decodeManifest(raw); err == nil {
			t.Fatal("missing/null policy accepted")
		}
	}
}

func mustManifestJSON(t *testing.T, m Manifest) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFiniteExpiryBoundaryUnchanged(t *testing.T) {
	f := newBundleFixture(t)
	expiry, _ := time.Parse(time.RFC3339, f.manifest.ExpiresAt)
	if _, err := Verify(bytes.NewReader(f.bundle), withNow(f.verifyOptions(), expiry.Add(-time.Nanosecond))); err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{expiry, expiry.Add(time.Nanosecond)} {
		if _, err := Verify(bytes.NewReader(f.bundle), withNow(f.verifyOptions(), at)); err == nil || !strings.Contains(err.Error(), "has expired") {
			t.Fatalf("finite expiry boundary: %v", err)
		}
	}
}
