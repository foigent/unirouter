package admin

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsAPIKeyPrefixCollisionTargetsOnlyPrefixIndex(t *testing.T) {
	if !isAPIKeyPrefixCollision(fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "23505", ConstraintName: "idx_api_keys_prefix"})) {
		t.Fatal("expected wrapped prefix-index violation to be retryable")
	}
	for _, err := range []error{
		&pgconn.PgError{Code: "23505", ConstraintName: "api_keys_pkey"},
		&pgconn.PgError{Code: "23503", ConstraintName: "idx_api_keys_prefix"},
		fmt.Errorf("duplicate key"),
	} {
		if isAPIKeyPrefixCollision(err) {
			t.Fatalf("unexpected retryable error: %v", err)
		}
	}
}
