package sourcefacts

import (
	"encoding/json"
	"errors"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

// C registration is intentionally separate from Registration.Validate/Hash.
// Adding content authority must not broaden any existing legacy call site.
func ValidateContentRegistration(r Registration) error {
	if r.SchemaVersion != RegistrationVersion || r.TokenFormat != auth.ReceiverFeedTokenFormat ||
		r.VerifierScheme != auth.ReceiverFeedVerifierScheme || !prefixPattern.MatchString(r.KeyPrefix) ||
		!operationPattern.MatchString(r.OperationID) || !positiveID(r.OrgID) || !positiveID(r.ProjectID) ||
		len(r.Profiles) != 1 || r.Profiles[0] != "unirouter.interaction-content.v1" {
		return errors.New("invalid content credential registration")
	}
	if _, err := eventconnector.NewOpaqueString("source_instance", r.SourceInstance, 1, 128, true); err != nil {
		return err
	}
	_, err := auth.DecodeReceiverFeedVerifier(r.VerifierSHA256)
	return err
}

func HashContentRegistration(r Registration) (string, error) {
	if err := ValidateContentRegistration(r); err != nil {
		return "", err
	}
	// Preserve the established struct order and marshal rules, not CanonicalJSON.
	wire, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return eventconnector.HashSourceBytes(wire), nil
}
