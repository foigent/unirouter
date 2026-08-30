package migrate

import (
	"strings"
	"testing"
)

func TestMigrationTransactionBodyRemovesOnlyStandaloneBoundaries(t *testing.T) {
	raw := `-- migration
BEGIN;
CREATE PROCEDURE commit_inside_procedure()
LANGUAGE plpgsql AS $$
BEGIN
    COMMIT;
END;
$$;
CREATE TABLE example (id BIGINT PRIMARY KEY);
COMMIT;
`

	body := migrationTransactionBody(raw)
	if strings.Contains(body, "-- migration\nBEGIN;\n") || strings.HasSuffix(strings.TrimSpace(body), "COMMIT;") {
		t.Fatalf("outer transaction boundaries remain: %q", body)
	}
	for _, want := range []string{"CREATE PROCEDURE", "BEGIN\n", "    COMMIT;\n", "END;\n$$;", "CREATE TABLE example"} {
		if !strings.Contains(body, want) {
			t.Fatalf("migration body lost %q: %q", want, body)
		}
	}
}

func TestMigrationTransactionBodyPreservesNonBoundaryTransactionStatements(t *testing.T) {
	raw := `CREATE TABLE before_transaction (id BIGINT PRIMARY KEY);
BEGIN;
CREATE TABLE inside_transaction (id BIGINT PRIMARY KEY);
COMMIT;
CREATE TABLE after_transaction (id BIGINT PRIMARY KEY);
`

	if got := migrationTransactionBody(raw); got != raw {
		t.Fatalf("non-boundary transaction statements were changed: %q", got)
	}
}

func TestMigrationTransactionBodyIgnoresSurroundingComments(t *testing.T) {
	raw := `-- leading comment
BEGIN;
CREATE TABLE example (id BIGINT PRIMARY KEY);
COMMIT;
-- trailing comment
`

	body := migrationTransactionBody(raw)
	if strings.Contains(body, "BEGIN;") || strings.Contains(body, "COMMIT;") {
		t.Fatalf("outer transaction boundaries remain: %q", body)
	}
	for _, want := range []string{"-- leading comment", "CREATE TABLE example", "-- trailing comment"} {
		if !strings.Contains(body, want) {
			t.Fatalf("migration body lost %q: %q", want, body)
		}
	}
}

func TestValidateExactLedgerPrefix(t *testing.T) {
	artifact := []string{"000001_first", "000002_second", "000003_third"}
	for _, actual := range [][]string{nil, {"000001_first"}, {"000001_first", "000002_second"}} {
		if err := validateExactLedgerPrefix(actual, artifact, "000002_second"); err != nil {
			t.Fatalf("exact prefix %v rejected: %v", actual, err)
		}
	}
	for _, actual := range [][]string{
		{"000002_second"},
		{"000001_first", "000003_third"},
		{"000001_first", "000002_second", "000003_third"},
		{"000001_first", "000002_unknown"},
	} {
		if err := validateExactLedgerPrefix(actual, artifact, "000002_second"); err == nil {
			t.Fatalf("non-prefix ledger %v accepted", actual)
		}
	}
}
