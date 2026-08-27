//go:build integration

package runtimechange_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestSealedModelExecutionContractReleasePinningIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	manager, err := runtimechange.NewManager(
		pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	planner, err := modelrouting.NewPlanner(pool, routingpolicy.Default())
	require.NoError(t, err)
	// This test changes a sealed profile version, not the provider product.
	// Use a matching Kimi route so exact product-identity checks remain active.
	fixture := createCanonicalDeploymentFixtureWithProduct(
		t, pool, "m82-execution-contract", "active", 100, 100,
		"kimi", "https://api.moonshot.cn/v1", "kimi-k3",
	)
	makeSoleInitialCustomer(t, pool, fixture.OrgID)

	_, err = pool.Exec(ctx, `
UPDATE model_registry
   SET execution_profile_id='kimi.k3.api',
       execution_profile_version='1.1.0',
       context_length=262144,
       output_limit=32768
 WHERE id=$1`, fixture.PublicModelID)
	require.NoError(t, err)

	initial := authorDeploymentRoutesFixture(
		t, pool, manager, fixture, []runtimechange.DeploymentRoute{deploymentRoute(fixture)},
		"seal the first effective model execution contract",
	)
	assertSealedExecutionContract(t, initial, "kimi.k3.api", "1.1.0", 262144, 32768)
	reverseLaterBindings := func() {
		// Keep the historical M82 rollback fixture at its supported predecessor;
		// the newer exact-public and Plan fences are independently guarded.
		for _, name := range []string{
			"000089_deepseek_chat_sse_usage_contract_v2.down.sql",
			"000088_model_capability_execution_qualification.down.sql",
			"000087_anthropic_messages_tools_public_binding.down.sql",
			"000086_exact_public_execution_bindings.down.sql",
			"000085_model_execution_plan_freeze_fence.down.sql",
		} {
			body, readErr := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), name))
			require.NoError(t, readErr)
			_, downErr := pool.Exec(ctx, string(body))
			require.NoError(t, downErr, name)
		}
	}
	currencyDraftDown, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000084_official_rate_source_currency_drafts.down.sql",
	))
	require.NoError(t, err)
	unitMeterDown, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000083_versioned_unit_meter_billing.down.sql",
	))
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000082_sealed_model_execution_contracts.down.sql",
	))
	require.NoError(t, err)
	reverseLaterBindings()
	_, err = pool.Exec(ctx, string(currencyDraftDown))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(unitMeterDown))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err, "an unchanged M81 projection is a supported downgrade pre-state")
	var migrationPresent, pinColumnsPresent bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  EXISTS(SELECT 1 FROM schema_migrations
          WHERE version='000082_sealed_model_execution_contracts'),
  EXISTS(SELECT 1 FROM information_schema.columns
          WHERE table_schema='public' AND table_name='runtime_change_releases'
            AND column_name='execution_profile_id')`).Scan(&migrationPresent, &pinColumnsPresent))
	require.False(t, migrationPresent)
	require.False(t, pinColumnsPresent)
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	backfilled, err := manager.GetDeploymentRelease(ctx, initial.ReleaseRef)
	require.NoError(t, err)
	assertSealedExecutionContract(t, backfilled, "kimi.k3.api", "1.1.0", 262144, 32768)

	activated := applyDeploymentLifecycleFixture(t, manager, fixture, runtimechange.DeploymentLifecycleMutation{
		Action:                 runtimechange.DeploymentLifecycleActivateInitial,
		ReleaseRef:             initial.ReleaseRef,
		ExpectedReleaseVersion: initial.Version,
		Reason:                 "activate the first sealed execution contract",
	})

	request := modelrouting.Request{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.OrgID,
		PublicModel: fixture.ModelName, ModelOperationID: fixture.OperationID,
		SelectionKey: "m82-stable-before-profile-change",
	}
	plan, err := planner.Plan(ctx, request)
	require.NoError(t, err, "sealed-profile route exclusions: %+v", plan.Exclusions)
	assertPlannedExecutionContract(t, plan, initial.ID, "kimi.k3.api", "1.1.0", 262144, 32768)

	_, err = pool.Exec(ctx, `
UPDATE model_registry
   SET execution_profile_id='kimi.k3.api',
       execution_profile_version='1.2.0',
       context_length=1000000,
       output_limit=32768
 WHERE id=$1`, fixture.PublicModelID)
	require.NoError(t, err)

	request.SelectionKey = "m82-stable-after-registry-change"
	plan, err = planner.Plan(ctx, request)
	require.NoError(t, err)
	assertPlannedExecutionContract(t, plan, initial.ID, "kimi.k3.api", "1.1.0", 262144, 32768)

	child := authorDeploymentRoutesFixture(
		t, pool, manager, fixture, []runtimechange.DeploymentRoute{deploymentRoute(fixture)},
		"author a new release for the reviewed profile change",
	)
	assertSealedExecutionContract(t, child, "kimi.k3.api", "1.2.0", 1000000, 32768)

	request.SelectionKey = "m82-stable-before-child-promotion"
	plan, err = planner.Plan(ctx, request)
	require.NoError(t, err)
	assertPlannedExecutionContract(t, plan, initial.ID, "kimi.k3.api", "1.1.0", 262144, 32768)

	started := applyDeploymentLifecycleFixture(t, manager, fixture, runtimechange.DeploymentLifecycleMutation{
		Action:                 runtimechange.DeploymentLifecycleStartCanary,
		ReleaseRef:             child.ReleaseRef,
		ExpectedReleaseVersion: child.Version,
		ExpectedSlotVersion:    activated.Slot.AfterVersion,
		CanaryBasisPoints:      1000,
		Reason:                 "stage the reviewed profile change",
	})
	canary := mustDeploymentRelease(t, manager, child.ReleaseRef)
	promoted := applyDeploymentLifecycleFixture(t, manager, fixture, runtimechange.DeploymentLifecycleMutation{
		Action:                 runtimechange.DeploymentLifecyclePromote,
		ReleaseRef:             canary.ReleaseRef,
		ExpectedReleaseVersion: canary.Version,
		ExpectedSlotVersion:    started.Slot.AfterVersion,
		Reason:                 "promote the reviewed profile change",
	})
	require.Equal(t, child.ID, *promoted.Slot.AfterStableReleaseID)

	request.SelectionKey = "m82-stable-after-child-promotion"
	plan, err = planner.Plan(ctx, request)
	require.NoError(t, err)
	assertPlannedExecutionContract(t, plan, child.ID, "kimi.k3.api", "1.2.0", 1000000, 32768)

	_, err = pool.Exec(ctx, `
UPDATE runtime_change_releases
   SET execution_profile_id='kimi.k2.6.api'
 WHERE id=$1`, child.ID)
	require.ErrorContains(t, err, "sealed model execution contract is immutable")

	reverseLaterBindings()
	_, err = pool.Exec(ctx, string(currencyDraftDown))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(unitMeterDown))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "durable release pins would be lost")

	var ledgerPresent, columnsPresent bool
	var retainedProfileID, retainedProfileVersion string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  EXISTS(SELECT 1 FROM schema_migrations
          WHERE version='000082_sealed_model_execution_contracts'),
  (SELECT COUNT(*)=4 FROM information_schema.columns
    WHERE table_schema='public' AND table_name='runtime_change_releases'
      AND column_name IN (
        'execution_profile_id','execution_profile_version',
        'public_context_limit','public_output_limit')),
  execution_profile_id,execution_profile_version
  FROM runtime_change_releases
 WHERE id=$1`, initial.ID).Scan(
		&ledgerPresent, &columnsPresent, &retainedProfileID, &retainedProfileVersion,
	))
	require.True(t, ledgerPresent)
	require.True(t, columnsPresent)
	require.Equal(t, "kimi.k3.api", retainedProfileID)
	require.Equal(t, "1.1.0", retainedProfileVersion)
}

func assertSealedExecutionContract(
	t *testing.T,
	release runtimechange.DeploymentRelease,
	profileID, profileVersion string,
	contextLimit, outputLimit int,
) {
	t.Helper()
	require.Equal(t, profileID, release.ExecutionProfileID)
	require.Equal(t, profileVersion, release.ExecutionProfileVersion)
	require.Equal(t, contextLimit, release.PublicContextLimit)
	require.Equal(t, outputLimit, release.PublicOutputLimit)
}

func assertPlannedExecutionContract(
	t *testing.T,
	plan modelrouting.Plan,
	releaseID int64,
	profileID, profileVersion string,
	contextLimit, outputLimit int,
) {
	t.Helper()
	require.Equal(t, releaseID, plan.RuntimeReleaseID)
	require.Equal(t, profileID, plan.ExecutionProfileID)
	require.Equal(t, profileVersion, plan.ExecutionProfileVersion)
	require.Equal(t, contextLimit, plan.PublicContextLimit)
	require.Equal(t, outputLimit, plan.PublicOutputLimit)
}
