package modelcapabilitypublication

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// Querier is the read-only database surface needed by baseline export and
// static projection verification.
type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// ExportBaseline reads the complete M100 static projection. Callers are
// responsible for proving that q belongs to a fresh database migrated through
// exactly M100; Target databases are cross-check inputs, never baseline
// sources.
func ExportBaseline(ctx context.Context, q Querier, expectedLedger []string) (Baseline, error) {
	if len(expectedLedger) == 0 || expectedLedger[len(expectedLedger)-1] != BaselineMigrationM100 {
		return Baseline{}, fmt.Errorf("M100 baseline export requires an exact artifact ledger ending at %s", BaselineMigrationM100)
	}
	actualLedger, err := readLedger(ctx, q)
	if err != nil {
		return Baseline{}, err
	}
	if !slices.Equal(actualLedger, expectedLedger) {
		return Baseline{}, fmt.Errorf("M100 baseline source ledger mismatch: actual=%v expected=%v", actualLedger, expectedLedger)
	}
	bindings, err := exportBindings(ctx, q)
	if err != nil {
		return Baseline{}, err
	}
	selectors, err := exportSelectors(ctx, q)
	if err != nil {
		return Baseline{}, err
	}
	baseline := normalizeBaseline(Baseline{
		SchemaVersion:    BaselineSchemaV1,
		ThroughMigration: BaselineMigrationM100,
		Bindings:         bindings,
		Selectors:        selectors,
	})
	if err := ValidateBaseline(baseline); err != nil {
		return Baseline{}, fmt.Errorf("validate exported M100 baseline: %w", err)
	}
	return baseline, nil
}

func readLedger(ctx context.Context, q Querier) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("query schema ledger: %w", err)
	}
	defer rows.Close()
	var ledger []string
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan schema ledger: %w", err)
		}
		ledger = append(ledger, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read schema ledger: %w", err)
	}
	return ledger, nil
}

func exportBindings(ctx context.Context, q Querier) ([]Binding, error) {
	rows, err := q.Query(ctx, `
SELECT operation_id, capability_id,
       execution_profile_id, execution_profile_version,
       protocol_profile_id, protocol_profile_version,
       probe_contract_id, probe_contract_version,
       expected_provider, expected_upstream_model, public_path,
       introduced_migration
  FROM model_capability_probe_contract_bindings
 ORDER BY operation_id, capability_id,
          execution_profile_id, execution_profile_version,
          protocol_profile_id, protocol_profile_version,
          probe_contract_id, probe_contract_version`)
	if err != nil {
		return nil, fmt.Errorf("query capability-contract bindings: %w", err)
	}
	defer rows.Close()
	var bindings []Binding
	for rows.Next() {
		var binding Binding
		if err := rows.Scan(
			&binding.OperationID, &binding.CapabilityID,
			&binding.ExecutionProfileID, &binding.ExecutionProfileVersion,
			&binding.ProtocolProfileID, &binding.ProtocolProfileVersion,
			&binding.ProbeContractID, &binding.ProbeContractVersion,
			&binding.ExpectedProvider, &binding.ExpectedUpstreamModel,
			&binding.PublicPath, &binding.IntroducedMigration,
		); err != nil {
			return nil, fmt.Errorf("scan capability-contract binding: %w", err)
		}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read capability-contract bindings: %w", err)
	}
	return bindings, nil
}

func exportSelectors(ctx context.Context, q Querier) ([]Selector, error) {
	rows, err := q.Query(ctx, `
SELECT operation_id, capability_id,
       execution_profile_id, execution_profile_version,
       protocol_profile_id, protocol_profile_version,
       probe_contract_id, probe_contract_version
  FROM model_capability_current_probe_contracts
 ORDER BY operation_id, capability_id,
          execution_profile_id, execution_profile_version,
          protocol_profile_id, protocol_profile_version`)
	if err != nil {
		return nil, fmt.Errorf("query current capability-contract selectors: %w", err)
	}
	defer rows.Close()
	var selectors []Selector
	for rows.Next() {
		var selector Selector
		if err := rows.Scan(
			&selector.OperationID, &selector.CapabilityID,
			&selector.ExecutionProfileID, &selector.ExecutionProfileVersion,
			&selector.ProtocolProfileID, &selector.ProtocolProfileVersion,
			&selector.ProbeContractID, &selector.ProbeContractVersion,
		); err != nil {
			return nil, fmt.Errorf("scan current capability-contract selector: %w", err)
		}
		selectors = append(selectors, selector)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read current capability-contract selectors: %w", err)
	}
	return selectors, nil
}
