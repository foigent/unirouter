package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/signing"
)

func TestRecoveryEnvelopeNativeProcess(t *testing.T) {
	raw := os.Getenv("RECOVERY_ENVELOPE_NATIVE_ARGS")
	if raw == "" {
		return
	}
	var args []string
	if json.Unmarshal([]byte(raw), &args) != nil {
		os.Exit(2)
	}
	if run(args, os.Stdout) != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestSignedRecoveryEnvelopeUsesNativeVerifierBeforeOldImports(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	privateText, err := signing.EncodePrivateKey(private)
	require.NoError(t, err)
	publicText, err := signing.EncodePublicKey(public)
	require.NoError(t, err)
	privatePath, ringPath := filepath.Join(dir, "synthetic-private.key"), filepath.Join(dir, "synthetic-public.json")
	require.NoError(t, os.WriteFile(privatePath, []byte(privateText+"\n"), 0600))
	ring, err := json.Marshal(map[string]string{"recovery-fixture": publicText})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(ringPath, ring, 0600))
	script := `import importlib.util,json,os,subprocess,sys
from pathlib import Path
root=Path(sys.argv[2])
spec=importlib.util.spec_from_file_location("recovery_fixture",root/"scripts/test-recovery-resume.py")
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
calls=[]
def native(args):
 env={"PATH":os.environ.get("PATH",""),"RECOVERY_ENVELOPE_NATIVE_ARGS":json.dumps([str(a) for a in args])}
 r=subprocess.run([sys.argv[1],"-test.run=^TestRecoveryEnvelopeNativeProcess$"],env=env,capture_output=True)
 calls.append(r.returncode)
 return r
report=m.native_envelope_cases(native,Path(sys.argv[3]),Path(sys.argv[4]),Path(sys.argv[5]))
report.update(native_calls=len(calls),native_refusals=sum(c!=0 for c in calls))
print(json.dumps(report))
`
	command := exec.CommandContext(t.Context(), "python3", "-I", "-B", "-c", script, os.Args[0], repo, dir, privatePath, ringPath)
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	require.NoError(t, command.Run(), "native recovery fixture failed: %s", stderr.String())
	var report struct {
		PositiveWindows          int `json:"positive_windows"`
		RefusedCases             int `json:"refused_cases"`
		OldDependenciesRefused   int `json:"old_dependencies_refused"`
		OrdinaryInstallerRefused int `json:"ordinary_installer_refused"`
		NativeCalls              int `json:"native_calls"`
		NativeRefusals           int `json:"native_refusals"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	require.Equal(t, 2, report.PositiveWindows)
	require.GreaterOrEqual(t, report.RefusedCases, 7)
	require.Equal(t, 3, report.OldDependenciesRefused)
	require.Equal(t, 2, report.OrdinaryInstallerRefused)
	require.Greater(t, report.NativeCalls, 5)
	require.Positive(t, report.NativeRefusals)
}
