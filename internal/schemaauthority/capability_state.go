package schemaauthority

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// HasCapabilityExpand chooses one of the two explicitly supported reader
// contracts. It is not a replacement for the complete startup/lease ledger gate.
func HasCapabilityExpand(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (bool, error) {
	var expanded bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, CapabilityExpandHead).Scan(&expanded)
	return expanded, err
}
