package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	qualification "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

type meaningfulProbeRoundTripper func(*http.Request) (*http.Response, error)

func (f meaningfulProbeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func meaningfulCapabilityFixture(t *testing.T) (*Handler, modelCapabilitySnapshot, string) {
	t.Helper()
	wire, err := os.ReadFile("../../capability-bundles/deepseek-v41-flash-provider-completion.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	d, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	row := bundle.BindingDelta[0]
	b, err := qualification.BindDefinition(qualification.Binding{OperationID: row.OperationID, CapabilityID: row.CapabilityID,
		ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion,
		ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion,
		ProbeContractID: row.ProbeContractID, ProbeContractVersion: qualification.DeepSeekMeaningfulStreamProbeVersion,
		ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath}, d)
	require.NoError(t, err)
	key := []byte("0123456789abcdef01234567")
	secret, err := auth.EncryptKey([]byte("synthetic-probe-only"), key)
	require.NoError(t, err)
	snapshot := modelCapabilitySnapshot{Definition: d, DataBinding: b, Provider: row.ExpectedProvider,
		BaseURL: "https://api.deepseek.com", AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`),
		UpstreamModel: row.ExpectedUpstreamModel, OperationID: row.OperationID, CapabilityID: row.CapabilityID,
		ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion,
		ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion,
		ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath,
		ProbeContractID: b.ProbeContractID, ProbeContractVersion: b.ProbeContractVersion, EncryptedSecret: secret}
	fixture, err := os.ReadFile("../adapters/testdata/deepseek_provider_completion_v6.sse")
	require.NoError(t, err)
	return NewHandler(nil, "synthetic-probe-jwt", key, nil), snapshot, strings.ReplaceAll(string(fixture), "UNIROUTER_TOOL_PROBE_OK", "6429")
}

func TestMeaningfulCapabilityActualDispatchUsesOwn30SecondClientAndParentDeadline(t *testing.T) {
	// Private process-local test clients are restored; this test is deliberately
	// serialized. It exercises executeModelCapabilityProbe, not just constants.
	oldCapability, oldOperation := modelCapabilityProbeDefaultHTTPClient, canonicalOperationProbeDefaultHTTPClient
	t.Cleanup(func() {
		modelCapabilityProbeDefaultHTTPClient, canonicalOperationProbeDefaultHTTPClient = oldCapability, oldOperation
	})
	capability, operation := newModelCapabilityProbeHTTPClient(), newCanonicalOperationProbeHTTPClient()
	require.Equal(t, 30*time.Second, capability.Timeout)
	require.Equal(t, 15*time.Second, operation.Timeout)
	require.NotSame(t, capability.Transport, operation.Transport)
	require.Equal(t, int64(64<<10), capability.Transport.(*http.Transport).MaxResponseHeaderBytes)
	require.ErrorIs(t, capability.CheckRedirect(&http.Request{}, nil), http.ErrUseLastResponse)
	modelCapabilityProbeDefaultHTTPClient, canonicalOperationProbeDefaultHTTPClient = capability, operation
	h, snapshot, stream := meaningfulCapabilityFixture(t)
	operationCalls := 0
	operation.Transport = meaningfulProbeRoundTripper(func(r *http.Request) (*http.Response, error) {
		operationCalls++
		deadline, ok := r.Context().Deadline()
		require.True(t, ok)
		require.InDelta(t, 15, time.Until(deadline).Seconds(), 1)
		return nil, fmt.Errorf("synthetic operation transport stop")
	})
	for _, budget := range []time.Duration{30 * time.Second, 3 * time.Second} {
		t.Run(budget.String(), func(t *testing.T) {
			calls := 0
			capability.Transport = meaningfulProbeRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				deadline, ok := r.Context().Deadline()
				require.True(t, ok)
				remaining := time.Until(deadline)
				require.Greater(t, remaining, budget-time.Second)
				require.LessOrEqual(t, remaining, budget)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, "32c62ada6083f2fd5c46321c51039dafc18d417babe2cd7676dbd937ee2b2a0c", fmt.Sprintf("%x", sha256.Sum256(body)))
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}, nil
			})
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			result := h.executeModelCapabilityProbe(ctx, snapshot, "synthetic-native-meaningful")
			require.True(t, result.ContractSuccess, result.ReasonCode)
			require.True(t, result.ReasoningObserved)
			require.True(t, result.ToolCallObserved)
			require.Equal(t, 1, calls)
			require.Zero(t, operationCalls)
		})
	}
	// Existing explicit administrative test transport injection remains honored.
	overrides := 0
	h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
		overrides++
		return nil, context.Canceled
	}))
	result := h.executeModelCapabilityProbe(t.Context(), snapshot, "synthetic-override")
	require.Equal(t, 1, overrides)
	require.Equal(t, "provider_request_failed", result.ReasonCode)
	require.False(t, result.ContractSuccess)
	require.True(t, result.OutcomeAmbiguous)
	request, err := http.NewRequest(http.MethodPost, "https://api.deepseek.com/chat/completions", nil)
	require.NoError(t, err)
	_, err = (&Handler{}).canonicalOperationProbeDoer().Do(request)
	require.Error(t, err)
	require.Equal(t, 1, operationCalls)
}
