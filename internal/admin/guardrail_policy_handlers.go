package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/streampolicy"
)

type guardrailPolicyRecord struct {
	ID                   int64
	OrgID                *int64
	ProjectID            *int64
	APIKeyID             *int64
	AccessCredentialID   *int64
	Name                 string
	Model                string
	Stage                string
	DetectorRef          string
	Action               string
	Mode                 string
	FailureMode          string
	Mandatory            bool
	Config               json.RawMessage
	Version              int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
	OrgName              string
	ProjectName          string
	APIKeyName           string
	APIKeyPrefix         string
	AccessCredentialName string
	AccessCredentialRef  string
}

type guardrailPolicyMutation struct {
	Version            *int64           `json:"version"`
	OrgID              *int64           `json:"org_id"`
	ProjectID          *int64           `json:"project_id"`
	APIKeyID           *int64           `json:"api_key_id"`
	AccessCredentialID *int64           `json:"access_credential_id"`
	Name               *string          `json:"name"`
	Model              *string          `json:"model"`
	Stage              *string          `json:"stage"`
	DetectorRef        *string          `json:"detector_ref"`
	Action             *string          `json:"action"`
	Mode               *string          `json:"mode"`
	FailureMode        *string          `json:"failure_mode"`
	Mandatory          *bool            `json:"mandatory"`
	Config             *json.RawMessage `json:"config"`
}

func (h *Handler) ListGuardrailPolicies(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)
	conditions := make([]string, 0)
	args := make([]interface{}, 0)
	argIndex := 1
	if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		if actorOrgID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("(p.org_id=$%d OR p.org_id IS NULL)", argIndex))
		args = append(args, actorOrgID)
		argIndex++
	}
	if stage := strings.TrimSpace(r.URL.Query().Get("stage")); stage != "" {
		if stage != guardrail.StageInput && stage != guardrail.StageNonStreamOutput && stage != guardrail.StageStreamOutput {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid stage"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("p.stage=$%d", argIndex))
		args = append(args, stage)
		argIndex++
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	var total int
	if err := h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM guardrail_policies p"+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}
	query := fmt.Sprintf(`
		SELECT p.id,p.org_id,p.project_id,p.api_key_id,p.access_credential_id,p.name,p.model,p.stage,
		       p.detector_ref,p.action,p.mode,p.failure_mode,p.mandatory,p.config,
		       p.version,p.created_at,p.updated_at,
		       COALESCE(o.name,''),COALESCE(project.name,''),
		       COALESCE(key.name,''),COALESCE(key.key_prefix,''),
		       COALESCE(credential.name,''),COALESCE(credential.credential_ref,'')
		FROM guardrail_policies p
		LEFT JOIN orgs o ON o.id=p.org_id
		LEFT JOIN projects project ON project.id=p.project_id AND project.org_id=p.org_id
		LEFT JOIN api_keys key ON key.id=p.api_key_id AND key.org_id=p.org_id
		LEFT JOIN access_credentials credential ON credential.id=p.access_credential_id AND credential.org_id=p.org_id
		%s ORDER BY p.mandatory DESC,p.id ASC LIMIT $%d OFFSET $%d`, where, argIndex, argIndex+1)
	args = append(args, pageSize, offset)
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()
	data := make([]map[string]interface{}, 0)
	for rows.Next() {
		record, err := scanGuardrailPolicy(rows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		payload := guardrailPolicyPayload(record)
		if record.OrgID == nil && !auth.IsPlatformAdmin(actorOrganizationRole(r.Context())) {
			payload["config"] = map[string]interface{}{}
			payload["config_redacted"] = true
		}
		data = append(data, payload)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}
	detectors := []string{guardrail.DetectorSensitiveWords, guardrail.DetectorPII}
	streamCapabilities := map[string]guardrail.StreamCapability{
		guardrail.DetectorSensitiveWords: {Synchronous: true, LookbehindBytes: guardrail.MaxStreamLookbehindBytes},
		guardrail.DetectorPII:            {Synchronous: true, LookbehindBytes: 512},
	}
	localDetectorMaxBytes, externalDetectorMaxBytes, detectorMaxTexts := guardrail.DefaultOptions().LocalDetectorMaxBytes, guardrail.DefaultOptions().ExternalDetectorMaxBytes, guardrail.DefaultOptions().MaxDetectorTexts
	policyLimits := guardrail.PolicyLimits{
		MaxActivePoliciesPerOrg:   guardrail.DefaultOptions().MaxActivePoliciesPerOrg,
		MaxActivePlatformPolicies: guardrail.DefaultOptions().MaxActivePlatformPolicies,
		MaxCachedPolicies:         guardrail.DefaultOptions().MaxCachedPolicies,
		MaxCachedTenantScopes:     guardrail.DefaultOptions().MaxCachedTenantScopes,
	}
	if h.guardrailController != nil {
		detectors = h.guardrailController.ApprovedDetectors()
		streamCapabilities = h.guardrailController.DetectorStreamCapabilities()
		localDetectorMaxBytes, externalDetectorMaxBytes, detectorMaxTexts = h.guardrailController.DetectorInputLimits()
		if provider, ok := h.guardrailController.(interface{ PolicyLimits() guardrail.PolicyLimits }); ok {
			policyLimits = provider.PolicyLimits()
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": data, "total": total, "approved_detectors": detectors,
		"external_deadline_ms": 1000, "automatic_detector_retry": false,
		"local_detector_max_bytes": localDetectorMaxBytes, "external_detector_max_bytes": externalDetectorMaxBytes,
		"detector_max_texts":                detectorMaxTexts,
		"org_active_policy_limit":           policyLimits.MaxActivePoliciesPerOrg,
		"platform_active_policy_limit":      policyLimits.MaxActivePlatformPolicies,
		"cached_policy_limit":               policyLimits.MaxCachedPolicies,
		"cached_tenant_limit":               policyLimits.MaxCachedTenantScopes,
		"stream_detector_capabilities":      streamCapabilities,
		"stream_enforce_buffer_max_bytes":   streampolicy.EnforceBufferMaxBytes,
		"stream_enforce_buffer_max_wait_ms": streampolicy.EnforceBufferMaxWait.Milliseconds(),
		"stream_checkpoint_interval_ms":     streampolicy.CheckpointInterval.Milliseconds(),
	})
}

func (h *Handler) CreateGuardrailPolicy(w http.ResponseWriter, r *http.Request) {
	mutation, err := decodeGuardrailPolicyMutation(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if err := validatePolicySelectorMutation(mutation.APIKeyID, mutation.AccessCredentialID); err != nil {
		writeGuardrailPolicyError(w, err)
		return
	}
	record := guardrailPolicyRecord{
		OrgID: positiveOrNil(mutation.OrgID), ProjectID: positiveOrNil(mutation.ProjectID), APIKeyID: positiveOrNil(mutation.APIKeyID),
		AccessCredentialID: positiveOrNil(mutation.AccessCredentialID),
		Name:               stringValue(mutation.Name), Model: stringValue(mutation.Model), Stage: stringValue(mutation.Stage),
		DetectorRef: stringValue(mutation.DetectorRef), Action: stringValue(mutation.Action),
		Mode: guardrail.ModeShadow, FailureMode: guardrail.FailureDegradedAllow, Config: json.RawMessage(`{}`), Version: 1,
	}
	applyGuardrailPolicyMutation(&record, mutation)
	if err := h.normalizeAndAuthorizeGuardrailPolicy(r.Context(), &record); err != nil {
		writeGuardrailPolicyError(w, err)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizePolicySelectorMutationTx(r.Context(), tx, policySelector{
		OrgID: record.OrgID, ProjectID: record.ProjectID, APIKeyID: record.APIKeyID,
		AccessCredentialID: record.AccessCredentialID,
	}); err != nil {
		writeGuardrailPolicyError(w, err)
		return
	}
	if err := h.configureGuardrailPolicyQuota(r.Context(), tx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to configure policy quota"})
		return
	}
	var id int64
	var createdAt, updatedAt time.Time
	err = tx.QueryRow(r.Context(), `
		INSERT INTO guardrail_policies (
		 org_id,project_id,api_key_id,access_credential_id,name,model,stage,detector_ref,action,mode,
		 failure_mode,mandatory,config,version,created_by,updated_by,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,1,$14,$14,NOW(),NOW())
		RETURNING id,created_at,updated_at`,
		nullablePolicyID(record.OrgID), nullablePolicyID(record.ProjectID), nullablePolicyID(record.APIKeyID), nullablePolicyID(record.AccessCredentialID),
		record.Name, record.Model, record.Stage, record.DetectorRef, record.Action, record.Mode,
		record.FailureMode, record.Mandatory, string(record.Config), nullablePositiveUserID(middleware.GetUserIDFromContext(r.Context())),
	).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		if isGuardrailPolicyQuotaError(err) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Guardrail active policy quota exceeded"})
		} else {
			writeJSON(w, policyMutationStatus(err), map[string]interface{}{"error": "Failed to create policy"})
		}
		return
	}
	record.ID, record.CreatedAt, record.UpdatedAt = id, createdAt, updatedAt
	record, err = readGuardrailPolicyFrom(r.Context(), tx, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read created policy"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "guardrail_policy.create", "guardrail_policy", strconv.FormatInt(record.ID, 10),
		map[string]interface{}{"after": guardrailPolicyAuditPayload(record)}, guardrailPolicyAuditMeta(r, record)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	applied := h.refreshGuardrailSnapshot(r.Context())
	payload := guardrailPolicyPayload(record)
	payload["runtime_snapshot_applied"] = applied
	writeJSON(w, http.StatusCreated, payload)
}

func (h *Handler) UpdateGuardrailPolicy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid policy ID"})
		return
	}
	current, err := h.readGuardrailPolicy(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Policy not found"})
		return
	}
	if current.OrgID == nil && !auth.IsPlatformAdmin(actorOrganizationRole(r.Context())) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	if current.OrgID != nil && !actorCanAccessOrg(r.Context(), *current.OrgID) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Policy not found"})
		return
	}
	mutation, decodeErr := decodeGuardrailPolicyMutation(w, r)
	if decodeErr != nil || mutation.Version == nil || *mutation.Version != current.Version {
		status := http.StatusBadRequest
		if mutation.Version != nil && *mutation.Version != current.Version {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]interface{}{"error": "A current matching version is required"})
		return
	}
	if err := validatePolicySelectorMutation(mutation.APIKeyID, mutation.AccessCredentialID); err != nil {
		writeGuardrailPolicyError(w, err)
		return
	}
	before := guardrailPolicyAuditPayload(current)
	applyGuardrailPolicyMutation(&current, mutation)
	if err := h.normalizeAndAuthorizeGuardrailPolicy(r.Context(), &current); err != nil {
		writeGuardrailPolicyError(w, err)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizePolicySelectorMutationTx(r.Context(), tx, policySelector{
		OrgID: current.OrgID, ProjectID: current.ProjectID, APIKeyID: current.APIKeyID,
		AccessCredentialID: current.AccessCredentialID,
	}); err != nil {
		writeGuardrailPolicyError(w, err)
		return
	}
	if err := h.configureGuardrailPolicyQuota(r.Context(), tx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to configure policy quota"})
		return
	}
	result, err := tx.Exec(r.Context(), `
		UPDATE guardrail_policies SET org_id=$2,project_id=$3,api_key_id=$4,access_credential_id=$5,name=$6,
		 model=$7,stage=$8,detector_ref=$9,action=$10,mode=$11,failure_mode=$12,
		 mandatory=$13,config=$14::jsonb,version=version+1,updated_by=$15,updated_at=NOW()
		WHERE id=$1 AND version=$16`,
		id, nullablePolicyID(current.OrgID), nullablePolicyID(current.ProjectID), nullablePolicyID(current.APIKeyID), nullablePolicyID(current.AccessCredentialID),
		current.Name, current.Model, current.Stage, current.DetectorRef, current.Action, current.Mode,
		current.FailureMode, current.Mandatory, string(current.Config), nullablePositiveUserID(middleware.GetUserIDFromContext(r.Context())), *mutation.Version)
	if err != nil {
		if isGuardrailPolicyQuotaError(err) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Guardrail active policy quota exceeded"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update policy"})
		}
		return
	}
	if result.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Policy version changed"})
		return
	}
	updated, err := readGuardrailPolicyFrom(r.Context(), tx, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read updated policy"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "guardrail_policy.update", "guardrail_policy", strconv.FormatInt(updated.ID, 10),
		map[string]interface{}{"before": before, "after": guardrailPolicyAuditPayload(updated)}, guardrailPolicyAuditMeta(r, updated)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	payload := guardrailPolicyPayload(updated)
	payload["runtime_snapshot_applied"] = h.refreshGuardrailSnapshot(r.Context())
	writeJSON(w, http.StatusOK, payload)
}

func decodeGuardrailPolicyMutation(w http.ResponseWriter, r *http.Request) (guardrailPolicyMutation, error) {
	var mutation guardrailPolicyMutation
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&mutation); err != nil {
		return mutation, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return mutation, fmt.Errorf("guardrail policy body must contain exactly one JSON object")
	}
	return mutation, nil
}

type guardrailPolicyScanner interface{ Scan(...any) error }

func scanGuardrailPolicy(scanner guardrailPolicyScanner) (guardrailPolicyRecord, error) {
	var record guardrailPolicyRecord
	err := scanner.Scan(&record.ID, &record.OrgID, &record.ProjectID, &record.APIKeyID, &record.AccessCredentialID, &record.Name, &record.Model,
		&record.Stage, &record.DetectorRef, &record.Action, &record.Mode, &record.FailureMode, &record.Mandatory,
		&record.Config, &record.Version, &record.CreatedAt, &record.UpdatedAt,
		&record.OrgName, &record.ProjectName, &record.APIKeyName, &record.APIKeyPrefix,
		&record.AccessCredentialName, &record.AccessCredentialRef)
	return record, err
}

func (h *Handler) readGuardrailPolicy(ctx context.Context, id int64) (guardrailPolicyRecord, error) {
	return readGuardrailPolicyFrom(ctx, h.db, id)
}

type guardrailPolicyQueryRower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

func readGuardrailPolicyFrom(ctx context.Context, queryer guardrailPolicyQueryRower, id int64) (guardrailPolicyRecord, error) {
	row := queryer.QueryRow(ctx, `
		SELECT p.id,p.org_id,p.project_id,p.api_key_id,p.access_credential_id,p.name,p.model,p.stage,
		       p.detector_ref,p.action,p.mode,p.failure_mode,p.mandatory,p.config,
		       p.version,p.created_at,p.updated_at,
		       COALESCE(o.name,''),COALESCE(project.name,''),COALESCE(key.name,''),COALESCE(key.key_prefix,''),
		       COALESCE(credential.name,''),COALESCE(credential.credential_ref,'')
		FROM guardrail_policies p
		LEFT JOIN orgs o ON o.id=p.org_id
		LEFT JOIN projects project ON project.id=p.project_id AND project.org_id=p.org_id
		LEFT JOIN api_keys key ON key.id=p.api_key_id AND key.org_id=p.org_id
		LEFT JOIN access_credentials credential ON credential.id=p.access_credential_id AND credential.org_id=p.org_id
		WHERE p.id=$1`, id)
	return scanGuardrailPolicy(row)
}

func (h *Handler) normalizeAndAuthorizeGuardrailPolicy(ctx context.Context, record *guardrailPolicyRecord) error {
	if record == nil {
		return fmt.Errorf("invalid policy")
	}
	record.Name, record.Model = strings.TrimSpace(record.Name), strings.TrimSpace(record.Model)
	record.Stage, record.DetectorRef = strings.TrimSpace(record.Stage), strings.TrimSpace(record.DetectorRef)
	record.Action, record.Mode, record.FailureMode = strings.TrimSpace(record.Action), strings.TrimSpace(record.Mode), strings.TrimSpace(record.FailureMode)
	if record.Name == "" || len(record.Name) > 120 {
		return fmt.Errorf("name is required and must be at most 120 characters")
	}
	role := actorOrganizationRole(ctx)
	if !auth.IsPlatformAdmin(role) {
		actorOrgID := middleware.GetOrgIDFromContext(ctx)
		if !auth.IsOrgAdmin(role) || actorOrgID <= 0 || record.Mandatory {
			return errGuardrailPolicyForbidden
		}
		if record.OrgID == nil {
			record.OrgID = &actorOrgID
		}
	}
	selector := policySelector{
		OrgID: record.OrgID, ProjectID: record.ProjectID, APIKeyID: record.APIKeyID,
		AccessCredentialID: record.AccessCredentialID,
	}
	if err := h.canonicalizePolicySelector(ctx, &selector); err != nil {
		if err == errLimitPolicyForbidden {
			return errGuardrailPolicyForbidden
		}
		return err
	}
	record.OrgID, record.ProjectID = selector.OrgID, selector.ProjectID
	record.APIKeyID, record.AccessCredentialID = selector.APIKeyID, selector.AccessCredentialID
	policy := guardrail.Policy{
		ID: record.ID, Ref: fmt.Sprintf("guardrail:%d", maxInt64(record.ID, 1)), Name: record.Name,
		Version: maxInt64(record.Version, 1), OrgID: record.OrgID, ProjectID: record.ProjectID, APIKeyID: record.APIKeyID,
		AccessCredentialID: record.AccessCredentialID,
		Model:              record.Model, Stage: record.Stage, DetectorRef: record.DetectorRef, Action: record.Action,
		Mode: record.Mode, FailureMode: record.FailureMode, Mandatory: record.Mandatory, Config: record.Config,
	}
	if h.guardrailController == nil {
		return fmt.Errorf("guardrail registry is unavailable")
	}
	if err := h.guardrailController.ValidatePolicy(policy); err != nil {
		return err
	}
	return nil
}

func applyGuardrailPolicyMutation(record *guardrailPolicyRecord, mutation guardrailPolicyMutation) {
	if mutation.OrgID != nil {
		record.OrgID = positiveOrNil(mutation.OrgID)
	}
	if mutation.ProjectID != nil {
		record.ProjectID = positiveOrNil(mutation.ProjectID)
	}
	if mutation.APIKeyID != nil {
		record.APIKeyID = positiveOrNil(mutation.APIKeyID)
		record.AccessCredentialID = nil
	}
	if mutation.AccessCredentialID != nil {
		record.AccessCredentialID = positiveOrNil(mutation.AccessCredentialID)
		record.APIKeyID = nil
	}
	if mutation.Name != nil {
		record.Name = *mutation.Name
	}
	if mutation.Model != nil {
		record.Model = *mutation.Model
	}
	if mutation.Stage != nil {
		record.Stage = *mutation.Stage
	}
	if mutation.DetectorRef != nil {
		record.DetectorRef = *mutation.DetectorRef
	}
	if mutation.Action != nil {
		record.Action = *mutation.Action
	}
	if mutation.Mode != nil {
		record.Mode = *mutation.Mode
	}
	if mutation.FailureMode != nil {
		record.FailureMode = *mutation.FailureMode
	}
	if mutation.Mandatory != nil {
		record.Mandatory = *mutation.Mandatory
	}
	if mutation.Config != nil {
		if len(*mutation.Config) == 0 || string(*mutation.Config) == "null" {
			record.Config = json.RawMessage(`{}`)
		} else {
			record.Config = append(json.RawMessage(nil), (*mutation.Config)...)
		}
	}
}

func guardrailPolicyPayload(record guardrailPolicyRecord) map[string]interface{} {
	var config interface{} = map[string]interface{}{}
	_ = json.Unmarshal(record.Config, &config)
	return map[string]interface{}{
		"id": record.ID, "name": record.Name, "version": record.Version,
		"org_id": nullablePolicyID(record.OrgID), "org_name": record.OrgName,
		"project_id": nullablePolicyID(record.ProjectID), "project_name": record.ProjectName,
		"api_key_id": nullablePolicyID(record.APIKeyID), "api_key_name": record.APIKeyName, "api_key_prefix": record.APIKeyPrefix,
		"access_credential_id":   nullablePolicyID(record.AccessCredentialID),
		"access_credential_name": record.AccessCredentialName, "access_credential_ref": record.AccessCredentialRef,
		"model": record.Model, "stage": record.Stage, "detector_ref": record.DetectorRef,
		"action": record.Action, "mode": record.Mode, "failure_mode": record.FailureMode,
		"mandatory": record.Mandatory, "config": config,
		"selector_semantics": "all_fields_and_all_matching_policies", "created_at": record.CreatedAt, "updated_at": record.UpdatedAt,
	}
}

func guardrailPolicyAuditPayload(record guardrailPolicyRecord) map[string]interface{} {
	payload := guardrailPolicyPayload(record)
	delete(payload, "config")
	canonicalConfig := append([]byte(nil), record.Config...)
	var config interface{}
	if json.Unmarshal(record.Config, &config) == nil {
		if encoded, err := json.Marshal(config); err == nil {
			canonicalConfig = encoded
		}
	}
	sum := sha256.Sum256(canonicalConfig)
	payload["config_sha256"] = fmt.Sprintf("%x", sum[:])
	payload["config_bytes"] = len(canonicalConfig)
	return payload
}

func guardrailPolicyAuditMeta(r *http.Request, record guardrailPolicyRecord) auditMeta {
	orgID := middleware.GetOrgIDFromContext(r.Context())
	if record.OrgID != nil {
		orgID = *record.OrgID
	}
	projectID := int64(0)
	if record.ProjectID != nil {
		projectID = *record.ProjectID
	}
	return requestAuditMeta(r, orgID, projectID)
}

func (h *Handler) refreshGuardrailSnapshot(ctx context.Context) bool {
	if h.guardrailController == nil {
		return false
	}
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := h.guardrailController.Refresh(refreshCtx); err != nil {
		log.Printf("[guardrail] committed policy will converge through refresh loop: %v", err)
		return false
	}
	return true
}

func (h *Handler) configureGuardrailPolicyQuota(ctx context.Context, tx pgx.Tx) error {
	limits := guardrail.PolicyLimits{
		MaxActivePoliciesPerOrg:   guardrail.DefaultOptions().MaxActivePoliciesPerOrg,
		MaxActivePlatformPolicies: guardrail.DefaultOptions().MaxActivePlatformPolicies,
		MaxCachedPolicies:         guardrail.DefaultOptions().MaxCachedPolicies,
		MaxCachedTenantScopes:     guardrail.DefaultOptions().MaxCachedTenantScopes,
	}
	if provider, ok := h.guardrailController.(interface{ PolicyLimits() guardrail.PolicyLimits }); ok {
		limits = provider.PolicyLimits()
	}
	_, err := tx.Exec(ctx, `SELECT
		set_config('unirouter.guardrail_org_active_policy_limit', $1, TRUE),
		set_config('unirouter.guardrail_platform_active_policy_limit', $2, TRUE),
		set_config('unirouter.guardrail_cached_policy_limit', $3, TRUE),
		set_config('unirouter.guardrail_cached_tenant_limit', $4, TRUE)`,
		strconv.Itoa(limits.MaxActivePoliciesPerOrg), strconv.Itoa(limits.MaxActivePlatformPolicies),
		strconv.Itoa(limits.MaxCachedPolicies), strconv.Itoa(limits.MaxCachedTenantScopes))
	return err
}

func isGuardrailPolicyQuotaError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.ConstraintName {
	case "guardrail_org_active_policy_quota", "guardrail_platform_active_policy_quota",
		"guardrail_active_policy_cache_quota", "guardrail_active_tenant_scope_quota":
		return true
	default:
		return false
	}
}

var errGuardrailPolicyForbidden = errors.New("forbidden")

func writeGuardrailPolicyError(w http.ResponseWriter, err error) {
	if errors.Is(err, errPolicySelectorNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Policy selector not found"})
		return
	}
	if errors.Is(err, errGuardrailPolicyForbidden) || errors.Is(err, errLimitPolicyForbidden) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
}

func maxInt64(value, fallback int64) int64 {
	if value > 0 {
		return value
	}
	return fallback
}
