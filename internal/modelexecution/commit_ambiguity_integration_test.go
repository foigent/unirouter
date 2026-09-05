//go:build integration

package modelexecution

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

var errInjectedAmbiguousCommit = errors.New("injected ambiguous commit result")

type ambiguousCommitMode uint8

const (
	commitThenError ambiguousCommitMode = iota + 1
	rollbackThenError
)

type ambiguousCommitStarter struct {
	pool        *pgxpool.Pool
	mode        ambiguousCommitMode
	injected    atomic.Bool
	afterCommit func(context.Context) error
}

func (starter *ambiguousCommitStarter) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := starter.pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	if starter.injected.CompareAndSwap(false, true) {
		return &ambiguousCommitTx{Tx: tx, mode: starter.mode, afterCommit: starter.afterCommit}, nil
	}
	return tx, nil
}

type ambiguousCommitTx struct {
	pgx.Tx
	mode        ambiguousCommitMode
	afterCommit func(context.Context) error
}

func (tx *ambiguousCommitTx) Commit(ctx context.Context) error {
	switch tx.mode {
	case commitThenError:
		if err := tx.Tx.Commit(ctx); err != nil {
			return err
		}
		if tx.afterCommit != nil {
			if err := tx.afterCommit(ctx); err != nil {
				return err
			}
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

func TestAuthorizeOwnerSurvivesCommitAmbiguityAcrossCompatibleExpandIntegration(t *testing.T) {
	fixture := newExecutionFixture(t, true, false)
	dir := "../../migrations"
	down, err := os.ReadFile(filepath.Join(dir, schemaauthority.CapabilityExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = fixture.pool.Exec(t.Context(), string(down))
	require.NoError(t, err)
	ledger, err := schemaauthority.ExpectedLedger(dir)
	require.NoError(t, err)
	wire, err := os.ReadFile(filepath.Join(dir, schemaauthority.CapabilityExpandHead+".up.sql"))
	require.NoError(t, err)
	process := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("a", 32), Artifact: "sha256:" + strings.Repeat("b", 64)}
	manifest := runtimegeneration.CurrentManifest([]string{schemaauthority.LegacyCapabilityHead, schemaauthority.CapabilityExpandHead})
	plan, route := fixture.plan(t, modelrouting.ModeCanonical, "bridge-ambiguous-owner")
	identity := fixture.freeze(t, "bridge-ambiguous-owner")
	starter := &ambiguousCommitStarter{pool: fixture.pool, mode: commitThenError, afterCommit: func(ctx context.Context) error {
		return migrate.RunCompatibleExpand(ctx, fixture.pool, dir, migrate.CompatibleExpandHooks{
			ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(wire)), Before: func(context.Context, *pgxpool.Conn) error { return nil },
			After: func(ctx context.Context, tx pgx.Tx) error {
				return runtimegeneration.BootstrapVerifiedBridge(ctx, tx, process, manifest, ledger)
			},
		})
	}}
	coordinator, err := New(starter, fixture.billing, DefaultConfig())
	require.NoError(t, err)
	tokens := 0
	coordinator.newToken = func() (string, error) { tokens++; return fmt.Sprintf("bridge-call-owner-%02d", tokens), nil }
	ctx := runtimegeneration.WithIdentity(t.Context(), process)
	decision, err := coordinator.Authorize(ctx, plan, route, identity)
	require.NoError(t, err)
	require.Equal(t, Applied, decision.Status)
	require.Equal(t, 1, tokens)
	fixture.requireAttemptCount(t, identity.RequestID, 1)
	var instance, owner string
	require.NoError(t, fixture.pool.QueryRow(ctx, `SELECT COALESCE(runtime_instance_id,''),dispatch_owner_hash FROM billing_provider_attempts WHERE request_id=$1`, identity.RequestID).Scan(&instance, &owner))
	require.Empty(t, instance, "M113 receipt cannot be retrofitted during recovery")
	require.Equal(t, probeTokenHash(decision.DispatchOwnerToken), owner)
	// No refresh or Renew is called: bootstrap must already support the next
	// ordinary request immediately after expand commit.
	next := fixture.freeze(t, "bridge-immediate-after-expand")
	decision, err = coordinator.Authorize(ctx, plan, route, next)
	require.NoError(t, err)
	require.Equal(t, Applied, decision.Status)
	require.NoError(t, fixture.pool.QueryRow(ctx, `SELECT runtime_instance_id FROM billing_provider_attempts WHERE request_id=$1`, next.RequestID).Scan(&instance))
	require.Equal(t, process.Instance, instance)
	duplicate, err := coordinator.Authorize(ctx, plan, route, identity)
	require.NoError(t, err)
	require.Equal(t, Stale, duplicate.Status, "another owner cannot recover the old attempt")
	fixture.requireAttemptCount(t, identity.RequestID, 1)
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
