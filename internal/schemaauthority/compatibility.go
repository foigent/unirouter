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
	if end >= 17 && artifact[end-1] == UpstreamObservationRestoreHead && artifact[end-2] == UpstreamReleaseObservationHead {
		if _, err := RuntimeLedgers(artifact[:end-1]); err != nil {
			return nil, err
		}
		return [][]string{slices.Clone(artifact)}, nil
	}
	if end >= 16 && artifact[end-1] == UpstreamReleaseObservationHead && artifact[end-2] == XInsightCommunicationHead {
		if _, err := RuntimeLedgers(artifact[:end-1]); err != nil {
			return nil, err
		}
		return [][]string{slices.Clone(artifact)}, nil
	}
	if end >= 15 && artifact[end-1] == XInsightCommunicationHead && artifact[end-2] == CommercialLicenseRetirementHead {
		if _, err := RuntimeLedgers(artifact[:end-1]); err != nil {
			return nil, err
		}
		// M127 is a maintenance boundary: an old writer cannot honor manual intent.
		return [][]string{slices.Clone(artifact)}, nil
	}
	if end >= 14 && artifact[end-1] == CommercialLicenseRetirementHead && artifact[end-2] == XInsightCredentialExpandHead {
		if _, err := RuntimeLedgers(artifact[:end-1]); err != nil {
			return nil, err
		}
		// Commercial-limit retirement is a maintenance boundary, never a rolling bridge.
		return [][]string{slices.Clone(artifact)}, nil
	}
	if end >= 13 && artifact[end-1] == XInsightCredentialExpandHead && artifact[end-2] == XInsightAdoptionExpandHead {
		prior, err := RuntimeLedgers(artifact[:end-1])
		if err != nil {
			return nil, err
		}
		return append(prior, slices.Clone(artifact)), nil
	}
	if end >= 12 && artifact[end-1] == XInsightAdoptionExpandHead && artifact[end-2] == XInsightPolicyExpandHead {
		prior, err := RuntimeLedgers(artifact[:end-1])
		if err != nil {
			return nil, err
		}
		return append(prior, slices.Clone(artifact)), nil
	}
	if end >= 11 && artifact[end-1] == XInsightPolicyExpandHead && artifact[end-2] == ScopedContentExpandHead {
		prior, err := RuntimeLedgers(artifact[:end-1])
		if err != nil {
			return nil, err
		}
		return append(prior, slices.Clone(artifact)), nil
	}
	if end >= 10 && artifact[end-1] == ScopedContentExpandHead && artifact[end-2] == PreDispatchExpandHead {
		prior, err := RuntimeLedgers(artifact[:end-1])
		if err != nil {
			return nil, err
		}
		return append(prior, slices.Clone(artifact)), nil
	}
	if end >= 9 && artifact[end-1] == PreDispatchExpandHead && artifact[end-2] == ContentReclaimExpandHead {
		prior, err := RuntimeLedgers(artifact[:end-1])
		if err != nil {
			return nil, err
		}
		return append(prior, slices.Clone(artifact)), nil
	}
	if end >= 8 && artifact[end-1] == ContentReclaimExpandHead && artifact[end-2] == ContentCaptureExpandHead {
		prior, err := RuntimeLedgers(artifact[:end-1])
		if err != nil {
			return nil, err
		}
		return append(prior, slices.Clone(artifact)), nil
	}
	if end >= 7 && artifact[end-1] == ContentCaptureExpandHead && artifact[end-2] == BudgetSnapshotExpandHead && artifact[end-3] == ObservationExpandHead && artifact[end-4] == SourceFactsExpandHead && artifact[end-5] == StreamCounterExpandHead && artifact[end-6] == CapabilityExpandHead && artifact[end-7] == LegacyCapabilityHead {
		return [][]string{slices.Clone(artifact[:end-6]), slices.Clone(artifact[:end-5]), slices.Clone(artifact[:end-4]), slices.Clone(artifact[:end-3]), slices.Clone(artifact[:end-2]), slices.Clone(artifact[:end-1]), slices.Clone(artifact)}, nil
	}
	if end >= 6 && artifact[end-1] == BudgetSnapshotExpandHead && artifact[end-2] == ObservationExpandHead && artifact[end-3] == SourceFactsExpandHead && artifact[end-4] == StreamCounterExpandHead && artifact[end-5] == CapabilityExpandHead && artifact[end-6] == LegacyCapabilityHead {
		return [][]string{slices.Clone(artifact[:end-5]), slices.Clone(artifact[:end-4]), slices.Clone(artifact[:end-3]), slices.Clone(artifact[:end-2]), slices.Clone(artifact[:end-1]), slices.Clone(artifact)}, nil
	}
	if end >= 5 && artifact[end-1] == ObservationExpandHead && artifact[end-2] == SourceFactsExpandHead && artifact[end-3] == StreamCounterExpandHead && artifact[end-4] == CapabilityExpandHead && artifact[end-5] == LegacyCapabilityHead {
		return [][]string{slices.Clone(artifact[:end-4]), slices.Clone(artifact[:end-3]), slices.Clone(artifact[:end-2]), slices.Clone(artifact[:end-1]), slices.Clone(artifact)}, nil
	}
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
	return fmt.Errorf("database ledger is outside this artifact's exact supported runtime ledgers")
}
