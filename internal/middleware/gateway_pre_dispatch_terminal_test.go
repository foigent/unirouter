package middleware

import (
	"context"
	"errors"
	"testing"
)

func TestGatewayNoRouteCaptureIsExplicitAndDoesNotWrapError(t *testing.T) {
	if err := CaptureGatewayNoRoute(context.Background()); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("capture unavailable")
	calls := 0
	ctx := context.WithValue(context.Background(), gatewayNoRouteCaptureKey{}, func(context.Context) error { calls++; return sentinel })
	if err := CaptureGatewayNoRoute(ctx); !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("capture=(%v,%d)", err, calls)
	}
}
