package sourcefacts

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/guardrail"
)

func (s *Service) CapturePolicies(ctx context.Context, snapshot executioncontext.Snapshot, values []guardrail.SourcePolicyObservation, overflow bool) error {
	if !s.ObservationsEnabled() || len(values) == 0 && !overflow {
		return nil
	}
	if overflow || len(values) > guardrail.SourcePolicyLimit {
		return errors.New("source policy observation bound exceeded")
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
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ;SET LOCAL lock_timeout='100ms';SET LOCAL statement_timeout='200ms'`); err != nil {
		return err
	}
	contextRef := (Document{SourceInstance: s.options.SourceInstance, OrgID: snapshot.Core.OrgID, ProjectID: snapshot.Core.ProjectID, RequestID: snapshot.Core.RequestID, Profile: ContextProfile, Stage: "authenticated"}).factID()
	docs := make([]Observation, 0, len(values)+1)
	for _, v := range values {
		stage := map[string]string{guardrail.StageInput: "input", guardrail.StageNonStreamOutput: "output", guardrail.StageStreamOutput: "stream"}[v.Stage]
		if stage == "" || v.FirstObservedAt.IsZero() || v.LastObservedAt.Before(v.FirstObservedAt) {
			return errors.New("invalid observed policy stage or time")
		}
		subject := sourceID("unirouter.observed-policy.v1", v.PolicyRef, v.PolicyVersion, v.DetectorRef, v.DetectorVersion, v.Mode)
		d := s.observation(org, project, snapshot.Core.RequestID, PolicyProfile, "policy", subject, stage+"_result", v.LastObservedAt)
		d.RecordedAt = executioncontext.Time(time.Now())
		d.ContextRef = contextRef
		detectorVersion := v.DetectorVersion
		if detectorVersion == "" {
			detectorVersion = "not_available"
		}
		action := map[string]string{guardrail.ActionAudit: "audit", guardrail.ActionBlock: "block", guardrail.ActionRedact: "replace"}[v.Action]
		if action == "" {
			action = "unknown"
		}
		failure := map[string]string{guardrail.FailureFailClosed: "closed", guardrail.FailureDegradedAllow: "open"}[v.FailureMode]
		if failure == "" {
			failure = "unknown"
		}
		p := &PolicyObservation{PolicyRef: v.PolicyRef, PolicyVersion: v.PolicyVersion, DetectorRef: v.DetectorRef, DetectorVersion: detectorVersion, Stage: stage, Mode: v.Mode, ConfiguredAction: action, Outcome: "unknown", FailureMode: failure, Degraded: v.Degraded, Mandatory: v.Mandatory, Effects: slices.Clone(v.Effects), DetectorOutcomes: slices.Clone(v.Outcomes), StageDecisions: slices.Clone(v.Decisions), EvidenceState: "observed"}
		switch {
		case slices.Contains(v.Effects, "input_blocked") || slices.Contains(v.Effects, "output_blocked") || slices.Contains(v.Effects, "stream_terminated"):
			p.Outcome = "block"
		case slices.Contains(v.Outcomes, guardrail.OutcomeUnavailable) || slices.Contains(v.Outcomes, guardrail.OutcomeIndeterminate):
			p.Outcome = "error"
		case slices.Contains(v.Outcomes, guardrail.OutcomeTransform):
			p.Outcome = "replace"
		case slices.Contains(v.Outcomes, guardrail.OutcomeMatch):
			p.Outcome = "match"
		case slices.Contains(v.Outcomes, guardrail.OutcomeSkipped):
			p.Outcome = "not_covered"
		case slices.Contains(v.Outcomes, guardrail.OutcomePass):
			p.Outcome = "allow"
		}
		if stage == "stream" {
			// Original advisory work can finish outside this request. This finite
			// summary does not claim that all frames or asynchronous checks completed.
			p.EvidenceState = "partial"
			d.Coverage = "partial"
			if slices.Contains(p.Effects, "stream_terminated") {
				var disclosed bool
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2 AND mode='stream' AND delivered_bytes>0)`, org, snapshot.Core.RequestID).Scan(&disclosed); err != nil {
					return err
				}
				if disclosed {
					p.Effects = append(p.Effects, "partial_output")
					slices.Sort(p.Effects)
				}
			}
		}
		d.Policy = p
		docs = append(docs, d)
	}
	if err = s.publishTogetherTx(ctx, tx, nil, docs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
