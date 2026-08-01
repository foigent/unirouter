// Package orgdirectory defines the source-neutral, product-local contract for
// enterprise organization projections. It does not connect to an HCM or IdP,
// accept credentials, or run on the model-request path.
package orgdirectory

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ContractVersion = "unirouter.enterprise-directory-projection.v1"

	AuthorityHCM = "hcm"
	AuthorityIDP = "idp"

	SnapshotFull  = "full"
	SnapshotDelta = "delta"

	ConnectionUnconfigured = "unconfigured"
	ConnectionConnected    = "connected"
	ConnectionDisabled     = "disabled"
	ConnectionError        = "error"

	FreshnessUnknown = "unknown"
	FreshnessFresh   = "fresh"
	FreshnessStale   = "stale"

	ReconciliationNever      = "never"
	ReconciliationPending    = "pending"
	ReconciliationReconciled = "reconciled"
	ReconciliationConflict   = "conflict"

	CoverageUnknown  = "unknown"
	CoverageComplete = "complete"
	CoverageGapped   = "gapped"

	UnitLegalEntity = "legal_entity"
	UnitBusiness    = "business_unit"
	UnitDepartment  = "department"
	UnitTeam        = "team"

	EmploymentActive     = "active"
	EmploymentLeave      = "leave"
	EmploymentTerminated = "terminated"

	AuthenticationActive   = "active"
	AuthenticationDisabled = "disabled"

	AttributionDirect      = "direct"
	AttributionShared      = "shared"
	AttributionUnallocated = "unallocated"
	AttributionConflict    = "conflict"
)

var ErrInvalid = errors.New("orgdirectory: invalid projection contract")

// Gap records a source-observed or reconciliation-derived coverage gap. It is
// product-local projection evidence, not a second definition of the shared
// Event Connector protocol.
type Gap struct {
	Code       string `json:"code"`
	Provenance string `json:"provenance"`
	Detail     string `json:"detail,omitempty"`
}

// SourceObservation contains only non-secret source and synchronization
// metadata. Credentials belong to a separately selected connector adapter.
type SourceObservation struct {
	ContractVersion string    `json:"contract_version"`
	OrgID           int64     `json:"org_id"`
	SourceRef       string    `json:"source_ref"`
	Authority       string    `json:"authority"`
	Connection      string    `json:"connection"`
	SourceRevision  string    `json:"source_revision,omitempty"`
	ObservedAt      time.Time `json:"observed_at,omitempty"`
	ReconciledAt    time.Time `json:"reconciled_at,omitempty"`
	HasConflict     bool      `json:"has_conflict"`
	Gaps            []Gap     `json:"gaps"`
}

// Health keeps independent synchronization axes instead of collapsing all
// conditions into one misleading healthy/error status.
type Health struct {
	Connection     string `json:"connection"`
	Freshness      string `json:"freshness"`
	Reconciliation string `json:"reconciliation"`
	Coverage       string `json:"coverage"`
}

type EffectiveWindow struct {
	ValidFrom time.Time  `json:"valid_from"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
}

type OrganizationUnitProjection struct {
	UnitRef        string          `json:"unit_ref"`
	ExternalRef    string          `json:"external_ref"`
	ParentUnitRef  string          `json:"parent_unit_ref,omitempty"`
	Kind           string          `json:"kind"`
	Name           string          `json:"name"`
	SourceRevision string          `json:"source_revision"`
	Effective      EffectiveWindow `json:"effective"`
	Tombstone      bool            `json:"tombstone,omitempty"`
}

type PersonProjection struct {
	PersonRef        string          `json:"person_ref"`
	ExternalRef      string          `json:"external_ref"`
	EmploymentStatus string          `json:"employment_status"`
	PrimaryUnitRef   string          `json:"primary_unit_ref,omitempty"`
	ManagerPersonRef string          `json:"manager_person_ref,omitempty"`
	CostCenterRef    string          `json:"cost_center_ref,omitempty"`
	SourceRevision   string          `json:"source_revision"`
	Effective        EffectiveWindow `json:"effective"`
	Tombstone        bool            `json:"tombstone,omitempty"`
}

type AccountBindingProjection struct {
	AccountRef          string          `json:"account_ref"`
	ExternalSubjectRef  string          `json:"external_subject_ref"`
	PersonRef           string          `json:"person_ref,omitempty"`
	LocalUserID         *int64          `json:"local_user_id,omitempty"`
	AuthenticationState string          `json:"authentication_state"`
	SourceRevision      string          `json:"source_revision"`
	Effective           EffectiveWindow `json:"effective"`
	Tombstone           bool            `json:"tombstone,omitempty"`
}

// ProjectionSnapshot is either a complete authority snapshot or a bounded
// delta. It deliberately excludes passwords, tokens, keys and provider-specific
// credential material.
type ProjectionSnapshot struct {
	ContractVersion  string                       `json:"contract_version"`
	OrgID            int64                        `json:"org_id"`
	SourceRef        string                       `json:"source_ref"`
	Authority        string                       `json:"authority"`
	Kind             string                       `json:"kind"`
	SourceRevision   string                       `json:"source_revision"`
	PreviousRevision string                       `json:"previous_revision,omitempty"`
	ObservedAt       time.Time                    `json:"observed_at"`
	Gaps             []Gap                        `json:"gaps"`
	Units            []OrganizationUnitProjection `json:"units"`
	People           []PersonProjection           `json:"people"`
	Accounts         []AccountBindingProjection   `json:"accounts"`
}

// EffectiveAssignment is an effective-dated HCM mapping used to snapshot
// historical organization and cost ownership at execution time.
type EffectiveAssignment struct {
	PersonRef      string          `json:"person_ref"`
	UnitRef        string          `json:"unit_ref,omitempty"`
	CostCenterRef  string          `json:"cost_center_ref,omitempty"`
	SourceRef      string          `json:"source_ref"`
	SourceRevision string          `json:"source_revision"`
	Effective      EffectiveWindow `json:"effective"`
}

type AttributionInput struct {
	RequestRef             string    `json:"request_ref"`
	ProjectRef             string    `json:"project_ref"`
	InitiatingPrincipalRef string    `json:"initiating_principal_ref"`
	ProvenPersonRef        string    `json:"proven_person_ref,omitempty"`
	SharedCredential       bool      `json:"shared_credential"`
	EffectiveAt            time.Time `json:"effective_at"`
}

// AttributionSnapshot separates proof of the initiating person from cost
// ownership. API-key ownership alone never populates ProvenPersonRef.
type AttributionSnapshot struct {
	RequestRef             string    `json:"request_ref"`
	ProjectRef             string    `json:"project_ref"`
	InitiatingPrincipalRef string    `json:"initiating_principal_ref"`
	PersonRef              string    `json:"person_ref,omitempty"`
	UnitRef                string    `json:"unit_ref,omitempty"`
	CostCenterRef          string    `json:"cost_center_ref,omitempty"`
	SourceRef              string    `json:"source_ref,omitempty"`
	SourceRevision         string    `json:"source_revision,omitempty"`
	PrincipalAttribution   string    `json:"principal_attribution"`
	CostAttribution        string    `json:"cost_attribution"`
	EffectiveAt            time.Time `json:"effective_at"`
}

func ValidateSnapshot(snapshot ProjectionSnapshot) error {
	if snapshot.ContractVersion != ContractVersion {
		return invalid("contract_version")
	}
	if snapshot.OrgID <= 0 || blank(snapshot.SourceRef) || blank(snapshot.SourceRevision) {
		return invalid("source_identity")
	}
	if snapshot.Authority != AuthorityHCM && snapshot.Authority != AuthorityIDP {
		return invalid("authority")
	}
	if snapshot.Kind != SnapshotFull && snapshot.Kind != SnapshotDelta {
		return invalid("snapshot_kind")
	}
	if snapshot.Kind == SnapshotDelta && blank(snapshot.PreviousRevision) {
		return invalid("delta_previous_revision")
	}
	if snapshot.SourceRevision == strings.TrimSpace(snapshot.PreviousRevision) {
		return invalid("snapshot_revision_cycle")
	}
	if !validObservedTime(snapshot.ObservedAt) {
		return invalid("observed_at")
	}
	for _, gap := range snapshot.Gaps {
		if blank(gap.Code) || blank(gap.Provenance) {
			return invalid("gap")
		}
	}
	if snapshot.Authority == AuthorityHCM && len(snapshot.Accounts) != 0 {
		return invalid("hcm_cannot_author_account_fields")
	}
	if snapshot.Authority == AuthorityIDP && (len(snapshot.Units) != 0 || len(snapshot.People) != 0) {
		return invalid("idp_cannot_author_hcm_fields")
	}
	if err := validateUnits(snapshot.Kind, snapshot.SourceRevision, snapshot.Units); err != nil {
		return err
	}
	if err := validatePeople(snapshot.Kind, snapshot.SourceRevision, snapshot.People); err != nil {
		return err
	}
	if err := validateAccounts(snapshot.Kind, snapshot.SourceRevision, snapshot.Accounts); err != nil {
		return err
	}
	return nil
}

func EvaluateHealth(observation SourceObservation, now time.Time, maxAge time.Duration) (Health, error) {
	if observation.ContractVersion != ContractVersion || observation.OrgID <= 0 || blank(observation.SourceRef) {
		return Health{}, invalid("source_observation")
	}
	if observation.Authority != AuthorityHCM && observation.Authority != AuthorityIDP {
		return Health{}, invalid("authority")
	}
	if maxAge <= 0 || now.IsZero() {
		return Health{}, invalid("freshness_window")
	}
	health := Health{
		Connection:     observation.Connection,
		Freshness:      FreshnessUnknown,
		Reconciliation: ReconciliationNever,
		Coverage:       CoverageUnknown,
	}
	if observation.Connection != ConnectionConnected {
		if observation.Connection != ConnectionUnconfigured && observation.Connection != ConnectionDisabled && observation.Connection != ConnectionError {
			return Health{}, invalid("connection")
		}
		return health, nil
	}
	if observation.HasConflict {
		health.Reconciliation = ReconciliationConflict
	}
	if !validObservedTime(observation.ObservedAt) || observation.ObservedAt.After(now) {
		return health, nil
	}
	if now.Sub(observation.ObservedAt) > maxAge {
		health.Freshness = FreshnessStale
	} else {
		health.Freshness = FreshnessFresh
	}
	if observation.HasConflict {
		health.Reconciliation = ReconciliationConflict
	} else if observation.ReconciledAt.IsZero() {
		health.Reconciliation = ReconciliationNever
	} else if observation.ReconciledAt.Before(observation.ObservedAt) {
		health.Reconciliation = ReconciliationPending
	} else {
		health.Reconciliation = ReconciliationReconciled
	}
	if len(observation.Gaps) == 0 {
		health.Coverage = CoverageComplete
	} else {
		for _, gap := range observation.Gaps {
			if blank(gap.Code) || blank(gap.Provenance) {
				return Health{}, invalid("gap")
			}
		}
		health.Coverage = CoverageGapped
	}
	return health, nil
}

// MayGrantNewPrivilege fails closed for stale, unreconciled, conflicting or
// gapped directory state. Existing request authorization continues to use the
// product-local IAM projection and is not decided by this function.
func MayGrantNewPrivilege(health Health) (bool, string) {
	if health.Connection != ConnectionConnected {
		return false, "directory_not_connected"
	}
	if health.Freshness != FreshnessFresh {
		return false, "directory_not_fresh"
	}
	if health.Reconciliation != ReconciliationReconciled {
		return false, "directory_not_reconciled"
	}
	if health.Coverage != CoverageComplete {
		return false, "directory_coverage_incomplete"
	}
	return true, "directory_evidence_current"
}

func ResolveAttribution(input AttributionInput, assignments []EffectiveAssignment) (AttributionSnapshot, error) {
	if blank(input.RequestRef) || blank(input.ProjectRef) || blank(input.InitiatingPrincipalRef) || input.EffectiveAt.IsZero() {
		return AttributionSnapshot{}, invalid("attribution_input")
	}
	result := AttributionSnapshot{
		RequestRef: input.RequestRef, ProjectRef: input.ProjectRef,
		InitiatingPrincipalRef: input.InitiatingPrincipalRef,
		PrincipalAttribution:   AttributionUnallocated,
		CostAttribution:        AttributionUnallocated,
		EffectiveAt:            input.EffectiveAt,
	}
	if blank(input.ProvenPersonRef) {
		if input.SharedCredential {
			result.PrincipalAttribution = AttributionShared
		}
		return result, nil
	}
	result.PersonRef = strings.TrimSpace(input.ProvenPersonRef)
	result.PrincipalAttribution = AttributionDirect

	var matches []EffectiveAssignment
	for _, assignment := range assignments {
		if assignment.PersonRef != result.PersonRef {
			continue
		}
		if err := validateAssignment(assignment); err != nil {
			return AttributionSnapshot{}, err
		}
		if effectiveAt(assignment.Effective, input.EffectiveAt) {
			matches = append(matches, assignment)
		}
	}
	if len(matches) == 0 {
		return result, nil
	}
	if len(matches) > 1 {
		result.CostAttribution = AttributionConflict
		return result, nil
	}
	match := matches[0]
	result.UnitRef = match.UnitRef
	result.CostCenterRef = match.CostCenterRef
	result.SourceRef = match.SourceRef
	result.SourceRevision = match.SourceRevision
	if blank(result.UnitRef) && blank(result.CostCenterRef) {
		return result, nil
	}
	result.CostAttribution = AttributionDirect
	return result, nil
}

func validateUnits(snapshotKind, snapshotRevision string, units []OrganizationUnitProjection) error {
	seen := make(map[string]struct{}, len(units))
	for _, unit := range units {
		if blank(unit.UnitRef) || blank(unit.SourceRevision) || unit.SourceRevision != strings.TrimSpace(snapshotRevision) {
			return invalid("organization_unit_identity")
		}
		if unit.Tombstone {
			if snapshotKind != SnapshotDelta || !blank(unit.ExternalRef) || !blank(unit.ParentUnitRef) ||
				!blank(unit.Kind) || !blank(unit.Name) || !unit.Effective.ValidFrom.IsZero() || unit.Effective.ValidTo != nil {
				return invalid("organization_unit_tombstone")
			}
			if _, exists := seen[unit.UnitRef]; exists {
				return invalid("duplicate_unit_ref")
			}
			seen[unit.UnitRef] = struct{}{}
			continue
		}
		if blank(unit.UnitRef) || blank(unit.ExternalRef) || blank(unit.Name) || blank(unit.SourceRevision) || !validWindow(unit.Effective) {
			return invalid("organization_unit")
		}
		if unit.ParentUnitRef == unit.UnitRef {
			return invalid("organization_unit_parent")
		}
		switch unit.Kind {
		case UnitLegalEntity, UnitBusiness, UnitDepartment, UnitTeam:
		default:
			return invalid("organization_unit_kind")
		}
		if _, exists := seen[unit.UnitRef]; exists {
			return invalid("duplicate_unit_ref")
		}
		seen[unit.UnitRef] = struct{}{}
	}
	return nil
}

func validatePeople(snapshotKind, snapshotRevision string, people []PersonProjection) error {
	seen := make(map[string]struct{}, len(people))
	for _, person := range people {
		if blank(person.PersonRef) || blank(person.SourceRevision) || person.SourceRevision != strings.TrimSpace(snapshotRevision) {
			return invalid("person_identity")
		}
		if person.Tombstone {
			if snapshotKind != SnapshotDelta || !blank(person.ExternalRef) || !blank(person.EmploymentStatus) ||
				!blank(person.PrimaryUnitRef) || !blank(person.ManagerPersonRef) || !blank(person.CostCenterRef) ||
				!person.Effective.ValidFrom.IsZero() || person.Effective.ValidTo != nil {
				return invalid("person_tombstone")
			}
			if _, exists := seen[person.PersonRef]; exists {
				return invalid("duplicate_person_ref")
			}
			seen[person.PersonRef] = struct{}{}
			continue
		}
		if blank(person.PersonRef) || blank(person.ExternalRef) || blank(person.SourceRevision) || !validWindow(person.Effective) {
			return invalid("person")
		}
		if person.ManagerPersonRef == person.PersonRef {
			return invalid("person_manager")
		}
		switch person.EmploymentStatus {
		case EmploymentActive, EmploymentLeave, EmploymentTerminated:
		default:
			return invalid("employment_status")
		}
		if _, exists := seen[person.PersonRef]; exists {
			return invalid("duplicate_person_ref")
		}
		seen[person.PersonRef] = struct{}{}
	}
	return nil
}

func validateAccounts(snapshotKind, snapshotRevision string, accounts []AccountBindingProjection) error {
	seen := make(map[string]struct{}, len(accounts))
	for _, account := range accounts {
		if blank(account.AccountRef) || blank(account.SourceRevision) || account.SourceRevision != strings.TrimSpace(snapshotRevision) {
			return invalid("account_identity")
		}
		if account.Tombstone {
			if snapshotKind != SnapshotDelta || !blank(account.ExternalSubjectRef) || !blank(account.PersonRef) ||
				account.LocalUserID != nil || !blank(account.AuthenticationState) ||
				!account.Effective.ValidFrom.IsZero() || account.Effective.ValidTo != nil {
				return invalid("account_tombstone")
			}
			if _, exists := seen[account.AccountRef]; exists {
				return invalid("duplicate_account_ref")
			}
			seen[account.AccountRef] = struct{}{}
			continue
		}
		if blank(account.AccountRef) || blank(account.ExternalSubjectRef) || blank(account.SourceRevision) || !validWindow(account.Effective) {
			return invalid("account")
		}
		if account.LocalUserID != nil && *account.LocalUserID <= 0 {
			return invalid("local_user_id")
		}
		if account.LocalUserID != nil && blank(account.PersonRef) {
			return invalid("local_user_person_binding")
		}
		if account.AuthenticationState != AuthenticationActive && account.AuthenticationState != AuthenticationDisabled {
			return invalid("authentication_state")
		}
		if _, exists := seen[account.AccountRef]; exists {
			return invalid("duplicate_account_ref")
		}
		seen[account.AccountRef] = struct{}{}
	}
	return nil
}

func validateAssignment(assignment EffectiveAssignment) error {
	if blank(assignment.PersonRef) || blank(assignment.SourceRef) || blank(assignment.SourceRevision) || !validWindow(assignment.Effective) {
		return invalid("effective_assignment")
	}
	return nil
}

func validWindow(window EffectiveWindow) bool {
	if !validObservedTime(window.ValidFrom) {
		return false
	}
	return window.ValidTo == nil || (validObservedTime(*window.ValidTo) && window.ValidTo.After(window.ValidFrom))
}

func effectiveAt(window EffectiveWindow, instant time.Time) bool {
	return !instant.Before(window.ValidFrom) && (window.ValidTo == nil || instant.Before(*window.ValidTo))
}

func validObservedTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1_000 == 0
}

func blank(value string) bool { return strings.TrimSpace(value) == "" }

func invalid(field string) error { return fmt.Errorf("%w: %s", ErrInvalid, field) }
