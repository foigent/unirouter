package gateway

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/streampolicy"
	"github.com/uniroute/unirouter/internal/tokenutil"
)

type publicStreamEvent interface {
	Event() string
	Texts() []endpointprotocol.PublicText
	CustomerUsage() endpointprotocol.PublicCustomerUsage
	Terminal() bool
	Success() bool
	Body() []byte
	CommitTexts([]string) error
}

// publicStreamBinding carries the protocol-specific parser and economic/audit
// identity for one exact request-bound stream. The shared transport must never
// infer these values from an event name or reuse Responses reasons for another
// operation.
type publicStreamBinding struct {
	parseEvent        func(string, []byte) (publicStreamEvent, error)
	textEventName     string
	endpointKind      endpointprotocol.EndpointKind
	interruptedCode   string
	interruptedReason string
	completedReason   string
}

type publicStreamBindingFactory func(*endpointprotocol.PublicUpstreamRequest) (publicStreamBinding, error)

func newPublicResponsesStreamBinding(upstream *endpointprotocol.PublicUpstreamRequest) (publicStreamBinding, error) {
	stream, err := upstream.NewResponsesStream()
	if err != nil {
		return publicStreamBinding{}, err
	}
	return publicStreamBinding{
		parseEvent: func(name string, data []byte) (publicStreamEvent, error) {
			return stream.ParseEvent(name, data)
		},
		textEventName:     "response.output_text.delta",
		endpointKind:      endpointprotocol.EndpointResponses,
		interruptedCode:   "public_responses_stream_interrupted",
		interruptedReason: "public_responses_stream_interrupted",
		completedReason:   "response_completed",
	}, nil
}

func newPublicAnthropicMessagesStreamBinding(upstream *endpointprotocol.PublicUpstreamRequest) (publicStreamBinding, error) {
	stream, err := upstream.NewAnthropicMessagesStream()
	if err != nil {
		return publicStreamBinding{}, err
	}
	return publicStreamBinding{
		parseEvent: func(name string, data []byte) (publicStreamEvent, error) {
			return stream.ParseEvent(name, data)
		},
		textEventName:     "content_block_delta",
		endpointKind:      endpointprotocol.EndpointMessages,
		interruptedCode:   "public_messages_stream_interrupted",
		interruptedReason: "public_messages_stream_interrupted",
		completedReason:   "message_completed",
	}, nil
}

func (binding publicStreamBinding) valid() bool {
	return binding.parseEvent != nil && binding.textEventName != "" && binding.endpointKind != "" &&
		binding.interruptedCode != "" && binding.interruptedReason != "" && binding.completedReason != ""
}

type publicStreamBilling interface {
	publicNonStreamBilling
	BeginStreamDelivery(context.Context, billing.StreamDeliveryStart) error
	RenewStreamDelivery(context.Context, billing.StreamDeliveryStart) error
	CheckpointStreamDelivery(context.Context, billing.StreamDeliveryCheckpoint) (*billing.StreamCheckpointResult, error)
	FinishStreamDelivery(context.Context, billing.StreamDeliveryStart, string, string) error
	PersistSettlementUsage(context.Context, billing.SettleRequest) error
}

type publicStreamOrchestrator struct {
	core         *publicNonStreamOrchestrator
	billing      publicStreamBilling
	pinned       *guardrail.Pinned
	streamPolicy streampolicy.Config
	relay        *Relay
	bindStream   publicStreamBindingFactory
}

type publicStreamAttemptResult struct {
	started          bool
	attemptCompleted bool
	healthAttempted  bool
	completionFailed bool
	providerState    string
	httpStatus       int
	failureKind      string
	retryable        bool
	health           PublicNonStreamHealthAction
	err              error
}

const (
	publicStreamProviderAccepted = "accepted"
	publicStreamProviderRejected = "rejected"
	publicStreamProviderUnknown  = "unknown"
	publicStreamProviderUnusable = "unusable"
)

func (orchestrator *publicStreamOrchestrator) execute(
	ctx context.Context,
	w http.ResponseWriter,
	request publicNonStreamRequest,
) error {
	if orchestrator == nil || orchestrator.core == nil || orchestrator.billing == nil ||
		orchestrator.bindStream == nil || w == nil {
		return errPublicNonStreamRuntimeUnavailable
	}
	if _, ok := w.(http.Flusher); !ok {
		return errPublicNonStreamDeliveryFailed
	}
	if err := orchestrator.streamPolicy.Validate(); err != nil {
		return errPublicNonStreamRuntimeUnavailable
	}
	preparation, err := orchestrator.core.prepare(ctx, request, true)
	if err != nil {
		return err
	}
	var lastFailure error
	for index := range preparation.routes {
		candidate := preparation.routes[index]
		bindingID := candidate.route.PublicEvidence.PublicExecutionBindingID
		attemptID := publicAttemptID(request.RequestID, index+1, bindingID)
		decision, authorizeErr := orchestrator.core.coordinator.AuthorizePublic(
			ctx, preparation.plan, candidate.route, modelexecution.AttemptIdentity{
				RequestID: request.RequestID, AttemptID: attemptID, OrgID: request.OrgID,
				APIKeyID: request.APIKeyID, AccessCredentialID: request.AccessCredentialID,
				CredentialRef: request.CredentialRef, AccessGrantID: request.AccessGrantID,
				RouteSequence: index + 1, RouteTotal: len(preparation.routes), Endpoint: request.Path,
			},
		)
		if authorizeErr != nil {
			return orchestrator.core.cancelFrozen(ctx, request, fmt.Errorf("%w: authorize", errPublicNonStreamRuntimeUnavailable))
		}
		if decision.Status != modelexecution.Applied {
			lastFailure = errPublicNonStreamUpstreamFailed
			continue
		}
		result := orchestrator.executeAttempt(ctx, w, request, preparation, candidate, attemptID, decision)
		if result.err == nil {
			return nil
		}
		if result.completionFailed {
			if result.started {
				return &publicResponseStartedError{err: result.err}
			}
			return result.err
		}
		if result.providerState == "undispatched" {
			health, healthErr := orchestrator.core.coordinator.ReleaseUndispatched(context.WithoutCancel(ctx), decision)
			if err := publicHealthResult(health, healthErr); err != nil {
				return orchestrator.core.cancelFrozen(ctx, request, err)
			}
			lastFailure = result.err
			continue
		}
		if !result.attemptCompleted {
			if completeErr := orchestrator.completeAttempt(
				context.WithoutCancel(ctx), request, attemptID, result,
			); completeErr != nil {
				if result.started {
					return &publicResponseStartedError{err: completeErr}
				}
				// Dispatch has occurred but its terminal receipt was not saved.
				// Retain the reservation for recovery; never infer a safe refund.
				return completeErr
			}
		}
		if !result.healthAttempted {
			if healthErr := orchestrator.core.applyTerminalHealth(
				context.WithoutCancel(ctx), decision, result.health,
			); healthErr != nil {
				if result.started || result.providerState == publicStreamProviderUnknown ||
					result.providerState == publicStreamProviderUnusable {
					return &publicResponseStartedError{err: errors.Join(result.err, healthErr)}
				}
				return orchestrator.core.cancelFrozen(ctx, request, errors.Join(result.err, healthErr))
			}
		}
		if result.started {
			return &publicResponseStartedError{err: result.err}
		}
		if result.providerState == publicStreamProviderRejected && result.retryable {
			lastFailure = result.err
			continue
		}
		if result.providerState == publicStreamProviderRejected {
			return orchestrator.core.cancelFrozen(ctx, request, result.err)
		}
		// A dispatched attempt with unknown or unusable provider liability has
		// already moved to settlement_pending. Cancellation or failover could
		// create a false refund or double supplier cost.
		return result.err
	}
	if lastFailure == nil {
		lastFailure = errPublicNonStreamUpstreamFailed
	}
	return orchestrator.core.cancelFrozen(ctx, request, fmt.Errorf("%w: all candidates failed", lastFailure))
}

func (orchestrator *publicStreamOrchestrator) completeAttempt(
	ctx context.Context,
	request publicNonStreamRequest,
	attemptID string,
	result publicStreamAttemptResult,
) error {
	outcome := billing.ProviderAttemptOutcomeUnknown
	switch result.providerState {
	case publicStreamProviderRejected:
		outcome = billing.ProviderAttemptOutcomeRejected
	case publicStreamProviderUnusable:
		outcome = billing.ProviderAttemptOutcomeResponseUnusable
	case publicStreamProviderAccepted:
		outcome = billing.ProviderAttemptOutcomeResponseAccepted
	}
	completion := billing.ProviderAttemptCompletion{
		RequestID: request.RequestID, AttemptID: attemptID, OrgID: request.OrgID,
		Outcome: outcome, HTTPStatus: result.httpStatus, FailureKind: result.failureKind,
		RetryAuthorized: result.retryable && outcome == billing.ProviderAttemptOutcomeRejected,
	}
	if outcome == billing.ProviderAttemptOutcomeResponseAccepted {
		completion.FailureKind = ""
	} else if result.health == PublicNonStreamHealthProviderFailure {
		// Match the existing non-stream ledger's bounded health vocabulary.
		completion.FailureKind = "channel_failure"
	}
	if err := orchestrator.billing.CompleteProviderAttempt(ctx, completion); err != nil {
		return fmt.Errorf("%w: complete stream attempt", errPublicNonStreamBillingFailed)
	}
	return nil
}

func (orchestrator *publicStreamOrchestrator) executeAttempt(
	ctx context.Context,
	w http.ResponseWriter,
	request publicNonStreamRequest,
	preparation publicExecutionPreparation,
	candidate preparedPublicNonStreamRoute,
	attemptID string,
	decision modelexecution.Decision,
) publicStreamAttemptResult {
	streamCtx, streamCancel := context.WithTimeout(ctx, orchestrator.streamPolicy.MaxDuration())
	defer streamCancel()
	httpRequest, err := candidate.upstream.NewHTTPRequest(streamCtx)
	if err != nil {
		return publicStreamAttemptResult{providerState: "undispatched", err: fmt.Errorf("%w: request", errPublicNonStreamRuntimeUnavailable)}
	}
	stream, err := orchestrator.bindStream(candidate.upstream)
	if err != nil {
		return publicStreamAttemptResult{providerState: "undispatched", err: fmt.Errorf("%w: stream binding", errPublicNonStreamContractMismatch)}
	}
	response, err := orchestrator.core.doer.Do(httpRequest)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		source, failureKind, _, sanitized := classifyPublicNonStreamTransport(ctx, err)
		health := PublicNonStreamHealthProviderFailure
		if source == PublicNonStreamSourceClient {
			health = PublicNonStreamHealthNone
		}
		return publicStreamAttemptResult{
			providerState: publicStreamProviderUnknown, failureKind: failureKind, health: health,
			err: errors.Join(errPublicNonStreamUpstreamFailed, sanitized),
		}
	}
	if response == nil || response.Body == nil {
		return publicStreamAttemptResult{
			providerState: publicStreamProviderUnknown, failureKind: "connection",
			health: PublicNonStreamHealthProviderFailure, err: errPublicNonStreamUpstreamFailed,
		}
	}
	defer response.Body.Close()
	status := sanitizePublicProviderStatus(response.StatusCode)
	if status != http.StatusOK {
		failureKind, retryable := candidate.upstream.ClassifyHTTPStatus(status)
		health := PublicNonStreamHealthProviderFailure
		switch failureKind {
		case "auth":
			health = PublicNonStreamHealthCredentialAuthFailure
		case "rate_limit":
			health = PublicNonStreamHealthCredentialRateLimited
		}
		return publicStreamAttemptResult{
			providerState: publicStreamProviderRejected, failureKind: failureKind, retryable: retryable,
			httpStatus: status, health: health, err: errPublicNonStreamUpstreamFailed,
		}
	}
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "text/event-stream" {
		return publicStreamAttemptResult{
			providerState: publicStreamProviderUnusable, failureKind: "invalid_response",
			httpStatus: status, health: PublicNonStreamHealthProviderFailure, err: errPublicNonStreamUpstreamFailed,
		}
	}
	// This records receipt of usable HTTP headers, not terminal provider success
	// or known usage. Existing Billing requires it before opening delivery.
	if err := orchestrator.completeAttempt(context.WithoutCancel(ctx), request, attemptID, publicStreamAttemptResult{
		providerState: publicStreamProviderAccepted, httpStatus: status,
	}); err != nil {
		return publicStreamAttemptResult{completionFailed: true, err: err}
	}

	run := &publicStreamRun{
		orchestrator: orchestrator, request: request, preparation: preparation,
		attemptID: attemptID, decision: decision, stream: stream, writer: w, body: response.Body,
		guardProfile: orchestrator.pinned.StreamProfile(), guardHistory: make(map[string]string),
		advisoryHistory: make(map[string]string),
		clientContext:   ctx,
	}
	if orchestrator.pinned == nil {
		run.guardProfile = guardrail.StreamProfile{PolicySetVersion: "unavailable", Enforced: true}
	}
	err = run.execute(streamCtx)
	if err != nil {
		state := publicStreamProviderUnusable
		// A 200 response was received even if its stream later failed. Keep
		// that observed status and unknown liability as response_unusable;
		// outcome_unknown is reserved for transport without a usable status.
		if run.attemptCompleted {
			state = publicStreamProviderAccepted
		}
		return publicStreamAttemptResult{
			started: run.started, attemptCompleted: run.attemptCompleted, healthAttempted: run.healthAttempted,
			completionFailed: run.completionFailed,
			providerState:    state, httpStatus: status, failureKind: "invalid_response",
			health: run.providerHealth, err: err,
		}
	}
	return publicStreamAttemptResult{}
}

type publicStreamRun struct {
	orchestrator     *publicStreamOrchestrator
	request          publicNonStreamRequest
	preparation      publicExecutionPreparation
	attemptID        string
	decision         modelexecution.Decision
	stream           publicStreamBinding
	writer           http.ResponseWriter
	body             io.Reader
	guardProfile     guardrail.StreamProfile
	guardHistory     map[string]string
	advisoryHistory  map[string]string
	started          bool
	deliveredBytes   int64
	deliveredChunks  int
	completionTokens int
	partialRecorded  bool
	lease            billing.StreamDeliveryStart
	fence            billing.StreamLeaseFence
	streamWriter     *sseStreamWriter
	providerHealth   PublicNonStreamHealthAction
	attemptCompleted bool
	healthAttempted  bool
	completionFailed bool
	clientContext    context.Context
	lastCheckpointAt time.Time
	checkpointBytes  int64
	checkpointTokens int
}

type publicStreamScannedLine struct {
	line string
	err  error
	done bool
}

func (run *publicStreamRun) execute(ctx context.Context) (returnErr error) {
	if run == nil || run.orchestrator == nil || !run.stream.valid() || run.writer == nil || run.body == nil {
		return errPublicNonStreamRuntimeUnavailable
	}
	run.providerHealth = PublicNonStreamHealthNone
	run.lease = billing.StreamDeliveryStart{
		RequestID: run.request.RequestID, AttemptID: run.attemptID, OwnerID: processStreamLeaseOwner,
		OrgID: run.request.OrgID, Generation: streampolicy.InitialLeaseGeneration,
		LeaseTTL: streampolicy.CheckpointLeaseDuration,
	}
	run.fence = run.lease.Fence()
	run.streamWriter = newSSEStreamWriter(run.writer, run.orchestrator.streamPolicy.WriteIdleTimeout())
	defer func() {
		if returnErr == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Persist provider uncertainty and its health fence before the delivery
		// projection. Interrupted delivery must not convert unknown usage into
		// delivery_pending/known_usage or make a later health claim stale.
		if !run.attemptCompleted {
			err := run.orchestrator.completeAttempt(ctx, run.request, run.attemptID, publicStreamAttemptResult{
				providerState: publicStreamProviderUnusable, httpStatus: http.StatusOK,
				failureKind: "invalid_response", health: run.providerHealth,
			})
			if err != nil {
				run.completionFailed = true
				returnErr = errors.Join(returnErr, err)
			} else {
				run.attemptCompleted = true
			}
		}
		if !run.completionFailed && !run.healthAttempted {
			run.healthAttempted = true
			returnErr = errors.Join(returnErr, run.orchestrator.core.applyTerminalHealth(ctx, run.decision, run.providerHealth))
		}
		if !run.started {
			return
		}
		if !run.completionFailed {
			returnErr = errors.Join(returnErr, run.orchestrator.billing.RecordDelivery(ctx, billing.DeliveryEvent{
				RequestID: run.request.RequestID, AttemptID: run.attemptID, OrgID: run.request.OrgID,
				Mode: billing.DeliveryModeStream, Outcome: billing.DeliveryOutcomeInterrupted,
				DeliveredBytes: run.deliveredBytes, DeliveredChunks: run.deliveredChunks,
				HTTPStatus: http.StatusOK, ErrorCode: run.stream.interruptedCode, StreamFence: &run.fence,
			}))
		}
		returnErr = errors.Join(returnErr, run.orchestrator.billing.FinishStreamDelivery(
			ctx, run.lease, billing.StreamLeaseStateInterrupted, run.stream.interruptedReason,
		))
	}()

	streamCtx := ctx
	lines := make(chan publicStreamScannedLine)
	go scanPublicStreamLines(streamCtx, run.body, run.orchestrator.streamPolicy.MaxSSELineBytes, lines)
	heartbeat := time.NewTicker(run.orchestrator.streamPolicy.HeartbeatInterval())
	defer heartbeat.Stop()

	var eventName, eventData string
	var hasEvent, hasData bool
	var pending []publicStreamEvent
	pendingBytes := 0
	var bufferTimer *time.Timer
	var bufferTimerC <-chan time.Time
	stopBufferTimer := func() {
		if bufferTimer == nil {
			return
		}
		if !bufferTimer.Stop() {
			select {
			case <-bufferTimer.C:
			default:
			}
		}
		bufferTimer = nil
		bufferTimerC = nil
	}
	defer stopBufferTimer()
	flushPending := func() error {
		if len(pending) == 0 {
			stopBufferTimer()
			return nil
		}
		err := run.flushGuardedDeltas(streamCtx, pending)
		pending = nil
		pendingBytes = 0
		stopBufferTimer()
		return err
	}

	for {
		select {
		case scanned, open := <-lines:
			if !open {
				run.providerHealth = PublicNonStreamHealthProviderFailure
				return errors.Join(errPublicNonStreamUpstreamFailed, io.ErrUnexpectedEOF)
			}
			if scanned.done {
				run.providerHealth = PublicNonStreamHealthProviderFailure
				if scanned.err != nil {
					return errors.Join(errPublicNonStreamUpstreamFailed, scanned.err)
				}
				if hasEvent || hasData {
					return errors.Join(errPublicNonStreamUpstreamFailed, io.ErrUnexpectedEOF)
				}
				if err := flushPending(); err != nil {
					return err
				}
				return errors.Join(errPublicNonStreamUpstreamFailed, io.ErrUnexpectedEOF)
			}
			line := scanned.line
			if !utf8.ValidString(line) {
				run.providerHealth = PublicNonStreamHealthProviderFailure
				return errPublicNonStreamUpstreamFailed
			}
			if line == "" {
				if !hasEvent || !hasData {
					run.providerHealth = PublicNonStreamHealthProviderFailure
					return errPublicNonStreamUpstreamFailed
				}
				if eventName != run.stream.textEventName {
					if err := flushPending(); err != nil {
						return err
					}
				}
				rawEventBytes := len(eventData)
				if eventName == run.stream.textEventName && len(pending) > 0 &&
					pendingBytes+rawEventBytes > streampolicy.EnforceBufferMaxBytes {
					if err := flushPending(); err != nil {
						return err
					}
				}
				event, err := run.stream.parseEvent(eventName, []byte(eventData))
				if err != nil {
					run.providerHealth = PublicNonStreamHealthProviderFailure
					return errors.Join(errPublicNonStreamUpstreamFailed, err)
				}
				run.providerHealth = PublicNonStreamHealthNone
				hasEvent, hasData = false, false
				eventName, eventData = "", ""
				if len(event.Texts()) > 0 {
					pending = append(pending, event)
					pendingBytes += rawEventBytes
					if run.guardProfile.Enforced && pendingBytes > streampolicy.EnforceBufferMaxBytes {
						return errStreamGuardrailBufferExceeded
					}
					if bufferTimer == nil {
						bufferTimer = time.NewTimer(streampolicy.EnforceBufferMaxWait)
						bufferTimerC = bufferTimer.C
					}
					if !run.guardProfile.Enforced || pendingBytes >= streampolicy.EnforceBufferMaxBytes {
						if err := flushPending(); err != nil {
							return err
						}
					}
					continue
				}
				// Some protocols use the same SSE event name for guarded text and
				// opaque control deltas (for example Anthropic thinking/signature).
				// Flush guarded predecessors before rendering the control event so
				// customer order and signature association remain exact.
				if len(pending) > 0 {
					if err := flushPending(); err != nil {
						return err
					}
				}
				if event.Terminal() && event.Success() {
					if event.CustomerUsage().OutputTokens > int64(run.preparation.outputEstimate) {
						run.providerHealth = PublicNonStreamHealthProviderFailure
						return errPublicNonStreamUpstreamFailed
					}
					if err := run.complete(event, streamCtx); err != nil {
						return err
					}
					run.providerHealth = PublicNonStreamHealthSuccess
					return nil
				}
				if err := run.writeEvent(event); err != nil {
					return err
				}
				if event.Terminal() {
					run.providerHealth = PublicNonStreamHealthProviderFailure
					return errPublicNonStreamUpstreamFailed
				}
				continue
			}
			field, ok := parseSSEFieldLine(line)
			if !ok || (field.name != "event" && field.name != "data") {
				run.providerHealth = PublicNonStreamHealthProviderFailure
				return errPublicNonStreamUpstreamFailed
			}
			switch field.name {
			case "event":
				if hasEvent || hasData || field.value == "" {
					run.providerHealth = PublicNonStreamHealthProviderFailure
					return errPublicNonStreamUpstreamFailed
				}
				hasEvent, eventName = true, field.value
			case "data":
				if !hasEvent || hasData || field.value == "" {
					run.providerHealth = PublicNonStreamHealthProviderFailure
					return errPublicNonStreamUpstreamFailed
				}
				hasData, eventData = true, field.value
			}
		case <-bufferTimerC:
			if err := flushPending(); err != nil {
				return err
			}
		case <-heartbeat.C:
			if !run.started {
				continue
			}
			heartbeatCtx, heartbeatCancel := context.WithTimeout(context.WithoutCancel(streamCtx), 5*time.Second)
			err := run.orchestrator.billing.RenewStreamDelivery(heartbeatCtx, run.lease)
			heartbeatCancel()
			if err != nil {
				return errors.Join(errPublicNonStreamBillingFailed, err)
			}
			if _, err := run.streamWriter.WriteAndFlushCount(": ping\n\n"); err != nil {
				return errors.Join(errPublicNonStreamDeliveryFailed, err)
			}
		case <-streamCtx.Done():
			if run.clientContext == nil || run.clientContext.Err() == nil {
				run.providerHealth = PublicNonStreamHealthProviderFailure
			} else {
				run.providerHealth = PublicNonStreamHealthNone
			}
			return errors.Join(errPublicNonStreamUpstreamFailed, streamCtx.Err())
		}
	}
}

func scanPublicStreamLines(ctx context.Context, reader io.Reader, maxLine int, output chan<- publicStreamScannedLine) {
	defer close(output)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLine)
	for scanner.Scan() {
		select {
		case output <- publicStreamScannedLine{line: scanner.Text()}:
		case <-ctx.Done():
			return
		}
	}
	select {
	case output <- publicStreamScannedLine{err: scanner.Err(), done: true}:
	case <-ctx.Done():
	}
}

func (run *publicStreamRun) flushGuardedDeltas(
	ctx context.Context,
	events []publicStreamEvent,
) error {
	frames := make([]*guardedStreamFrame, len(events))
	for index, event := range events {
		texts := event.Texts()
		if len(texts) != 1 {
			return errStreamGuardrailUnsupportedText
		}
		container := map[string]interface{}{"delta": texts[0].Text}
		frames[index] = &guardedStreamFrame{
			envelope: container,
			refs:     []*streamTextRef{{container: container, key: "delta", path: texts[0].Path, original: texts[0].Text}},
		}
	}
	if run.guardProfile.Enforced {
		evaluation, err := evaluateGuardedStreamBatch(ctx, run.orchestrator.pinned, frames, run.guardHistory)
		if run.orchestrator.relay != nil && len(evaluation.Result.Receipts) > 0 {
			run.orchestrator.relay.recordGuardrailResult(
				ctx, run.request.RequestID, run.request.ResolvedModel, run.request.PublicModel,
				guardrail.StageStreamOutput, evaluation.Result, nil, http.StatusOK, "",
			)
		}
		if err != nil {
			return errors.Join(errPublicNonStreamGuardFailed, err)
		}
		if evaluation.Result.Decision == guardrail.DecisionBlock {
			return ErrPublicNonStreamOutputWithheld
		}
		updateStreamGuardrailHistory(run.guardHistory, evaluation.DeliveredByPath, run.guardProfile.LookbehindBytes)
	}
	for index, event := range events {
		value, _ := frames[index].envelope["delta"].(string)
		if !run.guardProfile.Enforced {
			value = event.Texts()[0].Text
		}
		if err := event.CommitTexts([]string{value}); err != nil {
			return errors.Join(errPublicNonStreamGuardFailed, err)
		}
		run.completionTokens += tokenutil.EstimateTokens(value)
		if run.completionTokens > run.preparation.outputEstimate {
			run.providerHealth = PublicNonStreamHealthProviderFailure
			return errPublicNonStreamUpstreamFailed
		}
		if err := run.writeEvent(event); err != nil {
			return err
		}
		if run.guardProfile.Advisory && run.orchestrator.pinned != nil {
			texts := streamAdvisoryTextsWithHistory([]guardrail.Text{{Path: event.Texts()[0].Path, Content: value}}, run.advisoryHistory)
			if run.orchestrator.relay != nil {
				run.orchestrator.relay.evaluateStreamAdvisoryAfterDisclosure(
					ctx, run.orchestrator.pinned, texts, run.request.RequestID,
					run.request.ResolvedModel, run.request.PublicModel, nil, "",
				)
			}
		}
	}
	return nil
}

func (run *publicStreamRun) beginDelivery() error {
	if run.started {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := run.orchestrator.billing.BeginStreamDelivery(ctx, run.lease); err != nil {
		return errors.Join(errPublicNonStreamBillingFailed, err)
	}
	run.writer.Header().Set("Content-Type", "text/event-stream")
	run.writer.Header().Set("Cache-Control", "no-cache")
	run.writer.Header().Set("Connection", "keep-alive")
	run.started = true
	return nil
}

func (run *publicStreamRun) writeEvent(event publicStreamEvent) error {
	body := event.Body()
	if len(body) == 0 || event.Event() == "" {
		return errPublicNonStreamUpstreamFailed
	}
	if err := run.beginDelivery(); err != nil {
		return err
	}
	payload := fmt.Sprintf("event: %s\ndata: %s\n\n", event.Event(), body)
	written, err := run.streamWriter.WriteAndFlushCount("%s", payload)
	if written > 0 {
		run.deliveredBytes += int64(written)
	}
	if err != nil || written != len(payload) {
		return errors.Join(errPublicNonStreamDeliveryFailed, err)
	}
	run.deliveredChunks++
	if err := run.checkpoint(!run.partialRecorded || event.Terminal()); err != nil {
		return err
	}
	if !run.partialRecorded {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := run.orchestrator.billing.RecordDelivery(ctx, billing.DeliveryEvent{
			RequestID: run.request.RequestID, AttemptID: run.attemptID, OrgID: run.request.OrgID,
			Mode: billing.DeliveryModeStream, Outcome: billing.DeliveryOutcomePartial,
			DeliveredBytes: run.deliveredBytes, DeliveredChunks: run.deliveredChunks,
			HTTPStatus: http.StatusOK, StreamFence: &run.fence,
		})
		cancel()
		if err != nil {
			return errors.Join(errPublicNonStreamBillingFailed, err)
		}
		run.partialRecorded = true
	}
	return nil
}

func (run *publicStreamRun) checkpoint(force bool) error {
	if !force && !run.lastCheckpointAt.IsZero() &&
		time.Since(run.lastCheckpointAt) < streampolicy.CheckpointInterval &&
		run.deliveredBytes-run.checkpointBytes < streampolicy.CheckpointDeliveredBytes &&
		run.completionTokens-run.checkpointTokens < streampolicy.CheckpointCompletionTokens {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := run.orchestrator.billing.CheckpointStreamDelivery(ctx, billing.StreamDeliveryCheckpoint{
		RequestID: run.request.RequestID, AttemptID: run.attemptID, OwnerID: run.lease.OwnerID,
		OrgID: run.request.OrgID, Generation: run.lease.Generation,
		DeliveredBytes: run.deliveredBytes, DeliveredChunks: run.deliveredChunks,
		PromptTokens: run.preparation.maximumInput, CompletionTokens: run.completionTokens,
		PolicySetVersion: run.guardProfile.PolicySetVersion, LeaseTTL: streampolicy.CheckpointLeaseDuration,
	})
	if err != nil {
		return errors.Join(errPublicNonStreamBillingFailed, err)
	}
	run.lastCheckpointAt = time.Now()
	run.checkpointBytes = run.deliveredBytes
	run.checkpointTokens = run.completionTokens
	return nil
}

func (run *publicStreamRun) complete(
	event publicStreamEvent,
	ctx context.Context,
) error {
	usage := event.CustomerUsage()
	// Headers were accepted before delivery; only a valid terminal event can
	// authorize terminal success and provider-final usage.
	run.attemptCompleted = true
	run.providerHealth = PublicNonStreamHealthSuccess
	run.healthAttempted = true
	if err := run.orchestrator.core.applyTerminalHealth(
		context.WithoutCancel(ctx), run.decision, PublicNonStreamHealthSuccess,
	); err != nil {
		return err
	}
	if err := run.writeEvent(event); err != nil {
		return err
	}
	settle, err := publicSettleRequest(
		run.request, run.attemptID, usage, metering.UsagePurposeCustomerCharge, run.stream.endpointKind,
	)
	if err != nil {
		return err
	}
	settle.StreamFence = &run.fence
	persistCtx, persistCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer persistCancel()
	if err := run.orchestrator.billing.PersistSettlementUsage(persistCtx, settle); err != nil {
		return errors.Join(errPublicNonStreamBillingFailed, err)
	}
	if err := run.orchestrator.billing.RecordDelivery(persistCtx, billing.DeliveryEvent{
		RequestID: run.request.RequestID, AttemptID: run.attemptID, OrgID: run.request.OrgID,
		Mode: billing.DeliveryModeStream, Outcome: billing.DeliveryOutcomeCompleted,
		DeliveredBytes: run.deliveredBytes, DeliveredChunks: run.deliveredChunks,
		HTTPStatus: http.StatusOK, StreamFence: &run.fence,
	}); err != nil {
		return errors.Join(errPublicNonStreamBillingFailed, err)
	}
	settled, err := run.orchestrator.billing.Settle(persistCtx, settle)
	if err != nil || settled == nil || !settled.Success {
		return errors.Join(errPublicNonStreamBillingFailed, err)
	}
	if err := run.orchestrator.billing.FinishStreamDelivery(
		persistCtx, run.lease, billing.StreamLeaseStateCompleted, run.stream.completedReason,
	); err != nil {
		return errors.Join(errPublicNonStreamBillingFailed, err)
	}
	run.providerHealth = PublicNonStreamHealthSuccess
	return nil
}
