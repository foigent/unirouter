package main

import (
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"testing"
)

func TestCommercialRetirementControlRejectsExpansionAndOtherSchemas(t *testing.T) {
	artifact, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	predecessor := artifact[:len(artifact)-1]
	empty := []runtimegeneration.Identity{}
	for _, command := range []string{"inspect", "read", "prepare", "complete", "stop-check", "stop-apply"} {
		require.True(t, commercialRetirementControlAllowed(artifact, predecessor, "test", command, empty), command)
		require.False(t, commercialRetirementControlAllowed(artifact, predecessor, "production", command, empty), command)
		require.False(t, commercialRetirementControlAllowed(artifact, predecessor[1:], "test", command, empty), command)
		require.False(t, commercialRetirementControlAllowed(artifact, artifact, "test", command, empty), command)
	}
	for _, command := range []string{"prepare", "complete"} {
		require.False(t, commercialRetirementControlAllowed(artifact, predecessor, "test", command, nil), command)
		require.False(t, commercialRetirementControlAllowed(artifact, predecessor, "test", command, []runtimegeneration.Identity{{Instance: "any"}}), command)
	}
	for _, command := range []string{"serve", "register", "expand", "unknown"} {
		require.False(t, commercialRetirementControlAllowed(artifact, predecessor, "test", command, empty), command)
	}
}
