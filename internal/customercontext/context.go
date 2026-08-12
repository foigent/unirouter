// Package customercontext resolves the single customer organization owned by
// one UniRouter deployment. It deliberately keeps the deployment operator's
// identity organization separate from the customer organization.
package customercontext

import (
	"context"
	"fmt"

	"github.com/uniroute/unirouter/internal/db"
)

// State describes whether a deployment has one unambiguous customer context.
type State string

const (
	StateReady          State = "ready"
	StateNotInitialized State = "not_initialized"
	StateAmbiguous      State = "ambiguous"
	StateInactive       State = "inactive"
	StateUnavailable    State = "unavailable"
)

// Snapshot is the customer context exposed to authenticated admin requests.
// Organization fields are present only when exactly one customer root exists.
type Snapshot struct {
	State            State  `json:"state"`
	OrganizationID   int64  `json:"org_id,omitempty"`
	OrganizationName string `json:"org_name,omitempty"`
}

func (s Snapshot) Ready() bool {
	return s.State == StateReady && s.OrganizationID > 0
}

// Resolve returns the unique root customer organization without relying on
// database ordering. Multiple roots stay ambiguous even if only one is active.
func Resolve(ctx context.Context, q db.DBQuerier) (Snapshot, error) {
	if q == nil {
		return Snapshot{State: StateUnavailable}, fmt.Errorf("customer context database is unavailable")
	}

	var count int64
	var organizationID int64
	var organizationName, status string
	err := q.QueryRow(ctx, `
		SELECT COUNT(*),
		       COALESCE(MAX(id), 0),
		       COALESCE(MAX(name), ''),
		       COALESCE(MAX(status), '')
		  FROM orgs
		 WHERE parent_id IS NULL
		   AND org_type = 'customer'`).Scan(&count, &organizationID, &organizationName, &status)
	if err != nil {
		return Snapshot{State: StateUnavailable}, fmt.Errorf("resolve customer context: %w", err)
	}

	switch {
	case count == 0:
		return Snapshot{State: StateNotInitialized}, nil
	case count > 1:
		return Snapshot{State: StateAmbiguous}, nil
	case status != "active":
		return Snapshot{
			State:            StateInactive,
			OrganizationID:   organizationID,
			OrganizationName: organizationName,
		}, nil
	default:
		return Snapshot{
			State:            StateReady,
			OrganizationID:   organizationID,
			OrganizationName: organizationName,
		}, nil
	}
}
