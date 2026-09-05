package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

// Schema and capability state must come from one committed generation. This
// transaction is read-only; startup never repairs or publishes definitions.
func verifyRuntimeAuthority(ctx context.Context, pool *pgxpool.Pool, artifact []string) ([]string, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	actual, err := readCompatibleRuntimeSchema(ctx, tx, artifact)
	if err != nil {
		return nil, err
	}
	if err := modelcapabilitypublication.VerifyRuntimeStaticProjection(ctx, tx, actual); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return actual, nil
}

type schemaAuthorityReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// requireExactRuntimeSchema prevents a migration-skipping replica from
// serving with database triggers or invariants from another binary contract.
// Migration-running instances pass through the same gate so a future or
// otherwise mismatched ledger cannot be mistaken for a supported schema.
func requireExactRuntimeSchema(ctx context.Context, reader schemaAuthorityReader, expectedLedger []string) error {
	actualLedger, err := readRuntimeSchemaLedger(ctx, reader)
	if err != nil {
		return err
	}
	if err := schemaauthority.VerifyLedger(expectedLedger, actualLedger); err != nil {
		return fmt.Errorf("verify schema migration authority: %w", err)
	}
	return nil
}

func readCompatibleRuntimeSchema(ctx context.Context, reader schemaAuthorityReader, artifact []string) ([]string, error) {
	actual, err := readRuntimeSchemaLedger(ctx, reader)
	if err != nil {
		return nil, err
	}
	if err := schemaauthority.VerifyRuntimeLedger(artifact, actual); err != nil {
		return nil, err
	}
	return actual, nil
}

func readRuntimeSchemaLedger(ctx context.Context, reader schemaAuthorityReader) ([]string, error) {
	if reader == nil {
		return nil, fmt.Errorf("schema authority reader is required")
	}
	var encodedLedger string
	if err := reader.QueryRow(ctx, `
SELECT COALESCE(jsonb_agg(version ORDER BY version), '[]'::jsonb)::text
  FROM schema_migrations`).Scan(&encodedLedger); err != nil {
		return nil, fmt.Errorf("read schema migration authority: %w", err)
	}
	var actualLedger []string
	if err := json.Unmarshal([]byte(encodedLedger), &actualLedger); err != nil {
		return nil, fmt.Errorf("decode schema migration authority: %w", err)
	}
	return actualLedger, nil
}
