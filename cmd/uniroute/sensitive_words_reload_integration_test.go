//go:build integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestLoadSensitiveWordsReturnsVersionedPolicyAndRejectsInvalidShape(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	wantVersion := time.Date(2026, 7, 11, 12, 0, 0, 123000000, time.UTC)
	_, err := pool.Exec(ctx,
		`UPDATE settings SET value = '["p6f35-versioned"]'::jsonb, updated_at = $1 WHERE key = 'sensitive_words'`,
		wantVersion)
	require.NoError(t, err)

	words, version, err := loadSensitiveWords(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, []string{"p6f35-versioned"}, words)
	require.True(t, version.Equal(wantVersion))

	_, err = pool.Exec(ctx,
		`UPDATE settings SET value = '{"not":"an-array"}'::jsonb WHERE key = 'sensitive_words'`)
	require.NoError(t, err)
	_, _, err = loadSensitiveWords(ctx, pool)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse sensitive_words")
}
