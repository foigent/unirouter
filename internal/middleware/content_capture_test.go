package middleware

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
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
)

type middlewareContentStarter struct {
	t        *testing.T
	files    *contentcapture.FileStore
	binding  contentcapture.StoreBinding
	engine   *contentcapture.Engine
	ids      []contentcapture.Observation
	captures []*contentcapture.Capture
}

func (s *middlewareContentStarter) Begin(ctx context.Context, id contentcapture.Observation) (*contentcapture.Capture, error) {
	s.ids = append(s.ids, id)
	job := contentcapture.CaptureJob{Reservation: contentcapture.Reservation{ProjectID: id.ProjectID, Binding: s.binding, ObjectName: fmt.Sprintf("%064x", len(s.ids)), ReservedBytes: 1024}, Limits: contentcapture.StoreLimits{MaxSavedBytes: 1024, MaxReadBytes: 1024, MaxBufferBytes: 128}, Finalize: func(context.Context, contentcapture.CaptureOutcome) error { return nil }}
	c, err := s.engine.Start(ctx, id.ProjectID, func(context.Context) (contentcapture.CaptureJob, error) { return job, nil })
	s.captures = append(s.captures, c)
	return c, err
}
func TestContentClientCaptureOriginalBufferAndActualExit(t *testing.T) {
	for _, kind := range []string{"normal", "panic", "cancel", "non_json"} {
		t.Run(kind, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			binding, err := contentcapture.InitializeFileStore(root, "synthetic-client")
			require.NoError(t, err)
			files, err := contentcapture.OpenFileStore(root, binding)
			require.NoError(t, err)
			defer files.Close()
			engine, err := contentcapture.NewEngine(files, contentcapture.EngineOptions{Queue: contentcapture.QueueLimits{MaxItems: 32, MaxBytes: 4096, MaxProjectBytes: 4096, MaxChunkBytes: 128}, MaxActive: 4, MaxProjectActive: 4, PrepareTimeout: time.Second, FinalizeTimeout: time.Second, MaxLifetime: time.Minute})
			require.NoError(t, err)
			defer func() { require.NoError(t, engine.Close(context.Background())) }()
			s := &middlewareContentStarter{t: t, files: files, binding: binding, engine: engine}
			body := "  {\"model\":\"synthetic\"}  "
			if kind == "non_json" {
				body = "not json\x00"
			}
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
			project := int64(2)
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			ctx = context.WithValue(ctx, CtxAPIKey, &models.ApiKey{OrgID: 1, ProjectID: &project})
			ctx = context.WithValue(ctx, CtxOrgID, int64(1))
			ctx = context.WithValue(ctx, CtxRequestID, "original-server-id")
			r = r.WithContext(ctx)
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				retained, ok := payloadpolicy.BodyFromContext(r.Context())
				require.True(t, ok)
				// Later governance mutation cannot alter the already observed ingress.
				for i := range retained {
					retained[i] = 'x'
				}
				_, _ = w.Write([]byte("actual visible bytes"))
				if kind == "panic" {
					panic("synthetic panic")
				}
				if kind == "cancel" {
					cancel()
				}
			})
			inner := http.Handler(next)
			if kind == "non_json" {
				inner = StrictJSONIngress(inner)
			}
			guard := payloadpolicy.RequestMiddlewareWithMax(payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits()), 1024, GetOrgIDFromContext)
			h := guard(CaptureContent(s)(inner))
			w := httptest.NewRecorder()
			if kind == "panic" {
				require.PanicsWithValue(t, "synthetic panic", func() { h.ServeHTTP(w, r) })
			} else {
				h.ServeHTTP(w, r)
			}
			require.Len(t, s.captures, 2)
			require.Empty(t, s.ids[0].AttemptID)
			require.Equal(t, "original-server-id", s.ids[1].RequestID)
			for i, c := range s.captures {
				result, err := c.Wait(t.Context())
				require.NoError(t, err)
				require.NotNil(t, result.Outcome.Receipt)
				f, err := files.OpenVerified(t.Context(), *result.Outcome.Receipt, contentcapture.StoreLimits{MaxSavedBytes: 1024, MaxReadBytes: 1024, MaxBufferBytes: 128})
				require.NoError(t, err)
				saved, err := io.ReadAll(f)
				require.NoError(t, err)
				require.NoError(t, f.Close())
				if i == 0 {
					require.Equal(t, body, string(saved))
					require.Equal(t, "none", result.Outcome.Reason)
				} else {
					require.Equal(t, w.Body.Bytes(), saved)
					want := "none"
					if kind == "panic" {
						want = "stream_interrupted"
					}
					if kind == "cancel" {
						want = "request_cancelled"
					}
					require.Equal(t, want, result.Outcome.Reason)
				}
			}
			if kind == "non_json" {
				require.Equal(t, http.StatusBadRequest, w.Code)
			}
		})
	}
}
