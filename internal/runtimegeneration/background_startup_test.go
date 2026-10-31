package runtimegeneration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func awaitBackground(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background completion did not close")
	}
}

func TestBackgroundStartupHoldsWholeGroupAndStartsOnce(t *testing.T) {
	s := NewBackgroundStartup()
	defer s.Stop()
	var baseline, calls atomic.Int32
	var workers []<-chan struct{}
	for range 8 {
		workers = append(workers, s.Go(t.Context(), func(context.Context) {
			require.Equal(t, int32(1), baseline.Load())
			calls.Add(1)
		}))
	}
	s.Start(func(context.Context) error { baseline.Add(1); return nil })
	require.Zero(t, calls.Load())
	require.Zero(t, baseline.Load())
	for range 10 {
		s.Admit()
	}
	awaitBackground(t, s.Done())
	for _, done := range workers {
		awaitBackground(t, done)
	}
	require.NoError(t, s.Err())
	require.Equal(t, int32(8), calls.Load())
	require.Equal(t, int32(1), baseline.Load())
}

func TestBackgroundStartupCancelAndBaselineFailureLaunchNone(t *testing.T) {
	for _, scenario := range []string{"held_cancel", "baseline_failure", "baseline_cancel"} {
		t.Run(scenario, func(t *testing.T) {
			s := NewBackgroundStartup()
			defer s.Stop()
			var calls atomic.Int32
			var workers []<-chan struct{}
			for range 8 {
				workers = append(workers, s.Go(t.Context(), func(context.Context) { calls.Add(1) }))
			}
			entered := make(chan struct{})
			s.Start(func(ctx context.Context) error {
				close(entered)
				if scenario == "baseline_cancel" {
					<-ctx.Done()
					return nil // A late success must not override cancellation.
				}
				return errors.New("synthetic baseline failure")
			})
			if scenario != "held_cancel" {
				s.Admit()
				awaitBackground(t, entered)
			}
			if scenario != "baseline_failure" {
				s.Stop()
			}
			awaitBackground(t, s.Done())
			require.Error(t, s.Err())
			s.Admit()
			for _, done := range workers {
				awaitBackground(t, done)
			}
			require.Zero(t, calls.Load())
		})
	}
}

func TestBackgroundStartupReleaseCancelRaceAndTerminalDrain(t *testing.T) {
	for range 100 {
		s := NewBackgroundStartup()
		var calls atomic.Int32
		done := s.Go(t.Context(), func(context.Context) { calls.Add(1) })
		s.Start(nil)
		var race sync.WaitGroup
		race.Add(2)
		go func() { defer race.Done(); s.Admit() }()
		go func() { defer race.Done(); s.Stop() }()
		race.Wait()
		awaitBackground(t, s.Done())
		awaitBackground(t, done)
		require.LessOrEqual(t, calls.Load(), int32(1))
		if errors.Is(s.Err(), context.Canceled) {
			require.Zero(t, calls.Load())
		}
	}
	// Stop fences future startup, but leaves an accepted worker's terminal
	// persistence alive until its existing owner has finished draining it.
	s := NewBackgroundStartup()
	ctx, cancel := context.WithCancel(t.Context())
	entered, terminal := make(chan struct{}), make(chan struct{})
	done := s.Go(ctx, func(ctx context.Context) { close(entered); <-ctx.Done(); close(terminal) })
	s.Admit()
	s.Start(nil)
	awaitBackground(t, entered)
	s.Stop()
	select {
	case <-terminal:
		t.Fatal("startup stop canceled an accepted worker")
	default:
	}
	cancel()
	awaitBackground(t, done)
	awaitBackground(t, terminal)
}
