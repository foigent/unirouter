package sourcefacts

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnabledProfilesRespectsCaptureAndDefaultEnrollmentScope(t *testing.T) {
	for _, options := range []Options{
		{}, {Enabled: true}, {Enabled: true, ObservationsEnabled: true},
		{Enabled: true, ObservationsEnabled: true, BudgetSnapshotsEnabled: true},
	} {
		service := &Service{options: options}
		profiles := EnabledProfiles(service)
		require.True(t, slices.IsSorted(profiles))
		if !options.Enabled {
			require.Empty(t, profiles)
			continue
		}
		require.True(t, ValidateProfiles(profiles))
		require.True(t, service.ProfilesEnabled(profiles))
		for _, profile := range ObservationProfiles() {
			require.Equal(t, options.ObservationsEnabled, slices.Contains(profiles, profile))
		}
		defaults := DefaultEnrollmentProfiles(profiles)
		for _, profile := range BudgetProfiles() {
			require.Equal(t, options.BudgetSnapshotsEnabled, slices.Contains(profiles, profile))
			require.NotContains(t, defaults, profile)
		}
		require.NotContains(t, defaults, GatewayPreDispatchProfile, "the extra v2 profile is available for explicit consent, not silently added to the accepted baseline")
		for _, profile := range defaults {
			require.Contains(t, profiles, profile)
		}
	}
	require.Empty(t, EnabledProfiles(nil))
}
