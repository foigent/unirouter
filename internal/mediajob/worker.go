package mediajob

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Worker struct {
	store         Store
	objects       ObjectStore
	provider      Provider
	cipher        PayloadCipher
	owner         string
	leaseDuration time.Duration
	purgeLease    time.Duration
	pollInterval  time.Duration
	retention     time.Duration
	now           func() time.Time
	onError       func(error)
	errorMu       sync.Mutex
}

type WorkerOptions struct {
	Owner              string
	LeaseDuration      time.Duration
	PurgeLeaseDuration time.Duration
	PollInterval       time.Duration
	Now                func() time.Time
	OnError            func(error)
}

func NewWorker(
	store Store,
	objects ObjectStore,
	provider Provider,
	cipher PayloadCipher,
	options WorkerOptions,
) (*Worker, error) {
	options.Owner = strings.TrimSpace(options.Owner)
	if store == nil || objects == nil || provider == nil || cipher == nil ||
		options.Owner == "" || len(options.Owner) > 128 {
		return nil, domainError(ErrUnavailable, "worker_dependency_invalid")
	}
	if options.LeaseDuration == 0 {
		options.LeaseDuration = 5 * time.Minute
	}
	if options.PurgeLeaseDuration == 0 {
		options.PurgeLeaseDuration = time.Minute
	}
	if options.PollInterval == 0 {
		options.PollInterval = 500 * time.Millisecond
	}
	if options.LeaseDuration < time.Minute ||
		options.PurgeLeaseDuration < 5*time.Second ||
		options.PollInterval < 50*time.Millisecond ||
		options.PollInterval > time.Minute {
		return nil, domainError(ErrInvalid, "worker_timing_invalid")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Worker{
		store: store, objects: objects, provider: provider, cipher: cipher,
		owner: options.Owner, leaseDuration: options.LeaseDuration,
		purgeLease:   options.PurgeLeaseDuration,
		pollInterval: options.PollInterval,
		retention:    DefaultRetention, now: options.Now,
		onError: options.OnError,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	if ctx == nil {
		return domainError(ErrInvalid, "worker_context_invalid")
	}
	var loops sync.WaitGroup
	loops.Add(2)
	go func() {
		defer loops.Done()
		w.runJobLoop(ctx)
	}()
	go func() {
		defer loops.Done()
		w.runPurgeLoop(ctx)
	}()
	<-ctx.Done()
	loops.Wait()
	return ctx.Err()
}

func (w *Worker) runJobLoop(ctx context.Context) {
	timer := time.NewTicker(w.pollInterval)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if _, err := w.RunOnce(ctx); err != nil &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			w.reportError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) runPurgeLoop(ctx context.Context) {
	timer := time.NewTicker(w.pollInterval)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if _, err := w.RunPurgeOnce(ctx); err != nil &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			w.reportError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	now := w.now().UTC()
	job, err := w.store.Claim(ctx, w.owner, now, w.leaseDuration)
	if err != nil || job == nil {
		return false, err
	}
	lease := Lease{
		JobID: job.JobID, Scope: job.Scope, Owner: job.LeaseOwner,
		Fence: job.LeaseFence, Attempt: job.Attempt,
		Job: *job,
	}
	if job.LeaseExpiresAt != nil {
		lease.ExpiresAt = job.LeaseExpiresAt.UTC()
	}

	recovered, err := w.objects.Recover(ctx, *job)
	if err != nil {
		if job.Lifecycle.Outcome == OutcomeSucceeded {
			return true, w.retryRecoveryOrFail(
				ctx, lease, "object_recovery_failed",
			)
		}
		return true, w.retryOrFail(ctx, lease, "object_recovery_failed", true)
	}
	if recovered != nil {
		_, err = w.store.CompleteSuccess(
			ctx, lease, *recovered, w.retention, w.now().UTC(),
		)
		if errors.Is(err, ErrLeaseLost) {
			return true, nil
		}
		return true, err
	}
	if job.Lifecycle.Outcome == OutcomeSucceeded {
		return true, w.retryRecoveryOrFail(
			ctx, lease, "persisted_object_unavailable",
		)
	}

	request, err := decodeStoredRequest(w.cipher, *job)
	if err != nil {
		_, completeErr := w.store.CompleteFailure(
			ctx, lease, "stored_request_unavailable", w.now().UTC(),
		)
		if completeErr != nil {
			return true, completeErr
		}
		return true, err
	}
	lease.Request = request
	if err := w.store.MarkRunning(ctx, lease, w.now().UTC()); err != nil {
		if errors.Is(err, ErrCancelled) || errors.Is(err, ErrLeaseLost) {
			return true, nil
		}
		return true, err
	}
	providerResult, err := w.provider.Generate(ctx, *job, request)
	if err != nil {
		code, retryable, outcomeUnknown := providerFailure(err)
		if outcomeUnknown {
			_, completeErr := w.store.CompleteUnknown(
				ctx, lease, code, w.now().UTC(),
			)
			return true, completeErr
		}
		return true, w.retryOrFail(ctx, lease, code, retryable)
	}
	if err := w.markProviderSucceeded(
		ctx, lease, providerResult.MappedModel,
	); err != nil {
		return true, err
	}
	lease.Job.Lifecycle.Outcome = OutcomeSucceeded
	lease.Job.MappedModel = providerResult.MappedModel
	persisted, err := w.objects.Persist(ctx, lease.Job, providerResult)
	if err != nil {
		if recovered, recoverErr := w.objects.Recover(ctx, lease.Job); recoverErr == nil &&
			recovered != nil {
			_, completeErr := w.store.CompleteSuccess(
				ctx, lease, *recovered, w.retention, w.now().UTC(),
			)
			if errors.Is(completeErr, ErrLeaseLost) {
				return true, nil
			}
			return true, completeErr
		}
		return true, w.retryRecoveryOrFail(
			ctx, lease, objectFailureCode(err),
		)
	}
	_, err = w.store.CompleteSuccess(
		ctx, lease, persisted, w.retention, w.now().UTC(),
	)
	if errors.Is(err, ErrLeaseLost) {
		// The immutable receipt is intentionally retained. A reclaimed worker
		// can finish from Recover without another provider call.
		return true, nil
	}
	return true, err
}

func (w *Worker) markProviderSucceeded(
	ctx context.Context,
	lease Lease,
	mappedModel string,
) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		lastErr = w.store.MarkProviderSucceeded(
			persistCtx, lease, mappedModel, w.now().UTC(),
		)
		if lastErr == nil || errors.Is(lastErr, ErrLeaseLost) {
			return lastErr
		}
	}
	observed, observeErr := w.store.Get(
		persistCtx,
		lease.Scope,
		lease.JobID,
	)
	if observeErr == nil &&
		observed.Lifecycle.Queue == QueueLeased &&
		observed.Lifecycle.Outcome == OutcomeSucceeded &&
		observed.MappedModel == mappedModel &&
		observed.LeaseOwner == lease.Owner &&
		observed.LeaseFence == lease.Fence &&
		observed.LeaseExpiresAt != nil &&
		w.now().UTC().Before(observed.LeaseExpiresAt.UTC()) {
		// The success-fence write committed even though its response was lost.
		// The exact lease/fence observation is required before retaining the
		// in-memory provider output and proceeding to immutable persistence.
		return nil
	}
	return lastErr
}

func (w *Worker) retryRecoveryOrFail(
	ctx context.Context,
	lease Lease,
	code string,
) error {
	now := w.now().UTC()
	if lease.Attempt < lease.Job.MaxAttempts {
		_, err := w.store.RequeueRecovery(
			ctx,
			lease,
			code,
			now,
			now.Add(retryBackoff(lease.Attempt)),
		)
		return err
	}
	_, err := w.store.CompleteOutputFailure(ctx, lease, code, now)
	return err
}

func objectFailureCode(err error) string {
	var contractErr *Error
	if errors.As(err, &contractErr) && contractErr.Code != "" {
		return contractErr.Code
	}
	return "object_persist_failed"
}

func (w *Worker) retryOrFail(
	ctx context.Context,
	lease Lease,
	code string,
	retryable bool,
) error {
	now := w.now().UTC()
	if retryable && lease.Attempt < lease.Job.MaxAttempts {
		_, err := w.store.Requeue(
			ctx,
			lease,
			code,
			now,
			now.Add(retryBackoff(lease.Attempt)),
		)
		return err
	}
	_, err := w.store.CompleteFailure(ctx, lease, code, now)
	return err
}

func (w *Worker) RunPurgeOnce(ctx context.Context) (bool, error) {
	now := w.now().UTC()
	lease, err := w.store.ClaimExpired(
		ctx, w.owner+"-purge", now, w.purgeLease,
	)
	if err != nil || lease == nil {
		return false, err
	}
	if err := w.objects.Delete(ctx, lease.Job); err != nil {
		retryAt := w.now().UTC().Add(retryBackoff(lease.Job.PurgeAttempt))
		if retryErr := w.store.MarkPurgeRetry(
			ctx, *lease, "object_delete_failed", w.now().UTC(), retryAt,
		); retryErr != nil {
			return true, retryErr
		}
		return true, err
	}
	_, err = w.store.MarkPurged(ctx, *lease, w.now().UTC())
	if errors.Is(err, ErrLeaseLost) {
		return true, nil
	}
	return true, err
}

func (w *Worker) reportError(err error) {
	if w.onError != nil && err != nil {
		w.errorMu.Lock()
		defer w.errorMu.Unlock()
		w.onError(err)
	}
}

func retryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}

func (w *Worker) String() string {
	if w == nil {
		return "mediajob.Worker<nil>"
	}
	return fmt.Sprintf("mediajob.Worker<%s>", w.owner)
}
