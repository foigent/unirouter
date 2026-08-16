package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessgrantboundary"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelreadiness"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

type readinessProjectionReloader struct {
	projection limitpolicy.Readiness
	err        error
}

func (r readinessProjectionReloader) Refresh(context.Context) error { return nil }

func (r readinessProjectionReloader) Readiness(context.Context, limitpolicy.Subject) (limitpolicy.Readiness, error) {
	return r.projection, r.err
}

func TestModelReadinessRejectsNonCanonicalOrUnboundedQueriesBeforeLookup(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"missing project", "model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self"},
		{"model too long", "project_id=1&model=" + strings.Repeat("m", maxReadinessModelLength+1) + "&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self"},
		{"model control", "project_id=1&model=m%0Aother&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self"},
		{"dynamic endpoint", "project_id=1&model=m&endpoint=%2Fv1%2Fmedia%2Fjobs%2F42&caller=self"},
		{"unsupported endpoint", "project_id=1&model=m&endpoint=%2Fv1%2Fresponses&caller=self"},
		{"unknown caller", "project_id=1&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=other"},
		{"self application", "project_id=1&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self&application_ref=app_one"},
		{"workload application missing", "project_id=1&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=workload_application"},
		{"workload application too long", "project_id=1&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=workload_application&application_ref=app_" + strings.Repeat("a", maxReadinessApplicationRefLength)},
		{"workload application control", "project_id=1&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=workload_application&application_ref=app_one%0Aother"},
		{"connection with project", "mode=connection&project_id=1&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions"},
		{"connection with caller", "mode=connection&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self"},
		{"unknown mode", "mode=other&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &Handler{}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/model-readiness?"+test.query, nil)
			response := httptest.NewRecorder()
			handler.GetModelReadiness(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestModelReadinessConnectionModeDoesNotRequireOrReadProjectScope(t *testing.T) {
	database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(...any) error { return pgx.ErrNoRows }}
	}}
	handler := NewHandler(database, "jwt", []byte("0123456789abcdef01234567"), nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/model-readiness?mode=connection&model=deepseek-v4-pro&endpoint=%2Fv1%2Fchat%2Fcompletions", nil)
	ctx := context.WithValue(request.Context(), middleware.CtxOrgID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxOrganizationRole, auth.RolePlatformAdmin)
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()

	handler.GetModelReadiness(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload modelreadiness.Response
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Scope.Mode != modelreadiness.ModeConnection || payload.Scope.OrgID != 7 || payload.Scope.ProjectID != 0 || payload.Scope.Caller != "" {
		t.Fatalf("unexpected connection scope: %#v", payload.Scope)
	}
	if payload.Summary.CurrentProjectCallerCallable.State != modelreadiness.StateNotApplicable || payload.ExternalRequests != "none" {
		t.Fatalf("unexpected connection summary: %#v", payload)
	}
}

func TestReadinessGatewayLicenseAxisBlocksBeforeProviderRepair(t *testing.T) {
	handler := NewHandler(&testutil.MockDB{}, "jwt", []byte("0123456789abcdef01234567"), nil)
	handler.SetLicenseRuntime(licensing.NewRuntime(nil))
	response := &modelreadiness.Response{Scope: modelreadiness.Scope{ResolvedModel: "deepseek-v4-pro"}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/model-readiness", nil)
	ctx := context.WithValue(request.Context(), middleware.CtxOrganizationRole, auth.RolePlatformAdmin)
	request = request.WithContext(ctx)

	handler.readinessPlatformAxes(request, 7, response)

	axis := response.PlatformModel.GatewayLicense
	if axis.State != modelreadiness.StateBlocked || axis.ReasonCode != "license_error.missing" || axis.FixRoute != "/license" {
		t.Fatalf("gateway license axis=%#v", axis)
	}
}

func TestReadinessProviderCredentialFixTargetsTheExactConnection(t *testing.T) {
	database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(...any) error { return pgx.ErrNoRows }}
	}}
	handler := NewHandler(database, "jwt", []byte("0123456789abcdef01234567"), nil)
	handler.SetRoutingController(&routingControllerStub{preview: routingpolicy.Preview{
		Candidates: []routingpolicy.PreviewCandidate{{ChannelID: 102, ChannelName: "DeepSeek", EligibleKeyCount: 0}},
	}})
	response := &modelreadiness.Response{Scope: modelreadiness.Scope{ResolvedModel: "deepseek-v4-pro"}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/model-readiness", nil)
	ctx := context.WithValue(request.Context(), middleware.CtxOrganizationRole, auth.RolePlatformAdmin)
	request = request.WithContext(ctx)

	handler.readinessPlatformAxes(request, 7, response)

	axis := response.PlatformModel.ProviderCredential
	want := "/channel-keys?from=connections&channel_id=102&model=deepseek-v4-pro"
	if axis.State != modelreadiness.StateBlocked || axis.FixRoute != want {
		t.Fatalf("provider credential axis=%#v want_fix=%q", axis, want)
	}
}

func TestReadinessCountsEligibleCredentialOnAStagedExcludedEndpoint(t *testing.T) {
	database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(...any) error { return pgx.ErrNoRows }}
	}}
	handler := NewHandler(database, "jwt", []byte("0123456789abcdef01234567"), nil)
	handler.SetRoutingController(&routingControllerStub{preview: routingpolicy.Preview{
		Exclusions: []routingpolicy.PreviewCandidate{{
			ChannelID: 102, ChannelName: "DeepSeek", EligibleKeyCount: 1, Reason: "channel_inactive",
		}},
	}})
	response := &modelreadiness.Response{Scope: modelreadiness.Scope{ResolvedModel: "deepseek-v4-pro"}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/model-readiness", nil)
	ctx := context.WithValue(request.Context(), middleware.CtxOrganizationRole, auth.RolePlatformAdmin)
	request = request.WithContext(ctx)

	handler.readinessPlatformAxes(request, 7, response)

	axis := response.PlatformModel.ProviderCredential
	if axis.State != modelreadiness.StateReady || axis.ReasonCode != "provider_credential_available" {
		t.Fatalf("staged endpoint credential was mistaken for missing: %#v", axis)
	}
}

func TestReadinessProjectAndBudgetAxesPreserveIndependentSemantics(t *testing.T) {
	tests := []struct {
		name   string
		axis   modelreadiness.Axis
		state  modelreadiness.State
		reason string
	}{
		{"active project", readinessProjectAxis("active", "/projects/1"), modelreadiness.StateReady, "project_active"},
		{"inactive project", readinessProjectAxis("archived", "/projects/1"), modelreadiness.StateBlocked, "project_inactive"},
		{"budget absent", readinessProjectBudgetAxis(billing.BillingModePrepaidEnforced, sql.NullInt64{}, "", "/projects/1?tab=budget"), modelreadiness.StateNotApplicable, "project_budget_not_configured_monitoring_only"},
		{"budget monitoring", readinessProjectBudgetAxis(billing.BillingModePrepaidEnforced, sql.NullInt64{Int64: 100, Valid: true}, "monthly", "/projects/1?tab=budget"), modelreadiness.StateWarning, "project_budget_monitoring_only"},
		{"observation budget absent", readinessProjectBudgetAxis(billing.BillingModeCostObservation, sql.NullInt64{}, "", "/projects/1?tab=budget"), modelreadiness.StateNotApplicable, "project_budget_not_configured_or_enforced_under_cost_observation"},
		{"observation budget configured", readinessProjectBudgetAxis(billing.BillingModeCostObservation, sql.NullInt64{Int64: 100, Valid: true}, "monthly", "/projects/1?tab=budget"), modelreadiness.StateWarning, "project_budget_configured_only_not_enforced_under_cost_observation"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.axis.State != test.state || test.axis.ReasonCode != test.reason {
				t.Fatalf("axis=%#v", test.axis)
			}
		})
	}
	observation := readinessProjectBudgetAxis(
		billing.BillingModeCostObservation,
		sql.NullInt64{Int64: 100, Valid: true},
		"monthly",
		"/projects/1?tab=budget",
	)
	if observation.SafeEvidence["project_budget_admission_required"] != false ||
		observation.SafeEvidence["project_budget_enforced"] != false ||
		observation.SafeEvidence["calculated_cost_budget_alerts_available"] != false {
		t.Fatalf("observation budget evidence=%#v", observation.SafeEvidence)
	}
}

func TestCostObservationReadinessDoesNotQueryOrBlockOnWallet(t *testing.T) {
	handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt", "api-key", []byte("0123456789abcdef01234567"), nil)
	if err := handler.SetBillingMode(billing.BillingModeCostObservation); err != nil {
		t.Fatalf("SetBillingMode: %v", err)
	}
	response := &modelreadiness.Response{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/model-readiness", nil)
	handler.readinessWalletAxis(request, 99, response)

	wallet := response.ProjectCaller.Wallet
	if wallet.State != modelreadiness.StateNotApplicable || wallet.ReasonCode != "wallet_not_used_for_cost_observation_admission" {
		t.Fatalf("wallet axis=%#v", wallet)
	}
	if wallet.SafeEvidence["billing_mode"] != string(billing.BillingModeCostObservation) || wallet.SafeEvidence["wallet_admission_required"] != false {
		t.Fatalf("wallet evidence=%#v", wallet.SafeEvidence)
	}

	ready := modelreadiness.Ready("ready", nil)
	summary := modelreadiness.Summarize(
		modelreadiness.PlatformModel{Lifecycle: ready, PriceAuthority: ready, BaseRoute: ready},
		modelreadiness.ProjectCaller{
			Project: ready, Alias: modelreadiness.Optional("direct", nil), ModelSet: modelreadiness.Optional("unrestricted", nil),
			Wallet: wallet, Application: modelreadiness.Optional("self", nil), Grant: ready, Credential: ready,
			ModelAuthorization: ready, LimitPolicy: ready,
		},
	)
	if summary.CurrentProjectCallerCallable.Value == nil || !*summary.CurrentProjectCallerCallable.Value {
		t.Fatalf("observation wallet blocked readiness: %#v", summary.CurrentProjectCallerCallable)
	}
}

func TestLegacyReadinessPriceAuthorityUsesOnlyLegacyTable(t *testing.T) {
	queries := 0
	database := &testutil.MockDB{QueryRowFn: func(_ context.Context, query string, _ ...any) pgx.Row {
		queries++
		if !strings.Contains(query, "model_prices") {
			t.Fatalf("legacy price authority issued unexpected query: %s", query)
		}
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*dest[0].(*bool) = true
			return nil
		}}
	}}
	handler := NewHandler(database, "jwt", []byte("0123456789abcdef01234567"), nil)
	handler.SetRequestPricingAuthority("legacy")
	axis := handler.readinessPriceAuthorityAxis(httptest.NewRequest(http.MethodGet, "/", nil), 7, "model-a")
	if queries != 1 || axis.State != modelreadiness.StateReady || axis.ReasonCode != "model_price_configured" || axis.SafeEvidence["request_pricing_mode"] != "legacy" {
		t.Fatalf("legacy price authority axis=%#v queries=%d", axis, queries)
	}
}

func TestPrepaidReadinessStillBlocksMissingOrEmptyWallet(t *testing.T) {
	tests := []struct {
		name   string
		scan   func(...any) error
		reason string
	}{
		{name: "missing", scan: func(...any) error { return pgx.ErrNoRows }, reason: "wallet_missing"},
		{name: "empty", scan: func(dest ...any) error {
			*dest[0].(*int64) = 0
			*dest[1].(*int64) = 0
			*dest[2].(*int64) = 0
			return nil
		}, reason: "wallet_no_spendable_balance"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
				return &testutil.MockRow{ScanFn: test.scan}
			}}
			handler := NewHandlerWithAPIKeyHashSecret(database, "jwt", "api-key", []byte("0123456789abcdef01234567"), nil)
			response := &modelreadiness.Response{}
			handler.readinessWalletAxis(httptest.NewRequest(http.MethodGet, "/", nil), 99, response)
			wallet := response.ProjectCaller.Wallet
			if wallet.State != modelreadiness.StateBlocked || wallet.ReasonCode != test.reason {
				t.Fatalf("wallet axis=%#v", wallet)
			}
			if wallet.SafeEvidence["billing_mode"] != string(billing.BillingModePrepaidEnforced) || wallet.SafeEvidence["wallet_admission_required"] != true {
				t.Fatalf("wallet evidence=%#v", wallet.SafeEvidence)
			}
		})
	}
}

func TestReadinessGrantScopeUsesCanonicalProjectDefaultAndExplicitIntersection(t *testing.T) {
	tests := []struct {
		name      string
		grant     readinessGrantEvidence
		requested string
		resolved  string
		endpoint  string
		allowed   bool
	}{
		{"project default chat", readinessGrantEvidence{ScopeMode: accessscope.ModeProjectDefault}, "alias", "model", "/v1/chat/completions", true},
		{"project default media excluded", readinessGrantEvidence{ScopeMode: accessscope.ModeProjectDefault}, "model", "model", "/v1/media/jobs", false},
		{"explicit requested model", readinessGrantEvidence{ScopeMode: accessscope.ModeExplicit, AllowedModels: []string{"alias"}, AllowedEndpoints: []string{"/v1/embeddings"}}, "alias", "model", "/v1/embeddings", true},
		{"explicit resolved model", readinessGrantEvidence{ScopeMode: accessscope.ModeExplicit, AllowedModels: []string{"model"}, AllowedEndpoints: []string{"/v1/embeddings"}}, "alias", "model", "/v1/embeddings", true},
		{"explicit endpoint excluded", readinessGrantEvidence{ScopeMode: accessscope.ModeExplicit, AllowedModels: []string{"model"}, AllowedEndpoints: []string{"/v1/chat/completions"}}, "model", "model", "/v1/embeddings", false},
		{"unknown mode", readinessGrantEvidence{ScopeMode: "unknown", AllowedModels: []string{"model"}, AllowedEndpoints: []string{"/v1/chat/completions"}}, "model", "model", "/v1/chat/completions", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := readinessGrantScopeAllows(test.grant, test.requested, test.resolved, test.endpoint); got != test.allowed {
				t.Fatalf("allowed=%v want=%v", got, test.allowed)
			}
		})
	}
}

func TestReadinessPolicyAxisPreservesRuntimeFailClosedState(t *testing.T) {
	tests := []struct {
		name       string
		projection limitpolicy.Readiness
		state      modelreadiness.State
		reason     string
	}{
		{"explicit zero", limitpolicy.Readiness{State: limitpolicy.ReadinessBlocked, Reason: limitpolicy.ReasonRPSExceeded}, modelreadiness.StateBlocked, limitpolicy.ReasonRPSExceeded},
		{"stale snapshot", limitpolicy.Readiness{State: limitpolicy.ReadinessUnavailable, Reason: limitpolicy.ReasonSnapshotStale}, modelreadiness.StateUnavailable, limitpolicy.ReasonSnapshotStale},
		{"over-cap chain", limitpolicy.Readiness{State: limitpolicy.ReadinessUnavailable, Reason: limitpolicy.ReasonPolicyChainLarge}, modelreadiness.StateUnavailable, limitpolicy.ReasonPolicyChainLarge},
		{"redis unavailable", limitpolicy.Readiness{State: limitpolicy.ReadinessUnavailable, Reason: limitpolicy.ReasonStateUnavailable}, modelreadiness.StateUnavailable, limitpolicy.ReasonStateUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &Handler{limitPolicyReloader: readinessProjectionReloader{projection: test.projection}}
			response := &modelreadiness.Response{Scope: modelreadiness.Scope{
				OrgID: 1, ProjectID: 2, RequestedModel: "model", ResolvedModel: "model", Endpoint: "/v1/chat/completions", Caller: "self",
			}}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/model-readiness", nil)
			handler.readinessPolicyAxes(request, response, []accessgrantboundary.Snapshot{{}}, 3)
			if response.ProjectCaller.LimitPolicy.State != test.state || response.ProjectCaller.LimitPolicy.ReasonCode != test.reason {
				t.Fatalf("axis=%#v, want state=%s reason=%s", response.ProjectCaller.LimitPolicy, test.state, test.reason)
			}
		})
	}
}
