package sourcefacts

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uniroute/unirouter/internal/contentcapture"
)

const ContentBodyPathPrefix = "/api/v1/integrations/xinsight/source/v2/contents/"
const contentBodyChunkCeiling int64 = 32 << 10

type ContentHTTPFailure struct {
	Status    int
	Code      string
	Retryable bool
}
type ContentReadState struct {
	HasProfile       bool // The intersection of current connection and immutable credential profiles.
	GlobalPaused     bool
	ContentPaused    bool
	CredentialStatus string
}

// DecideContentReadR2 applies only AFTER authentication, exact scope and IP
// checks. Pausing never makes a staged credential eligible for content reads.
func DecideContentReadR2(s ContentReadState) *ContentHTTPFailure {
	if !s.HasProfile {
		return &ContentHTTPFailure{http.StatusForbidden, "profile_not_authorized", false}
	}
	if s.GlobalPaused || s.ContentPaused {
		return &ContentHTTPFailure{http.StatusServiceUnavailable, "source_paused", true}
	}
	if s.CredentialStatus == "feed_staged" {
		return &ContentHTTPFailure{http.StatusConflict, "staged_credential_requires_pause", false}
	}
	if s.CredentialStatus != "feed_active" {
		return &ContentHTTPFailure{http.StatusUnauthorized, "unauthorized", false}
	}
	return nil
}

type ContentBodySelector struct{ RequestID, SourceFactID, Revision, ContentRef, Purpose string }
type ContentBodyGrant struct {
	SourceInstance, OrgID, ProjectID, ContentScopeRevision string
	RecoveryEpoch                                          int64
	State                                                  ContentReadState
	proof                                                  *contentBodyProof
}
type ContentBodyObject struct {
	Receipt  contentcapture.StoredReceipt
	Metadata ContentReadMetadata
}

// ContentBodyAuthority is an integration boundary, NOT an implementation of DB
// authorization. Production Authenticate must check current credentials, exact
// source/org/project and IP before returning R2 inputs. Lookup must authorize
// the exact selector and human_review purpose BEFORE looking up an object or
// exposing availability, and derive metadata from its immutable manifest.
// Reauthorize must recheck current key, IP, membership, purpose, lifecycle,
// global/local state, generation and recovery epoch with no long-lived DB lock.
// Its returned snapshot is checked again by the transport; a test stub cannot
// establish real database revocation or grant enforcement.
type ContentBodyAuthority interface {
	Authenticate(context.Context, *http.Request) (ContentBodyGrant, *ContentHTTPFailure)
	Lookup(context.Context, ContentBodyGrant, ContentBodySelector) (ContentBodyObject, *ContentHTTPFailure)
	Reauthorize(context.Context, *http.Request, ContentBodyGrant, ContentBodySelector) (ContentBodyGrant, *ContentHTTPFailure)
}
type ContentBodyHandlerConfig struct {
	Store              *contentcapture.FileStore
	Authority          ContentBodyAuthority
	MaxReadBytes       int64
	MaxBufferBytes     int64
	MaxConcurrentReads int
	RequestTimeout     time.Duration
	WriteTimeout       time.Duration
}
type contentBodyHandler struct {
	config ContentBodyHandlerConfig
	slots  chan struct{}
}

// NewContentBodyHandler creates an unregistered transport primitive. The caller
// must provide a real authority and a response writer supporting write deadlines,
// exclude compression middleware, and operate the store as immutable inodes.
func NewContentBodyHandler(c ContentBodyHandlerConfig) (http.Handler, error) {
	if c.Authority == nil || c.MaxReadBytes <= 0 || c.MaxReadBytes > contentMaxSafeInteger || c.MaxBufferBytes <= 0 || c.MaxBufferBytes > contentMaxSafeInteger || c.MaxConcurrentReads <= 0 || c.MaxConcurrentReads > 1024 || c.RequestTimeout <= 0 || c.WriteTimeout <= 0 {
		return nil, errors.New("content body handler requires authority and finite byte, concurrency and deadline configuration")
	}
	return &contentBodyHandler{c, make(chan struct{}, c.MaxConcurrentReads)}, nil
}
func parseContentBodySelector(r *http.Request) (ContentBodySelector, bool) {
	var s ContentBodySelector
	if !strings.HasPrefix(r.URL.Path, ContentBodyPathPrefix) {
		return s, false
	}
	s.ContentRef = strings.TrimPrefix(r.URL.Path, ContentBodyPathPrefix)
	if !contentRefPattern.MatchString(s.ContentRef) {
		return s, false
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(q) != 4 {
		return s, false
	}
	for key, values := range q {
		if len(values) != 1 || !oneOf(key, "request_id", "source_fact_id", "revision", "purpose") {
			return s, false
		}
	}
	s.RequestID = q.Get("request_id")
	s.SourceFactID = q.Get("source_fact_id")
	s.Revision = q.Get("revision")
	s.Purpose = q.Get("purpose")
	return s, contentText(s.RequestID, 64) && contentFactPattern.MatchString(s.SourceFactID) && positiveID(s.Revision) && s.Purpose == ContentHumanReview
}
func validContentBodyGrant(g ContentBodyGrant) bool {
	return contentText(g.SourceInstance, 128) && positiveID(g.OrgID) && positiveID(g.ProjectID) && contentText(g.ContentScopeRevision, 128) && g.RecoveryEpoch > 0
}
func contentBodyUnavailable() *ContentHTTPFailure {
	return &ContentHTTPFailure{503, "source_unavailable", true}
}
func contentBodyFailure(w http.ResponseWriter, f *ContentHTTPFailure) {
	if f == nil || f.Status < 400 || f.Status > 599 || !contentProtocolPattern.MatchString(f.Code) {
		f = contentBodyUnavailable()
	}
	writeError(w, f.Status, f.Code, f.Retryable)
}
func (h *contentBodyHandler) recheck(r *http.Request, g ContentBodyGrant, s ContentBodySelector, expires time.Time) *ContentHTTPFailure {
	if r.Context().Err() != nil {
		return contentBodyUnavailable()
	}
	current, failure := h.config.Authority.Reauthorize(r.Context(), r, g, s)
	if failure != nil {
		return failure
	}
	if failure := DecideContentReadR2(current.State); failure != nil {
		return failure
	}
	if !validContentBodyGrant(current) {
		return contentBodyUnavailable()
	}
	if current.SourceInstance != g.SourceInstance || current.OrgID != g.OrgID || current.ProjectID != g.ProjectID {
		return &ContentHTTPFailure{404, "not_found", false}
	}
	if current.ContentScopeRevision != g.ContentScopeRevision {
		return &ContentHTTPFailure{409, "content_scope_changed", false}
	}
	if current.RecoveryEpoch != g.RecoveryEpoch {
		return &ContentHTTPFailure{409, "source_epoch_changed", false}
	}
	if !time.Now().Before(expires) {
		return &ContentHTTPFailure{410, "content_expired", false}
	}
	return nil
}
func (h *contentBodyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		contentBodyFailure(w, &ContentHTTPFailure{405, "method_not_allowed", false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.config.RequestTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	selector, ok := parseContentBodySelector(r)
	if !ok {
		contentBodyFailure(w, &ContentHTTPFailure{400, "invalid_request", false})
		return
	}
	for name := range r.Header {
		if strings.EqualFold(name, "Range") {
			contentBodyFailure(w, &ContentHTTPFailure{400, "invalid_request", false})
			return
		}
	}
	grant, failure := h.config.Authority.Authenticate(r.Context(), r)
	if failure != nil {
		contentBodyFailure(w, failure)
		return
	}
	if failure := DecideContentReadR2(grant.State); failure != nil {
		contentBodyFailure(w, failure)
		return
	}
	if !validContentBodyGrant(grant) {
		contentBodyFailure(w, contentBodyUnavailable())
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		contentBodyFailure(w, contentBodyUnavailable())
		return
	}
	object, failure := h.config.Authority.Lookup(r.Context(), grant, selector)
	if failure != nil {
		contentBodyFailure(w, failure)
		return
	}
	m := object.Metadata
	if m.Validate() != nil || m.SourceInstance != grant.SourceInstance || m.OrgID != grant.OrgID || m.ProjectID != grant.ProjectID || m.RequestID != selector.RequestID || m.SourceFactID != selector.SourceFactID || m.Revision != selector.Revision || m.ContentRef != selector.ContentRef || object.Receipt.SavedBytes != m.Representation.SavedBytes || object.Receipt.SHA256 != m.Representation.Digest {
		contentBodyFailure(w, &ContentHTTPFailure{503, "representation_mismatch", false})
		return
	}
	expires, _ := time.Parse(time.RFC3339Nano, m.ExpiresAt)
	if !time.Now().Before(expires) {
		contentBodyFailure(w, &ContentHTTPFailure{410, "content_expired", false})
		return
	}
	if object.Receipt.SavedBytes > h.config.MaxReadBytes {
		contentBodyFailure(w, contentBodyUnavailable())
		return
	}
	limits := contentcapture.StoreLimits{MaxSavedBytes: h.config.MaxReadBytes, MaxReadBytes: h.config.MaxReadBytes, MaxBufferBytes: h.config.MaxBufferBytes}
	if h.config.Store == nil {
		contentBodyFailure(w, contentBodyUnavailable())
		return
	}
	file, err := h.config.Store.OpenVerified(r.Context(), object.Receipt, limits)
	if err != nil {
		code, retry := "content_corrupt", false
		if errors.Is(err, os.ErrNotExist) {
			code = "content_missing"
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			code, retry = "source_unavailable", true
		}
		contentBodyFailure(w, &ContentHTTPFailure{503, code, retry})
		return
	}
	defer file.Close()
	metadataHeader, err := EncodeContentReadMetadataHeader(m)
	if err != nil {
		contentBodyFailure(w, &ContentHTTPFailure{503, "representation_mismatch", false})
		return
	}
	controller := http.NewResponseController(w)
	// Serialize deadline resets with cancellation. A cancellation callback cannot
	// outlive this handler and later modify a reused response writer.
	var deadlineMu sync.Mutex
	setDeadline := func() error {
		deadlineMu.Lock()
		defer deadlineMu.Unlock()
		if err := r.Context().Err(); err != nil {
			return err
		}
		deadline := time.Now().Add(h.config.WriteTimeout)
		if requestDeadline, ok := r.Context().Deadline(); ok && requestDeadline.Before(deadline) {
			deadline = requestDeadline
		}
		return controller.SetWriteDeadline(deadline)
	}
	if err := setDeadline(); err != nil {
		contentBodyFailure(w, contentBodyUnavailable())
		return
	}
	callbackDone := make(chan struct{})
	stop := context.AfterFunc(r.Context(), func() {
		defer close(callbackDone)
		deadlineMu.Lock()
		defer deadlineMu.Unlock()
		_ = controller.SetWriteDeadline(time.Now())
	})
	defer func() {
		if !stop() {
			<-callbackDone
		}
		_ = controller.SetWriteDeadline(time.Time{})
	}()
	// Prepare the first bounded block before committing success headers; the
	// final recheck immediately precedes each actual write (including the first).
	buffer := make([]byte, int(min(h.config.MaxBufferBytes, contentBodyChunkCeiling, h.config.MaxReadBytes)))
	remaining := m.Representation.SavedBytes
	started := false
	for remaining > 0 {
		n := int(min(int64(len(buffer)), remaining))
		if _, err := io.ReadFull(file, buffer[:n]); err != nil {
			if started {
				panic(http.ErrAbortHandler)
			}
			contentBodyFailure(w, &ContentHTTPFailure{503, "content_corrupt", false})
			return
		}
		if failure := h.recheck(r, grant, selector, expires); failure != nil {
			if started {
				panic(http.ErrAbortHandler)
			}
			contentBodyFailure(w, failure)
			return
		}
		if err := setDeadline(); err != nil {
			if started {
				panic(http.ErrAbortHandler)
			}
			contentBodyFailure(w, contentBodyUnavailable())
			return
		}
		if !started {
			writeContentBodyHeaders(w, m.Representation.SavedBytes, metadataHeader)
			started = true
		}
		written, err := w.Write(buffer[:n])
		if err != nil || written != n {
			panic(http.ErrAbortHandler)
		}
		// Force net/http's buffered bytes through while this chunk's deadline
		// and cancellation hook are still active, rather than after return.
		if err := controller.Flush(); err != nil {
			panic(http.ErrAbortHandler)
		}
		remaining -= int64(n)
	}
	if !started { // An authenticated, verified zero-byte object is a 200 response.
		if failure := h.recheck(r, grant, selector, expires); failure != nil {
			contentBodyFailure(w, failure)
			return
		}
		if err := setDeadline(); err != nil {
			contentBodyFailure(w, contentBodyUnavailable())
			return
		}
		writeContentBodyHeaders(w, 0, metadataHeader)
		if err := controller.Flush(); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
}
func writeContentBodyHeaders(w http.ResponseWriter, size int64, metadata string) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="content.bin"`)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("X-UR-Content-Metadata", metadata)
	w.Header().Del("Content-Encoding")
	w.WriteHeader(http.StatusOK)
}
