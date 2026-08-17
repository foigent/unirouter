package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMainLoadsSensitivePolicyBeforeListening(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	require.NoError(t, err)
	src := string(raw)
	loadIndex := strings.Index(src, "loadSensitiveWords(initialFilterCtx, postgres.Pool)")
	listenIndex := strings.Index(src, "srv.ListenAndServe()")
	require.GreaterOrEqual(t, loadIndex, 0)
	require.Greater(t, listenIndex, loadIndex)
}
