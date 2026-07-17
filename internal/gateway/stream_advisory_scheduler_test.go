package gateway

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/guardrail"
)

func TestStreamAdvisorySchedulerPreservesCrossTenantFairnessAndInflightBounds(t *testing.T) {
	started := make(chan int64, 8)
	releaseA := make(chan struct{})
	scheduler := newStreamAdvisoryScheduler(streamAdvisorySchedulerOptions{
		GlobalConcurrency: 2, GlobalQueueDepth: 8, OrgQueueDepth: 4, OrgConcurrency: 1,
		EvaluationTimeout: time.Second,
	}, func(ctx context.Context, job streamAdvisoryJob) {
		started <- job.orgID
		if job.orgID == 1 {
			select {
			case <-releaseA:
			case <-ctx.Done():
			}
		}
	}, func(context.Context, streamAdvisoryJob, string) {})
	t.Cleanup(func() {
		close(releaseA)
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = scheduler.Close(closeCtx)
	})

	if !scheduler.Submit(testAdvisoryJob(1, "a-1")) {
		t.Fatal("first tenant-A advisory was rejected")
	}
	select {
	case orgID := <-started:
		if orgID != 1 {
			t.Fatalf("first org = %d, want 1", orgID)
		}
	case <-time.After(time.Second):
		t.Fatal("first tenant-A advisory did not start")
	}
	if !scheduler.Submit(testAdvisoryJob(1, "a-2")) || !scheduler.Submit(testAdvisoryJob(2, "b-1")) {
		t.Fatal("bounded queued advisories were rejected")
	}
	select {
	case orgID := <-started:
		if orgID != 2 {
			t.Fatalf("tenant A consumed tenant B's runnable slot: got org %d", orgID)
		}
	case <-time.After(time.Second):
		t.Fatal("tenant B starved behind tenant A")
	}
}

func TestStreamAdvisorySchedulerOverloadTimeoutAndShutdownEmitExactPolicyGaps(t *testing.T) {
	type gapKey struct{ requestID, policy, reason string }
	var mu sync.Mutex
	gaps := make(map[gapKey]int64)
	record := func(_ context.Context, job streamAdvisoryJob, reason string) {
		mu.Lock()
		defer mu.Unlock()
		for _, policy := range job.policies {
			gaps[gapKey{requestID: job.requestID, policy: policy.Ref, reason: reason}]++
		}
	}
	started := make(chan struct{}, 1)
	scheduler := newStreamAdvisoryScheduler(streamAdvisorySchedulerOptions{
		GlobalConcurrency: 1, GlobalQueueDepth: 2, OrgQueueDepth: 1, OrgConcurrency: 1,
		EvaluationTimeout: 20 * time.Millisecond,
	}, func(ctx context.Context, _ streamAdvisoryJob) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
	}, record)

	if !scheduler.Submit(testAdvisoryJob(1, "active")) {
		t.Fatal("active job was rejected")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active job did not start")
	}
	if !scheduler.Submit(testAdvisoryJob(1, "queued")) {
		t.Fatal("one per-org queued job was rejected")
	}
	if scheduler.Submit(testAdvisoryJob(1, "overload")) {
		t.Fatal("per-org queue overflow was accepted")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		gotTimeout := gaps[gapKey{requestID: "active", policy: "guardrail:test", reason: streamAdvisoryGapTimeout}]
		mu.Unlock()
		if gotTimeout == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scheduler.Close(closeCtx); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if got := gaps[gapKey{requestID: "overload", policy: "guardrail:test", reason: streamAdvisoryGapOverload}]; got != 1 {
		t.Fatalf("overload gap count = %d, want 1", got)
	}
	if got := gaps[gapKey{requestID: "active", policy: "guardrail:test", reason: streamAdvisoryGapTimeout}]; got != 1 {
		t.Fatalf("timeout gap count = %d, want 1", got)
	}
}

func TestStreamAdvisorySchedulerShutdownEmitsExactActiveAndQueuedPolicyGaps(t *testing.T) {
	type gapKey struct{ requestID, policy, reason string }
	var mu sync.Mutex
	gaps := make(map[gapKey]int64)
	started := make(chan struct{}, 1)
	scheduler := newStreamAdvisoryScheduler(streamAdvisorySchedulerOptions{
		GlobalConcurrency: 1, GlobalQueueDepth: 2, OrgQueueDepth: 1, OrgConcurrency: 1,
		EvaluationTimeout: time.Second,
	}, func(ctx context.Context, _ streamAdvisoryJob) {
		started <- struct{}{}
		<-ctx.Done()
	}, func(_ context.Context, job streamAdvisoryJob, reason string) {
		mu.Lock()
		defer mu.Unlock()
		for _, policy := range job.policies {
			gaps[gapKey{requestID: job.requestID, policy: policy.Ref, reason: reason}]++
		}
	})

	if !scheduler.Submit(testAdvisoryJob(1, "active")) {
		t.Fatal("active job was rejected")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active job did not start")
	}
	if !scheduler.Submit(testAdvisoryJob(1, "queued")) {
		t.Fatal("queued job was rejected")
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scheduler.Close(closeCtx); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, requestID := range []string{"active", "queued"} {
		if got := gaps[gapKey{requestID: requestID, policy: "guardrail:test", reason: streamAdvisoryGapShutdown}]; got != 1 {
			t.Fatalf("%s shutdown gap count = %d, want 1", requestID, got)
		}
	}
}

func TestStreamAdvisorySchedulerSubmitDuringCloseUsesLiveShutdownContext(t *testing.T) {
	started := make(chan struct{}, 1)
	releaseWorker := make(chan struct{})
	activeGapStarted := make(chan struct{}, 1)
	releaseActiveGap := make(chan struct{})
	lateGapContextErr := make(chan error, 1)
	scheduler := newStreamAdvisoryScheduler(streamAdvisorySchedulerOptions{
		GlobalConcurrency: 1, GlobalQueueDepth: 2, OrgQueueDepth: 2, OrgConcurrency: 1,
		EvaluationTimeout: time.Second,
	}, func(ctx context.Context, _ streamAdvisoryJob) {
		started <- struct{}{}
		select {
		case <-releaseWorker:
		case <-ctx.Done():
		}
	}, func(ctx context.Context, job streamAdvisoryJob, reason string) {
		if reason != streamAdvisoryGapShutdown {
			return
		}
		switch job.requestID {
		case "active":
			activeGapStarted <- struct{}{}
			select {
			case <-releaseActiveGap:
			case <-ctx.Done():
			}
		case "late":
			lateGapContextErr <- ctx.Err()
		}
	})
	t.Cleanup(func() {
		select {
		case <-releaseWorker:
		default:
			close(releaseWorker)
		}
		select {
		case <-releaseActiveGap:
		default:
			close(releaseActiveGap)
		}
	})

	if !scheduler.Submit(testAdvisoryJob(1, "active")) {
		t.Fatal("active job was rejected")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active job did not start")
	}

	closeCtx, cancelClose := context.WithTimeout(context.Background(), time.Second)
	defer cancelClose()
	closeDone := make(chan error, 1)
	go func() { closeDone <- scheduler.Close(closeCtx) }()
	select {
	case <-activeGapStarted:
	case <-time.After(time.Second):
		t.Fatal("Close did not begin active shutdown gap")
	}

	if scheduler.Submit(testAdvisoryJob(1, "late")) {
		t.Fatal("submit after shutdown began was accepted")
	}
	select {
	case err := <-lateGapContextErr:
		if err != nil {
			t.Fatalf("late shutdown gap context was already canceled: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("late submit did not emit shutdown gap")
	}

	close(releaseActiveGap)
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish")
	}
}

func TestStreamAdvisorySchedulerBoundsJobsBytesAndInflight(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	overloads := make(map[string]int)
	scheduler := newStreamAdvisoryScheduler(streamAdvisorySchedulerOptions{
		GlobalConcurrency: 1, GlobalQueueDepth: 8, OrgQueueDepth: 4, OrgConcurrency: 1,
		MaxJobTextBytes: 8, GlobalQueueBytes: 10, OrgQueueBytes: 6,
		EvaluationTimeout: time.Second,
	}, func(ctx context.Context, _ streamAdvisoryJob) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
	}, func(_ context.Context, job streamAdvisoryJob, reason string) {
		if reason == streamAdvisoryGapOverload {
			mu.Lock()
			overloads[job.requestID]++
			mu.Unlock()
		}
	})
	active := testAdvisoryJobWithBytes(1, "active", 6)
	if !scheduler.Submit(active) {
		t.Fatal("active job within byte cap was rejected")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active job did not start")
	}
	if !scheduler.Submit(testAdvisoryJobWithBytes(1, "org-queued", 6)) {
		t.Fatal("per-org byte budget rejected its exact boundary")
	}
	if scheduler.Submit(testAdvisoryJobWithBytes(1, "org-byte-overload", 1)) {
		t.Fatal("per-org queued byte overflow was accepted")
	}
	if !scheduler.Submit(testAdvisoryJobWithBytes(2, "global-queued", 4)) {
		t.Fatal("global byte budget rejected its exact boundary")
	}
	if scheduler.Submit(testAdvisoryJobWithBytes(3, "global-byte-overload", 1)) {
		t.Fatal("global queued byte overflow was accepted")
	}
	if scheduler.Submit(testAdvisoryJobWithBytes(4, "job-byte-overload", 9)) {
		t.Fatal("oversized single job was accepted")
	}

	scheduler.mu.Lock()
	if scheduler.queued != 2 || scheduler.queuedBytes != 10 || len(scheduler.active) != 1 || scheduler.inflight[1] != 1 {
		t.Fatalf("scheduler bounds queued/bytes/active/inflight = %d/%d/%d/%d, want 2/10/1/1",
			scheduler.queued, scheduler.queuedBytes, len(scheduler.active), scheduler.inflight[1])
	}
	if scheduler.orgQueuedBytes[1] != 6 || scheduler.orgQueuedBytes[2] != 4 {
		t.Fatalf("per-org queued bytes = %#v, want org1=6 org2=4", scheduler.orgQueuedBytes)
	}
	scheduler.mu.Unlock()
	mu.Lock()
	for _, requestID := range []string{"org-byte-overload", "global-byte-overload", "job-byte-overload"} {
		if overloads[requestID] != 1 {
			t.Fatalf("%s overload gaps = %d, want 1", requestID, overloads[requestID])
		}
	}
	mu.Unlock()

	close(release)
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scheduler.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
}

func testAdvisoryJob(orgID int64, requestID string) streamAdvisoryJob {
	return streamAdvisoryJob{
		orgID: orgID, requestID: requestID,
		policies: []guardrail.PolicyIdentity{{Ref: "guardrail:test", Version: 3}},
	}
}

func testAdvisoryJobWithBytes(orgID int64, requestID string, textBytes int) streamAdvisoryJob {
	job := testAdvisoryJob(orgID, requestID)
	job.texts = []guardrail.Text{{Content: strings.Repeat("x", textBytes)}}
	return job
}
