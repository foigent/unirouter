package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func TestP6F08ChannelUpdateUsesOptionalRoutingEpochCASAndInvalidatesSnapshot(t *testing.T) {
	controller := &routingControllerStub{}
	var updateSQL string
	db := &testutil.MockDB{
		ExecFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			if strings.HasPrefix(sql, "UPDATE channels") {
				updateSQL = sql
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := &Handler{db: db, routingController: controller}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/channels/7", strings.NewReader(`{"routing_weight":250,"expected_routing_epoch":9}`))
	req.SetPathValue("id", "7")
	rec := httptest.NewRecorder()
	h.UpdateChannel(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(updateSQL, "routing_weight") || !strings.Contains(updateSQL, "routing_epoch = routing_epoch + 1") || !strings.Contains(updateSQL, "AND routing_epoch = $") {
		t.Fatalf("routing update did not use epoch CAS: %s", updateSQL)
	}
	if !controller.invalidated {
		t.Fatal("successful channel update did not invalidate local routing snapshots")
	}

	controller.invalidated = false
	db.ExecFn = func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/channels/7", strings.NewReader(`{"priority":20,"expected_routing_epoch":9}`))
	req.SetPathValue("id", "7")
	h.UpdateChannel(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale epoch expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if payload["error"] == "" || controller.invalidated {
		t.Fatalf("stale update contract violated: payload=%#v invalidated=%t", payload, controller.invalidated)
	}
}

func TestChannelUpdateValidatesSupportedStatusBeforeDatabaseAccess(t *testing.T) {
	for _, status := range []string{"active", "disabled", "deprecated"} {
		t.Run(status, func(t *testing.T) {
			var updateArgs []any
			db := &testutil.MockDB{
				ExecFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.HasPrefix(sql, "UPDATE channels") {
						updateArgs = append([]any(nil), args...)
					}
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
			}
			h := &Handler{db: db}
			req := httptest.NewRequest(http.MethodPut, "/api/v1/channels/7", strings.NewReader(fmt.Sprintf(`{"status":%q}`, status)))
			req.SetPathValue("id", "7")
			rec := httptest.NewRecorder()

			h.UpdateChannel(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 for %q, got %d: %s", status, rec.Code, rec.Body.String())
			}
			if len(updateArgs) == 0 || updateArgs[0] != status {
				t.Fatalf("status %q was not passed to the channel update: %#v", status, updateArgs)
			}
		})
	}

	databaseAccessed := false
	h := &Handler{db: &testutil.MockDB{
		ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			databaseAccessed = true
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/channels/7", strings.NewReader(`{"status":"paused"}`))
	req.SetPathValue("id", "7")
	rec := httptest.NewRecorder()

	h.UpdateChannel(rec, req)

	if rec.Code != http.StatusBadRequest || databaseAccessed {
		t.Fatalf("unsupported status must fail before database access: status=%d database_accessed=%t body=%s", rec.Code, databaseAccessed, rec.Body.String())
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
