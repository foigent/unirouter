package modelcapabilitypublication

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedPublicationIsDeterministicAndKeepsTargetRowsInPlan(t *testing.T) {
	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	declaration := fixtureDeclaration()
	first, err := GenerateMigration(baseline, declaration)
	require.NoError(t, err)
	second, err := GenerateMigration(baseline, declaration)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Contains(t, string(first.UpSQL), publicationMigrationMarker)
	require.NotContains(t, string(first.UpSQL), "ON CONFLICT DO NOTHING")
	require.NotContains(t, string(first.UpSQL), "INSERT INTO model_deployment_capabilities")
	require.Contains(t, string(first.DownSQL), "DELETE FROM schema_migrations")
	require.Contains(t, string(first.VerifySQL), "post-commit publication")
}

func TestVerifyGeneratedMigrationFilesRejectsDrift(t *testing.T) {
	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	declaration := fixtureDeclaration()
	generated, err := GenerateMigration(baseline, declaration)
	require.NoError(t, err)
	dir := t.TempDir()
	for suffix, wire := range map[string][]byte{
		".up.sql": generated.UpSQL, ".down.sql": generated.DownSQL, ".verify.sql": generated.VerifySQL,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, declaration.PublicationID+suffix), wire, 0o600))
	}
	require.NoError(t, VerifyGeneratedMigrationFiles(dir, baseline, declaration))
	require.NoError(t, os.WriteFile(filepath.Join(dir, declaration.PublicationID+".verify.sql"), []byte("drift\n"), 0o600))
	require.ErrorContains(t, VerifyGeneratedMigrationFiles(dir, baseline, declaration), "artifact drift")
}

func TestDecodeDeclarationRejectsDuplicateAndUnknownMembers(t *testing.T) {
	duplicate := `{"schema_version":"` + DeclarationSchemaV1 + `","schema_version":"` + DeclarationSchemaV1 + `"}`
	_, err := DecodeDeclaration([]byte(duplicate))
	require.ErrorContains(t, err, "duplicate")

	unknown := `{"schema_version":"` + DeclarationSchemaV1 + `","unknown":true}`
	_, err = DecodeDeclaration([]byte(unknown))
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "unknown") || strings.Contains(err.Error(), "required"))
}

func TestMigrationIDMatchesLedgerColumnWidth(t *testing.T) {
	require.NoError(t, validateMigrationID("migration", "000101_"+strings.Repeat("a", 57)))
	require.Error(t, validateMigrationID("migration", "000101_"+strings.Repeat("a", 58)))
}
