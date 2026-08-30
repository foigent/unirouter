package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/config"
)

func TestCapabilityContractPublicationCLIRequiresExplicitSubcommand(t *testing.T) {
	handled, err := runCLIWithAdminPasswordInput(context.Background(),
		[]string{"capability-contract-publication"}, &config.Config{}, nonTerminalAdminPasswordInput(""))
	require.True(t, handled)
	require.ErrorContains(t, err, "install-baseline|plan|apply|guarded-down|post-commit-recovery-check")
}

func TestRuntimePublicationArtifactBindsExecutableAndConfiguredIdentity(t *testing.T) {
	cfg := &config.Config{
		RuntimeVersion: "1.2.21-test", RuntimeCommit: strings.Repeat("a", 40), RuntimeTree: strings.Repeat("b", 40),
	}
	artifact, err := runtimePublicationArtifact(cfg)
	require.NoError(t, err)
	require.Equal(t, cfg.RuntimeCommit, artifact.Commit)
	require.Equal(t, cfg.RuntimeTree, artifact.Tree)
	require.Equal(t, cfg.RuntimeVersion, artifact.Version)
	require.Equal(t, "source", artifact.Kind)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, artifact.PublisherExecutableDigest)
}

func TestWriteNewPublicationPlanFileRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	require.NoError(t, writeNewPublicationPlanFile(path, []byte("first")))
	require.ErrorContains(t, writeNewPublicationPlanFile(path, []byte("second")), "refusing to overwrite")
	wire, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "first", string(wire))
}
