package gateway

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// sseStreamWriter serializes each write with its corresponding Flush. A Flush
// outside the same critical section can race a heartbeat write even when the
// byte write itself was locked.
type sseStreamWriter struct {
	mu           sync.Mutex
	writer       http.ResponseWriter
	controller   *http.ResponseController
	writeTimeout time.Duration
}

func newSSEStreamWriter(writer http.ResponseWriter, writeTimeout time.Duration) *sseStreamWriter {
	return &sseStreamWriter{
		writer:       writer,
		controller:   http.NewResponseController(writer),
		writeTimeout: writeTimeout,
	}
}

func (writer *sseStreamWriter) WriteAndFlush(format string, args ...any) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if writer.writeTimeout > 0 {
		err := writer.controller.SetWriteDeadline(time.Now().Add(writer.writeTimeout))
		if err != nil && !errors.Is(err, http.ErrNotSupported) {
			return fmt.Errorf("set stream write deadline: %w", err)
		}
	}
	if _, err := fmt.Fprintf(writer.writer, format, args...); err != nil {
		return fmt.Errorf("write stream event: %w", err)
	}
	if err := writer.controller.Flush(); err != nil {
		return fmt.Errorf("flush stream event: %w", err)
	}
	return nil
}
