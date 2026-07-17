package gateway

import (
	"bytes"
	"testing"

	"github.com/uniroute/unirouter/internal/guardrail"
)

func TestStreamGuardrailRejectsDuplicateJSONMembers(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "content",
			raw:  `{"choices":[{"index":0,"delta":{"content":"POLICY_MATCH_PLACEHOLDER","content":"SAFE_PLACEHOLDER"}}]}`,
		},
		{
			name: "escaped equivalent content member",
			raw:  `{"choices":[{"index":0,"delta":{"content":"POLICY_MATCH_PLACEHOLDER","\u0063ontent":"SAFE_PLACEHOLDER"}}]}`,
		},
		{
			name: "reasoning content",
			raw:  `{"choices":[{"index":0,"delta":{"reasoning_content":"POLICY_MATCH_PLACEHOLDER","reasoning_content":"SAFE_PLACEHOLDER"}}]}`,
		},
		{
			name: "nested text part",
			raw:  `{"choices":[{"index":0,"delta":{"content":[{"type":"text","text":"POLICY_MATCH_PLACEHOLDER","text":"SAFE_PLACEHOLDER"}]}}]}`,
		},
		{
			name: "function arguments",
			raw:  `{"choices":[{"index":0,"delta":{"function_call":{"arguments":"POLICY_MATCH_PLACEHOLDER","arguments":"SAFE_PLACEHOLDER"}}}]}`,
		},
		{
			name: "structural delta",
			raw:  `{"choices":[{"index":0,"delta":{"content":"POLICY_MATCH_PLACEHOLDER"},"delta":{"content":"SAFE_PLACEHOLDER"}}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseGuardedStreamFrame([]byte(test.raw)); err == nil {
				t.Fatal("duplicate JSON members must be rejected before guardrail evaluation")
			}
			inspection, err := inspectGuardedStreamBatch(
				t.Context(), nil, guardrail.StreamProfile{Enforced: true}, [][]byte{[]byte(test.raw)}, map[string]string{},
			)
			if err == nil || len(inspection.Frames) != 1 || len(inspection.Frames[0].Data) != 0 {
				t.Fatalf("enforced duplicate frame was not withheld: inspection=%+v err=%v", inspection, err)
			}
		})
	}
}

func TestStreamGuardrailDuplicateRejectionPreservesUniqueAndAdvisoryBehavior(t *testing.T) {
	unique := []byte(`{"choices":[{"index":0,"delta":{"content":"SAFE_PLACEHOLDER"}}]}`)
	frame, err := parseGuardedStreamFrame(unique)
	if err != nil {
		t.Fatalf("unique JSON frame was rejected: %v", err)
	}
	encoded, err := frame.data()
	if err != nil || !bytes.Equal(encoded, unique) {
		t.Fatalf("unique raw frame compatibility changed: encoded=%s err=%v", encoded, err)
	}

	duplicate := []byte(`{"choices":[{"index":0,"delta":{"content":"POLICY_MATCH_PLACEHOLDER","content":"SAFE_PLACEHOLDER"}}]}`)
	inspection, err := inspectGuardedStreamBatch(
		t.Context(), nil, guardrail.StreamProfile{Advisory: true}, [][]byte{duplicate}, map[string]string{},
	)
	if err != nil || len(inspection.Frames) != 1 || !bytes.Equal(inspection.Frames[0].Data, duplicate) {
		t.Fatalf("advisory-only policy unexpectedly became preventive: inspection=%+v err=%v", inspection, err)
	}
}
