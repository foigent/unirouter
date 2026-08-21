package runtimechange

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modellist"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

var cutoverPlanHashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// CutoverParityVerifier proves full executable-plan parity inside the same
// transaction that will CAS the runtime slot. The comparison must include
// provider/upstream/adapter config, ordered routing semantics, breaker state,
// explicit credential eligibility and operation/probe revisions; hashes must
// not contain secret material.
type CutoverParityVerifier interface {
	Verify(
		context.Context,
		pgx.Tx,
		int64,
		int64,
		int64,
		int64,
	) (legacyPlanHash string, deploymentPlanHash string, err error)
}

type txOptionsBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// CutoverDeploymentStable performs the deliberately narrow v1-to-v2 bridge
// only after a caller-supplied planner proves full parity in this transaction.
// A v1 stable release and v2 canary are never mixed in one slot: after proving
// that the sealed v2 manifest represents the same currently admitted legacy
// deployments, this method closes the v1 release and changes the stable slot
// identity atomically.
func (m *Manager) CutoverDeploymentStable(
	ctx context.Context,
	ref string,
	expectedVersion, actorUserID, customerOrgID int64,
	verifier CutoverParityVerifier,
) (DeploymentRelease, error) {
	return DeploymentRelease{}, fmt.Errorf(
		"%w: legacy cutover must use ApplyDeploymentLifecycle with cutover_legacy",
		ErrConflict,
	)
}

// RollbackDeploymentStableToLegacy is the inverse CAS bridge for a v1-to-v2
// cutover. It never reconstructs legacy authority from v2 JSON: the exact
// sealed v1 parent is reactivated after its own current channel/key plan is
// proven executable inside this transaction. A broken v2 route must not block
// emergency rollback. The v2 release returns to validated state so a later,
// freshly verified cutover can reuse it when its pinned revisions still hold.
func (m *Manager) RollbackDeploymentStableToLegacy(
	ctx context.Context,
	ref string,
	expectedVersion, actorUserID, customerOrgID int64,
) (DeploymentRelease, error) {
	return DeploymentRelease{}, fmt.Errorf(
		"%w: legacy rollback must use ApplyDeploymentLifecycle with rollback_legacy",
		ErrConflict,
	)
}

// lockDeploymentRouteAuthority prevents a parity proof from being committed
// against resource rows that changed after its repeatable-read snapshot. The
// deployment lock also serializes probe inserts through their parent FK; the
// binding/link locks serialize credential rotation and scope changes. Changes
// committed after the cutover remain stronger live state and may make the
// assignment fail closed, but cannot be hidden inside the cutover snapshot.
func lockDeploymentRouteAuthority(ctx context.Context, tx pgx.Tx, routes []DeploymentRoute) error {
	deploymentIDs := make([]int64, 0, len(routes))
	for _, route := range routes {
		deploymentIDs = append(deploymentIDs, route.DeploymentID)
	}
	rows, err := tx.Query(ctx, `
SELECT deployment.id
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id
 WHERE deployment.id=ANY($1::bigint[])
 ORDER BY deployment.id,operation.operation_id
 FOR UPDATE OF deployment,connection,operation`, deploymentIDs)
	if err != nil {
		return lifecycleWriteError("lock deployment route authority", err)
	}
	for rows.Next() {
		var ignored int64
		if err := rows.Scan(&ignored); err != nil {
			rows.Close()
			return lifecycleWriteError("scan deployment route authority lock", err)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return lifecycleWriteError("iterate deployment route authority locks", err)
	}
	rows.Close()

	rows, err = tx.Query(ctx, `
SELECT link.binding_id
  FROM credential_binding_deployments link
  JOIN provider_credential_bindings binding ON binding.id=link.binding_id
 WHERE link.deployment_id=ANY($1::bigint[])
 ORDER BY link.deployment_id,link.binding_id
 FOR UPDATE OF link,binding`, deploymentIDs)
	if err != nil {
		return lifecycleWriteError("lock deployment credential authority", err)
	}
	for rows.Next() {
		var ignored int64
		if err := rows.Scan(&ignored); err != nil {
			rows.Close()
			return lifecycleWriteError("scan deployment credential authority lock", err)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return lifecycleWriteError("iterate deployment credential authority locks", err)
	}
	rows.Close()
	return nil
}

// The inverse bridge is exceptional and global to a legacy model namespace.
// Locking all legacy route/key rows avoids restoring a plan while an existing
// channel or key is being mutated. New rows committed later are a subsequent
// runtime change and do not weaken the CAS performed here.
func lockLegacyRollbackAuthority(ctx context.Context, tx pgx.Tx) error {
	for _, query := range []string{
		`SELECT id FROM channels ORDER BY id FOR UPDATE`,
		`SELECT id FROM channel_keys ORDER BY id FOR UPDATE`,
	} {
		rows, err := tx.Query(ctx, query)
		if err != nil {
			return fmt.Errorf("%w: lock legacy rollback authority: %v", ErrUnavailable, err)
		}
		for rows.Next() {
			var ignored int64
			if err := rows.Scan(&ignored); err != nil {
				rows.Close()
				return fmt.Errorf("%w: scan legacy rollback authority lock: %v", ErrUnavailable, err)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("%w: iterate legacy rollback authority locks: %v", ErrUnavailable, err)
		}
		rows.Close()
	}
	return nil
}

func (m *Manager) validateLegacyRollbackPlanWithHash(
	ctx context.Context,
	tx pgx.Tx,
	candidate Candidate,
) (string, error) {
	normalized, err := routingpolicy.ValidateOverrides(candidate.Overrides)
	if err != nil {
		return "", fmt.Errorf("%w: invalid sealed v1 rollback candidate: %v", ErrUnavailable, err)
	}
	var dbNow time.Time
	if err := tx.QueryRow(ctx, `SELECT NOW()`).Scan(&dbNow); err != nil {
		return "", fmt.Errorf("%w: read rollback route clock: %v", ErrUnavailable, err)
	}
	rows, err := tx.Query(ctx, `
SELECT channel.id,channel.name,channel.provider,channel.models,
       channel.priority,channel.routing_weight,channel.status,channel.circuit_state,
       channel.half_open_until,channel.routing_epoch,
       COALESCE(key.id,0),COALESCE(key.status,''),key.cooldown_until,
       COALESCE(key.allowed_models,''),COALESCE(key.key_enc,'')
  FROM channels channel
  LEFT JOIN channel_keys key ON key.channel_id=channel.id
	 ORDER BY channel.id,key.id`)
	if err != nil {
		return "", fmt.Errorf("%w: read legacy rollback plan: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	channels := make([]routingpolicy.SelectionChannel, 0)
	channelIndex := make(map[int64]int)
	channelEpochs := make(map[int64]int64)
	for rows.Next() {
		var id, epoch, keyID int64
		var name, provider, models, status, circuitState string
		var priority, weight int
		var halfOpenUntil, cooldownUntil *time.Time
		var keyStatus, allowedModels, keyEnc string
		if err := rows.Scan(
			&id, &name, &provider, &models, &priority, &weight, &status, &circuitState,
			&halfOpenUntil, &epoch, &keyID, &keyStatus, &cooldownUntil, &allowedModels, &keyEnc,
		); err != nil {
			return "", fmt.Errorf("%w: scan legacy rollback plan: %v", ErrUnavailable, err)
		}
		channelEpochs[id] = epoch
		if !modellist.Contains(models, candidate.ModelName) {
			continue
		}
		position, exists := channelIndex[id]
		if !exists {
			position = len(channels)
			channelIndex[id] = position
			channels = append(channels, routingpolicy.SelectionChannel{
				ID: id, Name: name, Provider: provider, Priority: priority, Weight: weight,
				Status: status, CircuitState: circuitState, HalfOpenUntil: halfOpenUntil,
			})
		}
		if keyID > 0 && keyEnc != "" {
			channels[position].Keys = append(channels[position].Keys, routingpolicy.SelectionKey{
				ID: keyID, Status: keyStatus, CooldownUntil: cooldownUntil, AllowedModels: allowedModels,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("%w: iterate legacy rollback plan: %v", ErrUnavailable, err)
	}
	for _, override := range normalized {
		epoch, exists := channelEpochs[override.ChannelID]
		if !exists || epoch != override.ExpectedRoutingEpoch {
			return "", fmt.Errorf("%w: legacy rollback channel %d revision", ErrConflict, override.ChannelID)
		}
		position, declaresModel := channelIndex[override.ChannelID]
		if !declaresModel {
			if override.Admission == routingpolicy.AdmissionDeny {
				continue
			}
			return "", fmt.Errorf("%w: legacy rollback channel %d no longer declares the model", ErrConflict, override.ChannelID)
		}
		channel := &channels[position]
		switch override.Admission {
		case routingpolicy.AdmissionAllow:
			if channel.Status != "disabled" {
				channel.Status = "active"
			}
		case routingpolicy.AdmissionDeny:
			channel.Status = "disabled"
		}
		if override.Priority != nil {
			channel.Priority = *override.Priority
		}
		if override.Weight != nil {
			channel.Weight = *override.Weight
		}
	}
	evaluation := routingpolicy.EvaluateSelection(routingpolicy.SelectionSnapshot{
		Model: candidate.ModelName, DBNow: dbNow.UTC(), Version: "legacy-rollback-check",
		Source: "sealed-v1-rollback", Channels: channels,
	}, m.policy, "rollback:"+candidate.ModelName)
	if len(evaluation.Selected) == 0 {
		return "", fmt.Errorf("%w: sealed v1 rollback target has no executable route (%s)", ErrConflict, evaluation.ExclusionReasons)
	}
	return hashJSON(struct {
		DBNow      time.Time                        `json:"db_now"`
		Candidate  Candidate                        `json:"candidate"`
		Channels   []routingpolicy.SelectionChannel `json:"channels"`
		Evaluation routingpolicy.Evaluation         `json:"evaluation"`
	}{
		DBNow: dbNow.UTC(), Candidate: candidate, Channels: channels, Evaluation: evaluation,
	}), nil
}

type legacyCutoverChannel struct {
	ID            int64
	Models        string
	Status        string
	RoutingEpoch  int64
	Priority      int
	Weight        int
	DeploymentID  int64
	DeploymentRev int64
	DeploymentPri int
	DeploymentWgt int
}

// verifyLegacyDeploymentStructure is only an early corruption/provenance
// check. It is deliberately insufficient for cutover authority; the planner
// verifier above owns full executable semantics.
func verifyLegacyDeploymentStructure(
	ctx context.Context,
	tx pgx.Tx,
	orgID, publicModelID int64,
	modelName string,
	overrides []routingpolicy.ChannelOverride,
	routes []DeploymentRoute,
) error {
	normalized, err := routingpolicy.ValidateOverrides(overrides)
	if err != nil {
		return fmt.Errorf("%w: invalid v1 cutover overrides: %v", ErrUnavailable, err)
	}
	overrideByChannel := make(map[int64]routingpolicy.ChannelOverride, len(normalized))
	for _, override := range normalized {
		overrideByChannel[override.ChannelID] = override
	}

	rows, err := tx.Query(ctx, `SELECT id,models,status,routing_epoch,priority,routing_weight FROM channels ORDER BY id`)
	if err != nil {
		return fmt.Errorf("%w: list v1 cutover channels: %v", ErrUnavailable, err)
	}
	channels := make(map[int64]legacyCutoverChannel)
	for rows.Next() {
		var channel legacyCutoverChannel
		if err := rows.Scan(&channel.ID, &channel.Models, &channel.Status, &channel.RoutingEpoch, &channel.Priority, &channel.Weight); err != nil {
			rows.Close()
			return fmt.Errorf("%w: scan v1 cutover channel: %v", ErrUnavailable, err)
		}
		if modellist.Contains(channel.Models, modelName) {
			channels[channel.ID] = channel
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("%w: list v1 cutover channels: %v", ErrUnavailable, err)
	}
	rows.Close()

	for channelID, override := range overrideByChannel {
		channel, ok := channels[channelID]
		if !ok {
			var models string
			if err := tx.QueryRow(ctx, `SELECT models FROM channels WHERE id=$1`, channelID).Scan(&models); err != nil {
				return fmt.Errorf("%w: v1 cutover channel %d is unavailable", ErrConflict, channelID)
			}
			if override.Admission != routingpolicy.AdmissionDeny {
				return fmt.Errorf("%w: v1 cutover channel %d no longer declares the public model", ErrConflict, channelID)
			}
			continue
		}
		if channel.RoutingEpoch != override.ExpectedRoutingEpoch {
			return fmt.Errorf("%w: v1 cutover channel %d revision", ErrConflict, channelID)
		}
	}

	expected := make(map[int64]legacyCutoverChannel)
	for channelID, channel := range channels {
		override, overridden := overrideByChannel[channelID]
		admitted := channel.Status == "active"
		if overridden {
			switch override.Admission {
			case routingpolicy.AdmissionAllow:
				admitted = channel.Status != "disabled"
			case routingpolicy.AdmissionDeny:
				admitted = false
			}
			if override.Priority != nil {
				channel.Priority = *override.Priority
			}
			if override.Weight != nil {
				channel.Weight = *override.Weight
			}
		}
		if !admitted {
			continue
		}
		if err := tx.QueryRow(ctx, `
SELECT id,config_revision,priority,routing_weight
  FROM model_deployments
 WHERE customer_org_id=$1 AND public_model_id=$2
   AND legacy_channel_id=$3 AND legacy_public_model=$4
   AND admin_state<>'retired'`,
			orgID, publicModelID, channelID, modelName).Scan(
			&channel.DeploymentID, &channel.DeploymentRev, &channel.DeploymentPri, &channel.DeploymentWgt,
		); err != nil {
			return fmt.Errorf("%w: v1 channel %d has no unambiguous canonical deployment", ErrConflict, channelID)
		}
		if channel.DeploymentRev != channel.RoutingEpoch {
			return fmt.Errorf("%w: v1 channel %d deployment revision", ErrConflict, channelID)
		}
		expected[channel.DeploymentID] = channel
	}
	if len(expected) == 0 {
		return fmt.Errorf("%w: v1 stable route has no admitted deployment", ErrConflict)
	}

	seen := make(map[int64]struct{}, len(expected))
	for _, route := range routes {
		channel, ok := expected[route.DeploymentID]
		if !ok {
			return fmt.Errorf("%w: v2 route introduces deployment %d absent from v1 stable", ErrConflict, route.DeploymentID)
		}
		effectivePriority := channel.DeploymentPri
		if route.Priority != nil {
			effectivePriority = *route.Priority
		}
		effectiveWeight := channel.DeploymentWgt
		if route.Weight != nil {
			effectiveWeight = *route.Weight
		}
		if effectivePriority != channel.Priority || effectiveWeight != channel.Weight {
			return fmt.Errorf("%w: v2 deployment %d changes v1 priority or weight", ErrConflict, route.DeploymentID)
		}
		seen[route.DeploymentID] = struct{}{}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("%w: v2 manifest omits an admitted v1 deployment", ErrConflict)
	}
	return nil
}
