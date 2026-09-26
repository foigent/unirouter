package sourcefacts

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

const RegistrationVersion = "unirouter.xinsight.source-credential-registration.v1"

type Registration struct {
	SchemaVersion  string   `json:"schema_version"`
	TokenFormat    string   `json:"token_format"`
	VerifierScheme string   `json:"verifier_scheme"`
	KeyPrefix      string   `json:"key_prefix"`
	VerifierSHA256 string   `json:"verifier_sha256"`
	OperationID    string   `json:"operation_id"`
	SourceInstance string   `json:"source_instance"`
	OrgID          string   `json:"org_id"`
	ProjectID      string   `json:"project_id"`
	Profiles       []string `json:"profiles"`
}

var prefixPattern = regexp.MustCompile(`^ur-xf1-[0-9a-f]{13}$`)
var operationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func ValidateProfiles(profiles []string) bool {
	if len(profiles) < 1 || len(profiles) > 12 || !slices.IsSorted(profiles) {
		return false
	}
	for i, p := range profiles {
		if (!ValidProfile(p) && !validObservationProfile(p) && !validBudgetProfile(p)) || i > 0 && profiles[i-1] == p {
			return false
		}
	}
	return true
}

// Validate binds the receiver's non-secret verifier registration to one exact
// producer, project, and fixed profile set. It never accepts a bearer token.
func (r Registration) Validate() error {
	if r.SchemaVersion != RegistrationVersion || r.TokenFormat != auth.ReceiverFeedTokenFormat || r.VerifierScheme != auth.ReceiverFeedVerifierScheme || !prefixPattern.MatchString(r.KeyPrefix) || !operationPattern.MatchString(r.OperationID) || !positiveID(r.OrgID) || !positiveID(r.ProjectID) || !ValidateProfiles(r.Profiles) {
		return errors.New("invalid source credential registration")
	}
	if _, e := eventconnector.NewOpaqueString("source_instance", r.SourceInstance, 1, 128, true); e != nil {
		return e
	}
	_, e := auth.DecodeReceiverFeedVerifier(r.VerifierSHA256)
	return e
}

func (r Registration) Hash() (string, error) {
	if e := r.Validate(); e != nil {
		return "", e
	}
	wire, e := json.Marshal(r)
	if e != nil {
		return "", e
	}
	return eventconnector.HashSourceBytes(wire), nil
}

func (s *Service) SourceInstance() string {
	if s == nil {
		return ""
	}
	return s.options.SourceInstance
}

// ProfilesEnabled checks explicit product capture enablement separately from
// syntactic registration. Upgrading never grants or enables another profile.
func (s *Service) ProfilesEnabled(profiles []string) bool {
	if !s.Enabled() || !ValidateProfiles(profiles) {
		return false
	}
	for _, profile := range profiles {
		if validObservationProfile(profile) && !s.ObservationsEnabled() || validBudgetProfile(profile) && !s.BudgetsEnabled() {
			return false
		}
	}
	return true
}

// OnlyBudgetProfilesChanged preserves the immutable registrations and A/B
// cursor generation when the connection's D visibility alone changes.
func OnlyBudgetProfilesChanged(before, after []string) bool {
	if !ValidateProfiles(before) || !ValidateProfiles(after) {
		return false
	}
	events := func(profiles []string) []string {
		out := []string{}
		for _, p := range profiles {
			if !validBudgetProfile(p) {
				out = append(out, p)
			}
		}
		return out
	}
	return slices.Equal(events(before), events(after))
}
