package modelrouting

import (
	"context"
	"encoding/json"
	"time"
)

// pairedSnapshotCache is the only supported cache boundary for verify mode.
// An implementation stores one immutable entry containing both projections;
// it must never cache or refresh legacy and canonical halves independently.
// The first integration intentionally remains uncached until the gateway owns
// bounded TTL, invalidation and singleflight as one unit.
type pairedSnapshotCache interface {
	Get(context.Context, pairedSnapshotCacheKey, time.Time) (pairedSnapshotCacheEntry, bool)
	Put(context.Context, pairedSnapshotCacheKey, pairedSnapshotCacheEntry)
	InvalidatePublicModel(context.Context, int64, int64)
}

type pairedSnapshotCacheKey string

type pairedSnapshotCacheEntry struct {
	Value        snapshot
	SnapshotHash string
	ExpiresAt    time.Time
}

// newPairedSnapshotCacheKey is an authority-version lookup key. The stored
// entry's SnapshotHash additionally covers DB NOW and every executable fact.
// Secrets are absent from both hashes.
func newPairedSnapshotCacheKey(value snapshot) pairedSnapshotCacheKey {
	payload := struct {
		Mode                 Mode     `json:"mode"`
		CustomerOrgID        int64    `json:"customer_org_id"`
		PublicModelID        int64    `json:"public_model_id"`
		PublicModel          string   `json:"public_model"`
		OperationID          string   `json:"operation_id"`
		RequiredCapabilities []string `json:"required_capabilities"`
		SlotVersion          int64    `json:"slot_version"`
		SelectedReleaseID    int64    `json:"selected_release_id"`
		SelectedManifestHash string   `json:"selected_manifest_hash"`
		LegacyReleaseID      int64    `json:"legacy_release_id"`
		LegacyManifestHash   string   `json:"legacy_manifest_hash"`
	}{
		Mode: value.Mode, CustomerOrgID: value.CustomerOrgID,
		PublicModelID: value.PublicModelID, PublicModel: value.PublicModel,
		OperationID:          value.RequestedOperationID,
		RequiredCapabilities: append([]string(nil), value.RequiredCapabilities...),
		SlotVersion:          value.Slot.Version, SelectedReleaseID: value.Slot.Selected.ID,
		SelectedManifestHash: value.Slot.Selected.RouteItemsHash,
		LegacyReleaseID:      value.LegacyRelease.ID, LegacyManifestHash: value.LegacyRelease.RouteItemsHash,
	}
	body, _ := json.Marshal(payload)
	return pairedSnapshotCacheKey(digestBytes(body))
}
