// Package customercontext resolves the single customer organization owned by
// one UniRouter deployment. A legacy deployment with exactly one root
// organization adopts that organization as its customer context instead of
// forcing an otherwise empty second organization to be created.
package customercontext

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/principallock"
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

type customerQueryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s Snapshot) Ready() bool {
	return s.State == StateReady && s.OrganizationID > 0
}

// Resolve returns the explicit root customer organization when one exists. For
// legacy/local installations that have no explicit customer row, the sole root
// organization is the customer. Multiple roots remain ambiguous; UniRouter
// never selects one by database ordering.
func Resolve(ctx context.Context, q customerQueryRower) (Snapshot, error) {
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

	// Project and user mutations acquire the principal lane before project rows
	// and the commercial-limit advisory. Snapshot the current customer users,
	// lock only those identities in the same deterministic lane, and use that
	// fixed set below. A concurrently created user is intentionally reconciled by
	// the next baseline pass instead of being discovered after project locks.
	var baselineUserIDs []int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id ORDER BY id),ARRAY[]::bigint[])
		FROM users
		WHERE org_id=$1
		  AND (organization_role='platform_admin'
		       OR (organization_role='org_admin' AND status='active'))`, snapshot.OrganizationID).Scan(&baselineUserIDs); err != nil {
		return Baseline{Customer: snapshot}, fmt.Errorf("read baseline user inventory: %w", err)
	}
	if err := principallock.LockUsers(ctx, tx, baselineUserIDs...); err != nil {
		return Baseline{Customer: snapshot}, fmt.Errorf("lock baseline user inventory: %w", err)
	}
	confirmed, err := Resolve(ctx, tx)
	if err != nil || confirmed.State != snapshot.State ||
		confirmed.OrganizationID != snapshot.OrganizationID ||
		confirmed.OrganizationName != snapshot.OrganizationName {
		return Baseline{Customer: confirmed}, fmt.Errorf("customer context changed while ensuring baseline")
	}
	var lockedOrganizationName, lockedOrganizationStatus string
	if err := tx.QueryRow(ctx, `SELECT name,status FROM orgs WHERE id=$1 FOR SHARE`, snapshot.OrganizationID).Scan(
		&lockedOrganizationName, &lockedOrganizationStatus,
	); err != nil || lockedOrganizationName != snapshot.OrganizationName || lockedOrganizationStatus != "active" {
		return Baseline{Customer: snapshot}, fmt.Errorf("customer organization changed while ensuring baseline")
	}

	// A BEFORE INSERT commercial-limit trigger runs before ON CONFLICT can
	// discard a duplicate. Lock and reuse an existing row without entering the
	// advisory domain. Only the absent branch takes the advisory and rechecks,
	// which serializes stable/canary starts without reversing project-writer
	// row-lock order.
	var projectID int64
	err = tx.QueryRow(ctx,
		`SELECT id FROM projects WHERE org_id=$1 AND slug='default' FOR SHARE`,
		snapshot.OrganizationID,
	).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, lockErr := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(
			hashtextextended('unirouter.commercial_limit.active_projects',0))`); lockErr != nil {
			return Baseline{Customer: snapshot}, fmt.Errorf("lock default project authority: %w", lockErr)
		}
		err = tx.QueryRow(ctx,
			`SELECT id FROM projects WHERE org_id=$1 AND slug='default' FOR SHARE`,
			snapshot.OrganizationID,
		).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `
				INSERT INTO projects (org_id, name, slug, description, status)
				VALUES ($1, 'Default Project', 'default', 'Default project for this UniRouter deployment', 'active')
				RETURNING id`, snapshot.OrganizationID).Scan(&projectID)
		}
	}
	if err != nil {
		return Baseline{Customer: snapshot}, fmt.Errorf("ensure default project: %w", err)
	}

	// Fresh bootstrap and customer setup order their writes user -> project ->
	// wallet. Preserve that order, and avoid an unnecessary INSERT statement on
	// every restart because the micro-ledger evidence trigger is statement-level.
	var walletExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wallets WHERE org_id=$1)`, snapshot.OrganizationID).Scan(&walletExists); err != nil {
		return Baseline{Customer: snapshot}, fmt.Errorf("read customer wallet: %w", err)
	}
	if !walletExists {
		if _, err := tx.Exec(ctx, `
			INSERT INTO wallets (org_id)
			VALUES ($1)
			ON CONFLICT (org_id) DO NOTHING`, snapshot.OrganizationID); err != nil {
			return Baseline{Customer: snapshot}, fmt.Errorf("ensure customer wallet: %w", err)
		}
	}

	// Deployment operators have organization-wide break-glass authority and are
	// not customer project identities. Remove every legacy project projection so
	// it cannot leak into project workspaces, access reviews or membership APIs.
	if _, err := tx.Exec(ctx, `
		DELETE FROM project_members pm
		 USING projects p, users u
		 WHERE pm.project_id=p.id
		   AND p.org_id=$1
		   AND pm.user_id=u.id
		   AND u.id=ANY($2::bigint[])
		   AND u.org_id=p.org_id
		   AND u.organization_role='platform_admin'`, snapshot.OrganizationID, baselineUserIDs); err != nil {
		return Baseline{Customer: snapshot}, fmt.Errorf("remove deployment operators from customer projects: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role)
		SELECT $1, u.id, 'project_admin'
		  FROM users u
		 WHERE u.id=ANY($2::bigint[])
		   AND u.org_id=$3
		   AND u.status='active'
		   AND u.organization_role='org_admin'
		ON CONFLICT (project_id, user_id) DO NOTHING`, projectID, baselineUserIDs, snapshot.OrganizationID); err != nil {
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
