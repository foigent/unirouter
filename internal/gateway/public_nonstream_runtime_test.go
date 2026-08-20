package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

type publicRuntimePlanner struct {
	events *[]string
	plan   modelrouting.Plan
}

func (planner *publicRuntimePlanner) Plan(_ context.Context, request modelrouting.Request) (modelrouting.Plan, error) {
	*planner.events = append(*planner.events, "plan")
	if request.Mode != modelrouting.ModeCanonical || request.CustomerOrgID != 11 ||
		request.PublicModel != "public-text" || request.ModelOperationID != string(modeloperation.IDResponses) {
		return modelrouting.Plan{}, errors.New("unexpected public plan request")
	}
	return planner.plan, nil
}

type publicRuntimeCoordinator struct {
	events       *[]string
	authorize    []modelexecution.Status
	attemptIDs   []string
	healthStatus modelexecution.Status
	healthErr    error
}

func (coordinator *publicRuntimeCoordinator) AuthorizePublic(
	_ context.Context,
	_ modelrouting.Plan,
	_ modelrouting.Route,
	identity modelexecution.AttemptIdentity,
) (modelexecution.Decision, error) {
	*coordinator.events = append(*coordinator.events, "authorize:"+strconvItoa(identity.RouteSequence))
	coordinator.attemptIDs = append(coordinator.attemptIDs, identity.AttemptID)
	status := modelexecution.Applied
	if len(coordinator.authorize) >= identity.RouteSequence {
		status = coordinator.authorize[identity.RouteSequence-1]
	}
	return modelexecution.Decision{
		Status: status,
		ProviderAttemptKey: modelexecution.ProviderAttemptKey{
			OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID,
		},
	}, nil
}

func (coordinator *publicRuntimeCoordinator) health(name string) (modelexecution.HealthResult, error) {
	*coordinator.events = append(*coordinator.events, "health:"+name)
	status := coordinator.healthStatus
	if status == "" {
		status = modelexecution.Applied
	}
	return modelexecution.HealthResult{Status: status}, coordinator.healthErr
}

func (coordinator *publicRuntimeCoordinator) ReleaseUndispatched(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error) {
	return coordinator.health("released")
}
func (coordinator *publicRuntimeCoordinator) MarkSuccess(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error) {
	return coordinator.health("success")
}
func (coordinator *publicRuntimeCoordinator) MarkProviderFailure(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error) {
	return coordinator.health("provider_failure")
}
func (coordinator *publicRuntimeCoordinator) MarkCredentialAuthFailure(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error) {
	return coordinator.health("auth_failure")
}
func (coordinator *publicRuntimeCoordinator) MarkCredentialRateLimited(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error) {
	return coordinator.health("rate_limited")
}

type publicRuntimeBilling struct {
	events       *[]string
	freeze       []billing.FreezeRequest
	completions  []billing.ProviderAttemptCompletion
	prepared     []billing.SettleRequest
	helds        []billing.SettleRequest
	holdReasons  []string
	deliveries   []billing.DeliveryEvent
	settled      []billing.SettleRequest
	cancelled    int
	prepareError error
	cancelError  error
}

func (service *publicRuntimeBilling) Freeze(_ context.Context, request billing.FreezeRequest) (*billing.FreezeResult, error) {
	*service.events = append(*service.events, "freeze")
	service.freeze = append(service.freeze, request)
	return &billing.FreezeResult{Success: true}, nil
}

func (service *publicRuntimeBilling) CompleteProviderAttempt(_ context.Context, request billing.ProviderAttemptCompletion) error {
	*service.events = append(*service.events, "complete:"+request.Outcome)
	service.completions = append(service.completions, request)
	return nil
}

func (service *publicRuntimeBilling) PrepareNonStreamDelivery(_ context.Context, request billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
	*service.events = append(*service.events, "prepare_delivery")
	service.prepared = append(service.prepared, request)
	if service.prepareError != nil {
		return nil, service.prepareError
	}
	return &billing.PrepareDeliveryResult{Funded: true}, nil
}

func (service *publicRuntimeBilling) HoldNonStreamOutput(_ context.Context, request billing.SettleRequest, reason string) error {
	*service.events = append(*service.events, "hold:"+reason)
	service.helds = append(service.helds, request)
	service.holdReasons = append(service.holdReasons, reason)
	return nil
}

func (service *publicRuntimeBilling) RecordDelivery(_ context.Context, event billing.DeliveryEvent) error {
	*service.events = append(*service.events, "delivery:"+event.Outcome)
	service.deliveries = append(service.deliveries, event)
	return nil
}

func (service *publicRuntimeBilling) Settle(_ context.Context, request billing.SettleRequest) (*billing.SettleResult, error) {
	*service.events = append(*service.events, "settle")
	service.settled = append(service.settled, request)
	return &billing.SettleResult{Success: true}, nil
}

func (service *publicRuntimeBilling) Cancel(context.Context, string, int64) error {
	*service.events = append(*service.events, "cancel")
	service.cancelled++
	return service.cancelError
}

type publicRuntimeGuard struct {
	events         *[]string
	input          PublicOutputGuardDecision
	inputErr       error
	output         PublicOutputGuardDecision
	outputErr      error
	inputAdmission endpointprotocol.PublicAdmission
}

func (guard *publicRuntimeGuard) EvaluateInput(_ context.Context, admission endpointprotocol.PublicAdmission) (PublicOutputGuardDecision, error) {
	*guard.events = append(*guard.events, "guard:input")
	guard.inputAdmission = admission
	decision := guard.input
	if decision.Action == "" {
		decision.Action = PublicOutputGuardAllow
	}
	return decision, guard.inputErr
}

func (guard *publicRuntimeGuard) Evaluate(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
	*guard.events = append(*guard.events, "guard:output")
	decision := guard.output
	if decision.Action == "" && guard.outputErr == nil {
		decision.Action = PublicOutputGuardAllow
	}
	return decision, guard.outputErr
}

type publicRuntimeWriter struct {
	events *[]string
	header http.Header
	status int
	body   strings.Builder
}

func (writer *publicRuntimeWriter) Header() http.Header {
	if writer.header == nil {
		writer.header = make(http.Header)
	}
	return writer.header
}

func (writer *publicRuntimeWriter) WriteHeader(status int) { writer.status = status }

func (writer *publicRuntimeWriter) Write(body []byte) (int, error) {
	*writer.events = append(*writer.events, "customer_write")
	return writer.body.Write(body)
}

func TestPublicNonStreamOrchestratorSuccessUsesExactOrderAndCanonicalBody(t *testing.T) {
	events := []string{}
	fixture := newPublicRuntimeFixture(t, &events, 2)
	fixture.guard.input = PublicOutputGuardDecision{Action: PublicOutputGuardReplace, Replacements: []string{"safe hello"}}
	writer := &publicRuntimeWriter{events: &events}
	if err := fixture.orchestrator.execute(t.Context(), writer, fixture.request); err != nil {
		t.Fatal(err)
	}
	want := "guard:input,plan,build:ciphertext-1,build:ciphertext-2,freeze,authorize:1,provider:provider-1.example,guard:output,complete:response_accepted,health:success,prepare_delivery,delivery:write_started,customer_write,delivery:completed,settle"
	if got := strings.Join(events, ","); got != want {
		t.Fatalf("order=%s\nwant=%s", got, want)
	}
	if len(fixture.billing.freeze) != 1 || len(fixture.billing.prepared) != 1 ||
		len(fixture.billing.deliveries) != 2 || len(fixture.billing.settled) != 1 ||
		len(fixture.billing.helds) != 0 || fixture.billing.cancelled != 0 {
		t.Fatalf("billing calls freeze=%d prepared=%d deliveries=%d settled=%d held=%d cancel=%d",
			len(fixture.billing.freeze), len(fixture.billing.prepared), len(fixture.billing.deliveries),
			len(fixture.billing.settled), len(fixture.billing.helds), fixture.billing.cancelled)
	}
	if writer.status != http.StatusOK || !strings.Contains(writer.body.String(), `"model":"public-text"`) ||
		strings.Contains(writer.body.String(), "provider-private-model") || writer.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe delivery status=%d header=%v body=%s", writer.status, writer.Header(), writer.body.String())
	}
	if !strings.Contains(fixture.providerBodies["provider-1.example"], `"input":"safe hello"`) ||
		strings.Contains(fixture.providerBodies["provider-1.example"], `"input":"hello"`) {
		t.Fatalf("guarded request not strictly rebuilt: %s", fixture.providerBodies["provider-1.example"])
	}
	settle := fixture.billing.settled[0]
	if settle.UsagePurpose != metering.UsagePurposeCustomerCharge || settle.AttemptID != fixture.coordinator.attemptIDs[0] ||
		settle.PromptTokens != 10 || settle.CompletionTokens != 4 {
		t.Fatalf("settlement=%+v attempts=%v", settle, fixture.coordinator.attemptIDs)
	}
}

func TestPublicNonStreamOrchestratorGuardFailureRetainsEvidenceWithoutDelivery(t *testing.T) {
	for _, test := range []struct {
		name      string
		decision  PublicOutputGuardDecision
		guardErr  error
		wantCause error
	}{
		{name: "block", decision: PublicOutputGuardDecision{Action: PublicOutputGuardBlock}, wantCause: ErrPublicNonStreamOutputWithheld},
		{name: "error", guardErr: errors.New("private guard detail"), wantCause: ErrPublicNonStreamOutputGuard},
	} {
		t.Run(test.name, func(t *testing.T) {
			events := []string{}
			fixture := newPublicRuntimeFixture(t, &events, 1)
			fixture.guard.output = test.decision
			fixture.guard.outputErr = test.guardErr
			writer := &publicRuntimeWriter{events: &events}
			err := fixture.orchestrator.execute(t.Context(), writer, fixture.request)
			if !errors.Is(err, errPublicNonStreamGuardFailed) || !errors.Is(err, test.wantCause) {
				t.Fatalf("error=%v", err)
			}
			want := "guard:input,plan,build:ciphertext-1,freeze,authorize:1,provider:provider-1.example,guard:output,complete:response_accepted,health:success,hold:guardrail_withheld"
			if got := strings.Join(events, ","); got != want {
				t.Fatalf("order=%s\nwant=%s", got, want)
			}
			if len(fixture.billing.helds) != 1 || len(fixture.billing.prepared) != 0 ||
				len(fixture.billing.deliveries) != 0 || len(fixture.billing.settled) != 0 || writer.body.Len() != 0 {
				t.Fatalf("guard leakage held=%d prepared=%d delivery=%d settle=%d body=%q",
					len(fixture.billing.helds), len(fixture.billing.prepared), len(fixture.billing.deliveries),
					len(fixture.billing.settled), writer.body.String())
			}
			hold := fixture.billing.helds[0]
			if hold.UsagePurpose != metering.UsagePurposeProviderEvidence ||
				hold.AttemptID != fixture.coordinator.attemptIDs[0] || hold.PromptTokens != 10 || hold.CompletionTokens != 4 {
				t.Fatalf("hold=%+v attempts=%v", hold, fixture.coordinator.attemptIDs)
			}
			if len(fixture.billing.completions) != 1 || fixture.billing.completions[0].FailureKind != "" {
				t.Fatalf("guard success persisted failure metadata: %+v", fixture.billing.completions)
			}
		})
	}
}

func TestPublicNonStreamOrchestratorRetriesOnlyKnownRejectedAndCancelsAllFailure(t *testing.T) {
	for _, secondStatus := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(secondStatus), func(t *testing.T) {
			events := []string{}
			fixture := newPublicRuntimeFixture(t, &events, 2)
			fixture.providerStatuses["provider-1.example"] = http.StatusServiceUnavailable
			fixture.providerStatuses["provider-2.example"] = secondStatus
			writer := &publicRuntimeWriter{events: &events}
			err := fixture.orchestrator.execute(t.Context(), writer, fixture.request)
			if secondStatus == http.StatusOK {
				if err != nil || fixture.billing.cancelled != 0 || len(fixture.billing.completions) != 2 ||
					fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeRejected ||
					!fixture.billing.completions[0].RetryAuthorized {
					t.Fatalf("fallback err=%v completions=%+v cancel=%d", err, fixture.billing.completions, fixture.billing.cancelled)
				}
				if !strings.Contains(strings.Join(events, ","), "health:provider_failure,authorize:2,provider:provider-2.example") {
					t.Fatalf("fallback order=%s", strings.Join(events, ","))
				}
				return
			}
			if !errors.Is(err, errPublicNonStreamUpstreamFailed) || fixture.billing.cancelled != 1 ||
				len(fixture.billing.prepared) != 0 || len(fixture.billing.deliveries) != 0 || writer.body.Len() != 0 {
				t.Fatalf("all failure err=%v cancel=%d prepared=%d delivery=%d body=%q",
					err, fixture.billing.cancelled, len(fixture.billing.prepared), len(fixture.billing.deliveries), writer.body.String())
			}
		})
	}
}

func TestPublicNonStreamOrchestratorExactM77FencePrecedesSecretFreezeAndNetwork(t *testing.T) {
	events := []string{}
	fixture := newPublicRuntimeFixture(t, &events, 1)
	route := fixture.planner.plan.Routes[0]
	route.PublicEvidence = nil
	fixture.planner.plan.Routes[0] = route
	err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
	if !errors.Is(err, errPublicNonStreamContractMismatch) {
		t.Fatalf("error=%v", err)
	}
	if got := strings.Join(events, ","); got != "guard:input,plan" || len(fixture.billing.freeze) != 0 ||
		len(fixture.billing.completions) != 0 {
		t.Fatalf("M75 route crossed exact fence: events=%s freeze=%d attempts=%d",
			got, len(fixture.billing.freeze), len(fixture.billing.completions))
	}
}

func TestPublicNonStreamOrchestratorExactM77FenceRejectsPlanEvidenceDriftBeforeSecretFreezeAndNetwork(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*executionevidence.EvidenceV2)
	}{
		{name: "public model", mutate: func(value *executionevidence.EvidenceV2) { value.PublicModelID++ }},
		{name: "operation", mutate: func(value *executionevidence.EvidenceV2) { value.ModelOperationID = string(modeloperation.IDMessages) }},
		{name: "snapshot", mutate: func(value *executionevidence.EvidenceV2) {
			value.SnapshotHash = "mrs1_1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		}},
		{name: "release id", mutate: func(value *executionevidence.EvidenceV2) { value.RuntimeReleaseID++ }},
		{name: "release ref", mutate: func(value *executionevidence.EvidenceV2) { value.RuntimeReleaseRef = "rcr_drift" }},
		{name: "runtime stage", mutate: func(value *executionevidence.EvidenceV2) { value.RuntimeStage = "canary" }},
		{name: "slot version", mutate: func(value *executionevidence.EvidenceV2) { value.SlotVersion++ }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			events := []string{}
			fixture := newPublicRuntimeFixture(t, &events, 1)
			route := fixture.planner.plan.Routes[0]
			public := *route.PublicEvidence
			test.mutate(&public)
			route.PublicEvidence = &public
			fixture.planner.plan.Routes[0] = route

			err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
			if !errors.Is(err, errPublicNonStreamContractMismatch) {
				t.Fatalf("error=%v", err)
			}
			if got := strings.Join(events, ","); got != "guard:input,plan" || len(fixture.billing.freeze) != 0 ||
				len(fixture.billing.completions) != 0 || len(fixture.coordinator.attemptIDs) != 0 {
				t.Fatalf("drift crossed early fence: events=%s freeze=%d completions=%d attempts=%d",
					got, len(fixture.billing.freeze), len(fixture.billing.completions), len(fixture.coordinator.attemptIDs))
			}
		})
	}
}

func TestPublicNonStreamOrchestratorExactM77FenceRejectsStaticRouteDriftBeforeSecretFreezeAndNetwork(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*modelrouting.Route)
	}{
		{name: "embedded profile id", mutate: func(route *modelrouting.Route) {
			public := *route.PublicEvidence
			public.ProtocolProfileID = "wrong.public.profile"
			route.PublicEvidence = &public
		}},
		{name: "embedded profile version", mutate: func(route *modelrouting.Route) {
			public := *route.PublicEvidence
			public.ProtocolProfileVersion = "v99"
			route.PublicEvidence = &public
		}},
		{name: "probe requirement", mutate: func(route *modelrouting.Route) { route.RequiresProbe = !route.RequiresProbe }},
		{name: "approval", mutate: func(route *modelrouting.Route) { route.ProductionApprovalID = 0 }},
		{name: "provider", mutate: func(route *modelrouting.Route) { route.Provider = "" }},
		{name: "connection", mutate: func(route *modelrouting.Route) { route.ConnectionName = "" }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			events := []string{}
			fixture := newPublicRuntimeFixture(t, &events, 1)
			route := fixture.planner.plan.Routes[0]
			test.mutate(&route)
			fixture.planner.plan.Routes[0] = route

			err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
			if !errors.Is(err, errPublicNonStreamContractMismatch) {
				t.Fatalf("error=%v", err)
			}
			if got := strings.Join(events, ","); got != "guard:input,plan" || len(fixture.billing.freeze) != 0 ||
				len(fixture.billing.completions) != 0 || len(fixture.coordinator.attemptIDs) != 0 {
				t.Fatalf("static drift crossed early fence: events=%s freeze=%d completions=%d attempts=%d",
					got, len(fixture.billing.freeze), len(fixture.billing.completions), len(fixture.coordinator.attemptIDs))
			}
		})
	}
}

func TestPublicNonStreamOrchestratorHealthNonAppliedRetainsLiabilityAndStopsDelivery(t *testing.T) {
	events := []string{}
	fixture := newPublicRuntimeFixture(t, &events, 1)
	fixture.coordinator.healthStatus = modelexecution.Stale
	writer := &publicRuntimeWriter{events: &events}
	err := fixture.orchestrator.execute(t.Context(), writer, fixture.request)
	if !errors.Is(err, errPublicNonStreamHealthFailed) {
		t.Fatalf("error=%v", err)
	}
	want := "guard:input,plan,build:ciphertext-1,freeze,authorize:1,provider:provider-1.example,guard:output,complete:response_accepted,health:success,hold:provider_attempt_unresolved"
	if got := strings.Join(events, ","); got != want {
		t.Fatalf("order=%s\nwant=%s", got, want)
	}
	if len(fixture.billing.helds) != 1 || fixture.billing.helds[0].UsagePurpose != metering.UsagePurposeProviderEvidence ||
		len(fixture.billing.prepared) != 0 || len(fixture.billing.deliveries) != 0 ||
		len(fixture.billing.settled) != 0 || writer.body.Len() != 0 {
		t.Fatalf("health failure liability/delivery mismatch: held=%+v prepared=%d delivery=%d settle=%d body=%q",
			fixture.billing.helds, len(fixture.billing.prepared), len(fixture.billing.deliveries),
			len(fixture.billing.settled), writer.body.String())
	}
}

func TestPublicNonStreamOrchestratorUnknownLiabilityRemainsPendingWithoutCancelOrFallback(t *testing.T) {
	events := []string{}
	fixture := newPublicRuntimeFixture(t, &events, 2)
	fixture.orchestrator.doer = publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
		events = append(events, "provider:unknown")
		return nil, errors.New("private transport detail")
	})
	err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
	if !errors.Is(err, errPublicNonStreamUpstreamFailed) ||
		fixture.billing.cancelled != 0 || len(fixture.billing.completions) != 1 ||
		fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeUnknown ||
		fixture.billing.completions[0].FailureKind != "channel_failure" {
		t.Fatalf("unknown liability mishandled: err=%v cancel=%d completions=%+v",
			err, fixture.billing.cancelled, fixture.billing.completions)
	}
	want := "guard:input,plan,build:ciphertext-1,build:ciphertext-2,freeze,authorize:1,provider:unknown,complete:outcome_unknown,health:provider_failure"
	if got := strings.Join(events, ","); got != want || len(fixture.billing.prepared) != 0 ||
		len(fixture.billing.deliveries) != 0 || len(fixture.billing.settled) != 0 {
		t.Fatalf("unknown liability order=%s prepared=%d delivery=%d settle=%d",
			got, len(fixture.billing.prepared), len(fixture.billing.deliveries), len(fixture.billing.settled))
	}
}

func TestPublicNonStreamOrchestratorUnusableSuccessRemainsPendingWithoutCancelOrFallback(t *testing.T) {
	events := []string{}
	fixture := newPublicRuntimeFixture(t, &events, 2)
	fixture.providerStatuses["provider-1.example"] = http.StatusAccepted
	err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
	if !errors.Is(err, errPublicNonStreamUpstreamFailed) || fixture.billing.cancelled != 0 ||
		len(fixture.billing.completions) != 1 ||
		fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeResponseUnusable ||
		fixture.billing.completions[0].HTTPStatus != http.StatusAccepted ||
		fixture.billing.completions[0].FailureKind != "channel_failure" ||
		fixture.billing.completions[0].RetryAuthorized {
		t.Fatalf("unusable success mishandled: err=%v cancel=%d completions=%+v",
			err, fixture.billing.cancelled, fixture.billing.completions)
	}
	want := "guard:input,plan,build:ciphertext-1,build:ciphertext-2,freeze,authorize:1,provider:provider-1.example,complete:response_unusable,health:provider_failure"
	if got := strings.Join(events, ","); got != want || len(fixture.billing.prepared) != 0 ||
		len(fixture.billing.deliveries) != 0 || len(fixture.billing.settled) != 0 {
		t.Fatalf("unusable success order=%s prepared=%d delivery=%d settle=%d",
			got, len(fixture.billing.prepared), len(fixture.billing.deliveries), len(fixture.billing.settled))
	}
}

func TestPublicNonStreamOrchestratorUnpersistableUsageBecomesUnusableWithoutDeliveryOrCharge(t *testing.T) {
	events := []string{}
	fixture := newPublicRuntimeFixture(t, &events, 2)
	fixture.orchestrator.doer = publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
		events = append(events, "provider:"+request.URL.Host)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_1","object":"response","status":"completed","model":"provider-private-model",
			"error":null,"incomplete_details":null,
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"answer"}]}],
			"usage":{"input_tokens":3000000000,"output_tokens":0,"total_tokens":3000000000}
		}`))}, nil
	})
	err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
	if !errors.Is(err, errPublicNonStreamUpstreamFailed) || fixture.billing.cancelled != 0 ||
		len(fixture.billing.completions) != 1 ||
		fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeResponseUnusable ||
		len(fixture.billing.helds) != 0 || len(fixture.billing.prepared) != 0 ||
		len(fixture.billing.deliveries) != 0 || len(fixture.billing.settled) != 0 {
		t.Fatalf("unpersistable usage crossed charge/delivery: err=%v cancel=%d completions=%+v held=%d prepared=%d delivery=%d settle=%d",
			err, fixture.billing.cancelled, fixture.billing.completions, len(fixture.billing.helds),
			len(fixture.billing.prepared), len(fixture.billing.deliveries), len(fixture.billing.settled))
	}
	if got := strings.Join(events, ","); strings.Contains(got, "authorize:2") ||
		strings.Contains(got, "provider:provider-2.example") || !strings.HasSuffix(got, ",health:provider_failure") {
		t.Fatalf("unpersistable usage crossed fallback/health fence: %s", got)
	}
}

func TestPublicNonStreamOrchestratorRejectedHealthFailureCancelsWithoutFallback(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			events := []string{}
			fixture := newPublicRuntimeFixture(t, &events, 2)
			fixture.providerStatuses["provider-1.example"] = status
			fixture.coordinator.healthStatus = modelexecution.Stale
			err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
			if !errors.Is(err, errPublicNonStreamHealthFailed) || fixture.billing.cancelled != 1 ||
				len(fixture.billing.completions) != 1 ||
				fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeRejected ||
				len(fixture.billing.prepared) != 0 || len(fixture.billing.deliveries) != 0 ||
				len(fixture.billing.settled) != 0 {
				t.Fatalf("rejected health failure err=%v cancel=%d completions=%+v prepared=%d delivery=%d settle=%d",
					err, fixture.billing.cancelled, fixture.billing.completions, len(fixture.billing.prepared),
					len(fixture.billing.deliveries), len(fixture.billing.settled))
			}
			if got := strings.Join(events, ","); strings.Contains(got, "authorize:2") ||
				strings.Contains(got, "provider:provider-2.example") || !strings.HasSuffix(got, ",cancel") {
				t.Fatalf("health failure crossed fallback/cancel fence: %s", got)
			}
		})
	}
}

type publicRuntimeFixture struct {
	orchestrator     *publicNonStreamOrchestrator
	planner          *publicRuntimePlanner
	coordinator      *publicRuntimeCoordinator
	billing          *publicRuntimeBilling
	guard            *publicRuntimeGuard
	request          publicNonStreamRequest
	providerStatuses map[string]int
	providerBodies   map[string]string
}

func newPublicRuntimeFixture(t *testing.T, events *[]string, routeCount int) *publicRuntimeFixture {
	t.Helper()
	plan := publicRuntimePlan(t, routeCount)
	planner := &publicRuntimePlanner{events: events, plan: plan}
	coordinator := &publicRuntimeCoordinator{events: events}
	billingService := &publicRuntimeBilling{events: events}
	guard := &publicRuntimeGuard{events: events}
	statuses := make(map[string]int, routeCount)
	bodies := make(map[string]string, routeCount)
	for index := 1; index <= routeCount; index++ {
		statuses["provider-"+strconvItoa(index)+".example"] = http.StatusOK
	}
	doer := publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
		host := request.URL.Host
		*events = append(*events, "provider:"+host)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		bodies[host] = string(body)
		status := statuses[host]
		if status != http.StatusOK {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"error":"private"}`))}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(publicResponsesProviderBody("answer", ""))),
		}, nil
	})
	orchestrator := &publicNonStreamOrchestrator{
		mode: modelrouting.ModeCanonical, planner: planner, coordinator: coordinator,
		billing: billingService, registry: endpointprotocol.NewPublicNonStreamRegistry(),
		decrypt: func(ciphertext string, _ []byte) (string, error) {
			*events = append(*events, "build:"+ciphertext)
			return "provider-secret", nil
		},
		encryptionKey: []byte("test-key"), doer: doer, guard: guard,
		safetyFactor: "1.5", minFreeze: 10,
	}
	return &publicRuntimeFixture{
		orchestrator: orchestrator, planner: planner, coordinator: coordinator,
		billing: billingService, guard: guard,
		request: publicNonStreamRequest{
			Operation: modeloperation.IDResponses, Path: modeloperation.PathResponses,
			Body:        []byte(`{"model":"public-text","input":"hello","max_output_tokens":64}`),
			PublicModel: "public-text", ResolvedModel: "public-text",
			RequestID: "req-public-runtime", OrgID: 11,
			ProjectID: 12, APIKeyID: 13,
		},
		providerStatuses: statuses, providerBodies: bodies,
	}
}

func publicRuntimePlan(t *testing.T, routeCount int) modelrouting.Plan {
	t.Helper()
	identity, err := modelqualification.CompiledPublicBinding(string(modeloperation.IDResponses))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	capabilityHash, err := executionevidence.CapabilityDigest(nil)
	if err != nil {
		t.Fatal(err)
	}
	routes := make([]modelrouting.Route, 0, routeCount)
	for index := 1; index <= routeCount; index++ {
		base := executionevidence.EvidenceV1{
			ContractVersion:         executionevidence.ContractVersionV2,
			AuthorityMode:           executionevidence.ModeCanonical,
			ExecutionContractSource: executionevidence.ContractSourceCanonicalProfile,
			PublicModelID:           1, DeploymentID: int64(index + 1), ConnectionID: int64(index + 10),
			CredentialBindingID: int64(index + 20), ConnectionRevision: 1, DeploymentRevision: 1,
			CircuitRevision: 1, CredentialStateRevision: 1, CredentialLinkedAt: now,
			ModelOperationID: string(modeloperation.IDResponses), OperationRevision: 1,
			ProtocolProfileID: identity.ProfileID, ProtocolProfileVersion: identity.ProfileVersion,
			OperationEvidenceState: "production_approved", OperationEvidenceRef: "test:public-runtime",
			OperationVerifiedAt: &now, CapabilityEvidence: []executionevidence.CapabilityEntry{},
			CapabilityEvidenceHash: capabilityHash, CredentialProbeID: int64(index + 30),
			CredentialProbeTestedAt: now, RuntimeReleaseID: 51,
			RuntimeReleaseRef: "rcr_public_runtime", RuntimeStage: "active", SlotVersion: 1,
			SnapshotHash: "mrs1_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		}
		public, err := executionevidence.NormalizeV2(executionevidence.EvidenceV2{
			EvidenceV1: base, PublicExecutionBindingID: int64(100 + index),
			DeliveryContractVersion:    identity.DeliveryContractVersion,
			ChargeContractVersion:      identity.ChargeContractVersion,
			IdempotencyContractVersion: identity.IdempotencyContractVersion,
		})
		if err != nil {
			t.Fatal(err)
		}
		routes = append(routes, modelrouting.Route{
			ConnectionName: "connection-" + strconvItoa(index), Provider: "fixture",
			BaseURL: "https://provider-" + strconvItoa(index) + ".example", AdapterType: "unused-public",
			ProtocolProfileID: identity.ProfileID, ProtocolProfileVersion: identity.ProfileVersion,
			PublicPath: identity.PublicPath, ExecutorContractVersion: identity.ExecutorContractVersion,
			ProductionApprovalID: int64(200 + index), UpstreamModel: "provider-private-model",
			MaxContext: 128000, DefaultMaxOutput: 8192, EncryptedSecret: "ciphertext-" + strconvItoa(index),
			Priority: index, Weight: 100, PublicEvidence: &public,
		})
	}
	return modelrouting.Plan{
		AuthorityMode: modelrouting.ModeCanonical, PublicModelID: 1, PublicModel: "public-text",
		ModelOperationID: string(modeloperation.IDResponses), PublicContextLimit: 128000, PublicOutputLimit: 8192,
		SnapshotHash:     "mrs1_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		RuntimeReleaseID: 51, RuntimeReleaseRef: "rcr_public_runtime", RuntimeStage: "active", SlotVersion: 1,
		Routes: routes,
	}
}

func strconvItoa(value int) string { return strconv.Itoa(value) }
