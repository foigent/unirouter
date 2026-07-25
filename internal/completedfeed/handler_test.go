package completedfeed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
)

func TestHandlerBootstrapIsActivationForwardAtCurrentHighWatermark(t *testing.T) {
	service := newHandlerTestService(t, readerFunc(func(_ context.Context, orgID, projectID, after int64, limit int) (ReadWindow, error) {
		require.EqualValues(t, 7, orgID)
		require.EqualValues(t, 22, projectID)
		require.Zero(t, after)
		require.Equal(t, MaxPageItems+1, limit)
		return ReadWindow{HighWatermark: 41, RetainedAfter: 9, Remaining: 32}, nil
	}))
	request := completedFeedHandlerRequest(t, service, "")
	response := httptest.NewRecorder()

	service.Handler().ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	var page FeedPage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Equal(t, PageSchemaVersion, page.SchemaVersion)
	require.Equal(t, "handler-test-source", page.SourceInstance)
	require.Empty(t, page.Items)
	require.False(t, page.HasMore)
	next, err := service.codec.Decode(page.NextCursor, 22)
	require.NoError(t, err)
	require.EqualValues(t, 41, next)
	require.Equal(t, page.NextCursor, page.HighWatermark)
}

func TestHandlerCursorPageReplayAndHighWatermark(t *testing.T) {
	now := time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC)
	first := storedFeedItem(t, 10, "handler-first", now.Add(-3*time.Minute))
	second := storedFeedItem(t, 11, "handler-second", now.Add(-2*time.Minute))
	third := storedFeedItem(t, 12, "handler-third", now.Add(-time.Minute))
	var calls int
	service := newHandlerTestService(t, readerFunc(func(_ context.Context, _, _ int64, after int64, limit int) (ReadWindow, error) {
		calls++
		require.EqualValues(t, 9, after)
		require.Equal(t, 3, limit)
		return ReadWindow{
			Items:         []StoredItem{first, second, third},
			HighWatermark: 12,
			RetainedAfter: 3,
			Remaining:     3,
		}, nil
	}))
	service.now = func() time.Time { return now }
	cursor, err := service.codec.Encode(22, 9)
	require.NoError(t, err)

	read := func() FeedPage {
		request := completedFeedHandlerRequest(t, service, "?cursor="+cursor+"&limit=2")
		response := httptest.NewRecorder()
		service.Handler().ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var page FeedPage
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
		return page
	}
	firstPage := read()
	replayedPage := read()

	require.Equal(t, 2, calls)
	require.Len(t, firstPage.Items, 2)
	require.True(t, firstPage.HasMore)
	require.Equal(t, firstPage.Items, replayedPage.Items)
	require.Equal(t, firstPage.NextCursor, replayedPage.NextCursor)
	require.Equal(t, firstPage.HighWatermark, replayedPage.HighWatermark)
	next, err := service.codec.Decode(firstPage.NextCursor, 22)
	require.NoError(t, err)
	require.EqualValues(t, 11, next)
	high, err := service.codec.Decode(firstPage.HighWatermark, 22)
	require.NoError(t, err)
	require.EqualValues(t, 12, high)
}

func TestHandlerRejectsExpiredAndMalformedCursors(t *testing.T) {
	service := newHandlerTestService(t, readerFunc(func(_ context.Context, _, _ int64, _ int64, _ int) (ReadWindow, error) {
		return ReadWindow{HighWatermark: 9, RetainedAfter: 8}, nil
	}))
	expired, err := service.codec.Encode(22, 7)
	require.NoError(t, err)

	expiredRequest := completedFeedHandlerRequest(t, service, "?cursor="+expired)
	expiredResponse := httptest.NewRecorder()
	service.Handler().ServeHTTP(expiredResponse, expiredRequest)
	require.Equal(t, http.StatusGone, expiredResponse.Code)
	requireErrorCode(t, expiredResponse, "completed_execution_feed.cursor_expired")

	malformedRequest := completedFeedHandlerRequest(t, service, "?cursor=not-a-cursor")
	malformedResponse := httptest.NewRecorder()
	service.Handler().ServeHTTP(malformedResponse, malformedRequest)
	require.Equal(t, http.StatusBadRequest, malformedResponse.Code)
	requireErrorCode(t, malformedResponse, "completed_execution_feed.invalid_cursor")
}

func TestHandlerRejectsCursorAheadOfSourceHighWatermark(t *testing.T) {
	service := newHandlerTestService(t, readerFunc(func(_ context.Context, _, _ int64, after int64, _ int) (ReadWindow, error) {
		require.EqualValues(t, 9, after)
		return ReadWindow{HighWatermark: 8, RetainedAfter: 3}, nil
	}))
	ahead, err := service.codec.Encode(22, 9)
	require.NoError(t, err)
	request := completedFeedHandlerRequest(t, service, "?cursor="+ahead)
	response := httptest.NewRecorder()

	service.Handler().ServeHTTP(response, request)

	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "completed_execution_feed.cursor_regressed", body.Error.Code)
	require.False(t, body.Error.Retryable)
}

func TestHandlerFailsClosedOnRetainedSnapshotIntegrityMismatch(t *testing.T) {
	item := storedFeedItem(t, 5, "handler-integrity", time.Now().UTC())
	item.SnapshotSHA256 = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	service := newHandlerTestService(t, readerFunc(func(_ context.Context, _, _ int64, _ int64, _ int) (ReadWindow, error) {
		return ReadWindow{
			Items:         []StoredItem{item},
			HighWatermark: 5,
			Remaining:     1,
		}, nil
	}))
	cursor, err := service.codec.Encode(22, 0)
	require.NoError(t, err)
	request := completedFeedHandlerRequest(t, service, "?cursor="+cursor)
	response := httptest.NewRecorder()

	service.Handler().ServeHTTP(response, request)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	requireErrorCode(t, response, "completed_execution_feed.integrity_failure")
}

type readerFunc func(context.Context, int64, int64, int64, int) (ReadWindow, error)

func (read readerFunc) Read(ctx context.Context, orgID, projectID, after int64, limit int) (ReadWindow, error) {
	return read(ctx, orgID, projectID, after, limit)
}

func newHandlerTestService(t *testing.T, reader Reader) *Service {
	t.Helper()
	codec, err := NewCursorCodec("handler-test-secret", "handler-test-source")
	require.NoError(t, err)
	return &Service{
		reader:         reader,
		codec:          codec,
		sourceInstance: "handler-test-source",
		enabled:        true,
		now:            func() time.Time { return time.Now().UTC() },
		readSlots:      make(chan struct{}, maxConcurrentFeedRead),
	}
}

func completedFeedHandlerRequest(t *testing.T, _ *Service, query string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/integrations/xinsight/completed-executions"+query,
		nil,
	)
	ctx := context.WithValue(request.Context(), middleware.CtxOrgID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxProjectID, int64(22))
	return request.WithContext(ctx)
}

func storedFeedItem(t *testing.T, sequence int64, requestID string, terminalAt time.Time) StoredItem {
	t.Helper()
	_, payload, digest, err := BuildSnapshot(SnapshotInput{
		RequestID:            requestID,
		OrgID:                7,
		ProjectID:            22,
		TerminalStatus:       TerminalSettled,
		TerminalAvailableAt:  terminalAt.UTC(),
		Model:                "handler-model",
		PromptTokens:         5,
		UncachedPromptTokens: 5,
		CompletionTokens:     1,
		UsageSource:          "provider",
		UsageAuthority:       "authoritative",
		UsagePurpose:         "billing",
		UsageFinality:        "final",
		UsageContractVersion: "usage.v1",
		CacheUsageSchema:     "cache.v1",
		CacheUsageStatus:     "not_used",
		CalculatedCostMicros: 10,
		ChargedCostMicros:    10,
		PricingVersion:       "pricing.v1",
	})
	require.NoError(t, err)
	return StoredItem{
		Sequence:            sequence,
		RequestID:           requestID,
		TerminalStatus:      TerminalSettled,
		TerminalAvailableAt: terminalAt.UTC(),
		PublishedAt:         terminalAt.UTC(),
		Revision:            SnapshotRevision,
		Snapshot:            payload,
		SnapshotSHA256:      digest,
	}
}

func requireErrorCode(t *testing.T, response *httptest.ResponseRecorder, expected string) {
	t.Helper()
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, expected, body.Error.Code)
}
