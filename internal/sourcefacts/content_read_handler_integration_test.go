//go:build integration

package sourcefacts

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/contentcapture"
)

// These are actual migrated-PG test definitions, not evidence of a live DB run.
func contentReadPGRequest(f *contentBodyPGFixture, handler http.Handler, query string) *deadlineRecorder {
	r := f.request.Clone(context.Background())
	r.URL.RawQuery = query
	w := newDeadlineRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestContentReadPGCapabilitiesAvailableAndLateRevision(t *testing.T) {
	f := setupContentBodyPG(t, 0)
	config := contentReadTestConfig(f.authority)
	config.Store = f.store
	h, err := NewContentReadHandlers(config)
	require.NoError(t, err)
	w := contentReadPGRequest(f, h.Capabilities, "")
	require.Equal(t, 200, w.Code)
	capabilities, err := DecodeContentCapabilities(w.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, []string{"disabled", "metadata_only"}, capabilities.CaptureModes, "reading history does not implicitly enable capture")
	p := readTestPage(t, contentReadPGRequest(f, h.Manifests, "purpose=human_review&limit=1"))
	require.Len(t, p.Items, 1)
	require.Equal(t, "available", p.Items[0].Availability.State)
	require.Equal(t, "unknown", p.Coverage)
	require.Equal(t, "capture_health_unknown", p.Gaps[0].Reason)
	// A correction recorded after this page gets a new C sequence even though
	// its observation time remains old and no body object exists for revision 2.
	late := p.Items[0].Document
	late.Revision = "2"
	late.PreviousRevision = "1"
	late.PreviousHash = p.Items[0].SourceSHA256
	late.CorrectionReason = "late_capture_failure"
	late.EventID = late.EventIDForRevision()
	late.Capture.Completeness = "not_captured"
	late.Capture.Reason = "storage_write_failed"
	late.Representation = nil
	late.ContentRef = ""
	late.CapturedAt = ""
	late.ExpiresAt = ""
	_, err = f.source.AppendContentManifest(t.Context(), late, 1)
	require.NoError(t, err)
	next := readTestPage(t, contentReadPGRequest(f, h.Manifests, "purpose=human_review&limit=1&cursor="+p.NextCursor))
	require.Equal(t, "2", next.LastSequence)
	require.Len(t, next.Items, 1)
	require.Equal(t, "not_captured", next.Items[0].Availability.State)
	require.Equal(t, late.OccurredAt, next.Items[0].Document.OccurredAt)
}

func TestContentReadPGFloorCoverageAndInvalidCursor(t *testing.T) {
	f := setupContentBodyPG(t, 0)
	m := contentJournalFixture(f.org, f.project, "pending-after-floor")
	_, err := f.source.AppendContentManifest(t.Context(), m, 1)
	require.NoError(t, err)
	_, err = f.pool.Exec(t.Context(), `UPDATE execution_content_streams SET retained_after_sequence=1,retained_after_time=clock_timestamp() WHERE org_id=$1 AND project_id=$2`, f.org, f.project)
	require.NoError(t, err)
	config := contentReadTestConfig(f.authority)
	config.Store = f.store
	h, err := NewContentReadHandlers(config)
	require.NoError(t, err)
	p := readTestPage(t, contentReadPGRequest(f, h.Manifests, "purpose=human_review&limit=1"))
	require.Equal(t, "1", p.AfterSequence)
	require.Equal(t, "2", p.LastSequence)
	require.Len(t, p.Gaps, 2)
	require.Equal(t, "retention_floor_ahead", p.Gaps[1].Reason)
	require.Equal(t, "pending", p.Items[0].Availability.State)
	scope := ContentCursorScope{SourceInstance: f.source.SourceInstance(), OrgID: f.org, ProjectID: f.project, Profile: ContentProfile, AccessGeneration: 2, RecoveryEpoch: 1}
	for _, position := range []int64{0, 3} {
		cursor, err := EncodeContentCursor([]byte(f.source.options.CursorSecret), scope, position)
		require.NoError(t, err)
		w := contentReadPGRequest(f, h.Manifests, "purpose=human_review&limit=1&cursor="+cursor)
		if position == 0 {
			require.Equal(t, 410, w.Code)
			require.Contains(t, w.Body.String(), "cursor_expired")
		} else {
			require.Equal(t, 400, w.Code)
			require.Contains(t, w.Body.String(), "invalid_cursor")
		}
	}
}

func TestContentReadPGProofRevokedBetweenFileAndResponse(t *testing.T) {
	f := setupContentBodyPG(t, 0)
	config := contentReadTestConfig(f.authority)
	config.Store = f.store
	h := &contentReadHandler{config: config, authority: f.authority.(*contentBodyDBAuthority), slots: make(chan struct{}, 2)}
	h.verify = func(ctx context.Context, receipt contentcapture.StoredReceipt, limits contentcapture.StoreLimits) (*os.File, error) {
		file, err := f.store.OpenVerified(ctx, receipt, limits)
		if err != nil {
			return nil, err
		}
		// Successful updates here establish that no reader lock spans file I/O.
		_, err = f.pool.Exec(ctx, `UPDATE execution_source_connections SET status='paused' WHERE org_id=$1 AND project_id=$2`, f.org, f.project)
		require.NoError(t, err)
		_, err = f.pool.Exec(ctx, `UPDATE execution_source_connections SET status='active' WHERE org_id=$1 AND project_id=$2`, f.org, f.project)
		require.NoError(t, err)
		return file, nil
	}
	w := contentReadPGRequest(f, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.serve(w, r, true) }), "purpose=human_review&limit=1")
	require.Equal(t, 409, w.Code)
	require.Contains(t, w.Body.String(), "content_scope_changed")
	require.NotContains(t, w.Body.String(), "content_ref")
}

func TestContentReadPGVerificationBudgetAndR2(t *testing.T) {
	f := setupContentBodyPG(t, 0)
	config := contentReadTestConfig(f.authority)
	config.Store = f.store
	config.MaxVerifyBytesPerPage = 1
	h, err := NewContentReadHandlers(config)
	require.NoError(t, err)
	p := readTestPage(t, contentReadPGRequest(f, h.Manifests, "purpose=human_review&limit=1"))
	require.Equal(t, "source_unavailable", p.Items[0].Availability.State)
	require.True(t, p.Items[0].Availability.Retryable)
	require.Equal(t, f.receipt.SHA256, p.Items[0].Document.Representation.Digest)
	_, raw := insertContentBodyPGKey(t, f, "d", "legacy", "feed_staged", 0)
	f.request.Header.Set("Authorization", "Bearer "+raw)
	_, err = f.pool.Exec(t.Context(), `UPDATE execution_source_connections SET status='paused' WHERE org_id=$1 AND project_id=$2`, f.org, f.project)
	require.NoError(t, err)
	for _, handler := range []http.Handler{h.Capabilities, h.Manifests} {
		// Even an invalid query cannot override authenticated R2 precedence.
		w := contentReadPGRequest(f, handler, "invalid=query")
		require.Equal(t, 403, w.Code)
		require.Contains(t, w.Body.String(), "profile_not_authorized")
		require.NotContains(t, w.Body.String(), "content_ref")
	}
}
