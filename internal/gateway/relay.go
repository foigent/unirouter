package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/tokenutil"
)

// BillingProvider defines the billing operations that Relay depends on.
// *billing.Billing satisfies this interface.
type BillingProvider interface {
	QueryModelPriceForOrg(ctx context.Context, orgID int64, model string) (float64, float64, error)
	CalculateCost(ctx context.Context, model string, promptTokens, completionTokens int) (int, error)
	Freeze(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error)
	Settle(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error)
	Cancel(ctx context.Context, requestID string, orgID int64) error
}

// Relay handles forwarding requests to upstream providers.
type Relay struct {
	db          db.DBQuerier
	rdb         *redis.Client
	router      *Router
	billing     BillingProvider
	filter      *filter.DFAFilter
	encKey      []byte
	alertSvc    *billing.AlertService
	webhookURL  string
	alertThresh int
}

// NewRelay creates a new Relay.
func NewRelay(db db.DBQuerier, rdb *redis.Client, router *Router, b BillingProvider, f *filter.DFAFilter, encKey []byte) *Relay {
	return &Relay{db: db, rdb: rdb, router: router, billing: b, filter: f, encKey: encKey}
}

// SetAlertService configures the budget alert service for post-settle checks.
func (relay *Relay) SetAlertService(svc *billing.AlertService, webhookURL string, thresholdCents int) {
	relay.alertSvc = svc
	relay.webhookURL = webhookURL
	relay.alertThresh = thresholdCents
}

func upstreamEndpoint(baseURL, endpoint string) string {
	base := strings.TrimRight(baseURL, "/")
	path := strings.TrimLeft(endpoint, "/")
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "v1/") {
		path = strings.TrimPrefix(path, "v1/")
	}
	return base + "/" + path
}

func elapsedMilliseconds(start time.Time) int {
	ms := int(time.Since(start).Milliseconds())
	if ms < 0 {
		return 0
	}
	return ms
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

// HandleChatCompletion handles POST /v1/chat/completions.
func (relay *Relay) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
	apiKey := middleware.GetAPIKeyFromContext(ctx)
	if apiKey != nil && apiKey.AllowedModels != "" {
		allowed := false
		for _, m := range strings.Split(apiKey.AllowedModels, ",") {
			if strings.TrimSpace(m) == "*" || strings.TrimSpace(m) == req.Model {
				allowed = true
				break
			}
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "permission_error", "auth_error.model_not_allowed",
				fmt.Sprintf("Model '%s' is not allowed for this API key", req.Model))
			return
		}
	}

	// Estimate tokens for billing freeze
	estimatedTokens := estimateTokens(req.Messages, req.MaxTokens)

	// Freeze balance once (before trying any upstream)
	orgID := middleware.GetOrgIDFromContext(ctx)
	keyID := middleware.GetKeyIDFromContext(ctx)
	reqLog := logger.New(logger.Fields{RequestID: requestID, OrgID: uint(orgID), Module: "chat"})

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
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to process billing")
		return
	}
	if !freezeResult.Success {
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

	// Get all available routes for failover
	routes, err := relay.router.RouteAll(ctx, req.Model)
	if err != nil {
		relay.billing.Cancel(ctx, requestID, orgID)
		writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", fmt.Sprintf("No available channel for model: %s", req.Model))
		relay.recordRequest(ctx, requestID, orgID, keyID, 0, "", "", req.Model, req.Model, r.RemoteAddr, 0, 0, 0, 503, elapsedMilliseconds(requestStart), 0, false, "no_route")
		return
	}

	// Build upstream body once — replace model with mapped model name
	upstreamBody := make(map[string]interface{})
	json.Unmarshal(body, &upstreamBody)
	replacedBodyBase, _ := json.Marshal(upstreamBody)

	client := &http.Client{Timeout: 300 * time.Second}

	// Failover loop: try each route until one succeeds
	var lastErr error
	var lastStatusCode int
	for _, route := range routes {
		apiKey, err := auth.DecryptKey(route.ChannelKey.KeyEnc, relay.encKey)
		if err != nil {
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Failed to decrypt key: %v", err)
			continue
		}

		// Inject the correct mapped model for this route
		var bodyMap map[string]interface{}
		json.Unmarshal(replacedBodyBase, &bodyMap)
		bodyMap["model"] = route.MappedModel
		routeBody, _ := json.Marshal(bodyMap)

		upstreamURL := upstreamEndpoint(route.Channel.BaseURL, "/v1/chat/completions")
		upstreamReq, err := http.NewRequestWithContext(ctx, "POST", upstreamURL, bytes.NewReader(routeBody))
		if err != nil {
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Failed to create upstream request: %v", err)
			continue
		}

		upstreamReq.Header.Set("Content-Type", "application/json")
		upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)

		upstreamStart := time.Now()
		resp, err := client.Do(upstreamReq)
		upstreamDuration := time.Since(upstreamStart)
		middleware.RecordUpstreamLatency(route.Channel.Provider, req.Model, upstreamDuration)
		if err != nil {
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Upstream request failed: %v", err)
			relay.router.MarkChannelFail(ctx, route.Channel.ID, 5, 300)
			relay.router.MarkKeyFail(ctx, route.ChannelKey.ID)
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(resp.Body)
			reqLog.With(logger.Fields{ChannelID: uint(route.Channel.ID)}).Error("Upstream returned %d: %s", resp.StatusCode, string(errBody))
			// Per PRD F12: only server errors (408, 500, 502, 503) and connection
			// timeouts trigger circuit breaker. Client errors are not channel failures.
			switch {
			case resp.StatusCode == 408 || resp.StatusCode == 500 || resp.StatusCode == 502 || resp.StatusCode == 503:
				relay.router.MarkChannelFail(ctx, route.Channel.ID, 5, 300)
				relay.router.MarkKeyFail(ctx, route.ChannelKey.ID)
			case resp.StatusCode == 401 || resp.StatusCode == 403:
				// Auth failure — mark key as invalid immediately
				relay.router.MarkKeyCooldown(ctx, route.ChannelKey.ID, 3600)
			case resp.StatusCode == 429:
				// Rate limited — cooldown key for 60s
				relay.router.MarkKeyCooldown(ctx, route.ChannelKey.ID, 60)
			default:
				// 4xx client errors (400, 404, etc.) — don't mark as failure
			}
			lastStatusCode = resp.StatusCode
			lastErr = fmt.Errorf("upstream returned %d: %s", resp.StatusCode, string(errBody))
			resp.Body.Close()
			continue
		}

		// Success — mark channel healthy and forward
		relay.router.MarkChannelSuccess(ctx, route.Channel.ID)
		relay.router.MarkKeySuccess(ctx, route.ChannelKey.ID)

		if req.Stream {
			relay.relayStreamRequest(ctx, w, resp, &route, requestID, orgID, keyID, req.Model, req.Model, r.RemoteAddr, req.Messages, reqLog, requestStart)
		} else {
			relay.relayNonStreamRequest(ctx, w, resp, &route, requestID, orgID, keyID, req.Model, req.Model, r.RemoteAddr, reqLog, requestStart)
		}
		return
	}

	// All channels failed
	relay.billing.Cancel(ctx, requestID, orgID)
	if lastErr != nil {
		reqLog.Error("All upstream channels failed, last error: %v", lastErr)
	}
	// Log the failed request
	if len(routes) > 0 {
		relay.recordRequest(ctx, requestID, orgID, keyID, routes[0].Channel.ID, routes[0].Channel.Provider, routes[0].Channel.Name, req.Model, req.Model, r.RemoteAddr, 0, 0, 0, lastStatusCode, elapsedMilliseconds(requestStart), 0, false, "upstream_failed")
	}
	if lastStatusCode > 0 {
		writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_error", fmt.Sprintf("All upstream channels failed (last status: %d)", lastStatusCode))
	} else {
		writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_error", "All upstream channels failed")
	}
}

// relayNonStreamRequest handles non-streaming responses.
func (relay *Relay) relayNonStreamRequest(ctx context.Context, w http.ResponseWriter, resp *http.Response, route *RouteResult, requestID string, orgID, keyID int64, model, requestedModel, clientIP string, reqLog *logger.Logger, requestStart time.Time) {

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		reqLog.Error("Failed to read response body: %v", err)
		return
	}

	// Parse usage from response
	var usage struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	json.Unmarshal(body, &usage)

	// Settle billing with actual usage
	actualCost := relay.calculateCost(ctx, orgID, model, usage.Usage.PromptTokens, usage.Usage.CompletionTokens)
	_, err = relay.billing.Settle(ctx, billing.SettleRequest{
		RequestID:        requestID,
		OrgID:            orgID,
		ActualCost:       actualCost,
		PromptTokens:     usage.Usage.PromptTokens,
		CompletionTokens: usage.Usage.CompletionTokens,
		UsageSource:      "api",
	})
	if err != nil {
		reqLog.Error("Billing settle error: %v", err)
	}
	middleware.RecordTokenUsage("prompt", model, usage.Usage.PromptTokens)
	middleware.RecordTokenUsage("completion", model, usage.Usage.CompletionTokens)
	relay.checkBudgetAlert(ctx)

	// Record request log
	relay.recordRequest(ctx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, model, requestedModel, clientIP,
		usage.Usage.PromptTokens, usage.Usage.CompletionTokens, actualCost, resp.StatusCode, elapsedMilliseconds(requestStart), 0, false, "api")

	// Build response with _uniroute extension field
	var responseMap map[string]interface{}
	json.Unmarshal(body, &responseMap)
	responseMap["_uniroute"] = map[string]interface{}{
		"request_id": requestID,
		"cost": map[string]interface{}{
			"actual":    actualCost,
			"charged":   actualCost,
			"cache_hit": false,
		},
		"provider":  route.Channel.Provider,
		"channel":   route.Channel.Name,
		"cache_hit": false,
		"fallback":  false,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	json.NewEncoder(w).Encode(responseMap)
}

// relayStreamRequest handles streaming SSE responses.
// Balance check goroutine signals via a channel; the main loop owns all writes
// and the upstream body lifecycle. No goroutine touches resp.Body directly.
func (relay *Relay) relayStreamRequest(ctx context.Context, w http.ResponseWriter, resp *http.Response, route *RouteResult, requestID string, orgID, keyID int64, model, requestedModel, clientIP string, messages []Message, reqLog *logger.Logger, requestStart time.Time) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		reqLog.Error("ResponseWriter does not support Flusher")
		return
	}

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

	done := false
	for !done && scanner.Scan() {
		line := scanner.Text()

		// Track TTFT
		if !firstTokenReceived && strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
			firstTokenReceived = true
			ttftMs = int(time.Since(streamStart).Milliseconds())
		}

		if line == "data: [DONE]" {
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

	// If upstream didn't provide usage, estimate input tokens from the original messages
	if !usedUpstreamUsage {
		var msgPairs []struct{ Role, Content string }
		for _, m := range messages {
			msgPairs = append(msgPairs, struct{ Role, Content string }{Role: m.Role, Content: m.Content})
		}
		tokenCounter.SetEstimatedInputTokens(tokenutil.EstimateRequestTokens(msgPairs))
	}

	actualCost := relay.calculateCost(ctx, orgID, model, tokenCounter.InputTokens(), tokenCounter.OutputTokens())
	_, settleErr := relay.billing.Settle(ctx, billing.SettleRequest{
		RequestID:        requestID,
		OrgID:            orgID,
		ActualCost:       actualCost,
		PromptTokens:     tokenCounter.InputTokens(),
		CompletionTokens: tokenCounter.OutputTokens(),
		UsageSource: func() string {
			if usedUpstreamUsage {
				return "api"
			}
			return "stream_estimate"
		}(),
	})
	if settleErr != nil {
		reqLog.Error("Billing settle error: %v", settleErr)
	}
	middleware.RecordTokenUsage("prompt", model, tokenCounter.InputTokens())
	middleware.RecordTokenUsage("completion", model, tokenCounter.OutputTokens())
	relay.checkBudgetAlert(ctx)

	streamDuration := time.Since(streamStart)
	middleware.RecordUpstreamLatency(route.Channel.Provider, model, streamDuration)

	usageSource := "stream_estimate"
	if usedUpstreamUsage {
		usageSource = "api"
	}
	relay.recordRequest(ctx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, model, requestedModel, clientIP,
		tokenCounter.InputTokens(), tokenCounter.OutputTokens(), actualCost, http.StatusOK, elapsedMilliseconds(requestStart), ttftMs, true, usageSource)

	// Send usage + _uniroute extension as the final data chunk before [DONE].
	unirouteData := map[string]interface{}{
		"request_id": requestID,
		"cost": map[string]interface{}{
			"actual":    actualCost,
			"charged":   actualCost,
			"cache_hit": false,
		},
		"provider":  route.Channel.Provider,
		"channel":   route.Channel.Name,
		"ttft_ms":   ttftMs,
		"cache_hit": false,
		"fallback":  false,
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
func (relay *Relay) calculateCost(ctx context.Context, orgID int64, model string, promptTokens, completionTokens int) int {
	costLog := logger.New(logger.Fields{OrgID: uint(orgID), Module: "billing"})
	inputPerToken, outputPerToken, err := relay.billing.QueryModelPriceForOrg(ctx, orgID, model)
	if err != nil {
		costLog.Warn("QueryModelPriceForOrg error for %s: %v, using default", model, err)
		inputPerToken = 2.0
		outputPerToken = 10.0
	}
	cost := float64(promptTokens)*inputPerToken + float64(completionTokens)*outputPerToken
	result := int(cost / 1000)
	if result < 1 {
		result = 1
	}
	return result
}

// recordRequest logs the request to the call_logs table.
func (relay *Relay) recordRequest(ctx context.Context, requestID string, orgID, keyID, channelID int64, provider, channelName, model, requestedModel, clientIP string, promptTokens, completionTokens, costCents, statusCode, latencyMs, ttftMs int, isStream bool, usageSource string) {
	reqLog := logger.New(logger.Fields{RequestID: requestID, OrgID: uint(orgID), Module: "records"})
	_, err := relay.db.Exec(ctx,
		`INSERT INTO call_logs (request_id, org_id, api_key_id, model, requested_model, provider, channel, prompt_tokens, completion_tokens, total_tokens, cost_cents, status_code, latency_ms, ttft_ms, usage_source, is_stream, ip_address)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		requestID, orgID, keyID, model, requestedModel, provider, channelName, promptTokens, completionTokens, promptTokens+completionTokens, costCents, statusCode, latencyMs, ttftMs, usageSource, isStream, clientIP,
	)
	if err != nil {
		reqLog.Error("Failed to record request log: %v", err)
	}
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
	ctx := r.Context()
	requestStart := time.Now()
	requestID := middleware.GetRequestIDFromContext(ctx)

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
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to process billing")
		return
	}
	if !freezeResult.Success {
		middleware.RecordBillingFreeze("error")
		if freezeResult.ErrorCode == "insufficient_balance" {
			writeError(w, http.StatusPaymentRequired, "billing_error", "billing_error.insufficient_balance", "Insufficient balance")
		} else {
			writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", freezeResult.ErrorCode)
		}
		return
	}
	middleware.RecordBillingFreeze("success")

	// Get route
	route, err := relay.router.Route(ctx, req.Model)
	if err != nil {
		relay.billing.Cancel(ctx, requestID, orgID)
		writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.service_unavailable", fmt.Sprintf("No available channel for model: %s", req.Model))
		relay.recordRequest(ctx, requestID, orgID, keyID, 0, "", "", req.Model, req.Model, r.RemoteAddr, 0, 0, 0, 503, elapsedMilliseconds(requestStart), 0, false, "no_route")
		return
	}

	apiKey, err := auth.DecryptKey(route.ChannelKey.KeyEnc, relay.encKey)
	if err != nil {
		relay.billing.Cancel(ctx, requestID, orgID)
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to decrypt API key")
		return
	}

	// Replace model name and forward
	upstreamBody := make(map[string]interface{})
	json.Unmarshal(body, &upstreamBody)
	upstreamBody["model"] = route.MappedModel
	replacedBody, _ := json.Marshal(upstreamBody)

	upstreamURL := upstreamEndpoint(route.Channel.BaseURL, "/v1/embeddings")
	upstreamReq, err := http.NewRequestWithContext(ctx, "POST", upstreamURL, bytes.NewReader(replacedBody))
	if err != nil {
		relay.billing.Cancel(ctx, requestID, orgID)
		writeError(w, http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to create upstream request")
		return
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 120 * time.Second}
	upstreamStart := time.Now()
	resp, err := client.Do(upstreamReq)
	upstreamDuration := time.Since(upstreamStart)
	middleware.RecordUpstreamLatency(route.Channel.Provider, req.Model, upstreamDuration)
	if err != nil {
		reqLog.Error("Upstream embeddings request failed: %v", err)
		relay.billing.Cancel(ctx, requestID, orgID)
		writeError(w, http.StatusBadGateway, "server_error", "server_error.upstream_error", "Upstream request failed")
		return
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// Parse usage for settlement
	var usage struct {
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	json.Unmarshal(respBody, &usage)

	actualCost := relay.calculateCost(ctx, orgID, req.Model, usage.Usage.PromptTokens, 0)
	relay.billing.Settle(ctx, billing.SettleRequest{
		RequestID:    requestID,
		OrgID:        orgID,
		ActualCost:   actualCost,
		PromptTokens: usage.Usage.PromptTokens,
		UsageSource:  "api",
	})
	middleware.RecordTokenUsage("prompt", req.Model, usage.Usage.PromptTokens)
	relay.checkBudgetAlert(ctx)

	relay.recordRequest(ctx, requestID, orgID, keyID, route.Channel.ID, route.Channel.Provider, route.Channel.Name, req.Model, req.Model, r.RemoteAddr,
		usage.Usage.PromptTokens, 0, actualCost, resp.StatusCode, elapsedMilliseconds(requestStart), 0, false, "api")

	// Build response with _uniroute extension
	var responseMap map[string]interface{}
	json.Unmarshal(respBody, &responseMap)
	responseMap["_uniroute"] = map[string]interface{}{
		"request_id": requestID,
		"cost": map[string]interface{}{
			"actual":    actualCost,
			"charged":   actualCost,
			"cache_hit": false,
		},
		"provider":  route.Channel.Provider,
		"channel":   route.Channel.Name,
		"cache_hit": false,
		"fallback":  false,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	json.NewEncoder(w).Encode(responseMap)
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

// checkBudgetAlert triggers an async budget check after settlement.
func (relay *Relay) checkBudgetAlert(ctx context.Context) {
	if relay.alertSvc == nil {
		return
	}
	go relay.alertSvc.CheckAndNotify(context.Background(), relay.webhookURL, relay.alertThresh)
}
