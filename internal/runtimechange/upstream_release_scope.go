package runtimechange

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
)

// This is a prospective authoring/expansion guard, deliberately not a runtime
// qualification view or a replacement for the existing execution safety gates.
func requireAuthoringUpstreamScope(ctx context.Context, tx pgx.Tx, command DeploymentReleaseAuthoringCommand) error {
	hasRequiredPins := false
	if command.Mutation.ProfileDefinitionDigest != "" {
		definition, err := modelprofile.ReadDefinition(ctx, tx, command.Mutation.ProfileDefinitionDigest)
		if err != nil {
			return err
		}
		hasRequiredPins = len(requiredReleaseCapabilityPins(definition.Profile())) > 0
	}
	// Approval rows and Probe summaries are immutable. Read the requested set
	// once; native release validation still owns exact capability eligibility.
	approvals := map[string][]*upstreamrelease.Observation{}
	if len(command.Mutation.CapabilityApprovalIDs) > 0 {
		rows, err := tx.Query(ctx, `SELECT a.deployment_id,a.operation_id,p.request_summary->'upstream_release_observation'
   FROM model_capability_routing_approvals a JOIN model_capability_probes p ON p.capability_probe_id=a.capability_probe_id
   WHERE a.customer_org_id=$1 AND a.approval_id=ANY($2::bigint[]) ORDER BY a.approval_id`, command.CustomerOrgID, command.Mutation.CapabilityApprovalIDs)
		if err != nil {
			return err
		}
		for rows.Next() {
			var deploymentID int64
			var operationID string
			var raw json.RawMessage
			if err := rows.Scan(&deploymentID, &operationID, &raw); err != nil {
				rows.Close()
				return err
			}
			observation, err := upstreamrelease.Decode(raw)
			if err != nil {
				rows.Close()
				return err
			}
			key := fmt.Sprintf("%d/%s", deploymentID, operationID)
			approvals[key] = append(approvals[key], observation)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	for _, selection := range command.Mutation.Selections {
		current, actualScope, err := upstreamrelease.Read(ctx, tx, command.CustomerOrgID, selection.DeploymentID, true)
		if err != nil {
			return err
		}
		if current.Observation.Validate(actualScope) != nil || upstreamrelease.Identity(current.Observation) != selection.ExpectedUpstreamObservationIdentity {
			return fmt.Errorf("%w: upstream declaration confirmation changed", ErrConflict)
		}
		if current.Observation == nil {
			continue
		}
		var captured *upstreamrelease.Observation
		if modelqualification.IsQualificationRequired(selection.OperationID) {
			var probeID int64
			if err := tx.QueryRow(ctx, `SELECT probe_id FROM model_operation_execution_qualifications
 WHERE deployment_id=$1 AND operation_id=$2 AND qualified`, selection.DeploymentID, selection.OperationID).Scan(&probeID); err != nil {
				return fmt.Errorf("%w: upstream operation approval unavailable", ErrConflict)
			}
			captured, err = upstreamrelease.Probe(ctx, tx, false, command.CustomerOrgID, selection.DeploymentID, probeID)
			if err != nil || upstreamrelease.Require(current.Observation, captured, selection.ExpectedUpstreamObservationIdentity) != nil {
				return fmt.Errorf("%w: upstream operation approval scope", ErrConflict)
			}
		}
		for _, captured := range approvals[fmt.Sprintf("%d/%s", selection.DeploymentID, selection.OperationID)] {
			if upstreamrelease.Require(current.Observation, captured, selection.ExpectedUpstreamObservationIdentity) != nil {
				return fmt.Errorf("%w: upstream capability decision scope", ErrConflict)
			}
		}
		if !hasRequiredPins && !modelqualification.IsQualificationRequired(selection.OperationID) {
			var probeID int64
			// The native route validator still proves current connection/operation
			// identity. This additional read covers only its declared upstream epoch.
			if err := tx.QueryRow(ctx, `SELECT probe_id FROM model_deployment_probes WHERE deployment_id=$1 AND operation_id=$2 AND success ORDER BY tested_at DESC,probe_id DESC LIMIT 1`, selection.DeploymentID, selection.OperationID).Scan(&probeID); err != nil {
				return fmt.Errorf("%w: upstream operation probe unavailable", ErrConflict)
			}
			captured, err = upstreamrelease.Probe(ctx, tx, false, command.CustomerOrgID, selection.DeploymentID, probeID)
			if err != nil || upstreamrelease.Require(current.Observation, captured, selection.ExpectedUpstreamObservationIdentity) != nil {
				return fmt.Errorf("%w: upstream operation probe scope", ErrConflict)
			}
		}
	}
	return nil
}

// The immutable authoring receipt is the reviewed scope. Never substitute a
// later, unrelated successful Probe for the scope confirmed when authoring.
func requireExpansionUpstreamScope(ctx context.Context, tx pgx.Tx, orgID int64, release DeploymentRelease) error {
	var raw json.RawMessage
	if err := tx.QueryRow(ctx, `SELECT COALESCE(a.requested_selections,'[]'::jsonb)
 FROM runtime_change_releases r LEFT JOIN model_deployment_release_authoring_receipts a ON a.receipt_id=r.authoring_receipt_id
 WHERE r.id=$1`, release.ID).Scan(&raw); err != nil {
		return err
	}
	var selections []DeploymentReleaseSelection
	if json.Unmarshal(raw, &selections) != nil {
		return ErrConflict
	}
	confirmed := map[string]string{}
	for _, selection := range selections {
		confirmed[fmt.Sprintf("%d/%s", selection.DeploymentID, selection.OperationID)] = selection.ExpectedUpstreamObservationIdentity
	}
	for _, route := range release.Candidate.DeploymentRoutes {
		current, scope, err := upstreamrelease.Read(ctx, tx, orgID, route.DeploymentID, true)
		if err != nil {
			return err
		}
		if current.Observation.Validate(scope) != nil || upstreamrelease.Identity(current.Observation) != confirmed[fmt.Sprintf("%d/%s", route.DeploymentID, route.ModelOperationID)] {
			return fmt.Errorf("%w: retained authoring upstream scope differs", ErrConflict)
		}
	}
	return nil
}
