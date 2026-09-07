package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/capbilling"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/errdocs"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/modellist"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/responsecache"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimeconfig"
	"github.com/uniroute/unirouter/internal/streampolicy"
	"github.com/uniroute/unirouter/internal/telemetryexport"
	"github.com/uniroute/unirouter/internal/tokenutil"
	"github.com/uniroute/unirouter/internal/traceutil"
	"github.com/uniroute/unirouter/internal/workloadadmission"
)

// BillingProvider defines the billing operations that Relay depends on.
// *billing.Billing satisfies this interface.
type BillingProvider interface {
	QueryModelPriceMicrosForOrg(ctx context.Context, orgID int64, model string) (int64, int64, error)
	CalculateCostMicrosForOrg(ctx context.Context, orgID int64, model string, promptTokens, completionTokens int) (int64, error)
	Freeze(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error)
	ExtendReservation(ctx context.Context, req billing.ExtendReservationRequest) (*billing.ExtendReservationResult, error)
	BeginStreamDelivery(ctx context.Context, start billing.StreamDeliveryStart) error
	RenewStreamDelivery(ctx context.Context, start billing.StreamDeliveryStart) error
	CheckpointStreamDelivery(ctx context.Context, checkpoint billing.StreamDeliveryCheckpoint) (*billing.StreamCheckpointResult, error)
	FinishStreamDelivery(ctx context.Context, start billing.StreamDeliveryStart, state, reason string) error
	BeginProviderAttempt(ctx context.Context, req billing.ProviderAttemptStart) error
	CompleteProviderAttempt(ctx context.Context, req billing.ProviderAttemptCompletion) error
	PrepareNonStreamDelivery(ctx context.Context, req billing.SettleRequest) (*billing.PrepareDeliveryResult, error)
	PrepareResponseCacheDelivery(ctx context.Context, req billing.SettleRequest) (*billing.PrepareDeliveryResult, error)
	HoldNonStreamOutput(ctx context.Context, req billing.SettleRequest, reason string) error
	RecordDelivery(ctx context.Context, event billing.DeliveryEvent) error
	RecordResponseCacheDelivery(ctx context.Context, event billing.ResponseCacheDeliveryEvent) error
	MarkBillableOutput(ctx context.Context, requestID string, orgID int64) error
	PersistSettlementUsage(ctx context.Context, req billing.SettleRequest) error
	Settle(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error)
	Cancel(ctx context.Context, requestID string, orgID int64) error
}

// BillingAdmissionReadinessInspector exposes only the side-effect-free billing
// mode/readiness snapshot needed by pre-Freeze protocol policy. The gateway's
// wrapped BillingProvider remains the financial mutation boundary.
type BillingAdmissionReadinessInspector interface {
	InspectAdmissionReadiness(context.Context, int64, string) (billing.AdmissionReadiness, error)
}

type TraceExporter interface {
	TryExport(telemetryexport.SourceEvent) error
}

// Relay handles forwarding requests to upstream providers.
type Relay struct {
	db                        db.DBQuerier
	rdb                       *redis.Client
	router                    *Router
	billing                   BillingProvider
	billingAdmissionReadiness BillingAdmissionReadinessInspector
	filter                    *filter.DFAFilter
	encKey                    []byte
	eventSvc                  *billing.EventService
	outbox                    *outbox.Service
	traceExporter             TraceExporter

	traceEventsEnabled         bool
	traceEventSampleRate       float64
	payloadCaptureMode         string
	evidenceContextTrustSecret string
	streamPolicy               streampolicy.Config
	payloadBudget              *payloadpolicy.Budget
	guardrail                  *guardrail.Service
	freezeSafetyFactor         string
	minFreezeCents             int64
	modelAuthorization         *modelauthz.Evaluator
	invocationOptimization     *invocationopt.Manager
	responseCache              *responsecache.Service
	runtimeVersion             string
	runtimeCommit              string
	runtimeEnvironment         string
	modelExecution             *modelExecutionRuntime
	providerHTTPDoer           adapters.HTTPDoer
}

func (relay *Relay) SetModelAuthorization(evaluator *modelauthz.Evaluator) {
	if relay != nil {
		relay.modelAuthorization = evaluator
	}
}

// NewRelay creates a new Relay.
func NewRelay(db db.DBQuerier, rdb *redis.Client, router *Router, b BillingProvider, f *filter.DFAFilter, encKey []byte) *Relay {
	return &Relay{db: db, rdb: rdb, router: router, billing: b, filter: f, encKey: encKey, traceEventsEnabled: true, traceEventSampleRate: 1, payloadCaptureMode: traceutil.PayloadCaptureModeMetadataOnly, streamPolicy: streampolicy.Default(), payloadBudget: payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits()), freezeSafetyFactor: "1.5", minFreezeCents: 10}
}

func (relay *Relay) SetBillingAdmissionReadinessInspector(inspector BillingAdmissionReadinessInspector) {
	if relay != nil {
		relay.billingAdmissionReadiness = inspector
	}
}

// SetBillingFreezePolicy applies the deployment's existing cent-denominated
// minimum and an exact decimal safety factor without introducing float money.
func (relay *Relay) SetBillingFreezePolicy(safetyFactor string, minFreezeCents int64) error {
	parsed, err := money.ParseDecimal(safetyFactor)
	if err != nil || parsed.Sign() <= 0 || minFreezeCents < 0 {
		return fmt.Errorf("invalid billing freeze policy")
	}
	relay.freezeSafetyFactor = safetyFactor
	relay.minFreezeCents = minFreezeCents
	return nil
}

func (relay *Relay) SetPayloadBudget(budget *payloadpolicy.Budget) {
	if relay != nil && budget != nil {
		relay.payloadBudget = budget
	}
}

func (relay *Relay) SetGuardrail(service *guardrail.Service) {
	if relay != nil {
		relay.guardrail = service
	}
}

// SetProviderHTTPDoer configures the transport before the Relay starts serving.
// It lets integration callers exercise real admission/dispatch/settlement with
// an isolated upstream fixture. It does not bypass any execution authority.
func (relay *Relay) SetProviderHTTPDoer(doer adapters.HTTPDoer) {
	relay.providerHTTPDoer = doer
}

func (relay *Relay) currentPayloadBudget() *payloadpolicy.Budget {
	if relay == nil || relay.payloadBudget == nil {
		return payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits())
	}
	return relay.payloadBudget
}

func (relay *Relay) SetStreamPolicy(policy streampolicy.Config) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	relay.streamPolicy = policy
	return nil
}

func (relay *Relay) currentStreamPolicy() streampolicy.Config {
	if relay == nil || relay.streamPolicy.Validate() != nil {
		return streampolicy.Default()
	}
	return relay.streamPolicy
}

// SetEventService configures PRD2 notification events for post-settle project checks.
func (relay *Relay) SetEventService(svc *billing.EventService) {
	relay.eventSvc = svc
}

// SetOutbox enables bounded, replayable materialization of detailed gateway
// evidence. Financial terminal state remains owned by billing.
func (relay *Relay) SetOutbox(service *outbox.Service) {
	if relay != nil {
		relay.outbox = service
	}
}

// SetTraceExporter attaches an optional metadata-only, non-blocking export
// observer. Export failures never change source persistence or request results.
func (relay *Relay) SetTraceExporter(exporter TraceExporter) {
	if relay != nil {
		relay.traceExporter = exporter
	}
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
	inbound := traceutil.ExtractTrustedEvidenceContext(header, relay.evidenceContextTrustSecret)
	// Authentication-owned capability identity is stronger than the optional
	// trusted evidence side channel. A caller that knows the evidence header
	// secret may add trace/session ancestry, but cannot replace the workflow,
	// Agent or actor bound to the signed gateway capability.
	if authenticated := traceutil.EvidenceFromContext(ctx); !authenticated.IsZero() {
		if inbound.TraceID == "" {
			inbound.TraceID = authenticated.TraceID
		}
		if inbound.ParentSpanID == "" {
			inbound.ParentSpanID = authenticated.ParentSpanID
		}
		if inbound.SessionID == "" {
			inbound.SessionID = authenticated.SessionID
		}
		if authenticated.WorkflowID != "" {
			inbound.WorkflowID = authenticated.WorkflowID
		}
		if authenticated.AgentID != "" {
			inbound.AgentID = authenticated.AgentID
		}
		if authenticated.ActorType != "" {
			inbound.ActorType = authenticated.ActorType
		}
		if authenticated.ActorRef != "" {
			inbound.ActorRef = authenticated.ActorRef
		}
		if authenticated.OnBehalfOf != "" {
			inbound.OnBehalfOf = authenticated.OnBehalfOf
		}
	}
	ctx = traceutil.ContextWithEvidence(ctx, inbound)
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

func routingPolicyTraceMetadata(policy routingpolicy.Config, route *RouteResult, routeCount, routeIndex int) map[string]interface{} {
	metadata := map[string]interface{}{
		"route_count":                       routeCount,
		"route_index":                       routeIndex,
		"effective_config_version":          runtimeconfig.Version,
		"routing_config_version":            routingpolicy.Version,
		"routing_policy_version":            routingpolicy.SelectionVersion,
		"max_retry_count":                   policy.MaxRetryCount,
		"circuit_breaker_threshold":         policy.CircuitBreakerThreshold,
		"circuit_breaker_recover_seconds":   policy.CircuitBreakerRecoverSeconds,
		"circuit_breaker_half_open_percent": policy.CircuitBreakerHalfOpenPct,
		"rate_limit_cooldown_seconds":       policy.RateLimitCooldownSeconds,
		"auth_failure_cooldown_seconds":     policy.AuthFailureCooldownSeconds,
		"provider_key_failure_threshold":    policy.KeyFailureThreshold,
	}
	if route != nil {
		metadata["routing_snapshot_version"] = route.SnapshotVersion
		metadata["routing_snapshot_source"] = route.SnapshotSource
		metadata["routing_snapshot_degraded"] = route.SnapshotDegraded
		metadata["routing_candidate_count"] = route.CandidateCount
		metadata["routing_excluded_count"] = route.ExcludedCount
		metadata["routing_exclusion_reasons"] = route.ExclusionReasons
		metadata["routing_priority"] = route.Channel.Priority
		metadata["routing_weight"] = route.Channel.RoutingWeight
		metadata["routing_selection_reason"] = route.SelectionReason
		metadata["routing_requires_probe"] = route.RequiresProbe
		metadata["runtime_release_ref"] = route.RuntimeReleaseRef
		metadata["runtime_release_stage"] = route.RuntimeStage
		metadata["runtime_assignment_id"] = route.RuntimeAssignmentID
	}
	return metadata
}

func (relay *Relay) emitProviderChannelFailure(ctx context.Context, route RouteResult, requestID, requestedModel string, statusCode int, failureKind, message string) {
	if relay.eventSvc == nil {
		return
	}
	orgID := middleware.GetOrgIDFromContext(ctx)
	if orgID <= 0 || route.Channel.ID <= 0 {
		return
	}
	emit := func() {
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
	}
	if relay.outbox != nil {
		emit()
		return
	}
	go emit()
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

func settledCosts(result *billing.SettleResult) (actual, charged int64) {
	if result == nil {
		return 0, 0
	}
	return result.ActualCostMicros, result.ChargedCostMicros
}

func settledPrepaidTopUp(result *billing.SettleResult) int64 {
	if result == nil {
		return 0
	}
	return result.TopUpAmountMicros
}

func microsText(value int64) string {
	return strconv.FormatInt(value, 10)
}

func settlementCostExtension(usage adapters.Usage, result *billing.SettleResult, actualCost, chargedCost int64) map[string]interface{} {
	return map[string]interface{}{
		"actual_cost_micros":     microsText(actualCost),
		"charged_cost_micros":    microsText(chargedCost),
		"prepaid_topup_micros":   microsText(settledPrepaidTopUp(result)),
		"currency":               "CNY",
		"amount_scale":           money.MicrosPerCNY,
		"money_schema":           outbox.MoneyVersion,
		"cache_hit":              usage.HasAuthoritativeCacheHit(),
		"usage_authority":        usage.Authority,
		"usage_purpose":          usage.Purpose,
		"usage_finality":         usage.Finality,
		"usage_contract_version": usage.ContractVersion,
		"usage_estimate_version": usage.EstimateVersion,
	}
}

func preparedDeliveryCostExtension(usage adapters.Usage, result *billing.SettleResult, actualCost, projectedCharge int64) map[string]interface{} {
	extension := settlementCostExtension(usage, result, actualCost, 0)
	extension["projected_charge_micros"] = microsText(projectedCharge)
	extension["charge_finality"] = "pending_local_delivery"
	return extension
}

func settleRequestFromUsage(requestID, attemptID string, orgID int64, usage adapters.Usage) billing.SettleRequest {
	return billing.SettleRequest{
		RequestID:            requestID,
		AttemptID:            attemptID,
		OrgID:                orgID,
		PromptTokens:         usage.PromptTokens,
		UncachedPromptTokens: usage.UncachedPromptTokens,
		CacheReadTokens:      usage.CacheReadTokens,
		CacheCreationTokens:  usage.CacheCreationTokens,
		CompletionTokens:     usage.CompletionTokens,
		UsageSource:          usage.Source,
		UsageAuthority:       usage.Authority,
		UsagePurpose:         usage.Purpose,
		UsageFinality:        usage.Finality,
		UsageContractVersion: usage.ContractVersion,
		UsageEstimateVersion: usage.EstimateVersion,
		CacheUsageSchema:     usage.CacheUsageSchema,
		CacheUsageStatus:     usage.CacheUsageStatus,
		MeterUsage:           slices.Clone(usage.MeterUsage),
	}
}

func providerAttemptStartFromRoute(ctx context.Context, requestID string, orgID, apiKeyID int64, endpoint adapters.Endpoint, route *RouteResult) billing.ProviderAttemptStart {
	attempt := routeAttemptFromContext(ctx)
	return billing.ProviderAttemptStart{
		RequestID:     requestID,
		AttemptID:     attemptIDFromContext(ctx),
		OrgID:         orgID,
		APIKeyID:      apiKeyID,
		ChannelID:     route.Channel.ID,
		ChannelKeyID:  route.ChannelKey.ID,
		RouteSequence: attempt.Sequence,
		RouteTotal:    attempt.Total,
		Endpoint:      string(endpoint),
		Provider:      route.Channel.Provider,
		Channel:       route.Channel.Name,
	}
}

func providerAttemptCompletion(ctx context.Context, requestID string, orgID int64, outcome string, retryAuthorized bool, status int, failureKind, upstreamRequestID string) billing.ProviderAttemptCompletion {
	return billing.ProviderAttemptCompletion{
		RequestID:         requestID,
		AttemptID:         attemptIDFromContext(ctx),
		OrgID:             orgID,
		Outcome:           outcome,
		RetryAuthorized:   retryAuthorized,
		HTTPStatus:        status,
		FailureKind:       failureKind,
		UpstreamRequestID: upstreamRequestID,
	}
}

func (relay *Relay) beginProviderAttemptWithRetry(ctx context.Context, req billing.ProviderAttemptStart, reqLog *logger.Logger) error {
	attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var lastErr error
	for attempt := 1; attempt <= billingSettleMaxAttempts; attempt++ {
		if err := relay.billing.BeginProviderAttempt(attemptCtx, req); err == nil {
			return nil
		} else {
			lastErr = err
			reqLog.Error("Provider attempt begin %d/%d failed: %v", attempt, billingSettleMaxAttempts, err)
		}
		if attempt < billingSettleMaxAttempts {
			select {
			case <-attemptCtx.Done():
				return fmt.Errorf("begin provider attempt: %w", attemptCtx.Err())
			case <-time.After(billingSettleRetryDelay):
			}
		}
	}
	return fmt.Errorf("begin provider attempt failed after %d attempts: %w", billingSettleMaxAttempts, lastErr)
}

func (relay *Relay) completeProviderAttemptWithRetry(req billing.ProviderAttemptCompletion, reqLog *logger.Logger) error {
	attemptCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var lastErr error
	for attempt := 1; attempt <= billingSettleMaxAttempts; attempt++ {
		if err := relay.billing.CompleteProviderAttempt(attemptCtx, req); err == nil {
			return nil
		} else {
			lastErr = err
			reqLog.Error("Provider attempt completion %d/%d failed: %v", attempt, billingSettleMaxAttempts, err)
		}
		if attempt < billingSettleMaxAttempts {
			select {
			case <-attemptCtx.Done():
				return fmt.Errorf("complete provider attempt: %w", attemptCtx.Err())
			case <-time.After(billingSettleRetryDelay):
			}
		}
	}
	return fmt.Errorf("complete provider attempt failed after %d attempts: %w", billingSettleMaxAttempts, lastErr)
}

func (relay *Relay) prepareNonStreamDeliveryWithTimeout(ctx context.Context, req billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
	prepareCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return relay.billing.PrepareNonStreamDelivery(prepareCtx, req)
}

func (relay *Relay) holdNonStreamOutputWithRetry(ctx context.Context, req billing.SettleRequest, reason string, reqLog *logger.Logger) error {
	holdCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var lastErr error
	for attempt := 1; attempt <= billingSettleMaxAttempts; attempt++ {
		if err := relay.billing.HoldNonStreamOutput(holdCtx, req, reason); err == nil {
			return nil
		} else {
			lastErr = err
			if reqLog != nil {
				reqLog.Error("Provider output hold %d/%d failed: %v", attempt, billingSettleMaxAttempts, err)
			}
		}
		if attempt < billingSettleMaxAttempts {
			select {
			case <-holdCtx.Done():
				return fmt.Errorf("hold provider output: %w", holdCtx.Err())
			case <-time.After(billingSettleRetryDelay):
			}
		}
	}
	return fmt.Errorf("hold provider output failed after %d attempts: %w", billingSettleMaxAttempts, lastErr)
}

func cacheUsageMetadata(usage adapters.Usage) map[string]interface{} {
	return map[string]interface{}{
		"cache_scope":               "provider_prompt",
		"cache_usage_authoritative": usage.HasAuthoritativeCacheUsage(),
		"cache_hit":                 usage.HasAuthoritativeCacheHit(),
		"uncached_prompt_tokens":    usage.UncachedPromptTokens,
		"cache_read_tokens":         usage.CacheReadTokens,
		"cache_creation_tokens":     usage.CacheCreationTokens,
		"cache_usage_schema":        usage.CacheUsageSchema,
		"cache_usage_status":        usage.CacheUsageStatus,
	}
}

func providerPromptCacheExtension(usage adapters.Usage, result *billing.SettleResult) map[string]interface{} {
	extension := cacheUsageMetadata(usage)
	if result != nil {
		extension["cache_read_ratio"] = result.CacheReadRatio
		extension["cache_creation_ratio"] = result.CacheCreationRatio
		extension["cache_read_cost_micros"] = microsText(result.CacheReadCostMicros)
		extension["cache_creation_cost_micros"] = microsText(result.CacheCreationCostMicros)
		extension["pricing_version"] = result.PricingVersion
	}
	return extension
}

func settlementTraceMetadata(usage adapters.Usage, result *billing.SettleResult, actualCost, chargedCost int64) map[string]interface{} {
	metadata := cacheUsageMetadata(usage)
	metadata["prompt_tokens"] = usage.PromptTokens
	metadata["completion_tokens"] = usage.CompletionTokens
	metadata["total_tokens"] = usage.PromptTokens + usage.CompletionTokens
	metadata["actual_cost_micros"] = microsText(actualCost)
	metadata["charged_cost_micros"] = microsText(chargedCost)
	metadata["currency"] = "CNY"
	metadata["amount_scale"] = money.MicrosPerCNY
	metadata["money_schema"] = outbox.MoneyVersion
	metadata["usage_source"] = usage.Source
	metadata["usage_authority"] = usage.Authority
	metadata["usage_purpose"] = usage.Purpose
	metadata["usage_finality"] = usage.Finality
	metadata["usage_contract_version"] = usage.ContractVersion
	metadata["usage_estimate_version"] = usage.EstimateVersion
	metadata["usage_confidence"] = metering.SourceConfidence(usage.Source)
	if result != nil {
		metadata["prepaid_topup_micros"] = microsText(result.TopUpAmountMicros)
		metadata["pricing_version"] = result.PricingVersion
		metadata["cache_read_ratio"] = result.CacheReadRatio
		metadata["cache_creation_ratio"] = result.CacheCreationRatio
		metadata["cache_read_cost_micros"] = microsText(result.CacheReadCostMicros)
		metadata["cache_creation_cost_micros"] = microsText(result.CacheCreationCostMicros)
	}
	return metadata
}

func writeAgentCapabilityRuntimeRejection(w http.ResponseWriter, errorCode string) bool {
	switch errorCode {
	case capbilling.ErrorCodeCostBudgetExceeded:
		writeError(w, http.StatusForbidden, "permission_error", errorCode, "Agent workflow cost budget exhausted")
	case capbilling.ErrorCodeTokenBudgetExceeded:
		writeError(w, http.StatusForbidden, "permission_error", errorCode, "Agent workflow token budget exhausted")
	case capbilling.ErrorCodeRPSLimitExceeded:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "rate_limit_error", errorCode, "Agent request-per-second limit exceeded")
	case capbilling.ErrorCodeRPMLimitExceeded:
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limit_error", errorCode, "Agent request-per-minute limit exceeded")
	case capbilling.ErrorCodeConcurrencyExceeded:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "rate_limit_error", errorCode, "Agent concurrent request limit exceeded")
	case capbilling.ErrorCodeStateUnavailable:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "server_error", errorCode, "Agent runtime budget state is temporarily unavailable")
	case capbilling.ErrorCodeRevoked:
		writeError(w, http.StatusUnauthorized, "authentication_error", errorCode, "Agent workflow authority is no longer active")
	case capbilling.ErrorCodeConflict:
		writeError(w, http.StatusConflict, "agent_capability_error", errorCode, "Agent runtime reservation identity conflict")
	default:
		return false
	}
	return true
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
			} else if result.ErrorCode == "insufficient_topup" {
				return billingSettleOutcome{Result: result}, billing.ErrSettlementFundingPending
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
	if errors.Is(settleErr, billing.ErrSettlementFundingPending) {
		return outcome, settleErr
	}

	cancelCtx, cancelCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCancel()
	if cancelErr := relay.billing.Cancel(cancelCtx, req.RequestID, req.OrgID); cancelErr != nil {
		if errors.Is(cancelErr, billing.ErrProviderAttemptUnresolved) || errors.Is(cancelErr, billing.ErrSettlementPending) {
			return billingSettleOutcome{}, fmt.Errorf("%w; provider liability retained for reconciliation", settleErr)
		}
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

const maxChatCompletionTokens = 1<<31 - 1

func modelAllowedForAPIKey(apiKey *models.ApiKey, model, requestedModel string) bool {
	if apiKey == nil {
		return true
	}
	if apiKey.ModelScopeDenied {
		return false
	}
	if apiKey.AllowedModels == "" {
		return true
	}
	for _, allowedModel := range modellist.Parse(apiKey.AllowedModels) {
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

func (relay *Relay) requireModelExecutionRuntime(w http.ResponseWriter) bool {
	if relay != nil && relay.modelExecution != nil && relay.modelExecution.planner != nil &&
		relay.modelExecution.coordinator != nil && relay.modelExecution.registry != nil {
		return true
	}
	w.Header().Set("Retry-After", "1")
	writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Model execution is temporarily unavailable")
	return false
}

// HandleChatCompletion handles POST /v1/chat/completions.
func (relay *Relay) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	ctx := contextWithTelemetryOperationKind(
		relay.contextWithTrustedEvidence(r.Context(), r.Header),
		telemetryexport.OperationChatCompletion,
	)
	requestStart := time.Now()
	requestID := middleware.GetRequestIDFromContext(ctx)
	w.Header().Set("X-Request-ID", requestID)

	// Parse request body
	var req ChatCompletionRequest
	body, releaseRequestPayload, err := relay.requestPayload(r, payloadpolicy.ProfileChatRequest)
	if err != nil {
		if errors.Is(err, payloadpolicy.ErrPayloadTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "validation_error.payload_too_large", "Request payload exceeds the chat safety limit")
		} else if errors.Is(err, payloadpolicy.ErrBudgetExceeded) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.payload_capacity_exceeded", "Payload capacity is temporarily unavailable")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", "Failed to read request body")
		}
		return
	}
	defer releaseRequestPayload()
	defer r.Body.Close()

	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", "Invalid JSON body")
		return
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", "model is required")
		return
	}
	if message := validateChatOutputParameters(req); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", message)
		return
	}
	requestedModel := middleware.GetModelAliasFromContext(ctx)
	if requestedModel == "" {
		requestedModel = req.Model
	}
	// Caller model authorization is an ingress boundary. It must run before
	// guardrail evaluation or receipt emission so a denied principal cannot
	// cause policy-side work for a model it is not allowed to invoke.
	callerAPIKey := middleware.GetAPIKeyFromContext(ctx)
	if !modelAllowedForAPIKey(callerAPIKey, req.Model, requestedModel) {
		relay.rejectModelNotAllowed(w, ctx, requestID, req.Model, requestedModel, requestStart)
		return
	}
	if !relay.requireModelExecutionRuntime(w) {
		return
	}
	ctx = contextWithAssistantToolHistorySnapshot(ctx, body)

	// Pin one immutable policy set for both input and non-stream output. Existing
	// deployments without the P6-F10 service keep the historical sensitive-word
	// behavior for compatibility in repository tests and staged rollouts.
	pinnedGuardrail := relay.pinGuardrail(ctx, req.Model, "chat")
	if pinnedGuardrail != nil {
		inputInspection, inspectionErr := inspectChatInputGuardrail(body)
		guardrailResult := pinnedGuardrail.EvaluateWithCompleteness(ctx, guardrail.StageInput, inputInspection.texts, inspectionErr == nil && inputInspection.complete())
		relay.recordGuardrailResult(ctx, requestID, req.Model, requestedModel, normalizedGuardrailStage(guardrail.StageInput), guardrailResult, nil, 0, "")
		if guardrailResult.Decision == guardrail.DecisionBlock {
			writeInputGuardrailBlock(w, guardrailBlockedByUnavailable(guardrailResult))
			return
		}
		body, err = applyChatGuardrailTransform(body, &req, guardrailResult)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "guardrail_error.transform_failed", "Failed to apply content policy safely")
			return
		}
		if guardrailResult.Transformed {
			if r, err = payloadpolicy.ReplaceRequestBody(r, body); err != nil {
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.payload_capacity_exceeded", "Payload capacity is temporarily unavailable")
				return
			}
		}
		ctx = contextWithGuardrailPin(ctx, pinnedGuardrail)
	} else if relay.filter != nil && relay.filter.WordCount() > 0 {
		inputInspection, inspectionErr := inspectChatInputGuardrail(body)
		if inspectionErr != nil || !inputInspection.complete() {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.unsupported_semantics", "Request contains input that cannot be inspected safely")
			return
		}
		for _, text := range inputInspection.texts {
			if matched, _ := relay.filter.IsSensitive(text.Content); matched {
				writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.sensitive_word_detected", "Request contains prohibited content")
				return
			}
		}
	}

	optimizationState := relay.evaluateCompression(ctx, r.Header, requestID, requestedModel, callerAPIKey, req.Model, body)
	if !bytes.Equal(optimizationState.effectiveBody, body) {
		body = optimizationState.effectiveBody
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "prompt_compression.invalid_candidate", "Prompt compression candidate failed validation")
			return
		}
		w.Header().Set(compressionOptInHeader, "APPLIED")
	} else if optimizationState.compression != nil {
		w.Header().Set(compressionOptInHeader, strings.ToUpper(optimizationState.compression.EffectiveMode))
	}

	// Before routing we can bound only the raw JSON envelope. The selected
	// immutable protocol profile performs the strict shape validation below;
	// applying the generic text-only validator here would reject verified Kimi
	// media, partial, and dynamic-tool contracts before a profile is selected.
	admission, admissionErr := estimateChatPreProfileAdmission(body)
	if admissionErr != nil {
		relay.recordTraceEvent(ctx, requestID, "gateway.admission_rejected", "admission", "warning", req.Model, requestedModel, nil, http.StatusBadRequest, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code": "validation_error.unsupported_semantics", "pre_freeze": true,
		})
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.unsupported_semantics", "Request contains input that cannot be bounded safely")
		return
	}
	completionTokenLimit := req.completionTokenLimit()
	estimatedInputTokens := admission.InputTokens
	// The planner supplies the immutable public hard cap below. Zero here is
	// intentional: no heuristic default may enter route admission or Freeze.
	reservedCompletionTokens := 0
	if completionTokenLimit != nil {
		reservedCompletionTokens = *completionTokenLimit
	}
	estimatedTokens, estimateErr := checkedAdmissionInt(int64(estimatedInputTokens), int64(reservedCompletionTokens))
	if estimateErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.context_length_exceeded", "Estimated request tokens exceed the supported range")
		return
	}
	orgID := middleware.GetOrgIDFromContext(ctx)
	keyID := middleware.GetKeyIDFromContext(ctx)
	reqLog := logger.New(logger.Fields{RequestID: requestID, OrgID: uint(orgID), Module: "chat"})
	compressionReceiptRecorded := false
	recordCompressionReceipt := func(route *RouteResult) {
		if compressionReceiptRecorded {
			return
		}
		compressionReceiptRecorded = relay.recordCompressionReceipt(ctx, optimizationState, route, reqLog)
	}
	providerTimeout := 300 * time.Second
	if req.Stream {
		providerTimeout = relay.currentStreamPolicy().MaxDuration()
	}
	workloadClass := workloadadmission.ClassInteractive
	if req.Stream {
		workloadClass = workloadadmission.ClassStream
	}
	ctx = workloadadmission.ContextWithClass(ctx, workloadClass)
	ctx, limitPolicyCollector := contextWithLimitPolicyCollector(ctx)
	executionBatch, err := relay.prepareModelExecutionPlan(
		ctx, modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		req.Model, requestedModel, requestID,
		modelExecutionRequestLimits{
			EstimatedInputTokens: estimatedInputTokens, ExplicitOutputTokens: completionTokenLimit,
			EstimateVersion: admission.Version,
		},
		body, requestStart,
	)
	if err != nil {
		if status, code, message, tierError := processingTierAdmissionErrorDetails(err); tierError {
			relay.recordTraceEvent(ctx, requestID, "billing.processing_tier_rejected", "billing", "warning", req.Model, requestedModel, nil, status, elapsedMilliseconds(requestStart), map[string]interface{}{
				"error_code": code, "pre_freeze": true,
			})
			errorType := "billing_error"
			if status == http.StatusServiceUnavailable {
				errorType = "server_error"
			}
			writeError(w, status, errorType, code, message)
			return
		}
		if code, message, limitError := modelExecutionLimitErrorDetails(err); limitError {
			relay.recordTraceEvent(ctx, requestID, "routing.request_limit_rejected", "routing", "warning", req.Model, requestedModel, nil, http.StatusBadRequest, elapsedMilliseconds(requestStart), map[string]interface{}{
				"error_code": code, "pre_freeze": true,
			})
			writeError(w, http.StatusBadRequest, "invalid_request_error", code, message)
			return
		}
		recordCompressionReceipt(nil)
		relay.recordTraceEvent(ctx, requestID, "routing.no_route", "routing", "error", req.Model, requestedModel, nil, http.StatusServiceUnavailable, elapsedMilliseconds(requestStart), map[string]interface{}{
			"route_count": 0, "pre_freeze": true,
		})
		relay.recordRequest(ctx, requestID, orgID, keyID, 0, "", "", req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, http.StatusServiceUnavailable, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceNoRoute, estimatedTokens, "")
		writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", fmt.Sprintf("No available channel for model: %s", req.Model))
		return
	}
	if tierTimeout := time.Duration(executionBatch.profileEvidence.ProcessingTierTimeoutSeconds) * time.Second; tierTimeout > providerTimeout {
		providerTimeout = tierTimeout
	}
	if reasoningTimeout := time.Duration(executionBatch.profileEvidence.ReasoningTimeoutSeconds) * time.Second; reasoningTimeout > providerTimeout {
		providerTimeout = reasoningTimeout
	}
	ctx = agentcap.ContextWithInvocationLeaseTTL(ctx, providerTimeout)
	ctx = contextWithProcessingTierEvidence(ctx, executionBatch.profileEvidence)
	if err := configureProviderWriteDeadline(w, executionBatch.profileEvidence, providerTimeout); err != nil {
		eventName := "gateway.processing_tier_runtime_rejected"
		code := "server_error.processing_tier_runtime_unavailable"
		message := "Processing-tier runtime deadline could not be established"
		if executionBatch.profileEvidence.ProcessingTierPolicy == "" {
			eventName = "gateway.reasoning_runtime_rejected"
			code = "server_error.reasoning_runtime_unavailable"
			message = "Reasoning runtime deadline could not be established"
		}
		relay.recordTraceEvent(ctx, requestID, eventName, "gateway", "error", req.Model, requestedModel, nil, http.StatusServiceUnavailable, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code": code, "pre_freeze": true,
		})
		writeError(w, http.StatusServiceUnavailable, "server_error", code, message)
		return
	}
	if err := relay.materializeModelExecutionRoutes(
		ctx, adapters.EndpointChatCompletions, req.Model, requestedModel, requestID, requestStart, executionBatch,
	); err != nil {
		if code, message, limitError := modelExecutionLimitErrorDetails(err); limitError {
			relay.recordTraceEvent(ctx, requestID, "routing.request_limit_rejected", "routing", "warning", req.Model, requestedModel, nil, http.StatusBadRequest, elapsedMilliseconds(requestStart), map[string]interface{}{
				"error_code": code, "pre_freeze": true,
			})
			writeError(w, http.StatusBadRequest, "invalid_request_error", code, message)
			return
		}
		recordCompressionReceipt(nil)
		relay.recordTraceEvent(ctx, requestID, "routing.no_route", "routing", "error", req.Model, requestedModel, nil, http.StatusServiceUnavailable, elapsedMilliseconds(requestStart), map[string]interface{}{
			"route_count": 0, "pre_freeze": true,
		})
		relay.recordRequest(ctx, requestID, orgID, keyID, 0, "", "", req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, http.StatusServiceUnavailable, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceNoRoute, estimatedTokens, "")
		writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", fmt.Sprintf("No available channel for model: %s", req.Model))
		return
	}
	estimatedInputTokens = executionBatch.limits.EstimatedInputTokens
	reservedCompletionTokens = executionBatch.limits.EstimatedOutputTokens
	estimatedTokens, estimateErr = checkedAdmissionInt(int64(estimatedInputTokens), int64(reservedCompletionTokens))
	if estimateErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.context_length_exceeded", "Estimated request tokens exceed the supported range")
		return
	}
	// A published public limit is a hard cap, including streams.
	ctx = contextWithStreamReservation(ctx, reservedCompletionTokens, false)
	ctx = contextWithAdmissionInputEstimate(ctx, estimatedInputTokens)
	acceptedMetadata := map[string]interface{}{
		"stream": req.Stream, "estimated_tokens": estimatedTokens,
		"admission_estimate_version": executionBatch.limits.EstimateVersion,
	}
	for key, value := range executionBatch.profileEvidence.metadata() {
		acceptedMetadata[key] = value
	}
	relay.recordTraceEvent(ctx, requestID, "gateway.request.accepted", "gateway", "info", req.Model, requestedModel, nil, 0, elapsedMilliseconds(requestStart), acceptedMetadata)
	relay.recordPayloadPolicyDecision(ctx, requestID, req.Model, requestedModel, elapsedMilliseconds(requestStart))

	// Freeze balance once (before trying any upstream)
	freezeResult, err := relay.billing.Freeze(ctx, billing.FreezeRequest{
		RequestID:                requestID,
		OrgID:                    orgID,
		ProjectID:                middleware.GetProjectIDFromContext(ctx),
		Model:                    req.Model,
		EstimatedTokens:          estimatedTokens,
		EstimatedInputTokens:     estimatedInputTokens,
		ReservedCompletionTokens: reservedCompletionTokens,
		ReservedMeterUsage:       slices.Clone(executionBatch.profileEvidence.ReservedMeterUsage),
		ExecutionSnapshotHash:    executionBatch.plan.SnapshotHash,
		SafetyFactor:             relay.freezeSafetyFactor,
		MinFreezeCents:           relay.minFreezeCents,
	})
	relay.recordLimitPolicyAdmission(ctx, w, limitPolicyCollector, requestID, req.Model, requestedModel, elapsedMilliseconds(requestStart))
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
		middleware.RecordBillingFreeze("error")
		if writeAgentCapabilityRuntimeRejection(w, freezeResult.ErrorCode) {
			return
		}
		if writeLimitPolicyRuntimeRejection(w, freezeResult.ErrorCode) {
			return
		}
		switch freezeResult.ErrorCode {
		case "insufficient_balance":
			writeError(w, http.StatusPaymentRequired, "billing_error", "billing_error.insufficient_balance", "Insufficient balance")
		case "already_frozen":
			writeError(w, http.StatusConflict, "billing_error", "billing_error.idempotent_conflict", "Request already being processed")
		default:
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", freezeResult.ErrorCode)
		}
		return
	}
	middleware.RecordBillingFreeze("success")
	relay.recordTraceEvent(ctx, requestID, "billing.freeze.succeeded", "billing", "info", req.Model, requestedModel, nil, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
		"freeze_amount_micros": microsText(freezeResult.AmountMicros),
		"currency":             "CNY", "amount_scale": money.MicrosPerCNY,
	})

	// Every candidate was planned, decrypted and built before Freeze.
	routes := executionBatch.routes
	client := relay.providerClient(providerTimeout)
	routingPolicy := relay.modelExecution.policy
	providerAttempts := 0

	// Failover loop: try each route until one succeeds
	var lastErr error
	var lastStatusCode int
	var lastUpstreamRequestID string
	var lastRoute *RouteResult
	lastAttemptCtx := ctx
	for routeIndex := range routes {
		preparedRoute := &routes[routeIndex]
		route := &preparedRoute.compat
		attemptCtx := contextWithRouteAttempt(ctx, routeIndex+1, len(routes))
		lastAttemptCtx = attemptCtx
		lastStatusCode = 0
		lastUpstreamRequestID = ""
		routeForTrace := *route
		adapter := preparedRoute.adapter
		upstreamReq := preparedRoute.upstreamRequest
		cacheDecision := relay.evaluateResponseCache(
			attemptCtx, r.Header, requestID, requestedModel, callerAPIKey, optimizationState,
			route, upstreamReq, freezeResult.PricingVersion, reqLog,
		)
		if cacheDecision.hit != nil {
			recordCompressionReceipt(route)
			relay.recordResponseCacheDecision(attemptCtx, cacheDecision, reqLog)
			w.Header().Set(responseCacheOptInHeader, "HIT")
			relay.serveResponseCacheHit(
				attemptCtx, w, route, cacheDecision.hit, cacheDecision.identity,
				optimizationState.policy, requestID, orgID, keyID, req.Model,
				requestedModel, r.RemoteAddr, requestStart, reqLog,
			)
			return
		}
		if cacheDecision.reject {
			recordCompressionReceipt(route)
			relay.recordResponseCacheDecision(attemptCtx, cacheDecision, reqLog)
			relay.billing.Cancel(context.WithoutCancel(attemptCtx), requestID, orgID)
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "server_error", "response_cache.contention", "Response cache contention could not be resolved safely")
			return
		}
		if cacheDecision.miss != nil {
			attemptCtx = contextWithResponseCacheMiss(attemptCtx, cacheDecision.miss)
			w.Header().Set(responseCacheOptInHeader, "MISS")
		} else if cacheDecision.reason != "" {
			w.Header().Set(responseCacheOptInHeader, "BYPASS")
		}
		var status modelexecution.Status
		attemptCtx, status, err = relay.authorizePreparedModelRoute(
			attemptCtx, executionBatch, preparedRoute, routeIndex+1, len(routes), adapters.EndpointChatCompletions,
		)
		if err != nil {
			relay.releaseResponseCacheMiss(attemptCtx, reqLog)
			relay.billing.Cancel(context.WithoutCancel(attemptCtx), requestID, orgID)
			reqLog.Error("Model execution authorization failed: %v", err)
			relay.recordTraceEvent(attemptCtx, requestID, "routing.dispatch_authority_failed", "routing", "error", req.Model, requestedModel, route, 0, elapsedMilliseconds(requestStart), map[string]interface{}{"status": status})
			writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing authority is temporarily unavailable")
			return
		}
		if status != modelexecution.Applied {
			relay.releaseResponseCacheMiss(attemptCtx, reqLog)
			lastErr = fmt.Errorf("route dispatch authority: %s", status)
			relay.recordTraceEvent(attemptCtx, requestID, "routing.dispatch_authority_changed", "routing", "warning", req.Model, requestedModel, route, 0, elapsedMilliseconds(requestStart), map[string]interface{}{"status": status})
			continue
		}
		dispatchCtx, stopProbeLease := relay.maintainExecutionLease(attemptCtx, reqLog)
		upstreamReq = upstreamReq.Clone(dispatchCtx)
		relay.recordResponseCacheDecision(attemptCtx, cacheDecision, reqLog)
		lastRoute = route
		routeForTrace = *route
		relay.recordTraceEvent(attemptCtx, requestID, "routing.route_selected", "routing", "info", req.Model, requestedModel, route, 0, elapsedMilliseconds(requestStart), routingPolicyTraceMetadata(routingPolicy, route, len(routes), routeIndex+1))
		lastAttemptCtx = attemptCtx

		recordCompressionReceipt(route)
		providerAttempts++
		upstreamStart := time.Now()
		resp, err := client.Do(upstreamReq)
		upstreamDuration := time.Since(upstreamStart)
		middleware.RecordUpstreamLatency(route.Channel.Provider, req.Model, upstreamDuration)
		if err != nil {
			relay.releaseResponseCacheMiss(attemptCtx, reqLog)
			stopProbeLease()
			classified := adapters.ClassifyTransportError(attemptCtx, err)
			healthAuthorityFailed := false
			completionErr := relay.completeProviderAttemptWithRetry(providerAttemptCompletion(
				attemptCtx, requestID, orgID, billing.ProviderAttemptOutcomeUnknown, false, 0,
				string(classified.FailureKind), "",
			), reqLog)
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Upstream transport failed: kind=%s retryable=%t", classified.FailureKind, classified.Retryable)
			if classified.Retryable {
				if healthErr := relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthProviderFailure); healthErr != nil {
					reqLog.Error("Failed to record provider transport health: %v", healthErr)
					healthAuthorityFailed = relay.recordExecutionHealthFailure(attemptCtx, requestID, req.Model, requestedModel, route, 0, int(upstreamDuration.Milliseconds()), executionHealthProviderFailure, healthErr)
				}
				relay.emitProviderChannelFailure(attemptCtx, *route, requestID, requestedModel, 0, string(classified.FailureKind), providerFailureEventMessage(0, classified.FailureKind))
			}
			ledgerStatus := "recorded"
			if completionErr != nil {
				ledgerStatus = "persist_failed"
				reqLog.Error("Ambiguous provider attempt could not be confirmed after bounded retries: %v", completionErr)
			}
			relay.recordTraceEvent(attemptCtx, requestID, "provider.request_failed", "provider", "warning", req.Model, requestedModel, &routeForTrace, 0, int(upstreamDuration.Milliseconds()), map[string]interface{}{
				"failure_kind":               string(classified.FailureKind),
				"availability_retryable":     classified.Retryable,
				"financial_retry_authorized": false,
				"retry_decision":             "hold_financial_uncertainty",
				"outcome_certainty":          "unknown",
				"attempt_ledger_status":      ledgerStatus,
			})
			cleanupCtx := context.WithoutCancel(attemptCtx)
			relay.recordRequest(cleanupCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, 0, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceUpstreamFailed, estimatedTokens, "")
			if attemptCtx.Err() == nil {
				if healthAuthorityFailed {
					w.Header().Set("Retry-After", "1")
					writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
				} else {
					writeNonRetryableProviderError(w, 0, classified.FailureKind)
				}
			}
			return
		}
		upstreamRequestID := upstreamRequestIDFromHeader(resp.Header)
		if resp.StatusCode != http.StatusOK {
			relay.releaseResponseCacheMiss(attemptCtx, reqLog)
			stopProbeLease()
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
			// Per PRD F12: adapters normalize provider-specific HTTP semantics
			// into routing failure kinds. Client errors are not channel failures.
			classified := adapter.ClassifyHTTPStatus(resp.StatusCode, errBody)
			willRetry := classified.Retryable && providerAttempts <= routingPolicy.MaxRetryCount && routeIndex+1 < len(routes)
			if err := relay.completeProviderAttemptWithRetry(providerAttemptCompletion(
				attemptCtx, requestID, orgID, billing.ProviderAttemptOutcomeRejected,
				classified.Retryable, resp.StatusCode, string(classified.FailureKind), upstreamRequestID,
			), reqLog); err != nil {
				resp.Body.Close()
				relay.recordTraceEvent(attemptCtx, requestID, "billing.provider_attempt.complete_failed", "billing", "critical", req.Model, requestedModel, route, resp.StatusCode, int(upstreamDuration.Milliseconds()), map[string]interface{}{
					"error_code": "provider_attempt_complete_failed",
				})
				writeError(w, http.StatusInternalServerError, "server_error", "billing_error.provider_attempt_failed", "Failed to persist provider response authority")
				return
			}
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Upstream rejected response: status=%d kind=%s retryable=%t", resp.StatusCode, classified.FailureKind, classified.Retryable)
			var healthErr error
			switch classified.FailureKind {
			case adapters.FailureChannel:
				healthErr = relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthProviderFailure)
				relay.emitProviderChannelFailure(attemptCtx, *route, requestID, requestedModel, resp.StatusCode, string(classified.FailureKind), providerFailureEventMessage(resp.StatusCode, classified.FailureKind))
			case adapters.FailureAuth:
				healthErr = relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthCredentialAuthFailure)
				relay.emitProviderChannelFailure(attemptCtx, *route, requestID, requestedModel, resp.StatusCode, string(classified.FailureKind), providerFailureEventMessage(resp.StatusCode, classified.FailureKind))
			case adapters.FailureRateLimit:
				healthErr = relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthCredentialRateLimited)
				relay.emitProviderChannelFailure(attemptCtx, *route, requestID, requestedModel, resp.StatusCode, string(classified.FailureKind), providerFailureEventMessage(resp.StatusCode, classified.FailureKind))
			default:
				// 4xx client errors (400, 404, etc.) — don't mark as failure
			}
			if healthErr != nil {
				reqLog.Error("Failed to record provider rejection health: %v", healthErr)
				if relay.recordExecutionHealthFailure(attemptCtx, requestID, req.Model, requestedModel, route, resp.StatusCode, int(upstreamDuration.Milliseconds()), healthActionForFailureKind(classified.FailureKind), healthErr) {
					resp.Body.Close()
					relay.billing.Cancel(context.WithoutCancel(attemptCtx), requestID, orgID)
					w.Header().Set("Retry-After", "1")
					writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
					return
				}
			}
			relay.recordTraceEvent(attemptCtx, requestID, "provider.response_rejected", "provider", "warning", req.Model, requestedModel, &routeForTrace, resp.StatusCode, int(upstreamDuration.Milliseconds()), withProcessingTierResponseMetadata(attemptCtx, resp.Header, map[string]interface{}{
				"failure_kind":   string(classified.FailureKind),
				"retryable":      classified.Retryable,
				"retry_decision": providerRetryDecision(willRetry),
			}), upstreamRequestID)
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
		if err := relay.completeProviderAttemptWithRetry(providerAttemptCompletion(
			attemptCtx, requestID, orgID, billing.ProviderAttemptOutcomeResponseAccepted,
			false, resp.StatusCode, "", upstreamRequestID,
		), reqLog); err != nil {
			relay.releaseResponseCacheMiss(attemptCtx, reqLog)
			stopProbeLease()
			resp.Body.Close()
			relay.recordTraceEvent(attemptCtx, requestID, "billing.provider_attempt.complete_failed", "billing", "critical", req.Model, requestedModel, route, resp.StatusCode, int(upstreamDuration.Milliseconds()), map[string]interface{}{
				"error_code": "provider_attempt_complete_failed",
			})
			writeError(w, http.StatusInternalServerError, "server_error", "billing_error.provider_attempt_failed", "Failed to persist provider response authority")
			return
		}

		if req.Stream {
			relay.relayStreamRequest(attemptCtx, w, resp, route, requestID, orgID, keyID, req.Model, requestedModel, r.RemoteAddr, req.Messages, reqLog, requestStart, estimatedTokens, adapter, upstreamRequestID, stopProbeLease)
		} else {
			deliveryCtx := contextWithProviderRequestCount(attemptCtx, int64(providerAttempts))
			relay.relayNonStreamRequest(deliveryCtx, w, resp, route, requestID, orgID, keyID, req.Model, requestedModel, r.RemoteAddr, reqLog, requestStart, adapter, estimatedInputTokens, reservedCompletionTokens, upstreamRequestID, stopProbeLease)
		}
		return
	}

	// All channels failed
	recordCompressionReceipt(nil)
	relay.billing.Cancel(ctx, requestID, orgID)
	if providerAttempts == 0 {
		relay.recordTraceEvent(ctx, requestID, "routing.no_dispatch", "routing", "warning", req.Model, requestedModel, nil, http.StatusServiceUnavailable, elapsedMilliseconds(requestStart), map[string]interface{}{
			"route_count": len(routes),
			"error_code":  "routing_candidate_unavailable",
		})
		relay.recordRequest(ctx, requestID, orgID, keyID, 0, "", "", req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, http.StatusServiceUnavailable, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceNoRoute, estimatedTokens, "")
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing candidates are temporarily unavailable")
		return
	}
	if lastErr != nil {
		reqLog.Error("All upstream channels failed, last error: %v", lastErr)
	}
	// Log the failed request
	if lastRoute != nil {
		relay.recordRequest(lastAttemptCtx, requestID, orgID, keyID, lastRoute.Channel.ID, lastRoute.Channel.Provider, lastRoute.Channel.Name, req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, lastStatusCode, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceUpstreamFailed, estimatedTokens, lastUpstreamRequestID)
	}
	writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_error", "Upstream request failed")
}

// relayNonStreamRequest handles non-streaming responses.
func (relay *Relay) relayNonStreamRequest(ctx context.Context, w http.ResponseWriter, resp *http.Response, route *RouteResult, requestID string, orgID, keyID int64, model, requestedModel, clientIP string, reqLog *logger.Logger, requestStart time.Time, adapter adapters.Adapter, estimatedInputTokens, reservedCompletionTokens int, upstreamRequestID string, stopProbeLeaseFn ...func()) {
	stopProbeLease := func() {}
	if len(stopProbeLeaseFn) > 0 && stopProbeLeaseFn[0] != nil {
		stopProbeLease = stopProbeLeaseFn[0]
	}
	defer stopProbeLease()
	defer resp.Body.Close()
	cacheMiss := responseCacheMissFromContext(ctx)
	if cacheMiss != nil {
		defer relay.releaseResponseCacheMiss(ctx, reqLog)
	}

	body, releaseResponsePayload, err := relay.responsePayload(ctx, resp.Body)
	if err != nil {
		status, errorType, errorCode, message, failureKind := payloadReadFailure(err)
		reqLog.Error("Failed to read bounded response body: %v", err)
		stopProbeLease()
		healthAuthorityFailed := false
		if !errors.Is(err, payloadpolicy.ErrPayloadTooLarge) && !errors.Is(err, payloadpolicy.ErrBudgetExceeded) {
			if healthErr := relay.applyRouteHealth(context.WithoutCancel(ctx), route, executionHealthProviderFailure); healthErr != nil {
				reqLog.Error("Failed to record unusable provider response health: %v", healthErr)
				healthAuthorityFailed = relay.recordExecutionHealthFailure(ctx, requestID, model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), executionHealthProviderFailure, healthErr)
			}
		}
		if ledgerErr := relay.completeProviderAttemptWithRetry(providerAttemptCompletion(
			ctx, requestID, orgID, billing.ProviderAttemptOutcomeResponseUnusable,
			false, resp.StatusCode, failureKind, upstreamRequestID,
		), reqLog); ledgerErr != nil {
			reqLog.Error("Failed to retain unusable provider response authority: %v", ledgerErr)
		}
		relay.recordTraceEvent(ctx, requestID, "provider.response_unusable", "provider", "error", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
			"failure_kind": failureKind,
		}, upstreamRequestID)
		if healthAuthorityFailed {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
			return
		}
		if errors.Is(err, payloadpolicy.ErrBudgetExceeded) {
			w.Header().Set("Retry-After", "1")
		}
		writeError(w, status, errorType, errorCode, message)
		return
	}
	defer releaseResponsePayload()

	if adapter == nil {
		adapter = adapters.NewOpenAICompatible()
	}
	normalized, err := adapter.ParseResponse(ctx, adapters.ResponseInput{
		Endpoint:                        adapters.EndpointChatCompletions,
		StatusCode:                      resp.StatusCode,
		Header:                          resp.Header,
		Body:                            body,
		Channel:                         route.Channel,
		RequestedModel:                  requestedModel,
		UpstreamModel:                   route.MappedModel,
		ModelOperationID:                route.ModelOperationID,
		ExecutionProfileID:              route.ExecutionProfileID,
		ExecutionProfileVersion:         route.ExecutionProfileVersion,
		ProfileDefinition:               route.ProfileDefinition,
		CacheUsageProfile:               route.CacheUsageProfile,
		ProtocolProfile:                 route.ModelProtocolProfile,
		OperationProtocolProfileID:      route.OperationProtocolProfileID,
		OperationProtocolProfileVersion: route.OperationProtocolProfileVersion,
	})
	if err != nil {
		reqLog.Error("Failed to parse adapter response: %v", err)
		stopProbeLease()
		healthAuthorityFailed := false
		if healthErr := relay.applyRouteHealth(context.WithoutCancel(ctx), route, executionHealthProviderFailure); healthErr != nil {
			reqLog.Error("Failed to record provider parse health: %v", healthErr)
			healthAuthorityFailed = relay.recordExecutionHealthFailure(ctx, requestID, model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), executionHealthProviderFailure, healthErr)
		}
		if ledgerErr := relay.completeProviderAttemptWithRetry(providerAttemptCompletion(
			ctx, requestID, orgID, billing.ProviderAttemptOutcomeResponseUnusable,
			false, resp.StatusCode, "response_parse", upstreamRequestID,
		), reqLog); ledgerErr != nil {
			reqLog.Error("Failed to retain unusable provider response authority: %v", ledgerErr)
		}
		if healthAuthorityFailed {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
			return
		}
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to parse upstream response")
		return
	}
	if normalized.Usage.IsMissing() {
		meterUsage := slices.Clone(normalized.Usage.MeterUsage)
		normalized.Usage, err = adapters.NewConservativeEstimate(estimatedInputTokens, reservedCompletionTokens)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "billing_error.settle_failed", "Unable to establish bounded usage authority")
			return
		}
		normalized.Usage, err = normalized.Usage.WithMeterUsage(meterUsage)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "billing_error.settle_failed", "Unable to establish non-token usage authority")
			return
		}
		relay.recordTraceEvent(ctx, requestID, "usage.provider_missing", "billing", "warning", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
			"usage_authority":        normalized.Usage.Authority,
			"usage_finality":         normalized.Usage.Finality,
			"usage_estimate_version": normalized.Usage.EstimateVersion,
		}, upstreamRequestID)
	} else {
		normalized.Usage, err = normalized.Usage.ForCustomerCharge()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "billing_error.settle_failed", "Unable to establish usage authority")
			return
		}
	}
	estimatedTokens := estimatedInputTokens + reservedCompletionTokens
	responseBody := normalized.Body
	if len(responseBody) == 0 {
		responseBody = body
	}
	responseMap := normalized.DeliveryObject
	if responseMap != nil && !bytes.Equal(normalized.DeliveryObjectBody, responseBody) {
		responseMap = nil
	}
	var responseMapErr error
	if responseMap == nil {
		responseMapErr = decodeJSONUseNumber(responseBody, &responseMap)
	}
	if responseMapErr != nil || responseMap == nil {
		stopProbeLease()
		healthAuthorityFailed := false
		if healthErr := relay.applyRouteHealth(context.WithoutCancel(ctx), route, executionHealthProviderFailure); healthErr != nil {
			reqLog.Error("Failed to record malformed provider response health: %v", healthErr)
			healthAuthorityFailed = relay.recordExecutionHealthFailure(ctx, requestID, model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), executionHealthProviderFailure, healthErr)
		}
		if ledgerErr := relay.completeProviderAttemptWithRetry(providerAttemptCompletion(
			ctx, requestID, orgID, billing.ProviderAttemptOutcomeResponseUnusable,
			false, resp.StatusCode, "response_parse", upstreamRequestID,
		), reqLog); ledgerErr != nil {
			reqLog.Error("Failed to retain unusable provider response authority: %v", ledgerErr)
		}
		if healthAuthorityFailed {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
			return
		}
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to parse upstream response")
		return
	}
	holdUndeliveredOutput := func(status int, reason string) error {
		holdReq := settleRequestFromUsage(requestID, attemptIDFromContext(ctx), orgID, normalized.Usage)
		holdReq.UsagePurpose = metering.UsagePurposeProviderEvidence
		if err := relay.holdNonStreamOutputWithRetry(ctx, holdReq, reason, reqLog); err != nil {
			return err
		}
		evidenceCtx, cancelEvidence := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancelEvidence()
		evidenceUsage := normalized.Usage
		evidenceUsage.Purpose = metering.UsagePurposeProviderEvidence
		relay.recordRequestWithUsage(evidenceCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, model, requestedModel, clientIP,
			evidenceUsage, 0, status, elapsedMilliseconds(requestStart), 0, false, estimatedTokens, upstreamRequestID, nil)
		return nil
	}
	deliveryInspection := inspectNonStreamOutputTextsForRoute(responseMap, route)
	if !deliveryInspection.complete() {
		stopProbeLease()
		healthAuthorityFailed := false
		if healthErr := relay.applyRouteHealth(context.WithoutCancel(ctx), route, executionHealthProviderFailure); healthErr != nil {
			reqLog.Error("Failed to record unsupported provider response health: %v", healthErr)
			healthAuthorityFailed = relay.recordExecutionHealthFailure(ctx, requestID, model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), executionHealthProviderFailure, healthErr)
		}
		if err := holdUndeliveredOutput(http.StatusBadGateway, billing.OutputHoldReasonProviderResponseUnusable); err != nil {
			reqLog.Error("Failed to retain unsupported provider response usage: %v", err)
			writeError(w, http.StatusInternalServerError, "server_error", "billing_error.provider_attempt_failed", "Failed to retain provider cost authority")
			return
		}
		relay.recordTraceEvent(ctx, requestID, "provider.response_unusable", "provider", "warning", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
			"failure_kind": "unsupported_response_shape", "delivery_profile": chatDeliveryWhitelistVersion,
			"incomplete_paths": boundedIncompletePathList(deliveryInspection.incompletePaths, 240),
		}, upstreamRequestID)
		if healthAuthorityFailed {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
			return
		}
		writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_error", "Upstream response could not be delivered safely")
		return
	}
	stopProbeLease()
	if healthErr := relay.applyRouteHealth(context.WithoutCancel(ctx), route, executionHealthSuccess); healthErr != nil {
		reqLog.Error("Failed to record provider success health: %v", healthErr)
		if relay.recordExecutionHealthFailure(ctx, requestID, model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), executionHealthSuccess, healthErr) {
			if holdErr := holdUndeliveredOutput(http.StatusServiceUnavailable, billing.OutputHoldReasonProviderAttemptUnresolved); holdErr != nil {
				reqLog.Error("Failed to retain provider usage after routing health failure: %v", holdErr)
				writeError(w, http.StatusInternalServerError, "server_error", "billing_error.provider_attempt_failed", "Failed to retain provider cost authority")
				return
			}
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
			return
		}
	}
	relay.recordTraceEvent(ctx, requestID, "provider.response_accepted", "provider", "info", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), processingTierResponseMetadata(ctx, resp.Header), upstreamRequestID)
	if pinnedGuardrail := guardrailPinFromContext(ctx); pinnedGuardrail != nil {
		guardrailResult, outputRefs := evaluateNonStreamOutputGuardrailForProtocol(ctx, pinnedGuardrail, responseMap, route.ModelProtocolProfile)
		guardrailEvidenceCtx, cancelGuardrailEvidence := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		relay.recordGuardrailResult(guardrailEvidenceCtx, requestID, model, requestedModel, normalizedGuardrailStage(guardrail.StageNonStreamOutput), guardrailResult, route, resp.StatusCode, upstreamRequestID)
		cancelGuardrailEvidence()
		if guardrailResult.Decision == guardrail.DecisionBlock {
			unavailable := guardrailBlockedByUnavailable(guardrailResult)
			status := http.StatusForbidden
			if unavailable {
				status = http.StatusServiceUnavailable
			}
			if err := holdUndeliveredOutput(status, billing.OutputHoldReasonGuardrailWithheld); err != nil {
				reqLog.Error("Failed to retain guardrail-withheld provider usage: %v", err)
				relay.recordTraceEvent(ctx, requestID, "billing.guardrail_hold.failed", "billing", "error", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
					"error_code": "guardrail_output_hold_failed",
				}, upstreamRequestID)
				writeError(w, http.StatusInternalServerError, "server_error", "billing_error.provider_attempt_failed", "Failed to retain provider cost authority")
				return
			}
			writeOutputGuardrailBlock(w, unavailable)
			return
		}
		if err := applyProfileAwareOutputGuardrailTransform(route, guardrailResult, outputRefs); err != nil {
			if holdErr := holdUndeliveredOutput(http.StatusInternalServerError, billing.OutputHoldReasonGuardrailWithheld); holdErr != nil {
				reqLog.Error("Failed to retain transform-failed provider usage: %v", holdErr)
				relay.recordTraceEvent(ctx, requestID, "billing.guardrail_hold.failed", "billing", "error", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
					"error_code": "guardrail_transform_hold_failed",
				}, upstreamRequestID)
				writeError(w, http.StatusInternalServerError, "server_error", "billing_error.provider_attempt_failed", "Failed to retain provider cost authority")
				return
			}
			relay.recordTraceEvent(ctx, requestID, "guardrail.transform.failed", "guardrail_nonstream_output", "error", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
				"error_code": "guardrail_transform_failed",
			}, upstreamRequestID)
			writeError(w, http.StatusInternalServerError, "server_error", "guardrail_error.transform_failed", "Failed to apply output policy safely")
			return
		}
	}
	basePayload, responseMaxBytes, err := relay.marshalNonStreamResponseBase(responseMap)
	if err != nil {
		if errors.Is(err, payloadpolicy.ErrPayloadTooLarge) {
			if holdErr := holdUndeliveredOutput(http.StatusBadGateway, billing.OutputHoldReasonResponseTooLarge); holdErr != nil {
				reqLog.Error("Failed to retain oversized provider response usage: %v", holdErr)
				writeError(w, http.StatusInternalServerError, "server_error", "billing_error.provider_attempt_failed", "Failed to retain provider cost authority")
				return
			}
			relay.recordTraceEvent(ctx, requestID, "provider.response_unusable", "provider", "warning", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "response_too_large",
			}, upstreamRequestID)
			writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_response_too_large", "Upstream response exceeds the non-stream safety limit")
			return
		}
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to encode response")
		return
	}

	settleReq := settleRequestFromUsage(requestID, attemptIDFromContext(ctx), orgID, normalized.Usage)
	prepared, err := relay.prepareNonStreamDeliveryWithTimeout(ctx, settleReq)
	if err != nil || prepared == nil || !prepared.Funded {
		reqLog.Error("Billing delivery preparation failed; response withheld: %v", err)
		relay.recordTraceEvent(ctx, requestID, "billing.delivery_prepare.failed", "billing", "error", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code": "delivery_prepare_failed",
		})
		writeError(w, http.StatusInternalServerError, "server_error", "billing_error.settle_failed", "Failed to secure billing before delivery")
		return
	}
	preview := &prepared.Preview
	previewActual, previewCharged := settledCosts(preview)

	// Build the response from the fully funded preview. The embedded billing
	// status is deliberately non-terminal; the append-only idempotency receipt
	// becomes terminal only after the local write and settlement both finish.
	attempt := routeAttemptFromContext(ctx)
	responseExtension := map[string]interface{}{
		"request_id":            requestID,
		"cost":                  preparedDeliveryCostExtension(normalized.Usage, preview, previewActual, previewCharged),
		"provider":              route.Channel.Provider,
		"channel":               route.Channel.Name,
		"cache_hit":             normalized.Usage.HasAuthoritativeCacheHit(),
		"provider_prompt_cache": providerPromptCacheExtension(normalized.Usage, preview),
		"fallback":              attempt.Sequence > 1,
		"attempt_count":         attempt.Sequence,
		"route_count":           attempt.Total,
		"attempt_id":            attemptIDFromContext(ctx),
		"billing_status":        "delivery_prepared",
	}
	payload, err := appendNonStreamResponseExtension(basePayload, responseExtension, responseMaxBytes)
	if err != nil {
		deliveryCtx, deliveryCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		recordErr := relay.billing.RecordDelivery(deliveryCtx, billing.DeliveryEvent{
			RequestID: requestID, AttemptID: attemptIDFromContext(ctx), OrgID: orgID,
			Mode: billing.DeliveryModeNonStream, Outcome: billing.DeliveryOutcomeWriteFailed,
			HTTPStatus: resp.StatusCode, ErrorCode: "response_encode_failed",
		})
		deliveryCancel()
		if recordErr != nil {
			reqLog.Error("Failed to retain pre-delivery response encoding failure: %v", recordErr)
		}
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to encode response")
		return
	}
	deliveryEvent := billing.DeliveryEvent{
		RequestID: requestID, AttemptID: attemptIDFromContext(ctx), OrgID: orgID,
		Mode: billing.DeliveryModeNonStream, Outcome: billing.DeliveryOutcomeWriteStarted,
		HTTPStatus: resp.StatusCode,
	}
	deliveryCtx, deliveryCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	if err := relay.billing.RecordDelivery(deliveryCtx, deliveryEvent); err != nil {
		deliveryCancel()
		reqLog.Error("Failed to persist delivery start; response withheld: %v", err)
		writeError(w, http.StatusInternalServerError, "server_error", "billing_error.settle_failed", "Failed to establish delivery receipt")
		return
	}
	deliveryCancel()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	written, writeErr := w.Write(payload)
	if writeErr != nil || written != len(payload) {
		deliveryEvent.Outcome = billing.DeliveryOutcomeWriteFailed
		deliveryEvent.DeliveredBytes = int64(written)
		deliveryEvent.ErrorCode = "downstream_write_failed"
		failureCtx, failureCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		if err := relay.billing.RecordDelivery(failureCtx, deliveryEvent); err != nil {
			reqLog.Error("Failed to persist downstream write failure: %v", err)
		}
		failureCancel()
		reqLog.Error("Downstream non-stream write failed: wrote=%d expected=%d err=%v", written, len(payload), writeErr)
		return
	}
	deliveryEvent.Outcome = billing.DeliveryOutcomeCompleted
	deliveryEvent.DeliveredBytes = int64(written)
	deliveryEvent.DeliveredChunks = 1
	completedCtx, completedCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	if err := relay.billing.RecordDelivery(completedCtx, deliveryEvent); err != nil {
		completedCancel()
		reqLog.Error("Failed to persist completed delivery; settlement retained: %v", err)
		return
	}
	completedCancel()

	settleOutcome, settleErr := relay.settlePendingBillingWithRetry(settleReq, reqLog)
	if settleErr != nil {
		reqLog.Error("Billing settle failed after local delivery; recovery retained: %v", settleErr)
		relay.recordTraceEvent(ctx, requestID, "billing.settle.pending_recovery", "billing", "error", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code": "settle_failed",
		})
		relay.recordRequestWithUsage(ctx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, model, requestedModel, clientIP,
			normalized.Usage, 0, resp.StatusCode, elapsedMilliseconds(requestStart), 0, false, estimatedTokens, upstreamRequestID, nil)
		return
	}
	actualCost, chargedCost := settledCosts(settleOutcome.Result)
	relay.recordTraceEvent(ctx, requestID, "billing.settle.succeeded", "billing", "info", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), settlementTraceMetadata(normalized.Usage, settleOutcome.Result, actualCost, chargedCost))
	middleware.RecordTokenUsage("prompt", model, normalized.Usage.PromptTokens)
	middleware.RecordTokenUsage("completion", model, normalized.Usage.CompletionTokens)
	relay.recordRequestWithUsage(ctx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, model, requestedModel, clientIP,
		normalized.Usage, chargedCost, resp.StatusCode, elapsedMilliseconds(requestStart), 0, false, estimatedTokens, upstreamRequestID, settleOutcome.Result)
	if cacheMiss != nil {
		relay.commitResponseCacheMiss(ctx, cacheMiss, basePayload, normalized.Usage, requestID, providerRequestCountFromContext(ctx), reqLog)
	}
	relay.checkProjectBudgetAlert(ctx)
}

// relayStreamRequest handles streaming SSE responses.
// Background checks signal through channels; one serialized writer owns every
// downstream Write+Flush pair, and the main goroutine owns resp.Body lifecycle.
func normalizeProviderStreamFrameForRoute(route *RouteResult, data []byte) ([]byte, error) {
	normalized, _, err := adapters.NormalizeKimiK26ChatStreamChunk(
		route.Channel.Provider, route.MappedModel, route.ModelProtocolProfile, data,
	)
	return normalized, err
}

func (relay *Relay) relayStreamRequest(ctx context.Context, w http.ResponseWriter, resp *http.Response, route *RouteResult, requestID string, orgID, keyID int64, model, requestedModel, clientIP string, messages []Message, reqLog *logger.Logger, requestStart time.Time, estimatedTokens int, adapter adapters.Adapter, upstreamRequestID string, stopProbeLeaseFn ...func()) {
	stopProbeLease := func() {}
	if len(stopProbeLeaseFn) > 0 && stopProbeLeaseFn[0] != nil {
		stopProbeLease = stopProbeLeaseFn[0]
	}
	defer stopProbeLease()
	upstreamClosed := false
	closeUpstream := func() {
		if upstreamClosed {
			return
		}
		upstreamClosed = true
		if err := resp.Body.Close(); err != nil {
			reqLog.Warn("Failed to close upstream stream body: %v", err)
		}
	}
	defer closeUpstream()
	_, ok := w.(http.Flusher)
	if !ok {
		reqLog.Error("ResponseWriter does not support Flusher")
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
	streamPolicy := relay.currentStreamPolicy()
	streamWriter := newSSEStreamWriter(w, streamPolicy.WriteIdleTimeout())
	streamWriteFailures := make(chan error, 1)
	writeSSECount := func(format string, args ...any) (int, bool) {
		written, err := streamWriter.WriteAndFlushCount(format, args...)
		if err != nil {
			select {
			case streamWriteFailures <- err:
			default:
			}
			return written, false
		}
		return written, true
	}
	writeSSE := func(format string, args ...any) bool {
		_, ok := writeSSECount(format, args...)
		return ok
	}

	billableOutputMarked := false
	var deliveryStarted atomic.Bool
	preOutputBillingFailure := false
	leaseStart := billing.StreamDeliveryStart{
		RequestID: requestID, AttemptID: attemptIDFromContext(ctx), OwnerID: processStreamLeaseOwner,
		OrgID: orgID, Generation: streampolicy.InitialLeaseGeneration, LeaseTTL: streampolicy.CheckpointLeaseDuration,
	}
	streamFence := leaseStart.Fence()
	leaseTerminalState := billing.StreamLeaseStateInterrupted
	leaseTerminalReason := "stream_ended"
	leaveLeaseForRecovery := false
	defer func() {
		if !deliveryStarted.Load() || leaveLeaseForRecovery {
			return
		}
		finishCtx, finishCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer finishCancel()
		if err := relay.billing.FinishStreamDelivery(finishCtx, leaseStart, leaseTerminalState, leaseTerminalReason); err != nil {
			reqLog.Error("Failed to persist stream lease terminal state: %v", err)
		}
	}()
	markBillableOutput := func() bool {
		if deliveryStarted.Load() {
			return true
		}
		deliveryCtx, deliveryCancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := relay.billing.BeginStreamDelivery(deliveryCtx, leaseStart)
		deliveryCancel()
		if err != nil {
			preOutputBillingFailure = true
			reqLog.Error("Failed to atomically establish stream delivery and process lease; content withheld: %v", err)
			relay.recordTraceEvent(ctx, requestID, "delivery.receipt.failed", "billing", "error", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
				"error_code": "delivery_start_unpersisted",
			})
			cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if cancelErr := relay.billing.Cancel(cancelCtx, requestID, orgID); cancelErr != nil {
				reqLog.Error("Failed to cancel reservation after stream delivery start failure: %v", cancelErr)
			}
			cancel()
			writeSSE("data: {\"error\":{\"type\":\"billing_error\",\"code\":\"billing_error.settle_failed\",\"message\":\"Unable to establish durable billing state\"}}\n\n")
			return false
		}
		billableOutputMarked = true
		deliveryStarted.Store(true)
		return true
	}

	// SSE heartbeat — send ping every 15 seconds to prevent client timeout
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	leaseFailures := make(chan error, 1)
	var heartbeatWg sync.WaitGroup
	heartbeatWg.Add(1)
	go func() {
		defer heartbeatWg.Done()
		ticker := time.NewTicker(streamPolicy.HeartbeatInterval())
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if deliveryStarted.Load() {
					renewCtx, renewCancel := context.WithTimeout(context.WithoutCancel(heartbeatCtx), 5*time.Second)
					renewErr := relay.billing.RenewStreamDelivery(renewCtx, leaseStart)
					renewCancel()
					if renewErr != nil {
						select {
						case leaseFailures <- renewErr:
						default:
						}
						return
					}
				}
				if !writeSSE(": ping\n\n") {
					return
				}
			}
		}
	}()

	kimiStreamMechanics, currentKimiStream, kimiStreamSetupErr := adapters.ResolveKimiRuntimeChatStreamMechanics(
		adapters.KimiChatStreamRuntimeIdentity{
			ProfileDefinition: route.ProfileDefinition,
			OperationID:       route.ModelOperationID, ExecutionProfileID: route.ExecutionProfileID,
			ExecutionProfileVersion: route.ExecutionProfileVersion, Provider: route.Channel.Provider,
			UpstreamModel: route.MappedModel, ProtocolProfileID: route.OperationProtocolProfileID,
			ProtocolProfileVersion: route.OperationProtocolProfileVersion, CacheUsageProfile: route.CacheUsageProfile,
		},
	)
	deepSeekStreamMechanics, currentDeepSeekStream, deepSeekStreamSetupErr := adapters.ResolveDeepSeekRuntimeChatStreamMechanics(
		adapters.DeepSeekChatStreamRuntimeIdentity{
			ProfileDefinition: route.ProfileDefinition,
			OperationID:       route.ModelOperationID, ExecutionProfileID: route.ExecutionProfileID,
			ExecutionProfileVersion: route.ExecutionProfileVersion, Provider: route.Channel.Provider,
			UpstreamModel: route.MappedModel, ProtocolProfileID: route.OperationProtocolProfileID,
			ProtocolProfileVersion: route.OperationProtocolProfileVersion, CacheUsageProfile: route.CacheUsageProfile,
		},
	)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), streamPolicy.MaxSSELineBytes)
	functionToolStream := false
	if contract, ok := modelprofile.ResolveChatProtocolContract(route.ModelProtocolProfile); ok {
		functionToolStream = contract.StreamContract.FunctionToolDeltas
	}
	if currentKimiStream || functionToolStream {
		scanner.Split(adapters.SplitSSELines)
	}
	type streamScanEvent struct {
		line string
		err  error
		done bool
	}
	scanEvents := make(chan streamScanEvent)
	var scannerWg sync.WaitGroup
	scannerWg.Add(1)
	go func() {
		defer scannerWg.Done()
		defer close(scanEvents)
		for scanner.Scan() {
			select {
			case scanEvents <- streamScanEvent{line: scanner.Text()}:
			case <-heartbeatCtx.Done():
				return
			}
		}
		select {
		case scanEvents <- streamScanEvent{err: scanner.Err(), done: true}:
		case <-heartbeatCtx.Done():
		}
	}()

	deliveredCounter := tokenutil.NewStreamTokenCounter()
	if admittedInput, exists := admissionInputEstimateFromContext(ctx); exists {
		deliveredCounter.SetEstimatedInputTokens(admittedInput)
	} else {
		deliveredCounter.SetEstimatedInputTokens(estimateChatInputTokens(messages))
	}
	// The customer counter remains a best-effort settlement estimate for an
	// interrupted stream. This independent byte upper bound is the authority for
	// admission and the explicit/public output cap.
	admittedOutputTokens := 0
	usedUpstreamUsage := false
	streamUsage, _ := adapters.NewUsage(0, 0, 0, metering.UsageSourceCounterV2)
	var deliveredBytes int64
	deliveredChunks := 0
	deliveryPartialRecorded := false
	deliveryReceiptFailure := false
	streamTerminationCode := ""
	pinnedStreamGuardrail := guardrailPinFromContext(ctx)
	streamGuardrailProfile := guardrail.StreamProfile{PolicySetVersion: "none"}
	if pinnedStreamGuardrail != nil {
		streamGuardrailProfile = pinnedStreamGuardrail.StreamProfile()
	}
	streamGuardrailHistory := make(map[string]string)
	streamAdvisoryHistory := make(map[string]string)
	reservationState := streamReservationFromContext(ctx)
	reservationSequence := 0
	ensureStreamBudgetTokens := func(additionalTokens int) bool {
		if additionalTokens <= 0 {
			return true
		}
		projected64, safe := checkedAdmissionAdd(int64(admittedOutputTokens), int64(additionalTokens))
		if !safe || projected64 > int64(maxChatCompletionTokens) {
			streamTerminationCode = "stream_output_limit_exceeded"
			return false
		}
		projected := int(projected64)
		if !reservationState.AllowExtension {
			if projected <= reservationState.ReservedCompletionTokens {
				return true
			}
			streamTerminationCode = "stream_output_limit_exceeded"
			return false
		}
		for projected > reservationState.ReservedCompletionTokens-streampolicy.ReservationGuardTokens {
			reservationSequence++
			target := reservationState.ReservedCompletionTokens + streampolicy.ReservationExtensionTokens
			extensionCtx, extensionCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			result, err := relay.billing.ExtendReservation(extensionCtx, billing.ExtendReservationRequest{
				RequestID: requestID, OrgID: orgID, Sequence: reservationSequence,
				AdditionalCompletionTokens: streampolicy.ReservationExtensionTokens,
				TargetCompletionTokens:     target,
			})
			extensionCancel()
			if err != nil || result == nil || !result.Success {
				streamTerminationCode = "stream_budget_extension_denied"
				errorCode := "extension_error"
				if result != nil && result.ErrorCode != "" {
					errorCode = result.ErrorCode
				}
				relay.recordTraceEvent(ctx, requestID, "billing.stream_reservation_extension.denied", "billing", "warning", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
					"error_code": errorCode, "reservation_extension_sequence": reservationSequence,
					"target_completion_tokens": target,
				})
				return false
			}
			reservationState.ReservedCompletionTokens = target
			relay.recordTraceEvent(ctx, requestID, "billing.stream_reservation_extension.succeeded", "billing", "info", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
				"reservation_extension_sequence": reservationSequence,
				"target_completion_tokens":       target, "reserved_amount_micros": microsText(result.AmountMicros),
				"pricing_version": result.PricingVersion,
			})
		}
		return true
	}
	lastCheckpointAt := time.Now()
	var checkpointBytes int64
	checkpointTokens := 0
	checkpointDelivery := func(force bool) bool {
		if !deliveryStarted.Load() || deliveredBytes <= 0 || deliveredChunks <= 0 {
			return true
		}
		if !force && time.Since(lastCheckpointAt) < streampolicy.CheckpointInterval &&
			deliveredBytes-checkpointBytes < streampolicy.CheckpointDeliveredBytes &&
			admittedOutputTokens-checkpointTokens < streampolicy.CheckpointCompletionTokens {
			return true
		}
		checkpointCtx, checkpointCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		_, err := relay.billing.CheckpointStreamDelivery(checkpointCtx, billing.StreamDeliveryCheckpoint{
			RequestID: requestID, AttemptID: leaseStart.AttemptID, OwnerID: leaseStart.OwnerID, OrgID: orgID,
			Generation:     leaseStart.Generation,
			DeliveredBytes: deliveredBytes, DeliveredChunks: deliveredChunks,
			PromptTokens: deliveredCounter.InputTokens(), CompletionTokens: admittedOutputTokens,
			PolicySetVersion: streamGuardrailProfile.PolicySetVersion,
			LeaseTTL:         streampolicy.CheckpointLeaseDuration,
		})
		checkpointCancel()
		if err != nil {
			reqLog.Error("Failed to persist stream delivery checkpoint: %v", err)
			if streamTerminationCode == "" {
				streamTerminationCode = "stream_checkpoint_unpersisted"
			}
			return false
		}
		lastCheckpointAt = time.Now()
		checkpointBytes = deliveredBytes
		checkpointTokens = admittedOutputTokens
		return true
	}
	ttftMs := 0
	streamStart := time.Now()
	firstTokenReceived := false
	var streamNormalizer adapters.StreamNormalizer
	var streamFrameNormalizer adapters.StreamFrameNormalizer
	if adapter != nil {
		streamNormalizer, _ = adapter.(adapters.StreamNormalizer)
		streamFrameNormalizer, _ = adapter.(adapters.StreamFrameNormalizer)
	}
	streamState := &adapters.StreamState{}
	routeHealthRecorded := false
	routeHealthAuthorityFailed := false
	streamPayloadInvalid := false
	providerStreamErrorObserved := false
	providerFrameObserved := false
	providerTerminalObserved := false
	providerFinishReasonObserved := false
	exactStreamState := exactStreamStateForProtocol(route.ModelProtocolProfile, route.MappedModel)
	toolEnvelope, toolEnvelopeErr := adapters.NewToolStreamEnvelope(route.ModelProtocolProfile, route.Channel.Provider, route.MappedModel, route.CacheUsageProfile)
	var functionToolAssembler *adapters.ChatSSEEventAssembler
	var functionToolSetupErr error
	if exactStreamState != nil && exactStreamState.functionTools != nil {
		functionToolAssembler, functionToolSetupErr = adapters.NewChatSSEEventAssembler(streamPolicy.MaxSSELineBytes)
	}
	var kimiStreamAssembler *adapters.ChatSSEEventAssembler
	var kimiStreamState *adapters.KimiChatStreamState
	var deepSeekStreamState *adapters.DeepSeekChatStreamState
	if currentKimiStream && kimiStreamSetupErr == nil {
		kimiStreamAssembler, kimiStreamSetupErr = adapters.NewChatSSEEventAssembler(streamPolicy.MaxSSELineBytes)
	}
	if currentKimiStream && kimiStreamSetupErr == nil {
		kimiStreamState, kimiStreamSetupErr = adapters.NewKimiChatStreamState(kimiStreamMechanics)
	}
	if currentDeepSeekStream && deepSeekStreamSetupErr == nil {
		deepSeekStreamState, deepSeekStreamSetupErr = adapters.NewDeepSeekChatStreamState(deepSeekStreamMechanics)
	}
	streamMechanicsSetupErr := kimiStreamSetupErr
	if streamMechanicsSetupErr == nil {
		streamMechanicsSetupErr = deepSeekStreamSetupErr
	}
	if streamMechanicsSetupErr == nil {
		streamMechanicsSetupErr = functionToolSetupErr
	}
	if streamMechanicsSetupErr == nil {
		streamMechanicsSetupErr = toolEnvelopeErr
	}
	if streamMechanicsSetupErr != nil {
		streamPayloadInvalid = true
		streamTerminationCode = "upstream_stream_invalid_frame"
	}
	recordRouteHealth := func(action executionHealthAction) {
		if routeHealthRecorded {
			return
		}
		routeHealthRecorded = true
		stopProbeLease()
		healthCtx, healthCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err := relay.applyRouteHealth(healthCtx, route, action)
		healthCancel()
		if err != nil {
			reqLog.Warn("Failed to record terminal upstream stream health: action=%d error=%v", action, err)
			routeHealthAuthorityFailed = relay.recordExecutionHealthFailure(ctx, requestID, model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), action, err)
			return
		}
		if action == executionHealthSuccess {
			relay.recordTraceEvent(ctx, requestID, "provider.response_accepted", "provider", "info", model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), processingTierResponseMetadata(ctx, resp.Header), upstreamRequestID)
		}
	}

	done := streamMechanicsSetupErr != nil
	skipRawEventSeparator := false
	recordPartialDelivery := func() bool {
		if deliveryPartialRecorded {
			return true
		}
		deliveryCtx, deliveryCancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := relay.billing.RecordDelivery(deliveryCtx, billing.DeliveryEvent{
			RequestID: requestID, AttemptID: attemptIDFromContext(ctx), OrgID: orgID,
			Mode: billing.DeliveryModeStream, Outcome: billing.DeliveryOutcomePartial,
			DeliveredBytes: deliveredBytes, DeliveredChunks: deliveredChunks,
			HTTPStatus: http.StatusOK, StreamFence: &streamFence,
		})
		deliveryCancel()
		if err != nil {
			deliveryReceiptFailure = true
			reqLog.Error("Failed to persist partial stream delivery; settlement withheld and provider liability retained: %v", err)
			relay.recordTraceEvent(ctx, requestID, "delivery.receipt.failed", "billing", "error", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
				"error_code":       "delivery_partial_unpersisted",
				"delivered_bytes":  deliveredBytes,
				"delivered_chunks": deliveredChunks,
			})
			return false
		}
		deliveryPartialRecorded = true
		return true
	}
	type pendingStreamFrame struct {
		data      []byte
		doneAfter bool
	}
	var pendingFrames []pendingStreamFrame
	pendingBufferBytes := 0
	var guardrailBufferTimer *time.Timer
	var guardrailBufferTimerC <-chan time.Time
	stopGuardrailBufferTimer := func() {
		if guardrailBufferTimer == nil {
			return
		}
		if !guardrailBufferTimer.Stop() {
			select {
			case <-guardrailBufferTimer.C:
			default:
			}
		}
		guardrailBufferTimer = nil
		guardrailBufferTimerC = nil
	}
	var flushGuardrailBuffer func() bool
	flushGuardrailBuffer = func() bool {
		if len(pendingFrames) == 0 {
			stopGuardrailBufferTimer()
			return true
		}
		frames := pendingFrames
		pendingFrames = nil
		pendingBufferBytes = 0
		stopGuardrailBufferTimer()
		data := make([][]byte, len(frames))
		for index := range frames {
			data[index] = frames[index].data
		}
		inspection, inspectErr := inspectGuardedStreamBatchForProtocol(
			ctx, pinnedStreamGuardrail, streamGuardrailProfile, data, streamGuardrailHistory, route.ModelProtocolProfile,
		)
		if len(inspection.Result.Receipts) > 0 {
			relay.recordGuardrailResult(ctx, requestID, model, requestedModel, guardrail.StageStreamOutput, inspection.Result, route, http.StatusOK, upstreamRequestID)
		}
		if inspectErr != nil {
			if streamGuardrailProfile.Enforced {
				streamTerminationCode = "guardrail_stream_invalid_shape"
				if errors.Is(inspectErr, errStreamGuardrailCrossBoundary) || errors.Is(inspectErr, errStreamGuardrailArguments) {
					streamTerminationCode = "guardrail_stream_transform_unsafe"
				}
				if errors.Is(inspectErr, errStreamGuardrailBufferExceeded) {
					streamTerminationCode = "guardrail_stream_buffer_exceeded"
				}
			} else {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
			}
			return false
		}
		if inspection.Result.Decision == guardrail.DecisionBlock {
			streamTerminationCode = "guardrail_stream_blocked"
			if guardrailBlockedByUnavailable(inspection.Result) {
				streamTerminationCode = "guardrail_stream_detector_unavailable"
			}
			return false
		}
		if profileStreamGuardrailTransformUnsafe(route, inspection.Result) {
			streamTerminationCode = "guardrail_stream_transform_unsafe"
			return false
		}
		additionalTokens := 0
		for _, frame := range inspection.Frames {
			frameTokens := len([]byte(frame.Text))
			if frameTokens > maxChatCompletionTokens-additionalTokens {
				streamTerminationCode = "stream_output_limit_exceeded"
				return false
			}
			additionalTokens += frameTokens
		}
		if !ensureStreamBudgetTokens(additionalTokens) {
			return false
		}
		if !markBillableOutput() {
			return false
		}
		for index, frame := range inspection.Frames {
			if !firstTokenReceived {
				firstTokenReceived = true
				ttftMs = int(time.Since(streamStart).Milliseconds())
			}
			payload := fmt.Sprintf("data: %s\n\n", frame.Data)
			written, writeOK := writeSSECount("%s", payload)
			if written == len(payload) {
				deliveredBytes += int64(written)
				deliveredChunks++
				if frame.Text != "" {
					admittedOutputTokens += len([]byte(frame.Text))
					deliveredCounter.AddOutputDelta(frame.Text)
				}
				if !checkpointDelivery(!deliveryPartialRecorded) {
					return false
				}
				if !recordPartialDelivery() {
					streamTerminationCode = "delivery_receipt_unpersisted"
					return false
				}
				if streamGuardrailProfile.Advisory {
					advisoryTexts := streamAdvisoryTextsWithHistory(frame.AdvisoryTexts, streamAdvisoryHistory)
					relay.evaluateStreamAdvisoryAfterDisclosure(ctx, pinnedStreamGuardrail, advisoryTexts, requestID, model, requestedModel, route, upstreamRequestID)
				}
			} else if written > 0 {
				deliveredBytes += int64(written)
			}
			if !writeOK {
				streamTerminationCode = "downstream_stream_write_failed"
				return false
			}
			if frames[index].doneAfter {
				done = true
			}
		}
		return true
	}
	enqueueGuardedFrame := func(data []byte, doneAfter, authoritativeUsage, preparedForDelivery bool) bool {
		if !preparedForDelivery {
			normalized, normalizeErr := normalizeProviderStreamFrameForRoute(route, data)
			if normalizeErr != nil {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
				return false
			}
			data = normalized
		}
		parsed, parseErr := parseGuardedStreamFrameForProtocol(data, route.ModelProtocolProfile)
		if parseErr != nil || parsed.unsupported {
			if streamGuardrailProfile.Enforced {
				streamTerminationCode = "guardrail_stream_invalid_shape"
			} else {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
			}
			return false
		}
		if stateErr := exactStreamState.accept(parsed, authoritativeUsage); stateErr != nil {
			streamPayloadInvalid = true
			streamTerminationCode = "upstream_stream_invalid_frame"
			return false
		}
		if currentDeepSeekStream {
			choiceCount := 0
			if parsed.choiceFrame {
				choiceCount = 1
			}
			finishReason := ""
			if parsed.terminal {
				finishReason = "observed"
			}
			if stateErr := deepSeekStreamState.AcceptRuntimeFacts(adapters.DeepSeekChatStreamFacts{
				ChoiceCount: choiceCount, FinishReason: finishReason, HasUsage: authoritativeUsage,
			}); stateErr != nil {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
				return false
			}
		}
		if parsed.choiceFrame {
			providerFinishReasonObserved = parsed.terminal
		}
		frameBufferBytes := len(data)
		if streamGuardrailProfile.Enforced && frameBufferBytes > streampolicy.EnforceBufferMaxBytes {
			streamTerminationCode = "guardrail_stream_buffer_exceeded"
			return false
		}
		if streamGuardrailProfile.Enforced && len(pendingFrames) > 0 &&
			pendingBufferBytes+frameBufferBytes > streampolicy.EnforceBufferMaxBytes {
			if !flushGuardrailBuffer() {
				return false
			}
		}
		pendingFrames = append(pendingFrames, pendingStreamFrame{data: append([]byte(nil), data...), doneAfter: doneAfter})
		pendingBufferBytes += frameBufferBytes
		if streamGuardrailProfile.Enforced && guardrailBufferTimer == nil {
			guardrailBufferTimer = time.NewTimer(streampolicy.EnforceBufferMaxWait)
			guardrailBufferTimerC = guardrailBufferTimer.C
		}
		if !streamGuardrailProfile.Enforced || pendingBufferBytes >= streampolicy.EnforceBufferMaxBytes || doneAfter {
			return flushGuardrailBuffer()
		}
		return true
	}
	processCurrentKimiEvent := func(data []byte) bool {
		if string(data) == "[DONE]" {
			if stateErr := kimiStreamState.AcceptDone(); stateErr != nil {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
				return false
			}
			if stateErr := exactStreamState.validateDone(); stateErr != nil {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
				return false
			}
			providerTerminalObserved = true
			if !flushGuardrailBuffer() {
				return false
			}
			done = true
			return true
		}
		providerError, classifyErr := providerStreamErrorFrame(data)
		if classifyErr != nil {
			streamPayloadInvalid = true
			streamTerminationCode = "upstream_stream_invalid_frame"
			return false
		}
		if providerError {
			providerStreamErrorObserved = true
			streamTerminationCode = "upstream_stream_provider_error"
			return false
		}
		prepared, prepareErr := adapters.PrepareKimiChatStreamEvent(data, kimiStreamMechanics)
		if prepareErr != nil || kimiStreamState.Accept(prepared) != nil {
			streamPayloadInvalid = true
			streamTerminationCode = "upstream_stream_invalid_frame"
			return false
		}
		frameData := prepared.Body
		if prepared.HasTopLevelUsage {
			streamUsage = prepared.TopLevelUsage
			usedUpstreamUsage = true
		}
		if streamFrameNormalizer != nil {
			frameData, prepareErr = streamFrameNormalizer.NormalizeStreamFrame(ctx, adapters.StreamChunkInput{
				Data: frameData, Channel: route.Channel, RequestedModel: requestedModel,
				UpstreamModel: route.MappedModel, CacheUsageProfile: route.CacheUsageProfile, State: streamState,
			})
			if prepareErr != nil || !json.Valid(frameData) {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
				return false
			}
		}
		providerFrameObserved = true
		return enqueueGuardedFrame(frameData, false, prepared.HasTopLevelUsage, true)
	}
	processFunctionToolEvent := func(data []byte) bool {
		invalid := func() bool {
			streamPayloadInvalid = true
			streamTerminationCode = "upstream_stream_invalid_frame"
			return false
		}
		if string(data) == "[DONE]" {
			if toolEnvelope.AcceptDone() != nil || exactStreamState.validateDone() != nil {
				return invalid()
			}
			providerTerminalObserved = true
			done = true
			return flushGuardrailBuffer()
		}
		// Raw duplicate validation must precede provider metadata map decoding.
		if rejectDuplicateJSONMembers(data) != nil || !json.Valid(data) {
			return invalid()
		}
		if streamFrameNormalizer != nil {
			var err error
			data, err = streamFrameNormalizer.NormalizeStreamFrame(ctx, adapters.StreamChunkInput{Data: data, Channel: route.Channel, RequestedModel: requestedModel, UpstreamModel: route.MappedModel, CacheUsageProfile: route.CacheUsageProfile, State: streamState})
			if err != nil {
				return invalid()
			}
		}
		providerError, err := providerStreamErrorFrame(data)
		if err != nil {
			return invalid()
		}
		if providerError {
			providerStreamErrorObserved = true
			streamTerminationCode = "upstream_stream_provider_error"
			return false
		}
		data, skip, prepareErr := toolEnvelope.Prepare(data)
		if skip {
			return true
		}
		if data == nil {
			return invalid()
		}
		usage, present, err := adapters.ParseOpenAICompatibleStreamUsageWithCacheProfile(data, adapters.EndpointChatCompletions, metering.UsageSourceUpstream, route.CacheUsageProfile)
		if err != nil {
			return invalid()
		}
		if present {
			streamUsage = usage
			usedUpstreamUsage = true
		}
		if prepareErr != nil {
			return invalid()
		}
		if validateFunctionToolStreamInvocation(route, data) != nil {
			return invalid()
		}
		providerFrameObserved = true
		return enqueueGuardedFrame(data, false, present, false)
	}
	var scanErr error
	firstSSELine := true
streamLoop:
	for !done {
		var line string
		select {
		case event, open := <-scanEvents:
			if !open {
				if !flushGuardrailBuffer() {
					done = true
				}
				break streamLoop
			}
			if event.done {
				scanErr = event.err
				if functionToolAssembler != nil {
					assembled, ready, err := functionToolAssembler.Finish()
					if err != nil {
						streamPayloadInvalid = true
						streamTerminationCode = "upstream_stream_invalid_frame"
					} else if ready && !processFunctionToolEvent(assembled.Data) {
						done = true
					}
				}
				if kimiStreamAssembler != nil {
					assembled, ready, assembleErr := kimiStreamAssembler.Finish()
					if assembleErr != nil {
						streamPayloadInvalid = true
						streamTerminationCode = "upstream_stream_invalid_frame"
					} else if ready && !processCurrentKimiEvent(assembled.Data) {
						done = true
					}
				}
				if !flushGuardrailBuffer() {
					done = true
				}
				break streamLoop
			}
			line = event.line
		case <-guardrailBufferTimerC:
			if !flushGuardrailBuffer() {
				done = true
				break streamLoop
			}
			continue
		case leaseErr := <-leaseFailures:
			reqLog.Error("Stream delivery lease renewal failed; stopping before another model-data flush: %v", leaseErr)
			streamTerminationCode = "stream_lease_lost"
			done = true
			break streamLoop
		case writeErr := <-streamWriteFailures:
			reqLog.Error("Downstream stream write failed: %v", writeErr)
			streamTerminationCode = "downstream_stream_write_failed"
			done = true
			break streamLoop
		case <-ctx.Done():
			streamTerminationCode = "downstream_stream_write_failed"
			done = true
			break streamLoop
		}
		if firstSSELine {
			line = strings.TrimPrefix(line, "\uFEFF")
			firstSSELine = false
		}
		if !utf8.ValidString(line) {
			streamPayloadInvalid = true
			streamTerminationCode = "upstream_stream_invalid_frame"
			done = true
			continue
		}
		if functionToolAssembler != nil {
			assembled, ready, err := functionToolAssembler.AcceptLine(line)
			if err != nil {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
				done = true
			} else if ready && !processFunctionToolEvent(assembled.Data) {
				done = true
			}
			continue
		}
		if kimiStreamAssembler != nil {
			assembled, ready, assembleErr := kimiStreamAssembler.AcceptLine(line)
			if assembleErr != nil {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
				done = true
				continue
			}
			if ready && !processCurrentKimiEvent(assembled.Data) {
				done = true
			}
			continue
		}
		field, hasField := parseSSEFieldLine(line)

		if streamNormalizer != nil {
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, ":") {
				continue
			}
			if !hasField || field.name != "data" {
				continue
			}
			data := field.value
			if data == "[DONE]" {
				if currentDeepSeekStream {
					if stateErr := deepSeekStreamState.AcceptDone(); stateErr != nil {
						streamPayloadInvalid = true
						streamTerminationCode = "upstream_stream_invalid_frame"
						done = true
						continue
					}
				}
				providerTerminalObserved = true
				if !flushGuardrailBuffer() {
					done = true
					continue
				}
				done = true
				continue
			}
			chunk, err := streamNormalizer.NormalizeStreamChunk(ctx, adapters.StreamChunkInput{
				Data:              []byte(data),
				Channel:           route.Channel,
				RequestedModel:    requestedModel,
				UpstreamModel:     route.MappedModel,
				CacheUsageProfile: route.CacheUsageProfile,
				State:             streamState,
			})
			if err != nil {
				reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Failed to normalize stream chunk: %v", err)
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
				done = true
				continue
			}
			if chunk == nil {
				continue
			}
			if !chunk.Skip && len(chunk.Data) > 0 {
				providerError, classifyErr := providerStreamErrorFrame(chunk.Data)
				if classifyErr != nil {
					streamPayloadInvalid = true
					streamTerminationCode = "upstream_stream_invalid_frame"
					done = true
					continue
				}
				if providerError {
					providerStreamErrorObserved = true
					streamTerminationCode = "upstream_stream_provider_error"
					done = true
					continue
				}
			}
			providerFrameObserved = true
			if chunk.HasUsage {
				streamUsage = chunk.Usage
				usedUpstreamUsage = true
			}
			if !chunk.Skip && len(chunk.Data) > 0 {
				if !enqueueGuardedFrame(chunk.Data, chunk.Done, chunk.HasUsage, false) {
					done = true
				}
			}
			if chunk.Done {
				if currentDeepSeekStream {
					if stateErr := deepSeekStreamState.AcceptDone(); stateErr != nil {
						streamPayloadInvalid = true
						streamTerminationCode = "upstream_stream_invalid_frame"
					}
				}
				if stateErr := exactStreamState.validateDone(); stateErr != nil {
					streamPayloadInvalid = true
					streamTerminationCode = "upstream_stream_invalid_frame"
				} else {
					providerTerminalObserved = true
				}
				if !flushGuardrailBuffer() {
					done = true
				}
				done = true
			}
			if done {
				continue
			}
			continue
		}

		if skipRawEventSeparator {
			if strings.TrimSpace(line) == "" {
				skipRawEventSeparator = false
				continue
			}
			skipRawEventSeparator = false
		}

		if hasField && field.name == "data" && field.value == "[DONE]" {
			if currentDeepSeekStream {
				if stateErr := deepSeekStreamState.AcceptDone(); stateErr != nil {
					streamPayloadInvalid = true
					streamTerminationCode = "upstream_stream_invalid_frame"
					done = true
					continue
				}
			}
			if stateErr := exactStreamState.validateDone(); stateErr != nil {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
				done = true
				continue
			}
			providerTerminalObserved = true
			if !flushGuardrailBuffer() {
				done = true
				continue
			}
			done = true
			continue
		}
		if hasField && field.name == "data" {
			data := field.value
			if !json.Valid([]byte(data)) {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
				done = true
				continue
			}
			frameData := []byte(data)
			if streamFrameNormalizer != nil {
				var normalizeErr error
				frameData, normalizeErr = streamFrameNormalizer.NormalizeStreamFrame(ctx, adapters.StreamChunkInput{
					Data:              frameData,
					Channel:           route.Channel,
					RequestedModel:    requestedModel,
					UpstreamModel:     route.MappedModel,
					CacheUsageProfile: route.CacheUsageProfile,
					State:             streamState,
				})
				if normalizeErr != nil || !json.Valid(frameData) {
					streamPayloadInvalid = true
					streamTerminationCode = "upstream_stream_invalid_frame"
					done = true
					continue
				}
			}
			providerError, classifyErr := providerStreamErrorFrame(frameData)
			if classifyErr != nil {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
				done = true
				continue
			}
			if providerError {
				providerStreamErrorObserved = true
				streamTerminationCode = "upstream_stream_provider_error"
				done = true
				continue
			}
			parsedUsage, hasUsage, usageErr := adapters.ParseOpenAICompatibleStreamUsageWithCacheProfile(
				frameData, adapters.EndpointChatCompletions, metering.UsageSourceUpstream, route.CacheUsageProfile,
			)
			if usageErr != nil {
				reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Failed to normalize final stream usage: %v", usageErr)
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
				done = true
				continue
			}
			if hasUsage {
				streamUsage = parsedUsage
				usedUpstreamUsage = true
			}
			providerFrameObserved = true
			skipRawEventSeparator = true
			if !enqueueGuardedFrame(frameData, false, hasUsage, false) {
				done = true
			}
			continue
		}

		// Provider comments and event labels are not part of the modeled Chat
		// delta protocol. Blank separators and comments are ignored; any other
		// SSE field fails closed before disclosure.
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, ":") {
			continue
		}
		streamPayloadInvalid = true
		streamTerminationCode = "upstream_stream_invalid_frame"
		done = true
	}
	stopGuardrailBufferTimer()
	pendingFrames = nil

	if deliveryStarted.Load() && deliveredBytes > 0 && deliveredChunks > 0 {
		_ = checkpointDelivery(true)
	}
	// Stop and join the short-TTL heartbeat before granting the longer terminal
	// lease. Otherwise an in-flight heartbeat can overwrite the finalization
	// expiry with a shorter lease after this goroutine has already renewed it.
	heartbeatCancel()
	heartbeatWg.Wait()
	finalizationLeaseLost := false
	if deliveryStarted.Load() {
		finalizationLease := leaseStart
		finalizationLease.LeaseTTL = streampolicy.FinalizationLeaseDuration
		renewCtx, renewCancel := context.WithTimeout(context.Background(), 5*time.Second)
		renewErr := relay.billing.RenewStreamDelivery(renewCtx, finalizationLease)
		renewCancel()
		if renewErr != nil {
			finalizationLeaseLost = true
			leaveLeaseForRecovery = true
			streamTerminationCode = "stream_lease_lost"
			reqLog.Error("Failed to retain stream lease for terminal billing; handing off to checkpoint recovery: %v", renewErr)
		}
	}

	// Stop background activity before finalization and release the provider
	// connection immediately. closeUpstream is idempotent for early returns.
	closeUpstream()
	scannerWg.Wait()
	// Some OpenAI-compatible providers, including MiniMax, close a clean SSE
	// connection after a fully terminated choice instead of emitting [DONE].
	// Do not stop on the finish frame itself because a provider usage frame may
	// still follow it. Only a clean EOF after every choice has a supported
	// finish_reason becomes terminal; a truncated stream without that proof
	// continues to fail closed below.
	if scanErr == nil && currentKimiStream && streamTerminationCode == "" {
		if stateErr := kimiStreamState.FinishEOF(); stateErr != nil {
			streamPayloadInvalid = true
			streamTerminationCode = "upstream_stream_invalid_frame"
		} else {
			providerTerminalObserved = true
			done = true
		}
	}
	if scanErr == nil && currentDeepSeekStream && !providerTerminalObserved && streamTerminationCode == "" {
		if stateErr := deepSeekStreamState.FinishEOF(); stateErr != nil {
			streamPayloadInvalid = true
			streamTerminationCode = "upstream_stream_invalid_frame"
		} else {
			providerTerminalObserved = true
			done = true
		}
	}
	if scanErr == nil && !currentKimiStream && !currentDeepSeekStream && !providerTerminalObserved && streamTerminationCode == "" {
		if stateErr := exactStreamState.validateCleanEOF(providerFinishReasonObserved); stateErr != nil {
			if exactStreamState != nil {
				streamPayloadInvalid = true
				streamTerminationCode = "upstream_stream_invalid_frame"
			}
		} else {
			providerTerminalObserved = true
			done = true
		}
	}
	if scanErr != nil && !preOutputBillingFailure {
		reqLog.Error("Upstream stream ended with a read error: %v", scanErr)
		if errors.Is(scanErr, bufio.ErrTooLong) {
			streamTerminationCode = "stream_frame_too_large"
		} else {
			streamTerminationCode = "upstream_stream_read_failed"
		}
	}
	if !done && streamTerminationCode == "" && !preOutputBillingFailure {
		streamTerminationCode = "upstream_stream_read_failed"
	}
	providerStreamFailed := scanErr != nil || streamPayloadInvalid || providerStreamErrorObserved || (!providerTerminalObserved && streamTerminationCode == "upstream_stream_read_failed")
	switch {
	case providerStreamFailed:
		recordRouteHealth(executionHealthProviderFailure)
	case providerTerminalObserved || providerFrameObserved:
		// Local delivery, policy and budget termination do not make a provider
		// unhealthy once the provider has supplied a syntactically valid frame.
		// Success is deliberately terminal here, after all upstream parsing and
		// synchronous output guardrails, rather than on the first valid chunk.
		recordRouteHealth(executionHealthSuccess)
	}
	if routeHealthAuthorityFailed && streamTerminationCode == "" {
		streamTerminationCode = "routing_health_transition_failed"
	}
	select {
	case writeErr := <-streamWriteFailures:
		reqLog.Error("Downstream stream write failed: %v", writeErr)
		streamTerminationCode = "downstream_stream_write_failed"
	default:
	}
	if streamTerminationCode != "" {
		leaseTerminalReason = streamTerminationCode
	}
	if preOutputBillingFailure {
		return
	}
	recordCtx, recordCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer recordCancel()
	recordStreamTermination := func(code, terminalBillingStatus string) {
		relay.recordTraceEvent(recordCtx, requestID, "stream.terminated", "stream", "warning", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code":                code,
			"billing_status":            terminalBillingStatus,
			"stream_policy_version":     streampolicy.Version,
			"stream_max_sse_line_bytes": streamPolicy.MaxSSELineBytes,
		})
	}
	recordTerminalDelivery := func(outcome, errorCode string) bool {
		deliveryCtx, deliveryCancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := relay.billing.RecordDelivery(deliveryCtx, billing.DeliveryEvent{
			RequestID: requestID, AttemptID: attemptIDFromContext(ctx), OrgID: orgID,
			Mode: billing.DeliveryModeStream, Outcome: outcome,
			DeliveredBytes: deliveredBytes, DeliveredChunks: deliveredChunks,
			HTTPStatus: http.StatusOK, ErrorCode: errorCode, StreamFence: &streamFence,
		})
		deliveryCancel()
		if err != nil {
			leaveLeaseForRecovery = true
			reqLog.Error("Failed to persist terminal stream delivery receipt: %v", err)
			relay.recordTraceEvent(recordCtx, requestID, "delivery.receipt.failed", "billing", "error", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
				"error_code":       "delivery_terminal_unpersisted",
				"delivery_outcome": outcome,
				"delivered_bytes":  deliveredBytes,
				"delivered_chunks": deliveredChunks,
			})
			return false
		}
		return true
	}
	recordStreamMetrics := func(usage adapters.Usage, chargedCost int64, settlementResult *billing.SettleResult) {
		middleware.RecordTokenUsage("prompt", model, usage.PromptTokens)
		middleware.RecordTokenUsage("completion", model, usage.CompletionTokens)
		middleware.RecordUpstreamLatency(route.Channel.Provider, model, time.Since(streamStart))
		relay.recordRequestWithUsage(recordCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, model, requestedModel, clientIP,
			usage, chargedCost, http.StatusOK, elapsedMilliseconds(requestStart), ttftMs, true, estimatedTokens, upstreamRequestID, settlementResult)
	}
	writeTerminationFrame := func(code, billingStatus string) {
		switch code {
		case "upstream_stream_read_failed":
			writeSSE("data: {\"error\":{\"type\":\"stream_error\",\"code\":\"stream_error.upstream_read_failed\",\"message\":\"Upstream stream ended before a valid terminal event\",\"request_id\":%q,\"billing_status\":%q}}\n\n", requestID, billingStatus)
		case "stream_frame_too_large":
			writeSSE("data: {\"error\":{\"type\":\"stream_error\",\"code\":\"stream_error.frame_too_large\",\"message\":\"Upstream stream frame exceeded the configured safety limit\",\"request_id\":%q,\"billing_status\":%q}}\n\n", requestID, billingStatus)
		case "upstream_stream_invalid_frame":
			writeSSE("data: {\"error\":{\"type\":\"stream_error\",\"code\":\"stream_error.invalid_frame\",\"message\":\"Upstream returned an invalid stream frame\",\"request_id\":%q,\"billing_status\":%q}}\n\n", requestID, billingStatus)
		case "upstream_stream_provider_error":
			writeSSE("data: {\"error\":{\"type\":\"upstream_error\",\"code\":\"upstream_error.stream_failed\",\"message\":\"Upstream failed while producing the stream\",\"request_id\":%q,\"billing_status\":%q}}\n\n", requestID, billingStatus)
		case "guardrail_stream_blocked":
			writeSSE("data: {\"error\":{\"type\":\"guardrail_error\",\"code\":\"guardrail_error.stream_output_blocked\",\"message\":\"Stream output was stopped by content policy\",\"request_id\":%q,\"billing_status\":%q}}\n\n", requestID, billingStatus)
		case "guardrail_stream_detector_unavailable", "guardrail_stream_invalid_shape", "guardrail_stream_transform_unsafe", "guardrail_stream_buffer_exceeded":
			writeSSE("data: {\"error\":{\"type\":\"guardrail_error\",\"code\":\"guardrail_error.stream_inspection_unavailable\",\"message\":\"Stream output could not be inspected safely\",\"request_id\":%q,\"billing_status\":%q}}\n\n", requestID, billingStatus)
		case "stream_budget_extension_denied":
			writeSSE("data: {\"error\":{\"type\":\"billing_error\",\"code\":\"billing_error.stream_budget_exhausted\",\"message\":\"Stream budget could not be extended\",\"request_id\":%q,\"billing_status\":%q}}\n\n", requestID, billingStatus)
		case "stream_output_limit_exceeded":
			writeSSE("data: {\"error\":{\"type\":\"stream_error\",\"code\":\"stream_error.output_limit_exceeded\",\"message\":\"Stream exceeded its explicit output limit\",\"request_id\":%q,\"billing_status\":%q}}\n\n", requestID, billingStatus)
		case "stream_lease_lost", "stream_checkpoint_unpersisted":
			writeSSE("data: {\"error\":{\"type\":\"stream_error\",\"code\":\"stream_error.delivery_state_unavailable\",\"message\":\"Durable stream delivery state is unavailable\",\"request_id\":%q,\"billing_status\":%q}}\n\n", requestID, billingStatus)
		case "routing_health_transition_failed":
			writeSSE("data: {\"error\":{\"type\":\"stream_error\",\"code\":\"stream_error.routing_health_unavailable\",\"message\":\"Routing health authority is temporarily unavailable\",\"request_id\":%q,\"billing_status\":%q}}\n\n", requestID, billingStatus)
		}
	}

	providerUsage := streamUsage
	if usedUpstreamUsage {
		relay.recordTraceEvent(recordCtx, requestID, "provider.usage.observed", "provider", "info", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
			"prompt_tokens":          providerUsage.PromptTokens,
			"cache_read_tokens":      providerUsage.CacheReadTokens,
			"cache_creation_tokens":  providerUsage.CacheCreationTokens,
			"completion_tokens":      providerUsage.CompletionTokens,
			"usage_authority":        providerUsage.Authority,
			"usage_purpose":          providerUsage.Purpose,
			"usage_finality":         providerUsage.Finality,
			"usage_contract_version": providerUsage.ContractVersion,
			"delivered_bytes":        deliveredBytes,
			"delivered_chunks":       deliveredChunks,
		}, upstreamRequestID)
	}

	if deliveryReceiptFailure {
		leaveLeaseForRecovery = true
		evidenceUsage := adapters.NewMissingUsage()
		if usedUpstreamUsage {
			evidenceUsage = providerUsage
		}
		recordStreamMetrics(evidenceUsage, 0, nil)
		recordStreamTermination("delivery_receipt_unpersisted", "provider_cost_pending")
		return
	}

	// A complete upstream stream may use provider-reported usage for customer
	// charging only because every model data write was locally accepted. Any
	// earlier transport interruption uses the independent delivered counter;
	// provider usage remains separate evidence and cannot create customer debt.
	customerUsage, usageErr := adapters.NewUsage(
		deliveredCounter.InputTokens(),
		deliveredCounter.OutputTokens(),
		deliveredCounter.InputTokens()+deliveredCounter.OutputTokens(),
		metering.UsageSourceCounterV2,
	)
	if streamTerminationCode == "" && usedUpstreamUsage {
		customerUsage, usageErr = providerUsage.ForCustomerCharge()
	}
	if usageErr != nil {
		reqLog.Error("Final stream customer usage contract is invalid: %v", usageErr)
		leaveLeaseForRecovery = true
		streamTerminationCode = "stream_checkpoint_unpersisted"
		recordStreamMetrics(adapters.NewMissingUsage(), 0, nil)
		recordStreamTermination(streamTerminationCode, "checkpoint_recovery_pending")
		writeTerminationFrame(streamTerminationCode, "checkpoint_recovery_pending")
		return
	}
	if finalizationLeaseLost {
		recordStreamMetrics(customerUsage, 0, nil)
		recordStreamTermination("stream_lease_lost", "checkpoint_recovery_pending")
		writeTerminationFrame("stream_lease_lost", "checkpoint_recovery_pending")
		return
	}

	if !billableOutputMarked {
		cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cancelErr := relay.billing.Cancel(cancelCtx, requestID, orgID)
		cancel()
		billingStatus := "cancelled_no_billable_output"
		if cancelErr != nil {
			billingStatus = "cancellation_failed_no_billable_output"
			reqLog.Error("Failed to cancel stream with no billable output: %v", cancelErr)
		}
		evidenceUsage := adapters.NewMissingUsage()
		if usedUpstreamUsage {
			evidenceUsage = providerUsage
		}
		relay.recordTraceEvent(recordCtx, requestID, "billing.cancelled.no_billable_output", "billing", "info", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
			"billing_status": billingStatus,
		})
		recordStreamMetrics(evidenceUsage, 0, nil)
		if streamTerminationCode != "" {
			recordStreamTermination(streamTerminationCode, billingStatus)
			writeTerminationFrame(streamTerminationCode, billingStatus)
			return
		}
		attempt := routeAttemptFromContext(ctx)
		finalChunk := map[string]interface{}{
			"object": "chat.completion.chunk", "model": model, "choices": []interface{}{},
			"usage": map[string]int{
				"prompt_tokens": evidenceUsage.PromptTokens, "completion_tokens": evidenceUsage.CompletionTokens,
				"total_tokens": evidenceUsage.PromptTokens + evidenceUsage.CompletionTokens,
			},
			"_uniroute": map[string]interface{}{
				"request_id": requestID, "cost": settlementCostExtension(evidenceUsage, nil, 0, 0),
				"provider": route.Channel.Provider, "channel": route.Channel.Name, "ttft_ms": ttftMs,
				"cache_hit": evidenceUsage.HasAuthoritativeCacheHit(), "provider_prompt_cache": providerPromptCacheExtension(evidenceUsage, nil),
				"fallback": attempt.Sequence > 1, "attempt_count": attempt.Sequence, "route_count": attempt.Total,
				"attempt_id": attemptIDFromContext(ctx), "billing_status": billingStatus,
			},
		}
		finalJSON, _ := json.Marshal(finalChunk)
		if !writeSSE("data: %s\n\n", finalJSON) || !writeSSE("data: [DONE]\n\n") {
			recordStreamTermination("downstream_stream_write_failed", billingStatus)
		}
		return
	}

	if !deliveryPartialRecorded {
		failureCode := streamTerminationCode
		if failureCode == "" {
			failureCode = "downstream_stream_write_failed"
		}
		recordTerminalDelivery(billing.DeliveryOutcomeWriteFailed, failureCode)
		evidenceUsage := adapters.NewMissingUsage()
		if usedUpstreamUsage {
			evidenceUsage = providerUsage
		}
		recordStreamMetrics(evidenceUsage, 0, nil)
		recordStreamTermination(failureCode, "provider_cost_pending")
		return
	}

	settleReq := settleRequestFromUsage(requestID, attemptIDFromContext(ctx), orgID, customerUsage)
	settleReq.StreamFence = &streamFence
	if persistErr := relay.persistSettlementUsageWithRetry(settleReq, reqLog); persistErr != nil {
		leaveLeaseForRecovery = true
		billingStatus := "settlement_pending_usage_unpersisted"
		reqLog.Error("Delivered stream usage was not durably persisted; reservation retained: %v", persistErr)
		relay.recordTraceEvent(recordCtx, requestID, "billing.settlement_usage.failed", "billing", "critical", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
			"billing_status": billingStatus, "error_code": "settlement_usage_persist_failed",
		})
		recordTerminalDelivery(billing.DeliveryOutcomeInterrupted, "settlement_usage_persist_failed")
		recordStreamMetrics(customerUsage, 0, nil)
		writeSSE("data: {\"error\":{\"type\":\"billing_error\",\"code\":\"billing_error.settle_failed\",\"message\":\"Usage is pending durable reconciliation\",\"request_id\":%q,\"billing_status\":%q}}\n\n", requestID, billingStatus)
		recordStreamTermination("settlement_usage_persist_failed", billingStatus)
		return
	}

	if streamTerminationCode != "" {
		recordTerminalDelivery(billing.DeliveryOutcomeInterrupted, streamTerminationCode)
	}
	settleOutcome, settleErr := relay.settlePendingBillingWithRetry(settleReq, reqLog)
	var actualCost int64
	var chargedCost int64
	billingStatus := "settled"
	var settlementResult *billing.SettleResult
	if settleErr != nil {
		billingStatus = "settlement_pending_recovery"
		reqLog.Error("Billing settle failed after local stream delivery; recovery retained the reservation: %v", settleErr)
		relay.recordTraceEvent(recordCtx, requestID, "billing.settle.pending_recovery", "billing", "error", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), map[string]interface{}{
			"billing_status": billingStatus, "error_code": "settle_failed",
		})
	} else {
		settlementResult = settleOutcome.Result
		actualCost, chargedCost = settledCosts(settlementResult)
		traceMetadata := settlementTraceMetadata(customerUsage, settlementResult, actualCost, chargedCost)
		traceMetadata["billing_status"] = billingStatus
		traceMetadata["ttft_ms"] = ttftMs
		relay.recordTraceEvent(recordCtx, requestID, "billing.settle.succeeded", "billing", "info", model, requestedModel, route, http.StatusOK, elapsedMilliseconds(requestStart), traceMetadata)
	}
	settledSuccessfully := settleErr == nil
	if settledSuccessfully && streamTerminationCode != "" {
		billingStatus = "settled_partial_stream"
	}
	recordStreamMetrics(customerUsage, chargedCost, settlementResult)
	if settledSuccessfully {
		relay.checkProjectBudgetAlert(recordCtx)
	}
	if streamTerminationCode != "" {
		leaseTerminalReason = streamTerminationCode
		recordStreamTermination(streamTerminationCode, billingStatus)
		writeTerminationFrame(streamTerminationCode, billingStatus)
		return
	}

	// Settlement follows a durable partial-delivery receipt. The final protocol
	// frames then decide whether that local delivery is completed or interrupted.
	attempt := routeAttemptFromContext(ctx)
	unirouteData := map[string]interface{}{
		"request_id": requestID, "cost": settlementCostExtension(customerUsage, settlementResult, actualCost, chargedCost),
		"provider": route.Channel.Provider, "channel": route.Channel.Name, "ttft_ms": ttftMs,
		"cache_hit": customerUsage.HasAuthoritativeCacheHit(), "provider_prompt_cache": providerPromptCacheExtension(customerUsage, settlementResult),
		"fallback": attempt.Sequence > 1, "attempt_count": attempt.Sequence, "route_count": attempt.Total,
		"attempt_id": attemptIDFromContext(ctx), "billing_status": billingStatus,
	}
	finalChunk := map[string]interface{}{
		"object": "chat.completion.chunk", "model": model, "choices": []interface{}{},
		"usage": map[string]int{
			"prompt_tokens": customerUsage.PromptTokens, "completion_tokens": customerUsage.CompletionTokens,
			"total_tokens": customerUsage.PromptTokens + customerUsage.CompletionTokens,
		},
		"_uniroute": unirouteData,
	}
	finalJSON, _ := json.Marshal(finalChunk)
	if !writeSSE("data: %s\n\n", finalJSON) {
		leaseTerminalReason = "downstream_stream_write_failed"
		recordTerminalDelivery(billing.DeliveryOutcomeInterrupted, "downstream_stream_write_failed")
		terminalBillingStatus := billingStatus
		if settledSuccessfully {
			terminalBillingStatus = "settled_partial_stream"
		}
		recordStreamTermination("downstream_stream_write_failed", terminalBillingStatus)
		return
	}
	if !writeSSE("data: [DONE]\n\n") {
		leaseTerminalReason = "downstream_stream_write_failed"
		recordTerminalDelivery(billing.DeliveryOutcomeInterrupted, "downstream_stream_write_failed")
		terminalBillingStatus := billingStatus
		if settledSuccessfully {
			terminalBillingStatus = "settled_partial_stream"
		}
		recordStreamTermination("downstream_stream_write_failed", terminalBillingStatus)
		return
	}
	recordTerminalDelivery(billing.DeliveryOutcomeCompleted, "")
	leaseTerminalState = billing.StreamLeaseStateCompleted
	leaseTerminalReason = "stream_completed"
}

// calculateCost computes the exact terminal estimate in micro-CNY.
func (relay *Relay) calculateCost(ctx context.Context, orgID int64, model string, promptTokens, completionTokens int) (int64, error) {
	costLog := logger.New(logger.Fields{OrgID: uint(orgID), Module: "billing"})
	cost, err := relay.billing.CalculateCostMicrosForOrg(ctx, orgID, model, promptTokens, completionTokens)
	if err != nil {
		costLog.Error("CalculateCostMicrosForOrg error for %s: %v", model, err)
		return 0, err
	}
	return cost, nil
}

// recordRequest logs the request to the call_logs table.
func (relay *Relay) recordRequest(ctx context.Context, requestID string, orgID, keyID, channelID int64, provider, channelName, model, requestedModel, clientIP string, promptTokens, completionTokens int, costMicros int64, statusCode, latencyMs, ttftMs int, isStream bool, usageSource string, estimatedTokens int, upstreamRequestID string) {
	usage, err := adapters.NewUsage(promptTokens, completionTokens, promptTokens+completionTokens, usageSource)
	if err != nil {
		logger.New(logger.Fields{RequestID: requestID, OrgID: uint(orgID), Module: "records"}).Error("Invalid request usage: %v", err)
		return
	}
	relay.recordRequestWithUsage(
		ctx,
		requestID,
		orgID,
		keyID,
		channelID,
		provider,
		channelName,
		model,
		requestedModel,
		clientIP,
		usage,
		costMicros,
		statusCode,
		latencyMs,
		ttftMs,
		isStream,
		estimatedTokens,
		upstreamRequestID,
		nil,
	)
}

func (relay *Relay) recordRequestWithUsage(ctx context.Context, requestID string, orgID, keyID, channelID int64, provider, channelName, model, requestedModel, clientIP string, usage adapters.Usage, costMicros int64, statusCode, latencyMs, ttftMs int, isStream bool, estimatedTokens int, upstreamRequestID string, settlement *billing.SettleResult) {
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
	usageSource := metering.NormalizeUsageSource(usage.Source)
	usageConfidence := metering.SourceConfidence(usageSource)
	upstreamRequestID = sanitizeUpstreamRequestID(upstreamRequestID)
	payloadCaptureMode := relay.currentPayloadCaptureMode()
	traceID := requestID
	if evidenceContext.TraceID != "" {
		traceID = evidenceContext.TraceID
	}
	pricingVersion := ""
	var effectiveInputPrice, effectiveCacheReadPrice, effectiveCacheCreationPrice, effectiveOutputPrice int64
	var cacheReadCost, cacheCreationCost, nonTokenCost int64
	meterUsage := slices.Clone(usage.MeterUsage)
	unitRates := []billing.UnitRate{}
	if settlement != nil {
		pricingVersion = settlement.PricingVersion
		effectiveInputPrice = settlement.EffectiveInputPriceMicrosPerMillion
		effectiveCacheReadPrice = settlement.EffectiveCacheReadPriceMicrosPerMillion
		effectiveCacheCreationPrice = settlement.EffectiveCacheCreationPriceMicrosPerMillion
		effectiveOutputPrice = settlement.EffectiveOutputPriceMicrosPerMillion
		cacheReadCost = settlement.CacheReadCostMicros
		cacheCreationCost = settlement.CacheCreationCostMicros
		nonTokenCost = settlement.NonTokenCostMicros
		meterUsage = slices.Clone(settlement.MeterUsage)
		unitRates = slices.Clone(settlement.EffectiveUnitRates)
	}
	sourceEventHash := hashCallLogEvidence(requestID, requestIdentity.ClientRequestID, requestIdentity.IdempotencyKeyHash,
		orgID, projectIDArg, keyID, ownerUserIDArg, keyType, model, requestedModel, provider, channelName, channelID,
		usage.PromptTokens, usage.UncachedPromptTokens, usage.CacheReadTokens, usage.CacheCreationTokens,
		usage.CompletionTokens, costMicros, statusCode, latencyMs, ttftMs, usageSource,
		string(usage.Authority), string(usage.Purpose), string(usage.Finality), usage.ContractVersion, usage.EstimateVersion,
		usage.CacheUsageSchema, usage.CacheUsageStatus, pricingVersion, unitRates, meterUsage, nonTokenCost, isStream,
		estimatedTokens, usageConfidence, upstreamRequestID, traceID, evidenceContext.SessionID,
		evidenceContext.WorkflowID, evidenceContext.AgentID, attemptID, actorType, stringFromNullable(actorRefArg),
		evidenceContext.OnBehalfOf, payloadCaptureMode)
	payload := callLogOutboxPayload{
		RequestID:                               requestID,
		ClientRequestID:                         requestIdentity.ClientRequestID,
		IdempotencyKeyHash:                      requestIdentity.IdempotencyKeyHash,
		OrgID:                                   orgID,
		ProjectID:                               int64Pointer(projectID),
		APIKeyID:                                keyID,
		OwnerUserID:                             ownerUserID,
		KeyType:                                 keyType,
		Model:                                   model,
		RequestedModel:                          requestedModel,
		Provider:                                provider,
		Channel:                                 channelName,
		ChannelID:                               channelID,
		PromptTokens:                            usage.PromptTokens,
		UncachedPromptTokens:                    usage.UncachedPromptTokens,
		CacheReadTokens:                         usage.CacheReadTokens,
		CacheCreationTokens:                     usage.CacheCreationTokens,
		CompletionTokens:                        usage.CompletionTokens,
		MoneySchema:                             outbox.MoneyVersion,
		Currency:                                "CNY",
		AmountScale:                             money.MicrosPerCNY,
		CostMicros:                              microsText(costMicros),
		StatusCode:                              statusCode,
		LatencyMS:                               latencyMs,
		TTFTMS:                                  ttftMs,
		UsageSource:                             usageSource,
		UsageAuthority:                          string(usage.Authority),
		UsagePurpose:                            string(usage.Purpose),
		UsageFinality:                           string(usage.Finality),
		UsageContractVersion:                    usage.ContractVersion,
		UsageEstimateVersion:                    usage.EstimateVersion,
		CacheUsageSchema:                        usage.CacheUsageSchema,
		CacheUsageStatus:                        usage.CacheUsageStatus,
		PricingVersion:                          pricingVersion,
		EffectiveInputPriceMicrosPerMillion:     microsText(effectiveInputPrice),
		EffectiveCacheReadPriceMicrosPerMillion: microsText(effectiveCacheReadPrice),
		EffectiveCacheCreatePriceMicrosPerMillion: microsText(effectiveCacheCreationPrice),
		EffectiveOutputPriceMicrosPerMillion:      microsText(effectiveOutputPrice),
		CacheReadCostMicros:                       microsText(cacheReadCost),
		CacheCreationCostMicros:                   microsText(cacheCreationCost),
		EffectiveUnitRates:                        unitRates,
		MeterUsage:                                meterUsage,
		NonTokenCostMicros:                        microsText(nonTokenCost),
		IsStream:                                  isStream,
		IPAddress:                                 clientIP,
		EstimatedTokens:                           estimatedTokens,
		UsageConfidence:                           usageConfidence,
		TraceID:                                   traceID,
		SessionID:                                 evidenceContext.SessionID,
		SchemaVersion:                             evidenceSchemaVersion,
		CaptureMode:                               evidenceCaptureModeNative,
		EvidenceRef:                               fmt.Sprintf("%s:call_logs:request_id:%s", evidenceTraceRefSourceType, requestID),
		UpstreamRequestID:                         upstreamRequestID,
		WorkflowID:                                evidenceContext.WorkflowID,
		AgentID:                                   evidenceContext.AgentID,
		AttemptID:                                 attemptID,
		ActorType:                                 actorType,
		ActorRef:                                  stringFromNullable(actorRefArg),
		OnBehalfOf:                                evidenceContext.OnBehalfOf,
		PayloadCaptureMode:                        payloadCaptureMode,
		SourceEventHash:                           sourceEventHash,
		OccurredAt:                                time.Now().UTC(),
	}
	payload, err := relay.prepareCallLogMaterialization(ctx, payload)
	if err != nil {
		reqLog.Error("Failed to prepare request execution evidence: %v", err)
		return
	}
	if relay.outbox != nil {
		if err := relay.enqueueCallLogOutbox(ctx, payload); err == nil {
			relay.recordTraceEvent(ctx, requestID, "logging.call_log.queued", "logging", "info", model, requestedModel, nil, statusCode, latencyMs, map[string]interface{}{
				"prompt_tokens":           usage.PromptTokens,
				"completion_tokens":       usage.CompletionTokens,
				"cost_micros":             microsText(costMicros),
				"currency":                "CNY",
				"amount_scale":            money.MicrosPerCNY,
				"usage_source":            usageSource,
				"outbox_version":          outbox.Version,
				"materialization_pending": true,
			})
			return
		} else {
			reqLog.Error("Failed to enqueue request log; falling back to synchronous insert: %v", err)
		}
	}
	if err := relay.materializeCallLog(ctx, outbox.EventCallLog, payload, ""); err != nil {
		reqLog.Error("Failed to record request log: %v", err)
		return
	}
	relay.recordTraceEvent(ctx, requestID, "logging.call_log.recorded", "logging", "info", model, requestedModel, nil, statusCode, latencyMs, map[string]interface{}{
		"prompt_tokens":              usage.PromptTokens,
		"uncached_prompt_tokens":     usage.UncachedPromptTokens,
		"cache_read_tokens":          usage.CacheReadTokens,
		"cache_creation_tokens":      usage.CacheCreationTokens,
		"completion_tokens":          usage.CompletionTokens,
		"total_tokens":               usage.PromptTokens + usage.CompletionTokens,
		"cost_micros":                microsText(costMicros),
		"currency":                   "CNY",
		"amount_scale":               money.MicrosPerCNY,
		"estimated_tokens":           estimatedTokens,
		"is_stream":                  isStream,
		"ttft_ms":                    ttftMs,
		"usage_source":               usageSource,
		"usage_authority":            usage.Authority,
		"usage_purpose":              usage.Purpose,
		"usage_finality":             usage.Finality,
		"usage_contract_version":     usage.ContractVersion,
		"usage_estimate_version":     usage.EstimateVersion,
		"usage_confidence":           usageConfidence,
		"cache_usage_schema":         usage.CacheUsageSchema,
		"cache_usage_status":         usage.CacheUsageStatus,
		"cache_usage_authoritative":  usage.HasAuthoritativeCacheUsage(),
		"pricing_version":            pricingVersion,
		"cache_read_cost_micros":     microsText(cacheReadCost),
		"cache_creation_cost_micros": microsText(cacheCreationCost),
		"meter_usage":                meterUsage,
		"effective_unit_rates":       unitRates,
		"non_token_cost_micros":      microsText(nonTokenCost),
	})
}

// estimateTokens provides a CJK-aware estimate of tokens needed for the request.
func estimateTokenReservation(messages []Message, maxTokens *int) (inputTokens, reservedCompletionTokens int) {
	inputTokens = estimateChatInputTokens(messages)
	reservedCompletionTokens = 1000
	if maxTokens != nil {
		reservedCompletionTokens = *maxTokens
	}
	return inputTokens, reservedCompletionTokens
}

func estimateChatInputTokens(messages []Message) int {
	inputTokens := 0
	for _, msg := range messages {
		inputTokens += msg.estimatedInputTokens() + 4 // overhead per message
	}
	inputTokens += 2 // reply priming
	return inputTokens
}

func estimateTokens(messages []Message, maxTokens *int) int {
	inputTokens, reservedCompletionTokens := estimateTokenReservation(messages, maxTokens)
	return inputTokens + reservedCompletionTokens
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
	ctx := contextWithTelemetryOperationKind(
		relay.contextWithTrustedEvidence(r.Context(), r.Header),
		telemetryexport.OperationEmbedding,
	)
	requestStart := time.Now()
	requestID := middleware.GetRequestIDFromContext(ctx)
	w.Header().Set("X-Request-ID", requestID)

	var req EmbeddingsRequest
	body, releaseRequestPayload, err := relay.requestPayload(r, payloadpolicy.ProfileEmbeddingsRequest)
	if err != nil {
		if errors.Is(err, payloadpolicy.ErrPayloadTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "validation_error.payload_too_large", "Request payload exceeds the embeddings safety limit")
		} else if errors.Is(err, payloadpolicy.ErrBudgetExceeded) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.payload_capacity_exceeded", "Payload capacity is temporarily unavailable")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", "Failed to read request body")
		}
		return
	}
	defer releaseRequestPayload()
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
	// Keep the authorization boundary ahead of guardrail evaluation for the
	// same reason as Chat: denied callers must have no policy or routing side
	// effects beyond the explicit authorization-denial evidence.
	callerAPIKey := middleware.GetAPIKeyFromContext(ctx)
	if !modelAllowedForAPIKey(callerAPIKey, req.Model, requestedModel) {
		relay.rejectModelNotAllowed(w, ctx, requestID, req.Model, requestedModel, requestStart)
		return
	}
	if !relay.requireModelExecutionRuntime(w) {
		return
	}
	pinnedGuardrail := relay.pinGuardrail(ctx, req.Model, "embeddings")
	if pinnedGuardrail != nil {
		guardrailResult := pinnedGuardrail.Evaluate(ctx, guardrail.StageInput, embeddingsGuardrailTexts(req.Input))
		relay.recordGuardrailResult(ctx, requestID, req.Model, requestedModel, normalizedGuardrailStage(guardrail.StageInput), guardrailResult, nil, 0, "")
		if guardrailResult.Decision == guardrail.DecisionBlock {
			writeInputGuardrailBlock(w, guardrailBlockedByUnavailable(guardrailResult))
			return
		}
		body, err = applyEmbeddingsGuardrailTransform(body, &req, guardrailResult)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "guardrail_error.transform_failed", "Failed to apply content policy safely")
			return
		}
		if guardrailResult.Transformed {
			if r, err = payloadpolicy.ReplaceRequestBody(r, body); err != nil {
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.payload_capacity_exceeded", "Payload capacity is temporarily unavailable")
				return
			}
		}
		ctx = contextWithGuardrailPin(ctx, pinnedGuardrail)
	}
	orgID := middleware.GetOrgIDFromContext(ctx)
	keyID := middleware.GetKeyIDFromContext(ctx)
	reqLog := logger.New(logger.Fields{RequestID: requestID, OrgID: uint(orgID), Module: "embeddings"})

	admission, admissionErr := estimateEmbeddingsAdmission(body)
	if admissionErr != nil {
		relay.recordTraceEvent(ctx, requestID, "gateway.admission_rejected", "admission", "warning", req.Model, requestedModel, nil, http.StatusBadRequest, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code": "validation_error.unsupported_semantics", "pre_freeze": true,
		})
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.unsupported_semantics", "Embeddings input must be text, text arrays, token ID arrays, or token ID matrixes")
		return
	}
	inputTokens := admission.InputTokens
	estimateVersion := admission.Version
	responseExpectation, expectationErr := embeddingsResponseExpectationFromRequest(body)
	if expectationErr != nil {
		relay.recordTraceEvent(ctx, requestID, "gateway.admission_rejected", "admission", "warning", req.Model, requestedModel, nil, http.StatusBadRequest, elapsedMilliseconds(requestStart), map[string]interface{}{
			"error_code": "validation_error.unsupported_semantics", "pre_freeze": true,
		})
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.unsupported_semantics", "Embeddings encoding, dimensions, or input count is unsupported")
		return
	}
	const providerTimeout = 120 * time.Second
	ctx = agentcap.ContextWithInvocationLeaseTTL(ctx, providerTimeout)
	ctx = workloadadmission.ContextWithClass(ctx, workloadadmission.ClassEmbedding)
	ctx, limitPolicyCollector := contextWithLimitPolicyCollector(ctx)
	zeroOutputTokens := 0
	executionBatch, err := relay.prepareModelExecutionRoutes(
		ctx, modeloperation.IDEmbeddings, adapters.EndpointEmbeddings,
		req.Model, requestedModel, requestID,
		modelExecutionRequestLimits{EstimatedInputTokens: inputTokens, ExplicitOutputTokens: &zeroOutputTokens, EstimateVersion: admission.Version},
		body, requestStart,
	)
	if err != nil {
		if code, message, limitError := modelExecutionLimitErrorDetails(err); limitError {
			relay.recordTraceEvent(ctx, requestID, "routing.request_limit_rejected", "routing", "warning", req.Model, requestedModel, nil, http.StatusBadRequest, elapsedMilliseconds(requestStart), map[string]interface{}{
				"error_code": code, "pre_freeze": true,
			})
			writeError(w, http.StatusBadRequest, "invalid_request_error", code, message)
			return
		}
		relay.recordTraceEvent(ctx, requestID, "routing.no_route", "routing", "error", req.Model, requestedModel, nil, http.StatusServiceUnavailable, elapsedMilliseconds(requestStart), map[string]interface{}{
			"route_count": 0, "pre_freeze": true,
		})
		relay.recordRequest(ctx, requestID, orgID, keyID, 0, "", "", req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, http.StatusServiceUnavailable, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceNoRoute, inputTokens, "")
		writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", fmt.Sprintf("No available channel for model: %s", req.Model))
		return
	}
	inputTokens = executionBatch.limits.EstimatedInputTokens
	estimateVersion = executionBatch.limits.EstimateVersion
	relay.recordTraceEvent(ctx, requestID, "gateway.request.accepted", "gateway", "info", req.Model, requestedModel, nil, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
		"estimated_tokens": inputTokens, "stream": false,
		"admission_estimate_version": estimateVersion,
	})
	relay.recordPayloadPolicyDecision(ctx, requestID, req.Model, requestedModel, elapsedMilliseconds(requestStart))

	freezeResult, err := relay.billing.Freeze(ctx, billing.FreezeRequest{
		RequestID:                requestID,
		OrgID:                    orgID,
		ProjectID:                middleware.GetProjectIDFromContext(ctx),
		Model:                    req.Model,
		EstimatedTokens:          inputTokens,
		EstimatedInputTokens:     inputTokens,
		ReservedCompletionTokens: 0,
		ExecutionSnapshotHash:    executionBatch.plan.SnapshotHash,
		SafetyFactor:             relay.freezeSafetyFactor,
		MinFreezeCents:           relay.minFreezeCents,
	})
	relay.recordLimitPolicyAdmission(ctx, w, limitPolicyCollector, requestID, req.Model, requestedModel, elapsedMilliseconds(requestStart))
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
		if writeAgentCapabilityRuntimeRejection(w, freezeResult.ErrorCode) {
			return
		}
		if writeLimitPolicyRuntimeRejection(w, freezeResult.ErrorCode) {
			return
		}
		if freezeResult.ErrorCode == "insufficient_balance" {
			writeError(w, http.StatusPaymentRequired, "billing_error", "billing_error.insufficient_balance", "Insufficient balance")
		} else {
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", freezeResult.ErrorCode)
		}
		return
	}
	middleware.RecordBillingFreeze("success")
	relay.recordTraceEvent(ctx, requestID, "billing.freeze.succeeded", "billing", "info", req.Model, requestedModel, nil, 0, elapsedMilliseconds(requestStart), map[string]interface{}{
		"freeze_amount_micros": microsText(freezeResult.AmountMicros),
		"currency":             "CNY", "amount_scale": money.MicrosPerCNY,
	})

	// Every candidate was planned, decrypted and built before Freeze.
	routes := executionBatch.routes
	client := relay.providerClient(providerTimeout)
	routingPolicy := relay.modelExecution.policy
	providerAttempts := 0
	var lastErr error
	var lastStatusCode int
	var lastUpstreamRequestID string
	var lastRoute *RouteResult
	lastAttemptCtx := ctx

	for routeIndex := range routes {
		preparedRoute := &routes[routeIndex]
		route := &preparedRoute.compat
		attemptCtx := contextWithRouteAttempt(ctx, routeIndex+1, len(routes))
		lastAttemptCtx = attemptCtx
		lastStatusCode = 0
		lastUpstreamRequestID = ""

		adapter := preparedRoute.adapter
		upstreamReq := preparedRoute.upstreamRequest
		var status modelexecution.Status
		attemptCtx, status, err = relay.authorizePreparedModelRoute(
			attemptCtx, executionBatch, preparedRoute, routeIndex+1, len(routes), adapters.EndpointEmbeddings,
		)
		if err != nil {
			relay.billing.Cancel(context.WithoutCancel(attemptCtx), requestID, orgID)
			reqLog.Error("Embeddings execution authorization failed: %v", err)
			relay.recordTraceEvent(attemptCtx, requestID, "routing.dispatch_authority_failed", "routing", "error", req.Model, requestedModel, route, 0, elapsedMilliseconds(requestStart), map[string]interface{}{"status": status})
			writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing authority is temporarily unavailable")
			return
		}
		if status != modelexecution.Applied {
			lastErr = fmt.Errorf("route dispatch authority: %s", status)
			relay.recordTraceEvent(attemptCtx, requestID, "routing.dispatch_authority_changed", "routing", "warning", req.Model, requestedModel, route, 0, elapsedMilliseconds(requestStart), map[string]interface{}{"status": status})
			continue
		}
		dispatchCtx, stopProbeLease := relay.maintainExecutionLease(attemptCtx, reqLog)
		upstreamReq = upstreamReq.Clone(dispatchCtx)
		lastRoute = route
		relay.recordTraceEvent(attemptCtx, requestID, "routing.route_selected", "routing", "info", req.Model, requestedModel, route, 0, elapsedMilliseconds(requestStart), routingPolicyTraceMetadata(routingPolicy, route, len(routes), routeIndex+1))
		lastAttemptCtx = attemptCtx

		providerAttempts++
		upstreamStart := time.Now()
		resp, err := client.Do(upstreamReq)
		upstreamDuration := time.Since(upstreamStart)
		middleware.RecordUpstreamLatency(route.Channel.Provider, req.Model, upstreamDuration)
		if err != nil {
			stopProbeLease()
			classified := adapters.ClassifyTransportError(attemptCtx, err)
			healthAuthorityFailed := false
			completionErr := relay.completeProviderAttemptWithRetry(providerAttemptCompletion(
				attemptCtx, requestID, orgID, billing.ProviderAttemptOutcomeUnknown, false, 0,
				string(classified.FailureKind), "",
			), reqLog)
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Upstream embeddings transport failed: kind=%s retryable=%t", classified.FailureKind, classified.Retryable)
			if classified.Retryable {
				if healthErr := relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthProviderFailure); healthErr != nil {
					reqLog.Error("Failed to record embeddings transport health: %v", healthErr)
					healthAuthorityFailed = relay.recordExecutionHealthFailure(attemptCtx, requestID, req.Model, requestedModel, route, 0, int(upstreamDuration.Milliseconds()), executionHealthProviderFailure, healthErr)
				}
				relay.emitProviderChannelFailure(attemptCtx, *route, requestID, requestedModel, 0, string(classified.FailureKind), providerFailureEventMessage(0, classified.FailureKind))
			}
			ledgerStatus := "recorded"
			if completionErr != nil {
				ledgerStatus = "persist_failed"
				reqLog.Error("Ambiguous embeddings attempt could not be confirmed after bounded retries: %v", completionErr)
			}
			relay.recordTraceEvent(attemptCtx, requestID, "provider.request_failed", "provider", "warning", req.Model, requestedModel, route, 0, int(upstreamDuration.Milliseconds()), map[string]interface{}{
				"failure_kind":               string(classified.FailureKind),
				"availability_retryable":     classified.Retryable,
				"financial_retry_authorized": false,
				"retry_decision":             "hold_financial_uncertainty",
				"outcome_certainty":          "unknown",
				"attempt_ledger_status":      ledgerStatus,
			})
			cleanupCtx := context.WithoutCancel(attemptCtx)
			relay.recordRequest(cleanupCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, 0, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceUpstreamFailed, inputTokens, "")
			if attemptCtx.Err() == nil {
				if healthAuthorityFailed {
					w.Header().Set("Retry-After", "1")
					writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
				} else {
					writeNonRetryableProviderError(w, 0, classified.FailureKind)
				}
			}
			return
		}
		upstreamRequestID := upstreamRequestIDFromHeader(resp.Header)
		if resp.StatusCode != http.StatusOK {
			stopProbeLease()
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
			classified := adapter.ClassifyHTTPStatus(resp.StatusCode, errBody)
			willRetry := classified.Retryable && providerAttempts <= routingPolicy.MaxRetryCount && routeIndex+1 < len(routes)
			if err := relay.completeProviderAttemptWithRetry(providerAttemptCompletion(
				attemptCtx, requestID, orgID, billing.ProviderAttemptOutcomeRejected,
				classified.Retryable, resp.StatusCode, string(classified.FailureKind), upstreamRequestID,
			), reqLog); err != nil {
				resp.Body.Close()
				relay.recordTraceEvent(attemptCtx, requestID, "billing.provider_attempt.complete_failed", "billing", "critical", req.Model, requestedModel, route, resp.StatusCode, int(upstreamDuration.Milliseconds()), map[string]interface{}{
					"error_code": "provider_attempt_complete_failed",
				})
				writeError(w, http.StatusInternalServerError, "server_error", "billing_error.provider_attempt_failed", "Failed to persist provider response authority")
				return
			}
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Upstream embeddings rejected response: status=%d kind=%s retryable=%t", resp.StatusCode, classified.FailureKind, classified.Retryable)
			var healthErr error
			switch classified.FailureKind {
			case adapters.FailureChannel:
				healthErr = relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthProviderFailure)
				relay.emitProviderChannelFailure(attemptCtx, *route, requestID, requestedModel, resp.StatusCode, string(classified.FailureKind), providerFailureEventMessage(resp.StatusCode, classified.FailureKind))
			case adapters.FailureAuth:
				healthErr = relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthCredentialAuthFailure)
				relay.emitProviderChannelFailure(attemptCtx, *route, requestID, requestedModel, resp.StatusCode, string(classified.FailureKind), providerFailureEventMessage(resp.StatusCode, classified.FailureKind))
			case adapters.FailureRateLimit:
				healthErr = relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthCredentialRateLimited)
				relay.emitProviderChannelFailure(attemptCtx, *route, requestID, requestedModel, resp.StatusCode, string(classified.FailureKind), providerFailureEventMessage(resp.StatusCode, classified.FailureKind))
			}
			if healthErr != nil {
				reqLog.Error("Failed to record embeddings provider rejection health: %v", healthErr)
				if relay.recordExecutionHealthFailure(attemptCtx, requestID, req.Model, requestedModel, route, resp.StatusCode, int(upstreamDuration.Milliseconds()), healthActionForFailureKind(classified.FailureKind), healthErr) {
					resp.Body.Close()
					relay.billing.Cancel(context.WithoutCancel(attemptCtx), requestID, orgID)
					w.Header().Set("Retry-After", "1")
					writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
					return
				}
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
		if err := relay.completeProviderAttemptWithRetry(providerAttemptCompletion(
			attemptCtx, requestID, orgID, billing.ProviderAttemptOutcomeResponseAccepted,
			false, resp.StatusCode, "", upstreamRequestID,
		), reqLog); err != nil {
			stopProbeLease()
			resp.Body.Close()
			relay.recordTraceEvent(attemptCtx, requestID, "billing.provider_attempt.complete_failed", "billing", "critical", req.Model, requestedModel, route, resp.StatusCode, int(upstreamDuration.Milliseconds()), map[string]interface{}{
				"error_code": "provider_attempt_complete_failed",
			})
			writeError(w, http.StatusInternalServerError, "server_error", "billing_error.provider_attempt_failed", "Failed to persist provider response authority")
			return
		}

		respBody, releaseResponsePayload, readErr := relay.responsePayload(attemptCtx, resp.Body)
		resp.Body.Close()
		if readErr != nil {
			status, errorType, errorCode, message, failureKind := payloadReadFailure(readErr)
			stopProbeLease()
			healthAuthorityFailed := false
			if !errors.Is(readErr, payloadpolicy.ErrPayloadTooLarge) && !errors.Is(readErr, payloadpolicy.ErrBudgetExceeded) {
				if healthErr := relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthProviderFailure); healthErr != nil {
					reqLog.Error("Failed to record unusable embeddings response health: %v", healthErr)
					healthAuthorityFailed = relay.recordExecutionHealthFailure(attemptCtx, requestID, req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), executionHealthProviderFailure, healthErr)
				}
			}
			if ledgerErr := relay.completeProviderAttemptWithRetry(providerAttemptCompletion(
				attemptCtx, requestID, orgID, billing.ProviderAttemptOutcomeResponseUnusable,
				false, resp.StatusCode, failureKind, upstreamRequestID,
			), reqLog); ledgerErr != nil {
				reqLog.Error("Failed to retain unusable embeddings response authority: %v", ledgerErr)
			}
			relay.recordTraceEvent(attemptCtx, requestID, "provider.response_parse_failed", "provider", "error", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": failureKind,
			}, upstreamRequestID)
			if healthAuthorityFailed {
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
				return
			}
			if errors.Is(readErr, payloadpolicy.ErrBudgetExceeded) {
				w.Header().Set("Retry-After", "1")
			}
			writeError(w, status, errorType, errorCode, message)
			return
		}
		defer releaseResponsePayload()

		normalized, err := adapter.ParseResponse(attemptCtx, adapters.ResponseInput{
			Endpoint:          adapters.EndpointEmbeddings,
			StatusCode:        resp.StatusCode,
			Header:            resp.Header,
			Body:              respBody,
			Channel:           route.Channel,
			RequestedModel:    requestedModel,
			UpstreamModel:     route.MappedModel,
			CacheUsageProfile: route.CacheUsageProfile,
		})
		if err != nil {
			reqLog.Error("Failed to parse adapter response")
			stopProbeLease()
			healthAuthorityFailed := false
			if healthErr := relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthProviderFailure); healthErr != nil {
				reqLog.Error("Failed to record embeddings parse health: %v", healthErr)
				healthAuthorityFailed = relay.recordExecutionHealthFailure(attemptCtx, requestID, req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), executionHealthProviderFailure, healthErr)
			}
			if ledgerErr := relay.completeProviderAttemptWithRetry(providerAttemptCompletion(
				attemptCtx, requestID, orgID, billing.ProviderAttemptOutcomeResponseUnusable,
				false, resp.StatusCode, "response_parse", upstreamRequestID,
			), reqLog); ledgerErr != nil {
				reqLog.Error("Failed to retain unusable embeddings response authority: %v", ledgerErr)
			}
			relay.recordTraceEvent(attemptCtx, requestID, "provider.response_parse_failed", "provider", "error", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "response_parse",
			}, upstreamRequestID)
			if healthAuthorityFailed {
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
				return
			}
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to parse upstream response")
			return
		}
		if normalized.Usage.IsMissing() {
			normalized.Usage, err = adapters.NewConservativeEstimate(inputTokens, 0)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "server_error", "billing_error.settle_failed", "Unable to establish bounded usage authority")
				return
			}
			relay.recordTraceEvent(attemptCtx, requestID, "usage.provider_missing", "billing", "warning", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
				"usage_authority":        normalized.Usage.Authority,
				"usage_finality":         normalized.Usage.Finality,
				"usage_estimate_version": normalized.Usage.EstimateVersion,
			}, upstreamRequestID)
		} else {
			normalized.Usage, err = normalized.Usage.ForCustomerCharge()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "server_error", "billing_error.settle_failed", "Unable to establish usage authority")
				return
			}
		}

		// Validate the normalized response before charging or adding the
		// UniRouter extension. A malformed 200 response must not panic or settle.
		var responseMap map[string]interface{}
		if err := decodeJSONUseNumber(normalized.Body, &responseMap); err != nil || responseMap == nil {
			stopProbeLease()
			healthAuthorityFailed := false
			if healthErr := relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthProviderFailure); healthErr != nil {
				reqLog.Error("Failed to record malformed embeddings response health: %v", healthErr)
				healthAuthorityFailed = relay.recordExecutionHealthFailure(attemptCtx, requestID, req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), executionHealthProviderFailure, healthErr)
			}
			if ledgerErr := relay.completeProviderAttemptWithRetry(providerAttemptCompletion(
				attemptCtx, requestID, orgID, billing.ProviderAttemptOutcomeResponseUnusable,
				false, resp.StatusCode, "response_parse", upstreamRequestID,
			), reqLog); ledgerErr != nil {
				reqLog.Error("Failed to retain unusable embeddings response authority: %v", ledgerErr)
			}
			relay.recordTraceEvent(attemptCtx, requestID, "provider.response_parse_failed", "provider", "error", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "response_parse",
			}, upstreamRequestID)
			if healthAuthorityFailed {
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
				return
			}
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to parse upstream response")
			return
		}
		holdUndeliveredOutput := func(status int, reason string) error {
			holdReq := settleRequestFromUsage(requestID, attemptIDFromContext(attemptCtx), orgID, normalized.Usage)
			holdReq.UsagePurpose = metering.UsagePurposeProviderEvidence
			if err := relay.holdNonStreamOutputWithRetry(attemptCtx, holdReq, reason, reqLog); err != nil {
				return err
			}
			evidenceUsage := normalized.Usage
			evidenceUsage.Purpose = metering.UsagePurposeProviderEvidence
			evidenceCtx, cancelEvidence := context.WithTimeout(context.WithoutCancel(attemptCtx), 10*time.Second)
			defer cancelEvidence()
			relay.recordRequestWithUsage(evidenceCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, req.Model, requestedModel, r.RemoteAddr,
				evidenceUsage, 0, status, elapsedMilliseconds(requestStart), 0, false, inputTokens, upstreamRequestID, nil)
			return nil
		}
		deliveryInspection := inspectEmbeddingsResponseEnvelope(responseMap, responseExpectation)
		if !deliveryInspection.complete() {
			stopProbeLease()
			healthAuthorityFailed := false
			if healthErr := relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthProviderFailure); healthErr != nil {
				reqLog.Error("Failed to record unsupported embeddings response health: %v", healthErr)
				healthAuthorityFailed = relay.recordExecutionHealthFailure(attemptCtx, requestID, req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), executionHealthProviderFailure, healthErr)
			}
			if holdErr := holdUndeliveredOutput(http.StatusBadGateway, billing.OutputHoldReasonProviderResponseUnusable); holdErr != nil {
				reqLog.Error("Failed to retain unsupported embeddings response usage: %v", holdErr)
				writeError(w, http.StatusInternalServerError, "server_error", "billing_error.provider_attempt_failed", "Failed to retain provider cost authority")
				return
			}
			relay.recordTraceEvent(attemptCtx, requestID, "provider.response_unusable", "provider", "warning", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
				"failure_kind": "unsupported_response_shape", "delivery_profile": embeddingsDeliveryWhitelistVersion,
			}, upstreamRequestID)
			if healthAuthorityFailed {
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
				return
			}
			writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_error", "Upstream response could not be delivered safely")
			return
		}
		stopProbeLease()
		if healthErr := relay.applyRouteHealth(context.WithoutCancel(attemptCtx), route, executionHealthSuccess); healthErr != nil {
			reqLog.Error("Failed to record embeddings success health: %v", healthErr)
			if relay.recordExecutionHealthFailure(attemptCtx, requestID, req.Model, requestedModel, route, resp.StatusCode, int(upstreamDuration.Milliseconds()), executionHealthSuccess, healthErr) {
				if holdErr := holdUndeliveredOutput(http.StatusServiceUnavailable, billing.OutputHoldReasonProviderAttemptUnresolved); holdErr != nil {
					reqLog.Error("Failed to retain embeddings provider usage after routing health failure: %v", holdErr)
					writeError(w, http.StatusInternalServerError, "server_error", "billing_error.provider_attempt_failed", "Failed to retain provider cost authority")
					return
				}
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing health authority is temporarily unavailable")
				return
			}
		}
		relay.recordTraceEvent(attemptCtx, requestID, "provider.response_accepted", "provider", "info", req.Model, requestedModel, route, resp.StatusCode, int(upstreamDuration.Milliseconds()), map[string]interface{}{}, upstreamRequestID)
		basePayload, responseMaxBytes, err := relay.marshalNonStreamResponseBase(responseMap)
		if err != nil {
			if errors.Is(err, payloadpolicy.ErrPayloadTooLarge) {
				holdReq := settleRequestFromUsage(requestID, attemptIDFromContext(attemptCtx), orgID, normalized.Usage)
				holdReq.UsagePurpose = metering.UsagePurposeProviderEvidence
				holdErr := relay.holdNonStreamOutputWithRetry(attemptCtx, holdReq, billing.OutputHoldReasonResponseTooLarge, reqLog)
				if holdErr != nil {
					reqLog.Error("Failed to retain oversized embeddings response usage: %v", holdErr)
					writeError(w, http.StatusInternalServerError, "server_error", "billing_error.provider_attempt_failed", "Failed to retain provider cost authority")
					return
				}
				evidenceUsage := normalized.Usage
				evidenceUsage.Purpose = metering.UsagePurposeProviderEvidence
				evidenceCtx, cancelEvidence := context.WithTimeout(context.WithoutCancel(attemptCtx), 10*time.Second)
				relay.recordRequestWithUsage(evidenceCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, req.Model, requestedModel, r.RemoteAddr,
					evidenceUsage, 0, http.StatusBadGateway, elapsedMilliseconds(requestStart), 0, false, inputTokens, upstreamRequestID, nil)
				cancelEvidence()
				relay.recordTraceEvent(attemptCtx, requestID, "provider.response_unusable", "provider", "warning", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
					"failure_kind": "response_too_large",
				}, upstreamRequestID)
				writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_response_too_large", "Upstream response exceeds the non-stream safety limit")
				return
			}
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to encode response")
			return
		}

		settleReq := settleRequestFromUsage(requestID, attemptIDFromContext(attemptCtx), orgID, normalized.Usage)
		deliveryPrepared, err := relay.prepareNonStreamDeliveryWithTimeout(attemptCtx, settleReq)
		if err != nil || deliveryPrepared == nil || !deliveryPrepared.Funded {
			reqLog.Error("Billing delivery preparation failed; embeddings response withheld: %v", err)
			relay.recordTraceEvent(attemptCtx, requestID, "billing.delivery_prepare.failed", "billing", "error", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), map[string]interface{}{
				"error_code": "delivery_prepare_failed",
			})
			writeError(w, http.StatusInternalServerError, "server_error", "billing_error.settle_failed", "Failed to secure billing before delivery")
			return
		}
		preview := &deliveryPrepared.Preview
		previewActual, previewCharged := settledCosts(preview)
		attempt := routeAttemptFromContext(attemptCtx)
		responseExtension := map[string]interface{}{
			"request_id":            requestID,
			"cost":                  preparedDeliveryCostExtension(normalized.Usage, preview, previewActual, previewCharged),
			"provider":              route.Channel.Provider,
			"channel":               route.Channel.Name,
			"cache_hit":             normalized.Usage.HasAuthoritativeCacheHit(),
			"provider_prompt_cache": providerPromptCacheExtension(normalized.Usage, preview),
			"fallback":              attempt.Sequence > 1,
			"attempt_count":         attempt.Sequence,
			"route_count":           attempt.Total,
			"attempt_id":            attemptIDFromContext(attemptCtx),
			"billing_status":        "delivery_prepared",
		}
		payload, err := appendNonStreamResponseExtension(basePayload, responseExtension, responseMaxBytes)
		if err != nil {
			deliveryCtx, deliveryCancel := context.WithTimeout(context.WithoutCancel(attemptCtx), 10*time.Second)
			recordErr := relay.billing.RecordDelivery(deliveryCtx, billing.DeliveryEvent{
				RequestID: requestID, AttemptID: attemptIDFromContext(attemptCtx), OrgID: orgID,
				Mode: billing.DeliveryModeNonStream, Outcome: billing.DeliveryOutcomeWriteFailed,
				HTTPStatus: resp.StatusCode, ErrorCode: "response_encode_failed",
			})
			deliveryCancel()
			if recordErr != nil {
				reqLog.Error("Failed to retain pre-delivery embeddings encoding failure: %v", recordErr)
			}
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to encode response")
			return
		}
		deliveryEvent := billing.DeliveryEvent{
			RequestID: requestID, AttemptID: attemptIDFromContext(attemptCtx), OrgID: orgID,
			Mode: billing.DeliveryModeNonStream, Outcome: billing.DeliveryOutcomeWriteStarted,
			HTTPStatus: resp.StatusCode,
		}
		deliveryCtx, deliveryCancel := context.WithTimeout(context.WithoutCancel(attemptCtx), 10*time.Second)
		if err := relay.billing.RecordDelivery(deliveryCtx, deliveryEvent); err != nil {
			deliveryCancel()
			reqLog.Error("Failed to persist embeddings delivery start; response withheld: %v", err)
			writeError(w, http.StatusInternalServerError, "server_error", "billing_error.settle_failed", "Failed to establish delivery receipt")
			return
		}
		deliveryCancel()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		written, writeErr := w.Write(payload)
		if writeErr != nil || written != len(payload) {
			deliveryEvent.Outcome = billing.DeliveryOutcomeWriteFailed
			deliveryEvent.DeliveredBytes = int64(written)
			deliveryEvent.ErrorCode = "downstream_write_failed"
			failureCtx, failureCancel := context.WithTimeout(context.WithoutCancel(attemptCtx), 10*time.Second)
			if err := relay.billing.RecordDelivery(failureCtx, deliveryEvent); err != nil {
				reqLog.Error("Failed to persist embeddings write failure: %v", err)
			}
			failureCancel()
			return
		}
		deliveryEvent.Outcome = billing.DeliveryOutcomeCompleted
		deliveryEvent.DeliveredBytes = int64(written)
		deliveryEvent.DeliveredChunks = 1
		completedCtx, completedCancel := context.WithTimeout(context.WithoutCancel(attemptCtx), 10*time.Second)
		if err := relay.billing.RecordDelivery(completedCtx, deliveryEvent); err != nil {
			completedCancel()
			reqLog.Error("Failed to persist completed embeddings delivery; settlement retained: %v", err)
			return
		}
		completedCancel()

		settleOutcome, settleErr := relay.settlePendingBillingWithRetry(settleReq, reqLog)
		if settleErr != nil {
			reqLog.Error("Billing settle failed after embeddings delivery; recovery retained: %v", settleErr)
			relay.recordRequestWithUsage(attemptCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, req.Model, requestedModel, r.RemoteAddr,
				normalized.Usage, 0, resp.StatusCode, elapsedMilliseconds(requestStart), 0, false, inputTokens, upstreamRequestID, nil)
			return
		}
		actualCost, chargedCost := settledCosts(settleOutcome.Result)
		traceMetadata := settlementTraceMetadata(normalized.Usage, settleOutcome.Result, actualCost, chargedCost)
		relay.recordTraceEvent(attemptCtx, requestID, "billing.settle.succeeded", "billing", "info", req.Model, requestedModel, route, resp.StatusCode, elapsedMilliseconds(requestStart), traceMetadata)
		middleware.RecordTokenUsage("prompt", req.Model, normalized.Usage.PromptTokens)
		relay.recordRequestWithUsage(attemptCtx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, req.Model, requestedModel, r.RemoteAddr,
			normalized.Usage, chargedCost, resp.StatusCode, elapsedMilliseconds(requestStart), 0, false, inputTokens, upstreamRequestID, settleOutcome.Result)
		relay.checkProjectBudgetAlert(attemptCtx)
		return
	}

	relay.billing.Cancel(ctx, requestID, orgID)
	if providerAttempts == 0 {
		relay.recordTraceEvent(ctx, requestID, "routing.no_dispatch", "routing", "warning", req.Model, requestedModel, nil, http.StatusServiceUnavailable, elapsedMilliseconds(requestStart), map[string]interface{}{
			"route_count": len(routes),
			"error_code":  "routing_candidate_unavailable",
		})
		relay.recordRequest(ctx, requestID, orgID, keyID, 0, "", "", req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, http.StatusServiceUnavailable, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceNoRoute, inputTokens, "")
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", "Routing candidates are temporarily unavailable")
		return
	}
	if lastErr != nil {
		reqLog.Error("All upstream embeddings channels failed: %v", lastErr)
	}
	if lastRoute != nil {
		relay.recordRequest(lastAttemptCtx, requestID, orgID, keyID, lastRoute.Channel.ID, lastRoute.Channel.Provider, lastRoute.Channel.Name, req.Model, requestedModel, r.RemoteAddr, 0, 0, 0, lastStatusCode, elapsedMilliseconds(requestStart), 0, false, metering.UsageSourceUpstreamFailed, inputTokens, lastUpstreamRequestID)
	}
	writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_error", "All upstream channels failed")
}

// HandleModels handles GET /v1/models — returns available models for the caller.
// It intersects the API key's allowed_models (or model_prices fallback) with
// the same lifecycle and project model-set rules used for call admission.
func (relay *Relay) HandleModels(w http.ResponseWriter, r *http.Request) {
	orgID := middleware.GetOrgIDFromContext(r.Context())
	keyID := middleware.GetKeyIDFromContext(r.Context())
	if apiKey := middleware.GetAPIKeyFromContext(r.Context()); apiKey != nil {
		if apiKey.ModelScopeDenied {
			relay.writeAuthorizedModelList(w, r, nil)
			return
		}
		if modelListContainsWildcard(apiKey.AllowedModels) {
			relay.handleModelsFallback(w, r)
			return
		}
		if apiKey.AllowedModels != "" {
			relay.writeAllowedModelList(w, r, apiKey.AllowedModels)
			return
		}
		relay.handleModelsFallback(w, r)
		return
	}

	// Production authentication always supplies CtxAPIKey. Keep this lookup only
	// for direct handler callers, and fail closed if its authority is unavailable.
	var allowedModels string
	err := relay.db.QueryRow(r.Context(),
		`SELECT COALESCE(allowed_models, '') FROM api_keys WHERE id = $1 AND org_id = $2 AND status = 'active'`,
		keyID, orgID).Scan(&allowedModels)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to resolve API key models")
		return
	}
	if allowedModels != "" {
		if modelListContainsWildcard(allowedModels) {
			relay.handleModelsFallback(w, r)
			return
		}
		relay.writeAllowedModelList(w, r, allowedModels)
		return
	}

	// Fallback: return all models from model_prices
	relay.handleModelsFallback(w, r)
}

func modelListContainsWildcard(allowedModels string) bool {
	return modellist.Contains(allowedModels, "*")
}

func (relay *Relay) writeAllowedModelList(w http.ResponseWriter, r *http.Request, allowedModels string) {
	relay.writeAuthorizedModelList(w, r, modellist.Parse(allowedModels))
}

func (relay *Relay) handleModelsFallback(w http.ResponseWriter, r *http.Request) {
	rows, err := relay.db.Query(r.Context(),
		`SELECT model_name FROM model_prices ORDER BY model_name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to query models")
		return
	}
	defer rows.Close()

	candidates := make([]string, 0)
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to query models")
			return
		}
		candidates = append(candidates, model)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to query models")
		return
	}
	relay.writeAuthorizedModelList(w, r, candidates)
}

func (relay *Relay) writeAuthorizedModelList(w http.ResponseWriter, r *http.Request, candidates []string) {
	orgID := middleware.GetOrgIDFromContext(r.Context())
	projectID := middleware.GetProjectIDFromContext(r.Context())
	if apiKey := middleware.GetAPIKeyFromContext(r.Context()); apiKey != nil {
		orgID = apiKey.OrgID
		projectID = 0
		if apiKey.ProjectID != nil {
			projectID = *apiKey.ProjectID
		}
	}
	allowedIdentities, err := modelgov.ResolveCallableModelIdentities(r.Context(), relay.db, orgID, projectID, candidates)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to resolve authorized models")
		return
	}
	if relay.modelAuthorization != nil {
		apiKey := middleware.GetAPIKeyFromContext(r.Context())
		input := modelauthz.Input{OrgID: orgID, ProjectID: projectID, Endpoint: "/v1/models"}
		if apiKey != nil {
			input.APIKeyID = apiKey.ID
			if apiKey.AccessCredentialID != nil {
				input.AccessCredentialID = *apiKey.AccessCredentialID
			}
			if apiKey.OwnerUserID != nil {
				input.OwnerUserID = *apiKey.OwnerUserID
			}
		}
		if claims := middleware.GetAgentCapabilityFromContext(r.Context()); claims != nil {
			input.AgentID = claims.AgentID
		}
		policyIdentities := make([]modelauthz.ModelIdentity, 0, len(allowedIdentities))
		for _, identity := range allowedIdentities {
			policyIdentities = append(policyIdentities, modelauthz.ModelIdentity{
				RequestModel: identity.RequestModel, ResolvedModel: identity.ResolvedModel,
			})
		}
		policyIdentities, err = relay.modelAuthorization.FilterModelIdentities(r.Context(), input, policyIdentities)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.model_authorization_unavailable", "Model authorization is temporarily unavailable")
			return
		}
		allowedIdentities = allowedIdentities[:0]
		for _, identity := range policyIdentities {
			allowedIdentities = append(allowedIdentities, modelgov.CallableModelIdentity{
				RequestModel: identity.RequestModel, ResolvedModel: identity.ResolvedModel,
			})
		}
	}
	allowedModels := make([]string, 0, len(allowedIdentities))
	resolvedModels := make([]string, 0, len(allowedIdentities))
	for _, identity := range allowedIdentities {
		allowedModels = append(allowedModels, identity.RequestModel)
		resolvedModels = append(resolvedModels, identity.ResolvedModel)
	}

	var executionProfiles map[string]publicModelExecutionProfile
	if requestIncludesModelExecutionProfiles(r.URL.Query()["include"]) {
		executionProfiles, err = relay.loadPublicModelExecutionProfiles(r.Context(), orgID, resolvedModels)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to resolve model execution profiles")
			return
		}
	}

	listedModels := make([]map[string]interface{}, 0, len(allowedModels))
	for _, identity := range allowedIdentities {
		listedModel := map[string]interface{}{
			"id":       identity.RequestModel,
			"object":   "model",
			"owned_by": "uniroute",
		}
		if executionProfile, ok := executionProfiles[identity.ResolvedModel]; ok {
			listedModel["x_unirouter_execution"] = executionProfile
		}
		listedModels = append(listedModels, listedModel)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   listedModels,
	})
}

// checkProjectBudgetAlert triggers the one active, tenant-scoped budget-alert
// contract after settlement. The historical global first-wallet alert path is
// intentionally retired because it has no safe multi-tenant scope.
func (relay *Relay) checkProjectBudgetAlert(ctx context.Context) {
	if relay != nil && relay.outbox != nil {
		// The settled financial outbox event updates the incremental project
		// ledger and schedules a bounded evaluation exactly once.
		return
	}
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
