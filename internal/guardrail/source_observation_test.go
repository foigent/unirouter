package guardrail

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourcePolicyBufferUsesRealResultAndDoesNotRetainFindings(t *testing.T) {
	for _, mode := range []string{ModeShadow, ModeEnforce} {
		t.Run(mode, func(t *testing.T) {
			service := newGuardrailTestService(t, nil)
			policy := Policy{ID: 42, Ref: "synthetic-pii", Name: "synthetic", Version: 1, Stage: StageInput, DetectorRef: DetectorPII, Action: ActionRedact, Mode: mode, FailureMode: FailureFailClosed}
			installPolicies(service, policy)
			result := service.Pin(Subject{OrgID: 1}).Evaluate(t.Context(), StageInput, []Text{{Path: "synthetic-text", Content: "Contact fake.person@example.invalid"}})
			require.NotEmpty(t, result.Receipts)
			ctx, buffer := StartSourceObservations(t.Context())
			RecordSourceResult(ctx, StageInput, result)
			// A proposed transform has no applied effect until the original writer says so.
			interim := buffer.items
			for _, v := range interim {
				require.Empty(t, v.Effects)
			}
			if mode == ModeEnforce {
				MarkSourceReplacement(ctx, StageInput)
			}
			values, overflow := buffer.Close()
			require.False(t, overflow)
			require.Len(t, values, 1)
			if mode == ModeShadow {
				require.Empty(t, values[0].Effects)
				require.Contains(t, values[0].Outcomes, OutcomeMatch)
			} else {
				require.Equal(t, []string{"input_replaced"}, values[0].Effects)
				require.Contains(t, values[0].Outcomes, OutcomeTransform)
			}
			wire, err := json.Marshal(values)
			require.NoError(t, err)
			for _, private := range []string{"fake.person@example.invalid", "synthetic-text", "Fingerprints", "Categories", "MatchCount", "Texts"} {
				require.NotContains(t, string(wire), private)
			}
			for _, fingerprint := range result.Receipts[0].Fingerprints {
				require.NotContains(t, string(wire), fingerprint)
			}
		})
	}
}

func TestSourcePolicyStreamSummaryBoundedAndClosedWithoutAwaitingAdvisory(t *testing.T) {
	ctx, buffer := StartSourceObservations(t.Context())
	result := Result{Decision: DecisionAllow, Texts: []Text{{Content: strings.Repeat("secret", 100)}}, Receipts: []Receipt{{PolicyRef: "synthetic-stream", PolicyVersion: 1, Stage: StageStreamOutput, DetectorRef: DetectorPII, DetectorVersion: "1", Mode: ModeShadow, Action: ActionBlock, Outcome: OutcomeMatch, FailureMode: FailureFailClosed}}}
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				RecordSourceResult(ctx, StageStreamOutput, result)
			}
		}()
	}
	workers.Wait()
	values, overflow := buffer.Close()
	require.False(t, overflow)
	require.Len(t, values, 1)
	require.Empty(t, values[0].Effects)
	result.Receipts[0].Outcome = OutcomeUnavailable
	RecordSourceResult(ctx, StageStreamOutput, result)
	again, _ := buffer.Close()
	require.Equal(t, values, again)
	values[0].Outcomes[0] = "changed"
	again, _ = buffer.Close()
	require.Equal(t, []string{OutcomeMatch}, again[0].Outcomes)
}
