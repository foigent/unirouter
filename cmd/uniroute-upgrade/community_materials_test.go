package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/signing"
	"github.com/uniroute/unirouter/internal/upgradebundle"
)

// Actual full-v2 packing consumes the Python selection's ordinary sources.
// These material bodies and signing seed are exclusively synthetic fixture data.
func TestCommunityMaterialReuseKeepsFullV2PackageAndStreamingIntegrity(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(filepath.Join(root, "../.."))
	dir := t.TempDir()
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	current, previous := filepath.Join(dir, "current"), filepath.Join(dir, "previous")
	setup := `import importlib.util,json,hashlib,sys
from pathlib import Path
spec=importlib.util.spec_from_file_location("m",sys.argv[1]); m=importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
current,previous=map(Path,sys.argv[2:]); rows=[]
for name in m.OWNER:
 raw=("synthetic full payload "+name).encode()
 for root in (current,previous):
  path=root/name; path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(raw)
 rows.append({"path":name,"size":len(raw),"sha256":hashlib.sha256(raw).hexdigest()})
(current.parent/"inputs.json").write_text(json.dumps({"schema_version":m.SCHEMA,"artifacts":rows}))
(previous.parent/"previous-manifest.json").write_text(json.dumps({"artifacts":rows}))
`
	command := exec.Command("python3", "-I", "-B", "-c", setup, filepath.Join(root, ".github/tests/community-release-materials.py"), current, previous)
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("offline material setup: %v %s", err, raw)
	}
	type selectedSource struct {
		Path  string `json:"path"`
		File  string `json:"file_path"`
		SHA   string `json:"sha256"`
		Size  int64  `json:"size"`
		Media string `json:"media_type"`
	}
	selectInputs := func(t *testing.T, warm bool) ([]upgradebundle.Source, map[string]selectedSource, string) {
		t.Helper()
		args := []string{"-I", "-B", filepath.Join(root, ".github/tests/community-release-materials.py"), "--inventory", filepath.Join(dir, "inputs.json"), "--current", current}
		if warm {
			args = append(args, "--previous", previous, "--previous-manifest", filepath.Join(dir, "previous-manifest.json"))
		}
		inventory := filepath.Join(dir, "selected-"+strings.ReplaceAll(t.Name(), "/", "-")+fmt.Sprint(warm)+".json")
		args = append(args, "--artifact-inventory-out", inventory)
		raw, err := exec.Command("python3", args...).Output()
		if err != nil {
			t.Fatal(err)
		}
		var selection struct {
			Sources []selectedSource `json:"sources"`
		}
		if err := json.Unmarshal(raw, &selection); err != nil {
			t.Fatal(err)
		}
		var sources []upgradebundle.Source
		expected := map[string]selectedSource{}
		for _, row := range selection.Sources {
			sources = append(sources, upgradebundle.Source{BundlePath: row.Path, FilePath: row.File, MediaType: row.Media})
			expected[row.Path] = row
		}
		return sources, expected, inventory
	}
	seed := sha256.Sum256([]byte("synthetic test-only material reuse signing seed"))
	private := ed25519.NewKeyFromSeed(seed[:])
	public := private.Public().(ed25519.PublicKey)
	privateText, err := signing.EncodePrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(dir, "synthetic-test-only.key")
	if err := os.WriteFile(privatePath, []byte(privateText), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	for index, version := range []string{"1.15.0", "1.15.1"} {
		t.Run(version, func(t *testing.T) {
			// Every version supplies fresh release-owned bytes, including provenance.
			// Large stable source/system archives remain the original previous inputs.
			refresh := `import importlib.util,json,hashlib,sys
from pathlib import Path
spec=importlib.util.spec_from_file_location("m",sys.argv[1]);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
current=Path(sys.argv[2]);request=Path(sys.argv[3]);version=sys.argv[4];value=json.loads(request.read_bytes())
for row in value["artifacts"]:
 if row["path"] in m.GROUPS["release"]:
  raw=("synthetic current release "+version+" "+row["path"]).encode();(current/row["path"]).write_bytes(raw)
  row.update(size=len(raw),sha256=hashlib.sha256(raw).hexdigest())
request.write_text(json.dumps(value))
`
			if raw, err := exec.Command("python3", "-I", "-B", "-c", refresh, filepath.Join(root, ".github/tests/community-release-materials.py"), current, filepath.Join(dir, "inputs.json"), version).CombinedOutput(); err != nil {
				t.Fatalf("fresh current release inputs: %v %s", err, raw)
			}
			options := upgradebundle.ManifestOptions{KeyID: "synthetic-materials", ReleaseSequence: int64(17 + index), Version: version, Commit: strings.Repeat(fmt.Sprint(index+1), 40), CreatedAt: now, NoExpiry: true, TargetOS: "linux", TargetArch: "amd64", MinCurrentSequence: 0, MaxCurrentSequence: int64(16 + index), MinCurrentSchema: "000127_xinsight_communication_policy", TargetSchema: "000128_upstream_release_observation"}
			var cold []byte
			for _, warm := range []bool{false, true} {
				sources, expected, inventory := selectInputs(t, warm)
				for _, row := range sources {
					if row.BundlePath == upgradebundle.ProvenanceArtifactPath {
						options.ProvenanceSHA256 = expected[row.BundlePath].SHA
					}
				}
				manifest, err := upgradebundle.CreateManifest(options, sources)
				if err != nil {
					t.Fatal(err)
				}
				// This is the same pre-private-key owner check as actual CLI.
				if err := checkExpectedArtifacts(inventory, manifest.Artifacts); err != nil {
					t.Fatal(err)
				}
				var output bytes.Buffer
				if err := upgradebundle.Pack(&output, manifest, private, sources); err != nil {
					t.Fatal(err)
				}
				// Exercise the actual production CLI flag parser and pack owner too;
				// selection data becomes ordinary --artifact arguments, never shell text.
				cliPath := filepath.Join(dir, fmt.Sprintf("synthetic-%s-%t.tar.gz", version, warm))
				args := []string{"--key-id", options.KeyID, "--private-key", privatePath,
					"--expected-artifacts", inventory, "--release-sequence", fmt.Sprint(options.ReleaseSequence),
					"--version", version, "--commit", options.Commit, "--created-at", now.Format(time.RFC3339),
					"--no-expiry", "--target-os", "linux", "--target-arch", "amd64",
					"--min-current-sequence", "0", "--max-current-sequence", fmt.Sprint(options.MaxCurrentSequence),
					"--min-current-schema", options.MinCurrentSchema, "--target-schema", options.TargetSchema,
					"--provenance-sha256", options.ProvenanceSHA256, "--out", cliPath}
				for _, source := range sources {
					args = append(args, "--artifact", source.BundlePath+"="+source.FilePath)
				}
				if err := runPack(args, &bytes.Buffer{}); err != nil {
					t.Fatal(err)
				}
				cliBytes, err := os.ReadFile(cliPath)
				if err != nil || !bytes.Equal(output.Bytes(), cliBytes) {
					t.Fatal("actual pack CLI changed selected full package", err)
				}
				if !warm {
					cold = append([]byte(nil), output.Bytes()...)
				} else if !bytes.Equal(cold, output.Bytes()) {
					t.Fatal("warm reuse changed the full signed v2 package")
				}
				verified, err := upgradebundle.Verify(bytes.NewReader(output.Bytes()), upgradebundle.VerifyOptions{PublicKeys: map[string]ed25519.PublicKey{"synthetic-materials": public}, Now: now, TargetOS: "linux", TargetArch: "amd64", CurrentSequence: 0, CurrentSchema: options.TargetSchema})
				if err != nil {
					t.Fatal(err)
				}
				if verified.Manifest.SchemaVersion != "unirouter.upgrade-manifest.v2" {
					t.Fatal("full signed v2 contract changed")
				}
				if warm && index == 1 {
					var path string
					for _, row := range sources {
						if strings.Contains(row.BundlePath, "gcc-") {
							path = row.FilePath
						}
					}
					if path == "" {
						t.Fatal("corresponding source not included")
					}
					old, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, append(old, 'x'), 0600); err != nil {
						t.Fatal(err)
					}
					changed, err := upgradebundle.CreateManifest(options, sources)
					if err != nil {
						t.Fatal(err)
					}
					if err := checkExpectedArtifacts(inventory, changed.Artifacts); err == nil {
						t.Fatal("new hash silently replaced the selected inventory before signing")
					}
					var refused bytes.Buffer
					if err := upgradebundle.Pack(&refused, manifest, private, sources); err == nil {
						t.Fatal("native stream hash accepted changed reused bytes")
					}
					sum := sha256.Sum256(old)
					if hex.EncodeToString(sum[:]) != expected["payload/licenses/sources/gcc-15.2.0.tar.xz"].SHA {
						t.Fatal("test did not bind the actual selected archive")
					}
				}
			}
		})
	}
}
