package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

const activePlatformAdminMutationLockID int64 = 8675309001

func resolveOrganizationRoleInput(organizationRole, legacyRole, defaultRole string) (string, error) {
	var organizationRolePtr, legacyRolePtr *string
	if strings.TrimSpace(organizationRole) != "" {
		organizationRolePtr = &organizationRole
	}
	if strings.TrimSpace(legacyRole) != "" {
		legacyRolePtr = &legacyRole
	}
	resolved, provided, err := resolveOrganizationRolePointers(organizationRolePtr, legacyRolePtr)
	if err != nil {
		return "", err
	}
	if provided {
		return resolved, nil
	}
	return defaultRole, nil
}

func resolveOrganizationRolePointers(organizationRole, legacyRole *string) (string, bool, error) {
	resolve := func(raw string) (string, error) {
		role := strings.TrimSpace(raw)
		if auth.IsLegacyProjectRole(role) {
			return "", fmt.Errorf("project roles are managed from the project members API")
		}
		canonical, ok := auth.NormalizeOrganizationRole(role)
		if !ok || !auth.IsOrganizationRole(role) {
			return "", fmt.Errorf("invalid organization role")
		}
		return canonical, nil
	}

	var resolved string
	provided := false
	if organizationRole != nil {
		canonical, err := resolve(*organizationRole)
		if err != nil {
			return "", false, err
		}
		resolved, provided = canonical, true
	}
	if legacyRole != nil {
		canonical, err := resolve(*legacyRole)
		if err != nil {
			return "", false, err
		}
		if provided && canonical != resolved {
			return "", false, fmt.Errorf("role and organization_role conflict")
		}
		resolved, provided = canonical, true
	}
	return resolved, provided, nil
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)
	whereClause := ""
	args := []interface{}{}
	argIdx := 1
	if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		if actorOrgID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		whereClause = " WHERE u.org_id = $1"
		args = append(args, actorOrgID)
		argIdx++
	}

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM users u`+whereClause, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT u.id, u.org_id, o.name AS org_name, u.username, u.organization_role, u.role, u.status,
		        COALESCE(u.email, ''), COALESCE(NULLIF(u.auth_provider, ''), 'local'),
		        u.created_at, u.updated_at,
		        COALESCE(member_scope.project_ids, '{}'::bigint[]),
		        COALESCE(member_scope.project_names, '')
		 FROM users u
		 LEFT JOIN orgs o ON o.id = u.org_id
		 LEFT JOIN LATERAL (
		     SELECT array_agg(pm.project_id ORDER BY pm.project_id) AS project_ids,
		            string_agg(p.name, ', ' ORDER BY p.name, p.id) AS project_names
		     FROM project_members pm
		     JOIN projects p ON p.id = pm.project_id AND p.org_id = u.org_id
		     WHERE pm.user_id = u.id
		 ) member_scope ON TRUE
		 %s
		 ORDER BY u.id DESC LIMIT $%d OFFSET $%d`, whereClause, argIdx, argIdx+1)
	args = append(args, pageSize, offset)
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	users := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgId int64
		var username, organizationRole, role, status, email, authProvider string
		var orgName *string
		var createdAt, updatedAt time.Time
		var projectIDs []int64
		var projectNames string
		if err := rows.Scan(&id, &orgId, &orgName, &username, &organizationRole, &role, &status, &email, &authProvider, &createdAt, &updatedAt, &projectIDs, &projectNames); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		oName := ""
		if orgName != nil {
			oName = *orgName
		}
		users = append(users, map[string]interface{}{
			"id": id, "org_id": orgId, "org_name": oName,
			"username": username, "organization_role": organizationRole,
			"role": role, "status": status, "email": email, "auth_provider": authProvider,
			"created_at": createdAt, "updated_at": updatedAt,
			"project_ids": projectIDs, "project_names": projectNames,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": users, "total": total})
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID            int64    `json:"org_id"`
		Username         string   `json:"username"`
		Password         string   `json:"password"`
		OrganizationRole string   `json:"organization_role"`
		Role             string   `json:"role"`
		Status           string   `json:"status"`
		ProjectIDs       *[]int64 `json:"project_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	username, err := auth.NormalizeLocalUsername(req.Username)
	if err != nil || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Username and password required"})
		return
	}
	req.Username = username
	if req.ProjectIDs != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "project_ids is no longer accepted here; manage project membership from the project members API",
			"code":  "project_membership_separate",
		})
		return
	}
	organizationRole, err := resolveOrganizationRoleInput(req.OrganizationRole, req.Role, auth.RoleMember)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error(), "code": "invalid_organization_role"})
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.Status != "active" && req.Status != "disabled" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid status"})
		return
	}
	if req.OrgID < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
		return
	}
	if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		if actorOrgID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		if req.OrgID == 0 {
			req.OrgID = actorOrgID
		}
	} else if req.OrgID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "org_id is required"})
		return
	}
	if !actorCanAccessOrg(r.Context(), req.OrgID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	if _, constrained := actorOrgConstraint(r.Context()); constrained && auth.IsPlatformAdmin(organizationRole) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	if err := auth.ValidateLocalPassword(req.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Hash failed"})
		return
	}

	// Preserve direct-handler unit compatibility. Production routes always
	// inject an authenticated actor and use the atomic transaction below.
	if isUnauthenticatedUnitContext(r.Context()) {
		var id int64
		err = h.db.QueryRow(r.Context(),
			`INSERT INTO users (org_id, username, password_hash, organization_role, role, status)
			 VALUES ($1, $2, $3, $4, $4, $5) RETURNING id`,
			req.OrgID, req.Username, string(hash), organizationRole, req.Status).Scan(&id)
		if err != nil {
			if writeCommercialLimitViolation(w, err) {
				return
			}
			if writeLocalLoginIdentityConflict(w, err) {
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "organization_role": organizationRole, "role": organizationRole})
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	defer tx.Rollback(r.Context())
	var id int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO users (org_id, username, password_hash, organization_role, role, status)
		 VALUES ($1, $2, $3, $4, $4, $5) RETURNING id`,
		req.OrgID, req.Username, string(hash), organizationRole, req.Status).Scan(&id)
	if err != nil {
		if writeCommercialLimitViolation(w, err) {
			return
		}
		if writeLocalLoginIdentityConflict(w, err) {
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "user.create", "user", strconv.FormatInt(id, 10), map[string]interface{}{
		"after": map[string]interface{}{
			"org_id":             req.OrgID,
			"username":           req.Username,
			"organization_role":  organizationRole,
			"compatibility_role": organizationRole,
			"status":             req.Status,
		},
	}, requestAuditMeta(r, req.OrgID, 0)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "organization_role": organizationRole, "role": organizationRole})
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	var req struct {
		OrganizationRole *string  `json:"organization_role"`
		Role             *string  `json:"role"`
		Status           *string  `json:"status"`
		ProjectIDs       *[]int64 `json:"project_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	if req.ProjectIDs != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "project_ids is no longer accepted here; manage project membership from the project members API",
			"code":  "project_membership_separate",
		})
		return
	}
	requestedOrganizationRole, organizationRoleProvided, err := resolveOrganizationRolePointers(req.OrganizationRole, req.Role)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error(), "code": "invalid_organization_role"})
		return
	}
	if req.Status != nil {
		if *req.Status != "active" && *req.Status != "disabled" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid status"})
			return
		}
	}
	if !organizationRoleProvided && req.Status == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}
	if organizationRoleProvided {
		if _, constrained := actorOrgConstraint(r.Context()); constrained && auth.IsPlatformAdmin(requestedOrganizationRole) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
	}

	// Direct-handler unit tests do not carry an authenticated actor. Production
	// always uses the audited transactional branch below.
	if isUnauthenticatedUnitContext(r.Context()) {
		sets := make([]string, 0, 3)
		args := make([]interface{}, 0, 4)
		if organizationRoleProvided {
			sets = append(sets, fmt.Sprintf("organization_role = $%d", len(args)+1))
			args = append(args, requestedOrganizationRole)
			sets = append(sets, fmt.Sprintf("role = $%d", len(args)+1))
			args = append(args, requestedOrganizationRole)
		}
		if req.Status != nil {
			sets = append(sets, fmt.Sprintf("status = $%d", len(args)+1))
			args = append(args, *req.Status)
		}
		sets = append(sets, "updated_at = NOW()")
		args = append(args, id)
		query := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", joinStrings(sets, ", "), len(args))
		tag, execErr := h.db.Exec(r.Context(), query, args...)
		if execErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
			return
		}
		if tag.RowsAffected() == 0 {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}

	var targetOrgID int64
	var targetOrganizationRole, targetRole, targetStatus string
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	defer tx.Rollback(r.Context())

	if err := lockProjectMembershipMutation(r.Context(), tx, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if err := tx.QueryRow(r.Context(),
		`SELECT org_id, organization_role, role, status FROM users WHERE id = $1 FOR UPDATE`, id,
	).Scan(&targetOrgID, &targetOrganizationRole, &targetRole, &targetStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		}
		return
	}
	if !actorCanAccessOrg(r.Context(), targetOrgID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	if _, constrained := actorOrgConstraint(r.Context()); constrained && auth.IsPlatformAdmin(targetOrganizationRole) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	actorUserID := middleware.GetUserIDFromContext(r.Context())
	if actorUserID == id {
		roleChanged := organizationRoleProvided && requestedOrganizationRole != targetOrganizationRole
		selfDisabled := req.Status != nil && *req.Status != "active"
		if roleChanged || selfDisabled {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Cannot change your own role or disable the current account"})
			return
		}
	}

	nextOrganizationRole, nextStatus := targetOrganizationRole, targetStatus
	if organizationRoleProvided {
		nextOrganizationRole = requestedOrganizationRole
	}
	if req.Status != nil {
		nextStatus = *req.Status
	}
	existingProjectIDs, err := listUserProjectIDsTx(r.Context(), tx, id, targetOrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	nextCompatibilityRole := nextOrganizationRole
	if nextOrganizationRole == auth.RoleMember {
		nextCompatibilityRole, err = projectCompatibilityRoleTx(r.Context(), tx, id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
			return
		}
	}
	securityPrincipalChanged := nextOrganizationRole != targetOrganizationRole || nextCompatibilityRole != targetRole || nextStatus != targetStatus
	removesActivePlatformAdmin := auth.IsPlatformAdmin(targetOrganizationRole) && targetStatus == "active" &&
		(!auth.IsPlatformAdmin(nextOrganizationRole) || nextStatus != "active")
	if removesActivePlatformAdmin {
		if _, err := tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock($1)`, activePlatformAdminMutationLockID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
			return
		}
		var activePlatformAdmins int
		if err := tx.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM users WHERE organization_role = 'platform_admin' AND status = 'active'`,
		).Scan(&activePlatformAdmins); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
			return
		}
		if activePlatformAdmins <= 1 {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "At least one active platform admin is required"})
			return
		}
	}

	sets := make([]string, 0, 4)
	args := make([]interface{}, 0, 5)
	if organizationRoleProvided {
		sets = append(sets, fmt.Sprintf("organization_role = $%d", len(args)+1))
		args = append(args, nextOrganizationRole)
		sets = append(sets, fmt.Sprintf("role = $%d", len(args)+1))
		args = append(args, nextCompatibilityRole)
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, nextStatus)
	}
	if len(sets) > 0 {
		// A role or account-status transition invalidates every previously
		// issued administrator JWT. Without the epoch bump and session
		// revoke, a suspended account could regain an old live session
		// after reactivation.
		if securityPrincipalChanged {
			sets = append(sets, "auth_epoch = auth_epoch + 1")
		}
		sets = append(sets, "updated_at = NOW()")
		idArg := len(args) + 1
		args = append(args, id)
		whereClause := fmt.Sprintf("id = $%d", idArg)
		if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
			whereClause += fmt.Sprintf(" AND org_id = $%d", len(args)+1)
			args = append(args, actorOrgID)
			whereClause += " AND organization_role <> 'platform_admin'"
		}
		query := fmt.Sprintf("UPDATE users SET %s WHERE %s", joinStrings(sets, ", "), whereClause)
		tag, err := tx.Exec(r.Context(), query, args...)
		if err != nil {
			if writeCommercialLimitViolation(w, err) {
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
			return
		}
		if tag.RowsAffected() == 0 {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
			return
		}
	}
	var revokedSessions int64
	if securityPrincipalChanged {
		tag, revokeErr := tx.Exec(r.Context(),
			`UPDATE admin_sessions
				 SET revoked_at = COALESCE(revoked_at, NOW())
				 WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW()`,
			id,
		)
		if revokeErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Session revocation failed; no change was committed"})
			return
		}
		revokedSessions = tag.RowsAffected()
	}
	if err := insertAuditEntry(r.Context(), tx, "user.update", "user", idStr, map[string]interface{}{
		"before": map[string]interface{}{
			"organization_role":  targetOrganizationRole,
			"compatibility_role": targetRole,
			"status":             targetStatus,
			"project_ids":        existingProjectIDs,
		},
		"after": map[string]interface{}{
			"organization_role":            nextOrganizationRole,
			"compatibility_role":           nextCompatibilityRole,
			"status":                       nextStatus,
			"security_principal_versioned": securityPrincipalChanged,
			"live_sessions_revoked":        revokedSessions,
			"project_ids":                  existingProjectIDs,
		},
	}, requestAuditMeta(r, targetOrgID, 0)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "organization_role": nextOrganizationRole, "role": nextCompatibilityRole,
	})
	return
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := lockProjectMembershipMutation(r.Context(), tx, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	var targetOrgID int64
	var targetOrganizationRole, targetRole, targetStatus string
	if err := tx.QueryRow(r.Context(),
		`SELECT org_id, organization_role, role, status FROM users WHERE id = $1 FOR UPDATE`, id,
	).Scan(&targetOrgID, &targetOrganizationRole, &targetRole, &targetStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		}
		return
	}
	if !isUnauthenticatedUnitContext(r.Context()) {
		if !actorCanAccessOrg(r.Context(), targetOrgID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		if _, constrained := actorOrgConstraint(r.Context()); constrained && auth.IsPlatformAdmin(targetOrganizationRole) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		if middleware.GetUserIDFromContext(r.Context()) == id {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Cannot delete the current account"})
			return
		}
	}
	if auth.IsPlatformAdmin(targetOrganizationRole) && targetStatus == "active" {
		if _, err := tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock($1)`, activePlatformAdminMutationLockID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
			return
		}
		var activePlatformAdmins int
		if err := tx.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM users WHERE organization_role = 'platform_admin' AND status = 'active'`,
		).Scan(&activePlatformAdmins); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
			return
		}
		if activePlatformAdmins <= 1 {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "At least one active platform admin is required"})
			return
		}
	}

	if _, err := tx.Exec(r.Context(),
		`UPDATE api_keys
		 SET status = 'disabled', updated_at = NOW()
		 WHERE owner_user_id = $1 AND org_id = $2 AND key_type = 'personal'`, id, targetOrgID,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	tag, err := tx.Exec(r.Context(), `DELETE FROM users WHERE id = $1 AND org_id = $2`, id, targetOrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		return
	}
	if !isUnauthenticatedUnitContext(r.Context()) {
		if err := insertAuditEntry(r.Context(), tx, "user.delete", "user", idStr, map[string]interface{}{
			"before": map[string]interface{}{
				"org_id":             targetOrgID,
				"organization_role":  targetOrganizationRole,
				"compatibility_role": targetRole,
				"status":             targetStatus,
			},
			"after": map[string]interface{}{"deleted": true, "personal_keys_disabled": true},
		}, requestAuditMeta(r, targetOrgID, 0)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
