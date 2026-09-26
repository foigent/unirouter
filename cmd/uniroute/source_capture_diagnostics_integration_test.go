//go:build integration

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func sourceCaptureDiagnosticLine(phase, operation string, elapsed time.Duration, err error) string {
	switch phase {
	case "authenticated", "admission_rejection", "measurements", "policies":
	default:
		phase = "unknown"
	}
	// Only the server-generated operation shape can enter the diagnostic line.
	validOperation := len(operation) == 35 && strings.HasPrefix(operation, "op_")
	if validOperation {
		for _, c := range operation[3:] {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				validOperation = false
				break
			}
		}
	}
	if !validOperation {
		operation = "redacted"
	}
	class, state := "other", "none"
	var pgerr *pgconn.PgError
	switch {
	case errors.As(err, &pgerr):
		class = "postgres"
		state = "other"
		switch pgerr.Code {
		case "40001", "40P01", "55P03", "57014", "25P02", "23502", "23503", "23505", "23514", "08000", "08003", "08006", "53300", "53400", "53200", "XX000", "42P01", "42703":
			state = pgerr.Code
		}
	case errors.Is(err, context.DeadlineExceeded):
		class = "deadline"
	case errors.Is(err, context.Canceled):
		class = "canceled"
	}
	return fmt.Sprintf("source_capture phase=%s stage=%s operation=%s elapsed=%s class=%s sqlstate=%s", phase, sourcefacts.CaptureErrorStage(err), operation, elapsed, class, state)
}
func (s sourceCaptureDiagnostics) record(phase, operation string, start time.Time, err error) error {
	if err != nil {
		s.t.Log(sourceCaptureDiagnosticLine(phase, operation, time.Since(start), err))
	}
	return err
}

func (s sourceCaptureDiagnostics) CaptureAuthenticated(ctx context.Context, snapshot executioncontext.Snapshot) error {
	start := time.Now()
	return s.record("authenticated", snapshot.Core.RequestID, start, s.Service.CaptureAuthenticated(ctx, snapshot))
}

func (s sourceCaptureDiagnostics) CaptureAdmissionRejection(ctx context.Context, snapshot executioncontext.Snapshot, status int) error {
	start := time.Now()
	return s.record("admission_rejection", snapshot.Core.RequestID, start, s.Service.CaptureAdmissionRejection(ctx, snapshot, status))
}

func (s sourceCaptureDiagnostics) CaptureMeasurements(ctx context.Context, snapshot executioncontext.Snapshot, samples []executionmeasurement.Sample) error {
	start := time.Now()
	return s.record("measurements", snapshot.Core.RequestID, start, s.Service.CaptureMeasurements(ctx, snapshot, samples))
}

func (s sourceCaptureDiagnostics) CapturePolicies(ctx context.Context, snapshot executioncontext.Snapshot, observations []guardrail.SourcePolicyObservation, overflow bool) error {
	start := time.Now()
	return s.record("policies", snapshot.Core.RequestID, start, s.Service.CapturePolicies(ctx, snapshot, observations, overflow))
}

func TestSourceCaptureDiagnosticWhitelistOffline(t *testing.T) {
	poison := "secret-content\ncredential=value"
	err := fmt.Errorf("%s: %w", poison, &pgconn.PgError{Code: "40001", Message: poison, Detail: poison})
	operation := "op_" + strings.Repeat("a", 32)
	line := sourceCaptureDiagnosticLine("measurements", operation, 250*time.Millisecond, err)
	if strings.Contains(line, "secret") || !strings.Contains(line, "sqlstate=40001") || !strings.Contains(line, "operation="+operation) {
		t.Fatal("unsafe or incomplete diagnostic", line)
	}
	line = sourceCaptureDiagnosticLine(poison, poison, time.Millisecond, &pgconn.PgError{Code: poison, Message: poison})
	if strings.Contains(line, "secret") || !strings.Contains(line, "phase=unknown") || !strings.Contains(line, "operation=redacted") || !strings.Contains(line, "sqlstate=other") {
		t.Fatal("untrusted fields escaped whitelist", line)
	}
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled} {
		wrapped := fmt.Errorf("%s: %w", poison, cause)
		line = sourceCaptureDiagnosticLine("admission_rejection", operation, time.Millisecond, wrapped)
		if strings.Contains(line, "secret") || strings.Contains(line, "class=other") {
			t.Fatal("error chain lost", line)
		}
	}
}
