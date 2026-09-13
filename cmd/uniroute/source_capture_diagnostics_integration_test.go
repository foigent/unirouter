//go:build integration

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/executionmeasurement"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

// This test-only adapter logs failed capture phases after the original operation
// returns. It does not change contexts, transactions, errors or response policy.
type sourceCaptureDiagnostics struct {
	*sourcefacts.Service
	t *testing.T
}

func (s sourceCaptureDiagnostics) record(phase string, start time.Time, err error) error {
	if err == nil {
		return nil
	}
	class, state := "other", "none"
	var pgerr *pgconn.PgError
	switch {
	case errors.As(err, &pgerr):
		class, state = "postgres", pgerr.Code
	case errors.Is(err, context.DeadlineExceeded):
		class = "deadline"
	case errors.Is(err, context.Canceled):
		class = "canceled"
	}
	s.t.Logf("source_capture phase=%s elapsed=%s class=%s sqlstate=%s", phase, time.Since(start), class, state)
	return err
}

func (s sourceCaptureDiagnostics) CaptureAuthenticated(ctx context.Context, snapshot executioncontext.Snapshot) error {
	start := time.Now()
	return s.record("authenticated", start, s.Service.CaptureAuthenticated(ctx, snapshot))
}

func (s sourceCaptureDiagnostics) CaptureAdmissionRejection(ctx context.Context, snapshot executioncontext.Snapshot, status int) error {
	start := time.Now()
	return s.record("admission_rejection", start, s.Service.CaptureAdmissionRejection(ctx, snapshot, status))
}

func (s sourceCaptureDiagnostics) CaptureMeasurements(ctx context.Context, snapshot executioncontext.Snapshot, samples []executionmeasurement.Sample) error {
	start := time.Now()
	return s.record("measurements", start, s.Service.CaptureMeasurements(ctx, snapshot, samples))
}

func (s sourceCaptureDiagnostics) CapturePolicies(ctx context.Context, snapshot executioncontext.Snapshot, observations []guardrail.SourcePolicyObservation, overflow bool) error {
	start := time.Now()
	return s.record("policies", start, s.Service.CapturePolicies(ctx, snapshot, observations, overflow))
}
