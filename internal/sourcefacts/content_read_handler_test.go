package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

type contentReadTestFixture struct {
	base         *contentAuthorityFixture
	handler      *contentReadHandler
	documents    []ContentManifest
	floor        int64
	sizeOverride int64
	transactions []*contentReadTestTx
	loaded       []int64
	objectReads  int
	corruptHash  bool
	healthReason string
	healthDetail []byte
}
type contentReadTestTx struct {
	contentAuthorityTestTx
	fixture *contentReadTestFixture
}

func (t *contentReadTestTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "FROM api_keys") {
		return t.contentAuthorityTestTx.Query(ctx, sql, args...)
	}
	if !strings.Contains(sql, "octet_length(document)") {
		return nil, errors.New("unexpected synthetic query")
	}
	rows := []contentAuthorityTestRow{}
	after, high, limit := args[2].(int64), args[3].(int64), args[4].(int)
	for i, m := range t.fixture.documents {
		sequence := int64(i + 1)
		if sequence <= after || sequence > high {
			continue
		}
		if len(rows) >= limit {
			break
		}
		wire, err := EncodeContentManifest(m)
		if err != nil {
			return nil, err
		}
		size := int64(len(wire))
		if t.fixture.sizeOverride != 0 {
			size = t.fixture.sizeOverride
		}
		hash := eventconnector.HashSourceBytes(wire)
		if t.fixture.corruptHash {
			hash = "sha256:" + strings.Repeat("0", 64)
		}
		revision, _ := strconv.ParseInt(m.Revision, 10, 64)
		rows = append(rows, contentAuthorityTestRow{values: []any{sequence, size, m.SourceFactID, revision, m.RequestID, hash}})
	}
	return &contentAuthorityTestRows{rows: rows, index: -1}, nil
}
func (t *contentReadTestTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f := t.fixture
	if strings.Contains(sql, "FROM execution_content_health") {
		if f.healthDetail != nil {
			return contentAuthorityTestRow{values: []any{f.healthReason, f.healthDetail}}
		}
		return contentAuthorityTestRow{err: pgx.ErrNoRows}
	}
	if strings.Contains(sql, "FROM execution_content_streams") {
		return contentAuthorityTestRow{values: []any{t.access.instance, f.floor, *t.access.coverageTime, t.access.high, t.access.epoch, time.Now().UTC()}}
	}
	if strings.HasPrefix(sql, "SELECT document FROM") {
		sequence := args[2].(int64)
		f.loaded = append(f.loaded, sequence)
		wire, err := EncodeContentManifest(f.documents[sequence-1])
		return contentAuthorityTestRow{values: []any{wire}, err: err}
	}
	if strings.Contains(sql, "FROM execution_content_objects") {
		f.objectReads++
		for _, m := range f.documents {
			if m.SourceFactID == args[2].(string) {
				expires, _ := time.Parse(time.RFC3339Nano, m.ExpiresAt)
				digest := m.Representation.Digest
				return contentAuthorityTestRow{values: []any{"synthetic-store", strings.Repeat("b", 64), strings.Repeat("c", 64), "ready", m.Representation.SavedBytes, &digest, expires}}
			}
		}
		return contentAuthorityTestRow{err: pgx.ErrNoRows}
	}
	return contentAuthorityTestRow{err: errors.New("unexpected synthetic row")}
}
func contentReadTestConfig(a ContentBodyAuthority) ContentReadHandlerConfig {
	return ContentReadHandlerConfig{Authority: a, SupportedProtocols: []string{"openai.chat"}, ObservationPoints: []string{"client_request"}, CaptureModes: []string{"disabled", "metadata_only"}, Limits: ContentLimits{MaxSavedBytes: 1024, MaxReadBytes: 1024, MaxBufferBytes: 7, MaxManifestBytes: 65536, MaxManifestItems: 10000}, MaxPageBytes: MaxPageBytes, MaxVerifyBytesPerPage: 1024, MaxConcurrentRequests: 2, RequestTimeout: time.Second, WriteTimeout: time.Second}
}
func newContentReadTestFixture(t *testing.T, count int) *contentReadTestFixture {
	t.Helper()
	f := &contentReadTestFixture{base: newContentAuthorityFixture(t)}
	f.base.access.high = int64(count)
	for i := 0; i < count; i++ {
		m := syntheticContentManifest(nil)
		m.RequestID = "request-" + strconv.Itoa(i)
		m.SourceFactID = m.FactID()
		m.EventID = m.EventIDForRevision()
		m.Capture.Completeness = "pending"
		m.Capture.Reason = "capture_in_progress"
		m.Representation = nil
		m.ContentRef = ""
		m.CapturedAt = ""
		m.ExpiresAt = ""
		f.documents = append(f.documents, m)
	}
	f.base.a.begin = func(context.Context) (pgx.Tx, error) {
		tx := &contentReadTestTx{fixture: f, contentAuthorityTestTx: contentAuthorityTestTx{access: f.base.access}}
		f.transactions = append(f.transactions, tx)
		return tx, nil
	}
	c := contentReadTestConfig(f.base.a)
	if _, err := NewContentReadHandlers(c); err != nil {
		t.Fatal(err)
	}
	f.handler = &contentReadHandler{config: c, authority: f.base.a, slots: make(chan struct{}, c.MaxConcurrentRequests)}
	return f
}
func (f *contentReadTestFixture) request(query string, page bool) *deadlineRecorder {
	r := f.base.request.Clone(context.Background())
	r.URL.Path = ContentManifestsPath
	r.URL.RawQuery = query
	if !page {
		r.URL.Path = ContentCapabilitiesPath
	}
	w := newDeadlineRecorder()
	f.handler.serve(w, r, page)
	return w
}
func readTestPage(t *testing.T, w *deadlineRecorder) ContentManifestPage {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	p, err := DecodeContentManifestPage(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestContentReadClosedQueryAndActualCapabilities(t *testing.T) {
	f := newContentReadTestFixture(t, 0)
	for _, query := range []string{"", "limit=1", "purpose=human_review", "purpose=human_review&limit=01", "purpose=human_review&limit=10001", "purpose=human_review&limit=1&cursor=", "purpose=human_review&limit=1&extra=x", "purpose=human_review&purpose=human_review&limit=1"} {
		w := f.request(query, true)
		if w.Code != 400 {
			t.Fatalf("accepted %q: %d", query, w.Code)
		}
	}
	for _, tx := range f.transactions {
		if len(tx.queries) != 1 || !strings.Contains(tx.queries[0], "FROM api_keys") {
			t.Fatal("invalid query read manifest state")
		}
	}
	f.transactions = nil
	w := f.request("", false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	c, err := DecodeContentCapabilities(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.CaptureModes, ",") != "disabled,metadata_only" {
		t.Fatal("schema modes leaked into actual support")
	}
	if f.objectReads != 0 || len(f.loaded) != 0 || len(f.transactions) != 3 {
		t.Fatal("discovery read objects or reused proof")
	}
	if w := f.request("purpose=human_review", false); w.Code != 400 {
		t.Fatal("capabilities accepted query")
	}
	cfg := contentReadTestConfig(f.base.a)
	cfg.CaptureModes = append(cfg.CaptureModes, "redacted")
	if _, err := NewContentReadHandlers(cfg); err == nil {
		t.Fatal("advertised unimplemented redaction")
	}
	cfg = contentReadTestConfig(f.base.a)
	cfg.MaxPageBytes = MaxPageBytes + 1
	if _, err := NewContentReadHandlers(cfg); err == nil {
		t.Fatal("unbounded page")
	}
}

func TestContentReadFirstPageCoverageCursorAndLateRevision(t *testing.T) {
	f := newContentReadTestFixture(t, 3)
	f.floor = 1
	p := readTestPage(t, f.request("purpose=human_review&limit=1", true))
	if p.AfterSequence != "1" || p.LastSequence != "2" || len(p.Items) != 1 || !p.HasMore || p.Coverage != "unknown" || len(p.Gaps) != 2 {
		t.Fatalf("first page %#v", p)
	}
	if len(f.loaded) != 1 || f.loaded[0] != 2 || f.objectReads != 0 {
		t.Fatal("loaded limit+1 documents or pending catalog")
	}
	p2 := readTestPage(t, f.request("purpose=human_review&limit=1&cursor="+p.NextCursor, true))
	if p2.LastSequence != "3" || p2.HasMore {
		t.Fatal("cursor skipped or repeated member")
	}
	p3 := readTestPage(t, f.request("purpose=human_review&limit=1&cursor="+p2.NextCursor, true))
	if len(p3.Items) != 0 || p3.AfterSequence != "3" || p3.LastSequence != "3" {
		t.Fatal("empty page lost position")
	}
	for _, tx := range f.transactions {
		if !tx.ended {
			t.Fatal("open snapshot escaped")
		}
	}
}

func TestContentReadHealthGapRemainsUnknownAndCountsInPage(t *testing.T) {
	for _, kind := range []string{"known", "old_epoch", "malformed", "absent"} {
		t.Run(kind, func(t *testing.T) {
			f := newContentReadTestFixture(t, 1)
			if kind != "absent" {
				f.healthReason = "manifest_write_unavailable"
				health := contentCaptureHealth{RecoveryEpoch: strconv.FormatInt(f.base.access.epoch, 10), SourceFactID: f.documents[0].SourceFactID, ObservedAt: contentReadTime(time.Now())}
				if kind == "old_epoch" {
					health.RecoveryEpoch = strconv.FormatInt(f.base.access.epoch+1, 10)
				}
				var err error
				f.healthDetail, err = json.Marshal(health)
				if err != nil {
					t.Fatal(err)
				}
			}
			if kind == "malformed" {
				f.healthDetail = []byte("broken")
			}
			page := readTestPage(t, f.request("purpose=human_review&limit=1", true))
			if page.Coverage != "unknown" || page.Gaps[0].Reason != "capture_health_unknown" {
				t.Fatal("claimed bounded capture health")
			}
			want := 1
			if kind == "known" {
				want = 2
			}
			if len(page.Gaps) != want {
				t.Fatal(page.Gaps)
			}
			if kind == "known" && (page.Gaps[1].Reason != "manifest_write_unavailable" || page.Gaps[1].RangeKnown) {
				t.Fatal(page.Gaps)
			}
			if int64(len(f.request("purpose=human_review&limit=1", true).Body.Bytes())) > f.handler.config.MaxPageBytes {
				t.Fatal("health gap escaped page budget")
			}
		})
	}
}

func TestContentReadBudgetBeforeDocumentsAndPrefixProgress(t *testing.T) {
	f := newContentReadTestFixture(t, 100)
	// A lower explicit total budget admits a prefix without fetching all 100
	// documents. Each next request must resume after the returned last item.
	f.handler.config.MaxPageBytes = 4000
	p := readTestPage(t, f.request("purpose=human_review&limit=100", true))
	if len(p.Items) == 0 || len(p.Items) >= 100 || len(f.loaded) != len(p.Items) || !p.HasMore {
		t.Fatal("page budget did not limit fetch")
	}
	last, _ := strconv.ParseInt(p.LastSequence, 10, 64)
	p2 := readTestPage(t, f.request("purpose=human_review&limit=100&cursor="+p.NextCursor, true))
	first, _ := strconv.ParseInt(p2.Items[0].Sequence, 10, 64)
	if first != last+1 {
		t.Fatal("budget skipped a sequence")
	}
	f = newContentReadTestFixture(t, 1)
	f.handler.config.MaxPageBytes = 100
	if w := f.request("purpose=human_review&limit=1", true); w.Code != 503 || !strings.Contains(w.Body.String(), "source_unavailable") {
		t.Fatal("oversized first item not rejected")
	}
	if len(f.loaded) != 0 {
		t.Fatal("fetched first body before budgeting")
	}
	f = newContentReadTestFixture(t, 10000)
	f.sizeOverride = 65536
	f.handler.config.MaxPageBytes = 65535
	if w := f.request("purpose=human_review&limit=10000", true); w.Code != 503 {
		t.Fatal("large metadata prefix accepted")
	}
	if len(f.loaded) != 0 {
		t.Fatal("limit+1 materialized documents")
	}
}

func TestContentReadCursorFailuresAndCorruptManifest(t *testing.T) {
	for _, test := range []struct {
		name            string
		after           int64
		change          func(*ContentCursorScope)
		floor, coverage int64
		status          int
		code            string
	}{
		{"high", 4, nil, 0, 0, 400, "invalid_cursor"}, {"coverage", 0, nil, 0, 1, 400, "invalid_cursor"}, {"floor", 0, nil, 1, 0, 410, "cursor_expired"},
		{"scope", 0, func(s *ContentCursorScope) { s.AccessGeneration++ }, 0, 0, 409, "content_scope_changed"}, {"epoch", 0, func(s *ContentCursorScope) { s.RecoveryEpoch++ }, 0, 0, 409, "source_epoch_changed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newContentReadTestFixture(t, 3)
			f.floor = test.floor
			f.base.access.coverage = test.coverage
			s := ContentCursorScope{SourceInstance: "synthetic-ur", OrgID: 1, ProjectID: 2, Profile: ContentProfile, AccessGeneration: 2, RecoveryEpoch: 1}
			if test.change != nil {
				test.change(&s)
			}
			cursor, err := EncodeContentCursor([]byte(f.base.a.source.options.CursorSecret), s, test.after)
			if err != nil {
				t.Fatal(err)
			}
			w := f.request("purpose=human_review&limit=1&cursor="+cursor, true)
			if w.Code != test.status || !strings.Contains(w.Body.String(), test.code) {
				t.Fatal(w.Code, w.Body.String())
			}
			if len(f.loaded) != 0 {
				t.Fatal("invalid cursor read manifest")
			}
		})
	}
	f := newContentReadTestFixture(t, 1)
	f.corruptHash = true
	if w := f.request("purpose=human_review&limit=1", true); w.Code != 503 {
		t.Fatal("accepted corrupt manifest")
	}
}

func TestContentReadFileBudgetAndFreshProofAfterIO(t *testing.T) {
	for _, change := range []string{"none", "scope", "epoch", "revoke", "deadline"} {
		t.Run(change, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newContentReadTestFixture(t, 2)
				for i := range f.documents {
					m := syntheticContentManifest([]byte("body"))
					m.RequestID = "saved-" + strconv.Itoa(i)
					m.SourceFactID = m.FactID()
					m.EventID = m.EventIDForRevision()
					m.ExpiresAt = contentReadTime(time.Now().Add(time.Hour))
					f.documents[i] = m
				}
				f.handler.config.MaxVerifyBytesPerPage = 4
				calls := 0
				f.handler.verify = func(ctx context.Context, r contentcapture.StoredReceipt, l contentcapture.StoreLimits) (*os.File, error) {
					calls++
					for _, tx := range f.transactions {
						if !tx.ended {
							t.Fatal("file I/O inside transaction")
						}
					}
					if r.Binding.Token != strings.Repeat("b", 64) || l.MaxBufferBytes != 7 {
						t.Fatal("lost original token or buffer budget")
					}
					switch change {
					case "scope":
						f.base.access.safety += 2
					case "epoch":
						f.base.access.epoch++
					case "revoke":
						f.base.access.global = "revoked"
					case "deadline":
						<-ctx.Done()
						return nil, ctx.Err()
					}
					return nil, os.ErrNotExist
				}
				if change == "deadline" {
					f.handler.config.RequestTimeout = time.Millisecond
				}
				w := f.request("purpose=human_review&limit=2", true)
				if change == "none" {
					p := readTestPage(t, w)
					if p.Items[0].Availability.State != "missing" || p.Items[1].Availability.State != "source_unavailable" || !p.Items[1].Availability.Retryable {
						t.Fatal("availability budget semantics")
					}
				} else {
					if w.Code == 200 || strings.Contains(w.Body.String(), "content_ref") {
						t.Fatal("invalidated proof leaked references")
					}
				}
				if calls != 1 {
					t.Fatalf("verification budget calls=%d", calls)
				}
			})
		})
	}
}

func TestContentReadR2SlotsAndNoSelectionMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*contentBodyAccess)
		status int
		code   string
	}{
		{"legacy", func(a *contentBodyAccess) { a.scope = "legacy"; a.local = "paused"; a.keyStatus = "feed_staged" }, 403, "profile_not_authorized"},
		{"paused", func(a *contentBodyAccess) { a.local = "paused"; a.keyStatus = "feed_staged" }, 503, "source_paused"},
		{"staged", func(a *contentBodyAccess) { a.keyStatus = "feed_staged" }, 409, "staged_credential_requires_pause"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newContentReadTestFixture(t, 1)
			test.change(&f.base.access)
			for _, page := range []bool{false, true} {
				w := f.request("invalid=query", page)
				if w.Code != test.status || !strings.Contains(w.Body.String(), test.code) {
					t.Fatal("query error overrode R2", w.Code, w.Body.String())
				}
			}
			if len(f.loaded) != 0 || f.objectReads != 0 {
				t.Fatal("R2 rejection queried content")
			}
		})
	}
	f := newContentReadTestFixture(t, 0)
	grant := f.base.grant(t)
	if failure := f.base.a.recheckScope(t.Context(), f.base.request, grant); failure != nil {
		t.Fatal(failure)
	}
	if grant.proof.selection != nil {
		t.Fatal("page proof created body selection")
	}
	f.base.access.scope = "legacy"
	f.base.access.local = "paused"
	f.base.access.keyStatus = "feed_staged"
	w := f.request("", false)
	if w.Code != 403 {
		t.Fatal("R2 profile precedence")
	}
	f = newContentReadTestFixture(t, 0)
	f.handler.slots <- struct{}{}
	f.handler.slots <- struct{}{}
	w = f.request("purpose=human_review&limit=1", true)
	if w.Code != 503 || len(f.transactions) != 0 {
		t.Fatal("full slots reached DB")
	}
	// Body-independent discovery also requires a writer with a real deadline.
	f = newContentReadTestFixture(t, 0)
	r := httptest.NewRequest(http.MethodGet, ContentCapabilitiesPath, nil)
	r.Header = f.base.request.Header
	r.RemoteAddr = f.base.request.RemoteAddr
	plain := httptest.NewRecorder()
	f.handler.serve(plain, r, false)
	if plain.Code != 503 {
		t.Fatal("unbounded response writer accepted")
	}
}

func TestContentReadAvailabilityKeepsImmutableManifest(t *testing.T) {
	for _, state := range []string{"pending", "not_captured", "expired", "available", "missing", "corrupt", "source_unavailable"} {
		t.Run(state, func(t *testing.T) {
			f := newContentReadTestFixture(t, 1)
			m := syntheticContentManifest(nil)
			m.ExpiresAt = contentReadTime(time.Now().Add(time.Hour))
			switch state {
			case "pending", "not_captured":
				m.Capture.Completeness = state
				m.Capture.Reason = "capture_in_progress"
				if state == "not_captured" {
					m.Capture.Reason = "storage_write_failed"
				}
				m.Representation = nil
				m.ContentRef = ""
				m.CapturedAt = ""
				m.ExpiresAt = ""
			case "expired":
				m.ExpiresAt = contentReadTime(time.Now().Add(-time.Hour))
			}
			f.documents[0] = m
			original, err := EncodeContentManifest(m)
			if err != nil {
				t.Fatal(err)
			}
			verified := 0
			f.handler.verify = func(context.Context, contentcapture.StoredReceipt, contentcapture.StoreLimits) (*os.File, error) {
				verified++
				switch state {
				case "available":
					return os.CreateTemp(t.TempDir(), "synthetic-verified-")
				case "missing":
					return nil, os.ErrNotExist
				case "corrupt":
					return nil, contentcapture.ErrStoreIntegrity
				default:
					return nil, errors.New("synthetic storage unavailable")
				}
			}
			p := readTestPage(t, f.request("purpose=human_review&limit=1", true))
			if p.Items[0].Availability.State != state {
				t.Fatalf("state=%s got=%#v", state, p.Items[0].Availability)
			}
			current, err := EncodeContentManifest(p.Items[0].Document)
			if err != nil || string(current) != string(original) || p.Items[0].SourceSHA256 != eventconnector.HashSourceBytes(original) {
				t.Fatal("availability rewrote immutable manifest")
			}
			if state == "pending" || state == "not_captured" || state == "expired" {
				if f.objectReads != 0 || verified != 0 {
					t.Fatal("non-readable state queried or opened body")
				}
			}
		})
	}
}
