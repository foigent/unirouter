//go:build integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/resetplan"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestResetPreviewInventoriesMigratedDatabaseWithoutMutation(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var channelID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channels (name, provider, base_url, status)
		VALUES ('reset-preview', 'openai', 'https://example.invalid', 'disabled')
		RETURNING id`).Scan(&channelID))

	inventory, err := inspectResetInventory(ctx, pool)
	require.NoError(t, err)
	plan, err := resetplan.Build(inventory, "source-before", "source-after", time.Unix(0, 0))
	require.NoError(t, err)
	require.True(t, plan.ScopeComplete)
	require.Zero(t, plan.Summary.ReviewRequiredCount)
	require.False(t, plan.ActionAuthorized)
	require.False(t, plan.ApplySupported)
	require.False(t, plan.MutationPerformed)

	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM channels WHERE id=$1`, channelID).Scan(&rows))
	require.Equal(t, 1, rows, "read-only preview changed product data")
}
