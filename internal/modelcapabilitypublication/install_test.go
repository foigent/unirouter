package modelcapabilitypublication

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallEnvelopePreservesIndependentPlanSequence(t *testing.T) {
	artifact := ArtifactIdentity{
		Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "fixture",
		PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64),
	}
	target := TargetIdentity{Environment: "integration", DatabaseName: "fixture", SystemIdentifier: "12345"}
	ledger := []string{BaselineMigrationM100, "000101_fixture_publication", "000102_fixture_publication"}
	first := finalizedInstallPlan(t, artifact, target, ledger, ledger[:1], ledger[1:2], ledger[:2], "000101_fixture_publication")
	second := finalizedInstallPlan(t, artifact, target, ledger, ledger[:2], ledger[2:], ledger, "000102_fixture_publication")

	envelope, err := BuildInstallEnvelope(artifact, target, ledger[:1], []Plan{first, second})
	require.NoError(t, err)
	require.Equal(t, ledger, envelope.ExpectedLedger)
	require.Equal(t, []InstallSequenceItem{
		{PublicationID: first.PublicationID, PlanDigest: first.PlanDigest},
		{PublicationID: second.PublicationID, PlanDigest: second.PlanDigest},
	}, envelope.InstallSequence)
	require.NoError(t, ValidateInstallEnvelope(envelope, []Plan{first, second}))

	require.Error(t, ValidateInstallEnvelope(envelope, []Plan{second, first}))
	tampered := envelope
	tampered.InstallSequence[0].PlanDigest = "sha256:" + strings.Repeat("f", 64)
	require.Error(t, ValidateInstallEnvelope(tampered, []Plan{first, second}))
}

func finalizedInstallPlan(t *testing.T, artifact ArtifactIdentity, target TargetIdentity, artifactLedger,
	expected, apply, proposed []string, publicationID string,
) Plan {
	t.Helper()
	files := make([]MigrationFileIdentity, 0, len(artifactLedger))
	for index, version := range artifactLedger {
		files = append(files, MigrationFileIdentity{Version: version, UpSHA256: "sha256:" + strings.Repeat(string(rune('a'+index)), 64)})
	}
	plan, err := FinalizePlan(Plan{
		SchemaVersion: PlanSchemaV1, CanonicalizationVersion: CanonicalizationV1,
		PublicationID: publicationID, DeclarationDigest: "sha256:" + strings.Repeat("4", 64),
		Artifact: artifact, Target: target,
		ArtifactMigrationLedger: append([]string(nil), artifactLedger...),
		ArtifactMigrationFiles:  files,
		PublicationDownSHA256:   "sha256:" + strings.Repeat("d", 64),
		PublicationVerifySHA256: "sha256:" + strings.Repeat("e", 64),
		ExpectedLedger:          append([]string(nil), expected...),
		PendingMigrations:       append([]string(nil), artifactLedger[len(expected):]...),
		ApplyMigrations:         append([]string(nil), apply...),
		ProposedLedger:          append([]string(nil), proposed...),
		Validation:              ValidationValid,
	})
	require.NoError(t, err)
	return plan
}
