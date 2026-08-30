package modelcapabilitypublication

import (
	"encoding/json"
	"fmt"
	"slices"
)

// BuildInstallEnvelope creates orchestration evidence for an ordered sequence
// of independent Plans. It does not merge their authority or imply a
// cross-publication transaction.
func BuildInstallEnvelope(artifact ArtifactIdentity, target TargetIdentity, startLedger []string, plans []Plan) (InstallEnvelope, error) {
	if len(plans) == 0 {
		return InstallEnvelope{}, fmt.Errorf("%w: install sequence requires at least one independent Plan", ErrInvalidDeclaration)
	}
	currentLedger := append([]string(nil), startLedger...)
	seen := make(map[string]struct{}, len(plans))
	sequence := make([]InstallSequenceItem, 0, len(plans))
	for _, plan := range plans {
		if err := ValidatePlan(plan); err != nil {
			return InstallEnvelope{}, err
		}
		if plan.Validation != ValidationValid || plan.Artifact != artifact || plan.Target != target {
			return InstallEnvelope{}, fmt.Errorf("%w: install Plan artifact, Target, or validation mismatch", ErrPlanConflict)
		}
		if _, duplicate := seen[plan.PublicationID]; duplicate {
			return InstallEnvelope{}, fmt.Errorf("%w: duplicate install publication %s", ErrInvalidDeclaration, plan.PublicationID)
		}
		if !slices.Equal(plan.ExpectedLedger, currentLedger) {
			return InstallEnvelope{}, fmt.Errorf("%w: install publication %s does not start at the preceding exact ledger", ErrPlanConflict, plan.PublicationID)
		}
		seen[plan.PublicationID] = struct{}{}
		sequence = append(sequence, InstallSequenceItem{PublicationID: plan.PublicationID, PlanDigest: plan.PlanDigest})
		currentLedger = append([]string(nil), plan.ProposedLedger...)
	}
	return finalizeInstallEnvelope(InstallEnvelope{
		SchemaVersion:   InstallEnvelopeSchemaV1,
		Artifact:        artifact,
		Target:          target,
		StartLedger:     append([]string(nil), startLedger...),
		ExpectedLedger:  currentLedger,
		InstallSequence: sequence,
	})
}

// ValidateInstallEnvelope proves that an envelope still names the supplied
// independent Plans in exact predecessor order. It never reconstructs or
// substitutes a missing Plan.
func ValidateInstallEnvelope(envelope InstallEnvelope, plans []Plan) error {
	wantDigest := envelope.EnvelopeDigest
	envelope.EnvelopeDigest = ""
	wire, err := canonicalInstallEnvelope(envelope)
	if err != nil {
		return err
	}
	if wantDigest == "" || sha256Hex(wire) != wantDigest {
		return fmt.Errorf("%w: install envelope digest mismatch", ErrInvalidDeclaration)
	}
	rebuilt, err := BuildInstallEnvelope(envelope.Artifact, envelope.Target, envelope.StartLedger, plans)
	if err != nil {
		return err
	}
	if rebuilt.EnvelopeDigest != wantDigest || !slices.Equal(rebuilt.ExpectedLedger, envelope.ExpectedLedger) ||
		!slices.Equal(rebuilt.InstallSequence, envelope.InstallSequence) {
		return fmt.Errorf("%w: install envelope does not match the independent Plans", ErrPlanConflict)
	}
	return nil
}

func finalizeInstallEnvelope(envelope InstallEnvelope) (InstallEnvelope, error) {
	envelope.EnvelopeDigest = ""
	wire, err := canonicalInstallEnvelope(envelope)
	if err != nil {
		return InstallEnvelope{}, err
	}
	envelope.EnvelopeDigest = sha256Hex(wire)
	return envelope, nil
}

func canonicalInstallEnvelope(envelope InstallEnvelope) ([]byte, error) {
	if envelope.SchemaVersion != InstallEnvelopeSchemaV1 || len(envelope.StartLedger) == 0 ||
		len(envelope.ExpectedLedger) == 0 || len(envelope.InstallSequence) == 0 {
		return nil, fmt.Errorf("%w: incomplete install envelope", ErrInvalidDeclaration)
	}
	for _, ledger := range [][]string{envelope.StartLedger, envelope.ExpectedLedger} {
		for _, migrationID := range ledger {
			if err := validateMigrationID("install ledger entry", migrationID); err != nil {
				return nil, err
			}
		}
	}
	for _, item := range envelope.InstallSequence {
		if err := validateMigrationID("install publication_id", item.PublicationID); err != nil {
			return nil, err
		}
		if !sha256Pattern.MatchString(item.PlanDigest) {
			return nil, fmt.Errorf("%w: invalid install Plan digest", ErrInvalidDeclaration)
		}
	}
	return json.Marshal(envelope)
}
