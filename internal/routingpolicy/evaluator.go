package routingpolicy

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SelectionSnapshot is the credential-free, normalized input to the routing
// policy kernel. Gateway runtime routing and offline simulation both use this
// exact type so they cannot silently diverge.
type SelectionSnapshot struct {
	Model    string
	DBNow    time.Time
	Version  string
	Source   string
	Degraded bool
	Channels []SelectionChannel
}

type SelectionChannel struct {
	ID            int64
	Name          string
	Provider      string
	Priority      int
	Weight        int
	Status        string
	CircuitState  string
	HalfOpenUntil *time.Time
	Keys          []SelectionKey
}

type SelectionKey struct {
	ID            int64
	Status        string
	CooldownUntil *time.Time
	AllowedModels string
}

type SelectedCandidate struct {
	ChannelID     int64
	KeyID         int64
	RequiresProbe bool
	Preview       PreviewCandidate
}

type Evaluation struct {
	Selected         []SelectedCandidate
	Exclusions       []PreviewCandidate
	ExcludedCount    int
	ExclusionReasons string
}

type eligibleCandidate struct {
	channel       SelectionChannel
	keyID         int64
	requiresProbe bool
}

// EvaluateSelection evaluates one immutable, already-normalized snapshot. It
// has no persistence, network, secret, clock or random dependency.
func EvaluateSelection(snapshot SelectionSnapshot, policy Config, selectionKey string) Evaluation {
	if selectionKey == "" {
		selectionKey = "server:" + snapshot.Model
	}
	eligible := make([]eligibleCandidate, 0, len(snapshot.Channels))
	exclusions := make([]PreviewCandidate, 0)
	reasonCounts := make(map[string]int)

	for _, channel := range snapshot.Channels {
		keyID, requiresProbe, reason := evaluateChannel(channel, snapshot.Model, selectionKey, snapshot.DBNow, policy)
		if reason != "" {
			reasonCounts[reason]++
			exclusions = appendPreviewBounded(exclusions, previewExclusion(channel, reason), policy.MaxCandidateCount)
			continue
		}
		if len(eligible) >= policy.MaxCandidateCount {
			reason = "candidate_limit"
			reasonCounts[reason]++
			exclusions = appendPreviewBounded(exclusions, previewExclusion(channel, reason), policy.MaxCandidateCount)
			continue
		}
		eligible = append(eligible, eligibleCandidate{
			channel:       channel,
			keyID:         keyID,
			requiresProbe: requiresProbe,
		})
	}

	ordered := weightedPriorityOrder(eligible, selectionKey)
	selected := make([]SelectedCandidate, 0, len(ordered))
	for index, item := range ordered {
		reason := "priority_failover"
		if hasEqualPriorityPeer(ordered, index) {
			reason = "equal_priority_static_weight"
		}
		selected = append(selected, SelectedCandidate{
			ChannelID:     item.channel.ID,
			KeyID:         item.keyID,
			RequiresProbe: item.requiresProbe,
			Preview: PreviewCandidate{
				ChannelID:        item.channel.ID,
				ChannelName:      item.channel.Name,
				Provider:         item.channel.Provider,
				Priority:         item.channel.Priority,
				Weight:           item.channel.Weight,
				Order:            index + 1,
				CircuitState:     item.channel.CircuitState,
				EligibleKeyCount: eligibleKeyCount(item.channel, snapshot.Model, snapshot.DBNow),
				Reason:           reason,
				RequiresProbe:    item.requiresProbe,
			},
		})
	}
	return Evaluation{
		Selected:         selected,
		Exclusions:       exclusions,
		ExcludedCount:    totalReasonCount(reasonCounts),
		ExclusionReasons: summarizeReasons(reasonCounts),
	}
}

func evaluateChannel(
	channel SelectionChannel,
	modelName string,
	selectionKey string,
	dbNow time.Time,
	policy Config,
) (int64, bool, string) {
	if channel.Status != "active" {
		return 0, false, "channel_inactive"
	}
	requiresProbe := false
	switch channel.CircuitState {
	case "healthy":
	case "half_open":
		if channel.HalfOpenUntil == nil || channel.HalfOpenUntil.After(dbNow) {
			return 0, false, "circuit_recovery_pending"
		}
		if stableModulo(selectionKey+"|probe|"+strconv.FormatInt(channel.ID, 10), 100) >= uint64(policy.CircuitBreakerHalfOpenPct) {
			return 0, false, "probe_sample_not_selected"
		}
		requiresProbe = true
	default:
		return 0, false, "circuit_unavailable"
	}

	keys := make([]SelectionKey, 0, len(channel.Keys))
	hasActive, hasCooldown, hasModelMismatch := false, false, false
	for _, key := range channel.Keys {
		if key.Status != "active" {
			continue
		}
		hasActive = true
		if key.CooldownUntil != nil && key.CooldownUntil.After(dbNow) {
			hasCooldown = true
			continue
		}
		if !modelAllowed(key.AllowedModels, modelName) {
			hasModelMismatch = true
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		switch {
		case len(channel.Keys) == 0:
			return 0, false, "no_key"
		case !hasActive:
			return 0, false, "no_active_key"
		case hasCooldown:
			return 0, false, "key_cooldown"
		case hasModelMismatch:
			return 0, false, "key_model_not_allowed"
		default:
			return 0, false, "no_eligible_key"
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].ID < keys[j].ID })
	keyIndex := stableModulo(selectionKey+"|key|"+strconv.FormatInt(channel.ID, 10), uint64(len(keys)))
	return keys[keyIndex].ID, requiresProbe, ""
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

func eligibleKeyCount(channel SelectionChannel, modelName string, dbNow time.Time) int {
	count := 0
	for _, key := range channel.Keys {
		if key.Status == "active" &&
			(key.CooldownUntil == nil || !key.CooldownUntil.After(dbNow)) &&
			modelAllowed(key.AllowedModels, modelName) {
			count++
		}
	}
	return count
}

func weightedPriorityOrder(input []eligibleCandidate, selectionKey string) []eligibleCandidate {
	byPriority := make(map[int][]eligibleCandidate)
	priorities := make([]int, 0)
	for _, item := range input {
		priority := item.channel.Priority
		if _, exists := byPriority[priority]; !exists {
			priorities = append(priorities, priority)
		}
		byPriority[priority] = append(byPriority[priority], item)
	}
	sort.Ints(priorities)
	result := make([]eligibleCandidate, 0, len(input))
	for _, priority := range priorities {
		pool := append([]eligibleCandidate(nil), byPriority[priority]...)
		sort.Slice(pool, func(i, j int) bool { return pool[i].channel.ID < pool[j].channel.ID })
		for round := 0; len(pool) > 0; round++ {
			total := uint64(0)
			for _, item := range pool {
				total += uint64(item.channel.Weight)
			}
			ticket := stableModulo(selectionKey+"|weight|"+strconv.Itoa(priority)+"|"+strconv.Itoa(round), total)
			selected := 0
			for index, item := range pool {
				weight := uint64(item.channel.Weight)
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

func hasEqualPriorityPeer(routes []eligibleCandidate, index int) bool {
	priority := routes[index].channel.Priority
	return (index > 0 && routes[index-1].channel.Priority == priority) ||
		(index+1 < len(routes) && routes[index+1].channel.Priority == priority)
}

func previewExclusion(channel SelectionChannel, reason string) PreviewCandidate {
	return PreviewCandidate{
		ChannelID:        channel.ID,
		ChannelName:      channel.Name,
		Provider:         channel.Provider,
		Priority:         channel.Priority,
		Weight:           channel.Weight,
		CircuitState:     channel.CircuitState,
		EligibleKeyCount: 0,
		Reason:           reason,
	}
}

func appendPreviewBounded(items []PreviewCandidate, item PreviewCandidate, limit int) []PreviewCandidate {
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
