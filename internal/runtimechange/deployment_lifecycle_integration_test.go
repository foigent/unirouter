//go:build integration

package runtimechange_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDeploymentLifecycleReceiptStateMachineIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	recordedPool := &recordingBeginTxPool{Pool: pool}
	manager, err := runtimechange.NewManager(
		recordedPool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	ctx := context.Background()
	executor := canonicalLifecycleFixtureExecutor{t: t, manager: manager}

	t.Run("happy path revalidates promotion and restores healthy parent", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-happy")
		candidateFixture := createAdditionalDeploymentFixture(t, pool, fixture, "lifecycle-happy-child", "active", 90, 150)
		replaceCanonicalCredentialWithNative(t, pool, &candidateFixture)
		candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_v2_lifecycle_happy_child")

		canary, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 1250)
		require.NoError(t, err)
		require.Equal(t, "canary", canary.Stage)
		require.Equal(t, int64(3), canary.Version)
		require.Equal(t, 1250, canary.CanaryBasisPoints)
		recordedPool.assertLatestRepeatableRead(t)
		assertLifecycleSlot(t, pool, fixture, stable.ID, candidate.ID, 1250, 2)

		insertFailedProbe(t, pool, candidateFixture, "promotion-must-recheck")
		_, err = executor.promote(ctx, lifecycleRequest(fixture, canary, 2))
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		assertLifecycleSlot(t, pool, fixture, stable.ID, candidate.ID, 1250, 2)
		insertSuccessfulProbeForCredential(
			t, pool, candidateFixture, candidateFixture.CredentialID,
			candidateFixture.CredentialRevision, "promotion-current-proof", time.Now().UTC().Add(2*time.Second),
		)

		active, err := executor.promote(ctx, lifecycleRequest(fixture, canary, 2))
		require.NoError(t, err)
		require.Equal(t, "active", active.Stage)
		require.Equal(t, int64(4), active.Version)
		recordedPool.assertLatestRepeatableRead(t)
		assertLifecycleSlot(t, pool, fixture, active.ID, 0, 0, 3)
		stable = mustDeploymentRelease(t, manager, stable.ReleaseRef)
		require.Equal(t, "rolled_back", stable.Stage)

		_, err = pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET status='disabled',state_revision=state_revision+1,updated_at=NOW()
 WHERE id=$1`, candidateFixture.CredentialID)
		require.NoError(t, err, "the active target may be broken during emergency rollback")
		rolledBack, err := executor.rollback(ctx, lifecycleRequest(fixture, active, 3))
		require.NoError(t, err)
		require.Equal(t, "rolled_back", rolledBack.Stage)
		require.Equal(t, int64(5), rolledBack.Version)
		recordedPool.assertLatestRepeatableRead(t)
		stable = mustDeploymentRelease(t, manager, stable.ReleaseRef)
		require.Equal(t, "active", stable.Stage)
		assertLifecycleSlot(t, pool, fixture, stable.ID, 0, 0, 4)
		assertLifecycleEvents(t, pool, candidate.ID, []string{
			"create:none:draft", "validate:draft:validated", "advance:validated:canary",
			"promote:canary:active", "rollback:active:rolled_back",
		})
	})

	t.Run("canary rollback only removes canary", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-canary-rollback")
		candidateFixture := createAdditionalDeploymentFixture(t, pool, fixture, "lifecycle-canary-rollback-child", "active", 100, 100)
		candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_v2_canary_rollback_child")
		canary, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 500)
		require.NoError(t, err)
		rolledBack, err := executor.rollback(ctx, lifecycleRequest(fixture, canary, 2))
		require.NoError(t, err)
		require.Equal(t, "rolled_back", rolledBack.Stage)
		stable = mustDeploymentRelease(t, manager, stable.ReleaseRef)
		require.Equal(t, "active", stable.Stage)
		assertLifecycleSlot(t, pool, fixture, stable.ID, 0, 0, 3)
	})

	t.Run("start rejects stale revision probe binding and evidence", func(t *testing.T) {
		cases := []struct {
			name  string
			drift func(*testing.T, *pgxpool.Pool, canonicalDeploymentFixture)
		}{
			{
				name: "revision",
				drift: func(t *testing.T, pool *pgxpool.Pool, value canonicalDeploymentFixture) {
					_, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET evidence_ref='new revision',revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2`, value.DeploymentID, value.OperationID)
					require.NoError(t, err)
				},
			},
			{
				name: "latest probe",
				drift: func(t *testing.T, pool *pgxpool.Pool, value canonicalDeploymentFixture) {
					insertFailedProbe(t, pool, value, "stale-probe")
				},
			},
			{
				name: "binding scope epoch",
				drift: func(t *testing.T, pool *pgxpool.Pool, value canonicalDeploymentFixture) {
					_, err := pool.Exec(ctx, `
DELETE FROM credential_binding_deployments
 WHERE binding_id=$1 AND deployment_id=$2`, value.CredentialID, value.DeploymentID)
					require.NoError(t, err)
					_, err = pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (
    binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,NOW()+INTERVAL '1 second')`, value.CredentialID, value.DeploymentID, value.ConnectionID)
					require.NoError(t, err)
				},
			},
			{
				name: "operation evidence",
				drift: func(t *testing.T, pool *pgxpool.Pool, value canonicalDeploymentFixture) {
					_, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET evidence_state='declared',verified_at=NULL,evidence_ref='revoked',
       revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2`, value.DeploymentID, value.OperationID)
					require.NoError(t, err)
				},
			},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-drift-"+testCase.name)
				candidateFixture := createAdditionalDeploymentFixture(t, pool, fixture, "lifecycle-drift-child-"+testCase.name, "active", 100, 100)
				candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "")
				testCase.drift(t, pool, candidateFixture)
				_, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 100)
				require.ErrorIs(t, err, runtimechange.ErrConflict)
				candidate = mustDeploymentRelease(t, manager, candidate.ReleaseRef)
				require.Equal(t, "validated", candidate.Stage)
				assertLifecycleSlot(t, pool, fixture, stable.ID, 0, 0, 1)
			})
		}
	})

	t.Run("start rejects a target with no executable route", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-start-all-open")
		candidateFixture := createAdditionalDeploymentFixture(
			t, pool, fixture, "lifecycle-start-all-open-child", "active", 100, 100,
		)
		candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_v2_start_all_open")
		setDeploymentCircuitUnavailable(t, pool, candidateFixture.DeploymentID)

		_, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 1000)
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		candidate = mustDeploymentRelease(t, manager, candidate.ReleaseRef)
		require.Equal(t, "validated", candidate.Stage)
		assertLifecycleSlot(t, pool, fixture, stable.ID, 0, 0, 1)
	})

	t.Run("start requires positive deployment limits for every operation", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-start-limits")
		chatFixture := createAdditionalDeploymentFixture(
			t, pool, fixture, "lifecycle-start-limits-chat", "active", 100, 100,
		)
		embeddingFixture := createAdditionalDeploymentFixture(
			t, pool, fixture, "lifecycle-start-limits-embedding", "active", 100, 100,
		)
		embeddingFixture = addDeploymentOperation(
			t, pool, embeddingFixture, "openai.embeddings.v1", "integration.openai.embeddings.v1",
		)
		setDeploymentExecutionLimits(t, pool, &embeddingFixture, 0, 4096, "start-limits")
		candidate := prepareSealedChildRoutes(
			t, pool, manager, fixture,
			[]runtimechange.DeploymentRoute{deploymentRoute(chatFixture), deploymentRoute(embeddingFixture)},
			"rcr_v2_start_limits",
		)

		_, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 1000)
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		require.ErrorContains(t, err, embeddingFixture.OperationID)
		candidate = mustDeploymentRelease(t, manager, candidate.ReleaseRef)
		require.Equal(t, "validated", candidate.Stage)
		assertLifecycleSlot(t, pool, fixture, stable.ID, 0, 0, 1)
	})

	t.Run("start accepts a positive same-operation alternative", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-start-limit-alternative")
		missingLimits := createAdditionalDeploymentFixture(
			t, pool, fixture, "lifecycle-start-limit-alternative-missing", "active", 100, 100,
		)
		setDeploymentExecutionLimits(t, pool, &missingLimits, 128000, 0, "start-limit-alternative")
		usable := createAdditionalDeploymentFixture(
			t, pool, fixture, "lifecycle-start-limit-alternative-usable", "active", 110, 100,
		)
		candidate := prepareSealedChildRoutes(
			t, pool, manager, fixture,
			[]runtimechange.DeploymentRoute{deploymentRoute(missingLimits), deploymentRoute(usable)},
			"rcr_v2_start_limit_alternative",
		)

		canary, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 1000)
		require.NoError(t, err)
		require.Equal(t, "canary", canary.Stage)
		assertLifecycleSlot(t, pool, fixture, stable.ID, candidate.ID, 1000, 2)
	})

	t.Run("promotion rejects a canary with no executable route", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-promote-all-open")
		candidateFixture := createAdditionalDeploymentFixture(
			t, pool, fixture, "lifecycle-promote-all-open-child", "active", 100, 100,
		)
		candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_v2_promote_all_open")
		canary, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 1000)
		require.NoError(t, err)
		setDeploymentCircuitUnavailable(t, pool, candidateFixture.DeploymentID)

		_, err = executor.promote(ctx, lifecycleRequest(fixture, canary, 2))
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		canary = mustDeploymentRelease(t, manager, canary.ReleaseRef)
		require.Equal(t, "canary", canary.Stage)
		stable = mustDeploymentRelease(t, manager, stable.ReleaseRef)
		require.Equal(t, "active", stable.Stage)
		assertLifecycleSlot(t, pool, fixture, stable.ID, canary.ID, 1000, 2)
	})

	t.Run("promotion rechecks public execution limits", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-promote-limits")
		candidateFixture := createAdditionalDeploymentFixture(
			t, pool, fixture, "lifecycle-promote-limits-child", "active", 100, 100,
		)
		candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_v2_promote_limits")
		canary, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 1000)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE model_registry SET output_limit=0 WHERE id=$1`, fixture.PublicModelID)
		require.NoError(t, err)

		_, err = executor.promote(ctx, lifecycleRequest(fixture, canary, 2))
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		require.ErrorContains(t, err, "public model execution limits")
		canary = mustDeploymentRelease(t, manager, canary.ReleaseRef)
		require.Equal(t, "canary", canary.Stage)
		stable = mustDeploymentRelease(t, manager, stable.ReleaseRef)
		require.Equal(t, "active", stable.Stage)
		assertLifecycleSlot(t, pool, fixture, stable.ID, canary.ID, 1000, 2)
	})

	t.Run("start requires an executable route for every operation", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-start-operation-gap")
		chatFixture := createAdditionalDeploymentFixture(
			t, pool, fixture, "lifecycle-start-operation-gap-chat", "active", 100, 100,
		)
		embeddingFixture := createAdditionalDeploymentFixture(
			t, pool, fixture, "lifecycle-start-operation-gap-embedding", "active", 100, 100,
		)
		embeddingFixture = addDeploymentOperation(
			t, pool, embeddingFixture, "openai.embeddings.v1", "integration.openai.embeddings.v1",
		)
		candidate := prepareSealedChildRoutes(
			t, pool, manager, fixture,
			[]runtimechange.DeploymentRoute{deploymentRoute(chatFixture), deploymentRoute(embeddingFixture)},
			"rcr_v2_start_operation_gap",
		)
		setDeploymentCircuitUnavailable(t, pool, embeddingFixture.DeploymentID)

		_, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 1000)
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		require.ErrorContains(t, err, embeddingFixture.OperationID)
		candidate = mustDeploymentRelease(t, manager, candidate.ReleaseRef)
		require.Equal(t, "validated", candidate.Stage)
		assertLifecycleSlot(t, pool, fixture, stable.ID, 0, 0, 1)
	})

	t.Run("promotion requires an executable route for every operation", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-promote-operation-gap")
		chatFixture := createAdditionalDeploymentFixture(
			t, pool, fixture, "lifecycle-promote-operation-gap-chat", "active", 100, 100,
		)
		embeddingFixture := createAdditionalDeploymentFixture(
			t, pool, fixture, "lifecycle-promote-operation-gap-embedding", "active", 100, 100,
		)
		embeddingFixture = addDeploymentOperation(
			t, pool, embeddingFixture, "openai.embeddings.v1", "integration.openai.embeddings.v1",
		)
		candidate := prepareSealedChildRoutes(
			t, pool, manager, fixture,
			[]runtimechange.DeploymentRoute{deploymentRoute(chatFixture), deploymentRoute(embeddingFixture)},
			"rcr_v2_promote_operation_gap",
		)
		canary, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 1000)
		require.NoError(t, err)
		setDeploymentCircuitUnavailable(t, pool, embeddingFixture.DeploymentID)

		_, err = executor.promote(ctx, lifecycleRequest(fixture, canary, 2))
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		require.ErrorContains(t, err, embeddingFixture.OperationID)
		canary = mustDeploymentRelease(t, manager, canary.ReleaseRef)
		require.Equal(t, "canary", canary.Stage)
		stable = mustDeploymentRelease(t, manager, stable.ReleaseRef)
		require.Equal(t, "active", stable.Stage)
		assertLifecycleSlot(t, pool, fixture, stable.ID, canary.ID, 1000, 2)
	})

	t.Run("concurrent starts admit exactly one canary", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-concurrent")
		firstFixture := createAdditionalDeploymentFixture(t, pool, fixture, "lifecycle-concurrent-a", "active", 100, 100)
		secondFixture := createAdditionalDeploymentFixture(t, pool, fixture, "lifecycle-concurrent-b", "active", 100, 100)
		first := prepareSealedChild(t, pool, manager, fixture, firstFixture, "rcr_v2_concurrent_a")
		second := prepareSealedChild(t, pool, manager, fixture, secondFixture, "rcr_v2_concurrent_b")

		start := make(chan struct{})
		results := make(chan error, 2)
		var group sync.WaitGroup
		for _, candidate := range []runtimechange.DeploymentRelease{first, second} {
			candidate := candidate
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				_, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 1000)
				results <- err
			}()
		}
		close(start)
		group.Wait()
		close(results)
		successes, conflicts := 0, 0
		for result := range results {
			switch {
			case result == nil:
				successes++
			case isLifecycleConflict(result):
				conflicts++
			default:
				t.Fatalf("unexpected concurrent start error: %v", result)
			}
		}
		require.Equal(t, 1, successes)
		require.Equal(t, 1, conflicts)
		var stableID, canaryID, slotVersion int64
		require.NoError(t, pool.QueryRow(ctx, `
SELECT stable_release_id,canary_release_id,slot_version
  FROM runtime_change_slots WHERE public_model_id=$1`, fixture.PublicModelID).Scan(
			&stableID, &canaryID, &slotVersion,
		))
		require.Equal(t, stable.ID, stableID)
		require.Contains(t, []int64{first.ID, second.ID}, canaryID)
		require.Equal(t, int64(2), slotVersion)
	})

	t.Run("active rollback rejects a non-executable parent", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-broken-parent")
		candidateFixture := createAdditionalDeploymentFixture(t, pool, fixture, "lifecycle-broken-parent-child", "active", 90, 100)
		candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_v2_broken_parent_child")
		canary, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 1000)
		require.NoError(t, err)
		active, err := executor.promote(ctx, lifecycleRequest(fixture, canary, 2))
		require.NoError(t, err)
		insertFailedProbe(t, pool, fixture, "parent-no-longer-executable")

		_, err = executor.rollback(ctx, lifecycleRequest(fixture, active, 3))
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		active = mustDeploymentRelease(t, manager, active.ReleaseRef)
		stable = mustDeploymentRelease(t, manager, stable.ReleaseRef)
		require.Equal(t, "active", active.Stage)
		require.Equal(t, "rolled_back", stable.Stage)
		assertLifecycleSlot(t, pool, fixture, active.ID, 0, 0, 3)
	})

	t.Run("active rollback rechecks public execution limits", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-rollback-limits")
		candidateFixture := createAdditionalDeploymentFixture(
			t, pool, fixture, "lifecycle-rollback-limits-child", "active", 100, 100,
		)
		candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_v2_rollback_limits")
		canary, err := executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 1000)
		require.NoError(t, err)
		active, err := executor.promote(ctx, lifecycleRequest(fixture, canary, 2))
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE model_registry SET context_length=0 WHERE id=$1`, fixture.PublicModelID)
		require.NoError(t, err)

		_, err = executor.rollback(ctx, lifecycleRequest(fixture, active, 3))
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		require.ErrorContains(t, err, "public model execution limits")
		active = mustDeploymentRelease(t, manager, active.ReleaseRef)
		stable = mustDeploymentRelease(t, manager, stable.ReleaseRef)
		require.Equal(t, "active", active.Stage)
		require.Equal(t, "rolled_back", stable.Stage)
		assertLifecycleSlot(t, pool, fixture, active.ID, 0, 0, 3)
	})

	t.Run("late slot version CAS rejects stale caller", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "lifecycle-late-cas")
		candidateFixture := createAdditionalDeploymentFixture(t, pool, fixture, "lifecycle-late-cas-child", "active", 100, 100)
		candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_v2_late_cas_child")
		_, err := pool.Exec(ctx, `
UPDATE runtime_change_slots
   SET slot_version=slot_version+1,updated_at=NOW()
 WHERE public_model_id=$1 AND slot_version=1`, fixture.PublicModelID)
		require.NoError(t, err)

		_, err = executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 1), 100)
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		assertLifecycleSlot(t, pool, fixture, stable.ID, 0, 0, 2)
		_, err = executor.startCanary(ctx, lifecycleRequest(fixture, candidate, 2), 100)
		require.NoError(t, err)
		assertLifecycleSlot(t, pool, fixture, stable.ID, candidate.ID, 100, 3)
	})

	t.Run("regular lifecycle never parses a v1 candidate", func(t *testing.T) {
		fixture := createCanonicalDeploymentFixture(t, pool, "lifecycle-v1-boundary", "active", 100, 100)
		legacyID := insertLegacyStableRelease(t, pool, fixture)
		_, err := executor.startCanary(ctx, runtimechange.DeploymentLifecycleRequest{
			ReleaseRef: legacyReleaseRef(fixture), PublicModelID: fixture.PublicModelID,
			ExpectedReleaseVersion: 1, ExpectedSlotVersion: 1,
			ActorUserID: fixture.UserID, CustomerOrgID: fixture.OrgID,
		}, 100)
		require.ErrorIs(t, err, runtimechange.ErrNotFound)
		var stage string
		require.NoError(t, pool.QueryRow(ctx, `
SELECT stage FROM runtime_change_releases WHERE id=$1`, legacyID).Scan(&stage))
		require.Equal(t, "active", stage)
	})
}

func seedCanonicalStable(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	suffix string,
) (canonicalDeploymentFixture, runtimechange.DeploymentRelease) {
	t.Helper()
	fixture := createCanonicalDeploymentFixture(t, pool, suffix, "active", 100, 100)
	makeSoleInitialCustomer(t, pool, fixture.OrgID)
	sealed := authorDeploymentRoutesFixture(
		t, pool, manager, fixture, []runtimechange.DeploymentRoute{deploymentRoute(fixture)},
		"canonical stable fixture: "+suffix,
	)
	return fixture, activateInitialDeploymentReleaseFixture(t, manager, fixture, sealed)
}

func prepareSealedChild(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	base canonicalDeploymentFixture,
	routeFixture canonicalDeploymentFixture,
	releaseRef string,
) runtimechange.DeploymentRelease {
	return prepareSealedChildRoutes(
		t, pool, manager, base, []runtimechange.DeploymentRoute{deploymentRoute(routeFixture)}, releaseRef,
	)
}

func prepareSealedChildRoutes(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	base canonicalDeploymentFixture,
	routes []runtimechange.DeploymentRoute,
	releaseRef string,
) runtimechange.DeploymentRelease {
	t.Helper()
	return authorDeploymentRoutesFixture(
		t, pool, manager, base, routes, "sealed v2 replacement fixture: "+releaseRef,
	)
}

func addDeploymentOperation(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture canonicalDeploymentFixture,
	operationID, profileID string,
) canonicalDeploymentFixture {
	t.Helper()
	fixture.OperationID = operationID
	fixture.OperationRevision = 1
	fixture.ProtocolProfileID = profileID
	fixture.ProtocolVersion = "1.0.0"
	_, err := pool.Exec(context.Background(), `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','live_probed','integration operation fixture',NOW(),1,$3,$4)`,
		fixture.DeploymentID, fixture.OperationID, fixture.ProtocolProfileID, fixture.ProtocolVersion)
	require.NoError(t, err)
	insertSuccessfulProbe(t, pool, fixture, fmt.Sprintf("operation-%s-%d", operationID, fixture.DeploymentID))
	return fixture
}

func lifecycleRequest(
	fixture canonicalDeploymentFixture,
	release runtimechange.DeploymentRelease,
	slotVersion int64,
) runtimechange.DeploymentLifecycleRequest {
	return runtimechange.DeploymentLifecycleRequest{
		ReleaseRef: release.ReleaseRef, PublicModelID: fixture.PublicModelID,
		ExpectedReleaseVersion: release.Version, ExpectedSlotVersion: slotVersion,
		ActorUserID: fixture.UserID, CustomerOrgID: fixture.OrgID,
	}
}

func mustDeploymentRelease(
	t *testing.T,
	manager *runtimechange.Manager,
	ref string,
) runtimechange.DeploymentRelease {
	t.Helper()
	release, err := manager.GetDeploymentRelease(context.Background(), ref)
	require.NoError(t, err)
	return release
}

func assertLifecycleSlot(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture canonicalDeploymentFixture,
	stableID, canaryID int64,
	basisPoints int,
	version int64,
) {
	t.Helper()
	var storedStableID int64
	var storedCanaryID *int64
	var storedBasis int
	var storedVersion int64
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT stable_release_id,canary_release_id,canary_basis_points,slot_version
  FROM runtime_change_slots WHERE public_model_id=$1`, fixture.PublicModelID).Scan(
		&storedStableID, &storedCanaryID, &storedBasis, &storedVersion,
	))
	require.Equal(t, stableID, storedStableID)
	if canaryID == 0 {
		require.Nil(t, storedCanaryID)
	} else {
		require.NotNil(t, storedCanaryID)
		require.Equal(t, canaryID, *storedCanaryID)
	}
	require.Equal(t, basisPoints, storedBasis)
	require.Equal(t, version, storedVersion)
}

func assertLifecycleEvents(t *testing.T, pool *pgxpool.Pool, releaseID int64, expected []string) {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
SELECT action,from_stage,to_stage
  FROM runtime_change_events WHERE release_id=$1 ORDER BY sequence`, releaseID)
	require.NoError(t, err)
	defer rows.Close()
	actual := make([]string, 0, len(expected))
	for rows.Next() {
		var action, fromStage, toStage string
		require.NoError(t, rows.Scan(&action, &fromStage, &toStage))
		actual = append(actual, action+":"+fromStage+":"+toStage)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, expected, actual)
}

func insertFailedProbe(t *testing.T, pool *pgxpool.Pool, fixture canonicalDeploymentFixture, source string) {
	t.Helper()
	fingerprint := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(source)))
	_, err := pool.Exec(context.Background(), `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    credential_binding_id,credential_state_revision,success,status_code,latency_ms,
    error_summary,source_kind,source_ref,source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,$4,$5,$6,$6,$7,$8,$9,$10,FALSE,503,2,'fixture failure',
        'integration.route',$11,$12,$13,NOW()+INTERVAL '1 second')`, fixture.DeploymentID,
		fixture.ConnectionID, fixture.OperationID, fixture.ProtocolProfileID, fixture.ProtocolVersion,
		fixture.UpstreamModel, fixture.ConnectionRevision, fixture.DeploymentRevision,
		fixture.CredentialID, fixture.CredentialRevision, source, fingerprint, fixture.UserID)
	require.NoError(t, err)
}

func setDeploymentCircuitUnavailable(t *testing.T, pool *pgxpool.Pool, deploymentID int64) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	var legacyChannelID *int64
	require.NoError(t, tx.QueryRow(ctx, `
SELECT legacy_channel_id FROM model_deployments WHERE id=$1 FOR UPDATE`, deploymentID).Scan(&legacyChannelID))
	var circuitRevision int64
	if legacyChannelID != nil {
		require.NoError(t, tx.QueryRow(ctx, `
UPDATE channels
   SET circuit_state='half_open',
       half_open_until=NOW()+INTERVAL '10 minutes',
       circuit_epoch=circuit_epoch+1,
       updated_at=NOW()
 WHERE id=$1
 RETURNING circuit_epoch`, *legacyChannelID).Scan(&circuitRevision))
	} else {
		require.NoError(t, tx.QueryRow(ctx, `
SELECT circuit_revision+1 FROM model_deployments WHERE id=$1`, deploymentID).Scan(&circuitRevision))
	}
	_, err = tx.Exec(ctx, `
UPDATE model_deployments
   SET circuit_state='half_open',
       half_open_until=NOW()+INTERVAL '10 minutes',
       circuit_revision=$2,
       updated_at=NOW()
 WHERE id=$1`, deploymentID, circuitRevision)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}

func setDeploymentExecutionLimits(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture *canonicalDeploymentFixture,
	maxContext, defaultMaxOutput int,
	source string,
) {
	t.Helper()
	require.NoError(t, pool.QueryRow(context.Background(), `
UPDATE model_deployments
   SET max_context=$2,default_max_output=$3,config_revision=config_revision+1,updated_at=NOW()
 WHERE id=$1
 RETURNING config_revision`, fixture.DeploymentID, maxContext, defaultMaxOutput).Scan(&fixture.DeploymentRevision))
	insertSuccessfulProbe(t, pool, *fixture, fmt.Sprintf("%s-%d", source, fixture.DeploymentID))
}

func safeReleaseSuffix(value string) string {
	result := make([]rune, 0, len(value))
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			result = append(result, char)
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}

func isLifecycleConflict(err error) bool {
	return errors.Is(err, runtimechange.ErrConflict)
}
