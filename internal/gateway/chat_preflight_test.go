package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

const preflightToolRequest = `{"model":"internal-successor-private","stream":true,"tool_choice":"auto","max_tokens":8192,"messages":[{"role":"user","content":"private-prompt-marker"}],"tools":[{"type":"function","function":{"name":"read","description":"Read a fixture","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}]}`

func newPreflightFixture(t *testing.T) (*Relay, *executionPlannerStub, *executionCoordinatorStub) {
	t.Helper()
	key := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://private-provider.invalid/api/paas/v4", key, nil)
	setExecutionTestProfile(t, &plan, "glm.5.3.api", "1.8.0")
	plan.PublicOutputLimit = 65536
	plan.PublicModel = "internal-successor-private"
	plan.DatabaseTime = time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	plan.Routes[0].Provider, plan.Routes[0].UpstreamModel = "glm", "glm-5.3"
	plan.Routes[0].AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)
	plan.Routes[0].EncryptedSecret = "invalid-ciphertext-must-never-be-decrypted"
	db := &testutil.MockDB{ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		t.Fatal("preflight must not write receipts, quota, qualification or execution state")
		return pgconn.CommandTag{}, nil
	}}
	relay := NewRelay(db, nil, nil, &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
		t.Fatal("preflight must not freeze funds")
		return nil, errors.New("forbidden")
	}}, nil, key)
	relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("preflight must not call a Provider")
		return nil, errors.New("forbidden")
	})}
	planner, coordinator := &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()))
	return relay, planner, coordinator
}

func runChatPreflight(t *testing.T, relay *Relay, body string) (*httptest.ResponseRecorder, chatPreflightResult) {
	t.Helper()
	r := executionRequest(http.MethodPost, "/v1/chat/completions", "preflight-fixture", body)
	r = r.WithContext(context.WithValue(r.Context(), middleware.CtxModelAlias, "glm-5.3"))
	budget := payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits())
	h := payloadpolicy.RequestMiddleware(budget, payloadpolicy.ProfileChatRequest, middleware.GetOrgIDFromContext)(middleware.StrictJSONIngress(http.HandlerFunc(relay.HandleChatPreflight)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var result chatPreflightResult
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	}
	for _, private := range []string{"private-prompt-marker", "internal-successor-private", "private-provider", "invalid-ciphertext", "fixture-connection", "function_tool_choice", "DeclaredFunctionNames"} {
		require.NotContains(t, w.Body.String(), private)
	}
	return w, result
}

func TestChatPreflightUsesCanonicalPreparationWithoutExecutionEffects(t *testing.T) {
	relay, planner, coordinator := newPreflightFixture(t)
	w, result := runChatPreflight(t, relay, preflightToolRequest)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, result.LocallyAdmissible, w.Body.String())
	require.Equal(t, "glm-5.3", result.RequestModel)
	require.Equal(t, "glm.5.3.api", result.ProfileID)
	require.Equal(t, planner.plan.DatabaseTime, *result.SnapshotAt)
	require.Contains(t, result.RequiredCapabilities, modelprofile.CapabilityTools)
	require.Contains(t, result.RequiredCapabilities, modelprofile.CapabilityStreaming)
	require.Contains(t, result.NotChecked, "execution_quota_and_reservation")
	require.Contains(t, result.NotChecked, "response_delivery")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Len(t, planner.requests, 1)
	require.Zero(t, coordinator.authorizeCount.Load())
	require.Zero(t, coordinator.renewCount.Load())
	require.Zero(t, coordinator.releaseCount.Load())
}

func TestChatPreflightDistinguishesProfileRejectionQualificationAndRuntime(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Relay, *executionPlannerStub)
		want   string
	}{
		{"missing streaming qualification", func(_ *Relay, p *executionPlannerStub) {
			p.plan.Routes = nil
			p.plan.Exclusions = []modelrouting.Exclusion{{DeploymentID: 99, Reason: "unsupported_capability:streaming"}, {DeploymentID: 88, Reason: "unsupported_capability:streaming"}, {Reason: "private-reason-marker"}}
			p.err = modelrouting.ErrNoRoute
		}, "unsupported_capability:streaming"},
		{"stale", func(_ *Relay, p *executionPlannerStub) { p.err = modelrouting.ErrManifestStale }, "runtime_manifest_stale"},
		{"unsealed", func(_ *Relay, p *executionPlannerStub) { p.err = modelrouting.ErrManifestUnsealed }, "runtime_manifest_unsealed"},
		{"legacy", func(r *Relay, _ *executionPlannerStub) { r.modelExecution.mode = modelrouting.ModeLegacy }, "canonical_preflight_unavailable"},
		{"not configured", func(r *Relay, _ *executionPlannerStub) { r.modelExecution = nil }, "canonical_preflight_unavailable"},
		{"unsupported adapter", func(_ *Relay, p *executionPlannerStub) { p.plan.Routes[0].AdapterType = "anthropic_native" }, "adapter_preflight_not_supported"},
		{"invalid config", func(_ *Relay, p *executionPlannerStub) {
			p.plan.Routes[0].AdapterConfig = json.RawMessage(`{"unexpected":true}`)
		}, "provider_request_build_failed"},
		{"nonstream exact profile", func(_ *Relay, p *executionPlannerStub) {
			setExecutionTestProfile(t, &p.plan, "minimax.m2.7.payg", "1.2.0")
		}, "validation_error.model_execution_profile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, p, c := newPreflightFixture(t)
			tc.change(r, p)
			w, result := runChatPreflight(t, r, preflightToolRequest)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.False(t, result.LocallyAdmissible)
			require.Contains(t, result.Reasons, tc.want)
			require.NotContains(t, w.Body.String(), "private-reason-marker")
			require.Zero(t, c.authorizeCount.Load())
		})
	}
}

func TestChatPreflightOutputBoundsAndToolContinuation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		pass       bool
		http       int
	}{
		{"omitted default", strings.Replace(preflightToolRequest, `"max_tokens":8192,`, "", 1), true, 200},
		{"explicit profile default", strings.Replace(preflightToolRequest, `8192`, `65536`, 1), true, 200},
		{"context includes input", strings.Replace(preflightToolRequest, `8192`, `128000`, 1), false, 200},
		{"conflicting", strings.Replace(preflightToolRequest, `"max_tokens":8192`, `"max_completion_tokens":8192,"max_tokens":4096`, 1), false, 400},
		{"unsupported exact output field", strings.Replace(preflightToolRequest, `"max_tokens":8192`, `"max_completion_tokens":8192`, 1), false, 200},
		{"fractional", strings.Replace(preflightToolRequest, `8192`, `1.5`, 1), false, 400},
		{"negative", strings.Replace(preflightToolRequest, `8192`, `-1`, 1), false, 400},
		{"duplicate", strings.Replace(preflightToolRequest, `"stream":true`, `"stream":true,"stream":false`, 1), false, 400},
		{"tool continuation", strings.Replace(preflightToolRequest, `"private-prompt-marker"}]`, `"private-prompt-marker"},{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"fixture\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"fixture result"}]`, 1), true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := newPreflightFixture(t)
			w, result := runChatPreflight(t, r, tc.body)
			require.Equal(t, tc.http, w.Code, w.Body.String())
			require.Equal(t, tc.pass, result.LocallyAdmissible, w.Body.String())
		})
	}
}

func TestChatPreflightModelScopeRejectsBeforePlanner(t *testing.T) {
	r, p, _ := newPreflightFixture(t)
	request := executionRequest("POST", "/v1/chat/completions", "denied", preflightToolRequest)
	request = request.WithContext(context.WithValue(request.Context(), middleware.CtxAPIKey, &models.ApiKey{AllowedModels: "other"}))
	h := payloadpolicy.RequestMiddleware(payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits()), payloadpolicy.ProfileChatRequest, nil)(http.HandlerFunc(r.HandleChatPreflight))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	require.Equal(t, 403, w.Code)
	require.Empty(t, p.requests)
}

// These are the exact 2026-09-04 active identities from the retained 14-row
// capture, not a mutable client capability catalog. The sanitized shared shape
// proves the existing blockers locally; no row claims live qualification.
func TestCapturedFourteenClientShapesKeepContractAndQualificationDistinct(t *testing.T) {
	for _, tc := range []struct {
		id, version string
		glm         bool
	}{
		{"deepseek.v4-flash.api", "1.5.0", false}, {"deepseek.v4-pro.api", "1.5.0", false},
		{"kimi.k2.6.api", "1.7.0", false}, {"kimi.k2.7-code.api", "1.3.0", false}, {"kimi.k3.api", "1.9.0", false},
		{"minimax.m3.payg", "1.2.0", false}, {"minimax.m2.7.payg", "1.2.0", false}, {"minimax.m2.7-highspeed.payg", "1.2.0", false},
		{"glm.5.api", "1.0.0", true}, {"glm.5-turbo.api", "1.0.0", true}, {"glm.5.1.api", "1.0.0", true},
		{"glm.5.2.api", "1.8.0", true}, {"glm.5.3.api", "1.8.0", true}, {"glm.5v-turbo.api", "1.11.0", true},
	} {
		t.Run(tc.id+"@"+tc.version, func(t *testing.T) {
			r, p, c := newPreflightFixture(t)
			setExecutionTestProfile(t, &p.plan, tc.id, tc.version)
			p.plan.Routes = nil
			p.plan.Exclusions = []modelrouting.Exclusion{{Reason: "unsupported_capability:streaming"}}
			p.err = modelrouting.ErrNoRoute
			body := preflightToolRequest
			if !tc.glm {
				body = strings.Replace(body, `"max_tokens":8192`, `"max_completion_tokens":8192`, 1)
			}
			w, result := runChatPreflight(t, r, body)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.False(t, result.LocallyAdmissible)
			if tc.glm {
				require.Contains(t, result.Reasons, "unsupported_capability:streaming")
				require.Equal(t, tc.id, result.ProfileID)
			} else {
				require.Contains(t, result.Reasons, "validation_error.model_execution_profile")
			}
			require.Zero(t, c.authorizeCount.Load())
		})
	}
}
