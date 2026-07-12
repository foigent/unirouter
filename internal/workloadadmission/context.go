package workloadadmission

import "context"

type classContextKey struct{}

func ContextWithClass(ctx context.Context, class string) context.Context {
	return context.WithValue(ctx, classContextKey{}, class)
}

func ClassFromContext(ctx context.Context) string {
	if ctx != nil {
		if class, ok := ctx.Value(classContextKey{}).(string); ok {
			return class
		}
	}
	return ClassInteractive
}
