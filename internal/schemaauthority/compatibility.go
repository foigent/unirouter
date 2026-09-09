package schemaauthority

import (
	"fmt"
	"slices"
)

// RuntimeLedgers is a finite, named bridge. The entire predecessor ledger is
// retained; matching a maximum migration number alone is never sufficient.
func RuntimeLedgers(artifact []string) ([][]string, error) {
	if err := validateLedger("migration artifact", artifact, false); err != nil {
		return nil, err
	}
	end := len(artifact)
	if end >= 4 && artifact[end-1] == SourceFactsExpandHead && artifact[end-2] == StreamCounterExpandHead && artifact[end-3] == CapabilityExpandHead && artifact[end-4] == LegacyCapabilityHead {
		return [][]string{slices.Clone(artifact[:end-3]), slices.Clone(artifact[:end-2]), slices.Clone(artifact[:end-1]), slices.Clone(artifact)}, nil
	}
	if end >= 3 && artifact[end-1] == StreamCounterExpandHead && artifact[end-2] == CapabilityExpandHead && artifact[end-3] == LegacyCapabilityHead {
		return [][]string{slices.Clone(artifact[:end-2]), slices.Clone(artifact[:end-1]), slices.Clone(artifact)}, nil
	}
	// Retained immutable Artifact records may contain the original bridge.
	// Startup still derives ExpectedLedger using this binary's exact ceiling.
	if end >= 2 && artifact[end-1] == CapabilityExpandHead && artifact[end-2] == LegacyCapabilityHead {
		return [][]string{slices.Clone(artifact[:end-1]), slices.Clone(artifact)}, nil
	}
	return nil, fmt.Errorf("artifact does not contain a known exact compatibility bridge")
}

func VerifyRuntimeLedger(artifact, actual []string) error {
	ledgers, err := RuntimeLedgers(artifact)
	if err != nil {
		return err
	}
	if err := validateLedger("database schema", actual, false); err != nil {
		return err
	}
	for _, supported := range ledgers {
		if slices.Equal(supported, actual) {
			return nil
		}
	}
	return fmt.Errorf("database ledger is outside the exact M113/M114/M115/M116 runtime compatibility bridge")
}
