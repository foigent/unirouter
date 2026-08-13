//go:build integration

package customercontext

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEnsureBaselineAdoptsSoleRootAndPreservesExistingState(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	var userID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (
			org_id,username,password_hash,organization_role,role,status,auth_provider)
		VALUES ($1,'single-customer-admin','fixture','platform_admin','platform_admin','active','local')
		RETURNING id`, orgID).Scan(&userID))

	type baselineResult struct {
		baseline Baseline
		err      error
	}
	results := make(chan baselineResult, 2)
	for i := 0; i < 2; i++ {
		go func() {
			baseline, err := EnsureBaseline(ctx, pool)
			results <- baselineResult{baseline: baseline, err: err}
		}()
	}
	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	baseline := first.baseline
	require.Equal(t, baseline.ProjectID, second.baseline.ProjectID)
	require.Equal(t, StateReady, baseline.Customer.State)
	require.Equal(t, orgID, baseline.Customer.OrganizationID)
	require.Positive(t, baseline.ProjectID)

	var walletCount, membershipCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallets WHERE org_id=$1`, orgID).Scan(&walletCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM project_members
		WHERE project_id=$1 AND user_id=$2 AND role='project_admin'`, baseline.ProjectID, userID).Scan(&membershipCount))
	require.Equal(t, 1, walletCount)
	require.Equal(t, 1, membershipCount)

	_, err := pool.Exec(ctx, `UPDATE wallets SET balance_micros=123456 WHERE org_id=$1`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE projects SET status='disabled' WHERE id=$1`, baseline.ProjectID)
	require.NoError(t, err)
	replayed, err := EnsureBaseline(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, baseline.ProjectID, replayed.ProjectID)

	var balance int64
	var projectStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT balance_micros FROM wallets WHERE org_id=$1`, orgID).Scan(&balance))
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM projects WHERE id=$1`, baseline.ProjectID).Scan(&projectStatus))
	require.EqualValues(t, 123456, balance)
	require.Equal(t, "disabled", projectStatus)
}
