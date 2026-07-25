package capapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCapabilityControlHTTPFlow(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	database := capabilityHandlerDB(t)
	service, err := agentcap.NewService(database, rdb, "capability-handler-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service)
	rootKey := &models.ApiKey{ID: 9, OrgID: 4, Status: "active", AllowedModels: "gpt-4o", RPMLimit: 60, QPSLimit: 10}

	exchangeBody := `{"root_agent_id":"agent:root","workflow_ttl_seconds":3600,"limits":{"max_depth":2,"max_children":2,"max_descendants":4,"cost_budget_cents":1000,"token_budget":10000,"rpm_limit":60,"rps_limit":10,"concurrency_limit":2,"allowed_models":["gpt-4o"],"allowed_endpoints":["/v1/chat/completions"]}}`
	exchangeRequest := httptest.NewRequest(http.MethodPost, "/v1/agent-capabilities/exchange", strings.NewReader(exchangeBody))
	exchangeRequest = exchangeRequest.WithContext(rootContext(exchangeRequest.Context(), rootKey))
	exchangeRecorder := httptest.NewRecorder()
	handler.Exchange(exchangeRecorder, exchangeRequest)
	if exchangeRecorder.Code != http.StatusCreated || exchangeRecorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("exchange status=%d headers=%v body=%s", exchangeRecorder.Code, exchangeRecorder.Header(), exchangeRecorder.Body.String())
	}
	var rootToken agentcap.TokenResponse
	if err := json.Unmarshal(exchangeRecorder.Body.Bytes(), &rootToken); err != nil {
		t.Fatal(err)
	}
	rootAuth, err := service.Authenticate(context.Background(), rootToken.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}

	delegateBody := `{"child_agent_id":"agent:child","limits":{"max_depth":1,"max_children":0,"max_descendants":0,"cost_budget_cents":100,"token_budget":1000,"rpm_limit":10,"rps_limit":2,"concurrency_limit":1,"allowed_models":["gpt-4o"],"allowed_endpoints":["/v1/chat/completions"]}}`
	delegateRequest := httptest.NewRequest(http.MethodPost, "/v1/agent-capabilities/delegate", strings.NewReader(delegateBody))
	delegateContext := context.WithValue(delegateRequest.Context(), middleware.CtxAuthKind, middleware.AuthKindAgentCapability)
	delegateContext = context.WithValue(delegateContext, middleware.CtxAgentCapability, rootAuth.Claims)
	delegateRequest = delegateRequest.WithContext(delegateContext)
	delegateRecorder := httptest.NewRecorder()
	handler.Delegate(delegateRecorder, delegateRequest)
	if delegateRecorder.Code != http.StatusCreated {
		t.Fatalf("delegate status=%d body=%s", delegateRecorder.Code, delegateRecorder.Body.String())
	}
	var childToken agentcap.TokenResponse
	if err := json.Unmarshal(delegateRecorder.Body.Bytes(), &childToken); err != nil {
		t.Fatal(err)
	}

	refreshRequest := httptest.NewRequest(http.MethodPost, "/v1/agent-capabilities/refresh", nil)
	refreshContext := context.WithValue(refreshRequest.Context(), middleware.CtxAuthKind, middleware.AuthKindAgentCapability)
	refreshContext = context.WithValue(refreshContext, middleware.CtxAgentCapability, rootAuth.Claims)
	refreshRequest = refreshRequest.WithContext(refreshContext)
	refreshRecorder := httptest.NewRecorder()
	handler.Refresh(refreshRecorder, refreshRequest)
	if refreshRecorder.Code != http.StatusConflict {
		t.Fatalf("early refresh status=%d body=%s", refreshRecorder.Code, refreshRecorder.Body.String())
	}

	revokeRequest := httptest.NewRequest(http.MethodPost, "/v1/agent-capabilities/revoke", strings.NewReader(`{"workflow_id":"`+rootToken.WorkflowID+`"}`))
	revokeRequest = revokeRequest.WithContext(rootContext(revokeRequest.Context(), rootKey))
	revokeRecorder := httptest.NewRecorder()
	handler.Revoke(revokeRecorder, revokeRequest)
	if revokeRecorder.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", revokeRecorder.Code, revokeRecorder.Body.String())
	}
	if _, err := service.Authenticate(context.Background(), childToken.CapabilityToken); err == nil {
		t.Fatal("revoked child capability remained valid")
	}
}

func rootContext(ctx context.Context, key *models.ApiKey) context.Context {
	ctx = context.WithValue(ctx, middleware.CtxAuthKind, middleware.AuthKindAPIKey)
	return context.WithValue(ctx, middleware.CtxAPIKey, key)
}

func capabilityHandlerDB(t *testing.T) *testutil.MockDB {
	t.Helper()
	return &testutil.MockDB{QueryRowFn: func(_ context.Context, _ string, args ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			if len(args) != 1 || args[0] != int64(9) {
				t.Fatalf("unexpected authority lookup: %v", args)
			}
			if len(dest) != 19 {
				t.Fatalf("unexpected authority scan width: %d", len(dest))
			}
			*dest[0].(*int64) = 9
			*dest[1].(*int64) = 4
			*dest[2].(*int64) = 0
			*dest[3].(*int64) = 0
			*dest[4].(*string) = "service"
			*dest[5].(*string) = "root"
			*dest[6].(*string) = "ur-0004-root"
			*dest[7].(*string) = "active"
			*dest[8].(*string) = ""
			*dest[9].(*string) = "gpt-4o"
			*dest[10].(*string) = ""
			*dest[11].(*int) = 60
			*dest[12].(*int) = 10
			*dest[13].(*int) = -1
			*dest[14].(*string) = "active"
			*dest[15].(*string) = ""
			*dest[16].(*string) = ""
			*dest[17].(*string) = ""
			*dest[18].(*string) = ""
			return nil
		}}
	}}
}
