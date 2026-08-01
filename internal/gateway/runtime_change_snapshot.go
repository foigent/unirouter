package gateway

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

// BuildRuntimeChangeSnapshots exports credential-free, same-clock snapshots
// for the governance kernel. It never exposes base URLs, mappings, encrypted
// keys, provider responses or request payloads.
func (r *Router) BuildRuntimeChangeSnapshots(
	ctx context.Context,
	modelName string,
	baselineOverrides, candidateOverrides []routingpolicy.ChannelOverride,
) (governancesim.RouteSnapshot, governancesim.RouteSnapshot, error) {
	load, err := r.loadRouteSnapshot(ctx, modelName)
	if err != nil {
		return governancesim.RouteSnapshot{}, governancesim.RouteSnapshot{}, err
	}
	baselineLoad, err := applyRuntimeAssignment(load, routingpolicy.RuntimeAssignment{Overrides: baselineOverrides})
	if err != nil {
		return governancesim.RouteSnapshot{}, governancesim.RouteSnapshot{}, fmt.Errorf("build baseline snapshot: %w", err)
	}
	candidateLoad, err := applyRuntimeAssignment(load, routingpolicy.RuntimeAssignment{Overrides: candidateOverrides})
	if err != nil {
		return governancesim.RouteSnapshot{}, governancesim.RouteSnapshot{}, fmt.Errorf("build candidate snapshot: %w", err)
	}
	return governanceSnapshot(baselineLoad.Snapshot), governanceSnapshot(candidateLoad.Snapshot), nil
}

func governanceSnapshot(snapshot *routeSnapshot) governancesim.RouteSnapshot {
	result := governancesim.RouteSnapshot{
		SnapshotID: snapshot.Version,
		Model:      snapshot.Model,
		DBNow:      formatUTCMicros(snapshot.DBNow),
		Source:     "sanitized_export",
		Channels:   make([]governancesim.ChannelSnapshot, 0, len(snapshot.Channels)),
	}
	for _, item := range snapshot.Channels {
		channel := item.Channel
		status := channel.Status
		if status != "active" {
			status = "disabled"
		}
		circuitState := channel.CircuitState
		switch circuitState {
		case "healthy", "half_open", "open":
		default:
			circuitState = "open"
		}
		entry := governancesim.ChannelSnapshot{
			ID:           strconv.FormatInt(channel.ID, 10),
			Name:         channel.Name,
			Provider:     strings.ToLower(strings.TrimSpace(channel.Provider)),
			Priority:     channel.Priority,
			Weight:       channel.RoutingWeight,
			Status:       status,
			CircuitState: circuitState,
			Keys:         make([]governancesim.KeySnapshot, 0, len(item.Keys)),
		}
		if channel.HalfOpenUntil != nil {
			entry.HalfOpenUntil = formatUTCMicros(*channel.HalfOpenUntil)
		}
		for _, key := range item.Keys {
			status := key.Key.Status
			if status != "active" {
				status = "disabled"
			}
			keyEntry := governancesim.KeySnapshot{
				ID:            strconv.FormatInt(key.Key.ID, 10),
				Status:        status,
				AllowedModels: key.AllowedModels,
			}
			if key.Key.CooldownUntil != nil {
				keyEntry.CooldownUntil = formatUTCMicros(*key.Key.CooldownUntil)
			}
			entry.Keys = append(entry.Keys, keyEntry)
		}
		result.Channels = append(result.Channels, entry)
	}
	return result
}

func formatUTCMicros(value time.Time) string {
	return value.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z")
}
