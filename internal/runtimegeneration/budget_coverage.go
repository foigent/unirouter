package runtimegeneration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
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
	var encoded string
	err = q.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(version ORDER BY version),'[]'::jsonb)::text FROM schema_migrations`).Scan(&encoded)
	if err != nil {
		return c, err
	}
	var ledger []string
	if err := json.Unmarshal([]byte(encoded), &ledger); err != nil {
		return c, err
	}
	if err := verifyBudgetCoverageLedger(ledger, head, ledgerDigest); err != nil {
		return c, err
	}

	return c, nil
}

// M119 through M122 retain the exact M118 proof. A known additive suffix does not
// recertify history or replace the original migration/manifest/ledger identity.
func verifyBudgetCoverageLedger(ledger []string, certifiedHead, certifiedDigest string) error {
	if certifiedHead != schemaauthority.BudgetSnapshotExpandHead || !artifactPattern.MatchString("sha256:"+certifiedDigest) || len(ledger) == 0 {
		return fmt.Errorf("budget coverage schema head mismatch")
	}
	head := ledger[len(ledger)-1]
	if head == schemaauthority.CommercialLicenseRetirementHead {
		if err := schemaauthority.VerifyRuntimeLedger(ledger, ledger); err != nil {
			return err
		}
		return verifyBudgetCoverageLedger(ledger[:len(ledger)-1], certifiedHead, certifiedDigest)
	}
	if head == schemaauthority.XInsightCredentialExpandHead {
		if err := schemaauthority.VerifyRuntimeLedger(ledger, ledger); err != nil {
			return err
		}
		if err := CurrentManifest(schemaauthority.SupportedSchemaHeads()).Require(head, XInsightCredentialRequirements()); err != nil {
			return err
		}
		return verifyBudgetCoverageLedger(ledger[:len(ledger)-1], certifiedHead, certifiedDigest)
	}
	if head == schemaauthority.XInsightAdoptionExpandHead {
		if err := schemaauthority.VerifyRuntimeLedger(ledger, ledger); err != nil {
			return err
		}
		if err := CurrentManifest(schemaauthority.SupportedSchemaHeads()).Require(head, XInsightAdoptionRequirements()); err != nil {
			return err
		}
		return verifyBudgetCoverageLedger(ledger[:len(ledger)-1], certifiedHead, certifiedDigest)
	}
	if head != schemaauthority.BudgetSnapshotExpandHead && head != schemaauthority.ContentCaptureExpandHead && head != schemaauthority.ContentReclaimExpandHead && head != schemaauthority.PreDispatchExpandHead && (head != schemaauthority.ScopedContentExpandHead && head != schemaauthority.XInsightPolicyExpandHead) {
		return fmt.Errorf("budget coverage schema head mismatch")
	}
	if err := schemaauthority.VerifyRuntimeLedger(ledger, ledger); err != nil {
		return fmt.Errorf("budget coverage schema ledger: %w", err)
	}
	prefix := ledger
	if head == schemaauthority.ContentCaptureExpandHead || head == schemaauthority.ContentReclaimExpandHead || head == schemaauthority.PreDispatchExpandHead || (head == schemaauthority.ScopedContentExpandHead || head == schemaauthority.XInsightPolicyExpandHead) {
		prefix = ledger[:len(ledger)-1]
		if head == schemaauthority.XInsightPolicyExpandHead {
			prefix = ledger[:len(ledger)-5]
		} else if head == schemaauthority.ScopedContentExpandHead {
			prefix = ledger[:len(ledger)-4]
		} else if head == schemaauthority.PreDispatchExpandHead {
			prefix = ledger[:len(ledger)-3]
		} else if head == schemaauthority.ContentReclaimExpandHead {
			prefix = ledger[:len(ledger)-2]
		}
	}
	if prefix[len(prefix)-1] != certifiedHead || fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(prefix, "\n")))) != certifiedDigest {
		return fmt.Errorf("budget coverage certified predecessor mismatch")
	}
	refs := BudgetCoverageRequirements()
	if head == schemaauthority.ContentCaptureExpandHead || head == schemaauthority.ContentReclaimExpandHead || head == schemaauthority.PreDispatchExpandHead || (head == schemaauthority.ScopedContentExpandHead || head == schemaauthority.XInsightPolicyExpandHead) {
		refs = append(refs, ContentLifecycleRequirements()...)
	}
	if head == schemaauthority.XInsightPolicyExpandHead {
		refs = append(refs, XInsightPolicyRequirements()...)
	}
	return CurrentManifest(schemaauthority.SupportedSchemaHeads()).Require(head, refs)
}
