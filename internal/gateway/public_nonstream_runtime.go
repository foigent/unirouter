package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

var (
	errPublicNonStreamRuntimeUnavailable = errors.New("gateway: public nonstream runtime unavailable")
	errPublicNonStreamContractMismatch   = errors.New("gateway: public nonstream contract mismatch")
	errPublicNonStreamRequestInvalid     = errors.New("gateway: public nonstream request invalid")
	errPublicNonStreamInputWithheld      = errors.New("gateway: public nonstream input withheld")
	errPublicNonStreamGuardFailed        = errors.New("gateway: public nonstream guard failed")
	errPublicNonStreamUpstreamFailed     = errors.New("gateway: public nonstream upstream failed")
	errPublicNonStreamHealthFailed       = errors.New("gateway: public nonstream health authority failed")
	errPublicNonStreamBillingFailed      = errors.New("gateway: public nonstream billing failed")
	errPublicNonStreamDeliveryFailed     = errors.New("gateway: public nonstream delivery failed")
)

// publicModelExecutionCoordinator deliberately omits Authorize. The public
// orchestrator can compile only against the V2-only M77 boundary and therefore
// cannot accidentally downgrade a route to the legacy/V1 coordinator entry.
type publicModelExecutionCoordinator interface {
	AuthorizePublic(context.Context, modelrouting.Plan, modelrouting.Route, modelexecution.AttemptIdentity) (modelexecution.Decision, error)
	ReleaseUndispatched(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error)
	MarkSuccess(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error)
	MarkProviderFailure(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error)
	MarkCredentialAuthFailure(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error)
	MarkCredentialRateLimited(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error)
}

// publicNonStreamBilling is intentionally narrower than BillingProvider. In
// particular, it has no BeginProviderAttempt: only Coordinator may create the
// durable V2 attempt receipt before provider I/O.
type publicNonStreamBilling interface {
	Freeze(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error)
	CompleteProviderAttempt(context.Context, billing.ProviderAttemptCompletion) error
	PrepareNonStreamDelivery(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error)
	HoldNonStreamOutput(context.Context, billing.SettleRequest, string) error
	RecordDelivery(context.Context, billing.DeliveryEvent) error
	Settle(context.Context, billing.SettleRequest) (*billing.SettleResult, error)
	Cancel(context.Context, string, int64) error
}

type publicNonStreamGuard interface {
	PublicNonStreamOutputGuard
	EvaluateInput(context.Context, endpointprotocol.PublicAdmission) (PublicOutputGuardDecision, error)
}

type allowPublicNonStreamGuard struct{}

func (allowPublicNonStreamGuard) EvaluateInput(context.Context, endpointprotocol.PublicAdmission) (PublicOutputGuardDecision, error) {
	return PublicOutputGuardDecision{Action: PublicOutputGuardAllow}, nil
}

func (allowPublicNonStreamGuard) Evaluate(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
	return PublicOutputGuardDecision{Action: PublicOutputGuardAllow}, nil
}

type publicSecretDecryptor func(ciphertext string, key []byte) (string, error)

type publicNonStreamOrchestrator struct {
	mode          modelrouting.Mode
	planner       ModelRoutePlanner
	coordinator   publicModelExecutionCoordinator
	billing       publicNonStreamBilling
	registry      *endpointprotocol.PublicNonStreamRegistry
	decrypt       publicSecretDecryptor
	encryptionKey []byte
	doer          PublicNonStreamHTTPDoer
	guard         publicNonStreamGuard
	safetyFactor  string
	minFreeze     int64
}

type publicNonStreamRequest struct {
	Operation modeloperation.ID
	Path      string
	Body      []byte
	// PublicModel is the caller-visible model identity retained in the public
	// protocol document and delivery. ResolvedModel is the server-authorized
	// canonical catalog identity used by planning, policy pinning, and billing.
	PublicModel        string
	ResolvedModel      string
	RequestID          string
	OrgID              int64
	ProjectID          int64
	APIKeyID           int64
	AccessCredentialID int64
	CredentialRef      string
	AccessGrantID      int64
}

type preparedPublicNonStreamRoute struct {
	route         modelrouting.Route
	upstream      *endpointprotocol.PublicUpstreamRequest
	inputEstimate int
}

type publicAttemptOutcome struct {
	completion billing.ProviderAttemptCompletion
	retry      bool
	health     PublicNonStreamHealthAction
}

func (orchestrator *publicNonStreamOrchestrator) validateRuntime(
	request publicNonStreamRequest,
) (endpointprotocol.PublicContractIdentity, endpointprotocol.PublicNonStreamProfile, error) {
	if orchestrator == nil || orchestrator.mode != modelrouting.ModeCanonical || orchestrator.planner == nil ||
		orchestrator.coordinator == nil || orchestrator.billing == nil || orchestrator.registry == nil ||
		orchestrator.decrypt == nil || orchestrator.doer == nil || orchestrator.guard == nil ||
		strings.TrimSpace(orchestrator.safetyFactor) == "" || orchestrator.minFreeze < 0 {
		return endpointprotocol.PublicContractIdentity{}, nil, errPublicNonStreamRuntimeUnavailable
	}
	definition, known := modeloperation.LookupID(request.Operation)
	if !known || (request.Operation != modeloperation.IDResponses && request.Operation != modeloperation.IDMessages) ||
		definition.Path != request.Path || definition.Method != http.MethodPost || !definition.Paid ||
		!modelqualification.IsQualificationRequired(string(request.Operation)) {
		return endpointprotocol.PublicContractIdentity{}, nil, errPublicNonStreamContractMismatch
	}
	identity, err := modelqualification.CompiledPublicBinding(string(request.Operation))
	if err != nil || identity.PublicPath != request.Path {
		return endpointprotocol.PublicContractIdentity{}, nil, errPublicNonStreamContractMismatch
	}
	profile, err := orchestrator.registry.Resolve(identity)
	if err != nil || profile.Descriptor().Identity != identity || profile.Descriptor().Streaming ||
		profile.Descriptor().CachePolicy != endpointprotocol.PublicCachePolicyBypass {
		return endpointprotocol.PublicContractIdentity{}, nil, fmt.Errorf("%w: registry", errPublicNonStreamContractMismatch)
	}
	return identity, profile, nil
}

func validatePublicNonStreamRequest(request publicNonStreamRequest) error {
	publicModel := strings.TrimSpace(request.PublicModel)
	resolvedModel := strings.TrimSpace(request.ResolvedModel)
	requestID := strings.TrimSpace(request.RequestID)
	credentialRef := strings.TrimSpace(request.CredentialRef)
	if publicModel == "" || publicModel != request.PublicModel || len(publicModel) > 200 ||
		resolvedModel == "" || resolvedModel != request.ResolvedModel || len(resolvedModel) > 160 ||
		requestID == "" || requestID != request.RequestID || len(requestID) > 64 ||
		credentialRef != request.CredentialRef ||
		request.OrgID <= 0 || request.APIKeyID <= 0 || len(request.Body) == 0 || request.ProjectID < 0 ||
		strings.ContainsAny(publicModel+resolvedModel+requestID, "\x00\r\n") {
		return errPublicNonStreamRequestInvalid
	}
	accessEmpty := request.AccessCredentialID == 0 && request.CredentialRef == "" && request.AccessGrantID == 0
	accessComplete := request.AccessCredentialID > 0 && request.CredentialRef != "" && request.AccessGrantID > 0
	if !accessEmpty && !accessComplete {
		return errPublicNonStreamRequestInvalid
	}
	return nil
}

func publicContractIdentityForRoute(route modelrouting.Route) (endpointprotocol.PublicContractIdentity, error) {
	if route.PublicEvidence == nil || !reflect.DeepEqual(route.Evidence, executionevidence.EvidenceV1{}) {
		return endpointprotocol.PublicContractIdentity{}, errPublicNonStreamContractMismatch
	}
	public, err := executionevidence.NormalizeV2(*route.PublicEvidence)
	if err != nil || public.PublicExecutionBindingID <= 0 {
		return endpointprotocol.PublicContractIdentity{}, errPublicNonStreamContractMismatch
	}
	base, err := public.RouteEvidenceV1()
	if err != nil || base.AuthorityMode != executionevidence.ModeCanonical {
		return endpointprotocol.PublicContractIdentity{}, errPublicNonStreamContractMismatch
	}
	return endpointprotocol.PublicContractIdentity{
		ModelOperationID:           base.ModelOperationID,
		PublicPath:                 route.PublicPath,
		ExecutorContractVersion:    route.ExecutorContractVersion,
		ProfileID:                  route.ProtocolProfileID,
		ProfileVersion:             route.ProtocolProfileVersion,
		DeliveryContractVersion:    public.DeliveryContractVersion,
		ChargeContractVersion:      public.ChargeContractVersion,
		IdempotencyContractVersion: public.IdempotencyContractVersion,
	}, nil
}

func validatePublicNonStreamPlan(
	request publicNonStreamRequest,
	plan modelrouting.Plan,
	identity endpointprotocol.PublicContractIdentity,
) error {
	if plan.AuthorityMode != modelrouting.ModeCanonical || plan.ModelOperationID != string(request.Operation) ||
		plan.PublicModel != request.ResolvedModel || plan.PublicModelID <= 0 || plan.RuntimeReleaseID <= 0 ||
		plan.SnapshotHash == "" || plan.PublicContextLimit <= 0 || plan.PublicOutputLimit <= 0 || len(plan.Routes) == 0 {
		return errPublicNonStreamContractMismatch
	}
	for _, route := range plan.Routes {
		routeIdentity, err := publicContractIdentityForRoute(route)
		if err != nil || routeIdentity != identity || modelqualification.ValidateCompiledPublicBinding(routeIdentity) != nil {
			return errPublicNonStreamContractMismatch
		}
		public, err := executionevidence.NormalizeV2(*route.PublicEvidence)
		if err != nil {
			return errPublicNonStreamContractMismatch
		}
		base, err := public.RouteEvidenceV1()
		if err != nil || base.PublicModelID != plan.PublicModelID ||
			base.ModelOperationID != plan.ModelOperationID || base.SnapshotHash != plan.SnapshotHash ||
			base.RuntimeReleaseID != plan.RuntimeReleaseID || base.RuntimeReleaseRef != plan.RuntimeReleaseRef ||
			base.RuntimeStage != plan.RuntimeStage || base.SlotVersion != plan.SlotVersion ||
			base.ProtocolProfileID != identity.ProfileID || base.ProtocolProfileVersion != identity.ProfileVersion ||
			base.RequiresProbe != route.RequiresProbe || route.ProductionApprovalID <= 0 ||
			strings.TrimSpace(route.Provider) == "" || strings.TrimSpace(route.ConnectionName) == "" {
			return errPublicNonStreamContractMismatch
		}
	}
	return nil
}

func publicAttemptID(requestID string, sequence int, bindingID int64) string {
	digest := sha256.Sum256([]byte(requestID + "\x00" + strconv.Itoa(sequence) + "\x00" + strconv.FormatInt(bindingID, 10)))
	return "att_" + hex.EncodeToString(digest[:16])
}

func publicProviderAttemptOutcome(
	request publicNonStreamRequest,
	attemptID string,
	execution PublicNonStreamExecution,
	executionErr error,
) (publicAttemptOutcome, error) {
	var failure *PublicNonStreamExecutionError
	if !errors.As(executionErr, &failure) {
		return publicAttemptOutcome{}, errPublicNonStreamUpstreamFailed
	}
	outcome := publicAttemptOutcome{
		completion: billing.ProviderAttemptCompletion{
			RequestID: request.RequestID, AttemptID: attemptID, OrgID: request.OrgID,
			HTTPStatus: failure.ProviderStatus, FailureKind: failure.FailureKind,
		},
		health: failure.HealthAction(),
	}
	switch {
	case failure.Stage == PublicNonStreamStageOutputGuardrail && execution.Delivery.Body() != nil:
		outcome.completion.Outcome = billing.ProviderAttemptOutcomeResponseAccepted
	case failure.Stage == PublicNonStreamStageProviderContract &&
		failure.ProviderStatus >= http.StatusOK && failure.ProviderStatus < http.StatusMultipleChoices:
		outcome.completion.Outcome = billing.ProviderAttemptOutcomeResponseUnusable
	case failure.Source == PublicNonStreamSourceProviderHTTP:
		outcome.completion.Outcome = billing.ProviderAttemptOutcomeRejected
		outcome.retry = failure.Retryable
		outcome.completion.RetryAuthorized = outcome.retry
	default:
		outcome.completion.Outcome = billing.ProviderAttemptOutcomeUnknown
	}
	// The attempt ledger owns a deliberately small health vocabulary. Preserve
	// transport/provider specifics in request-local trace evidence, while the
	// durable completion carries only the exact kind accepted by its terminal
	// health fence. A locally withheld, strictly parsed response is provider
	// success and therefore must not persist failure metadata.
	switch outcome.health {
	case PublicNonStreamHealthSuccess:
		outcome.completion.FailureKind = ""
	case PublicNonStreamHealthProviderFailure:
		outcome.completion.FailureKind = "channel_failure"
	}
	return outcome, nil
}

func publicSettleRequest(
	request publicNonStreamRequest,
	attemptID string,
	usage endpointprotocol.PublicCustomerUsage,
	purpose metering.UsagePurpose,
	endpoint endpointprotocol.EndpointKind,
) (billing.SettleRequest, error) {
	input, err := checkedAdmissionInt(usage.InputTokens, 0)
	if err != nil {
		return billing.SettleRequest{}, errPublicNonStreamBillingFailed
	}
	cacheRead, err := checkedAdmissionInt(usage.CacheReadTokens, 0)
	if err != nil {
		return billing.SettleRequest{}, errPublicNonStreamBillingFailed
	}
	cacheWrite, err := checkedAdmissionInt(usage.CacheWriteTokens, 0)
	if err != nil {
		return billing.SettleRequest{}, errPublicNonStreamBillingFailed
	}
	output, err := checkedAdmissionInt(usage.OutputTokens, 0)
	if err != nil || cacheRead+cacheWrite > input {
		return billing.SettleRequest{}, errPublicNonStreamBillingFailed
	}
	schema, status := publicCacheUsageIdentity(endpoint, cacheRead, cacheWrite)
	return billing.SettleRequest{
		RequestID: request.RequestID, AttemptID: attemptID, OrgID: request.OrgID,
		PromptTokens: input, UncachedPromptTokens: input - cacheRead - cacheWrite,
		CacheReadTokens: cacheRead, CacheCreationTokens: cacheWrite, CompletionTokens: output,
		UsageSource: metering.UsageSourceUpstream, UsageAuthority: metering.UsageAuthorityProviderReported,
		UsagePurpose: purpose, UsageFinality: metering.UsageFinalityFinal,
		UsageContractVersion: metering.UsageContractVersion,
		CacheUsageSchema:     schema, CacheUsageStatus: status,
	}, nil
}

func publicCacheUsageIdentity(endpoint endpointprotocol.EndpointKind, read, write int) (string, string) {
	if read == 0 && write == 0 {
		return "", adapters.CacheUsageStatusNotReported
	}
	schema := adapters.CacheUsageSchemaOpenAIV2
	if endpoint == endpointprotocol.EndpointMessages {
		schema = adapters.CacheUsageSchemaAnthropicV1
	}
	switch {
	case read > 0 && write > 0:
		return schema, adapters.CacheUsageStatusReadWriteReported
	case read > 0:
		return schema, adapters.CacheUsageStatusReadReported
	default:
		return schema, adapters.CacheUsageStatusWriteReported
	}
}

func publicHealthResult(result modelexecution.HealthResult, err error) error {
	if err != nil {
		return fmt.Errorf("%w: %v", errPublicNonStreamHealthFailed, err)
	}
	if result.Status != modelexecution.Applied {
		return fmt.Errorf("%w: terminal status %s", errPublicNonStreamHealthFailed, result.Status)
	}
	return nil
}

type publicResponseStartedError struct{ err error }

func (failure *publicResponseStartedError) Error() string {
	if failure == nil || failure.err == nil {
		return errPublicNonStreamDeliveryFailed.Error()
	}
	return failure.err.Error()
}

func (failure *publicResponseStartedError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.err
}

func publicResponseWasStarted(err error) bool {
	var started *publicResponseStartedError
	return errors.As(err, &started)
}

func (orchestrator *publicNonStreamOrchestrator) execute(
	ctx context.Context,
	w http.ResponseWriter,
	request publicNonStreamRequest,
) error {
	if w == nil {
		return errPublicNonStreamRequestInvalid
	}
	if err := validatePublicNonStreamRequest(request); err != nil {
		return err
	}
	identity, profile, err := orchestrator.validateRuntime(request)
	if err != nil {
		return err
	}
	prepared, err := profile.PrepareRequest(request.Body, request.PublicModel)
	if err != nil {
		return fmt.Errorf("%w: %v", errPublicNonStreamRequestInvalid, err)
	}
	prepared, err = orchestrator.applyInputGuard(ctx, profile, prepared, request.PublicModel)
	if err != nil {
		return err
	}
	admission := prepared.Admission()
	inputEstimate, err := estimateProviderJSONAdmission(prepared.CanonicalBody())
	if err != nil {
		return fmt.Errorf("%w: request estimate", errPublicNonStreamRequestInvalid)
	}
	outputEstimate, err := checkedAdmissionInt(admission.ExplicitMaxOutputTokens, 0)
	if err != nil || outputEstimate <= 0 {
		return fmt.Errorf("%w: output estimate", errPublicNonStreamRequestInvalid)
	}

	plan, err := orchestrator.planner.Plan(ctx, modelrouting.Request{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: request.OrgID,
		PublicModel: request.ResolvedModel, ModelOperationID: string(request.Operation),
		SelectionKey: request.RequestID,
	})
	if err != nil {
		return fmt.Errorf("%w: plan", errPublicNonStreamUpstreamFailed)
	}
	if err := validatePublicNonStreamPlan(request, plan, identity); err != nil {
		return err
	}
	routes, maximumInput, err := orchestrator.prepareRoutes(
		ctx, request, plan, identity, prepared, inputEstimate.InputTokens, outputEstimate,
	)
	if err != nil {
		return err
	}
	estimatedTokens, err := checkedAdmissionInt(int64(maximumInput), int64(outputEstimate))
	if err != nil {
		return fmt.Errorf("%w: freeze estimate", errPublicNonStreamRequestInvalid)
	}
	freeze, err := orchestrator.billing.Freeze(ctx, billing.FreezeRequest{
		RequestID: request.RequestID, OrgID: request.OrgID, ProjectID: request.ProjectID,
		APIKeyID: request.APIKeyID, AccessCredentialID: request.AccessCredentialID,
		CredentialRef: request.CredentialRef, AccessGrantID: request.AccessGrantID,
		Model: request.ResolvedModel, EstimatedTokens: estimatedTokens,
		EstimatedInputTokens: maximumInput, ReservedCompletionTokens: outputEstimate,
		SafetyFactor: orchestrator.safetyFactor, MinFreezeCents: orchestrator.minFreeze,
	})
	if err != nil || freeze == nil || !freeze.Success {
		return fmt.Errorf("%w: freeze", errPublicNonStreamBillingFailed)
	}
	executor, err := NewPublicNonStreamExecutor(orchestrator.doer, orchestrator.guard)
	if err != nil {
		return orchestrator.cancelFrozen(ctx, request, fmt.Errorf("%w: executor", errPublicNonStreamRuntimeUnavailable))
	}

	var lastFailure error
	for index := range routes {
		candidate := routes[index]
		bindingID := candidate.route.PublicEvidence.PublicExecutionBindingID
		attemptID := publicAttemptID(request.RequestID, index+1, bindingID)
		decision, authorizeErr := orchestrator.coordinator.AuthorizePublic(ctx, plan, candidate.route, modelexecution.AttemptIdentity{
			RequestID: request.RequestID, AttemptID: attemptID, OrgID: request.OrgID,
			APIKeyID: request.APIKeyID, AccessCredentialID: request.AccessCredentialID,
			CredentialRef: request.CredentialRef, AccessGrantID: request.AccessGrantID,
			RouteSequence: index + 1, RouteTotal: len(routes), Endpoint: request.Path,
		})
		if authorizeErr != nil {
			return orchestrator.cancelFrozen(ctx, request, fmt.Errorf("%w: authorize", errPublicNonStreamRuntimeUnavailable))
		}
		if decision.Status != modelexecution.Applied {
			lastFailure = errPublicNonStreamUpstreamFailed
			continue
		}

		execution, executionErr := executor.Execute(ctx, candidate.upstream)
		if executionErr != nil {
			var failure *PublicNonStreamExecutionError
			if errors.As(executionErr, &failure) && failure.Stage == PublicNonStreamStagePreflight {
				health, healthErr := orchestrator.coordinator.ReleaseUndispatched(context.WithoutCancel(ctx), decision)
				if err := publicHealthResult(health, healthErr); err != nil {
					return orchestrator.cancelFrozen(ctx, request, err)
				}
				lastFailure = executionErr
				continue
			}
			outcome, outcomeErr := publicProviderAttemptOutcome(request, attemptID, execution, executionErr)
			if outcomeErr != nil {
				return orchestrator.cancelFrozen(ctx, request, outcomeErr)
			}
			if err := orchestrator.billing.CompleteProviderAttempt(context.WithoutCancel(ctx), outcome.completion); err != nil {
				return orchestrator.cancelFrozen(ctx, request, fmt.Errorf("%w: complete attempt", errPublicNonStreamBillingFailed))
			}
			if err := orchestrator.applyTerminalHealth(context.WithoutCancel(ctx), decision, outcome.health); err != nil {
				switch outcome.completion.Outcome {
				case billing.ProviderAttemptOutcomeResponseAccepted:
					return orchestrator.retainAfterHealthFailure(ctx, request, attemptID, execution, err)
				case billing.ProviderAttemptOutcomeRejected:
					// A durable rejection carries no provider liability. Fail closed
					// on the health fence and release the frozen reservation.
					return orchestrator.cancelFrozen(ctx, request, errors.Join(err, executionErr))
				case billing.ProviderAttemptOutcomeUnknown, billing.ProviderAttemptOutcomeResponseUnusable:
					// CompleteProviderAttempt has moved the reservation to
					// settlement_pending. Cancel would falsely refund an unresolved
					// provider liability and is therefore forbidden.
					return errors.Join(err, errPublicNonStreamUpstreamFailed, executionErr)
				default:
					return errors.Join(errPublicNonStreamBillingFailed, err)
				}
			}
			if outcome.completion.Outcome == billing.ProviderAttemptOutcomeResponseAccepted {
				hold, holdErr := publicSettleRequest(
					request, attemptID, execution.Delivery.CustomerUsage(),
					metering.UsagePurposeProviderEvidence, execution.Delivery.EndpointKind(),
				)
				if holdErr != nil {
					return holdErr
				}
				if err := orchestrator.billing.HoldNonStreamOutput(
					context.WithoutCancel(ctx), hold, billing.OutputHoldReasonGuardrailWithheld,
				); err != nil {
					return fmt.Errorf("%w: hold guardrail output", errPublicNonStreamBillingFailed)
				}
				return errors.Join(errPublicNonStreamGuardFailed, executionErr)
			}
			switch outcome.completion.Outcome {
			case billing.ProviderAttemptOutcomeRejected:
				lastFailure = executionErr
				if outcome.retry {
					continue
				}
				break
			case billing.ProviderAttemptOutcomeUnknown, billing.ProviderAttemptOutcomeResponseUnusable:
				// These are durable settlement-pending states. Do not try a
				// second provider and do not cancel the reservation.
				return errors.Join(errPublicNonStreamUpstreamFailed, executionErr)
			default:
				return errors.Join(errPublicNonStreamBillingFailed, executionErr)
			}
			break
		}

		completion := billing.ProviderAttemptCompletion{
			RequestID: request.RequestID, AttemptID: attemptID, OrgID: request.OrgID,
			Outcome: billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: http.StatusOK,
		}
		if err := orchestrator.billing.CompleteProviderAttempt(context.WithoutCancel(ctx), completion); err != nil {
			return orchestrator.cancelFrozen(ctx, request, fmt.Errorf("%w: complete accepted attempt", errPublicNonStreamBillingFailed))
		}
		if err := orchestrator.applyTerminalHealth(
			context.WithoutCancel(ctx), decision, PublicNonStreamHealthSuccess,
		); err != nil {
			return orchestrator.retainAfterHealthFailure(ctx, request, attemptID, execution, err)
		}
		return orchestrator.deliver(context.WithoutCancel(ctx), w, request, attemptID, execution)
	}
	if lastFailure == nil {
		lastFailure = errPublicNonStreamUpstreamFailed
	}
	return orchestrator.cancelFrozen(ctx, request, fmt.Errorf("%w: all candidates failed", errPublicNonStreamUpstreamFailed))
}

func (orchestrator *publicNonStreamOrchestrator) applyInputGuard(
	ctx context.Context,
	profile endpointprotocol.PublicNonStreamProfile,
	prepared *endpointprotocol.PublicPreparedRequest,
	publicModel string,
) (*endpointprotocol.PublicPreparedRequest, error) {
	decision, err := orchestrator.guard.EvaluateInput(ctx, prepared.Admission())
	if err != nil {
		return nil, fmt.Errorf("%w: input evaluation", errPublicNonStreamGuardFailed)
	}
	switch decision.Action {
	case PublicOutputGuardAllow:
		if len(decision.Replacements) != 0 {
			return nil, fmt.Errorf("%w: invalid input decision", errPublicNonStreamGuardFailed)
		}
	case PublicOutputGuardReplace:
		prepared, err = prepared.WithTexts(decision.Replacements)
		if err != nil {
			return nil, fmt.Errorf("%w: input replacement", errPublicNonStreamGuardFailed)
		}
	case PublicOutputGuardBlock:
		return nil, errPublicNonStreamInputWithheld
	default:
		return nil, fmt.Errorf("%w: invalid input action", errPublicNonStreamGuardFailed)
	}
	// Always cross the strict parser again, even for an allow decision. This
	// prevents the guard integration from becoming an alternate request codec.
	reprepared, err := profile.PrepareRequest(prepared.CanonicalBody(), publicModel)
	if err != nil {
		return nil, fmt.Errorf("%w: transformed request", errPublicNonStreamGuardFailed)
	}
	return reprepared, nil
}

func (orchestrator *publicNonStreamOrchestrator) prepareRoutes(
	ctx context.Context,
	request publicNonStreamRequest,
	plan modelrouting.Plan,
	identity endpointprotocol.PublicContractIdentity,
	prepared *endpointprotocol.PublicPreparedRequest,
	inputEstimate int,
	outputEstimate int,
) ([]preparedPublicNonStreamRoute, int, error) {
	maximumInput := inputEstimate
	routes := make([]preparedPublicNonStreamRoute, 0, len(plan.Routes))
	for _, route := range plan.Routes {
		routeIdentity, err := publicContractIdentityForRoute(route)
		if err != nil || routeIdentity != identity {
			return nil, 0, errPublicNonStreamContractMismatch
		}
		profile, err := orchestrator.registry.Resolve(routeIdentity)
		if err != nil {
			return nil, 0, errPublicNonStreamContractMismatch
		}
		secret, err := orchestrator.decrypt(route.EncryptedSecret, orchestrator.encryptionKey)
		if err != nil || strings.TrimSpace(secret) == "" {
			continue
		}
		upstream, err := profile.BuildUpstream(ctx, prepared, endpointprotocol.PublicUpstreamInput{
			BaseURL: route.BaseURL, APIKey: secret, RequestID: request.RequestID, MappedModel: route.UpstreamModel,
		})
		if err != nil {
			continue
		}
		providerRequest, err := upstream.NewHTTPRequest(ctx)
		if err != nil {
			continue
		}
		providerBody, err := providerRequestBody(providerRequest)
		if err != nil {
			continue
		}
		estimate, err := estimateProviderJSONAdmission(providerBody)
		if err != nil {
			continue
		}
		limits := modelExecutionRequestLimits{
			EstimatedInputTokens: estimate.InputTokens, EstimatedOutputTokens: outputEstimate,
			EstimateVersion: estimate.Version,
		}
		if err := limits.validatePublic(plan); err != nil || !limits.routeFits(plan, route) {
			continue
		}
		maximumInput = max(maximumInput, estimate.InputTokens)
		routes = append(routes, preparedPublicNonStreamRoute{route: route, upstream: upstream, inputEstimate: estimate.InputTokens})
	}
	if len(routes) == 0 {
		return nil, 0, fmt.Errorf("%w: no locally executable candidate", errPublicNonStreamUpstreamFailed)
	}
	maximumLimits := modelExecutionRequestLimits{EstimatedInputTokens: maximumInput, EstimatedOutputTokens: outputEstimate}
	if err := maximumLimits.validatePublic(plan); err != nil {
		return nil, 0, fmt.Errorf("%w: plan limit", errPublicNonStreamRequestInvalid)
	}
	return routes, maximumInput, nil
}

func (orchestrator *publicNonStreamOrchestrator) applyTerminalHealth(
	ctx context.Context,
	decision modelexecution.Decision,
	action PublicNonStreamHealthAction,
) error {
	var result modelexecution.HealthResult
	var err error
	switch action {
	case PublicNonStreamHealthNone:
		return nil
	case PublicNonStreamHealthSuccess:
		result, err = orchestrator.coordinator.MarkSuccess(ctx, decision)
	case PublicNonStreamHealthProviderFailure:
		result, err = orchestrator.coordinator.MarkProviderFailure(ctx, decision)
	case PublicNonStreamHealthCredentialAuthFailure:
		result, err = orchestrator.coordinator.MarkCredentialAuthFailure(ctx, decision)
	case PublicNonStreamHealthCredentialRateLimited:
		result, err = orchestrator.coordinator.MarkCredentialRateLimited(ctx, decision)
	default:
		return errPublicNonStreamHealthFailed
	}
	return publicHealthResult(result, err)
}

func (orchestrator *publicNonStreamOrchestrator) retainAfterHealthFailure(
	ctx context.Context,
	request publicNonStreamRequest,
	attemptID string,
	execution PublicNonStreamExecution,
	healthErr error,
) error {
	if execution.Delivery.Body() == nil {
		return healthErr
	}
	hold, err := publicSettleRequest(
		request, attemptID, execution.Delivery.CustomerUsage(),
		metering.UsagePurposeProviderEvidence, execution.Delivery.EndpointKind(),
	)
	if err != nil {
		return errors.Join(healthErr, err)
	}
	if err := orchestrator.billing.HoldNonStreamOutput(
		context.WithoutCancel(ctx), hold, billing.OutputHoldReasonProviderAttemptUnresolved,
	); err != nil {
		return errors.Join(healthErr, fmt.Errorf("%w: hold unresolved health", errPublicNonStreamBillingFailed))
	}
	return healthErr
}

func (orchestrator *publicNonStreamOrchestrator) cancelFrozen(
	ctx context.Context,
	request publicNonStreamRequest,
	cause error,
) error {
	if err := orchestrator.billing.Cancel(context.WithoutCancel(ctx), request.RequestID, request.OrgID); err != nil {
		return errors.Join(cause, fmt.Errorf("%w: cancel frozen reservation", errPublicNonStreamBillingFailed))
	}
	return cause
}

func (orchestrator *publicNonStreamOrchestrator) deliver(
	ctx context.Context,
	w http.ResponseWriter,
	request publicNonStreamRequest,
	attemptID string,
	execution PublicNonStreamExecution,
) error {
	body := execution.Delivery.Body()
	if execution.StatusCode != http.StatusOK || len(body) == 0 || execution.Delivery.CachePolicy() != endpointprotocol.PublicCachePolicyBypass {
		return errPublicNonStreamDeliveryFailed
	}
	settle, err := publicSettleRequest(
		request, attemptID, execution.Delivery.CustomerUsage(),
		metering.UsagePurposeCustomerCharge, execution.Delivery.EndpointKind(),
	)
	if err != nil {
		return err
	}
	prepared, err := orchestrator.billing.PrepareNonStreamDelivery(ctx, settle)
	if err != nil || prepared == nil || !prepared.Funded {
		return fmt.Errorf("%w: prepare delivery", errPublicNonStreamBillingFailed)
	}
	event := billing.DeliveryEvent{
		RequestID: request.RequestID, AttemptID: attemptID, OrgID: request.OrgID,
		Mode: billing.DeliveryModeNonStream, Outcome: billing.DeliveryOutcomeWriteStarted,
		HTTPStatus: http.StatusOK,
	}
	if err := orchestrator.billing.RecordDelivery(ctx, event); err != nil {
		return fmt.Errorf("%w: record write start", errPublicNonStreamBillingFailed)
	}
	w.Header().Set("Content-Type", execution.Delivery.ContentType())
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	written, writeErr := w.Write(body)
	if writeErr != nil || written != len(body) {
		event.Outcome = billing.DeliveryOutcomeWriteFailed
		event.DeliveredBytes = int64(written)
		event.ErrorCode = "downstream_write_failed"
		recordErr := orchestrator.billing.RecordDelivery(ctx, event)
		return &publicResponseStartedError{err: errors.Join(
			fmt.Errorf("%w: downstream write", errPublicNonStreamDeliveryFailed), recordErr,
		)}
	}
	event.Outcome = billing.DeliveryOutcomeCompleted
	event.DeliveredBytes = int64(written)
	event.DeliveredChunks = 1
	if err := orchestrator.billing.RecordDelivery(ctx, event); err != nil {
		return &publicResponseStartedError{err: fmt.Errorf("%w: record completed delivery", errPublicNonStreamBillingFailed)}
	}
	settled, err := orchestrator.billing.Settle(ctx, settle)
	if err != nil || settled == nil || !settled.Success {
		return &publicResponseStartedError{err: fmt.Errorf("%w: settle delivered response", errPublicNonStreamBillingFailed)}
	}
	return nil
}
