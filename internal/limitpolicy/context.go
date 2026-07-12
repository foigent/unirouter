package limitpolicy

import (
	"context"
	"sync"
)

type decisionCollectorContextKey struct{}

type DecisionCollector struct {
	mu        sync.Mutex
	decisions []Decision
	version   string
}

func ContextWithDecisionCollector(ctx context.Context) (context.Context, *DecisionCollector) {
	if existing := DecisionCollectorFromContext(ctx); existing != nil {
		return ctx, existing
	}
	collector := &DecisionCollector{}
	return context.WithValue(ctx, decisionCollectorContextKey{}, collector), collector
}

func DecisionCollectorFromContext(ctx context.Context) *DecisionCollector {
	if ctx == nil {
		return nil
	}
	collector, _ := ctx.Value(decisionCollectorContextKey{}).(*DecisionCollector)
	return collector
}

func recordDecision(ctx context.Context, decision Decision, version string) {
	collector := DecisionCollectorFromContext(ctx)
	if collector == nil {
		return
	}
	collector.mu.Lock()
	collector.decisions = append(collector.decisions, decision)
	collector.version = version
	collector.mu.Unlock()
}

// RecordSupplementalDecision appends a server-owned runtime decision without
// replacing the version of the distributed policy snapshot already collected
// for the request.
func RecordSupplementalDecision(ctx context.Context, decision Decision) {
	collector := DecisionCollectorFromContext(ctx)
	if collector == nil {
		return
	}
	collector.mu.Lock()
	collector.decisions = append(collector.decisions, decision)
	collector.mu.Unlock()
}

func (c *DecisionCollector) Snapshot() ([]Decision, string) {
	if c == nil {
		return nil, ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Decision(nil), c.decisions...), c.version
}
