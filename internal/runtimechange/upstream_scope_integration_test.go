//go:build integration

package runtimechange_test

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
	"strings"
	"testing"
	"time"
)

func recordLifecycleDeclaration(t *testing.T, pool *pgxpool.Pool, f canonicalDeploymentFixture) {
	t.Helper()
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	state, scope, err := upstreamrelease.Read(ctx, tx, f.OrgID, f.DeploymentID, true)
	require.NoError(t, err)
	next, err := upstreamrelease.Next(scope, state, upstreamrelease.Input{ExpectedRevision: state.Revision, ExpectedIdentity: upstreamrelease.Identity(state.Observation), ReleaseLabel: "new upstream declaration", SourceURL: "https://provider.example.test/release"}, f.UserID, time.Now())
	require.NoError(t, err)
	wire, err := json.Marshal(next.Observation)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE model_deployments SET upstream_release_observation=$2,upstream_observation_revision=$3 WHERE id=$1`, f.DeploymentID, wire, next.Revision)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}
func newScopeLifecycleManager(t *testing.T, pool *pgxpool.Pool) *runtimechange.Manager {
	t.Helper()
	m, err := runtimechange.NewManager(&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)
	return m
}
func requireRetainedScopeConflict(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, runtimechange.ErrConflict)
	require.ErrorContains(t, err, "retained authoring upstream scope differs")
}

func TestUpstreamObservationExpansionAndSafetyOwnersIntegration(t *testing.T) {
	t.Run("initial", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		m := newScopeLifecycleManager(t, pool)
		f, sealed := prepareInitialDeploymentRelease(t, pool, m, "upstream_initial")
		recordLifecycleDeclaration(t, pool, f)
		executor := canonicalLifecycleFixtureExecutor{t: t, manager: m}
		_, err := executor.activateInitial(t.Context(), initialDeploymentActivationRequest(f, sealed))
		requireRetainedScopeConflict(t, err)
		require.Equal(t, "validated", mustDeploymentRelease(t, m, sealed.ReleaseRef).Stage)
	})
	t.Run("canary", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		m := newScopeLifecycleManager(t, pool)
		f, stable := seedCanonicalStable(t, pool, m, "upstream_canary")
		child := createAdditionalDeploymentFixture(t, pool, f, "upstream-canary-child", "active", 100, 100)
		candidate := prepareSealedChild(t, pool, m, f, child, "rcr_upstream_canary")
		recordLifecycleDeclaration(t, pool, child)
		executor := canonicalLifecycleFixtureExecutor{t: t, manager: m}
		_, err := executor.startCanary(t.Context(), lifecycleRequest(f, candidate, 1), 1250)
		requireRetainedScopeConflict(t, err)
		assertLifecycleSlot(t, pool, f, stable.ID, 0, 0, 1)
	})
	t.Run("promote and cancel", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		m := newScopeLifecycleManager(t, pool)
		f, stable := seedCanonicalStable(t, pool, m, "upstream_promote")
		child := createAdditionalDeploymentFixture(t, pool, f, "upstream-promote-child", "active", 100, 100)
		candidate := prepareSealedChild(t, pool, m, f, child, "rcr_upstream_promote")
		executor := canonicalLifecycleFixtureExecutor{t: t, manager: m}
		canary, err := executor.startCanary(t.Context(), lifecycleRequest(f, candidate, 1), 1250)
		require.NoError(t, err)
		recordLifecycleDeclaration(t, pool, child)
		_, err = executor.promote(t.Context(), lifecycleRequest(f, canary, 2))
		requireRetainedScopeConflict(t, err)
		_, err = executor.rollback(t.Context(), lifecycleRequest(f, canary, 2))
		require.NoError(t, err)
		assertLifecycleSlot(t, pool, f, stable.ID, 0, 0, 3)
	})
	t.Run("stable rollback", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		m := newScopeLifecycleManager(t, pool)
		f, stable := seedCanonicalStable(t, pool, m, "upstream_rollback")
		child := createAdditionalDeploymentFixture(t, pool, f, "upstream-rollback-child", "active", 100, 100)
		candidate := prepareSealedChild(t, pool, m, f, child, "rcr_upstream_rollback")
		executor := canonicalLifecycleFixtureExecutor{t: t, manager: m}
		canary, err := executor.startCanary(t.Context(), lifecycleRequest(f, candidate, 1), 1250)
		require.NoError(t, err)
		active, err := executor.promote(t.Context(), lifecycleRequest(f, canary, 2))
		require.NoError(t, err)
		recordLifecycleDeclaration(t, pool, f)
		recordLifecycleDeclaration(t, pool, child)
		_, err = executor.rollback(t.Context(), lifecycleRequest(f, active, 3))
		require.NoError(t, err)
		assertLifecycleSlot(t, pool, f, stable.ID, 0, 0, 4)
	})
}

func TestUpstreamObservationLegacyCutoverAndRollbackOwnersIntegration(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "cutover"
		if rollback {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			planner, err := modelrouting.NewPlanner(pool, routingpolicy.Default())
			require.NoError(t, err)
			m, err := runtimechange.NewManagerWithLegacyCutoverVerifier(pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(), planner)
			require.NoError(t, err)
			f := createCanonicalDeploymentFixture(t, pool, "upstream_legacy_"+name, "active", 100, 100)
			insertLegacyStableRelease(t, pool, f)
			_, err = m.PrepareLegacyCompatibilitySeal(ctx, legacyReleaseRef(f), 1, f.OrgID, f.PublicModelID)
			require.NoError(t, err)
			target := authorLegacyCutoverRelease(t, pool, m, f, "upstream legacy fixture")
			mutation := runtimechange.DeploymentLifecycleMutation{Action: runtimechange.DeploymentLifecycleCutoverLegacy, ReleaseRef: target.ReleaseRef, ExpectedReleaseVersion: target.Version, ExpectedSlotVersion: 1, Reason: "scoped legacy fixture"}
			if !rollback {
				recordLifecycleDeclaration(t, pool, f)
				_, err = applyLegacyLifecycle(t, pool, m, f, f.UserID, mutation)
				requireRetainedScopeConflict(t, err)
				return
			}
			_, err = applyLegacyLifecycle(t, pool, m, f, f.UserID, mutation)
			require.NoError(t, err)
			recordLifecycleDeclaration(t, pool, f)
			active := mustDeploymentRelease(t, m, target.ReleaseRef)
			mutation.Action = runtimechange.DeploymentLifecycleRollbackLegacy
			mutation.ExpectedReleaseVersion = active.Version
			mutation.ExpectedSlotVersion = 2
			mutation.Reason = "safe legacy contraction"
			_, err = applyLegacyLifecycle(t, pool, m, f, f.UserID, mutation)
			require.NoError(t, err)
		})
	}
}

func TestUpstreamObservationSuccessorOwnersIntegration(t *testing.T) {
	for _, mode := range []string{"published_first", "never_activated", "primary_alias"} {
		t.Run(mode, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			m := newScopeLifecycleManager(t, pool)
			base := createCanonicalDeploymentFixture(t, pool, "upstream_successor_"+mode, "active", 100, 100)
			makeSoleInitialCustomer(t, pool, base.OrgID)
			if mode == "never_activated" {
				_, err := pool.Exec(ctx, `UPDATE model_registry SET lifecycle='staging',approved_catalog=FALSE WHERE id=$1`, base.PublicModelID)
				require.NoError(t, err)
			}
			next := preciseSuccessorFixture(t, pool, base, "upstream-next-"+strings.ReplaceAll(mode, "_", "-"), "1.2.0")
			target := sealInitialDeploymentRoutes(t, pool, m, next, []runtimechange.DeploymentRoute{deploymentRoute(next)}, "rcr_upstream_successor_"+mode)
			command := repeatTestCommand(base.ModelName, base, next, target, "upstream_"+mode)
			if mode == "never_activated" {
				command.Mutation.PredecessorAuthorityMode = runtimechange.ModelSuccessorPredecessorNeverActivated
				command.Mutation.ExpectedPredecessorLifecycle = "staging"
				command.Mutation.ExpectedPredecessorApprovedCatalog = false
			}
			if mode == "primary_alias" {
				first, err := m.ApplyModelSuccessorCutover(ctx, command)
				require.NoError(t, err)
				require.NotZero(t, first.Lifecycle.ReceiptID)
				third := preciseSuccessorFixture(t, pool, base, "upstream-primary-third", "1.3.0")
				thirdRelease := sealInitialDeploymentRoutes(t, pool, m, third, []runtimechange.DeploymentRoute{deploymentRoute(third)}, "rcr_upstream_primary_third")
				command = repeatAuthority(t, pool, repeatTestCommand(base.ModelName, next, third, thirdRelease, "upstream_primary_repeat"))
				next = third
			}
			recordLifecycleDeclaration(t, pool, next)
			_, err := m.ApplyModelSuccessorCutover(ctx, command)
			requireRetainedScopeConflict(t, err)
		})
	}
}
