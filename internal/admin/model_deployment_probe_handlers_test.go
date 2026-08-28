package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelqualification"
)

type canonicalOperationProbeDoerFunc func(*http.Request) (*http.Response, error)

func (fn canonicalOperationProbeDoerFunc) Do(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestCanonicalOperationProbeIngressIsStrictAndBounded(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "valid", body: `{"credential_binding_id":7,"probe_text":"safe"}`},
		{name: "missing exact binding", body: `{}`},
		{name: "unknown", body: `{"credential_binding_id":7,"model":"fallback"}`},
		{name: "duplicate", body: `{"credential_binding_id":7,"credential_binding_id":8}`},
		{name: "trailing", body: `{"credential_binding_id":7} {}`},
		{name: "oversized text", body: `{"credential_binding_id":7,"probe_text":"` + strings.Repeat("x", canonicalOperationProbeMaxTextBytes+1) + `"}`},
		{name: "oversized body", body: `{"credential_binding_id":7,"probe_text":"` + strings.Repeat("x", canonicalOperationProbeMaxRequestBytes) + `"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/probe", strings.NewReader(test.body))
			response := httptest.NewRecorder()
			got, err := decodeCanonicalOperationProbeRequest(response, request)
			if test.name == "valid" {
				require.NoError(t, err)
				require.Equal(t, int64(7), got.CredentialBindingID)
				require.Equal(t, "safe", got.ProbeText)
				return
			}
			require.ErrorIs(t, err, errCanonicalOperationProbeInvalid)
		})
	}
}

func TestCanonicalOperationProbeBuildsExactChatAndEmbeddingsRequests(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)

	for _, test := range []struct {
		name          string
		operation     modeloperation.ID
		wantPath      string
		responseBody  string
		assertPayload func(*testing.T, map[string]json.RawMessage)
	}{
		{
			name: "chat completions", operation: modeloperation.IDChatCompletions,
			wantPath:     "/v1/chat/completions",
			responseBody: `{"model":"provider-chat","choices":[{"message":{"role":"assistant","content":"OK"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
			assertPayload: func(t *testing.T, payload map[string]json.RawMessage) {
				require.Contains(t, payload, "messages")
				require.JSONEq(t, `1`, string(payload["max_tokens"]))
				require.JSONEq(t, `false`, string(payload["stream"]))
				require.NotContains(t, payload, "tools")
			},
		},
		{
			name: "embeddings", operation: modeloperation.IDEmbeddings,
			wantPath:     "/v1/embeddings",
			responseBody: `{"model":"provider-embedding","data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`,
			assertPayload: func(t *testing.T, payload map[string]json.RawMessage) {
				require.Contains(t, payload, "input")
				require.NotContains(t, payload, "messages")
				require.NotContains(t, payload, "tools")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			handler := &Handler{encKey: encryptionKey}
			handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				deadline, bounded := request.Context().Deadline()
				require.True(t, bounded)
				require.Positive(t, time.Until(deadline))
				require.LessOrEqual(t, time.Until(deadline), canonicalOperationProbeTimeout)
				require.Equal(t, test.wantPath, request.URL.Path)
				require.Equal(t, "Bearer provider-secret", request.Header.Get("Authorization"))
				require.Equal(t, "probe-request", request.Header.Get("X-Request-ID"))
				body, readErr := io.ReadAll(request.Body)
				require.NoError(t, readErr)
				var payload map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(body, &payload))
				require.JSONEq(t, `"upstream-exact"`, string(payload["model"]))
				test.assertPayload(t, payload)
				return &http.Response{
					StatusCode: http.StatusOK, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(test.responseBody)),
				}, nil
			}))

			snapshot := canonicalOperationProbeUnitSnapshot(t, test.operation, encrypted)
			outcome := handler.executeCanonicalOperationProbe(context.Background(), snapshot, "safe text", "probe-request")
			require.Equal(t, 1, calls)
			require.True(t, outcome.ProviderSuccess)
			require.Equal(t, "ok", outcome.ReasonCode)
			require.Equal(t, http.StatusOK, outcome.StatusCode)
			require.NotEmpty(t, outcome.ReportedModel)
		})
	}
}

func TestCanonicalOperationProbeRejectsOversizedAndInvalidSuccessResponses(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)

	for _, test := range []struct {
		name   string
		body   string
		reason string
	}{
		{name: "oversized", body: strings.Repeat("x", canonicalOperationProbeMaxResponseBytes+1), reason: "provider_response_too_large"},
		{name: "missing chat choices", body: `{"model":"provider-chat","usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, reason: "provider_response_invalid"},
		{name: "null chat message", body: `{"model":"provider-chat","choices":[{"message":null}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, reason: "provider_response_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := &Handler{encKey: encryptionKey}
			handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(test.body)),
				}, nil
			}))
			outcome := handler.executeCanonicalOperationProbe(
				context.Background(), canonicalOperationProbeUnitSnapshot(t, modeloperation.IDChatCompletions, encrypted), "", "probe-request",
			)
			require.False(t, outcome.ProviderSuccess)
			require.Equal(t, test.reason, outcome.ReasonCode)
		})
	}
}

func TestCanonicalOperationProbeDefaultTransportBoundsTimeAndHeadersAndRejectsRedirects(t *testing.T) {
	client, ok := (&Handler{}).canonicalOperationProbeDoer().(*http.Client)
	require.True(t, ok)
	require.Equal(t, canonicalOperationProbeTimeout, client.Timeout)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.Equal(t, int64(canonicalOperationProbeMaxHeaderBytes), transport.MaxResponseHeaderBytes)
	require.ErrorIs(t, client.CheckRedirect(&http.Request{}, nil), http.ErrUseLastResponse)
}

func TestCanonicalOperationProbeUsesConfiguredAzureAdapterForEmbeddings(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("azure-secret"), encryptionKey)
	require.NoError(t, err)
	handler := &Handler{encKey: encryptionKey}
	handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/openai/deployments/azure-deployment/embeddings", request.URL.Path)
		require.Equal(t, "2024-10-21", request.URL.Query().Get("api-version"))
		require.Equal(t, "azure-secret", request.Header.Get("api-key"))
		require.Empty(t, request.Header.Get("Authorization"))
		body, readErr := io.ReadAll(request.Body)
		require.NoError(t, readErr)
		var payload map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Contains(t, payload, "input")
		require.NotContains(t, payload, "model")
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"model":"azure-reported","data":[{"embedding":[0.1]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`,
			)),
		}, nil
	}))
	snapshot := canonicalOperationProbeUnitSnapshot(t, modeloperation.IDEmbeddings, encrypted)
	snapshot.BaseURL = "https://azure.example"
	snapshot.AdapterType = "azure_openai"
	snapshot.AdapterConfig = json.RawMessage(`{"api_version":"2024-10-21","deployment":"azure-deployment"}`)
	outcome := handler.executeCanonicalOperationProbe(context.Background(), snapshot, "azure probe", "probe-request")
	require.True(t, outcome.ProviderSuccess, "outcome=%+v", outcome)
	require.Equal(t, "ok", outcome.ReasonCode)
	require.Equal(t, "azure-reported", outcome.ReportedModel)
}

func TestCanonicalOperationProbeValidatesTheConfiguredAdaptersNormalizedChatResponse(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("anthropic-secret"), encryptionKey)
	require.NoError(t, err)
	handler := &Handler{encKey: encryptionKey}
	handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1/messages", request.URL.Path)
		require.Equal(t, "anthropic-secret", request.Header.Get("x-api-key"))
		require.Empty(t, request.Header.Get("Authorization"))
		body, readErr := io.ReadAll(request.Body)
		require.NoError(t, readErr)
		var payload map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &payload))
		require.JSONEq(t, `1`, string(payload["max_tokens"]))
		require.Contains(t, payload, "messages")
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"id":"msg_probe","type":"message","role":"assistant","model":"claude-reported","content":[{"type":"text","text":"OK"}],"stop_reason":"max_tokens","usage":{"input_tokens":2,"output_tokens":1}}`,
			)),
		}, nil
	}))
	snapshot := canonicalOperationProbeUnitSnapshot(t, modeloperation.IDChatCompletions, encrypted)
	snapshot.BaseURL = "https://api.anthropic.com"
	snapshot.Provider = "anthropic"
	snapshot.AdapterType = "anthropic_native"
	outcome := handler.executeCanonicalOperationProbe(context.Background(), snapshot, "anthropic probe", "probe-request")
	require.True(t, outcome.ProviderSuccess, "outcome=%+v", outcome)
	require.Equal(t, "ok", outcome.ReasonCode)
	require.Equal(t, "claude-reported", outcome.ReportedModel)
}

func TestCanonicalOperationProbeSupportsFourExactOperationContracts(t *testing.T) {
	for _, operation := range []modeloperation.ID{"unknown.operation.v1"} {
		_, err := canonicalOperationProbeSpecFor(operation)
		require.ErrorIs(t, err, errCanonicalOperationProbeUnsupported)
	}
	for _, operation := range []modeloperation.ID{
		modeloperation.IDChatCompletions, modeloperation.IDEmbeddings,
		modeloperation.IDResponses, modeloperation.IDMessages,
	} {
		spec, err := canonicalOperationProbeSpecFor(operation)
		require.NoError(t, err)
		if operation == modeloperation.IDResponses || operation == modeloperation.IDMessages {
			require.NotNil(t, spec.ExecutionBinding)
			require.Equal(t, modelqualification.LiveProbeSourceKind, spec.SourceKind)
		} else {
			require.Nil(t, spec.ExecutionBinding)
		}
	}
	v2, err := canonicalOperationProbeSpecForProfile(
		modeloperation.IDResponses,
		endpointprotocol.ProfileOpenAIResponsesTextV2,
		"1.0.0",
	)
	require.NoError(t, err)
	require.NotNil(t, v2.ExecutionBinding)
	require.Equal(t, modelqualification.ResponsesDualExecutorContractV1, v2.ExecutionBinding.ExecutorContractVersion)
	require.Equal(t, endpointprotocol.ProfileOpenAIResponsesTextV2, v2.ProfileID)

	_, err = canonicalOperationProbeSpecForProfile(
		modeloperation.IDResponses,
		endpointprotocol.ProfileOpenAIResponsesTextV2,
		"2.0.0",
	)
	require.ErrorIs(t, err, errCanonicalOperationProbeUnsupported)
}

func TestCanonicalOperationProbeHandlerRejectsUnsupportedOperationBeforeStorageOrFallback(t *testing.T) {
	handler := &Handler{}
	for _, test := range []struct {
		operation string
		status    int
		code      string
	}{
		{operation: "unknown.operation.v1", status: http.StatusUnprocessableEntity, code: "canonical_operation_probe_unsupported"},
		{operation: " openai.chat_completions.v1", status: http.StatusBadRequest, code: "canonical_operation_probe_invalid"},
	} {
		request := httptest.NewRequest(http.MethodPost, "/probe", strings.NewReader(`{"credential_binding_id":7}`))
		request.SetPathValue("deploymentID", "9")
		request.SetPathValue("operationID", test.operation)
		response := httptest.NewRecorder()
		handler.ProbeCanonicalModelOperation(response, request)
		require.Equal(t, test.status, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), test.code)
	}
}

func TestCanonicalOperationProbeRunsResponsesAndMessagesThroughExactEndpointProtocolNetworkContract(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)

	for _, test := range []struct {
		name         string
		operation    modeloperation.ID
		wantPath     string
		responseBody string
		assertWire   func(*testing.T, *http.Request, map[string]json.RawMessage)
	}{
		{
			name: "responses", operation: modeloperation.IDResponses, wantPath: "/v1/responses",
			responseBody: `{"id":"resp_probe","object":"response","status":"completed","model":"upstream-exact","error":null,"incomplete_details":null,"output":[{"id":"msg_probe","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`,
			assertWire: func(t *testing.T, request *http.Request, payload map[string]json.RawMessage) {
				require.Equal(t, "Bearer provider-secret", request.Header.Get("Authorization"))
				require.Equal(t, "probe-request", request.Header.Get("X-Client-Request-Id"))
				require.JSONEq(t, `"safe text"`, string(payload["input"]))
				require.JSONEq(t, `16`, string(payload["max_output_tokens"]))
				require.JSONEq(t, `false`, string(payload["store"]))
				require.JSONEq(t, `false`, string(payload["stream"]))
			},
		},
		{
			name: "messages", operation: modeloperation.IDMessages, wantPath: "/v1/messages",
			responseBody: `{"id":"msg_probe","type":"message","role":"assistant","model":"upstream-exact","content":[{"type":"text","text":"OK"}],"stop_reason":"max_tokens","stop_sequence":null,"usage":{"input_tokens":2,"output_tokens":1}}`,
			assertWire: func(t *testing.T, request *http.Request, payload map[string]json.RawMessage) {
				require.Equal(t, "provider-secret", request.Header.Get("x-api-key"))
				require.Equal(t, "2023-06-01", request.Header.Get("anthropic-version"))
				require.Empty(t, request.Header.Get("Authorization"))
				require.JSONEq(t, `1`, string(payload["max_tokens"]))
				require.JSONEq(t, `false`, string(payload["stream"]))
				require.Contains(t, payload, "messages")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			handler := &Handler{encKey: encryptionKey}
			handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, "provider.example", request.URL.Host)
				require.Equal(t, test.wantPath, request.URL.Path)
				body, readErr := io.ReadAll(request.Body)
				require.NoError(t, readErr)
				var payload map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(body, &payload))
				require.JSONEq(t, `"upstream-exact"`, string(payload["model"]))
				test.assertWire(t, request, payload)
				return &http.Response{
					StatusCode: http.StatusOK, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(test.responseBody)),
				}, nil
			}))
			snapshot := canonicalOperationProbeUnitSnapshot(t, test.operation, encrypted)
			outcome := handler.executeCanonicalOperationProbe(context.Background(), snapshot, "safe text", "probe-request")
			require.Equal(t, 1, calls)
			require.True(t, outcome.ProviderSuccess, "outcome=%+v", outcome)
			require.Equal(t, "ok", outcome.ReasonCode)
			require.Equal(t, "upstream-exact", outcome.ReportedModel)
			require.Positive(t, outcome.OutputCount)
			require.NotEmpty(t, outcome.Units)
		})
	}
}

func TestCanonicalOperationProbeRejectsNonTerminalOrUsageLessEndpointProtocolSuccess(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)
	for _, test := range []struct {
		name      string
		operation modeloperation.ID
		body      string
	}{
		{
			name: "responses non-final", operation: modeloperation.IDResponses,
			body: `{"id":"resp_pending","object":"response","status":"in_progress","model":"upstream-exact","output":[],"usage":null}`,
		},
		{
			name: "responses missing usage", operation: modeloperation.IDResponses,
			body: `{"id":"resp_probe","object":"response","status":"completed","model":"upstream-exact","error":null,"incomplete_details":null,"output":[{"id":"msg_probe","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"OK"}]}]}`,
		},
		{
			name: "messages missing usage", operation: modeloperation.IDMessages,
			body: `{"id":"msg_probe","type":"message","role":"assistant","model":"upstream-exact","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","stop_sequence":null}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := &Handler{encKey: encryptionKey}
			handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.body))}, nil
			}))
			outcome := handler.executeCanonicalOperationProbe(
				context.Background(), canonicalOperationProbeUnitSnapshot(t, test.operation, encrypted), "safe", "probe-request",
			)
			require.False(t, outcome.ProviderSuccess)
			require.Equal(t, "provider_response_invalid", outcome.ReasonCode)
		})
	}
}

func TestCanonicalOperationProbeActorAuthoritySeparatesDeploymentAndCustomerOrganizations(t *testing.T) {
	const customerOrgID int64 = 41
	require.True(t, canonicalOperationProbeActorAllowed(canonicalOperationProbeActorSnapshot{
		UserID: 7, OrgID: 99, OrganizationRole: auth.RolePlatformAdmin,
	}, customerOrgID))
	require.True(t, canonicalOperationProbeActorAllowed(canonicalOperationProbeActorSnapshot{
		UserID: 8, OrgID: customerOrgID, OrganizationRole: auth.RoleOrgAdmin,
	}, customerOrgID))
	require.False(t, canonicalOperationProbeActorAllowed(canonicalOperationProbeActorSnapshot{
		UserID: 9, OrgID: 99, OrganizationRole: auth.RoleOrgAdmin,
	}, customerOrgID))
	require.False(t, canonicalOperationProbeActorAllowed(canonicalOperationProbeActorSnapshot{
		UserID: 10, OrgID: customerOrgID, OrganizationRole: auth.RoleOrgViewer,
	}, customerOrgID))

	before := canonicalOperationProbeActorSnapshot{
		UserID: 7, OrgID: 99, OrganizationRole: auth.RolePlatformAdmin,
		CompatibilityRole: auth.RolePlatformAdmin, AuthEpoch: 3,
	}
	require.True(t, canonicalOperationProbeActorMatches(before, before))
	after := before
	after.AuthEpoch++
	require.False(t, canonicalOperationProbeActorMatches(before, after))
}

func TestCanonicalOperationProbeRouteInputsExcludeOnlyOperationEvidenceRevision(t *testing.T) {
	before := canonicalOperationProbeSnapshot{
		CustomerOrgID: 41, DeploymentID: 51, ConnectionID: 61, CredentialBindingID: 71,
		ConnectionState: "enabled", ConnectionRevision: 2,
		DeploymentState: "enabled", DeploymentRevision: 3, UpstreamModel: "upstream-exact",
		OperationID: string(modeloperation.IDChatCompletions), OperationSupportState: "enabled",
		OperationEvidenceState: "declared", OperationRevision: 4,
		ProfileID: "openai.chat_completions", ProfileVersion: "1.0.0",
		EncryptedSecret: "ciphertext", CredentialStatus: "active", CredentialRevision: 5,
		CredentialEligible: true, LinkEligible: true,
		LinkCreatedAt: time.Date(2026, 8, 20, 1, 2, 3, 0, time.UTC),
	}
	current := before
	current.OperationEvidenceState = "live_probed"
	current.OperationRevision++
	require.True(t, canonicalOperationProbeRouteInputsMatch(before, current),
		"a concurrent evidence-only operation update must not poison successful provider evidence")
	require.NotEqual(t, before.OperationRevision, current.OperationRevision,
		"the full current snapshot must still be reported stale and must not advance twice")

	for _, mutate := range []func(*canonicalOperationProbeSnapshot){
		func(snapshot *canonicalOperationProbeSnapshot) { snapshot.ConnectionRevision++ },
		func(snapshot *canonicalOperationProbeSnapshot) { snapshot.DeploymentRevision++ },
		func(snapshot *canonicalOperationProbeSnapshot) { snapshot.CredentialRevision++ },
		func(snapshot *canonicalOperationProbeSnapshot) { snapshot.UpstreamModel = "changed" },
		func(snapshot *canonicalOperationProbeSnapshot) { snapshot.ProfileVersion = "2.0.0" },
		func(snapshot *canonicalOperationProbeSnapshot) {
			snapshot.LinkCreatedAt = snapshot.LinkCreatedAt.Add(time.Nanosecond)
		},
	} {
		changed := current
		mutate(&changed)
		require.False(t, canonicalOperationProbeRouteInputsMatch(before, changed))
	}
}

func canonicalOperationProbeUnitSnapshot(t *testing.T, operation modeloperation.ID, encrypted string) canonicalOperationProbeSnapshot {
	t.Helper()
	spec, err := canonicalOperationProbeSpecFor(operation)
	require.NoError(t, err)
	snapshot := canonicalOperationProbeSnapshot{
		ConnectionName: "exact", Provider: "openai", BaseURL: "https://provider.example/v1",
		AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{}`),
		UpstreamModel: "upstream-exact", OperationID: string(operation),
		ProfileID: spec.ProfileID, ProfileVersion: spec.ProfileVersion,
		EncryptedSecret: encrypted,
	}
	if spec.ExecutionBinding != nil {
		snapshot.BaseURL = "https://provider.example"
	}
	return snapshot
}
