package runtimegeneration

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

func TestBudgetCoveragePreservesExactM118ProofAcrossM119(t *testing.T) {
	old := []string{"000001_init", schemaauthority.LegacyCapabilityHead, schemaauthority.CapabilityExpandHead, schemaauthority.StreamCounterExpandHead, schemaauthority.SourceFactsExpandHead, schemaauthority.ObservationExpandHead, schemaauthority.BudgetSnapshotExpandHead}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(old, "\n"))))
	require.NoError(t, verifyBudgetCoverageLedger(old, schemaauthority.BudgetSnapshotExpandHead, digest))
	current := append(slices.Clone(old), schemaauthority.ContentCaptureExpandHead)
	require.NoError(t, verifyBudgetCoverageLedger(current, schemaauthority.BudgetSnapshotExpandHead, digest))
	// Historical proof remains for the old complete prefix, never a rewritten current ledger.
	currentDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(current, "\n"))))
	require.Error(t, verifyBudgetCoverageLedger(current, schemaauthority.BudgetSnapshotExpandHead, currentDigest))
	require.Error(t, verifyBudgetCoverageLedger(current, schemaauthority.ContentCaptureExpandHead, digest))
	require.Error(t, verifyBudgetCoverageLedger(append(slices.Clone(current), "000120_unknown"), schemaauthority.BudgetSnapshotExpandHead, digest))
	missing := append(slices.Clone(current[:3]), current[4:]...)
	require.Error(t, verifyBudgetCoverageLedger(missing, schemaauthority.BudgetSnapshotExpandHead, digest))
	altered := slices.Clone(current)
	altered[0] = "000001_different"
	require.Error(t, verifyBudgetCoverageLedger(altered, schemaauthority.BudgetSnapshotExpandHead, digest))
	require.Error(t, verifyBudgetCoverageLedger(old[:len(old)-1], schemaauthority.BudgetSnapshotExpandHead, digest))
	require.Error(t, verifyBudgetCoverageLedger(nil, schemaauthority.BudgetSnapshotExpandHead, digest))
}

func TestBudgetCoveragePreservesM118ProofAcrossM120(t *testing.T) {
	old := []string{"000001_init", schemaauthority.LegacyCapabilityHead, schemaauthority.CapabilityExpandHead, schemaauthority.StreamCounterExpandHead, schemaauthority.SourceFactsExpandHead, schemaauthority.ObservationExpandHead, schemaauthority.BudgetSnapshotExpandHead}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(old, "\n"))))
	current := append(slices.Clone(old), schemaauthority.ContentCaptureExpandHead, schemaauthority.ContentReclaimExpandHead)
	require.NoError(t, verifyBudgetCoverageLedger(current, schemaauthority.BudgetSnapshotExpandHead, digest))
	require.Error(t, verifyBudgetCoverageLedger(append(slices.Clone(old), schemaauthority.ContentReclaimExpandHead), schemaauthority.BudgetSnapshotExpandHead, digest))
	require.Error(t, verifyBudgetCoverageLedger(current, schemaauthority.BudgetSnapshotExpandHead, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(current, "\n"))))))
	m := CurrentManifest(schemaauthority.SupportedSchemaHeads())
	require.NoError(t, requireLeaseManifest(m, schemaauthority.ContentReclaimExpandHead))
	m.SchemaHeads = slices.DeleteFunc(slices.Clone(m.SchemaHeads), func(h string) bool { return h == schemaauthority.ContentReclaimExpandHead })
	require.Error(t, requireLeaseManifest(m, schemaauthority.ContentReclaimExpandHead))
}
