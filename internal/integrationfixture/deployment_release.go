//go:build integration

// Package integrationfixture keeps cross-package integration setup on public
// production commands without adding helpers to a production domain package.
package integrationfixture

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/runtimechange"
)

var atomicReleaseSequence atomic.Uint64

type AtomicRelease struct {
	PublicModelID int64
	CustomerOrgID int64
	ActorUserID   int64
	ActorOrgID    int64
	Selections    []runtimechange.DeploymentReleaseSelection
	Reason        string
}

func AuthorSealedDeploymentRelease(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	fixture AtomicRelease,
) runtimechange.DeploymentRelease {
	t.Helper()
	receipt, err := AttemptAuthorSealedDeploymentRelease(t, pool, manager, fixture)
	require.NoError(t, err)
	require.Equal(t, "validated", receipt.Release.Stage)
	require.EqualValues(t, 2, receipt.Release.Version)
	require.NotNil(t, receipt.Release.RouteItemsSealedAt)
	return receipt.Release
}

func AttemptAuthorSealedDeploymentRelease(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	fixture AtomicRelease,
) (runtimechange.DeploymentReleaseAuthoringReceipt, error) {
	t.Helper()
	baseline := exactBaseline(t, pool, fixture.PublicModelID, fixture.CustomerOrgID)
	identity, keyHash, fingerprint, operationID := identity(t, fixture.PublicModelID)
	receipt, err := manager.AuthorSealedDeploymentRelease(t.Context(), runtimechange.DeploymentReleaseAuthoringCommand{
		Mutation: runtimechange.DeploymentReleaseAuthoringMutation{
			PublicModelID: fixture.PublicModelID,
			Baseline:      baseline,
			Selections:    append([]runtimechange.DeploymentReleaseSelection(nil), fixture.Selections...),
			Reason:        fixture.Reason,
		},
		CustomerOrgID:      fixture.CustomerOrgID,
		ActorUserID:        fixture.ActorUserID,
		ActorOrgID:         fixture.ActorOrgID,
		IdempotencyKeyHash: keyHash,
		RequestFingerprint: fingerprint,
		ServerOperationID:  operationID,
		Audit: runtimechange.DeploymentReleaseAuthoringAudit{
			RequestID: identity,
			Source:    "integration_test",
		},
	})
	return receipt, err
}

func ApplyDeploymentLifecycle(
	t *testing.T,
	manager *runtimechange.Manager,
	fixture AtomicRelease,
	mutation runtimechange.DeploymentLifecycleMutation,
) runtimechange.DeploymentLifecycleReceipt {
	t.Helper()
	requestID, keyHash, fingerprint, operationID := identity(t, fixture.PublicModelID)
	receipt, err := manager.ApplyDeploymentLifecycle(t.Context(), runtimechange.DeploymentLifecycleCommand{
		Mutation:           mutation,
		CustomerOrgID:      fixture.CustomerOrgID,
		ActorUserID:        fixture.ActorUserID,
		ActorOrgID:         fixture.ActorOrgID,
		IdempotencyKeyHash: keyHash,
		RequestFingerprint: fingerprint,
		ServerOperationID:  operationID,
		Audit: runtimechange.DeploymentLifecycleAudit{
			RequestID: requestID,
			Source:    "integration_test",
		},
	})
	require.NoError(t, err)
	return receipt
}

func ActivateInitialDeploymentRelease(
	t *testing.T,
	manager *runtimechange.Manager,
	fixture AtomicRelease,
	release runtimechange.DeploymentRelease,
) runtimechange.DeploymentRelease {
	t.Helper()
	ApplyDeploymentLifecycle(t, manager, fixture, runtimechange.DeploymentLifecycleMutation{
		Action:                 runtimechange.DeploymentLifecycleActivateInitial,
		ReleaseRef:             release.ReleaseRef,
		ExpectedReleaseVersion: release.Version,
		Reason:                 "activate initial integration fixture",
	})
	loaded, err := manager.GetDeploymentRelease(t.Context(), release.ReleaseRef)
	require.NoError(t, err)
	return loaded
}

func identity(t *testing.T, publicModelID int64) (requestID, keyHash, fingerprint, operationID string) {
	t.Helper()
	sequence := atomicReleaseSequence.Add(1)
	identity := fmt.Sprintf("fixture/%s/%d/%d", t.Name(), publicModelID, sequence)
	requestDigest := sha256.Sum256([]byte("request/" + identity))
	keyDigest := sha256.Sum256([]byte("key/" + identity))
	fingerprintDigest := sha256.Sum256([]byte("fingerprint/" + identity))
	operationDigest := sha256.Sum256([]byte("operation/" + identity))
	return "fixture-" + hex.EncodeToString(requestDigest[:16]),
		hex.EncodeToString(keyDigest[:]),
		"hmac-sha256:" + hex.EncodeToString(fingerprintDigest[:]),
		"fixture-" + hex.EncodeToString(operationDigest[:16])
}

func exactBaseline(
	t *testing.T,
	pool *pgxpool.Pool,
	publicModelID, customerOrgID int64,
) runtimechange.DeploymentReleaseBaseline {
	t.Helper()
	rows, err := pool.Query(t.Context(), `
SELECT slot.slot_version,slot.public_model_id,stable.release_ref,stable.version,
       stable.candidate_version
  FROM model_registry model
  LEFT JOIN runtime_change_slots slot
    ON slot.model_name=model.model_name OR slot.public_model_id=model.id
  LEFT JOIN runtime_change_releases stable ON stable.id=slot.stable_release_id
 WHERE model.id=$1 AND model.org_id=$2`, publicModelID, customerOrgID)
	require.NoError(t, err)
	defer rows.Close()
	type stableBaseline struct {
		slotVersion, releaseVersion *int64
		slotPublicModelID           *int64
		releaseRef                  *string
		candidateVersion            *string
	}
	values := make([]stableBaseline, 0, 2)
	for rows.Next() {
		var value stableBaseline
		require.NoError(t, rows.Scan(
			&value.slotVersion, &value.slotPublicModelID, &value.releaseRef,
			&value.releaseVersion, &value.candidateVersion,
		))
		values = append(values, value)
	}
	require.NoError(t, rows.Err())
	require.Len(t, values, 1, "fixture public model must resolve exactly once")
	if values[0].slotVersion == nil {
		return runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial}
	}
	require.NotNil(t, values[0].releaseRef, "fixture stable slot must name a stable release")
	require.NotNil(t, values[0].releaseVersion, "fixture stable slot must name a stable release version")
	kind := runtimechange.DeploymentReleaseBaselineStable
	if values[0].slotPublicModelID == nil {
		require.NotNil(t, values[0].candidateVersion)
		require.Equal(t, runtimechange.CandidateVersion, *values[0].candidateVersion)
		kind = runtimechange.DeploymentReleaseBaselineLegacyCutover
	}
	return runtimechange.DeploymentReleaseBaseline{
		Kind:                         kind,
		ExpectedSlotVersion:          *values[0].slotVersion,
		StableReleaseRef:             *values[0].releaseRef,
		ExpectedStableReleaseVersion: *values[0].releaseVersion,
	}
}
