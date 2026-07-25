package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/traceutil"
)

func TestGatewayAuthWithAgentCapabilitiesBindsTrustedIdentityAndEndpoint(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	database := agentCapabilityAuthorityDB(t)
	service, err := agentcap.NewService(database, rdb, "capability-middleware-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	rootKey := &models.ApiKey{ID: 42, OrgID: 7, Status: "active", AllowedModels: "gpt-4o", RPMLimit: 60, QPSLimit: 10}
	issued, err := service.Exchange(context.Background(), rootKey, agentcap.ExchangeRequest{
		RootAgentID:        "agent:supervisor",
		WorkflowTTLSeconds: 3600,
		Limits: agentcap.Limits{
			MaxDepth: 1, MaxChildren: 1, MaxDescendants: 1,
			CostBudgetCents: 100, TokenBudget: 1000,
			RPMLimit: 60, RPSLimit: 10, ConcurrencyLimit: 1,
			AllowedModels: []string{"gpt-4o"}, AllowedEndpoints: []string{"/v1/chat/completions"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if GetAuthKindFromContext(r.Context()) != AuthKindAgentCapability || GetKeyIDFromContext(r.Context()) != 42 {
			t.Fatalf("unexpected capability auth context")
		}
		claims := GetAgentCapabilityFromContext(r.Context())
		if claims == nil || claims.AgentID != "agent:supervisor" || claims.WorkflowID != issued.WorkflowID {
			t.Fatalf("unexpected claims: %#v", claims)
		}
		evidence := traceutil.EvidenceFromContext(r.Context())
		if evidence.WorkflowID != issued.WorkflowID || evidence.AgentID != "agent:supervisor" || evidence.ActorType != "agent" || evidence.OnBehalfOf != "api_key:42" {
			t.Fatalf("unexpected trusted evidence: %#v", evidence)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	authenticated := GatewayAuthWithAgentCapabilities(database, "capability-middleware-test-secret", service)(next)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+issued.CapabilityToken)
	recorder := httptest.NewRecorder()
	authenticated.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent || !called {
		t.Fatalf("status=%d called=%v body=%s", recorder.Code, called, recorder.Body.String())
	}

	denied := httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
	denied.Header.Set("Authorization", "Bearer "+issued.CapabilityToken)
	deniedRecorder := httptest.NewRecorder()
	authenticated.ServeHTTP(deniedRecorder, denied)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("disallowed endpoint status=%d body=%s", deniedRecorder.Code, deniedRecorder.Body.String())
	}

	mr.Close()
	unavailable := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	unavailable.Header.Set("Authorization", "Bearer "+issued.CapabilityToken)
	unavailableRecorder := httptest.NewRecorder()
	authenticated.ServeHTTP(unavailableRecorder, unavailable)
	if unavailableRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("Redis outage status=%d body=%s", unavailableRecorder.Code, unavailableRecorder.Body.String())
	}
}

func agentCapabilityAuthorityDB(t *testing.T) *testutil.MockDB {
	t.Helper()
	return &testutil.MockDB{QueryRowFn: func(_ context.Context, _ string, args ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			if len(args) == 2 && args[0] == int64(7) && args[1] == int64(42) {
				*dest[0].(*bool) = false
				return nil
			}
			if len(args) != 1 || args[0] != int64(42) {
				t.Fatalf("unexpected authority lookup: %v", args)
			}
			*dest[0].(*int64) = 42
			*dest[1].(*int64) = 7
			*dest[2].(*int64) = 0
			*dest[3].(*int64) = 0
			*dest[4].(*string) = "service"
			*dest[5].(*string) = "agent-root"
			*dest[6].(*string) = "ur-0007-root"
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

func TestAgentCapabilityTokenTTLConstant(t *testing.T) {
	if agentcap.TokenTTL != 5*time.Minute || agentcap.AuthorityCacheTTL > 30*time.Second {
		t.Fatalf("unexpected capability lifecycle constants")
	}
}
