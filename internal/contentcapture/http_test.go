package contentcapture

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type httpCaptureStarter struct {
	t        *testing.T
	e        *Engine
	binding  StoreBinding
	mu       sync.Mutex
	ids      []Observation
	captures []*Capture
	err      error
}

func newHTTPStarter(t *testing.T) *httpCaptureStarter {
	e, _, binding := testEngine(t, nil)
	return &httpCaptureStarter{t: t, e: e, binding: binding}
}
func (s *httpCaptureStarter) Begin(ctx context.Context, id Observation) (*Capture, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ids = append(s.ids, id)
	if s.err != nil {
		return nil, s.err
	}
	job := testCaptureJob(s.t, s.binding, id.ProjectID, 2048, nil)
	c, err := s.e.Start(ctx, id.ProjectID, func(context.Context) (CaptureJob, error) { return job, nil })
	s.captures = append(s.captures, c)
	return c, err
}
func httpObservation() Observation {
	return Observation{OrgID: 1, ProjectID: 1, RequestID: "synthetic-http", Protocol: "openai.chat_completions.v1", AttemptID: "real-test-attempt"}
}
func (s *httpCaptureStarter) body(index int) ([]byte, string) {
	s.mu.Lock()
	c := s.captures[index]
	s.mu.Unlock()
	r := waitCapture(s.t, c)
	if r.Outcome.Receipt == nil {
		require.NotEqual(s.t, "none", r.Outcome.Reason)
		return nil, r.Outcome.Reason
	}
	return capturedBytes(s.t, s.e, c, r), r.Outcome.Reason
}

func TestContentHTTPActualTransportPreservesRequestAndResponse(t *testing.T) {
	want := []byte{0, 255, 'a', '\n'}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, want, body)
		require.Equal(t, "synthetic", r.Header.Get("X-Test"))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(201)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	s := newHTTPStarter(t)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, bytes.NewReader(want))
	require.NoError(t, err)
	req.Header.Set("X-Test", "synthetic")
	wrapped, observe := ObserveProvider(s, httpObservation(), req)
	require.Equal(t, req.ContentLength, wrapped.ContentLength)
	require.Equal(t, req.Context(), wrapped.Context())
	replay, err := wrapped.GetBody()
	require.NoError(t, err)
	body, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(t, want, body)
	require.NoError(t, replay.Close())
	response, err := server.Client().Do(wrapped)
	observe(response, err)
	require.NoError(t, err)
	require.Equal(t, 201, response.StatusCode)
	body, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, want, body)
	for i := range 2 {
		saved, reason := s.body(i)
		require.Equal(t, want, saved)
		require.Equal(t, "none", reason)
	}
	require.Equal(t, "provider_request", s.ids[0].ObservationPoint)
	require.Equal(t, "provider_response", s.ids[1].ObservationPoint)
	require.Equal(t, "real-test-attempt", s.ids[1].AttemptID)
}

func TestContentHTTPEmptyAndUnknownLengthKeepTransportSemantics(t *testing.T) {
	for _, body := range []io.ReadCloser{nil, http.NoBody} {
		s := newHTTPStarter(t)
		req, _ := http.NewRequestWithContext(t.Context(), "POST", "http://synthetic.invalid", nil)
		req.Body = body
		wrapped, observe := ObserveProvider(s, httpObservation(), req)
		require.Equal(t, body, wrapped.Body)
		response := &http.Response{Body: body}
		observe(response, nil)
		require.Equal(t, body, response.Body)
		for i := range 2 {
			saved, reason := s.body(i)
			require.Empty(t, saved)
			require.Equal(t, "none", reason)
		}
	}
	s := newHTTPStarter(t)
	req, _ := http.NewRequestWithContext(t.Context(), "POST", "http://synthetic.invalid", nil)
	req.Body = io.NopCloser(bytes.NewReader([]byte("not consumed")))
	req.ContentLength = 0
	wrapped, _ := ObserveProvider(s, httpObservation(), req)
	require.NoError(t, wrapped.Body.Close())
	_, reason := s.body(0)
	require.Equal(t, "stream_interrupted", reason)
}

type httpResultBody struct {
	n             int
	err, closeErr error
}

func (b httpResultBody) Read(p []byte) (int, error) { copy(p, []byte("abc")); return b.n, b.err }
func (b httpResultBody) Close() error               { return b.closeErr }
func TestContentHTTPReadErrorsAndExactLength(t *testing.T) {
	sentinel := errors.New("synthetic-read")
	for _, tc := range []struct {
		name string
		n    int
		err  error
		want string
	}{
		{"eof_with_bytes", 3, io.EOF, "none"}, {"error_at_exact_length", 3, sentinel, "stream_interrupted"}, {"close_at_exact_length", 3, nil, "none"}, {"invalid_n", 8, sentinel, "stream_interrupted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newHTTPStarter(t)
			c, _ := s.Begin(t.Context(), httpObservation())
			b := &observedBody{body: httpResultBody{n: tc.n, err: tc.err}, capture: c, ctx: t.Context(), length: 3}
			n, err := b.Read(make([]byte, 3))
			require.Equal(t, tc.n, n)
			require.Equal(t, tc.err, err)
			require.NoError(t, b.Close())
			saved, reason := s.body(0)
			require.Equal(t, tc.want, reason)
			if tc.n == 3 {
				require.Equal(t, []byte("abc"), saved)
			} else {
				require.Empty(t, saved)
			}
		})
	}
}

type httpBlockedBody struct {
	reading, closed chan struct{}
	once            sync.Once
}

func (b *httpBlockedBody) Read([]byte) (int, error) {
	close(b.reading)
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *httpBlockedBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return io.ErrClosedPipe
}
func TestContentHTTPCloseInterruptsConcurrentRead(t *testing.T) {
	s := newHTTPStarter(t)
	c, _ := s.Begin(t.Context(), httpObservation())
	inner := &httpBlockedBody{reading: make(chan struct{}), closed: make(chan struct{})}
	b := &observedBody{body: inner, capture: c, ctx: t.Context(), length: -1}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = b.Read(make([]byte, 8)) }()
	<-inner.reading
	closed := make(chan error, 1)
	go func() { closed <- b.Close() }()
	select {
	case err := <-closed:
		require.ErrorIs(t, err, io.ErrClosedPipe)
	case <-time.After(time.Second):
		t.Fatal("Close blocked behind Read")
	}
	<-done
	_, reason := s.body(0)
	require.Equal(t, "stream_interrupted", reason)
}

type httpPlainWriter struct {
	header   http.Header
	accepted []byte
	codes    []int
	err      error
	n        int
	deadline time.Time
}

func (w *httpPlainWriter) Header() http.Header  { return w.header }
func (w *httpPlainWriter) WriteHeader(code int) { w.codes = append(w.codes, code) }
func (w *httpPlainWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w.n >= 0 {
		n = w.n
	}
	if n <= len(p) {
		w.accepted = append(w.accepted, p[:n]...)
	}
	return n, w.err
}
func (w *httpPlainWriter) SetWriteDeadline(at time.Time) error { w.deadline = at; return w.err }

type httpFlushWriter struct {
	*httpPlainWriter
	flushes  int
	flushErr error
}

func (w *httpFlushWriter) Flush()            { _ = w.FlushError() }
func (w *httpFlushWriter) FlushError() error { w.flushes++; return w.flushErr }
func TestContentHTTPResponseWriterPreservesControllerAndFailures(t *testing.T) {
	for _, flush := range []bool{false, true} {
		t.Run(map[bool]string{true: "flusher", false: "plain"}[flush], func(t *testing.T) {
			s := newHTTPStarter(t)
			base := &httpPlainWriter{header: make(http.Header), n: -1}
			var original http.ResponseWriter = base
			fw := &httpFlushWriter{httpPlainWriter: base}
			if flush {
				original = fw
			}
			w, finish := ObserveResponse(t.Context(), s, httpObservation(), original)
			_, hasFlush := w.(http.Flusher)
			require.Equal(t, flush, hasFlush)
			w.WriteHeader(103)
			require.Empty(t, s.ids)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(202)
			n, err := w.Write([]byte("prefix"))
			require.NoError(t, err)
			require.Equal(t, 6, n)
			deadline := time.Now().Add(time.Second)
			require.NoError(t, http.NewResponseController(w).SetWriteDeadline(deadline))
			require.Equal(t, deadline, base.deadline)
			if flush {
				require.NoError(t, http.NewResponseController(w).Flush())
				select {
				case <-s.captures[0].done:
					t.Fatal("normal flush ended capture")
				default:
				}
			}
			base.n = 2
			base.err = io.ErrClosedPipe
			n, err = w.Write([]byte("suffix"))
			require.Equal(t, 2, n)
			require.ErrorIs(t, err, io.ErrClosedPipe)
			finish("none")
			saved, reason := s.body(0)
			require.Equal(t, base.accepted, saved)
			require.Equal(t, "prefixsu", string(saved))
			require.Equal(t, "stream_interrupted", reason)
			require.Equal(t, []int{103, 202}, base.codes)
		})
	}
	s := newHTTPStarter(t)
	original := &httpFlushWriter{httpPlainWriter: &httpPlainWriter{header: make(http.Header), n: -1}, flushErr: io.ErrClosedPipe}
	w, finish := ObserveResponse(t.Context(), s, httpObservation(), original)
	require.ErrorIs(t, http.NewResponseController(w).Flush(), io.ErrClosedPipe)
	finish("none")
	_, reason := s.body(0)
	require.Equal(t, "stream_interrupted", reason)
}

func TestContentHTTPCaptureFailureDoesNotChangeOriginalObjects(t *testing.T) {
	s := newHTTPStarter(t)
	s.err = errors.New("synthetic capture unavailable")
	req, _ := http.NewRequestWithContext(t.Context(), "POST", "http://synthetic.invalid", bytes.NewReader([]byte("original")))
	wrapped, observe := ObserveProvider(s, httpObservation(), req)
	require.Same(t, req, wrapped)
	response := &http.Response{Body: io.NopCloser(bytes.NewReader([]byte("response")))}
	before := response.Body
	observe(response, nil)
	require.Equal(t, before, response.Body)
	original := httptest.NewRecorder()
	w, finish := ObserveResponse(t.Context(), s, httpObservation(), original)
	n, err := w.Write([]byte("visible"))
	finish("none")
	require.Equal(t, 7, n)
	require.NoError(t, err)
	require.Equal(t, "visible", original.Body.String())
}

// The relay stops consuming at a protocol terminal marker. These socket-free
// cases exercise the real observer and durable writer at that boundary; a
// protocol terminal does not establish transport EOF or permission to drain.
type httpBoundaryBody struct {
	chunks        [][]byte
	finalError    error
	reads, closes int
}

func (b *httpBoundaryBody) Read(p []byte) (int, error) {
	b.reads++
	if len(b.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.chunks[0])
	b.chunks[0] = b.chunks[0][n:]
	if len(b.chunks[0]) == 0 {
		b.chunks = b.chunks[1:]
	}
	if len(b.chunks) == 0 {
		return n, b.finalError
	}
	return n, nil
}
func (b *httpBoundaryBody) Close() error { b.closes++; return nil }

func TestContentHTTPStreamTerminalDoesNotProveByteEOF(t *testing.T) {
	const terminal = "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	const tail = ": trailing bytes\n\n"
	sentinel := errors.New("synthetic-read-error")
	for _, tc := range []struct {
		name, body, tail, reason string
		known, readAll, cancel   bool
		finalError               error
	}{
		{name: "unknown_DONE_without_EOF", body: terminal, reason: "stream_interrupted"},
		{name: "unknown_DONE_with_EOF_in_same_read", body: terminal, finalError: io.EOF, reason: "none"},
		{name: "unknown_DONE_then_actual_EOF", body: terminal, readAll: true, reason: "none"},
		{name: "unknown_DONE_leaves_trailing_bytes", body: terminal, tail: tail, reason: "stream_interrupted"},
		{name: "known_length_complete", body: terminal, known: true, reason: "none"},
		{name: "known_length_with_unread_tail", body: terminal, tail: tail, known: true, reason: "stream_interrupted"},
		{name: "error_with_terminal_bytes", body: terminal, finalError: sentinel, reason: "stream_interrupted"},
		{name: "cancel_after_terminal", body: terminal, cancel: true, reason: "request_cancelled"},
		{name: "early_close", body: "data: unfinished", reason: "stream_interrupted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newHTTPStarter(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "POST", "http://synthetic.invalid", nil)
			_, observe := ObserveProvider(s, httpObservation(), req)
			inner := &httpBoundaryBody{chunks: [][]byte{[]byte(tc.body)}, finalError: tc.finalError}
			if tc.tail != "" {
				inner.chunks = append(inner.chunks, []byte(tc.tail))
			}
			length := int64(-1)
			if tc.known {
				length = int64(len(tc.body) + len(tc.tail))
			}
			response := &http.Response{Body: inner, ContentLength: length, Header: http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}}}
			observe(response, nil)
			if tc.name == "early_close" {
				// Closing an unconsumed body must never manufacture a successful capture.
			} else if tc.readAll {
				got, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.Equal(t, tc.body, string(got))
			} else {
				scanner := bufio.NewScanner(response.Body)
				found := false
				for scanner.Scan() {
					if scanner.Text() == "data: [DONE]" {
						found = true
						break
					}
				}
				require.True(t, found)
			}
			reads := inner.reads
			if tc.cancel {
				cancel()
			}
			require.NoError(t, response.Body.Close())
			require.Equal(t, reads, inner.reads, "Close must not drain to force complete")
			require.Equal(t, 1, inner.closes)
			saved, reason := s.body(1)
			require.Equal(t, tc.reason, reason)
			expected := tc.body
			if tc.name == "early_close" {
				expected = ""
			}
			require.Equal(t, expected, string(saved))
			result := waitCapture(t, s.captures[1])
			if tc.name == "early_close" {
				require.Nil(t, result.Outcome.Receipt, "unconsumed interruption is not a complete empty body")
			} else {
				require.NotNil(t, result.Outcome.Receipt)
				hash := sha256.Sum256(saved)
				require.Equal(t, "sha256:"+hex.EncodeToString(hash[:]), result.Outcome.Receipt.SHA256)
				require.EqualValues(t, len(saved), result.Outcome.Receipt.SavedBytes)
			}
			require.Equal(t, "provider_response", s.ids[1].ObservationPoint)
			require.Equal(t, "text/event-stream; charset=utf-8", s.ids[1].MediaType)
			require.Equal(t, httpObservation().AttemptID, s.ids[1].AttemptID)
			if tc.tail != "" {
				require.Len(t, inner.chunks, 1)
				require.Equal(t, tc.tail, string(inner.chunks[0]))
			}
		})
	}
}
