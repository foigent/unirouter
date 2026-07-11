package adapters

import (
	"context"
	"errors"
	"io"
	"syscall"
	"testing"
)

type classifiedNetError struct {
	timeout   bool
	temporary bool
}

func (e classifiedNetError) Error() string   { return "classified network error" }
func (e classifiedNetError) Timeout() bool   { return e.timeout }
func (e classifiedNetError) Temporary() bool { return e.temporary }

func TestClassifyHTTPFailureMatrix(t *testing.T) {
	tests := []struct {
		status    int
		kind      FailureKind
		retryable bool
	}{
		{400, FailureClientRequest, false},
		{401, FailureAuth, false},
		{403, FailureAuth, false},
		{404, FailureClientRequest, false},
		{409, FailureClientRequest, false},
		{413, FailureClientRequest, false},
		{422, FailureClientRequest, false},
		{408, FailureChannel, true},
		{429, FailureRateLimit, true},
		{500, FailureChannel, true},
		{502, FailureChannel, true},
		{503, FailureChannel, true},
		{504, FailureChannel, true},
		{501, FailureUnknown, false},
		{599, FailureUnknown, false},
	}
	for _, test := range tests {
		kind, retryable := classifyHTTPFailure(test.status)
		if kind != test.kind || retryable != test.retryable {
			t.Fatalf("status %d = kind %s retryable %t, want %s %t", test.status, kind, retryable, test.kind, test.retryable)
		}
	}
}

func TestClassifyTransportErrorMatrix(t *testing.T) {
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name      string
		ctx       context.Context
		err       error
		kind      FailureKind
		retryable bool
	}{
		{name: "caller context canceled", ctx: cancelledCtx, err: errors.New("transport failed"), kind: FailureClientRequest},
		{name: "explicit cancel", ctx: context.Background(), err: context.Canceled, kind: FailureClientRequest},
		{name: "upstream deadline", ctx: context.Background(), err: context.DeadlineExceeded, kind: FailureChannel, retryable: true},
		{name: "unexpected eof", ctx: context.Background(), err: io.ErrUnexpectedEOF, kind: FailureChannel, retryable: true},
		{name: "connection reset", ctx: context.Background(), err: syscall.ECONNRESET, kind: FailureChannel, retryable: true},
		{name: "network timeout", ctx: context.Background(), err: classifiedNetError{timeout: true}, kind: FailureChannel, retryable: true},
		{name: "temporary network", ctx: context.Background(), err: classifiedNetError{temporary: true}, kind: FailureChannel, retryable: true},
		{name: "deterministic tls config", ctx: context.Background(), err: errors.New("certificate is not trusted"), kind: FailureUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ClassifyTransportError(test.ctx, test.err)
			if got.FailureKind != test.kind || got.Retryable != test.retryable {
				t.Fatalf("got kind %s retryable %t, want %s %t", got.FailureKind, got.Retryable, test.kind, test.retryable)
			}
		})
	}
}
