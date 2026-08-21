package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

type schemaAuthorityReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// requireExactRuntimeSchema prevents a migration-skipping replica from
// serving with database triggers or invariants from another binary contract.
// Migration-running instances pass through the same gate so a future or
// otherwise mismatched ledger cannot be mistaken for a supported schema.
func requireExactRuntimeSchema(ctx context.Context, reader schemaAuthorityReader, expectedLedger []string) error {
	if reader == nil {
		return fmt.Errorf("schema authority reader is required")
	}
	var encodedLedger string
	if err := reader.QueryRow(ctx, `
SELECT COALESCE(jsonb_agg(version ORDER BY version), '[]'::jsonb)::text
  FROM schema_migrations`).Scan(&encodedLedger); err != nil {
		return fmt.Errorf("read schema migration authority: %w", err)
	}
	var actualLedger []string
	if err := json.Unmarshal([]byte(encodedLedger), &actualLedger); err != nil {
		return fmt.Errorf("decode schema migration authority: %w", err)
	}
	if err := schemaauthority.VerifyLedger(expectedLedger, actualLedger); err != nil {
		return fmt.Errorf("verify schema migration authority: %w", err)
	}
	return nil
}
