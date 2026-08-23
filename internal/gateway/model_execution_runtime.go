package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

// ModelRoutePlanner is the read-only authority needed by the public gateway.
// The concrete planner owns its Repeatable Read snapshot; Relay does not query
// route tables or reinterpret a plan.
type ModelRoutePlanner interface {
	Plan(context.Context, modelrouting.Request) (modelrouting.Plan, error)
}

// ModelExecutionCoordinator is the last write boundary before provider I/O.
// Only an Applied Authorize result permits a network request.
type ModelExecutionCoordinator interface {
	Authorize(context.Context, modelrouting.Plan, modelrouting.Route, modelexecution.AttemptIdentity) (modelexecution.Decision, error)
	RenewLease(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error)
	ReleaseUndispatched(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error)
	MarkSuccess(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error)
	MarkProviderFailure(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error)
	MarkCredentialAuthFailure(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error)
	MarkCredentialRateLimited(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error)
}

type modelExecutionRuntime struct {
	mode              modelrouting.Mode
	planner           ModelRoutePlanner
	coordinator       ModelExecutionCoordinator
	publicCoordinator publicModelExecutionCoordinator
	registry          *adapters.Registry
	publicRegistry    *endpointprotocol.PublicNonStreamRegistry
	policy            routingpolicy.Config
	leaseInterval     time.Duration
}

// SetModelExecutionRuntime switches Chat and Embeddings to the canonical
// execution invariant. Authority mode is explicit and never falls back.
func (relay *Relay) SetModelExecutionRuntime(
	mode modelrouting.Mode,
	planner ModelRoutePlanner,
	coordinator ModelExecutionCoordinator,
	policy routingpolicy.Config,
) error {
	if relay == nil || planner == nil || coordinator == nil {
		return fmt.Errorf("gateway model execution runtime requires relay, planner, and coordinator")
	}
	switch mode {
	case modelrouting.ModeLegacy, modelrouting.ModeVerify, modelrouting.ModeCanonical:
	default:
		return fmt.Errorf("gateway model execution authority must be exactly legacy, verify, or canonical, got %q", mode)
	}
	if err := policy.Validate(); err != nil {
		return fmt.Errorf("gateway model execution routing policy: %w", err)
	}
	leaseInterval := time.Duration(policy.HalfOpenProbeLeaseSeconds) * time.Second / 3
	if leaseInterval < time.Second {
		leaseInterval = time.Second
	}
	publicCoordinator, _ := coordinator.(publicModelExecutionCoordinator)
	relay.modelExecution = &modelExecutionRuntime{
		mode: mode, planner: planner, coordinator: coordinator, publicCoordinator: publicCoordinator,
		registry: adapters.DefaultRegistry(), publicRegistry: endpointprotocol.NewPublicNonStreamRegistry(),
		policy: policy, leaseInterval: leaseInterval,
	}
	return nil
}

type preparedModelExecutionRoute struct {
	route           modelrouting.Route
	compat          RouteResult
	adapter         adapters.Adapter
	upstreamRequest *http.Request
}

func (relay *Relay) providerClient(timeout time.Duration) adapters.HTTPDoer {
	if relay != nil && relay.providerHTTPDoer != nil {
		return relay.providerHTTPDoer
	}
	return &http.Client{Timeout: timeout}
}

type preparedModelExecutionBatch struct {
	plan            modelrouting.Plan
	routes          []preparedModelExecutionRoute
	limits          modelExecutionRequestLimits
	profileEvidence modelExecutionProfileEvidence
}

var (
	errModelExecutionOutputLimitExceeded   = errors.New("model execution output limit exceeded")
	errModelExecutionContextLimitExceeded  = errors.New("model execution context limit exceeded")
	errModelExecutionOutputLimitRequired   = errors.New("model execution output limit is required")
	errModelExecutionProfileRequestInvalid = errors.New("model execution profile request is invalid")
)

// legacyImplicitOutputLimitV1 preserves the pre-catalog compatibility
// reservation without pretending a route's DefaultMaxOutput is a maximum. It
// becomes a real hard bound only because the legacy adapter contract injects
// the same value upstream before Freeze and dispatch.
const legacyImplicitOutputLimitV1 = 1000

// modelExecutionRequestLimits binds admission accounting to the exact provider
// request built before Freeze. When the caller omits an output limit,
// ProviderOutputLimit is injected into every compatible provider request so the
// reserved amount is also an upstream liability cap.
type modelExecutionRequestLimits struct {
	EstimatedInputTokens  int
	EstimatedOutputTokens int
	ExplicitOutputTokens  *int
	ProviderOutputLimit   int
	EstimateVersion       string
}

func (limits modelExecutionRequestLimits) estimatedTotal() int64 {
	input := max(limits.EstimatedInputTokens, 0)
	output := max(limits.EstimatedOutputTokens, 0)
	return int64(input) + int64(output)
}

func (limits modelExecutionRequestLimits) validatePublic(plan modelrouting.Plan) error {
	if limits.ExplicitOutputTokens == nil && limits.EstimatedOutputTokens <= 0 {
		return errModelExecutionOutputLimitRequired
	}
	if limits.ExplicitOutputTokens != nil && plan.PublicOutputLimit > 0 &&
		*limits.ExplicitOutputTokens > plan.PublicOutputLimit {
		return errModelExecutionOutputLimitExceeded
	}
	if plan.PublicContextLimit > 0 && limits.estimatedTotal() > int64(plan.PublicContextLimit) {
		return errModelExecutionContextLimitExceeded
	}
	return nil
}

func (limits modelExecutionRequestLimits) resolveOutputReservation(plan modelrouting.Plan, requiredCapabilities []string) (modelExecutionRequestLimits, error) {
	resolved := limits
	if resolved.ExplicitOutputTokens != nil {
		resolved.EstimatedOutputTokens = *resolved.ExplicitOutputTokens
	} else {
		resolved.ProviderOutputLimit = plan.PublicOutputLimit
		if resolved.ProviderOutputLimit <= 0 && plan.AuthorityMode == modelrouting.ModeLegacy {
			resolved.ProviderOutputLimit = serverDerivedLegacyOutputLimit(plan, requiredCapabilities)
		}
		if resolved.ProviderOutputLimit <= 0 {
			return modelExecutionRequestLimits{}, errModelExecutionOutputLimitRequired
		}
		resolved.EstimatedOutputTokens = resolved.ProviderOutputLimit
	}
	if err := resolved.validatePublic(plan); err != nil {
		return modelExecutionRequestLimits{}, err
	}
	return resolved, nil
}

func serverDerivedLegacyOutputLimit(plan modelrouting.Plan, requiredCapabilities []string) int {
	for _, route := range plan.Routes {
		if _, supported := providerOutputLimitField(plan, route, requiredCapabilities); supported {
			return legacyImplicitOutputLimitV1
		}
	}
	return 0
}

// providerOutputLimitField is a wire-contract decision, never a model-name
// heuristic. A verified model protocol profile may bind a provider-specific
// field; otherwise the route's execution contract supplies only the historical
// non-reasoning OpenAI-compatible max_tokens behavior.
func providerOutputLimitField(plan modelrouting.Plan, route modelrouting.Route, requiredCapabilities []string) (string, bool) {
	adapterType := adapters.NormalizeType(route.AdapterType)
	if adapterType == adapters.TypeAnthropicNative {
		// AnthropicNative parses the downstream limit and deterministically emits
		// native max_tokens; this is an adapter-owned contract.
		return "max_tokens", true
	}
	if adapterType != adapters.TypeOpenAICompatible && adapterType != adapters.TypeAzureOpenAI {
		return "", false
	}
	// MiniMax's current OpenAI-compatible contract recommends
	// max_completion_tokens for M3 and M2.x, including reasoning requests. The
	// immutable model profile is the authority; aliases and provider labels are
	// deliberately ignored here.
	if strings.TrimSpace(plan.ModelProtocolProfile) == "protocol.minimax-openai-chat.v2" {
		return "max_completion_tokens", true
	}
	if slices.Contains(requiredCapabilities, modelCapabilityReasoning) {
		return "", false
	}

	profileID := strings.TrimSpace(route.ProtocolProfileID)
	profileVersion := strings.TrimSpace(route.ProtocolProfileVersion)
	switch profileID + "\x00" + profileVersion {
	case "openai.chat_completions\x001.0.0",
		"openai-compatible.v1\x00v1",
		"openai.chat.text\x00v1",
		"integration.openai.chat\x001.0.0":
		return "max_tokens", true
	}
	// A legacy execution may retain a probe-profile label for compatibility.
	// That label is never treated as a wire profile. The immutable execution
	// contract source plus the concrete adapter contract is the authority.
	if route.Evidence.ExecutionContractSource == executionevidence.ContractSourceLegacyAdapter {
		return "max_tokens", true
	}
	return "", false
}

func injectChatOutputLimit(body []byte, field string, limit int) ([]byte, error) {
	if limit <= 0 {
		return nil, errModelExecutionOutputLimitRequired
	}
	if field != "max_tokens" && field != "max_completion_tokens" {
		return nil, errModelExecutionOutputLimitRequired
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return nil, fmt.Errorf("inject provider output limit: chat body must be an object")
	}
	encoded, err := json.Marshal(limit)
	if err != nil {
		return nil, fmt.Errorf("inject provider output limit: %w", err)
	}
	payload[field] = encoded
	otherField := "max_completion_tokens"
	if field == otherField {
		otherField = "max_tokens"
	}
	if other, exists := payload[otherField]; exists && isJSONNull(other) {
		delete(payload, otherField)
	}
	return json.Marshal(payload)
}

func providerRequestBody(request *http.Request) ([]byte, error) {
	if request == nil || request.GetBody == nil {
		return nil, fmt.Errorf("provider request body is not replayable")
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, fmt.Errorf("open provider request body: %w", err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read provider request body: %w", err)
	}
	return data, nil
}

func (limits modelExecutionRequestLimits) routeFits(plan modelrouting.Plan, route modelrouting.Route) bool {
	if route.MaxContext <= 0 {
		return false
	}
	// A pre-catalog legacy model intentionally has no public registry limit; its
	// long-standing channel max_context remains the only hard admission bound.
	// Verify/canonical plans always carry the public limit and take the stricter
	// of the two. This preserves legacy startup compatibility without weakening
	// canonical public-model authority.
	effectiveContext := route.MaxContext
	if plan.PublicContextLimit > 0 {
		effectiveContext = min(plan.PublicContextLimit, route.MaxContext)
	}
	return limits.estimatedTotal() <= int64(effectiveContext)
}

func modelExecutionLimitErrorDetails(err error) (code, message string, ok bool) {
	switch {
	case errors.Is(err, errModelExecutionOutputLimitExceeded):
		return "validation_error.output_limit_exceeded", "Requested output token limit exceeds the model's published limit", true
	case errors.Is(err, errModelExecutionContextLimitExceeded):
		return "validation_error.context_length_exceeded", "Estimated request tokens exceed the model's available context limit", true
	case errors.Is(err, errModelExecutionOutputLimitRequired):
		return "validation_error.output_limit_required", "An explicit output token limit is required because no provider-compatible published output limit is available", true
	case errors.Is(err, errModelExecutionProfileRequestInvalid):
		return "validation_error.model_execution_profile", "Request is incompatible with the selected model execution profile", true
	default:
		return "", "", false
	}
}

func (relay *Relay) prepareModelExecutionRoutes(
	ctx context.Context,
	operation modeloperation.ID,
	endpoint adapters.Endpoint,
	publicModel, requestedModel, requestID string,
	requiredCapabilities []string,
	limits modelExecutionRequestLimits,
	body []byte,
	requestStart time.Time,
) (*preparedModelExecutionBatch, error) {
	runtime := relay.modelExecution
	if runtime == nil || runtime.planner == nil || runtime.registry == nil {
		return nil, fmt.Errorf("model execution runtime is not configured")
	}
	plan, err := runtime.planner.Plan(ctx, modelrouting.Request{
		Mode: runtime.mode, CustomerOrgID: middleware.GetOrgIDFromContext(ctx),
		PublicModel: publicModel, ModelOperationID: string(operation),
		RequiredCapabilities: slices.Clone(requiredCapabilities), SelectionKey: requestID,
	})
	if err != nil {
		return nil, err
	}
	if err := requireModelExecutionDispatchWired(operation, plan); err != nil {
		return nil, err
	}
	profileEvidence := modelExecutionProfileEvidence{}
	if endpoint == adapters.EndpointChatCompletions {
		profileEvidence, err = validateChatExecutionProfileRequest(body, plan)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errModelExecutionProfileRequestInvalid, err)
		}
		body, err = profileEvidence.normalizeProviderRequest(body)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errModelExecutionProfileRequestInvalid, err)
		}
		profileAdmission, admissionErr := estimateChatAdmissionForPlan(body, plan)
		if admissionErr != nil {
			return nil, admissionErr
		}
		limits.EstimatedInputTokens = profileAdmission.InputTokens
		limits.EstimateVersion = profileAdmission.Version
	}
	limits, err = limits.resolveOutputReservation(plan, requiredCapabilities)
	if err != nil {
		return nil, err
	}
	batch := &preparedModelExecutionBatch{plan: plan, limits: limits, profileEvidence: profileEvidence}
	maximumInputEstimate := limits.EstimatedInputTokens
	limitExcluded := 0
	outputLimitUnsupported := 0
	for index := range plan.Routes {
		route := plan.Routes[index]
		compat := compatibilityRouteFromExecutionPlan(plan, route)
		secret, decryptErr := auth.DecryptKey(route.EncryptedSecret, relay.encKey)
		if decryptErr != nil {
			relay.recordTraceEvent(ctx, requestID, "provider.key_decrypt_failed", "provider", "error", publicModel, requestedModel, &compat, 0, elapsedMilliseconds(requestStart), map[string]interface{}{"failure_kind": "auth"})
			continue
		}
		adapter, adapterErr := runtime.registry.Get(route.AdapterType)
		if adapterErr != nil {
			relay.recordTraceEvent(ctx, requestID, "provider.adapter_unknown", "provider", "error", publicModel, requestedModel, &compat, 0, elapsedMilliseconds(requestStart), map[string]interface{}{"failure_kind": "configuration"})
			continue
		}
		routeLimits := limits
		providerDownstreamBody := body
		if limits.ProviderOutputLimit > 0 && endpoint == adapters.EndpointChatCompletions {
			field, supported := providerOutputLimitField(plan, route, requiredCapabilities)
			if !supported {
				outputLimitUnsupported++
				relay.recordTraceEvent(ctx, requestID, "provider.output_limit_unsupported", "provider", "error", publicModel, requestedModel, &compat, 0, elapsedMilliseconds(requestStart), map[string]interface{}{"failure_kind": "configuration"})
				continue
			}
			providerDownstreamBody, err = injectChatOutputLimit(body, field, limits.ProviderOutputLimit)
			if err != nil {
				return nil, err
			}
			injectedAdmission, admissionErr := estimateChatAdmissionForPlan(providerDownstreamBody, plan)
			if admissionErr != nil {
				return nil, admissionErr
			}
			if injectedAdmission.InputTokens > routeLimits.EstimatedInputTokens {
				routeLimits.EstimatedInputTokens = injectedAdmission.InputTokens
				routeLimits.EstimateVersion = injectedAdmission.Version
			}
		}
		if !routeLimits.routeFits(plan, route) {
			limitExcluded++
			continue
		}
		upstreamRequest, buildErr := adapter.BuildRequest(ctx, adapters.RequestInput{
			Endpoint: endpoint, DownstreamBody: providerDownstreamBody, RequestedModel: requestedModel,
			MappedModel: route.UpstreamModel, Channel: compat.Channel,
			APIKey: secret, RequestID: requestID,
		})
		if buildErr != nil {
			relay.recordTraceEvent(ctx, requestID, "provider.request_build_failed", "provider", "error", publicModel, requestedModel, &compat, 0, elapsedMilliseconds(requestStart), map[string]interface{}{"failure_kind": "request_build"})
			continue
		}
		actualBody, bodyErr := providerRequestBody(upstreamRequest)
		if bodyErr != nil {
			relay.recordTraceEvent(ctx, requestID, "provider.request_body_unavailable", "provider", "error", publicModel, requestedModel, &compat, 0, elapsedMilliseconds(requestStart), map[string]interface{}{"failure_kind": "configuration"})
			continue
		}
		var providerAdmission requestAdmissionEstimate
		var admissionErr error
		if endpoint == adapters.EndpointEmbeddings {
			providerAdmission, admissionErr = estimateProviderEmbeddingsAdmission(actualBody)
		} else {
			providerAdmission, admissionErr = estimateProviderJSONAdmissionForPlan(actualBody, plan)
		}
		if admissionErr != nil {
			relay.recordTraceEvent(ctx, requestID, "provider.request_body_unmeasurable", "provider", "error", publicModel, requestedModel, &compat, 0, elapsedMilliseconds(requestStart), map[string]interface{}{"failure_kind": "configuration"})
			continue
		}
		if providerAdmission.InputTokens > routeLimits.EstimatedInputTokens {
			routeLimits.EstimatedInputTokens = providerAdmission.InputTokens
			routeLimits.EstimateVersion = providerAdmission.Version
		}
		if !routeLimits.routeFits(plan, route) {
			limitExcluded++
			continue
		}
		if routeLimits.EstimatedInputTokens > maximumInputEstimate {
			maximumInputEstimate = routeLimits.EstimatedInputTokens
			batch.limits.EstimateVersion = routeLimits.EstimateVersion
		}
		batch.routes = append(batch.routes, preparedModelExecutionRoute{
			route: route, compat: compat, adapter: adapter, upstreamRequest: upstreamRequest,
		})
	}
	batch.limits.EstimatedInputTokens = maximumInputEstimate
	if len(batch.routes) == 0 {
		if len(plan.Routes) > 0 && limitExcluded == len(plan.Routes) {
			return nil, errModelExecutionContextLimitExceeded
		}
		if len(plan.Routes) > 0 && outputLimitUnsupported+limitExcluded == len(plan.Routes) && outputLimitUnsupported > 0 {
			return nil, errModelExecutionOutputLimitRequired
		}
		return nil, fmt.Errorf("%w: no locally executable route", modelrouting.ErrNoRoute)
	}
	return batch, nil
}

func compatibilityRouteFromExecutionPlan(plan modelrouting.Plan, route modelrouting.Route) RouteResult {
	legacyChannelID := route.Evidence.LegacyChannelID
	legacyKeyID := route.Evidence.LegacyChannelKeyID
	legacyRoutingRevision := route.Evidence.LegacyRoutingRevision
	legacyCircuitRevision := route.Evidence.LegacyCircuitRevision
	legacyCredentialRevision := route.Evidence.LegacyCredentialStateRevision
	// Canonical execution has no legacy identity. Zero is retained all the way
	// through adapters, logs and metrics instead of manufacturing one.
	if route.Evidence.AuthorityMode == executionevidence.ModeCanonical {
		legacyChannelID = 0
		legacyKeyID = 0
		legacyRoutingRevision = 0
		legacyCircuitRevision = 0
		legacyCredentialRevision = 0
	}
	return RouteResult{
		Channel: models.Channel{
			ID: legacyChannelID, Name: route.ConnectionName, Provider: route.Provider,
			BaseURL: route.BaseURL, AdapterType: adapters.NormalizeType(route.AdapterType),
			AdapterConfig: slices.Clone(route.AdapterConfig), MaxContext: route.MaxContext,
			DefaultMaxOutput: route.DefaultMaxOutput, Priority: route.Priority,
			RoutingWeight: route.Weight, RoutingEpoch: legacyRoutingRevision,
			CircuitEpoch: legacyCircuitRevision,
		},
		ChannelKey: models.ChannelKey{
			ID: legacyKeyID, ChannelID: legacyChannelID, StateEpoch: legacyCredentialRevision,
		}, MappedModel: route.UpstreamModel,
		RoutingPolicyVersion: routingpolicy.SelectionVersion,
		SnapshotVersion:      plan.SnapshotHash, SnapshotSource: string(plan.AuthorityMode),
		CandidateCount: len(plan.Routes), ExcludedCount: len(plan.Exclusions),
		SelectionReason: route.SelectionReason, RequiresProbe: route.RequiresProbe,
		RuntimeReleaseRef: plan.RuntimeReleaseRef, RuntimeStage: plan.RuntimeStage,
		ExecutionProfileID: plan.ExecutionProfileID, ExecutionProfileVersion: plan.ExecutionProfileVersion,
		ReasoningProfile: plan.ReasoningProfile, CacheUsageProfile: plan.CacheUsageProfile,
	}
}

type executionDecisionContextKey struct{}

func contextWithExecutionDecision(ctx context.Context, decision modelexecution.Decision) context.Context {
	return context.WithValue(ctx, executionDecisionContextKey{}, decision)
}

func executionDecisionFromContext(ctx context.Context) (modelexecution.Decision, bool) {
	if ctx == nil {
		return modelexecution.Decision{}, false
	}
	decision, ok := ctx.Value(executionDecisionContextKey{}).(modelexecution.Decision)
	return decision, ok && decision.Status == modelexecution.Applied
}

func (relay *Relay) authorizePreparedModelRoute(
	ctx context.Context,
	batch *preparedModelExecutionBatch,
	prepared *preparedModelExecutionRoute,
	sequence, total int,
	endpoint adapters.Endpoint,
) (context.Context, modelexecution.Status, error) {
	if relay.modelExecution == nil || batch == nil || prepared == nil {
		return ctx, modelexecution.Stale, fmt.Errorf("model execution authorization is not configured")
	}
	requestID := middleware.GetRequestIDFromContext(ctx)
	attemptID := newAttemptID()
	attemptCtx := contextWithPreDispatchProviderAttempt(ctx, attemptID)
	apiKey := middleware.GetAPIKeyFromContext(ctx)
	identity := modelexecution.AttemptIdentity{
		RequestID: requestID, AttemptID: attemptID,
		OrgID: middleware.GetOrgIDFromContext(ctx), APIKeyID: middleware.GetKeyIDFromContext(ctx),
		RouteSequence: sequence, RouteTotal: total, Endpoint: string(endpoint),
	}
	if apiKey != nil {
		if apiKey.AccessCredentialID != nil {
			identity.AccessCredentialID = *apiKey.AccessCredentialID
		}
		identity.CredentialRef = apiKey.CredentialRef
		if apiKey.AccessGrantID != nil {
			identity.AccessGrantID = *apiKey.AccessGrantID
		}
	}
	decision, err := relay.modelExecution.coordinator.Authorize(attemptCtx, batch.plan, prepared.route, identity)
	if err != nil {
		return attemptCtx, decision.Status, err
	}
	switch decision.Status {
	case modelexecution.Stale, modelexecution.Disabled, modelexecution.LeaseLost:
		return attemptCtx, decision.Status, nil
	case modelexecution.Applied:
		// Continue to bind the exact immutable evidence below.
	default:
		return attemptCtx, decision.Status, fmt.Errorf("model execution authorization returned unsupported status %q", decision.Status)
	}
	compact, err := executionevidence.Compact(decision.Evidence)
	if err != nil {
		return relay.releaseAppliedDecision(attemptCtx, decision, fmt.Errorf("compact execution evidence: %w", err))
	}
	attemptCtx, err = ContextWithCoordinatorExecutionProjection(
		attemptCtx, identity.OrgID, requestID, attemptID, compact,
	)
	if err != nil {
		return relay.releaseAppliedDecision(attemptCtx, decision, err)
	}
	attemptCtx = contextWithExecutionDecision(attemptCtx, decision)
	return attemptCtx, modelexecution.Applied, nil
}

func (relay *Relay) releaseAppliedDecision(
	ctx context.Context,
	decision modelexecution.Decision,
	cause error,
) (context.Context, modelexecution.Status, error) {
	result, err := relay.modelExecution.coordinator.ReleaseUndispatched(context.WithoutCancel(ctx), decision)
	if err != nil {
		return ctx, modelexecution.Stale, errors.Join(cause, fmt.Errorf("release undispatched execution: %w", err))
	}
	if result.Status != modelexecution.Applied {
		return ctx, result.Status, errors.Join(cause, fmt.Errorf("release undispatched execution: %s", result.Status))
	}
	return ctx, modelexecution.Stale, cause
}

type executionHealthAction uint8

const (
	executionHealthSuccess executionHealthAction = iota + 1
	executionHealthProviderFailure
	executionHealthCredentialAuthFailure
	executionHealthCredentialRateLimited
)

func (action executionHealthAction) String() string {
	switch action {
	case executionHealthSuccess:
		return "success"
	case executionHealthProviderFailure:
		return "provider_failure"
	case executionHealthCredentialAuthFailure:
		return "credential_auth_failure"
	case executionHealthCredentialRateLimited:
		return "credential_rate_limited"
	default:
		return "unknown"
	}
}

func healthActionForFailureKind(kind adapters.FailureKind) executionHealthAction {
	switch kind {
	case adapters.FailureAuth:
		return executionHealthCredentialAuthFailure
	case adapters.FailureRateLimit:
		return executionHealthCredentialRateLimited
	default:
		return executionHealthProviderFailure
	}
}

// recordExecutionHealthFailure makes a failed Coordinator fence observable.
// A Coordinator decision is authority and must fail closed.
func (relay *Relay) recordExecutionHealthFailure(
	ctx context.Context,
	requestID, model, requestedModel string,
	route *RouteResult,
	statusCode, latencyMS int,
	action executionHealthAction,
	err error,
) bool {
	if err == nil {
		return false
	}
	if _, ok := executionDecisionFromContext(ctx); !ok {
		return false
	}
	relay.recordTraceEvent(
		context.WithoutCancel(ctx), requestID,
		"routing.health_transition_failed", "routing", "error",
		model, requestedModel, route, statusCode, latencyMS,
		map[string]interface{}{
			"error_code":    "routing_health_transition_failed",
			"health_action": action.String(),
		},
	)
	return true
}

const (
	modelCapabilityTools             = "tools"
	modelCapabilityParallelToolCalls = "parallel_tool_calls"
	modelCapabilityJSONObject        = "json_object"
	modelCapabilityJSONSchema        = "json_schema"
	modelCapabilityReasoning         = "reasoning"
	modelCapabilityVision            = "vision"
	modelCapabilityAudio             = "audio"
	modelCapabilityStreaming         = "streaming"
	modelCapabilityPromptCache       = "prompt_cache"
)

func (relay *Relay) applyRouteHealth(ctx context.Context, _ *RouteResult, action executionHealthAction) error {
	decision, ok := executionDecisionFromContext(ctx)
	if !ok || relay.modelExecution == nil || relay.modelExecution.coordinator == nil {
		return fmt.Errorf("model execution health authority is unavailable")
	}
	var (
		result modelexecution.HealthResult
		err    error
	)
	switch action {
	case executionHealthSuccess:
		result, err = relay.modelExecution.coordinator.MarkSuccess(ctx, decision)
	case executionHealthProviderFailure:
		result, err = relay.modelExecution.coordinator.MarkProviderFailure(ctx, decision)
	case executionHealthCredentialAuthFailure:
		result, err = relay.modelExecution.coordinator.MarkCredentialAuthFailure(ctx, decision)
	case executionHealthCredentialRateLimited:
		result, err = relay.modelExecution.coordinator.MarkCredentialRateLimited(ctx, decision)
	default:
		return fmt.Errorf("unsupported route health action")
	}
	if err != nil {
		return err
	}
	if result.Status != modelexecution.Applied {
		return fmt.Errorf("model execution health transition: %s", result.Status)
	}
	return nil
}

// maintainExecutionLease keeps an Applied half-open decision alive while the
// provider request is in flight. A renewal failure cancels only provider I/O;
// it is also surfaced through the request logger rather than being swallowed.
func (relay *Relay) maintainExecutionLease(
	ctx context.Context,
	reqLog *logger.Logger,
) (context.Context, func()) {
	decision, ok := executionDecisionFromContext(ctx)
	if !ok || !decision.Evidence.RequiresProbe || relay.modelExecution == nil {
		return ctx, func() {}
	}
	dispatchCtx, cancelDispatch := context.WithCancel(ctx)
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		ticker := time.NewTicker(relay.modelExecution.leaseInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-dispatchCtx.Done():
				return
			case <-ticker.C:
				renewCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				result, err := relay.modelExecution.coordinator.RenewLease(renewCtx, decision)
				cancel()
				if err != nil || result.Status != modelexecution.Applied {
					if reqLog != nil {
						reqLog.Error("Model execution probe lease renewal failed: status=%s error=%v", result.Status, err)
					}
					cancelDispatch()
					return
				}
			}
		}
	}()
	return dispatchCtx, func() {
		once.Do(func() {
			close(stop)
			<-done
			cancelDispatch()
		})
	}
}

func requiredCapabilitiesForChat(body []byte) ([]string, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return nil, fmt.Errorf("invalid chat capability envelope")
	}
	capabilities := make(map[string]struct{})
	add := func(value string) { capabilities[value] = struct{}{} }
	streamEnabled := false
	if raw, exists := envelope["stream"]; exists {
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return nil, fmt.Errorf("stream must be a boolean")
		}
		if enabled {
			streamEnabled = true
			add(modelCapabilityStreaming)
		}
	}
	if raw, exists := envelope["stream_options"]; exists && !isJSONNull(raw) {
		if firstJSONToken(raw) != '{' {
			return nil, fmt.Errorf("stream_options must be an object or null")
		}
		var options map[string]json.RawMessage
		if err := json.Unmarshal(raw, &options); err != nil || options == nil {
			return nil, fmt.Errorf("stream_options must be an object or null")
		}
		if !streamEnabled {
			return nil, fmt.Errorf("stream_options requires stream=true")
		}
	}
	if raw, exists := envelope["tools"]; exists && !isJSONNull(raw) {
		var tools []json.RawMessage
		if err := json.Unmarshal(raw, &tools); err != nil {
			return nil, fmt.Errorf("tools must be an array")
		}
		if len(tools) > 0 {
			add(modelCapabilityTools)
		}
	}
	if raw, exists := envelope["functions"]; exists && !isJSONNull(raw) {
		var functions []json.RawMessage
		if err := json.Unmarshal(raw, &functions); err != nil {
			return nil, fmt.Errorf("functions must be an array")
		}
		if len(functions) > 0 {
			add(modelCapabilityTools)
		}
	}
	if raw, exists := envelope["tool_choice"]; exists && !isJSONNull(raw) {
		switch firstJSONToken(raw) {
		case '"':
			var choice string
			if err := json.Unmarshal(raw, &choice); err != nil {
				return nil, fmt.Errorf("tool_choice must be a string or object")
			}
			switch choice {
			case "none":
			case "auto", "required":
				add(modelCapabilityTools)
			default:
				return nil, fmt.Errorf("unsupported tool_choice")
			}
		case '{':
			var choice map[string]json.RawMessage
			if err := json.Unmarshal(raw, &choice); err != nil || choice == nil {
				return nil, fmt.Errorf("tool_choice must be a string or object")
			}
			add(modelCapabilityTools)
		default:
			return nil, fmt.Errorf("tool_choice must be a string or object")
		}
	}
	if raw, exists := envelope["function_call"]; exists && !isJSONNull(raw) {
		switch firstJSONToken(raw) {
		case '"':
			var choice string
			if err := json.Unmarshal(raw, &choice); err != nil {
				return nil, fmt.Errorf("function_call must be a string or object")
			}
			if choice != "" && choice != "none" {
				add(modelCapabilityTools)
			}
		case '{':
			var choice map[string]json.RawMessage
			if err := json.Unmarshal(raw, &choice); err != nil || choice == nil {
				return nil, fmt.Errorf("function_call must be a string or object")
			}
			if len(choice) > 0 {
				add(modelCapabilityTools)
			}
		default:
			return nil, fmt.Errorf("function_call must be a string or object")
		}
	}
	if raw, exists := envelope["parallel_tool_calls"]; exists && !isJSONNull(raw) {
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return nil, fmt.Errorf("parallel_tool_calls must be a boolean")
		}
		if enabled {
			add(modelCapabilityTools)
			add(modelCapabilityParallelToolCalls)
		}
	}
	if raw, exists := envelope["response_format"]; exists && !isJSONNull(raw) {
		var format struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &format); err != nil {
			return nil, fmt.Errorf("response_format must be an object")
		}
		switch strings.TrimSpace(format.Type) {
		case "text":
		case "json_object":
			add(modelCapabilityJSONObject)
		case "json_schema":
			add(modelCapabilityJSONSchema)
		default:
			return nil, fmt.Errorf("unsupported response_format type")
		}
	}
	for _, field := range []string{"reasoning", "reasoning_effort", "thinking"} {
		if raw, exists := envelope[field]; exists && !isJSONNull(raw) {
			add(modelCapabilityReasoning)
		}
	}
	if raw, exists := envelope["prompt_cache_key"]; exists && !isJSONNull(raw) {
		add(modelCapabilityPromptCache)
	}
	if raw, exists := envelope["modalities"]; exists && !isJSONNull(raw) {
		var modalities []string
		if err := json.Unmarshal(raw, &modalities); err != nil {
			return nil, fmt.Errorf("modalities must be an array of strings")
		}
		for _, modality := range modalities {
			switch modality {
			case "text":
			case "audio":
				add(modelCapabilityAudio)
			default:
				return nil, fmt.Errorf("unsupported modality %q", modality)
			}
		}
	}
	if raw, exists := envelope["audio"]; exists && !isJSONNull(raw) {
		if firstJSONToken(raw) != '{' {
			return nil, fmt.Errorf("audio must be an object")
		}
		var audio map[string]json.RawMessage
		if err := json.Unmarshal(raw, &audio); err != nil || audio == nil {
			return nil, fmt.Errorf("audio must be an object")
		}
		add(modelCapabilityAudio)
	}
	if raw, exists := envelope["messages"]; exists {
		var messages []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &messages); err != nil {
			return nil, fmt.Errorf("messages must be an array")
		}
		for _, message := range messages {
			var role string
			if rawRole, exists := message["role"]; exists {
				if err := json.Unmarshal(rawRole, &role); err != nil {
					return nil, fmt.Errorf("message role must be a string")
				}
			}
			if role == "tool" || role == "function" {
				add(modelCapabilityTools)
			}
			if rawCalls, exists := message["tool_calls"]; exists && !isJSONNull(rawCalls) {
				var calls []json.RawMessage
				if err := json.Unmarshal(rawCalls, &calls); err != nil {
					return nil, fmt.Errorf("message tool_calls must be an array")
				}
				if len(calls) > 0 {
					add(modelCapabilityTools)
				}
			}
			if rawCall, exists := message["function_call"]; exists && !isJSONNull(rawCall) {
				if firstJSONToken(rawCall) != '{' {
					return nil, fmt.Errorf("message function_call must be an object or null")
				}
				var call map[string]json.RawMessage
				if err := json.Unmarshal(rawCall, &call); err != nil || call == nil {
					return nil, fmt.Errorf("message function_call must be an object or null")
				}
				add(modelCapabilityTools)
			}
			rawContent, exists := message["content"]
			if !exists || isJSONNull(rawContent) || firstJSONToken(rawContent) == '"' {
				continue
			}
			if firstJSONToken(rawContent) != '[' {
				return nil, fmt.Errorf("message content must be text, array, or null")
			}
			var parts []map[string]json.RawMessage
			if err := json.Unmarshal(rawContent, &parts); err != nil {
				return nil, fmt.Errorf("message content parts must be objects")
			}
			for _, part := range parts {
				var kind string
				if rawType, exists := part["type"]; exists {
					if err := json.Unmarshal(rawType, &kind); err != nil {
						return nil, fmt.Errorf("message content part type must be a string")
					}
				}
				switch kind {
				case "text", "input_text", "refusal":
				case "image_url", "input_image":
					add(modelCapabilityVision)
				case "input_audio", "audio":
					add(modelCapabilityAudio)
				case "tool_use", "tool_result":
					add(modelCapabilityTools)
				case "":
					return nil, fmt.Errorf("message content part type is required")
				default:
					return nil, fmt.Errorf("unsupported message content part type %q", kind)
				}
			}
		}
	}
	result := make([]string, 0, len(capabilities))
	for capability := range capabilities {
		result = append(result, capability)
	}
	slices.Sort(result)
	return result, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func firstJSONToken(raw json.RawMessage) byte {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return 0
	}
	return trimmed[0]
}
