package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunRequiresExactCompiledPublicationAndOutputDirectory(t *testing.T) {
	require.Error(t, run(nil))
	require.Error(t, run([]string{"--publication", "unknown", "--migrations-dir", t.TempDir()}))
	require.Error(t, run([]string{"--publication", "unknown", "--migrations-dir", t.TempDir(), "extra"}))
}

func TestRunReviewPrintsExactSourceDelta(t *testing.T) {
	var output bytes.Buffer
	args := []string{"--publication", "000105_glm_toggle_function_tools_qualification", "--review"}
	require.NoError(t, runWithOutput(args, &output))
	require.Contains(t, output.String(), "New bindings (5)")
	require.Contains(t, output.String(), "Current selectors (5)")
	require.Contains(t, output.String(), "glm.5v-turbo.api@1.11.0")
	require.Contains(t, output.String(), "(absent) -> unirouter.capability-probe.glm-toggle-chat-function-tools.v1@1.0.0")
	require.Contains(t, output.String(), "Target state requires a separate plan")
	require.Error(t, runWithOutput(append(args, "--migrations-dir", t.TempDir()), &output))
}
