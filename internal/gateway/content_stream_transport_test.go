package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/models"
)

// The database/billing boundary is mocked; both HTTP legs, relay scanner,
// provider observer, capture engine and filesystem are the product implementations.
type streamHTTPCapture struct {
	engine  *contentcapture.Engine
	binding contentcapture.StoreBinding
	capture chan *contentcapture.Capture
}

func (s *streamHTTPCapture) Begin(ctx context.Context, id contentcapture.Observation) (*contentcapture.Capture, error) {
	if id.ObservationPoint != "provider_response" {
		return nil, nil
	}
	c, err := s.engine.Start(ctx, 1, func(context.Context) (contentcapture.CaptureJob, error) {
		return contentcapture.CaptureJob{
			Reservation: contentcapture.Reservation{ProjectID: 1, Binding: s.binding, ObjectName: strings.Repeat("a", 64), ReservedBytes: 1 << 20},
			Limits:      contentcapture.StoreLimits{MaxSavedBytes: 1 << 20, MaxReadBytes: 1 << 20, MaxBufferBytes: 32 << 10},
			Finalize:    func(context.Context, contentcapture.CaptureOutcome) error { return nil },
		}, nil
	})
	s.capture <- c
	return c, err
}

func TestContentStreamRealHTTPTransportEOF(t *testing.T) {
	const prefix = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\n"
	const terminal = "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":17,\"completion_tokens\":3,\"total_tokens\":20}}\n\ndata: [DONE]\n\n"
	for _, mode := range []string{"chunked", "comments_after_done", "delayed_eof", "reset_after_done", "data_after_done", "opaque_tail", "never_eof", "request_deadline", "client_cancel"} {
		t.Run(mode, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			binding, err := contentcapture.InitializeFileStore(root, "synthetic-stream-http")
			require.NoError(t, err)
			files, err := contentcapture.OpenFileStore(root, binding)
			require.NoError(t, err)
			defer files.Close()
			engine, err := contentcapture.NewEngine(files, contentcapture.EngineOptions{
				Queue:     contentcapture.QueueLimits{MaxItems: 128, MaxBytes: 2 << 20, MaxProjectBytes: 2 << 20, MaxChunkBytes: 64 << 10},
				MaxActive: 2, MaxProjectActive: 2, PrepareTimeout: time.Second, FinalizeTimeout: time.Second, MaxLifetime: 5 * time.Second,
			})
			require.NoError(t, err)
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, engine.Close(ctx))
			}()
			starter := &streamHTTPCapture{engine: engine, binding: binding, capture: make(chan *contentcapture.Capture, 1)}
			terminalSent, releaseEOF := make(chan struct{}), make(chan struct{})
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				// Flush guarantees unknown-length chunked HTTP rather than an inferred Content-Length.
				_, _ = io.WriteString(w, prefix)
				w.(http.Flusher).Flush()
				_, _ = io.WriteString(w, terminal)
				w.(http.Flusher).Flush()
				close(terminalSent)
				switch mode {
				case "comments_after_done":
					_, _ = io.WriteString(w, ": transport tail\n\n")
				case "delayed_eof":
					select {
					case <-releaseEOF:
					case <-r.Context().Done():
					}
				case "reset_after_done":
					panic(http.ErrAbortHandler) // omit the terminating HTTP chunk
				case "data_after_done", "opaque_tail":
					_, _ = io.WriteString(w, "data: {\"secret_tail\":true,\"usage\":{\"prompt_tokens\":999,\"completion_tokens\":999,\"total_tokens\":1998}}\n\n")
					if mode == "opaque_tail" {
						_, _ = w.Write([]byte{0xff, '\n'})
					}
					w.(http.Flusher).Flush()
				case "never_eof", "request_deadline", "client_cancel":
					<-r.Context().Done()
				}
			}))
			defer provider.Close()
			var deliveryOutcomes []string
			var settled billing.SettleRequest
			relay := newTestRelay(streamTransportTestDB(), &mockBilling{settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
				settled = req
				return &billing.SettleResult{Success: true}, nil
			}, recordDeliveryFn: func(_ context.Context, event billing.DeliveryEvent) error {
				deliveryOutcomes = append(deliveryOutcomes, event.Outcome)
				return nil
			}})
			completed := make(chan struct{})
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(completed)
				ctx := r.Context()
				if mode == "request_deadline" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, 150*time.Millisecond)
					defer cancel()
				}
				req, e := http.NewRequestWithContext(ctx, http.MethodGet, provider.URL, nil)
				if e != nil {
					t.Error(e)
					return
				}
				req, observe := contentcapture.ObserveProvider(starter, contentcapture.Observation{OrgID: 1, ProjectID: 1, RequestID: "synthetic-stream", AttemptID: "synthetic-attempt", Protocol: "openai.chat_completions.v1"}, req)
				timeout := 3 * time.Second
				if mode == "never_eof" {
					timeout = 150 * time.Millisecond
				}
				// This is the same providerClient factory used by the real dispatch path.
				resp, e := relay.providerClient(timeout).Do(req)
				observe(resp, e)
				if e != nil {
					t.Error(e)
					return
				}
				if resp.ContentLength != -1 {
					t.Errorf("want chunked unknown length, got %d", resp.ContentLength)
				}
				relay.relayStreamRequest(ctx, w, resp, &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}, "synthetic-stream", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{}), time.Now(), 1000, adapters.NewOpenAICompatible(), "")
			}))
			defer gateway.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, gateway.URL, nil)
			require.NoError(t, err)
			type responseResult struct {
				body string
				err  error
			}
			result := make(chan responseResult, 1)
			go func() {
				resp, e := gateway.Client().Do(req)
				if e != nil {
					result <- responseResult{err: e}
					return
				}
				defer resp.Body.Close()
				b, e := io.ReadAll(resp.Body)
				result <- responseResult{string(b), e}
			}()
			select {
			case <-terminalSent:
			case <-ctx.Done():
				t.Fatal("provider did not send terminal")
			}
			var c *contentcapture.Capture
			select {
			case c = <-starter.capture:
				require.NotNil(t, c)
			case <-ctx.Done():
				t.Fatal("provider capture did not start")
			}
			if mode == "delayed_eof" {
				select {
				case got := <-result:
					close(releaseEOF)
					t.Fatalf("relay completed before actual EOF: %s / %v", got.body, got.err)
				case <-time.After(40 * time.Millisecond):
				}
				close(releaseEOF)
			}
			if mode == "client_cancel" {
				cancel()
			}
			var got responseResult
			select {
			case got = <-result:
			case <-time.After(4 * time.Second):
				t.Fatal("HTTP request did not terminate")
			}
			select {
			case <-completed:
			case <-time.After(time.Second):
				t.Fatal("relay/scanner did not join")
			}
			waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
			defer waitCancel()
			capture, e := c.Wait(waitCtx)
			require.NoError(t, e)
			require.NoError(t, capture.FinalizeError)
			require.NoError(t, capture.Outcome.StorageError)
			require.NotContains(t, got.body, "secret_tail")
			switch mode {
			case "chunked", "comments_after_done", "delayed_eof", "data_after_done", "opaque_tail":
				require.NoError(t, got.err)
				require.Contains(t, got.body, "answer")
				require.Contains(t, got.body, "data: [DONE]")
				require.NotContains(t, got.body, "\"error\"")
				require.Equal(t, "none", capture.Outcome.Reason)
				require.NotNil(t, capture.Outcome.Receipt)
				file, e := files.OpenVerified(waitCtx, *capture.Outcome.Receipt, contentcapture.StoreLimits{MaxSavedBytes: 1 << 20, MaxReadBytes: 1 << 20, MaxBufferBytes: 32 << 10})
				require.NoError(t, e)
				saved, e := io.ReadAll(file)
				require.NoError(t, e)
				require.NoError(t, file.Close())
				expected := prefix + terminal
				if mode == "comments_after_done" {
					expected += ": transport tail\n\n"
				}
				if mode == "data_after_done" || mode == "opaque_tail" {
					expected += "data: {\"secret_tail\":true,\"usage\":{\"prompt_tokens\":999,\"completion_tokens\":999,\"total_tokens\":1998}}\n\n"
				}
				if mode == "opaque_tail" {
					expected += string([]byte{0xff, '\n'})
				}
				require.Equal(t, expected, string(saved))
				require.Contains(t, deliveryOutcomes, billing.DeliveryOutcomeCompleted)
				require.Equal(t, 17, settled.PromptTokens)
				require.Equal(t, 3, settled.CompletionTokens)
				require.Equal(t, metering.UsageSourceUpstream, settled.UsageSource)
			default:
				require.NotContains(t, got.body, "data: [DONE]", fmt.Sprintf("mode=%s", mode))
				require.NotContains(t, deliveryOutcomes, billing.DeliveryOutcomeCompleted)
				require.NotEqual(t, "none", capture.Outcome.Reason)
				if mode != "client_cancel" && mode != "request_deadline" {
					require.NoError(t, got.err)
					require.Contains(t, got.body, "\"error\"")
				}
			}
		})
	}
}
