package schemaauthority

import (
	"fmt"
	"slices"
)

// RuntimeLedgers is a finite, named bridge. The entire predecessor ledger is
// retained; matching a maximum migration number alone is never sufficient.
func RuntimeLedgers(artifact []string) ([][]string, error) {
	if err := validateLedger("migration artifact", artifact, true); err != nil {
		return nil, err
	}
	if len(artifact) < 2 || artifact[len(artifact)-1] != CapabilityExpandHead ||
		artifact[len(artifact)-2] != LegacyCapabilityHead {
		return nil, fmt.Errorf("artifact does not contain the exact capability compatibility bridge")
	}
	return [][]string{slices.Clone(artifact[:len(artifact)-1]), slices.Clone(artifact)}, nil
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
	return fmt.Errorf("database ledger is outside the exact M113/M114 runtime compatibility bridge")
}
