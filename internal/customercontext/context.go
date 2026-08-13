// Package customercontext resolves the single customer organization owned by
// one UniRouter deployment. A legacy deployment with exactly one root
// organization adopts that organization as its customer context instead of
// forcing an otherwise empty second organization to be created.
package customercontext

import (
	"context"
	"fmt"

	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/modelgov"
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
// Organization fields are present only when one root is unambiguous.
type Snapshot struct {
	State            State  `json:"state"`
	OrganizationID   int64  `json:"org_id,omitempty"`
	OrganizationName string `json:"org_name,omitempty"`
}

// Baseline contains the small, idempotent customer defaults needed by the
// normal single-deployment flow. It never overwrites an existing wallet,
// project, balance, status, membership, or customer-owned setting.
type Baseline struct {
	Customer  Snapshot
	ProjectID int64
}

func (s Snapshot) Ready() bool {
	return s.State == StateReady && s.OrganizationID > 0
}

// Resolve returns the explicit root customer organization when one exists. For
// legacy/local installations that have no explicit customer row, the sole root
// organization is the customer. Multiple roots remain ambiguous; UniRouter
// never selects one by database ordering.
func Resolve(ctx context.Context, q db.DBQuerier) (Snapshot, error) {
	if q == nil {
		return Snapshot{State: StateUnavailable}, fmt.Errorf("customer context database is unavailable")
	}

	var count, organizationID int64
	var organizationName, status string
	err := q.QueryRow(ctx, `
		WITH roots AS (
			SELECT id, name, status, org_type
			  FROM orgs
			 WHERE parent_id IS NULL
		), target AS (
			SELECT id, name, status FROM roots WHERE org_type = 'customer'
			UNION ALL
			SELECT id, name, status FROM roots
			 WHERE NOT EXISTS (SELECT 1 FROM roots WHERE org_type = 'customer')
		)
		SELECT COUNT(*),
		       COALESCE(MAX(id), 0),
		       COALESCE(MAX(name), ''),
		       COALESCE(MAX(status), '')
		  FROM target`).Scan(&count, &organizationID, &organizationName, &status)
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

// EnsureBaseline creates only the defaults that old migrations and reset
// workflows may legitimately leave absent. It is safe for stable and canary
// processes to call concurrently.
func EnsureBaseline(ctx context.Context, q db.DBQuerier) (Baseline, error) {
	snapshot, err := Resolve(ctx, q)
	if err != nil || !snapshot.Ready() {
		return Baseline{Customer: snapshot}, err
	}

	tx, err := q.Begin(ctx)
	if err != nil {
		return Baseline{Customer: Snapshot{State: StateUnavailable}}, fmt.Errorf("begin customer baseline: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO wallets (org_id)
		VALUES ($1)
		ON CONFLICT (org_id) DO NOTHING`, snapshot.OrganizationID); err != nil {
		return Baseline{Customer: snapshot}, fmt.Errorf("ensure customer wallet: %w", err)
	}

	var projectID int64
	if _, err := tx.Exec(ctx, `
		INSERT INTO projects (org_id, name, slug, description, status)
		VALUES ($1, 'Default Project', 'default', 'Default project for this UniRouter deployment', 'active')
		ON CONFLICT (org_id, slug) DO NOTHING`, snapshot.OrganizationID); err != nil {
		return Baseline{Customer: snapshot}, fmt.Errorf("ensure default project: %w", err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT id FROM projects WHERE org_id=$1 AND slug='default'`,
		snapshot.OrganizationID,
	).Scan(&projectID); err != nil {
		return Baseline{Customer: snapshot}, fmt.Errorf("read default project: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role)
		SELECT $1, u.id, 'project_admin'
		  FROM users u
		 WHERE u.org_id=$2
		   AND u.status='active'
		   AND u.organization_role IN ('platform_admin','org_admin')
		ON CONFLICT (project_id, user_id) DO NOTHING`, projectID, snapshot.OrganizationID); err != nil {
		return Baseline{Customer: snapshot}, fmt.Errorf("ensure default project administrators: %w", err)
	}

	if err := modelgov.ReconcileActivatedCatalogModels(ctx, tx, snapshot.OrganizationID); err != nil {
		return Baseline{Customer: snapshot}, fmt.Errorf("reconcile activated customer models: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Baseline{Customer: snapshot}, fmt.Errorf("commit customer baseline: %w", err)
	}
	return Baseline{Customer: snapshot, ProjectID: projectID}, nil
}
