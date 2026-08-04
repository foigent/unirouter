package promptcompression

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func testConfig() Config {
	return Config{MinimumInputTokens: 4, MinimumSavedTokens: 1, MaximumReductionBPS: 5000}
}

func TestEvaluateBuildsWhitespaceOnlyCandidate(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"system","content":"Keep   system spacing"},{"role":"user","content":"  Alpha                    beta.\n\n\n   Gamma                    delta.  "}],"stream":false}`)
	result := Evaluate(body, testConfig())
	require.True(t, result.Eligible, "%+v", result)
	require.True(t, result.SafetyPassed)
	require.True(t, result.QualityPassed)
	require.Positive(t, result.SavedTokens)
	require.NotContains(t, string(result.CandidateBody), "Alpha     beta")
	require.Contains(t, string(result.CandidateBody), "Keep   system spacing")
	require.Equal(t, stripWhitespace("Keep   system spacing\x00  Alpha     beta.\n\n\n   Gamma      delta.  "), stripWhitespace("Keep   system spacing\x00Alpha beta.\n\nGamma delta."))
}

func TestEvaluateRejectsRiskyOrUnsupportedShapes(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		reason string
	}{
		{"duplicate", `{"messages":[],"messages":[]}`, ReasonInvalidJSON},
		{"stream", `{"stream":true,"messages":[{"role":"user","content":"lots     of     text"}]}`, ReasonStreaming},
		{"tools", `{"tools":[{"type":"function"}],"messages":[{"role":"user","content":"lots     of     text"}]}`, ReasonTools},
		{"tool choice", `{"tool_choice":"auto","messages":[{"role":"user","content":"lots     of     text"}]}`, ReasonTools},
		{"response format", `{"response_format":{"type":"json_object"},"messages":[{"role":"user","content":"lots     of     text"}]}`, ReasonStructuredControls},
		{"multimodal", `{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`, ReasonUnsupportedShape},
		{"code", `{"messages":[{"role":"user","content":"` + "```go\\nfunc  main() {}\\n```" + `"}]}`, ReasonSensitiveSyntax},
		{"unfenced code", `{"messages":[{"role":"user","content":"def  calculate(x):\\n    return  x + 1"}]}`, ReasonSensitiveSyntax},
		{"yaml", `{"messages":[{"role":"user","content":"service:    gateway\\nreplicas:   3"}]}`, ReasonSensitiveSyntax},
		{"url", `{"messages":[{"role":"user","content":"Read   https://example.com/a  now"}]}`, ReasonSensitiveSyntax},
		{"json", `{"messages":[{"role":"user","content":"{\"a\":   1}"}]}`, ReasonSensitiveSyntax},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Evaluate([]byte(test.body), testConfig())
			require.False(t, result.Eligible)
			require.Equal(t, test.reason, result.Reason)
			require.Empty(t, result.CandidateBody)
		})
	}
}

func TestEvaluateDoesNotPersistOrReturnContentInJSON(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"` + strings.Repeat("private     words     ", 40) + `"}]}`)
	result := Evaluate(body, testConfig())
	require.True(t, result.Eligible)
	encoded := []byte(`{"contract_version":"` + result.ContractVersion + `","reason":"` + result.Reason + `"}`)
	require.NotContains(t, string(encoded), "private")
}

func TestEvaluatePreservesExactNumericLexemesOutsideUserContent(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","seed":9223372036854775807,"temperature":0.1234567890123456789,"messages":[{"role":"user","content":"Alpha  beta  gamma  delta  epsilon  zeta  eta  theta"}]}`)
	result := Evaluate(body, Config{MinimumInputTokens: 1, MinimumSavedTokens: 1, MaximumReductionBPS: 5000})
	require.True(t, result.Eligible, "%+v", result)
	require.Contains(t, string(result.CandidateBody), `"seed":9223372036854775807`)
	require.Contains(t, string(result.CandidateBody), `"temperature":0.1234567890123456789`)
}
