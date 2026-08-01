package routingpolicy

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

const RuntimeAssignmentVersion = "unirouter.runtime-route-assignment.v1"

const (
	AdmissionInherit = "inherit"
	AdmissionAllow   = "allow"
	AdmissionDeny    = "deny"
)

// ChannelOverride is the deliberately narrow hot-release surface. It cannot
// change credentials, endpoints, model mappings, retry policy, breaker state,
// billing or IAM authority.
type ChannelOverride struct {
	ChannelID            int64  `json:"channel_id"`
	ExpectedRoutingEpoch int64  `json:"expected_routing_epoch"`
	Admission            string `json:"admission"`
	Priority             *int   `json:"priority,omitempty"`
	Weight               *int   `json:"weight,omitempty"`
}

type RuntimeAssignment struct {
	ContractVersion string            `json:"contract_version"`
	Identity        string            `json:"identity"`
	ReleaseRef      string            `json:"release_ref"`
	Stage           string            `json:"stage"`
	Overrides       []ChannelOverride `json:"overrides"`
}

// RuntimeResolver selects an already-governed assignment for one server-owned
// selection key. Implementations must fail closed when governed release state
// exists but cannot be read.
type RuntimeResolver interface {
	Resolve(context.Context, string, string) (RuntimeAssignment, error)
	Stable(context.Context, string) (RuntimeAssignment, error)
	Invalidate()
}

func ValidateOverrides(overrides []ChannelOverride) ([]ChannelOverride, error) {
	if len(overrides) == 0 || len(overrides) > 128 {
		return nil, fmt.Errorf("runtime overrides must contain 1-128 channels")
	}
	result := slices.Clone(overrides)
	slices.SortFunc(result, func(left, right ChannelOverride) int {
		switch {
		case left.ChannelID < right.ChannelID:
			return -1
		case left.ChannelID > right.ChannelID:
			return 1
		default:
			return 0
		}
	})
	var previous int64
	for index := range result {
		item := &result[index]
		item.Admission = strings.TrimSpace(strings.ToLower(item.Admission))
		if item.Admission == "" {
			item.Admission = AdmissionInherit
		}
		if item.ChannelID <= 0 || item.ChannelID == previous || item.ExpectedRoutingEpoch <= 0 {
			return nil, fmt.Errorf("runtime override identity is invalid")
		}
		previous = item.ChannelID
		switch item.Admission {
		case AdmissionInherit, AdmissionAllow, AdmissionDeny:
		default:
			return nil, fmt.Errorf("runtime override admission is invalid")
		}
		if item.Priority != nil && (*item.Priority < 1 || *item.Priority > 10000) {
			return nil, fmt.Errorf("runtime override priority must be 1-10000")
		}
		if item.Weight != nil && (*item.Weight < 1 || *item.Weight > 1000) {
			return nil, fmt.Errorf("runtime override weight must be 1-1000")
		}
		if item.Admission == AdmissionInherit && item.Priority == nil && item.Weight == nil {
			return nil, fmt.Errorf("runtime override has no effect")
		}
	}
	return result, nil
}
