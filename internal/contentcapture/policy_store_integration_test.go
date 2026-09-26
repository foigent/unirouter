//go:build integration

package contentcapture

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func setupPolicyStorePG(t *testing.T) (*pgxpool.Pool, *PolicyStore, int64, int64) {
	t.Helper()
	pool := testutil.SetupMigratedTestDB(t)
	org := testutil.NewTestOrg(t, pool)
	var project int64
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'Capture policy','capture-policy','active','none') RETURNING id`, org).Scan(&project))
	s, err := NewPolicyStore(pool, policyStoreTestOptions())
	require.NoError(t, err)
	return pool, s, org, project
}

func policyStorePGTx(t *testing.T, pool *pgxpool.Pool, org, project int64) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	// Synthetic stand-in for the admin layer's existing authority lock. These
	// repository tests do not exercise administrator authentication.
	var id int64
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT id FROM projects WHERE org_id=$1 AND id=$2 FOR SHARE`, org, project).Scan(&id))
	return tx
}

func TestPolicyStorePGReadOnlyPendingAndRollback(t *testing.T) {
	pool, s, org, project := setupPolicyStorePG(t)
	first, err := s.Get(t.Context(), org, project, "not_configured")
	require.NoError(t, err)
	second, err := s.Get(t.Context(), org, project, "not_configured")
	require.NoError(t, err)
	require.Equal(t, first, second)
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_content_policies WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&count))
	require.Zero(t, count)
	tx := policyStorePGTx(t, pool, org, project)
	put := PolicyPut{SchemaVersion: PolicyPutSchema, ExpectedPolicyVersion: "1", Desired: testFullPolicy()}
	pending, err := s.PutTx(t.Context(), tx, org, project, put, PolicyApplication{StorageStatus: "unavailable", Now: time.Now()})
	require.NoError(t, err)
	require.Equal(t, "2", pending.Desired.PolicyVersion)
	require.Equal(t, "1", pending.Effective.PolicyVersion)
	require.Equal(t, first.EffectiveAt, pending.EffectiveAt)
	require.Equal(t, "pending", pending.ApplicationStatus)
	// A later audit failure must be able to roll back both materialization and
	// update. PutTx must not independently commit either change.
	require.NoError(t, tx.Rollback(t.Context()))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_content_policies WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&count))
	require.Zero(t, count)
	tx = policyStorePGTx(t, pool, org, project)
	_, err = s.PutTx(t.Context(), tx, org, project, put, PolicyApplication{StorageStatus: "unavailable", Now: time.Now()})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	read, err := s.Get(t.Context(), org, project, "unavailable")
	require.NoError(t, err)
	require.Equal(t, pending.Desired, read.Desired)
	require.Equal(t, pending.Effective, read.Effective)
	put.ExpectedPolicyVersion = "2"
	put.Desired = testMetadataPolicy()
	put.Desired.Mode = ModeDisabled
	tx = policyStorePGTx(t, pool, org, project)
	disabled, err := s.PutTx(t.Context(), tx, org, project, put, PolicyApplication{StorageStatus: "unavailable", Now: time.Now()})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	require.Equal(t, "3", disabled.Effective.PolicyVersion)
	require.Equal(t, ModeDisabled, disabled.Effective.Config.Mode)
	require.Equal(t, "applied", disabled.ApplicationStatus)
}

func TestPolicyStorePGConcurrentCASWithExistingSharedProjectLocks(t *testing.T) {
	pool, s, org, project := setupPolicyStorePG(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tx1 := policyStorePGTx(t, pool, org, project)
	tx2 := policyStorePGTx(t, pool, org, project)
	put := PolicyPut{SchemaVersion: PolicyPutSchema, ExpectedPolicyVersion: "1", Desired: testFullPolicy()}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, tx := range []pgx.Tx{tx1, tx2} {
		go func(tx pgx.Tx) {
			<-start
			_, err := s.PutTx(ctx, tx, org, project, put, PolicyApplication{StorageStatus: "ready", ApplyNow: true, Now: time.Now()})
			if err == nil {
				err = tx.Commit(ctx)
			} else {
				_ = tx.Rollback(context.Background())
			}
			results <- err
		}(tx)
	}
	close(start)
	var successes, conflicts int
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err == nil {
				successes++
			} else if errors.Is(err, ErrPolicyVersionConflict) {
				conflicts++
			} else {
				t.Fatalf("CAS/lock failure: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("policy CAS blocked with two project FOR SHARE locks", ctx.Err())
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	view, err := s.Get(t.Context(), org, project, "ready")
	require.NoError(t, err)
	require.Equal(t, "2", view.Desired.PolicyVersion)
	require.Equal(t, view.Desired, view.Effective)
}
