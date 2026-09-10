package sourcefacts

import (
	"context"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/executioncontext"
)

// CaptureAdmissionRejection runs before the first client-error response header.
// Only an authenticated request without any original admission is a rejection.
// Errors after a reservation or media admission keep their original result path.
func (s *Service) CaptureAdmissionRejection(ctx context.Context, snapshot executioncontext.Snapshot, status int) error {
	if !s.ObservationsEnabled() || status < 400 || status >= 500 {
		return nil
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	org, _ := strconv.ParseInt(snapshot.Core.OrgID, 10, 64)
	project, _ := strconv.ParseInt(snapshot.Core.ProjectID, 10, 64)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ; SET LOCAL lock_timeout='100ms'; SET LOCAL statement_timeout='200ms'`); err != nil {
		return err
	}
	var admitted bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2)
		OR EXISTS(SELECT 1 FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2)
		OR EXISTS(SELECT 1 FROM async_media_jobs WHERE org_id=$1 AND project_id=$3 AND request_id=$2)`, org, snapshot.Core.RequestID, project).Scan(&admitted); err != nil {
		return err
	}
	if admitted {
		return nil
	}
	d := s.observation(org, project, snapshot.Core.RequestID, RuntimeProfile, "request", snapshot.Core.RequestID, "admission_rejected", time.Now())
	d.ContextRef = (Document{SourceInstance: s.options.SourceInstance, OrgID: snapshot.Core.OrgID, ProjectID: snapshot.Core.ProjectID, RequestID: snapshot.Core.RequestID, Profile: ContextProfile, Stage: "authenticated"}).factID()
	d.Runtime = &RuntimeObservation{Operation: snapshot.Core.Operation, StreamMode: "not_observed", Admission: "rejected", Provider: "not_started", LocalDelivery: "not_started", Financial: "not_created", Capture: "observed"}
	if err = s.publishTogetherTx(ctx, tx, nil, []Observation{d}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
