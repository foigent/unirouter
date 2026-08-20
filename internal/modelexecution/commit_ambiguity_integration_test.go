//go:build integration

package modelexecution

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

var errInjectedAmbiguousCommit = errors.New("injected ambiguous commit result")

type ambiguousCommitMode uint8

const (
	commitThenError ambiguousCommitMode = iota + 1
	rollbackThenError
)

type ambiguousCommitStarter struct {
	pool     *pgxpool.Pool
	mode     ambiguousCommitMode
	injected atomic.Bool
}

func (starter *ambiguousCommitStarter) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := starter.pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	if starter.injected.CompareAndSwap(false, true) {
		return &ambiguousCommitTx{Tx: tx, mode: starter.mode}, nil
	}
	return tx, nil
}

type ambiguousCommitTx struct {
	pgx.Tx
	mode ambiguousCommitMode
}

func (tx *ambiguousCommitTx) Commit(ctx context.Context) error {
	switch tx.mode {
	case commitThenError:
		if err := tx.Tx.Commit(ctx); err != nil {
			return err
		}
	case rollbackThenError:
		if err := tx.Tx.Rollback(ctx); err != nil {
			return err
		}
	default:
		return tx.Tx.Commit(ctx)
	}
	return errInjectedAmbiguousCommit
}

func TestAuthorizeRecoversOneAmbiguousCommitWithTheSameOwner(t *testing.T) {
	for _, test := range []struct {
		name string
		mode ambiguousCommitMode
	}{
		{name: "commit landed", mode: commitThenError},
		{name: "commit rolled back", mode: rollbackThenError},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionFixture(t, true, false)
			plan, route := fixture.plan(t, modelrouting.ModeCanonical, "authorize-ambiguous-"+test.name)
			identity := fixture.freeze(t, "authorize-ambiguous-"+test.name)
			starter := &ambiguousCommitStarter{pool: fixture.pool, mode: test.mode}
			coordinator, err := New(starter, fixture.billing, DefaultConfig())
			require.NoError(t, err)
			tokenCalls := 0
			coordinator.newToken = func() (string, error) {
				tokenCalls++
				return fmt.Sprintf("one-call-owner-%02d", tokenCalls), nil
			}

			decision, err := coordinator.Authorize(t.Context(), plan, route, identity)
			require.NoError(t, err)
			require.Equal(t, Applied, decision.Status)
			require.Equal(t, 1, tokenCalls, "commit recovery must reuse the prepared raw owner")
			fixture.requireAttemptCount(t, identity.RequestID, 1)
			var ownerHash string
			require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT dispatch_owner_hash FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, fixture.orgID,
				identity.RequestID, identity.AttemptID).Scan(&ownerHash))
			require.Equal(t, probeTokenHash(decision.DispatchOwnerToken), ownerHash)
		})
	}
}

func TestTerminalHealthRecoversAmbiguousCommitWithoutDoubleApplyingBreaker(t *testing.T) {
	for _, test := range []struct {
		name string
		mode ambiguousCommitMode
	}{
		{name: "commit landed", mode: commitThenError},
		{name: "commit rolled back", mode: rollbackThenError},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionFixture(t, true, false)
			_, decision := fixture.authorizedDecision(t, modelrouting.ModeCanonical, "health-ambiguous-"+test.name)
			fixture.completeAcceptedAttempt(t, decision)
			starter := &ambiguousCommitStarter{pool: fixture.pool, mode: test.mode}
			coordinator, err := New(starter, fixture.billing, DefaultConfig())
			require.NoError(t, err)

			result, err := coordinator.MarkProviderFailure(t.Context(), decision)
			require.NoError(t, err)
			require.Equal(t, Applied, result.Status)
			_, breaker := fixture.breakers(t)
			require.Equal(t, 1, breaker.failCount,
				"terminal fence replay must not apply breaker failure twice")
			var outcome string
			require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT health_outcome FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, fixture.orgID,
				decision.ProviderAttemptKey.RequestID, decision.ProviderAttemptKey.AttemptID).Scan(&outcome))
			require.Equal(t, "provider_failure", outcome)
		})
	}
}

func TestRenewLeaseRecoversAmbiguousCommitWithOwnerAndCAS(t *testing.T) {
	for _, test := range []struct {
		name string
		mode ambiguousCommitMode
	}{
		{name: "commit landed", mode: commitThenError},
		{name: "commit rolled back", mode: rollbackThenError},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionFixture(t, true, true)
			_, decision := fixture.authorizedDecision(t, modelrouting.ModeCanonical, "renew-ambiguous-"+test.name)
			_, before := fixture.breakers(t)
			starter := &ambiguousCommitStarter{pool: fixture.pool, mode: test.mode}
			coordinator, err := New(starter, fixture.billing, DefaultConfig())
			require.NoError(t, err)

			result, err := coordinator.RenewLease(t.Context(), decision)
			require.NoError(t, err)
			require.Equal(t, Applied, result.Status)
			_, after := fixture.breakers(t)
			require.NotNil(t, before.probeUntil)
			require.NotNil(t, after.probeUntil)
			require.True(t, after.probeUntil.After(*before.probeUntil))
			require.Equal(t, before.circuitRevision, after.circuitRevision,
				"lease recovery must not create a breaker transition")
		})
	}
}
