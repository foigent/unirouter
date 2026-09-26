package contentcapture

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func testEngine(t *testing.T, mutate func(*EngineOptions)) (*Engine, string, StoreBinding) {
	t.Helper()
	files, root, binding := testFileStore(t)
	options := EngineOptions{Queue: QueueLimits{MaxItems: 128, MaxBytes: 4096, MaxProjectBytes: 4096, MaxChunkBytes: 32}, MaxActive: 8, MaxProjectActive: 4, PrepareTimeout: time.Second, FinalizeTimeout: time.Second, MaxLifetime: time.Minute}
	if mutate != nil {
		mutate(&options)
	}
	e, err := NewEngine(files, options)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		require.NoError(t, e.Close(ctx))
	})
	return e, root, binding
}

func testCaptureJob(t *testing.T, binding StoreBinding, project int64, limit int64, finish func(context.Context, CaptureOutcome) error) CaptureJob {
	t.Helper()
	name, err := randomStoreName()
	require.NoError(t, err)
	if finish == nil {
		finish = func(context.Context, CaptureOutcome) error { return nil }
	}
	return CaptureJob{Reservation: Reservation{ProjectID: project, Binding: binding, ObjectName: name, ReservedBytes: limit}, Limits: StoreLimits{MaxSavedBytes: limit, MaxReadBytes: limit, MaxBufferBytes: 7}, Finalize: finish}
}

func startTestCapture(t *testing.T, e *Engine, job CaptureJob) *Capture {
	t.Helper()
	c, err := e.Start(t.Context(), job.Reservation.ProjectID, func(context.Context) (CaptureJob, error) { return job, nil })
	require.NoError(t, err)
	return c
}

func waitCapture(t *testing.T, c *Capture) CaptureResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	r, err := c.Wait(ctx)
	require.NoError(t, err)
	return r
}

func capturedBytes(t *testing.T, e *Engine, c *Capture, r CaptureResult) []byte {
	t.Helper()
	require.NoError(t, r.Outcome.PrepareError)
	require.NoError(t, r.FinalizeError)
	require.NoError(t, r.Outcome.StorageError)
	require.NotNil(t, r.Outcome.Receipt)
	f, err := e.files.OpenVerified(t.Context(), *r.Outcome.Receipt, c.job.Limits)
	require.NoError(t, err)
	body, err := io.ReadAll(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return body
}

func TestCaptureEngineInterleavedAndZero(t *testing.T) {
	e, _, binding := testEngine(t, nil)
	a := startTestCapture(t, e, testCaptureJob(t, binding, 1, 128, nil))
	b := startTestCapture(t, e, testCaptureJob(t, binding, 2, 128, nil))
	require.NoError(t, a.Offer(t.Context(), []byte("prefix-")))
	require.NoError(t, b.Offer(t.Context(), []byte{0, 255, 42}))
	require.NoError(t, b.Finish("none"))
	br := waitCapture(t, b)
	require.Equal(t, []byte{0, 255, 42}, capturedBytes(t, e, b, br))
	select {
	case <-a.done:
		t.Fatal("unfinished A was finalized")
	default:
	}
	require.NoError(t, a.Offer(t.Context(), []byte("suffix")))
	require.NoError(t, a.Finish("none"))
	ar := waitCapture(t, a)
	require.Equal(t, []byte("prefix-suffix"), capturedBytes(t, e, a, ar))
	zero := startTestCapture(t, e, testCaptureJob(t, binding, 1, 128, nil))
	require.NoError(t, zero.Finish("none"))
	zr := waitCapture(t, zero)
	require.Empty(t, capturedBytes(t, e, zero, zr))
	require.Equal(t, "none", zr.Outcome.Reason)
}

func TestCaptureEngineLimitsAndCancellation(t *testing.T) {
	for _, kind := range []string{"exact", "over", "canceled_prefix", "canceled_zero"} {
		t.Run(kind, func(t *testing.T) {
			e, _, binding := testEngine(t, nil)
			c := startTestCapture(t, e, testCaptureJob(t, binding, 1, 10, nil))
			switch kind {
			case "exact":
				require.NoError(t, c.Offer(t.Context(), []byte("1234567890")))
				require.NoError(t, c.Finish("none"))
			case "over":
				require.ErrorIs(t, c.Offer(t.Context(), []byte("1234567890overflow")), ErrStoreLimit)
			default:
				if kind == "canceled_prefix" {
					require.NoError(t, c.Offer(t.Context(), []byte("1234567890")))
				}
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				require.ErrorIs(t, c.Offer(ctx, []byte("extra")), context.Canceled)
			}
			r := waitCapture(t, c)
			if kind == "canceled_zero" {
				require.Nil(t, r.Outcome.Receipt)
				require.Equal(t, "request_cancelled", r.Outcome.Reason)
			} else {
				require.Equal(t, []byte("1234567890"), capturedBytes(t, e, c, r))
			}
			if kind == "over" {
				require.Equal(t, "limit_exceeded", r.Outcome.Reason)
			}
			require.ErrorIs(t, c.Offer(t.Context(), nil), ErrCaptureClosed)
		})
	}
}

func TestCaptureEngineFullQueueAndGracefulClose(t *testing.T) {
	e, _, binding := testEngine(t, func(o *EngineOptions) { o.Queue.MaxItems = 2 })
	entered, release := make(chan struct{}), make(chan struct{})
	block := startTestCapture(t, e, testCaptureJob(t, binding, 1, 128, func(context.Context, CaptureOutcome) error { close(entered); <-release; return nil }))
	require.NoError(t, block.Finish("none"))
	<-entered
	c := startTestCapture(t, e, testCaptureJob(t, binding, 2, 128, nil))
	require.NoError(t, c.Offer(t.Context(), []byte("12345678901234")))
	require.ErrorIs(t, c.Offer(t.Context(), []byte("x")), ErrQueueFull)
	_, _, usage := e.Usage(2)
	require.Equal(t, 2, usage.Items)
	require.EqualValues(t, 14, usage.Bytes)
	closed := make(chan error, 1)
	go func() { closed <- e.Close(t.Context()) }()
	close(release)
	require.NoError(t, <-closed)
	r := waitCapture(t, c)
	require.Equal(t, "queue_full", r.Outcome.Reason)
	require.Equal(t, []byte("12345678901234"), capturedBytes(t, e, c, r))
	active, project, usage := e.Usage(2)
	require.Zero(t, active)
	require.Zero(t, project)
	require.Zero(t, usage.Items)
	require.Zero(t, usage.Bytes)
}

func TestCaptureEnginePreparingCapacityAndLateClose(t *testing.T) {
	e, _, binding := testEngine(t, func(o *EngineOptions) { o.MaxActive = 2; o.MaxProjectActive = 1 })
	entered, release := make(chan struct{}), make(chan struct{})
	var finalized atomic.Int32
	job := testCaptureJob(t, binding, 1, 128, func(ctx context.Context, o CaptureOutcome) error {
		finalized.Add(1)
		if o.Receipt != nil {
			return errors.New("late allocation became readable")
		}
		return ctx.Err()
	})
	type started struct {
		c   *Capture
		err error
	}
	done := make(chan started, 1)
	go func() {
		c, err := e.Start(t.Context(), 1, func(context.Context) (CaptureJob, error) { close(entered); <-release; return job, nil })
		done <- started{c, err}
	}()
	<-entered
	_, err := e.Start(t.Context(), 1, func(context.Context) (CaptureJob, error) { t.Error("over-quota prepare ran"); return job, nil })
	require.ErrorIs(t, err, ErrCaptureCapacity)
	other := startTestCapture(t, e, testCaptureJob(t, binding, 2, 128, nil))
	_, err = e.Start(t.Context(), 3, func(context.Context) (CaptureJob, error) { t.Error("node-full prepare ran"); return job, nil })
	require.ErrorIs(t, err, ErrCaptureCapacity)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, e.Close(ctx), context.Canceled)
	active, project, _ := e.Usage(1)
	require.GreaterOrEqual(t, active, 1)
	require.Equal(t, 1, project)
	close(release)
	late := <-done
	require.Error(t, late.err)
	r := waitCapture(t, late.c)
	require.Nil(t, r.Outcome.Receipt)
	require.Equal(t, "stream_interrupted", r.Outcome.Reason)
	require.EqualValues(t, 1, finalized.Load())
	_ = waitCapture(t, other)
	require.NoError(t, e.Close(t.Context()))
}

func TestCaptureEngineFinalizingRemainsChargedAfterCloseDeadline(t *testing.T) {
	e, _, binding := testEngine(t, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	sentinel := errors.New("synthetic ambiguous publication")
	var calls atomic.Int32
	c := startTestCapture(t, e, testCaptureJob(t, binding, 1, 128, func(context.Context, CaptureOutcome) error { calls.Add(1); close(entered); <-release; return sentinel }))
	require.NoError(t, c.Finish("none"))
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, e.Close(ctx), context.Canceled)
	active, project, _ := e.Usage(1)
	require.Equal(t, 1, active)
	require.Equal(t, 1, project)
	close(release)
	r := waitCapture(t, c)
	require.ErrorIs(t, r.FinalizeError, sentinel)
	require.EqualValues(t, 1, calls.Load())
	require.NotNil(t, r.Outcome.Receipt, "ambiguous publication retains precise durable file receipt for recovery")
}

func TestCaptureEngineFileFailureDrainsWithoutReceipt(t *testing.T) {
	e, root, binding := testEngine(t, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	block := startTestCapture(t, e, testCaptureJob(t, binding, 1, 128, func(context.Context, CaptureOutcome) error { close(entered); <-release; return nil }))
	require.NoError(t, block.Finish("none"))
	<-entered
	var calls atomic.Int32
	job := testCaptureJob(t, binding, 2, 128, func(context.Context, CaptureOutcome) error { calls.Add(1); return nil })
	require.NoError(t, os.WriteFile(filepath.Join(root, job.Reservation.ObjectName), []byte("existing"), 0600))
	c := startTestCapture(t, e, job)
	require.NoError(t, c.Offer(t.Context(), bytes.Repeat([]byte{255}, 100)))
	require.NoError(t, c.Finish("none"))
	close(release)
	r := waitCapture(t, c)
	require.Nil(t, r.Outcome.Receipt)
	require.Error(t, r.Outcome.StorageError)
	require.Equal(t, "storage_write_failed", r.Outcome.Reason)
	require.EqualValues(t, 1, calls.Load())
	_, _, usage := e.Usage(2)
	require.Zero(t, usage.Items)
	require.Zero(t, usage.Bytes)
	got, err := os.ReadFile(filepath.Join(root, job.Reservation.ObjectName))
	require.NoError(t, err)
	require.Equal(t, []byte("existing"), got)
}

func TestCaptureEngineLifetimeAndConcurrentTerminal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e, _, binding := testEngine(t, func(o *EngineOptions) { o.MaxLifetime = time.Second })
		c := startTestCapture(t, e, testCaptureJob(t, binding, 1, 128, nil))
		time.Sleep(time.Second)
		synctest.Wait()
		r := waitCapture(t, c)
		require.Equal(t, "stream_interrupted", r.Outcome.Reason)
		require.Nil(t, r.Outcome.Receipt)
	})
	e, _, binding := testEngine(t, nil)
	var finalized atomic.Int32
	c := startTestCapture(t, e, testCaptureJob(t, binding, 1, 4096, func(context.Context, CaptureOutcome) error { finalized.Add(1); return nil }))
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = c.Offer(t.Context(), []byte("abcdefg")); _ = c.Finish("none") }()
	}
	wg.Wait()
	r := waitCapture(t, c)
	require.NoError(t, r.FinalizeError)
	require.EqualValues(t, 1, finalized.Load())
	if r.Outcome.Receipt != nil {
		require.Zero(t, r.Outcome.Receipt.SavedBytes%7)
		require.Equal(t, bytes.Repeat([]byte("abcdefg"), int(r.Outcome.Receipt.SavedBytes/7)), capturedBytes(t, e, c, r))
	}
}

func TestCaptureEngineSeventeenMiBPlusOne(t *testing.T) {
	const size = 17*1024*1024 + 1
	e, _, binding := testEngine(t, func(o *EngineOptions) {
		o.Queue = QueueLimits{MaxItems: 1024, MaxBytes: 18 * 1024 * 1024, MaxProjectBytes: 18 * 1024 * 1024, MaxChunkBytes: 32 * 1024}
	})
	job := testCaptureJob(t, binding, 1, size, nil)
	job.Limits.MaxBufferBytes = 32 * 1024
	c := startTestCapture(t, e, job)
	chunk := bytes.Repeat([]byte{0, 255, 7, 128}, 8*1024)
	for left := size; left > 0; {
		n := min(left, len(chunk))
		require.NoError(t, c.Offer(t.Context(), chunk[:n]))
		left -= n
	}
	require.NoError(t, c.Finish("none"))
	r := waitCapture(t, c)
	require.NoError(t, r.FinalizeError)
	require.NoError(t, r.Outcome.StorageError)
	require.NotNil(t, r.Outcome.Receipt)
	require.EqualValues(t, size, r.Outcome.Receipt.SavedBytes)
	f, err := e.files.OpenVerified(t.Context(), *r.Outcome.Receipt, job.Limits)
	require.NoError(t, err)
	count, err := io.Copy(io.Discard, f)
	require.NoError(t, err)
	require.EqualValues(t, size, count)
	require.NoError(t, f.Close())
	_, _, usage := e.Usage(1)
	require.Zero(t, usage.Bytes)
	require.Zero(t, usage.Items)
}

func TestCaptureEnginePrepareTimeoutAndFinalizeContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e, _, binding := testEngine(t, nil)
		job := testCaptureJob(t, binding, 1, 128, func(_ context.Context, o CaptureOutcome) error { return o.PrepareError })
		c, err := e.Start(t.Context(), 1, func(ctx context.Context) (CaptureJob, error) { <-ctx.Done(); return job, nil })
		require.ErrorIs(t, err, context.DeadlineExceeded)
		r := waitCapture(t, c)
		require.ErrorIs(t, r.Outcome.PrepareError, context.DeadlineExceeded)
		require.ErrorIs(t, r.FinalizeError, context.DeadlineExceeded)
		require.Nil(t, r.Outcome.Receipt)
		require.Equal(t, "capture_unavailable", r.Outcome.Reason)
		job = testCaptureJob(t, binding, 1, 128, func(ctx context.Context, _ CaptureOutcome) error { <-ctx.Done(); return ctx.Err() })
		c = startTestCapture(t, e, job)
		require.NoError(t, c.Finish("none"))
		r = waitCapture(t, c)
		require.ErrorIs(t, r.FinalizeError, context.DeadlineExceeded)
		require.NotNil(t, r.Outcome.Receipt)
	})
}

func TestCaptureEngineRejectsWrongProjectAndDoesNotRetryPreparation(t *testing.T) {
	e, _, binding := testEngine(t, nil)
	var calls atomic.Int32
	job := testCaptureJob(t, binding, 2, 128, func(context.Context, CaptureOutcome) error { calls.Add(1); return nil })
	c, err := e.Start(t.Context(), 1, func(context.Context) (CaptureJob, error) { return job, nil })
	require.ErrorIs(t, err, ErrCaptureJob)
	r := waitCapture(t, c)
	require.ErrorIs(t, r.Outcome.PrepareError, ErrCaptureJob)
	require.Nil(t, r.Outcome.Receipt)
	require.EqualValues(t, 1, calls.Load())
	sentinel := errors.New("synthetic prepare commit unknown")
	calls.Store(0)
	c, err = e.Start(t.Context(), 1, func(context.Context) (CaptureJob, error) { calls.Add(1); return CaptureJob{}, sentinel })
	require.ErrorIs(t, err, sentinel)
	r = waitCapture(t, c)
	require.ErrorIs(t, r.Outcome.PrepareError, sentinel)
	require.EqualValues(t, 1, calls.Load())
}
