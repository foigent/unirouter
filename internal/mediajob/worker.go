package mediajob

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/workloadadmission"
)

type Worker struct {
	store         WorkerStore
	objects       ObjectStore
	provider      Provider
	cipher        PayloadCipher
	limits        LimitPolicyRuntime
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
	LimitPolicy        LimitPolicyRuntime
}

func NewWorker(
	store WorkerStore,
	objects ObjectStore,
	provider Provider,
	cipher PayloadCipher,
	options WorkerOptions,
) (*Worker, error) {
	options.Owner = strings.TrimSpace(options.Owner)
	if store == nil || objects == nil || provider == nil || cipher == nil || options.LimitPolicy == nil ||
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
		store: store, objects: objects, provider: provider, cipher: cipher, limits: options.LimitPolicy,
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
	if reconciled, err := w.reconcileExecutionTerminal(ctx, "", 0); reconciled || err != nil {
		return reconciled, err
	}
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
	limitSubject, err := w.store.ExecutionLimitSubject(ctx, lease)
	if err != nil {
		code := "execution_authority_unavailable"
		if errors.Is(err, ErrForbidden) {
			code = "execution_authority_changed"
		}
		_, completeErr := w.store.CompleteFailure(ctx, lease, code, w.now().UTC())
		if errors.Is(completeErr, ErrLeaseLost) {
			completeErr = nil
		}
		return true, errors.Join(err, completeErr)
	}
	now = w.now().UTC()
	leaseTTL := lease.ExpiresAt.Sub(now)
	if leaseTTL <= 0 {
		return true, ErrLeaseLost
	}
	operationID := mediaLimitOperationID("execution", fmt.Sprintf("%s:%d", lease.JobID, lease.Fence))
	reservation, err := w.limits.Reserve(ctx, limitpolicy.AdmissionRequest{
		OperationID:   operationID,
		Subject:       limitSubject,
		LeaseTTL:      leaseTTL,
		WorkloadClass: workloadadmission.ClassInteractive,
		Phase:         limitpolicy.AdmissionPhaseExecution,
	})
	if err != nil {
		return true, w.retryOrFail(ctx, lease, executionLimitErrorCode(err), true)
	}
	expiresAt := now.Add(w.limits.TerminalStateTTL(leaseTTL))
	attached, _, err := w.store.AttachExecutionReservation(
		ctx, lease, limitSubject, reservation.Durable(), operationID, expiresAt, now,
	)
	if err != nil {
		cleanupErr := w.cancelExecutionBeforeDispatch(ctx, reservation)
		switch {
		case errors.Is(err, ErrCancelled):
			_, reconcileErr := w.reconcileExecutionTerminal(ctx, lease.JobID, lease.Fence)
			return true, errors.Join(cleanupErr, reconcileErr)
		case errors.Is(err, ErrLeaseLost):
			return true, cleanupErr
		case errors.Is(err, ErrForbidden), errors.Is(err, ErrConflict), errors.Is(err, ErrUnavailable):
			code := "execution_authority_changed"
			if errors.Is(err, ErrUnavailable) {
				code = "execution_authority_unavailable"
			}
			_, completeErr := w.store.CompleteFailure(
				ctx, lease, code, w.now().UTC(),
			)
			if errors.Is(completeErr, ErrLeaseLost) {
				completeErr = nil
			}
			return true, errors.Join(err, cleanupErr, completeErr)
		default:
			transitionErr := w.retryOrFail(ctx, lease, "execution_admission_persist_failed", true)
			return true, errors.Join(err, cleanupErr, transitionErr)
		}
	}
	lease.Attempt = attached.ProviderAttempt
	lease.Job.Attempt = attached.ProviderAttempt
	if provider, ok := w.provider.(interface{ SourceObservationsEnabled() bool }); ok && provider.SourceObservationsEnabled() {
		var observations *sourceMediaSamples
		ctx, observations = startSourceMediaSamples(ctx)
		defer func() {
			if sink, ok := w.store.(interface {
				CaptureSourceMediaRoutes(context.Context, Job, []SourceMediaHTTP)
			}); ok {
				sink.CaptureSourceMediaRoutes(ctx, lease.Job, observations.samples)
			}
		}()
	}
	dispatchCtx, cancelDispatch := context.WithDeadline(ctx, lease.ExpiresAt)
	// AttachExecutionReservation is the provider-dispatch fence: it is the
	// first transition that consumes a provider attempt. Pass that exact
	// persisted attempt to the route/provider so queue leasing and admission
	// contention never masquerade as provider work.
	providerResult, err := w.provider.Generate(dispatchCtx, lease.Job, request)
	cancelDispatch()
	if err != nil {
		code, retryable, outcomeUnknown := providerFailure(err)
		if outcomeUnknown {
			_, completeErr := w.store.CompleteUnknown(
				ctx, lease, code, w.now().UTC(),
			)
			return true, w.finishExecutionTransition(ctx, lease, completeErr)
		}
		return true, w.finishExecutionTransition(ctx, lease, w.retryOrFail(ctx, lease, code, retryable))
	}
	if err := w.markProviderSucceeded(
		ctx, lease, providerResult.MappedModel,
	); err != nil {
		return true, err
	}
	if err := w.finishExecutionTransition(ctx, lease, nil); err != nil {
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

func executionLimitErrorCode(err error) string {
	switch {
	case errors.Is(err, limitpolicy.ErrConcurrencyExceeded):
		return "limit_policy.concurrency_exceeded"
	case errors.Is(err, limitpolicy.ErrWorkloadCapacity):
		return "workload_admission.capacity_exceeded"
	case errors.Is(err, limitpolicy.ErrWorkloadConfig):
		return "workload_admission.configuration_conflict"
	case errors.Is(err, limitpolicy.ErrConflict):
		return "limit_policy.operation_conflict"
	default:
		return "limit_policy.state_unavailable"
	}
}

func (w *Worker) cancelExecutionBeforeDispatch(ctx context.Context, reservation *limitpolicy.Reservation) error {
	if reservation == nil {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return w.limits.CancelBeforeDispatch(cleanupCtx, reservation)
}

func (w *Worker) finishExecutionTransition(ctx context.Context, lease Lease, transitionErr error) error {
	if transitionErr != nil {
		if errors.Is(transitionErr, ErrLeaseLost) {
			return nil
		}
		return transitionErr
	}
	_, err := w.reconcileExecutionTerminal(ctx, lease.JobID, lease.Fence)
	return err
}

func (w *Worker) reconcileExecutionTerminal(ctx context.Context, jobID string, leaseFence int64) (bool, error) {
	now := w.now().UTC()
	item, err := w.store.PendingExecutionTerminal(
		ctx, jobID, leaseFence, w.owner+"-limit-terminal", now, 15*time.Second,
	)
	if err != nil || item == nil {
		return false, err
	}
	if !now.Before(item.ExpiresAt) {
		return true, w.store.MarkExecutionTerminalized(ctx, *item, now, "operation_state_expired")
	}
	reservation, err := limitpolicy.RestoreDurableReservation(&item.Reservation)
	if err == nil {
		terminalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		if item.TerminalKind == "settled" {
			err = w.limits.Finalize(terminalCtx, reservation, 0)
		} else {
			err = w.limits.Cancel(terminalCtx, reservation)
		}
		cancel()
	}
	if err != nil {
		now = w.now().UTC()
		if !now.Before(item.ExpiresAt) {
			return true, w.store.MarkExecutionTerminalized(ctx, *item, now, "operation_state_expired")
		}
		releaseErr := w.store.ReleaseExecutionTerminalClaim(ctx, *item, now, err.Error())
		return true, errors.Join(err, releaseErr)
	}
	return true, w.store.MarkExecutionTerminalized(ctx, *item, w.now().UTC(), item.TerminalKind)
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
