package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	q "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const diagnosticPrivateMarker = "private_probe_marker"
const diagnosticM27Response = `{"id":"fixture","model":"MiniMax-M2.7","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"<think>private_probe_marker</think>","tool_calls":[{"id":"call_function_fixture_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":600,"completion_tokens":10,"total_tokens":610,"prompt_tokens_details":{"cached_tokens":0}}}`

type diagnosticFailReader struct{ err error }

func (r diagnosticFailReader) Read([]byte) (int, error) { return 0, r.err }

func diagnosticM27Snapshot(t *testing.T, data bool) modelCapabilitySnapshot {
	t.Helper()
	p, ok := modelprofile.Lookup("minimax.m2.7.payg", "1.3.0")
	require.True(t, ok)
	var definition modelprofile.Definition
	if data {
		wire, err := os.ReadFile("../../capability-bundles/minimax-m27-streamed-tools.json")
		require.NoError(t, err)
		var bundle struct {
			Definitions []json.RawMessage `json:"definitions"`
		}
		require.NoError(t, json.Unmarshal(wire, &bundle))
		definition, err = modelprofile.DecodeDefinition(bundle.Definitions[0])
		require.NoError(t, err)
		p = definition.Profile()
	}
	binding := q.Binding{
		OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityCacheReadUsage,
		ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version,
		ProtocolProfileID: p.ProtocolProfile, ProtocolProfileVersion: "1.0.0",
		ProbeContractID: q.MiniMaxPAYGM2ChatFunctionToolsProbeContractID, ProbeContractVersion: "2.0.0",
		ExpectedProvider: p.Provider, ExpectedUpstreamModel: p.UpstreamModelIDs[0], PublicPath: "/v1/chat/completions",
	}
	if data {
		var err error
		binding, err = q.BindDefinition(binding, definition)
		require.NoError(t, err)
	}
	return modelCapabilitySnapshot{
		Provider: p.Provider, BaseURL: "https://api.minimax.cn/v1", AdapterType: adapters.TypeOpenAICompatible,
		UpstreamModel: binding.ExpectedUpstreamModel, OperationID: binding.OperationID, CapabilityID: binding.CapabilityID,
		ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version,
		ProtocolProfileID: binding.ProtocolProfileID, ProtocolProfileVersion: binding.ProtocolProfileVersion,
		ExpectedProvider: binding.ExpectedProvider, ExpectedUpstreamModel: binding.ExpectedUpstreamModel, PublicPath: binding.PublicPath,
		ProbeContractID: binding.ProbeContractID, ProbeContractVersion: binding.ProbeContractVersion,
		Definition: definition, DataBinding: binding,
	}
}

func TestCapabilityNormalizationDiagnosticPreservesFailureAndPrivateBoundary(t *testing.T) {
	tooLarge := strings.Repeat(" ", q.OpenAIChatFunctionToolsMaxResponseBytes+2)
	for _, tc := range []struct {
		name, body, stage string
		readErr           error
		full              bool
	}{
		{name: "partial read", body: diagnosticPrivateMarker, stage: "read_error", readErr: errors.New(diagnosticPrivateMarker)},
		{name: "zero byte read", stage: "read_error", readErr: errors.New(diagnosticPrivateMarker)},
		{name: "canceled", body: "prefix", stage: "read_canceled", readErr: context.Canceled},
		{name: "deadline", stage: "read_deadline", readErr: context.DeadlineExceeded},
		{name: "size bound", body: tooLarge, stage: "response_too_large"},
		{name: "empty full body", stage: "invalid_json", full: true},
		{name: "malformed JSON", body: diagnosticPrivateMarker, stage: "invalid_json", full: true},
		{name: "escaped duplicate", body: `{"private_probe_marker":1,"\u0070rivate_probe_marker":2}`, stage: "duplicate_json", full: true},
		{name: "missing cache usage", body: strings.Replace(diagnosticM27Response, `,"prompt_tokens_details":{"cached_tokens":0}`, "", 1), stage: "adapter_usage", full: true},
		{name: "invalid cache counter", body: strings.Replace(diagnosticM27Response, `"cached_tokens":0`, `"cached_tokens":null`, 1), stage: "adapter_usage", full: true},
		{name: "invalid envelope", body: strings.Replace(diagnosticM27Response, `"id":"fixture"`, `"id":"fixture","base_resp":{"status_code":"private_probe_marker"}`, 1), stage: "", full: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := []byte("0123456789abcdef01234567")
			snapshot := diagnosticM27Snapshot(t, true)
			var err error
			snapshot.EncryptedSecret, err = auth.EncryptKey([]byte(diagnosticPrivateMarker), key)
			require.NoError(t, err)
			h := NewHandler(nil, "fixture", key, nil)
			calls := 0
			h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(*http.Request) (*http.Response, error) {
				calls++
				var reader io.Reader = strings.NewReader(tc.body)
				if tc.readErr != nil {
					reader = io.MultiReader(reader, diagnosticFailReader{tc.readErr})
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(reader)}, nil
			}))
			outcome := h.executeModelCapabilityProbe(context.Background(), snapshot, "fixture")
			require.Equal(t, 1, calls)
			require.True(t, outcome.ProviderSuccess)
			require.False(t, outcome.ContractSuccess)
			require.Equal(t, "provider_function_tools_contract_invalid", outcome.ReasonCode)
			require.Equal(t, "delivery_shape", outcome.ContractErrorCode)
			require.Zero(t, outcome.TotalTokens) // No diagnostic field grants usage authority.
			d := outcome.NormalizationDiagnostic
			if tc.stage == "" {
				// Invalid metadata remains for the existing downstream validator.
				require.Nil(t, d)
				require.Positive(t, outcome.ResponseBytes)
				return
			}
			require.NotNil(t, d)
			require.Equal(t, tc.stage, d.Stage)
			require.Equal(t, tc.full, d.Full)
			captured := []byte(tc.body)
			if len(captured) > q.OpenAIChatFunctionToolsMaxResponseBytes+1 {
				captured = captured[:q.OpenAIChatFunctionToolsMaxResponseBytes+1]
			}
			digest := sha256.Sum256(captured)
			require.Equal(t, hex.EncodeToString(digest[:]), d.BodySHA256)
			require.Equal(t, len(captured), d.Bytes)
			require.Equal(t, d.Bytes, outcome.ResponseBytes)
			serialized, err := json.Marshal(outcome)
			require.NoError(t, err)
			require.NotContains(t, string(serialized), diagnosticPrivateMarker)
		})
	}
}

func TestCapabilityNormalizationDiagnosticKeepsLegacyAndDataSuccess(t *testing.T) {
	for _, data := range []bool{false, true} {
		snapshot := diagnosticM27Snapshot(t, data)
		normalized, err := normalizeOpenAIChatFunctionToolsProbeResponse(context.Background(), adapters.NewOpenAICompatible(), snapshot,
			&http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(diagnosticM27Response))})
		require.NoError(t, err)
		observation, err := q.ValidateNormalizedOpenAIChatFunctionToolsForBinding(modelCapabilityProbeBindingFromSnapshot(snapshot), "application/json", strings.NewReader(string(normalized.Body)))
		require.NoError(t, err)
		require.Equal(t, int64(610), observation.TotalTokens)
	}
}

func TestCapabilityNormalizationDiagnosticProfileFailureAndErrorChain(t *testing.T) {
	snapshot := diagnosticM27Snapshot(t, false)
	snapshot.ExecutionProfileVersion = "unsupported"
	_, err := normalizeOpenAIChatFunctionToolsProbeResponse(context.Background(), adapters.NewOpenAICompatible(), snapshot,
		&http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(diagnosticM27Response))})
	var outcome modelCapabilityProbeOutcome
	applyCapabilityNormalizationDiagnostic(&outcome, err)
	require.Equal(t, "profile_unavailable", outcome.NormalizationDiagnostic.Stage)
	require.True(t, outcome.NormalizationDiagnostic.Full)
	wrapped := capabilityNormalizationError("read_canceled", nil, false, context.Canceled)
	require.ErrorIs(t, wrapped, context.Canceled)
	require.Equal(t, context.Canceled.Error(), wrapped.Error())
	applyCapabilityNormalizationDiagnostic(&outcome, capabilityNormalizationError(diagnosticPrivateMarker, nil, false, errors.New(diagnosticPrivateMarker)))
	require.Equal(t, "profile_unavailable", outcome.NormalizationDiagnostic.Stage)
}
