package modelexecution

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executionevidence"
)

type releasableCanonicalLease struct {
	breakerSnapshot
	expectedConfigRevision  int64
	expectedCircuitRevision int64
}

// releaseUndispatchedProjection is deliberately subordinate to the durable
// provider-attempt fence. Once exact owner/evidence proves that no dispatch
// occurred, stale or disabled route projections must not leave an unresolved
// financial attempt. A probe group is cleared only when every authority row
// still carries the exact live lease; any route drift makes the whole cleanup
// a successful no-op, preserving both admin priority and verify equivalence.
func releaseUndispatchedProjection(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
) (time.Time, error) {
	if !request.evidence.RequiresProbe {
		return readReleaseClock(ctx, tx)
	}

	canonical, canonicalReady, err := lockReleasableCanonicalLeases(ctx, tx, request)
	if err != nil {
		return time.Time{}, err
	}
	legacy, legacyReady, err := lockReleasableLegacyLease(ctx, tx, request)
	if err != nil {
		return time.Time{}, err
	}
	dbNow, err := readReleaseClock(ctx, tx)
	if err != nil {
		return time.Time{}, err
	}

	allLive := true
	if request.evidence.AuthorityMode != executionevidence.ModeLegacy {
		allLive = canonicalReady && len(canonical) > 0
		for _, current := range canonical {
			allLive = allLive && canonicalLeaseIsLive(current, request, dbNow)
		}
	}
	if request.evidence.AuthorityMode != executionevidence.ModeCanonical {
		allLive = allLive && legacyReady && legacyLeaseIsLive(legacy, request, dbNow)
	}
	if !allLive {
		return dbNow, nil
	}

	for _, current := range canonical {
		tag, execErr := tx.Exec(ctx, `
UPDATE model_deployments
   SET half_open_probe_until=NULL,half_open_probe_token=NULL,updated_at=$4
 WHERE id=$1 AND admin_state='enabled' AND config_revision=$2 AND circuit_revision=$3
   AND circuit_state='half_open' AND half_open_until<=$4
   AND half_open_probe_token=$5 AND half_open_probe_until IS NOT DISTINCT FROM $6
   AND half_open_probe_until>$4`,
			current.id, current.expectedConfigRevision, current.expectedCircuitRevision,
			dbNow, request.token, current.probeUntil,
		)
		if execErr != nil {
			return time.Time{}, execErr
		}
		if tag.RowsAffected() != 1 {
			return time.Time{}, fmt.Errorf("release canonical probe lease: locked row changed")
		}
	}
	if legacy != nil {
		tag, execErr := tx.Exec(ctx, `
UPDATE channels
   SET half_open_probe_until=NULL,half_open_probe_token=NULL,updated_at=$4
 WHERE id=$1 AND (status='active' OR ($7 AND status='staged'))
   AND routing_epoch=$2 AND circuit_epoch=$3
   AND circuit_state='half_open' AND half_open_until<=$4
   AND half_open_probe_token=$5 AND half_open_probe_until IS NOT DISTINCT FROM $6
   AND half_open_probe_until>$4`,
			request.evidence.LegacyChannelID, request.evidence.LegacyRoutingRevision,
			request.evidence.LegacyCircuitRevision, dbNow, request.token, legacy.probeUntil,
			legacyHealthAllowsStaged(request.evidence),
		)
		if execErr != nil {
			return time.Time{}, execErr
		}
		if tag.RowsAffected() != 1 {
			return time.Time{}, fmt.Errorf("release legacy probe lease: locked row changed")
		}
	}
	return dbNow, nil
}

func lockReleasableCanonicalLeases(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
) ([]releasableCanonicalLease, bool, error) {
	if request.evidence.AuthorityMode == executionevidence.ModeLegacy {
		return nil, true, nil
	}
	var connectionState string
	var connectionRevision int64
	var legacyChannelID sql.NullInt64
	err := tx.QueryRow(ctx, `
SELECT admin_state,config_revision,legacy_channel_id
  FROM provider_connections WHERE id=$1 FOR SHARE`, request.evidence.ConnectionID).Scan(
		&connectionState, &connectionRevision, &legacyChannelID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	connectionReady := connectionState == "enabled" && connectionRevision == request.evidence.ConnectionRevision
	if request.evidence.AuthorityMode == executionevidence.ModeVerify {
		connectionReady = connectionReady && legacyChannelID.Valid && legacyChannelID.Int64 == request.evidence.LegacyChannelID
	}

	query := `
SELECT id,connection_id,public_model_id,legacy_channel_id,admin_state,config_revision,
       circuit_state,circuit_revision,half_open_until,half_open_probe_until,
       COALESCE(half_open_probe_token,'')
  FROM model_deployments WHERE id=$1 ORDER BY id FOR UPDATE`
	argument := request.evidence.DeploymentID
	if request.evidence.AuthorityMode == executionevidence.ModeVerify {
		query = `
SELECT id,connection_id,public_model_id,legacy_channel_id,admin_state,config_revision,
       circuit_state,circuit_revision,half_open_until,half_open_probe_until,
       COALESCE(half_open_probe_token,'')
  FROM model_deployments
 WHERE legacy_channel_id=$1 AND admin_state<>'retired'
 ORDER BY id FOR UPDATE`
		argument = request.evidence.LegacyChannelID
	}
	rows, err := tx.Query(ctx, query, argument)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	leases := make([]releasableCanonicalLease, 0, 1)
	selectedFound := false
	rowsReady := true
	for rows.Next() {
		var value releasableCanonicalLease
		if err := rows.Scan(
			&value.id, &value.connectionID, &value.publicModelID, &value.legacyChannelID,
			&value.adminState, &value.configRevision, &value.circuitState, &value.circuitRevision,
			&value.halfOpenUntil, &value.probeUntil, &value.probeToken,
		); err != nil {
			return nil, false, err
		}
		value.expectedConfigRevision = request.evidence.LegacyRoutingRevision
		value.expectedCircuitRevision = request.evidence.LegacyCircuitRevision
		if value.id == request.evidence.DeploymentID {
			selectedFound = true
			value.expectedConfigRevision = request.evidence.DeploymentRevision
			value.expectedCircuitRevision = request.evidence.CircuitRevision
			rowsReady = rowsReady && value.publicModelID == request.evidence.PublicModelID
		}
		rowsReady = rowsReady && value.connectionID == request.evidence.ConnectionID
		if request.evidence.AuthorityMode == executionevidence.ModeVerify {
			rowsReady = rowsReady && value.legacyChannelID.Valid && value.legacyChannelID.Int64 == request.evidence.LegacyChannelID
		}
		leases = append(leases, value)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return leases, connectionReady && rowsReady && selectedFound, nil
}

func lockReleasableLegacyLease(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
) (*breakerSnapshot, bool, error) {
	if request.evidence.AuthorityMode == executionevidence.ModeCanonical {
		return nil, true, nil
	}
	var current breakerSnapshot
	err := tx.QueryRow(ctx, `
SELECT id,status,routing_epoch,circuit_state,circuit_epoch,
       half_open_until,half_open_probe_until,COALESCE(half_open_probe_token,'')
  FROM channels WHERE id=$1 FOR UPDATE`, request.evidence.LegacyChannelID).Scan(
		&current.id, &current.adminState, &current.configRevision, &current.circuitState,
		&current.circuitRevision, &current.halfOpenUntil, &current.probeUntil, &current.probeToken,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &current, true, nil
}

func canonicalLeaseIsLive(current releasableCanonicalLease, request healthRequest, now time.Time) bool {
	return current.adminState == "enabled" &&
		current.configRevision == current.expectedConfigRevision &&
		current.circuitRevision == current.expectedCircuitRevision &&
		current.circuitState == "half_open" && current.halfOpenUntil != nil && !current.halfOpenUntil.After(now) &&
		current.probeToken == request.token && current.probeUntil != nil && current.probeUntil.After(now)
}

func legacyLeaseIsLive(current *breakerSnapshot, request healthRequest, now time.Time) bool {
	return current != nil && legacyChannelHealthEligible(request.evidence, current.adminState) &&
		current.configRevision == request.evidence.LegacyRoutingRevision &&
		current.circuitRevision == request.evidence.LegacyCircuitRevision &&
		current.circuitState == "half_open" && current.halfOpenUntil != nil && !current.halfOpenUntil.After(now) &&
		current.probeToken == request.token && current.probeUntil != nil && current.probeUntil.After(now)
}

func readReleaseClock(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var dbNow time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&dbNow); err != nil {
		return time.Time{}, err
	}
	return dbNow.UTC(), nil
}
