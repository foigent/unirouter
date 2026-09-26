package middleware

import "context"

type gatewayNoRouteCaptureKey struct{}

func WithGatewayNoRouteCapture(ctx context.Context, capture func(context.Context) error) context.Context {
	if capture == nil {
		return ctx
	}
	return context.WithValue(ctx, gatewayNoRouteCaptureKey{}, capture)
}

// CaptureGatewayNoRoute records only the exact pre-dispatch no-route branch.
// An absent source producer does not change the gateway response.
func CaptureGatewayNoRoute(ctx context.Context) error {
	capture, ok := ctx.Value(gatewayNoRouteCaptureKey{}).(func(context.Context) error)
	if !ok {
		return nil
	}
	return capture(ctx)
}
