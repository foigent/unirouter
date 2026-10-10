package admin

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/runtimechange"
)

func TestCanonicalDeploymentLifecycleFingerprintNormalizesEquivalentRequest(t *testing.T) {
	t.Parallel()

	left, err := canonicalDeploymentLifecycleMutation(
		runtimechange.DeploymentLifecycleRollbackStable,
		" RCR_ROLLBACK-ONE ",
		canonicalDeploymentLifecycleRequest{
			ExpectedReleaseVersion: 4, ExpectedSlotVersion: 5, Reason: " incident ",
		},
	)
	require.NoError(t, err)
	right, err := canonicalDeploymentLifecycleMutation(
		runtimechange.DeploymentLifecycleRollbackStable,
		"rcr_rollback-one",
		canonicalDeploymentLifecycleRequest{
			ExpectedReleaseVersion: 4, ExpectedSlotVersion: 5, Reason: "incident",
		},
	)
	require.NoError(t, err)

	leftFingerprint, err := canonicalDeploymentLifecycleFingerprint(left, "receipt-secret")
	require.NoError(t, err)
	rightFingerprint, err := canonicalDeploymentLifecycleFingerprint(right, "receipt-secret")
	require.NoError(t, err)
	require.Equal(t, leftFingerprint, rightFingerprint)
	require.True(t, strings.HasPrefix(leftFingerprint, "hmac-sha256:"))
	require.Len(t, leftFingerprint, 76)

	right.ExpectedSlotVersion++
	different, err := canonicalDeploymentLifecycleFingerprint(right, "receipt-secret")
	require.NoError(t, err)
	require.NotEqual(t, leftFingerprint, different)
}

func TestCanonicalDeploymentLifecycleActionsSeparateRoleLicenseAndSafety(t *testing.T) {
	t.Parallel()

	canary, slot := canonicalLifecycleCanaryFixture()
	viewer := canonicalDeploymentLifecycleActions(canary, slot, nil, false)
	licensedWriter := canonicalDeploymentLifecycleActions(canary, slot, nil, true)
	unlicensedWriter := canonicalDeploymentLifecycleActions(canary, slot, nil, true)

	promoteViewer := lifecycleActionDescriptor(t, viewer, runtimechange.DeploymentLifecyclePromote)
	require.True(t, promoteViewer.StructurallyAvailable)
	require.False(t, promoteViewer.Authorized)
	require.True(t, promoteViewer.Licensed)
	require.False(t, promoteViewer.Available)
	require.Equal(t, "organization_write_role_required", promoteViewer.ReasonCode)
	require.Empty(t, promoteViewer.LicenseFeature)

	promoteLicensed := lifecycleActionDescriptor(t, licensedWriter, runtimechange.DeploymentLifecyclePromote)
	require.True(t, promoteLicensed.Available)
	require.Equal(t, "ready_for_transactional_preflight", promoteLicensed.ReasonCode)

	promoteUnlicensed := lifecycleActionDescriptor(t, unlicensedWriter, runtimechange.DeploymentLifecyclePromote)
	require.True(t, promoteUnlicensed.Available)
	require.Equal(t, "ready_for_transactional_preflight", promoteUnlicensed.ReasonCode)

	cancel := lifecycleActionDescriptor(t, unlicensedWriter, runtimechange.DeploymentLifecycleCancelCanary)
	require.True(t, cancel.Available, "safe contraction must not depend on Gateway License")
	require.True(t, cancel.Licensed)
	require.Empty(t, cancel.LicenseFeature)
	require.Equal(t, "contraction", cancel.SafetyDirection)
	require.Equal(t, "/api/v1/model-deployment-releases/{releaseRef}/cancel-canary", cancel.Path)
}

func TestCanonicalDeploymentLifecycleActionsAreStageSpecific(t *testing.T) {
	t.Parallel()

	canary, slot := canonicalLifecycleCanaryFixture()
	descriptors := canonicalDeploymentLifecycleActions(canary, slot, nil, true)
	require.True(t, lifecycleActionDescriptor(t, descriptors, runtimechange.DeploymentLifecyclePromote).StructurallyAvailable)
	require.True(t, lifecycleActionDescriptor(t, descriptors, runtimechange.DeploymentLifecycleCancelCanary).StructurallyAvailable)
	require.False(t, lifecycleActionDescriptor(t, descriptors, runtimechange.DeploymentLifecycleRollbackStable).StructurallyAvailable)

	active := canary
	active.Stage = "active"
	active.Version++
	active.CanaryBasisPoints = 0
	slot.StableReleaseID = lifecycleInt64Pointer(active.ID)
	slot.CanaryReleaseID = nil
	slot.CanaryBasisPoints = 0
	descriptors = canonicalDeploymentLifecycleActions(active, slot, nil, true)
	require.True(t, lifecycleActionDescriptor(t, descriptors, runtimechange.DeploymentLifecycleRollbackStable).StructurallyAvailable)
	require.False(t, lifecycleActionDescriptor(t, descriptors, runtimechange.DeploymentLifecycleCancelCanary).StructurallyAvailable)

	initial := canary
	initial.Stage = "validated"
	initial.ParentStableReleaseID = nil
	initial.CanaryBasisPoints = 0
	descriptors = canonicalDeploymentLifecycleActions(initial, runtimechange.DeploymentLifecycleSlotState{}, nil, true)
	require.True(t, lifecycleActionDescriptor(t, descriptors, runtimechange.DeploymentLifecycleActivateInitial).StructurallyAvailable)
	require.Equal(t, []string{"expected_release_version"}, lifecycleActionDescriptor(
		t, descriptors, runtimechange.DeploymentLifecycleActivateInitial,
	).RequiredFields)
}

func TestCanonicalDeploymentLifecycleActionsUseOnlyLegacyBridgeForV1Parent(t *testing.T) {
	t.Parallel()

	sealedAt := time.Date(2026, time.August, 20, 2, 3, 4, 0, time.UTC)
	parentID := int64(61)
	targetID := int64(62)
	publicModelID := int64(7)
	release := runtimechange.DeploymentRelease{
		ID: targetID, ReleaseRef: "rcr_legacy-bridge", PublicModelID: publicModelID,
		ModelName: "legacy-model", CandidateVersion: runtimechange.DeploymentCandidateVersion,
		RouteItemsSealedAt: &sealedAt, Stage: "validated", Version: 2,
		ParentStableReleaseID: &parentID,
	}
	parent := &runtimechange.DeploymentReleaseSummary{
		ID: parentID, ReleaseRef: "rcr_legacy-parent", PublicModelID: publicModelID,
		ModelName: "legacy-model", CandidateVersion: runtimechange.CandidateVersion,
		RouteItemsSealedAt: &sealedAt, RouteItemsCount: 1, Stage: "active", Version: 4,
	}
	slot := runtimechange.DeploymentLifecycleSlotState{
		Exists: true, PublicModelID: publicModelID, ModelName: "legacy-model",
		StableReleaseID: &parentID, Version: 9,
	}
	descriptors := canonicalDeploymentLifecycleActions(release, slot, parent, true)
	require.Len(t, descriptors, 2)
	require.Equal(t, runtimechange.DeploymentLifecycleCutoverLegacy, descriptors[0].Action)
	require.True(t, descriptors[0].Available)
	require.Equal(t, "/api/v1/model-deployment-releases/{releaseRef}/cutover-legacy", descriptors[0].Path)
	require.Equal(t, []string{"expected_release_version", "expected_slot_version", "reason"}, descriptors[0].RequiredFields)
	require.Equal(t, runtimechange.DeploymentLifecycleRollbackLegacy, descriptors[1].Action)
	require.False(t, descriptors[1].StructurallyAvailable)
	for _, descriptor := range descriptors {
		require.NotEqual(t, runtimechange.DeploymentLifecycleStartCanary, descriptor.Action)
		require.NotEqual(t, runtimechange.DeploymentLifecycleRollbackStable, descriptor.Action)
	}

	release.Stage = "active"
	release.Version = 3
	parent.Stage = "rolled_back"
	slot.AssignedPublicModelID = &publicModelID
	slot.StableReleaseID = &targetID
	descriptors = canonicalDeploymentLifecycleActions(release, slot, parent, true)
	rollback := lifecycleActionDescriptor(t, descriptors, runtimechange.DeploymentLifecycleRollbackLegacy)
	require.True(t, rollback.Available, "legacy rollback is a no-License safety contraction")
	require.True(t, rollback.Licensed)
	require.Empty(t, rollback.LicenseFeature)
	require.Equal(t, "contraction", rollback.SafetyDirection)
	require.False(t, lifecycleActionDescriptor(t, descriptors, runtimechange.DeploymentLifecycleCutoverLegacy).StructurallyAvailable)
}

func canonicalLifecycleCanaryFixture() (runtimechange.DeploymentRelease, runtimechange.DeploymentLifecycleSlotState) {
	sealedAt := time.Date(2026, time.August, 20, 1, 2, 3, 0, time.UTC)
	parentID := int64(41)
	targetID := int64(42)
	release := runtimechange.DeploymentRelease{
		ID: targetID, ReleaseRef: "rcr_canary-fixture", PublicModelID: 7,
		ModelName: "canonical-model", CandidateVersion: runtimechange.DeploymentCandidateVersion,
		RouteItemsSealedAt: &sealedAt, Stage: "canary", Version: 3,
		ParentStableReleaseID: &parentID, CanaryBasisPoints: 500,
	}
	slot := runtimechange.DeploymentLifecycleSlotState{
		Exists: true, PublicModelID: 7, ModelName: "canonical-model",
		StableReleaseID: &parentID, CanaryReleaseID: &targetID,
		CanaryBasisPoints: 500, Version: 4,
	}
	return release, slot
}

func lifecycleActionDescriptor(
	t *testing.T,
	descriptors []canonicalDeploymentLifecycleActionDescriptor,
	action runtimechange.DeploymentLifecycleAction,
) canonicalDeploymentLifecycleActionDescriptor {
	t.Helper()
	for _, descriptor := range descriptors {
		if descriptor.Action == action {
			return descriptor
		}
	}
	t.Fatalf("missing lifecycle descriptor %s", action)
	return canonicalDeploymentLifecycleActionDescriptor{}
}

func lifecycleInt64Pointer(value int64) *int64 { return &value }
