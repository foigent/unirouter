package modelauthz

import "context"

type decisionContextKey struct{}

func WithDecision(ctx context.Context, decision Decision) context.Context {
	return context.WithValue(ctx, decisionContextKey{}, decision)
}

func DecisionFromContext(ctx context.Context) (Decision, bool) {
	decision, ok := ctx.Value(decisionContextKey{}).(Decision)
	return decision, ok
}
