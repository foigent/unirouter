package modelexecution

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executionevidence"
)

type legacyHealthState struct {
	breaker    breakerSnapshot
	credential *credentialSnapshot
}

func legacyHealthAllowsStaged(evidence executionevidence.EvidenceV1) bool {
	return evidence.AuthorityMode == executionevidence.ModeLegacy
}

func legacyChannelHealthEligible(evidence executionevidence.EvidenceV1, status string) bool {
	return status == "active" || (legacyHealthAllowsStaged(evidence) && status == "staged")
}

func lockLegacyHealth(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	canonicalBreaker *breakerSnapshot,
	canonicalCredential *credentialSnapshot,
) (legacyHealthState, Status, error) {
	breaker, status, err := lockLegacyBreaker(ctx, tx, request.evidence)
	if err != nil || status != Applied {
		return legacyHealthState{}, status, err
	}
	if canonicalBreaker != nil {
		if !sameBreakerProjection(breaker, *canonicalBreaker) {
			return legacyHealthState{}, Stale, nil
		}
		if breaker.probeToken != canonicalBreaker.probeToken ||
			!sameOptionalTime(breaker.probeUntil, canonicalBreaker.probeUntil) {
			return legacyHealthState{}, LeaseLost, nil
		}
	}
	state := legacyHealthState{breaker: breaker}
	if actionNeedsCredential(request.action) {
		credential, credentialStatus, credentialErr := lockLegacyCredential(ctx, tx, request.evidence)
		if credentialErr != nil || credentialStatus != Applied {
			return legacyHealthState{}, credentialStatus, credentialErr
		}
		if canonicalCredential != nil &&
			(credential.failCount != canonicalCredential.failCount ||
				!sameOptionalTime(credential.lastUsedAt, canonicalCredential.lastUsedAt) ||
				!sameOptionalTime(credential.cooldownUntil, canonicalCredential.cooldownUntil)) {
			return legacyHealthState{}, Stale, nil
		}
		state.credential = &credential
	}
	return state, Applied, nil
}

func (c *Coordinator) applyLockedLegacyHealth(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	dbNow time.Time,
	_ legacyHealthState,
) (Status, error) {
	// lockLegacyHealth already established verify equivalence and fixed row
	// order. The existing CAS helpers re-read only rows owned by this transaction
	// so their zero-row classification remains centralized.
	return c.applyLegacyHealth(ctx, tx, request, dbNow, nil, nil)
}

func (c *Coordinator) applyLegacyHealth(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	dbNow time.Time,
	canonicalBreaker *breakerSnapshot,
	canonicalCredential *credentialSnapshot,
) (Status, error) {
	if request.action == actionRenewLease {
		return c.renewLegacyLease(ctx, tx, request, dbNow, canonicalBreaker)
	}
	if request.action == actionCredentialAuthFailure || request.action == actionCredentialRateLimited {
		if request.evidence.RequiresProbe {
			status, err := c.releaseLegacyLease(ctx, tx, request, canonicalBreaker, true)
			if err != nil || status != Applied {
				return status, err
			}
		} else {
			status, err := validateLegacyChannelForCredential(ctx, tx, request.evidence)
			if err != nil || status != Applied {
				return status, err
			}
		}
		return c.updateLegacyCredential(
			ctx, tx, request, dbNow, canonicalCredential, request.evidence.LegacyCircuitRevision,
		)
	}

	current, status, err := c.legacyBreakerSource(ctx, tx, request, canonicalBreaker)
	if err != nil || status != Applied {
		return status, err
	}
	next, status := c.nextBreaker(current, request, dbNow)
	if status != Applied {
		return status, nil
	}
	status, err = updateLegacyBreaker(ctx, tx, request, current, next)
	if err != nil || status != Applied {
		return status, err
	}
	if request.action == actionSuccess {
		return c.updateLegacyCredential(ctx, tx, request, dbNow, canonicalCredential, next.circuitRevision)
	}
	return Applied, nil
}

func (c *Coordinator) legacyBreakerSource(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	canonical *breakerSnapshot,
) (breakerSnapshot, Status, error) {
	if canonical != nil {
		return *canonical, Applied, nil
	}
	value, status, err := lockLegacyBreaker(ctx, tx, request.evidence)
	if err != nil || status != Applied {
		return breakerSnapshot{}, status, err
	}
	return value, Applied, nil
}

func lockLegacyBreaker(ctx context.Context, tx pgx.Tx, evidence executionevidence.EvidenceV1) (breakerSnapshot, Status, error) {
	var value breakerSnapshot
	err := tx.QueryRow(ctx, `
SELECT id,status,routing_epoch,circuit_state,circuit_epoch,fail_count,degradation_threshold,
       half_open_until,half_open_probe_until,COALESCE(half_open_probe_token,'')
  FROM channels
 WHERE id=$1
 FOR UPDATE`, evidence.LegacyChannelID).Scan(
		&value.id, &value.adminState, &value.configRevision, &value.circuitState, &value.circuitRevision,
		&value.failCount, &value.degradationThreshold, &value.halfOpenUntil, &value.probeUntil, &value.probeToken,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return breakerSnapshot{}, Stale, nil
	}
	if err != nil {
		return breakerSnapshot{}, "", err
	}
	if !legacyChannelHealthEligible(evidence, value.adminState) {
		return breakerSnapshot{}, Disabled, nil
	}
	if value.configRevision != evidence.LegacyRoutingRevision || value.circuitRevision != evidence.LegacyCircuitRevision {
		return breakerSnapshot{}, Stale, nil
	}
	value.adminState = "enabled"
	return value, Applied, nil
}

func updateLegacyBreaker(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	current breakerSnapshot,
	next breakerNext,
) (Status, error) {
	tag, err := tx.Exec(ctx, `
UPDATE channels
   SET fail_count=$5,circuit_state=$6,half_open_until=$7,
       half_open_probe_until=$8,half_open_probe_token=NULLIF($9,''),
       circuit_epoch=$10,updated_at=NOW()
 WHERE id=$1 AND (status='active' OR ($16 AND status='staged'))
   AND routing_epoch=$2 AND circuit_epoch=$3
   AND circuit_state=$4 AND fail_count=$11 AND degradation_threshold=$12
   AND half_open_until IS NOT DISTINCT FROM $13
   AND half_open_probe_until IS NOT DISTINCT FROM $14
   AND COALESCE(half_open_probe_token,'')=$15`,
		request.evidence.LegacyChannelID, request.evidence.LegacyRoutingRevision,
		request.evidence.LegacyCircuitRevision, current.circuitState,
		next.failCount, next.circuitState, next.halfOpenUntil, next.probeUntil, next.probeToken,
		next.circuitRevision, current.failCount, current.degradationThreshold, current.halfOpenUntil,
		current.probeUntil, current.probeToken, legacyHealthAllowsStaged(request.evidence),
	)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 1 {
		return Applied, nil
	}
	return classifyLegacyBreakerMiss(ctx, tx, request, current)
}

func (c *Coordinator) renewLegacyLease(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	dbNow time.Time,
	canonical *breakerSnapshot,
) (Status, error) {
	current := breakerSnapshot{}
	if canonical == nil {
		var status Status
		var err error
		current, status, err = c.legacyBreakerSource(ctx, tx, request, nil)
		if err != nil || status != Applied {
			return status, err
		}
	} else {
		legacy, status, err := lockLegacyBreaker(ctx, tx, request.evidence)
		if err != nil || status != Applied {
			return status, err
		}
		if legacy.probeToken != canonical.probeToken || !sameOptionalTime(legacy.probeUntil, canonical.probeUntil) {
			return LeaseLost, nil
		}
		if !sameBreakerProjection(legacy, *canonical) {
			return Stale, nil
		}
		current = legacy
	}
	if status := validateProbeOwnership(current, request, dbNow); status != Applied {
		return status, nil
	}
	target := dbNow.Add(c.config.HalfOpenLease)
	if current.probeUntil != nil && !target.After(*current.probeUntil) {
		return Applied, nil
	}
	tag, err := tx.Exec(ctx, `
UPDATE channels
   SET half_open_probe_until=$6,updated_at=NOW()
 WHERE id=$1 AND (status='active' OR ($8 AND status='staged'))
   AND routing_epoch=$2 AND circuit_epoch=$3
   AND circuit_state='half_open' AND half_open_until<=$4
   AND half_open_probe_token=$5 AND half_open_probe_until>$4
   AND half_open_probe_until<$6
   AND half_open_probe_until IS NOT DISTINCT FROM $7`,
		request.evidence.LegacyChannelID, request.evidence.LegacyRoutingRevision,
		request.evidence.LegacyCircuitRevision, dbNow, request.token, target, current.probeUntil,
		legacyHealthAllowsStaged(request.evidence),
	)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 1 {
		return Applied, nil
	}
	return classifyLegacyBreakerMiss(ctx, tx, request, current)
}

func (c *Coordinator) releaseLegacyLease(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	canonical *breakerSnapshot,
	requireLive bool,
) (Status, error) {
	current := breakerSnapshot{}
	if canonical == nil {
		var status Status
		var err error
		current, status, err = lockLegacyBreaker(ctx, tx, request.evidence)
		if err != nil || status != Applied {
			return status, err
		}
	} else {
		current = *canonical
	}
	predicate := ""
	arguments := []any{
		request.evidence.LegacyChannelID, request.evidence.LegacyRoutingRevision,
		request.evidence.LegacyCircuitRevision, request.token, current.probeUntil,
		legacyHealthAllowsStaged(request.evidence),
	}
	if requireLive {
		predicate = " AND half_open_probe_until>$7"
		arguments = append(arguments, request.databaseTime)
	}
	tag, err := tx.Exec(ctx, `
UPDATE channels
   SET half_open_probe_until=NULL,half_open_probe_token=NULL,updated_at=NOW()
 WHERE id=$1 AND (status='active' OR ($6 AND status='staged'))
   AND routing_epoch=$2 AND circuit_epoch=$3
   AND circuit_state='half_open' AND half_open_probe_token=$4
   AND half_open_probe_until IS NOT DISTINCT FROM $5`+predicate,
		arguments...,
	)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 1 {
		return Applied, nil
	}
	return classifyLegacyBreakerMiss(ctx, tx, request, current)
}

func (c *Coordinator) updateLegacyCredential(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	dbNow time.Time,
	canonical *credentialSnapshot,
	channelCircuitRevision int64,
) (Status, error) {
	current := credentialSnapshot{}
	if canonical == nil {
		var status Status
		var err error
		current, status, err = lockLegacyCredential(ctx, tx, request.evidence)
		if err != nil || status != Applied {
			return status, err
		}
	} else {
		current = *canonical
	}
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
UPDATE channel_keys AS key
   SET fail_count=$5,last_used_at=$6,cooldown_until=$7,state_epoch=$8,updated_at=NOW()
  FROM channels AS channel
 WHERE key.id=$1 AND key.channel_id=$2 AND key.status='active' AND key.state_epoch=$3
   AND key.fail_count=$4 AND key.last_used_at IS NOT DISTINCT FROM $9
   AND key.cooldown_until IS NOT DISTINCT FROM $10
   AND channel.id=key.channel_id
   AND (channel.status='active' OR ($13 AND channel.status='staged'))
   AND channel.routing_epoch=$11 AND channel.circuit_epoch=$12`,
		request.evidence.LegacyChannelKeyID, request.evidence.LegacyChannelID,
		request.evidence.LegacyCredentialStateRevision, current.failCount,
		nextFailCount, nextLastUsed, nextCooldown, nextRevision, current.lastUsedAt, current.cooldownUntil,
		request.evidence.LegacyRoutingRevision, channelCircuitRevision,
		legacyHealthAllowsStaged(request.evidence),
	)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 1 {
		return Applied, nil
	}
	return classifyLegacyCredentialMiss(ctx, tx, request, current)
}

func validateLegacyChannelForCredential(ctx context.Context, tx pgx.Tx, evidence executionevidence.EvidenceV1) (Status, error) {
	var state string
	var routingRevision, circuitRevision int64
	err := tx.QueryRow(ctx, `
SELECT status,routing_epoch,circuit_epoch
  FROM channels
 WHERE id=$1
 FOR UPDATE`, evidence.LegacyChannelID).Scan(&state, &routingRevision, &circuitRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if !legacyChannelHealthEligible(evidence, state) {
		return Disabled, nil
	}
	if routingRevision != evidence.LegacyRoutingRevision || circuitRevision != evidence.LegacyCircuitRevision {
		return Stale, nil
	}
	return Applied, nil
}

func lockLegacyCredential(ctx context.Context, tx pgx.Tx, evidence executionevidence.EvidenceV1) (credentialSnapshot, Status, error) {
	var value credentialSnapshot
	err := tx.QueryRow(ctx, `
SELECT id,channel_id,status,state_epoch,fail_count,last_used_at,cooldown_until
  FROM channel_keys
 WHERE id=$1
 FOR UPDATE`, evidence.LegacyChannelKeyID).Scan(
		&value.id, &value.connectionID, &value.status, &value.stateRevision,
		&value.failCount, &value.lastUsedAt, &value.cooldownUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return credentialSnapshot{}, Stale, nil
	}
	if err != nil {
		return credentialSnapshot{}, "", err
	}
	if value.status != "active" {
		return credentialSnapshot{}, Disabled, nil
	}
	if value.connectionID != evidence.LegacyChannelID || value.stateRevision != evidence.LegacyCredentialStateRevision {
		return credentialSnapshot{}, Stale, nil
	}
	return value, Applied, nil
}
