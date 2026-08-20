package schemaauthority

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExpectedLedgerReturnsCompleteSortedArtifactIdentity(t *testing.T) {
	dir := t.TempDir()
	writeMigrationArtifact(t, dir, LatestSupportedMigration)
	writeMigrationArtifact(t, dir, "000001_init_schema")
	if err := os.WriteFile(filepath.Join(dir, "000001_init_schema.down.sql"), []byte("SELECT 1;"), 0o600); err != nil {
		t.Fatalf("write ignored down migration: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write ignored fixture: %v", err)
	}

	got, err := ExpectedLedger(dir)
	if err != nil {
		t.Fatalf("load expected ledger: %v", err)
	}
	want := []string{"000001_init_schema", LatestSupportedMigration}
	if !slices.Equal(got, want) {
		t.Fatalf("expected ledger = %v, want %v", got, want)
	}
}

func TestExpectedLedgerRejectsEmptyAndArtifactCeilingMismatch(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr string
	}{
		{name: "empty artifact", wantErr: "migration artifact ledger is empty"},
		{name: "older artifact ceiling", version: "000078_atomic_model_release_authoring", wantErr: "artifact ceiling"},
		{name: "future artifact ceiling", version: "000080_future_contract", wantErr: "artifact ceiling"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if test.version != "" {
				writeMigrationArtifact(t, dir, test.version)
			}
			_, err := ExpectedLedger(dir)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("artifact error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestVerifyLedgerRejectsIncompleteOrExpandedDatabaseIdentity(t *testing.T) {
	expected := []string{
		"000001_init_schema",
		"000078_atomic_model_release_authoring",
		LatestSupportedMigration,
	}
	tests := []struct {
		name    string
		actual  []string
		wantErr string
	}{
		{name: "exact", actual: slices.Clone(expected)},
		{name: "missing", actual: []string{expected[0], expected[2]}, wantErr: "missing=[000078_atomic_model_release_authoring]"},
		{name: "extra", actual: []string{expected[0], "000077_unexpected_contract", expected[1], expected[2]}, wantErr: "extra=[000077_unexpected_contract]"},
		{name: "future", actual: append(slices.Clone(expected), "000080_future_contract"), wantErr: "extra=[000080_future_contract]"},
		{name: "duplicate", actual: []string{expected[0], expected[1], expected[1], expected[2]}, wantErr: "duplicate migration"},
		{name: "empty", actual: []string{}, wantErr: "database schema ledger is empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyLedger(expected, test.actual)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("exact ledger rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("verify error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestVerifyLedgerRejectsInvalidExpectedArtifactIdentity(t *testing.T) {
	tests := []struct {
		name     string
		expected []string
		wantErr  string
	}{
		{name: "empty", expected: []string{}, wantErr: "migration artifact ledger is empty"},
		{name: "duplicate", expected: []string{LatestSupportedMigration, LatestSupportedMigration}, wantErr: "duplicate migration"},
		{name: "ceiling mismatch", expected: []string{"000078_atomic_model_release_authoring"}, wantErr: "artifact ceiling"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyLedger(test.expected, []string{LatestSupportedMigration})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("verify error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func writeMigrationArtifact(t *testing.T, dir, version string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, version+upMigrationSuffix), []byte("SELECT 1;"), 0o600); err != nil {
		t.Fatalf("write migration artifact %q: %v", version, err)
	}
}
