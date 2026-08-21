package modelexecution

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func sameBreakerProjection(left, right breakerSnapshot) bool {
	return left.circuitState == right.circuitState && left.failCount == right.failCount &&
		left.degradationThreshold == right.degradationThreshold && sameOptionalTime(left.halfOpenUntil, right.halfOpenUntil)
}

func classifyCanonicalBreakerMiss(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	expected breakerSnapshot,
) (Status, error) {
	current, status, err := readCanonicalBreaker(ctx, tx, expected.id)
	if err != nil || status != Applied {
		return status, err
	}
	if current.adminState != "enabled" {
		return Disabled, nil
	}
	if current.configRevision != expected.configRevision || current.circuitRevision != expected.circuitRevision {
		return Stale, nil
	}
	if request.evidence.RequiresProbe &&
		(current.probeToken != request.token || current.probeUntil == nil ||
			!current.probeUntil.After(request.databaseTime) || !sameOptionalTime(current.probeUntil, expected.probeUntil)) {
		return LeaseLost, nil
	}
	return Stale, nil
}

func readCanonicalBreaker(ctx context.Context, tx pgx.Tx, id int64) (breakerSnapshot, Status, error) {
	var value breakerSnapshot
	err := tx.QueryRow(ctx, `
SELECT id,connection_id,public_model_id,legacy_channel_id,admin_state,config_revision,
       circuit_state,circuit_revision,fail_count,degradation_threshold,
       half_open_until,half_open_probe_until,COALESCE(half_open_probe_token,'')
  FROM model_deployments WHERE id=$1 FOR UPDATE`, id).Scan(
		&value.id, &value.connectionID, &value.publicModelID, &value.legacyChannelID,
		&value.adminState, &value.configRevision, &value.circuitState, &value.circuitRevision,
		&value.failCount, &value.degradationThreshold, &value.halfOpenUntil, &value.probeUntil, &value.probeToken,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return breakerSnapshot{}, Stale, nil
	}
	if err != nil {
		return breakerSnapshot{}, "", err
	}
	return value, Applied, nil
}

func classifyLegacyBreakerMiss(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	expected breakerSnapshot,
) (Status, error) {
	current, status, err := lockLegacyBreaker(ctx, tx, request.evidence)
	if err != nil || status != Applied {
		return status, err
	}
	if current.configRevision != request.evidence.LegacyRoutingRevision ||
		current.circuitRevision != request.evidence.LegacyCircuitRevision {
		return Stale, nil
	}
	if request.evidence.RequiresProbe &&
		(current.probeToken != request.token || current.probeUntil == nil ||
			!current.probeUntil.After(request.databaseTime) || !sameOptionalTime(current.probeUntil, expected.probeUntil)) {
		return LeaseLost, nil
	}
	if expected.id != 0 && !sameBreakerProjection(current, expected) {
		return Stale, nil
	}
	return Stale, nil
}

func classifyCanonicalCredentialMiss(ctx context.Context, tx pgx.Tx, expected credentialSnapshot) (Status, error) {
	var state string
	var revision int64
	err := tx.QueryRow(ctx, `
SELECT status,state_revision FROM provider_credential_bindings WHERE id=$1 FOR UPDATE`, expected.id).Scan(&state, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if state != "active" {
		return Disabled, nil
	}
	if revision != expected.stateRevision {
		return Stale, nil
	}
	return Stale, nil
}

func classifyLegacyCredentialMiss(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
	expected credentialSnapshot,
) (Status, error) {
	current, status, err := lockLegacyCredential(ctx, tx, request.evidence)
	if err != nil || status != Applied {
		return status, err
	}
	if current.stateRevision != request.evidence.LegacyCredentialStateRevision ||
		(expected.id != 0 && (!sameOptionalTime(current.cooldownUntil, expected.cooldownUntil) ||
			!sameOptionalTime(current.lastUsedAt, expected.lastUsedAt) || current.failCount != expected.failCount)) {
		return Stale, nil
	}
	return Stale, nil
}

func validateProbeOwnership(current breakerSnapshot, request healthRequest, now time.Time) Status {
	if !request.evidence.RequiresProbe {
		if current.circuitState != "healthy" || current.probeToken != "" {
			return Stale
		}
		return Applied
	}
	if current.circuitState != "half_open" || current.halfOpenUntil == nil || current.halfOpenUntil.After(now) {
		return Stale
	}
	if current.probeToken != request.token || current.probeUntil == nil || !current.probeUntil.After(now) {
		return LeaseLost
	}
	return Applied
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
