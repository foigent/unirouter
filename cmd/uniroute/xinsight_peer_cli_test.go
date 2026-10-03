package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestXInsightPeerPrepareCheckPreservesExactArtifact(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	file := filepath.Join(dir, "peer.env")
	args := []string{"prepare", "--xi-origin", "https://xinsight.internal.example", "--xi-deployment-id", "xid_ZZZZZZZZZZZZZZZZZZZZZZ",
		"--ur-origin", "https://unirouter.internal.example", "--receiver-ip", "203.0.113.7/32", "--out", file}
	var out bytes.Buffer
	require.NoError(t, runXInsightPeerCLI(args, &out))
	wire, err := os.ReadFile(file)
	require.NoError(t, err)
	before, err := os.Stat(file)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), before.Mode().Perm())
	require.NoError(t, runXInsightPeerCLI([]string{"check", "--file", file}, &out))
	after, err := os.Stat(file)
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime())
	current, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, wire, current)
	require.Error(t, runXInsightPeerCLI(args, &out))
	require.NoFileExists(t, filepath.Join(dir, "state.json"))
	require.Contains(t, out.String(), `"database_writes":0`)
	link := filepath.Join(dir, "linked.env")
	require.NoError(t, os.Symlink(file, link))
	require.Error(t, runXInsightPeerCLI([]string{"check", "--file", link}, &out))
	parentLink := filepath.Join(dir, "linked-parent")
	require.NoError(t, os.Symlink(dir, parentLink))
	copy := append([]string(nil), args...)
	copy[len(copy)-1] = filepath.Join(parentLink, "unexpected.env")
	require.Error(t, runXInsightPeerCLI(copy, &out))
	require.NoFileExists(t, filepath.Join(dir, "unexpected.env"))
}
