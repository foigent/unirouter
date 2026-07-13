package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/uniroute/unirouter/internal/guardrail"
)

type streamReservationContextKey struct{}

type streamReservationState struct {
	ReservedCompletionTokens int
	AllowExtension           bool
}

func contextWithStreamReservation(ctx context.Context, reservedCompletionTokens int, allowExtension bool) context.Context {
	return context.WithValue(ctx, streamReservationContextKey{}, streamReservationState{
		ReservedCompletionTokens: reservedCompletionTokens,
		AllowExtension:           allowExtension,
	})
}

func streamReservationFromContext(ctx context.Context) streamReservationState {
	if ctx != nil {
		if state, ok := ctx.Value(streamReservationContextKey{}).(streamReservationState); ok {
			return state
		}
	}
	return streamReservationState{ReservedCompletionTokens: 1000, AllowExtension: true}
}

var processStreamLeaseOwner = buildStreamLeaseOwner()

func buildStreamLeaseOwner() string {
	hostname, _ := os.Hostname()
	hostDigest := sha256.Sum256([]byte(hostname))
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		nonce = []byte(fmt.Sprintf("%08x", time.Now().UnixNano()))
	}
	return fmt.Sprintf("node-%s-pid-%d-boot-%s", hex.EncodeToString(hostDigest[:4]), os.Getpid(), hex.EncodeToString(nonce))
}

type guardedStreamInspection struct {
	Frame         *guardedStreamFrame
	Data          []byte
	Delivered     map[string]string
	Text          string
	AdvisoryTexts []guardrail.Text
	Result        guardrail.Result
}

func inspectGuardedStreamFrame(ctx context.Context, pinned *guardrail.Pinned, profile guardrail.StreamProfile, data []byte, history map[string]string) (guardedStreamInspection, error) {
	batch, err := inspectGuardedStreamBatch(ctx, pinned, profile, [][]byte{data}, history)
	if len(batch.Frames) == 0 {
		return guardedStreamInspection{}, err
	}
	inspection := batch.Frames[0]
	inspection.Result = batch.Result
	return inspection, err
}

var streamAdvisorySlots = make(chan struct{}, 64)

func streamAdvisoryTextsWithHistory(texts []guardrail.Text, history map[string]string) []guardrail.Text {
	if len(texts) == 0 {
		return nil
	}
	result := make([]guardrail.Text, 0, len(texts))
	for _, item := range texts {
		content := history[item.Path] + item.Content
		result = append(result, guardrail.Text{Path: item.Path, Content: content})
		history[item.Path] = utf8SafeSuffix(content, guardrail.MaxStreamLookbehindBytes)
	}
	return result
}

func (relay *Relay) evaluateStreamAdvisoryAfterDisclosure(ctx context.Context, pinned *guardrail.Pinned, texts []guardrail.Text, requestID, model, requestedModel string, route *RouteResult, upstreamRequestID string) {
	if relay == nil || pinned == nil || len(texts) == 0 {
		return
	}
	copied := append([]guardrail.Text(nil), texts...)
	select {
	case streamAdvisorySlots <- struct{}{}:
	default:
		relay.recordTraceEvent(ctx, requestID, "guardrail.stream_advisory.dropped", "stream_output", "warning", model, requestedModel, route, 200, 0, map[string]interface{}{
			"pre_verdict_disclosure": true, "reason": "bounded_advisory_capacity",
		}, upstreamRequestID)
		return
	}
	go func() {
		defer func() { <-streamAdvisorySlots }()
		base := context.Background()
		if ctx != nil {
			base = context.WithoutCancel(ctx)
		}
		advisoryCtx, cancel := context.WithTimeout(base, 2*time.Second)
		defer cancel()
		result := pinned.EvaluateStreamAdvisory(advisoryCtx, copied)
		relay.recordGuardrailResult(advisoryCtx, requestID, model, requestedModel, guardrail.StageStreamOutput, result, route, 200, upstreamRequestID)
		relay.recordTraceEvent(advisoryCtx, requestID, "guardrail.stream_advisory.completed", "stream_output", "info", model, requestedModel, route, 200, 0, map[string]interface{}{
			"pre_verdict_disclosure":       true,
			"guardrail_policy_set_version": result.PolicySetVersion,
			"guardrail_decision":           result.Decision,
			"guardrail_degraded":           result.Degraded,
		}, upstreamRequestID)
	}()
}
