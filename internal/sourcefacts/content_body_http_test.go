package sourcefacts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/executioncontext"
)

// These are transport tests using explicit stubs, never production authorization
// or database revocation evidence. No server, socket, PG, or credentials are used.
type stubContentAuthority struct {
	onLookup                   func()
	onContext                  func(string, context.Context) *ContentHTTPFailure
	grant                      ContentBodyGrant
	object                     ContentBodyObject
	events                     []string
	authFailure, lookupFailure *ContentHTTPFailure
	recheck                    func(int, ContentBodyGrant) (ContentBodyGrant, *ContentHTTPFailure)
	checks                     int
}

func (a *stubContentAuthority) Authenticate(ctx context.Context, _ *http.Request) (ContentBodyGrant, *ContentHTTPFailure) {
	a.events = append(a.events, "authenticate")
	if a.onContext != nil {
		if failure := a.onContext("authenticate", ctx); failure != nil {
			return ContentBodyGrant{}, failure
		}
	}
	return a.grant, a.authFailure
}
func (a *stubContentAuthority) Lookup(ctx context.Context, _ ContentBodyGrant, _ ContentBodySelector) (ContentBodyObject, *ContentHTTPFailure) {
	a.events = append(a.events, "lookup")
	if a.onContext != nil {
		if failure := a.onContext("lookup", ctx); failure != nil {
			return ContentBodyObject{}, failure
		}
	}
	if a.onLookup != nil {
		a.onLookup()
	}
	return a.object, a.lookupFailure
}
func (a *stubContentAuthority) Reauthorize(ctx context.Context, _ *http.Request, g ContentBodyGrant, _ ContentBodySelector) (ContentBodyGrant, *ContentHTTPFailure) {
	a.events = append(a.events, "reauthorize")
	a.checks++
	if a.onContext != nil {
		if failure := a.onContext("reauthorize", ctx); failure != nil {
			return ContentBodyGrant{}, failure
		}
	}
	if a.recheck != nil {
		return a.recheck(a.checks, g)
	}
	return g, nil
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	mu          sync.Mutex
	deadline    time.Time
	changed     chan struct{}
	writes      int
	chunks      []int
	beforeWrite func()
	block       bool
}

func newDeadlineRecorder() *deadlineRecorder {
	return &deadlineRecorder{ResponseRecorder: httptest.NewRecorder(), changed: make(chan struct{}, 1)}
}
func (w *deadlineRecorder) SetWriteDeadline(d time.Time) error {
	w.mu.Lock()
	w.deadline = d
	w.mu.Unlock()
	select {
	case w.changed <- struct{}{}:
	default:
	}
	return nil
}
func (w *deadlineRecorder) Write(p []byte) (int, error) {
	w.writes++
	w.chunks = append(w.chunks, len(p))
	if w.beforeWrite != nil {
		w.beforeWrite()
	}
	if !w.block {
		return w.ResponseRecorder.Write(p)
	}
	for {
		w.mu.Lock()
		d := w.deadline
		w.mu.Unlock()
		if d.IsZero() {
			return 0, errors.New("missing write deadline")
		}
		remaining := time.Until(d)
		if remaining <= 0 {
			return 0, os.ErrDeadlineExceeded
		}
		timer := time.NewTimer(remaining)
		select {
		case <-timer.C:
			return 0, os.ErrDeadlineExceeded
		case <-w.changed:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}
func serveContentRecorder(h http.Handler, w http.ResponseWriter, r *http.Request) (aborted bool) {
	defer func() {
		if p := recover(); p != nil {
			if p != http.ErrAbortHandler {
				panic(p)
			}
			aborted = true
		}
	}()
	h.ServeHTTP(w, r)
	return false
}
func bodyHTTPFixture(t *testing.T, body []byte) (ContentBodyHandlerConfig, *stubContentAuthority, *http.Request, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binding, err := contentcapture.InitializeFileStore(root, "synthetic-http-store")
	if err != nil {
		t.Fatal(err)
	}
	store, err := contentcapture.OpenFileStore(root, binding)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	receipt, err := store.Save(t.Context(), bytes.NewReader(body), contentcapture.StoreLimits{MaxSavedBytes: 1024, MaxReadBytes: 1024, MaxBufferBytes: 2})
	if err != nil {
		t.Fatal(err)
	}
	manifest := syntheticContentManifest(body)
	manifest.ExpiresAt = executioncontext.Time(time.Now().Add(time.Hour))
	metadata, err := manifest.ReadMetadata()
	if err != nil {
		t.Fatal(err)
	}
	a := &stubContentAuthority{grant: ContentBodyGrant{SourceInstance: manifest.SourceInstance, OrgID: manifest.OrgID, ProjectID: manifest.ProjectID, ContentScopeRevision: "1", RecoveryEpoch: 1, State: ContentReadState{HasProfile: true, CredentialStatus: "feed_active"}}, object: ContentBodyObject{Receipt: receipt, Metadata: metadata}}
	q := url.Values{"request_id": {manifest.RequestID}, "source_fact_id": {manifest.SourceFactID}, "revision": {manifest.Revision}, "purpose": {ContentHumanReview}}
	request := httptest.NewRequest(http.MethodGet, ContentBodyPathPrefix+manifest.ContentRef+"?"+q.Encode(), nil)
	return ContentBodyHandlerConfig{Store: store, Authority: a, MaxReadBytes: 1024, MaxBufferBytes: 2, MaxConcurrentReads: 1, RequestTimeout: time.Second, WriteTimeout: time.Second}, a, request, root
}
func mustBodyHandler(t *testing.T, c ContentBodyHandlerConfig) http.Handler {
	t.Helper()
	h, err := NewContentBodyHandler(c)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func bodyErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Error.Code
}
func TestContentBodyR2Ordering(t *testing.T) {
	for _, tc := range []struct {
		name   string
		state  ContentReadState
		status int
		code   string
		retry  bool
	}{
		{"missing_before_pause_and_staged", ContentReadState{GlobalPaused: true, ContentPaused: true, CredentialStatus: "feed_staged"}, 403, "profile_not_authorized", false},
		{"global_pause_before_staged", ContentReadState{HasProfile: true, GlobalPaused: true, CredentialStatus: "feed_staged"}, 503, "source_paused", true},
		{"local_pause_before_staged", ContentReadState{HasProfile: true, ContentPaused: true, CredentialStatus: "feed_staged"}, 503, "source_paused", true},
		{"staged", ContentReadState{HasProfile: true, CredentialStatus: "feed_staged"}, 409, "staged_credential_requires_pause", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := DecideContentReadR2(tc.state)
			if f == nil || f.Status != tc.status || f.Code != tc.code || f.Retryable != tc.retry {
				t.Fatalf("R2: %+v", f)
			}
			c, a, r, _ := bodyHTTPFixture(t, []byte("hidden"))
			a.grant.State = tc.state
			w := newDeadlineRecorder()
			serveContentRecorder(mustBodyHandler(t, c), w, r)
			if w.Code != tc.status || bodyErrorCode(t, w.ResponseRecorder) != tc.code {
				t.Fatal("wrong error mapping")
			}
			if strings.Join(a.events, ",") != "authenticate" || w.Header().Get("X-UR-Content-Metadata") != "" {
				t.Fatal("authorization failure exposed lookup or metadata")
			}
			if tc.retry && w.Header().Get("Retry-After") != "1" {
				t.Fatal("missing inherited retry hint")
			}
		})
	}
	if DecideContentReadR2(ContentReadState{HasProfile: true, CredentialStatus: "feed_active"}) != nil {
		t.Fatal("active denied")
	}
}
func TestContentBodyZeroAndBinaryVerifiedDelivery(t *testing.T) {
	for _, body := range [][]byte{{}, {0, 255, 1, 0, 2}} {
		c, a, r, _ := bodyHTTPFixture(t, body)
		w := newDeadlineRecorder()
		w.beforeWrite = func() { a.events = append(a.events, "write") }
		if serveContentRecorder(mustBodyHandler(t, c), w, r) {
			t.Fatal("unexpected abort")
		}
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), body) {
			t.Fatal("raw body changed")
		}
		for k, v := range map[string]string{"Content-Type": "application/octet-stream", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "Content-Disposition": `attachment; filename="content.bin"`, "Content-Length": strconv.Itoa(len(body)), "Content-Encoding": ""} {
			if w.Header().Get(k) != v {
				t.Fatalf("%s=%q", k, w.Header().Get(k))
			}
		}
		decoded, err := DecodeContentReadMetadataHeader(w.Header().Get("X-UR-Content-Metadata"))
		if err != nil || decoded.SourceSHA256 != a.object.Metadata.SourceSHA256 {
			t.Fatalf("metadata: %v", err)
		}
		for _, n := range w.chunks {
			if n > 2 {
				t.Fatal("oversized chunk")
			}
		}
		if len(body) == 0 {
			if a.checks != 1 || w.writes != 0 {
				t.Fatal("zero byte success must reauthorize without synthesizing bytes")
			}
		} else {
			want := "authenticate,lookup,reauthorize,write,reauthorize,write,reauthorize,write"
			if strings.Join(a.events, ",") != want {
				t.Fatalf("order: %v", a.events)
			}
		}
		w.mu.Lock()
		cleared := w.deadline.IsZero()
		w.mu.Unlock()
		if !cleared {
			t.Fatal("deadline leaked past handler")
		}
	}
}
func TestContentBodyStrictSelectorAndRange(t *testing.T) {
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Set("Range", "bytes=0-1") }, func(r *http.Request) { r.Header["range"] = []string{""} }, func(r *http.Request) { r.URL.RawQuery += "&purpose=human_review" }, func(r *http.Request) { r.URL.RawQuery += "&extra=1" }, func(r *http.Request) { r.URL.RawQuery = strings.Replace(r.URL.RawQuery, "human_review", "export", 1) }, func(r *http.Request) { r.URL.RawQuery += "&bad=%xx" }} {
		c, a, r, _ := bodyHTTPFixture(t, nil)
		mutate(r)
		w := newDeadlineRecorder()
		serveContentRecorder(mustBodyHandler(t, c), w, r)
		if w.Code != 400 || len(a.events) != 0 {
			t.Fatalf("selector did work: %d %v", w.Code, a.events)
		}
	}
}
func TestContentBodyAuthenticationAndPrewriteRecheckDoNotLeak(t *testing.T) {
	c, a, r, _ := bodyHTTPFixture(t, []byte("body"))
	a.authFailure = &ContentHTTPFailure{401, "unauthorized", false}
	w := newDeadlineRecorder()
	serveContentRecorder(mustBodyHandler(t, c), w, r)
	if w.Code != 401 || strings.Join(a.events, ",") != "authenticate" {
		t.Fatal("looked up unauthenticated object")
	}
	for _, failure := range []*ContentHTTPFailure{{403, "profile_not_authorized", false}, {503, "source_paused", true}, {409, "staged_credential_requires_pause", false}, {410, "content_expired", false}} {
		c, a, r, _ := bodyHTTPFixture(t, []byte("body"))
		a.recheck = func(_ int, g ContentBodyGrant) (ContentBodyGrant, *ContentHTTPFailure) { return g, failure }
		w := newDeadlineRecorder()
		serveContentRecorder(mustBodyHandler(t, c), w, r)
		if w.Code != failure.Status || bodyErrorCode(t, w.ResponseRecorder) != failure.Code || w.writes != 1 || w.Header().Get("X-UR-Content-Metadata") != "" {
			t.Fatalf("lost exact prewrite error: %d %s", w.Code, w.Body.String())
		}
	}
}
func TestContentBodyTamperMissingAndBindingFailBeforeSuccess(t *testing.T) {
	for _, kind := range []string{"tampered", "missing", "scope", "receipt", "expired"} {
		t.Run(kind, func(t *testing.T) {
			c, a, r, root := bodyHTTPFixture(t, []byte("good"))
			switch kind {
			case "tampered":
				if err := os.WriteFile(filepath.Join(root, a.object.Receipt.ObjectName), []byte("goXX"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Rename(filepath.Join(root, a.object.Receipt.ObjectName), filepath.Join(root, "held-object")); err != nil {
					t.Fatal(err)
				}
			case "scope":
				a.object.Metadata.ProjectID = "99"
			case "receipt":
				a.object.Receipt.SavedBytes++
			case "expired":
				a.object.Metadata.ExpiresAt = "2020-01-01T00:00:00.000000Z"
			}
			w := newDeadlineRecorder()
			serveContentRecorder(mustBodyHandler(t, c), w, r)
			want := "content_corrupt"
			if kind == "missing" {
				want = "content_missing"
			}
			if kind == "scope" || kind == "receipt" {
				want = "representation_mismatch"
			}
			if kind == "expired" {
				want = "content_expired"
			}
			if bodyErrorCode(t, w.ResponseRecorder) != want || w.Header().Get("X-UR-Content-Metadata") != "" || a.checks != 0 {
				t.Fatalf("unexpected prewrite result: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestContentBodyMidstreamRevokeAbortsAndReleasesPermit(t *testing.T) {
	c, a, r, _ := bodyHTTPFixture(t, []byte("abcdef"))
	a.recheck = func(n int, g ContentBodyGrant) (ContentBodyGrant, *ContentHTTPFailure) {
		if n == 2 {
			return g, &ContentHTTPFailure{403, "profile_not_authorized", false}
		}
		return g, nil
	}
	h := mustBodyHandler(t, c)
	w := newDeadlineRecorder()
	if !serveContentRecorder(h, w, r) {
		t.Fatal("revoked stream not aborted")
	}
	if w.Code != 200 || w.Body.String() != "ab" || strings.Contains(w.Body.String(), "error") {
		t.Fatal("JSON or extra bytes appended after revocation")
	}
	a.recheck = nil
	a.checks = 0
	again := newDeadlineRecorder()
	if serveContentRecorder(h, again, r) || again.Body.String() != "abcdef" {
		t.Fatal("read slot leaked on abort")
	}
}
func TestContentBodySnapshotChangesFailExplicitly(t *testing.T) {
	for _, tc := range []struct {
		change func(*ContentBodyGrant)
		code   string
	}{{func(g *ContentBodyGrant) { g.ContentScopeRevision = "2" }, "content_scope_changed"}, {func(g *ContentBodyGrant) { g.RecoveryEpoch++ }, "source_epoch_changed"}, {func(g *ContentBodyGrant) { g.State.ContentPaused = true }, "source_paused"}} {
		c, a, r, _ := bodyHTTPFixture(t, []byte("body"))
		a.recheck = func(_ int, g ContentBodyGrant) (ContentBodyGrant, *ContentHTTPFailure) { tc.change(&g); return g, nil }
		w := newDeadlineRecorder()
		serveContentRecorder(mustBodyHandler(t, c), w, r)
		if bodyErrorCode(t, w.ResponseRecorder) != tc.code {
			t.Fatal("changed snapshot silently accepted")
		}
	}
}
func TestContentBodyBlockedWriteDeadlineAndCancellation(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		c, _, r, _ := bodyHTTPFixture(t, []byte("body"))
		c.WriteTimeout = 30 * time.Millisecond
		if cancelEarly {
			c.WriteTimeout = time.Hour
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		r = r.WithContext(ctx)
		w := newDeadlineRecorder()
		w.block = true
		if cancelEarly {
			w.beforeWrite = cancel
		}
		start := time.Now()
		if !serveContentRecorder(mustBodyHandler(t, c), w, r) {
			t.Fatal("blocked write did not abort")
		}
		if time.Since(start) > time.Second {
			t.Fatal("write cancellation/deadline was not bounded")
		}
		if w.Body.Len() != 0 || w.writes != 1 {
			t.Fatal("wrote error JSON after blocked body")
		}
		w.mu.Lock()
		cleared := w.deadline.IsZero()
		w.mu.Unlock()
		if !cleared {
			t.Fatal("cancellation callback leaked deadline")
		}
	}
}
func TestContentBodyFiniteLimitsAndUnsupportedWriter(t *testing.T) {
	c, a, r, _ := bodyHTTPFixture(t, []byte("body"))
	for _, change := range []func(*ContentBodyHandlerConfig){func(c *ContentBodyHandlerConfig) { c.MaxReadBytes = 0 }, func(c *ContentBodyHandlerConfig) { c.MaxBufferBytes = 0 }, func(c *ContentBodyHandlerConfig) { c.MaxConcurrentReads = 0 }, func(c *ContentBodyHandlerConfig) { c.RequestTimeout = 0 }, func(c *ContentBodyHandlerConfig) { c.RequestTimeout = -time.Second }, func(c *ContentBodyHandlerConfig) { c.WriteTimeout = 0 }} {
		bad := c
		change(&bad)
		if _, err := NewContentBodyHandler(bad); err == nil {
			t.Fatal("accepted unbounded configuration")
		}
	}
	w := httptest.NewRecorder()
	serveContentRecorder(mustBodyHandler(t, c), w, r)
	if w.Code != 503 || a.checks != 0 {
		t.Fatal("writer without deadlines was admitted")
	}
	c.MaxReadBytes = 2
	w = httptest.NewRecorder()
	serveContentRecorder(mustBodyHandler(t, c), w, r)
	if w.Code != 503 {
		t.Fatal("read beyond configured max")
	}
}

func TestContentBodyUnconfiguredStorePreservesAuthorityAndExpiry(t *testing.T) {
	for _, kind := range []string{"authorized", "denied", "staged", "lookup", "expired", "selector", "range"} {
		t.Run(kind, func(t *testing.T) {
			c, a, r, _ := bodyHTTPFixture(t, []byte("retained"))
			c.Store = nil
			want := "source_unavailable"
			switch kind {
			case "denied":
				a.authFailure = &ContentHTTPFailure{401, "unauthorized", false}
				want = "unauthorized"
			case "staged":
				a.grant.State.CredentialStatus = "feed_staged"
				want = "staged_credential_requires_pause"
			case "lookup":
				a.lookupFailure = &ContentHTTPFailure{404, "not_found", false}
				want = "not_found"
			case "expired":
				a.object.Metadata.ExpiresAt = executioncontext.Time(time.Now().Add(-time.Second))
				want = "content_expired"
			case "selector":
				r.URL.RawQuery = ""
				want = "invalid_request"
			case "range":
				r.Header.Set("Range", "bytes=0-1")
				want = "invalid_request"
			}
			w := httptest.NewRecorder()
			mustBodyHandler(t, c).ServeHTTP(w, r)
			if code := bodyErrorCode(t, w); code != want {
				t.Fatalf("got %s want %s", code, want)
			}
			if (kind == "selector" || kind == "range") && len(a.events) != 0 {
				t.Fatal("selector/range ordering changed")
			}
			if (kind == "denied" || kind == "staged") && len(a.events) != 1 {
				t.Fatal("read proceeded after auth rejection")
			}
		})
	}
}

func TestContentBodyPermitRejectsBeforeLookupAndReleasesOnLookupFailure(t *testing.T) {
	c, a, r, _ := bodyHTTPFixture(t, []byte("body"))
	h := mustBodyHandler(t, c)
	a.lookupFailure = &ContentHTTPFailure{503, "content_pending", true}
	first := newDeadlineRecorder()
	serveContentRecorder(h, first, r)
	if first.Code != 503 || bodyErrorCode(t, first.ResponseRecorder) != "content_pending" {
		t.Fatal("lookup failure lost")
	}
	a.lookupFailure = nil
	a.events = nil
	a.onLookup = func() {
		nested := newDeadlineRecorder()
		serveContentRecorder(h, nested, r)
		if nested.Code != 503 || nested.Header().Get("X-UR-Content-Metadata") != "" {
			t.Fatal("busy reader admitted")
		}
	}
	second := newDeadlineRecorder()
	if serveContentRecorder(h, second, r) || second.Body.String() != "body" {
		t.Fatal("lookup failure leaked permit")
	}
	lookups := 0
	for _, event := range a.events {
		if event == "lookup" {
			lookups++
		}
	}
	if lookups != 1 {
		t.Fatal("capacity rejection performed an object lookup")
	}
}

func waitContentRequestDeadline(t *testing.T, ctx context.Context) {
	t.Helper()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("operation received no request deadline")
	}
	// The watchdog fails a broken transport without waiting for the package timeout.
	watchdog := time.NewTimer(time.Second)
	defer watchdog.Stop()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("unexpected request cancellation: %v", ctx.Err())
		}
	case <-watchdog.C:
		t.Fatal("operation outlived the configured request deadline")
	}
}

func TestContentBodyRequestTimeoutDuringAuthority(t *testing.T) {
	for _, stage := range []string{"authenticate", "lookup"} {
		t.Run(stage, func(t *testing.T) {
			c, a, r, _ := bodyHTTPFixture(t, []byte("body"))
			c.RequestTimeout = 25 * time.Millisecond
			a.onContext = func(current string, ctx context.Context) *ContentHTTPFailure {
				if current == stage {
					waitContentRequestDeadline(t, ctx)
					return contentBodyUnavailable()
				}
				return nil
			}
			h := mustBodyHandler(t, c)
			w := newDeadlineRecorder()
			if serveContentRecorder(h, w, r) || w.Code != 503 || bodyErrorCode(t, w.ResponseRecorder) != "source_unavailable" || w.Header().Get("X-UR-Content-Metadata") != "" || a.checks != 0 {
				t.Fatal("timed-out authority exposed a successful representation")
			}
			a.onContext = nil
			again := newDeadlineRecorder()
			if serveContentRecorder(h, again, r) || again.Body.String() != "body" {
				t.Fatal("authority timeout leaked the read permit")
			}
		})
	}
}

func TestContentBodyShorterParentDeadlineAndReturnCleanup(t *testing.T) {
	c, a, r, _ := bodyHTTPFixture(t, []byte("body"))
	c.RequestTimeout, c.WriteTimeout = time.Hour, time.Hour
	parentDeadline := time.Now().Add(time.Minute)
	parent, cancel := context.WithDeadline(r.Context(), parentDeadline)
	defer cancel()
	r = r.WithContext(parent)
	var observed context.Context
	a.onContext = func(_ string, ctx context.Context) *ContentHTTPFailure {
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.Equal(parentDeadline) {
			t.Fatal("shorter parent deadline was not inherited")
		}
		observed = ctx
		return nil
	}
	w := newDeadlineRecorder()
	w.beforeWrite = func() {
		w.mu.Lock()
		deadline := w.deadline
		w.mu.Unlock()
		if !deadline.Equal(parentDeadline) {
			t.Fatal("body write extended the parent deadline")
		}
	}
	if serveContentRecorder(mustBodyHandler(t, c), w, r) || w.Body.String() != "body" {
		t.Fatal("bounded body delivery failed")
	}
	if observed == nil || !errors.Is(observed.Err(), context.Canceled) || parent.Err() != nil {
		t.Fatal("handler did not cancel its own context independently on return")
	}
}

func TestContentBodyRequestTimeoutBoundsBlockedWrite(t *testing.T) {
	c, a, r, _ := bodyHTTPFixture(t, []byte("body"))
	c.RequestTimeout, c.WriteTimeout = 25*time.Millisecond, time.Hour
	var requestDeadline time.Time
	a.onContext = func(_ string, ctx context.Context) *ContentHTTPFailure {
		var ok bool
		requestDeadline, ok = ctx.Deadline()
		if !ok {
			t.Fatal("missing request deadline")
		}
		return nil
	}
	w := newDeadlineRecorder()
	w.block = true
	w.beforeWrite = func() {
		w.mu.Lock()
		deadline := w.deadline
		w.mu.Unlock()
		if !deadline.Equal(requestDeadline) {
			t.Fatal("blocked write was allowed past the request deadline")
		}
	}
	h := mustBodyHandler(t, c)
	if !serveContentRecorder(h, w, r) || w.Body.Len() != 0 || w.writes != 1 {
		t.Fatal("blocked write did not abort without appending error JSON")
	}
	w.mu.Lock()
	cleared := w.deadline.IsZero()
	w.mu.Unlock()
	if !cleared {
		t.Fatal("timeout callback leaked a write deadline")
	}
	again := newDeadlineRecorder()
	if serveContentRecorder(h, again, r) || again.Body.String() != "body" {
		t.Fatal("blocked write timeout leaked the read permit")
	}
}

func TestContentBodyRequestTimeoutDuringMidstreamRecheck(t *testing.T) {
	c, a, r, _ := bodyHTTPFixture(t, []byte("abcdef"))
	c.RequestTimeout = 25 * time.Millisecond
	a.onContext = func(stage string, ctx context.Context) *ContentHTTPFailure {
		if stage == "reauthorize" && a.checks == 2 {
			waitContentRequestDeadline(t, ctx)
			return contentBodyUnavailable()
		}
		return nil
	}
	h := mustBodyHandler(t, c)
	w := newDeadlineRecorder()
	if !serveContentRecorder(h, w, r) || w.Body.String() != "ab" || w.writes != 1 {
		t.Fatal("timed-out recheck wrote another block or error JSON")
	}
	a.onContext = nil
	a.checks = 0
	again := newDeadlineRecorder()
	if serveContentRecorder(h, again, r) || again.Body.String() != "abcdef" {
		t.Fatal("midstream timeout leaked the read permit")
	}
}
