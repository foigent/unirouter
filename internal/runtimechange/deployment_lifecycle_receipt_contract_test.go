package runtimechange

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeDeploymentLifecycleMutationKeepsActionsStageSpecific(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutation DeploymentLifecycleMutation
	}{
		{
			name: "initial activation proves slot absence rather than a version",
			mutation: DeploymentLifecycleMutation{
				Action: DeploymentLifecycleActivateInitial, ReleaseRef: " RCR_RELEASE-INITIAL ",
				ExpectedReleaseVersion: 2,
			},
		},
		{
			name: "start canary has a bounded traffic share",
			mutation: DeploymentLifecycleMutation{
				Action: DeploymentLifecycleStartCanary, ReleaseRef: "rcr_release-canary",
				ExpectedReleaseVersion: 2, ExpectedSlotVersion: 3, CanaryBasisPoints: 500,
			},
		},
		{
			name: "promote is exact",
			mutation: DeploymentLifecycleMutation{
				Action: DeploymentLifecyclePromote, ReleaseRef: "rcr_release-promote",
				ExpectedReleaseVersion: 3, ExpectedSlotVersion: 4,
			},
		},
		{
			name: "cancel canary is not stable rollback",
			mutation: DeploymentLifecycleMutation{
				Action: DeploymentLifecycleCancelCanary, ReleaseRef: "rcr_release-cancel",
				ExpectedReleaseVersion: 3, ExpectedSlotVersion: 4,
			},
		},
		{
			name: "rollback stable is not canary cancellation",
			mutation: DeploymentLifecycleMutation{
				Action: DeploymentLifecycleRollbackStable, ReleaseRef: "rcr_release-rollback",
				ExpectedReleaseVersion: 4, ExpectedSlotVersion: 5, Reason: "  incident rollback  ",
			},
		},
		{
			name: "legacy cutover is exact",
			mutation: DeploymentLifecycleMutation{
				Action: DeploymentLifecycleCutoverLegacy, ReleaseRef: "rcr_release-legacy-cutover",
				ExpectedReleaseVersion: 2, ExpectedSlotVersion: 7, Reason: " verified cutover ",
			},
		},
		{
			name: "legacy rollback is exact",
			mutation: DeploymentLifecycleMutation{
				Action: DeploymentLifecycleRollbackLegacy, ReleaseRef: "rcr_release-legacy-rollback",
				ExpectedReleaseVersion: 3, ExpectedSlotVersion: 8, Reason: " safety rollback ",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := NormalizeDeploymentLifecycleMutation(test.mutation)
			require.NoError(t, err)
			require.Equal(t, strings.ToLower(strings.TrimSpace(test.mutation.ReleaseRef)), normalized.ReleaseRef)
			require.Equal(t, strings.TrimSpace(test.mutation.Reason), normalized.Reason)
		})
	}
}

func TestNormalizeDeploymentLifecycleMutationRejectsAmbiguousOrInapplicableFields(t *testing.T) {
	t.Parallel()

	valid := DeploymentLifecycleMutation{
		Action: DeploymentLifecyclePromote, ReleaseRef: "rcr_release-exact",
		ExpectedReleaseVersion: 2, ExpectedSlotVersion: 3,
	}
	tests := []struct {
		name   string
		mutate func(*DeploymentLifecycleMutation)
	}{
		{name: "unknown action", mutate: func(item *DeploymentLifecycleMutation) { item.Action = "rollback" }},
		{name: "missing release version", mutate: func(item *DeploymentLifecycleMutation) { item.ExpectedReleaseVersion = 0 }},
		{name: "missing slot version", mutate: func(item *DeploymentLifecycleMutation) { item.ExpectedSlotVersion = 0 }},
		{name: "canary share on promote", mutate: func(item *DeploymentLifecycleMutation) { item.CanaryBasisPoints = 1 }},
		{name: "multiline reason", mutate: func(item *DeploymentLifecycleMutation) { item.Reason = "first\nsecond" }},
		{name: "oversized reason", mutate: func(item *DeploymentLifecycleMutation) { item.Reason = strings.Repeat("x", 1001) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutation := valid
			test.mutate(&mutation)
			_, err := NormalizeDeploymentLifecycleMutation(mutation)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}

	initial := valid
	initial.Action = DeploymentLifecycleActivateInitial
	initial.ExpectedSlotVersion = 1
	_, err := NormalizeDeploymentLifecycleMutation(initial)
	require.ErrorIs(t, err, ErrInvalid)

	canary := valid
	canary.Action = DeploymentLifecycleStartCanary
	canary.CanaryBasisPoints = 5001
	_, err = NormalizeDeploymentLifecycleMutation(canary)
	require.ErrorIs(t, err, ErrInvalid)

	legacy := valid
	legacy.Action = DeploymentLifecycleCutoverLegacy
	legacy.Reason = "   "
	_, err = NormalizeDeploymentLifecycleMutation(legacy)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestNormalizeDeploymentLifecycleCommandRequiresHashedIdempotencyIdentity(t *testing.T) {
	t.Parallel()

	valid := DeploymentLifecycleCommand{
		Mutation: DeploymentLifecycleMutation{
			Action: DeploymentLifecycleCancelCanary, ReleaseRef: "rcr_release-cancel",
			ExpectedReleaseVersion: 2, ExpectedSlotVersion: 3,
		},
		CustomerOrgID: 1, ActorUserID: 2, ActorOrgID: 1,
		IdempotencyKeyHash: strings.Repeat("a", 64),
		RequestFingerprint: "hmac-sha256:" + strings.Repeat("b", 64),
		ServerOperationID:  " op_lifecycle_1 ",
	}
	normalized, err := NormalizeDeploymentLifecycleCommand(valid)
	require.NoError(t, err)
	require.Equal(t, "op_lifecycle_1", normalized.ServerOperationID)

	tests := []struct {
		name   string
		mutate func(*DeploymentLifecycleCommand)
	}{
		{name: "missing customer", mutate: func(item *DeploymentLifecycleCommand) { item.CustomerOrgID = 0 }},
		{name: "missing actor", mutate: func(item *DeploymentLifecycleCommand) { item.ActorUserID = 0 }},
		{name: "uppercase key hash", mutate: func(item *DeploymentLifecycleCommand) { item.IdempotencyKeyHash = strings.Repeat("A", 64) }},
		{name: "raw key", mutate: func(item *DeploymentLifecycleCommand) { item.IdempotencyKeyHash = "raw-key" }},
		{name: "malformed fingerprint", mutate: func(item *DeploymentLifecycleCommand) { item.RequestFingerprint = strings.Repeat("b", 64) }},
		{name: "missing operation", mutate: func(item *DeploymentLifecycleCommand) { item.ServerOperationID = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := valid
			test.mutate(&command)
			_, err := NormalizeDeploymentLifecycleCommand(command)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
}
