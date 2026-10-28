package main

import (
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"slices"
	"testing"
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
