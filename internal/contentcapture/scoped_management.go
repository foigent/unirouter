package contentcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

const ScopedPolicyManagementSchema = "unirouter.payload-policy.v2"

// ScopedPolicyLayer describes one stored authority. An inherited project quota
// ledger is deliberately not reported as an explicit project override.
type ScopedPolicyLayer struct {
	PolicyRef                    string         `json:"policy_ref"`
	Explicit                     bool           `json:"explicit"`
	Desired                      *PolicyVersion `json:"desired,omitempty"`
	Effective                    *PolicyVersion `json:"effective,omitempty"`
	DesiredExecutionMode         string         `json:"desired_execution_storage_mode,omitempty"`
	EffectiveExecutionMode       string         `json:"effective_execution_storage_mode,omitempty"`
	EffectiveAt                  string         `json:"effective_at,omitempty"`
	ApplicationStatus            string         `json:"application_status,omitempty"`
	ApplicationReason            string         `json:"application_reason,omitempty"`
	ProjectQuotaBytes            *int64         `json:"project_quota_bytes,omitempty"`
	DesiredDefaultProjectQuota   *int64         `json:"desired_default_project_quota_bytes,omitempty"`
	EffectiveDefaultProjectQuota *int64         `json:"effective_default_project_quota_bytes,omitempty"`
}

type ScopedManagementView struct {
	SchemaVersion      string                `json:"schema_version"`
	SourceInstance     string                `json:"source_instance"`
	OrgID              string                `json:"org_id"`
	ProjectID          string                `json:"project_id,omitempty"`
	UserID             string                `json:"user_id,omitempty"`
	Enterprise         ScopedPolicyLayer     `json:"enterprise"`
	Project            *ScopedPolicyLayer    `json:"project,omitempty"`
	User               *ScopedPolicyLayer    `json:"user,omitempty"`
	Effective          *ScopedPolicySnapshot `json:"effective,omitempty"`
	VersionVector      string                `json:"version_vector"`
	StorageStatus      string                `json:"storage_status"`
	DeploymentMode     string                `json:"deployment_mode"`
	FullContentEnabled bool                  `json:"full_content_enabled"`
	Limits             PolicyLimits          `json:"limits"`
	PolicyCapabilities *PolicyCapabilities   `json:"policy_capabilities,omitempty"`
}

// PolicyCapabilities describes what this source can accept in a policy write.
// It is neither an active policy nor a default selection.
type PolicyCapabilities struct {
	SupportedProtocols []string `json:"supported_protocols"`
	ObservationPoints  []string `json:"observation_points"`
}

func (s *PolicyStore) policyCapabilities() *PolicyCapabilities {
	return &PolicyCapabilities{
		SupportedProtocols: append([]string{}, s.options.SupportedProtocols...),
		ObservationPoints:  append([]string{}, s.options.ObservationPoints...),
	}
}

// ScopedManagementPut binds a write both to its own desired-version CAS and
// to the complete observed effective path. For an absent layer, version 0 and
// the parent vector make a concurrent inherited change a 409.
type ScopedManagementPut struct {
	SchemaVersion        string       `json:"schema_version"`
	ExpectedPolicyRef    string       `json:"expected_policy_ref"`
	ExpectedVersion      string       `json:"expected_version"`
	ExpectedVector       string       `json:"expected_vector"`
	Desired              PolicyConfig `json:"desired"`
	ExecutionStorageMode string       `json:"execution_storage_mode"`
	ProjectQuotaBytes    *int64       `json:"project_quota_bytes,omitempty"`
}

func DecodeScopedManagementPut(body []byte) (ScopedManagementPut, error) {
	var put ScopedManagementPut
	if len(body) == 0 || len(body) > MaxPolicyJSONBytes {
		return put, invalidPolicy("invalid scoped policy JSON size")
	}
	if eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{RejectNull: true}) != nil {
		return put, invalidPolicy("invalid scoped policy JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&put); err != nil {
		return put, invalidPolicy("invalid scoped policy update")
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return put, invalidPolicy("trailing scoped policy JSON")
	}
	if put.SchemaVersion != ScopedPolicyManagementSchema || put.ExpectedPolicyRef == "" ||
		put.ExpectedVersion == "" || put.ExpectedVector == "" {
		return put, invalidPolicy("missing scoped CAS identity")
	}
	if put.ExecutionStorageMode != ExecutionStorageTemporary && put.ExecutionStorageMode != ExecutionStorageStrict {
		return put, invalidPolicy("unsupported execution storage mode")
	}
	if put.ExecutionStorageMode == ExecutionStorageStrict && put.Desired.Mode == ModeFullContent {
		return put, invalidPolicy("full review content conflicts with strict no-persist")
	}
	if put.ProjectQuotaBytes != nil && (*put.ProjectQuotaBytes <= 0 || *put.ProjectQuotaBytes > maxPolicyInteger) {
		return put, invalidPolicy("invalid project quota")
	}
	if put.ExpectedVersion != "0" {
		if _, err := policyID(put.ExpectedVersion); err != nil {
			return put, err
		}
	}
	return put, nil
}

func enterprisePolicyRef(orgID int64) string {
	return fmt.Sprintf("content-policy:%d:enterprise", orgID)
}
func projectPolicyRef(orgID, projectID int64) string {
	return fmt.Sprintf("content-policy:%d:%d", orgID, projectID)
}
func userPolicyRef(orgID, projectID, userID int64) string {
	return fmt.Sprintf("content-policy:%d:%d:user:%d", orgID, projectID, userID)
}

func (s *PolicyStore) scopedLayerTx(ctx context.Context, tx pgx.Tx, scope string, orgID, projectID, userID int64) (ScopedPolicyLayer, error) {
	var layer ScopedPolicyLayer
	var desired, effective []byte
	var desiredVersion, effectiveVersion int64
	var at time.Time
	var desiredMode, effectiveMode string
	var status, reason string
	var err error
	switch scope {
	case PolicyScopeEnterprise:
		layer.PolicyRef = enterprisePolicyRef(orgID)
		var desiredQuota, effectiveQuota int64
		err = tx.QueryRow(ctx, `SELECT desired_version,effective_version,desired_config,effective_config,
      desired_execution_storage_mode,effective_execution_storage_mode,desired_default_project_quota_bytes,
      effective_default_project_quota_bytes,effective_at,application_status,application_reason
      FROM execution_content_enterprise_policies WHERE org_id=$1 FOR SHARE`, orgID).
			Scan(&desiredVersion, &effectiveVersion, &desired, &effective, &desiredMode, &effectiveMode,
				&desiredQuota, &effectiveQuota, &at, &status, &reason)
		if err == nil {
			layer.DesiredDefaultProjectQuota, layer.EffectiveDefaultProjectQuota = &desiredQuota, &effectiveQuota
		}
	case PolicyScopeProject:
		layer.PolicyRef = projectPolicyRef(orgID, projectID)
		var explicit bool
		err = tx.QueryRow(ctx, `SELECT desired_version,effective_version,desired_config,effective_config,
      desired_execution_storage_mode,effective_execution_storage_mode,project_quota_bytes,
      explicit_override,effective_at,application_status,application_reason
      FROM execution_content_policies WHERE org_id=$1 AND project_id=$2 FOR SHARE`, orgID, projectID).
			Scan(&desiredVersion, &effectiveVersion, &desired, &effective, &desiredMode, &effectiveMode,
				&layer.ProjectQuotaBytes, &explicit, &at, &status, &reason)
		if err == nil && !explicit {
			return layer, nil
		}
	case PolicyScopeUser:
		layer.PolicyRef = userPolicyRef(orgID, projectID, userID)
		err = tx.QueryRow(ctx, `SELECT desired_version,effective_version,desired_config,effective_config,
      desired_execution_storage_mode,effective_execution_storage_mode,effective_at,application_status,application_reason
      FROM execution_content_user_policies WHERE org_id=$1 AND project_id=$2 AND user_id=$3 FOR SHARE`, orgID, projectID, userID).
			Scan(&desiredVersion, &effectiveVersion, &desired, &effective, &desiredMode, &effectiveMode, &at, &status, &reason)
	default:
		return layer, invalidPolicy("unknown policy scope")
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return layer, nil
	}
	if err != nil {
		return layer, err
	}
	var desiredConfig, effectiveConfig PolicyConfig
	if scope == PolicyScopeUser {
		if err = json.Unmarshal(desired, &desiredConfig); err != nil {
			return layer, invalidPolicy("invalid stored user desired config")
		}
		if err = json.Unmarshal(effective, &effectiveConfig); err != nil {
			return layer, invalidPolicy("invalid stored user effective config")
		}
	} else {
		if desiredConfig, err = DecodePolicyConfig(desired); err != nil {
			return layer, err
		}
		if effectiveConfig, err = DecodePolicyConfig(effective); err != nil {
			return layer, err
		}
	}
	layer.Explicit = true
	layer.Desired = &PolicyVersion{PolicyVersion: strconv.FormatInt(desiredVersion, 10), Config: desiredConfig}
	layer.Effective = &PolicyVersion{PolicyVersion: strconv.FormatInt(effectiveVersion, 10), Config: effectiveConfig}
	layer.DesiredExecutionMode, layer.EffectiveExecutionMode = desiredMode, effectiveMode
	layer.EffectiveAt = at.UTC().Format("2006-01-02T15:04:05.000000Z")
	layer.ApplicationStatus, layer.ApplicationReason = status, reason
	return layer, nil
}

// ScopedManagementTx is read-only and must be called inside the transaction
// holding the administrator's exact project authorization. A personal preview
// is descriptive, not a substitute for a live key's capture authority.
func (s *PolicyStore) ScopedManagementTx(ctx context.Context, tx pgx.Tx, orgID, projectID, userID int64, storageStatus string) (ScopedManagementView, error) {
	if s == nil || tx == nil || orgID <= 0 || projectID <= 0 {
		return ScopedManagementView{}, invalidPolicy("authorized project scope required")
	}
	principal := CapturePrincipal{KeyType: "shared"}
	if userID > 0 {
		principal.KeyType, principal.OwnerUserID = "personal", userID
		var status, membership string
		if err := tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1 AND org_id=$2 FOR SHARE`, userID, orgID).Scan(&status); err != nil || status != "active" {
			return ScopedManagementView{}, ErrPolicyInactive
		}
		if err := tx.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2 FOR SHARE`, projectID, userID).Scan(&membership); err != nil ||
			(membership != "project_admin" && membership != "project_user" && membership != "project_developer" && membership != "developer") {
			return ScopedManagementView{}, ErrPolicyInactive
		}
	}
	snapshot, err := s.readScopedEffectiveTx(ctx, tx, principal, orgID, projectID, storageStatus, false, true)
	if err != nil {
		return ScopedManagementView{}, err
	}
	enterprise, err := s.scopedLayerTx(ctx, tx, PolicyScopeEnterprise, orgID, projectID, userID)
	if err != nil {
		return ScopedManagementView{}, err
	}
	project, err := s.scopedLayerTx(ctx, tx, PolicyScopeProject, orgID, projectID, userID)
	if err != nil {
		return ScopedManagementView{}, err
	}
	view := ScopedManagementView{SchemaVersion: ScopedPolicyManagementSchema, SourceInstance: s.options.SourceInstance,
		OrgID: strconv.FormatInt(orgID, 10), ProjectID: strconv.FormatInt(projectID, 10), Enterprise: enterprise,
		Project: &project, Effective: &snapshot, VersionVector: snapshot.Vector,
		StorageStatus: storageStatus, DeploymentMode: s.options.DefaultMode,
		FullContentEnabled: s.options.FullContentEnabled, Limits: s.options.Limits,
		PolicyCapabilities: s.policyCapabilities()}
	if userID > 0 {
		layer, err := s.scopedLayerTx(ctx, tx, PolicyScopeUser, orgID, projectID, userID)
		if err != nil {
			return ScopedManagementView{}, err
		}
		view.UserID, view.User = strconv.FormatInt(userID, 10), &layer
	}
	return view, nil
}

// EnterpriseManagementTx works even before an organization has any projects.
// The caller holds the exact organization authorization lock.
func (s *PolicyStore) EnterpriseManagementTx(ctx context.Context, tx pgx.Tx, orgID int64, storageStatus string) (ScopedManagementView, error) {
	if s == nil || tx == nil || orgID <= 0 {
		return ScopedManagementView{}, invalidPolicy("authorized enterprise scope required")
	}
	layer, err := s.scopedLayerTx(ctx, tx, PolicyScopeEnterprise, orgID, 0, 0)
	if err != nil {
		return ScopedManagementView{}, err
	}
	effectiveVersion := "0"
	if layer.Effective != nil {
		effectiveVersion = layer.Effective.PolicyVersion
	}
	return ScopedManagementView{SchemaVersion: ScopedPolicyManagementSchema, SourceInstance: s.options.SourceInstance,
		OrgID: strconv.FormatInt(orgID, 10), Enterprise: layer,
		VersionVector: fmt.Sprintf(`{"enterprise":%s}`, effectiveVersion),
		StorageStatus: storageStatus, DeploymentMode: s.options.DefaultMode,
		FullContentEnabled: s.options.FullContentEnabled, Limits: s.options.Limits,
		PolicyCapabilities: s.policyCapabilities()}, nil
}

func (s *PolicyStore) PutEnterpriseTx(ctx context.Context, tx pgx.Tx, orgID int64, put ScopedManagementPut, application PolicyApplication) (ScopedManagementView, error) {
	if tx == nil || application.Now.IsZero() || put.SchemaVersion != ScopedPolicyManagementSchema {
		return ScopedManagementView{}, invalidPolicy("invalid enterprise update context")
	}
	before, err := s.EnterpriseManagementTx(ctx, tx, orgID, application.StorageStatus)
	if err != nil {
		return ScopedManagementView{}, err
	}
	version := "0"
	if before.Enterprise.Desired != nil {
		version = before.Enterprise.Desired.PolicyVersion
	}
	if put.ExpectedPolicyRef != before.Enterprise.PolicyRef || put.ExpectedVersion != version ||
		put.ExpectedVector != before.VersionVector {
		return ScopedManagementView{}, ErrPolicyVersionConflict
	}
	if version == strconv.FormatInt(math.MaxInt64, 10) || put.ProjectQuotaBytes == nil {
		return ScopedManagementView{}, invalidPolicy("enterprise version or default quota invalid")
	}
	quota := *put.ProjectQuotaBytes
	_, wire, err := s.validateScopedDesired(put, PolicyScopeEnterprise, quota, application)
	if err != nil {
		return ScopedManagementView{}, err
	}
	nextVersion := int64(1)
	if version != "0" {
		old, _ := strconv.ParseInt(version, 10, 64)
		nextVersion = old + 1
	}
	_, err = tx.Exec(ctx, `INSERT INTO execution_content_enterprise_policies
      (org_id,desired_version,effective_version,desired_config,effective_config,
       desired_execution_storage_mode,effective_execution_storage_mode,desired_default_project_quota_bytes,
       effective_default_project_quota_bytes,effective_at,application_status,application_reason)
      VALUES($1,$2,$2,$3,$3,$4,$4,$5,$5,$6,'applied','none')
      ON CONFLICT(org_id) DO UPDATE SET desired_version=EXCLUDED.desired_version,effective_version=EXCLUDED.effective_version,
       desired_config=EXCLUDED.desired_config,effective_config=EXCLUDED.effective_config,
       desired_execution_storage_mode=EXCLUDED.desired_execution_storage_mode,
       effective_execution_storage_mode=EXCLUDED.effective_execution_storage_mode,
       desired_default_project_quota_bytes=EXCLUDED.desired_default_project_quota_bytes,
       effective_default_project_quota_bytes=EXCLUDED.effective_default_project_quota_bytes,
       effective_at=EXCLUDED.effective_at,application_status='applied',application_reason='none'`,
		orgID, nextVersion, wire, put.ExecutionStorageMode, quota, application.Now.UTC())
	if err != nil {
		return ScopedManagementView{}, err
	}
	return s.EnterpriseManagementTx(ctx, tx, orgID, application.StorageStatus)
}

func (s *PolicyStore) validateScopedDesired(put ScopedManagementPut, scope string, quota int64, application PolicyApplication) (PolicyConfig, []byte, error) {
	config := clonePolicyConfig(put.Desired)
	if scope == PolicyScopeUser {
		if config.QuotaBytes != nil || put.ProjectQuotaBytes != nil {
			return PolicyConfig{}, nil, invalidPolicy("user exception cannot define a private quota")
		}
		if config.Mode == ModeFullContent {
			if quota <= 0 {
				return PolicyConfig{}, nil, invalidPolicy("personal full content requires project quota")
			}
			config.QuotaBytes = &quota
		}
	} else if config.Mode == ModeFullContent && put.ProjectQuotaBytes != nil && config.QuotaBytes != nil && *put.ProjectQuotaBytes != *config.QuotaBytes {
		return PolicyConfig{}, nil, invalidPolicy("full content quota conflicts with project quota")
	}
	if scope == PolicyScopeEnterprise && config.Mode == ModeFullContent && (config.QuotaBytes == nil || *config.QuotaBytes != quota) {
		return PolicyConfig{}, nil, invalidPolicy("enterprise full content quota must equal its default project quota")
	}
	if err := s.validateSupportedConfig(config); err != nil {
		return PolicyConfig{}, nil, err
	}
	if config.Mode == ModeFullContent && (s.options.DefaultMode == ModeDisabled || !s.options.FullContentEnabled ||
		!application.ApplyNow || application.StorageStatus != "ready") {
		return PolicyConfig{}, nil, ErrPolicyInactive
	}
	stored := clonePolicyConfig(config)
	if scope == PolicyScopeUser {
		stored.QuotaBytes = nil
	}
	wire, err := json.Marshal(stored)
	if err != nil || len(wire) > maxStoredPolicyConfigBytes {
		return PolicyConfig{}, nil, invalidPolicy("invalid stored scoped config")
	}
	return stored, wire, nil
}

// PutScopedTx performs one immediately applied scoped update under the caller's
// authorization transaction. Pending rows remain readable, but new V2 writes
// require ready storage rather than creating an ambiguous inherited fallback.
func (s *PolicyStore) PutScopedTx(ctx context.Context, tx pgx.Tx, scope string, orgID, projectID, userID int64, put ScopedManagementPut, application PolicyApplication) (ScopedManagementView, error) {
	if tx == nil || put.SchemaVersion != ScopedPolicyManagementSchema || application.Now.IsZero() {
		return ScopedManagementView{}, invalidPolicy("invalid scoped update context")
	}
	if scope != PolicyScopeProject && scope != PolicyScopeUser {
		return ScopedManagementView{}, invalidPolicy("unknown scoped update target")
	}
	before, err := s.ScopedManagementTx(ctx, tx, orgID, projectID, userID, application.StorageStatus)
	if err != nil {
		return ScopedManagementView{}, err
	}
	var target ScopedPolicyLayer
	switch scope {
	case PolicyScopeProject:
		target = *before.Project
	case PolicyScopeUser:
		if userID <= 0 || before.User == nil {
			return ScopedManagementView{}, invalidPolicy("user scope required")
		}
		target = *before.User
	}
	version := "0"
	if target.Explicit && target.Desired != nil {
		version = target.Desired.PolicyVersion
	}
	if put.ExpectedPolicyRef != target.PolicyRef || put.ExpectedVersion != version ||
		put.ExpectedVector != before.Effective.Vector {
		return ScopedManagementView{}, ErrPolicyVersionConflict
	}
	if target.Explicit && version == strconv.FormatInt(math.MaxInt64, 10) {
		return ScopedManagementView{}, invalidPolicy("policy version exhausted")
	}
	quota := before.Effective.ProjectQuotaBytes
	if scope == PolicyScopeProject && put.ProjectQuotaBytes != nil {
		quota = *put.ProjectQuotaBytes
	}
	stored, wire, err := s.validateScopedDesired(put, scope, quota, application)
	if err != nil {
		return ScopedManagementView{}, err
	}
	_ = stored
	nextVersion := int64(1)
	if target.Explicit {
		old, _ := strconv.ParseInt(version, 10, 64)
		nextVersion = old + 1
	}
	if scope == PolicyScopeProject {
		_, err = tx.Exec(ctx, `INSERT INTO execution_content_policies
      (org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,
       application_status,application_reason,explicit_override,desired_execution_storage_mode,
       effective_execution_storage_mode,project_quota_bytes)
      VALUES($1,$2,$3,$3,$4,$4,$5,'applied','none',true,$6,$6,$7)
      ON CONFLICT(org_id,project_id) DO UPDATE SET desired_version=EXCLUDED.desired_version,
       effective_version=EXCLUDED.effective_version,desired_config=EXCLUDED.desired_config,
       effective_config=EXCLUDED.effective_config,effective_at=EXCLUDED.effective_at,
       application_status='applied',application_reason='none',explicit_override=true,
       desired_execution_storage_mode=EXCLUDED.desired_execution_storage_mode,
       effective_execution_storage_mode=EXCLUDED.effective_execution_storage_mode,
       project_quota_bytes=EXCLUDED.project_quota_bytes`,
			orgID, projectID, nextVersion, wire, application.Now.UTC(), put.ExecutionStorageMode, put.ProjectQuotaBytes)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO execution_content_user_policies
      (org_id,project_id,user_id,desired_version,effective_version,desired_config,effective_config,
       desired_execution_storage_mode,effective_execution_storage_mode,effective_at,application_status,application_reason)
      VALUES($1,$2,$3,$4,$4,$5,$5,$6,$6,$7,'applied','none')
      ON CONFLICT(org_id,project_id,user_id) DO UPDATE SET desired_version=EXCLUDED.desired_version,
       effective_version=EXCLUDED.effective_version,desired_config=EXCLUDED.desired_config,
       effective_config=EXCLUDED.effective_config,desired_execution_storage_mode=EXCLUDED.desired_execution_storage_mode,
       effective_execution_storage_mode=EXCLUDED.effective_execution_storage_mode,effective_at=EXCLUDED.effective_at,
       application_status='applied',application_reason='none'`,
			orgID, projectID, userID, nextVersion, wire, put.ExecutionStorageMode, application.Now.UTC())
	}
	if err != nil {
		return ScopedManagementView{}, err
	}
	return s.ScopedManagementTx(ctx, tx, orgID, projectID, userID, application.StorageStatus)
}
