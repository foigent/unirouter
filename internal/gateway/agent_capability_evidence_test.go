package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/traceutil"
)

func TestCapabilityEvidenceCannotBeOverriddenByTrustedHeaders(t *testing.T) {
	relay := &Relay{evidenceContextTrustSecret: "trusted-side-channel"}
	ctx := traceutil.ContextWithEvidence(context.Background(), traceutil.EvidenceContext{
		WorkflowID: "capwf_server_owned",
		AgentID:    "agent:registered-child",
		ActorType:  "agent",
		ActorRef:   "agent:registered-child",
		OnBehalfOf: "api_key:42",
	})
	header := make(http.Header)
	header.Set(traceutil.EvidenceContextAuthHeader, "trusted-side-channel")
	header.Set(traceutil.EvidenceContextTraceIDHeader, "0123456789abcdef0123456789abcdef")
	header.Set(traceutil.EvidenceContextWorkflowIDHeader, "caller-workflow")
	header.Set(traceutil.EvidenceContextAgentIDHeader, "agent:forged")
	header.Set(traceutil.EvidenceContextActorTypeHeader, "human")
	header.Set(traceutil.EvidenceContextActorRefHeader, "user:forged")
	header.Set(traceutil.EvidenceContextOnBehalfOfHeader, "user:forged")

	merged := traceutil.EvidenceFromContext(relay.contextWithTrustedEvidence(ctx, header))
	if merged.TraceID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("trusted trace ancestry was not retained: %#v", merged)
	}
	if merged.WorkflowID != "capwf_server_owned" || merged.AgentID != "agent:registered-child" ||
		merged.ActorType != "agent" || merged.ActorRef != "agent:registered-child" || merged.OnBehalfOf != "api_key:42" {
		t.Fatalf("capability identity was overridden: %#v", merged)
	}
}

func TestCapabilityWildcardModelsReturnsCatalogInsteadOfWildcardLiteral(t *testing.T) {
	database := &testutil.MockDB{QueryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
		return &testutil.MockRows{Rows: []func(dest ...any) error{
			func(dest ...any) error { *dest[0].(*string) = "gpt-4o"; return nil },
			func(dest ...any) error { *dest[0].(*string) = "text-embedding-3-small"; return nil },
		}}, nil
	}}
	relay := &Relay{db: database}
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxAPIKey, &models.ApiKey{ID: 42, OrgID: 7, AllowedModels: "*"})
	ctx = context.WithValue(ctx, middleware.CtxAgentCapability, &agentcap.Claims{})
	recorder := httptest.NewRecorder()
	relay.HandleModels(recorder, req.WithContext(ctx))

	var response struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 2 || response.Data[0]["id"] == "*" {
		t.Fatalf("unexpected model catalog: %#v", response.Data)
	}
}
