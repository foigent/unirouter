package middleware

import (
	"errors"
	"net/http"
)

var errSourceAdmissionUnavailable = errors.New("source admission observation unavailable")

// Only the first final header can trigger the mandatory rejection record. This
// writer buffers no payload and preserves the underlying flush/error boundary.
type sourceAdmissionWriter struct {
	http.ResponseWriter
	beforeHeader    func(int) error
	unavailable     func()
	started, failed bool
}

func (w *sourceAdmissionWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *sourceAdmissionWriter) WriteHeader(status int) {
	if w.started {
		return
	}
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.started = true
	if status >= 400 && status < 500 {
		if err := w.beforeHeader(status); err != nil {
			w.failed = true
			w.Header().Del("Content-Length")
			w.unavailable()
			return
		}
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *sourceAdmissionWriter) Write(p []byte) (int, error) {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}
	if w.failed {
		return 0, errSourceAdmissionUnavailable
	}
	return w.ResponseWriter.Write(p)
}

type sourceAdmissionFlushWriter struct{ *sourceAdmissionWriter }

func (w *sourceAdmissionFlushWriter) Flush() { _ = w.FlushError() }
func (w *sourceAdmissionFlushWriter) FlushError() error {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}
	if w.failed {
		return errSourceAdmissionUnavailable
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func wrapSourceAdmissionWriter(w http.ResponseWriter, beforeHeader func(int) error, unavailable func()) http.ResponseWriter {
	base := &sourceAdmissionWriter{ResponseWriter: w, beforeHeader: beforeHeader, unavailable: unavailable}
	if _, ok := w.(http.Flusher); ok {
		return &sourceAdmissionFlushWriter{base}
	}
	return base
}
