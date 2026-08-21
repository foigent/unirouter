package limitpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
)

type decisionCollectorContextKey struct{}

type DecisionCollector struct {
	mu        sync.Mutex
	decisions []Decision
	version   string
	versions  map[string]struct{}
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
	if version != "" {
		if collector.versions == nil {
			collector.versions = make(map[string]struct{}, 2)
		}
		collector.versions[version] = struct{}{}
		collector.version = composedDecisionVersion(collector.versions)
	}
	collector.mu.Unlock()
}

func composedDecisionVersion(versions map[string]struct{}) string {
	if len(versions) == 0 {
		return ""
	}
	ordered := make([]string, 0, len(versions))
	for version := range versions {
		ordered = append(ordered, version)
	}
	sort.Strings(ordered)
	if len(ordered) == 1 {
		return ordered[0]
	}
	sum := sha256.Sum256([]byte("unirouter.limit-policy-phases.v1\x00" + strings.Join(ordered, "\x00")))
	return hex.EncodeToString(sum[:8])
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
