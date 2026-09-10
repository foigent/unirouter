package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/mediajob"
)

// Media uses its original job/charge transaction, never a synthetic text
// reservation or I-01 record. A records admission and first admitted dispatch;
// attempt-by-attempt process observations belong to the separately planned B.
func (s *Service) MaterializeMediaStageTx(ctx context.Context, tx pgx.Tx, job mediajob.Job, stage string, snapshot *executioncontext.Snapshot) error {
	if !s.Enabled() {
		return nil
	}
	if stage != "admission" && stage != "dispatch" && stage != "terminal" {
		return errors.New("invalid media source stage")
	}
	firstPublication := true
	instant := executioncontext.Time(job.CreatedAt)
	if stage == "terminal" {
		if !job.Terminal() || job.TerminalAt == nil {
			return errors.New("media source terminal required")
		}
		instant = executioncontext.Time(*job.TerminalAt)
		var wire []byte
		if e := tx.QueryRow(ctx, `SELECT source_context FROM async_media_jobs WHERE job_id=$1 AND org_id=$2 AND project_id=$3`, job.JobID, job.Scope.OrgID, job.Scope.ProjectID).Scan(&wire); e != nil {
			return e
		}
		if len(wire) > 0 {
			snapshot = &executioncontext.Snapshot{}
			if e := json.Unmarshal(wire, snapshot); e != nil {
				return e
			}
		}
	} else {
		if snapshot == nil {
			return errors.New("media source snapshot required")
		}
		instant = snapshot.Core.CapturedAt
		if e := snapshot.Validate(); e != nil {
			return e
		}
		wire, e := json.Marshal(snapshot)
		if e != nil {
			return e
		}
		query := `UPDATE async_media_jobs SET source_context=$2 WHERE job_id=$1 AND source_context IS NULL`
		if stage == "dispatch" {
			query = `UPDATE async_media_jobs SET source_dispatch_context=$2 WHERE job_id=$1 AND source_dispatch_context IS NULL`
		}
		tag, e := tx.Exec(ctx, query, job.JobID, wire)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			firstPublication = false
			if stage != "dispatch" || !s.ObservationsEnabled() {
				return nil
			}
		}
	}
	if snapshot != nil {
		if e := snapshot.Validate(); e != nil {
			return e
		}
		if snapshot.Core.RequestID != job.RequestID || snapshot.Core.OrgID != executioncontext.ID(job.Scope.OrgID) || snapshot.Core.ProjectID != executioncontext.ID(job.Scope.ProjectID) {
			return errors.New("media source scope mismatch")
		}
	}
	docs := s.contexts(job.Scope.OrgID, job.Scope.ProjectID, job.RequestID, stage, instant, instant, snapshot)
	if stage == "terminal" {
		cost := s.document(job.Scope.OrgID, job.Scope.ProjectID, job.RequestID, CostProfile, stage, instant, instant)
		cost.Cost = &CostDocument{Model: job.RoutingModel, TerminalStatus: job.Lifecycle.Outcome, BasisStatus: "not_applicable", Reference: EconomicReference{Source: "async_media_charge_fact", SourceInstance: s.options.SourceInstance, OrgID: cost.OrgID, ProjectID: cost.ProjectID, RequestID: job.RequestID, Revision: "1", PublicationStatus: "not_applicable"}}
		if job.ChargeFact != nil {
			cost.Cost.Reference.ChargeFactRef = job.ChargeFact.FactID
			cost.Cost.MediaAmountState = job.ChargeFact.AmountState
			cost.Cost.BasisStatus = "not_captured"
		} else {
			cost.Cost.MediaAmountState = "not_eligible"
		}
		docs = append(docs, cost)
	}
	if !firstPublication {
		docs = nil
	}
	at, e := time.Parse("2006-01-02T15:04:05.000000Z", instant)
	if e != nil {
		return e
	}
	observations, e := s.mediaObservationsTx(ctx, tx, job, stage, at)
	if e != nil {
		return e
	}
	return s.publishTogetherTx(ctx, tx, docs, observations)
}
