package modelcapabilityqualification

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"os"
	"testing"
)

type retainedTaskRequest struct {
	Binding Binding `json:"binding"`
	Body    string  `json:"body"`
	SHA256  string  `json:"sha256"`
}

func retainedTaskBindings(t *testing.T) []Binding {
	t.Helper()
	all, err := allBindings()
	require.NoError(t, err)
	out := []Binding{}
	for _, b := range all {
		if b.ProbeContractID == GLMStreamFunctionToolsProbeContractID || b.ProbeContractID == MiniMaxM27StreamFunctionToolsProbeContractID {
			out = append(out, b)
		}
	}
	for _, completion := range []bool{false, true} {
		for _, cap := range []string{modelprofile.CapabilityStreaming, modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningBudget, modelprofile.CapabilityTools, modelprofile.CapabilityFunctionTools, modelprofile.CapabilityToolChoiceAuto} {
			out = append(out, qwenDataContractBinding(t, cap, completion))
		}
	}
	require.Len(t, out, 47)
	return out
}
func TestRetainedStreamTaskRequestBytes(t *testing.T) {
	raw, err := os.ReadFile("testdata/retained_stream_task_requests.json")
	require.NoError(t, err)
	var rows []retainedTaskRequest
	require.NoError(t, json.Unmarshal(raw, &rows))
	require.Len(t, rows, 47)
	expected := map[string]retainedTaskRequest{}
	for _, row := range rows {
		key, err := json.Marshal(row.Binding)
		require.NoError(t, err)
		_, duplicate := expected[string(key)]
		require.False(t, duplicate)
		expected[string(key)] = row
	}
	for _, b := range retainedTaskBindings(t) {
		key, err := json.Marshal(b)
		require.NoError(t, err)
		row, ok := expected[string(key)]
		require.True(t, ok)
		body, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
		require.NoError(t, err)
		require.Equal(t, row.Body, string(body), "exact retained request for %s/%s", b.ExecutionProfileID, b.CapabilityID)
		require.Equal(t, row.SHA256, fmt.Sprintf("%x", sha256.Sum256(body)))
		bad := b
		bad.ExpectedProvider = "unbound-provider"
		rejected, err := BuildOpenAIChatFunctionToolsProbeRequest(bad)
		require.Error(t, err)
		require.Empty(t, rejected)
	}
}
