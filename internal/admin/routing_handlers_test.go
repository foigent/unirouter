package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

type routingControllerStub struct {
	preview     routingpolicy.Preview
	err         error
	invalidated bool
}

func (stub *routingControllerStub) Preview(context.Context, string) (routingpolicy.Preview, error) {
	return stub.preview, stub.err
}

func (stub *routingControllerStub) Invalidate() {
	stub.invalidated = true
}

func TestP6F08PreviewRoutingReturnsOnlySafeContract(t *testing.T) {
	controller := &routingControllerStub{preview: routingpolicy.Preview{
		PolicyVersion:   routingpolicy.SelectionVersion,
		ConfigVersion:   routingpolicy.Version,
		SnapshotVersion: "rs1_safe",
		SnapshotSource:  "live",
		Model:           "gpt-4o",
		Candidates: []routingpolicy.PreviewCandidate{{
			ChannelID: 7, ChannelName: "primary", Provider: "openai", Priority: 10, Weight: 200, Order: 1, CircuitState: "healthy", EligibleKeyCount: 2, Reason: "equal_priority_static_weight",
		}},
	}}
	h := &Handler{routingController: controller}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/routing/preview?model=gpt-4o", nil)
	rec := httptest.NewRecorder()
	h.PreviewRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"policy_version":"routing-policy.v1"`) || !strings.Contains(body, `"snapshot_version":"rs1_safe"`) {
		t.Fatalf("missing safe routing contract: %s", body)
	}
	for _, forbidden := range []string{"base_url", "key_enc", "probe_token", "adapter_config"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("preview exposed forbidden field %q: %s", forbidden, body)
		}
	}
}

func TestP6F08PreviewRoutingValidatesModelAndAvailability(t *testing.T) {
	h := &Handler{}
	for _, test := range []struct {
		url    string
		status int
	}{
		{"/api/v1/routing/preview", http.StatusBadRequest},
		{"/api/v1/routing/preview?model=" + strings.Repeat("x", 201), http.StatusBadRequest},
		{"/api/v1/routing/preview?model=gpt-4o", http.StatusServiceUnavailable},
	} {
		rec := httptest.NewRecorder()
		h.PreviewRouting(rec, httptest.NewRequest(http.MethodGet, test.url, nil))
		if rec.Code != test.status {
			t.Fatalf("%s: expected %d, got %d", test.url, test.status, rec.Code)
		}
	}
}

func TestPreviewRoutingExplainsStaleRuntimeAssignment(t *testing.T) {
	h := &Handler{routingController: &routingControllerStub{err: gateway.ErrRuntimeAssignmentStale}}
	rec := httptest.NewRecorder()
	h.PreviewRouting(rec, httptest.NewRequest(http.MethodGet, "/api/v1/routing/preview?model=kimi-k3", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"runtime_assignment_stale"`) ||
		!strings.Contains(rec.Body.String(), "处理该连接保留的旧发布记录") {
		t.Fatalf("missing actionable stale-assignment contract: %s", rec.Body.String())
	}
}

func TestP6F08ListChannelsFailsClosedOnNewRoutingFieldScanError(t *testing.T) {
	h := &Handler{db: &testutil.MockDB{QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &testutil.MockRows{Rows: []func(...any) error{
			func(...any) error { return fmt.Errorf("routing_weight decode failed") },
		}}, nil
	}}}
	rec := httptest.NewRecorder()
	h.ListChannels(rec, httptest.NewRequest(http.MethodGet, "/api/v1/channels", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), `"id":0`) {
		t.Fatalf("scan failure emitted zero-value channel data: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
