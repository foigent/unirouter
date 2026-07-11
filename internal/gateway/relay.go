package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/errdocs"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimeconfig"
	"github.com/uniroute/unirouter/internal/tokenutil"
	"github.com/uniroute/unirouter/internal/traceutil"
)

// BillingProvider defines the billing operations that Relay depends on.
// *billing.Billing satisfies this interface.
type BillingProvider interface {
	QueryModelPriceForOrg(ctx context.Context, orgID int64, model string) (float64, float64, error)
	CalculateCost(ctx context.Context, model string, promptTokens, completionTokens int) (int, error)
	Freeze(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error)
	MarkBillableOutput(ctx context.Context, requestID string, orgID int64) error
	PersistSettlementUsage(ctx context.Context, req billing.SettleRequest) error
	Settle(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error)
	Cancel(ctx context.Context, requestID string, orgID int64) error
}

// Relay handles forwarding requests to upstream providers.
type Relay struct {
	db       db.DBQuerier
	rdb      *redis.Client
	router   *Router
	billing  BillingProvider
	filter   *filter.DFAFilter
	encKey   []byte
	eventSvc *billing.EventService

	traceEventsEnabled         bool
	traceEventSampleRate       float64
	payloadCaptureMode         string
	evidenceContextTrustSecret string
}

// NewRelay creates a new Relay.
func NewRelay(db db.DBQuerier, rdb *redis.Client, router *Router, b BillingProvider, f *filter.DFAFilter, encKey []byte) *Relay {
	return &Relay{db: db, rdb: rdb, router: router, billing: b, filter: f, encKey: encKey, traceEventsEnabled: true, traceEventSampleRate: 1, payloadCaptureMode: traceutil.PayloadCaptureModeMetadataOnly}
}

// SetEventService configures PRD2 notification events for post-settle project checks.
func (relay *Relay) SetEventService(svc *billing.EventService) {
	relay.eventSvc = svc
}

// SetTraceEvents configures metadata-only request trace recording.
func (relay *Relay) SetTraceEvents(enabled bool, sampleRate float64) {
	relay.traceEventsEnabled = enabled
	switch {
	case sampleRate < 0:
		relay.traceEventSampleRate = 0
	case sampleRate > 1:
		relay.traceEventSampleRate = 1
	default:
		relay.traceEventSampleRate = sampleRate
	}
}

// SetPayloadCaptureMode configures payload evidence state. It does not enable
// raw prompt/response capture.
func (relay *Relay) SetPayloadCaptureMode(mode string) {
	if relay == nil {
		return
	}
	mode = traceutil.NormalizePayloadCaptureMode(mode)
	if !traceutil.IsSupportedPayloadCaptureMode(mode) {
		mode = traceutil.PayloadCaptureModeMetadataOnly
	}
	relay.payloadCaptureMode = mode
}

func (relay *Relay) currentPayloadCaptureMode() string {
	if relay == nil {
		return traceutil.PayloadCaptureModeMetadataOnly
	}
	mode := traceutil.NormalizePayloadCaptureMode(relay.payloadCaptureMode)
	if !traceutil.IsSupportedPayloadCaptureMode(mode) {
		return traceutil.PayloadCaptureModeMetadataOnly
	}
	return mode
}

// SetEvidenceContextTrustSecret enables trusted inbound evidence context
// headers. Empty keeps inbound evidence context disabled.
func (relay *Relay) SetEvidenceContextTrustSecret(secret string) {
	if relay == nil {
		return
	}
	relay.evidenceContextTrustSecret = strings.TrimSpace(secret)
}

func (relay *Relay) contextWithTrustedEvidence(ctx context.Context, header http.Header) context.Context {
	if relay == nil {
		return ctx
	}
	ctx = traceutil.ContextWithEvidence(ctx, traceutil.ExtractTrustedEvidenceContext(header, relay.evidenceContextTrustSecret))
	return contextWithEvidenceSpanState(ctx)
}

var upstreamRequestIDHeaderNames = []string{
	"X-Request-Id",
	"X-Request-ID",
	"Request-Id",
	"Request-ID",
	"OpenAI-Request-ID",
	"X-OpenAI-Request-ID",
	"Anthropic-Request-Id",
	"X-Anthropic-Request-Id",
	"Apim-Request-Id",
	"X-MS-Request-ID",
}

func upstreamRequestIDFromHeader(header http.Header) string {
	for _, name := range upstreamRequestIDHeaderNames {
		if requestID := sanitizeUpstreamRequestID(header.Get(name)); requestID != "" {
			return requestID
		}
	}
	return ""
}

func providerRetryDecision(willRetry bool) string {
	if willRetry {
		return "retry"
	}
	return "stop"
}

func providerFailureEventMessage(statusCode int, failureKind adapters.FailureKind) string {
	if statusCode > 0 {
		return fmt.Sprintf("Provider request failed with HTTP %d (%s)", statusCode, failureKind)
	}
	return fmt.Sprintf("Provider transport failed (%s)", failureKind)
}

func writeNonRetryableProviderError(w http.ResponseWriter, statusCode int, failureKind adapters.FailureKind) {
	if failureKind == adapters.FailureClientRequest && statusCode >= 400 && statusCode < 500 {
		writeError(w, statusCode, "invalid_request_error", "validation_error.invalid_request", "Upstream rejected the request")
		return
	}
	writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_error", "Upstream request failed")
}

func routingPolicyTraceMetadata(policy routingpolicy.Config, routeCount, routeIndex int) map[string]interface{} {
	return map[string]interface{}{
		"route_count":                       routeCount,
		"route_index":                       routeIndex,
		"effective_config_version":          runtimeconfig.Version,
		"routing_config_version":            routingpolicy.Version,
		"max_retry_count":                   policy.MaxRetryCount,
		"circuit_breaker_threshold":         policy.CircuitBreakerThreshold,
		"circuit_breaker_recover_seconds":   policy.CircuitBreakerRecoverSeconds,
		"circuit_breaker_half_open_percent": policy.CircuitBreakerHalfOpenPct,
		"rate_limit_cooldown_seconds":       policy.RateLimitCooldownSeconds,
		"auth_failure_cooldown_seconds":     policy.AuthFailureCooldownSeconds,
		"provider_key_failure_threshold":    policy.KeyFailureThreshold,
	}
}

func (relay *Relay) emitProviderChannelFailure(ctx context.Context, route RouteResult, requestID, requestedModel string, statusCode int, failureKind, message string) {
	if relay.eventSvc == nil {
		return
	}
	orgID := middleware.GetOrgIDFromContext(ctx)
	if orgID <= 0 || route.Channel.ID <= 0 {
		return
	}
	go func() {
		_, _ = relay.eventSvc.Emit(context.Background(), billing.EventInput{
			OrgID:      orgID,
			EventType:  billing.EventProviderChannelFailure,
			Severity:   billing.NotificationSeverityWarning,
			Title:      "Provider channel failure",
			Message:    message,
			Resource:   "channel",
			ResourceID: fmt.Sprintf("%d", route.Channel.ID),
			DedupeKey:  fmt.Sprintf("provider.channel.failure:%d:%d:%s", route.Channel.ID, statusCode, time.Now().UTC().Format("20060102T1504")),
			Payload: map[string]interface{}{
				"org_id":          orgID,
				"channel_id":      route.Channel.ID,
				"channel":         route.Channel.Name,
				"provider":        route.Channel.Provider,
				"adapter_type":    route.Channel.AdapterType,
				"request_id":      requestID,
				"requested_model": requestedModel,
				"mapped_model":    route.MappedModel,
				"status_code":     statusCode,
				"failure_kind":    failureKind,
				"message":         message,
			},
		})
	}()
}

func upstreamEndpoint(baseURL, endpoint string) string {
	return adapters.UpstreamEndpoint(baseURL, endpoint)
}

func elapsedMilliseconds(start time.Time) int {
	ms := int(time.Since(start).Milliseconds())
	if ms < 0 {
		return 0
	}
	return ms
}

const (
	billingSettleMaxAttempts = 3
	billingSettleRetryDelay  = 25 * time.Millisecond
)

type billingSettleOutcome struct {
	Result    *billing.SettleResult
	Settled   bool
	Cancelled bool
}

func settledCosts(result *billing.SettleResult) (actual, charged int) {
	if result == nil {
		return 0, 0
	}
	actual = result.ActualCost
	charged = result.ChargedCost
	// Compatibility for injected test/provider implementations that predate
	// the explicit charged-cost receipt.
	if charged == 0 && actual > 0 {
		charged = actual
	}
	return actual, charged
}

func (relay *Relay) settleBillingAttempts(req billing.SettleRequest, reqLog *logger.Logger) (billingSettleOutcome, error) {
	var lastErr error
	settleCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for attempt := 1; attempt <= billingSettleMaxAttempts; attempt++ {
		result, err := relay.billing.Settle(settleCtx, req)
		if err == nil && result != nil && result.Success {
			return billingSettleOutcome{Result: result, Settled: true}, nil
		}
		if err == nil {
			if result == nil {
				err = fmt.Errorf("settle returned nil result")
			} else {
				err = fmt.Errorf("settle unsuccessful: %s", result.ErrorCode)
			}
		}
		lastErr = err
		reqLog.Error("Billing settle attempt %d/%d failed: %v", attempt, billingSettleMaxAttempts, err)
		if attempt == billingSettleMaxAttempts {
			break
		}
		select {
		case <-settleCtx.Done():
			lastErr = settleCtx.Err()
			attempt = billingSettleMaxAttempts
		case <-time.After(billingSettleRetryDelay):
		}
	}
	return billingSettleOutcome{}, fmt.Errorf("settle failed after %d attempts: %w", billingSettleMaxAttempts, lastErr)
}

func (relay *Relay) settleBillingWithRetry(req billing.SettleRequest, reqLog *logger.Logger) (billingSettleOutcome, error) {
	outcome, settleErr := relay.settleBillingAttempts(req, reqLog)
	if settleErr == nil {
		return outcome, nil
	}

	cancelCtx, cancelCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCancel()
	if cancelErr := relay.billing.Cancel(cancelCtx, req.RequestID, req.OrgID); cancelErr != nil {
		return billingSettleOutcome{}, fmt.Errorf("%w; cancel failed: %v", settleErr, cancelErr)
	}
	return billingSettleOutcome{Cancelled: true}, fmt.Errorf("%w; frozen amount cancelled", settleErr)
}

func (relay *Relay) settlePendingBillingWithRetry(req billing.SettleRequest, reqLog *logger.Logger) (billingSettleOutcome, error) {
	outcome, err := relay.settleBillingAttempts(req, reqLog)
	if err == nil {
		return outcome, nil
	}
	return billingSettleOutcome{}, fmt.Errorf("%w; billable output retained for recovery", err)
}

func (relay *Relay) persistSettlementUsageWithRetry(req billing.SettleRequest, reqLog *logger.Logger) error {
	var lastErr error
	persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for attempt := 1; attempt <= billingSettleMaxAttempts; attempt++ {
		if err := relay.billing.PersistSettlementUsage(persistCtx, req); err == nil {
			return nil
		} else {
			lastErr = err
			reqLog.Error("Billing usage persistence attempt %d/%d failed: %v", attempt, billingSettleMaxAttempts, err)
		}
		if attempt < billingSettleMaxAttempts {
			select {
			case <-persistCtx.Done():
				return fmt.Errorf("persist settlement usage: %w", persistCtx.Err())
			case <-time.After(billingSettleRetryDelay):
			}
		}
	}
	return fmt.Errorf("persist settlement usage failed after %d attempts: %w", billingSettleMaxAttempts, lastErr)
}

// ChatCompletionRequest represents an incoming chat completion request.
type ChatCompletionRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	Stream    bool      `json:"stream"`
	MaxTokens *int      `json:"max_tokens,omitempty"`
}

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func modelAllowedForAPIKey(apiKey *models.ApiKey, model, requestedModel string) bool {
	if apiKey == nil || apiKey.AllowedModels == "" {
		return true
	}
	for _, allowedModel := range strings.Split(apiKey.AllowedModels, ",") {
		allowedModel = strings.TrimSpace(allowedModel)
		if allowedModel == "*" || allowedModel == model || allowedModel == requestedModel {
			return true
		}
	}
	return false
}

func (relay *Relay) rejectModelNotAllowed(w http.ResponseWriter, ctx context.Context, requestID, model, requestedModel string, requestStart time.Time) {
	relay.recordTraceEvent(ctx, requestID, "auth.model_not_allowed", "auth", "warning", model, requestedModel, nil, http.StatusForbidden, elapsedMilliseconds(requestStart), map[string]interface{}{
		"error_code": "auth_error.model_not_allowed",
	})
	writeError(w, http.StatusForbidden, "permission_error", "auth_error.model_not_allowed",
		fmt.Sprintf("Model '%s' is not allowed for this API key", model))
}

// HandleChatCompletion handles POST /v1/chat/completions.
func (relay *Relay) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	ctx := relay.contextWithTrustedEvidence(r.Context(), r.Header)
	requestStart := time.Now()
	requestID := middleware.GetRequestIDFromContext(ctx)
	w.Header().Set("X-Request-ID", requestID)

	// Parse request body
	var req ChatCompletionRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", "Failed to read request body")
		return
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", "Invalid JSON body")
		return
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", "model is required")
		return
	}
	requestedModel := middleware.GetModelAliasFromContext(ctx)
	if requestedModel == "" {
		requestedModel = req.Model
	}

	// Check sensitive words in messages
	if relay.filter != nil && relay.filter.WordCount() > 0 {
		for _, msg := range req.Messages {
			if matched, _ := relay.filter.IsSensitive(msg.Content); matched {
				writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.sensitive_word_detected", "Request contains prohibited content")
				return
			}
		}
	}

	// Check model permission before billing
	callerAPIKey := middleware.GetAPIKeyFromContext(ctx)
	if !modelAllowedForAPIKey(callerAPIKey, req.Model, requestedModel) {
		relay.rejectModelNotAllowed(w, ctx, requestID, req.Model, requestedModel, requestStart)
		return
	}

	// Estimate tokens for billing freeze
	estimatedTokens := estimateTokens(req.Messages, req.MaxTokens)
	orgID := middleware.GetOrgIDFromContext(ctx)
	keyID := middleware.GetKeyIDFromContext(ctx)
	reqLog := logger.New(logger.Fields{RequestID: requestID, OrgID: uint(orgID), Module: "chat"})
	relay.recordTraceEvent(ctx, requestID, "gateway.request.accepted", "gateway", "info", req.Model, requestedModel, nil, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
		"stream":           req.Stream,
		"estimated_tokens": estimatedTokens,
	})
	relay.recordPayloadPolicyDecision(ctx, requestID, req.Model, requestedModel, elapsedMilliseconds(requestStart))

	// Freeze balance once (before trying any upstream)
	freezeResult, err := relay.billing.Freeze(ctx, billing.FreezeRequest{
		RequestID:       requestID,
		OrgID:           orgID,
		Model:           req.Model,
		EstimatedTokens: estimatedTokens,
		SafetyFactor:    1.5,
		MinFreezeAmount: 10,
	})
	if err != nil {
		reqLog.Error("Billing freeze error: %v", err)
		relay.recordTraceEvent(ctx, requestID, "billing.freeze.failed", "billing", "error", req.Model, requestedModel, nil, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code": "freeze_error",
		})
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to process billing")
		return
	}
	if !freezeResult.Success {
		relay.recordTraceEvent(ctx, requestID, "billing.freeze.rejected", "billing", "warning", req.Model, requestedModel, nil, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code": freezeResult.ErrorCode,
		})
		switch freezeResult.ErrorCode {
		case "insufficient_balance":
			middleware.RecordBillingFreeze("error")
			writeError(w, http.StatusPaymentRequired, "billing_error", "billing_error.insufficient_balance", "Insufficient balance")
		case "already_frozen":
			middleware.RecordBillingFreeze("error")
			writeError(w, http.StatusConflict, "billing_error", "billing_error.idempotent_conflict", "Request already being processed")
		default:
			middleware.RecordBillingFreeze("error")
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", freezeResult.ErrorCode)
		}
		return
	}
	middleware.RecordBillingFreeze("success")
	relay.recordTraceEvent(ctx, requestID, "billing.freeze.succeeded", "billing", "info", req.Model, requestedModel, nil, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
		"freeze_amount_cents": freezeResult.Amount,
	})

	// Get all available routes for failover
	routes, err := relay.router.RouteAll(ctx, req.Model)
	if err != nil {
		relay.billing.Cancel(ctx, requestID, orgID)
		writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", fmt.Sprintf("No available channel for model: %s", req.Model))
		relay.recordTraceEvent(ctx, requestID, "routing.no_route", "routing", "error", req.Model, requestedModel, nil, 503, elapsedMilliseconds(requestStart), map[string]interface{}{
			"route_count": 0,
		})
		relay.recordRequest(ctx, requestID, orgID, keyID, 0, "", "", req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, 503, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceNoRoute, estimatedTokens, "")
		return
	}

	client := &http.Client{Timeout: 300 * time.Second}
	adapterRegistry := adapters.DefaultRegistry()
	routingPolicy := relay.router.Policy()
	providerAttempts := 0

	// Failover loop: try each route until one succeeds
	var lastErr error
	var lastStatusCode int
	var lastUpstreamRequestID string
	var lastRoute *RouteResult
	lastAttemptCtx := ctx
	for routeIndex, route := range routes {
		attemptCtx := contextWithAttemptID(ctx, newAttemptID())
		attemptCtx = contextWithRouteAttempt(attemptCtx, routeIndex+1, len(routes))
		lastAttemptCtx = attemptCtx
		lastRoute = &route
		lastStatusCode = 0
		lastUpstreamRequestID = ""
		routeForTrace := route
		relay.recordTraceEvent(attemptCtx, requestID, "routing.route_selected", "routing", "info", req.Model, requestedModel, &routeForTrace, 0, elapsedMilliseconds(requestStart), routingPolicyTraceMetadata(routingPolicy, len(routes), routeIndex+1))
		apiKey, err := auth.DecryptKey(route.ChannelKey.KeyEnc, relay.encKey)
		if err != nil {
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Failed to decrypt key: %v", err)
			relay.recordTraceEvent(attemptCtx, requestID, "provider.key_decrypt_failed", "provider", "error", req.Model, requestedModel, &routeForTrace, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "auth",
			})
			lastErr = err
			continue
		}

		adapter, err := adapterRegistry.Get(route.Channel.AdapterType)
		if err != nil {
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Unknown adapter_type %q: %v", route.Channel.AdapterType, err)
			relay.recordTraceEvent(attemptCtx, requestID, "provider.adapter_unknown", "provider", "error", req.Model, requestedModel, &routeForTrace, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "configuration",
			})
			lastErr = err
			continue
		}
		upstreamReq, err := adapter.BuildRequest(attemptCtx, adapters.RequestInput{
			Endpoint:       adapters.EndpointChatCompletions,
			DownstreamBody: body,
			RequestedModel: requestedModel,
			MappedModel:    route.MappedModel,
			Channel:        route.Channel,
			APIKey:         apiKey,
			RequestID:      requestID,
		})
		if err != nil {
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Failed to create upstream request: %v", err)
			relay.recordTraceEvent(attemptCtx, requestID, "provider.request_build_failed", "provider", "error", req.Model, requestedModel, &routeForTrace, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "request_build",
			})
			lastErr = err
			continue
		}

		providerAttempts++
		upstreamStart := time.Now()
		resp, err := client.Do(upstreamReq)
		upstreamDuration := time.Since(upstreamStart)
		middleware.RecordUpstreamLatency(route.Channel.Provider, req.Model, upstreamDuration)
		if err != nil {
			classified := adapters.ClassifyTransportError(attemptCtx, err)
			willRetry := classified.Retryable && providerAttempts <= routingPolicy.MaxRetryCount && routeIndex+1 < len(routes)
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Upstream transport failed: kind=%s retryable=%t", classified.FailureKind, classified.Retryable)
			if classified.Retryable {
				relay.router.MarkChannelFail(ctx, route.Channel.ID)
				relay.router.MarkKeyFail(ctx, route.ChannelKey.ID)
				relay.emitProviderChannelFailure(attemptCtx, route, requestID, requestedModel, 0, string(classified.FailureKind), providerFailureEventMessage(0, classified.FailureKind))
			}
			relay.recordTraceEvent(attemptCtx, requestID, "provider.request_failed", "provider", "warning", req.Model, requestedModel, &routeForTrace, 0, int(upstreamDuration.Milliseconds()), map[string]interface{}{
				"failure_kind":   string(classified.FailureKind),
				"retryable":      classified.Retryable,
				"retry_decision": providerRetryDecision(willRetry),
			})
			lastErr = fmt.Errorf("upstream transport failed (%s)", classified.FailureKind)
			if classified.Retryable {
				if willRetry {
					continue
				}
				break
			}

			cleanupCtx := context.WithoutCancel(attemptCtx)
			relay.billing.Cancel(cleanupCtx, requestID, orgID)
			relay.recordRequest(cleanupCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, 0, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceUpstreamFailed, estimatedTokens, "")
			if attemptCtx.Err() == nil {
				writeNonRetryableProviderError(w, 0, classified.FailureKind)
			}
			return
		}
		upstreamRequestID := upstreamRequestIDFromHeader(resp.Header)
		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(resp.Body)
			// Per PRD F12: adapters normalize provider-specific HTTP semantics
			// into routing failure kinds. Client errors are not channel failures.
			classified := adapter.ClassifyHTTPStatus(resp.StatusCode, errBody)
			willRetry := classified.Retryable && providerAttempts <= routingPolicy.MaxRetryCount && routeIndex+1 < len(routes)
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Upstream rejected response: status=%d kind=%s retryable=%t", resp.StatusCode, classified.FailureKind, classified.Retryable)
			switch classified.FailureKind {
			case adapters.FailureChannel:
				relay.router.MarkChannelFail(ctx, route.Channel.ID)
				relay.router.MarkKeyFail(ctx, route.ChannelKey.ID)
				relay.emitProviderChannelFailure(attemptCtx, route, requestID, requestedModel, resp.StatusCode, string(classified.FailureKind), providerFailureEventMessage(resp.StatusCode, classified.FailureKind))
			case adapters.FailureAuth:
				// Auth failure — mark key as invalid immediately
				relay.router.MarkKeyAuthFailure(ctx, route.ChannelKey.ID)
				relay.emitProviderChannelFailure(attemptCtx, route, requestID, requestedModel, resp.StatusCode, string(classified.FailureKind), providerFailureEventMessage(resp.StatusCode, classified.FailureKind))
			case adapters.FailureRateLimit:
				// Rate limited — cooldown key for 60s
				relay.router.MarkKeyRateLimited(ctx, route.ChannelKey.ID)
				relay.emitProviderChannelFailure(attemptCtx, route, requestID, requestedModel, resp.StatusCode, string(classified.FailureKind), providerFailureEventMessage(resp.StatusCode, classified.FailureKind))
			default:
				// 4xx client errors (400, 404, etc.) — don't mark as failure
			}
			relay.recordTraceEvent(attemptCtx, requestID, "provider.response_rejected", "provider", "warning", req.Model, requestedModel, &routeForTrace, resp.StatusCode, int(upstreamDuration.Milliseconds()), map[string]interface{}{
				"failure_kind":   string(classified.FailureKind),
				"retryable":      classified.Retryable,
				"retry_decision": providerRetryDecision(willRetry),
			}, upstreamRequestID)
			lastStatusCode = resp.StatusCode
			lastUpstreamRequestID = upstreamRequestID
			lastErr = fmt.Errorf("upstream returned HTTP %d (%s)", resp.StatusCode, classified.FailureKind)
			resp.Body.Close()
			if classified.Retryable {
				if willRetry {
					continue
				}
				break
			}

			relay.billing.Cancel(attemptCtx, requestID, orgID)
			relay.recordRequest(attemptCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, resp.StatusCode, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceUpstreamFailed, estimatedTokens, upstreamRequestID)
			writeNonRetryableProviderError(w, resp.StatusCode, classified.FailureKind)
			return
		}

		// Success — mark channel healthy and forward
		relay.router.MarkChannelSuccess(ctx, route.Channel.ID)
		relay.router.MarkKeySuccess(ctx, route.ChannelKey.ID)
		relay.recordTraceEvent(attemptCtx, requestID, "provider.response_accepted", "provider", "info", req.Model, requestedModel, &routeForTrace, resp.StatusCode, int(upstreamDuration.Milliseconds()), map[string]interface{}{}, upstreamRequestID)

		if req.Stream {
			relay.relayStreamRequest(attemptCtx, w, resp, &route, requestID, orgID, keyID, req.Model, requestedModel, r.RemoteAddr, req.Messages, reqLog, requestStart, estimatedTokens, adapter, upstreamRequestID)
		} else {
			relay.relayNonStreamRequest(attemptCtx, w, resp, &route, requestID, orgID, keyID, req.Model, requestedModel, r.RemoteAddr, reqLog, requestStart, adapter, estimatedTokens, upstreamRequestID)
		}
		return
	}

	// All channels failed
	relay.billing.Cancel(ctx, requestID, orgID)
	if lastErr != nil {
		reqLog.Error("All upstream channels failed, last error: %v", lastErr)
	}
	// Log the failed request
	if lastRoute != nil {
		relay.recordRequest(lastAttemptCtx, requestID, orgID, keyID, lastRoute.Channel.ID, lastRoute.Channel.Provider, lastRoute.Channel.Name, req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, lastStatusCode, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceUpstreamFailed, estimatedTokens, lastUpstreamRequestID)
	}
	if lastStatusCode > 0 {
		writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_error", fmt.Sprintf("All upstream channels failed (last status: %d)", lastStatusCode))
	} else {
		writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_error", "All upstream channels failed")
	}
}

// relayNonStreamRequest handles non-streaming responses.
func (relay *Relay) relayNonStreamRequest(ctx context.Context, w http.ResponseWriter, resp *http.Response, route *RouteResult, requestID string, orgID, keyID int64, model, requestedModel, clientIP string, reqLog *logger.Logger, requestStart time.Time, adapter adapters.Adapter, estimatedTokens int, upstreamRequestID string) {

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		reqLog.Error("Failed to read response body: %v", err)
		relay.billing.Cancel(ctx, requestID, orgID)
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to read upstream response")
		return
	}

	if adapter == nil {
		adapter = adapters.NewOpenAICompatible()
	}
	normalized, err := adapter.ParseResponse(ctx, adapters.ResponseInput{
		StatusCode:     resp.StatusCode,
		Header:         resp.Header,
		Body:           body,
		Channel:        route.Channel,
		RequestedModel: requestedModel,
		UpstreamModel:  route.MappedModel,
	})
	if err != nil {
		reqLog.Error("Failed to parse adapter response: %v", err)
		relay.billing.Cancel(ctx, requestID, orgID)
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to parse upstream response")
		return
	}

	// Settle from the immutable effective price/multiplier snapshot captured by
	// Freeze. A later catalog edit or lookup outage must not rewrite or block an
	// already accepted operation.
	settleOutcome, err := relay.settleBillingWithRetry(billing.SettleRequest{
		RequestID:        requestID,
		OrgID:            orgID,
		PromptTokens:     normalized.Usage.PromptTokens,
		CompletionTokens: normalized.Usage.CompletionTokens,
		UsageSource:      normalized.Usage.Source,
	}, reqLog)
	if err != nil {
		reqLog.Error("Billing settle failed; response withheld: %v", err)
		relay.recordTraceEvent(ctx, requestID, "billing.settle.failed", "billing", "error", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code": "settle_failed",
		})
		writeError(w, http.StatusInternalServerError, "server_error", "billing_error.settle_failed", "Failed to settle billing")
		return
	}
	actualCost, chargedCost := settledCosts(settleOutcome.Result)
	relay.recordTraceEvent(ctx, requestID, "billing.settle.succeeded", "billing", "info", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
		"prompt_tokens":      normalized.Usage.PromptTokens,
		"completion_tokens":  normalized.Usage.CompletionTokens,
		"total_tokens":       normalized.Usage.PromptTokens + normalized.Usage.CompletionTokens,
		"cost_cents":         actualCost,
		"usage_source":       normalized.Usage.Source,
		"usage_confidence":   metering.SourceConfidence(normalized.Usage.Source),
		"charged_cost_cents": chargedCost,
		"pricing_version":    settleOutcome.Result.PricingVersion,
	})
	middleware.RecordTokenUsage("prompt", model, normalized.Usage.PromptTokens)
	middleware.RecordTokenUsage("completion", model, normalized.Usage.CompletionTokens)

	// Record request log
	relay.recordRequest(ctx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, model, requestedModel, clientIP,
		normalized.Usage.PromptTokens, normalized.Usage.CompletionTokens, chargedCost, resp.StatusCode, elapsedMilliseconds(requestStart), 0, false, normalized.Usage.Source, estimatedTokens, upstreamRequestID)
	relay.checkProjectBudgetAlert(ctx)

	// Build response with _uniroute extension field
	var responseMap map[string]interface{}
	responseBody := normalized.Body
	if len(responseBody) == 0 {
		responseBody = body
	}
	json.Unmarshal(responseBody, &responseMap)
	attempt := routeAttemptFromContext(ctx)
	responseMap["_uniroute"] = map[string]interface{}{
		"request_id": requestID,
		"cost": map[string]interface{}{
			"actual":    actualCost,
			"charged":   chargedCost,
			"cache_hit": false,
		},
		"provider":      route.Channel.Provider,
		"channel":       route.Channel.Name,
		"cache_hit":     false,
		"fallback":      attempt.Sequence > 1,
		"attempt_count": attempt.Sequence,
		"route_count":   attempt.Total,
		"attempt_id":    attemptIDFromContext(ctx),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	json.NewEncoder(w).Encode(responseMap)
}

// relayStreamRequest handles streaming SSE responses.
// Balance check goroutine signals via a channel; the main loop owns all writes
// and the upstream body lifecycle. No goroutine touches resp.Body directly.
func (relay *Relay) relayStreamRequest(ctx context.Context, w http.ResponseWriter, resp *http.Response, route *RouteResult, requestID string, orgID, keyID int64, model, requestedModel, clientIP string, messages []Message, reqLog *logger.Logger, requestStart time.Time, estimatedTokens int, adapter adapters.Adapter, upstreamRequestID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		reqLog.Error("ResponseWriter does not support Flusher")
		resp.Body.Close()
		cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := relay.billing.Cancel(cancelCtx, requestID, orgID); err != nil {
			reqLog.Error("Failed to cancel pre-output reservation: %v", err)
		}
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Streaming is not supported by this response writer")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Periodic balance check — signal main loop via channel instead of closing body directly.
	// This eliminates the race condition where two goroutines both call resp.Body.Close().
	balanceExhausted := make(chan struct{}, 1)
	balanceCancelCtx, balanceCancel := context.WithCancel(ctx)
	defer balanceCancel()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-balanceCancelCtx.Done():
				return
			case <-ticker.C:
				var balance int
				err := relay.db.QueryRow(balanceCancelCtx,
					`SELECT COALESCE(w.balance + w.granted_balance, 0) FROM wallets w WHERE w.org_id = $1`, orgID).Scan(&balance)
				if err != nil {
					reqLog.Error("Balance check error: %v", err)
					continue
				}
				if balance <= 0 {
					reqLog.Warn("Balance exhausted during streaming, signaling main loop")
					select {
					case balanceExhausted <- struct{}{}:
					default:
					}
					return
				}
			}
		}
	}()

	// SSE write mutex — protect concurrent writes from heartbeat and main loop
	var writeMu sync.Mutex
	billableOutputMarked := false
	preOutputBillingFailure := false
	markBillableOutput := func() bool {
		if billableOutputMarked {
			return true
		}
		markerCtx, markerCancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := relay.billing.MarkBillableOutput(markerCtx, requestID, orgID)
		markerCancel()
		if err == nil {
			billableOutputMarked = true
			return true
		}

		preOutputBillingFailure = true
		reqLog.Error("Failed to persist billable output marker; stream content withheld: %v", err)
		relay.recordTraceEvent(ctx, requestID, "billing.output_marker.failed", "billing", "error", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code": "output_marker_failed",
		})
		cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if cancelErr := relay.billing.Cancel(cancelCtx, requestID, orgID); cancelErr != nil {
			reqLog.Error("Failed to cancel reservation after output marker failure: %v", cancelErr)
		}
		cancel()
		writeMu.Lock()
		fmt.Fprintf(w, "data: {\"error\":{\"type\":\"billing_error\",\"code\":\"billing_error.settle_failed\",\"message\":\"Unable to establish durable billing state\"}}\n\n")
		flusher.Flush()
		writeMu.Unlock()
		resp.Body.Close()
		return false
	}

	// SSE heartbeat — send ping every 15 seconds to prevent client timeout
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	var heartbeatWg sync.WaitGroup
	heartbeatWg.Add(1)
	go func() {
		defer heartbeatWg.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				writeMu.Lock()
				fmt.Fprintf(w, ": ping\n\n")
				flusher.Flush()
				writeMu.Unlock()
			}
		}
	}()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	tokenCounter := tokenutil.NewStreamTokenCounter()
	usedUpstreamUsage := false
	ttftMs := 0
	streamStart := time.Now()
	firstTokenReceived := false
	var streamNormalizer adapters.StreamNormalizer
	if adapter != nil {
		streamNormalizer, _ = adapter.(adapters.StreamNormalizer)
	}
	streamState := &adapters.StreamState{}

	done := false
	for !done && scanner.Scan() {
		line := scanner.Text()

		if streamNormalizer != nil {
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "event:") || strings.HasPrefix(line, ":") {
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				done = true
				continue
			}
			chunk, err := streamNormalizer.NormalizeStreamChunk(ctx, adapters.StreamChunkInput{
				Data:           []byte(data),
				Channel:        route.Channel,
				RequestedModel: requestedModel,
				UpstreamModel:  route.MappedModel,
				State:          streamState,
			})
			if err != nil {
				reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Failed to normalize stream chunk: %v", err)
				continue
			}
			if chunk == nil {
				continue
			}
			if chunk.HasUsage {
				tokenCounter.SetUpstreamUsage(chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens)
				usedUpstreamUsage = true
			}
			if !usedUpstreamUsage && chunk.TextDelta != "" {
				tokenCounter.AddOutputDelta(chunk.TextDelta)
			}
			if !chunk.Skip && len(chunk.Data) > 0 {
				if !markBillableOutput() {
					done = true
					continue
				}
				if !firstTokenReceived {
					firstTokenReceived = true
					ttftMs = int(time.Since(streamStart).Milliseconds())
				}
				writeMu.Lock()
				fmt.Fprintf(w, "data: %s\n\n", chunk.Data)
				writeMu.Unlock()
				flusher.Flush()
			}
			if chunk.Done {
				done = true
			}
			if done {
				continue
			}
			// Check for balance exhaustion signal from background goroutine
			select {
			case <-balanceExhausted:
				reqLog.Warn("Balance exhausted at %d output tokens, sending error", tokenCounter.OutputTokens())
				writeMu.Lock()
				fmt.Fprintf(w, "data: {\"error\":{\"type\":\"insufficient_balance\",\"message\":\"Balance exhausted\"}}\n\n")
				flusher.Flush()
				writeMu.Unlock()
				resp.Body.Close()
				done = true
			default:
			}

			// Token-count-based balance check — every 100 output tokens (inline, no extra goroutine)
			if !done && tokenCounter.ShouldCheckBalance() {
				var balance int
				err := relay.db.QueryRow(ctx,
					`SELECT COALESCE(w.balance + w.granted_balance, 0) FROM wallets w WHERE w.org_id = $1`, orgID).Scan(&balance)
				if err == nil && balance <= 0 {
					reqLog.Warn("Balance exhausted at %d output tokens, closing upstream", tokenCounter.OutputTokens())
					writeMu.Lock()
					fmt.Fprintf(w, "data: {\"error\":{\"type\":\"insufficient_balance\",\"message\":\"Balance exhausted\"}}\n\n")
					flusher.Flush()
					writeMu.Unlock()
					resp.Body.Close()
					done = true
				}
			}
			continue
		}

		// Track TTFT
		if !firstTokenReceived && strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
			firstTokenReceived = true
			ttftMs = int(time.Since(streamStart).Milliseconds())
		}

		if line == "data: [DONE]" {
			done = true
			continue
		}

		// Persist the irreversible billing marker before forwarding the first
		// upstream data event. Heartbeats and event labels are non-billable and
		// may be sent earlier, but model data may not cross this barrier.
		if strings.HasPrefix(line, "data: ") && !markBillableOutput() {
			done = true
			continue
		}

		// Forward the upstream line as-is, except for [DONE]. UniRouter sends
		// its final usage/cost extension before the single terminal [DONE].
		writeMu.Lock()
		fmt.Fprintf(w, "%s\n", line)
		writeMu.Unlock()
		flusher.Flush()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Try to parse upstream usage (sent in the final chunk when stream_options.include_usage=true)
		var usageChunk struct {
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage,omitempty"`
		}
		if json.Unmarshal([]byte(data), &usageChunk) == nil && usageChunk.Usage != nil && usageChunk.Usage.PromptTokens > 0 {
			tokenCounter.SetUpstreamUsage(usageChunk.Usage.PromptTokens, usageChunk.Usage.CompletionTokens)
			usedUpstreamUsage = true
			continue
		}

		// Fallback: estimate output tokens from delta content
		if !usedUpstreamUsage {
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(data), &chunk) == nil {
				for _, choice := range chunk.Choices {
					tokenCounter.AddOutputDelta(choice.Delta.Content)
				}
			}
		}

		// Check for balance exhaustion signal from background goroutine
		select {
		case <-balanceExhausted:
			reqLog.Warn("Balance exhausted at %d output tokens, sending error", tokenCounter.OutputTokens())
			writeMu.Lock()
			fmt.Fprintf(w, "data: {\"error\":{\"type\":\"insufficient_balance\",\"message\":\"Balance exhausted\"}}\n\n")
			flusher.Flush()
			writeMu.Unlock()
			resp.Body.Close()
			done = true
		default:
		}

		// Token-count-based balance check — every 100 output tokens (inline, no extra goroutine)
		if !done && tokenCounter.ShouldCheckBalance() {
			var balance int
			err := relay.db.QueryRow(ctx,
				`SELECT COALESCE(w.balance + w.granted_balance, 0) FROM wallets w WHERE w.org_id = $1`, orgID).Scan(&balance)
			if err == nil && balance <= 0 {
				reqLog.Warn("Balance exhausted at %d output tokens, closing upstream", tokenCounter.OutputTokens())
				writeMu.Lock()
				fmt.Fprintf(w, "data: {\"error\":{\"type\":\"insufficient_balance\",\"message\":\"Balance exhausted\"}}\n\n")
				flusher.Flush()
				writeMu.Unlock()
				resp.Body.Close()
				done = true
			}
		}
	}

	// Stop heartbeat
	heartbeatCancel()
	heartbeatWg.Wait()
	if scanErr := scanner.Err(); scanErr != nil && !preOutputBillingFailure {
		reqLog.Error("Upstream stream ended with a read error: %v", scanErr)
	}
	if preOutputBillingFailure {
		return
	}
	recordCtx := context.WithoutCancel(ctx)

	// If upstream didn't provide usage, estimate input tokens from the original messages
	if !usedUpstreamUsage {
		var msgPairs []struct{ Role, Content string }
		for _, m := range messages {
			msgPairs = append(msgPairs, struct{ Role, Content string }{Role: m.Role, Content: m.Content})
		}
		tokenCounter.SetEstimatedInputTokens(tokenutil.EstimateRequestTokens(msgPairs))
	}

	usageSource := metering.UsageSourceCounterV2
	if usedUpstreamUsage {
		usageSource = metering.UsageSourceUpstream
	}
	var settleErr error
	actualCost := 0
	chargedCost := 0
	billingStatus := "settled"
	settleReq := billing.SettleRequest{
		RequestID:        requestID,
		OrgID:            orgID,
		PromptTokens:     tokenCounter.InputTokens(),
		CompletionTokens: tokenCounter.OutputTokens(),
		UsageSource:      usageSource,
	}

	if !billableOutputMarked {
		cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		settleErr = relay.billing.Cancel(cancelCtx, requestID, orgID)
		cancel()
		if settleErr != nil {
			billingStatus = "cancellation_failed_no_billable_output"
			reqLog.Error("Failed to cancel stream with no billable output: %v", settleErr)
		} else {
			billingStatus = "cancelled_no_billable_output"
		}
		relay.recordTraceEvent(recordCtx, requestID, "billing.cancelled.no_billable_output", "billing", "info", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
			"billing_status": billingStatus,
		})
	} else {
		if persistErr := relay.persistSettlementUsageWithRetry(settleReq, reqLog); persistErr != nil {
			billingStatus = "settlement_pending_usage_unpersisted"
			reqLog.Error("Final stream usage was not durably persisted; reservation retained: %v", persistErr)
			relay.recordTraceEvent(recordCtx, requestID, "billing.settlement_usage.failed", "billing", "critical", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
				"billing_status": billingStatus,
				"error_code":     "settlement_usage_persist_failed",
			})
			middleware.RecordTokenUsage("prompt", model, tokenCounter.InputTokens())
			middleware.RecordTokenUsage("completion", model, tokenCounter.OutputTokens())
			middleware.RecordUpstreamLatency(route.Channel.Provider, model, time.Since(streamStart))
			relay.recordRequest(recordCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, model, requestedModel, clientIP,
				tokenCounter.InputTokens(), tokenCounter.OutputTokens(), 0, http.StatusOK, elapsedMilliseconds(requestStart), ttftMs, true, usageSource, estimatedTokens, upstreamRequestID)
			writeMu.Lock()
			fmt.Fprintf(w, "data: {\"error\":{\"type\":\"billing_error\",\"code\":\"billing_error.settle_failed\",\"message\":\"Usage is pending durable reconciliation\",\"request_id\":%q,\"billing_status\":%q}}\n\n", requestID, billingStatus)
			flusher.Flush()
			writeMu.Unlock()
			// Deliberately omit [DONE]: the model output was delivered, but the
			// terminal billing receipt is not yet durable.
			return
		}

		settleOutcome, pendingSettleErr := relay.settlePendingBillingWithRetry(settleReq, reqLog)
		settleErr = pendingSettleErr
		if settleErr != nil {
			billingStatus = "settlement_pending_recovery"
			reqLog.Error("Billing settle failed after stream completed; recovery retained the reservation: %v", settleErr)
			relay.recordTraceEvent(recordCtx, requestID, "billing.settle.pending_recovery", "billing", "error", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
				"billing_status": billingStatus,
				"error_code":     "settle_failed",
			})
		} else {
			actualCost, chargedCost = settledCosts(settleOutcome.Result)
			relay.recordTraceEvent(recordCtx, requestID, "billing.settle.succeeded", "billing", "info", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
				"prompt_tokens":      tokenCounter.InputTokens(),
				"completion_tokens":  tokenCounter.OutputTokens(),
				"total_tokens":       tokenCounter.InputTokens() + tokenCounter.OutputTokens(),
				"cost_cents":         actualCost,
				"charged_cost_cents": chargedCost,
				"usage_source":       usageSource,
				"usage_confidence":   metering.SourceConfidence(usageSource),
				"billing_status":     billingStatus,
				"ttft_ms":            ttftMs,
			})
		}
	}
	middleware.RecordTokenUsage("prompt", model, tokenCounter.InputTokens())
	middleware.RecordTokenUsage("completion", model, tokenCounter.OutputTokens())

	streamDuration := time.Since(streamStart)
	middleware.RecordUpstreamLatency(route.Channel.Provider, model, streamDuration)

	relay.recordRequest(recordCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, model, requestedModel, clientIP,
		tokenCounter.InputTokens(), tokenCounter.OutputTokens(), chargedCost, http.StatusOK, elapsedMilliseconds(requestStart), ttftMs, true, usageSource, estimatedTokens, upstreamRequestID)
	if settleErr == nil && billingStatus == "settled" {
		relay.checkProjectBudgetAlert(recordCtx)
	}

	// Send usage + _uniroute extension as the final data chunk before [DONE].
	attempt := routeAttemptFromContext(ctx)
	unirouteData := map[string]interface{}{
		"request_id": requestID,
		"cost": map[string]interface{}{
			"actual":    actualCost,
			"charged":   chargedCost,
			"cache_hit": false,
		},
		"provider":       route.Channel.Provider,
		"channel":        route.Channel.Name,
		"ttft_ms":        ttftMs,
		"cache_hit":      false,
		"fallback":       attempt.Sequence > 1,
		"attempt_count":  attempt.Sequence,
		"route_count":    attempt.Total,
		"attempt_id":     attemptIDFromContext(ctx),
		"billing_status": billingStatus,
	}
	finalChunk := map[string]interface{}{
		"object":  "chat.completion.chunk",
		"model":   model,
		"choices": []interface{}{},
		"usage": map[string]int{
			"prompt_tokens":     tokenCounter.InputTokens(),
			"completion_tokens": tokenCounter.OutputTokens(),
			"total_tokens":      tokenCounter.InputTokens() + tokenCounter.OutputTokens(),
		},
		"_uniroute": unirouteData,
	}
	finalJSON, _ := json.Marshal(finalChunk)
	writeMu.Lock()
	fmt.Fprintf(w, "data: %s\n\n", finalJSON)
	writeMu.Unlock()
	flusher.Flush()

	// Send [DONE]
	writeMu.Lock()
	fmt.Fprintf(w, "data: [DONE]\n\n")
	writeMu.Unlock()
	flusher.Flush()
}

// calculateCost computes the actual cost in cents using model prices.
func (relay *Relay) calculateCost(ctx context.Context, orgID int64, model string, promptTokens, completionTokens int) (int, error) {
	costLog := logger.New(logger.Fields{OrgID: uint(orgID), Module: "billing"})
	inputPerToken, outputPerToken, err := relay.billing.QueryModelPriceForOrg(ctx, orgID, model)
	if err != nil {
		costLog.Error("QueryModelPriceForOrg error for %s: %v", model, err)
		return 0, err
	}
	cost := float64(promptTokens)*inputPerToken + float64(completionTokens)*outputPerToken
	result := int(cost / 1000)
	if result < 1 {
		result = 1
	}
	return result, nil
}

// recordRequest logs the request to the call_logs table.
func (relay *Relay) recordRequest(ctx context.Context, requestID string, orgID, keyID, channelID int64, provider, channelName, model, requestedModel, clientIP string, promptTokens, completionTokens, costCents, statusCode, latencyMs, ttftMs int, isStream bool, usageSource string, estimatedTokens int, upstreamRequestID string) {
	reqLog := logger.New(logger.Fields{RequestID: requestID, OrgID: uint(orgID), Module: "records"})
	projectID := middleware.GetProjectIDFromContext(ctx)
	var projectIDArg interface{}
	if projectID > 0 {
		projectIDArg = projectID
	}
	var ownerUserIDArg interface{}
	var ownerUserID *int64
	keyType := "unknown"
	if apiKey := middleware.GetAPIKeyFromContext(ctx); apiKey != nil {
		if apiKey.OwnerUserID != nil && *apiKey.OwnerUserID > 0 {
			ownerUserIDArg = *apiKey.OwnerUserID
			ownerUserID = apiKey.OwnerUserID
		}
		if apiKey.KeyType != "" {
			keyType = apiKey.KeyType
		}
	}
	actorType, actorRefArg := actorEvidence(keyType, keyID, ownerUserID)
	evidenceContext := traceutil.EvidenceFromContext(ctx)
	requestIdentity := middleware.GetRequestIdentityFromContext(ctx)
	attemptID := attemptIDFromContext(ctx)
	if evidenceContext.ActorType != "" {
		actorType = evidenceContext.ActorType
		actorRefArg = nullableString(evidenceContext.ActorRef)
	}
	usageSource = metering.NormalizeUsageSource(usageSource)
	usageConfidence := metering.SourceConfidence(usageSource)
	upstreamRequestID = sanitizeUpstreamRequestID(upstreamRequestID)
	payloadCaptureMode := relay.currentPayloadCaptureMode()
	traceID := requestID
	if evidenceContext.TraceID != "" {
		traceID = evidenceContext.TraceID
	}
	sourceEventHash := hashCallLogEvidence(requestID, requestIdentity.ClientRequestID, requestIdentity.IdempotencyKeyHash,
		orgID, projectIDArg, keyID, ownerUserIDArg, keyType, model, requestedModel, provider, channelName,
		promptTokens, completionTokens, costCents, statusCode, latencyMs, ttftMs, usageSource, isStream,
		estimatedTokens, usageConfidence, upstreamRequestID, traceID, evidenceContext.SessionID,
		evidenceContext.WorkflowID, evidenceContext.AgentID, attemptID, actorType, stringFromNullable(actorRefArg),
		evidenceContext.OnBehalfOf, payloadCaptureMode)
	_, err := relay.db.Exec(ctx,
		`INSERT INTO call_logs (
			request_id, client_request_id, idempotency_key_hash,
			org_id, project_id, api_key_id, owner_user_id, key_type,
			model, requested_model, provider, channel, prompt_tokens, completion_tokens,
			total_tokens, cost_cents, status_code, latency_ms, ttft_ms, usage_source,
			is_stream, ip_address, estimated_tokens, usage_confidence,
			trace_id, session_id, schema_version, capture_mode, evidence_ref, upstream_request_id,
			workflow_id, agent_id, attempt_id,
			actor_type, actor_ref, on_behalf_of, payload_capture_mode, source_event_hash
		)
		 VALUES (
			$1, $2, $3,
			$4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14,
			$15, $16, $17, $18, $19, $20,
			$21, $22, $23, $24,
			$25, $26, $27, $28, $29, $30,
			$31, $32, $33,
			$34, $35, $36, $37, $38
		)`,
		requestID, nullableString(requestIdentity.ClientRequestID), nullableString(requestIdentity.IdempotencyKeyHash),
		orgID, projectIDArg, keyID, ownerUserIDArg, keyType,
		model, requestedModel, provider, channelName, promptTokens, completionTokens,
		promptTokens+completionTokens, costCents, statusCode, latencyMs, ttftMs, usageSource,
		isStream, clientIP, estimatedTokens, usageConfidence,
		traceID, nullableString(evidenceContext.SessionID), evidenceSchemaVersion, evidenceCaptureModeNative, fmt.Sprintf("%s:call_logs:request_id:%s", evidenceTraceRefSourceType, requestID),
		nullableString(upstreamRequestID), nullableString(evidenceContext.WorkflowID), nullableString(evidenceContext.AgentID), nullableString(attemptID),
		actorType, actorRefArg, nullableString(evidenceContext.OnBehalfOf), payloadCaptureMode, sourceEventHash,
	)
	if err != nil {
		reqLog.Error("Failed to record request log: %v", err)
		return
	}
	relay.recordTraceEvent(ctx, requestID, "logging.call_log.recorded", "logging", "info", model, requestedModel, nil, statusCode, latencyMs, map[string]interface{}{
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
		"total_tokens":      promptTokens + completionTokens,
		"cost_cents":        costCents,
		"estimated_tokens":  estimatedTokens,
		"is_stream":         isStream,
		"ttft_ms":           ttftMs,
		"usage_source":      usageSource,
		"usage_confidence":  usageConfidence,
	})
}

// estimateTokens provides a CJK-aware estimate of tokens needed for the request.
func estimateTokens(messages []Message, maxTokens *int) int {
	inputTokens := 0
	for _, msg := range messages {
		inputTokens += tokenutil.EstimateTokens(msg.Content) + 4 // overhead per message
	}
	inputTokens += 2 // reply priming
	outputTokens := 1000
	if maxTokens != nil {
		outputTokens = *maxTokens
	}
	return inputTokens + outputTokens
}

// writeError writes an OpenAI-compatible error response with both type and code fields.
// errType is the OpenAI-style type (e.g. "invalid_request_error"), code is the
// two-part {category}.{specific} code (e.g. "validation_error.invalid_request").
func writeError(w http.ResponseWriter, statusCode int, errType, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"type":    errType,
			"code":    code,
			"message": message,
			"doc_url": errdocs.URLForCode(code),
			"param":   nil,
		},
	})
}

// EmbeddingsRequest represents an incoming embeddings request.
type EmbeddingsRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

// HandleEmbeddings handles POST /v1/embeddings.
func (relay *Relay) HandleEmbeddings(w http.ResponseWriter, r *http.Request) {
	ctx := relay.contextWithTrustedEvidence(r.Context(), r.Header)
	requestStart := time.Now()
	requestID := middleware.GetRequestIDFromContext(ctx)
	w.Header().Set("X-Request-ID", requestID)

	var req EmbeddingsRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", "Failed to read request body")
		return
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", "Invalid JSON body")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", "model is required")
		return
	}
	requestedModel := middleware.GetModelAliasFromContext(ctx)
	if requestedModel == "" {
		requestedModel = req.Model
	}
	callerAPIKey := middleware.GetAPIKeyFromContext(ctx)
	if !modelAllowedForAPIKey(callerAPIKey, req.Model, requestedModel) {
		relay.rejectModelNotAllowed(w, ctx, requestID, req.Model, requestedModel, requestStart)
		return
	}

	orgID := middleware.GetOrgIDFromContext(ctx)
	keyID := middleware.GetKeyIDFromContext(ctx)
	reqLog := logger.New(logger.Fields{RequestID: requestID, OrgID: uint(orgID), Module: "embeddings"})

	// Estimate tokens (embeddings are typically input-only)
	inputTokens := 500
	if s, ok := req.Input.(string); ok {
		inputTokens = tokenutil.EstimateTokens(s)
	}
	if arr, ok := req.Input.([]interface{}); ok {
		inputTokens = 0
		for _, item := range arr {
			if s, ok := item.(string); ok {
				inputTokens += tokenutil.EstimateTokens(s)
			} else {
				inputTokens += 100
			}
		}
	}
	relay.recordTraceEvent(ctx, requestID, "gateway.request.accepted", "gateway", "info", req.Model, requestedModel, nil, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
		"estimated_tokens": inputTokens,
		"stream":           false,
	})
	relay.recordPayloadPolicyDecision(ctx, requestID, req.Model, requestedModel, elapsedMilliseconds(requestStart))

	freezeResult, err := relay.billing.Freeze(ctx, billing.FreezeRequest{
		RequestID:       requestID,
		OrgID:           orgID,
		Model:           req.Model,
		EstimatedTokens: inputTokens,
		SafetyFactor:    1.5,
		MinFreezeAmount: 10,
	})
	if err != nil {
		reqLog.Error("Billing freeze error: %v", err)
		relay.recordTraceEvent(ctx, requestID, "billing.freeze.failed", "billing", "error", req.Model, requestedModel, nil, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code": "freeze_error",
		})
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to process billing")
		return
	}
	if !freezeResult.Success {
		middleware.RecordBillingFreeze("error")
		relay.recordTraceEvent(ctx, requestID, "billing.freeze.rejected", "billing", "warning", req.Model, requestedModel, nil, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code": freezeResult.ErrorCode,
		})
		if freezeResult.ErrorCode == "insufficient_balance" {
			writeError(w, http.StatusPaymentRequired, "billing_error", "billing_error.insufficient_balance", "Insufficient balance")
		} else {
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", freezeResult.ErrorCode)
		}
		return
	}
	middleware.RecordBillingFreeze("success")
	relay.recordTraceEvent(ctx, requestID, "billing.freeze.succeeded", "billing", "info", req.Model, requestedModel, nil, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
		"freeze_amount_cents": freezeResult.Amount,
	})

	// Get all eligible routes so Embeddings follows the same bounded failover
	// contract as Chat.
	routes, err := relay.router.RouteAll(ctx, req.Model)
	if err != nil {
		relay.billing.Cancel(ctx, requestID, orgID)
		writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", fmt.Sprintf("No available channel for model: %s", req.Model))
		relay.recordTraceEvent(ctx, requestID, "routing.no_route", "routing", "error", req.Model, requestedModel, nil, 503, elapsedMilliseconds(requestStart), map[string]interface{}{
			"route_count": 0,
		})
		relay.recordRequest(ctx, requestID, orgID, keyID, 0, "", "", req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, 503, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceNoRoute, inputTokens, "")
		return
	}
	client := &http.Client{Timeout: 120 * time.Second}
	adapterRegistry := adapters.DefaultRegistry()
	routingPolicy := relay.router.Policy()
	providerAttempts := 0
	var lastErr error
	var lastStatusCode int
	var lastUpstreamRequestID string
	var lastRoute *RouteResult
	lastAttemptCtx := ctx

	for routeIndex := range routes {
		route := &routes[routeIndex]
		attemptCtx := contextWithAttemptID(ctx, newAttemptID())
		attemptCtx = contextWithRouteAttempt(attemptCtx, routeIndex+1, len(routes))
		lastAttemptCtx = attemptCtx
		lastRoute = route
		lastStatusCode = 0
		lastUpstreamRequestID = ""
		relay.recordTraceEvent(attemptCtx, requestID, "routing.route_selected", "routing", "info", req.Model, requestedModel, route, 0, elapsedMilliseconds(requestStart), routingPolicyTraceMetadata(routingPolicy, len(routes), routeIndex+1))

		apiKey, err := auth.DecryptKey(route.ChannelKey.KeyEnc, relay.encKey)
		if err != nil {
			relay.recordTraceEvent(attemptCtx, requestID, "provider.key_decrypt_failed", "provider", "error", req.Model, requestedModel, route, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "auth",
			})
			lastErr = fmt.Errorf("provider key decrypt failed")
			continue
		}

		adapter, err := adapterRegistry.Get(route.Channel.AdapterType)
		if err != nil {
			relay.recordTraceEvent(attemptCtx, requestID, "provider.adapter_unknown", "provider", "error", req.Model, requestedModel, route, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "configuration",
			})
			lastErr = fmt.Errorf("provider adapter unavailable")
			continue
		}
		upstreamReq, err := adapter.BuildRequest(attemptCtx, adapters.RequestInput{
			Endpoint:       adapters.EndpointEmbeddings,
			DownstreamBody: body,
			RequestedModel: requestedModel,
			MappedModel:    route.MappedModel,
			Channel:        route.Channel,
			APIKey:         apiKey,
			RequestID:      requestID,
		})
		if err != nil {
			relay.recordTraceEvent(attemptCtx, requestID, "provider.request_build_failed", "provider", "error", req.Model, requestedModel, route, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "request_build",
			})
			lastErr = fmt.Errorf("provider request build failed")
			continue
		}

		providerAttempts++
		upstreamStart := time.Now()
		resp, err := client.Do(upstreamReq)
		upstreamDuration := time.Since(upstreamStart)
		middleware.RecordUpstreamLatency(route.Channel.Provider, req.Model, upstreamDuration)
		if err != nil {
			classified := adapters.ClassifyTransportError(attemptCtx, err)
			willRetry := classified.Retryable && providerAttempts <= routingPolicy.MaxRetryCount && routeIndex+1 < len(routes)
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Upstream embeddings transport failed: kind=%s retryable=%t", classified.FailureKind, classified.Retryable)
			if classified.Retryable {
				relay.router.MarkChannelFail(ctx, route.Channel.ID)
				relay.router.MarkKeyFail(ctx, route.ChannelKey.ID)
				relay.emitProviderChannelFailure(attemptCtx, *route, requestID, requestedModel, 0, string(classified.FailureKind), providerFailureEventMessage(0, classified.FailureKind))
			}
			relay.recordTraceEvent(attemptCtx, requestID, "provider.request_failed", "provider", "warning", req.Model, requestedModel, route, 0, int(upstreamDuration.Milliseconds()), map[string]interface{}{
				"failure_kind":   string(classified.FailureKind),
				"retryable":      classified.Retryable,
				"retry_decision": providerRetryDecision(willRetry),
			})
			lastErr = fmt.Errorf("upstream embeddings transport failed (%s)", classified.FailureKind)
			if classified.Retryable {
				if willRetry {
					continue
				}
				break
			}

			cleanupCtx := context.WithoutCancel(attemptCtx)
			relay.billing.Cancel(cleanupCtx, requestID, orgID)
			relay.recordRequest(cleanupCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, 0, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceUpstreamFailed, inputTokens, "")
			if attemptCtx.Err() == nil {
				writeNonRetryableProviderError(w, 0, classified.FailureKind)
			}
			return
		}
		upstreamRequestID := upstreamRequestIDFromHeader(resp.Header)
		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(resp.Body)
			classified := adapter.ClassifyHTTPStatus(resp.StatusCode, errBody)
			willRetry := classified.Retryable && providerAttempts <= routingPolicy.MaxRetryCount && routeIndex+1 < len(routes)
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Upstream embeddings rejected response: status=%d kind=%s retryable=%t", resp.StatusCode, classified.FailureKind, classified.Retryable)
			switch classified.FailureKind {
			case adapters.FailureChannel:
				relay.router.MarkChannelFail(ctx, route.Channel.ID)
				relay.router.MarkKeyFail(ctx, route.ChannelKey.ID)
				relay.emitProviderChannelFailure(attemptCtx, *route, requestID, requestedModel, resp.StatusCode, string(classified.FailureKind), providerFailureEventMessage(resp.StatusCode, classified.FailureKind))
			case adapters.FailureAuth:
				relay.router.MarkKeyAuthFailure(ctx, route.ChannelKey.ID)
				relay.emitProviderChannelFailure(attemptCtx, *route, requestID, requestedModel, resp.StatusCode, string(classified.FailureKind), providerFailureEventMessage(resp.StatusCode, classified.FailureKind))
			case adapters.FailureRateLimit:
				relay.router.MarkKeyRateLimited(ctx, route.ChannelKey.ID)
				relay.emitProviderChannelFailure(attemptCtx, *route, requestID, requestedModel, resp.StatusCode, string(classified.FailureKind), providerFailureEventMessage(resp.StatusCode, classified.FailureKind))
			}
			relay.recordTraceEvent(attemptCtx, requestID, "provider.response_rejected", "provider", "warning", req.Model, requestedModel, route, resp.StatusCode, int(upstreamDuration.Milliseconds()), map[string]interface{}{
				"failure_kind":   string(classified.FailureKind),
				"retryable":      classified.Retryable,
				"retry_decision": providerRetryDecision(willRetry),
			}, upstreamRequestID)
			lastStatusCode = resp.StatusCode
			lastUpstreamRequestID = upstreamRequestID
			lastErr = fmt.Errorf("upstream embeddings returned HTTP %d (%s)", resp.StatusCode, classified.FailureKind)
			resp.Body.Close()
			if classified.Retryable {
				if willRetry {
					continue
				}
				break
			}

			relay.billing.Cancel(attemptCtx, requestID, orgID)
			relay.recordRequest(attemptCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, resp.StatusCode, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceUpstreamFailed, inputTokens, upstreamRequestID)
			writeNonRetryableProviderError(w, resp.StatusCode, classified.FailureKind)
			return
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			relay.billing.Cancel(attemptCtx, requestID, orgID)
			relay.recordTraceEvent(attemptCtx, requestID, "provider.response_parse_failed", "provider", "error", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "response_read",
			}, upstreamRequestID)
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to read upstream response")
			return
		}

		normalized, err := adapter.ParseResponse(attemptCtx, adapters.ResponseInput{
			StatusCode:     resp.StatusCode,
			Header:         resp.Header,
			Body:           respBody,
			Channel:        route.Channel,
			RequestedModel: requestedModel,
			UpstreamModel:  route.MappedModel,
		})
		if err != nil {
			reqLog.Error("Failed to parse adapter response")
			relay.billing.Cancel(attemptCtx, requestID, orgID)
			relay.recordTraceEvent(attemptCtx, requestID, "provider.response_parse_failed", "provider", "error", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "response_parse",
			}, upstreamRequestID)
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to parse upstream response")
			return
		}

		// Validate the normalized response before charging or adding the
		// UniRouter extension. A malformed 200 response must not panic or settle.
		var responseMap map[string]interface{}
		if err := json.Unmarshal(normalized.Body, &responseMap); err != nil || responseMap == nil {
			relay.billing.Cancel(attemptCtx, requestID, orgID)
			relay.recordTraceEvent(attemptCtx, requestID, "provider.response_parse_failed", "provider", "error", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "response_parse",
			}, upstreamRequestID)
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to parse upstream response")
			return
		}
		relay.router.MarkChannelSuccess(ctx, route.Channel.ID)
		relay.router.MarkKeySuccess(ctx, route.ChannelKey.ID)
		relay.recordTraceEvent(attemptCtx, requestID, "provider.response_accepted", "provider", "info", req.Model, requestedModel, route, resp.StatusCode, int(upstreamDuration.Milliseconds()), map[string]interface{}{}, upstreamRequestID)

		settleOutcome, err := relay.settleBillingWithRetry(billing.SettleRequest{
			RequestID:    requestID,
			OrgID:        orgID,
			PromptTokens: normalized.Usage.PromptTokens,
			UsageSource:  normalized.Usage.Source,
		}, reqLog)
		if err != nil {
			reqLog.Error("Billing settle failed; embeddings response withheld: %v", err)
			relay.recordTraceEvent(attemptCtx, requestID, "billing.settle.failed", "billing", "error", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
				"error_code": "settle_failed",
			})
			writeError(w, http.StatusInternalServerError, "server_error", "billing_error.settle_failed", "Failed to settle billing")
			return
		}
		actualCost, chargedCost := settledCosts(settleOutcome.Result)
		pricingVersion := ""
		if settleOutcome.Result != nil {
			pricingVersion = settleOutcome.Result.PricingVersion
		}
		relay.recordTraceEvent(attemptCtx, requestID, "billing.settle.succeeded", "billing", "info", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
			"prompt_tokens":      normalized.Usage.PromptTokens,
			"completion_tokens":  0,
			"total_tokens":       normalized.Usage.PromptTokens,
			"cost_cents":         actualCost,
			"charged_cost_cents": chargedCost,
			"pricing_version":    pricingVersion,
			"usage_source":       normalized.Usage.Source,
			"usage_confidence":   metering.SourceConfidence(normalized.Usage.Source),
		})
		middleware.RecordTokenUsage("prompt", req.Model, normalized.Usage.PromptTokens)

		relay.recordRequest(attemptCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, req.Model, requestedModel, r.RemoteAddr,
			normalized.Usage.PromptTokens, 0, chargedCost, resp.StatusCode, elapsedMilliseconds(requestStart), 0, false, normalized.Usage.Source, inputTokens, upstreamRequestID)
		relay.checkProjectBudgetAlert(attemptCtx)

		attempt := routeAttemptFromContext(attemptCtx)
		responseMap["_uniroute"] = map[string]interface{}{
			"request_id": requestID,
			"cost": map[string]interface{}{
				"actual":    actualCost,
				"charged":   chargedCost,
				"cache_hit": false,
			},
			"provider":      route.Channel.Provider,
			"channel":       route.Channel.Name,
			"cache_hit":     false,
			"fallback":      attempt.Sequence > 1,
			"attempt_count": attempt.Sequence,
			"route_count":   attempt.Total,
			"attempt_id":    attemptIDFromContext(attemptCtx),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_ = json.NewEncoder(w).Encode(responseMap)
		return
	}

	relay.billing.Cancel(ctx, requestID, orgID)
	if lastErr != nil {
		reqLog.Error("All upstream embeddings channels failed: %v", lastErr)
	}
	if lastRoute != nil {
		relay.recordRequest(lastAttemptCtx, requestID, orgID, keyID, lastRoute.Channel.ID, lastRoute.Channel.Provider, lastRoute.Channel.Name, req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, lastStatusCode, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceUpstreamFailed, inputTokens, lastUpstreamRequestID)
	}
	writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_error", "All upstream channels failed")
}

// HandleModels handles GET /v1/models — returns available models for the caller.
// Uses the API key's allowed_models field; falls back to all model_prices.
func (relay *Relay) HandleModels(w http.ResponseWriter, r *http.Request) {
	orgID := middleware.GetOrgIDFromContext(r.Context())
	keyID := middleware.GetKeyIDFromContext(r.Context())

	// Try per-key model list from api_keys.allowed_models (comma-separated)
	var allowedModels string
	err := relay.db.QueryRow(r.Context(),
		`SELECT COALESCE(allowed_models, '') FROM api_keys WHERE id = $1 AND org_id = $2 AND status = 'active'`,
		keyID, orgID).Scan(&allowedModels)
	if err == nil && allowedModels != "" {
		var models []map[string]interface{}
		for _, m := range strings.Split(allowedModels, ",") {
			m = strings.TrimSpace(m)
			if m != "" {
				models = append(models, map[string]interface{}{
					"id":       m,
					"object":   "model",
					"owned_by": "uniroute",
				})
			}
		}
		if len(models) > 0 {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "list",
				"data":   models,
			})
			return
		}
	}

	// Fallback: return all models from model_prices
	relay.handleModelsFallback(w, r)
}

func (relay *Relay) handleModelsFallback(w http.ResponseWriter, r *http.Request) {
	rows, err := relay.db.Query(r.Context(),
		`SELECT model_name FROM model_prices ORDER BY model_name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to query models")
		return
	}
	defer rows.Close()

	var models []map[string]interface{}
	for rows.Next() {
		var model string
		rows.Scan(&model)
		models = append(models, map[string]interface{}{
			"id":       model,
			"object":   "model",
			"owned_by": "uniroute",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}

// checkProjectBudgetAlert triggers the one active, tenant-scoped budget-alert
// contract after settlement. The historical global first-wallet alert path is
// intentionally retired because it has no safe multi-tenant scope.
func (relay *Relay) checkProjectBudgetAlert(ctx context.Context) {
	if relay.eventSvc != nil {
		orgID := middleware.GetOrgIDFromContext(ctx)
		projectID := middleware.GetProjectIDFromContext(ctx)
		if orgID > 0 && projectID > 0 {
			go func() {
				if _, err := relay.eventSvc.CheckProjectBudget(context.Background(), orgID, projectID); err != nil {
					logger.New(logger.Fields{OrgID: uint(orgID), Module: "notifications"}).Error("Project budget notification check failed for project_id=%d: %v", projectID, err)
				}
			}()
		}
	}
}
