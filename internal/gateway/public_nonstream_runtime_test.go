package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/metering"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/policybilling"
)

func TestNativeInlineJPEGUsesTypedCapabilityAndAdmissionBudget(t *testing.T) {
	rawBundle, err := os.ReadFile("../../capability-bundles/native-responses-messages-client-tools.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(rawBundle)
	require.NoError(t, err)
	image := testJPEGDataURL(t, 32, 32)
	for _, raw := range bundle.Definitions {
		definition, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		old := definition.Profile()
		t.Run(old.ID, func(t *testing.T) {
			profile := old
			profile.ID += ".inline-jpeg"
			profile.Capabilities = append(slices.Clone(old.Capabilities), modelprofile.CapabilityVision)
			identity := endpointprotocol.PublicContractIdentity{ProfileVersion: "1.0.0",
				ChargeContractVersion: endpointprotocol.PublicCustomerChargeContractV1, IdempotencyContractVersion: endpointprotocol.PublicIdempotencyContractV1}
			var input []byte
			if old.APISurface == modelprofile.APISurfaceOpenAIResponses {
				identity.ModelOperationID, identity.PublicPath = string(modeloperation.IDResponses), modeloperation.PathResponses
				identity.ProfileID = endpointprotocol.ProfileOpenAIResponsesInlineJPEG
				identity.ExecutorContractVersion = endpointprotocol.PublicResponsesInlineJPEGExecutorV1
				identity.DeliveryContractVersion = endpointprotocol.PublicResponsesToolsDeliveryV1
				input = []byte(fmt.Sprintf(`{"model":"project-vision","max_output_tokens":64,"input":[{"role":"user","content":[{"type":"input_text","text":"count the blocks"},{"type":"input_image","image_url":%q}]}]}`, image))
			} else {
				identity.ModelOperationID, identity.PublicPath = string(modeloperation.IDMessages), modeloperation.PathMessages
				identity.ProfileID = endpointprotocol.ProfileAnthropicMessagesInlineJPEG
				identity.ExecutorContractVersion = endpointprotocol.PublicMessagesInlineJPEGExecutorV1
				identity.DeliveryContractVersion = endpointprotocol.PublicAnthropicMessagesToolsDeliveryV1
				_, data, ok := strings.Cut(image, ",")
				require.True(t, ok)
				input = []byte(fmt.Sprintf(`{"model":"project-vision","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"count the blocks"},{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":%q}}]}]}`, data))
			}
			profile.ProtocolProfile = identity.ProfileID
			contract, err := publicRequestContractForProfile(profile, identity.ModelOperationID, identity)
			require.NoError(t, err)
			codec, err := endpointprotocol.NewPublicNonStreamRegistry().Resolve(identity)
			require.NoError(t, err)
			prepared, err := codec.PrepareRequest(input, "project-vision", contract)
			require.NoError(t, err)
			requirements, err := publicInvocationCapabilityRequirements(profile, prepared)
			require.NoError(t, err)
			require.ElementsMatch(t, []modelrouting.CapabilityRequirement{
				{CapabilityID: modelprofile.CapabilityVision, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
				{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
			}, requirements)
			_, err = publicInvocationCapabilityRequirements(old, prepared)
			require.ErrorContains(t, err, "inline JPEG capability is not supported")
			body := prepared.CanonicalBody()
			require.True(t, json.Valid(body))
			plain, err := estimateProviderJSONAdmission(body)
			require.NoError(t, err)
			bound, err := estimatePublicRequestAdmission(body, prepared.Admission().InlineImagePixels)
			require.NoError(t, err)
			require.Equal(t, plain.InputTokens+4*32*32, bound.InputTokens)
		})
	}
}

func TestPublicNonStreamSingleUpstreamAttemptUsesOnePhysicalSend(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		single  bool
		unknown bool
		want    int
	}{
		{name: "default can switch after known rejection", want: 2},
		{name: "opt-in stops after known rejection", single: true, want: 1},
		{name: "opt-in stops on unknown outcome", single: true, unknown: true, want: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count := calls.Add(1)
				_, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				if scenario.single {
					require.Equal(t, 1, r.ProtoMajor)
				}
				if count == 1 && scenario.unknown {
					conn, _, err := w.(http.Hijacker).Hijack()
					require.NoError(t, err)
					require.NoError(t, conn.Close())
					return
				}
				if count == 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, err = io.WriteString(w, publicResponsesProviderBody("accepted second route", ""))
				require.NoError(t, err)
			}))
			upstream.EnableHTTP2 = true
			upstream.StartTLS()
			t.Cleanup(upstream.Close)
			var events []string
			fixture := newPublicRuntimeFixture(t, &events, 2)
			for i := range fixture.planner.plan.Routes {
				fixture.planner.plan.Routes[i].BaseURL = upstream.URL
			}
			client := PublicNonStreamHTTPDoer(upstream.Client())
			if scenario.single {
				client = newSingleSendProviderClient(upstream.Client().Transport, time.Second)
			}
			fixture.orchestrator.doer = publicNonStreamDoerFunc(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, !scenario.single, r.GetBody != nil)
				return client.Do(r)
			})
			fixture.request.SingleUpstreamAttempt = scenario.single
			err := fixture.orchestrator.execute(t.Context(), httptest.NewRecorder(), fixture.request)
			if scenario.single {
				require.ErrorIs(t, err, errPublicNonStreamUpstreamFailed)
			} else {
				require.NoError(t, err)
			}
			require.EqualValues(t, scenario.want, calls.Load())
			require.Len(t, fixture.coordinator.attemptIDs, scenario.want)
		})
	}
}

func TestNativeVisionSameReleaseKeepsTextAndToolContinuation(t *testing.T) {
	rawBundle, err := os.ReadFile("../../capability-bundles/native-responses-messages-client-tools.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(rawBundle)
	require.NoError(t, err)
	readFixture := func(t *testing.T, id string) (string, string) {
		t.Helper()
		wire, err := os.ReadFile(filepath.Join("../../internal/endpointprotocol/artifacts/fixtures", id+".json"))
		require.NoError(t, err)
		var fixture struct {
			Request  struct{ Body string }
			Response struct{ Body string }
		}
		require.NoError(t, json.Unmarshal(wire, &fixture))
		return fixture.Request.Body, fixture.Response.Body
	}
	for _, raw := range bundle.Definitions {
		source, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		profile := source.Profile()
		t.Run(profile.ID, func(t *testing.T) {
			responses := profile.APISurface == modelprofile.APISurfaceOpenAIResponses
			operation, path, requestKey, outputKey := modeloperation.IDResponses, modeloperation.PathResponses, "input", "max_output_tokens"
			imageFixture, toolsFixture := "openai-responses-inline-jpeg.v1", "openai-responses-text-tools.v3"
			if !responses {
				operation, path, requestKey, outputKey = modeloperation.IDMessages, modeloperation.PathMessages, "messages", "max_tokens"
				imageFixture, toolsFixture = "anthropic-messages-inline-jpeg.v1", "anthropic-messages-text-tools.v3"
			}
			profile.ProtocolProfile = endpointprotocol.ProfileOpenAIResponsesInlineJPEG
			if !responses {
				profile.ProtocolProfile = endpointprotocol.ProfileAnthropicMessagesInlineJPEG
			}
			profile.Version = "1.1.0"
			profile.Capabilities = append(slices.Clone(profile.Capabilities), modelprofile.CapabilityVision)
			profile.ProviderCapabilities = append(slices.Clone(profile.ProviderCapabilities), modelprofile.CapabilityVision)
			definition, err := modelprofile.NewDefinition(profile, source.ProductLineageID(), source.Digest())
			require.NoError(t, err)
			var events []string
			fixture := newPublicRuntimeFixture(t, &events, 1)
			p := publicRuntimePlanForExecutionProfile(t, 1, profile, operation)
			p.Definition = definition
			p.Routes[0].UpstreamModel = profile.UpstreamModelIDs[0]
			fixture.planner.plan = p
			fixture.request.Operation, fixture.request.Path = operation, path
			fixture.request.PublicModel, fixture.request.ResolvedModel = p.PublicModel, p.PublicModel
			imageRequest, answer := readFixture(t, imageFixture)
			toolRequest, toolReply := readFixture(t, toolsFixture)
			bindResponse := func(wire string) string {
				var response map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(wire), &response))
				response["model"], _ = json.Marshal(p.Routes[0].UpstreamModel)
				out, err := json.Marshal(response)
				require.NoError(t, err)
				return string(out)
			}
			var providerCalls int
			var expectedReply string
			fixture.orchestrator.doer = publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
				providerCalls++
				require.Equal(t, path, request.URL.Path)
				var body map[string]json.RawMessage
				require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				require.Equal(t, `"`+p.Routes[0].UpstreamModel+`"`, string(body["model"]))
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(expectedReply))}, nil
			})
			run := func(body string, reply string) []byte {
				t.Helper()
				var envelope map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(body), &envelope))
				envelope["model"], _ = json.Marshal(p.PublicModel)
				envelope[outputKey] = json.RawMessage("64")
				wire, err := json.Marshal(envelope)
				require.NoError(t, err)
				fixture.request.Body = wire
				fixture.request.RequestID = fmt.Sprintf("fixture-native-vision-%d", providerCalls+1)
				expectedReply = bindResponse(reply)
				w := httptest.NewRecorder()
				require.NoError(t, fixture.orchestrator.execute(t.Context(), w, fixture.request))
				require.Equal(t, http.StatusOK, w.Code)
				return w.Body.Bytes()
			}
			text := `{"input":"Give a short answer."}`
			if !responses {
				text = `{"messages":[{"role":"user","content":"Give a short answer."}]}`
			}
			run(text, answer)
			toolDelivery := run(toolRequest, toolReply)
			var delivery map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(toolDelivery, &delivery))
			var continuation map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(toolRequest), &continuation))
			if responses {
				var history []json.RawMessage
				require.NoError(t, json.Unmarshal(delivery["output"], &history))
				var callID string
				for _, item := range history {
					var fields map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(item, &fields))
					if string(fields["type"]) == `"function_call"` {
						require.NoError(t, json.Unmarshal(fields["call_id"], &callID))
					}
				}
				require.NotEmpty(t, callID)
				var originalInput string
				require.NoError(t, json.Unmarshal(continuation[requestKey], &originalInput))
				firstTurn, err := json.Marshal(map[string]string{"type": "message", "role": "user", "content": originalInput})
				require.NoError(t, err)
				history = append([]json.RawMessage{firstTurn}, history...)
				result, err := json.Marshal(map[string]string{"type": "function_call_output", "call_id": callID, "output": "local fixture result"})
				require.NoError(t, err)
				continuation[requestKey], err = json.Marshal(append(history, result))
				require.NoError(t, err)
			} else {
				var history []json.RawMessage
				require.NoError(t, json.Unmarshal(continuation[requestKey], &history))
				history = append(history, json.RawMessage(`{"role":"assistant","content":`+string(delivery["content"])+`}`),
					json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_fixture","content":"local fixture result"}]}`))
				continuation[requestKey], err = json.Marshal(history)
				require.NoError(t, err)
			}
			second, err := json.Marshal(continuation)
			require.NoError(t, err)
			run(string(second), answer)
			run(imageRequest, answer)
			require.Equal(t, 4, providerCalls)

			var mixed map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(imageRequest), &mixed))
			mixed["model"], _ = json.Marshal(p.PublicModel)
			mixed["tools"] = continuation["tools"]
			fixture.request.Body, err = json.Marshal(mixed)
			require.NoError(t, err)
			err = fixture.orchestrator.execute(t.Context(), httptest.NewRecorder(), fixture.request)
			require.ErrorContains(t, err, "tools_with_inline_jpeg_unsupported")
			require.Equal(t, 4, providerCalls)
		})
	}
}

type publicRuntimePlanner struct {
	events       *[]string
	plan         modelrouting.Plan
	requirements []modelrouting.CapabilityRequirement
}

func (planner *publicRuntimePlanner) Plan(_ context.Context, request modelrouting.Request) (modelrouting.Plan, error) {
	*planner.events = append(*planner.events, "plan")
	expectedPublicModel := planner.plan.PublicModel
	if expectedPublicModel == "" {
		expectedPublicModel = "public-text"
	}
	expectedOperation := planner.plan.ModelOperationID
	if expectedOperation == "" {
		expectedOperation = string(modeloperation.IDResponses)
	}
	if request.Mode != modelrouting.ModeCanonical || request.CustomerOrgID != 11 ||
		request.PublicModel != expectedPublicModel || request.ModelOperationID != expectedOperation {
		return modelrouting.Plan{}, errors.New("unexpected public plan request")
	}
	plan := planner.plan
	if request.DeriveInvocationCapabilities != nil {
		profile, ok := modelprofile.Lookup(plan.ExecutionProfileID, plan.ExecutionProfileVersion)
		if plan.Definition.Digest() != "" {
			profile, ok = plan.Definition.Profile(), true
		}
		if !ok {
			return modelrouting.Plan{}, errors.New("public planner fixture profile is unavailable")
		}
		requirements, err := request.DeriveInvocationCapabilities(profile, plan.Definition)
		if err != nil {
			return modelrouting.Plan{}, err
		}
		slices.SortFunc(requirements, func(left, right modelrouting.CapabilityRequirement) int {
			return strings.Compare(left.CapabilityID, right.CapabilityID)
		})
		plan.CapabilityRequirements = requirements
		planner.requirements = slices.Clone(requirements)
		plan.RequiredCapabilities = make([]string, len(requirements))
		for index := range requirements {
			plan.RequiredCapabilities[index] = requirements[index].CapabilityID
		}
	}
	return plan, nil
}

type publicRuntimeCoordinator struct {
	events       *[]string
	authorize    []modelexecution.Status
	authorizeErr error
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
	}, coordinator.authorizeErr
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
	events          *[]string
	freeze          []billing.FreezeRequest
	freezeResult    *billing.FreezeResult
	completions     []billing.ProviderAttemptCompletion
	prepared        []billing.SettleRequest
	helds           []billing.SettleRequest
	holdReasons     []string
	deliveries      []billing.DeliveryEvent
	settled         []billing.SettleRequest
	cancelled       int
	prepareError    error
	cancelError     error
	completionError error
}

func (service *publicRuntimeBilling) Freeze(_ context.Context, request billing.FreezeRequest) (*billing.FreezeResult, error) {
	*service.events = append(*service.events, "freeze")
	service.freeze = append(service.freeze, request)
	if service.freezeResult != nil {
		return service.freezeResult, nil
	}
	return &billing.FreezeResult{Success: true}, nil
}

func (service *publicRuntimeBilling) CompleteProviderAttempt(_ context.Context, request billing.ProviderAttemptCompletion) error {
	*service.events = append(*service.events, "complete:"+request.Outcome)
	service.completions = append(service.completions, request)
	return service.completionError
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
	want := "plan,guard:input,build:ciphertext-1,build:ciphertext-2,freeze,authorize:1,provider:provider-1.example,guard:output,complete:response_accepted,health:success,prepare_delivery,delivery:write_started,customer_write,delivery:completed,settle"
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

func TestPublicResponsesReasoningEffortIsBoundToExactExecutionProfile(t *testing.T) {
	events := []string{}
	fixture := newPublicRuntimeFixture(t, &events, 1)
	fixture.request.Body = []byte(`{
		"model":"public-text","input":"hello","max_output_tokens":64,
		"reasoning":{"effort":"xhigh"}
	}`)
	fixture.planner.plan.ExecutionProfileVersion = "1.2.0"
	fixture.planner.plan.RequiredCapabilities = []string{modelprofile.CapabilityReasoningEffort}
	writer := &publicRuntimeWriter{events: &events}
	if err := fixture.orchestrator.execute(t.Context(), writer, fixture.request); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fixture.providerBodies["provider-1.example"], `"reasoning":{"effort":"xhigh"}`) {
		t.Fatalf("reasoning effort was not preserved: %s", fixture.providerBodies["provider-1.example"])
	}

	events = nil
	terraCurrent := newPublicRuntimeFixture(t, &events, 1)
	terraCurrent.request.Body = fixture.request.Body
	terraCurrent.planner.plan.ExecutionProfileID = "openai.gpt-5.6-terra.responses"
	terraCurrent.planner.plan.ExecutionProfileVersion = "1.2.0"
	terraCurrent.planner.plan.RequiredCapabilities = []string{modelprofile.CapabilityReasoningEffort}
	if err := terraCurrent.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, terraCurrent.request); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(terraCurrent.providerBodies["provider-1.example"], `"reasoning":{"effort":"xhigh"}`) {
		t.Fatalf("Terra reasoning effort was not preserved: %s", terraCurrent.providerBodies["provider-1.example"])
	}

	events = nil
	lunaCurrent := newPublicRuntimeFixture(t, &events, 1)
	lunaCurrent.request.Body = fixture.request.Body
	lunaCurrent.planner.plan.ExecutionProfileID = "openai.gpt-5.6-luna.responses"
	lunaCurrent.planner.plan.ExecutionProfileVersion = "1.2.0"
	lunaCurrent.planner.plan.RequiredCapabilities = []string{modelprofile.CapabilityReasoningEffort}
	if err := lunaCurrent.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, lunaCurrent.request); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lunaCurrent.providerBodies["provider-1.example"], `"reasoning":{"effort":"xhigh"}`) {
		t.Fatalf("Luna reasoning effort was not preserved: %s", lunaCurrent.providerBodies["provider-1.example"])
	}

	events = nil
	core := newPublicRuntimeFixture(t, &events, 1)
	core.request.Body = fixture.request.Body
	err := core.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, core.request)
	if !errors.Is(err, errPublicNonStreamRequestInvalid) || strings.Join(events, ",") != "plan" ||
		len(core.billing.freeze) != 0 {
		t.Fatalf("core profile crossed reasoning fence: err=%v events=%v freeze=%d", err, events, len(core.billing.freeze))
	}

	for _, profileID := range []string{"openai.gpt-5.6-terra.responses", "openai.gpt-5.6-luna.responses"} {
		t.Run(profileID, func(t *testing.T) {
			events = nil
			coreSibling := newPublicRuntimeFixture(t, &events, 1)
			coreSibling.request.Body = fixture.request.Body
			coreSibling.planner.plan.ExecutionProfileID = profileID
			err = coreSibling.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, coreSibling.request)
			if !errors.Is(err, errPublicNonStreamRequestInvalid) || strings.Join(events, ",") != "plan" ||
				len(coreSibling.billing.freeze) != 0 {
				t.Fatalf("core sibling crossed Sol reasoning fence: err=%v events=%v freeze=%d", err, events, len(coreSibling.billing.freeze))
			}
		})
	}
}

func TestPublicClaudeSonnet5ContractDerivesProviderDefaultReasoningAndStrictMessagesPolicy(t *testing.T) {
	profile, ok := modelprofile.Lookup("anthropic.claude-sonnet-5.api", "1.1.0")
	if !ok {
		t.Fatal("Claude Sonnet 5 exact Profile unavailable")
	}
	identity, err := modelqualification.CompiledPublicBinding(modelprofile.OperationAnthropicMessage)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := publicRequestContractForProfile(profile, modelprofile.OperationAnthropicMessage, identity)
	if err != nil {
		t.Fatal(err)
	}
	if contract.AnthropicMessages == nil || !contract.AnthropicMessages.RequireFinalUserMessage ||
		!contract.AnthropicMessages.RefusalStopDetails ||
		contract.AnthropicMessages.ResponseInferenceGeo || contract.AnthropicMessages.ResponseServiceTier ||
		!slices.Equal(contract.AnthropicMessages.ForbiddenRootFields, []string{"temperature", "top_p"}) ||
		!slices.Contains(contract.AnthropicMessages.AllowedResponseStops, "refusal") {
		t.Fatalf("contract=%+v", contract)
	}
	body := []byte(`{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
	publicProfile, err := endpointprotocol.NewPublicNonStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := publicProfile.PrepareRequest(body, "public-claude", contract)
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := publicInvocationCapabilityRequirements(profile, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 1 || requirements[0].CapabilityID != modelprofile.CapabilityReasoning ||
		!slices.Equal(requirements[0].Origins, []string{modelrouting.CapabilityOriginProviderDefault}) {
		t.Fatalf("requirements=%+v", requirements)
	}
	if _, err := publicProfile.PrepareRequest(
		[]byte(`{"model":"public-claude","max_tokens":64,"temperature":0.4,"messages":[{"role":"user","content":"hello"}]}`),
		"public-claude", contract,
	); err == nil {
		t.Fatal("Sonnet 5 sampling override crossed the exact public contract")
	}
}

func TestPublicClaudeSonnet5ControlsDeriveOriginsFromTypedPreparation(t *testing.T) {
	profile, ok := modelprofile.Lookup("anthropic.claude-sonnet-5.api", "1.2.0")
	if !ok {
		t.Fatal("Claude Sonnet 5 controls Profile unavailable")
	}
	identity, err := modelqualification.CompiledPublicBinding(modelprofile.OperationAnthropicMessage)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := publicRequestContractForProfile(profile, modelprofile.OperationAnthropicMessage, identity)
	if err != nil {
		t.Fatal(err)
	}
	if contract.AnthropicMessages == nil || contract.AnthropicMessages.Reasoning == nil ||
		!contract.AnthropicMessages.ResponseInferenceGeo || !contract.AnthropicMessages.ResponseServiceTier {
		t.Fatalf("contract=%+v", contract)
	}
	reasoning := contract.AnthropicMessages.Reasoning
	if reasoning.ThinkingObjectField != "thinking" || reasoning.OutputConfigObjectField != "output_config" ||
		!slices.Equal(reasoning.AllowedThinkingTypes, []string{"adaptive", "disabled"}) ||
		!slices.Equal(reasoning.AllowedEfforts, []string{"low", "medium", "high", "xhigh", "max"}) ||
		reasoning.ThinkingProviderDefault != "adaptive" || reasoning.EffortProviderDefault != "high" {
		t.Fatalf("reasoning=%+v", reasoning)
	}
	publicProfile, err := endpointprotocol.NewPublicNonStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name            string
		body            string
		reasoningOrigin string
		effortOrigin    string
	}{
		{
			name:            "provider defaults",
			body:            `{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`,
			reasoningOrigin: modelrouting.CapabilityOriginProviderDefault,
			effortOrigin:    modelrouting.CapabilityOriginProviderDefault,
		},
		{
			name:            "explicit controls",
			body:            `{"model":"public-claude","max_tokens":64,"thinking":{"type":"disabled"},"output_config":{"effort":"max"},"messages":[{"role":"user","content":"hello"}]}`,
			reasoningOrigin: modelrouting.CapabilityOriginExplicitRequest,
			effortOrigin:    modelrouting.CapabilityOriginExplicitRequest,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared, prepareErr := publicProfile.PrepareRequest([]byte(test.body), "public-claude", contract)
			if prepareErr != nil {
				t.Fatal(prepareErr)
			}
			requirements, deriveErr := publicInvocationCapabilityRequirements(profile, prepared)
			if deriveErr != nil {
				t.Fatal(deriveErr)
			}
			if len(requirements) != 2 || requirements[0].CapabilityID != modelprofile.CapabilityReasoning ||
				requirements[0].Origins[0] != test.reasoningOrigin ||
				requirements[1].CapabilityID != modelprofile.CapabilityReasoningEffort ||
				requirements[1].Origins[0] != test.effortOrigin {
				t.Fatalf("requirements=%+v", requirements)
			}
		})
	}
}

func TestPublicClaudeOpus5ControlsModelDisabledThinkingEffortConstraint(t *testing.T) {
	profile, ok := modelprofile.Lookup("anthropic.claude-opus-5.api", "1.1.0")
	if !ok {
		t.Fatal("Claude Opus 5 controls Profile unavailable")
	}
	identity, err := modelqualification.CompiledPublicBinding(modelprofile.OperationAnthropicMessage)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := publicRequestContractForProfile(profile, modelprofile.OperationAnthropicMessage, identity)
	if err != nil {
		t.Fatal(err)
	}
	if contract.AnthropicMessages == nil || contract.AnthropicMessages.Reasoning == nil ||
		!contract.AnthropicMessages.ResponseInferenceGeo || !contract.AnthropicMessages.ResponseServiceTier ||
		!slices.Equal(contract.AnthropicMessages.Reasoning.DisabledThinkingTypes, []string{"disabled"}) ||
		!slices.Equal(contract.AnthropicMessages.Reasoning.EffortsAllowedDisabled, []string{"low", "medium", "high"}) {
		t.Fatalf("contract=%+v", contract)
	}
	publicProfile, err := endpointprotocol.NewPublicNonStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := publicProfile.PrepareRequest(
		[]byte(`{"model":"public-claude","max_tokens":64,"thinking":{"type":"disabled"},"output_config":{"effort":"high"},"messages":[{"role":"user","content":"hello"}]}`),
		"public-claude", contract,
	)
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := publicInvocationCapabilityRequirements(profile, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 2 || requirements[0].Origins[0] != modelrouting.CapabilityOriginExplicitRequest ||
		requirements[1].Origins[0] != modelrouting.CapabilityOriginExplicitRequest {
		t.Fatalf("requirements=%+v", requirements)
	}

	for _, effort := range []string{"xhigh", "max"} {
		t.Run("disabled "+effort+" fails before financial or provider authority", func(t *testing.T) {
			events := []string{}
			fixture := newPublicRuntimeFixture(t, &events, 1)
			fixture.orchestrator.planner = &publicRuntimePlanner{events: &events, plan: modelrouting.Plan{
				AuthorityMode: modelrouting.ModeCanonical,
				PublicModel:   "public-claude", ModelOperationID: string(modeloperation.IDMessages),
				ExecutionProfileID: "anthropic.claude-opus-5.api", ExecutionProfileVersion: "1.1.0",
			}}
			fixture.request.Operation = modeloperation.IDMessages
			fixture.request.Path = modeloperation.PathMessages
			fixture.request.Body = []byte(`{"model":"public-claude","max_tokens":64,"thinking":{"type":"disabled"},"output_config":{"effort":"` + effort + `"},"messages":[{"role":"user","content":"hello"}]}`)
			fixture.request.PublicModel = "public-claude"
			fixture.request.ResolvedModel = "public-claude"
			err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
			if !errors.Is(err, errPublicNonStreamRequestInvalid) || strings.Join(events, ",") != "plan" ||
				len(fixture.billing.freeze) != 0 || len(fixture.coordinator.attemptIDs) != 0 || len(fixture.providerBodies) != 0 {
				t.Fatalf("invalid Opus combination crossed pre-dispatch fence: err=%v events=%v freeze=%d attempts=%d providerBodies=%v",
					err, events, len(fixture.billing.freeze), len(fixture.coordinator.attemptIDs), fixture.providerBodies)
			}
		})
	}
}

func TestPublicClaudeHaiku45ThinkingBudgetContractDerivesExactInvocationCapabilities(t *testing.T) {
	profile, ok := modelprofile.Lookup("anthropic.claude-haiku-4.5.api", "1.1.0")
	if !ok {
		t.Fatal("Claude Haiku 4.5 thinking-budget Profile unavailable")
	}
	identity, err := modelqualification.CompiledPublicBinding(modelprofile.OperationAnthropicMessage)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := publicRequestContractForProfile(profile, modelprofile.OperationAnthropicMessage, identity)
	if err != nil {
		t.Fatal(err)
	}
	if contract.AnthropicMessages == nil || contract.AnthropicMessages.Reasoning == nil ||
		!contract.AnthropicMessages.ResponseInferenceGeo || !contract.AnthropicMessages.ResponseServiceTier ||
		!slices.Equal(contract.AnthropicMessages.ForbiddenRootFields, []string{"temperature", "top_p"}) {
		t.Fatalf("contract=%+v", contract)
	}
	reasoning := contract.AnthropicMessages.Reasoning
	if !slices.Equal(reasoning.AllowedThinkingTypes, []string{"enabled", "disabled"}) ||
		reasoning.ThinkingProviderDefault != "disabled" || reasoning.BudgetField != "budget_tokens" ||
		reasoning.BudgetMinimum != 1_024 || reasoning.BudgetMaximum != 63_999 ||
		!slices.Equal(reasoning.BudgetRequiredTypes, []string{"enabled"}) ||
		reasoning.ResponseThinkingTextMode != modelprofile.ReasoningResponseThinkingSummarized {
		t.Fatalf("reasoning=%+v", reasoning)
	}
	publicProfile, err := endpointprotocol.NewPublicNonStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		body        string
		wantOrigins []string
		wantCaps    []string
		wantBudget  int64
	}{
		{
			name:        "provider default disabled",
			body:        `{"model":"public-claude","max_tokens":4096,"messages":[{"role":"user","content":"hello"}]}`,
			wantCaps:    []string{modelprofile.CapabilityReasoning},
			wantOrigins: []string{modelrouting.CapabilityOriginProviderDefault},
		},
		{
			name:        "explicit enabled budget",
			body:        `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"hello"}]}`,
			wantCaps:    []string{modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningBudget},
			wantOrigins: []string{modelrouting.CapabilityOriginExplicitRequest, modelrouting.CapabilityOriginExplicitRequest},
			wantBudget:  1024,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared, prepareErr := publicProfile.PrepareRequest([]byte(test.body), "public-claude", contract)
			if prepareErr != nil {
				t.Fatal(prepareErr)
			}
			if prepared.InvocationSemantics().ReasoningBudgetEffective != test.wantBudget {
				t.Fatalf("semantics=%+v", prepared.InvocationSemantics())
			}
			requirements, deriveErr := publicInvocationCapabilityRequirements(profile, prepared)
			if deriveErr != nil {
				t.Fatal(deriveErr)
			}
			if len(requirements) != len(test.wantCaps) {
				t.Fatalf("requirements=%+v", requirements)
			}
			for index := range requirements {
				if requirements[index].CapabilityID != test.wantCaps[index] ||
					!slices.Equal(requirements[index].Origins, []string{test.wantOrigins[index]}) {
					t.Fatalf("requirements=%+v", requirements)
				}
			}
		})
	}
}

func TestPublicClaudeHaiku45InvalidThinkingBudgetsFailBeforeFreezeCredentialAndDispatch(t *testing.T) {
	for name, body := range map[string]string{
		"missing budget":            `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hello"}]}`,
		"budget below minimum":      `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":1023},"messages":[{"role":"user","content":"hello"}]}`,
		"budget reaches max tokens": `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":4096},"messages":[{"role":"user","content":"hello"}]}`,
		"adaptive thinking":         `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"adaptive"},"messages":[{"role":"user","content":"hello"}]}`,
		"effort":                    `{"model":"public-claude","max_tokens":4096,"output_config":{"effort":"high"},"messages":[{"role":"user","content":"hello"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			events := []string{}
			fixture := newPublicRuntimeFixture(t, &events, 1)
			fixture.orchestrator.planner = &publicRuntimePlanner{events: &events, plan: modelrouting.Plan{
				AuthorityMode: modelrouting.ModeCanonical,
				PublicModel:   "public-claude", ModelOperationID: string(modeloperation.IDMessages),
				ExecutionProfileID: "anthropic.claude-haiku-4.5.api", ExecutionProfileVersion: "1.1.0",
			}}
			fixture.request.Operation = modeloperation.IDMessages
			fixture.request.Path = modeloperation.PathMessages
			fixture.request.Body = []byte(body)
			fixture.request.PublicModel = "public-claude"
			fixture.request.ResolvedModel = "public-claude"
			err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
			if !errors.Is(err, errPublicNonStreamRequestInvalid) || strings.Join(events, ",") != "plan" ||
				len(fixture.billing.freeze) != 0 || len(fixture.coordinator.attemptIDs) != 0 || len(fixture.providerBodies) != 0 {
				t.Fatalf("invalid Haiku request crossed pre-dispatch fence: err=%v events=%v freeze=%d attempts=%d providerBodies=%v",
					err, events, len(fixture.billing.freeze), len(fixture.coordinator.attemptIDs), fixture.providerBodies)
			}
		})
	}
}

func TestPublicClaudeSonnet5InvalidControlsFailBeforeFreezeCredentialAndDispatch(t *testing.T) {
	for name, body := range map[string]string{
		"null thinking":      `{"model":"public-claude","max_tokens":64,"thinking":null,"messages":[{"role":"user","content":"hello"}]}`,
		"unknown thinking":   `{"model":"public-claude","max_tokens":64,"thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hello"}]}`,
		"thinking display":   `{"model":"public-claude","max_tokens":64,"thinking":{"type":"adaptive","display":"summarized"},"messages":[{"role":"user","content":"hello"}]}`,
		"null output config": `{"model":"public-claude","max_tokens":64,"output_config":null,"messages":[{"role":"user","content":"hello"}]}`,
		"unknown effort":     `{"model":"public-claude","max_tokens":64,"output_config":{"effort":"ultra"},"messages":[{"role":"user","content":"hello"}]}`,
		"output format":      `{"model":"public-claude","max_tokens":64,"output_config":{"effort":"high","format":{"type":"json_schema"}},"messages":[{"role":"user","content":"hello"}]}`,
		"sampling":           `{"model":"public-claude","max_tokens":64,"temperature":0.5,"messages":[{"role":"user","content":"hello"}]}`,
		"assistant prefill":  `{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"prefix"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			events := []string{}
			fixture := newPublicRuntimeFixture(t, &events, 1)
			plan := modelrouting.Plan{
				AuthorityMode: modelrouting.ModeCanonical,
				PublicModel:   "public-claude", ModelOperationID: string(modeloperation.IDMessages),
				ExecutionProfileID: "anthropic.claude-sonnet-5.api", ExecutionProfileVersion: "1.2.0",
			}
			planner := &publicRuntimePlanner{events: &events, plan: plan}
			fixture.orchestrator.planner = planner
			fixture.request.Operation = modeloperation.IDMessages
			fixture.request.Path = modeloperation.PathMessages
			fixture.request.Body = []byte(body)
			fixture.request.PublicModel = "public-claude"
			fixture.request.ResolvedModel = "public-claude"
			writer := &publicRuntimeWriter{events: &events}
			err := fixture.orchestrator.execute(t.Context(), writer, fixture.request)
			if !errors.Is(err, errPublicNonStreamRequestInvalid) || strings.Join(events, ",") != "plan" ||
				len(fixture.billing.freeze) != 0 || len(fixture.coordinator.attemptIDs) != 0 ||
				len(fixture.providerBodies) != 0 {
				t.Fatalf("invalid request crossed pre-dispatch fence: err=%v events=%v freeze=%d attempts=%d providerBodies=%v",
					err, events, len(fixture.billing.freeze), len(fixture.coordinator.attemptIDs), fixture.providerBodies)
			}
		})
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
			want := "plan,guard:input,build:ciphertext-1,freeze,authorize:1,provider:provider-1.example,guard:output,complete:response_accepted,health:success,hold:guardrail_withheld"
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
	if got := strings.Join(events, ","); got != "plan" || len(fixture.billing.freeze) != 0 ||
		len(fixture.billing.completions) != 0 {
		t.Fatalf("M75 route crossed exact fence: events=%s freeze=%d attempts=%d",
			got, len(fixture.billing.freeze), len(fixture.billing.completions))
	}
}

func TestPublicNonStreamOrchestratorOutputLimitFailsBeforeSecretFreezeAndNetwork(t *testing.T) {
	events := []string{}
	fixture := newPublicRuntimeFixture(t, &events, 1)
	fixture.request.Body = []byte(`{"model":"public-text","input":"hello","max_output_tokens":8193}`)
	err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
	if !errors.Is(err, errPublicNonStreamRequestInvalid) ||
		strings.Join(events, ",") != "plan,guard:input" || len(fixture.billing.freeze) != 0 ||
		len(fixture.coordinator.attemptIDs) != 0 || len(fixture.providerBodies) != 0 {
		t.Fatalf("output bound crossed admission fence: err=%v events=%v", err, events)
	}
}

func TestPublicNonStreamOrchestratorHistoricalUnverifiedBindingRemainsStrictV1(t *testing.T) {
	for _, operation := range []modeloperation.ID{modeloperation.IDResponses, modeloperation.IDMessages} {
		t.Run(string(operation), func(t *testing.T) {
			events := []string{}
			fixture := newPublicRuntimeFixture(t, &events, 1)
			if operation == modeloperation.IDMessages {
				fixture.planner.plan = publicRuntimePlanForProfileOperation(
					t, 1, "anthropic.claude-sonnet-5.api", "1.1.0", operation,
				)
				fixture.request.Operation = operation
				fixture.request.Path = modeloperation.PathMessages
				fixture.request.Body = []byte(`{"model":"public-text","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
			}
			fixture.planner.plan.ExecutionProfileID = modelprofile.UnverifiedProfileID
			fixture.planner.plan.ExecutionProfileVersion = modelprofile.UnverifiedProfileVersion
			preparation, err := fixture.orchestrator.prepare(t.Context(), fixture.request, false)
			if err != nil || preparation.identity.DeliveryContractVersion != endpointprotocol.PublicDeliveryContractV1 ||
				len(fixture.planner.requirements) != 0 || len(fixture.billing.freeze) != 1 {
				t.Fatalf("historical v1 preparation: err=%v requirements=%v", err, fixture.planner.requirements)
			}
		})
	}
}

func TestPublicNonStreamOrchestratorHistoricalUnverifiedCannotAcquireCurrentCapabilities(t *testing.T) {
	for _, name := range []string{"v2 binding", "stream", "reasoning", "tools"} {
		t.Run(name, func(t *testing.T) {
			events := []string{}
			fixture := newPublicRuntimeFixture(t, &events, 1)
			if name == "v2 binding" {
				fixture.planner.plan = publicRuntimePlanForProfile(t, 1, "openai.gpt-5.6-sol.responses", "1.3.0")
			}
			fixture.planner.plan.ExecutionProfileID = modelprofile.UnverifiedProfileID
			fixture.planner.plan.ExecutionProfileVersion = modelprofile.UnverifiedProfileVersion
			switch name {
			case "reasoning":
				fixture.request.Body = []byte(`{"model":"public-text","input":"hello","max_output_tokens":64,"reasoning":{"effort":"high"}}`)
			case "tools":
				fixture.request.Body = []byte(`{"model":"public-text","input":"hello","max_output_tokens":64,"tools":[{"type":"web_search"}]}`)
			}
			_, err := fixture.orchestrator.prepare(t.Context(), fixture.request, name == "stream")
			if (!errors.Is(err, errPublicNonStreamRequestInvalid) && !errors.Is(err, errPublicNonStreamContractMismatch)) ||
				strings.Join(events, ",") != "plan" || len(fixture.billing.freeze) != 0 ||
				len(fixture.coordinator.attemptIDs) != 0 || len(fixture.providerBodies) != 0 {
				t.Fatalf("historical profile gained capabilities: err=%v events=%v", err, events)
			}
		})
	}
}

func TestPublicNonStreamOrchestratorSelectsExactResponsesV2BindingForAddressableProfile(t *testing.T) {
	events := []string{}
	fixture := newPublicRuntimeFixture(t, &events, 1)
	fixture.planner.plan = publicRuntimePlanForProfile(
		t, 1, "openai.gpt-5.6-sol.responses", "1.3.0",
	)
	writer := &publicRuntimeWriter{events: &events}
	if err := fixture.orchestrator.execute(t.Context(), writer, fixture.request); err != nil {
		t.Fatal(err)
	}
	route := fixture.planner.plan.Routes[0]
	if route.ProtocolProfileID != endpointprotocol.ProfileOpenAIResponsesTextV2 ||
		route.ProtocolProfileVersion != "1.0.0" ||
		route.ExecutorContractVersion != modelqualification.ResponsesDualExecutorContractV1 ||
		route.PublicEvidence == nil ||
		route.PublicEvidence.DeliveryContractVersion != endpointprotocol.PublicResponsesDeliveryContractV2 {
		t.Fatalf("v2 route identity=%+v evidence=%+v", route, route.PublicEvidence)
	}
	if len(fixture.billing.freeze) != 1 ||
		fixture.billing.freeze[0].ExecutionSnapshotHash != fixture.planner.plan.SnapshotHash ||
		len(fixture.coordinator.attemptIDs) != 1 || writer.body.Len() == 0 {
		t.Fatalf("v2 execution freeze=%+v attempts=%v body=%q", fixture.billing.freeze, fixture.coordinator.attemptIDs, writer.body.String())
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
			if got := strings.Join(events, ","); got != "plan" || len(fixture.billing.freeze) != 0 ||
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
			if got := strings.Join(events, ","); got != "plan" || len(fixture.billing.freeze) != 0 ||
				len(fixture.billing.completions) != 0 || len(fixture.coordinator.attemptIDs) != 0 {
				t.Fatalf("static drift crossed early fence: events=%s freeze=%d completions=%d attempts=%d",
					got, len(fixture.billing.freeze), len(fixture.billing.completions), len(fixture.coordinator.attemptIDs))
			}
		})
	}
}

func TestPublicNonStreamOrchestratorHealthActionsWithoutTransition(t *testing.T) {
	var events []string
	orchestrator := &publicNonStreamOrchestrator{coordinator: &publicRuntimeCoordinator{events: &events}}
	decision := modelexecution.Decision{Status: modelexecution.Applied}
	if err := orchestrator.applyTerminalHealth(t.Context(), decision, PublicNonStreamHealthNone); err != nil {
		t.Fatalf("explicit no-op action: %v", err)
	}
	for _, action := range []PublicNonStreamHealthAction{"", "unknown"} {
		if err := orchestrator.applyTerminalHealth(t.Context(), decision, action); !errors.Is(err, errPublicNonStreamHealthFailed) {
			t.Fatalf("unsupported health action %q: %v", action, err)
		}
	}
	if len(events) != 0 {
		t.Fatalf("unexpected health writes: %v", events)
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
	want := "plan,guard:input,build:ciphertext-1,freeze,authorize:1,provider:provider-1.example,guard:output,complete:response_accepted,health:success,hold:provider_attempt_unresolved"
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
	want := "plan,guard:input,build:ciphertext-1,build:ciphertext-2,freeze,authorize:1,provider:unknown,complete:outcome_unknown,health:provider_failure"
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
	want := "plan,guard:input,build:ciphertext-1,build:ciphertext-2,freeze,authorize:1,provider:provider-1.example,complete:response_unusable,health:provider_failure"
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

func TestPublicRuntimeAttributesAdmissionAndDispatchFailures(t *testing.T) {
	const privateDetail = "private database detail"
	tests := []struct {
		name         string
		freezeResult *billing.FreezeResult
		authorizeErr error
		publicError  error
		publicCode   string
		diagnostic   string
		wantCancels  int
	}{
		{
			name: "policy unavailable", freezeResult: &billing.FreezeResult{ErrorCode: policybilling.ErrorCodeStateUnavailable},
			publicError: errPublicNonStreamBillingFailed, publicCode: "server_error.billing_unavailable",
			diagnostic: "phase=execution_admission reason=limit_policy_state_unavailable",
		},
		{
			name: "missing binding", freezeResult: &billing.FreezeResult{ErrorCode: "billing_contract_unavailable"},
			publicError: errPublicCustomerPriceUnavailable, publicCode: "billing_error.billing_contract_unavailable",
		},
		{
			name: "dispatch serialization", authorizeErr: fmt.Errorf("authorize attempt: %w", &pgconn.PgError{Code: "40001", Message: privateDetail}),
			publicError: errPublicNonStreamRuntimeUnavailable, publicCode: "server_error.public_execution_unavailable",
			diagnostic: "phase=dispatch_authorization reason=postgres_serialization_failure", wantCancels: 1,
		},
		{
			name: "opaque dispatch error", authorizeErr: errors.New(privateDetail + " with text 40001"),
			publicError: errPublicNonStreamRuntimeUnavailable, publicCode: "server_error.public_execution_unavailable",
			diagnostic: "phase=dispatch_authorization reason=authorization_error", wantCancels: 1,
		},
	}

	for _, stream := range []bool{false, true} {
		for _, test := range tests {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, test.name), func(t *testing.T) {
				var events []string
				coordinator := &publicRuntimeCoordinator{events: &events, authorizeErr: test.authorizeErr}
				var request publicNonStreamRequest
				var financial *publicRuntimeBilling
				var execute func(context.Context, http.ResponseWriter, publicNonStreamRequest) error
				if stream {
					fixture := newPublicResponsesStreamFixture(t, &events, "", disabledPublicStreamGuard(t))
					fixture.orchestrator.core.coordinator = coordinator
					execute, request, financial = fixture.orchestrator.execute, fixture.request, fixture.billing
				} else {
					fixture := newPublicRuntimeFixture(t, &events, 2)
					fixture.orchestrator.coordinator = coordinator
					execute, request, financial = fixture.orchestrator.execute, fixture.request, fixture.billing
				}
				financial.freezeResult = test.freezeResult
				var logs bytes.Buffer
				previous := log.Writer()
				log.SetOutput(&logs)
				t.Cleanup(func() { log.SetOutput(previous) })
				writer := httptest.NewRecorder()
				err := execute(t.Context(), writer, request)
				log.SetOutput(previous)

				if !errors.Is(err, test.publicError) || publicResponseWasStarted(err) {
					t.Fatalf("pre-response error = %v", err)
				}
				writePublicNonStreamError(writer, httptest.NewRequest(http.MethodPost, request.Path, nil), request.RequestID, err)
				if writer.Code != http.StatusServiceUnavailable || !strings.Contains(writer.Body.String(), test.publicCode) ||
					strings.Contains(writer.Body.String(), privateDetail) || strings.Contains(writer.Body.String(), "phase=") {
					t.Fatalf("public response = %d %s", writer.Code, writer.Body.String())
				}
				if len(coordinator.attemptIDs) != test.wantCancels || financial.cancelled != test.wantCancels ||
					len(financial.completions) != 0 || len(financial.settled) != 0 ||
					strings.Contains(strings.Join(events, ","), "provider:") {
					t.Fatalf("unexpected provider or wallet lifecycle: %v", events)
				}
				logged := logs.String()
				if test.diagnostic == "" {
					if strings.Contains(logged, "module=public_execution") {
						t.Fatalf("missing binding misclassified: %s", logged)
					}
				} else if !strings.Contains(logged, fmt.Sprintf("request_id=%q %s", request.RequestID, test.diagnostic)) ||
					strings.Contains(logged, privateDetail) {
					t.Fatalf("diagnostic = %s", logged)
				}
			})
		}
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
	return publicRuntimePlanForProfile(t, routeCount, "openai.gpt-5.6-sol.responses", "1.1.0")
}

func publicRuntimePlanForProfile(
	t *testing.T,
	routeCount int,
	executionProfileID string,
	executionProfileVersion string,
) modelrouting.Plan {
	return publicRuntimePlanForProfileOperation(
		t, routeCount, executionProfileID, executionProfileVersion, modeloperation.IDResponses,
	)
}

func publicRuntimePlanForProfileOperation(
	t *testing.T,
	routeCount int,
	executionProfileID string,
	executionProfileVersion string,
	operation modeloperation.ID,
) modelrouting.Plan {
	t.Helper()
	profile, ok := modelprofile.Lookup(executionProfileID, executionProfileVersion)
	if !ok {
		t.Fatalf("profile %s@%s is unavailable", executionProfileID, executionProfileVersion)
	}
	return publicRuntimePlanForExecutionProfile(t, routeCount, profile, operation)
}

func publicRuntimePlanForExecutionProfile(t *testing.T, routeCount int, profile modelprofile.Profile, operation modeloperation.ID) modelrouting.Plan {
	t.Helper()
	executionProfileID, executionProfileVersion := profile.ID, profile.Version
	binding, err := modelqualification.CompiledBindingFor(
		string(operation), profile.ProtocolProfile, profile.ProtocolProfileVersion,
	)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := modelqualification.CompiledPublicBindingFor(binding)
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
			ModelOperationID: string(operation), OperationRevision: 1,
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
		ModelOperationID: string(operation), PublicContextLimit: 128000, PublicOutputLimit: 8192,
		ExecutionProfileID: executionProfileID, ExecutionProfileVersion: executionProfileVersion,
		SnapshotHash:     "mrs1_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		RuntimeReleaseID: 51, RuntimeReleaseRef: "rcr_public_runtime", RuntimeStage: "active", SlotVersion: 1,
		Routes: routes,
	}
}

func strconvItoa(value int) string { return strconv.Itoa(value) }

func TestPublicNativeClientToolProfilesKeepImageInputOutsideTheirContract(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/native-responses-messages-client-tools.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	require.Len(t, bundle.Definitions, 5)
	imageURL := testJPEGDataURL(t, 144, 96)
	registry := endpointprotocol.NewPublicNonStreamRegistry()
	for _, raw := range bundle.Definitions {
		definition, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		p := definition.Profile()
		t.Run(p.ID, func(t *testing.T) {
			op := p.Operations[0]
			binding, err := publicBindingForExecutionProfile(p, op)
			require.NoError(t, err)
			identity, err := modelqualification.CompiledPublicBindingFor(binding)
			require.NoError(t, err)
			protocol, err := registry.Resolve(identity)
			require.NoError(t, err)
			contract, err := publicRequestContractForProfile(p, op, identity)
			require.NoError(t, err)
			const public = "friendly-model"
			var text, withImage string
			if op == modelprofile.OperationOpenAIResponses {
				text = `{"model":"friendly-model","max_output_tokens":512,"store":false,"input":"read the color"}`
				withImage = fmt.Sprintf(`{"model":"friendly-model","max_output_tokens":512,"store":false,"input":[{"role":"user","content":[{"type":"input_text","text":"read the color"},{"type":"input_image","image_url":%q}]}]}`, imageURL)
			} else {
				const controls = `{"model":"friendly-model","max_tokens":512,"tools":[{"name":"lookup","description":"A local lookup.","input_schema":{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}}],"tool_choice":{"type":"auto"},`
				text = controls + `"messages":[{"role":"user","content":"read the color"}]}`
				withImage = controls + fmt.Sprintf(`"messages":[{"role":"user","content":[{"type":"text","text":"read the color"},{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":%q}}]}]}`,
					strings.TrimPrefix(imageURL, "data:image/jpeg;base64,"))
			}
			_, err = protocol.PrepareRequest([]byte(text), public, contract)
			require.NoError(t, err, "text on the very same route must still work")
			_, err = protocol.PrepareRequest([]byte(withImage), public, contract)
			if op == modelprofile.OperationOpenAIResponses {
				require.ErrorIs(t, err, endpointprotocol.ErrUnsupportedSemantics)
			} else {
				require.ErrorIs(t, err, endpointprotocol.ErrInvalidRequest)
				require.ErrorContains(t, err, "tool_result_without_tool_use")
			}
		})
	}
}
