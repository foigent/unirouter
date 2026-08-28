package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/promptcompression"
	"github.com/uniroute/unirouter/internal/responsecache"
)

const (
	responseCacheOptInHeader = "X-UniRouter-Response-Cache"
	compressionOptInHeader   = "X-UniRouter-Prompt-Compression"
)

type invocationOptimizationState struct {
	policy            invocationopt.Policy
	found             bool
	compression       *invocationopt.Receipt
	effectiveBody     []byte
	compressionResult promptcompression.Result
}

type responseCacheMissState struct {
	reservation *responsecache.Reservation
	identity    responsecache.Identity
}

type responseCacheMissContextKey struct{}

type providerRequestCountContextKey struct{}

func contextWithResponseCacheMiss(ctx context.Context, state *responseCacheMissState) context.Context {
	if state == nil {
		return ctx
	}
	return context.WithValue(ctx, responseCacheMissContextKey{}, state)
}

func responseCacheMissFromContext(ctx context.Context) *responseCacheMissState {
	state, _ := ctx.Value(responseCacheMissContextKey{}).(*responseCacheMissState)
	return state
}

func contextWithProviderRequestCount(ctx context.Context, count int64) context.Context {
	if count <= 0 {
		return ctx
	}
	return context.WithValue(ctx, providerRequestCountContextKey{}, count)
}

func providerRequestCountFromContext(ctx context.Context) int64 {
	count, _ := ctx.Value(providerRequestCountContextKey{}).(int64)
	return count
}

func (relay *Relay) releaseResponseCacheMiss(ctx context.Context, reqLog *logger.Logger) {
	state := responseCacheMissFromContext(ctx)
	if relay == nil || relay.responseCache == nil || state == nil || state.reservation == nil {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	err := relay.responseCache.Release(releaseCtx, state.reservation)
	cancel()
	if err != nil && reqLog != nil {
		reqLog.Warn("Failed to release response-cache miss lease: %v", err)
	}
}

func (relay *Relay) SetInvocationOptimization(
	manager *invocationopt.Manager,
	cache *responsecache.Service,
	runtimeVersion, runtimeCommit, runtimeEnvironment string,
) {
	if relay == nil {
		return
	}
	relay.invocationOptimization = manager
	relay.responseCache = cache
	relay.runtimeVersion = strings.TrimSpace(runtimeVersion)
	relay.runtimeCommit = strings.TrimSpace(runtimeCommit)
	relay.runtimeEnvironment = strings.TrimSpace(runtimeEnvironment)
}

func (relay *Relay) evaluateCompression(
	ctx context.Context,
	header http.Header,
	requestID string,
	requestedModel string,
	apiKey *models.ApiKey,
	model string,
	body []byte,
) invocationOptimizationState {
	state := invocationOptimizationState{effectiveBody: body}
	if relay == nil || relay.invocationOptimization == nil || apiKey == nil {
		return state
	}
	resolveCtx, cancelResolve := context.WithTimeout(ctx, time.Second)
	policy, found, err := relay.invocationOptimization.Resolve(
		resolveCtx,
		apiKey.OrgID,
		middleware.GetProjectIDFromContext(ctx),
		model,
	)
	cancelResolve()
	if err != nil || !found {
		return state
	}
	state.policy, state.found = policy, true
	mode := policy.EffectiveMode(invocationopt.DriverCompression)
	if mode == invocationopt.EffectiveDisabled {
		return state
	}
	started := time.Now()
	result := promptcompression.Evaluate(body, policy.CompressionConfig())
	state.compressionResult = result
	assigned := false
	if mode == invocationopt.EffectiveCanary {
		assigned = invocationopt.Assigned(policy, invocationopt.DriverCompression, apiKey.ID, requestID)
	}
	optedIn := requestHeaderAllows(header, compressionOptInHeader)
	decision := "shadow_observed"
	mutationApplied := false
	if mode == invocationopt.EffectiveCanary {
		switch {
		case !assigned:
			decision = "canary_unassigned"
		case !optedIn:
			decision = "request_opt_in_required"
		case !result.Eligible:
			decision = "ineligible"
		default:
			decision = "compressed"
			mutationApplied = true
			state.effectiveBody = result.CandidateBody
		}
	}
	state.compression = &invocationopt.Receipt{
		OrgID: apiKey.OrgID, ProjectID: middleware.GetProjectIDFromContext(ctx), RequestID: requestID,
		RequestedModel: requestedModel, ResolvedModel: model,
		Driver: invocationopt.DriverCompression, PolicyRef: policy.PolicyRef, PolicyRevision: policy.Revision,
		Stage: policy.Stage, EffectiveMode: mode, Decision: decision, Reason: result.Reason,
		Assigned: assigned, Eligible: result.Eligible, MutationApplied: mutationApplied,
		OriginalBytes: result.OriginalBytes, CandidateBytes: result.CandidateBytes,
		OriginalEstimatedTokens: result.OriginalTokens, CandidateEstimatedTokens: result.CandidateTokens,
		EstimatedSavedTokens: result.SavedTokens, ReductionBasisPoints: result.ReductionBasisPoints,
		SafetyPassed: result.SafetyPassed, QualityPassed: result.QualityPassed,
		LatencyMicros:  time.Since(started).Microseconds(),
		IdentityDigest: invocationopt.AssignmentDigest(policy, invocationopt.DriverCompression, apiKey.ID, requestID),
		CreatedAt:      time.Now().UTC(),
	}
	return state
}

func (relay *Relay) recordCompressionReceipt(
	ctx context.Context,
	state invocationOptimizationState,
	route *RouteResult,
	reqLog *logger.Logger,
) bool {
	if relay == nil || relay.invocationOptimization == nil || state.compression == nil {
		return true
	}
	if route != nil {
		state.compression.ResolvedModel = route.MappedModel
		state.compression.Provider = route.Channel.Provider
		state.compression.RouteChannelID = route.Channel.ID
		state.compression.RouteCredentialID = route.ChannelKey.ID
		state.compression.RuntimeAssignmentID = route.RuntimeAssignmentID
	}
	recordCtx, cancelRecord := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	err := relay.invocationOptimization.Record(recordCtx, *state.compression)
	cancelRecord()
	if err != nil {
		if reqLog != nil {
			reqLog.Warn("Failed to record metadata-only compression receipt: %v", err)
		}
		return false
	}
	return true
}

func requestHeaderAllows(header http.Header, name string) bool {
	return strings.EqualFold(strings.TrimSpace(header.Get(name)), "allow")
}

func requestCacheNoStore(header http.Header) bool {
	for _, directive := range strings.Split(header.Get("Cache-Control"), ",") {
		if strings.EqualFold(strings.TrimSpace(directive), "no-store") {
			return true
		}
	}
	return false
}

func exactUpstreamBody(request *http.Request, maximum int64) ([]byte, error) {
	if request == nil || request.Body == nil || maximum <= 0 {
		return nil, fmt.Errorf("upstream request body unavailable")
	}
	if request.GetBody != nil {
		copyBody, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		defer copyBody.Close()
		body, err := io.ReadAll(io.LimitReader(copyBody, maximum+1))
		if err != nil || int64(len(body)) > maximum {
			return nil, fmt.Errorf("upstream request body exceeds cache identity limit")
		}
		return body, nil
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maximum+1))
	if err != nil || int64(len(body)) > maximum {
		return nil, fmt.Errorf("upstream request body exceeds cache identity limit")
	}
	request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	return body, nil
}

func (relay *Relay) responseCacheIdentity(
	ctx context.Context,
	apiKey *models.ApiKey,
	policy invocationopt.Policy,
	route *RouteResult,
	requestedModel string,
	pricingVersion string,
) responsecache.Identity {
	guardrailVersion := "none"
	if pinned := guardrailPinFromContext(ctx); pinned != nil {
		guardrailVersion = pinned.PolicySetVersion()
	}
	runtimeStage := portableOrDefault(route.RuntimeStage, "stable")
	runtimeAssignment := portableOrDefault(route.RuntimeAssignmentID, "none")
	routePolicy := portableOrDefault(route.RoutingPolicyVersion, "routing-policy-unavailable")
	return responsecache.Identity{
		Scope: responsecache.Scope{
			OrgID: apiKey.OrgID, ProjectID: middleware.GetProjectIDFromContext(ctx),
			Region: policy.Region, Residency: policy.Residency,
		},
		APIKeyID: apiKey.ID, AuthorizationEpoch: "epoch:" + strconv.FormatInt(apiKey.AuthEpoch, 10),
		Endpoint: responsecache.EndpointChatCompletions, RequestedModel: requestedModel,
		ResolvedModel: route.MappedModel, Provider: route.Channel.Provider,
		ChannelID: route.Channel.ID, ChannelRoutingEpoch: route.Channel.RoutingEpoch,
		ChannelCircuitEpoch: route.Channel.CircuitEpoch,
		CredentialID:        route.ChannelKey.ID, CredentialEpoch: route.ChannelKey.StateEpoch,
		AdapterProfileVersion:     adapterProfileIdentity(route.Channel),
		RoutePolicyVersion:        routePolicy,
		RuntimeReleaseRef:         relay.runtimeReleaseIdentity(route),
		RuntimeStage:              runtimeStage,
		RuntimeAssignmentID:       runtimeAssignment,
		GuardrailPolicyVersion:    portableOrDigest(guardrailVersion),
		OptimizationPolicyVersion: policy.PolicyRef + "-r" + strconv.FormatInt(policy.Revision, 10),
		CustomerPricingVersion:    portableOrDigest(pricingVersion),
		ResponseCachePriceVersion: invocationopt.ResponseCachePriceVersion,
		RequestSchemaVersion:      responsecache.RequestSchemaChatV1,
		ResponseSchemaVersion:     responsecache.ResponseSchemaChatV1,
	}
}

func adapterProfileIdentity(channel models.Channel) string {
	hash := sha256.New()
	for _, value := range []string{channel.AdapterType, channel.BaseURL, string(channel.AdapterConfig)} {
		_, _ = hash.Write([]byte(strconv.Itoa(len(value))))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(value))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func (relay *Relay) runtimeReleaseIdentity(route *RouteResult) string {
	values := []string{relay.runtimeEnvironment, relay.runtimeVersion, relay.runtimeCommit, route.RuntimeReleaseRef}
	return digestIdentity(values...)
}

func portableOrDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return portableOrDigest(value)
}

func portableOrDigest(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && len(value) <= 160 {
		valid := true
		for index, r := range value {
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
				r == '.' || r == '_' || r == ':' || r == '/' || r == '@' || r == '+' || r == '-') ||
				(index == 0 && !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))) {
				valid = false
				break
			}
		}
		if valid {
			return value
		}
	}
	return digestIdentity(value)
}

func digestIdentity(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte(strconv.Itoa(len(value))))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(value))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

type cacheRouteDecision struct {
	hit      *responsecache.Hit
	miss     *responseCacheMissState
	receipt  *invocationopt.Receipt
	reject   bool
	reason   string
	identity responsecache.Identity
}

func (relay *Relay) evaluateResponseCache(
	ctx context.Context,
	header http.Header,
	requestID string,
	requestedModel string,
	apiKey *models.ApiKey,
	state invocationOptimizationState,
	route *RouteResult,
	upstreamRequest *http.Request,
	pricingVersion string,
	reqLog *logger.Logger,
) cacheRouteDecision {
	result := cacheRouteDecision{}
	if relay == nil || relay.invocationOptimization == nil || !state.found || apiKey == nil || route == nil {
		return result
	}
	// Exact Response Cache v1 is intentionally keyed by legacy channel/key
	// epochs. A canonical route has no such IDs, so it must bypass until a
	// deployment/binding-aware cache identity is introduced. Never manufacture
	// legacy IDs merely to make an optimization eligible.
	if route.SnapshotSource == string(modelrouting.ModeCanonical) ||
		route.Channel.ID <= 0 || route.ChannelKey.ID <= 0 {
		result.reason = responsecache.ReasonInvalidConfiguration
		return result
	}
	mode := state.policy.EffectiveMode(invocationopt.DriverResponseCache)
	if mode == invocationopt.EffectiveDisabled {
		return result
	}
	body, err := exactUpstreamBody(upstreamRequest, 1024*1024)
	if err != nil {
		result.reason = responsecache.ReasonIneligibleShape
		return result
	}
	identity := relay.responseCacheIdentity(ctx, apiKey, state.policy, route, requestedModel, pricingVersion)
	request := responsecache.Request{
		Identity: identity, EffectiveBody: body,
		TTL:                time.Duration(state.policy.ResponseCacheTTLSeconds) * time.Second,
		OptIn:              requestHeaderAllows(header, responseCacheOptInHeader),
		PersistenceAllowed: state.policy.ResponseCachePersistence,
		NoStore:            requestCacheNoStore(header),
	}
	assigned := false
	if mode == invocationopt.EffectiveCanary {
		assigned = invocationopt.Assigned(state.policy, invocationopt.DriverResponseCache, apiKey.ID, requestID)
	}
	decision := responsecache.EvaluateRequest(request, 1024*1024)
	decisionName := "shadow_observed"
	if apiKey.AuthEpoch <= 0 {
		decision = responsecache.Decision{Kind: responsecache.DecisionBypass, Reason: responsecache.ReasonInvalidConfiguration}
		decisionName = "invalid_authorization_epoch"
	} else if mode == invocationopt.EffectiveCanary {
		switch {
		case !assigned:
			decisionName = "canary_unassigned"
		case relay.responseCache == nil:
			decision = responsecache.Decision{Kind: responsecache.DecisionBypass, Reason: responsecache.ReasonInvalidConfiguration}
			decisionName = "runtime_unavailable"
		default:
			decision = relay.responseCache.Begin(ctx, request)
			if decision.Kind == responsecache.DecisionWait {
				decision = relay.responseCache.Await(ctx, decision.Wait)
			}
			// An invalid optimization identity/configuration must fail closed for
			// cache use but fail open for the already-authorized provider request.
			// Only bounded healthy-cache contention is allowed to reject in order
			// to prevent a provider stampede.
			if decision.Kind == responsecache.DecisionReject && decision.Reason == responsecache.ReasonInvalidConfiguration {
				decision = responsecache.Decision{Kind: responsecache.DecisionBypass, Reason: decision.Reason}
			}
			decisionName = string(decision.Kind)
		}
	}
	identityDigest := invocationopt.AssignmentDigest(state.policy, invocationopt.DriverResponseCache, apiKey.ID, requestID)
	if decision.Hit != nil {
		identityDigest = decision.Hit.IdentityDigest
	} else if decision.Reservation != nil {
		identityDigest = decision.Reservation.IdentityDigest()
	}
	receipt := invocationopt.Receipt{
		OrgID: apiKey.OrgID, ProjectID: middleware.GetProjectIDFromContext(ctx), RequestID: requestID,
		RequestedModel: requestedModel, ResolvedModel: identity.ResolvedModel, Provider: identity.Provider,
		Driver: invocationopt.DriverResponseCache, PolicyRef: state.policy.PolicyRef,
		PolicyRevision: state.policy.Revision, Stage: state.policy.Stage, EffectiveMode: mode,
		Decision: decisionName, Reason: decision.Reason, Assigned: assigned,
		Eligible: decision.Kind == responsecache.DecisionEligible || decision.Kind == responsecache.DecisionHit ||
			decision.Kind == responsecache.DecisionOwner || decision.Kind == responsecache.DecisionWait,
		MutationApplied: decision.Kind == responsecache.DecisionHit,
		OriginalBytes:   len(body), CandidateBytes: len(body), SafetyPassed: true, QualityPassed: true,
		RouteChannelID: route.Channel.ID, RouteCredentialID: route.ChannelKey.ID,
		RuntimeAssignmentID: route.RuntimeAssignmentID, IdentityDigest: identityDigest, CreatedAt: time.Now().UTC(),
	}
	if !receipt.Eligible {
		receipt.SafetyPassed, receipt.QualityPassed = false, false
	}
	result.identity, result.reason, result.receipt = identity, decision.Reason, &receipt
	// Shadow and unassigned Canary traffic are evidence-only. Even an invalid
	// cache identity/configuration must not reject, delay, serve, or reserve the
	// otherwise valid provider request outside the assigned Canary population.
	if mode != invocationopt.EffectiveCanary || !assigned {
		return result
	}
	switch decision.Kind {
	case responsecache.DecisionHit:
		result.hit = decision.Hit
	case responsecache.DecisionOwner:
		result.miss = &responseCacheMissState{reservation: decision.Reservation, identity: identity}
	case responsecache.DecisionReject:
		result.reject = true
	}
	return result
}

func (relay *Relay) recordResponseCacheDecision(
	ctx context.Context,
	decision cacheRouteDecision,
	reqLog *logger.Logger,
) {
	if relay == nil || relay.invocationOptimization == nil || decision.receipt == nil {
		return
	}
	recordCtx, cancelRecord := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	err := relay.invocationOptimization.Record(recordCtx, *decision.receipt)
	cancelRecord()
	if err != nil && reqLog != nil {
		reqLog.Warn("Failed to record response-cache decision receipt: %v", err)
	}
}

func (relay *Relay) serveResponseCacheHit(
	ctx context.Context,
	w http.ResponseWriter,
	route *RouteResult,
	hit *responsecache.Hit,
	identity responsecache.Identity,
	policy invocationopt.Policy,
	requestID string,
	orgID, keyID int64,
	model, requestedModel, clientIP string,
	requestStart time.Time,
	reqLog *logger.Logger,
) {
	var responseMap map[string]any
	if hit == nil || decodeJSONUseNumber(hit.SourceResponse, &responseMap) != nil || responseMap == nil {
		relay.billing.Cancel(context.WithoutCancel(ctx), requestID, orgID)
		writeError(w, http.StatusInternalServerError, "server_error", "response_cache.integrity_error", "Cached response failed validation")
		return
	}
	if inspection := inspectNonStreamOutputTextsForRoute(responseMap, route); !inspection.complete() {
		relay.billing.Cancel(context.WithoutCancel(ctx), requestID, orgID)
		writeError(w, http.StatusInternalServerError, "server_error", "response_cache.integrity_error", "Cached response failed validation")
		return
	}
	if pinned := guardrailPinFromContext(ctx); pinned != nil {
		result, refs := evaluateNonStreamOutputGuardrailForProtocol(ctx, pinned, responseMap, route.ModelProtocolProfile)
		relay.recordGuardrailResult(context.WithoutCancel(ctx), requestID, model, requestedModel, guardrail.StageNonStreamOutput, result, route, http.StatusOK, "")
		if result.Decision == guardrail.DecisionBlock {
			relay.billing.Cancel(context.WithoutCancel(ctx), requestID, orgID)
			writeOutputGuardrailBlock(w, guardrailBlockedByUnavailable(result))
			return
		}
		if err := applyProfileAwareOutputGuardrailTransform(route, result, refs); err != nil {
			relay.billing.Cancel(context.WithoutCancel(ctx), requestID, orgID)
			writeError(w, http.StatusInternalServerError, "server_error", "guardrail_error.transform_failed", "Failed to apply content policy safely")
			return
		}
	}
	applyResponseCacheDeliveryIdentity(responseMap, requestID, requestStart)
	applyResponseCacheDeliveryUsage(responseMap)
	basePayload, maximum, err := relay.marshalNonStreamResponseBase(responseMap)
	if err != nil {
		relay.billing.Cancel(context.WithoutCancel(ctx), requestID, orgID)
		writeError(w, http.StatusInternalServerError, "server_error", "response_cache.encode_error", "Failed to encode cached response")
		return
	}
	usage := adapters.NewResponseCacheUsage()
	settleRequest := billing.ResponseCacheSettleRequest(requestID, orgID)
	prepareCtx, prepareCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	prepared, err := relay.billing.PrepareResponseCacheDelivery(prepareCtx, settleRequest)
	prepareCancel()
	if err != nil || prepared == nil || !prepared.Funded {
		reqLog.Error("Response-cache delivery preparation failed: %v", err)
		relay.cancelResponseCacheReservation(ctx, requestID, orgID, reqLog)
		writeError(w, http.StatusInternalServerError, "server_error", "billing_error.settle_failed", "Failed to secure cache delivery")
		return
	}
	preview := &prepared.Preview
	extension := map[string]any{
		"request_id": requestID,
		"cost":       preparedDeliveryCostExtension(usage, preview, 0, 0),
		"provider":   route.Channel.Provider, "channel": route.Channel.Name,
		"cache_hit": false, "response_cache_hit": true,
		"provider_prompt_cache": providerPromptCacheExtension(usage, preview),
		"fallback":              false, "attempt_count": 0, "route_count": 1,
		"billing_status": "delivery_prepared",
		"response_cache": map[string]any{
			"contract_version": responsecache.Version, "hit": true,
			"current_provider_requests": 0, "current_provider_cost_micros": "0",
			"customer_charge_micros": "0",
		},
	}
	payload, err := appendNonStreamResponseExtension(basePayload, extension, maximum)
	if err != nil {
		if relay.recordResponseCacheDelivery(ctx, billing.ResponseCacheDeliveryEvent{
			RequestID: requestID, OrgID: orgID, Outcome: billing.DeliveryOutcomeWriteFailed,
			HTTPStatus: http.StatusOK, ErrorCode: "response_encode_failed",
		}, reqLog) {
			relay.settleFailedResponseCacheDelivery(settleRequest, reqLog)
		}
		writeError(w, http.StatusInternalServerError, "server_error", "response_cache.encode_error", "Failed to encode cached response")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(responseCacheOptInHeader, "HIT")
	w.WriteHeader(http.StatusOK)
	written, writeErr := w.Write(payload)
	if writeErr != nil || written != len(payload) {
		if relay.recordResponseCacheDelivery(ctx, billing.ResponseCacheDeliveryEvent{
			RequestID: requestID, OrgID: orgID, Outcome: billing.DeliveryOutcomeWriteFailed,
			DeliveredBytes: int64(written), HTTPStatus: http.StatusOK, ErrorCode: "downstream_write_failed",
		}, reqLog) {
			relay.settleFailedResponseCacheDelivery(settleRequest, reqLog)
		}
		return
	}
	if !relay.recordResponseCacheDelivery(ctx, billing.ResponseCacheDeliveryEvent{
		RequestID: requestID, OrgID: orgID, Outcome: billing.DeliveryOutcomeCompleted,
		DeliveredBytes: int64(written), DeliveredChunks: 1, HTTPStatus: http.StatusOK,
	}, reqLog) {
		return
	}
	relay.recordResponseCacheCommercial(ctx, hit, identity, policy, requestID, orgID, requestedModel, preview, reqLog)
	settled, settleErr := relay.settlePendingBillingWithRetry(settleRequest, reqLog)
	if settleErr != nil {
		reqLog.Error("Response-cache zero-charge settlement retained for recovery: %v", settleErr)
		return
	}
	actual, charged := settledCosts(settled.Result)
	if actual != 0 || charged != 0 {
		reqLog.Error("Response-cache settlement violated zero-charge invariant: actual=%d charged=%d", actual, charged)
		return
	}
	relay.recordRequestWithUsage(context.WithoutCancel(ctx), requestID, orgID, keyID, route.Channel.ID,
		route.Channel.Provider, route.Channel.Name, model, requestedModel, clientIP,
		usage, charged, http.StatusOK, elapsedMilliseconds(requestStart), 0, false, 0, "", settled.Result)
	relay.recordTraceEvent(ctx, requestID, "response_cache.hit.delivered", "optimization", "info", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]any{
		"provider_requests": 0, "provider_cost_micros": "0", "customer_charge_micros": microsText(charged),
	})
}

// applyResponseCacheDeliveryIdentity prevents the cached provider execution
// identifier and timestamp from masquerading as the current UniRouter
// operation. The encrypted source bytes remain immutable evidence and are
// linked by source_response_sha256 in the commercial receipt.
func applyResponseCacheDeliveryIdentity(response map[string]any, requestID string, deliveredAt time.Time) {
	if response == nil || requestID == "" {
		return
	}
	response["id"] = "chatcmpl-cache-" + requestID
	response["created"] = deliveredAt.UTC().Unix()
}

// applyResponseCacheDeliveryUsage separates the immutable upstream source
// proof from the current cache delivery. A cache hit does not invoke a model,
// so downstream SDKs must not attribute the source request's historical token
// counters to the current request. The original response bytes and usage remain
// encrypted in the cache entry and bound by SourceResponseSHA256.
func applyResponseCacheDeliveryUsage(response map[string]any) {
	if response == nil {
		return
	}
	response["usage"] = map[string]any{
		"prompt_tokens":     0,
		"completion_tokens": 0,
		"total_tokens":      0,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": 0,
		},
	}
}

func (relay *Relay) settleFailedResponseCacheDelivery(request billing.SettleRequest, reqLog *logger.Logger) {
	settled, err := relay.settlePendingBillingWithRetry(request, reqLog)
	if err != nil || settled.Result == nil {
		if reqLog != nil {
			reqLog.Error("Response-cache failed-delivery zero settlement retained for recovery: %v", err)
		}
		return
	}
	actual, charged := settledCosts(settled.Result)
	if (actual != 0 || charged != 0) && reqLog != nil {
		reqLog.Error("Response-cache failed delivery violated zero-charge invariant: actual=%d charged=%d", actual, charged)
	}
}

func (relay *Relay) cancelResponseCacheReservation(ctx context.Context, requestID string, orgID int64, reqLog *logger.Logger) {
	if relay == nil || relay.billing == nil {
		return
	}
	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	err := relay.billing.Cancel(cancelCtx, requestID, orgID)
	cancel()
	if err != nil && reqLog != nil {
		// An ambiguous successful preparation is already settlement_pending and
		// deliberately non-cancellable; recovery remains authoritative there.
		reqLog.Warn("Response-cache pre-delivery reservation cancellation was not applied: %v", err)
	}
}

func (relay *Relay) recordResponseCacheCommercial(
	ctx context.Context,
	hit *responsecache.Hit,
	identity responsecache.Identity,
	policy invocationopt.Policy,
	requestID string,
	orgID int64,
	requestedModel string,
	preview *billing.SettleResult,
	reqLog *logger.Logger,
) {
	if relay == nil || relay.invocationOptimization == nil || hit == nil || preview == nil {
		return
	}
	avoidedCalculation, err := money.Calculate(
		money.Component{Tokens: hit.SourceProof.UncachedPromptTokens, PriceMicrosPerMillion: preview.EffectiveInputPriceMicrosPerMillion},
		money.Component{Tokens: hit.SourceProof.CacheReadTokens, PriceMicrosPerMillion: preview.EffectiveCacheReadPriceMicrosPerMillion},
		money.Component{Tokens: hit.SourceProof.CacheCreationTokens, PriceMicrosPerMillion: preview.EffectiveCacheCreationPriceMicrosPerMillion},
		money.Component{Tokens: hit.SourceProof.CompletionTokens, PriceMicrosPerMillion: preview.EffectiveOutputPriceMicrosPerMillion},
	)
	avoided := int64(0)
	if err != nil {
		if reqLog != nil {
			reqLog.Warn("Response-cache avoided-cost calculation was not recordable: %v", err)
		}
		return
	}
	avoided = avoidedCalculation.TotalMicros
	recordCtx, cancelRecord := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	err = relay.invocationOptimization.RecordCommercial(recordCtx, invocationopt.CommercialReceipt{
		OrgID: orgID, ProjectID: middleware.GetProjectIDFromContext(ctx), RequestID: requestID,
		SourceRequestID: hit.SourceProof.SourceRequestID, PolicyRef: policy.PolicyRef,
		PolicyRevision: policy.Revision, IdentityDigest: hit.IdentityDigest,
		SourceResponseSHA256: hit.SourceResponseSHA256, RequestedModel: requestedModel,
		ResolvedModel: identity.ResolvedModel, Provider: identity.Provider,
		ChannelID: identity.ChannelID, CredentialID: identity.CredentialID,
		CredentialEpoch: identity.CredentialEpoch, RuntimeAssignmentID: identity.RuntimeAssignmentID,
		CustomerPricingVersion:    identity.CustomerPricingVersion,
		ResponseCachePriceVersion: invocationopt.ResponseCachePriceVersion,
		ProviderRequests:          0, ProviderCostMicros: 0, CustomerChargeMicros: 0,
		AvoidedCustomerCostMicros: avoided, DeliveryStatus: responsecache.DeliveryCompleted,
		HTTPStatus: http.StatusOK, CreatedAt: time.Now().UTC(),
	})
	cancelRecord()
	if err != nil {
		if reqLog != nil {
			reqLog.Warn("Failed to persist response-cache commercial receipt: %v", err)
		}
	}
}

func (relay *Relay) recordResponseCacheDelivery(ctx context.Context, event billing.ResponseCacheDeliveryEvent, reqLog *logger.Logger) bool {
	for attempt := 1; attempt <= billingSettleMaxAttempts; attempt++ {
		eventCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		err := relay.billing.RecordResponseCacheDelivery(eventCtx, event)
		cancel()
		if err == nil {
			return true
		}
		if reqLog != nil {
			reqLog.Error("Response-cache delivery receipt %d/%d failed: %v", attempt, billingSettleMaxAttempts, err)
		}
	}
	return false
}

func (relay *Relay) commitResponseCacheMiss(
	ctx context.Context,
	state *responseCacheMissState,
	sourceResponse []byte,
	usage adapters.Usage,
	requestID string,
	providerRequests int64,
	reqLog *logger.Logger,
) {
	if relay == nil || relay.responseCache == nil || state == nil || state.reservation == nil {
		return
	}
	proof := responsecache.UsageProof{
		SourceRequestID: requestID,
		PromptTokens:    int64(usage.PromptTokens), UncachedPromptTokens: int64(usage.UncachedPromptTokens),
		CacheReadTokens: int64(usage.CacheReadTokens), CacheCreationTokens: int64(usage.CacheCreationTokens),
		CompletionTokens: int64(usage.CompletionTokens), UsageSource: usage.Source,
		UsageAuthority: usage.Authority, UsagePurpose: usage.Purpose, UsageFinality: usage.Finality,
		UsageContractVersion: usage.ContractVersion,
		CustomerPricing:      state.identity.CustomerPricingVersion,
		DeliveryFinality:     responsecache.DeliveryCompleted, SettlementFinality: responsecache.SettlementSettled,
		Provider: state.identity.Provider, MappedModel: state.identity.ResolvedModel,
		ProviderRequests: providerRequests, HTTPStatus: http.StatusOK,
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	err := relay.responseCache.Commit(commitCtx, state.reservation, responsecache.StoreEntry{
		SourceResponse: sourceResponse,
		Proof:          proof,
	})
	cancel()
	if err != nil && reqLog != nil {
		reqLog.Warn("Response-cache write skipped or failed safely: %v", err)
	}
}
