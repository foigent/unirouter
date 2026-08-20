//go:build integration

package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

const (
	publicGatewayPGIdempotencyHashSecret = "m77-public-idempotency-hash-secret"
	publicGatewayPGFingerprintSecret     = "m77-public-fingerprint-secret"
)

type publicGatewayPGDoer struct {
	operation     modeloperation.ID
	upstreamModel string
	secret        string
	responseText  string
	transportErr  error
	calls         atomic.Int64
	mu            sync.Mutex
	requests      []publicGatewayPGProviderRequest
}

type publicGatewayPGProviderRequest struct {
	path             string
	body             string
	authorization    string
	apiKey           string
	anthropicVersion string
}

func (doer *publicGatewayPGDoer) Do(request *http.Request) (*http.Response, error) {
	doer.calls.Add(1)
	providerRequest := publicGatewayPGProviderRequest{}
	if request != nil {
		providerRequest.path = request.URL.Path
		providerRequest.authorization = request.Header.Get("Authorization")
		providerRequest.apiKey = request.Header.Get("x-api-key")
		providerRequest.anthropicVersion = request.Header.Get("anthropic-version")
		if request.Body != nil {
			body, _ := io.ReadAll(request.Body)
			providerRequest.body = string(body)
		}
	}
	doer.mu.Lock()
	doer.requests = append(doer.requests, providerRequest)
	doer.mu.Unlock()
	if doer.transportErr != nil {
		return nil, doer.transportErr
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(publicGatewayPGProviderResponse(
			doer.operation, doer.upstreamModel, doer.responseText,
		))),
	}, nil
}

func (doer *publicGatewayPGDoer) snapshot() []publicGatewayPGProviderRequest {
	doer.mu.Lock()
	defer doer.mu.Unlock()
	return append([]publicGatewayPGProviderRequest(nil), doer.requests...)
}

type publicGatewayPGMutatingCoordinator struct {
	publicModelExecutionCoordinator
	once        sync.Once
	before      func(context.Context) error
	mutationErr error
}

func (coordinator *publicGatewayPGMutatingCoordinator) AuthorizePublic(
	ctx context.Context,
	plan modelrouting.Plan,
	route modelrouting.Route,
	identity modelexecution.AttemptIdentity,
) (modelexecution.Decision, error) {
	coordinator.once.Do(func() {
		if coordinator.before != nil {
			coordinator.mutationErr = coordinator.before(ctx)
		}
	})
	if coordinator.mutationErr != nil {
		return modelexecution.Decision{}, coordinator.mutationErr
	}
	return coordinator.publicModelExecutionCoordinator.AuthorizePublic(ctx, plan, route, identity)
}

func TestPublicModelExecutionAuthorityGatewayIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)

	t.Run("responses and messages settle exact V2 delivery with metadata-only idempotency", func(t *testing.T) {
		suite := newPublicGatewayPGSuite(t, pool, "success")
		for _, operation := range []modeloperation.ID{modeloperation.IDResponses, modeloperation.IDMessages} {
			t.Run(string(operation), func(t *testing.T) {
				fixture := suite.newRoute(t, string(operation), operation)
				requestedModel := "public-alias-" + publicGatewayPGHash(fixture.releaseSuffix)[:16]
				doer := &publicGatewayPGDoer{
					operation: operation, upstreamModel: fixture.upstreamModel,
					secret: fixture.providerSecret, responseText: "public answer",
				}
				relay := publicGatewayPGRelay(t, fixture, doer)
				body := publicGatewayPGRequestBody(operation, requestedModel)
				handler := publicGatewayPGHandler(fixture, relay, requestedModel, fixture.apiKeyIDs[0])
				idempotencyKey := "same-metadata-key-" + string(operation)

				first := publicGatewayPGServe(handler, fixture.identity.PublicPath, body, idempotencyKey)
				require.Equal(t, http.StatusOK, first.Code, first.Body.String())
				require.Contains(t, first.Body.String(), `"model":"`+requestedModel+`"`)
				require.NotContains(t, first.Body.String(), fixture.model)
				require.NotContains(t, first.Body.String(), fixture.upstreamModel)
				require.EqualValues(t, 1, doer.calls.Load())
				requestID := first.Header().Get(middleware.OperationIDHeader)
				require.NotEmpty(t, requestID)
				publicGatewayPGRequireSettledV2(t, fixture, requestID)

				var freezes int
				require.NoError(t, pool.QueryRow(t.Context(), `
	SELECT COUNT(*) FROM freeze_idempotency WHERE org_id=$1 AND model_name=$2`, fixture.orgID, fixture.model).Scan(&freezes))
				require.Equal(t, 1, freezes)

				replay := publicGatewayPGServe(handler, fixture.identity.PublicPath, body, idempotencyKey)
				require.Equal(t, http.StatusConflict, replay.Code, replay.Body.String())
				require.Equal(t, requestID, replay.Header().Get(middleware.OriginalOperationHeader))
				require.EqualValues(t, 1, doer.calls.Load(), "metadata replay must not dispatch")
				require.NoError(t, pool.QueryRow(t.Context(), `
	SELECT COUNT(*) FROM freeze_idempotency WHERE org_id=$1 AND model_name=$2`, fixture.orgID, fixture.model).Scan(&freezes))
				require.Equal(t, 1, freezes, "metadata replay must not freeze again")

				otherKeyHandler := publicGatewayPGHandler(fixture, relay, requestedModel, fixture.apiKeyIDs[1])
				otherKey := publicGatewayPGServe(otherKeyHandler, fixture.identity.PublicPath, body, idempotencyKey)
				require.Equal(t, http.StatusOK, otherKey.Code, otherKey.Body.String())
				require.EqualValues(t, 2, doer.calls.Load(), "an independent API-key scope must not hit the first key")
				publicGatewayPGRequireSettledV2(t, fixture, otherKey.Header().Get(middleware.OperationIDHeader))

				providerRequests := doer.snapshot()
				require.Len(t, providerRequests, 2)
				for _, providerRequest := range providerRequests {
					require.Equal(t, fixture.identity.PublicPath, providerRequest.path)
					require.Contains(t, providerRequest.body, `"model":"`+fixture.upstreamModel+`"`)
					require.NotContains(t, providerRequest.body, requestedModel)
					if operation == modeloperation.IDResponses {
						require.Equal(t, "Bearer "+fixture.providerSecret, providerRequest.authorization)
						require.Empty(t, providerRequest.apiKey)
					} else {
						require.Empty(t, providerRequest.authorization)
						require.Equal(t, fixture.providerSecret, providerRequest.apiKey)
						require.Equal(t, "2023-06-01", providerRequest.anthropicVersion)
					}
				}
			})
		}
	})

	t.Run("post-freeze structural and credential drift roll back attempt and cancel safely", func(t *testing.T) {
		for _, test := range []struct {
			name   string
			mutate func(context.Context, *publicGatewayPGFixture) error
		}{
			{name: "structural revision", mutate: func(ctx context.Context, fixture *publicGatewayPGFixture) error {
				return fixture.mutateStructuralRevision(ctx)
			}},
			{name: "selected credential", mutate: func(ctx context.Context, fixture *publicGatewayPGFixture) error {
				return fixture.mutateCredentialDisabled(ctx)
			}},
		} {
			t.Run(test.name, func(t *testing.T) {
				fixture := newPublicGatewayPGFixture(t, pool, "drift-"+test.name, modeloperation.IDResponses)
				doer := &publicGatewayPGDoer{
					operation: fixture.operation, upstreamModel: fixture.upstreamModel,
					secret: fixture.providerSecret, responseText: "must not dispatch",
				}
				coordinator := &publicGatewayPGMutatingCoordinator{
					publicModelExecutionCoordinator: fixture.coordinator,
					before:                          func(ctx context.Context) error { return test.mutate(ctx, fixture) },
				}
				orchestrator := fixture.newOrchestratorWithCoordinator(coordinator, doer, allowPublicNonStreamGuard{})
				request := publicGatewayPGRequest(fixture, "drift", fixture.model)
				err := orchestrator.execute(t.Context(), httptest.NewRecorder(), request)
				require.Error(t, err)
				require.EqualValues(t, 0, doer.calls.Load())
				publicGatewayPGRequireUndispatchedCancel(t, fixture, request.RequestID)
			})
		}
	})

	t.Run("output block retains accepted provider evidence without delivery or charge", func(t *testing.T) {
		fixture := newPublicGatewayPGFixture(t, pool, "output-block", modeloperation.IDResponses)
		events := []string{}
		guard := &publicRuntimeGuard{
			events: &events,
			output: PublicOutputGuardDecision{Action: PublicOutputGuardBlock},
		}
		doer := &publicGatewayPGDoer{
			operation: fixture.operation, upstreamModel: fixture.upstreamModel,
			secret: fixture.providerSecret, responseText: "withheld answer",
		}
		request := publicGatewayPGRequest(fixture, "output-block", fixture.model)
		recorder := httptest.NewRecorder()
		err := fixture.newOrchestrator(doer, guard).execute(t.Context(), recorder, request)
		require.ErrorIs(t, err, ErrPublicNonStreamOutputWithheld)
		require.Empty(t, recorder.Body.String(), "guarded provider body is evidence-only and must never be written")
		require.EqualValues(t, 1, doer.calls.Load())
		require.Contains(t, events, "guard:output")

		var reservationStatus, pendingReason, usagePurpose string
		var attemptState, healthOutcome, failureKind, evidenceVersion string
		var publicBindingID int64
		var promptTokens, completionTokens int
		require.NoError(t, pool.QueryRow(t.Context(), `
SELECT status,settlement_pending_reason,usage_purpose,prompt_tokens,completion_tokens
  FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, fixture.orgID, request.RequestID,
		).Scan(&reservationStatus, &pendingReason, &usagePurpose, &promptTokens, &completionTokens))
		require.NoError(t, pool.QueryRow(t.Context(), `
SELECT state,health_outcome,COALESCE(failure_kind,''),route_evidence_version,
       route_public_execution_binding_id
  FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, request.RequestID,
		).Scan(&attemptState, &healthOutcome, &failureKind, &evidenceVersion, &publicBindingID))
		require.Equal(t, "settlement_pending", reservationStatus)
		require.Equal(t, billing.OutputHoldReasonGuardrailWithheld, pendingReason)
		require.Equal(t, string(metering.UsagePurposeProviderEvidence), usagePurpose)
		require.Equal(t, 10, promptTokens)
		require.Equal(t, 4, completionTokens)
		require.Equal(t, billing.ProviderAttemptStateOutputWithheld, attemptState)
		require.Equal(t, "success", healthOutcome)
		require.Empty(t, failureKind)
		require.Equal(t, executionevidence.ContractVersionV2, evidenceVersion)
		require.Equal(t, fixture.activeBindingID, publicBindingID)
		publicGatewayPGRequireNoDeliveryOrTerminalCharge(t, fixture, request.RequestID)
	})

	t.Run("transport ambiguity is terminal pending without retry cancel delivery or settlement", func(t *testing.T) {
		fixture := newPublicGatewayPGFixture(t, pool, "transport-ambiguity", modeloperation.IDMessages)
		doer := &publicGatewayPGDoer{
			operation: fixture.operation, upstreamModel: fixture.upstreamModel,
			secret: fixture.providerSecret, transportErr: errors.New("ambiguous transport fixture"),
		}
		request := publicGatewayPGRequest(fixture, "transport-ambiguity", fixture.model)
		recorder := httptest.NewRecorder()
		err := fixture.newOrchestrator(doer, allowPublicNonStreamGuard{}).execute(t.Context(), recorder, request)
		require.ErrorIs(t, err, errPublicNonStreamUpstreamFailed)
		require.Empty(t, recorder.Body.String())
		require.EqualValues(t, 1, doer.calls.Load(), "ambiguous transport must never retry")

		var reservationStatus, pendingReason string
		var attemptState, healthOutcome, failureKind, evidenceVersion string
		var publicBindingID int64
		require.NoError(t, pool.QueryRow(t.Context(), `
SELECT status,settlement_pending_reason
  FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, fixture.orgID, request.RequestID,
		).Scan(&reservationStatus, &pendingReason))
		require.NoError(t, pool.QueryRow(t.Context(), `
SELECT state,health_outcome,COALESCE(failure_kind,''),route_evidence_version,
       route_public_execution_binding_id
  FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, request.RequestID,
		).Scan(&attemptState, &healthOutcome, &failureKind, &evidenceVersion, &publicBindingID))
		require.Equal(t, "settlement_pending", reservationStatus)
		require.Equal(t, "ambiguous_attempt", pendingReason)
		require.Equal(t, billing.ProviderAttemptOutcomeUnknown, attemptState)
		require.Equal(t, "provider_failure", healthOutcome)
		require.Equal(t, "channel_failure", failureKind)
		require.Equal(t, executionevidence.ContractVersionV2, evidenceVersion)
		require.Equal(t, fixture.activeBindingID, publicBindingID)
		publicGatewayPGRequireNoDeliveryOrTerminalCharge(t, fixture, request.RequestID)
	})
}

func publicGatewayPGRelay(t *testing.T, fixture *publicGatewayPGFixture, doer PublicNonStreamHTTPDoer) *Relay {
	t.Helper()
	relay := NewRelay(fixture.pool, nil, nil, fixture.billing, nil, fixture.encryptionKey)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, fixture.planner, fixture.coordinator, routingpolicy.Default(),
	))
	require.NoError(t, relay.SetBillingFreezePolicy("1", 1))
	service, err := guardrail.NewService(
		fixture.pool, filter.NewDFAFilter(nil), []byte("m77-public-guardrail-secret"), guardrail.DefaultOptions(),
	)
	require.NoError(t, err)
	relay.SetGuardrail(service)
	relay.providerHTTPDoer = doer
	return relay
}

func publicGatewayPGHandler(
	fixture *publicGatewayPGFixture,
	relay *Relay,
	requestedModel string,
	apiKeyID int64,
) http.Handler {
	apiKey := &models.ApiKey{ID: apiKeyID, OrgID: fixture.orgID, Status: "active"}
	endpoint := fixture.identity.PublicPath
	final := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		relay.HandlePublicNonStream(w, request, fixture.operation)
	})
	guarded := middleware.IdempotencyGuard(
		middleware.NewPostgresIdempotencyStore(fixture.pool), publicGatewayPGFingerprintSecret, final,
	)
	strict := middleware.StrictJSONIngress(guarded)
	budget := payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits())
	retained := payloadpolicy.RequestMiddlewareWithMax(
		budget, 8*payloadpolicy.MiB, middleware.GetOrgIDFromContext,
	)(strict)
	bound := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		ctx := context.WithValue(request.Context(), middleware.CtxAPIKey, apiKey)
		ctx = context.WithValue(ctx, middleware.CtxOrgID, fixture.orgID)
		ctx = context.WithValue(ctx, middleware.CtxKeyID, apiKeyID)
		ctx = context.WithValue(ctx, middleware.CtxModelAlias, requestedModel)
		ctx = modelauthz.WithDecision(ctx, modelauthz.Decision{
			ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: endpoint,
			RequestedModel: requestedModel, ResolvedModel: fixture.model, RuntimeMutation: "none",
		})
		retained.ServeHTTP(w, request.WithContext(ctx))
	})
	return middleware.OperationIdentityWithSecret(publicGatewayPGIdempotencyHashSecret)(bound)
}

func publicGatewayPGServe(handler http.Handler, path string, body []byte, idempotencyKey string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(middleware.IdempotencyKeyHeader, idempotencyKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func publicGatewayPGRequest(
	fixture *publicGatewayPGFixture,
	label string,
	requestedModel string,
) publicNonStreamRequest {
	return publicNonStreamRequest{
		Operation: fixture.operation, Path: fixture.identity.PublicPath,
		Body:        publicGatewayPGRequestBody(fixture.operation, requestedModel),
		PublicModel: requestedModel, ResolvedModel: fixture.model,
		RequestID: "op_pg_" + publicGatewayPGHash(fixture.releaseSuffix + ":" + label)[:32],
		OrgID:     fixture.orgID, APIKeyID: fixture.apiKeyIDs[0],
	}
}

func publicGatewayPGRequestBody(operation modeloperation.ID, model string) []byte {
	encodedModel, _ := json.Marshal(model)
	if operation == modeloperation.IDMessages {
		return []byte(`{"model":` + string(encodedModel) + `,"max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
	}
	return []byte(`{"model":` + string(encodedModel) + `,"input":"hello","max_output_tokens":64}`)
}

func publicGatewayPGProviderResponse(operation modeloperation.ID, model, text string) string {
	encodedModel, _ := json.Marshal(model)
	encodedText, _ := json.Marshal(text)
	if operation == modeloperation.IDMessages {
		return `{"id":"msg_pg","type":"message","role":"assistant","model":` + string(encodedModel) +
			`,"content":[{"type":"text","text":` + string(encodedText) +
			`}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":4}}`
	}
	return `{"id":"resp_pg","object":"response","status":"completed","model":` + string(encodedModel) +
		`,"error":null,"incomplete_details":null,"output":[{"id":"msg_pg","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":` + string(encodedText) +
		`}]}],"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}`
}

func publicGatewayPGRequireSettledV2(t *testing.T, fixture *publicGatewayPGFixture, requestID string) {
	t.Helper()
	var reservationStatus, reservationModel string
	var attemptState, healthOutcome, evidenceVersion string
	var publicBindingID int64
	var deliveryReceipts, settleTransactions int
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
	SELECT status,model_name FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&reservationStatus, &reservationModel))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT state,health_outcome,route_evidence_version,route_public_execution_binding_id
  FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&attemptState, &healthOutcome, &evidenceVersion, &publicBindingID))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&deliveryReceipts))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type='settle'`, fixture.orgID, requestID,
	).Scan(&settleTransactions))
	require.Equal(t, "settled", reservationStatus)
	require.Equal(t, fixture.model, reservationModel)
	require.Equal(t, "settled", attemptState)
	require.Equal(t, "success", healthOutcome)
	require.Equal(t, executionevidence.ContractVersionV2, evidenceVersion)
	require.Equal(t, fixture.activeBindingID, publicBindingID)
	require.Equal(t, 2, deliveryReceipts)
	require.Equal(t, 1, settleTransactions)
}

func publicGatewayPGRequireUndispatchedCancel(t *testing.T, fixture *publicGatewayPGFixture, requestID string) {
	t.Helper()
	var reservationStatus string
	var attempts, providerTerminals, deliveries int
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT status FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&reservationStatus))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&attempts))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type IN ('settle','cancel')`, fixture.orgID, requestID,
	).Scan(&providerTerminals))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&deliveries))
	require.Equal(t, "cancelled", reservationStatus)
	require.Zero(t, attempts, "stale authority must roll back attempt-first receipt")
	require.Equal(t, 1, providerTerminals, "safe cancellation is the only terminal ledger event")
	require.Zero(t, deliveries)
}

func publicGatewayPGRequireNoDeliveryOrTerminalCharge(t *testing.T, fixture *publicGatewayPGFixture, requestID string) {
	t.Helper()
	var deliveries, terminalTransactions int
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&deliveries))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type IN ('settle','cancel')`, fixture.orgID, requestID,
	).Scan(&terminalTransactions))
	require.Zero(t, deliveries)
	require.Zero(t, terminalTransactions)
}
