//go:build integration

package gateway

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/testutil"
)

// SourceFactsGatewayFixture exposes the existing real gateway fixture only to
// external integration tests. Keeping the source consumer outside package
// gateway avoids a sourcefacts -> mediajob -> gateway test import cycle.
type SourceFactsGatewayFixture struct {
	Pool                 *pgxpool.Pool
	Billing              *billing.Billing
	OrgID, UserID, KeyID int64
	Path, Model          string
	Operation            modeloperation.ID
	Relay                *Relay
	Body                 []byte
	ProviderCalls        func() int64
	ProviderRequests     func() []string
	ConnectionID         int64
	BeforeProvider       func(func())
}

// SetBilling attaches an existing billing decorator to this test-only relay.
func (f SourceFactsGatewayFixture) SetBilling(provider BillingProvider) {
	f.Relay.billing = provider
}

func NewSourceFactsGatewayFixture(t *testing.T, operation modeloperation.ID) SourceFactsGatewayFixture {
	return newSourceFactsGatewayFixture(t, operation, "", "")
}

func NewSourceFactsGatewayStreamFixture(t *testing.T, operation modeloperation.ID, version, mode string) SourceFactsGatewayFixture {
	return newSourceFactsGatewayFixture(t, operation, version, mode)
}

func newSourceFactsGatewayFixture(t *testing.T, operation modeloperation.ID, version, mode string) SourceFactsGatewayFixture {
	t.Helper()
	pool := testutil.SetupMigratedTestDB(t)
	suite := newPublicGatewayPGSuite(t, pool, "source-facts")
	profile := publicGatewayPGExecutionProfile{id: "openai.gpt-5.6-sol.responses", version: "1.3.0", baseURL: "https://api.openai.com", upstreamModel: "gpt-5.6-sol",
		capabilities: []string{modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningEffort}}
	if operation == modeloperation.IDMessages {
		profile.id, profile.version, profile.baseURL, profile.upstreamModel = "anthropic.claude-sonnet-5.api", "1.3.0", "https://api.anthropic.com", "claude-sonnet-5"
	}
	if version != "" {
		profile.version = version
	}
	if mode != "" && mode != "reject_v1" {
		profile.capabilities = append(profile.capabilities, modelprofile.CapabilityStreaming)
	}
	fixture := suite.newRouteWithExecutionProfile(t, "source", operation, profile)
	doer := &publicGatewayPGDoer{operation: operation, upstreamModel: fixture.upstreamModel, secret: fixture.providerSecret, responseText: "synthetic answer"}
	var transport PublicNonStreamHTTPDoer = doer
	calls := doer.calls.Load
	requests := func() []string {
		var bodies []string
		for _, r := range doer.snapshot() {
			bodies = append(bodies, r.body)
		}
		return bodies
	}
	body := publicGatewayPGRequestBody(operation, fixture.model)
	if mode != "" {
		body = []byte(strings.TrimSuffix(string(body), "}") + `,"stream":true}`)
		sse, terminal := publicResponsesSSE("safe", " answer"), "event: response.completed"
		if operation == modeloperation.IDMessages {
			sse, terminal = publicAnthropicMessagesSSE("safe", " answer"), "event: message_stop"
		}
		sse = strings.ReplaceAll(sse, "provider-private-model", fixture.upstreamModel)
		if mode == "interrupted" {
			sse = strings.Split(sse, terminal)[0]
		}
		var count atomic.Int64
		calls = count.Load
		var bodies []string
		requests = func() []string { return append([]string(nil), bodies...) }
		transport = publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
			count.Add(1)
			wire, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			_ = request.Body.Close()
			bodies = append(bodies, string(wire))
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse))}, nil
		})
	}
	selectedTransport := transport
	var before func()
	transport = publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
		if before != nil {
			before()
		}
		return selectedTransport.Do(request)
	})
	return SourceFactsGatewayFixture{Pool: pool, Billing: fixture.billing, OrgID: fixture.orgID, UserID: fixture.userID, KeyID: fixture.apiKeyIDs[0], Path: fixture.identity.PublicPath, Model: fixture.model, Operation: operation,
		Relay: publicGatewayPGRelay(t, fixture, transport), Body: body, ProviderCalls: calls, ProviderRequests: requests, ConnectionID: fixture.connectionID, BeforeProvider: func(hook func()) { before = hook }}
}

func (f SourceFactsGatewayFixture) SetDispatchMaterializer(t *testing.T, materializer modelexecution.DispatchMaterializer) {
	t.Helper()
	coordinator, ok := f.Relay.modelExecution.coordinator.(*modelexecution.Coordinator)
	if !ok {
		t.Fatal("fixture requires original execution coordinator")
	}
	coordinator.SetDispatchMaterializer(materializer)
}
