package orgdirectory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const activePlatformAdminMutationLockID int64 = 8675309001

const (
	IAMActionNoChange       = "no_change"
	IAMActionReviewRequired = "review_required"
	IAMActionDisableUser    = "disable_user"

	IAMResultRecorded = "recorded"
	IAMResultApplied  = "applied"
	IAMResultNoop     = "noop"
	IAMResultConflict = "conflict"
)

type IAMActionEvidence struct {
	ID           int64          `json:"id"`
	SnapshotRef  string         `json:"snapshot_ref"`
	SubjectKind  string         `json:"subject_kind"`
	SubjectRef   string         `json:"subject_ref"`
	PersonRef    string         `json:"person_ref"`
	AccountRef   string         `json:"account_ref"`
	LocalUserID  *int64         `json:"local_user_id,omitempty"`
	Action       string         `json:"action"`
	Reason       string         `json:"reason"`
	Result       string         `json:"result"`
	BeforeStatus string         `json:"before_status"`
	AfterStatus  string         `json:"after_status"`
	Detail       map[string]any `json:"detail"`
	CreatedAt    time.Time      `json:"created_at"`
	AppliedAt    *time.Time     `json:"applied_at,omitempty"`
}

type materializedProjection struct {
	units    map[string]OrganizationUnitProjection
	people   map[string]PersonProjection
	accounts map[string]AccountBindingProjection
}

func emptyMaterializedProjection() materializedProjection {
	return materializedProjection{
		units:    make(map[string]OrganizationUnitProjection),
		people:   make(map[string]PersonProjection),
		accounts: make(map[string]AccountBindingProjection),
	}
}

// ApplyProjectionSnapshot persists source-owned HCM or IdP facts and converges
// only proven terminal identity states into existing product-local IAM. It has
// no external connector, does not create users, and never grants or restores a
// role, project membership, API key or other privilege.
func (s *PostgresStore) ApplyProjectionSnapshot(ctx context.Context, snapshot ProjectionSnapshot) (ApplyResult, error) {
	if err := ValidateSnapshot(snapshot); err != nil {
		return ApplyResult{}, err
	}
	snapshotRef, err := SnapshotIdentity(snapshot)
	if err != nil {
		return ApplyResult{}, err
	}
	gapsJSON, err := json.Marshal(append([]Gap{}, snapshot.Gaps...))
	if err != nil {
		return ApplyResult{}, fmt.Errorf("encode directory gaps: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("begin directory projection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	lockKey := fmt.Sprintf("enterprise-directory:%d:%s", snapshot.OrgID, strings.TrimSpace(snapshot.SourceRef))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return ApplyResult{}, fmt.Errorf("lock directory source: %w", err)
	}

	var sourceID int64
	var authority, connection, currentRevision, currentSnapshotRef string
	err = tx.QueryRow(ctx, `
		SELECT id, authority, connection, current_revision, current_snapshot_ref
		FROM enterprise_directory_sources
		WHERE org_id = $1 AND source_ref = $2
		FOR UPDATE`, snapshot.OrgID, strings.TrimSpace(snapshot.SourceRef),
	).Scan(&sourceID, &authority, &connection, &currentRevision, &currentSnapshotRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApplyResult{}, ErrSourceMissing
	}
	if err != nil {
		return ApplyResult{}, fmt.Errorf("load directory source: %w", err)
	}
	if authority != snapshot.Authority {
		return s.commitProjectionConflict(ctx, tx, sourceID, fmt.Errorf("%w: source authority changed", ErrConflict))
	}
	if connection != ConnectionConnected {
		return ApplyResult{}, ErrSourceOffline
	}

	var existingRef string
	var existingUnits, existingPeople, existingAccounts, existingActions int
	err = tx.QueryRow(ctx, `
		SELECT snapshot_ref,
		       (SELECT COUNT(*) FROM enterprise_directory_unit_snapshots u WHERE u.snapshot_id = s.id),
		       (SELECT COUNT(*) FROM enterprise_directory_person_snapshots p WHERE p.snapshot_id = s.id),
		       (SELECT COUNT(*) FROM enterprise_directory_account_snapshots a WHERE a.snapshot_id = s.id),
		       (SELECT COUNT(*) FROM enterprise_directory_iam_actions i WHERE i.snapshot_id = s.id)
		FROM enterprise_directory_snapshots s
		WHERE s.org_id = $1 AND s.source_id = $2 AND s.source_revision = $3`,
		snapshot.OrgID, sourceID, snapshot.SourceRevision,
	).Scan(&existingRef, &existingUnits, &existingPeople, &existingAccounts, &existingActions)
	if err == nil {
		if existingRef != snapshotRef {
			return s.commitProjectionConflict(ctx, tx, sourceID, fmt.Errorf("%w: revision bytes changed", ErrConflict))
		}
		if err := tx.Commit(ctx); err != nil {
			return ApplyResult{}, fmt.Errorf("commit duplicate directory projection: %w", err)
		}
		return ApplyResult{
			Result: ApplyResultDuplicate, SnapshotRef: snapshotRef, SourceRevision: snapshot.SourceRevision,
			UnitCount: existingUnits, PersonCount: existingPeople, AccountCount: existingAccounts,
			IAMActionCount: existingActions,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ApplyResult{}, fmt.Errorf("check directory projection replay: %w", err)
	}
	if strings.TrimSpace(snapshot.PreviousRevision) != currentRevision {
		return s.commitProjectionConflict(ctx, tx, sourceID, fmt.Errorf(
			"%w: current=%q previous=%q", ErrStaleRevision, currentRevision, snapshot.PreviousRevision,
		))
	}

	previous := emptyMaterializedProjection()
	if currentSnapshotRef != "" {
		previous, err = loadMaterializedProjection(ctx, tx, snapshot.OrgID, sourceID, currentSnapshotRef)
		if err != nil {
			return ApplyResult{}, err
		}
	}
	materialized := emptyMaterializedProjection()
	if snapshot.Kind == SnapshotDelta {
		if currentSnapshotRef == "" {
			return s.commitProjectionConflict(ctx, tx, sourceID, fmt.Errorf("%w: delta has no base snapshot", ErrStaleRevision))
		}
		materialized = cloneMaterializedProjection(previous)
	}
	applySnapshotFacts(&materialized, snapshot)
	if err := validateMaterializedProjection(materialized, snapshot.Authority, len(snapshot.Gaps) > 0); err != nil {
		return s.commitProjectionConflict(ctx, tx, sourceID, err)
	}
	if err := validateLocalBindings(ctx, tx, snapshot, materialized); err != nil {
		return s.commitProjectionConflict(ctx, tx, sourceID, err)
	}

	var snapshotID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO enterprise_directory_snapshots (
		  snapshot_ref, org_id, source_id, kind, source_revision,
		  previous_revision, observed_at, gaps
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id`,
		snapshotRef, snapshot.OrgID, sourceID, snapshot.Kind, snapshot.SourceRevision,
		strings.TrimSpace(snapshot.PreviousRevision), snapshot.ObservedAt, gapsJSON,
	).Scan(&snapshotID)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("insert directory projection: %w", err)
	}
	if err := insertMaterializedProjection(ctx, tx, snapshotID, snapshot.OrgID, sourceID, materialized); err != nil {
		return ApplyResult{}, err
	}

	actionCount, err := convergeIAM(ctx, tx, snapshotID, snapshotRef, snapshot, previous)
	if err != nil {
		return ApplyResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE enterprise_directory_sources
		SET current_snapshot_ref = $1,
		    current_revision = $2,
		    observed_at = $3,
		    reconciled_at = $3,
		    has_conflict = FALSE,
		    gaps = $4,
		    updated_at = NOW()
		WHERE id = $5 AND org_id = $6`,
		snapshotRef, snapshot.SourceRevision, snapshot.ObservedAt, gapsJSON, sourceID, snapshot.OrgID,
	); err != nil {
		return ApplyResult{}, fmt.Errorf("advance directory source: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplyResult{}, fmt.Errorf("commit directory projection: %w", err)
	}
	return ApplyResult{
		Result: ApplyResultApplied, SnapshotRef: snapshotRef, SourceRevision: snapshot.SourceRevision,
		UnitCount: len(materialized.units), PersonCount: len(materialized.people),
		AccountCount: len(materialized.accounts), IAMActionCount: actionCount,
	}, nil
}

func applySnapshotFacts(materialized *materializedProjection, snapshot ProjectionSnapshot) {
	for _, unit := range snapshot.Units {
		if unit.Tombstone {
			delete(materialized.units, unit.UnitRef)
		} else {
			materialized.units[unit.UnitRef] = unit
		}
	}
	for _, person := range snapshot.People {
		if person.Tombstone {
			delete(materialized.people, person.PersonRef)
		} else {
			materialized.people[person.PersonRef] = person
		}
	}
	for _, account := range snapshot.Accounts {
		if account.Tombstone {
			delete(materialized.accounts, account.AccountRef)
		} else {
			materialized.accounts[account.AccountRef] = account
		}
	}
}

func cloneMaterializedProjection(source materializedProjection) materializedProjection {
	result := emptyMaterializedProjection()
	for key, value := range source.units {
		result.units[key] = value
	}
	for key, value := range source.people {
		result.people[key] = value
	}
	for key, value := range source.accounts {
		result.accounts[key] = value
	}
	return result
}

func validateMaterializedProjection(materialized materializedProjection, authority string, allowReferenceGaps bool) error {
	if authority == AuthorityHCM && len(materialized.accounts) != 0 {
		return fmt.Errorf("%w: HCM projection contains account facts", ErrConflict)
	}
	if authority == AuthorityIDP && (len(materialized.units) != 0 || len(materialized.people) != 0) {
		return fmt.Errorf("%w: IdP projection contains HCM facts", ErrConflict)
	}
	if err := validateMaterializedHierarchy(materialized.units); err != nil {
		return err
	}
	for personRef, person := range materialized.people {
		if person.Tombstone {
			return fmt.Errorf("%w: materialized person tombstone %q", ErrConflict, personRef)
		}
		if person.EmploymentStatus != EmploymentTerminated && !allowReferenceGaps && person.PrimaryUnitRef != "" {
			if _, ok := materialized.units[person.PrimaryUnitRef]; !ok {
				return fmt.Errorf("%w: primary unit %q for person %q is missing", ErrConflict, person.PrimaryUnitRef, personRef)
			}
		}
		if !allowReferenceGaps && person.ManagerPersonRef != "" {
			if _, ok := materialized.people[person.ManagerPersonRef]; !ok {
				return fmt.Errorf("%w: manager %q for person %q is missing", ErrConflict, person.ManagerPersonRef, personRef)
			}
		}
	}
	state := make(map[string]uint8, len(materialized.people))
	var visit func(string) error
	visit = func(personRef string) error {
		switch state[personRef] {
		case 1:
			return fmt.Errorf("%w: manager cycle at %q", ErrConflict, personRef)
		case 2:
			return nil
		}
		state[personRef] = 1
		manager := materialized.people[personRef].ManagerPersonRef
		if manager != "" {
			if _, ok := materialized.people[manager]; ok {
				if err := visit(manager); err != nil {
					return err
				}
			}
		}
		state[personRef] = 2
		return nil
	}
	for personRef := range materialized.people {
		if err := visit(personRef); err != nil {
			return err
		}
	}
	for accountRef, account := range materialized.accounts {
		if account.Tombstone {
			return fmt.Errorf("%w: materialized account tombstone %q", ErrConflict, accountRef)
		}
		if account.LocalUserID != nil && strings.TrimSpace(account.PersonRef) == "" {
			return fmt.Errorf("%w: local account binding %q has no person reference", ErrConflict, accountRef)
		}
	}
	return nil
}

func validateLocalBindings(ctx context.Context, tx pgx.Tx, snapshot ProjectionSnapshot, materialized materializedProjection) error {
	if snapshot.Authority != AuthorityIDP {
		return nil
	}
	for accountRef, account := range materialized.accounts {
		if account.LocalUserID == nil {
			continue
		}
		var userOrgID int64
		if err := tx.QueryRow(ctx, `SELECT org_id FROM users WHERE id = $1`, *account.LocalUserID).Scan(&userOrgID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: local user for account %q is missing", ErrConflict, accountRef)
			}
			return fmt.Errorf("validate local account binding %q: %w", accountRef, err)
		}
		if userOrgID != snapshot.OrgID {
			return fmt.Errorf("%w: account %q crosses organization scope", ErrConflict, accountRef)
		}
		var personMatches int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM enterprise_directory_sources s
			JOIN enterprise_directory_snapshots snap
			  ON snap.org_id = s.org_id AND snap.source_id = s.id
			 AND snap.snapshot_ref = s.current_snapshot_ref
			JOIN enterprise_directory_person_snapshots p
			  ON p.snapshot_id = snap.id AND p.org_id = snap.org_id AND p.source_id = snap.source_id
			WHERE s.org_id = $1 AND s.authority = 'hcm' AND p.person_ref = $2
			  AND p.valid_from <= $3 AND (p.valid_to IS NULL OR p.valid_to > $3)`,
			snapshot.OrgID, account.PersonRef, snapshot.ObservedAt,
		).Scan(&personMatches); err != nil {
			return fmt.Errorf("validate account person binding %q: %w", accountRef, err)
		}
		if personMatches != 1 {
			return fmt.Errorf("%w: account %q person reference resolves %d times", ErrConflict, accountRef, personMatches)
		}
	}
	return nil
}

func loadMaterializedProjection(ctx context.Context, tx pgx.Tx, orgID, sourceID int64, snapshotRef string) (materializedProjection, error) {
	result := emptyMaterializedProjection()
	units, err := loadMaterializedUnits(ctx, tx, orgID, sourceID, snapshotRef)
	if err != nil {
		return result, err
	}
	result.units = units

	peopleRows, err := tx.Query(ctx, `
		SELECT p.person_ref, p.external_ref, p.employment_status, p.primary_unit_ref,
		       p.manager_person_ref, p.cost_center_ref, p.source_revision, p.valid_from, p.valid_to
		FROM enterprise_directory_snapshots s
		JOIN enterprise_directory_person_snapshots p
		  ON p.snapshot_id = s.id AND p.org_id = s.org_id AND p.source_id = s.source_id
		WHERE s.org_id = $1 AND s.source_id = $2 AND s.snapshot_ref = $3`, orgID, sourceID, snapshotRef)
	if err != nil {
		return result, fmt.Errorf("load materialized people: %w", err)
	}
	for peopleRows.Next() {
		var person PersonProjection
		var validTo *time.Time
		if err := peopleRows.Scan(
			&person.PersonRef, &person.ExternalRef, &person.EmploymentStatus, &person.PrimaryUnitRef,
			&person.ManagerPersonRef, &person.CostCenterRef, &person.SourceRevision,
			&person.Effective.ValidFrom, &validTo,
		); err != nil {
			peopleRows.Close()
			return result, fmt.Errorf("scan materialized person: %w", err)
		}
		person.Effective.ValidFrom = person.Effective.ValidFrom.UTC()
		if validTo != nil {
			value := validTo.UTC()
			person.Effective.ValidTo = &value
		}
		result.people[person.PersonRef] = person
	}
	if err := peopleRows.Err(); err != nil {
		peopleRows.Close()
		return result, fmt.Errorf("iterate materialized people: %w", err)
	}
	peopleRows.Close()

	accountRows, err := tx.Query(ctx, `
		SELECT a.account_ref, a.external_subject_ref, a.person_ref, a.local_user_id,
		       a.authentication_state, a.source_revision, a.valid_from, a.valid_to
		FROM enterprise_directory_snapshots s
		JOIN enterprise_directory_account_snapshots a
		  ON a.snapshot_id = s.id AND a.org_id = s.org_id AND a.source_id = s.source_id
		WHERE s.org_id = $1 AND s.source_id = $2 AND s.snapshot_ref = $3`, orgID, sourceID, snapshotRef)
	if err != nil {
		return result, fmt.Errorf("load materialized accounts: %w", err)
	}
	defer accountRows.Close()
	for accountRows.Next() {
		var account AccountBindingProjection
		var validTo *time.Time
		if err := accountRows.Scan(
			&account.AccountRef, &account.ExternalSubjectRef, &account.PersonRef, &account.LocalUserID,
			&account.AuthenticationState, &account.SourceRevision, &account.Effective.ValidFrom, &validTo,
		); err != nil {
			return result, fmt.Errorf("scan materialized account: %w", err)
		}
		account.Effective.ValidFrom = account.Effective.ValidFrom.UTC()
		if validTo != nil {
			value := validTo.UTC()
			account.Effective.ValidTo = &value
		}
		result.accounts[account.AccountRef] = account
	}
	if err := accountRows.Err(); err != nil {
		return result, fmt.Errorf("iterate materialized accounts: %w", err)
	}
	return result, nil
}

func insertMaterializedProjection(ctx context.Context, tx pgx.Tx, snapshotID, orgID, sourceID int64, materialized materializedProjection) error {
	unitRefs := sortedMapKeys(materialized.units)
	for _, unitRef := range unitRefs {
		unit := materialized.units[unitRef]
		if _, err := tx.Exec(ctx, `
			INSERT INTO enterprise_directory_unit_snapshots (
			  snapshot_id, org_id, source_id, unit_ref, external_ref,
			  parent_unit_ref, kind, name, source_revision, valid_from, valid_to
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			snapshotID, orgID, sourceID, unit.UnitRef, unit.ExternalRef, unit.ParentUnitRef,
			unit.Kind, unit.Name, unit.SourceRevision, unit.Effective.ValidFrom, unit.Effective.ValidTo,
		); err != nil {
			return fmt.Errorf("insert directory unit %q: %w", unitRef, err)
		}
	}
	personRefs := sortedMapKeys(materialized.people)
	for _, personRef := range personRefs {
		person := materialized.people[personRef]
		if _, err := tx.Exec(ctx, `
			INSERT INTO enterprise_directory_person_snapshots (
			  snapshot_id, org_id, source_id, person_ref, external_ref, employment_status,
			  primary_unit_ref, manager_person_ref, cost_center_ref, source_revision, valid_from, valid_to
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			snapshotID, orgID, sourceID, person.PersonRef, person.ExternalRef, person.EmploymentStatus,
			person.PrimaryUnitRef, person.ManagerPersonRef, person.CostCenterRef,
			person.SourceRevision, person.Effective.ValidFrom, person.Effective.ValidTo,
		); err != nil {
			return fmt.Errorf("insert directory person %q: %w", personRef, err)
		}
	}
	accountRefs := sortedMapKeys(materialized.accounts)
	for _, accountRef := range accountRefs {
		account := materialized.accounts[accountRef]
		if _, err := tx.Exec(ctx, `
			INSERT INTO enterprise_directory_account_snapshots (
			  snapshot_id, org_id, source_id, account_ref, external_subject_ref,
			  person_ref, local_user_id, authentication_state, source_revision, valid_from, valid_to
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			snapshotID, orgID, sourceID, account.AccountRef, account.ExternalSubjectRef,
			account.PersonRef, account.LocalUserID, account.AuthenticationState,
			account.SourceRevision, account.Effective.ValidFrom, account.Effective.ValidTo,
		); err != nil {
			return fmt.Errorf("insert directory account %q: %w", accountRef, err)
		}
	}
	return nil
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type boundAccount struct {
	SourceRef   string
	AccountRef  string
	PersonRef   string
	LocalUserID int64
}

func convergeIAM(ctx context.Context, tx pgx.Tx, snapshotID int64, snapshotRef string, snapshot ProjectionSnapshot, previous materializedProjection) (int, error) {
	actionCount := 0
	record := func(subjectKind, subjectRef, personRef, accountRef string, userID *int64, action, reason, result, before, after string, detail map[string]any, applied bool) error {
		if detail == nil {
			detail = map[string]any{}
		}
		detail["snapshot_ref"] = snapshotRef
		detail["source_revision"] = snapshot.SourceRevision
		detailJSON, err := json.Marshal(detail)
		if err != nil {
			return fmt.Errorf("encode IAM action detail: %w", err)
		}
		var appliedAt *time.Time
		if applied {
			value := time.Now().UTC().Truncate(time.Microsecond)
			appliedAt = &value
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO enterprise_directory_iam_actions (
			  snapshot_id, org_id, source_id, subject_kind, subject_ref, person_ref,
			  account_ref, local_user_id, action, reason, result, before_status,
			  after_status, detail, applied_at
			) SELECT $1,$2,id,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14
			  FROM enterprise_directory_sources
			  WHERE org_id = $2 AND source_ref = $15`,
			snapshotID, snapshot.OrgID, subjectKind, subjectRef, personRef, accountRef,
			userID, action, reason, result, before, after, detailJSON, appliedAt, strings.TrimSpace(snapshot.SourceRef),
		); err != nil {
			return fmt.Errorf("record IAM convergence action: %w", err)
		}
		actionCount++
		return nil
	}

	coverageGapped := len(snapshot.Gaps) > 0
	if snapshot.Authority == AuthorityIDP {
		for _, incoming := range snapshot.Accounts {
			account := incoming
			reason := ""
			if incoming.Tombstone {
				var ok bool
				account, ok = previous.accounts[incoming.AccountRef]
				if !ok || account.LocalUserID == nil {
					if err := record("account", incoming.AccountRef, "", incoming.AccountRef, nil, IAMActionNoChange, "account_tombstone_without_local_binding", IAMResultNoop, "", "", nil, false); err != nil {
						return actionCount, err
					}
					continue
				}
				reason = "idp_account_tombstone"
			} else if incoming.AuthenticationState == AuthenticationDisabled && incoming.LocalUserID != nil {
				reason = "idp_account_disabled"
			} else if incoming.AuthenticationState == AuthenticationActive && incoming.LocalUserID != nil {
				var userStatus string
				if err := tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1 AND org_id=$2`, *incoming.LocalUserID, snapshot.OrgID).Scan(&userStatus); err != nil {
					return actionCount, fmt.Errorf("read local user activation state: %w", err)
				}
				if userStatus != "active" {
					if err := record("account", incoming.AccountRef, incoming.PersonRef, incoming.AccountRef, incoming.LocalUserID, IAMActionReviewRequired, "reactivation_never_restores_local_access", IAMResultRecorded, userStatus, userStatus, nil, false); err != nil {
						return actionCount, err
					}
				}
				continue
			} else {
				continue
			}
			if account.LocalUserID == nil {
				continue
			}
			if coverageGapped {
				if err := record("account", account.AccountRef, account.PersonRef, account.AccountRef, account.LocalUserID, IAMActionReviewRequired, "terminal_state_with_capture_gap", IAMResultRecorded, "", "", map[string]any{"gap_count": len(snapshot.Gaps)}, false); err != nil {
					return actionCount, err
				}
				continue
			}
			result, before, after, applied, err := disableLocalUser(ctx, tx, snapshot.OrgID, *account.LocalUserID)
			if err != nil {
				return actionCount, err
			}
			action := IAMActionDisableUser
			if result == IAMResultConflict {
				action = IAMActionReviewRequired
			}
			if err := record("account", account.AccountRef, account.PersonRef, account.AccountRef, account.LocalUserID, action, reason, result, before, after, nil, applied); err != nil {
				return actionCount, err
			}
		}
		return actionCount, nil
	}

	for _, incoming := range snapshot.People {
		person := incoming
		reason := ""
		if incoming.Tombstone {
			var ok bool
			person, ok = previous.people[incoming.PersonRef]
			if !ok {
				if err := record("person", incoming.PersonRef, incoming.PersonRef, "", nil, IAMActionNoChange, "person_tombstone_without_prior_binding", IAMResultNoop, "", "", nil, false); err != nil {
					return actionCount, err
				}
				continue
			}
			reason = "hcm_person_tombstone"
		} else if incoming.EmploymentStatus == EmploymentTerminated {
			reason = "hcm_employment_terminated"
		} else {
			continue
		}
		accounts, err := currentBoundAccounts(ctx, tx, snapshot.OrgID, person.PersonRef, snapshot.ObservedAt)
		if err != nil {
			return actionCount, err
		}
		if len(accounts) == 0 {
			if err := record("person", person.PersonRef, person.PersonRef, "", nil, IAMActionNoChange, reason+"_without_local_binding", IAMResultNoop, "", "", nil, false); err != nil {
				return actionCount, err
			}
			continue
		}
		for _, account := range accounts {
			userID := account.LocalUserID
			subjectRef := account.SourceRef + ":" + account.AccountRef
			if coverageGapped {
				if err := record("account", subjectRef, person.PersonRef, account.AccountRef, &userID, IAMActionReviewRequired, "terminal_state_with_capture_gap", IAMResultRecorded, "", "", map[string]any{"account_source_ref": account.SourceRef, "gap_count": len(snapshot.Gaps)}, false); err != nil {
					return actionCount, err
				}
				continue
			}
			result, before, after, applied, err := disableLocalUser(ctx, tx, snapshot.OrgID, userID)
			if err != nil {
				return actionCount, err
			}
			action := IAMActionDisableUser
			if result == IAMResultConflict {
				action = IAMActionReviewRequired
			}
			if err := record("account", subjectRef, person.PersonRef, account.AccountRef, &userID, action, reason, result, before, after, map[string]any{"account_source_ref": account.SourceRef}, applied); err != nil {
				return actionCount, err
			}
		}
	}
	return actionCount, nil
}

func currentBoundAccounts(ctx context.Context, tx pgx.Tx, orgID int64, personRef string, effectiveAt time.Time) ([]boundAccount, error) {
	rows, err := tx.Query(ctx, `
		SELECT s.source_ref, a.account_ref, a.person_ref, a.local_user_id
		FROM enterprise_directory_sources s
		JOIN enterprise_directory_snapshots snap
		  ON snap.org_id=s.org_id AND snap.source_id=s.id AND snap.snapshot_ref=s.current_snapshot_ref
		JOIN enterprise_directory_account_snapshots a
		  ON a.snapshot_id=snap.id AND a.org_id=snap.org_id AND a.source_id=snap.source_id
		WHERE s.org_id=$1 AND s.authority='idp' AND a.person_ref=$2 AND a.local_user_id IS NOT NULL
		  AND a.valid_from <= $3 AND (a.valid_to IS NULL OR a.valid_to > $3)
		ORDER BY a.account_ref`, orgID, personRef, effectiveAt)
	if err != nil {
		return nil, fmt.Errorf("load current account bindings: %w", err)
	}
	defer rows.Close()
	accounts := make([]boundAccount, 0)
	for rows.Next() {
		var account boundAccount
		if err := rows.Scan(&account.SourceRef, &account.AccountRef, &account.PersonRef, &account.LocalUserID); err != nil {
			return nil, fmt.Errorf("scan current account binding: %w", err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current account bindings: %w", err)
	}
	return accounts, nil
}

func disableLocalUser(ctx context.Context, tx pgx.Tx, orgID, userID int64) (result, before, after string, applied bool, err error) {
	var role, status string
	if err := tx.QueryRow(ctx, `SELECT role, status FROM users WHERE id=$1 AND org_id=$2 FOR UPDATE`, userID, orgID).Scan(&role, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IAMResultConflict, "missing", "missing", false, nil
		}
		return "", "", "", false, fmt.Errorf("lock local user for directory convergence: %w", err)
	}
	if status != "active" {
		return IAMResultNoop, status, status, false, nil
	}
	if role == "admin" || role == "platform_admin" {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, activePlatformAdminMutationLockID); err != nil {
			return "", "", "", false, fmt.Errorf("lock platform administrator convergence: %w", err)
		}
		var activePlatformAdmins int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role IN ('admin','platform_admin') AND status='active'`).Scan(&activePlatformAdmins); err != nil {
			return "", "", "", false, fmt.Errorf("count active platform administrators: %w", err)
		}
		if activePlatformAdmins <= 1 {
			return IAMResultConflict, status, status, false, nil
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE users SET status='disabled', auth_epoch=auth_epoch+1, updated_at=NOW()
		WHERE id=$1 AND org_id=$2`, userID, orgID); err != nil {
		return "", "", "", false, fmt.Errorf("disable local user from directory evidence: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE admin_sessions SET revoked_at=COALESCE(revoked_at,NOW())
		WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>NOW()`, userID); err != nil {
		return "", "", "", false, fmt.Errorf("revoke local sessions from directory evidence: %w", err)
	}
	// Account disable is reversible. Keep credential families and secret
	// versions intact; runtime authorization and effective-status read models
	// fail closed on the disabled subject. Permanent containment happens only
	// on account deletion, membership loss, workload archive, or grant revoke.
	return IAMResultApplied, status, "disabled", true, nil
}

// ListIAMActions returns product-local convergence evidence only. It never
// exposes a connector credential or allows the caller to mutate IAM.
func (s *PostgresStore) ListIAMActions(ctx context.Context, orgID int64, limit int) ([]IAMActionEvidence, error) {
	if orgID <= 0 || limit <= 0 || limit > 500 {
		return nil, invalid("iam_action_list")
	}
	rows, err := s.db.Query(ctx, `
		SELECT i.id, snap.snapshot_ref, i.subject_kind, i.subject_ref, i.person_ref,
		       i.account_ref, i.local_user_id, i.action, i.reason, i.result,
		       i.before_status, i.after_status, i.detail, i.created_at, i.applied_at
		FROM enterprise_directory_iam_actions i
		JOIN enterprise_directory_snapshots snap
		  ON snap.id=i.snapshot_id AND snap.org_id=i.org_id AND snap.source_id=i.source_id
		WHERE i.org_id=$1
		ORDER BY i.created_at DESC, i.id DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, fmt.Errorf("list directory IAM actions: %w", err)
	}
	defer rows.Close()
	actions := make([]IAMActionEvidence, 0)
	for rows.Next() {
		var action IAMActionEvidence
		var detailJSON []byte
		if err := rows.Scan(
			&action.ID, &action.SnapshotRef, &action.SubjectKind, &action.SubjectRef,
			&action.PersonRef, &action.AccountRef, &action.LocalUserID, &action.Action,
			&action.Reason, &action.Result, &action.BeforeStatus, &action.AfterStatus,
			&detailJSON, &action.CreatedAt, &action.AppliedAt,
		); err != nil {
			return nil, fmt.Errorf("scan directory IAM action: %w", err)
		}
		if err := json.Unmarshal(detailJSON, &action.Detail); err != nil {
			return nil, fmt.Errorf("decode directory IAM action: %w", err)
		}
		action.CreatedAt = action.CreatedAt.UTC()
		if action.AppliedAt != nil {
			value := action.AppliedAt.UTC()
			action.AppliedAt = &value
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate directory IAM actions: %w", err)
	}
	return actions, nil
}
