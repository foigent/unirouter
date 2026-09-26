package gateway

import (
	"context"
	"errors"
	"github.com/uniroute/unirouter/internal/executionmeasurement"
	"net/http"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/gatewayerror"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
)

type pinnedPublicNonStreamGuard struct {
	relay          *Relay
	pinned         *guardrail.Pinned
	requestID      string
	requestedModel string
	resolvedModel  string
}

func (adapter *pinnedPublicNonStreamGuard) EvaluateInput(
	ctx context.Context,
	admission endpointprotocol.PublicAdmission,
) (PublicOutputGuardDecision, error) {
	return adapter.evaluate(ctx, guardrail.StageInput, admission.Texts, 0)
}

func (adapter *pinnedPublicNonStreamGuard) Evaluate(
	ctx context.Context,
	input PublicOutputGuardInput,
) (PublicOutputGuardDecision, error) {
	return adapter.evaluate(ctx, guardrail.StageNonStreamOutput, input.Texts, http.StatusOK)
}

func (adapter *pinnedPublicNonStreamGuard) evaluate(
	ctx context.Context,
	stage string,
	texts []endpointprotocol.PublicText,
	statusCode int,
) (PublicOutputGuardDecision, error) {
	if adapter == nil || adapter.relay == nil || adapter.pinned == nil {
		return PublicOutputGuardDecision{}, errors.New("public guardrail snapshot unavailable")
	}
	guardTexts := make([]guardrail.Text, len(texts))
	for index, value := range texts {
		guardTexts[index] = guardrail.Text{Path: value.Path, Content: value.Text}
	}
	result := adapter.pinned.EvaluateWithCompleteness(ctx, stage, guardTexts, true)
	// Public execution never constructs a legacy RouteResult. Guardrail
	// evidence is request-local and provider-neutral at this boundary.
	adapter.relay.recordGuardrailResult(
		context.WithoutCancel(ctx), adapter.requestID, adapter.resolvedModel, adapter.requestedModel,
		stage, result, nil, statusCode, "",
	)
	if result.Decision == guardrail.DecisionBlock {
		return PublicOutputGuardDecision{Action: PublicOutputGuardBlock}, nil
	}
	if result.Decision != guardrail.DecisionAllow || len(result.Texts) != len(guardTexts) {
		return PublicOutputGuardDecision{}, errors.New("public guardrail decision invalid")
	}
	replacements := make([]string, len(result.Texts))
	changed := result.Transformed
	for index, value := range result.Texts {
		if value.Path != guardTexts[index].Path {
			return PublicOutputGuardDecision{}, errors.New("public guardrail path changed")
		}
		replacements[index] = value.Content
		if value.Content != guardTexts[index].Content {
			changed = true
		}
	}
	if changed {
		return PublicOutputGuardDecision{Action: PublicOutputGuardReplace, Replacements: replacements}, nil
	}
	return PublicOutputGuardDecision{Action: PublicOutputGuardAllow}, nil
}

// HandlePublicNonStream is the compatibility name for the exact public
// Responses/Messages entry. Exact v2 identities may select their separately
// compiled SSE executor; historical identities remain non-stream.
func (relay *Relay) HandlePublicNonStream(w http.ResponseWriter, r *http.Request, operation modeloperation.ID) {
	if w == nil || r == nil {
		return
	}
	requestID := middleware.GetRequestIDFromContext(r.Context())
	w.Header().Set("X-Request-ID", requestID)
	if operation == modeloperation.IDMessages {
		w.Header().Set("request-id", requestID)
	}
	body, retained := payloadpolicy.BodyFromContext(r.Context())
	apiKey := middleware.GetAPIKeyFromContext(r.Context())
	publicModel := middleware.GetModelAliasFromContext(r.Context())
	decision, authorized := modelauthz.DecisionFromContext(r.Context())
	resolvedModel := decision.ResolvedModel
	if relay == nil || !retained || apiKey == nil || publicModel == "" || requestID == "" ||
		publicModel != strings.TrimSpace(publicModel) || len(publicModel) > 200 ||
		!authorized || decision.ContractVersion != modelauthz.ContractVersion || !decision.Allowed ||
		decision.Endpoint != r.URL.Path || decision.RequestedModel != publicModel ||
		decision.RuntimeMutation != "none" || resolvedModel == "" || resolvedModel != strings.TrimSpace(resolvedModel) ||
		len(resolvedModel) > 160 || strings.ContainsAny(publicModel+resolvedModel, "\x00\r\n") {
		writePublicNonStreamError(w, r, requestID, errPublicNonStreamRuntimeUnavailable)
		return
	}

	var runtime *modelExecutionRuntime
	if relay != nil {
		runtime = relay.modelExecution
	}
	if runtime == nil {
		writePublicNonStreamError(w, r, requestID, errPublicNonStreamRuntimeUnavailable)
		return
	}
	guard := &pinnedPublicNonStreamGuard{
		relay: relay, pinned: relay.pinGuardrail(r.Context(), resolvedModel, r.URL.Path),
		requestID: requestID, requestedModel: publicModel, resolvedModel: resolvedModel,
	}
	orchestrator := &publicNonStreamOrchestrator{
		contentCapture: relay.contentCapture,
		mode:           runtime.mode, planner: runtime.planner, coordinator: runtime.publicCoordinator,
		billing: relay.billing, registry: runtime.publicRegistry,
		decrypt: auth.DecryptKey, encryptionKey: relay.encKey,
		doer: relay.providerClient(300 * time.Second), guard: guard,
		safetyFactor: relay.freezeSafetyFactor, minFreeze: relay.minFreezeCents,
	}
	request := publicNonStreamRequest{
		Operation: operation, Path: r.URL.Path, Body: body, PublicModel: publicModel, ResolvedModel: resolvedModel,
		RequestID: requestID, OrgID: apiKey.OrgID, APIKeyID: apiKey.ID,
	}
	if apiKey.ProjectID != nil {
		request.ProjectID = *apiKey.ProjectID
	}
	if apiKey.AccessCredentialID != nil {
		request.AccessCredentialID = *apiKey.AccessCredentialID
	}
	request.CredentialRef = apiKey.CredentialRef
	if apiKey.AccessGrantID != nil {
		request.AccessGrantID = *apiKey.AccessGrantID
	}
	var inspectStreaming func([]byte) (bool, error)
	var bindStream publicStreamBindingFactory
	switch operation {
	case modeloperation.IDResponses:
		inspectStreaming = endpointprotocol.PublicResponsesStreamingRequested
		bindStream = newPublicResponsesStreamBinding
	case modeloperation.IDMessages:
		inspectStreaming = endpointprotocol.PublicAnthropicMessagesStreamingRequested
		bindStream = newPublicAnthropicMessagesStreamBinding
	}
	if inspectStreaming != nil {
		streaming, inspectErr := inspectStreaming(body)
		if inspectErr != nil {
			writePublicNonStreamError(w, r, requestID, errPublicNonStreamRequestInvalid)
			return
		}
		executionmeasurement.SetStreaming(r.Context(), streaming)
		if streaming {
			streamPolicy := relay.currentStreamPolicy()
			streamCore := &publicNonStreamOrchestrator{
				contentCapture: relay.contentCapture,
				mode:           runtime.mode, planner: runtime.planner, coordinator: runtime.publicCoordinator,
				billing: relay.billing, registry: runtime.publicStreamRegistry,
				decrypt: auth.DecryptKey, encryptionKey: relay.encKey,
				// The request-scoped stream policy below is the only whole-stream
				// deadline. A second http.Client timeout would truncate the body on
				// a different clock and make that policy non-authoritative.
				doer: relay.providerClient(0), guard: guard,
				safetyFactor: relay.freezeSafetyFactor, minFreeze: relay.minFreezeCents,
			}
			streamOrchestrator := &publicStreamOrchestrator{
				core: streamCore, billing: relay.billing, pinned: guard.pinned,
				streamPolicy: streamPolicy, relay: relay, bindStream: bindStream,
			}
			err := streamOrchestrator.execute(r.Context(), w, request)
			if err == nil {
				return
			}
			if publicResponseWasStarted(err) {
				logger.New(logger.Fields{
					RequestID: requestID, OrgID: uint(apiKey.OrgID), Module: "public_operation_stream",
				}).Error("post-write public operation stream terminal failed: %v", err)
				return
			}
			writePublicNonStreamError(w, r, requestID, err)
			return
		}
	}

	err := orchestrator.execute(r.Context(), w, request)
	if err == nil {
		return
	}
	if publicResponseWasStarted(err) {
		logger.New(logger.Fields{
			RequestID: requestID, OrgID: uint(apiKey.OrgID), Module: "public_nonstream",
		}).Error("post-write public execution terminal failed: %v", err)
		return
	}
	writePublicNonStreamError(w, r, requestID, err)
}

func writePublicNonStreamError(w http.ResponseWriter, r *http.Request, requestID string, err error) {
	status := http.StatusServiceUnavailable
	code := "server_error.public_execution_unavailable"
	message := "Public model execution is temporarily unavailable"
	switch {
	case errors.Is(err, errPublicNonStreamRequestInvalid):
		status = http.StatusBadRequest
		code = "validation_error.invalid_request"
		message = "Invalid request"
	case errors.Is(err, errPublicNonStreamInputWithheld):
		status = http.StatusBadRequest
		code = "guardrail_error.input_blocked"
		message = "Request content was blocked by policy"
	case errors.Is(err, ErrPublicNonStreamOutputWithheld):
		status = http.StatusForbidden
		code = "guardrail_error.output_blocked"
		message = "Response content was withheld by policy"
	case errors.Is(err, errPublicNonStreamGuardFailed):
		code = "server_error.guardrail_unavailable"
		message = "Content policy evaluation is temporarily unavailable"
	case errors.Is(err, errPublicNonStreamHealthFailed):
		code = "server_error.execution_state_unavailable"
		message = "Public model execution state is temporarily unavailable"
	case errors.Is(err, errPublicNonStreamBillingFailed):
		code = "server_error.billing_unavailable"
		message = "Billing state is temporarily unavailable"
	case errors.Is(err, errPublicNonStreamDeliveryFailed):
		status = http.StatusInternalServerError
		code = "server_error.delivery_failed"
		message = "Response delivery failed"
	case errors.Is(err, errPublicNonStreamUpstreamFailed):
		status = http.StatusBadGateway
		code = "server_error.upstream_unavailable"
		message = "Public model execution failed"
	case errors.Is(err, errPublicNonStreamContractMismatch), errors.Is(err, errPublicNonStreamRuntimeUnavailable):
		// Keep contract/runtime topology private and retryable.
	default:
		code = "server_error.public_execution_failed"
		message = "Public model execution failed"
	}
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "1")
	}
	path := ""
	if r != nil && r.URL != nil {
		path = r.URL.Path
	}
	gatewayerror.Write(w, path, requestID, status, code, message)
}
