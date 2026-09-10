package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceAdmissionResponseCommitsBeforeHeaderAndFailsClosed(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "recorded", true: "source_failure"}[fail], func(t *testing.T) {
			underlying := httptest.NewRecorder()
			calls := 0
			w := wrapSourceAdmissionWriter(underlying, func(status int) error {
				calls++
				require.Equal(t, 402, status)
				require.Equal(t, http.StatusOK, underlying.Code)
				require.Zero(t, underlying.Body.Len())
				if fail {
					return errors.New("synthetic source failure")
				}
				return nil
			}, func() { underlying.WriteHeader(503); _, _ = underlying.Write([]byte("source unavailable")) })
			w.Header().Set("Content-Length", "8")
			w.WriteHeader(402)
			n, err := w.Write([]byte("rejected"))
			w.WriteHeader(400)
			require.Equal(t, 1, calls)
			if fail {
				require.Zero(t, n)
				require.ErrorIs(t, err, errSourceAdmissionUnavailable)
				require.Equal(t, 503, underlying.Code)
				require.Equal(t, "source unavailable", underlying.Body.String())
				require.Empty(t, underlying.Header().Get("Content-Length"))
			} else {
				require.NoError(t, err)
				require.Equal(t, 8, n)
				require.Equal(t, 402, underlying.Code)
			}
		})
	}
}

type sourceAdmissionFailedTransport struct {
	header   http.Header
	flushErr error
}

func (w *sourceAdmissionFailedTransport) Header() http.Header         { return w.header }
func (w *sourceAdmissionFailedTransport) WriteHeader(int)             {}
func (w *sourceAdmissionFailedTransport) Write(p []byte) (int, error) { return len(p) - 1, nil }
func (w *sourceAdmissionFailedTransport) Flush()                      {}
func (w *sourceAdmissionFailedTransport) FlushError() error           { return w.flushErr }

func TestSourceAdmissionResponsePreservesTransportFailures(t *testing.T) {
	sentinel := errors.New("synthetic flush failure")
	w := wrapSourceAdmissionWriter(&sourceAdmissionFailedTransport{header: http.Header{}, flushErr: sentinel}, func(int) error { t.Fatal("successful output cannot be rejected"); return nil }, func() { t.Fatal("unexpected replacement") })
	_, ok := w.(http.Flusher)
	require.True(t, ok)
	n, err := w.Write([]byte("test"))
	require.Equal(t, 3, n)
	require.NoError(t, err)
	require.ErrorIs(t, http.NewResponseController(w).Flush(), sentinel)
}
