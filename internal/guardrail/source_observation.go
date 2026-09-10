package guardrail

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SourcePolicyLimit covers the original maximum of 256 matching policies and
// an unavailable-snapshot receipt in each of the three supported stages. It
// does not grow with output frames, detector texts, findings or retries.
const SourcePolicyLimit = 3 * (256 + 1)

type SourcePolicyObservation struct {
	PolicyRef, PolicyVersion, DetectorRef, DetectorVersion string
	Stage, Mode, Action, FailureMode                       string
	Outcomes, Decisions, Effects                           []string
	Degraded, Mandatory                                    bool
	FirstObservedAt, LastObservedAt                        time.Time
}

type SourcePolicyBuffer struct {
	mu       sync.Mutex
	closed   bool
	overflow bool
	items    map[string]SourcePolicyObservation
}
type sourcePolicyContextKey struct{}

func StartSourceObservations(ctx context.Context) (context.Context, *SourcePolicyBuffer) {
	b := &SourcePolicyBuffer{items: map[string]SourcePolicyObservation{}}
	return context.WithValue(ctx, sourcePolicyContextKey{}, b), b
}
func sourcePolicyBuffer(ctx context.Context) *SourcePolicyBuffer {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(sourcePolicyContextKey{}).(*SourcePolicyBuffer)
	return b
}
func sourceStage(stage string) string { return strings.TrimPrefix(stage, "guardrail_") }
func appendUnique(values []string, value string) []string {
	if !slices.Contains(values, value) {
		values = append(values, value)
		slices.Sort(values)
	}
	return values
}

// RecordSourceResult copies only fixed decision metadata. Texts, paths,
// findings, counts, categories, fingerprints, config and latency never enter
// this buffer. Advisory work finishing after the request closes is not awaited
// or replayed; stream documents explicitly describe partial observations.
func RecordSourceResult(ctx context.Context, stage string, result Result) {
	b := sourcePolicyBuffer(ctx)
	if b == nil {
		return
	}
	stage = sourceStage(stage)
	if stage != StageInput && stage != StageNonStreamOutput && stage != StageStreamOutput {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	now := time.Now()
	for _, r := range result.Receipts {
		if r.Mode != ModeEnforce && r.Mode != ModeShadow {
			continue
		}
		if !slices.Contains([]string{OutcomePass, OutcomeMatch, OutcomeTransform, OutcomeIndeterminate, OutcomeUnavailable, OutcomeSkipped}, r.Outcome) {
			continue
		}
		key := stage + "\x00" + r.PolicyRef + "\x00" + strconv.FormatInt(r.PolicyVersion, 10) + "\x00" + r.DetectorRef + "\x00" + r.DetectorVersion + "\x00" + r.Mode
		v, ok := b.items[key]
		if !ok {
			if len(b.items) >= SourcePolicyLimit {
				b.overflow = true
				continue
			}
			v = SourcePolicyObservation{PolicyRef: r.PolicyRef, PolicyVersion: strconv.FormatInt(r.PolicyVersion, 10), DetectorRef: r.DetectorRef, DetectorVersion: r.DetectorVersion, Stage: stage, Mode: r.Mode, Action: r.Action, FailureMode: r.FailureMode, Mandatory: r.Mandatory, FirstObservedAt: now, Outcomes: []string{}, Decisions: []string{}, Effects: []string{}}
		}
		v.LastObservedAt = now
		v.Degraded = v.Degraded || r.Degraded
		v.Outcomes = appendUnique(v.Outcomes, r.Outcome)
		if result.Decision == DecisionAllow || result.Decision == DecisionBlock {
			v.Decisions = appendUnique(v.Decisions, result.Decision)
		}
		if r.Mode == ModeEnforce && result.Decision == DecisionBlock && result.BlockedBy == r.PolicyRef {
			effect := map[string]string{StageInput: "input_blocked", StageNonStreamOutput: "output_blocked", StageStreamOutput: "stream_terminated"}[stage]
			v.Effects = appendUnique(v.Effects, effect)
		}
		b.items[key] = v
	}
}

// MarkSourceReplacement is called only after the original payload mutation
// succeeds. A detector's proposed transform alone is not a replacement effect.
func MarkSourceReplacement(ctx context.Context, stage string) {
	b := sourcePolicyBuffer(ctx)
	if b == nil {
		return
	}
	stage = sourceStage(stage)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	for key, v := range b.items {
		if v.Stage != stage || v.Mode != ModeEnforce || !slices.Contains(v.Outcomes, OutcomeTransform) {
			continue
		}
		effect := "output_replaced"
		if stage == StageInput {
			effect = "input_replaced"
		}
		v.Effects = appendUnique(v.Effects, effect)
		b.items[key] = v
	}
}
func (b *SourcePolicyBuffer) Close() ([]SourcePolicyObservation, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	keys := make([]string, 0, len(b.items))
	for key := range b.items {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	values := make([]SourcePolicyObservation, 0, len(keys))
	for _, key := range keys {
		v := b.items[key]
		v.Outcomes = slices.Clone(v.Outcomes)
		v.Decisions = slices.Clone(v.Decisions)
		v.Effects = slices.Clone(v.Effects)
		values = append(values, v)
	}
	return values, b.overflow
}
