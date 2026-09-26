package contentcapture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
)

const maxStoredPolicyConfigBytes = 16384

var (
	ErrPolicyProjectNotFound = errors.New("content policy project not found")
	ErrPolicyInactive        = errors.New("content capture policy is not active")
)

type PolicyStoreOptions struct {
	SourceInstance string
	// DefaultMode reflects the existing deployment mode. An explicit global
	// disabled value remains a stop even if a stored project policy says full.
	DefaultMode        string
	SupportedProtocols []string
	ObservationPoints  []string
	Limits             PolicyLimits
	// This is an explicit deployment feature gate, never inferred from a path,
	// storage identity, credential, or old desired/effective row.
	FullContentEnabled bool
}

// PolicyApplication is supplied by the validated storage dependency. This
// repository neither probes files nor infers readiness from identity strings.
// ApplyNow is meaningful for full_content; disabled/metadata apply immediately.
type PolicyApplication struct {
	StorageStatus string
	ApplyNow      bool
	Now           time.Time
}

type PolicyStore struct {
	db            db.DBQuerier
	options       PolicyStoreOptions
	defaultConfig PolicyConfig
}

func NewPolicyStore(database db.DBQuerier, options PolicyStoreOptions) (*PolicyStore, error) {
	if database == nil || !policyText(options.SourceInstance) {
		return nil, invalidPolicy("policy store database and source identity are required")
	}
	if options.DefaultMode != ModeDisabled && options.DefaultMode != ModeMetadataOnly {
		return nil, invalidPolicy("default mode must preserve disabled or metadata_only")
	}
	c := PolicyConfig{Mode: options.DefaultMode, SupportedProtocols: append([]string{}, options.SupportedProtocols...), ObservationPoints: append([]string{}, options.ObservationPoints...), AllowedPurposes: []string{}}
	if err := c.ValidateSupport(options.SupportedProtocols, options.Limits); err != nil {
		return nil, err
	}
	if _, err := encodeStoredPolicyConfig(c); err != nil {
		return nil, err
	}
	options.SupportedProtocols = append([]string{}, options.SupportedProtocols...)
	options.ObservationPoints = append([]string{}, options.ObservationPoints...)
	return &PolicyStore{db: database, options: options, defaultConfig: c}, nil
}

// Get is read-only. The caller must have already checked project management or
// internal capture authority. Both persisted versions are returned faithfully;
// a GET response by itself is not runtime permission to capture body bytes.
func (s *PolicyStore) Get(ctx context.Context, orgID, projectID int64, storageStatus string) (PolicyView, error) {
	if s == nil {
		return PolicyView{}, invalidPolicy("policy store is required")
	}
	return s.get(ctx, s.db, orgID, projectID, storageStatus)
}

// GetTx keeps management reads in the transaction holding the original
// project authorization locks. It never creates a default policy row.
func (s *PolicyStore) GetTx(ctx context.Context, tx pgx.Tx, orgID, projectID int64, storageStatus string) (PolicyView, error) {
	if tx == nil {
		return PolicyView{}, invalidPolicy("authorized transaction is required")
	}
	view, err := s.get(ctx, tx, orgID, projectID, storageStatus)
	if err != nil {
		return PolicyView{}, err
	}
	// Do not report a stored policy as operational after deployment capture
	// has been stopped. Preserve the stored state; GET must not rewrite it.
	if s.options.DefaultMode == ModeDisabled && view.Effective.Config.Mode != ModeDisabled ||
		view.Effective.Config.Mode == ModeFullContent && !s.options.FullContentEnabled {
		return PolicyView{}, ErrPolicyInactive
	}
	return view, nil
}

func (s *PolicyStore) get(ctx context.Context, query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, orgID, projectID int64, storageStatus string) (PolicyView, error) {
	if err := s.validateTarget(orgID, projectID, storageStatus); err != nil {
		return PolicyView{}, err
	}
	var row storedPolicyRow
	err := query.QueryRow(ctx, `SELECT p.created_at,c.desired_version,c.effective_version,c.desired_config,c.effective_config,c.effective_at,c.application_status,c.application_reason
FROM projects p LEFT JOIN execution_content_policies c ON c.org_id=p.org_id AND c.project_id=p.id
WHERE p.org_id=$1 AND p.id=$2`, orgID, projectID).Scan(&row.projectCreatedAt, &row.desiredVersion, &row.effectiveVersion, &row.desiredConfig, &row.effectiveConfig, &row.effectiveAt, &row.applicationStatus, &row.applicationReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return PolicyView{}, ErrPolicyProjectNotFound
	}
	if err != nil {
		return PolicyView{}, err
	}
	return s.rowView(orgID, projectID, storageStatus, row)
}

// EffectiveForCapture reads current effective state at capture start. It never
// activates pending desired state or caches an earlier effective snapshot. The
// returned version/time must remain bound to that capture after this call.
func (s *PolicyStore) EffectiveForCapture(ctx context.Context, orgID, projectID int64, storageStatus string) (PolicyView, error) {
	view, err := s.Get(ctx, orgID, projectID, storageStatus)
	if err != nil {
		return PolicyView{}, err
	}
	if s.options.DefaultMode == ModeDisabled || view.Effective.Config.Mode == ModeDisabled {
		return PolicyView{}, ErrPolicyInactive
	}
	if view.Effective.Config.Mode == ModeFullContent {
		if !s.options.FullContentEnabled || storageStatus != "ready" {
			return PolicyView{}, ErrPolicyInactive
		}
		if err := s.validateSupportedConfig(view.Effective.Config); err != nil {
			return PolicyView{}, fmt.Errorf("%w: stored effective policy is unsupported", ErrPolicyInactive)
		}
	}
	return view, nil
}

// PutTx requires an existing transaction in which the administrator's original
// project/organization management authority has already been rechecked. It
// does not authorize, begin, commit, roll back, or write the audit record. The
// caller must roll back on ANY error and append its audit in this transaction.
//
// Only the policy row is locked FOR UPDATE. In particular this method does not
// upgrade a project lock already held FOR SHARE by the authorization layer.
func (s *PolicyStore) PutTx(ctx context.Context, tx pgx.Tx, orgID, projectID int64, put PolicyPut, application PolicyApplication) (PolicyView, error) {
	if tx == nil {
		return PolicyView{}, invalidPolicy("authorized transaction is required")
	}
	if err := s.validateTarget(orgID, projectID, application.StorageStatus); err != nil {
		return PolicyView{}, err
	}
	if err := put.Validate(); err != nil {
		return PolicyView{}, err
	}
	if s.options.DefaultMode == ModeDisabled && put.Desired.Mode != ModeDisabled {
		return PolicyView{}, invalidPolicy("capture is stopped by deployment policy")
	}
	if application.Now.IsZero() {
		return PolicyView{}, invalidPolicy("server application time is required")
	}
	if err := s.validateSupportedConfig(put.Desired); err != nil {
		return PolicyView{}, err
	}
	if put.Desired.Mode == ModeFullContent && (!s.options.FullContentEnabled || s.options.DefaultMode == ModeDisabled) {
		return PolicyView{}, invalidPolicy("full content capture is disabled by deployment policy")
	}
	if put.Desired.Mode == ModeFullContent && application.ApplyNow && application.StorageStatus != "ready" {
		return PolicyView{}, invalidPolicy("storage has not been verified ready")
	}
	if _, err := encodeStoredPolicyConfig(put.Desired); err != nil {
		return PolicyView{}, err
	}
	var createdAt time.Time
	err := tx.QueryRow(ctx, `SELECT created_at FROM projects WHERE org_id=$1 AND id=$2`, orgID, projectID).Scan(&createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PolicyView{}, ErrPolicyProjectNotFound
	}
	if err != nil {
		return PolicyView{}, err
	}
	initial, err := s.defaultView(orgID, projectID, application.StorageStatus, createdAt)
	if err != nil {
		return PolicyView{}, err
	}
	defaultBytes, err := encodeStoredPolicyConfig(initial.Desired.Config)
	if err != nil {
		return PolicyView{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason)
VALUES($1,$2,1,1,$3,$3,$4,'applied','none') ON CONFLICT(org_id,project_id) DO NOTHING`, orgID, projectID, defaultBytes, createdAt)
	if err != nil {
		return PolicyView{}, err
	}
	row := storedPolicyRow{projectCreatedAt: createdAt}
	err = tx.QueryRow(ctx, `SELECT desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason
FROM execution_content_policies WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, orgID, projectID).Scan(&row.desiredVersion, &row.effectiveVersion, &row.desiredConfig, &row.effectiveConfig, &row.effectiveAt, &row.applicationStatus, &row.applicationReason)
	if err != nil {
		return PolicyView{}, err
	}
	current, err := s.rowView(orgID, projectID, application.StorageStatus, row)
	if err != nil {
		return PolicyView{}, err
	}
	if put.ExpectedPolicyVersion != current.Desired.PolicyVersion {
		return PolicyView{}, ErrPolicyVersionConflict
	}
	// BIGINT is the actual persistent version limit, even though the wire ID
	// grammar can express a few larger nineteen-digit values.
	if current.Desired.PolicyVersion == strconv.FormatInt(math.MaxInt64, 10) {
		return PolicyView{}, invalidPolicy("persistent policy version exhausted")
	}
	next, err := TransitionPolicy(current, put, application.ApplyNow, application.Now)
	if err != nil {
		return PolicyView{}, err
	}
	desired, err := encodeStoredPolicyConfig(next.Desired.Config)
	if err != nil {
		return PolicyView{}, err
	}
	effective, err := encodeStoredPolicyConfig(next.Effective.Config)
	if err != nil {
		return PolicyView{}, err
	}
	desiredVersion, _ := strconv.ParseInt(next.Desired.PolicyVersion, 10, 64)
	effectiveVersion, _ := strconv.ParseInt(next.Effective.PolicyVersion, 10, 64)
	effectiveAt, err := time.Parse(time.RFC3339Nano, next.EffectiveAt)
	if err != nil {
		return PolicyView{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE execution_content_policies SET desired_version=$3,effective_version=$4,desired_config=$5,effective_config=$6,effective_at=$7,application_status=$8,application_reason=$9
WHERE org_id=$1 AND project_id=$2 AND desired_version=$10`, orgID, projectID, desiredVersion, effectiveVersion, desired, effective, effectiveAt, next.ApplicationStatus, next.ApplicationReason, *row.desiredVersion)
	if err != nil {
		return PolicyView{}, err
	}
	if tag.RowsAffected() != 1 {
		return PolicyView{}, ErrPolicyVersionConflict
	}
	return next, nil
}

type storedPolicyRow struct {
	projectCreatedAt  time.Time
	desiredVersion    *int64
	effectiveVersion  *int64
	desiredConfig     []byte
	effectiveConfig   []byte
	effectiveAt       *time.Time
	applicationStatus *string
	applicationReason *string
}

func (s *PolicyStore) rowView(orgID, projectID int64, storageStatus string, row storedPolicyRow) (PolicyView, error) {
	if row.desiredVersion == nil {
		if row.effectiveVersion != nil || row.desiredConfig != nil || row.effectiveConfig != nil || row.effectiveAt != nil || row.applicationStatus != nil || row.applicationReason != nil {
			return PolicyView{}, invalidPolicy("partial stored policy row")
		}
		return s.defaultView(orgID, projectID, storageStatus, row.projectCreatedAt)
	}
	if row.effectiveVersion == nil || row.effectiveAt == nil || row.applicationStatus == nil || row.applicationReason == nil {
		return PolicyView{}, invalidPolicy("incomplete stored policy row")
	}
	if len(row.desiredConfig) > maxStoredPolicyConfigBytes || len(row.effectiveConfig) > maxStoredPolicyConfigBytes {
		return PolicyView{}, invalidPolicy("stored config exceeds schema limit")
	}
	desired, err := DecodePolicyConfig(row.desiredConfig)
	if err != nil {
		return PolicyView{}, err
	}
	effective, err := DecodePolicyConfig(row.effectiveConfig)
	if err != nil {
		return PolicyView{}, err
	}
	view := s.viewIdentity(orgID, projectID, storageStatus)
	view.Desired = PolicyVersion{PolicyVersion: strconv.FormatInt(*row.desiredVersion, 10), Config: desired}
	view.Effective = PolicyVersion{PolicyVersion: strconv.FormatInt(*row.effectiveVersion, 10), Config: effective}
	view.EffectiveAt = row.effectiveAt.UTC().Format("2006-01-02T15:04:05.000000Z")
	view.ApplicationStatus = *row.applicationStatus
	view.ApplicationReason = *row.applicationReason
	if err := view.Validate(); err != nil {
		return PolicyView{}, err
	}
	return view, nil
}
func (s *PolicyStore) defaultView(orgID, projectID int64, storageStatus string, createdAt time.Time) (PolicyView, error) {
	if createdAt.IsZero() {
		return PolicyView{}, invalidPolicy("stable project creation time is required")
	}
	view := s.viewIdentity(orgID, projectID, storageStatus)
	view.Desired = PolicyVersion{PolicyVersion: "1", Config: clonePolicyConfig(s.defaultConfig)}
	view.Effective = PolicyVersion{PolicyVersion: "1", Config: clonePolicyConfig(s.defaultConfig)}
	view.EffectiveAt = createdAt.UTC().Format("2006-01-02T15:04:05.000000Z")
	view.ApplicationStatus = "applied"
	view.ApplicationReason = "none"
	if err := view.Validate(); err != nil {
		return PolicyView{}, err
	}
	return view, nil
}
func (s *PolicyStore) viewIdentity(orgID, projectID int64, storageStatus string) PolicyView {
	return PolicyView{SchemaVersion: PolicyViewSchema, SourceInstance: s.options.SourceInstance, OrgID: strconv.FormatInt(orgID, 10), ProjectID: strconv.FormatInt(projectID, 10), PolicyRef: fmt.Sprintf("content-policy:%d:%d", orgID, projectID), StorageStatus: storageStatus, Limits: s.options.Limits}
}
func (s *PolicyStore) validateTarget(orgID, projectID int64, storageStatus string) error {
	if s == nil || s.db == nil || orgID <= 0 || projectID <= 0 {
		return invalidPolicy("policy store and exact project target are required")
	}
	switch storageStatus {
	case "ready", "not_configured", "unavailable", "capacity_exhausted":
		return nil
	default:
		return invalidPolicy("verified storage status is required")
	}
}
func (s *PolicyStore) validateSupportedConfig(c PolicyConfig) error {
	if err := c.ValidateSupport(s.options.SupportedProtocols, s.options.Limits); err != nil {
		return err
	}
	for _, point := range c.ObservationPoints {
		found := false
		for _, supported := range s.options.ObservationPoints {
			if point == supported {
				found = true
				break
			}
		}
		if !found {
			return invalidPolicy("observation point is unsupported by this deployment")
		}
	}
	return nil
}
func encodeStoredPolicyConfig(c PolicyConfig) ([]byte, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if len(data) > maxStoredPolicyConfigBytes {
		return nil, invalidPolicy("config exceeds persistent schema limit")
	}
	return data, nil
}
