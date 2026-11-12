package upgradebundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

const deltaTestChange = "new signed artifact bytes\n"

func testDeltaPair(t *testing.T) (bundleFixture, bundleFixture) {
	t.Helper()
	basis := newBundleFixture(t)
	target := basis
	target.manifest.ReleaseSequence++
	target.manifest.MaxCurrentSequence = basis.manifest.ReleaseSequence
	target.manifest.Version = "1.99.2"
	target.manifest.Commit = "2222222222222222222222222222222222222222"
	target.manifest.Artifacts = append([]Artifact(nil), basis.manifest.Artifacts...)
	target.sources = append([]Source(nil), basis.sources...)
	changed := filepath.Join(t.TempDir(), "changed")
	if err := os.WriteFile(changed, []byte(deltaTestChange), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, artifact := range target.manifest.Artifacts {
		if artifact.Path != ProvenanceArtifactPath {
			target.manifest.Artifacts[i].Size = int64(len(deltaTestChange))
			target.manifest.Artifacts[i].SHA256 = deltaTestDigest([]byte(deltaTestChange))
			for j, source := range target.sources {
				if source.BundlePath == artifact.Path {
					target.sources[j].FilePath = changed
				}
			}
			break
		}
	}
	var out bytes.Buffer
	if err := Pack(&out, target.manifest, target.privateKey, target.sources); err != nil {
		t.Fatal(err)
	}
	target.bundle = out.Bytes()
	return basis, target
}

func deltaTestDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func TestDeltaRoundTripAndBinding(t *testing.T) {
	basis, target := testDeltaPair(t)
	var patch bytes.Buffer
	report, err := CreateDelta(&patch, bytes.NewReader(basis.bundle), bytes.NewReader(target.bundle), basis.verifyOptions())
	if err != nil {
		t.Fatal(err)
	}
	if report.ReusedBytes <= 0 || report.NewBytes != int64(len(deltaTestChange)) {
		t.Fatalf("unexpected reuse report %+v", report)
	}
	var output bytes.Buffer
	options := basis.verifyOptions()
	if _, err := MaterializeDelta(&output, bytes.NewReader(basis.bundle), bytes.NewReader(patch.Bytes()), options,
		deltaTestDigest(basis.bundle), deltaTestDigest(target.bundle)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), target.bundle) {
		t.Fatal("materialized bytes differ from the signed full archive")
	}

	tampered := rewriteBundle(t, patch.Bytes(), func(entries []testEntry) []testEntry {
		entries[2].data[0] ^= 1
		return entries
	})
	duplicate := rewriteBundle(t, patch.Bytes(), func(entries []testEntry) []testEntry {
		return append(entries, entries[2])
	})
	missing := rewriteBundle(t, patch.Bytes(), func(entries []testEntry) []testEntry {
		return entries[:2]
	})
	if _, err := CreateDelta(io.Discard, bytes.NewReader(basis.bundle), bytes.NewReader(basis.bundle), options); err == nil {
		t.Fatal("delta creation did not enforce the signed version advance")
	}

	for _, tc := range []struct {
		name                      string
		basis, patch              []byte
		basisSHA256, targetSHA256 string
	}{
		{"wrong basis", target.bundle, patch.Bytes(), deltaTestDigest(basis.bundle), deltaTestDigest(target.bundle)},
		{"tampered changed member", basis.bundle, tampered, deltaTestDigest(basis.bundle), deltaTestDigest(target.bundle)},
		{"duplicate changed member", basis.bundle, duplicate, deltaTestDigest(basis.bundle), deltaTestDigest(target.bundle)},
		{"missing changed member", basis.bundle, missing, deltaTestDigest(basis.bundle), deltaTestDigest(target.bundle)},
		{"wrong target binding", basis.bundle, patch.Bytes(), deltaTestDigest(basis.bundle), deltaTestDigest(basis.bundle)},
		{"truncated patch", basis.bundle, patch.Bytes()[:patch.Len()-7], deltaTestDigest(basis.bundle), deltaTestDigest(target.bundle)},
		{"trailing patch bytes", basis.bundle, append(bytes.Clone(patch.Bytes()), 'x'), deltaTestDigest(basis.bundle), deltaTestDigest(target.bundle)},
		{"corrupt basis", append(bytes.Clone(basis.bundle), 'x'), patch.Bytes(), deltaTestDigest(basis.bundle), deltaTestDigest(target.bundle)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			if _, err := MaterializeDelta(&output, bytes.NewReader(tc.basis), bytes.NewReader(tc.patch), options, tc.basisSHA256, tc.targetSHA256); err == nil {
				t.Fatal("unsafe delta was accepted")
			}
		})
	}
}
