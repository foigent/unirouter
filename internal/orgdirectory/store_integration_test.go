//go:build integration

package orgdirectory

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPostgresStoreAppliesReplaySafeTenantScopedOrganizationSnapshots(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)

	orgA := insertDirectoryTestOrg(t, ctx, pool, "directory-a")
	orgB := insertDirectoryTestOrg(t, ctx, pool, "directory-b")
	_, err = store.RegisterSource(ctx, SourceRegistration{
		OrgID: orgA, SourceRef: "hcm-primary", Authority: AuthorityHCM, Connection: ConnectionConnected,
	})
	require.NoError(t, err)
	_, err = store.RegisterSource(ctx, SourceRegistration{
		OrgID: orgB, SourceRef: "hcm-primary", Authority: AuthorityHCM, Connection: ConnectionConnected,
	})
	require.NoError(t, err)

	now := time.Date(2026, 8, 2, 8, 0, 0, 123456000, time.UTC)
	full := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: orgA, SourceRef: "hcm-primary",
		Authority: AuthorityHCM, Kind: SnapshotFull, SourceRevision: "rev-1", ObservedAt: now,
		Units: []OrganizationUnitProjection{
			{
				UnitRef: "legal-a", ExternalRef: "L-1", Kind: UnitLegalEntity,
				Name: "Legal A", SourceRevision: "rev-1", Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
			},
			{
				UnitRef: "dept-a", ExternalRef: "D-1", ParentUnitRef: "legal-a", Kind: UnitDepartment,
				Name: "Department A", SourceRevision: "rev-1", Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
			},
		},
	}
	applied, err := store.ApplyOrganizationSnapshot(ctx, full)
	require.NoError(t, err)
	require.Equal(t, ApplyResultApplied, applied.Result)
	require.Equal(t, 2, applied.UnitCount)

	replay, err := store.ApplyOrganizationSnapshot(ctx, full)
	require.NoError(t, err)
	require.Equal(t, ApplyResultDuplicate, replay.Result)
	require.Equal(t, applied.SnapshotRef, replay.SnapshotRef)

	conflictingReplay := full
	conflictingReplay.Units = append([]OrganizationUnitProjection(nil), full.Units...)
	conflictingReplay.Units[1].Name = "Changed bytes under the same revision"
	_, err = store.ApplyOrganizationSnapshot(ctx, conflictingReplay)
	require.ErrorIs(t, err, ErrConflict)

	states, err := store.ListSources(ctx, orgA, now.Add(time.Minute), 15*time.Minute)
	require.NoError(t, err)
	require.Len(t, states, 1)
	require.Equal(t, ReconciliationConflict, states[0].Health.Reconciliation)
	allowed, reason := MayGrantNewPrivilege(states[0].Health)
	require.False(t, allowed)
	require.Equal(t, "directory_not_reconciled", reason)

	delta := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: orgA, SourceRef: "hcm-primary",
		Authority: AuthorityHCM, Kind: SnapshotDelta, PreviousRevision: "rev-1",
		SourceRevision: "rev-2", ObservedAt: now.Add(time.Minute),
		Units: []OrganizationUnitProjection{
			{UnitRef: "dept-a", SourceRevision: "rev-2", Tombstone: true},
			{
				UnitRef: "team-b", ExternalRef: "T-2", ParentUnitRef: "legal-a", Kind: UnitTeam,
				Name: "Team B", SourceRevision: "rev-2", Effective: EffectiveWindow{ValidFrom: now},
			},
		},
	}
	applied, err = store.ApplyOrganizationSnapshot(ctx, delta)
	require.NoError(t, err)
	require.Equal(t, ApplyResultApplied, applied.Result)
	require.Equal(t, 2, applied.UnitCount)

	tree, err := store.ListOrganizationTree(ctx, orgA, "hcm-primary", now.Add(2*time.Minute))
	require.NoError(t, err)
	require.Len(t, tree, 2)
	require.ElementsMatch(t, []string{"legal-a", "team-b"}, []string{tree[0].UnitRef, tree[1].UnitRef})

	otherTree, err := store.ListOrganizationTree(ctx, orgB, "hcm-primary", now.Add(2*time.Minute))
	require.NoError(t, err)
	require.Empty(t, otherTree)

	stale := delta
	stale.SourceRevision = "rev-3"
	stale.PreviousRevision = "rev-0"
	for i := range stale.Units {
		stale.Units[i].SourceRevision = "rev-3"
	}
	_, err = store.ApplyOrganizationSnapshot(ctx, stale)
	require.ErrorIs(t, err, ErrStaleRevision)
}

func TestPostgresStoreRefusesCrossAuthoritySourceMutation(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertDirectoryTestOrg(t, ctx, pool, "directory-authority")

	_, err = store.RegisterSource(ctx, SourceRegistration{
		OrgID: orgID, SourceRef: "workforce", Authority: AuthorityHCM, Connection: ConnectionUnconfigured,
	})
	require.NoError(t, err)
	_, err = store.RegisterSource(ctx, SourceRegistration{
		OrgID: orgID, SourceRef: "workforce", Authority: AuthorityIDP, Connection: ConnectionConnected,
	})
	require.ErrorIs(t, err, ErrConflict)
}

func insertDirectoryTestOrg(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type)
		VALUES ($1, 'active', 'standard') RETURNING id`, name,
	).Scan(&orgID))
	return orgID
}
