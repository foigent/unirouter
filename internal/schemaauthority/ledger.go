package schemaauthority

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

const upMigrationSuffix = ".up.sql"

// ExpectedLedger derives the binary artifact's complete schema identity from
// its trusted migration directory. The returned slice is a new, sorted value
// that callers may retain for later lifecycle checks.
func ExpectedLedger(migrationsDir string) ([]string, error) {
	if strings.TrimSpace(migrationsDir) == "" {
		return nil, fmt.Errorf("migration artifact directory is required")
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("read migration artifact directory %q: %w", migrationsDir, err)
	}

	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, upMigrationSuffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect migration artifact %q: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("migration artifact %q is not a regular file", name)
		}
		versions = append(versions, strings.TrimSuffix(name, upMigrationSuffix))
	}
	sort.Strings(versions)
	if err := validateLedger("migration artifact", versions, true); err != nil {
		return nil, err
	}
	return versions, nil
}

// VerifyLedger requires the database ledger to be the complete, ordered
// identity derived from ExpectedLedger. It rejects missing, extra, future,
// duplicate, empty, and reordered database histories rather than treating the
// highest applied version as sufficient evidence.
func VerifyLedger(expected, actual []string) error {
	if err := validateLedger("migration artifact", expected, true); err != nil {
		return err
	}
	if err := validateLedger("database schema", actual, false); err != nil {
		return err
	}

	expectedSet := make(map[string]struct{}, len(expected))
	actualSet := make(map[string]struct{}, len(actual))
	for _, version := range expected {
		expectedSet[version] = struct{}{}
	}
	for _, version := range actual {
		actualSet[version] = struct{}{}
	}

	missing := make([]string, 0)
	for _, version := range expected {
		if _, ok := actualSet[version]; !ok {
			missing = append(missing, version)
		}
	}
	extra := make([]string, 0)
	for _, version := range actual {
		if _, ok := expectedSet[version]; !ok {
			extra = append(extra, version)
		}
	}
	if len(missing) != 0 || len(extra) != 0 {
		return fmt.Errorf(
			"database schema ledger does not match migration artifact: missing=%v extra=%v",
			missing, extra,
		)
	}
	return nil
}

func validateLedger(label string, versions []string, requireBinaryCeiling bool) error {
	if len(versions) == 0 {
		return fmt.Errorf("%s ledger is empty", label)
	}
	seen := make(map[string]struct{}, len(versions))
	for index, version := range versions {
		if version == "" || version != strings.TrimSpace(version) {
			return fmt.Errorf("%s ledger contains an invalid empty or padded version", label)
		}
		if _, ok := seen[version]; ok {
			return fmt.Errorf("%s ledger contains duplicate migration %q", label, version)
		}
		seen[version] = struct{}{}
		if index > 0 && versions[index-1] >= version {
			return fmt.Errorf(
				"%s ledger is not strictly ordered at %q after %q",
				label, version, versions[index-1],
			)
		}
	}
	if requireBinaryCeiling && versions[len(versions)-1] != LatestSupportedMigration {
		return fmt.Errorf(
			"migration artifact ceiling %q does not match binary authority %q",
			versions[len(versions)-1], LatestSupportedMigration,
		)
	}
	return nil
}
