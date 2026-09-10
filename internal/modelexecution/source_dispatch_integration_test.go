//go:build integration

package modelexecution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

// The source package reaches this package through the media gateway. This
// boundary test therefore uses an isolated receipt table; the source package
// separately tests its real M117 writer and strict documents.
type dispatchReceiptRecorder struct{ fail bool }

func (r dispatchReceiptRecorder) MaterializeDispatchTx(ctx context.Context, tx pgx.Tx, plan modelrouting.Plan, route modelrouting.Route, start billing.ProviderAttemptStart, hash string, at time.Time) error {
	var original bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3)`, start.OrgID, start.RequestID, start.AttemptID).Scan(&original); err != nil {
		return err
	}
	if !original || hash == "" || at.IsZero() {
		return errors.New("source invoked before durable dispatch authority")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO test_dispatch_materializer_receipts(request_id,attempt_id,evidence_hash,recorded_at) VALUES($1,$2,$3,$4)`, start.RequestID, start.AttemptID, hash, at); err != nil {
		return err
	}
	if r.fail {
		return errors.New("injected source failure after publication")
	}
	return nil
}
func setupDispatchReceipt(t *testing.T, f *executionFixture) {
	t.Helper()
	_, err := f.pool.Exec(t.Context(), `CREATE TABLE test_dispatch_materializer_receipts(request_id TEXT NOT NULL,attempt_id TEXT NOT NULL,evidence_hash TEXT NOT NULL,recorded_at TIMESTAMPTZ NOT NULL,PRIMARY KEY(request_id,attempt_id))`)
	require.NoError(t, err)
}
func requireDispatchReceipt(t *testing.T, f *executionFixture, request string, count int) {
	t.Helper()
	var found int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM test_dispatch_materializer_receipts WHERE request_id=$1`, request).Scan(&found))
	require.Equal(t, count, found)
}
func TestSourceDispatchFailureRollsBackOriginalAttemptAndPublication(t *testing.T) {
	f := newExecutionFixture(t, true, false)
	setupDispatchReceipt(t, f)
	plan, route := f.plan(t, modelrouting.ModeCanonical, "source-rollback")
	identity := f.freeze(t, "source-rollback")
	c, err := New(f.pool, f.billing, DefaultConfig())
	require.NoError(t, err)
	c.SetDispatchMaterializer(dispatchReceiptRecorder{fail: true})
	_, err = c.Authorize(t.Context(), plan, route, identity)
	require.ErrorContains(t, err, "injected source failure after publication")
	f.requireAttemptCount(t, identity.RequestID, 0)
	requireDispatchReceipt(t, f, identity.RequestID, 0)
	c.SetDispatchMaterializer(dispatchReceiptRecorder{})
	decision, err := c.Authorize(t.Context(), plan, route, identity)
	require.NoError(t, err)
	require.Equal(t, Applied, decision.Status)
	f.requireAttemptCount(t, identity.RequestID, 1)
	requireDispatchReceipt(t, f, identity.RequestID, 1)
}
func TestSourceDispatchAmbiguousCommitPreservesOnePublicationAndOwner(t *testing.T) {
	for _, mode := range []ambiguousCommitMode{commitThenError, rollbackThenError} {
		t.Run(map[ambiguousCommitMode]string{commitThenError: "landed", rollbackThenError: "rolled-back"}[mode], func(t *testing.T) {
			f := newExecutionFixture(t, true, false)
			setupDispatchReceipt(t, f)
			plan, route := f.plan(t, modelrouting.ModeCanonical, "source-ambiguity")
			identity := f.freeze(t, "source-ambiguity")
			c, err := New(&ambiguousCommitStarter{pool: f.pool, mode: mode}, f.billing, DefaultConfig())
			require.NoError(t, err)
			c.SetDispatchMaterializer(dispatchReceiptRecorder{})
			decision, err := c.Authorize(t.Context(), plan, route, identity)
			require.NoError(t, err)
			require.Equal(t, Applied, decision.Status)
			f.requireAttemptCount(t, identity.RequestID, 1)
			requireDispatchReceipt(t, f, identity.RequestID, 1)
			replay, err := c.Authorize(t.Context(), plan, route, identity)
			require.NoError(t, err)
			require.Equal(t, Stale, replay.Status)
			f.requireAttemptCount(t, identity.RequestID, 1)
			requireDispatchReceipt(t, f, identity.RequestID, 1)
		})
	}
}
