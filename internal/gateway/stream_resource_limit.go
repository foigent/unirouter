package gateway

import (
	"errors"
	"io"
)

var errStreamResourceLimit = errors.New("stream transport resource limit exceeded")

// boundedStreamReader counts raw upstream bytes, including ignored SSE fields
// and delimiters. It reads at most one byte past the cap to distinguish exact EOF
// from overflow, and never accumulates a response-sized buffer.
type boundedStreamReader struct {
	reader    io.Reader
	remaining int64
}

func (r *boundedStreamReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining < 0 {
		return 0, errStreamResourceLimit
	}
	if int64(len(p)) > r.remaining+1 {
		p = p[:int(r.remaining+1)]
	}
	n, err := r.reader.Read(p)
	if int64(n) > r.remaining {
		r.remaining = -1
		return 0, errStreamResourceLimit
	}
	r.remaining -= int64(n)
	return n, err
}
