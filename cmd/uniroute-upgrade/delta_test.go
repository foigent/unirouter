package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/signing"
)

func TestDeltaCLIRoundTripAndOutputProtection(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, content []byte) string {
		t.Helper()
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, content, 0o600))
		return path
	}
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		return data
	}
	identity := func(path string) deltaFileIdentity {
		data := read(path)
		return deltaFileIdentity{Size: int64(len(data)), SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	}

	public, private, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	privateText, err := signing.EncodePrivateKey(private)
	require.NoError(t, err)
	publicText, err := signing.EncodePublicKey(public)
	require.NoError(t, err)
	keysJSON, err := json.Marshal(map[string]string{"delta-test": publicText})
	require.NoError(t, err)
	privatePath, keys := write("test-only.key", []byte(privateText)), write("public-keys.json", keysJSON)

	// A stable, incompressible member makes the signed sidecar smaller than
	// either full package without relying on a production archive.
	stableBytes := make([]byte, 16*1024)
	rand.NewChaCha8([32]byte{1}).Read(stableBytes)
	stable := write("stable", stableBytes)
	schema := "000129_upstream_observation_restore_namespace"
	created := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	pack := func(sequence int, changed string) string {
		t.Helper()
		id := strconv.Itoa(sequence)
		path := filepath.Join(dir, "release-"+id+".tar.gz")
		args := []string{"pack", "--key-id", "delta-test", "--private-key", privatePath,
			"--release-sequence", id, "--version", "1.0." + id, "--commit", strings.Repeat(id, 40),
			"--created-at", created, "--no-expiry", "--target-os", "linux", "--target-arch", "amd64",
			"--min-current-sequence", "0", "--max-current-sequence", strconv.Itoa(sequence - 1),
			"--min-current-schema", schema, "--target-schema", schema,
			"--artifact", "payload/stable=" + stable, "--artifact", "payload/change=" + changed, "--out", path}
		require.NoError(t, run(args, &bytes.Buffer{}))
		return path
	}
	basis := pack(1, write("before", []byte("before")))
	target := pack(2, write("after", []byte("after")))
	sidecar := filepath.Join(dir, "delta.tar.gz")
	var stdout bytes.Buffer
	create := []string{"create-delta", "--basis", basis, "--target", target,
		"--public-keys", keys, "--current-schema", schema, "--out", sidecar}
	require.NoError(t, run(create, &stdout))

	var createdDelta struct {
		Basis, Delta, Target deltaFileIdentity
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &createdDelta))
	require.Equal(t, identity(basis), createdDelta.Basis)
	require.Equal(t, identity(sidecar), createdDelta.Delta)
	require.Equal(t, identity(target), createdDelta.Target)
	originalSidecar := read(sidecar)
	require.ErrorContains(t, run(create, &bytes.Buffer{}), "output path must be unused")
	require.Equal(t, originalSidecar, read(sidecar))

	materialize := func(out, basisSHA, targetSHA string) error {
		return run([]string{"materialize-delta", "--basis", basis, "--basis-sha256", basisSHA,
			"--delta", sidecar, "--delta-sha256", createdDelta.Delta.SHA256,
			"--target-sha256", targetSHA, "--target-size", strconv.FormatInt(createdDelta.Target.Size, 10),
			"--public-keys", keys, "--current-schema", schema, "--out", out}, &bytes.Buffer{})
	}
	out := filepath.Join(dir, "materialized.tar.gz")
	require.NoError(t, materialize(out, createdDelta.Basis.SHA256, createdDelta.Target.SHA256))
	require.Equal(t, read(target), read(out))
	require.ErrorContains(t, materialize(out, createdDelta.Basis.SHA256, createdDelta.Target.SHA256), "output path must be unused")
	require.Equal(t, read(target), read(out))

	rejected := filepath.Join(dir, "rejected.tar.gz")
	require.ErrorContains(t, materialize(rejected, createdDelta.Target.SHA256, createdDelta.Target.SHA256), "basis differs")
	require.NoFileExists(t, rejected)
	require.ErrorContains(t, materialize(rejected, createdDelta.Basis.SHA256, createdDelta.Basis.SHA256), "pinned target bytes")
	require.NoFileExists(t, rejected)
}
