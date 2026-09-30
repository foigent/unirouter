package contentcapture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CapturePrincipal is copied only from the already authenticated API key.
// The database row is rechecked under shared locks before a scoped policy is used.
type CapturePrincipal struct {
	APIKeyID    int64
	KeyType     string
	OwnerUserID int64
}

type ScopedPolicySnapshot struct {
	Scope                string        `json:"scope"`
	PolicyRef            string        `json:"policy_ref"`
	Effective            PolicyVersion `json:"effective"`
	EffectiveAt          string        `json:"effective_at"`
	ExecutionStorageMode string        `json:"execution_storage_mode"`
	ProjectQuotaBytes    int64         `json:"project_quota_bytes"`
	// Vector is an internal, deterministic snapshot for Begin/Reserve CAS.
	Vector string `json:"version_vector"`
}

type scopedVersionVector struct {
	Enterprise        int64 `json:"enterprise"`
	Project           int64 `json:"project"`
	ProjectExplicit   bool  `json:"project_explicit"`
	User              int64 `json:"user"`
	UserID            int64 `json:"user_id"`
	ProjectQuotaBytes int64 `json:"project_quota_bytes"`
}

func (s *PolicyStore) scopedSchemaActive(ctx context.Context) (bool, error) {
	// Production policy stores use a pool. Synthetic DBQuerier unit fixtures
	// model the pre-expansion policy row and have no schema ledger.
	if _, ok := s.db.(*pgxpool.Pool); !ok {
		return false, nil
	}
	var active bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version='000122_scoped_content_policy')`).Scan(&active)
	return active, err
}

// ViewForCapture keeps the M121 project-only path valid before the compatible
// expansion. Once M122 is present, missing authenticated key identity fails
// closed rather than silently bypassing enterprise or personal exceptions.
func (s *PolicyStore) ViewForCapture(ctx context.Context, principal CapturePrincipal, orgID, projectID int64, storageStatus string) (PolicyView, *ScopedPolicySnapshot, error) {
	active, err := s.scopedSchemaActive(ctx)
	if err != nil {
		return PolicyView{}, nil, err
	}
	if !active {
		view, err := s.Get(ctx, orgID, projectID, storageStatus)
		return view, nil, err
	}
	snapshot, err := s.EffectiveScopedForCapture(ctx, principal, orgID, projectID, storageStatus)
	if err != nil {
		return PolicyView{}, nil, err
	}
	view := PolicyView{SchemaVersion: PolicyViewSchema, SourceInstance: s.options.SourceInstance,
		OrgID: strconv.FormatInt(orgID, 10), ProjectID: strconv.FormatInt(projectID, 10), PolicyRef: snapshot.PolicyRef,
		Desired: snapshot.Effective, Effective: snapshot.Effective, EffectiveAt: snapshot.EffectiveAt,
		ApplicationStatus: "applied", ApplicationReason: "none", StorageStatus: storageStatus, Limits: s.options.Limits}
	if err := view.Validate(); err != nil {
		return PolicyView{}, nil, err
	}
	return view, &snapshot, nil
}

// EffectiveScopedForCapture is a short, read-only transactional observation.
// Reserve must independently recheck the same principal and exact vector while
// holding its project quota row, before allocating any body storage.
func (s *PolicyStore) EffectiveScopedForCapture(ctx context.Context, principal CapturePrincipal, orgID, projectID int64, storageStatus string) (ScopedPolicySnapshot, error) {
	if s == nil || principal.APIKeyID <= 0 {
		return ScopedPolicySnapshot{}, invalidPolicy("authenticated API key is required")
	}
	if err := s.validateTarget(orgID, projectID, storageStatus); err != nil {
		return ScopedPolicySnapshot{}, err
	}
	pool, ok := s.db.(*pgxpool.Pool)
	if !ok || pool == nil {
		return ScopedPolicySnapshot{}, ErrReservationTransaction
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return ScopedPolicySnapshot{}, err
	}
	defer tx.Rollback(context.Background())
	if err := lockCapturePrincipal(ctx, tx, principal, orgID, projectID); err != nil {
		return ScopedPolicySnapshot{}, err
	}
	result, err := s.readScopedEffectiveTx(ctx, tx, principal, orgID, projectID, storageStatus, false, false)
	if err != nil {
		return ScopedPolicySnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ScopedPolicySnapshot{}, err
	}
	return result, nil
}

// ExecutionStorageForCommitTx resolves the storage constraint in the same
// transaction as a durable async-media admission. The caller must hold its
// authenticated access binding until commit. Review-content deployment gates
// do not govern execution-required temporary storage, but the principal and
// all policy layers are still locked and rechecked here.
func (s *PolicyStore) ExecutionStorageForCommitTx(ctx context.Context, tx pgx.Tx, principal CapturePrincipal, orgID, projectID int64) (ScopedPolicySnapshot, error) {
	if s == nil || tx == nil || principal.APIKeyID <= 0 {
		return ScopedPolicySnapshot{}, ErrPolicyInactive
	}
	if err := lockCapturePrincipal(ctx, tx, principal, orgID, projectID); err != nil {
		return ScopedPolicySnapshot{}, err
	}
	return s.readScopedEffectiveTx(ctx, tx, principal, orgID, projectID, "unavailable", false, true)
}

// Caller locks the authenticated principal before the content stream. FOR SHARE
// blocks revocation/status changes but permits concurrent captures to read.
func lockCapturePrincipal(ctx context.Context, tx pgx.Tx, principal CapturePrincipal, orgID, projectID int64) error {
	if principal.APIKeyID <= 0 || orgID <= 0 || projectID <= 0 {
		return ErrPolicyInactive
	}
	var keyType, keyStatus, keyPurpose string
	var ownerID, keyOrgID, keyProjectID int64
	// This first read only locates the potential owner. It grants nothing.
	err := tx.QueryRow(ctx, `SELECT org_id,COALESCE(project_id,0),COALESCE(owner_user_id,0),
      COALESCE(NULLIF(key_type,''),'shared') FROM api_keys WHERE id=$1`, principal.APIKeyID).Scan(&keyOrgID, &keyProjectID, &ownerID, &keyType)
	if err != nil {
		return ErrPolicyInactive
	}
	if keyOrgID != orgID || keyProjectID != projectID || keyType != principal.KeyType || ownerID != principal.OwnerUserID {
		return ErrPolicyInactive
	}
	if keyType == "personal" {
		var userStatus, role string
		err = tx.QueryRow(ctx, `SELECT status,organization_role FROM users WHERE id=$1 AND org_id=$2 FOR SHARE`, ownerID, orgID).Scan(&userStatus, &role)
		if err != nil || userStatus != "active" || role == "org_viewer" {
			return ErrPolicyInactive
		}
	}
	var orgStatus, projectStatus string
	if err = tx.QueryRow(ctx, `SELECT status FROM orgs WHERE id=$1 FOR SHARE`, orgID).Scan(&orgStatus); err != nil || orgStatus != "active" {
		return ErrPolicyInactive
	}
	if err = tx.QueryRow(ctx, `SELECT status FROM projects WHERE id=$1 AND org_id=$2 FOR SHARE`, projectID, orgID).Scan(&projectStatus); err != nil || projectStatus != "active" {
		return ErrPolicyInactive
	}
	if keyType == "personal" {
		var membership string
		err = tx.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2 FOR SHARE`, projectID, ownerID).Scan(&membership)
		if err != nil || (membership != "project_admin" && membership != "project_user" && membership != "project_developer" && membership != "developer") {
			return ErrPolicyInactive
		}
	}
	var currentOwner, currentOrg, currentProject int64
	err = tx.QueryRow(ctx, `SELECT org_id,COALESCE(project_id,0),COALESCE(owner_user_id,0),
      COALESCE(NULLIF(key_type,''),'shared'),status,COALESCE(purpose,'')
      FROM api_keys WHERE id=$1 FOR SHARE`, principal.APIKeyID).Scan(&currentOrg, &currentProject, &currentOwner, &keyType, &keyStatus, &keyPurpose)
	if err != nil || currentOrg != orgID || currentProject != projectID || currentOwner != ownerID || keyType != principal.KeyType || keyStatus != "active" || keyPurpose != "" {
		return ErrPolicyInactive
	}
	return nil
}

func (s *PolicyStore) readScopedEffectiveTx(ctx context.Context, tx pgx.Tx, principal CapturePrincipal, orgID, projectID int64, storageStatus string, lockQuota, managementPreview bool) (ScopedPolicySnapshot, error) {
	input := ScopedPolicyInput{
		Deployment: clonePolicyConfig(s.defaultConfig), KeyType: principal.KeyType, OwnerUserID: principal.OwnerUserID,
		FullContentEnabled: s.options.FullContentEnabled, StorageReady: storageStatus == "ready",
		SupportedProtocols: s.options.SupportedProtocols, Limits: s.options.Limits,
		IgnoreDeploymentGate: managementPreview,
	}
	vector := scopedVersionVector{}
	var enterpriseBytes []byte
	var enterpriseMode string
	var enterpriseAt time.Time
	var enterpriseQuota int64
	err := tx.QueryRow(ctx, `SELECT effective_version,effective_config,effective_execution_storage_mode,
      effective_default_project_quota_bytes,effective_at FROM execution_content_enterprise_policies
      WHERE org_id=$1 FOR SHARE`, orgID).Scan(&vector.Enterprise, &enterpriseBytes, &enterpriseMode, &enterpriseQuota, &enterpriseAt)
	if err == nil {
		config, err := DecodePolicyConfig(enterpriseBytes)
		if err != nil {
			return ScopedPolicySnapshot{}, err
		}
		input.Enterprise, input.EnterpriseExecutionStorageMode = &config, enterpriseMode
		input.ProjectQuotaBytes = enterpriseQuota
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ScopedPolicySnapshot{}, err
	}

	var projectBytes []byte
	var projectMode string
	var projectAt time.Time
	var projectQuota *int64
	lock := "FOR SHARE"
	if lockQuota {
		lock = "FOR UPDATE"
	}
	err = tx.QueryRow(ctx, `SELECT effective_version,effective_config,explicit_override,effective_execution_storage_mode,
      project_quota_bytes,effective_at FROM execution_content_policies
      WHERE org_id=$1 AND project_id=$2 `+lock, orgID, projectID).Scan(&vector.Project, &projectBytes, &vector.ProjectExplicit, &projectMode, &projectQuota, &projectAt)
	if err == nil {
		if vector.ProjectExplicit {
			config, err := DecodePolicyConfig(projectBytes)
			if err != nil {
				return ScopedPolicySnapshot{}, err
			}
			input.Project, input.ProjectExecutionStorageMode = &config, projectMode
			if config.QuotaBytes != nil {
				input.ProjectQuotaBytes = *config.QuotaBytes
			}
		}
		if projectQuota != nil {
			input.ProjectQuotaBytes = *projectQuota
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ScopedPolicySnapshot{}, err
	} else {
		// Reserve materializes the inherited project quota ledger at version 1.
		// Give the absent row the same logical vector so first capture does not
		// conflict with its own ledger creation. A concurrent explicit PUT still
		// changes the vector and is rejected on Reserve's transactional reread.
		vector.Project = 1
	}

	var userAt time.Time
	if principal.KeyType == "personal" {
		var userBytes []byte
		var userMode string
		err = tx.QueryRow(ctx, `SELECT effective_version,effective_config,effective_execution_storage_mode,effective_at
          FROM execution_content_user_policies WHERE org_id=$1 AND project_id=$2 AND user_id=$3 FOR SHARE`,
			orgID, projectID, principal.OwnerUserID).Scan(&vector.User, &userBytes, &userMode, &userAt)
		if err == nil {
			var config PolicyConfig
			if len(userBytes) > maxStoredPolicyConfigBytes || json.Unmarshal(userBytes, &config) != nil || config.QuotaBytes != nil {
				return ScopedPolicySnapshot{}, invalidPolicy("invalid stored user exception")
			}
			input.User = &UserPolicyException{UserID: principal.OwnerUserID, ExecutionStorageMode: userMode, Mode: config.Mode,
				SupportedProtocols: config.SupportedProtocols, ObservationPoints: config.ObservationPoints,
				RetentionSeconds: config.RetentionSeconds, MaxSavedBytes: config.MaxSavedBytes,
				AllowedPurposes: config.AllowedPurposes, OnLimit: config.OnLimit}
			vector.UserID = principal.OwnerUserID
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return ScopedPolicySnapshot{}, err
		}
	}
	resolved, err := ResolveScopedEffective(input)
	if err != nil {
		return ScopedPolicySnapshot{}, err
	}
	var ref, version, effectiveAt string
	switch resolved.Scope {
	case PolicyScopeEnterprise:
		ref, version, effectiveAt = fmt.Sprintf("content-policy:%d:enterprise", orgID), strconv.FormatInt(vector.Enterprise, 10), enterpriseAt.UTC().Format("2006-01-02T15:04:05.000000Z")
	case PolicyScopeProject:
		ref, version, effectiveAt = fmt.Sprintf("content-policy:%d:%d", orgID, projectID), strconv.FormatInt(vector.Project, 10), projectAt.UTC().Format("2006-01-02T15:04:05.000000Z")
	case PolicyScopeUser:
		ref, version, effectiveAt = fmt.Sprintf("content-policy:%d:%d:user:%d", orgID, projectID, principal.OwnerUserID), strconv.FormatInt(vector.User, 10), userAt.UTC().Format("2006-01-02T15:04:05.000000Z")
	default:
		var createdAt time.Time
		if err := tx.QueryRow(ctx, `SELECT created_at FROM projects WHERE id=$1 AND org_id=$2`, projectID, orgID).Scan(&createdAt); err != nil {
			return ScopedPolicySnapshot{}, err
		}
		ref, version, effectiveAt = fmt.Sprintf("content-policy:%d:%d", orgID, projectID), "1", createdAt.UTC().Format("2006-01-02T15:04:05.000000Z")
	}
	vector.ProjectQuotaBytes = input.ProjectQuotaBytes
	wire, err := json.Marshal(vector)
	if err != nil {
		return ScopedPolicySnapshot{}, err
	}
	return ScopedPolicySnapshot{Scope: resolved.Scope, PolicyRef: ref, Effective: PolicyVersion{PolicyVersion: version, Config: resolved.Config},
		EffectiveAt: effectiveAt, ExecutionStorageMode: resolved.ExecutionStorageMode, ProjectQuotaBytes: input.ProjectQuotaBytes, Vector: string(wire)}, nil
}
