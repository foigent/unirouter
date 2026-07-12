package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const maxRoutingModelLength = 200

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
 WHERE (',' || REPLACE(c.models, ' ', '') || ',' LIKE '%,' || $1 || ',%')
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

type eligibleRoute struct {
	channel       snapshotChannel
	key           models.ChannelKey
	requiresProbe bool
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
	routes, _, _ := r.evaluateSnapshot(load, modelName, selectionKey)
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
	_, candidates, exclusions := r.evaluateSnapshot(load, modelName, "preview:"+modelName)
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

func (r *Router) evaluateSnapshot(load snapshotLoad, modelName, selectionKey string) ([]RouteResult, []routingpolicy.PreviewCandidate, []routingpolicy.PreviewCandidate) {
	snapshot := load.Snapshot
	if selectionKey == "" {
		selectionKey = "server:" + modelName
	}
	eligible := make([]eligibleRoute, 0, len(snapshot.Channels))
	exclusions := make([]routingpolicy.PreviewCandidate, 0)
	reasonCounts := make(map[string]int)

	for _, candidate := range snapshot.Channels {
		key, requiresProbe, reason := r.evaluateChannel(candidate, modelName, selectionKey, snapshot.DBNow)
		if reason != "" {
			reasonCounts[reason]++
			exclusions = appendPreviewBounded(exclusions, previewExclusion(candidate, reason), r.policy.MaxCandidateCount)
			continue
		}
		if len(eligible) >= r.policy.MaxCandidateCount {
			reason = "candidate_limit"
			reasonCounts[reason]++
			exclusions = appendPreviewBounded(exclusions, previewExclusion(candidate, reason), r.policy.MaxCandidateCount)
			continue
		}
		eligible = append(eligible, eligibleRoute{channel: candidate, key: key, requiresProbe: requiresProbe})
	}

	ordered := weightedPriorityOrder(eligible, selectionKey)
	exclusionSummary := summarizeReasons(reasonCounts)
	routes := make([]RouteResult, 0, len(ordered))
	previews := make([]routingpolicy.PreviewCandidate, 0, len(ordered))
	for index, item := range ordered {
		ch := item.channel.Channel
		selectionReason := "priority_failover"
		if hasEqualPriorityPeer(ordered, index) {
			selectionReason = "equal_priority_static_weight"
		}
		routes = append(routes, RouteResult{
			Channel:              ch,
			ChannelKey:           item.key,
			MappedModel:          resolveModelMapping(modelName, ch.ModelMapping),
			RoutingPolicyVersion: routingpolicy.SelectionVersion,
			SnapshotVersion:      snapshot.Version,
			SnapshotSource:       load.Source,
			SnapshotDegraded:     load.Degraded,
			CandidateCount:       len(ordered),
			ExcludedCount:        totalReasonCount(reasonCounts),
			ExclusionReasons:     exclusionSummary,
			SelectionReason:      selectionReason,
			RequiresProbe:        item.requiresProbe,
		})
		previews = append(previews, routingpolicy.PreviewCandidate{
			ChannelID:        ch.ID,
			ChannelName:      ch.Name,
			Provider:         ch.Provider,
			Priority:         ch.Priority,
			Weight:           ch.RoutingWeight,
			Order:            index + 1,
			CircuitState:     ch.CircuitState,
			EligibleKeyCount: eligibleKeyCount(item.channel, modelName, snapshot.DBNow),
			Reason:           selectionReason,
			RequiresProbe:    item.requiresProbe,
		})
	}
	return routes, previews, exclusions
}

func (r *Router) evaluateChannel(candidate snapshotChannel, modelName, selectionKey string, dbNow time.Time) (models.ChannelKey, bool, string) {
	ch := candidate.Channel
	if ch.Status != "active" {
		return models.ChannelKey{}, false, "channel_inactive"
	}
	requiresProbe := false
	switch ch.CircuitState {
	case "healthy":
	case "half_open":
		if ch.HalfOpenUntil == nil || ch.HalfOpenUntil.After(dbNow) {
			return models.ChannelKey{}, false, "circuit_recovery_pending"
		}
		if stableModulo(selectionKey+"|probe|"+strconv.FormatInt(ch.ID, 10), 100) >= uint64(r.policy.CircuitBreakerHalfOpenPct) {
			return models.ChannelKey{}, false, "probe_sample_not_selected"
		}
		requiresProbe = true
	default:
		return models.ChannelKey{}, false, "circuit_unavailable"
	}

	keys := make([]models.ChannelKey, 0, len(candidate.Keys))
	hasActive, hasCooldown, hasModelMismatch := false, false, false
	for _, item := range candidate.Keys {
		if item.Key.Status != "active" {
			continue
		}
		hasActive = true
		if item.Key.CooldownUntil != nil && item.Key.CooldownUntil.After(dbNow) {
			hasCooldown = true
			continue
		}
		if !modelAllowed(item.AllowedModels, modelName) {
			hasModelMismatch = true
			continue
		}
		keys = append(keys, item.Key)
	}
	if len(keys) == 0 {
		switch {
		case len(candidate.Keys) == 0:
			return models.ChannelKey{}, false, "no_key"
		case !hasActive:
			return models.ChannelKey{}, false, "no_active_key"
		case hasCooldown:
			return models.ChannelKey{}, false, "key_cooldown"
		case hasModelMismatch:
			return models.ChannelKey{}, false, "key_model_not_allowed"
		default:
			return models.ChannelKey{}, false, "no_eligible_key"
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].ID < keys[j].ID })
	keyIndex := stableModulo(selectionKey+"|key|"+strconv.FormatInt(ch.ID, 10), uint64(len(keys)))
	return keys[keyIndex], requiresProbe, ""
}

func modelAllowed(allowedModels, modelName string) bool {
	allowedModels = strings.TrimSpace(allowedModels)
	if allowedModels == "" {
		return true
	}
	for _, allowed := range strings.Split(allowedModels, ",") {
		if strings.TrimSpace(allowed) == modelName {
			return true
		}
	}
	return false
}

func eligibleKeyCount(candidate snapshotChannel, modelName string, dbNow time.Time) int {
	count := 0
	for _, item := range candidate.Keys {
		if item.Key.Status == "active" && (item.Key.CooldownUntil == nil || !item.Key.CooldownUntil.After(dbNow)) && modelAllowed(item.AllowedModels, modelName) {
			count++
		}
	}
	return count
}

func weightedPriorityOrder(input []eligibleRoute, selectionKey string) []eligibleRoute {
	byPriority := make(map[int][]eligibleRoute)
	priorities := make([]int, 0)
	for _, item := range input {
		priority := item.channel.Channel.Priority
		if _, exists := byPriority[priority]; !exists {
			priorities = append(priorities, priority)
		}
		byPriority[priority] = append(byPriority[priority], item)
	}
	sort.Ints(priorities)
	result := make([]eligibleRoute, 0, len(input))
	for _, priority := range priorities {
		pool := append([]eligibleRoute(nil), byPriority[priority]...)
		sort.Slice(pool, func(i, j int) bool { return pool[i].channel.Channel.ID < pool[j].channel.Channel.ID })
		for round := 0; len(pool) > 0; round++ {
			total := uint64(0)
			for _, item := range pool {
				total += uint64(item.channel.Channel.RoutingWeight)
			}
			ticket := stableModulo(selectionKey+"|weight|"+strconv.Itoa(priority)+"|"+strconv.Itoa(round), total)
			selected := 0
			for index, item := range pool {
				weight := uint64(item.channel.Channel.RoutingWeight)
				if ticket < weight {
					selected = index
					break
				}
				ticket -= weight
			}
			result = append(result, pool[selected])
			pool = append(pool[:selected], pool[selected+1:]...)
		}
	}
	return result
}

func stableModulo(value string, modulus uint64) uint64 {
	if modulus == 0 {
		return 0
	}
	hash := sha256.Sum256([]byte(value))
	return binary.BigEndian.Uint64(hash[:8]) % modulus
}

func hasEqualPriorityPeer(routes []eligibleRoute, index int) bool {
	priority := routes[index].channel.Channel.Priority
	return (index > 0 && routes[index-1].channel.Channel.Priority == priority) ||
		(index+1 < len(routes) && routes[index+1].channel.Channel.Priority == priority)
}

func previewExclusion(candidate snapshotChannel, reason string) routingpolicy.PreviewCandidate {
	ch := candidate.Channel
	return routingpolicy.PreviewCandidate{
		ChannelID:        ch.ID,
		ChannelName:      ch.Name,
		Provider:         ch.Provider,
		Priority:         ch.Priority,
		Weight:           ch.RoutingWeight,
		CircuitState:     ch.CircuitState,
		EligibleKeyCount: 0,
		Reason:           reason,
	}
}

func appendPreviewBounded(items []routingpolicy.PreviewCandidate, item routingpolicy.PreviewCandidate, limit int) []routingpolicy.PreviewCandidate {
	if len(items) >= limit {
		return items
	}
	return append(items, item)
}

func summarizeReasons(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strconv.Itoa(counts[key]))
	}
	return strings.Join(parts, ",")
}

func totalReasonCount(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}
