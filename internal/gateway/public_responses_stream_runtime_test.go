package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/streampolicy"
)

type publicRuntimeStreamBilling struct {
	*publicRuntimeBilling
	starts      []billing.StreamDeliveryStart
	checkpoints []billing.StreamDeliveryCheckpoint
	persisted   []billing.SettleRequest
	finishes    []struct{ state, reason string }
}

func (service *publicRuntimeStreamBilling) BeginStreamDelivery(_ context.Context, start billing.StreamDeliveryStart) error {
	*service.events = append(*service.events, "stream:begin")
	service.starts = append(service.starts, start)
	return nil
}

func (service *publicRuntimeStreamBilling) RenewStreamDelivery(context.Context, billing.StreamDeliveryStart) error {
	*service.events = append(*service.events, "stream:renew")
	return nil
}

func (service *publicRuntimeStreamBilling) CheckpointStreamDelivery(_ context.Context, checkpoint billing.StreamDeliveryCheckpoint) (*billing.StreamCheckpointResult, error) {
	*service.events = append(*service.events, "stream:checkpoint")
	service.checkpoints = append(service.checkpoints, checkpoint)
	return &billing.StreamCheckpointResult{Sequence: len(service.checkpoints)}, nil
}

func (service *publicRuntimeStreamBilling) FinishStreamDelivery(_ context.Context, _ billing.StreamDeliveryStart, state, reason string) error {
	*service.events = append(*service.events, "stream:finish:"+state)
	service.finishes = append(service.finishes, struct{ state, reason string }{state, reason})
	return nil
}

func (service *publicRuntimeStreamBilling) PersistSettlementUsage(_ context.Context, request billing.SettleRequest) error {
	*service.events = append(*service.events, "stream:persist_usage")
	service.persisted = append(service.persisted, request)
	return nil
}

func (writer *publicRuntimeWriter) Flush() {}

func TestPublicStreamRuntimeRejectsMissingBindingFactoryBeforePlanOrFreeze(t *testing.T) {
	events := []string{}
	fixture := newPublicResponsesStreamFixture(
		t, &events, publicResponsesSSE("safe", " answer"), disabledPublicStreamGuard(t),
	)
	fixture.orchestrator.bindStream = nil
	err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
	if !errors.Is(err, errPublicNonStreamRuntimeUnavailable) || len(events) != 0 ||
		len(fixture.billing.freeze) != 0 || len(fixture.billing.completions) != 0 {
		t.Fatalf("error=%v events=%v freeze=%d completions=%d",
			err, events, len(fixture.billing.freeze), len(fixture.billing.completions))
	}
}

func TestPublicResponsesStreamRuntimeUsesExactPlanGuardDeliveryUsageAndSettlement(t *testing.T) {
	events := []string{}
	fixture := newPublicResponsesStreamFixture(t, &events, publicResponsesSSE("safe", " answer"), disabledPublicStreamGuard(t))
	writer := &publicRuntimeWriter{events: &events}
	if err := fixture.orchestrator.execute(t.Context(), writer, fixture.request); err != nil {
		t.Fatal(err)
	}
	if len(fixture.billing.freeze) != 1 || fixture.billing.freeze[0].ExecutionSnapshotHash != fixture.planner.plan.SnapshotHash ||
		len(fixture.streamBilling.starts) != 1 || len(fixture.streamBilling.persisted) != 1 ||
		len(fixture.billing.settled) != 1 || len(fixture.streamBilling.finishes) != 1 ||
		fixture.streamBilling.finishes[0].state != billing.StreamLeaseStateCompleted {
		t.Fatalf("freeze=%+v starts=%d persisted=%d settled=%d finishes=%+v",
			fixture.billing.freeze, len(fixture.streamBilling.starts), len(fixture.streamBilling.persisted),
			len(fixture.billing.settled), fixture.streamBilling.finishes)
	}
	if got := fixture.planner.requirements; len(got) != 2 ||
		got[0].CapabilityID != "reasoning_effort" || len(got[0].Origins) != 1 || got[0].Origins[0] != "provider_default" ||
		got[1].CapabilityID != "streaming" || len(got[1].Origins) != 1 || got[1].Origins[0] != "explicit_request" {
		t.Fatalf("invocation requirements=%+v", got)
	}
	if len(fixture.billing.completions) != 1 ||
		fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeResponseAccepted {
		t.Fatalf("completions=%+v", fixture.billing.completions)
	}
	settle := fixture.streamBilling.persisted[0]
	if settle.AttemptID == "" || settle.StreamFence == nil || settle.StreamFence.AttemptID != settle.AttemptID ||
		settle.UsagePurpose != metering.UsagePurposeCustomerCharge || settle.UsageAuthority != metering.UsageAuthorityProviderReported ||
		settle.PromptTokens != 10 || settle.CacheReadTokens != 3 || settle.CacheCreationTokens != 2 || settle.CompletionTokens != 4 {
		t.Fatalf("settlement=%+v", settle)
	}
	body := writer.body.String()
	if !strings.Contains(body, "event: response.completed") || !strings.Contains(body, `"model":"public-text"`) ||
		!strings.Contains(body, "safe answer") || strings.Contains(body, "provider-private-model") {
		t.Fatalf("unsafe stream body=%s", body)
	}
	providerBody := fixture.providerBodies["provider-1.example"]
	if !strings.Contains(providerBody, `"stream":true`) || !strings.Contains(providerBody, `"model":"provider-private-model"`) {
		t.Fatalf("provider body=%s", providerBody)
	}
	deadline := fixture.providerDeadlines["provider-1.example"]
	remaining := time.Until(deadline)
	if deadline.IsZero() || remaining <= 0 || remaining > fixture.orchestrator.streamPolicy.MaxDuration() ||
		remaining < fixture.orchestrator.streamPolicy.MaxDuration()-5*time.Second {
		t.Fatalf("provider request deadline=%v remaining=%v policy=%v",
			deadline, remaining, fixture.orchestrator.streamPolicy.MaxDuration())
	}
	joined := strings.Join(events, ",")
	for _, required := range []string{
		"plan,guard:input,build:ciphertext-1,freeze,authorize:1,provider:provider-1.example,complete:response_accepted,stream:begin",
		"health:success,customer_write,stream:checkpoint,stream:persist_usage,delivery:completed,settle,stream:finish:completed",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("event order=%s missing=%s", joined, required)
		}
	}
	if strings.Index(joined, "stream:begin") > strings.Index(joined, "customer_write") {
		t.Fatalf("bytes preceded durable stream start: %s", joined)
	}
}

func TestPublicAnthropicMessagesStreamRuntimeUsesExactPlanGuardUsageAndMessagesSettlement(t *testing.T) {
	events := []string{}
	fixture := newPublicAnthropicMessagesStreamFixture(
		t, &events, publicAnthropicMessagesSSE("safe", " answer"), disabledPublicMessagesStreamGuard(t),
	)
	writer := &publicRuntimeWriter{events: &events}
	if err := fixture.orchestrator.execute(t.Context(), writer, fixture.request); err != nil {
		t.Fatal(err)
	}
	if len(fixture.billing.freeze) != 1 ||
		fixture.billing.freeze[0].ExecutionSnapshotHash != fixture.planner.plan.SnapshotHash ||
		len(fixture.streamBilling.persisted) != 1 || len(fixture.billing.settled) != 1 ||
		len(fixture.streamBilling.finishes) != 1 ||
		fixture.streamBilling.finishes[0] != (struct{ state, reason string }{
			state: billing.StreamLeaseStateCompleted, reason: "message_completed",
		}) {
		t.Fatalf("freeze=%+v persisted=%d settled=%d finishes=%+v",
			fixture.billing.freeze, len(fixture.streamBilling.persisted),
			len(fixture.billing.settled), fixture.streamBilling.finishes)
	}
	if got := fixture.planner.requirements; len(got) != 3 ||
		got[0].CapabilityID != modelprofile.CapabilityReasoning ||
		len(got[0].Origins) != 1 || got[0].Origins[0] != modelrouting.CapabilityOriginProviderDefault ||
		got[1].CapabilityID != modelprofile.CapabilityReasoningEffort ||
		len(got[1].Origins) != 1 || got[1].Origins[0] != modelrouting.CapabilityOriginProviderDefault ||
		got[2].CapabilityID != modelprofile.CapabilityStreaming ||
		len(got[2].Origins) != 1 || got[2].Origins[0] != modelrouting.CapabilityOriginExplicitRequest {
		t.Fatalf("Messages invocation requirements=%+v", got)
	}
	settle := fixture.streamBilling.persisted[0]
	if settle.PromptTokens != 10 || settle.CacheReadTokens != 3 || settle.CacheCreationTokens != 2 ||
		settle.CompletionTokens != 4 || settle.CacheUsageSchema != "anthropic.prompt_cache.v1" ||
		settle.UsageAuthority != metering.UsageAuthorityProviderReported {
		t.Fatalf("Messages settlement=%+v", settle)
	}
	body := writer.body.String()
	if !strings.Contains(body, "event: message_stop") || !strings.Contains(body, `"model":"public-text"`) ||
		!strings.Contains(body, `"text":"safe"`) || !strings.Contains(body, `"text":" answer"`) ||
		strings.Contains(body, "provider-private-model") ||
		strings.Contains(body, "private provider diagnostic") {
		t.Fatalf("unsafe Messages body=%s", body)
	}
	providerBody := fixture.providerBodies["provider-1.example"]
	if !strings.Contains(providerBody, `"stream":true`) ||
		!strings.Contains(providerBody, `"model":"provider-private-model"`) {
		t.Fatalf("provider body=%s", providerBody)
	}
	joined := strings.Join(events, ",")
	for _, required := range []string{
		"plan,guard:input,build:ciphertext-1,freeze,authorize:1,provider:provider-1.example,complete:response_accepted,stream:begin",
		"health:success,customer_write,stream:checkpoint,stream:persist_usage,delivery:completed,settle,stream:finish:completed",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("event order=%s missing=%s", joined, required)
		}
	}
}

func TestPublicAnthropicMessagesStreamRuntimeReusesExactV2ContractForOpusCandidate(t *testing.T) {
	events := []string{}
	fixture := newPublicAnthropicMessagesStreamFixtureForProfile(
		t, &events, publicAnthropicMessagesSSE("opus", " answer"), disabledPublicMessagesStreamGuard(t),
		"anthropic.claude-opus-5.api", "1.2.0",
	)
	writer := &publicRuntimeWriter{events: &events}
	if err := fixture.orchestrator.execute(t.Context(), writer, fixture.request); err != nil {
		t.Fatal(err)
	}
	if fixture.planner.plan.ExecutionProfileID != "anthropic.claude-opus-5.api" ||
		fixture.planner.plan.ExecutionProfileVersion != "1.2.0" {
		t.Fatalf("plan=%+v", fixture.planner.plan)
	}
	if got := fixture.planner.requirements; len(got) != 3 ||
		got[0].CapabilityID != modelprofile.CapabilityReasoning ||
		len(got[0].Origins) != 1 || got[0].Origins[0] != modelrouting.CapabilityOriginProviderDefault ||
		got[1].CapabilityID != modelprofile.CapabilityReasoningEffort ||
		len(got[1].Origins) != 1 || got[1].Origins[0] != modelrouting.CapabilityOriginProviderDefault ||
		got[2].CapabilityID != modelprofile.CapabilityStreaming ||
		len(got[2].Origins) != 1 || got[2].Origins[0] != modelrouting.CapabilityOriginExplicitRequest {
		t.Fatalf("Opus invocation requirements=%+v", got)
	}
	if len(fixture.streamBilling.persisted) != 1 || len(fixture.billing.settled) != 1 ||
		!strings.Contains(writer.body.String(), `"text":"opus"`) ||
		!strings.Contains(writer.body.String(), `"text":" answer"`) ||
		strings.Contains(writer.body.String(), "provider-private-model") {
		t.Fatalf("body=%s persisted=%d settled=%d",
			writer.body.String(), len(fixture.streamBilling.persisted), len(fixture.billing.settled))
	}
}

func TestPublicAnthropicMessagesStreamRuntimePreservesOpusDisabledEffortConstraintBeforeFreeze(t *testing.T) {
	events := []string{}
	fixture := newPublicAnthropicMessagesStreamFixtureForProfile(
		t, &events, publicAnthropicMessagesSSE("unused", " answer"), disabledPublicMessagesStreamGuard(t),
		"anthropic.claude-opus-5.api", "1.2.0",
	)
	fixture.request.Body = []byte(`{
		"model":"public-text","max_tokens":64,"stream":true,
		"thinking":{"type":"disabled"},"output_config":{"effort":"max"},
		"messages":[{"role":"user","content":"hello"}]
	}`)
	err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
	if !errors.Is(err, errPublicNonStreamRequestInvalid) || len(fixture.billing.freeze) != 0 ||
		len(fixture.billing.completions) != 0 {
		t.Fatalf("error=%v freeze=%d completions=%d events=%v",
			err, len(fixture.billing.freeze), len(fixture.billing.completions), events)
	}
}

func TestPublicAnthropicMessagesStreamRuntimeGuardsQueuedHaikuSummariesBeforeSignature(t *testing.T) {
	events := []string{}
	fixture := newPublicAnthropicMessagesStreamFixtureForProfile(
		t, &events, publicAnthropicHaiku45ThinkingSSE("first summary ", "second summary", "safe", " answer"),
		enforcedPublicMessagesStreamGuard(t), "anthropic.claude-haiku-4.5.api", "1.2.0",
	)
	fixture.request.Body = []byte(`{
		"model":"public-text","max_tokens":4096,"stream":true,
		"thinking":{"type":"enabled","budget_tokens":1024},
		"messages":[{"role":"user","content":"hello"}]
	}`)
	writer := &publicRuntimeWriter{events: &events}
	if err := fixture.orchestrator.execute(t.Context(), writer, fixture.request); err != nil {
		t.Fatal(err)
	}
	if got := fixture.planner.requirements; len(got) != 3 ||
		got[0].CapabilityID != modelprofile.CapabilityReasoning ||
		len(got[0].Origins) != 1 || got[0].Origins[0] != modelrouting.CapabilityOriginExplicitRequest ||
		got[1].CapabilityID != modelprofile.CapabilityReasoningBudget ||
		len(got[1].Origins) != 1 || got[1].Origins[0] != modelrouting.CapabilityOriginExplicitRequest ||
		got[2].CapabilityID != modelprofile.CapabilityStreaming ||
		len(got[2].Origins) != 1 || got[2].Origins[0] != modelrouting.CapabilityOriginExplicitRequest {
		t.Fatalf("Haiku invocation requirements=%+v", got)
	}
	body := writer.body.String()
	firstSummary := strings.Index(body, `"thinking":"first summary "`)
	secondSummary := strings.Index(body, `"thinking":"second summary"`)
	signature := strings.Index(body, `"signature":"opaque-signature"`)
	answer := strings.Index(body, `"text":"safe"`)
	if firstSummary < 0 || secondSummary <= firstSummary || signature <= secondSummary || answer <= signature ||
		strings.Contains(body, "provider-private-model") || len(fixture.streamBilling.persisted) != 1 ||
		len(fixture.billing.settled) != 1 {
		t.Fatalf("body=%s persisted=%d settled=%d", body,
			len(fixture.streamBilling.persisted), len(fixture.billing.settled))
	}
	settle := fixture.streamBilling.persisted[0]
	if settle.PromptTokens != 5 || settle.CompletionTokens != 8 || settle.UsageAuthority != metering.UsageAuthorityProviderReported {
		t.Fatalf("settlement=%+v", settle)
	}
}

func TestPublicAnthropicMessagesStreamRuntimeBlocksSensitiveHaikuSummaryWithoutSettlement(t *testing.T) {
	events := []string{}
	fixture := newPublicAnthropicMessagesStreamFixtureForProfile(
		t, &events, publicAnthropicHaiku45ThinkingSSE("alice@", "example.com", "unused", " answer"),
		enforcedPublicMessagesStreamGuard(t), "anthropic.claude-haiku-4.5.api", "1.2.0",
	)
	fixture.request.Body = []byte(`{
		"model":"public-text","max_tokens":4096,"stream":true,
		"thinking":{"type":"enabled","budget_tokens":1024},
		"messages":[{"role":"user","content":"hello"}]
	}`)
	writer := &publicRuntimeWriter{events: &events}
	err := fixture.orchestrator.execute(t.Context(), writer, fixture.request)
	if !publicResponseWasStarted(err) || !errors.Is(err, ErrPublicNonStreamOutputWithheld) {
		t.Fatalf("error=%v", err)
	}
	body := writer.body.String()
	if strings.Contains(body, "alice@") || strings.Contains(body, "example.com") ||
		strings.Contains(body, "opaque-signature") || len(fixture.streamBilling.persisted) != 0 ||
		len(fixture.billing.settled) != 0 || len(fixture.streamBilling.finishes) != 1 ||
		fixture.streamBilling.finishes[0].state != billing.StreamLeaseStateInterrupted {
		t.Fatalf("body=%s persisted=%d settled=%d finishes=%+v",
			body, len(fixture.streamBilling.persisted), len(fixture.billing.settled), fixture.streamBilling.finishes)
	}
}

func TestPublicResponsesStreamRuntimeReusesExactV2ContractForTerraProfile(t *testing.T) {
	events := []string{}
	fixture := newPublicResponsesStreamFixtureForProfile(
		t, &events, publicResponsesSSE("terra", " answer"), disabledPublicStreamGuard(t),
		"openai.gpt-5.6-terra.responses", "1.3.0",
	)
	writer := &publicRuntimeWriter{events: &events}
	if err := fixture.orchestrator.execute(t.Context(), writer, fixture.request); err != nil {
		t.Fatal(err)
	}
	if got := fixture.planner.requirements; len(got) != 2 ||
		got[0].CapabilityID != modelprofile.CapabilityReasoningEffort ||
		len(got[0].Origins) != 1 || got[0].Origins[0] != modelrouting.CapabilityOriginProviderDefault ||
		got[1].CapabilityID != modelprofile.CapabilityStreaming ||
		len(got[1].Origins) != 1 || got[1].Origins[0] != modelrouting.CapabilityOriginExplicitRequest {
		t.Fatalf("Terra invocation requirements=%+v", got)
	}
	if len(fixture.streamBilling.persisted) != 1 || len(fixture.billing.settled) != 1 ||
		!strings.Contains(writer.body.String(), "terra answer") ||
		strings.Contains(writer.body.String(), "provider-private-model") {
		t.Fatalf("body=%s persisted=%d settled=%d",
			writer.body.String(), len(fixture.streamBilling.persisted), len(fixture.billing.settled))
	}
}

func TestPublicResponsesStreamRuntimeReusesExactV2ContractForLunaCandidate(t *testing.T) {
	events := []string{}
	fixture := newPublicResponsesStreamFixtureForProfile(
		t, &events, publicResponsesSSE("luna", " answer"), disabledPublicStreamGuard(t),
		"openai.gpt-5.6-luna.responses", "1.3.0",
	)
	writer := &publicRuntimeWriter{events: &events}
	if err := fixture.orchestrator.execute(t.Context(), writer, fixture.request); err != nil {
		t.Fatal(err)
	}
	if got := fixture.planner.requirements; len(got) != 2 ||
		got[0].CapabilityID != modelprofile.CapabilityReasoningEffort ||
		len(got[0].Origins) != 1 || got[0].Origins[0] != modelrouting.CapabilityOriginProviderDefault ||
		got[1].CapabilityID != modelprofile.CapabilityStreaming ||
		len(got[1].Origins) != 1 || got[1].Origins[0] != modelrouting.CapabilityOriginExplicitRequest {
		t.Fatalf("Luna invocation requirements=%+v", got)
	}
	if len(fixture.streamBilling.persisted) != 1 || len(fixture.billing.settled) != 1 ||
		!strings.Contains(writer.body.String(), "luna answer") ||
		strings.Contains(writer.body.String(), "provider-private-model") {
		t.Fatalf("body=%s persisted=%d settled=%d",
			writer.body.String(), len(fixture.streamBilling.persisted), len(fixture.billing.settled))
	}
}

func TestPublicResponsesStreamRuntimeBlocksCrossDeltaTextWithoutSettlement(t *testing.T) {
	events := []string{}
	fixture := newPublicResponsesStreamFixture(
		t, &events, publicResponsesSSE("alice@", "example.com"), p6f11StreamGuardrail(t, guardrail.ActionBlock),
	)
	writer := &publicRuntimeWriter{events: &events}
	err := fixture.orchestrator.execute(t.Context(), writer, fixture.request)
	if !publicResponseWasStarted(err) || !errors.Is(err, ErrPublicNonStreamOutputWithheld) {
		t.Fatalf("error=%v", err)
	}
	if strings.Contains(writer.body.String(), "alice@") || strings.Contains(writer.body.String(), "example.com") ||
		len(fixture.streamBilling.persisted) != 0 || len(fixture.billing.settled) != 0 ||
		len(fixture.streamBilling.finishes) != 1 || fixture.streamBilling.finishes[0].state != billing.StreamLeaseStateInterrupted {
		t.Fatalf("body=%s persisted=%d settled=%d finishes=%+v",
			writer.body.String(), len(fixture.streamBilling.persisted), len(fixture.billing.settled), fixture.streamBilling.finishes)
	}
	if len(fixture.billing.completions) != 2 || fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeResponseAccepted || fixture.billing.completions[1].Outcome != billing.ProviderAttemptOutcomeResponseUnusable || fixture.billing.completions[1].HTTPStatus != http.StatusOK {
		t.Fatalf("completion=%+v", fixture.billing.completions)
	}
}

func TestPublicResponsesStreamRuntimeRedactsAcrossBufferedDeltasAndReconcilesFinalEvents(t *testing.T) {
	events := []string{}
	fixture := newPublicResponsesStreamFixture(
		t, &events, publicResponsesSSE("alice@", "example.com"), p6f11StreamGuardrail(t, guardrail.ActionRedact),
	)
	writer := &publicRuntimeWriter{events: &events}
	if err := fixture.orchestrator.execute(t.Context(), writer, fixture.request); err != nil {
		t.Fatal(err)
	}
	body := writer.body.String()
	if strings.Contains(body, "alice@") || strings.Contains(body, "example.com") ||
		!strings.Contains(body, "[REDACTED]") || !strings.Contains(body, "event: response.completed") {
		t.Fatalf("redacted body=%s", body)
	}
	if len(fixture.streamBilling.persisted) != 1 || len(fixture.billing.settled) != 1 ||
		len(fixture.billing.completions) != 1 || fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeResponseAccepted {
		t.Fatalf("persisted=%d settled=%d completions=%+v",
			len(fixture.streamBilling.persisted), len(fixture.billing.settled), fixture.billing.completions)
	}
}

func TestPublicResponsesStreamRuntimeFailoverOccursOnlyBeforeCustomerBytes(t *testing.T) {
	events := []string{}
	fixture := newPublicResponsesStreamFixture(t, &events, publicResponsesSSE("safe", " answer"), disabledPublicStreamGuard(t))
	fixture.planner.plan = publicRuntimePlanForProfile(t, 2, "openai.gpt-5.6-sol.responses", "1.3.0")
	fixture.providerStatuses["provider-1.example"] = http.StatusServiceUnavailable
	fixture.providerStatuses["provider-2.example"] = http.StatusOK
	writer := &publicRuntimeWriter{events: &events}
	if err := fixture.orchestrator.execute(t.Context(), writer, fixture.request); err != nil {
		t.Fatal(err)
	}
	if len(fixture.billing.completions) != 2 ||
		fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeRejected ||
		fixture.billing.completions[0].HTTPStatus != http.StatusServiceUnavailable ||
		!fixture.billing.completions[0].RetryAuthorized ||
		fixture.billing.completions[1].Outcome != billing.ProviderAttemptOutcomeResponseAccepted ||
		fixture.billing.completions[1].HTTPStatus != http.StatusOK {
		t.Fatalf("completions=%+v", fixture.billing.completions)
	}
	joined := strings.Join(events, ",")
	if strings.Index(joined, "authorize:2") < strings.Index(joined, "health:provider_failure") ||
		strings.Count(joined, "stream:begin") != 1 {
		t.Fatalf("failover/delivery order=%s", joined)
	}
}

func TestPublicResponsesStreamRuntimeKeepsUnknownLiabilityReserved(t *testing.T) {
	for _, completionFailure := range []bool{false, true} {
		t.Run(fmt.Sprint("completion_failure=", completionFailure), func(t *testing.T) {
			events := []string{}
			fixture := newPublicResponsesStreamFixture(t, &events, "", disabledPublicStreamGuard(t))
			fixture.orchestrator.core.doer = publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("transport interrupted")
			})
			if completionFailure {
				fixture.billing.completionError = errors.New("receipt store unavailable")
			}
			err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
			if err == nil || fixture.billing.cancelled != 0 || len(fixture.billing.settled) != 0 ||
				len(fixture.billing.completions) != 1 ||
				fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeUnknown ||
				fixture.billing.completions[0].HTTPStatus != 0 {
				t.Fatalf("err=%v cancelled=%d completions=%+v", err, fixture.billing.cancelled, fixture.billing.completions)
			}
			if completionFailure && (!errors.Is(err, errPublicNonStreamBillingFailed) || strings.Contains(strings.Join(events, ","), "health:")) {
				t.Fatalf("unpersisted completion must not advance health: err=%v events=%v", err, events)
			}
		})
	}
}

func TestPublicResponsesStreamRuntimeTerminalFailureIsInterruptedWithoutSettlement(t *testing.T) {
	events := []string{}
	progress := `{"id":"resp_1","object":"response","status":"in_progress","model":"provider-private-model","error":null,"incomplete_details":null,"output":[],"usage":null}`
	sse := strings.Join([]string{
		`event: response.created`, `data: {"type":"response.created","response":` + progress + `,"sequence_number":0}`, ``,
		`event: response.failed`, `data: {"type":"response.failed","response":{"id":"resp_1","object":"response","status":"failed","model":"provider-private-model","error":{"code":"private","message":"provider diagnostic"},"incomplete_details":null,"output":[],"usage":{"input_tokens":99,"output_tokens":99,"total_tokens":198}},"sequence_number":1}`, ``,
	}, "\n") + "\n"
	fixture := newPublicResponsesStreamFixture(t, &events, sse, disabledPublicStreamGuard(t))
	writer := &publicRuntimeWriter{events: &events}
	err := fixture.orchestrator.execute(t.Context(), writer, fixture.request)
	if !publicResponseWasStarted(err) {
		t.Fatalf("error=%v", err)
	}
	body := writer.body.String()
	if !strings.Contains(body, "event: response.failed") || !strings.Contains(body, `"model":"public-text"`) ||
		strings.Contains(body, "provider-private-model") || strings.Contains(body, "provider diagnostic") || strings.Contains(body, "99") {
		t.Fatalf("failure body=%s", body)
	}
	if len(fixture.streamBilling.persisted) != 0 || len(fixture.billing.settled) != 0 ||
		len(fixture.streamBilling.finishes) != 1 || fixture.streamBilling.finishes[0].state != billing.StreamLeaseStateInterrupted ||
		len(fixture.billing.completions) != 2 || fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeResponseAccepted || fixture.billing.completions[1].Outcome != billing.ProviderAttemptOutcomeResponseUnusable || fixture.billing.completions[1].HTTPStatus != http.StatusOK ||
		!strings.Contains(strings.Join(events, ","), "health:provider_failure") {
		t.Fatalf("events=%s completions=%+v persisted=%d settled=%d finishes=%+v",
			strings.Join(events, ","), fixture.billing.completions, len(fixture.streamBilling.persisted),
			len(fixture.billing.settled), fixture.streamBilling.finishes)
	}
}

func TestPublicResponsesStreamRuntimeInvalidEventAfterBytesIsProviderFailureWithoutSettlement(t *testing.T) {
	events := []string{}
	progress := `{"id":"resp_1","object":"response","status":"in_progress","model":"provider-private-model","error":null,"incomplete_details":null,"output":[],"usage":null}`
	sse := strings.Join([]string{
		`event: response.created`, `data: {"type":"response.created","response":` + progress + `,"sequence_number":0}`, ``,
		`event: response.web_search_call.in_progress`, `data: {"type":"response.web_search_call.in_progress","sequence_number":1}`, ``,
	}, "\n") + "\n"
	fixture := newPublicResponsesStreamFixture(t, &events, sse, disabledPublicStreamGuard(t))
	writer := &publicRuntimeWriter{events: &events}
	err := fixture.orchestrator.execute(t.Context(), writer, fixture.request)
	if !publicResponseWasStarted(err) || len(fixture.streamBilling.persisted) != 0 || len(fixture.billing.settled) != 0 ||
		len(fixture.billing.completions) != 2 || fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeResponseAccepted || fixture.billing.completions[1].Outcome != billing.ProviderAttemptOutcomeResponseUnusable || fixture.billing.completions[1].HTTPStatus != http.StatusOK ||
		!strings.Contains(strings.Join(events, ","), "health:provider_failure") {
		t.Fatalf("error=%v events=%s completions=%+v persisted=%d settled=%d",
			err, strings.Join(events, ","), fixture.billing.completions,
			len(fixture.streamBilling.persisted), len(fixture.billing.settled))
	}
}

func TestPublicResponsesStreamRuntimeRejectsConflictingFinalUsageWithoutSettlement(t *testing.T) {
	events := []string{}
	sse := strings.Replace(publicResponsesSSE("safe", " answer"), `"total_tokens":14`, `"total_tokens":15`, 1)
	fixture := newPublicResponsesStreamFixture(t, &events, sse, disabledPublicStreamGuard(t))
	writer := &publicRuntimeWriter{events: &events}
	err := fixture.orchestrator.execute(t.Context(), writer, fixture.request)
	if !publicResponseWasStarted(err) || len(fixture.streamBilling.persisted) != 0 || len(fixture.billing.settled) != 0 ||
		len(fixture.billing.completions) != 2 || fixture.billing.completions[0].Outcome != billing.ProviderAttemptOutcomeResponseAccepted || fixture.billing.completions[1].Outcome != billing.ProviderAttemptOutcomeResponseUnusable || fixture.billing.completions[1].HTTPStatus != http.StatusOK ||
		!strings.Contains(strings.Join(events, ","), "health:provider_failure") {
		t.Fatalf("error=%v events=%s completions=%+v persisted=%d settled=%d",
			err, strings.Join(events, ","), fixture.billing.completions,
			len(fixture.streamBilling.persisted), len(fixture.billing.settled))
	}
}

type publicResponsesStreamFixture struct {
	orchestrator      *publicStreamOrchestrator
	planner           *publicRuntimePlanner
	billing           *publicRuntimeBilling
	streamBilling     *publicRuntimeStreamBilling
	request           publicNonStreamRequest
	providerStatuses  map[string]int
	providerBodies    map[string]string
	providerDeadlines map[string]time.Time
}

func newPublicResponsesStreamFixture(
	t *testing.T,
	events *[]string,
	sse string,
	pinned *guardrail.Pinned,
) *publicResponsesStreamFixture {
	return newPublicResponsesStreamFixtureForProfile(
		t, events, sse, pinned, "openai.gpt-5.6-sol.responses", "1.3.0",
	)
}

func newPublicResponsesStreamFixtureForProfile(
	t *testing.T,
	events *[]string,
	sse string,
	pinned *guardrail.Pinned,
	profileID, profileVersion string,
) *publicResponsesStreamFixture {
	t.Helper()
	base := newPublicRuntimeFixture(t, events, 1)
	base.planner.plan = publicRuntimePlanForProfile(t, 1, profileID, profileVersion)
	base.request.Body = []byte(`{"model":"public-text","input":"hello","max_output_tokens":64,"stream":true}`)
	streamBilling := &publicRuntimeStreamBilling{publicRuntimeBilling: base.billing}
	providerDeadlines := make(map[string]time.Time)
	base.orchestrator.billing = streamBilling
	base.orchestrator.registry = endpointprotocol.NewPublicStreamRegistry()
	base.orchestrator.doer = publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
		host := request.URL.Host
		*events = append(*events, "provider:"+host)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		base.providerBodies[host] = string(body)
		if deadline, ok := request.Context().Deadline(); ok {
			providerDeadlines[host] = deadline
		}
		status := base.providerStatuses[host]
		if status != http.StatusOK {
			return &http.Response{
				StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"error":"private"}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}},
			Body: io.NopCloser(strings.NewReader(sse)),
		}, nil
	})
	streamPolicy := streampolicy.Default()
	return &publicResponsesStreamFixture{
		orchestrator: &publicStreamOrchestrator{
			core: base.orchestrator, billing: streamBilling, pinned: pinned, streamPolicy: streamPolicy,
			bindStream: newPublicResponsesStreamBinding,
		},
		planner: base.planner, billing: base.billing, streamBilling: streamBilling, request: base.request,
		providerStatuses: base.providerStatuses, providerBodies: base.providerBodies,
		providerDeadlines: providerDeadlines,
	}
}

func newPublicAnthropicMessagesStreamFixture(
	t *testing.T,
	events *[]string,
	sse string,
	pinned *guardrail.Pinned,
) *publicResponsesStreamFixture {
	t.Helper()
	return newPublicAnthropicMessagesStreamFixtureForProfile(
		t, events, sse, pinned, "anthropic.claude-sonnet-5.api", "1.3.0",
	)
}

func newPublicAnthropicMessagesStreamFixtureForProfile(
	t *testing.T,
	events *[]string,
	sse string,
	pinned *guardrail.Pinned,
	profileID string,
	profileVersion string,
) *publicResponsesStreamFixture {
	t.Helper()
	base := newPublicRuntimeFixture(t, events, 1)
	base.planner.plan = publicRuntimePlanForProfileOperation(
		t, 1, profileID, profileVersion, modeloperation.IDMessages,
	)
	base.request.Operation = modeloperation.IDMessages
	base.request.Path = modeloperation.PathMessages
	base.request.Body = []byte(`{
		"model":"public-text","max_tokens":64,"stream":true,
		"messages":[{"role":"user","content":"hello"}]
	}`)
	streamBilling := &publicRuntimeStreamBilling{publicRuntimeBilling: base.billing}
	providerDeadlines := make(map[string]time.Time)
	base.orchestrator.billing = streamBilling
	base.orchestrator.registry = endpointprotocol.NewPublicStreamRegistry()
	base.orchestrator.doer = publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
		host := request.URL.Host
		*events = append(*events, "provider:"+host)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		base.providerBodies[host] = string(body)
		if deadline, ok := request.Context().Deadline(); ok {
			providerDeadlines[host] = deadline
		}
		status := base.providerStatuses[host]
		if status != http.StatusOK {
			return &http.Response{
				StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"error":"private"}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(sse)),
		}, nil
	})
	return &publicResponsesStreamFixture{
		orchestrator: &publicStreamOrchestrator{
			core: base.orchestrator, billing: streamBilling, pinned: pinned,
			streamPolicy: streampolicy.Default(), bindStream: newPublicAnthropicMessagesStreamBinding,
		},
		planner: base.planner, billing: base.billing, streamBilling: streamBilling, request: base.request,
		providerStatuses: base.providerStatuses, providerBodies: base.providerBodies,
		providerDeadlines: providerDeadlines,
	}
}

func disabledPublicStreamGuard(t *testing.T) *guardrail.Pinned {
	t.Helper()
	service := newP6F10GuardrailService(t, guardrail.Policy{
		ID: 901, Name: "disabled public stream fixture", Version: 1,
		Stage: guardrail.StageStreamOutput, DetectorRef: guardrail.DetectorSensitiveWords,
		Action: guardrail.ActionBlock, Mode: guardrail.ModeDisabled, FailureMode: guardrail.FailureFailClosed,
		Config: json.RawMessage(`{}`),
	})
	return service.Pin(guardrail.Subject{OrgID: 11, APIKeyID: 13, Model: "public-text", Endpoint: "/v1/responses"})
}

func disabledPublicMessagesStreamGuard(t *testing.T) *guardrail.Pinned {
	t.Helper()
	service := newP6F10GuardrailService(t, guardrail.Policy{
		ID: 902, Name: "disabled public Messages stream fixture", Version: 1,
		Stage: guardrail.StageStreamOutput, DetectorRef: guardrail.DetectorSensitiveWords,
		Action: guardrail.ActionBlock, Mode: guardrail.ModeDisabled, FailureMode: guardrail.FailureFailClosed,
		Config: json.RawMessage(`{}`),
	})
	return service.Pin(guardrail.Subject{
		OrgID: 11, APIKeyID: 13, Model: "public-text", Endpoint: modeloperation.PathMessages,
	})
}

func enforcedPublicMessagesStreamGuard(t *testing.T) *guardrail.Pinned {
	t.Helper()
	service := newP6F10GuardrailService(t, guardrail.Policy{
		ID: 903, Name: "enforced public Messages stream fixture", Version: 1,
		Stage: guardrail.StageStreamOutput, DetectorRef: guardrail.DetectorPII,
		Action: guardrail.ActionBlock, Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureFailClosed,
		Config: json.RawMessage(`{"entities":["email"]}`),
	})
	return service.Pin(guardrail.Subject{
		OrgID: 11, APIKeyID: 13, Model: "public-text", Endpoint: modeloperation.PathMessages,
	})
}

func publicResponsesSSE(first, second string) string {
	providerText := first + second
	escape := func(value string) string {
		body, _ := json.Marshal(value)
		return string(body)
	}
	response := `{"id":"resp_1","object":"response","status":"%s","model":"provider-private-model","error":null,"incomplete_details":null,"output":%s,"usage":%s}`
	completedOutput := `[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":` + escape(providerText) + `,"annotations":[]}]}]`
	completedUsage := `{"input_tokens":10,"input_tokens_details":{"cached_tokens":3,"cache_write_tokens":2},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":14}`
	progress := fmt.Sprintf(response, "in_progress", `[]`, `null`)
	lines := []string{
		`event: response.created`, `data: {"type":"response.created","response":` + progress + `,"sequence_number":0}`, ``,
		`event: response.in_progress`, `data: {"type":"response.in_progress","response":` + progress + `,"sequence_number":1}`, ``,
		`event: response.output_item.added`, `data: {"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","status":"in_progress","role":"assistant","content":[]},"sequence_number":2}`, ``,
		`event: response.content_part.added`, `data: {"type":"response.content_part.added","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":"","annotations":[]},"sequence_number":3}`, ``,
		`event: response.output_text.delta`, `data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":` + escape(first) + `,"sequence_number":4}`, ``,
		`event: response.output_text.delta`, `data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":` + escape(second) + `,"sequence_number":5}`, ``,
		`event: response.output_text.done`, `data: {"type":"response.output_text.done","item_id":"msg_1","output_index":0,"content_index":0,"text":` + escape(providerText) + `,"sequence_number":6}`, ``,
		`event: response.content_part.done`, `data: {"type":"response.content_part.done","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":` + escape(providerText) + `,"annotations":[]},"sequence_number":7}`, ``,
		`event: response.output_item.done`, `data: {"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":` + escape(providerText) + `,"annotations":[]}]},"sequence_number":8}`, ``,
		`event: response.completed`, `data: {"type":"response.completed","response":` + fmt.Sprintf(response, "completed", completedOutput, completedUsage) + `,"sequence_number":9}`, ``,
	}
	return strings.Join(lines, "\n") + "\n"
}

func publicAnthropicMessagesSSE(first, second string) string {
	escape := func(value string) string {
		body, _ := json.Marshal(value)
		return string(body)
	}
	lines := []string{
		`event: message_start`, `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"provider-private-model","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"cache_creation_input_tokens":2,"cache_read_input_tokens":3,"output_tokens":0}}}`, ``,
		`event: content_block_start`, `data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`, ``,
		`event: content_block_delta`, `data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque-signature"}}`, ``,
		`event: content_block_stop`, `data: {"type":"content_block_stop","index":0}`, ``,
		`event: content_block_start`, `data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`, ``,
		`event: content_block_delta`, `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":` + escape(first) + `}}`, ``,
		`event: content_block_delta`, `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":` + escape(second) + `}}`, ``,
		`event: content_block_stop`, `data: {"type":"content_block_stop","index":1}`, ``,
		`event: message_delta`, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":4,"output_tokens_details":{"thinking_tokens":1}}}`, ``,
		`event: message_stop`, `data: {"type":"message_stop"}`, ``,
	}
	return strings.Join(lines, "\n") + "\n"
}

func publicAnthropicHaiku45ThinkingSSE(firstSummary, secondSummary, firstAnswer, secondAnswer string) string {
	escape := func(value string) string {
		body, _ := json.Marshal(value)
		return string(body)
	}
	lines := []string{
		`event: message_start`, `data: {"type":"message_start","message":{"id":"msg_haiku","type":"message","role":"assistant","model":"provider-private-model","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":0}}}`, ``,
		`event: content_block_start`, `data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`, ``,
		`event: content_block_delta`, `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":` + escape(firstSummary) + `}}`, ``,
		`event: content_block_delta`, `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":` + escape(secondSummary) + `}}`, ``,
		`event: content_block_delta`, `data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque-signature"}}`, ``,
		`event: content_block_stop`, `data: {"type":"content_block_stop","index":0}`, ``,
		`event: content_block_start`, `data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`, ``,
		`event: content_block_delta`, `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":` + escape(firstAnswer) + `}}`, ``,
		`event: content_block_delta`, `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":` + escape(secondAnswer) + `}}`, ``,
		`event: content_block_stop`, `data: {"type":"content_block_stop","index":1}`, ``,
		`event: message_delta`, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":8,"output_tokens_details":{"thinking_tokens":5}}}`, ``,
		`event: message_stop`, `data: {"type":"message_stop"}`, ``,
	}
	return strings.Join(lines, "\n") + "\n"
}

var _ publicStreamBilling = (*publicRuntimeStreamBilling)(nil)
