package runtimegeneration

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	assets "github.com/uniroute/unirouter/contracts/source-budgets/v1"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

func BudgetCoverageRequirements() []modelprofile.ImplementationReference {
	return []modelprofile.ImplementationReference{{Kind: "source_budgets", ID: assets.SchemaVersion}, {Kind: "financial_journal", ID: assets.WriterContract}}
}

// CertifyBudgetCoverageTx runs only in the original compatible-expand owner's
// transaction after fleet verification and M118. Readiness never calls it.
func CertifyBudgetCoverageTx(ctx context.Context, tx pgx.Tx, manifest Manifest, migrationDigest string) error {
	if !artifactPattern.MatchString(migrationDigest) {
		return fmt.Errorf("budget migration digest is invalid")
	}
	if err := manifest.Require(schemaauthority.BudgetSnapshotExpandHead, BudgetCoverageRequirements()); err != nil {
		return err
	}
	wire, err := manifest.CanonicalBytes()
	if err != nil {
		return err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE execution_source_budget_coverage SET state='certified',certified_from=clock_timestamp(),
 schema_ledger_sha256=(SELECT encode(sha256(convert_to(string_agg(version,E'\n' ORDER BY version),'UTF8')),'hex') FROM schema_migrations),
 migration_sha256=$1,support_manifest_sha256=$2,support_manifest_bytes=$3
 WHERE state='pending' AND reason='initial_certification' AND ended_at IS NULL`, migrationDigest, digest, wire)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("budget initial certification requires exactly one new epoch")
	}
	return nil
}

type BudgetCoverage struct {
	Epoch           int64
	CertifiedFrom   time.Time
	RestoreBoundary *time.Time
	Reason          string
}

type coverageReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// ReadBudgetCoverage verifies the retained proof and the binary's current
// implementation support. It does not extend a historical certified interval.
func ReadBudgetCoverage(ctx context.Context, q coverageReader) (BudgetCoverage, error) {
	var c BudgetCoverage
	var state, writer, head, migration, digest, ledgerDigest string
	var wire []byte
	err := q.QueryRow(ctx, `SELECT coverage_epoch,certified_from,restore_boundary,reason,state,writer_contract,schema_head,
 migration_sha256,support_manifest_sha256,support_manifest_bytes,schema_ledger_sha256 FROM execution_source_budget_coverage WHERE ended_at IS NULL`).Scan(
		&c.Epoch, &c.CertifiedFrom, &c.RestoreBoundary, &c.Reason, &state, &writer, &head, &migration, &digest, &wire, &ledgerDigest)
	if err != nil {
		return c, err
	}
	if c.Epoch <= 0 || state != "certified" || writer != assets.WriterContract || head != schemaauthority.BudgetSnapshotExpandHead || !artifactPattern.MatchString(migration) || c.CertifiedFrom.IsZero() {
		return c, fmt.Errorf("budget coverage certificate is invalid")
	}
	manifest, err := VerifyManifest(wire, digest)
	if err != nil {
		return c, err
	}
	if err = manifest.Require(head, BudgetCoverageRequirements()); err != nil {
		return c, err
	}
	if err = CurrentManifest(schemaauthority.SupportedSchemaHeads()).Require(head, BudgetCoverageRequirements()); err != nil {
		return c, err
	}
	var present bool
	err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1) AND (SELECT MAX(version)=$1 AND encode(sha256(convert_to(string_agg(version,E'\n' ORDER BY version),'UTF8')),'hex')=$2 FROM schema_migrations)`, head, ledgerDigest).Scan(&present)
	if err != nil {
		return c, err
	}
	if !present {
		return c, fmt.Errorf("budget coverage schema head mismatch")
	}
	return c, nil
}
