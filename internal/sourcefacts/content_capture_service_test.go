package sourcefacts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/contentcapture"
)

func TestContentCaptureIdentityAndObservedMediaType(t *testing.T) {
	view := contentcapture.PolicyView{SourceInstance: "synthetic-source", PolicyRef: "content-policy:1:2", Effective: contentcapture.PolicyVersion{PolicyVersion: "1", Config: contentcapture.PolicyConfig{Mode: contentcapture.ModeFullContent}}, EffectiveAt: "2026-09-20T00:00:00.000000Z"}
	id := ContentCaptureIdentity{OrgID: 1, ProjectID: 2, RequestID: "synthetic-request", Protocol: "openai.chat", ObservationPoint: "provider_response", AttemptID: "actual-dispatch-attempt", OccurredAt: time.Now()}
	m, err := captureInitialManifest(id, view)
	require.NoError(t, err)
	require.Equal(t, id.AttemptID, m.AttemptID)
	require.Equal(t, "body-0", m.ContentItemID)
	id.AttemptID = ""
	_, err = captureInitialManifest(id, view)
	require.Error(t, err)
	id.ObservationPoint = "client_response"
	_, err = captureInitialManifest(id, view)
	require.NoError(t, err)
	id.AttemptID = "fabricated-client-attempt"
	_, err = captureInitialManifest(id, view)
	require.Error(t, err)
	require.Equal(t, "application/json", captureMediaType("Application/JSON; charset=utf-8"))
	require.Equal(t, "application/octet-stream", captureMediaType("broken\r\nHeader: secret"))
}
