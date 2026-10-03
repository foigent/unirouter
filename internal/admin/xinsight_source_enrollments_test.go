package admin

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

func TestSourceEnrollmentScopePublishedProfiles(t *testing.T) {
	basic := sourcefacts.Profiles()
	slices.Sort(basic)
	eight := append(slices.Clone(basic), sourcefacts.RuntimeProfile, sourcefacts.AttemptProfile, sourcefacts.CapabilityProfile, sourcefacts.PolicyProfile, sourcefacts.DestinationProfile)
	slices.Sort(eight)
	require.Len(t, eight, 8)
	for _, tc := range []struct {
		name     string
		profiles []string
		content  bool
		valid    bool
	}{
		{"original_three", basic, false, true},
		{"explicit_eight", eight, false, true},
		{"content_only", nil, true, true},
		{"no_explicit_scope", nil, false, false},
		{"unknown", []string{"unirouter.unknown.v1"}, true, false},
		{"duplicate", []string{sourcefacts.ContextProfile, sourcefacts.ContextProfile}, false, false},
		{"unsorted", []string{sourcefacts.RuntimeProfile, sourcefacts.ContextProfile}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := sourceEnrollmentInput{InstallationID: "xci_scope_test", Coverage: "all_current_future", Profiles: tc.profiles, IncludeContent: tc.content}
			project, _, valid := input.scope()
			require.Equal(t, tc.valid, valid)
			require.Zero(t, project)
		})
	}
}
