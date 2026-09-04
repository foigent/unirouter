package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
)

func validateChatOutputParameters(req ChatCompletionRequest) string {
	for _, field := range []struct {
		name  string
		value *int
	}{{"max_tokens", req.MaxTokens}, {"max_completion_tokens", req.MaxCompletionTokens}} {
		if field.value != nil && (*field.value < 0 || *field.value > maxChatCompletionTokens) {
			return field.name + " is outside the supported range"
		}
	}
	if req.MaxTokens != nil && req.MaxCompletionTokens != nil && *req.MaxTokens != *req.MaxCompletionTokens {
		return "max_tokens and max_completion_tokens must match when both are provided"
	}
	return ""
}

// This is an ephemeral, caller-authorized projection, never an execution
// entitlement. In particular it contains neither the internal Plan nor routes.
type chatPreflightResult struct {
	Object                     string     `json:"object"`
	RequestModel               string     `json:"request_model"`
	Operation                  string     `json:"operation"`
	LocallyAdmissible          bool       `json:"locally_admissible"`
	SnapshotAt                 *time.Time `json:"snapshot_at,omitempty"`
	ProfileID                  string     `json:"profile_id,omitempty"`
	ProfileVersion             string     `json:"profile_version,omitempty"`
	ProtocolProfile            string     `json:"protocol_profile,omitempty"`
	RequiredCapabilities       []string   `json:"required_capabilities"`
	ContextWindow              int        `json:"context_window,omitempty"`
	MaxOutputTokens            int        `json:"max_output_tokens,omitempty"`
	RequestDefaultOutputTokens int        `json:"request_default_output_tokens,omitempty"`
	Reasons                    []string   `json:"reasons"`
	NotChecked                 []string   `json:"not_checked"`
}

// HandleChatPreflight must be mounted behind the real Chat caller, retained
// payload, strict JSON, license, model-governance and model-authorization gates.
// It deliberately does not call materialize/Freeze/Authorize or any transport.
func (relay *Relay) HandleChatPreflight(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-ID", middleware.GetRequestIDFromContext(ctx))
	body, retained := payloadpolicy.BodyFromContext(ctx)
	if !retained {
		writeError(w, http.StatusServiceUnavailable, "server_error", "server_error.preflight_unavailable", "Retained request payload is unavailable")
		return
	}
	var req ChatCompletionRequest
	if json.Unmarshal(body, &req) != nil || strings.TrimSpace(req.Model) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", "A valid Chat document and model are required")
		return
	}
	if message := validateChatOutputParameters(req); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", message)
		return
	}
	requested := middleware.GetModelAliasFromContext(ctx)
	if requested == "" {
		requested = req.Model
	}
	if !modelAllowedForAPIKey(middleware.GetAPIKeyFromContext(ctx), req.Model, requested) {
		writeError(w, http.StatusForbidden, "permission_error", "auth_error.model_not_allowed", "The requested model is not allowed")
		return
	}
	result := chatPreflightResult{
		Object: "chat.completion.preflight", RequestModel: requested, Operation: string(modeloperation.IDChatCompletions),
		Reasons: []string{}, RequiredCapabilities: []string{},
		NotChecked: []string{"execution_quota_and_reservation", "content_policy_and_compression", "credential_liveness_and_provider_behavior", "response_delivery", "agent_outcome"},
	}
	if relay.modelExecution == nil || relay.modelExecution.mode != modelrouting.ModeCanonical {
		result.Reasons = append(result.Reasons, "canonical_preflight_unavailable")
		writeChatPreflight(w, result)
		return
	}
	admission, err := estimateChatPreProfileAdmission(body)
	if err != nil {
		result.Reasons = append(result.Reasons, "validation_error.unsupported_semantics")
		writeChatPreflight(w, result)
		return
	}
	ctx = contextWithAssistantToolHistorySnapshot(ctx, body)
	batch, err := relay.prepareModelExecutionPlan(ctx, modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		req.Model, requested, middleware.GetRequestIDFromContext(ctx), modelExecutionRequestLimits{
			EstimatedInputTokens: admission.InputTokens, ExplicitOutputTokens: req.completionTokenLimit(), EstimateVersion: admission.Version,
		}, body, time.Now())
	if batch != nil {
		plan := batch.plan
		result.ProfileID, result.ProfileVersion = plan.ExecutionProfileID, plan.ExecutionProfileVersion
		result.ProtocolProfile = plan.ModelProtocolProfile
		result.ContextWindow, result.MaxOutputTokens = plan.PublicContextLimit, plan.PublicOutputLimit
		result.RequestDefaultOutputTokens = plan.RequestDefaultOutputLimit
		result.RequiredCapabilities = append(result.RequiredCapabilities, plan.RequiredCapabilities...)
		if !plan.DatabaseTime.IsZero() {
			snapshotAt := plan.DatabaseTime.UTC()
			result.SnapshotAt = &snapshotAt
		}
		for _, excluded := range plan.Exclusions {
			result.Reasons = append(result.Reasons, publicPreflightExclusion(excluded.Reason, plan.RequiredCapabilities))
		}
	}
	if err != nil {
		result.Reasons = append(result.Reasons, preflightFailureCode(err))
		writeChatPreflight(w, result)
		return
	}
	for _, route := range batch.plan.Routes {
		adapter, lookupErr := relay.modelExecution.registry.Get(route.AdapterType)
		openAI, supported := adapter.(*adapters.OpenAICompatible)
		if lookupErr != nil || !supported {
			result.Reasons = append(result.Reasons, "adapter_preflight_not_supported")
			continue
		}
		shaped, limits, prepareErr := prepareExecutionRouteBody(adapters.EndpointChatCompletions, batch, route)
		if prepareErr != nil {
			result.Reasons = append(result.Reasons, preflightFailureCode(prepareErr))
			continue
		}
		upstream, buildErr := openAI.PrepareRequest(ctx, adapters.RequestInput{
			Endpoint: adapters.EndpointChatCompletions, DownstreamBody: shaped,
			RequestedModel: requested, MappedModel: route.UpstreamModel,
			Channel: compatibilityRouteFromExecutionPlan(batch.plan, route).Channel,
		})
		if buildErr != nil {
			result.Reasons = append(result.Reasons, "provider_request_build_failed")
			continue
		}
		actual, bodyErr := providerRequestBody(upstream)
		_ = upstream.Body.Close()
		if bodyErr == nil {
			_, bodyErr = validateExecutionOutboundLimits(adapters.EndpointChatCompletions, actual, batch.plan, route, limits)
		}
		if bodyErr != nil {
			result.Reasons = append(result.Reasons, preflightFailureCode(bodyErr))
			continue
		}
		result.LocallyAdmissible = true
	}
	writeChatPreflight(w, result)
}

func writeChatPreflight(w http.ResponseWriter, result chatPreflightResult) {
	slices.Sort(result.Reasons)
	result.Reasons = slices.Compact(result.Reasons)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func preflightFailureCode(err error) string {
	if code, _, ok := modelExecutionLimitErrorDetails(err); ok {
		return code
	}
	if _, code, _, ok := processingTierAdmissionErrorDetails(err); ok {
		return code
	}
	switch {
	case errors.Is(err, modelrouting.ErrNoRoute):
		return "no_eligible_route"
	case errors.Is(err, modelrouting.ErrManifestStale):
		return "runtime_manifest_stale"
	case errors.Is(err, modelrouting.ErrManifestUnsealed):
		return "runtime_manifest_unsealed"
	case errors.Is(err, modelrouting.ErrAuthorityNotReady):
		return "runtime_authority_not_ready"
	default:
		return "preflight_unavailable"
	}
}

// This allowlist is a disclosure boundary, not another route evaluator. Future
// internal reasons remain private until deliberately exposed as public codes.
func publicPreflightExclusion(reason string, capabilities []string) string {
	if suffix, ok := strings.CutPrefix(reason, "unsupported_capability:"); ok && slices.Contains(capabilities, suffix) {
		return reason
	}
	switch reason {
	case "retired", "administratively_disabled", "execution_limits_missing", "stale_revision",
		"unsupported_operation", "profile_product_identity_mismatch", "protocol_profile_missing",
		"unverified_operation", "explicit_binding_missing", "binding_probe_missing",
		"credential_secret_missing", "credential_unavailable", "circuit_open", "half_open_busy":
		return reason
	default:
		return "route_unavailable"
	}
}
