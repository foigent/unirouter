package main

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRequestSetCommandReportsOnlySafeIdentity(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	fixture := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "contracts", "source-evidence", "v1", "fixtures", "request-set.valid.v1.json"))
	var output bytes.Buffer
	require.NoError(t, run(context.Background(), []string{"validate-request-set", "--request-set", fixture}, &output))
	require.Contains(t, output.String(), "valid=true\n")
	require.Contains(t, output.String(), "schema_version=unirouter.source-evidence.request-set.v1\n")
	require.Contains(t, output.String(), "semantic_sha256=sha256:")
	require.Contains(t, output.String(), "source_scope_schema=unirouter.source-evidence.source-scope.v1\n")
	require.Contains(t, output.String(), "source_binding_sha256=sha256:")
	require.Contains(t, output.String(), "requests=1\n")
	require.NotContains(t, output.String(), "req-poc-chat-001")
}

func TestCaptureRejectsRetiredOperatorRuntimeReadbackFlag(t *testing.T) {
	var output bytes.Buffer
	err := run(context.Background(), []string{"capture", "--runtime-readback", "operator.json"}, &output)
	require.ErrorContains(t, err, "flag provided but not defined")
	require.Empty(t, output.String())
}

func TestCommandsFailClosedWithoutInputs(t *testing.T) {
	for _, arguments := range [][]string{
		nil,
		{"unknown"},
		{"validate-request-set"},
		{"prepare"},
		{"acknowledge"},
		{"verify"},
		{"capture"},
	} {
		var output bytes.Buffer
		require.Error(t, run(context.Background(), arguments, &output))
		require.Empty(t, output.String())
	}
}
