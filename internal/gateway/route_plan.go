package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const maxRoutingModelLength = 200

// ErrRuntimeAssignmentStale means a retained release points at an older
// channel routing epoch. Operators must reconcile or roll back that release;
// silently ignoring the pin would make the preview disagree with live traffic.
var ErrRuntimeAssignmentStale = errors.New("runtime routing assignment is stale")

// channelQuery deliberately returns control-plane state that may make a
// channel ineligible. Filtering happens against one PostgreSQL clock value in
// Go so the preview can explain exclusions and all keys in a snapshot make the
// same cooldown/recovery decision.
const channelQuery = `WITH route_clock AS (SELECT NOW() AS db_now)
SELECT c.id, c.name, c.provider, c.base_url, c.models, c.model_mapping,
       COALESCE(c.adapter_type, 'openai_compatible'), COALESCE(c.adapter_config, '{}'::jsonb),
       c.max_context, c.default_max_output, c.priority, c.status,
       c.circuit_state, c.half_open_until, c.fail_count,
       COALESCE(ck.id, 0), COALESCE(ck.key_enc, ''), COALESCE(ck.status, ''), ck.cooldown_until,
       c.degradation_threshold, c.routing_weight, c.routing_epoch, c.circuit_epoch,
       c.half_open_probe_until, COALESCE(c.half_open_probe_token, ''),
       COALESCE(ck.state_epoch, 1), COALESCE(ck.allowed_models, ''), ck.last_used_at,
       route_clock.db_now
  FROM channels c
 CROSS JOIN route_clock
  LEFT JOIN channel_keys ck ON ck.channel_id = c.id
 WHERE EXISTS (
       SELECT 1
         FROM regexp_split_to_table(COALESCE(c.models, ''), '[,，;；\r\n]+') AS configured(model)
        WHERE BTRIM(configured.model) = $1
 )
 ORDER BY c.priority ASC, c.id ASC, ck.id ASC`

type routeSnapshot struct {
	Model    string
	LoadedAt time.Time
	DBNow    time.Time
	Version  string
	Channels []snapshotChannel
}

type snapshotChannel struct {
	Channel models.Channel
	Keys    []snapshotKey
}

type snapshotKey struct {
	Key           models.ChannelKey
	AllowedModels string
}

type snapshotLoad struct {
	Snapshot *routeSnapshot
	Source   string
	Degraded bool
}

// Route selects the first route from routing-policy.v1. It remains for callers
// that do not need failover; gateway requests should pass their operation ID to
// RouteAllWithKey so equal-priority weights distribute across requests.
func (r *Router) Route(ctx context.Context, modelName string) (*RouteResult, error) {
	routes, err := r.RouteAllWithKey(ctx, modelName, "route:"+modelName)
	if err != nil {
		return nil, err
	}
	return &routes[0], nil
}

// RouteAll is the compatibility entry point used by tests and internal tools.
func (r *Router) RouteAll(ctx context.Context, modelName string) ([]RouteResult, error) {
	return r.RouteAllWithKey(ctx, modelName, "route-all:"+modelName)
}

// RouteAllWithKey builds one deterministic failover plan. selectionKey must be
// the server-owned gateway request ID in production; no client-supplied routing
// hint is trusted.
func (r *Router) RouteAllWithKey(ctx context.Context, modelName, selectionKey string) ([]RouteResult, error) {
	load, err := r.loadRouteSnapshot(ctx, modelName)
	if err != nil {
		return nil, err
	}
	assignment, err := r.resolveRuntimeAssignment(ctx, modelName, selectionKey, false)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime routing assignment: %w", err)
	}
	load, err = applyRuntimeAssignment(load, assignment)
	if err != nil {
		return nil, err
	}
	routes, _, _ := r.evaluateSnapshot(load, modelName, selectionKey, assignment)
	if len(routes) == 0 {
		return nil, fmt.Errorf("no available channel for model %s", modelName)
	}
	return routes, nil
}

// Preview returns the same credential-free evaluation used by Chat and
// Embeddings. It is read-only and never claims a half-open probe lease.
func (r *Router) Preview(ctx context.Context, modelName string) (routingpolicy.Preview, error) {
	load, err := r.loadRouteSnapshot(ctx, modelName)
	if err != nil {
		return routingpolicy.Preview{}, err
	}
	assignment, err := r.resolveRuntimeAssignment(ctx, modelName, "preview:"+modelName, true)
	if err != nil {
		return routingpolicy.Preview{}, fmt.Errorf("resolve stable routing assignment: %w", err)
	}
	load, err = applyRuntimeAssignment(load, assignment)
	if err != nil {
		return routingpolicy.Preview{}, err
	}
	_, candidates, exclusions := r.evaluateSnapshot(load, modelName, "preview:"+modelName, assignment)
	return routingpolicy.Preview{
		PolicyVersion:   routingpolicy.SelectionVersion,
		ConfigVersion:   routingpolicy.Version,
		SnapshotVersion: load.Snapshot.Version,
		SnapshotSource:  load.Source,
		Degraded:        load.Degraded,
		Model:           modelName,
		Candidates:      candidates,
		Exclusions:      exclusions,
	}, nil
}

// Invalidate discards only process-local snapshots. Other replicas converge
// through the bounded refresh TTL; no cross-replica cache invalidation channel
// is required for this MVP.
func (r *Router) Invalidate() {
	if r == nil {
		return
	}
	r.cacheMu.Lock()
	r.cache = make(map[string]*routeSnapshot)
	r.cacheMu.Unlock()
	if r.runtime != nil {
		r.runtime.Invalidate()
	}
}

func (r *Router) loadRouteSnapshot(ctx context.Context, modelName string) (snapshotLoad, error) {
	if r == nil || r.db == nil {
		return snapshotLoad{}, fmt.Errorf("routing snapshot unavailable")
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return snapshotLoad{}, fmt.Errorf("model is required")
	}
	if len(modelName) > maxRoutingModelLength {
		return snapshotLoad{}, fmt.Errorf("model is too long")
	}
	now := r.clock()
	if cached := r.cachedSnapshot(modelName); snapshotAge(now, cached) < r.policy.SnapshotRefreshTTL() {
		return snapshotLoad{Snapshot: cached, Source: "cache"}, nil
	}

	value, err, _ := r.refreshGroup.Do(modelName, func() (interface{}, error) {
		now := r.clock()
		cached := r.cachedSnapshot(modelName)
		if snapshotAge(now, cached) < r.policy.SnapshotRefreshTTL() {
			return snapshotLoad{Snapshot: cached, Source: "cache"}, nil
		}
		loaded, loadErr := r.queryRouteSnapshot(ctx, modelName, now)
		if loadErr == nil {
			r.storeSnapshot(modelName, loaded)
			return snapshotLoad{Snapshot: loaded, Source: "live"}, nil
		}
		if cached != nil && snapshotAge(now, cached) <= r.policy.SnapshotStaleTTL() {
			return snapshotLoad{Snapshot: cached, Source: "last_known_good", Degraded: true}, nil
		}
		return snapshotLoad{}, fmt.Errorf("query routing snapshot: %w", loadErr)
	})
	if err != nil {
		return snapshotLoad{}, err
	}
	return value.(snapshotLoad), nil
}

func (r *Router) storeSnapshot(modelName string, snapshot *routeSnapshot) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if _, exists := r.cache[modelName]; !exists && len(r.cache) >= r.policy.MaxSnapshotCount {
		oldestModel := ""
		var oldestTime time.Time
		for cachedModel, cached := range r.cache {
			if cached == nil || oldestModel == "" || cached.LoadedAt.Before(oldestTime) {
				oldestModel = cachedModel
				if cached != nil {
					oldestTime = cached.LoadedAt
				}
			}
		}
		delete(r.cache, oldestModel)
	}
	r.cache[modelName] = snapshot
}

func (r *Router) cachedSnapshot(modelName string) *routeSnapshot {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()
	return r.cache[modelName]
}

func (r *Router) clock() time.Time {
	if r != nil && r.now != nil {
		return r.now().UTC()
	}
	return time.Now().UTC()
}

func snapshotAge(now time.Time, snapshot *routeSnapshot) time.Duration {
	if snapshot == nil {
		return time.Duration(1<<63 - 1)
	}
	age := now.Sub(snapshot.LoadedAt)
	if age < 0 {
		return 0
	}
	return age
}

func (r *Router) queryRouteSnapshot(ctx context.Context, modelName string, loadedAt time.Time) (*routeSnapshot, error) {
	rows, err := r.db.Query(ctx, channelQuery, modelName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	channelIndex := make(map[int64]int)
	snapshot := &routeSnapshot{Model: modelName, LoadedAt: loadedAt, DBNow: loadedAt}
	for rows.Next() {
		var ch models.Channel
		var ck models.ChannelKey
		var allowedModels string
		var dbNow time.Time
		if err := rows.Scan(
			&ch.ID, &ch.Name, &ch.Provider, &ch.BaseURL, &ch.Models, &ch.ModelMapping,
			&ch.AdapterType, &ch.AdapterConfig, &ch.MaxContext, &ch.DefaultMaxOutput, &ch.Priority, &ch.Status,
			&ch.CircuitState, &ch.HalfOpenUntil, &ch.FailCount,
			&ck.ID, &ck.KeyEnc, &ck.Status, &ck.CooldownUntil,
			&ch.DegradationThreshold, &ch.RoutingWeight, &ch.RoutingEpoch, &ch.CircuitEpoch,
			&ch.HalfOpenProbeUntil, &ch.HalfOpenProbeToken,
			&ck.StateEpoch, &allowedModels, &ck.LastUsedAt, &dbNow,
		); err != nil {
			return nil, fmt.Errorf("scan routing snapshot: %w", err)
		}
		normalizeRouteState(r.policy, &ch, &ck, &dbNow, loadedAt)
		snapshot.DBNow = dbNow.UTC()

		index, exists := channelIndex[ch.ID]
		if !exists {
			ch.AdapterType = adapters.NormalizeType(ch.AdapterType)
			index = len(snapshot.Channels)
			channelIndex[ch.ID] = index
			snapshot.Channels = append(snapshot.Channels, snapshotChannel{Channel: ch})
		}
		if ck.ID > 0 {
			snapshot.Channels[index].Keys = append(snapshot.Channels[index].Keys, snapshotKey{Key: ck, AllowedModels: allowedModels})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routing snapshot: %w", err)
	}
	snapshot.Version = routeSnapshotVersion(snapshot)
	return snapshot, nil
}

func normalizeRouteState(policy routingpolicy.Config, ch *models.Channel, ck *models.ChannelKey, dbNow *time.Time, fallback time.Time) {
	if ch.RoutingWeight < 1 {
		ch.RoutingWeight = 100
	}
	if ch.RoutingEpoch < 1 {
		ch.RoutingEpoch = 1
	}
	if ch.CircuitEpoch < 1 {
		ch.CircuitEpoch = 1
	}
	if ch.DegradationThreshold < 1 {
		ch.DegradationThreshold = policy.CircuitBreakerThreshold
	}
	if ck.ID > 0 && ck.StateEpoch < 1 {
		ck.StateEpoch = 1
	}
	if dbNow.IsZero() {
		*dbNow = fallback
	}
}

func routeSnapshotVersion(snapshot *routeSnapshot) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%s|%d|", snapshot.Model, snapshot.DBNow.Unix())
	for _, candidate := range snapshot.Channels {
		ch := candidate.Channel
		fmt.Fprintf(hash, "c:%d:%s:%s:%d:%d:%d:%d:%s:%d:%v:%v|", ch.ID, ch.Status, ch.CircuitState, ch.Priority, ch.RoutingWeight, ch.RoutingEpoch, ch.CircuitEpoch, ch.Models, ch.FailCount, ch.HalfOpenUntil, ch.HalfOpenProbeUntil)
		for _, item := range candidate.Keys {
			keyHash := sha256.Sum256([]byte(item.Key.KeyEnc))
			fmt.Fprintf(hash, "k:%d:%s:%d:%v:%s:%x|", item.Key.ID, item.Key.Status, item.Key.StateEpoch, item.Key.CooldownUntil, item.AllowedModels, keyHash[:])
		}
	}
	return "rs1_" + hex.EncodeToString(hash.Sum(nil)[:12])
}

func (r *Router) evaluateSnapshot(load snapshotLoad, modelName, selectionKey string, assignment routingpolicy.RuntimeAssignment) ([]RouteResult, []routingpolicy.PreviewCandidate, []routingpolicy.PreviewCandidate) {
	snapshot := load.Snapshot
	policySnapshot := routingpolicy.SelectionSnapshot{
		Model:    modelName,
		DBNow:    snapshot.DBNow,
		Version:  snapshot.Version,
		Source:   load.Source,
		Degraded: load.Degraded,
		Channels: make([]routingpolicy.SelectionChannel, 0, len(snapshot.Channels)),
	}
	channelByID := make(map[int64]snapshotChannel, len(snapshot.Channels))
	for _, candidate := range snapshot.Channels {
		channelByID[candidate.Channel.ID] = candidate
		keys := make([]routingpolicy.SelectionKey, 0, len(candidate.Keys))
		for _, item := range candidate.Keys {
			keys = append(keys, routingpolicy.SelectionKey{
				ID:            item.Key.ID,
				Status:        item.Key.Status,
				CooldownUntil: item.Key.CooldownUntil,
				AllowedModels: item.AllowedModels,
			})
		}
		policySnapshot.Channels = append(policySnapshot.Channels, routingpolicy.SelectionChannel{
			ID:            candidate.Channel.ID,
			Name:          candidate.Channel.Name,
			Provider:      candidate.Channel.Provider,
			Priority:      candidate.Channel.Priority,
			Weight:        candidate.Channel.RoutingWeight,
			Status:        candidate.Channel.Status,
			CircuitState:  candidate.Channel.CircuitState,
			HalfOpenUntil: candidate.Channel.HalfOpenUntil,
			Keys:          keys,
		})
	}
	evaluation := routingpolicy.EvaluateSelection(policySnapshot, r.policy, selectionKey)
	routes := make([]RouteResult, 0, len(evaluation.Selected))
	previews := make([]routingpolicy.PreviewCandidate, 0, len(evaluation.Selected))
	for _, item := range evaluation.Selected {
		candidate := channelByID[item.ChannelID]
		ch := candidate.Channel
		var selectedKey models.ChannelKey
		for _, key := range candidate.Keys {
			if key.Key.ID == item.KeyID {
				selectedKey = key.Key
				break
			}
		}
		routes = append(routes, RouteResult{
			Channel:              ch,
			ChannelKey:           selectedKey,
			MappedModel:          resolveModelMapping(modelName, ch.ModelMapping),
			RoutingPolicyVersion: routingpolicy.SelectionVersion,
			SnapshotVersion:      snapshot.Version,
			SnapshotSource:       load.Source,
			SnapshotDegraded:     load.Degraded,
			CandidateCount:       len(evaluation.Selected),
			ExcludedCount:        evaluation.ExcludedCount,
			ExclusionReasons:     evaluation.ExclusionReasons,
			SelectionReason:      item.Preview.Reason,
			RequiresProbe:        item.RequiresProbe,
			RuntimeReleaseRef:    assignment.ReleaseRef,
			RuntimeStage:         assignment.Stage,
			RuntimeAssignmentID:  assignment.Identity,
		})
		previews = append(previews, item.Preview)
	}
	return routes, previews, evaluation.Exclusions
}

func (r *Router) resolveRuntimeAssignment(ctx context.Context, modelName, selectionKey string, stable bool) (routingpolicy.RuntimeAssignment, error) {
	if r == nil || r.runtime == nil {
		return routingpolicy.RuntimeAssignment{}, nil
	}
	if stable {
		return r.runtime.Stable(ctx, modelName)
	}
	return r.runtime.Resolve(ctx, modelName, selectionKey)
}

func applyRuntimeAssignment(load snapshotLoad, assignment routingpolicy.RuntimeAssignment) (snapshotLoad, error) {
	if len(assignment.Overrides) == 0 {
		return load, nil
	}
	normalized, err := routingpolicy.ValidateOverrides(assignment.Overrides)
	if err != nil {
		return snapshotLoad{}, fmt.Errorf("invalid runtime routing assignment: %w", err)
	}
	copySnapshot := *load.Snapshot
	copySnapshot.Channels = append([]snapshotChannel(nil), load.Snapshot.Channels...)
	index := make(map[int64]int, len(copySnapshot.Channels))
	for position := range copySnapshot.Channels {
		copySnapshot.Channels[position].Keys = append([]snapshotKey(nil), copySnapshot.Channels[position].Keys...)
		index[copySnapshot.Channels[position].Channel.ID] = position
	}
	for _, override := range normalized {
		position, exists := index[override.ChannelID]
		if !exists {
			return snapshotLoad{}, fmt.Errorf("runtime assignment channel %d is absent from model snapshot", override.ChannelID)
		}
		channel := &copySnapshot.Channels[position].Channel
		if channel.RoutingEpoch != override.ExpectedRoutingEpoch {
			return snapshotLoad{}, fmt.Errorf("%w: channel %d epoch conflict", ErrRuntimeAssignmentStale, override.ChannelID)
		}
		// Emergency/manual disable remains stronger than a progressive release.
		if override.Admission == routingpolicy.AdmissionAllow && channel.Status != "disabled" {
			channel.Status = "active"
		}
		if override.Admission == routingpolicy.AdmissionDeny {
			channel.Status = "disabled"
		}
		if override.Priority != nil {
			channel.Priority = *override.Priority
		}
		if override.Weight != nil {
			channel.RoutingWeight = *override.Weight
		}
	}
	copySnapshot.Version = routeSnapshotVersion(&copySnapshot)
	load.Snapshot = &copySnapshot
	return load, nil
}
