//go:build integration

package orgdirectory

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestProjectionStoreIDPTerminalStateRevokesLocalAccessWithoutRestoringIt(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertDirectoryTestOrg(t, ctx, pool, "directory-jml-idp")
	now := time.Date(2026, 8, 2, 9, 0, 0, 123456000, time.UTC)

	registerConnectedDirectorySource(t, ctx, store, orgID, "hcm-primary", AuthorityHCM)
	registerConnectedDirectorySource(t, ctx, store, orgID, "idp-primary", AuthorityIDP)
	applyHCMFixture(t, ctx, store, orgID, now)
	userID := insertDirectoryLocalUser(t, ctx, pool, orgID, "directory-user", "org_viewer")
	insertDirectorySessionAndPersonalKey(t, ctx, pool, orgID, userID, now)

	idpFull := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: orgID, SourceRef: "idp-primary",
		Authority: AuthorityIDP, Kind: SnapshotFull, SourceRevision: "idp-1", ObservedAt: now.Add(time.Minute),
		Accounts: []AccountBindingProjection{{
			AccountRef: "account-1", ExternalSubjectRef: "subject-1", PersonRef: "person-1",
			LocalUserID: &userID, AuthenticationState: AuthenticationActive, SourceRevision: "idp-1",
			Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
		}},
	}
	result, err := store.ApplyProjectionSnapshot(ctx, idpFull)
	require.NoError(t, err)
	require.Equal(t, 1, result.AccountCount)
	require.Zero(t, result.IAMActionCount)

	disabled := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: orgID, SourceRef: "idp-primary",
		Authority: AuthorityIDP, Kind: SnapshotDelta, PreviousRevision: "idp-1",
		SourceRevision: "idp-2", ObservedAt: now.Add(2 * time.Minute),
		Accounts: []AccountBindingProjection{{
			AccountRef: "account-1", ExternalSubjectRef: "subject-1", PersonRef: "person-1",
			LocalUserID: &userID, AuthenticationState: AuthenticationDisabled, SourceRevision: "idp-2",
			Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
		}},
	}
	result, err = store.ApplyProjectionSnapshot(ctx, disabled)
	require.NoError(t, err)
	require.Equal(t, 1, result.IAMActionCount)

	var userStatus, keyStatus string
	var authEpoch int64
	var revokedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,auth_epoch FROM users WHERE id=$1`, userID).Scan(&userStatus, &authEpoch))
	require.Equal(t, "disabled", userStatus)
	require.Equal(t, int64(1), authEpoch)
	require.NoError(t, pool.QueryRow(ctx, `SELECT revoked_at FROM admin_sessions WHERE user_id=$1`, userID).Scan(&revokedAt))
	require.NotNil(t, revokedAt)
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE owner_user_id=$1 AND key_type='personal'`, userID).Scan(&keyStatus))
	require.Equal(t, "disabled", keyStatus)

	replay, err := store.ApplyProjectionSnapshot(ctx, disabled)
	require.NoError(t, err)
	require.Equal(t, ApplyResultDuplicate, replay.Result)
	require.Equal(t, 1, replay.IAMActionCount)

	reactivated := disabled
	reactivated.PreviousRevision = "idp-2"
	reactivated.SourceRevision = "idp-3"
	reactivated.ObservedAt = now.Add(3 * time.Minute)
	reactivated.Accounts = append([]AccountBindingProjection(nil), disabled.Accounts...)
	reactivated.Accounts[0].SourceRevision = "idp-3"
	reactivated.Accounts[0].AuthenticationState = AuthenticationActive
	result, err = store.ApplyProjectionSnapshot(ctx, reactivated)
	require.NoError(t, err)
	require.Equal(t, 1, result.IAMActionCount)
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,auth_epoch FROM users WHERE id=$1`, userID).Scan(&userStatus, &authEpoch))
	require.Equal(t, "disabled", userStatus)
	require.Equal(t, int64(1), authEpoch)

	actions, err := store.ListIAMActions(ctx, orgID, 20)
	require.NoError(t, err)
	require.Len(t, actions, 2)
	require.Equal(t, IAMActionReviewRequired, actions[0].Action)
	require.Equal(t, "reactivation_never_restores_local_access", actions[0].Reason)
	require.Equal(t, IAMResultApplied, actions[1].Result)
}

func TestProjectionStoreDistinguishesFullOmissionFromExplicitTombstoneAndGaps(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertDirectoryTestOrg(t, ctx, pool, "directory-jml-omission")
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	registerConnectedDirectorySource(t, ctx, store, orgID, "hcm-primary", AuthorityHCM)
	registerConnectedDirectorySource(t, ctx, store, orgID, "idp-primary", AuthorityIDP)
	applyHCMFixture(t, ctx, store, orgID, now)
	userID := insertDirectoryLocalUser(t, ctx, pool, orgID, "omission-user", "org_viewer")

	full := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: orgID, SourceRef: "idp-primary", Authority: AuthorityIDP,
		Kind: SnapshotFull, SourceRevision: "idp-1", ObservedAt: now.Add(time.Minute),
		Accounts: []AccountBindingProjection{{
			AccountRef: "account-1", ExternalSubjectRef: "subject-1", PersonRef: "person-1", LocalUserID: &userID,
			AuthenticationState: AuthenticationActive, SourceRevision: "idp-1", Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
		}},
	}
	_, err = store.ApplyProjectionSnapshot(ctx, full)
	require.NoError(t, err)

	omission := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: orgID, SourceRef: "idp-primary", Authority: AuthorityIDP,
		Kind: SnapshotFull, PreviousRevision: "idp-1", SourceRevision: "idp-2", ObservedAt: now.Add(2 * time.Minute),
	}
	result, err := store.ApplyProjectionSnapshot(ctx, omission)
	require.NoError(t, err)
	require.Zero(t, result.AccountCount)
	require.Zero(t, result.IAMActionCount)
	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM users WHERE id=$1`, userID).Scan(&status))
	require.Equal(t, "active", status)

	// Restore the binding, then prove that an explicit tombstone with a capture
	// gap records review evidence but cannot mutate IAM.
	restore := full
	restore.PreviousRevision = "idp-2"
	restore.SourceRevision = "idp-3"
	restore.ObservedAt = now.Add(3 * time.Minute)
	restore.Accounts = append([]AccountBindingProjection(nil), full.Accounts...)
	restore.Accounts[0].SourceRevision = "idp-3"
	_, err = store.ApplyProjectionSnapshot(ctx, restore)
	require.NoError(t, err)

	tombstone := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: orgID, SourceRef: "idp-primary", Authority: AuthorityIDP,
		Kind: SnapshotDelta, PreviousRevision: "idp-3", SourceRevision: "idp-4", ObservedAt: now.Add(4 * time.Minute),
		Gaps:     []Gap{{Code: "partial_page", Provenance: "source"}},
		Accounts: []AccountBindingProjection{{AccountRef: "account-1", SourceRevision: "idp-4", Tombstone: true}},
	}
	result, err = store.ApplyProjectionSnapshot(ctx, tombstone)
	require.NoError(t, err)
	require.Equal(t, 1, result.IAMActionCount)
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM users WHERE id=$1`, userID).Scan(&status))
	require.Equal(t, "active", status)
	actions, err := store.ListIAMActions(ctx, orgID, 10)
	require.NoError(t, err)
	require.Equal(t, IAMActionReviewRequired, actions[0].Action)
	require.Equal(t, "terminal_state_with_capture_gap", actions[0].Reason)
}

func TestProjectionStoreRejectsCrossTenantAndAmbiguousBindings(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgA := insertDirectoryTestOrg(t, ctx, pool, "directory-binding-a")
	orgB := insertDirectoryTestOrg(t, ctx, pool, "directory-binding-b")
	now := time.Date(2026, 8, 2, 11, 0, 0, 0, time.UTC)
	registerConnectedDirectorySource(t, ctx, store, orgA, "hcm-primary", AuthorityHCM)
	registerConnectedDirectorySource(t, ctx, store, orgA, "idp-primary", AuthorityIDP)
	applyHCMFixture(t, ctx, store, orgA, now)
	foreignUser := insertDirectoryLocalUser(t, ctx, pool, orgB, "foreign-user", "org_viewer")

	snapshot := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: orgA, SourceRef: "idp-primary", Authority: AuthorityIDP,
		Kind: SnapshotFull, SourceRevision: "idp-1", ObservedAt: now.Add(time.Minute),
		Accounts: []AccountBindingProjection{{
			AccountRef: "account-foreign", ExternalSubjectRef: "subject-foreign", PersonRef: "person-1",
			LocalUserID: &foreignUser, AuthenticationState: AuthenticationActive, SourceRevision: "idp-1",
			Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
		}},
	}
	_, err = store.ApplyProjectionSnapshot(ctx, snapshot)
	require.ErrorIs(t, err, ErrConflict)

	states, err := store.ListSources(ctx, orgA, now.Add(2*time.Minute), time.Hour)
	require.NoError(t, err)
	var idpState *SourceState
	for i := range states {
		if states[i].Observation.SourceRef == "idp-primary" {
			idpState = &states[i]
		}
	}
	require.NotNil(t, idpState)
	require.Equal(t, ReconciliationConflict, idpState.Health.Reconciliation)
}

func TestProjectionStoreNeverDisablesTheLastPlatformAdministrator(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertDirectoryTestOrg(t, ctx, pool, "directory-last-platform-admin")
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	registerConnectedDirectorySource(t, ctx, store, orgID, "hcm-primary", AuthorityHCM)
	registerConnectedDirectorySource(t, ctx, store, orgID, "idp-primary", AuthorityIDP)
	applyHCMFixture(t, ctx, store, orgID, now)
	userID := insertDirectoryLocalUser(t, ctx, pool, orgID, "last-platform-admin", "platform_admin")
	_, err = store.ApplyProjectionSnapshot(ctx, ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: orgID, SourceRef: "idp-primary", Authority: AuthorityIDP,
		Kind: SnapshotFull, SourceRevision: "idp-1", ObservedAt: now.Add(time.Minute),
		Accounts: []AccountBindingProjection{{
			AccountRef: "admin-account", ExternalSubjectRef: "admin-subject", PersonRef: "person-1",
			LocalUserID: &userID, AuthenticationState: AuthenticationActive, SourceRevision: "idp-1",
			Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
		}},
	})
	require.NoError(t, err)

	result, err := store.ApplyProjectionSnapshot(ctx, ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: orgID, SourceRef: "hcm-primary", Authority: AuthorityHCM,
		Kind: SnapshotDelta, PreviousRevision: "hcm-1", SourceRevision: "hcm-2", ObservedAt: now.Add(2 * time.Minute),
		People: []PersonProjection{{
			PersonRef: "person-1", ExternalRef: "person-external-1", EmploymentStatus: EmploymentTerminated,
			PrimaryUnitRef: "dept-1", CostCenterRef: "cc-1", SourceRevision: "hcm-2",
			Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.IAMActionCount)
	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM users WHERE id=$1`, userID).Scan(&status))
	require.Equal(t, "active", status)
	actions, err := store.ListIAMActions(ctx, orgID, 10)
	require.NoError(t, err)
	require.Equal(t, IAMActionReviewRequired, actions[0].Action)
	require.Equal(t, IAMResultConflict, actions[0].Result)
	require.Equal(t, "hcm_employment_terminated", actions[0].Reason)
}

func registerConnectedDirectorySource(t *testing.T, ctx context.Context, store *PostgresStore, orgID int64, sourceRef, authority string) {
	t.Helper()
	_, err := store.RegisterSource(ctx, SourceRegistration{
		OrgID: orgID, SourceRef: sourceRef, Authority: authority, Connection: ConnectionConnected,
	})
	require.NoError(t, err)
}

func applyHCMFixture(t *testing.T, ctx context.Context, store *PostgresStore, orgID int64, now time.Time) {
	t.Helper()
	_, err := store.ApplyProjectionSnapshot(ctx, ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: orgID, SourceRef: "hcm-primary", Authority: AuthorityHCM,
		Kind: SnapshotFull, SourceRevision: "hcm-1", ObservedAt: now,
		Units: []OrganizationUnitProjection{{
			UnitRef: "dept-1", ExternalRef: "department-external-1", Kind: UnitDepartment,
			Name: "Department One", SourceRevision: "hcm-1", Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
		}},
		People: []PersonProjection{{
			PersonRef: "person-1", ExternalRef: "person-external-1", EmploymentStatus: EmploymentActive,
			PrimaryUnitRef: "dept-1", CostCenterRef: "cc-1", SourceRevision: "hcm-1",
			Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
		}},
	})
	require.NoError(t, err)
}

func insertDirectoryLocalUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID int64, username, role string) int64 {
	t.Helper()
	var userID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status)
		VALUES ($1,$2,'test-hash',$3,'active') RETURNING id`, orgID, username, role).Scan(&userID))
	return userID
}

func insertDirectorySessionAndPersonalKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, userID int64, now time.Time) {
	t.Helper()
	_ = now
	_, err := pool.Exec(ctx, `
		INSERT INTO admin_sessions (user_id,sid_hash,issued_at,expires_at)
		VALUES ($1,$2,NOW() - INTERVAL '1 minute',NOW() + INTERVAL '1 hour')`,
		userID, bytes.Repeat([]byte{0x4a}, 32))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO api_keys (
		  org_id,owner_user_id,key_type,name,key_prefix,key_hash,status
		) VALUES ($1,$2,'personal','directory-personal','dir_personal','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','active')`, orgID, userID)
	require.NoError(t, err)
}
