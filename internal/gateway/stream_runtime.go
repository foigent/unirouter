package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/middleware"
)

type streamReservationContextKey struct{}

const streamAdvisoryGapRecordWait = 2 * time.Second

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
	policies := pinned.StreamAdvisoryPolicyIdentities()
	orgID := middleware.GetOrgIDFromContext(ctx)
	if orgID <= 0 || len(policies) == 0 {
		return
	}
	currentStreamAdvisoryScheduler().Submit(streamAdvisoryJob{
		ctx: ctx, relay: relay, pinned: pinned, texts: texts, orgID: orgID,
		requestID: requestID, policies: policies, model: model, requestedModel: requestedModel,
		route: route, upstreamRequestID: upstreamRequestID,
	})
}

var defaultStreamAdvisoryRuntime struct {
	sync.Mutex
	scheduler *streamAdvisoryScheduler
	gapWriter *streamAdvisoryGapWriter
}

func currentStreamAdvisoryScheduler() *streamAdvisoryScheduler {
	defaultStreamAdvisoryRuntime.Lock()
	defer defaultStreamAdvisoryRuntime.Unlock()
	if defaultStreamAdvisoryRuntime.scheduler != nil {
		return defaultStreamAdvisoryRuntime.scheduler
	}
	writer := newStreamAdvisoryGapWriter(defaultStreamAdvisoryGapWriterOptions())
	scheduler := newStreamAdvisoryScheduler(
		defaultStreamAdvisorySchedulerOptions(),
		executeStreamAdvisoryJob,
		func(ctx context.Context, job streamAdvisoryJob, reason string) {
			recordStreamAdvisoryGap(ctx, writer, job, reason)
		},
	)
	defaultStreamAdvisoryRuntime.gapWriter = writer
	defaultStreamAdvisoryRuntime.scheduler = scheduler
	return scheduler
}

func executeStreamAdvisoryJob(ctx context.Context, job streamAdvisoryJob) {
	if job.relay == nil || job.pinned == nil || len(job.texts) == 0 {
		return
	}
	result := job.pinned.EvaluateStreamAdvisory(ctx, job.texts)
	if ctx.Err() != nil {
		return
	}
	job.relay.recordGuardrailResult(ctx, job.requestID, job.model, job.requestedModel, guardrail.StageStreamOutput, result, job.route, 200, job.upstreamRequestID)
	job.relay.recordTraceEvent(ctx, job.requestID, "guardrail.stream_advisory.completed", "stream_output", "info", job.model, job.requestedModel, job.route, 200, 0, map[string]interface{}{
		"pre_verdict_disclosure":       true,
		"guardrail_policy_set_version": result.PolicySetVersion,
		"guardrail_decision":           result.Decision,
		"guardrail_degraded":           result.Degraded,
	}, job.upstreamRequestID)
}

func recordStreamAdvisoryGap(ctx context.Context, writer *streamAdvisoryGapWriter, job streamAdvisoryJob, reason string) {
	var err error
	if reason == streamAdvisoryGapShutdown {
		_, err = writer.Enqueue(ctx, job, reason)
	} else {
		if ctx == nil {
			ctx = context.Background()
		}
		recordCtx, cancel := context.WithTimeout(ctx, streamAdvisoryGapRecordWait)
		err = writer.Record(recordCtx, job, reason)
		cancel()
	}
	if err != nil {
		log.Printf("[guardrail] failed to queue durable stream advisory gap: %v", err)
		return
	}
	if job.relay == nil || reason == streamAdvisoryGapShutdown {
		return
	}
	traceCtx := context.Background()
	if job.ctx != nil {
		traceCtx = context.WithoutCancel(job.ctx)
	}
	traceCtx, cancel := context.WithTimeout(traceCtx, time.Second)
	defer cancel()
	job.relay.recordTraceEvent(traceCtx, job.requestID, "guardrail.stream_advisory.gap", "stream_output", "warning", job.model, job.requestedModel, job.route, 200, 0, map[string]interface{}{
		"pre_verdict_disclosure": true,
		"reason":                 reason,
		"policy_count":           len(job.policies),
	}, job.upstreamRequestID)
}

// ShutdownStreamAdvisoryScheduler records queued and active work as shutdown
// gaps, waits for workers, and flushes the durable metadata writer.
func ShutdownStreamAdvisoryScheduler(ctx context.Context) error {
	defaultStreamAdvisoryRuntime.Lock()
	scheduler := defaultStreamAdvisoryRuntime.scheduler
	writer := defaultStreamAdvisoryRuntime.gapWriter
	defaultStreamAdvisoryRuntime.Unlock()
	if scheduler == nil {
		return nil
	}
	return shutdownStreamAdvisoryRuntime(ctx, scheduler, writer)
}

func shutdownStreamAdvisoryRuntime(ctx context.Context, scheduler *streamAdvisoryScheduler, writer *streamAdvisoryGapWriter) error {
	schedulerErr := scheduler.Close(ctx)
	writerErr := writer.Close(ctx)
	return joinStreamAdvisoryShutdownErrors(schedulerErr, writerErr)
}
