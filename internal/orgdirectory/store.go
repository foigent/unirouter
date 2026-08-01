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
	internaldb "github.com/uniroute/unirouter/internal/db"
)

const (
	ApplyResultApplied   = "applied"
	ApplyResultDuplicate = "duplicate_noop"
)

var (
	ErrConflict      = errors.New("orgdirectory: projection conflict")
	ErrStaleRevision = errors.New("orgdirectory: stale or non-contiguous revision")
	ErrSourceMissing = errors.New("orgdirectory: source is not registered")
	ErrSourceOffline = errors.New("orgdirectory: source is not connected")
)

type SourceRegistration struct {
	OrgID      int64  `json:"org_id"`
	SourceRef  string `json:"source_ref"`
	Authority  string `json:"authority"`
	Connection string `json:"connection"`
}

type SourceState struct {
	Observation SourceObservation `json:"observation"`
	Health      Health            `json:"health"`
}

type ApplyResult struct {
	Result         string `json:"result"`
	SnapshotRef    string `json:"snapshot_ref"`
	SourceRevision string `json:"source_revision"`
	UnitCount      int    `json:"unit_count"`
	PersonCount    int    `json:"person_count"`
	AccountCount   int    `json:"account_count"`
	IAMActionCount int    `json:"iam_action_count"`
}

type PostgresStore struct {
	db internaldb.DBQuerier
}

func NewPostgresStore(database internaldb.DBQuerier) (*PostgresStore, error) {
	if database == nil {
		return nil, invalid("database")
	}
	return &PostgresStore{db: database}, nil
}

// RegisterSource stores metadata only. Connector credentials and provider-
// specific configuration are intentionally outside this table and package.
func (s *PostgresStore) RegisterSource(ctx context.Context, registration SourceRegistration) (int64, error) {
	registration.SourceRef = strings.TrimSpace(registration.SourceRef)
	if registration.OrgID <= 0 || registration.SourceRef == "" {
		return 0, invalid("source_registration")
	}
	if registration.Authority != AuthorityHCM && registration.Authority != AuthorityIDP {
		return 0, invalid("authority")
	}
	switch registration.Connection {
	case ConnectionUnconfigured, ConnectionConnected, ConnectionDisabled, ConnectionError:
	default:
		return 0, invalid("connection")
	}

	var id int64
	err := s.db.QueryRow(ctx, `
		INSERT INTO enterprise_directory_sources (
		  org_id, contract_version, source_ref, authority, connection
		) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (org_id, source_ref) DO UPDATE
		SET connection = EXCLUDED.connection, updated_at = NOW()
		WHERE enterprise_directory_sources.authority = EXCLUDED.authority
		RETURNING id`,
		registration.OrgID, ContractVersion, registration.SourceRef,
		registration.Authority, registration.Connection,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("%w: source authority is immutable", ErrConflict)
	}
	if err != nil {
		return 0, fmt.Errorf("register directory source: %w", err)
	}
	return id, nil
}

// ApplyOrganizationSnapshot applies only the HCM-owned organization tree.
// People and account convergence are added by ORG-2 and cannot leak into this
// narrower foundation path.
func (s *PostgresStore) ApplyOrganizationSnapshot(ctx context.Context, snapshot ProjectionSnapshot) (ApplyResult, error) {
	if err := ValidateSnapshot(snapshot); err != nil {
		return ApplyResult{}, err
	}
	if snapshot.Authority != AuthorityHCM || len(snapshot.People) != 0 || len(snapshot.Accounts) != 0 {
		return ApplyResult{}, invalid("organization_snapshot_scope")
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
		return ApplyResult{}, fmt.Errorf("begin directory snapshot: %w", err)
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
	var existingUnits int
	err = tx.QueryRow(ctx, `
		SELECT snapshot_ref,
		       (SELECT COUNT(*) FROM enterprise_directory_unit_snapshots u WHERE u.snapshot_id = s.id)
		FROM enterprise_directory_snapshots s
		WHERE s.org_id = $1 AND s.source_id = $2 AND s.source_revision = $3`,
		snapshot.OrgID, sourceID, snapshot.SourceRevision,
	).Scan(&existingRef, &existingUnits)
	if err == nil {
		if existingRef != snapshotRef {
			return s.commitProjectionConflict(ctx, tx, sourceID, fmt.Errorf("%w: revision bytes changed", ErrConflict))
		}
		if err := tx.Commit(ctx); err != nil {
			return ApplyResult{}, fmt.Errorf("commit duplicate directory snapshot: %w", err)
		}
		return ApplyResult{
			Result: ApplyResultDuplicate, SnapshotRef: snapshotRef,
			SourceRevision: snapshot.SourceRevision, UnitCount: existingUnits,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ApplyResult{}, fmt.Errorf("check directory snapshot replay: %w", err)
	}

	if strings.TrimSpace(snapshot.PreviousRevision) != currentRevision {
		return s.commitProjectionConflict(ctx, tx, sourceID, fmt.Errorf(
			"%w: current=%q previous=%q", ErrStaleRevision, currentRevision, snapshot.PreviousRevision,
		))
	}

	materialized := make(map[string]OrganizationUnitProjection)
	if snapshot.Kind == SnapshotDelta {
		if currentSnapshotRef == "" {
			return s.commitProjectionConflict(ctx, tx, sourceID, fmt.Errorf("%w: delta has no base snapshot", ErrStaleRevision))
		}
		materialized, err = loadMaterializedUnits(ctx, tx, snapshot.OrgID, sourceID, currentSnapshotRef)
		if err != nil {
			return ApplyResult{}, err
		}
	}
	for _, unit := range snapshot.Units {
		if unit.Tombstone {
			delete(materialized, unit.UnitRef)
			continue
		}
		materialized[unit.UnitRef] = unit
	}
	if err := validateMaterializedHierarchy(materialized); err != nil {
		return s.commitProjectionConflict(ctx, tx, sourceID, err)
	}

	var snapshotID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO enterprise_directory_snapshots (
		  snapshot_ref, org_id, source_id, kind, source_revision,
		  previous_revision, observed_at, gaps
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id`,
		snapshotRef, snapshot.OrgID, sourceID, snapshot.Kind,
		snapshot.SourceRevision, strings.TrimSpace(snapshot.PreviousRevision),
		snapshot.ObservedAt, gapsJSON,
	).Scan(&snapshotID)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("insert directory snapshot: %w", err)
	}

	unitRefs := make([]string, 0, len(materialized))
	for unitRef := range materialized {
		unitRefs = append(unitRefs, unitRef)
	}
	sort.Strings(unitRefs)
	for _, unitRef := range unitRefs {
		unit := materialized[unitRef]
		if _, err := tx.Exec(ctx, `
			INSERT INTO enterprise_directory_unit_snapshots (
			  snapshot_id, org_id, source_id, unit_ref, external_ref,
			  parent_unit_ref, kind, name, source_revision, valid_from, valid_to
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			snapshotID, snapshot.OrgID, sourceID, unit.UnitRef, unit.ExternalRef,
			unit.ParentUnitRef, unit.Kind, unit.Name, unit.SourceRevision,
			unit.Effective.ValidFrom, unit.Effective.ValidTo,
		); err != nil {
			return ApplyResult{}, fmt.Errorf("insert directory unit %q: %w", unitRef, err)
		}
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
		snapshotRef, snapshot.SourceRevision, snapshot.ObservedAt,
		gapsJSON, sourceID, snapshot.OrgID,
	); err != nil {
		return ApplyResult{}, fmt.Errorf("advance directory source: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplyResult{}, fmt.Errorf("commit directory snapshot: %w", err)
	}
	return ApplyResult{
		Result: ApplyResultApplied, SnapshotRef: snapshotRef,
		SourceRevision: snapshot.SourceRevision, UnitCount: len(materialized),
	}, nil
}

func (s *PostgresStore) commitProjectionConflict(
	ctx context.Context,
	tx pgx.Tx,
	sourceID int64,
	conflict error,
) (ApplyResult, error) {
	if _, err := tx.Exec(ctx, `
		UPDATE enterprise_directory_sources
		SET has_conflict = TRUE, reconciled_at = NULL, updated_at = NOW()
		WHERE id = $1`, sourceID); err != nil {
		return ApplyResult{}, fmt.Errorf("record directory conflict: %v: %w", conflict, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplyResult{}, fmt.Errorf("commit directory conflict: %v: %w", conflict, err)
	}
	return ApplyResult{}, conflict
}

func (s *PostgresStore) ListSources(
	ctx context.Context,
	orgID int64,
	now time.Time,
	maxAge time.Duration,
) ([]SourceState, error) {
	if orgID <= 0 || now.IsZero() || maxAge <= 0 {
		return nil, invalid("source_list")
	}
	rows, err := s.db.Query(ctx, `
		SELECT source_ref, authority, connection, current_revision,
		       observed_at, reconciled_at, has_conflict, gaps
		FROM enterprise_directory_sources
		WHERE org_id = $1
		ORDER BY authority, source_ref`, orgID)
	if err != nil {
		return nil, fmt.Errorf("list directory sources: %w", err)
	}
	defer rows.Close()

	states := make([]SourceState, 0)
	for rows.Next() {
		var observation SourceObservation
		var observedAt, reconciledAt *time.Time
		var gapsJSON []byte
		observation.ContractVersion = ContractVersion
		observation.OrgID = orgID
		if err := rows.Scan(
			&observation.SourceRef, &observation.Authority, &observation.Connection,
			&observation.SourceRevision, &observedAt, &reconciledAt,
			&observation.HasConflict, &gapsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan directory source: %w", err)
		}
		if observedAt != nil {
			observation.ObservedAt = observedAt.UTC()
		}
		if reconciledAt != nil {
			observation.ReconciledAt = reconciledAt.UTC()
		}
		if err := json.Unmarshal(gapsJSON, &observation.Gaps); err != nil {
			return nil, fmt.Errorf("decode directory gaps: %w", err)
		}
		health, err := EvaluateHealth(observation, now.UTC(), maxAge)
		if err != nil {
			return nil, err
		}
		states = append(states, SourceState{Observation: observation, Health: health})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate directory sources: %w", err)
	}
	return states, nil
}

func (s *PostgresStore) ListOrganizationTree(
	ctx context.Context,
	orgID int64,
	sourceRef string,
	effectiveAt time.Time,
) ([]OrganizationUnitProjection, error) {
	if orgID <= 0 || strings.TrimSpace(sourceRef) == "" || !validObservedTime(effectiveAt) {
		return nil, invalid("organization_tree_query")
	}
	rows, err := s.db.Query(ctx, `
		SELECT u.unit_ref, u.external_ref, u.parent_unit_ref, u.kind, u.name,
		       u.source_revision, u.valid_from, u.valid_to
		FROM enterprise_directory_sources s
		JOIN enterprise_directory_snapshots snap
		  ON snap.org_id = s.org_id
		 AND snap.source_id = s.id
		 AND snap.snapshot_ref = s.current_snapshot_ref
		JOIN enterprise_directory_unit_snapshots u
		  ON u.org_id = snap.org_id
		 AND u.source_id = snap.source_id
		 AND u.snapshot_id = snap.id
		WHERE s.org_id = $1
		  AND s.source_ref = $2
		  AND u.valid_from <= $3
		  AND (u.valid_to IS NULL OR u.valid_to > $3)
		ORDER BY u.kind, u.name, u.unit_ref`,
		orgID, strings.TrimSpace(sourceRef), effectiveAt,
	)
	if err != nil {
		return nil, fmt.Errorf("list organization tree: %w", err)
	}
	defer rows.Close()

	units := make([]OrganizationUnitProjection, 0)
	for rows.Next() {
		var unit OrganizationUnitProjection
		var validTo *time.Time
		if err := rows.Scan(
			&unit.UnitRef, &unit.ExternalRef, &unit.ParentUnitRef,
			&unit.Kind, &unit.Name, &unit.SourceRevision,
			&unit.Effective.ValidFrom, &validTo,
		); err != nil {
			return nil, fmt.Errorf("scan organization tree: %w", err)
		}
		unit.Effective.ValidFrom = unit.Effective.ValidFrom.UTC()
		if validTo != nil {
			value := validTo.UTC()
			unit.Effective.ValidTo = &value
		}
		units = append(units, unit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate organization tree: %w", err)
	}
	return units, nil
}

func loadMaterializedUnits(
	ctx context.Context,
	tx pgx.Tx,
	orgID, sourceID int64,
	snapshotRef string,
) (map[string]OrganizationUnitProjection, error) {
	rows, err := tx.Query(ctx, `
		SELECT u.unit_ref, u.external_ref, u.parent_unit_ref, u.kind, u.name,
		       u.source_revision, u.valid_from, u.valid_to
		FROM enterprise_directory_snapshots s
		JOIN enterprise_directory_unit_snapshots u
		  ON u.snapshot_id = s.id AND u.org_id = s.org_id AND u.source_id = s.source_id
		WHERE s.org_id = $1 AND s.source_id = $2 AND s.snapshot_ref = $3`,
		orgID, sourceID, snapshotRef,
	)
	if err != nil {
		return nil, fmt.Errorf("load materialized organization tree: %w", err)
	}
	defer rows.Close()
	materialized := make(map[string]OrganizationUnitProjection)
	for rows.Next() {
		var unit OrganizationUnitProjection
		var validTo *time.Time
		if err := rows.Scan(
			&unit.UnitRef, &unit.ExternalRef, &unit.ParentUnitRef,
			&unit.Kind, &unit.Name, &unit.SourceRevision,
			&unit.Effective.ValidFrom, &validTo,
		); err != nil {
			return nil, fmt.Errorf("scan materialized organization tree: %w", err)
		}
		unit.Effective.ValidFrom = unit.Effective.ValidFrom.UTC()
		if validTo != nil {
			value := validTo.UTC()
			unit.Effective.ValidTo = &value
		}
		materialized[unit.UnitRef] = unit
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate materialized organization tree: %w", err)
	}
	return materialized, nil
}

func validateMaterializedHierarchy(units map[string]OrganizationUnitProjection) error {
	for unitRef, unit := range units {
		if unit.Tombstone {
			return fmt.Errorf("%w: materialized tombstone %q", ErrConflict, unitRef)
		}
		if unit.ParentUnitRef != "" {
			if _, ok := units[unit.ParentUnitRef]; !ok {
				return fmt.Errorf("%w: parent %q for unit %q is missing", ErrConflict, unit.ParentUnitRef, unitRef)
			}
		}
	}
	state := make(map[string]uint8, len(units))
	var visit func(string) error
	visit = func(unitRef string) error {
		switch state[unitRef] {
		case 1:
			return fmt.Errorf("%w: organization hierarchy cycle at %q", ErrConflict, unitRef)
		case 2:
			return nil
		}
		state[unitRef] = 1
		parent := units[unitRef].ParentUnitRef
		if parent != "" {
			if err := visit(parent); err != nil {
				return err
			}
		}
		state[unitRef] = 2
		return nil
	}
	for unitRef := range units {
		if err := visit(unitRef); err != nil {
			return err
		}
	}
	return nil
}
