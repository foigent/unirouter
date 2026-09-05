package schemaauthority

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// LockExecutionCompatibility must be the first business operation of a
// transaction. Utility table locks do not establish a REPEATABLE READ snapshot:
// a writer waiting for an ingress change therefore sees its committed result.
// Existing release-before-slot lock order is preserved. The advisory lock is
// also used by SQL guards to reject unaware writers at the mutation boundary.
func LockExecutionCompatibility(ctx context.Context, tx pgx.Tx) error {
	for _, statement := range []string{
		`LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE`,
		`LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE`,
		`SELECT pg_advisory_xact_lock(86420517831702492)`,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
