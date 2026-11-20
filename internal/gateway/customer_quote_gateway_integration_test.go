//go:build integration

package gateway

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/enterprisefinance"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/streampolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPublicModelExecutionAuthorityCustomerQuoteSurvivesUpstreamFailoverIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	suite := newPublicGatewayPGSuite(t, pool, "standard-price-failover")
	a := suite.stageRoute(t, "supplier-a", modeloperation.IDResponses, publicGatewayPGExecutionProfile{baseURL: "https://supplier-a.example"}, nil)
	b := suite.stageRoute(t, "supplier-b", modeloperation.IDResponses, publicGatewayPGExecutionProfile{baseURL: "https://supplier-b.example"}, a)
	suite.activateRoutes(t, a, b)
	suite.billing.WithRequestPricingAuthority(billing.RequestPricingAuthorityCustomerStandard)

	store, err := enterprisefinance.NewPostgresStore(pool)
	require.NoError(t, err)
	writeCustomerGatewayPrice(t, store, a, "first-plan", "1000000")
	writeCustomerGatewayPrice(t, store, a, "replacement-plan", "9000000")
	_, err = store.BindCustomerPriceTable(ctx, enterprisefinance.CustomerPriceBinding{
		OrgID: suite.orgID, TableRef: "first-plan", TableVersion: 1,
	}, 0, "user:2")
	require.NoError(t, err)

	responses := map[string]*publicGatewayPGDoer{
		"supplier-a.example": {operation: a.operation, upstreamModel: a.upstreamModel, secret: a.providerSecret, responseText: "answer"},
		"supplier-b.example": {operation: b.operation, upstreamModel: b.upstreamModel, secret: b.providerSecret, responseText: "answer"},
	}
	var hosts []string
	doer := publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
		hosts = append(hosts, request.URL.Host)
		if len(hosts) == 1 {
			_, err := store.BindCustomerPriceTable(ctx, enterprisefinance.CustomerPriceBinding{
				OrgID: suite.orgID, TableRef: "replacement-plan", TableVersion: 1,
			}, 1, "user:2")
			if err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("provider unavailable"))}, nil
		}
		return responses[request.URL.Host].Do(request)
	})
	relay := publicGatewayPGRelay(t, a, doer)
	handler := publicGatewayPGHandler(a, relay, a.model, suite.apiKeyIDs[0])
	body := publicGatewayPGRequestBody(a.operation, a.model)
	result := publicGatewayPGServe(handler, a.identity.PublicPath, body, "standard-quote-failover")
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	require.ElementsMatch(t, []string{"supplier-a.example", "supplier-b.example"}, hosts)
	requestID := result.Header().Get(middleware.OperationIDHeader)

	var quotedPlan, status, version string
	var charged, promptTokens, completionTokens, attempts int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT q.receipt#>>'{selection,binding,table_ref}', f.status, f.pricing_version,
		       f.charged_cost_micros, f.prompt_tokens, f.completion_tokens
		FROM freeze_idempotency f JOIN customer_charge_quote_receipts q USING (org_id,request_id)
		WHERE f.org_id=$1 AND f.request_id=$2 AND f.pricing_version=q.pricing_version`, suite.orgID, requestID).
		Scan(&quotedPlan, &status, &version, &charged, &promptTokens, &completionTokens))
	require.Equal(t, "first-plan", quotedPlan)
	require.Equal(t, "settled", status)
	require.Regexp(t, "^ccq1_", version)
	require.Equal(t, promptTokens+completionTokens, charged)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(DISTINCT route_connection_id)
		FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, suite.orgID, requestID).Scan(&attempts))
	require.EqualValues(t, 2, attempts)
}

func TestPublicModelExecutionAuthorityCustomerQuoteStreamingOverrunIntegration(t *testing.T) {
	t.Run("accepted terminal event with higher input usage", func(t *testing.T) {
		testCustomerQuoteStreamingOverrun(t, "input_tokens", 20000, 20004, true)
	})
	t.Run("withheld terminal event with output above the protocol cap", func(t *testing.T) {
		testCustomerQuoteStreamingOverrun(t, "output_tokens", 26000, 26010, false)
	})
}

func testCustomerQuoteStreamingOverrun(t *testing.T, meter string, count, total int, terminalAllowed bool) {
	t.Helper()
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	suite := newPublicGatewayPGSuite(t, pool, "standard-price-stream-overrun")
	fixture := suite.newRouteWithExecutionProfile(t, "overrun", modeloperation.IDResponses, publicGatewayPGExecutionProfile{
		id: "openai.gpt-5.6-sol.responses", version: "1.3.0",
		baseURL: "https://api.openai.com", upstreamModel: "gpt-5.6-sol",
		capabilities: []string{modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningEffort, modelprofile.CapabilityStreaming},
	})
	suite.billing.WithRequestPricingAuthority(billing.RequestPricingAuthorityCustomerStandard)
	store, err := enterprisefinance.NewPostgresStore(pool)
	require.NoError(t, err)
	writeCustomerGatewayPrice(t, store, fixture, "stream-plan", "1000000")
	_, err = store.BindCustomerPriceTable(ctx, enterprisefinance.CustomerPriceBinding{
		OrgID: suite.orgID, TableRef: "stream-plan", TableVersion: 1,
	}, 0, "user:2")
	require.NoError(t, err)

	originalCount := 10
	if meter == "output_tokens" {
		originalCount = 4
	}
	sse := strings.NewReplacer(
		"provider-private-model", fixture.upstreamModel,
		fmt.Sprintf(`"%s":%d`, meter, originalCount), fmt.Sprintf(`"%s":%d`, meter, count),
		`"total_tokens":14`, fmt.Sprintf(`"total_tokens":%d`, total),
	).Replace(publicResponsesSSE("safe", " answer"))
	doer := publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(sse))}, nil
	})
	core := fixture.newOrchestrator(doer, allowPublicNonStreamGuard{})
	core.registry = endpointprotocol.NewPublicStreamRegistry()
	orchestrator := &publicStreamOrchestrator{
		core: core, billing: fixture.billing, pinned: disabledPublicStreamGuard(t),
		streamPolicy: streampolicy.Default(), bindStream: newPublicResponsesStreamBinding,
	}
	request := publicGatewayPGRequest(fixture, "customer-stream-overrun", fixture.model)
	request.Body = []byte(strings.TrimSuffix(string(request.Body), "}") + `,"stream":true}`)
	var events []string
	writer := &publicRuntimeWriter{events: &events}
	err = orchestrator.execute(ctx, writer, request)
	require.True(t, publicResponseWasStarted(err), "error=%v", err)
	require.Equal(t, terminalAllowed, strings.Contains(writer.body.String(), "event: response.completed"))
	require.Contains(t, writer.body.String(), "safe")

	var reason, status string
	var promptTokens, outputTokens, charged, topup, hold, maximum int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT f.settlement_pending_reason,f.status,f.prompt_tokens,f.completion_tokens,f.charged_cost_micros,
		       f.topup_amount_micros,f.frozen_amount_micros,q.maximum_authorized_micros
		FROM freeze_idempotency f JOIN customer_charge_quote_receipts q USING (org_id,request_id)
		WHERE f.org_id=$1 AND f.request_id=$2`, suite.orgID, request.RequestID).
		Scan(&reason, &status, &promptTokens, &outputTokens, &charged, &topup, &hold, &maximum))
	require.Equal(t, "customer_authorization_exceeded", reason, "stream error=%v", err)
	require.Equal(t, "settlement_pending", status)
	require.EqualValues(t, total, promptTokens+outputTokens)
	require.Zero(t, charged)
	require.Zero(t, topup)
	require.Equal(t, maximum, hold)
}

func writeCustomerGatewayPrice(t *testing.T, store *enterprisefinance.PostgresStore, fixture *publicGatewayPGFixture, ref, rate string) {
	t.Helper()
	var components []enterprisefinance.RateComponent
	for _, meter := range []string{enterprisefinance.MeterInputTokens, enterprisefinance.MeterOutputTokens, enterprisefinance.MeterCacheReadTokens, enterprisefinance.MeterCacheCreationTokens} {
		components = append(components, enterprisefinance.RateComponent{Meter: meter, UnitQuantity: "1000000",
			Amount: enterprisefinance.Amount{ValueMinor: rate, Currency: "CNY", Scale: 1_000_000}})
	}
	_, err := store.CreateCustomerPriceTable(t.Context(), enterprisefinance.CustomerPriceTable{
		OrgID: fixture.orgID, Ref: ref, Version: 1, EffectiveFrom: time.Now().UTC().Add(-time.Hour),
		Entries: []enterprisefinance.CustomerPriceEntry{{PublicModel: fixture.model, Operation: fixture.operation, Components: components}},
	}, "user:1")
	require.NoError(t, err)
	require.NoError(t, store.ApproveCustomerPriceTable(t.Context(), fixture.orgID, ref, 1, "user:2"))
}
