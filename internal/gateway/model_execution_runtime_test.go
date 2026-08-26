package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/executionprojection"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

func toolChoiceConstraint(names []string, selected string) *modelrouting.FunctionToolChoiceConstraint {
	return &modelrouting.FunctionToolChoiceConstraint{
		DeclaredFunctionNames: slices.Clone(names),
		SelectedFunctionName:  selected,
	}
}

type executionPlannerStub struct {
	mu       sync.Mutex
	plan     modelrouting.Plan
	err      error
	requests []modelrouting.Request
}

type executionRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn executionRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type executionCountingGuardrailDetector struct {
	calls *atomic.Int64
}

func TestInvocationCapabilityRequirementsForChatUsesExactReasoningEffortIdentity(t *testing.T) {
	profile, ok := modelprofile.Lookup("doubao.seed-2.1-turbo.api", "1.1.0")
	if !ok {
		t.Fatal("Doubao Turbo candidate profile is unavailable")
	}
	tests := []struct {
		name, body, origin string
	}{
		{name: "provider default", body: `{"model":"doubao-seed-2-1-turbo-260628","messages":[]}`, origin: modelrouting.CapabilityOriginProviderDefault},
		{name: "explicit low", body: `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"reasoning_effort":"low"}`, origin: modelrouting.CapabilityOriginExplicitRequest},
		{name: "explicit minimal", body: `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"reasoning_effort":"minimal"}`, origin: modelrouting.CapabilityOriginExplicitRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), modelExecutionPlanForProfile(profile))
			if err != nil {
				t.Fatalf("validate exact profile request: %v", err)
			}
			requirements, err := invocationCapabilityRequirementsForChat(profile, evidence, []byte(test.body))
			if err != nil {
				t.Fatalf("derive invocation capability requirements: %v", err)
			}
			want := []modelrouting.CapabilityRequirement{{
				CapabilityID: modelprofile.CapabilityReasoningEffort,
				Origins:      []string{test.origin},
			}}
			if !slices.EqualFunc(want, requirements, func(left, right modelrouting.CapabilityRequirement) bool {
				return left.CapabilityID == right.CapabilityID && slices.Equal(left.Origins, right.Origins)
			}) {
				t.Fatalf("requirements = %+v, want %+v", requirements, want)
			}
		})
	}
}

func TestInvocationCapabilityRequirementsForDoubaoSeedEvolvingPreserveOrigin(t *testing.T) {
	profile, ok := modelprofile.Lookup("doubao.seed-evolving.api", "1.3.0")
	if !ok {
		t.Fatal("Doubao Seed Evolving reasoning profile is unavailable")
	}
	tests := []struct {
		name, body, capability, origin string
	}{
		{
			name:       "provider default effort",
			body:       `{"model":"doubao-seed-evolving","messages":[]}`,
			capability: modelprofile.CapabilityReasoningEffort,
			origin:     modelrouting.CapabilityOriginProviderDefault,
		},
		{
			name:       "explicit thinking toggle",
			body:       `{"model":"doubao-seed-evolving","messages":[],"thinking":{"type":"enabled"}}`,
			capability: modelprofile.CapabilityReasoning,
			origin:     modelrouting.CapabilityOriginExplicitRequest,
		},
		{
			name:       "explicit high effort",
			body:       `{"model":"doubao-seed-evolving","messages":[],"reasoning_effort":"high"}`,
			capability: modelprofile.CapabilityReasoningEffort,
			origin:     modelrouting.CapabilityOriginExplicitRequest,
		},
		{
			name:       "explicit compatibility effort",
			body:       `{"model":"doubao-seed-evolving","messages":[],"reasoning_effort":"xhigh"}`,
			capability: modelprofile.CapabilityReasoningEffort,
			origin:     modelrouting.CapabilityOriginExplicitRequest,
		},
		{
			name:       "explicit disabled effort",
			body:       `{"model":"doubao-seed-evolving","messages":[],"reasoning_effort":"minimal"}`,
			capability: modelprofile.CapabilityReasoningEffort,
			origin:     modelrouting.CapabilityOriginExplicitRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), modelExecutionPlanForProfile(profile))
			if err != nil {
				t.Fatalf("validate exact profile request: %v", err)
			}
			requirements, err := invocationCapabilityRequirementsForChat(profile, evidence, []byte(test.body))
			if err != nil {
				t.Fatalf("derive invocation capabilities: %v", err)
			}
			want := []modelrouting.CapabilityRequirement{{
				CapabilityID: test.capability,
				Origins:      []string{test.origin},
			}}
			if !slices.EqualFunc(want, requirements, func(left, right modelrouting.CapabilityRequirement) bool {
				return left.CapabilityID == right.CapabilityID && slices.Equal(left.Origins, right.Origins)
			}) {
				t.Fatalf("requirements = %+v, want %+v", requirements, want)
			}
		})
	}
}

func TestInvocationCapabilityRequirementsForChatPreservesCombinedReasoningControls(t *testing.T) {
	profile, ok := modelprofile.Lookup("deepseek.v4-pro.api", "1.2.0")
	if !ok {
		t.Fatal("DeepSeek current profile is unavailable")
	}
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled"},"reasoning_effort":"max"}`)
	evidence, err := validateChatExecutionProfileRequest(body, modelExecutionPlanForProfile(profile))
	if err != nil {
		t.Fatalf("validate combined reasoning controls: %v", err)
	}
	requirements, err := invocationCapabilityRequirementsForChat(profile, evidence, body)
	if err != nil {
		t.Fatalf("derive combined reasoning capabilities: %v", err)
	}
	want := []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
		{CapabilityID: modelprofile.CapabilityReasoningEffort, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}
	if !slices.EqualFunc(want, requirements, func(left, right modelrouting.CapabilityRequirement) bool {
		return left.CapabilityID == right.CapabilityID && slices.Equal(left.Origins, right.Origins)
	}) {
		t.Fatalf("requirements = %+v, want %+v", requirements, want)
	}
}

func (detector executionCountingGuardrailDetector) Version() string { return "test-v1" }

func (detector executionCountingGuardrailDetector) Detect(_ guardrail.Context, text guardrail.Text, _ json.RawMessage) ([]guardrail.Finding, error) {
	detector.calls.Add(1)
	if text.Content == "" {
		return nil, nil
	}
	return []guardrail.Finding{{Category: "test", Start: 0, End: 1, ConfidenceBucket: "high"}}, nil
}

func (stub *executionPlannerStub) Plan(_ context.Context, request modelrouting.Request) (modelrouting.Plan, error) {
	plan := stub.plan
	recorded := request
	if request.DeriveInvocationCapabilities != nil {
		profile, ok := modelprofile.Lookup(plan.ExecutionProfileID, plan.ExecutionProfileVersion)
		if !ok {
			profile, ok = modelprofile.Lookup(modelprofile.UnverifiedProfileID, modelprofile.UnverifiedProfileVersion)
		}
		if !ok {
			return modelrouting.Plan{}, errors.New("execution planner stub profile is unavailable")
		}
		requirements, err := modelrouting.ResolveCapabilityRequirements(profile, request)
		if err != nil {
			return modelrouting.Plan{}, err
		}
		recorded.DeriveInvocationCapabilities = nil
		// This is a read-only test observation of the invocation-derived subset,
		// not a Request that may be replayed into the Planner. The full Plan below
		// remains the sole normalized authority and retains always-required origins.
		recorded.InvocationCapabilities = invocationCapabilityObservation(requirements)
		plan.CapabilityRequirements = requirements
		plan.RequiredCapabilities = make([]string, len(requirements))
		for index := range requirements {
			plan.RequiredCapabilities[index] = requirements[index].CapabilityID
		}
	}
	stub.mu.Lock()
	stub.requests = append(stub.requests, recorded)
	stub.mu.Unlock()
	return plan, stub.err
}

func invocationCapabilityObservation(requirements []modelrouting.CapabilityRequirement) []modelrouting.CapabilityRequirement {
	observed := make([]modelrouting.CapabilityRequirement, 0, len(requirements))
	for _, requirement := range requirements {
		origins := make([]string, 0, len(requirement.Origins))
		for _, origin := range requirement.Origins {
			if origin != modelrouting.CapabilityOriginProfileAlwaysRequired {
				origins = append(origins, origin)
			}
		}
		if len(origins) > 0 {
			observedRequirement := modelrouting.CapabilityRequirement{
				CapabilityID: requirement.CapabilityID,
				Origins:      origins,
			}
			if requirement.FunctionToolChoice != nil {
				observedRequirement.FunctionToolChoice = toolChoiceConstraint(
					requirement.FunctionToolChoice.DeclaredFunctionNames,
					requirement.FunctionToolChoice.SelectedFunctionName,
				)
			}
			observed = append(observed, observedRequirement)
		}
	}
	return observed
}

type executionCoordinatorStub struct {
	authorizeFn func(modelrouting.Plan, modelrouting.Route, modelexecution.AttemptIdentity) (modelexecution.Decision, error)
	renewFn     func(modelexecution.Decision) (modelexecution.HealthResult, error)
	releaseFn   func(modelexecution.Decision) (modelexecution.HealthResult, error)
	successFn   func(modelexecution.Decision) (modelexecution.HealthResult, error)
	providerFn  func(modelexecution.Decision) (modelexecution.HealthResult, error)
	authFn      func(modelexecution.Decision) (modelexecution.HealthResult, error)
	rateFn      func(modelexecution.Decision) (modelexecution.HealthResult, error)

	authorizeCount atomic.Int64
	renewCount     atomic.Int64
	releaseCount   atomic.Int64
	successCount   atomic.Int64
	providerCount  atomic.Int64
	authCount      atomic.Int64
	rateCount      atomic.Int64
}

func (stub *executionCoordinatorStub) Authorize(_ context.Context, plan modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
	stub.authorizeCount.Add(1)
	if stub.authorizeFn != nil {
		return stub.authorizeFn(plan, route, identity)
	}
	return modelexecution.Decision{Status: modelexecution.Stale}, nil
}

func (stub *executionCoordinatorStub) RenewLease(_ context.Context, decision modelexecution.Decision) (modelexecution.HealthResult, error) {
	stub.renewCount.Add(1)
	if stub.renewFn != nil {
		return stub.renewFn(decision)
	}
	return modelexecution.HealthResult{Status: modelexecution.Applied}, nil
}

func (stub *executionCoordinatorStub) ReleaseUndispatched(_ context.Context, decision modelexecution.Decision) (modelexecution.HealthResult, error) {
	stub.releaseCount.Add(1)
	if stub.releaseFn != nil {
		return stub.releaseFn(decision)
	}
	return modelexecution.HealthResult{Status: modelexecution.Applied}, nil
}

func (stub *executionCoordinatorStub) MarkSuccess(_ context.Context, decision modelexecution.Decision) (modelexecution.HealthResult, error) {
	stub.successCount.Add(1)
	if stub.successFn != nil {
		return stub.successFn(decision)
	}
	return modelexecution.HealthResult{Status: modelexecution.Applied}, nil
}

func (stub *executionCoordinatorStub) MarkProviderFailure(_ context.Context, decision modelexecution.Decision) (modelexecution.HealthResult, error) {
	stub.providerCount.Add(1)
	if stub.providerFn != nil {
		return stub.providerFn(decision)
	}
	return modelexecution.HealthResult{Status: modelexecution.Applied}, nil
}

func (stub *executionCoordinatorStub) MarkCredentialAuthFailure(_ context.Context, decision modelexecution.Decision) (modelexecution.HealthResult, error) {
	stub.authCount.Add(1)
	if stub.authFn != nil {
		return stub.authFn(decision)
	}
	return modelexecution.HealthResult{Status: modelexecution.Applied}, nil
}

func (stub *executionCoordinatorStub) MarkCredentialRateLimited(_ context.Context, decision modelexecution.Decision) (modelexecution.HealthResult, error) {
	stub.rateCount.Add(1)
	if stub.rateFn != nil {
		return stub.rateFn(decision)
	}
	return modelexecution.HealthResult{Status: modelexecution.Applied}, nil
}

func executionTestEvidence(t *testing.T, mode modelrouting.Mode, operation modeloperation.ID, capabilities []executionevidence.CapabilityEntry) executionevidence.EvidenceV1 {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	capabilityHash, err := executionevidence.CapabilityDigest(capabilities)
	if err != nil {
		t.Fatalf("capability digest: %v", err)
	}
	evidence := executionevidence.EvidenceV1{
		ContractVersion: executionevidence.ContractVersion, AuthorityMode: mode,
		ExecutionContractSource: executionevidence.ContractSourceCanonicalProfile,
		PublicModelID:           11, DeploymentID: 12, ConnectionID: 13, CredentialBindingID: 14,
		ConnectionRevision: 2, DeploymentRevision: 3, CircuitRevision: 4,
		CredentialStateRevision: 5, CredentialLinkedAt: now.Add(-time.Hour),
		ModelOperationID: string(operation), OperationRevision: 6,
		ProtocolProfileID: "openai-compatible.v1", ProtocolProfileVersion: "v1",
		OperationEvidenceState: "production_approved", OperationEvidenceRef: "fixture://operation",
		OperationVerifiedAt: &now, CapabilityEvidenceHash: capabilityHash, CapabilityEvidence: capabilities,
		CredentialProbeID: 15, CredentialProbeTestedAt: now,
		RuntimeReleaseID: 16, RuntimeReleaseRef: "release-fixture", RuntimeStage: "active", SlotVersion: 7,
		SnapshotHash: "mrs1_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if mode == modelrouting.ModeVerify {
		evidence.LegacyChannelID = 21
		evidence.LegacyChannelKeyID = 22
		evidence.LegacyRoutingRevision = 8
		evidence.LegacyCircuitRevision = 9
		evidence.LegacyCredentialStateRevision = 10
	}
	if mode == modelrouting.ModeLegacy {
		evidence = executionevidence.EvidenceV1{
			ContractVersion: executionevidence.ContractVersion, AuthorityMode: mode,
			ExecutionContractSource: executionevidence.ContractSourceLegacyAdapter,
			ModelOperationID:        string(operation), LegacyChannelID: 21, LegacyChannelKeyID: 22,
			LegacyRoutingRevision: 8, LegacyCircuitRevision: 9, LegacyCredentialStateRevision: 10,
		}
	}
	if _, err := executionevidence.Normalize(evidence); err != nil {
		t.Fatalf("fixture evidence: %v", err)
	}
	return evidence
}

func executionTestPlan(t *testing.T, mode modelrouting.Mode, operation modeloperation.ID, baseURL string, encKey []byte, capabilities []executionevidence.CapabilityEntry) modelrouting.Plan {
	t.Helper()
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encKey)
	if err != nil {
		t.Fatalf("encrypt fixture secret: %v", err)
	}
	evidence := executionTestEvidence(t, mode, operation, capabilities)
	route := modelrouting.Route{
		ConnectionName: "fixture-connection", Provider: "openai", BaseURL: baseURL,
		AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
		ProtocolProfileID: "openai-compatible.v1", ProtocolProfileVersion: "v1",
		UpstreamModel: "upstream-model", MaxContext: 128000, DefaultMaxOutput: 4096,
		EncryptedSecret: encrypted, Priority: 10, Weight: 100, SelectionReason: "fixture", Evidence: evidence,
	}
	plan := modelrouting.Plan{
		AuthorityMode: mode, PublicModelID: evidence.PublicModelID, PublicModel: "public-model",
		ModelOperationID: string(operation), PublicContextLimit: 128000, PublicOutputLimit: 8192,
		RequestDefaultOutputLimit: 8192,
		ExecutionProfileID:        modelprofile.UnverifiedProfileID, ExecutionProfileVersion: modelprofile.UnverifiedProfileVersion,
		TokenizerProfileID: modelprofile.TokenizerByteUpperID, TokenizerProfileVersion: modelprofile.TokenizerByteUpperVersion,
		SnapshotHash:     evidence.SnapshotHash,
		RuntimeReleaseID: evidence.RuntimeReleaseID, RuntimeReleaseRef: evidence.RuntimeReleaseRef,
		RuntimeStage: evidence.RuntimeStage, SlotVersion: evidence.SlotVersion, Routes: []modelrouting.Route{route},
	}
	setExecutionTestProfile(t, &plan, modelprofile.UnverifiedProfileID, modelprofile.UnverifiedProfileVersion)
	if mode == modelrouting.ModeLegacy {
		plan.RequestDefaultOutputLimit = 0
		plan.PublicModelID = 0
		plan.SnapshotHash = "legacy-snapshot"
		plan.RuntimeReleaseID = 0
		plan.RuntimeReleaseRef = ""
		plan.RuntimeStage = ""
		plan.SlotVersion = 0
	}
	return plan
}

func setExecutionTestProfile(t *testing.T, plan *modelrouting.Plan, id, version string) {
	t.Helper()
	profile, ok := modelprofile.Lookup(id, version)
	if !ok {
		t.Fatalf("unknown execution test profile %s@%s", id, version)
	}
	plan.ExecutionProfileID = profile.ID
	plan.ExecutionProfileVersion = profile.Version
	plan.TokenizerProfileID = profile.TokenizerProfileID
	plan.TokenizerProfileVersion = profile.TokenizerProfileVersion
	plan.ReasoningProfile = profile.ReasoningProfile
	plan.CacheUsageProfile = profile.CacheUsageProfile
	plan.ModelProtocolProfile = profile.ProtocolProfile
	if profile.DefaultOutputLimit > 0 {
		plan.RequestDefaultOutputLimit = profile.DefaultOutputLimit
	}
}

func executionTestDB() *testutil.MockDB {
	return &testutil.MockDB{ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}}
}

func executionRequest(method, path, requestID, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(request.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(2))
	ctx = context.WithValue(ctx, middleware.CtxProjectID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, requestID)
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 2, OrgID: 1, AuthEpoch: 1, Status: "active"})
	return request.WithContext(ctx)
}

func executionGuardrailService(t *testing.T, policy guardrail.Policy, detector guardrail.Detector) *guardrail.Service {
	t.Helper()
	database := &testutil.MockDB{QueryFn: func(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
		if strings.Contains(query, "HAVING COUNT(*)") {
			return &testutil.MockRows{}, nil
		}
		return &testutil.MockRows{Rows: []func(...any) error{func(dest ...any) error {
			*dest[0].(*int64) = policy.ID
			*dest[1].(**int64) = policy.OrgID
			*dest[2].(**int64) = policy.ProjectID
			*dest[3].(**int64) = policy.APIKeyID
			*dest[4].(**int64) = policy.AccessCredentialID
			*dest[5].(*string) = policy.Name
			*dest[6].(*string) = policy.Model
			*dest[7].(*string) = policy.Stage
			*dest[8].(*string) = policy.DetectorRef
			*dest[9].(*string) = policy.Action
			*dest[10].(*string) = policy.Mode
			*dest[11].(*string) = policy.FailureMode
			*dest[12].(*bool) = policy.Mandatory
			*dest[13].(*json.RawMessage) = append(json.RawMessage(nil), policy.Config...)
			*dest[14].(*int64) = policy.Version
			*dest[15].(*time.Time) = time.Now().UTC()
			*dest[16].(*time.Time) = time.Now().UTC()
			return nil
		}}}, nil
	}}
	service, err := guardrail.NewService(database, nil, []byte("0123456789abcdef0123456789abcdef"), guardrail.DefaultOptions())
	if err != nil {
		t.Fatalf("new guardrail service: %v", err)
	}
	if err := service.RegisterDetector(policy.DetectorRef, detector); err != nil {
		t.Fatalf("register detector: %v", err)
	}
	if err := service.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh guardrail service: %v", err)
	}
	return service
}

func TestModelAuthorizationDenialPrecedesGuardrailRoutingAndFreeze(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		body   string
		handle func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{name: "chat", path: "/v1/chat/completions", body: `{"model":"public-model","messages":[{"role":"user","content":"secret"}]}`, handle: (*Relay).HandleChatCompletion},
		{name: "embeddings", path: "/v1/embeddings", body: `{"model":"public-model","input":"secret"}`, handle: (*Relay).HandleEmbeddings},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var detectorCalls, receiptTraces, freezes atomic.Int64
			database := &testutil.MockDB{ExecFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(query, "INSERT INTO request_trace_events") && len(args) > 9 && args[8] == "guardrail.decision" {
					receiptTraces.Add(1)
				}
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			}}
			relay := newTestRelay(database, &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
				freezes.Add(1)
				return &billing.FreezeResult{Success: true}, nil
			}})
			service := executionGuardrailService(t, guardrail.Policy{
				ID: 31, Name: "model authorization ordering", Version: 1, Stage: guardrail.StageInput,
				DetectorRef: "external:model_auth_order", Action: guardrail.ActionBlock,
				Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureFailClosed,
			}, executionCountingGuardrailDetector{calls: &detectorCalls})
			relay.SetGuardrail(service)
			planner := &executionPlannerStub{}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeLegacy, planner, &executionCoordinatorStub{}, routingpolicy.Default()); err != nil {
				t.Fatalf("set model execution runtime: %v", err)
			}
			request := executionRequest(http.MethodPost, test.path, "req-auth-order-denied-"+test.name, test.body)
			request = request.WithContext(context.WithValue(request.Context(), middleware.CtxAPIKey, &models.ApiKey{
				ID: 2, OrgID: 1, Status: "active", AllowedModels: "different-model",
			}))
			recorder := httptest.NewRecorder()
			test.handle(relay, recorder, request)

			planner.mu.Lock()
			planCalls := len(planner.requests)
			planner.mu.Unlock()
			if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"auth_error.model_not_allowed"`) {
				t.Fatalf("denied model did not retain the 403 contract: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if detectorCalls.Load() != 0 || receiptTraces.Load() != 0 || planCalls != 0 || freezes.Load() != 0 {
				t.Fatalf("denied model crossed a later ingress boundary: detector=%d receipts=%d plans=%d freezes=%d",
					detectorCalls.Load(), receiptTraces.Load(), planCalls, freezes.Load())
			}
		})
	}
}

func TestAllowedModelStillRunsInputGuardrailBeforeRoutingAndFreeze(t *testing.T) {
	tests := []struct {
		name                  string
		path                  string
		body                  string
		expectedDetectorCalls int64
		handle                func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{name: "chat", path: "/v1/chat/completions", body: `{"model":"public-model","messages":[{"role":"user","content":"secret"}]}`, expectedDetectorCalls: 2, handle: (*Relay).HandleChatCompletion},
		{name: "embeddings", path: "/v1/embeddings", body: `{"model":"public-model","input":"secret"}`, expectedDetectorCalls: 1, handle: (*Relay).HandleEmbeddings},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var detectorCalls, receiptTraces, freezes atomic.Int64
			database := &testutil.MockDB{ExecFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(query, "INSERT INTO request_trace_events") && len(args) > 9 && args[8] == "guardrail.decision" {
					receiptTraces.Add(1)
				}
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			}}
			relay := newTestRelay(database, &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
				freezes.Add(1)
				return &billing.FreezeResult{Success: true}, nil
			}})
			service := executionGuardrailService(t, guardrail.Policy{
				ID: 32, Name: "allowed model guardrail ordering", Version: 1, Stage: guardrail.StageInput,
				DetectorRef: "external:model_auth_order", Action: guardrail.ActionBlock,
				Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureFailClosed,
			}, executionCountingGuardrailDetector{calls: &detectorCalls})
			relay.SetGuardrail(service)
			planner := &executionPlannerStub{}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeLegacy, planner, &executionCoordinatorStub{}, routingpolicy.Default()); err != nil {
				t.Fatalf("set model execution runtime: %v", err)
			}
			request := executionRequest(http.MethodPost, test.path, "req-auth-order-allowed-"+test.name, test.body)
			request = request.WithContext(context.WithValue(request.Context(), middleware.CtxAPIKey, &models.ApiKey{
				ID: 2, OrgID: 1, Status: "active", AllowedModels: "public-model",
			}))
			recorder := httptest.NewRecorder()
			test.handle(relay, recorder, request)

			planner.mu.Lock()
			planCalls := len(planner.requests)
			planner.mu.Unlock()
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"guardrail_error.input_blocked"`) {
				t.Fatalf("allowed model no longer reached its input guardrail: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if detectorCalls.Load() != test.expectedDetectorCalls || receiptTraces.Load() != 1 || planCalls != 0 || freezes.Load() != 0 {
				t.Fatalf("allowed model guardrail ordering changed: detector=%d receipts=%d plans=%d freezes=%d",
					detectorCalls.Load(), receiptTraces.Load(), planCalls, freezes.Load())
			}
		})
	}
}

func TestModelExecutionPreparationFailurePrecedesFreezeAndDispatch(t *testing.T) {
	tests := []struct {
		name      string
		operation modeloperation.ID
		path      string
		body      string
		handle    func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{name: "chat", operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions", body: `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`, handle: (*Relay).HandleChatCompletion},
		{name: "embeddings", operation: modeloperation.IDEmbeddings, path: "/v1/embeddings", body: `{"model":"public-model","input":"hello"}`, handle: (*Relay).HandleEmbeddings},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, test.operation, "https://provider.invalid", encKey, nil)
			plan.Routes[0].AdapterType = "unsupported-adapter"
			planner := &executionPlannerStub{plan: plan}
			coordinator := &executionCoordinatorStub{}
			var freezes, attempts, network atomic.Int64
			billingStub := &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
				freezes.Add(1)
				return &billing.FreezeResult{Success: true}, nil
			}, beginAttemptFn: func(context.Context, billing.ProviderAttemptStart) error {
				attempts.Add(1)
				return nil
			}}
			relay := NewRelay(executionTestDB(), nil, nil, billingStub, nil, encKey)
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				network.Add(1)
				return nil, errors.New("unexpected provider dispatch")
			})}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
				t.Fatalf("set runtime: %v", err)
			}
			recorder := httptest.NewRecorder()
			test.handle(relay, recorder, executionRequest(http.MethodPost, test.path, "req-build-fail-"+test.name, test.body))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if freezes.Load() != 0 || attempts.Load() != 0 || coordinator.authorizeCount.Load() != 0 || network.Load() != 0 {
				t.Fatalf("preparation failure crossed financial/dispatch boundary: freeze=%d attempts=%d authorize=%d network=%d",
					freezes.Load(), attempts.Load(), coordinator.authorizeCount.Load(), network.Load())
			}
		})
	}
}

func TestModelExecutionCapabilityNoRoutePrecedesFreeze(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	planner := &executionPlannerStub{err: modelrouting.ErrNoRoute}
	coordinator := &executionCoordinatorStub{}
	var freezes, attempts, network atomic.Int64
	billingStub := &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
		freezes.Add(1)
		return &billing.FreezeResult{Success: true}, nil
	}, beginAttemptFn: func(context.Context, billing.ProviderAttemptStart) error {
		attempts.Add(1)
		return nil
	}}
	relay := NewRelay(executionTestDB(), nil, nil, billingStub, nil, encKey)
	relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
		network.Add(1)
		return nil, errors.New("unexpected provider dispatch")
	})}
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
		t.Fatalf("set runtime: %v", err)
	}
	recorder := httptest.NewRecorder()
	relay.HandleChatCompletion(recorder, executionRequest(http.MethodPost, "/v1/chat/completions", "req-capability", `{"model":"public-model","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}]}`))
	if recorder.Code != http.StatusServiceUnavailable || freezes.Load() != 0 || attempts.Load() != 0 || coordinator.authorizeCount.Load() != 0 || network.Load() != 0 {
		t.Fatalf("capability rejection must be pre-freeze: status=%d freeze=%d attempts=%d authorize=%d network=%d",
			recorder.Code, freezes.Load(), attempts.Load(), coordinator.authorizeCount.Load(), network.Load())
	}
	planner.mu.Lock()
	defer planner.mu.Unlock()
	if len(planner.requests) != 1 || len(planner.requests[0].InvocationCapabilities) != 1 || planner.requests[0].InvocationCapabilities[0].CapabilityID != modelCapabilityTools {
		t.Fatalf("planner did not receive exact tool capability: %#v", planner.requests)
	}
}

func TestKnownTypedProviderExtensionsReachPlannerWithExactCapabilities(t *testing.T) {
	tests := []struct {
		name, profileID, profileVersion string
		body, publicModel               string
		want                            []string
	}{
		{name: "Qwen reasoning and tool stream", profileID: "qwen.3.8-max.api", profileVersion: "1.8.0", publicModel: "qwen3.8-max", body: `{"model":"qwen3.8-max","messages":[],"stream":true,"tools":[{"type":"function","function":{"name":"lookup","description":"Lookup"}}],"tool_stream":true,"enable_thinking":true,"thinking_budget":4096,"preserve_thinking":true}`, want: []string{"reasoning", "reasoning_budget", "streaming", "tool_stream", "tools"}},
		{name: "Qwen search", profileID: "qwen.3.8-max.api", profileVersion: "1.7.0", publicModel: "qwen3.8-max", body: `{"model":"qwen3.8-max","messages":[],"enable_search":true,"search_options":{"search_strategy":"turbo"}}`, want: []string{"builtin_web_search_request", "reasoning"}},
		{name: "Hy3 search", profileID: "hy3.api", profileVersion: "1.6.0", publicModel: "hy3", body: `{"model":"hy3","messages":[],"web_search_options":{"enable":true,"search_source":"lite"}}`, want: []string{"builtin_web_search_request"}},
		{name: "Qwen explicit cache", profileID: "qwen.3.8-max.api", profileVersion: "1.7.0", publicModel: "qwen3.8-max", body: `{"model":"qwen3.8-max","prompt_cache_options":{"mode":"explicit","ttl":"30m"},"messages":[{"role":"system","content":[{"type":"text","text":"prefix","prompt_cache_breakpoint":{"mode":"explicit"}}]}]}`, want: []string{"explicit_prompt_cache", "prompt_cache", "reasoning"}},
		{name: "Qwen remote media", profileID: "qwen.3.8-max.api", profileVersion: "1.7.0", publicModel: "qwen3.8-max", body: `{"model":"qwen3.8-max","messages":[{"role":"user","content":[{"type":"image_url","image_url":"https://example.test/image.webp"},{"type":"video_url","video_url":"https://example.test/video.mp4"}]}]}`, want: []string{"reasoning", "video", "vision"}},
		{name: "MiMo remote media", profileID: "mimo.v2.5.api", profileVersion: "1.6.0", publicModel: "mimo-v2.5", body: `{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"https://example.test/audio.wav"}},{"type":"video_url","video_url":"https://example.test/video.mp4","fps":1,"media_resolution":"default"}]}]}`, want: []string{"audio", "reasoning", "video"}},
		{name: "Doubao Turbo provider-default effort", profileID: "doubao.seed-2.1-turbo.api", profileVersion: "1.1.0", publicModel: "doubao-seed-2-1-turbo-260628", body: `{"model":"doubao-seed-2-1-turbo-260628","messages":[]}`, want: []string{"reasoning_effort"}},
		{name: "Doubao Turbo explicit disabled effort", profileID: "doubao.seed-2.1-turbo.api", profileVersion: "1.1.0", publicModel: "doubao-seed-2-1-turbo-260628", body: `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"reasoning_effort":"minimal"}`, want: []string{"reasoning_effort"}},
		{name: "Doubao Evolving plain Chat core", profileID: "doubao.seed-evolving.api", profileVersion: "1.2.0", publicModel: "doubao-seed-evolving", body: `{"model":"doubao-seed-evolving","messages":[]}`, want: []string{}},
		{name: "Doubao Evolving provider-default reasoning", profileID: "doubao.seed-evolving.api", profileVersion: "1.3.0", publicModel: "doubao-seed-evolving", body: `{"model":"doubao-seed-evolving","messages":[]}`, want: []string{"reasoning_effort"}},
		{name: "Doubao Evolving explicit thinking", profileID: "doubao.seed-evolving.api", profileVersion: "1.3.0", publicModel: "doubao-seed-evolving", body: `{"model":"doubao-seed-evolving","messages":[],"thinking":{"type":"enabled"}}`, want: []string{"reasoning"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := modelrouting.Plan{}
			setExecutionTestProfile(t, &plan, test.profileID, test.profileVersion)
			planner := &executionPlannerStub{plan: plan, err: modelrouting.ErrNoRoute}
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()); err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			relay.HandleChatCompletion(recorder, executionRequest(http.MethodPost, "/v1/chat/completions", "req-known-extension", test.body))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			planner.mu.Lock()
			defer planner.mu.Unlock()
			if len(planner.requests) != 1 {
				t.Fatalf("planner requests=%#v want one request", planner.requests)
			}
			got := make([]string, len(planner.requests[0].InvocationCapabilities))
			for index := range planner.requests[0].InvocationCapabilities {
				got[index] = planner.requests[0].InvocationCapabilities[index].CapabilityID
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("planner requests=%#v want capabilities=%v", planner.requests, test.want)
			}
		})
	}
}

func TestModelExecutionLimitsRejectBeforeFreezeAttemptAndNetwork(t *testing.T) {
	tests := []struct {
		name      string
		operation modeloperation.ID
		path      string
		body      string
		wantCode  string
		editPlan  func(*modelrouting.Plan)
		handle    func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{
			name:      "chat explicit output exceeds public output",
			operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions",
			body:     `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":8193}`,
			wantCode: "validation_error.output_limit_exceeded", handle: (*Relay).HandleChatCompletion,
		},
		{
			name:      "chat estimate exceeds every effective route context",
			operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions",
			body:     `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":1}`,
			wantCode: "validation_error.context_length_exceeded", handle: (*Relay).HandleChatCompletion,
			editPlan: func(plan *modelrouting.Plan) { plan.Routes[0].MaxContext = 1 },
		},
		{
			name:      "K3 invalid reasoning effort",
			operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions",
			body:     `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"medium","max_completion_tokens":128}`,
			wantCode: "validation_error.model_execution_profile", handle: (*Relay).HandleChatCompletion,
			editPlan: func(plan *modelrouting.Plan) {
				setExecutionTestProfile(t, plan, "kimi.k3.api", "1.1.0")
			},
		},
		{
			name:      "embeddings input exceeds every effective route context",
			operation: modeloperation.IDEmbeddings, path: "/v1/embeddings",
			body:     `{"model":"public-model","input":"hello world"}`,
			wantCode: "validation_error.context_length_exceeded", handle: (*Relay).HandleEmbeddings,
			editPlan: func(plan *modelrouting.Plan) { plan.Routes[0].MaxContext = 1 },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, test.operation, "https://provider.test", encKey, nil)
			if test.editPlan != nil {
				test.editPlan(&plan)
			}
			planner := &executionPlannerStub{plan: plan}
			coordinator := &executionCoordinatorStub{}
			var freezes, legacyAttempts, network atomic.Int64
			billingStub := &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					freezes.Add(1)
					return &billing.FreezeResult{Success: true}, nil
				},
				beginAttemptFn: func(context.Context, billing.ProviderAttemptStart) error {
					legacyAttempts.Add(1)
					return nil
				},
			}
			relay := NewRelay(executionTestDB(), nil, nil, billingStub, nil, encKey)
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				network.Add(1)
				return nil, errors.New("unexpected provider dispatch")
			})}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
				t.Fatalf("set runtime: %v", err)
			}
			recorder := httptest.NewRecorder()
			test.handle(relay, recorder, executionRequest(http.MethodPost, test.path, "req-limit-"+strings.ReplaceAll(test.name, " ", "-"), test.body))
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("limit rejection contract mismatch: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if freezes.Load() != 0 || legacyAttempts.Load() != 0 || coordinator.authorizeCount.Load() != 0 || network.Load() != 0 {
				t.Fatalf("limit rejection crossed pre-dispatch boundary: freeze=%d legacy_attempt=%d authorize=%d network=%d",
					freezes.Load(), legacyAttempts.Load(), coordinator.authorizeCount.Load(), network.Load())
			}
		})
	}
}

func TestModelExecutionDefaultMaxOutputIsNotAHardRequestLimit(t *testing.T) {
	plan := modelrouting.Plan{PublicContextLimit: 128000, PublicOutputLimit: 8192}
	route := modelrouting.Route{MaxContext: 128000, DefaultMaxOutput: 1}
	explicitOutput := 4096
	limits := modelExecutionRequestLimits{
		EstimatedInputTokens: 100, EstimatedOutputTokens: explicitOutput, ExplicitOutputTokens: &explicitOutput,
	}
	if err := limits.validatePublic(plan); err != nil || !limits.routeFits(plan, route) {
		t.Fatalf("route default output was incorrectly treated as a hard maximum: err=%v fits=%t", err, limits.routeFits(plan, route))
	}
}

func TestModelExecutionDriftAfterFreezeCancelsWithoutDispatchForChatAndEmbeddings(t *testing.T) {
	tests := []struct {
		name      string
		operation modeloperation.ID
		path      string
		body      string
		handle    func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{name: "chat", operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions", body: `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`, handle: (*Relay).HandleChatCompletion},
		{name: "embeddings", operation: modeloperation.IDEmbeddings, path: "/v1/embeddings", body: `{"model":"public-model","input":"hello"}`, handle: (*Relay).HandleEmbeddings},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			var network atomic.Int64
			plan := executionTestPlan(t, modelrouting.ModeCanonical, test.operation, "https://provider.test", encKey, nil)
			planner := &executionPlannerStub{plan: plan}
			coordinator := &executionCoordinatorStub{}
			var freezes, cancels, legacyBegins atomic.Int64
			billingStub := &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					freezes.Add(1)
					return &billing.FreezeResult{Success: true, AmountMicros: 1}, nil
				},
				cancelFn:       func(context.Context, string, int64) error { cancels.Add(1); return nil },
				beginAttemptFn: func(context.Context, billing.ProviderAttemptStart) error { legacyBegins.Add(1); return nil },
			}
			relay := NewRelay(executionTestDB(), nil, nil, billingStub, nil, encKey)
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				network.Add(1)
				return nil, errors.New("network must not be reached")
			})}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
				t.Fatalf("set runtime: %v", err)
			}
			recorder := httptest.NewRecorder()
			test.handle(relay, recorder, executionRequest(http.MethodPost, test.path, "req-drift-"+test.name, test.body))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if freezes.Load() != 1 || cancels.Load() != 1 || coordinator.authorizeCount.Load() != 1 || network.Load() != 0 || legacyBegins.Load() != 0 {
				t.Fatalf("unexpected boundary counts freeze=%d cancel=%d authorize=%d network=%d legacy_begin=%d", freezes.Load(), cancels.Load(), coordinator.authorizeCount.Load(), network.Load(), legacyBegins.Load())
			}
		})
	}
}

func TestModelExecutionHealthyAppliedProjectionFailureReleasesAndCancelsWithoutDispatch(t *testing.T) {
	tests := []struct {
		name      string
		operation modeloperation.ID
		path      string
		body      string
		handle    func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{name: "chat", operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions", body: `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`, handle: (*Relay).HandleChatCompletion},
		{name: "embeddings", operation: modeloperation.IDEmbeddings, path: "/v1/embeddings", body: `{"model":"public-model","input":"hello"}`, handle: (*Relay).HandleEmbeddings},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, test.operation, "https://provider.test", encKey, nil)
			if plan.Routes[0].RequiresProbe || plan.Routes[0].Evidence.RequiresProbe {
				t.Fatal("fixture must exercise the healthy, non-probe release path")
			}
			coordinator := &executionCoordinatorStub{
				authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
					invalid := route.Evidence
					invalid.ContractVersion = ""
					return modelexecution.Decision{
						Status: modelexecution.Applied, Evidence: invalid,
						DispatchOwnerToken: "healthy-release-owner",
						ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID},
					}, nil
				},
				releaseFn: func(decision modelexecution.Decision) (modelexecution.HealthResult, error) {
					if decision.Evidence.RequiresProbe {
						t.Fatal("healthy release was incorrectly converted into a probe-only operation")
					}
					return modelexecution.HealthResult{Status: modelexecution.Applied}, nil
				},
			}
			var freezes, cancels, network atomic.Int64
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					freezes.Add(1)
					return &billing.FreezeResult{Success: true, AmountMicros: 1}, nil
				},
				cancelFn: func(context.Context, string, int64) error { cancels.Add(1); return nil },
			}, nil, encKey)
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				network.Add(1)
				return nil, errors.New("unexpected provider dispatch")
			})}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, coordinator, routingpolicy.Default()); err != nil {
				t.Fatalf("set runtime: %v", err)
			}
			recorder := httptest.NewRecorder()
			test.handle(relay, recorder, executionRequest(http.MethodPost, test.path, "req-healthy-release-"+test.name, test.body))
			if recorder.Code != http.StatusServiceUnavailable || freezes.Load() != 1 || coordinator.authorizeCount.Load() != 1 || coordinator.releaseCount.Load() != 1 || cancels.Load() != 1 || network.Load() != 0 {
				t.Fatalf("healthy projection failure did not release/cancel cleanly: status=%d freeze=%d authorize=%d release=%d cancel=%d network=%d body=%s",
					recorder.Code, freezes.Load(), coordinator.authorizeCount.Load(), coordinator.releaseCount.Load(), cancels.Load(), network.Load(), recorder.Body.String())
			}
		})
	}
}

func TestModelExecutionAppliedCanonicalDispatchesChatAndEmbeddingsWithExactProjection(t *testing.T) {
	tests := []struct {
		name         string
		operation    modeloperation.ID
		path         string
		body         string
		responseBody string
		handle       func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{name: "chat", operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions", body: `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`, responseBody: `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, handle: (*Relay).HandleChatCompletion},
		{name: "embeddings", operation: modeloperation.IDEmbeddings, path: "/v1/embeddings", body: `{"model":"public-model","input":"hello"}`, responseBody: `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"upstream-model","usage":{"prompt_tokens":1,"total_tokens":1}}`, handle: (*Relay).HandleEmbeddings},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			var network atomic.Int64
			plan := executionTestPlan(t, modelrouting.ModeCanonical, test.operation, "https://provider.test", encKey, nil)
			planner := &executionPlannerStub{plan: plan}
			coordinator := &executionCoordinatorStub{}
			var identity modelexecution.AttemptIdentity
			coordinator.authorizeFn = func(_ modelrouting.Plan, route modelrouting.Route, current modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
				identity = current
				digest, err := executionevidence.Digest(route.Evidence)
				if err != nil {
					return modelexecution.Decision{}, err
				}
				return modelexecution.Decision{
					Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
					ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: current.OrgID, RequestID: current.RequestID, AttemptID: current.AttemptID},
				}, nil
			}
			var legacyBegins atomic.Int64
			billingStub := &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					return &billing.FreezeResult{Success: true, AmountMicros: 1, PricingVersion: "price-v1"}, nil
				},
				beginAttemptFn: func(context.Context, billing.ProviderAttemptStart) error { legacyBegins.Add(1); return nil },
			}
			relay := NewRelay(executionTestDB(), nil, nil, billingStub, nil, encKey)
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				network.Add(1)
				return &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(strings.NewReader(test.responseBody)),
				}, nil
			})}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
				t.Fatalf("set runtime: %v", err)
			}
			recorder := httptest.NewRecorder()
			test.handle(relay, recorder, executionRequest(http.MethodPost, test.path, "req-applied-"+test.name, test.body))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if identity.Endpoint != test.path || identity.AttemptID == "" || network.Load() != 1 || legacyBegins.Load() != 0 || coordinator.successCount.Load() != 1 {
				t.Fatalf("unexpected applied boundary identity=%+v network=%d legacy_begin=%d success=%d", identity, network.Load(), legacyBegins.Load(), coordinator.successCount.Load())
			}
			compat := compatibilityRouteFromExecutionPlan(plan, plan.Routes[0])
			if compat.Channel.ID != 0 || compat.ChannelKey.ID != 0 {
				t.Fatalf("canonical adapter view invented legacy IDs: %+v", compat)
			}
			if compat.ExecutionProfileID != plan.ExecutionProfileID ||
				compat.ExecutionProfileVersion != plan.ExecutionProfileVersion ||
				compat.ReasoningProfile != plan.ReasoningProfile ||
				compat.CacheUsageProfile != plan.CacheUsageProfile {
				t.Fatalf("canonical adapter view lost model profile authority: %+v", compat)
			}
		})
	}
}

func TestModelExecutionNonAppliedSuccessHealthFailsClosedBeforeDelivery(t *testing.T) {
	tests := []struct {
		name         string
		operation    modeloperation.ID
		path         string
		body         string
		responseBody string
		handle       func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{name: "chat", operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions", body: `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`, responseBody: `{"id":"chatcmpl-health","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, handle: (*Relay).HandleChatCompletion},
		{name: "embeddings", operation: modeloperation.IDEmbeddings, path: "/v1/embeddings", body: `{"model":"public-model","input":"hello"}`, responseBody: `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"upstream-model","usage":{"prompt_tokens":1,"total_tokens":1}}`, handle: (*Relay).HandleEmbeddings},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, test.operation, "https://provider.test", encKey, nil)
			coordinator := &executionCoordinatorStub{
				authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
					digest, err := executionevidence.Digest(route.Evidence)
					return modelexecution.Decision{
						Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
						DispatchOwnerToken: "health-owner",
						ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID},
					}, err
				},
				successFn: func(modelexecution.Decision) (modelexecution.HealthResult, error) {
					return modelexecution.HealthResult{Status: modelexecution.Stale}, nil
				},
			}
			var network, holds, deliveries atomic.Int64
			billingStub := &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					return &billing.FreezeResult{Success: true, AmountMicros: 1, PricingVersion: "price-v1"}, nil
				},
				holdOutputFn: func(_ context.Context, _ billing.SettleRequest, reason string) error {
					if reason != billing.OutputHoldReasonProviderAttemptUnresolved {
						t.Fatalf("unexpected hold reason %q", reason)
					}
					holds.Add(1)
					return nil
				},
				prepareDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
					deliveries.Add(1)
					return &billing.PrepareDeliveryResult{Funded: true}, nil
				},
			}
			relay := NewRelay(executionTestDB(), nil, nil, billingStub, nil, encKey)
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				network.Add(1)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(test.responseBody))}, nil
			})}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, coordinator, routingpolicy.Default()); err != nil {
				t.Fatalf("set runtime: %v", err)
			}
			recorder := httptest.NewRecorder()
			test.handle(relay, recorder, executionRequest(http.MethodPost, test.path, "req-health-stale-"+test.name, test.body))
			if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"code":"server_error.service_unavailable"`) {
				t.Fatalf("non-applied health was swallowed: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if network.Load() != 1 || coordinator.successCount.Load() != 1 || holds.Load() != 1 || deliveries.Load() != 0 {
				t.Fatalf("unexpected health-failure boundary: network=%d success=%d holds=%d deliveries=%d",
					network.Load(), coordinator.successCount.Load(), holds.Load(), deliveries.Load())
			}
		})
	}
}

func TestModelExecutionProviderFailureHealthMappingForChatAndEmbeddings(t *testing.T) {
	handlers := []struct {
		name      string
		operation modeloperation.ID
		path      string
		body      string
		handle    func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{name: "chat", operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions", body: `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`, handle: (*Relay).HandleChatCompletion},
		{name: "embeddings", operation: modeloperation.IDEmbeddings, path: "/v1/embeddings", body: `{"model":"public-model","input":"hello"}`, handle: (*Relay).HandleEmbeddings},
	}
	failures := []struct {
		name       string
		statusCode int
		transport  error
		want       executionHealthAction
	}{
		{name: "provider", statusCode: http.StatusInternalServerError, want: executionHealthProviderFailure},
		{name: "network", transport: io.ErrUnexpectedEOF, want: executionHealthProviderFailure},
		{name: "credential_auth", statusCode: http.StatusUnauthorized, want: executionHealthCredentialAuthFailure},
		{name: "credential_rate_limit", statusCode: http.StatusTooManyRequests, want: executionHealthCredentialRateLimited},
	}
	for _, handler := range handlers {
		for _, failure := range failures {
			t.Run(handler.name+"/"+failure.name, func(t *testing.T) {
				encKey := []byte("0123456789abcdef0123456789abcdef")
				plan := executionTestPlan(t, modelrouting.ModeCanonical, handler.operation, "https://provider.test", encKey, nil)
				coordinator := &executionCoordinatorStub{authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
					digest, err := executionevidence.Digest(route.Evidence)
					return modelexecution.Decision{
						Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
						DispatchOwnerToken: "failure-owner",
						ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID},
					}, err
				}}
				relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					return &billing.FreezeResult{Success: true, AmountMicros: 1}, nil
				}}, nil, encKey)
				relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
					if failure.transport != nil {
						return nil, failure.transport
					}
					return &http.Response{
						StatusCode: failure.statusCode, Header: http.Header{"Content-Type": []string{"application/json"}},
						Body: io.NopCloser(strings.NewReader(`{"error":{"message":"fixture"}}`)),
					}, nil
				})}
				if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, coordinator, routingpolicy.Default()); err != nil {
					t.Fatalf("set runtime: %v", err)
				}
				recorder := httptest.NewRecorder()
				handler.handle(relay, recorder, executionRequest(http.MethodPost, handler.path, "req-health-map-"+handler.name+"-"+failure.name, handler.body))
				got := map[executionHealthAction]int64{
					executionHealthProviderFailure:       coordinator.providerCount.Load(),
					executionHealthCredentialAuthFailure: coordinator.authCount.Load(),
					executionHealthCredentialRateLimited: coordinator.rateCount.Load(),
				}
				if got[failure.want] != 1 || got[executionHealthProviderFailure]+got[executionHealthCredentialAuthFailure]+got[executionHealthCredentialRateLimited] != 1 {
					t.Fatalf("wrong health mapping for status=%d transport=%v: %#v response=%d %s",
						failure.statusCode, failure.transport, got, recorder.Code, recorder.Body.String())
				}
			})
		}
	}
}

func TestModelExecutionNonAppliedTransportHealthFailsClosedForChatAndEmbeddings(t *testing.T) {
	tests := []struct {
		name      string
		operation modeloperation.ID
		path      string
		body      string
		handle    func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{name: "chat", operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions", body: `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`, handle: (*Relay).HandleChatCompletion},
		{name: "embeddings", operation: modeloperation.IDEmbeddings, path: "/v1/embeddings", body: `{"model":"public-model","input":"hello"}`, handle: (*Relay).HandleEmbeddings},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, test.operation, "https://provider.test", encKey, nil)
			coordinator := &executionCoordinatorStub{
				authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
					digest, err := executionevidence.Digest(route.Evidence)
					return modelexecution.Decision{
						Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
						DispatchOwnerToken: "transport-health-owner",
						ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID},
					}, err
				},
				providerFn: func(modelexecution.Decision) (modelexecution.HealthResult, error) {
					return modelexecution.HealthResult{Status: modelexecution.Stale}, nil
				},
			}
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
				return &billing.FreezeResult{Success: true, AmountMicros: 1}, nil
			}}, nil, encKey)
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, io.ErrUnexpectedEOF
			})}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, coordinator, routingpolicy.Default()); err != nil {
				t.Fatalf("set runtime: %v", err)
			}
			recorder := httptest.NewRecorder()
			test.handle(relay, recorder, executionRequest(http.MethodPost, test.path, "req-transport-health-stale-"+test.name, test.body))
			if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" ||
				!strings.Contains(recorder.Body.String(), `"code":"server_error.service_unavailable"`) {
				t.Fatalf("non-applied transport health was swallowed: status=%d retry=%q body=%s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
			}
			if coordinator.providerCount.Load() != 1 || coordinator.successCount.Load() != 0 {
				t.Fatalf("wrong terminal health action: provider=%d success=%d", coordinator.providerCount.Load(), coordinator.successCount.Load())
			}
		})
	}
}

func TestModelExecutionInjectedLegacyAuthorityUsesPlannerCoordinatorForChatAndEmbeddings(t *testing.T) {
	tests := []struct {
		name         string
		operation    modeloperation.ID
		path         string
		body         string
		responseBody string
		handle       func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{name: "chat", operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions", body: `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`, responseBody: `{"id":"chatcmpl-legacy","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, handle: (*Relay).HandleChatCompletion},
		{name: "embeddings", operation: modeloperation.IDEmbeddings, path: "/v1/embeddings", body: `{"model":"public-model","input":"hello"}`, responseBody: `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"upstream-model","usage":{"prompt_tokens":1,"total_tokens":1}}`, handle: (*Relay).HandleEmbeddings},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeLegacy, test.operation, "https://provider.test", encKey, nil)
			// Legacy authority deliberately supports channels created before a
			// model_registry row existed. In that compatibility state the plan has
			// no public limits, so Chat must supply an explicit output ceiling while
			// retaining the channel max_context contract.
			plan.PublicContextLimit = 0
			plan.PublicOutputLimit = 0
			if plan.Routes[0].Evidence.ModelOperationID != string(test.operation) ||
				plan.Routes[0].Evidence.ModelOperationID == "legacy.any.v1" {
				t.Fatalf("legacy adapter execution contract lost the exact wire operation: %+v", plan.Routes[0].Evidence)
			}
			planner := &executionPlannerStub{plan: plan}
			coordinator := &executionCoordinatorStub{authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
				digest, err := executionevidence.Digest(route.Evidence)
				return modelexecution.Decision{
					Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
					DispatchOwnerToken: "legacy-dispatch-owner",
					ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID},
				}, err
			}}
			var network, legacyBegin atomic.Int64
			billingStub := &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					return &billing.FreezeResult{Success: true, AmountMicros: 1, PricingVersion: "price-v1"}, nil
				},
				beginAttemptFn: func(context.Context, billing.ProviderAttemptStart) error {
					legacyBegin.Add(1)
					return nil
				},
			}
			relay := NewRelay(p6f11RelayDB(), nil, nil, billingStub, nil, encKey)
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				network.Add(1)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(test.responseBody))}, nil
			})}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeLegacy, planner, coordinator, routingpolicy.Default()); err != nil {
				t.Fatalf("set runtime: %v", err)
			}
			recorder := httptest.NewRecorder()
			test.handle(relay, recorder, executionRequest(http.MethodPost, test.path, "req-legacy-"+test.name, test.body))
			if recorder.Code != http.StatusOK || network.Load() != 1 || legacyBegin.Load() != 0 ||
				coordinator.authorizeCount.Load() != 1 || coordinator.successCount.Load() != 1 {
				t.Fatalf("legacy production path bypassed planner/coordinator: status=%d network=%d legacy_begin=%d authorize=%d success=%d body=%s",
					recorder.Code, network.Load(), legacyBegin.Load(), coordinator.authorizeCount.Load(), coordinator.successCount.Load(), recorder.Body.String())
			}
		})
	}
}

func TestModelExecutionVerifyDecisionBindsExactCompactProjection(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeVerify, modeloperation.IDChatCompletions, "https://provider.invalid", encKey, nil)
	coordinator := &executionCoordinatorStub{authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
		digest, err := executionevidence.Digest(route.Evidence)
		return modelexecution.Decision{
			Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
			ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID},
		}, err
	}}
	relay := &Relay{modelExecution: &modelExecutionRuntime{mode: modelrouting.ModeVerify, coordinator: coordinator}}
	batch := &preparedModelExecutionBatch{plan: plan, routes: []preparedModelExecutionRoute{{route: plan.Routes[0]}}}
	ctx := executionRequest(http.MethodPost, "/v1/chat/completions", "req-verify", `{}`).Context()
	ctx, status, err := relay.authorizePreparedModelRoute(ctx, batch, &batch.routes[0], 1, 1, adapters.EndpointChatCompletions)
	if err != nil || status != modelexecution.Applied {
		t.Fatalf("authorize verify: status=%s err=%v", status, err)
	}
	projection, ok := executionProjectionFromContext(ctx, 1, "req-verify", attemptIDFromContext(ctx))
	if !ok || projection.Source != executionprojection.SourceProviderAttempt || projection.Compact == nil ||
		projection.Compact.AuthorityMode != executionevidence.ModeVerify || projection.Compact.LegacyChannelID != 21 || projection.Compact.DeploymentID != 12 {
		t.Fatalf("unexpected verify projection: %#v ok=%t", projection, ok)
	}
	relay.db = &testutil.MockDB{ExecutionProjectionFn: func(context.Context, executionprojection.ResolveInput) (executionprojection.Projection, error) {
		t.Fatal("coordinator-bound projection must not re-read provider-attempt evidence")
		return executionprojection.Projection{}, nil
	}}
	trace, err := relay.prepareTraceMaterialization(ctx, traceOutboxPayload{
		OrgID: 1, RequestID: "req-verify", AttemptID: attemptIDFromContext(ctx),
		ProviderAttemptStage: string(providerAttemptStageDurable), EventType: "routing.route_selected",
		SourceEventHash: strings.Repeat("b", 64),
	})
	if err != nil || trace.RouteProjection.Source != executionprojection.SourceProviderAttempt ||
		trace.RouteProjection.Compact == nil || trace.RouteProjection.Compact.EvidenceHash != projection.Compact.EvidenceHash {
		t.Fatalf("durable route trace did not use exact in-context projection: trace=%+v err=%v", trace, err)
	}
}

func TestModelExecutionLegacyAndVerifyPreserveResponseCacheEpochIdentity(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	for _, mode := range []modelrouting.Mode{modelrouting.ModeLegacy, modelrouting.ModeVerify} {
		t.Run(string(mode), func(t *testing.T) {
			plan := executionTestPlan(t, mode, modeloperation.IDChatCompletions, "https://provider.invalid", encKey, nil)
			route := compatibilityRouteFromExecutionPlan(plan, plan.Routes[0])
			identity := (&Relay{}).responseCacheIdentity(
				context.Background(), &models.ApiKey{ID: 2, OrgID: 1, AuthEpoch: 1}, invocationopt.Policy{},
				&route, "public-model", "price-v1",
			)
			if identity.ChannelID != 21 || identity.CredentialID != 22 ||
				identity.ChannelRoutingEpoch != 8 || identity.ChannelCircuitEpoch != 9 || identity.CredentialEpoch != 10 {
				t.Fatalf("%s compatibility cache identity lost immutable epochs: %+v", mode, identity)
			}
		})
	}

	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://provider.invalid", encKey, nil)
	route := compatibilityRouteFromExecutionPlan(plan, plan.Routes[0])
	if route.Channel.ID != 0 || route.Channel.RoutingEpoch != 0 || route.Channel.CircuitEpoch != 0 ||
		route.ChannelKey.ID != 0 || route.ChannelKey.StateEpoch != 0 {
		t.Fatalf("canonical adapter view invented legacy cache identity: %+v", route)
	}
}

func TestModelExecutionReleaseAndHealthMapping(t *testing.T) {
	coordinator := &executionCoordinatorStub{authorizeFn: func(_ modelrouting.Plan, _ modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
		return modelexecution.Decision{
			Status:             modelexecution.Applied,
			ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID},
		}, nil
	}}
	relay := &Relay{modelExecution: &modelExecutionRuntime{mode: modelrouting.ModeCanonical, coordinator: coordinator, leaseInterval: time.Millisecond}}
	batch := &preparedModelExecutionBatch{plan: modelrouting.Plan{Routes: []modelrouting.Route{{}}}, routes: []preparedModelExecutionRoute{{}}}
	ctx := executionRequest(http.MethodPost, "/v1/chat/completions", "req-release", `{}`).Context()
	if _, _, err := relay.authorizePreparedModelRoute(ctx, batch, &batch.routes[0], 1, 1, adapters.EndpointChatCompletions); err == nil || coordinator.releaseCount.Load() != 1 {
		t.Fatalf("invalid applied evidence must release undispatched authority: err=%v releases=%d", err, coordinator.releaseCount.Load())
	}

	decision := modelexecution.Decision{Status: modelexecution.Applied}
	healthCtx := contextWithExecutionDecision(context.Background(), decision)
	route := &RouteResult{}
	for _, action := range []executionHealthAction{
		executionHealthSuccess, executionHealthProviderFailure,
		executionHealthCredentialAuthFailure, executionHealthCredentialRateLimited,
	} {
		if err := relay.applyRouteHealth(healthCtx, route, action); err != nil {
			t.Fatalf("health action %d: %v", action, err)
		}
	}
	if coordinator.successCount.Load() != 1 || coordinator.providerCount.Load() != 1 || coordinator.authCount.Load() != 1 || coordinator.rateCount.Load() != 1 {
		t.Fatalf("health mapping mismatch: success=%d provider=%d auth=%d rate=%d", coordinator.successCount.Load(), coordinator.providerCount.Load(), coordinator.authCount.Load(), coordinator.rateCount.Load())
	}
}

func TestModelExecutionRejectsUnknownAuthorizationStatus(t *testing.T) {
	coordinator := &executionCoordinatorStub{authorizeFn: func(modelrouting.Plan, modelrouting.Route, modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
		return modelexecution.Decision{Status: modelexecution.Status("unexpected")}, nil
	}}
	relay := &Relay{modelExecution: &modelExecutionRuntime{mode: modelrouting.ModeCanonical, coordinator: coordinator}}
	batch := &preparedModelExecutionBatch{plan: modelrouting.Plan{Routes: []modelrouting.Route{{}}}, routes: []preparedModelExecutionRoute{{}}}
	ctx := executionRequest(http.MethodPost, "/v1/chat/completions", "req-unknown-authorize-status", `{}`).Context()
	_, status, err := relay.authorizePreparedModelRoute(ctx, batch, &batch.routes[0], 1, 1, adapters.EndpointChatCompletions)
	if status != modelexecution.Status("unexpected") || err == nil || coordinator.releaseCount.Load() != 0 {
		t.Fatalf("unknown authorization result must fail closed without pretending to release: status=%q err=%v releases=%d", status, err, coordinator.releaseCount.Load())
	}
}

func TestModelExecutionHalfOpenLeaseIsRenewed(t *testing.T) {
	coordinator := &executionCoordinatorStub{}
	relay := &Relay{modelExecution: &modelExecutionRuntime{coordinator: coordinator, leaseInterval: time.Millisecond}}
	ctx := contextWithExecutionDecision(context.Background(), modelexecution.Decision{
		Status: modelexecution.Applied, Evidence: executionevidence.EvidenceV1{RequiresProbe: true},
	})
	_, stop := relay.maintainExecutionLease(ctx, nil)
	deadline := time.After(250 * time.Millisecond)
	for coordinator.renewCount.Load() == 0 {
		select {
		case <-deadline:
			stop()
			t.Fatal("half-open lease was not renewed")
		case <-time.After(time.Millisecond):
		}
	}
	stop()
}

func TestModelExecutionStreamHealthIsRecordedOnlyAtProviderTerminalState(t *testing.T) {
	tests := []struct {
		name                   string
		segments               [][]byte
		successStatus          modelexecution.Status
		wantSuccess            int64
		wantProviderErr        int64
		wantHealthAuthoritySSE bool
	}{
		{
			name: "complete valid stream",
			segments: [][]byte{
				[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"),
				[]byte("data: [DONE]\n\n"),
			},
			wantSuccess: 1,
		},
		{
			name: "valid frame followed by invalid frame",
			segments: [][]byte{
				[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"),
				[]byte("data: {not-json}\n\n"),
			},
			wantProviderErr: 1,
		},
		{
			name: "complete stream with stale success fence",
			segments: [][]byte{
				[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"),
				[]byte("data: [DONE]\n\n"),
			},
			successStatus:          modelexecution.Stale,
			wantSuccess:            1,
			wantHealthAuthoritySSE: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			now := time.Now().UTC().Truncate(time.Microsecond)
			capabilities := []executionevidence.CapabilityEntry{{
				CapabilityID: modelCapabilityStreaming, SupportState: "enabled",
				EvidenceState: "production_approved", EvidenceRef: "fixture://streaming",
				VerifiedAt: &now, Revision: 1,
			}}
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://provider.test", encKey, capabilities)
			plan.Routes[0].RequiresProbe = true
			plan.Routes[0].Evidence.RequiresProbe = true
			plan.Routes[0].Evidence.ProbeLeaseTokenHash = "sha256:" + strings.Repeat("a", 64)
			planner := &executionPlannerStub{plan: plan}
			var accepted atomic.Bool
			reader := &delayedSegmentReader{segments: test.segments, delay: 25 * time.Millisecond}
			coordinator := &executionCoordinatorStub{}
			coordinator.authorizeFn = func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
				digest, err := executionevidence.Digest(route.Evidence)
				return modelexecution.Decision{
					Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
					ProbeLeaseToken: "probe-token", DispatchOwnerToken: "dispatch-owner",
					ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID},
				}, err
			}
			coordinator.renewFn = func(modelexecution.Decision) (modelexecution.HealthResult, error) {
				if !accepted.Load() {
					t.Error("probe lease renewed before response_accepted was durably completed")
				}
				return modelexecution.HealthResult{Status: modelexecution.Applied}, nil
			}
			coordinator.successFn = func(modelexecution.Decision) (modelexecution.HealthResult, error) {
				status := test.successStatus
				if status == "" {
					status = modelexecution.Applied
				}
				return modelexecution.HealthResult{Status: status}, nil
			}
			billingStub := &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					return &billing.FreezeResult{Success: true, AmountMicros: 1, PricingVersion: "price-v1"}, nil
				},
				completeAttemptFn: func(_ context.Context, completion billing.ProviderAttemptCompletion) error {
					if completion.Outcome == billing.ProviderAttemptOutcomeResponseAccepted {
						accepted.Store(true)
					}
					return nil
				},
			}
			relay := NewRelay(p6f11RelayDB(), nil, nil, billingStub, nil, encKey)
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
					Body: io.NopCloser(reader),
				}, nil
			})}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
				t.Fatalf("set runtime: %v", err)
			}
			relay.modelExecution.leaseInterval = 2 * time.Millisecond
			recorder := httptest.NewRecorder()
			relay.HandleChatCompletion(recorder, executionRequest(http.MethodPost, "/v1/chat/completions", "req-stream-terminal", `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true}`))
			if !accepted.Load() || coordinator.renewCount.Load() == 0 {
				t.Fatalf("headers/lease ordering missing: accepted=%t renew=%d", accepted.Load(), coordinator.renewCount.Load())
			}
			if coordinator.successCount.Load() != test.wantSuccess || coordinator.providerCount.Load() != test.wantProviderErr {
				t.Fatalf("terminal health mismatch: success=%d provider_failure=%d", coordinator.successCount.Load(), coordinator.providerCount.Load())
			}
			if got := coordinator.successCount.Load() + coordinator.providerCount.Load(); got != 1 {
				t.Fatalf("stream wrote %d terminal provider health outcomes, want exactly one", got)
			}
			hasHealthAuthoritySSE := strings.Contains(recorder.Body.String(), `"code":"stream_error.routing_health_unavailable"`)
			if hasHealthAuthoritySSE != test.wantHealthAuthoritySSE {
				t.Fatalf("health authority terminal SSE mismatch: got=%t body=%s", hasHealthAuthoritySSE, recorder.Body.String())
			}
		})
	}
}

func TestModelExecutionLocalStreamGuardrailTerminationDoesNotFailProvider(t *testing.T) {
	requestID := "req-stream-local-guardrail"
	coordinator := &executionCoordinatorStub{}
	relay := newTestRelay(p6f11RelayDB(), &mockBilling{})
	relay.modelExecution = &modelExecutionRuntime{coordinator: coordinator}
	decision := modelexecution.Decision{Status: modelexecution.Applied}
	ctx := p6f11StreamContext(p6f11StreamGuardrail(t, "block"), requestID)
	ctx = contextWithExecutionDecision(ctx, decision)
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"alice@example.com\"}}]}\n\n" +
		"data: [DONE]\n\n"
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}
	route := &RouteResult{Channel: models.Channel{Name: "openai", Provider: "openai"}}
	recorder := httptest.NewRecorder()
	relay.relayStreamRequest(ctx, recorder, response, route, requestID, 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", nil,
		logger.New(logger.Fields{RequestID: requestID, OrgID: 1}), time.Now(), 1_000, nil, "")
	if coordinator.successCount.Load() != 1 || coordinator.providerCount.Load() != 0 {
		t.Fatalf("local guardrail termination corrupted provider health: success=%d provider_failure=%d", coordinator.successCount.Load(), coordinator.providerCount.Load())
	}
}
