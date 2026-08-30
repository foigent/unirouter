package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunRequiresExactCompiledPublicationAndOutputDirectory(t *testing.T) {
	require.Error(t, run(nil))
	require.Error(t, run([]string{"--publication", "unknown", "--migrations-dir", t.TempDir()}))
	require.Error(t, run([]string{"--publication", "unknown", "--migrations-dir", t.TempDir(), "extra"}))
}
