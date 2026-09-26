package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/config"
)

func TestContentStoreMainNeedsNoRuntimeConfiguration(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	parent, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(root, 0700))
	require.NoError(t, os.Chmod(parent, 0700))
	token := filepath.Join(parent, "binding")
	base := []string{"--root", root, "--identity", "test-cli", "--token-file", token}
	for _, action := range []string{"init", "check"} {
		wire, err := json.Marshal(append([]string{action}, base...))
		require.NoError(t, err)
		command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestProcessContentStorageEntry$")
		// Deliberately exclude DB/JWT/encryption/license/runtime configuration.
		command.Env = []string{"CONTENT_STORE_TEST_ARGS=" + string(wire), "PATH=" + os.Getenv("PATH")}
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err = command.Run()
		require.NoError(t, err, "local command stdout: %s; stderr: %s", stdout.String(), stderr.String())
		var report map[string]string
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &report), "local command stdout: %s; stderr: %s", stdout.String(), stderr.String())
		require.Equal(t, "ok", report["status"])
		secret, err := os.ReadFile(token)
		require.NoError(t, err)
		require.NotContains(t, stdout.String(), strings.TrimSpace(string(secret)))
		require.NotContains(t, stderr.String(), strings.TrimSpace(string(secret)))
	}
	store, err := openContentRuntimeStore(&config.Config{SourceContentStorageRoot: root, SourceContentStorageIdentity: "test-cli", SourceContentStorageTokenFile: token})
	require.NoError(t, err)
	require.NoError(t, store.Close())
	wire, err := json.Marshal([]string{"unknown-subcommand-secret"})
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestProcessContentStorageEntry$")
	command.Env = []string{"CONTENT_STORE_TEST_ARGS=" + string(wire)}
	output, err := command.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), contentStoreUsage)
	require.NotContains(t, string(output), "unknown-subcommand-secret")
	require.NotContains(t, string(output), "Failed to load config")
}

func TestProcessContentStorageEntry(t *testing.T) {
	raw := os.Getenv("CONTENT_STORE_TEST_ARGS")
	if raw == "" {
		return
	}
	var args []string
	if json.Unmarshal([]byte(raw), &args) != nil {
		os.Exit(2)
	}
	os.Args = append([]string{"uniroute", "content-store"}, args...)
	main()
	os.Exit(0)
}

func TestContentStoreCLIInvalidArgumentsNeverEchoValues(t *testing.T) {
	for _, args := range [][]string{nil, {"secret-subcommand"}, {"init", "--token=secret-value"}, {"check", "secret-positional"}, {"export-token", "--root", "only-one-argument"}} {
		var output bytes.Buffer
		err := runContentStoreCLI(context.Background(), args, &output)
		require.Error(t, err)
		require.Equal(t, contentStoreUsage, err.Error())
		require.Empty(t, output.String())
	}
}
