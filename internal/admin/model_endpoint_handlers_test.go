package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestApplyGatewayProjectionUsesGatewayCandidatesAsRouteAuthority(t *testing.T) {
	model := &modelEndpointModelView{
		PublicModel:      "company-fast",
		StableReleaseRef: "rcr_stable",
		Targets: []modelEndpointTargetView{{
			ChannelID: 13, ConfigStatus: "active", RoutingEpoch: 4,
			EligibleCredentialCount: 1, StableAssignment: true, StableExpectedEpoch: 4,
			LastProbe: &modelEndpointProbeView{Success: true, Current: true},
		}},
	}
	h := &Handler{routingController: &routingControllerStub{preview: routingpolicy.Preview{
		Model:      "company-fast",
		Candidates: []routingpolicy.PreviewCandidate{{ChannelID: 13}},
	}}}
	h.applyGatewayProjection(httptest.NewRequest(http.MethodGet, "/", nil), model)

	if !model.RouteAvailable || model.State != "active" || model.ReasonCode != "stable_route_available" {
		t.Fatalf("unexpected model projection: %+v", model)
	}
	if !model.Targets[0].RouteCandidate || model.Targets[0].State != "active" {
		t.Fatalf("unexpected target projection: %+v", model.Targets[0])
	}
}

func TestApplyGatewayProjectionDoesNotReportARetiredModelRouteAsAvailable(t *testing.T) {
	model := &modelEndpointModelView{
		PublicModel: "company-retired", Lifecycle: "retired", StableReleaseRef: "rcr_stable",
		Targets: []modelEndpointTargetView{{
			ChannelID: 13, ConfigStatus: "active", RoutingEpoch: 4,
			EligibleCredentialCount: 1, StableAssignment: true, StableExpectedEpoch: 4,
		}},
	}
	h := &Handler{routingController: &routingControllerStub{preview: routingpolicy.Preview{
		Model: "company-retired", Candidates: []routingpolicy.PreviewCandidate{{ChannelID: 13}},
	}}}
	h.applyGatewayProjection(httptest.NewRequest(http.MethodGet, "/", nil), model)

	if model.RouteAvailable || model.State != "governance_blocked" || model.ReasonCode != "model_retired" {
		t.Fatalf("retired lifecycle must override a raw routing candidate: %+v", model)
	}
}

func TestApplyGatewayProjectionNeverCallsAReleaseRoutableWithoutAGatewayCandidate(t *testing.T) {
	model := &modelEndpointModelView{
		PublicModel: "company-fast", StableReleaseRef: "rcr_stable",
		Targets: []modelEndpointTargetView{{
			ChannelID: 13, ConfigStatus: "active", RoutingEpoch: 4,
			EligibleCredentialCount: 1, StableAssignment: true, StableExpectedEpoch: 4,
			LastProbe: &modelEndpointProbeView{Success: true, Current: true},
		}},
	}
	h := &Handler{routingController: &routingControllerStub{preview: routingpolicy.Preview{
		Model:      "company-fast",
		Exclusions: []routingpolicy.PreviewCandidate{{ChannelID: 13, Reason: "no_eligible_keys"}},
	}}}
	h.applyGatewayProjection(httptest.NewRequest(http.MethodGet, "/", nil), model)

	if model.RouteAvailable || model.State != "degraded" || model.ReasonCode != "stable_route_unavailable" {
		t.Fatalf("release metadata was mistaken for callability: %+v", model)
	}
	if model.Targets[0].RouteCandidate || model.Targets[0].RouteExclusionReason != "no_eligible_keys" || model.Targets[0].State != "ready" {
		t.Fatalf("unexpected excluded target projection: %+v", model.Targets[0])
	}
}

func TestApplyGatewayProjectionDoesNotMistakeCanaryMetadataForACandidate(t *testing.T) {
	model := &modelEndpointModelView{
		PublicModel: "company-fast", CanaryReleaseRef: "rcr_canary",
		Targets: []modelEndpointTargetView{{
			ChannelID: 14, ConfigStatus: "staged", RoutingEpoch: 8,
			CanaryAssignment: true, CanaryExpectedEpoch: 8,
			EligibleCredentialCount: 0,
		}},
	}
	h := &Handler{routingController: &routingControllerStub{preview: routingpolicy.Preview{Model: "company-fast"}}}
	h.applyGatewayProjection(httptest.NewRequest(http.MethodGet, "/", nil), model)

	if model.RouteAvailable || model.State != "degraded" || model.ReasonCode != "canary_stable_route_unavailable" {
		t.Fatalf("unexpected canary model projection: %+v", model)
	}
	if model.Targets[0].State != "needs_credential" || model.Targets[0].ReasonCode != "no_eligible_provider_credential" {
		t.Fatalf("canary release metadata hid an unusable target: %+v", model.Targets[0])
	}
}

func TestApplyGatewayProjectionDistinguishesStableAndCanaryTargets(t *testing.T) {
	model := &modelEndpointModelView{
		PublicModel: "company-fast", StableReleaseRef: "rcr_stable", CanaryReleaseRef: "rcr_canary",
		Targets: []modelEndpointTargetView{
			{
				ChannelID: 13, ConfigStatus: "staged", RoutingEpoch: 4,
				StableAssignment: true, StableExpectedEpoch: 4, CanaryAssignment: true, CanaryExpectedEpoch: 4,
				EligibleCredentialCount: 1, LastProbe: &modelEndpointProbeView{Success: true, Current: true},
			},
			{
				ChannelID: 14, ConfigStatus: "staged", RoutingEpoch: 8,
				CanaryAssignment: true, CanaryExpectedEpoch: 8,
				EligibleCredentialCount: 1, LastProbe: &modelEndpointProbeView{Success: true, Current: true},
			},
		},
	}
	h := &Handler{routingController: &routingControllerStub{preview: routingpolicy.Preview{
		Model: "company-fast", Candidates: []routingpolicy.PreviewCandidate{{ChannelID: 13}},
	}}}
	h.applyGatewayProjection(httptest.NewRequest(http.MethodGet, "/", nil), model)

	if !model.RouteAvailable || model.State != "canary" {
		t.Fatalf("unexpected canary model projection: %+v", model)
	}
	if model.Targets[0].State != "active" || model.Targets[1].State != "canary" {
		t.Fatalf("stable and canary targets were collapsed: %+v", model.Targets)
	}
}

func TestApplyGatewayProjectionSurfacesStaleCanaryPin(t *testing.T) {
	model := &modelEndpointModelView{
		PublicModel: "company-fast",
		Targets: []modelEndpointTargetView{{
			ChannelID: 14, ConfigStatus: "staged", RoutingEpoch: 9,
			CanaryAssignment: true, CanaryExpectedEpoch: 8,
			EligibleCredentialCount: 1,
			LastProbe:               &modelEndpointProbeView{Success: true, Current: true},
		}},
	}
	h := &Handler{routingController: &routingControllerStub{preview: routingpolicy.Preview{Model: "company-fast"}}}
	h.applyGatewayProjection(httptest.NewRequest(http.MethodGet, "/", nil), model)

	if model.Targets[0].State != "stale" || model.Targets[0].ReasonCode != "canary_assignment_epoch_mismatch" {
		t.Fatalf("canary pin drift was not surfaced: %+v", model.Targets[0])
	}
}

func TestRequireCurrentSuccessfulProbeFailsClosed(t *testing.T) {
	var capturedSQL string
	var capturedArgs []any
	database := &testutil.MockDB{QueryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
		capturedSQL, capturedArgs = sql, args
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*dest[0].(*bool) = false
			return nil
		}}
	}}
	h := &Handler{db: database}
	err := h.requireCurrentSuccessfulProbe(context.Background(), modelEndpointActivationRequest{
		ChannelID: 13, Model: "company-fast", ExpectedRoutingEpoch: 4,
	})
	if err == nil || !strings.Contains(err.Error(), "no longer matches") {
		t.Fatalf("expected current-probe rejection, got %v", err)
	}
	if len(capturedArgs) != 3 || capturedArgs[0] != int64(13) || capturedArgs[1] != "company-fast" || capturedArgs[2] != int64(4) {
		t.Fatalf("unexpected current-probe lookup args: %#v", capturedArgs)
	}
	for _, required := range []string{"probe.routing_epoch=c.routing_epoch", "probe.upstream_model=", "credential.state_epoch=probe.credential_state_epoch"} {
		if !strings.Contains(capturedSQL, required) {
			t.Fatalf("current-probe lookup omitted %q: %s", required, capturedSQL)
		}
	}
}

func TestDecodeModelEndpointActivationRejectsAmbiguousModelIdentity(t *testing.T) {
	for _, body := range []string{
		`{"channel_id":13,"model":"company-fast\nother","expected_routing_epoch":4}`,
		`{"channel_id":0,"model":"company-fast","expected_routing_epoch":4}`,
		`{"channel_id":13,"model":"company-fast","expected_routing_epoch":0}`,
	} {
		recorder := httptest.NewRecorder()
		_, ok := decodeModelEndpointActivation(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		if ok || recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s accepted=%v status=%d", body, ok, recorder.Code)
		}
	}
}

func TestPersistChannelModelProbePinsExactConnectionCredentialAndModelIdentity(t *testing.T) {
	var capturedSQL string
	var capturedArgs []any
	database := &testutil.MockDB{ExecFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		capturedSQL, capturedArgs = sql, args
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}}
	h := &Handler{db: database}
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	ctx := context.WithValue(request.Context(), middleware.CtxUserID, int64(42))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RolePlatformAdmin)
	request = request.WithContext(ctx)
	err := h.persistChannelModelProbe(request, channelModelProbeRecord{
		ChannelID: 13, PublicModel: "company-fast", UpstreamModel: "deepseek/deepseek-v4-flash",
		RoutingEpoch: 4, CredentialID: 99, CredentialStateEpoch: 3,
		Success: true, StatusCode: 200, LatencyMS: 18,
	})
	if err != nil {
		t.Fatalf("persist probe: %v", err)
	}
	if !strings.Contains(capturedSQL, "ON CONFLICT (channel_id,public_model) DO UPDATE") {
		t.Fatalf("probe persistence is not a latest-state upsert: %s", capturedSQL)
	}
	want := []any{int64(13), "company-fast", "deepseek/deepseek-v4-flash", int64(4), int64(99), int64(3), true, 200, int64(18)}
	if len(capturedArgs) != 11 {
		t.Fatalf("probe args = %#v", capturedArgs)
	}
	for index, value := range want {
		if capturedArgs[index] != value {
			t.Fatalf("probe arg %d = %#v, want %#v", index, capturedArgs[index], value)
		}
	}
	if capturedArgs[10] != int64(42) {
		t.Fatalf("tested_by = %#v, want 42", capturedArgs[10])
	}
}
