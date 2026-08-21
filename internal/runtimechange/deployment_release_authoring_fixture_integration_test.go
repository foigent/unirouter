//go:build integration

package runtimechange_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/runtimechange"
)

var deploymentReleaseAuthoringFixtureSequence atomic.Uint64

// authorDeploymentRoutesFixture is the only success fixture for canonical v2
// release creation. Tests supply route membership only; every revision,
// profile, public binding, contract, hash, parent and release reference is
// projected by the production atomic authoring command.
func authorDeploymentRoutesFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	fixture canonicalDeploymentFixture,
	routes []runtimechange.DeploymentRoute,
	reason string,
) runtimechange.DeploymentRelease {
	t.Helper()
	receipt, err := attemptAuthorDeploymentRoutesFixture(t, pool, manager, fixture, routes, reason)
	require.NoError(t, err)
	require.Equal(t, "validated", receipt.Release.Stage)
	require.EqualValues(t, 2, receipt.Release.Version)
	require.NotNil(t, receipt.Release.RouteItemsSealedAt)
	return receipt.Release
}

func attemptAuthorDeploymentRoutesFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	fixture canonicalDeploymentFixture,
	routes []runtimechange.DeploymentRoute,
	reason string,
) (runtimechange.DeploymentReleaseAuthoringReceipt, error) {
	t.Helper()
	selections := make([]runtimechange.DeploymentReleaseSelection, len(routes))
	for index, route := range routes {
		selections[index] = runtimechange.DeploymentReleaseSelection{
			DeploymentID: route.DeploymentID,
			OperationID:  route.ModelOperationID,
		}
	}
	baseline := exactDeploymentReleaseAuthoringFixtureBaseline(t, pool, fixture)
	sequence := deploymentReleaseAuthoringFixtureSequence.Add(1)
	identity := fmt.Sprintf("%s/%d/%d", t.Name(), fixture.PublicModelID, sequence)
	keyDigest := sha256.Sum256([]byte("key/" + identity))
	fingerprintDigest := sha256.Sum256([]byte("fingerprint/" + identity))
	operationDigest := sha256.Sum256([]byte("operation/" + identity))
	return manager.AuthorSealedDeploymentRelease(t.Context(), runtimechange.DeploymentReleaseAuthoringCommand{
		Mutation: runtimechange.DeploymentReleaseAuthoringMutation{
			PublicModelID: fixture.PublicModelID,
			Baseline:      baseline,
			Selections:    selections,
			Reason:        reason,
		},
		CustomerOrgID:      fixture.OrgID,
		ActorUserID:        fixture.UserID,
		ActorOrgID:         fixture.OrgID,
		IdempotencyKeyHash: hex.EncodeToString(keyDigest[:]),
		RequestFingerprint: "hmac-sha256:" + hex.EncodeToString(fingerprintDigest[:]),
		ServerOperationID:  "test-author-" + hex.EncodeToString(operationDigest[:16]),
		Audit: runtimechange.DeploymentReleaseAuthoringAudit{
			RequestID: "test-author-" + hex.EncodeToString(operationDigest[:16]),
			Source:    "integration_test",
		},
	})
}

func applyDeploymentLifecycleFixture(
	t *testing.T,
	manager *runtimechange.Manager,
	fixture canonicalDeploymentFixture,
	mutation runtimechange.DeploymentLifecycleMutation,
) runtimechange.DeploymentLifecycleReceipt {
	t.Helper()
	receipt, err := attemptDeploymentLifecycleFixture(
		t, t.Context(), manager, fixture.PublicModelID, fixture.OrgID,
		fixture.UserID, fixture.OrgID, mutation,
	)
	require.NoError(t, err)
	return receipt
}

func attemptDeploymentLifecycleFixture(
	t *testing.T,
	ctx context.Context,
	manager *runtimechange.Manager,
	publicModelID, customerOrgID, actorUserID, actorOrgID int64,
	mutation runtimechange.DeploymentLifecycleMutation,
) (runtimechange.DeploymentLifecycleReceipt, error) {
	t.Helper()
	sequence := deploymentReleaseAuthoringFixtureSequence.Add(1)
	identity := fmt.Sprintf("%s/lifecycle/%d/%d", t.Name(), publicModelID, sequence)
	keyDigest := sha256.Sum256([]byte("key/" + identity))
	fingerprintDigest := sha256.Sum256([]byte("fingerprint/" + identity))
	operationDigest := sha256.Sum256([]byte("operation/" + identity))
	return manager.ApplyDeploymentLifecycle(ctx, runtimechange.DeploymentLifecycleCommand{
		Mutation:           mutation,
		CustomerOrgID:      customerOrgID,
		ActorUserID:        actorUserID,
		ActorOrgID:         actorOrgID,
		IdempotencyKeyHash: hex.EncodeToString(keyDigest[:]),
		RequestFingerprint: "hmac-sha256:" + hex.EncodeToString(fingerprintDigest[:]),
		ServerOperationID:  "test-lifecycle-" + hex.EncodeToString(operationDigest[:16]),
		Audit: runtimechange.DeploymentLifecycleAudit{
			RequestID: "test-lifecycle-" + hex.EncodeToString(operationDigest[:16]),
			Source:    "integration_test",
		},
	})
}

func attemptInitialActivationFixture(
	t *testing.T,
	ctx context.Context,
	manager *runtimechange.Manager,
	request runtimechange.InitialDeploymentActivationRequest,
) (runtimechange.DeploymentRelease, error) {
	t.Helper()
	actorOrgID := request.ActorOrgID
	if actorOrgID == 0 {
		actorOrgID = request.CustomerOrgID
	}
	_, err := attemptDeploymentLifecycleFixture(
		t, ctx, manager, request.PublicModelID, request.CustomerOrgID,
		request.ActorUserID, actorOrgID,
		runtimechange.DeploymentLifecycleMutation{
			Action:                 runtimechange.DeploymentLifecycleActivateInitial,
			ReleaseRef:             request.ReleaseRef,
			ExpectedReleaseVersion: request.ExpectedReleaseVersion,
			Reason:                 "initial activation integration fixture",
		},
	)
	if err != nil {
		return runtimechange.DeploymentRelease{}, err
	}
	return manager.GetDeploymentRelease(ctx, request.ReleaseRef)
}

func attemptCanonicalLifecycleFixture(
	t *testing.T,
	ctx context.Context,
	manager *runtimechange.Manager,
	request runtimechange.DeploymentLifecycleRequest,
	action runtimechange.DeploymentLifecycleAction,
	canaryBasisPoints int,
) (runtimechange.DeploymentRelease, error) {
	t.Helper()
	actorOrgID := request.ActorOrgID
	if actorOrgID == 0 {
		actorOrgID = request.CustomerOrgID
	}
	_, err := attemptDeploymentLifecycleFixture(
		t, ctx, manager, request.PublicModelID, request.CustomerOrgID,
		request.ActorUserID, actorOrgID,
		runtimechange.DeploymentLifecycleMutation{
			Action:                 action,
			ReleaseRef:             request.ReleaseRef,
			ExpectedReleaseVersion: request.ExpectedReleaseVersion,
			ExpectedSlotVersion:    request.ExpectedSlotVersion,
			CanaryBasisPoints:      canaryBasisPoints,
			Reason:                 "canonical lifecycle integration fixture",
		},
	)
	if err != nil {
		return runtimechange.DeploymentRelease{}, err
	}
	return manager.GetDeploymentRelease(ctx, request.ReleaseRef)
}

func attemptCanonicalRollbackFixture(
	t *testing.T,
	ctx context.Context,
	manager *runtimechange.Manager,
	request runtimechange.DeploymentLifecycleRequest,
) (runtimechange.DeploymentRelease, error) {
	t.Helper()
	target, err := manager.GetDeploymentRelease(ctx, request.ReleaseRef)
	if err != nil {
		return runtimechange.DeploymentRelease{}, err
	}
	action := runtimechange.DeploymentLifecycleCancelCanary
	if target.Stage == "active" {
		action = runtimechange.DeploymentLifecycleRollbackStable
	}
	return attemptCanonicalLifecycleFixture(t, ctx, manager, request, action, 0)
}

type canonicalLifecycleFixtureExecutor struct {
	t       *testing.T
	manager *runtimechange.Manager
}

func (executor canonicalLifecycleFixtureExecutor) activateInitial(
	ctx context.Context,
	request runtimechange.InitialDeploymentActivationRequest,
) (runtimechange.DeploymentRelease, error) {
	return attemptInitialActivationFixture(executor.t, ctx, executor.manager, request)
}

func (executor canonicalLifecycleFixtureExecutor) startCanary(
	ctx context.Context,
	request runtimechange.DeploymentLifecycleRequest,
	basisPoints int,
) (runtimechange.DeploymentRelease, error) {
	return attemptCanonicalLifecycleFixture(
		executor.t, ctx, executor.manager, request,
		runtimechange.DeploymentLifecycleStartCanary, basisPoints,
	)
}

func (executor canonicalLifecycleFixtureExecutor) promote(
	ctx context.Context,
	request runtimechange.DeploymentLifecycleRequest,
) (runtimechange.DeploymentRelease, error) {
	return attemptCanonicalLifecycleFixture(
		executor.t, ctx, executor.manager, request,
		runtimechange.DeploymentLifecyclePromote, 0,
	)
}

func (executor canonicalLifecycleFixtureExecutor) rollback(
	ctx context.Context,
	request runtimechange.DeploymentLifecycleRequest,
) (runtimechange.DeploymentRelease, error) {
	return attemptCanonicalRollbackFixture(executor.t, ctx, executor.manager, request)
}

func activateInitialDeploymentReleaseFixture(
	t *testing.T,
	manager *runtimechange.Manager,
	fixture canonicalDeploymentFixture,
	release runtimechange.DeploymentRelease,
) runtimechange.DeploymentRelease {
	t.Helper()
	applyDeploymentLifecycleFixture(t, manager, fixture, runtimechange.DeploymentLifecycleMutation{
		Action:                 runtimechange.DeploymentLifecycleActivateInitial,
		ReleaseRef:             release.ReleaseRef,
		ExpectedReleaseVersion: release.Version,
		Reason:                 "activate initial canonical fixture",
	})
	return mustDeploymentRelease(t, manager, release.ReleaseRef)
}

func exactDeploymentReleaseAuthoringFixtureBaseline(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture canonicalDeploymentFixture,
) runtimechange.DeploymentReleaseBaseline {
	t.Helper()
	rows, err := pool.Query(t.Context(), `
SELECT slot.slot_version,stable.release_ref,stable.version
  FROM runtime_change_slots slot
  LEFT JOIN runtime_change_releases stable ON stable.id=slot.stable_release_id
 WHERE slot.model_name=$1 OR slot.public_model_id=$2
 ORDER BY slot.model_name`, fixture.ModelName, fixture.PublicModelID)
	require.NoError(t, err)
	defer rows.Close()
	type stableBaseline struct {
		slotVersion, releaseVersion int64
		releaseRef                  string
	}
	values := make([]stableBaseline, 0, 2)
	for rows.Next() {
		var value stableBaseline
		require.NoError(t, rows.Scan(&value.slotVersion, &value.releaseRef, &value.releaseVersion))
		values = append(values, value)
	}
	require.NoError(t, rows.Err())
	if len(values) == 0 {
		return runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial}
	}
	require.Len(t, values, 1, "fixture baseline must resolve one exact slot identity")
	return runtimechange.DeploymentReleaseBaseline{
		Kind:                         runtimechange.DeploymentReleaseBaselineStable,
		ExpectedSlotVersion:          values[0].slotVersion,
		StableReleaseRef:             values[0].releaseRef,
		ExpectedStableReleaseVersion: values[0].releaseVersion,
	}
}
