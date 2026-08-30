package modelcapabilitypublication

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// VerifyArtifactPublicationInventory proves the artifact has a bijection
// between compiled post-M100 declarations and exact generated publication
// migrations. A compiled declaration can never silently affect runtime
// qualification without an artifact migration and ledger identity.
func VerifyArtifactPublicationInventory(migrationsDir string, artifactLedger []string) error {
	declarations, err := CompiledDeclarations()
	if err != nil {
		return err
	}
	return verifyArtifactPublicationInventory(migrationsDir, artifactLedger, declarations)
}

func verifyArtifactPublicationInventory(migrationsDir string, artifactLedger []string, declarations []Declaration) error {
	declared := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		if _, duplicate := declared[declaration.PublicationID]; duplicate {
			return fmt.Errorf("%w: duplicate compiled declaration %s", ErrInvalidDeclaration, declaration.PublicationID)
		}
		declared[declaration.PublicationID] = struct{}{}
	}
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migration artifact directory: %w", err)
	}
	marked := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		version := strings.TrimSuffix(entry.Name(), ".up.sql")
		wire, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read migration artifact %s: %w", entry.Name(), err)
		}
		if IsPublicationMigration(wire) {
			marked[version] = struct{}{}
			if _, exists := declared[version]; !exists {
				return fmt.Errorf("%w: publication migration %s lacks a compiled declaration", ErrInvalidDeclaration, version)
			}
		}
	}
	predecessor, err := CompiledBaseline()
	if err != nil {
		return err
	}
	for _, declaration := range declarations {
		index := slices.Index(artifactLedger, declaration.PublicationID)
		if index < 0 {
			return fmt.Errorf("%w: compiled declaration %s lacks an artifact migration identity", ErrInvalidDeclaration,
				declaration.PublicationID)
		}
		if index == 0 || artifactLedger[index-1] != declaration.ExpectedSchemaPredecessor {
			return fmt.Errorf("%w: compiled declaration %s does not follow exact schema predecessor %s", ErrInvalidDeclaration,
				declaration.PublicationID, declaration.ExpectedSchemaPredecessor)
		}
		if _, exists := marked[declaration.PublicationID]; !exists {
			return fmt.Errorf("%w: compiled declaration %s lacks a marked migration", ErrInvalidDeclaration, declaration.PublicationID)
		}
		if err := VerifyGeneratedMigrationFiles(migrationsDir, predecessor, declaration); err != nil {
			return err
		}
		predecessor, err = Fold(predecessor, []Declaration{declaration})
		if err != nil {
			return err
		}
	}
	return nil
}

// VerifyRuntimeStaticProjection is the startup-compatible, read-only exact
// verifier. Dynamic Probe/Approval/qualification lifecycle remains owned by
// the existing resolvers and is deliberately not scanned here.
func VerifyRuntimeStaticProjection(ctx context.Context, q Querier, ledger []string) error {
	expected, err := FoldedProjectionForLedger(ledger)
	if err != nil {
		return err
	}
	return VerifyStaticProjection(ctx, q, expected)
}
