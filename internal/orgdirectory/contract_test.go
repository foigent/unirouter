package orgdirectory

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateSnapshotSeparatesHCMAuthorityFromIDPAuthority(t *testing.T) {
	now := time.Date(2026, 8, 1, 8, 0, 0, 123456000, time.UTC)
	localUserID := int64(7)
	hcm := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: 3, SourceRef: "hcm-primary",
		Authority: AuthorityHCM, Kind: SnapshotFull, SourceRevision: "rev-17", ObservedAt: now,
		Units: []OrganizationUnitProjection{{
			UnitRef: "dept-finance", ExternalRef: "D-17", Kind: UnitDepartment,
			Name: "Finance", SourceRevision: "rev-17", Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
		}},
		People: []PersonProjection{{
			PersonRef: "person-1", ExternalRef: "E-1", EmploymentStatus: EmploymentActive,
			PrimaryUnitRef: "dept-finance", CostCenterRef: "cc-100", SourceRevision: "rev-17",
			Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
		}},
	}
	require.NoError(t, ValidateSnapshot(hcm))

	idp := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: 3, SourceRef: "idp-primary",
		Authority: AuthorityIDP, Kind: SnapshotDelta, PreviousRevision: "delta-7", SourceRevision: "delta-8", ObservedAt: now,
		Accounts: []AccountBindingProjection{{
			AccountRef: "account-1", ExternalSubjectRef: "oidc-sub-1", PersonRef: "person-1",
			LocalUserID: &localUserID, AuthenticationState: AuthenticationActive,
			SourceRevision: "delta-8", Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)},
		}},
	}
	require.NoError(t, ValidateSnapshot(idp))

	idp.People = hcm.People
	err := ValidateSnapshot(idp)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalid))
	require.ErrorContains(t, err, "idp_cannot_author_hcm_fields")
}

func TestEvaluateHealthUsesOrthogonalFailClosedAxes(t *testing.T) {
	now := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	observation := SourceObservation{
		ContractVersion: ContractVersion, OrgID: 3, SourceRef: "hcm-primary", Authority: AuthorityHCM,
		Connection: ConnectionConnected, SourceRevision: "rev-17",
		ObservedAt: now.Add(-5 * time.Minute), ReconciledAt: now.Add(-4 * time.Minute),
	}
	health, err := EvaluateHealth(observation, now, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, Health{
		Connection: ConnectionConnected, Freshness: FreshnessFresh,
		Reconciliation: ReconciliationReconciled, Coverage: CoverageComplete,
	}, health)
	allowed, reason := MayGrantNewPrivilege(health)
	require.True(t, allowed)
	require.Equal(t, "directory_evidence_current", reason)

	observation.ObservedAt = now.Add(-20 * time.Minute)
	health, err = EvaluateHealth(observation, now, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, FreshnessStale, health.Freshness)
	allowed, reason = MayGrantNewPrivilege(health)
	require.False(t, allowed)
	require.Equal(t, "directory_not_fresh", reason)

	observation.ObservedAt = now.Add(-5 * time.Minute)
	observation.Gaps = []Gap{{Code: "missing_delta", Provenance: "reconciliation"}}
	health, err = EvaluateHealth(observation, now, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, CoverageGapped, health.Coverage)
	allowed, reason = MayGrantNewPrivilege(health)
	require.False(t, allowed)
	require.Equal(t, "directory_coverage_incomplete", reason)
}

func TestResolveAttributionNeverTreatsAKeyOwnerAsTheCaller(t *testing.T) {
	now := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	input := AttributionInput{
		RequestRef: "request-1", ProjectRef: "project-7", InitiatingPrincipalRef: "api-key-9",
		SharedCredential: true, EffectiveAt: now,
	}
	result, err := ResolveAttribution(input, nil)
	require.NoError(t, err)
	require.Equal(t, AttributionShared, result.PrincipalAttribution)
	require.Equal(t, AttributionUnallocated, result.CostAttribution)
	require.Empty(t, result.PersonRef)
	require.Empty(t, result.CostCenterRef)
}

func TestResolveAttributionUsesEffectiveDatedSourceRevisionWithoutRewritingHistory(t *testing.T) {
	beforeMove := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	moveAt := beforeMove.Add(time.Hour)
	assignments := []EffectiveAssignment{
		{
			PersonRef: "person-1", UnitRef: "dept-a", CostCenterRef: "cc-a",
			SourceRef: "hcm-primary", SourceRevision: "rev-17",
			Effective: EffectiveWindow{ValidFrom: beforeMove.Add(-time.Hour), ValidTo: &moveAt},
		},
		{
			PersonRef: "person-1", UnitRef: "dept-b", CostCenterRef: "cc-b",
			SourceRef: "hcm-primary", SourceRevision: "rev-18",
			Effective: EffectiveWindow{ValidFrom: moveAt},
		},
	}
	base := AttributionInput{
		RequestRef: "request-1", ProjectRef: "project-7", InitiatingPrincipalRef: "oidc-account-1",
		ProvenPersonRef: "person-1", EffectiveAt: beforeMove,
	}
	oldSnapshot, err := ResolveAttribution(base, assignments)
	require.NoError(t, err)
	require.Equal(t, "dept-a", oldSnapshot.UnitRef)
	require.Equal(t, "cc-a", oldSnapshot.CostCenterRef)
	require.Equal(t, "rev-17", oldSnapshot.SourceRevision)

	base.RequestRef = "request-2"
	base.EffectiveAt = moveAt
	newSnapshot, err := ResolveAttribution(base, assignments)
	require.NoError(t, err)
	require.Equal(t, "dept-b", newSnapshot.UnitRef)
	require.Equal(t, "cc-b", newSnapshot.CostCenterRef)
	require.Equal(t, "rev-18", newSnapshot.SourceRevision)
}

func TestResolveAttributionExposesOverlappingAssignmentsAsConflict(t *testing.T) {
	now := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	assignments := []EffectiveAssignment{
		{PersonRef: "person-1", UnitRef: "dept-a", SourceRef: "hcm", SourceRevision: "1", Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)}},
		{PersonRef: "person-1", UnitRef: "dept-b", SourceRef: "hcm", SourceRevision: "2", Effective: EffectiveWindow{ValidFrom: now.Add(-time.Hour)}},
	}
	result, err := ResolveAttribution(AttributionInput{
		RequestRef: "request-1", ProjectRef: "project-7", InitiatingPrincipalRef: "account-1",
		ProvenPersonRef: "person-1", EffectiveAt: now,
	}, assignments)
	require.NoError(t, err)
	require.Equal(t, AttributionDirect, result.PrincipalAttribution)
	require.Equal(t, AttributionConflict, result.CostAttribution)
	require.Empty(t, result.UnitRef)
}

func TestSnapshotRejectsSubMicrosecondTimestamps(t *testing.T) {
	snapshot := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: 3, SourceRef: "hcm", Authority: AuthorityHCM,
		Kind: SnapshotFull, SourceRevision: "1",
		ObservedAt: time.Date(2026, 8, 1, 8, 0, 0, 123456789, time.UTC),
	}
	err := ValidateSnapshot(snapshot)
	require.ErrorContains(t, err, "observed_at")
}

func TestHealthSurfacesConflictBeforeFirstSuccessfulObservation(t *testing.T) {
	now := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	health, err := EvaluateHealth(SourceObservation{
		ContractVersion: ContractVersion, OrgID: 3, SourceRef: "idp",
		Authority: AuthorityIDP, Connection: ConnectionConnected, HasConflict: true,
	}, now, time.Hour)
	require.NoError(t, err)
	require.Equal(t, ReconciliationConflict, health.Reconciliation)
	require.Equal(t, FreshnessUnknown, health.Freshness)
	require.Equal(t, CoverageUnknown, health.Coverage)
}

func TestDeltaRequiresExactPreviousRevisionAndExplicitTombstoneShape(t *testing.T) {
	now := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	snapshot := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: 3, SourceRef: "hcm", Authority: AuthorityHCM,
		Kind: SnapshotDelta, SourceRevision: "rev-18", ObservedAt: now,
		Units: []OrganizationUnitProjection{{UnitRef: "dept-old", SourceRevision: "rev-18", Tombstone: true}},
	}
	require.ErrorContains(t, ValidateSnapshot(snapshot), "delta_previous_revision")

	snapshot.PreviousRevision = "rev-17"
	require.NoError(t, ValidateSnapshot(snapshot))
	snapshot.Units[0].Name = "fabricated deleted department"
	require.ErrorContains(t, ValidateSnapshot(snapshot), "organization_unit_tombstone")
}

func TestPeopleAndAccountTombstonesAreDeltaOnlyAndRevisionBound(t *testing.T) {
	now := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	personDelta := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: 3, SourceRef: "hcm", Authority: AuthorityHCM,
		Kind: SnapshotDelta, PreviousRevision: "rev-1", SourceRevision: "rev-2", ObservedAt: now,
		People: []PersonProjection{{PersonRef: "person-1", SourceRevision: "rev-2", Tombstone: true}},
	}
	require.NoError(t, ValidateSnapshot(personDelta))
	personDelta.People[0].EmploymentStatus = EmploymentTerminated
	require.ErrorContains(t, ValidateSnapshot(personDelta), "person_tombstone")

	userID := int64(7)
	accountDelta := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: 3, SourceRef: "idp", Authority: AuthorityIDP,
		Kind: SnapshotDelta, PreviousRevision: "rev-1", SourceRevision: "rev-2", ObservedAt: now,
		Accounts: []AccountBindingProjection{{AccountRef: "account-1", SourceRevision: "rev-2", Tombstone: true}},
	}
	require.NoError(t, ValidateSnapshot(accountDelta))
	accountDelta.Accounts[0] = AccountBindingProjection{
		AccountRef: "account-1", ExternalSubjectRef: "subject-1", LocalUserID: &userID,
		AuthenticationState: AuthenticationActive, SourceRevision: "rev-2",
		Effective: EffectiveWindow{ValidFrom: now},
	}
	require.ErrorContains(t, ValidateSnapshot(accountDelta), "local_user_person_binding")
	accountDelta.Accounts[0].PersonRef = "person-1"
	require.NoError(t, ValidateSnapshot(accountDelta))
	accountDelta.Accounts[0].SourceRevision = "rev-stale"
	require.ErrorContains(t, ValidateSnapshot(accountDelta), "account_identity")
}

func TestSnapshotIdentityIsOrderIndependentButRevisionAndBytesBound(t *testing.T) {
	now := time.Date(2026, 8, 1, 8, 0, 0, 123456000, time.UTC)
	unitA := OrganizationUnitProjection{
		UnitRef: "legal-a", ExternalRef: "L-1", Kind: UnitLegalEntity,
		Name: "Legal A", SourceRevision: "rev-1", Effective: EffectiveWindow{ValidFrom: now},
	}
	unitB := OrganizationUnitProjection{
		UnitRef: "dept-b", ExternalRef: "D-2", ParentUnitRef: "legal-a", Kind: UnitDepartment,
		Name: "Department B", SourceRevision: "rev-1", Effective: EffectiveWindow{ValidFrom: now},
	}
	base := ProjectionSnapshot{
		ContractVersion: ContractVersion, OrgID: 3, SourceRef: "hcm", Authority: AuthorityHCM,
		Kind: SnapshotFull, SourceRevision: "rev-1", ObservedAt: now,
		Gaps:  []Gap{{Code: "manager_missing", Provenance: "source"}},
		Units: []OrganizationUnitProjection{unitB, unitA},
	}
	first, err := SnapshotIdentity(base)
	require.NoError(t, err)
	base.Units = []OrganizationUnitProjection{unitA, unitB}
	second, err := SnapshotIdentity(base)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Regexp(t, `^sha256:[a-f0-9]{64}$`, first)

	base.Units[1].Name = "Department C"
	changed, err := SnapshotIdentity(base)
	require.NoError(t, err)
	require.NotEqual(t, first, changed)
}

func TestMaterializedHierarchyRejectsMissingParentAndCycle(t *testing.T) {
	now := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	unit := func(ref, parent string) OrganizationUnitProjection {
		return OrganizationUnitProjection{
			UnitRef: ref, ExternalRef: "external-" + ref, ParentUnitRef: parent,
			Kind: UnitDepartment, Name: ref, SourceRevision: "rev-1",
			Effective: EffectiveWindow{ValidFrom: now},
		}
	}
	require.ErrorIs(t, validateMaterializedHierarchy(map[string]OrganizationUnitProjection{
		"dept-a": unit("dept-a", "missing"),
	}), ErrConflict)
	require.ErrorIs(t, validateMaterializedHierarchy(map[string]OrganizationUnitProjection{
		"dept-a": unit("dept-a", "dept-b"),
		"dept-b": unit("dept-b", "dept-a"),
	}), ErrConflict)
}
