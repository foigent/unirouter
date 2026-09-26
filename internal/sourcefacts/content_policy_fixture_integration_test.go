//go:build integration

package sourcefacts

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/contentcapture"
)

// Synthetic ready-row fixtures preserve a valid policy snapshot matching their
// manifest; this does not replace exercising the real Reserve/Publish owner.
func contentFixturePolicy(t *testing.T, m ContentManifest) []byte {
	t.Helper()
	start, err := time.Parse(time.RFC3339Nano, m.CapturedAt)
	require.NoError(t, err)
	end, err := time.Parse(time.RFC3339Nano, m.ExpiresAt)
	require.NoError(t, err)
	retention := int64(end.Sub(start) / time.Second)
	maximum := max(int64(1), m.Representation.SavedBytes)
	config := contentcapture.PolicyConfig{Mode: m.Capture.Mode, SupportedProtocols: []string{m.Protocol}, ObservationPoints: []string{m.ObservationPoint}, AllowedPurposes: []string{"human_review"}, RetentionSeconds: &retention, QuotaBytes: &maximum, MaxSavedBytes: &maximum, OnLimit: "record_capture_gap"}
	require.NoError(t, config.Validate())
	wire, err := json.Marshal(config)
	require.NoError(t, err)
	return wire
}
