package modelcapabilitypublication

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// VerifyArtifactPublicationInventory proves that every declaration-owned
// migration in the artifact has one compiled declaration with the same exact
// identity. Extra compiled declarations are inactive candidates and do not
// become database current state merely by being present in the binary.
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
	for _, declaration := range declarations {
		if !slices.Contains(artifactLedger, declaration.PublicationID) {
			continue
		}
		if _, exists := marked[declaration.PublicationID]; !exists {
			return fmt.Errorf("%w: active declaration %s lacks a marked migration", ErrInvalidDeclaration, declaration.PublicationID)
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
