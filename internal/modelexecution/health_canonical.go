package modelexecution

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executionevidence"
)

func lockCanonicalConnection(ctx context.Context, tx pgx.Tx, evidence executionevidence.EvidenceV1) (Status, error) {
	var state string
	var revision int64
	var legacyChannelID sql.NullInt64
	err := tx.QueryRow(ctx, `
SELECT admin_state,config_revision,legacy_channel_id
  FROM provider_connections
 WHERE id=$1
 FOR SHARE`, evidence.ConnectionID).Scan(&state, &revision, &legacyChannelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if state != "enabled" {
		return Disabled, nil
	}
	if revision != evidence.ConnectionRevision ||
		(evidence.AuthorityMode == executionevidence.ModeVerify && (!legacyChannelID.Valid || legacyChannelID.Int64 != evidence.LegacyChannelID)) {
		return Stale, nil
	}
	return Applied, nil
}

func lockCanonicalBreakers(ctx context.Context, tx pgx.Tx, request healthRequest) ([]breakerSnapshot, Status, error) {
	query := `
SELECT id,connection_id,public_model_id,legacy_channel_id,admin_state,config_revision,
       circuit_state,circuit_revision,fail_count,degradation_threshold,
       half_open_until,half_open_probe_until,COALESCE(half_open_probe_token,'')
  FROM model_deployments
 WHERE id=$1
 FOR UPDATE`
	argument := request.evidence.DeploymentID
	if request.evidence.AuthorityMode == executionevidence.ModeVerify &&
		(actionChangesBreaker(request.action) || request.evidence.RequiresProbe) {
		query = `
SELECT id,connection_id,public_model_id,legacy_channel_id,admin_state,config_revision,
       circuit_state,circuit_revision,fail_count,degradation_threshold,
       half_open_until,half_open_probe_until,COALESCE(half_open_probe_token,'')
  FROM model_deployments
 WHERE legacy_channel_id=$1 AND admin_state<>'retired'
 ORDER BY id
 FOR UPDATE`
		argument = request.evidence.LegacyChannelID
	}
	rows, err := tx.Query(ctx, query, argument)
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
			&value.failCount, &value.degradationThreshold, &value.halfOpenUntil, &value.probeUntil, &value.probeToken,
		); err != nil {
			return nil, "", err
		}
		if value.id == request.evidence.DeploymentID {
			selected = len(values)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if selected < 0 {
		status, classifyErr := classifyMissingDeployment(ctx, tx, request.evidence.DeploymentID)
		return nil, status, classifyErr
	}
	values[0], values[selected] = values[selected], values[0]
	selectedValue := values[0]
	for index := range values {
		value := values[index]
		if value.adminState != "enabled" {
			return nil, Disabled, nil
		}
		if value.id == request.evidence.DeploymentID {
			if value.connectionID != request.evidence.ConnectionID || value.publicModelID != request.evidence.PublicModelID ||
				value.configRevision != request.evidence.DeploymentRevision || value.circuitRevision != request.evidence.CircuitRevision {
				return nil, Stale, nil
			}
			if request.evidence.AuthorityMode == executionevidence.ModeVerify &&
				(!value.legacyChannelID.Valid || value.legacyChannelID.Int64 != request.evidence.LegacyChannelID) {
				return nil, Stale, nil
			}
			continue
		}
		if request.evidence.AuthorityMode != executionevidence.ModeVerify || !value.legacyChannelID.Valid ||
			value.legacyChannelID.Int64 != request.evidence.LegacyChannelID ||
			value.connectionID != request.evidence.ConnectionID ||
			value.configRevision != request.evidence.LegacyRoutingRevision ||
			value.circuitRevision != request.evidence.LegacyCircuitRevision ||
			!sameBreakerProjection(value, selectedValue) {
			return nil, Stale, nil
		}
		if request.evidence.RequiresProbe {
			if value.probeToken != selectedValue.probeToken ||
				!sameOptionalTime(value.probeUntil, selectedValue.probeUntil) {
				return nil, LeaseLost, nil
			}
		} else if value.probeToken != "" {
			return nil, LeaseLost, nil
		}
	}
	return values, Applied, nil
}

func classifyMissingDeployment(ctx context.Context, tx pgx.Tx, id int64) (Status, error) {
	var state string
	err := tx.QueryRow(ctx, `SELECT admin_state FROM model_deployments WHERE id=$1 FOR UPDATE`, id).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if state != "enabled" {
		return Disabled, nil
	}
	return Stale, nil
}

func lockCanonicalCredential(ctx context.Context, tx pgx.Tx, evidence executionevidence.EvidenceV1) (*credentialSnapshot, Status, error) {
	var value credentialSnapshot
	err := tx.QueryRow(ctx, `
SELECT id,connection_id,legacy_channel_key_id,status,state_revision,fail_count,last_used_at,cooldown_until
  FROM provider_credential_bindings
 WHERE id=$1
 FOR UPDATE`, evidence.CredentialBindingID).Scan(
		&value.id, &value.connectionID, &value.legacyKeyID, &value.status, &value.stateRevision,
		&value.failCount, &value.lastUsedAt, &value.cooldownUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, Stale, nil
	}
	if err != nil {
		return nil, "", err
	}
	if value.status != "active" {
		return nil, Disabled, nil
	}
	if value.connectionID != evidence.ConnectionID || value.stateRevision != evidence.CredentialStateRevision ||
		(evidence.AuthorityMode == executionevidence.ModeVerify && (!value.legacyKeyID.Valid || value.legacyKeyID.Int64 != evidence.LegacyChannelKeyID)) {
		return nil, Stale, nil
	}
	return &value, Applied, nil
}

func (c *Coordinator) applyCanonicalHealth(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	dbNow time.Time,
	breakers []breakerSnapshot,
	credential *credentialSnapshot,
) (Status, error) {
	selected := breakers[0]
	if request.action == actionRenewLease {
		for _, current := range breakers {
			status, err := c.renewCanonicalLease(ctx, tx, request, dbNow, current)
			if err != nil || status != Applied {
				return status, err
			}
		}
		return Applied, nil
	}
	if request.action == actionCredentialAuthFailure || request.action == actionCredentialRateLimited {
		if request.evidence.RequiresProbe {
			for _, current := range breakers {
				status, err := releaseCanonicalLease(ctx, tx, request, current, true)
				if err != nil || status != Applied {
					return status, err
				}
			}
		}
		return c.updateCanonicalCredential(ctx, tx, request, dbNow, *credential)
	}

	next, status := c.nextBreaker(selected, request, dbNow)
	if status != Applied {
		return status, nil
	}
	for _, value := range breakers {
		status, err := updateCanonicalBreaker(ctx, tx, request, value, next)
		if err != nil || status != Applied {
			return status, err
		}
	}
	if request.action == actionSuccess {
		return c.updateCanonicalCredential(ctx, tx, request, dbNow, *credential)
	}
	return Applied, nil
}

func (c *Coordinator) nextBreaker(current breakerSnapshot, request healthRequest, dbNow time.Time) (breakerNext, Status) {
	next := breakerNext{
		circuitState: current.circuitState, circuitRevision: current.circuitRevision,
		failCount: current.failCount, halfOpenUntil: current.halfOpenUntil,
		probeUntil: current.probeUntil, probeToken: current.probeToken,
	}
	if request.evidence.RequiresProbe {
		if current.adminState != "enabled" {
			return breakerNext{}, Disabled
		}
		if current.circuitState != "half_open" || current.probeToken != request.token ||
			current.probeUntil == nil || !current.probeUntil.After(dbNow) {
			return breakerNext{}, LeaseLost
		}
	} else if current.circuitState != "healthy" || current.probeToken != "" {
		return breakerNext{}, Stale
	}

	switch request.action {
	case actionSuccess:
		next.failCount = 0
		if request.evidence.RequiresProbe {
			next.circuitState = "healthy"
			next.circuitRevision++
			next.halfOpenUntil = nil
			next.probeUntil = nil
			next.probeToken = ""
		}
	case actionProviderFailure:
		next.failCount++
		if request.evidence.RequiresProbe || next.failCount >= current.degradationThreshold {
			next.circuitState = "half_open"
			next.circuitRevision++
			until := dbNow.Add(c.config.BreakerRecovery)
			next.halfOpenUntil = &until
			next.probeUntil = nil
			next.probeToken = ""
		}
	default:
		return breakerNext{}, Stale
	}
	return next, Applied
}

func updateCanonicalBreaker(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	current breakerSnapshot,
	next breakerNext,
) (Status, error) {
	tag, err := tx.Exec(ctx, `
UPDATE model_deployments
   SET fail_count=$5,circuit_state=$6,half_open_until=$7,
       half_open_probe_until=$8,half_open_probe_token=NULLIF($9,''),
       circuit_revision=$10,updated_at=NOW()
 WHERE id=$1 AND admin_state='enabled' AND config_revision=$2 AND circuit_revision=$3
   AND circuit_state=$4 AND fail_count=$11 AND degradation_threshold=$12
   AND half_open_until IS NOT DISTINCT FROM $13
   AND half_open_probe_until IS NOT DISTINCT FROM $14
   AND COALESCE(half_open_probe_token,'')=$15`,
		current.id, current.configRevision, current.circuitRevision, current.circuitState,
		next.failCount, next.circuitState, next.halfOpenUntil, next.probeUntil, next.probeToken,
		next.circuitRevision, current.failCount, current.degradationThreshold, current.halfOpenUntil,
		current.probeUntil, current.probeToken,
	)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 1 {
		return Applied, nil
	}
	return classifyCanonicalBreakerMiss(ctx, tx, request, current)
}

func (c *Coordinator) renewCanonicalLease(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	dbNow time.Time,
	current breakerSnapshot,
) (Status, error) {
	if status := validateProbeOwnership(current, request, dbNow); status != Applied {
		return status, nil
	}
	target := dbNow.Add(c.config.HalfOpenLease)
	if current.probeUntil != nil && !target.After(*current.probeUntil) {
		return Applied, nil
	}
	tag, err := tx.Exec(ctx, `
UPDATE model_deployments
   SET half_open_probe_until=$6,updated_at=NOW()
 WHERE id=$1 AND admin_state='enabled' AND config_revision=$2 AND circuit_revision=$3
   AND circuit_state='half_open' AND half_open_until<=$4
   AND half_open_probe_token=$5 AND half_open_probe_until>$4
   AND half_open_probe_until<$6
   AND half_open_probe_until IS NOT DISTINCT FROM $7`,
		current.id, current.configRevision, current.circuitRevision, dbNow, request.token, target, current.probeUntil,
	)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 1 {
		return Applied, nil
	}
	return classifyCanonicalBreakerMiss(ctx, tx, request, current)
}

func releaseCanonicalLease(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	current breakerSnapshot,
	requireLive bool,
) (Status, error) {
	predicate := ""
	arguments := []any{current.id, current.configRevision, current.circuitRevision, request.token, current.probeUntil}
	if requireLive {
		predicate = " AND half_open_probe_until>$6"
		arguments = append(arguments, request.databaseTime)
	}
	tag, err := tx.Exec(ctx, `
UPDATE model_deployments
   SET half_open_probe_until=NULL,half_open_probe_token=NULL,updated_at=NOW()
 WHERE id=$1 AND admin_state='enabled' AND config_revision=$2 AND circuit_revision=$3
   AND circuit_state='half_open' AND half_open_probe_token=$4
   AND half_open_probe_until IS NOT DISTINCT FROM $5`+predicate, arguments...)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 1 {
		return Applied, nil
	}
	return classifyCanonicalBreakerMiss(ctx, tx, request, current)
}

func (c *Coordinator) updateCanonicalCredential(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	dbNow time.Time,
	current credentialSnapshot,
) (Status, error) {
	nextRevision := current.stateRevision
	nextFailCount := current.failCount
	nextLastUsed := current.lastUsedAt
	nextCooldown := current.cooldownUntil
	switch request.action {
	case actionSuccess:
		nextFailCount = 0
		nextLastUsed = &dbNow
	case actionCredentialAuthFailure:
		nextRevision++
		until := dbNow.Add(c.config.AuthFailureCooldown)
		nextCooldown = &until
	case actionCredentialRateLimited:
		nextRevision++
		until := dbNow.Add(c.config.RateLimitCooldown)
		nextCooldown = &until
	default:
		return Stale, nil
	}
	tag, err := tx.Exec(ctx, `
UPDATE provider_credential_bindings
   SET fail_count=$5,last_used_at=$6,cooldown_until=$7,state_revision=$8,updated_at=NOW()
 WHERE id=$1 AND connection_id=$2 AND status='active' AND state_revision=$3
   AND fail_count=$4 AND last_used_at IS NOT DISTINCT FROM $9
   AND cooldown_until IS NOT DISTINCT FROM $10`,
		current.id, current.connectionID, current.stateRevision, current.failCount,
		nextFailCount, nextLastUsed, nextCooldown, nextRevision, current.lastUsedAt, current.cooldownUntil,
	)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 1 {
		return Applied, nil
	}
	return classifyCanonicalCredentialMiss(ctx, tx, current)
}
