package mediajob

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type workerStoreFixture struct {
	job               *Job
	observedJob       *Job
	markRunningErr    error
	markProviderErr   error
	markProviderCalls int
	providerMarked    bool
	successes         int
	failures          int
	outputFailures    int
	unknowns          int
	requeues          int
	recoverQueues     int
	lastPersisted     PersistedResult
	purgeLease        *PurgeLease
	purgeRetries      int
	purged            int
}

func (s *workerStoreFixture) CreateOrGet(context.Context, CreateRecord) (Job, bool, error) {
	return Job{}, false, nil
}
func (s *workerStoreFixture) Get(context.Context, Scope, string) (Job, error) {
	if s.observedJob != nil {
		return cloneJob(*s.observedJob), nil
	}
	return Job{}, ErrNotFound
}
func (s *workerStoreFixture) List(context.Context, ListFilter) (ListResult, error) {
	return ListResult{}, nil
}
func (s *workerStoreFixture) RequestCancellation(
	context.Context, Scope, string, time.Time,
) (CancellationResult, error) {
	return CancellationResult{}, nil
}
func (s *workerStoreFixture) Claim(
	context.Context, string, time.Time, time.Duration,
) (*Job, error) {
	if s.job == nil {
		return nil, nil
	}
	job := cloneJob(*s.job)
	s.job = nil
	return &job, nil
}
func (s *workerStoreFixture) MarkRunning(context.Context, Lease, time.Time) error {
	return s.markRunningErr
}
func (s *workerStoreFixture) MarkProviderSucceeded(
	context.Context, Lease, string, time.Time,
) error {
	s.markProviderCalls++
	s.providerMarked = true
	return s.markProviderErr
}
func (s *workerStoreFixture) CompleteSuccess(
	_ context.Context,
	_ Lease,
	result PersistedResult,
	_ time.Duration,
	_ time.Time,
) (Job, error) {
	s.successes++
	s.lastPersisted = result
	return Job{}, nil
}
func (s *workerStoreFixture) CompleteFailure(
	context.Context, Lease, string, time.Time,
) (Job, error) {
	s.failures++
	return Job{}, nil
}
func (s *workerStoreFixture) CompleteUnknown(
	context.Context, Lease, string, time.Time,
) (Job, error) {
	s.unknowns++
	return Job{}, nil
}
func (s *workerStoreFixture) CompleteOutputFailure(
	context.Context, Lease, string, time.Time,
) (Job, error) {
	s.outputFailures++
	return Job{}, nil
}
func (s *workerStoreFixture) Requeue(
	context.Context, Lease, string, time.Time, time.Time,
) (Job, error) {
	s.requeues++
	return Job{}, nil
}
func (s *workerStoreFixture) RequeueRecovery(
	context.Context, Lease, string, time.Time, time.Time,
) (Job, error) {
	s.recoverQueues++
	return Job{}, nil
}
func (s *workerStoreFixture) ClaimExpired(
	context.Context, string, time.Time, time.Duration,
) (*PurgeLease, error) {
	if s.purgeLease == nil {
		return nil, nil
	}
	lease := *s.purgeLease
	s.purgeLease = nil
	return &lease, nil
}
func (s *workerStoreFixture) MarkPurgeRetry(
	context.Context, PurgeLease, string, time.Time, time.Time,
) error {
	s.purgeRetries++
	return nil
}
func (s *workerStoreFixture) MarkPurged(
	context.Context, PurgeLease, time.Time,
) (Job, error) {
	s.purged++
	return Job{}, nil
}

type workerObjectFixture struct {
	recovered     *PersistedResult
	recoverErr    error
	persisted     PersistedResult
	persistErr    error
	deleteErr     error
	persistCalls  int
	deleteCalls   int
	beforePersist func()
	deleteSignal  chan struct{}
}

func (o *workerObjectFixture) Persist(
	context.Context, Job, ProviderResult,
) (PersistedResult, error) {
	o.persistCalls++
	if o.beforePersist != nil {
		o.beforePersist()
	}
	return o.persisted, o.persistErr
}
func (o *workerObjectFixture) Recover(
	context.Context, Job,
) (*PersistedResult, error) {
	return o.recovered, o.recoverErr
}
func (o *workerObjectFixture) Read(context.Context, Job) (OutputContent, error) {
	return OutputContent{}, nil
}
func (o *workerObjectFixture) Delete(context.Context, Job) error {
	o.deleteCalls++
	if o.deleteSignal != nil {
		select {
		case o.deleteSignal <- struct{}{}:
		default:
		}
	}
	return o.deleteErr
}

type workerProviderFixture struct {
	result ProviderResult
	err    error
	calls  int
}

type blockingWorkerProvider struct {
	started chan struct{}
	once    sync.Once
}

func (p *blockingWorkerProvider) Generate(
	ctx context.Context,
	_ Job,
	_ NormalizedRequest,
) (ProviderResult, error) {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return ProviderResult{}, ctx.Err()
}

func (p *workerProviderFixture) Generate(
	context.Context, Job, NormalizedRequest,
) (ProviderResult, error) {
	p.calls++
	return p.result, p.err
}

func TestWorkerRecoversPersistedReceiptWithoutSecondProviderCall(t *testing.T) {
	now := time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC)
	job, cipher := leasedWorkerJob(t, now)
	persisted := persistedFixture(now)
	store := &workerStoreFixture{job: &job}
	objects := &workerObjectFixture{recovered: &persisted}
	provider := &workerProviderFixture{}
	worker := newWorkerFixture(t, store, objects, provider, cipher, now)
	claimed, err := worker.RunOnce(context.Background())
	if err != nil || !claimed || store.successes != 1 ||
		provider.calls != 0 || objects.persistCalls != 0 {
		t.Fatalf(
			"claimed=%t err=%v successes=%d provider=%d persist=%d",
			claimed, err, store.successes, provider.calls, objects.persistCalls,
		)
	}
}

func TestWorkerCancellationAndRetryBoundaries(t *testing.T) {
	now := time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC)
	t.Run("cancelled before dispatch", func(t *testing.T) {
		job, cipher := leasedWorkerJob(t, now)
		store := &workerStoreFixture{job: &job, markRunningErr: ErrCancelled}
		objects := &workerObjectFixture{}
		provider := &workerProviderFixture{}
		worker := newWorkerFixture(t, store, objects, provider, cipher, now)
		claimed, err := worker.RunOnce(context.Background())
		if err != nil || !claimed || provider.calls != 0 || objects.persistCalls != 0 {
			t.Fatalf("claimed=%t err=%v provider=%d persist=%d", claimed, err, provider.calls, objects.persistCalls)
		}
	})
	t.Run("retryable provider failure", func(t *testing.T) {
		job, cipher := leasedWorkerJob(t, now)
		store := &workerStoreFixture{job: &job}
		objects := &workerObjectFixture{}
		provider := &workerProviderFixture{
			err: &ProviderError{Code: "provider_timeout", Retryable: true},
		}
		worker := newWorkerFixture(t, store, objects, provider, cipher, now)
		_, err := worker.RunOnce(context.Background())
		if err != nil || store.requeues != 1 || store.failures != 0 {
			t.Fatalf("err=%v requeues=%d failures=%d", err, store.requeues, store.failures)
		}
	})
	t.Run("attempt exhaustion", func(t *testing.T) {
		job, cipher := leasedWorkerJob(t, now)
		job.Attempt = job.MaxAttempts
		store := &workerStoreFixture{job: &job}
		objects := &workerObjectFixture{}
		provider := &workerProviderFixture{
			err: &ProviderError{Code: "provider_timeout", Retryable: true},
		}
		worker := newWorkerFixture(t, store, objects, provider, cipher, now)
		_, err := worker.RunOnce(context.Background())
		if err != nil || store.requeues != 0 || store.failures != 1 {
			t.Fatalf("err=%v requeues=%d failures=%d", err, store.requeues, store.failures)
		}
	})
	t.Run("ambiguous provider outcome is terminal and never retried", func(t *testing.T) {
		job, cipher := leasedWorkerJob(t, now)
		store := &workerStoreFixture{job: &job}
		objects := &workerObjectFixture{}
		provider := &workerProviderFixture{
			err: &ProviderError{
				Code: "provider_transport_outcome_unknown", OutcomeUnknown: true,
			},
		}
		worker := newWorkerFixture(t, store, objects, provider, cipher, now)
		_, err := worker.RunOnce(context.Background())
		if err != nil || store.unknowns != 1 || store.requeues != 0 ||
			provider.calls != 1 {
			t.Fatalf(
				"err=%v unknowns=%d requeues=%d provider=%d",
				err, store.unknowns, store.requeues, provider.calls,
			)
		}
	})
	t.Run("unclassified provider error fails closed as unknown", func(t *testing.T) {
		job, cipher := leasedWorkerJob(t, now)
		store := &workerStoreFixture{job: &job}
		objects := &workerObjectFixture{}
		provider := &workerProviderFixture{
			err: errors.New("provider implementation omitted outcome classification"),
		}
		worker := newWorkerFixture(t, store, objects, provider, cipher, now)
		_, err := worker.RunOnce(context.Background())
		if err != nil || store.unknowns != 1 || store.requeues != 0 ||
			store.failures != 0 || provider.calls != 1 {
			t.Fatalf(
				"err=%v unknowns=%d requeues=%d failures=%d provider=%d",
				err, store.unknowns, store.requeues, store.failures,
				provider.calls,
			)
		}
	})
}

func TestWorkerPersistsBeforeSuccessAndRetriesPurge(t *testing.T) {
	now := time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC)
	job, cipher := leasedWorkerJob(t, now)
	persisted := persistedFixture(now)
	store := &workerStoreFixture{job: &job}
	objects := &workerObjectFixture{persisted: persisted}
	objects.beforePersist = func() {
		if !store.providerMarked {
			t.Fatal("output persistence began before provider success was fenced")
		}
	}
	provider := &workerProviderFixture{result: ProviderResult{
		MappedModel: "gpt-image-1", Bytes: fixturePNG(),
		MediaType: "image/png", Outcome: imageOutcome(),
	}}
	worker := newWorkerFixture(t, store, objects, provider, cipher, now)
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if provider.calls != 1 || objects.persistCalls != 1 ||
		store.successes != 1 || store.lastPersisted.ContentSHA256 == "" {
		t.Fatalf("provider=%d persist=%d success=%d result=%+v", provider.calls, objects.persistCalls, store.successes, store.lastPersisted)
	}

	purgeJob := job
	purgeJob.PurgeAttempt = 1
	purgeJob.ObjectKey = persisted.ObjectKey
	purgeJob.ReceiptKey = persisted.ReceiptKey
	store.purgeLease = &PurgeLease{Job: purgeJob, Owner: "worker-purge", Fence: 1}
	objects.deleteErr = errors.New("fixture object outage")
	claimed, err := worker.RunPurgeOnce(context.Background())
	if !claimed || err == nil || store.purgeRetries != 1 || store.purged != 0 {
		t.Fatalf("claimed=%t err=%v retries=%d purged=%d", claimed, err, store.purgeRetries, store.purged)
	}
}

func TestWorkerRecoversAmbiguousProviderSuccessFenceCommit(t *testing.T) {
	now := time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC)
	job, cipher := leasedWorkerJob(t, now)
	observed := cloneJob(job)
	observed.Lifecycle.Outcome = OutcomeSucceeded
	observed.MappedModel = "gpt-image-1"
	store := &workerStoreFixture{
		job:             &job,
		observedJob:     &observed,
		markProviderErr: errors.New("commit response unavailable"),
	}
	persisted := persistedFixture(now)
	objects := &workerObjectFixture{persisted: persisted}
	provider := &workerProviderFixture{result: ProviderResult{
		MappedModel: "gpt-image-1", Bytes: fixturePNG(),
		MediaType: "image/png", Outcome: imageOutcome(),
	}}
	worker := newWorkerFixture(t, store, objects, provider, cipher, now)

	claimed, err := worker.RunOnce(context.Background())
	if err != nil || !claimed ||
		store.markProviderCalls != 3 ||
		provider.calls != 1 ||
		objects.persistCalls != 1 ||
		store.successes != 1 {
		t.Fatalf(
			"claimed=%t err=%v mark_calls=%d provider=%d persist=%d successes=%d",
			claimed, err, store.markProviderCalls, provider.calls,
			objects.persistCalls, store.successes,
		)
	}
}

func TestWorkerNeverCallsProviderAgainAfterValidatedProviderSuccess(t *testing.T) {
	now := time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC)
	job, cipher := leasedWorkerJob(t, now)
	store := &workerStoreFixture{job: &job}
	objects := &workerObjectFixture{
		persistErr: domainError(ErrUnavailable, "image_put_failed"),
	}
	provider := &workerProviderFixture{result: ProviderResult{
		MappedModel: "gpt-image-1", Bytes: fixturePNG(),
		MediaType: "image/png", Outcome: imageOutcome(),
	}}
	worker := newWorkerFixture(t, store, objects, provider, cipher, now)
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("provider-success run: %v", err)
	}
	if provider.calls != 1 || !store.providerMarked || store.recoverQueues != 1 {
		t.Fatalf(
			"provider=%d marked=%t recovery_queues=%d",
			provider.calls, store.providerMarked, store.recoverQueues,
		)
	}

	recovery := job
	recovery.RequestCiphertext = ""
	recovery.Lifecycle.Outcome = OutcomeSucceeded
	recovery.Lifecycle.Execution = ExecutionPending
	recovery.Attempt = 2
	store.job = &recovery
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("recovery-only run: %v", err)
	}
	if provider.calls != 1 || store.recoverQueues != 2 {
		t.Fatalf(
			"recovery called provider: provider=%d queues=%d",
			provider.calls, store.recoverQueues,
		)
	}

	recovery.Attempt = recovery.MaxAttempts
	store.job = &recovery
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("recovery exhaustion: %v", err)
	}
	if provider.calls != 1 || store.outputFailures != 1 {
		t.Fatalf(
			"exhaustion provider=%d output_failures=%d",
			provider.calls, store.outputFailures,
		)
	}
}

func TestWorkerPurgeLoopIsNotBlockedByProviderExecution(t *testing.T) {
	now := time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC)
	job, cipher := leasedWorkerJob(t, now)
	purgeJob := job
	purgeJob.PurgeAttempt = 1
	store := &workerStoreFixture{
		job: &job,
		purgeLease: &PurgeLease{
			Job: purgeJob, Owner: "worker-purge", Fence: 1,
		},
	}
	objects := &workerObjectFixture{deleteSignal: make(chan struct{}, 1)}
	provider := &blockingWorkerProvider{started: make(chan struct{})}
	worker, err := NewWorker(
		store,
		objects,
		provider,
		cipher,
		WorkerOptions{
			Owner:              "worker-concurrency",
			LeaseDuration:      time.Minute,
			PurgeLeaseDuration: time.Minute,
			PollInterval:       50 * time.Millisecond,
			Now:                func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("provider execution did not start")
	}
	select {
	case <-objects.deleteSignal:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("purge was blocked by the active provider execution")
	}
	cancel()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("run error = %v", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}

func leasedWorkerJob(t *testing.T, now time.Time) (Job, PayloadCipher) {
	t.Helper()
	cipher, err := NewApplicationCipher([]byte(strings.Repeat("w", 32)))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	request := NormalizedRequest{Model: "company-image", Prompt: "fixture", N: 1}
	hash, body, err := RequestSHA256(request)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	ciphertext, err := cipher.Seal(body)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	expires := now.Add(5 * time.Minute)
	return Job{
		ContractVersion: ContractVersion,
		JobID:           "mediajob_0123456789abcdef0123456789abcdef",
		Scope:           Scope{OrgID: 1, ProjectID: 2},
		ProfileID:       "unirouter.openai.images.gpt-image.v1",
		RequestedModel:  "company-image", RoutingModel: "company-image",
		RequestSHA256: hash, RequestCiphertext: ciphertext,
		Lifecycle: Lifecycle{
			Queue: QueueLeased, Lease: LeaseLeased,
			Execution: ExecutionPending, Outcome: OutcomePending,
			Cancellation: CancellationNotRequested, Object: ObjectPending,
		},
		Attempt: 1, MaxAttempts: 3,
		LeaseOwner: "worker", LeaseFence: 1, LeaseExpiresAt: &expires,
	}, cipher
}

func persistedFixture(now time.Time) PersistedResult {
	return PersistedResult{
		ObjectKey: "internal/image", ReceiptKey: "internal/receipt",
		ContentSHA256: sha256Digest(fixturePNG()), MediaType: "image/png",
		SizeBytes: int64(len(fixturePNG())), PersistedAt: now,
		MappedModel: "gpt-image-1", Outcome: imageOutcome(),
	}
}

func newWorkerFixture(
	t *testing.T,
	store Store,
	objects ObjectStore,
	provider Provider,
	cipher PayloadCipher,
	now time.Time,
) *Worker {
	t.Helper()
	worker, err := NewWorker(
		store, objects, provider, cipher,
		WorkerOptions{
			Owner: "worker", LeaseDuration: 5 * time.Minute,
			PurgeLeaseDuration: time.Minute, PollInterval: time.Second,
			Now: func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatalf("worker: %v", err)
	}
	return worker
}
