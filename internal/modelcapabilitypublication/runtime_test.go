package modelcapabilitypublication

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFoldedProjectionForLedgerKeepsInactiveDeclarationsOut(t *testing.T) {
	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	projection, err := FoldedProjectionForLedger([]string{"000001_init_schema", BaselineMigrationM100})
	require.NoError(t, err)
	require.Equal(t, baseline, projection)
}

func TestFoldedProjectionForLedgerRequiresM100Baseline(t *testing.T) {
	_, err := FoldedProjectionForLedger([]string{"000001_init_schema"})
	require.ErrorContains(t, err, "does not contain baseline")
}

func TestVerifyArtifactPublicationInventoryRejectsUncompiledMarkedMigration(t *testing.T) {
	dir := t.TempDir()
	publicationID := "000101_uncompiled_publication"
	require.NoError(t, os.WriteFile(filepath.Join(dir, publicationID+".up.sql"),
		[]byte(publicationMigrationMarker+"\nSELECT 1;\n"), 0o600))
	err := VerifyArtifactPublicationInventory(dir, []string{BaselineMigrationM100, publicationID})
	require.ErrorContains(t, err, "lacks a compiled declaration")
}

func TestVerifyArtifactPublicationInventoryIgnoresOrdinaryMigration(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, BaselineMigrationM100+".up.sql"), []byte("SELECT 1;\n"), 0o600))
	require.NoError(t, VerifyArtifactPublicationInventory(dir, []string{BaselineMigrationM100}))

	wire, err := os.ReadFile(filepath.Join(dir, BaselineMigrationM100+".up.sql"))
	require.NoError(t, err)
	require.False(t, strings.Contains(string(wire), publicationMigrationMarker))
}

func TestVerifyArtifactPublicationInventoryRejectsActiveDeclarationWithoutMarker(t *testing.T) {
	dir := t.TempDir()
	declaration := fixtureDeclaration()
	require.NoError(t, os.WriteFile(filepath.Join(dir, declaration.PublicationID+".up.sql"), []byte("SELECT 1;\n"), 0o600))
	err := verifyArtifactPublicationInventory(dir, []string{BaselineMigrationM100, declaration.PublicationID}, []Declaration{declaration})
	require.ErrorContains(t, err, "lacks a marked migration")
}
