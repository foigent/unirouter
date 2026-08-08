package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

const (
	limitPolicyModeDisabled = "disabled"
	limitPolicyModeShadow   = "shadow"
	limitPolicyModeEnforce  = "enforce"

	limitPolicyFailClosed    = "fail_closed"
	limitPolicyLastKnownSafe = "last_known_safe"
)

type limitPolicyRecord struct {
	ID               int64
	OrgID            *int64
	ProjectID        *int64
	APIKeyID         *int64
	Name             string
	Model            string
	PolicyLabel      string
	Mode             string
	FailureMode      string
	RPSLimit         *int64
	RPMLimit         *int64
	TPMLimit         *int64
	ConcurrencyLimit *int64
	Version          int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	OrgName          string
	ProjectName      string
	APIKeyName       string
	APIKeyPrefix     string
}

type limitPolicyMutation struct {
	Version          *int64  `json:"version"`
	OrgID            *int64  `json:"org_id"`
	ProjectID        *int64  `json:"project_id"`
	APIKeyID         *int64  `json:"api_key_id"`
	Name             *string `json:"name"`
	Model            *string `json:"model"`
	PolicyLabel      *string `json:"policy_label"`
	Mode             *string `json:"mode"`
	FailureMode      *string `json:"failure_mode"`
	RPSLimit         *int64  `json:"rps_limit"`
	RPMLimit         *int64  `json:"rpm_limit"`
	TPMLimit         *int64  `json:"tpm_limit"`
	ConcurrencyLimit *int64  `json:"concurrency_limit"`
}

func (h *Handler) ListLimitPolicies(w http.ResponseWriter, r *http.Request) {
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
		conditions = append(conditions, fmt.Sprintf("(p.org_id = $%d OR p.org_id IS NULL)", argIndex))
		args = append(args, actorOrgID)
		argIndex++
	} else if value := r.URL.Query().Get("org_id"); value != "" {
		orgID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || orgID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("p.org_id = $%d", argIndex))
		args = append(args, orgID)
		argIndex++
	}
	if mode := strings.TrimSpace(r.URL.Query().Get("mode")); mode != "" {
		if !validLimitPolicyMode(mode) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid mode"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("p.mode = $%d", argIndex))
		args = append(args, mode)
		argIndex++
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM distributed_limit_policies p"+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}
	query := fmt.Sprintf(`
		SELECT p.id, p.org_id, p.project_id, p.api_key_id, p.name, p.model, p.policy_label,
		       p.mode, p.failure_mode, p.rps_limit, p.rpm_limit, p.tpm_limit,
		       p.concurrency_limit, p.version, p.created_at, p.updated_at,
		       COALESCE(o.name, ''), COALESCE(project.name, ''),
		       COALESCE(key.name, ''), COALESCE(key.key_prefix, '')
		FROM distributed_limit_policies p
		LEFT JOIN orgs o ON o.id = p.org_id
		LEFT JOIN projects project ON project.id = p.project_id AND project.org_id = p.org_id
		LEFT JOIN api_keys key ON key.id = p.api_key_id AND key.org_id = p.org_id
		%s
		ORDER BY p.id ASC
		LIMIT $%d OFFSET $%d`, where, argIndex, argIndex+1)
	args = append(args, pageSize, offset)
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	data := make([]map[string]interface{}, 0)
	for rows.Next() {
		record, err := scanLimitPolicy(rows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		data = append(data, limitPolicyPayload(record))
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}
	builtinLimits := map[string]int64{}
	if provider, ok := h.limitPolicyReloader.(interface{ BuiltinLimits() map[string]int64 }); ok {
		builtinLimits = provider.BuiltinLimits()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": data, "total": total, "builtin_limits": builtinLimits})
}

func (h *Handler) CreateLimitPolicy(w http.ResponseWriter, r *http.Request) {
	var mutation limitPolicyMutation
	if err := json.NewDecoder(r.Body).Decode(&mutation); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	record := limitPolicyRecord{
		OrgID: positiveOrNil(mutation.OrgID), ProjectID: positiveOrNil(mutation.ProjectID), APIKeyID: positiveOrNil(mutation.APIKeyID),
		Name: stringValue(mutation.Name), Model: stringValue(mutation.Model),
		PolicyLabel: stringValue(mutation.PolicyLabel), Mode: stringValue(mutation.Mode),
		FailureMode: stringValue(mutation.FailureMode), RPSLimit: mutation.RPSLimit,
		RPMLimit: mutation.RPMLimit, TPMLimit: mutation.TPMLimit,
		ConcurrencyLimit: mutation.ConcurrencyLimit, Version: 1,
	}
	if record.Mode == "" {
		record.Mode = limitPolicyModeShadow
	}
	if record.FailureMode == "" {
		record.FailureMode = limitPolicyFailClosed
	}
	if err := h.normalizeAndAuthorizeLimitPolicy(r.Context(), &record); err != nil {
		writeLimitPolicyError(w, err)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())

	var id int64
	var createdAt, updatedAt time.Time
	err = tx.QueryRow(r.Context(), `
		INSERT INTO distributed_limit_policies (
			org_id, project_id, api_key_id, name, model, policy_label, mode, failure_mode,
			rps_limit, rpm_limit, tpm_limit, concurrency_limit, version, created_by, updated_by,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,$13,$13,NOW(),NOW())
		RETURNING id, created_at, updated_at`,
		nullablePolicyID(record.OrgID), nullablePolicyID(record.ProjectID), nullablePolicyID(record.APIKeyID),
		record.Name, record.Model, record.PolicyLabel, record.Mode, record.FailureMode,
		nullablePolicyID(record.RPSLimit), nullablePolicyID(record.RPMLimit), nullablePolicyID(record.TPMLimit),
		nullablePolicyID(record.ConcurrencyLimit), nullablePositiveUserID(middleware.GetUserIDFromContext(r.Context())),
	).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		writeJSON(w, policyMutationStatus(err), map[string]interface{}{"error": "Failed to create policy"})
		return
	}
	record.ID = id
	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	if err := insertAuditEntry(r.Context(), tx, "limit_policy.create", "distributed_limit_policy", strconv.FormatInt(record.ID, 10),
		map[string]interface{}{"after": limitPolicyPayload(record)}, limitPolicyAuditMeta(r, record)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	applied := h.refreshLimitPolicySnapshot(r.Context())
	payload := limitPolicyPayload(record)
	payload["runtime_snapshot_applied"] = applied
	writeJSON(w, http.StatusCreated, payload)
}

func (h *Handler) UpdateLimitPolicy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid policy ID"})
		return
	}
	current, err := h.readLimitPolicy(r.Context(), id)
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

	before := current
	var mutation limitPolicyMutation
	if err := json.NewDecoder(r.Body).Decode(&mutation); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if mutation.Version == nil || *mutation.Version <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "version is required and must be positive"})
		return
	}
	expectedVersion := *mutation.Version
	applyLimitPolicyMutation(&current, mutation)
	if err := h.normalizeAndAuthorizeLimitPolicy(r.Context(), &current); err != nil {
		writeLimitPolicyError(w, err)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `
		UPDATE distributed_limit_policies SET
			org_id=$1, project_id=$2, api_key_id=$3, name=$4, model=$5, policy_label=$6,
			mode=$7, failure_mode=$8, rps_limit=$9, rpm_limit=$10, tpm_limit=$11,
			concurrency_limit=$12, version=version+1, updated_by=$13, updated_at=NOW()
		WHERE id=$14 AND version=$15`,
		nullablePolicyID(current.OrgID), nullablePolicyID(current.ProjectID), nullablePolicyID(current.APIKeyID),
		current.Name, current.Model, current.PolicyLabel, current.Mode, current.FailureMode,
		nullablePolicyID(current.RPSLimit), nullablePolicyID(current.RPMLimit), nullablePolicyID(current.TPMLimit),
		nullablePolicyID(current.ConcurrencyLimit), nullablePositiveUserID(middleware.GetUserIDFromContext(r.Context())), id, expectedVersion)
	if err != nil {
		writeJSON(w, policyMutationStatus(err), map[string]interface{}{"error": "Failed to update policy"})
		return
	}
	if tag.RowsAffected() == 0 {
		latest, readErr := readLimitPolicyFrom(r.Context(), tx, id)
		if readErr != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Policy not found"})
			return
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Policy changed since it was loaded; refresh before saving", "current_version": latest.Version,
		})
		return
	}
	updated, err := readLimitPolicyFrom(r.Context(), tx, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read updated policy"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "limit_policy.update", "distributed_limit_policy", strconv.FormatInt(updated.ID, 10),
		map[string]interface{}{"before": limitPolicyPayload(before), "after": limitPolicyPayload(updated)}, limitPolicyAuditMeta(r, updated)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	applied := h.refreshLimitPolicySnapshot(r.Context())
	payload := limitPolicyPayload(updated)
	payload["runtime_snapshot_applied"] = applied
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) readLimitPolicy(ctx context.Context, id int64) (limitPolicyRecord, error) {
	return readLimitPolicyFrom(ctx, h.db, id)
}

type limitPolicyQueryRower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

func readLimitPolicyFrom(ctx context.Context, queryer limitPolicyQueryRower, id int64) (limitPolicyRecord, error) {
	row := queryer.QueryRow(ctx, `
		SELECT p.id, p.org_id, p.project_id, p.api_key_id, p.name, p.model, p.policy_label,
		       p.mode, p.failure_mode, p.rps_limit, p.rpm_limit, p.tpm_limit,
		       p.concurrency_limit, p.version, p.created_at, p.updated_at,
		       COALESCE(o.name, ''), COALESCE(project.name, ''),
		       COALESCE(key.name, ''), COALESCE(key.key_prefix, '')
		FROM distributed_limit_policies p
		LEFT JOIN orgs o ON o.id = p.org_id
		LEFT JOIN projects project ON project.id = p.project_id AND project.org_id = p.org_id
		LEFT JOIN api_keys key ON key.id = p.api_key_id AND key.org_id = p.org_id
		WHERE p.id = $1`, id)
	return scanLimitPolicy(row)
}

type limitPolicyScanner interface {
	Scan(dest ...interface{}) error
}

func scanLimitPolicy(scanner limitPolicyScanner) (limitPolicyRecord, error) {
	var record limitPolicyRecord
	var orgID, projectID, apiKeyID sql.NullInt64
	var rps, rpm, tpm, concurrency sql.NullInt64
	err := scanner.Scan(
		&record.ID, &orgID, &projectID, &apiKeyID, &record.Name, &record.Model, &record.PolicyLabel,
		&record.Mode, &record.FailureMode, &rps, &rpm, &tpm, &concurrency, &record.Version,
		&record.CreatedAt, &record.UpdatedAt, &record.OrgName, &record.ProjectName,
		&record.APIKeyName, &record.APIKeyPrefix,
	)
	record.OrgID = nullIntPointer(orgID)
	record.ProjectID = nullIntPointer(projectID)
	record.APIKeyID = nullIntPointer(apiKeyID)
	record.RPSLimit = nullIntPointer(rps)
	record.RPMLimit = nullIntPointer(rpm)
	record.TPMLimit = nullIntPointer(tpm)
	record.ConcurrencyLimit = nullIntPointer(concurrency)
	return record, err
}

func (h *Handler) normalizeAndAuthorizeLimitPolicy(ctx context.Context, record *limitPolicyRecord) error {
	if record == nil {
		return fmt.Errorf("invalid policy")
	}
	record.Name = strings.TrimSpace(record.Name)
	record.Model = strings.TrimSpace(record.Model)
	record.PolicyLabel = strings.TrimSpace(record.PolicyLabel)
	if record.Name == "" || len(record.Name) > 120 {
		return fmt.Errorf("name is required and must be at most 120 characters")
	}
	if len(record.Model) > 200 {
		return fmt.Errorf("model must be at most 200 characters")
	}
	if record.PolicyLabel != "" {
		label, validationErr := normalizePolicyLabels([]string{record.PolicyLabel})
		if validationErr != "" {
			return fmt.Errorf("%s", validationErr)
		}
		record.PolicyLabel = label
	}
	if !validLimitPolicyMode(record.Mode) {
		return fmt.Errorf("mode must be disabled, shadow, or enforce")
	}
	if record.FailureMode != limitPolicyFailClosed && record.FailureMode != limitPolicyLastKnownSafe {
		return fmt.Errorf("failure_mode must be fail_closed or last_known_safe")
	}
	if !validLimitPolicyLimits(record) {
		return fmt.Errorf("at least one positive RPM, RPS, TPM, or concurrency limit is required")
	}

	role := actorOrganizationRole(ctx)
	if !auth.IsPlatformAdmin(role) {
		actorOrgID := middleware.GetOrgIDFromContext(ctx)
		if !auth.IsOrgAdmin(role) || actorOrgID <= 0 {
			return errLimitPolicyForbidden
		}
		if record.OrgID == nil {
			record.OrgID = &actorOrgID
		}
		if *record.OrgID != actorOrgID {
			return errLimitPolicyForbidden
		}
	}
	if record.OrgID != nil && !actorCanAccessOrg(ctx, *record.OrgID) {
		return errLimitPolicyForbidden
	}
	return h.validateLimitPolicySelectorOwnership(ctx, record)
}

var errLimitPolicyForbidden = fmt.Errorf("forbidden")

func (h *Handler) validateLimitPolicySelectorOwnership(ctx context.Context, record *limitPolicyRecord) error {
	var projectOrgID int64
	if record.ProjectID != nil {
		if *record.ProjectID <= 0 {
			return fmt.Errorf("project_id must be positive")
		}
		if err := h.db.QueryRow(ctx, `SELECT org_id FROM projects WHERE id=$1`, *record.ProjectID).Scan(&projectOrgID); err != nil {
			return fmt.Errorf("project_id does not exist")
		}
		if record.OrgID == nil {
			record.OrgID = &projectOrgID
		} else if *record.OrgID != projectOrgID {
			return fmt.Errorf("project_id does not belong to org_id")
		}
	}
	if record.APIKeyID != nil {
		if *record.APIKeyID <= 0 {
			return fmt.Errorf("api_key_id must be positive")
		}
		var keyOrgID, keyProjectID int64
		if err := h.db.QueryRow(ctx, `SELECT org_id, COALESCE(project_id,0) FROM api_keys WHERE id=$1`, *record.APIKeyID).Scan(&keyOrgID, &keyProjectID); err != nil {
			return fmt.Errorf("api_key_id does not exist")
		}
		if record.OrgID == nil {
			record.OrgID = &keyOrgID
		} else if *record.OrgID != keyOrgID {
			return fmt.Errorf("api_key_id does not belong to org_id")
		}
		if record.ProjectID != nil && *record.ProjectID != keyProjectID {
			return fmt.Errorf("api_key_id does not belong to project_id")
		}
	}
	if record.OrgID != nil && !actorCanAccessOrg(ctx, *record.OrgID) {
		return errLimitPolicyForbidden
	}
	return nil
}

func validLimitPolicyLimits(record *limitPolicyRecord) bool {
	limits := []struct {
		value *int64
		max   int64
	}{
		{record.RPSLimit, 2_147_483_647},
		{record.RPMLimit, 2_147_483_647},
		{record.TPMLimit, 9_000_000_000_000_000},
		{record.ConcurrencyLimit, 2_147_483_647},
	}
	found := false
	for _, limit := range limits {
		if limit.value == nil {
			continue
		}
		if *limit.value <= 0 || *limit.value > limit.max {
			return false
		}
		found = true
	}
	return found
}

func validLimitPolicyMode(mode string) bool {
	return mode == limitPolicyModeDisabled || mode == limitPolicyModeShadow || mode == limitPolicyModeEnforce
}

func applyLimitPolicyMutation(record *limitPolicyRecord, mutation limitPolicyMutation) {
	if mutation.OrgID != nil {
		record.OrgID = positiveOrNil(mutation.OrgID)
	}
	if mutation.ProjectID != nil {
		record.ProjectID = positiveOrNil(mutation.ProjectID)
	}
	if mutation.APIKeyID != nil {
		record.APIKeyID = positiveOrNil(mutation.APIKeyID)
	}
	if mutation.Name != nil {
		record.Name = *mutation.Name
	}
	if mutation.Model != nil {
		record.Model = *mutation.Model
	}
	if mutation.PolicyLabel != nil {
		record.PolicyLabel = *mutation.PolicyLabel
	}
	if mutation.Mode != nil {
		record.Mode = *mutation.Mode
	}
	if mutation.FailureMode != nil {
		record.FailureMode = *mutation.FailureMode
	}
	if mutation.RPSLimit != nil {
		record.RPSLimit = positiveOrNil(mutation.RPSLimit)
	}
	if mutation.RPMLimit != nil {
		record.RPMLimit = positiveOrNil(mutation.RPMLimit)
	}
	if mutation.TPMLimit != nil {
		record.TPMLimit = positiveOrNil(mutation.TPMLimit)
	}
	if mutation.ConcurrencyLimit != nil {
		record.ConcurrencyLimit = positiveOrNil(mutation.ConcurrencyLimit)
	}
}

func positiveOrNil(value *int64) *int64 {
	if value == nil || *value == 0 {
		return nil
	}
	result := *value
	return &result
}

func limitPolicyPayload(record limitPolicyRecord) map[string]interface{} {
	return map[string]interface{}{
		"id": record.ID, "name": record.Name, "version": record.Version,
		"org_id": nullablePolicyID(record.OrgID), "org_name": record.OrgName,
		"project_id": nullablePolicyID(record.ProjectID), "project_name": record.ProjectName,
		"api_key_id": nullablePolicyID(record.APIKeyID), "api_key_name": record.APIKeyName,
		"api_key_prefix": record.APIKeyPrefix, "model": record.Model, "policy_label": record.PolicyLabel,
		"mode": record.Mode, "failure_mode": record.FailureMode,
		"rps_limit": nullablePolicyID(record.RPSLimit), "rpm_limit": nullablePolicyID(record.RPMLimit),
		"tpm_limit": nullablePolicyID(record.TPMLimit), "concurrency_limit": nullablePolicyID(record.ConcurrencyLimit),
		"selector_semantics": "all_fields_and_all_matching_policies",
		"created_at":         record.CreatedAt, "updated_at": record.UpdatedAt,
	}
}

func limitPolicyAuditMeta(r *http.Request, record limitPolicyRecord) auditMeta {
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

func (h *Handler) refreshLimitPolicySnapshot(ctx context.Context) bool {
	if h.limitPolicyReloader == nil {
		return false
	}
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := h.limitPolicyReloader.Refresh(refreshCtx); err != nil {
		log.Printf("[limit-policy] committed policy will converge through refresh loop: %v", err)
		return false
	}
	return true
}

func writeLimitPolicyError(w http.ResponseWriter, err error) {
	if err == errLimitPolicyForbidden {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
}

func policyMutationStatus(err error) int {
	if err == nil {
		return http.StatusInternalServerError
	}
	if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(err.Error(), "23505") {
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullablePolicyID(value *int64) interface{} {
	if value == nil {
		return nil
	}
	return *value
}

func nullablePositiveUserID(value int64) interface{} {
	if value <= 0 {
		return nil
	}
	return value
}

func nullIntPointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
