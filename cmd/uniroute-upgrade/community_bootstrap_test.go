package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/signing"
	"github.com/uniroute/unirouter/internal/upgradebundle"
)

// Exercise the real binary, signature format and Python preparation parser.
// Transport is a local fixture, not proof of a public HTTPS download or install.
func TestCommunityBootstrapSignedPreparation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("community bootstrap uses Unix ownership and private paths")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3.10+ is required")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(cwd, "../.."))
	dir := t.TempDir()
	verifier := filepath.Join(dir, "uniroute-upgrade")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-trimpath", "-buildvcs=false", "-o", verifier, "./cmd/uniroute-upgrade")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real verifier: %v\n%s", err, output)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
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
	ring := func(name string, key ed25519.PublicKey) string {
		text, _ := signing.EncodePublicKey(key)
		raw, _ := json.Marshal(map[string]string{"bootstrap-test": text})
		return write(name, raw)
	}
	ring("public-keys.json", public)
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ring("wrong-keys.json", other)
	var sources []upgradebundle.Source
	for _, name := range []string{"install.py", "docker-compose.yml", ".env.example", "preflight.sh"} {
		sources = append(sources, upgradebundle.Source{BundlePath: "payload/deploy/" + name, FilePath: filepath.Join(root, "deploy/customer", name)})
	}
	images := map[string]string{}
	for _, name := range []string{"backend", "frontend", "postgres", "redis", "prometheus", "grafana"} {
		images[name] = "example.invalid/" + name + "@sha256:" + strings.Repeat("a", 64)
	}
	descriptor, _ := json.Marshal(map[string]any{"schema_version": "unirouter.install.v1", "images": images})
	for name, raw := range map[string][]byte{
		"payload/install.json":                                               descriptor,
		"payload/deploy/config/prometheus.yml":                               []byte("global: {}\n"),
		"payload/deploy/config/grafana/provisioning/datasources/default.yml": []byte("apiVersion: 1\n"),
		"payload/deploy/config/grafana/dashboards/default.json":              []byte("{}\n"),
	} {
		sources = append(sources, upgradebundle.Source{BundlePath: name, FilePath: write(name, raw)})
	}
	now := time.Now().UTC()
	for _, expired := range []bool{false, true} {
		name, created, expires := "release.tar.gz", now.Add(-time.Minute), now.Add(time.Hour)
		if expired {
			name, created, expires = "expired.tar.gz", now.Add(-2*time.Hour), now.Add(-time.Hour)
		}
		manifest, err := upgradebundle.CreateManifest(upgradebundle.ManifestOptions{
			KeyID: "bootstrap-test", Version: "1.11.0", Commit: strings.Repeat("a", 40), ReleaseSequence: 1,
			CreatedAt: created, ExpiresAt: expires, TargetOS: "linux", TargetArch: "amd64",
			MinCurrentSequence: 0, MaxCurrentSequence: 0, MinCurrentSchema: "000126_commercial_license_retirement",
			TargetSchema: "000126_commercial_license_retirement",
		}, sources)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		packErr := upgradebundle.Pack(file, manifest, private, sources)
		closeErr := file.Close()
		if packErr != nil || closeErr != nil {
			t.Fatalf("pack=%v close=%v", packErr, closeErr)
		}
	}
	const script = `import hashlib, importlib.util, json, pathlib, sys
from unittest import mock
root, fixture = map(lambda p:pathlib.Path(p).resolve(),sys.argv[1:])
def load(name,path):
    spec=importlib.util.spec_from_file_location(name,path)
    module=importlib.util.module_from_spec(spec); spec.loader.exec_module(module); return module
b=load('bootstrap',root/'deploy/community/bootstrap.py')
installer=load('installer',root/'deploy/customer/install.py')
for name, package, ring, key, want in [('valid','release.tar.gz','public-keys.json','bootstrap-test',True),
    ('expired','expired.tar.gz','public-keys.json','bootstrap-test',False),
    ('wrong-key','release.tar.gz','wrong-keys.json','bootstrap-test',False),
    ('wrong-id','release.tar.gz','public-keys.json','other-trusted-name',False)]:
    source={'verifier':fixture/'uniroute-upgrade','public_keys':fixture/ring,'package':fixture/package}
    value={'schema_version':'unirouter.community-bootstrap.v1','platform':{'os':'linux','arch':'amd64'},
      'release':{'version':'1.11.0','commit':'a'*40,'release_sequence':1,'key_id':key,'target_schema':b.SCHEMA},
      'assets':{k:{'url':'https://example.invalid/'+k,'size':p.stat().st_size,
                  'sha256':hashlib.sha256(p.read_bytes()).hexdigest()} for k,p in source.items()}}
    raw=json.dumps(value).encode(); anchor=b.Bootstrap(raw,hashlib.sha256(raw).hexdigest())
    out=fixture/('prepared-'+name)
    def fetch(asset,target): b.write_new(target,source[asset['url'].rsplit('/',1)[-1]].read_bytes())
    with mock.patch.object(b.platform,'system',return_value='Linux'),mock.patch.object(b.platform,'machine',return_value='x86_64'):
        try: b.prepare(anchor,out,fetch=fetch)
        except b.BootstrapError:
            assert not want and not (out/b.RECEIPT).exists() and not (out/b.INSTALLER).exists()
        else:
            assert want
            before={str(p.relative_to(out)):p.read_bytes() for p in out.rglob('*') if p.is_file()}
            b.check_prepared(anchor,out)
            installer.Release(out,out/'release.tar.gz',out/'uniroute-upgrade',out/'public-keys.json','amd64')
            assert before=={str(p.relative_to(out)):p.read_bytes() for p in out.rglob('*') if p.is_file()}
print('real_verifier_bootstrap=PASS; valid/expired/wrong-key/wrong-id; installer contract matched; no services')
`
	command := exec.Command(python, "-I", "-B", "-c", script, root, dir)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap interoperability: %v\n%s", err, output)
	} else {
		t.Logf("%s", output)
	}
}
