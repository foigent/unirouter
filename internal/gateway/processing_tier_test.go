package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

type processingTierDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadline  time.Time
	deadlines []time.Time
	err       error
}

type processingTierDeadlineWrapper struct {
	http.ResponseWriter
}

func (writer *processingTierDeadlineWrapper) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *processingTierDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	writer.deadline = deadline
	writer.deadlines = append(writer.deadlines, deadline)
	return writer.err
}

func (writer *processingTierDeadlineWriter) maximumDeadline() time.Time {
	maximum := time.Time{}
	for _, deadline := range writer.deadlines {
		if deadline.After(maximum) {
			maximum = deadline
		}
	}
	return maximum
}

func TestConfigureProviderWriteDeadlineCoversTypedTierAndReasoningPolicies(t *testing.T) {
	writer := &processingTierDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	before := time.Now().Add(15*time.Minute + time.Minute)
	require.NoError(t, configureProviderWriteDeadline(writer, modelExecutionProfileEvidence{
		ProcessingTierPolicy:    modelprofile.ProcessingTierPolicyGeminiOpenAICompatibilityV1,
		ProcessingTierEffective: modelprofile.ProcessingTierFlex,
	}, 15*time.Minute))
	require.False(t, writer.deadline.Before(before))
	require.True(t, writer.deadline.Before(time.Now().Add(16*time.Minute+time.Second)))

	standard := &processingTierDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	require.NoError(t, configureProviderWriteDeadline(standard, modelExecutionProfileEvidence{
		ProcessingTierPolicy:    modelprofile.ProcessingTierPolicyGeminiOpenAICompatibilityV1,
		ProcessingTierEffective: modelprofile.ProcessingTierStandard,
	}, 5*time.Minute))
	require.False(t, standard.deadline.IsZero())
	require.True(t, standard.deadline.After(time.Now().Add(5*time.Minute)))

	unmodeled := &processingTierDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	require.NoError(t, configureProviderWriteDeadline(unmodeled, modelExecutionProfileEvidence{}, 5*time.Minute))
	require.True(t, unmodeled.deadline.IsZero())

	wantErr := errors.New("deadline unavailable")
	failing := &processingTierDeadlineWriter{ResponseRecorder: httptest.NewRecorder(), err: wantErr}
	require.ErrorIs(t, configureProviderWriteDeadline(failing, modelExecutionProfileEvidence{
		ProcessingTierPolicy:    modelprofile.ProcessingTierPolicyGeminiOpenAICompatibilityV1,
		ProcessingTierEffective: modelprofile.ProcessingTierFlex,
	}, 15*time.Minute), wantErr)

	wrappedTarget := &processingTierDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	wrapped := &processingTierDeadlineWrapper{ResponseWriter: wrappedTarget}
	require.NoError(t, configureProviderWriteDeadline(wrapped, modelExecutionProfileEvidence{
		ProcessingTierPolicy:    modelprofile.ProcessingTierPolicyGeminiOpenAICompatibilityV1,
		ProcessingTierEffective: modelprofile.ProcessingTierFlex,
	}, 15*time.Minute))
	require.False(t, wrappedTarget.deadline.IsZero(), "ResponseController must reach the server writer through middleware")

	reasoning := &processingTierDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	reasoningBefore := time.Now().Add(30*time.Minute + time.Minute)
	require.NoError(t, configureProviderWriteDeadline(reasoning, modelExecutionProfileEvidence{
		ReasoningTimeoutSeconds: 1_800,
	}, 30*time.Minute))
	require.False(t, reasoning.deadline.Before(reasoningBefore))
	require.True(t, reasoning.deadline.Before(time.Now().Add(31*time.Minute+time.Second)))
}

func TestProcessingTierResponseMetadataRequiresProviderEvidence(t *testing.T) {
	ctx := contextWithProcessingTierEvidence(t.Context(), modelExecutionProfileEvidence{
		ProcessingTierPolicy:    modelprofile.ProcessingTierPolicyGeminiOpenAICompatibilityV1,
		ProcessingTierRequest:   "priority",
		ProcessingTierEffective: modelprofile.ProcessingTierPriority,
		ProcessingTierExplicit:  true,
	})

	missing := processingTierResponseMetadata(ctx, http.Header{})
	require.Equal(t, "missing", missing["processing_tier_observation"])
	require.NotContains(t, missing, "processing_tier_observed")

	unknown := processingTierResponseMetadata(ctx, http.Header{"X-Gemini-Service-Tier": []string{"future"}})
	require.Equal(t, "unknown", unknown["processing_tier_observation"])
	require.NotContains(t, unknown, "processing_tier_observed")

	conflicting := processingTierResponseMetadata(ctx, http.Header{"X-Gemini-Service-Tier": []string{"priority", "standard"}})
	require.Equal(t, "conflicting", conflicting["processing_tier_observation"])
	require.NotContains(t, conflicting, "processing_tier_observed")
	require.NotContains(t, conflicting, "processing_tier_disposition")

	confirmed := processingTierResponseMetadata(ctx, http.Header{"X-Gemini-Service-Tier": []string{"priority"}})
	require.Equal(t, "priority", confirmed["processing_tier_observed"])
	require.Equal(t, "confirmed", confirmed["processing_tier_disposition"])

	downgraded := processingTierResponseMetadata(ctx, http.Header{"X-Gemini-Service-Tier": []string{"standard"}})
	require.Equal(t, "standard", downgraded["processing_tier_observed"])
	require.Equal(t, "provider_downgraded", downgraded["processing_tier_disposition"])
	require.Equal(t, "response_header:x-gemini-service-tier", downgraded["processing_tier_observation_source"])
}

func TestProcessingTierResponseMetadataDoesNotAffectUnmodeledProfiles(t *testing.T) {
	require.Empty(t, processingTierResponseMetadata(t.Context(), http.Header{
		"X-Gemini-Service-Tier": []string{"priority"},
	}))
}

func TestRejectedProcessingTierResponseRecordsHeaderWithoutSuccessDisposition(t *testing.T) {
	ctx := contextWithProcessingTierEvidence(t.Context(), modelExecutionProfileEvidence{
		ProcessingTierPolicy:    modelprofile.ProcessingTierPolicyGeminiOpenAICompatibilityV1,
		ProcessingTierRequest:   "priority",
		ProcessingTierEffective: modelprofile.ProcessingTierPriority,
		ProcessingTierExplicit:  true,
	})

	metadata := withProcessingTierResponseMetadata(ctx, http.Header{
		"X-Gemini-Service-Tier": []string{"standard"},
	}, map[string]interface{}{"failure_kind": "rate_limit"})
	require.Equal(t, "observed_on_rejected_response", metadata["processing_tier_observation"])
	require.Equal(t, "standard", metadata["processing_tier_observed"])
	require.NotContains(t, metadata, "processing_tier_disposition")
	require.Equal(t, "rate_limit", metadata["failure_kind"])
}

func TestProcessingTierAdmissionErrorsRemainDistinct(t *testing.T) {
	status, code, _, ok := processingTierAdmissionErrorDetails(errProcessingTierContractUnavailable)
	require.True(t, ok)
	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, "billing_error.processing_tier_contract_unavailable", code)

	status, code, _, ok = processingTierAdmissionErrorDetails(errProcessingTierStateUnavailable)
	require.True(t, ok)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "billing_error.processing_tier_state_unavailable", code)
}

func TestDoubaoSeedEvolvingReasoningHandlerUsesTypedDeadlineBeforeFinancialSideEffects(t *testing.T) {
	newRelay := func(t *testing.T, invalidEncryptedSecret bool, doer executionRoundTripFunc, billingProvider *mockBilling) (*Relay, *executionCoordinatorStub) {
		t.Helper()
		encKey := []byte("0123456789abcdef0123456789abcdef")
		now := time.Now().UTC().Truncate(time.Microsecond)
		capabilities := []executionevidence.CapabilityEntry{
			{
				CapabilityID: modelprofile.CapabilityReasoning, SupportState: "enabled",
				EvidenceState: "production_approved", EvidenceRef: "fixture://reasoning",
				VerifiedAt: &now, Revision: 1,
			},
			{
				CapabilityID: modelprofile.CapabilityReasoningEffort, SupportState: "enabled",
				EvidenceState: "production_approved", EvidenceRef: "fixture://reasoning-effort",
				VerifiedAt: &now, Revision: 1,
			},
		}
		plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions,
			"https://ark.cn-beijing.volces.com/api/v3", encKey, capabilities)
		setExecutionTestProfile(t, &plan, "doubao.seed-evolving.api", "1.3.0")
		plan.Routes[0].Provider = "doubao"
		plan.Routes[0].UpstreamModel = "doubao-seed-evolving"
		plan.Routes[0].AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)
		if invalidEncryptedSecret {
			plan.Routes[0].EncryptedSecret = "invalid-ciphertext-must-not-be-decrypted"
		}

		coordinator := &executionCoordinatorStub{}
		coordinator.authorizeFn = func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
			digest, err := executionevidence.Digest(route.Evidence)
			return modelexecution.Decision{
				Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
				ProviderAttemptKey: modelexecution.ProviderAttemptKey{
					OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID,
				},
			}, err
		}
		relay := NewRelay(executionTestDB(), nil, nil, billingProvider, nil, encKey)
		relay.providerHTTPDoer = &http.Client{Transport: doer}
		require.NoError(t, relay.SetModelExecutionRuntime(
			modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, coordinator, routingpolicy.Default(),
		))
		return relay, coordinator
	}

	t.Run("provider default reasoning extends the writer deadline", func(t *testing.T) {
		billingProvider := &mockBilling{}
		relay, coordinator := newRelay(t, false, func(request *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), `"model":"doubao-seed-evolving"`)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(
					`{"id":"chatcmpl-evolving-reasoning","object":"chat.completion","model":"doubao-seed-evolving","choices":[{"index":0,"message":{"role":"assistant","content":"ok","reasoning_content":"summary","encrypted_content":"opaque"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
				)),
			}, nil
		}, billingProvider)
		writer := &processingTierDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
		relay.HandleChatCompletion(writer, executionRequest(
			http.MethodPost, "/v1/chat/completions", "req-evolving-reasoning-deadline",
			`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":128}`,
		))
		require.Equal(t, http.StatusOK, writer.Code, writer.Body.String())
		require.Greater(t, time.Until(writer.deadline), 30*time.Minute+50*time.Second)
		require.LessOrEqual(t, time.Until(writer.deadline), 31*time.Minute)
		require.Equal(t, int64(1), coordinator.authorizeCount.Load())
		require.Equal(t, int64(1), coordinator.successCount.Load())
	})

	t.Run("deadline failure is pre-freeze and pre-dispatch", func(t *testing.T) {
		freezeCalls := 0
		providerCalls := 0
		billingProvider := &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
			freezeCalls++
			return &billing.FreezeResult{Success: true}, nil
		}}
		relay, coordinator := newRelay(t, true, func(*http.Request) (*http.Response, error) {
			providerCalls++
			return nil, errors.New("provider dispatch must not occur")
		}, billingProvider)
		writer := &processingTierDeadlineWriter{
			ResponseRecorder: httptest.NewRecorder(),
			err:              errors.New("deadline unavailable"),
		}
		relay.HandleChatCompletion(writer, executionRequest(
			http.MethodPost, "/v1/chat/completions", "req-evolving-reasoning-deadline-failure",
			`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":128}`,
		))
		require.Equal(t, http.StatusServiceUnavailable, writer.Code, writer.Body.String())
		require.Contains(t, writer.Body.String(), "server_error.reasoning_runtime_unavailable")
		require.Zero(t, freezeCalls)
		require.Zero(t, providerCalls)
		require.Zero(t, coordinator.authorizeCount.Load())
	})
}

func TestGemini37ProcessingTierHandlerCarriesRuntimeAndResponseEvidence(t *testing.T) {
	newRelay := func(
		t *testing.T,
		stream bool,
		doer executionRoundTripFunc,
		metadata *map[string]interface{},
	) (*Relay, *executionCoordinatorStub) {
		t.Helper()
		encKey := []byte("0123456789abcdef0123456789abcdef")
		now := time.Now().UTC().Truncate(time.Microsecond)
		capabilities := []executionevidence.CapabilityEntry{{
			CapabilityID: modelprofile.CapabilityServiceTier, SupportState: "enabled",
			EvidenceState: "production_approved", EvidenceRef: "fixture://service-tier",
			VerifiedAt: &now, Revision: 1,
		}}
		if stream {
			capabilities = append(capabilities, executionevidence.CapabilityEntry{
				CapabilityID: modelprofile.CapabilityStreaming, SupportState: "enabled",
				EvidenceState: "production_approved", EvidenceRef: "fixture://streaming",
				VerifiedAt: &now, Revision: 1,
			})
		}
		plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions,
			"https://generativelanguage.googleapis.com/v1beta/openai", encKey, capabilities)
		setExecutionTestProfile(t, &plan, "gemini.3.7-flash.api", "1.7.0")
		plan.RequiredCapabilities = []string{modelprofile.CapabilityServiceTier}
		if stream {
			plan.RequiredCapabilities = append(plan.RequiredCapabilities, modelprofile.CapabilityStreaming)
		}
		plan.Routes[0].Provider = "gemini"
		plan.Routes[0].UpstreamModel = "gemini-3.7-flash"
		plan.Routes[0].AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)

		coordinator := &executionCoordinatorStub{}
		coordinator.authorizeFn = func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
			digest, err := executionevidence.Digest(route.Evidence)
			return modelexecution.Decision{
				Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
				ProviderAttemptKey: modelexecution.ProviderAttemptKey{
					OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID,
				},
			}, err
		}
		database := &testutil.MockDB{ExecFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "INSERT INTO request_trace_events") && len(args) == traceInsertArgCount {
				eventType, _ := args[8].(string)
				if eventType == "provider.response_accepted" || eventType == "provider.response_rejected" {
					var decoded map[string]interface{}
					require.NoError(t, json.Unmarshal([]byte(args[17].(string)), &decoded))
					*metadata = decoded
				}
			}
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}}
		relay := NewRelay(database, nil, nil, &mockBilling{}, nil, encKey)
		relay.SetBillingAdmissionReadinessInspector(&processingTierReadinessStub{
			result: billing.AdmissionReadiness{BillingMode: billing.BillingModeCostObservation},
		})
		relay.providerHTTPDoer = &http.Client{Transport: doer}
		require.NoError(t, relay.SetModelExecutionRuntime(
			modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, coordinator, routingpolicy.Default(),
		))
		return relay, coordinator
	}

	t.Run("non-stream flex preserves payload and confirms accepted header", func(t *testing.T) {
		var metadata map[string]interface{}
		relay, coordinator := newRelay(t, false, func(request *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), `"service_tier":"flex"`)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type":          []string{"application/json"},
					"X-Gemini-Service-Tier": []string{"flex"},
				},
				Body: io.NopCloser(strings.NewReader(
					`{"id":"chatcmpl-tier","object":"chat.completion","model":"gemini-3.7-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
				)),
			}, nil
		}, &metadata)
		writer := &processingTierDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
		relay.HandleChatCompletion(writer, executionRequest(
			http.MethodPost, "/v1/chat/completions", "req-tier-flex-handler",
			`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"service_tier":"flex","max_completion_tokens":128}`,
		))
		require.Equal(t, http.StatusOK, writer.Code, writer.Body.String())
		require.Equal(t, "observed", metadata["processing_tier_observation"])
		require.Equal(t, "flex", metadata["processing_tier_observed"])
		require.Equal(t, "confirmed", metadata["processing_tier_disposition"])
		require.Greater(t, time.Until(writer.deadline), 15*time.Minute+50*time.Second)
		require.Equal(t, int64(1), coordinator.successCount.Load())
	})

	t.Run("priority rejection records header without a delivery disposition", func(t *testing.T) {
		var metadata map[string]interface{}
		relay, coordinator := newRelay(t, false, func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header: http.Header{
					"Content-Type":          []string{"application/json"},
					"X-Gemini-Service-Tier": []string{"standard"},
				},
				Body: io.NopCloser(strings.NewReader(`{"error":{"message":"busy"}}`)),
			}, nil
		}, &metadata)
		writer := &processingTierDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
		relay.HandleChatCompletion(writer, executionRequest(
			http.MethodPost, "/v1/chat/completions", "req-tier-priority-rejected",
			`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"service_tier":"priority","max_completion_tokens":128}`,
		))
		require.Equal(t, http.StatusBadGateway, writer.Code, writer.Body.String())
		require.Equal(t, "observed_on_rejected_response", metadata["processing_tier_observation"])
		require.Equal(t, "standard", metadata["processing_tier_observed"])
		require.NotContains(t, metadata, "processing_tier_disposition")
		require.Equal(t, int64(1), coordinator.rateCount.Load())
		require.Zero(t, coordinator.successCount.Load())
	})

	t.Run("deadline failure stops before Freeze and provider dispatch", func(t *testing.T) {
		var metadata map[string]interface{}
		freezeCalls := 0
		providerCalls := 0
		relay, coordinator := newRelay(t, false, func(*http.Request) (*http.Response, error) {
			providerCalls++
			return nil, errors.New("provider dispatch must not occur")
		}, &metadata)
		relay.billing = &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
			freezeCalls++
			return &billing.FreezeResult{Success: true}, nil
		}}
		writer := &processingTierDeadlineWriter{
			ResponseRecorder: httptest.NewRecorder(),
			err:              errors.New("deadline unavailable"),
		}
		relay.HandleChatCompletion(writer, executionRequest(
			http.MethodPost, "/v1/chat/completions", "req-tier-flex-deadline-failure",
			`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"service_tier":"flex","max_completion_tokens":128}`,
		))
		require.Equal(t, http.StatusServiceUnavailable, writer.Code, writer.Body.String())
		require.Contains(t, writer.Body.String(), "server_error.processing_tier_runtime_unavailable")
		require.Zero(t, freezeCalls)
		require.Zero(t, providerCalls)
		require.Zero(t, coordinator.authorizeCount.Load())
	})

	t.Run("stream flex uses stream policy and captures accepted header", func(t *testing.T) {
		var metadata map[string]interface{}
		relay, coordinator := newRelay(t, true, func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type":          []string{"text/event-stream"},
					"X-Gemini-Service-Tier": []string{"flex"},
				},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
						"data: [DONE]\n\n",
				)),
			}, nil
		}, &metadata)
		writer := &processingTierDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
		relay.HandleChatCompletion(writer, executionRequest(
			http.MethodPost, "/v1/chat/completions", "req-tier-flex-stream",
			`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true,"service_tier":"flex","max_completion_tokens":128}`,
		))
		require.Equal(t, http.StatusOK, writer.Code, writer.Body.String())
		require.False(t, writer.deadline.IsZero())
		require.Greater(t, time.Until(writer.maximumDeadline()), 30*time.Minute+50*time.Second)
		require.Equal(t, "confirmed", metadata["processing_tier_disposition"])
		require.Equal(t, int64(1), coordinator.successCount.Load())
	})
}
