package modelexecution

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executionevidence"
)

// lockVerifyCanonicalProbeGroup locks every non-retired canonical projection
// of one legacy channel in stable id order. Verify authority treats that group
// as one breaker/lease projection: selecting a different public model cannot
// create a second half-open owner for the same legacy provider endpoint.
func lockVerifyCanonicalProbeGroup(
	ctx context.Context,
	tx pgx.Tx,
	evidence executionevidence.EvidenceV1,
	dbNow time.Time,
) ([]breakerSnapshot, Status, error) {
	rows, err := tx.Query(ctx, `
SELECT id,connection_id,public_model_id,legacy_channel_id,admin_state,config_revision,
       circuit_state,circuit_revision,fail_count,degradation_threshold,
       half_open_until,half_open_probe_until,COALESCE(half_open_probe_token,'')
  FROM model_deployments
 WHERE legacy_channel_id=$1 AND admin_state<>'retired'
 ORDER BY id
 FOR UPDATE`, evidence.LegacyChannelID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	values := make([]breakerSnapshot, 0, 1)
	selected := -1
	for rows.Next() {
		var value breakerSnapshot
		if err := rows.Scan(
			&value.id, &value.connectionID, &value.publicModelID, &value.legacyChannelID,
			&value.adminState, &value.configRevision, &value.circuitState, &value.circuitRevision,
			&value.failCount, &value.degradationThreshold, &value.halfOpenUntil,
			&value.probeUntil, &value.probeToken,
		); err != nil {
			return nil, "", err
		}
		if value.id == evidence.DeploymentID {
			selected = len(values)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if selected < 0 {
		status, classifyErr := classifyMissingDeployment(ctx, tx, evidence.DeploymentID)
		return nil, status, classifyErr
	}

	selectedValue := values[selected]
	for _, value := range values {
		if value.adminState != "enabled" {
			return nil, Disabled, nil
		}
		if !value.legacyChannelID.Valid || value.legacyChannelID.Int64 != evidence.LegacyChannelID ||
			value.connectionID != evidence.ConnectionID {
			return nil, Stale, nil
		}
		if value.id == evidence.DeploymentID {
			if value.publicModelID != evidence.PublicModelID ||
				value.configRevision != evidence.DeploymentRevision ||
				value.circuitRevision != evidence.CircuitRevision {
				return nil, Stale, nil
			}
		} else if value.configRevision != evidence.LegacyRoutingRevision ||
			value.circuitRevision != evidence.LegacyCircuitRevision ||
			!sameBreakerProjection(value, selectedValue) {
			return nil, Stale, nil
		}
		if status := breakerEligibility(
			value.circuitState, value.halfOpenUntil, value.probeUntil, value.probeToken, dbNow, true,
		); status != Applied {
			return nil, status, nil
		}
	}
	return values, Applied, nil
}

func claimCanonicalProbeGroup(
	ctx context.Context,
	tx pgx.Tx,
	values []breakerSnapshot,
	dbNow time.Time,
	target time.Time,
	probeToken string,
) (Status, error) {
	for _, value := range values {
		tag, err := tx.Exec(ctx, `
UPDATE model_deployments
   SET half_open_probe_token=$8,half_open_probe_until=$9,updated_at=NOW()
 WHERE id=$1 AND connection_id=$2 AND admin_state='enabled'
   AND config_revision=$3 AND circuit_revision=$4 AND circuit_state='half_open'
   AND half_open_until IS NOT DISTINCT FROM $5 AND half_open_until<=$6
   AND half_open_probe_until IS NOT DISTINCT FROM $7
   AND COALESCE(half_open_probe_token,'')=$10
   AND (half_open_probe_until IS NULL OR half_open_probe_until<=$6)`,
			value.id, value.connectionID, value.configRevision, value.circuitRevision,
			value.halfOpenUntil, dbNow, value.probeUntil, probeToken, target, value.probeToken,
		)
		if err != nil {
			return "", err
		}
		if tag.RowsAffected() != 1 {
			return LeaseLost, nil
		}
	}
	return Applied, nil
}

func claimLegacyProbe(
	ctx context.Context,
	tx pgx.Tx,
	evidence executionevidence.EvidenceV1,
	current breakerSnapshot,
	dbNow time.Time,
	target time.Time,
	probeToken string,
) (Status, error) {
	tag, err := tx.Exec(ctx, `
UPDATE channels
   SET half_open_probe_token=$9,half_open_probe_until=$10,updated_at=NOW()
 WHERE id=$1 AND status='active' AND routing_epoch=$2 AND circuit_epoch=$3
   AND circuit_state=$4 AND half_open_until IS NOT DISTINCT FROM $5
   AND half_open_until<=$6 AND half_open_probe_until IS NOT DISTINCT FROM $7
   AND COALESCE(half_open_probe_token,'')=$8
   AND (half_open_probe_until IS NULL OR half_open_probe_until<=$6)`,
		evidence.LegacyChannelID, evidence.LegacyRoutingRevision, evidence.LegacyCircuitRevision,
		current.circuitState, current.halfOpenUntil, dbNow, current.probeUntil,
		current.probeToken, probeToken, target,
	)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return LeaseLost, nil
	}
	return Applied, nil
}
