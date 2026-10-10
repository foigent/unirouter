package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/signing"
)

// Reexecute this test binary as the real verifier. No Docker, service, model,
// production key, downloaded tool or published release participates in this test.
func TestCommunityInstallerVerifierProcess(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--" {
			if err := run(os.Args[i+1:], os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
}

func TestCommunityInstallerSignedRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux installer verification uses Unix file semantics")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3.10+ is required for installer interoperability tests")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(cwd, "../.."))
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateText, _ := signing.EncodePrivateKey(private)
	publicText, _ := signing.EncodePublicKey(public)
	privatePath := write("test-only-private.key", []byte(privateText))
	ring, _ := json.Marshal(map[string]string{"installer-test": publicText})
	ringPath := write("public-keys.json", ring)
	images := map[string]string{}
	for i, name := range []string{"backend", "frontend", "postgres", "redis", "prometheus", "grafana"} {
		images[name] = fmt.Sprintf("registry.example.invalid/%s@sha256:%s", name, strings.Repeat(fmt.Sprint(i+1), 64))
	}
	descriptor, _ := json.Marshal(map[string]any{"schema_version": "unirouter.install.v1", "images": images})
	payload := map[string][]byte{
		"payload/install.json":                                               descriptor,
		"payload/deploy/config/prometheus.yml":                               []byte("global: {}\n"),
		"payload/deploy/config/grafana/provisioning/datasources/default.yml": []byte("apiVersion: 1\n"),
		"payload/deploy/config/grafana/dashboards/default.json":              []byte("{}\n"),
	}
	for _, name := range []string{"install.py", "docker-compose.yml", ".env.example", "preflight.sh"} {
		data, err := os.ReadFile(filepath.Join(root, "deploy/customer", name))
		if err != nil {
			t.Fatal(err)
		}
		payload["payload/deploy/"+name] = data
	}
	const schema = "000126_commercial_license_retirement"
	packagePath := filepath.Join(dir, "release.tar.gz")
	now := time.Now().UTC()
	args := []string{"pack", "--key-id", "installer-test", "--private-key", privatePath,
		"--release-sequence", "1", "--version", "1.10.0", "--commit", strings.Repeat("a", 40),
		"--created-at", now.Add(-time.Minute).Format(time.RFC3339), "--expires-at", now.Add(time.Hour).Format(time.RFC3339),
		"--target-os", "linux", "--target-arch", runtime.GOARCH,
		"--min-current-sequence", "0", "--max-current-sequence", "0",
		"--min-current-schema", schema, "--target-schema", schema, "--out", packagePath}
	names := make([]string, 0, len(payload))
	for name := range payload {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		args = append(args, "--artifact", name+"="+write(name, payload[name]))
	}
	if err := run(args, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	// Python's executable script avoids shell interpolation of the test path.
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helperArgs, _ := json.Marshal([]string{binary, "-test.run=^TestCommunityInstallerVerifierProcess$", "--"})
	helper := write("verifier", []byte("#!"+python+"\nimport subprocess, sys\nsys.exit(subprocess.call("+string(helperArgs)+" + sys.argv[1:]))\n"))
	if err := os.Chmod(helper, 0o700); err != nil {
		t.Fatal(err)
	}
	const script = `import importlib.util, pathlib, sys
root, directory, verifier, keys, arch = sys.argv[1:]
spec = importlib.util.spec_from_file_location("installer", pathlib.Path(root) / "deploy/customer/install.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
directory = pathlib.Path(directory)
module.Release(directory, directory / "release.tar.gz", pathlib.Path(verifier), pathlib.Path(keys), arch)
print("signed_install_release=PASS")
`
	check := func(wantOK bool) {
		t.Helper()
		cmd := exec.Command(python, "-B", "-c", script, root, dir, helper, ringPath, runtime.GOARCH)
		output, err := cmd.CombinedOutput()
		if (err == nil) != wantOK || (wantOK && !bytes.Contains(output, []byte("signed_install_release=PASS"))) {
			t.Fatalf("verified release success=%v, want %v: %v\n%s", err == nil, wantOK, err, output)
		}
	}
	t.Run("valid signed package and prepared payload", func(t *testing.T) { check(true) })
	t.Run("modified prepared payload", func(t *testing.T) {
		name := "payload/install.json"
		write(name, []byte("{}"))
		check(false)
		write(name, payload[name])
	})
	t.Run("untrusted signing key", func(t *testing.T) {
		other, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		text, _ := signing.EncodePublicKey(other)
		wrongRing, _ := json.Marshal(map[string]string{"installer-test": text})
		write("public-keys.json", wrongRing)
		check(false)
		write("public-keys.json", ring)
	})
	t.Run("tampered package", func(t *testing.T) {
		data, err := os.ReadFile(packagePath)
		if err != nil {
			t.Fatal(err)
		}
		data[len(data)/2] ^= 0xff
		write("release.tar.gz", data)
		check(false)
	})
}
