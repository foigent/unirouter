package contentcapture

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// Observation carries an already authenticated request and, for provider
// points, the original committed dispatch identity. It grants no authority.
type Observation struct {
	OrgID, ProjectID                                            int64
	RequestID, Protocol, ObservationPoint, AttemptID, MediaType string
	OccurredAt                                                  time.Time
}

type Starter interface {
	Begin(context.Context, Observation) (*Capture, error)
}

func beginObservation(ctx context.Context, s Starter, id Observation, point, media string) *Capture {
	if s == nil {
		return nil
	}
	id.ObservationPoint, id.MediaType, id.OccurredAt = point, media, time.Now().UTC()
	c, _ := s.Begin(ctx, id)
	return c
}

// ObserveBytes observes a buffer already retained by ingress. No second whole
// body copy is made; Offer owns only its bounded queue prefix.
func ObserveBytes(ctx context.Context, s Starter, id Observation, body []byte) {
	c := beginObservation(ctx, s, id, "client_request", id.MediaType)
	if c != nil {
		_ = c.Offer(context.WithoutCancel(ctx), body)
		_ = c.Finish("none")
	}
}

// ObserveProvider prepares request capture BEFORE the caller's network timer.
// Call observeResponse exactly once AFTER finishing that timer. It observes
// only bytes consumed by the original transport/parser; it never drains a body,
// retries a dispatch, changes its errors or interferes with GetBody replays.
func ObserveProvider(s Starter, id Observation, request *http.Request) (*http.Request, func(*http.Response, error)) {
	if s == nil || request == nil {
		return request, func(*http.Response, error) {}
	}
	ctx := request.Context()
	requestCapture := beginObservation(ctx, s, id, "provider_request", request.Header.Get("Content-Type"))
	if requestCapture != nil {
		if request.Body == nil || request.Body == http.NoBody {
			_ = requestCapture.Finish("none")
		} else {
			request = request.Clone(ctx)
			length := request.ContentLength
			if length == 0 {
				length = -1
			} // Go: non-nil request Body with zero means unknown.
			request.Body = &observedBody{body: request.Body, capture: requestCapture, ctx: ctx, length: length}
		}
	}
	return request, func(response *http.Response, err error) {
		if err != nil && requestCapture != nil {
			_ = requestCapture.Finish(interruptionReason(ctx))
		}
		media := ""
		if response != nil {
			media = response.Header.Get("Content-Type")
		}
		c := beginObservation(ctx, s, id, "provider_response", media)
		if c == nil {
			return
		}
		if response == nil {
			_ = c.Finish(interruptionReason(ctx))
		} else if response.Body == nil || response.Body == http.NoBody {
			reason := "none"
			if err != nil {
				reason = interruptionReason(ctx)
			}
			_ = c.Finish(reason)
		} else {
			response.Body = &observedBody{body: response.Body, capture: c, ctx: ctx, length: response.ContentLength}
		}
	}
}

func interruptionReason(ctx context.Context) string {
	if ctx.Err() != nil {
		return "request_cancelled"
	}
	return "stream_interrupted"
}

type observedBody struct {
	body    io.ReadCloser
	capture *Capture
	ctx     context.Context
	length  int64
	mu      sync.Mutex // only observation state, NEVER held across underlying Read/Close
	read    int64
	closed  bool
}

func (b *observedBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		if n < 0 || n > len(p) {
			_ = b.capture.Finish("stream_interrupted")
		} else {
			b.read += int64(n)
			_ = b.capture.Offer(context.WithoutCancel(b.ctx), p[:n])
			if err == io.EOF {
				_ = b.capture.Finish("none")
			} else if err != nil {
				_ = b.capture.Finish(interruptionReason(b.ctx))
			}
		}
	}
	return n, err
}

func (b *observedBody) Close() error {
	err := b.body.Close() // must be able to interrupt a concurrent blocked Read
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	reason := interruptionReason(b.ctx)
	if err == nil && b.length >= 0 && b.read == b.length {
		reason = "none"
	}
	_ = b.capture.Finish(reason)
	return err
}

// ObserveResponse preserves the original writer's Flusher capability and
// controller unwrapping. Finish must receive the handler's actual exit reason.
// Response policy/media type freeze at the first final header/write/flush.
func ObserveResponse(ctx context.Context, s Starter, id Observation, w http.ResponseWriter) (http.ResponseWriter, func(string)) {
	if s == nil {
		return w, func(string) {}
	}
	base := &observedResponse{ResponseWriter: w, ctx: ctx, starter: s, id: id}
	var wrapped http.ResponseWriter = base
	if _, ok := w.(http.Flusher); ok {
		wrapped = &observedFlusher{base}
	}
	return wrapped, func(reason string) {
		base.begin()
		if base.capture != nil {
			_ = base.capture.Finish(reason)
		}
	}
}

type observedResponse struct {
	http.ResponseWriter
	ctx     context.Context
	starter Starter
	id      Observation
	capture *Capture
	started bool
}

func (w *observedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *observedResponse) begin() {
	if w.started {
		return
	}
	w.started = true
	w.capture = beginObservation(w.ctx, w.starter, w.id, "client_response", w.Header().Get("Content-Type"))
}
func (w *observedResponse) WriteHeader(status int) {
	if status < 100 || status >= 200 {
		w.begin()
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *observedResponse) Write(p []byte) (int, error) {
	w.begin()
	n, err := w.ResponseWriter.Write(p)
	if w.capture != nil {
		if n < 0 || n > len(p) {
			_ = w.capture.Finish("stream_interrupted")
		} else {
			_ = w.capture.Offer(context.WithoutCancel(w.ctx), p[:n])
		}
		if err != nil || n != len(p) {
			_ = w.capture.Finish(interruptionReason(w.ctx))
		}
	}
	return n, err
}

type observedFlusher struct{ *observedResponse }

func (w *observedFlusher) Flush() { _ = w.FlushError() }
func (w *observedResponse) FlushError() error {
	w.begin()
	err := http.NewResponseController(w.ResponseWriter).Flush()
	if err != nil && w.capture != nil {
		_ = w.capture.Finish(interruptionReason(w.ctx))
	}
	return err
}
