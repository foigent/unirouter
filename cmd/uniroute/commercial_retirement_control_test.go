package main

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

func TestCommercialRetirementControlRejectsExpansionAndOtherSchemas(t *testing.T) {
	artifact, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	artifact = artifact[:slices.Index(artifact, schemaauthority.CommercialLicenseRetirementHead)+1] // Exact historical M126 artifact.
	predecessor := artifact[:len(artifact)-1]
	empty := []runtimegeneration.Identity{}
	for _, command := range []string{"inspect", "read", "prepare", "complete", "stop-check", "stop-apply"} {
		require.True(t, maintenancePredecessorControlAllowed(artifact, predecessor, "test", command, empty), command)
		require.False(t, maintenancePredecessorControlAllowed(artifact, predecessor, "production", command, empty), command)
		require.False(t, maintenancePredecessorControlAllowed(artifact, predecessor[1:], "test", command, empty), command)
		require.False(t, maintenancePredecessorControlAllowed(artifact, artifact, "test", command, empty), command)
	}
	for _, command := range []string{"prepare", "complete"} {
		require.False(t, maintenancePredecessorControlAllowed(artifact, predecessor, "test", command, nil), command)
		require.False(t, maintenancePredecessorControlAllowed(artifact, predecessor, "test", command, []runtimegeneration.Identity{{Instance: "any"}}), command)
	}
	for _, command := range []string{"serve", "register", "expand", "unknown"} {
		require.False(t, maintenancePredecessorControlAllowed(artifact, predecessor, "test", command, empty), command)
	}
}

func TestCommunicationMaintenanceCarrierRejectsExpansionAndOtherSchemas(t *testing.T) {
	artifact, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	artifact = artifact[:slices.Index(artifact, schemaauthority.XInsightCommunicationHead)+1]
	prior := artifact[:len(artifact)-1]
	require.Equal(t, schemaauthority.CommercialLicenseRetirementHead, prior[len(prior)-1])
	for _, command := range []string{"inspect", "read", "stop-check", "stop-apply", "prepare", "complete"} {
		require.True(t, maintenancePredecessorControlAllowed(artifact, prior, "test", command, []runtimegeneration.Identity{}))
		require.False(t, maintenancePredecessorControlAllowed(artifact, prior, "production", command, []runtimegeneration.Identity{}))
		require.False(t, maintenancePredecessorControlAllowed(artifact, prior[1:], "test", command, []runtimegeneration.Identity{}))
	}
	for _, command := range []string{"register", "expand", "serve", "unknown"} {
		require.False(t, maintenancePredecessorControlAllowed(artifact, prior, "test", command, nil))
	}
	require.False(t, maintenancePredecessorControlAllowed(artifact, prior, "test", "prepare", []runtimegeneration.Identity{{Instance: "any"}}))
}

func TestCorrectedObservationMaintenanceCarrierKeepsFiniteWithdrawalBoundary(t *testing.T) {
	artifact, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	artifact = artifact[:slices.Index(artifact, schemaauthority.UpstreamObservationRestoreHead)+1]
	for _, prior := range [][]string{artifact[:len(artifact)-1], artifact[:len(artifact)-2]} {
		for _, cmd := range []string{"inspect", "read", "stop-check", "stop-apply", "prepare", "complete"} {
			require.True(t, maintenancePredecessorControlAllowed(artifact, prior, "test", cmd, []runtimegeneration.Identity{}))
			require.False(t, maintenancePredecessorControlAllowed(artifact, prior, "production", cmd, []runtimegeneration.Identity{}))
			require.False(t, maintenancePredecessorControlAllowed(artifact, prior[1:], "test", cmd, []runtimegeneration.Identity{}))
		}
		require.False(t, maintenancePredecessorControlAllowed(artifact, prior, "test", "prepare", []runtimegeneration.Identity{{Instance: "any"}}))
		for _, cmd := range []string{"register", "expand", "serve", "unknown"} {
			require.False(t, maintenancePredecessorControlAllowed(artifact, prior, "test", cmd, nil))
		}
	}
	require.False(t, maintenancePredecessorControlAllowed(artifact, artifact[:len(artifact)-3], "test", "inspect", nil))
}

func TestAdmissionRecoveryCarrierOnlyInspectsExactM129InTest(t *testing.T) {
	artifact, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	historical := slices.Index(artifact, schemaauthority.UpgradeAdmissionRecoveryHead)
	require.GreaterOrEqual(t, historical, 1)
	artifact = artifact[:historical+1]
	prior := artifact[:len(artifact)-1]
	require.True(t, maintenancePredecessorControlAllowed(artifact, prior, "test", "inspect", nil))
	require.False(t, maintenancePredecessorControlAllowed(artifact, prior, "production", "inspect", nil))
	require.False(t, maintenancePredecessorControlAllowed(artifact, prior[1:], "test", "inspect", nil))
	for _, command := range []string{"prepare", "complete", "read", "stop-check", "stop-apply"} {
		require.False(t, maintenancePredecessorControlAllowed(artifact, prior, "test", command, []runtimegeneration.Identity{}))
	}
}

func TestCustomerPricingCarrierOnlyInspectsExactM130InTest(t *testing.T) {
	artifact, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	historical := slices.Index(artifact, schemaauthority.CustomerStandardPricingHead)
	require.GreaterOrEqual(t, historical, 1)
	artifact = artifact[:historical+1]
	prior := artifact[:len(artifact)-1]
	require.True(t, maintenancePredecessorControlAllowed(artifact, prior, "test", "inspect", nil))
	require.False(t, maintenancePredecessorControlAllowed(artifact, prior, "production", "inspect", nil))
	require.False(t, maintenancePredecessorControlAllowed(artifact, prior[:len(prior)-1], "test", "inspect", nil))
	require.False(t, maintenancePredecessorControlAllowed(artifact, prior, "test", "prepare", nil))
}

func TestNativeAndMediaCarriersOnlyInspectExactPredecessorsInTest(t *testing.T) {
	artifact, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	for _, release := range []struct{ head, predecessor string }{
		{schemaauthority.MediaChargeSourceHead, schemaauthority.OpenAIResponsesToolsHead},
		{schemaauthority.NativeInlineJPEGHead, schemaauthority.MediaChargeSourceHead},
		{schemaauthority.CanonicalMediaJobHead, schemaauthority.NativeInlineJPEGHead},
		{schemaauthority.AsyncMediaProviderTaskHead, schemaauthority.CanonicalMediaJobHead},
	} {
		t.Run(release.head, func(t *testing.T) {
			index := slices.Index(artifact, release.head)
			require.GreaterOrEqual(t, index, 2)
			prior := artifact[:index]
			current := artifact[:index+1]
			require.Equal(t, release.predecessor, prior[len(prior)-1])
			require.True(t, maintenancePredecessorControlAllowed(current, prior, "test", "inspect", nil))
			require.False(t, maintenancePredecessorControlAllowed(current, prior, "production", "inspect", nil))
			require.False(t, maintenancePredecessorControlAllowed(current, prior[:len(prior)-1], "test", "inspect", nil))
			require.False(t, maintenancePredecessorControlAllowed(current, prior, "test", "prepare", nil))
		})
	}
}
