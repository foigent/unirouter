//go:build integration

package runtimechange_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestInitialDeploymentLifecycleReceiptStateMachineIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	recordedPool := &recordingBeginTxPool{Pool: pool}
	manager, err := runtimechange.NewManager(
		recordedPool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	ctx := context.Background()
	executor := canonicalLifecycleFixtureExecutor{t: t, manager: manager}

	t.Run("creates the first stable slot and immutable event atomically", func(t *testing.T) {
		fixture, sealed := prepareInitialDeploymentRelease(t, pool, manager, "initial_success")
		activated, err := executor.activateInitial(
			ctx, initialDeploymentActivationRequest(fixture, sealed),
		)
		require.NoError(t, err)
		require.Equal(t, "active", activated.Stage)
		require.Equal(t, sealed.Version+1, activated.Version)
		require.Nil(t, activated.ParentStableReleaseID)
		require.NotNil(t, activated.ActivatedBy)
		require.Equal(t, fixture.UserID, *activated.ActivatedBy)
		require.NotNil(t, activated.ActivatedAt)
		recordedPool.assertLatestRepeatableRead(t)

		var stableID, publicModelID, slotVersion int64
		var canaryID *int64
		var basisPoints int
		require.NoError(t, pool.QueryRow(ctx, `
SELECT stable_release_id,canary_release_id,canary_basis_points,slot_version,public_model_id
  FROM runtime_change_slots WHERE model_name=$1`, fixture.ModelName).Scan(
			&stableID, &canaryID, &basisPoints, &slotVersion, &publicModelID,
		))
		require.Equal(t, activated.ID, stableID)
		require.Nil(t, canaryID)
		require.Zero(t, basisPoints)
		require.Equal(t, int64(1), slotVersion)
		require.Equal(t, fixture.PublicModelID, publicModelID)

		var sequence, expectedVersion, resultingVersion, actorID int64
		var action, fromStage, toStage, initial, slotPrecondition, resultingSlotVersion string
		require.NoError(t, pool.QueryRow(ctx, `
SELECT sequence,action,from_stage,to_stage,actor_user_id,
       expected_version,resulting_version,
       detail->>'initial_activation',detail->>'slot_precondition',
       detail->>'resulting_slot_version'
  FROM runtime_change_events
 WHERE release_id=$1 AND action='promote'`, activated.ID).Scan(
			&sequence, &action, &fromStage, &toStage, &actorID,
			&expectedVersion, &resultingVersion, &initial, &slotPrecondition,
			&resultingSlotVersion,
		))
		require.Equal(t, sealed.Version+1, sequence)
		require.Equal(t, "promote", action)
		require.Equal(t, "validated", fromStage)
		require.Equal(t, "active", toStage)
		require.Equal(t, fixture.UserID, actorID)
		require.Equal(t, sealed.Version, expectedVersion)
		require.Equal(t, sealed.Version+1, resultingVersion)
		require.Equal(t, "true", initial)
		require.Equal(t, "absent", slotPrecondition)
		require.Equal(t, "1", resultingSlotVersion)

		assignment, err := manager.StableDeployment(ctx, fixture.OrgID, fixture.ModelName)
		require.NoError(t, err)
		require.Equal(t, activated.ReleaseRef, assignment.ReleaseRef)
		require.Equal(t, activated.RouteItemsHash, assignment.RouteItemsHash)
		require.Len(t, assignment.Routes, 1)
	})

	t.Run("two concurrent parentless releases admit exactly one stable", func(t *testing.T) {
		fixture := createCanonicalDeploymentFixture(t, pool, "initial_concurrent", "active", 100, 100)
		makeSoleInitialCustomer(t, pool, fixture.OrgID)
		otherRoute := createAdditionalDeploymentFixture(
			t, pool, fixture, "initial-concurrent-other", "active", 110, 100,
		)
		first := sealInitialDeploymentRoutes(
			t, pool, manager, fixture, []runtimechange.DeploymentRoute{deploymentRoute(fixture)},
			"rcr_initial_concurrent_a",
		)
		second := sealInitialDeploymentRoutes(
			t, pool, manager, fixture, []runtimechange.DeploymentRoute{deploymentRoute(otherRoute)},
			"rcr_initial_concurrent_b",
		)

		start := make(chan struct{})
		results := make(chan error, 2)
		var group sync.WaitGroup
		for _, release := range []runtimechange.DeploymentRelease{first, second} {
			release := release
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				_, activateErr := executor.activateInitial(
					ctx, initialDeploymentActivationRequest(fixture, release),
				)
				results <- activateErr
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
			case errors.Is(result, runtimechange.ErrConflict):
				conflicts++
			default:
				t.Fatalf("unexpected concurrent initial activation error: %v", result)
			}
		}
		require.Equal(t, 1, successes)
		require.Equal(t, 1, conflicts)

		var activeCount, validatedCount, slotCount, promotionCount int
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FILTER (WHERE stage='active'),
       COUNT(*) FILTER (WHERE stage='validated')
  FROM runtime_change_releases WHERE id=ANY($1::bigint[])`, []int64{first.ID, second.ID}).Scan(
			&activeCount, &validatedCount,
		))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM runtime_change_slots
 WHERE model_name=$1 OR public_model_id=$2`, fixture.ModelName, fixture.PublicModelID).Scan(&slotCount))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM runtime_change_events
 WHERE release_id=ANY($1::bigint[]) AND action='promote'`, []int64{first.ID, second.ID}).Scan(&promotionCount))
		require.Equal(t, 1, activeCount)
		require.Equal(t, 1, validatedCount)
		require.Equal(t, 1, slotCount)
		require.Equal(t, 1, promotionCount)
	})

	t.Run("rejects every pre-existing slot shape", func(t *testing.T) {
		for _, testCase := range []struct {
			name string
			seed func(*testing.T, *pgxpool.Pool, canonicalDeploymentFixture)
		}{
			{
				name: "empty canonical placeholder",
				seed: func(t *testing.T, pool *pgxpool.Pool, fixture canonicalDeploymentFixture) {
					_, err := pool.Exec(ctx, `
INSERT INTO runtime_change_slots (
    model_name,stable_release_id,canary_release_id,canary_basis_points,
    slot_version,public_model_id)
VALUES ($1,NULL,NULL,0,1,$2)`, fixture.ModelName, fixture.PublicModelID)
					require.NoError(t, err)
				},
			},
			{
				name: "legacy stable",
				seed: func(t *testing.T, pool *pgxpool.Pool, fixture canonicalDeploymentFixture) {
					insertLegacyStableRelease(t, pool, fixture)
				},
			},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				fixture, sealed := prepareInitialDeploymentRelease(
					t, pool, manager, "initial_slot_"+safeReleaseSuffix(testCase.name),
				)
				testCase.seed(t, pool, fixture)
				_, err := executor.activateInitial(
					ctx, initialDeploymentActivationRequest(fixture, sealed),
				)
				require.ErrorIs(t, err, runtimechange.ErrConflict)
				assertInitialTargetUnchanged(t, pool, manager, sealed, true)
			})
		}
	})

	t.Run("rejects foreign actor and ambiguous customer authority", func(t *testing.T) {
		t.Run("inactive actor", func(t *testing.T) {
			fixture, sealed := prepareInitialDeploymentRelease(t, pool, manager, "initial_inactive_actor")
			_, err := pool.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, fixture.UserID)
			require.NoError(t, err)
			_, err = executor.activateInitial(
				ctx, initialDeploymentActivationRequest(fixture, sealed),
			)
			require.ErrorIs(t, err, runtimechange.ErrConflict)
			assertInitialTargetUnchanged(t, pool, manager, sealed, false)
		})

		t.Run("foreign actor", func(t *testing.T) {
			fixture, sealed := prepareInitialDeploymentRelease(t, pool, manager, "initial_foreign_actor")
			foreignOrgID := testutil.NewTestOrg(t, pool)
			var foreignUserID int64
			require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'initial-foreign-actor','fixture','platform_admin','platform_admin','active')
RETURNING id`, foreignOrgID).Scan(&foreignUserID))
			request := initialDeploymentActivationRequest(fixture, sealed)
			request.ActorUserID = foreignUserID
			_, err := executor.activateInitial(ctx, request)
			require.ErrorIs(t, err, runtimechange.ErrConflict)
			assertInitialTargetUnchanged(t, pool, manager, sealed, false)
		})

		t.Run("two root customers", func(t *testing.T) {
			fixture, sealed := prepareInitialDeploymentRelease(t, pool, manager, "initial_ambiguous_customer")
			otherOrgID := testutil.NewTestOrg(t, pool)
			_, err := pool.Exec(ctx, `UPDATE orgs SET org_type='customer' WHERE id=$1`, otherOrgID)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), `UPDATE orgs SET org_type='standard' WHERE id=$1`, otherOrgID)
			})
			_, err = executor.activateInitial(
				ctx, initialDeploymentActivationRequest(fixture, sealed),
			)
			require.ErrorIs(t, err, runtimechange.ErrConflict)
			assertInitialTargetUnchanged(t, pool, manager, sealed, false)
		})
	})

	t.Run("rejects a retired public model", func(t *testing.T) {
		fixture, sealed := prepareInitialDeploymentRelease(t, pool, manager, "initial_retired")
		_, err := pool.Exec(ctx, `UPDATE model_registry SET lifecycle='retired' WHERE id=$1`, fixture.PublicModelID)
		require.NoError(t, err)
		_, err = executor.activateInitial(
			ctx, initialDeploymentActivationRequest(fixture, sealed),
		)
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		assertInitialTargetUnchanged(t, pool, manager, sealed, false)
	})

	t.Run("rejects a stale release version without a slot revision input", func(t *testing.T) {
		fixture, sealed := prepareInitialDeploymentRelease(t, pool, manager, "initial_stale_release")
		request := initialDeploymentActivationRequest(fixture, sealed)
		request.ExpectedReleaseVersion--
		_, err := executor.activateInitial(ctx, request)
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		assertInitialTargetUnchanged(t, pool, manager, sealed, false)
	})

	t.Run("requires an executable route for every operation", func(t *testing.T) {
		fixture := createCanonicalDeploymentFixture(t, pool, "initial_operation", "active", 100, 100)
		makeSoleInitialCustomer(t, pool, fixture.OrgID)
		embedding := createAdditionalDeploymentFixture(
			t, pool, fixture, "initial-operation-embedding", "active", 100, 100,
		)
		embedding = addDeploymentOperation(
			t, pool, embedding, "openai.embeddings.v1", "integration.openai.embeddings.v1",
		)
		sealed := sealInitialDeploymentRoutes(
			t, pool, manager, fixture,
			[]runtimechange.DeploymentRoute{deploymentRoute(fixture), deploymentRoute(embedding)},
			"rcr_initial_operation_gap",
		)
		setDeploymentCircuitUnavailable(t, pool, embedding.DeploymentID)

		_, err := executor.activateInitial(
			ctx, initialDeploymentActivationRequest(fixture, sealed),
		)
		require.ErrorIs(t, err, runtimechange.ErrConflict)
		require.ErrorContains(t, err, embedding.OperationID)
		assertInitialTargetUnchanged(t, pool, manager, sealed, false)
	})

	t.Run("rechecks revisions probes credentials and breaker state", func(t *testing.T) {
		tests := []struct {
			name    string
			prepare func(*testing.T, *pgxpool.Pool, canonicalDeploymentFixture) canonicalDeploymentFixture
			drift   func(*testing.T, *pgxpool.Pool, canonicalDeploymentFixture)
		}{
			{
				name: "operation revision",
				drift: func(t *testing.T, pool *pgxpool.Pool, fixture canonicalDeploymentFixture) {
					_, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET evidence_ref='initial activation drift',revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.DeploymentID, fixture.OperationID)
					require.NoError(t, err)
				},
			},
			{
				name: "latest probe",
				drift: func(t *testing.T, pool *pgxpool.Pool, fixture canonicalDeploymentFixture) {
					insertFailedProbe(t, pool, fixture, "initial-activation-latest-probe")
				},
			},
			{
				name: "credential revision",
				prepare: func(t *testing.T, pool *pgxpool.Pool, fixture canonicalDeploymentFixture) canonicalDeploymentFixture {
					replaceCanonicalCredentialWithNative(t, pool, &fixture)
					return fixture
				},
				drift: func(t *testing.T, pool *pgxpool.Pool, fixture canonicalDeploymentFixture) {
					_, err := pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET status='disabled',state_revision=state_revision+1,updated_at=NOW()
 WHERE id=$1`, fixture.CredentialID)
					require.NoError(t, err)
				},
			},
			{
				name: "all circuits unavailable",
				drift: func(t *testing.T, pool *pgxpool.Pool, fixture canonicalDeploymentFixture) {
					setDeploymentCircuitUnavailable(t, pool, fixture.DeploymentID)
				},
			},
		}
		for _, testCase := range tests {
			t.Run(testCase.name, func(t *testing.T) {
				fixture := createCanonicalDeploymentFixture(
					t, pool, "initial_drift_"+safeReleaseSuffix(testCase.name), "active", 100, 100,
				)
				makeSoleInitialCustomer(t, pool, fixture.OrgID)
				if testCase.prepare != nil {
					fixture = testCase.prepare(t, pool, fixture)
				}
				sealed := sealInitialDeploymentRoutes(
					t, pool, manager, fixture, []runtimechange.DeploymentRoute{deploymentRoute(fixture)},
					"rcr_initial_drift_"+safeReleaseSuffix(testCase.name),
				)
				testCase.drift(t, pool, fixture)
				_, err := executor.activateInitial(
					ctx, initialDeploymentActivationRequest(fixture, sealed),
				)
				require.ErrorIs(t, err, runtimechange.ErrConflict)
				assertInitialTargetUnchanged(t, pool, manager, sealed, false)
			})
		}
	})

	t.Run("event failure rolls back release and slot writes", func(t *testing.T) {
		fixture, sealed := prepareInitialDeploymentRelease(t, pool, manager, "initial_event_failure")
		_, err := pool.Exec(ctx, fmt.Sprintf(`
CREATE FUNCTION reject_initial_activation_event_%d()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.release_id=%d AND NEW.action='promote' THEN
        RAISE EXCEPTION 'injected initial activation event failure';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER reject_initial_activation_event_%d
BEFORE INSERT ON runtime_change_events
FOR EACH ROW EXECUTE FUNCTION reject_initial_activation_event_%d()`,
			sealed.ID, sealed.ID, sealed.ID, sealed.ID))
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), fmt.Sprintf(`
DROP TRIGGER IF EXISTS reject_initial_activation_event_%d ON runtime_change_events;
DROP FUNCTION IF EXISTS reject_initial_activation_event_%d()`, sealed.ID, sealed.ID))
		})

		_, err = executor.activateInitial(
			ctx, initialDeploymentActivationRequest(fixture, sealed),
		)
		require.ErrorIs(t, err, runtimechange.ErrUnavailable)
		require.ErrorContains(t, err, "append deployment lifecycle event")
		assertInitialTargetUnchanged(t, pool, manager, sealed, false)
	})
}

func prepareInitialDeploymentRelease(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	suffix string,
) (canonicalDeploymentFixture, runtimechange.DeploymentRelease) {
	t.Helper()
	fixture := createCanonicalDeploymentFixture(t, pool, suffix, "active", 100, 100)
	makeSoleInitialCustomer(t, pool, fixture.OrgID)
	sealed := sealInitialDeploymentRoutes(
		t, pool, manager, fixture, []runtimechange.DeploymentRoute{deploymentRoute(fixture)},
		"rcr_"+safeReleaseSuffix(suffix),
	)
	return fixture, sealed
}

func sealInitialDeploymentRoutes(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	fixture canonicalDeploymentFixture,
	routes []runtimechange.DeploymentRoute,
	releaseRef string,
) runtimechange.DeploymentRelease {
	t.Helper()
	sealed := authorDeploymentRoutesFixture(
		t, pool, manager, fixture, routes, "initial canonical stable fixture: "+releaseRef,
	)
	require.Equal(t, "validated", sealed.Stage)
	require.Nil(t, sealed.ParentStableReleaseID)
	return sealed
}

func initialDeploymentActivationRequest(
	fixture canonicalDeploymentFixture,
	release runtimechange.DeploymentRelease,
) runtimechange.InitialDeploymentActivationRequest {
	return runtimechange.InitialDeploymentActivationRequest{
		ReleaseRef: release.ReleaseRef, PublicModelID: fixture.PublicModelID,
		ExpectedReleaseVersion: release.Version,
		ActorUserID:            fixture.UserID, CustomerOrgID: fixture.OrgID,
	}
}

func makeSoleInitialCustomer(t *testing.T, pool *pgxpool.Pool, orgID int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
UPDATE orgs
   SET status='active',org_type='customer',parent_id=NULL,updated_at=NOW()
 WHERE id=$1`, orgID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
UPDATE orgs SET org_type='standard',updated_at=NOW() WHERE id=$1`, orgID)
	})
}

func assertInitialTargetUnchanged(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	release runtimechange.DeploymentRelease,
	allowExistingSlot bool,
) {
	t.Helper()
	stored := mustDeploymentRelease(t, manager, release.ReleaseRef)
	require.Equal(t, "validated", stored.Stage)
	require.Equal(t, release.Version, stored.Version)
	require.Nil(t, stored.ActivatedBy)
	require.Nil(t, stored.ActivatedAt)
	var promotionCount int
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT COUNT(*) FROM runtime_change_events
 WHERE release_id=$1 AND action='promote'`, release.ID).Scan(&promotionCount))
	require.Zero(t, promotionCount)
	if !allowExistingSlot {
		var slotCount int
		require.NoError(t, pool.QueryRow(context.Background(), `
SELECT COUNT(*) FROM runtime_change_slots
 WHERE model_name=$1 OR public_model_id=$2`, release.ModelName, release.PublicModelID).Scan(&slotCount))
		require.Zero(t, slotCount)
	}
}
