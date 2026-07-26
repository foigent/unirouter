package completedfeed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/middleware"
)

const feedReadTimeout = 5 * time.Second

func (service *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			service.writeError(w, http.StatusMethodNotAllowed, "completed_execution_feed.method_not_allowed", "Only GET is supported", false, "method_not_allowed")
			return
		}
		if service == nil || !service.enabled || service.codec == nil || service.reader == nil {
			service.writeError(w, http.StatusServiceUnavailable, "completed_execution_feed.disabled", "Completed-execution feed is not active", true, "disabled")
			return
		}
		orgID := middleware.GetOrgIDFromContext(r.Context())
		projectID := middleware.GetProjectIDFromContext(r.Context())
		keyID := middleware.GetKeyIDFromContext(r.Context())
		apiKey := middleware.GetAPIKeyFromContext(r.Context())
		connectionStatus := ConnectionStatusActive
		if orgID <= 0 || projectID <= 0 {
			service.writeError(w, http.StatusForbidden, "completed_execution_feed.project_scope_required", "Project-scoped feed authorization is required", false, "project_scope_required")
			return
		}
		if service.access != nil {
			var err error
			connectionStatus, err = service.access.RecordAuthenticatedPull(r.Context(), orgID, projectID, keyID)
			if err != nil {
				w.Header().Set("Retry-After", "1")
				service.writeError(w, http.StatusServiceUnavailable, "completed_execution_feed.connection_unavailable", "Completed-execution feed connection state is temporarily unavailable", true, "connection_unavailable")
				return
			}
			if connectionStatus == ConnectionStatusActive &&
				(apiKey == nil || apiKey.Status != middleware.APIKeyStatusFeedActive) {
				service.writeError(w, http.StatusConflict, "completed_execution_feed.staged_credential_requires_pause", "Staged feed credentials cannot read while the connection is active", false, "staged_active_denied")
				return
			}
		}
		select {
		case service.readSlots <- struct{}{}:
			defer func() { <-service.readSlots }()
		default:
			w.Header().Set("Retry-After", "1")
			service.writeError(w, http.StatusServiceUnavailable, "completed_execution_feed.overloaded", "Completed-execution feed is temporarily overloaded", true, "overloaded")
			return
		}

		cursorValues, cursorPresent := r.URL.Query()["cursor"]
		limitValues := r.URL.Query()["limit"]
		for key := range r.URL.Query() {
			if key != "cursor" && key != "limit" {
				service.writeError(w, http.StatusBadRequest, "completed_execution_feed.invalid_query", "Unsupported query parameter", false, "invalid_query")
				return
			}
		}
		if len(cursorValues) > 1 || len(limitValues) > 1 {
			service.writeError(w, http.StatusBadRequest, "completed_execution_feed.invalid_query", "Query parameter must appear at most once", false, "invalid_query")
			return
		}
		limit := MaxPageItems
		if len(limitValues) == 1 && limitValues[0] != "" {
			parsed, err := strconv.Atoi(limitValues[0])
			if err != nil || parsed < 1 || parsed > MaxPageItems {
				service.writeError(w, http.StatusBadRequest, "completed_execution_feed.invalid_limit", "Limit must be between 1 and 50", false, "invalid_limit")
				return
			}
			limit = parsed
		}

		bootstrap := !cursorPresent || len(cursorValues) == 0 || cursorValues[0] == ""
		after := int64(0)
		if !bootstrap {
			var err error
			after, err = service.codec.Decode(cursorValues[0], projectID)
			if err != nil {
				service.writeError(w, http.StatusBadRequest, "completed_execution_feed.invalid_cursor", "Cursor is invalid", false, "invalid_cursor")
				return
			}
		}
		if bootstrap && connectionStatus == ConnectionStatusPaused {
			w.Header().Set("Retry-After", "5")
			service.writeError(w, http.StatusServiceUnavailable, "completed_execution_feed.connection_paused", "Completed-execution feed connection is paused", true, "connection_paused")
			return
		}

		readCtx := r.Context()
		if _, hasDeadline := readCtx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			readCtx, cancel = context.WithTimeout(readCtx, feedReadTimeout)
			defer cancel()
		}
		window, err := service.reader.Read(readCtx, orgID, projectID, after, limit+1)
		if err != nil {
			w.Header().Set("Retry-After", "1")
			service.writeError(w, http.StatusServiceUnavailable, "completed_execution_feed.source_unavailable", "Completed-execution feed is temporarily unavailable", true, "source_unavailable")
			return
		}
		if !bootstrap && after < window.RetainedAfter {
			service.writeError(w, http.StatusGone, "completed_execution_feed.cursor_expired", "Cursor is outside the retained replay window", false, "cursor_expired")
			return
		}
		if !bootstrap && after > window.HighWatermark {
			service.writeError(w, http.StatusConflict, "completed_execution_feed.cursor_regressed", "Cursor is ahead of the source high watermark", false, "cursor_regressed")
			return
		}
		if !bootstrap && service.access != nil {
			if err := service.access.Acknowledge(readCtx, orgID, projectID, keyID, after); err != nil {
				w.Header().Set("Retry-After", "1")
				service.writeError(w, http.StatusServiceUnavailable, "completed_execution_feed.acknowledgement_unavailable", "Receiver acknowledgement could not be recorded", true, "acknowledgement_unavailable")
				return
			}
		}
		if connectionStatus == ConnectionStatusPaused {
			w.Header().Set("Retry-After", "5")
			service.writeError(w, http.StatusServiceUnavailable, "completed_execution_feed.connection_paused", "Completed-execution feed connection is paused", true, "connection_paused")
			return
		}

		if bootstrap {
			nextCursor, encodeErr := service.codec.Encode(projectID, window.HighWatermark)
			if encodeErr != nil {
				service.writeError(w, http.StatusInternalServerError, "completed_execution_feed.cursor_failure", "Completed-execution feed cursor could not be produced", false, "cursor_failure")
				return
			}
			if !service.writePage(w, FeedPage{
				SchemaVersion:  PageSchemaVersion,
				SourceInstance: service.sourceInstance,
				GeneratedAt:    service.now().UTC(),
				Items:          []FeedItem{},
				NextCursor:     nextCursor,
				HighWatermark:  nextCursor,
				HasMore:        false,
			}) {
				return
			}
			feedCursorLag.Set(0)
			feedOldestReadyAge.Set(0)
			return
		}

		hasMore := len(window.Items) > limit
		var oldestRemaining *time.Time
		if hasMore {
			remainingPublishedAt := window.Items[limit].PublishedAt.UTC()
			oldestRemaining = &remainingPublishedAt
			window.Items = window.Items[:limit]
		}
		items := make([]FeedItem, 0, len(window.Items))
		nextSequence := after
		for _, stored := range window.Items {
			digest := sha256.Sum256(stored.Snapshot)
			if "sha256:"+hex.EncodeToString(digest[:]) != stored.SnapshotSHA256 {
				service.writeError(w, http.StatusInternalServerError, "completed_execution_feed.integrity_failure", "Retained completed-execution item failed integrity validation", false, "integrity_failure")
				return
			}
			snapshot, decodeErr := DecodeSnapshot(stored.Snapshot)
			if decodeErr != nil ||
				snapshot.RequestID != stored.RequestID ||
				snapshot.Terminal.Status != stored.TerminalStatus ||
				!canonicalTerminalAvailableAt(snapshot.Terminal.AvailableAt).
					Equal(canonicalTerminalAvailableAt(stored.TerminalAvailableAt)) ||
				stored.Revision != SnapshotRevision {
				service.writeError(w, http.StatusInternalServerError, "completed_execution_feed.integrity_failure", "Retained completed-execution item failed integrity validation", false, "integrity_failure")
				return
			}
			items = append(items, FeedItem{
				RequestID:           stored.RequestID,
				TerminalAvailableAt: stored.TerminalAvailableAt.UTC(),
				Revision:            SnapshotRevision,
				TimelineComplete:    true,
				SnapshotSHA256:      stored.SnapshotSHA256,
				Snapshot:            append(json.RawMessage(nil), stored.Snapshot...),
			})
			nextSequence = stored.Sequence
		}
		nextCursor, err := service.codec.Encode(projectID, nextSequence)
		if err != nil {
			service.writeError(w, http.StatusInternalServerError, "completed_execution_feed.cursor_failure", "Completed-execution feed cursor could not be produced", false, "cursor_failure")
			return
		}
		highWatermark, err := service.codec.Encode(projectID, window.HighWatermark)
		if err != nil {
			service.writeError(w, http.StatusInternalServerError, "completed_execution_feed.cursor_failure", "Completed-execution feed cursor could not be produced", false, "cursor_failure")
			return
		}
		if !service.writePage(w, FeedPage{
			SchemaVersion:  PageSchemaVersion,
			SourceInstance: service.sourceInstance,
			GeneratedAt:    service.now().UTC(),
			Items:          items,
			NextCursor:     nextCursor,
			HighWatermark:  highWatermark,
			HasMore:        hasMore,
		}) {
			return
		}
		remaining := window.Remaining - int64(len(items))
		if remaining < 0 {
			remaining = 0
		}
		feedCursorLag.Set(float64(remaining))
		if remaining == 0 || oldestRemaining == nil {
			feedOldestReadyAge.Set(0)
		} else {
			age := service.now().UTC().Sub(oldestRemaining.UTC()).Seconds()
			if age < 0 {
				age = 0
			}
			feedOldestReadyAge.Set(age)
		}
	})
}

func (service *Service) writePage(w http.ResponseWriter, page FeedPage) bool {
	payload, err := json.Marshal(page)
	if err != nil || len(payload) >= MaxPageBytes {
		service.writeError(w, http.StatusInternalServerError, "completed_execution_feed.page_contract_failure", "Completed-execution feed page could not be produced", false, "page_contract_failure")
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
	feedRequests.WithLabelValues("success").Inc()
	feedItems.Add(float64(len(page.Items)))
	feedBytes.Add(float64(len(payload)))
	return true
}

func (service *Service) writeError(w http.ResponseWriter, status int, code, message string, retryable bool, metricResult string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorBody{
			Code:      code,
			Message:   message,
			Retryable: retryable,
		},
	})
	feedRequests.WithLabelValues(metricResult).Inc()
}
