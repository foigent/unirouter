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
