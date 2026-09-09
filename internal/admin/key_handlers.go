package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/accessprovisioning"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/workloadapp"
)

const apiKeyPrefixInsertMaxAttempts = 4

type createAPIKeyRequest struct {
	OrgID            int64    `json:"org_id"`
	ProjectID        int64    `json:"project_id"`
	OwnerUserID      *int64   `json:"owner_user_id"`
	KeyType          string   `json:"key_type"`
	Name             string   `json:"name"`
	RPMLimit         int      `json:"rpm_limit"`
	RPSLimit         *int     `json:"rps_limit"`
	QPSLimit         int      `json:"qps_limit"`
	BudgetLimitCents *int64   `json:"budget_limit_cents"`
	AllowedIPs       string   `json:"allowed_ips"`
	AllowedModels    string   `json:"allowed_models"`
	PolicyLabels     []string `json:"policy_labels"`
	Purpose          string   `json:"purpose"`
	ApplicationRef   string   `json:"workload_application_ref"`
	Environment      string   `json:"environment"`
	ProductionUse    bool     `json:"production_use"`
}

var errAPIKeyAuthorizationChanged = errors.New("API-key authorization changed")

type apiKeyMutationOperation string

const (
	apiKeyMutationCreate  apiKeyMutationOperation = "create"
	apiKeyMutationUpdate  apiKeyMutationOperation = "update"
	apiKeyMutationDisable apiKeyMutationOperation = "disable"
)

type apiKeyObjectAccess uint8

const (
	apiKeyObjectRead apiKeyObjectAccess = iota
	apiKeyObjectWrite
)

type apiKeyAuthorizationIntent struct {
	operation               apiKeyMutationOperation
	targetOrgID             int64
	sourceProjectID         int64
	targetProjectID         int64
	targetProjectMustActive bool
	validateOwnership       bool
	ownerUserID             int64
	keyType                 string
	workloadApplicationID   int64
}

type apiKeyAuditState struct {
	OrgID         int64
	ProjectID     int64
	OwnerUserID   int64
	KeyType       string
	Purpose       string
	Name          string
	Status        string
	RPMLimit      int
	QPSLimit      int
	AllowedIPs    string
	AllowedModels string
	PolicyLabels  string
	AccessGrantID int64
}

func writeAPIKeyNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "API key not found"})
}

// actorScopedAPIKeyLookupClause constrains the first object lookup itself.
// That makes an absent ID and an existing but inaccessible ID indistinguishable
// and prevents handlers from interpreting purpose-specific metadata before the
// caller has live access to the key's exact project.
func actorScopedAPIKeyLookupClause(ctx context.Context, access apiKeyObjectAccess, relation string, firstArg int) (string, []interface{}, bool) {
	if isUnauthenticatedUnitContext(ctx) {
		return "", nil, true
	}
	actorOrgID, constrained := actorOrgConstraint(ctx)
	if !constrained && actorOrganizationRole(ctx) == auth.RolePlatformAdmin {
		// Compatibility path for direct-handler tests and rolling upgrades that
		// have not installed CustomerContext. Production admin routes always do.
		return "", nil, true
	}
	if actorOrgID <= 0 {
		return "", nil, false
	}
	if actorOrganizationRole(ctx) == auth.RolePlatformAdmin || actorOrganizationRole(ctx) == auth.RoleOrgAdmin {
		return fmt.Sprintf(" AND %s.org_id = $%d", relation, firstArg), []interface{}{actorOrgID}, true
	}
	actorUserID := middleware.GetUserIDFromContext(ctx)
	if actorUserID <= 0 {
		return "", nil, false
	}

	orgArg, userArg, rolesArg := firstArg, firstArg+1, firstArg+2
	roles := []string{
		projectMemberAdmin, projectMemberUser, projectMemberViewer,
		projectMemberLegacyDeveloper, auth.RoleDeveloper, auth.RoleViewer,
	}
	rolePredicate := fmt.Sprintf("pm.role = ANY($%d::text[])", rolesArg)
	if access == apiKeyObjectWrite {
		roles = []string{projectMemberAdmin, projectMemberUser, projectMemberLegacyDeveloper, auth.RoleDeveloper}
		rolePredicate = fmt.Sprintf(`(
			pm.role = '%s'
			OR (
				COALESCE(NULLIF(%s.key_type, ''), 'shared') = 'personal'
				AND %s.owner_user_id = $%d
				AND pm.role = ANY($%d::text[])
			)
		)`, projectMemberAdmin, relation, relation, userArg, rolesArg)
	} else if access != apiKeyObjectRead {
		return "", nil, false
	}

	clause := fmt.Sprintf(` AND %s.org_id = $%d
		AND %s.project_id IS NOT NULL
		AND EXISTS (
			SELECT 1
			FROM project_members pm
			JOIN projects scope_project ON scope_project.id = pm.project_id
			JOIN users scope_user ON scope_user.id = pm.user_id AND scope_user.org_id = scope_project.org_id
			WHERE pm.project_id = %s.project_id
			  AND pm.user_id = $%d
			  AND scope_project.org_id = %s.org_id
			  AND scope_user.org_id = %s.org_id
			  AND %s
		)`, relation, orgArg, relation, relation, userArg, relation, relation, rolePredicate)
	return clause, []interface{}{actorOrgID, actorUserID, roles}, true
}

func isAPIKeyPrefixCollision(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_api_keys_prefix"
}

// reauthorizeAPIKeyMutationTx establishes one commit order with user and
// project-membership revocation. It deliberately takes every user advisory
// lock before any API-key row lock; user mutation paths use the same lock
// order before disabling personal keys.
func (h *Handler) reauthorizeAPIKeyMutationTx(ctx context.Context, tx pgx.Tx, intent apiKeyAuthorizationIntent) error {
	actorUserID := middleware.GetUserIDFromContext(ctx)
	if actorUserID <= 0 || intent.targetOrgID <= 0 {
		return errAPIKeyAuthorizationChanged
	}
	if intent.validateOwnership && (!validAPIKeyType(intent.keyType) ||
		(intent.keyType == "personal" && intent.ownerUserID <= 0) ||
		(intent.keyType != "personal" && intent.ownerUserID != 0)) {
		return errAPIKeyAuthorizationChanged
	}

	additionalUserIDs := make([]int64, 0, 1)
	if intent.validateOwnership && intent.ownerUserID > 0 && intent.ownerUserID != actorUserID {
		additionalUserIDs = append(additionalUserIDs, intent.ownerUserID)
	}
	var targetProjectRequirement *committingActorProjectRequirement
	if intent.targetProjectID > 0 {
		allowedProjectRoles := []string{projectMemberAdmin}
		if intent.keyType == "personal" && intent.ownerUserID == actorUserID {
			allowedProjectRoles = append(allowedProjectRoles, projectMemberUser)
		}
		targetProjectRequirement = &committingActorProjectRequirement{
			ProjectID:    intent.targetProjectID,
			AllowedRoles: allowedProjectRoles,
			MustBeActive: intent.targetProjectMustActive,
		}
	}
	actorState, err := h.reauthorizeCommittingActorTx(ctx, tx, committingActorRequirement{
		TargetOrgID:              intent.targetOrgID,
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
		Project:                  targetProjectRequirement,
		AdditionalUserIDs:        additionalUserIDs,
	})
	if err != nil {
		if errors.Is(err, errCommittingActorAuthorizationChanged) || errors.Is(err, errCommittingActorScopeNotFound) {
			return errAPIKeyAuthorizationChanged
		}
		return err
	}
	if actorState.UserID == 0 && !isUnauthenticatedUnitContext(ctx) {
		return errAPIKeyAuthorizationChanged
	}

	switch intent.operation {
	case apiKeyMutationCreate, apiKeyMutationUpdate, apiKeyMutationDisable:
	default:
		return errAPIKeyAuthorizationChanged
	}
	projectRequirements := make(map[int64]bool, 2)
	if intent.sourceProjectID > 0 {
		projectRequirements[intent.sourceProjectID] = false
	}
	if intent.targetProjectID > 0 {
		projectRequirements[intent.targetProjectID] = projectRequirements[intent.targetProjectID] || intent.targetProjectMustActive
	}
	if intent.operation == apiKeyMutationCreate && intent.targetProjectID <= 0 {
		return errAPIKeyAuthorizationChanged
	}
	if intent.operation == apiKeyMutationUpdate && intent.sourceProjectID <= 0 &&
		actorState.OrganizationRole != auth.RolePlatformAdmin && actorState.OrganizationRole != auth.RoleOrgAdmin {
		return errAPIKeyAuthorizationChanged
	}
	projectIDs := make([]int64, 0, len(projectRequirements))
	for projectID := range projectRequirements {
		projectIDs = append(projectIDs, projectID)
	}
	sort.Slice(projectIDs, func(i, j int) bool { return projectIDs[i] < projectIDs[j] })
	for _, projectID := range projectIDs {
		mustBeActive := projectRequirements[projectID]
		if err := reauthorizeAPIKeyProjectTx(ctx, tx, actorState.OrganizationRole, actorUserID, intent.targetOrgID, projectID, mustBeActive, intent); err != nil {
			return err
		}
	}
	if intent.validateOwnership {
		if err := reauthorizeAPIKeyOwnerTx(ctx, tx, intent.targetOrgID, intent.targetProjectID, intent.ownerUserID, intent.keyType); err != nil {
			return err
		}
	}
	if intent.workloadApplicationID > 0 {
		var kind, identityMode, status string
		err := tx.QueryRow(ctx, `
			SELECT kind, identity_mode, status
			  FROM workload_applications
			 WHERE id=$1 AND org_id=$2 AND project_id=$3
			 FOR SHARE`, intent.workloadApplicationID, intent.targetOrgID, intent.targetProjectID).Scan(
			&kind, &identityMode, &status)
		if errors.Is(err, pgx.ErrNoRows) || status != workloadapp.StatusActive ||
			!credentialTypeMatchesApplication(intent.keyType, kind, identityMode) {
			return errAPIKeyAuthorizationChanged
		}
		if err != nil {
			return fmt.Errorf("read workload application authorization: %w", err)
		}
	}
	return nil
}

func reauthorizeAPIKeyProjectTx(
	ctx context.Context,
	tx pgx.Tx,
	organizationRole string,
	actorUserID, orgID, projectID int64,
	mustBeActive bool,
	intent apiKeyAuthorizationIntent,
) error {
	var projectStatus string
	err := tx.QueryRow(ctx,
		`SELECT status FROM projects WHERE id = $1 AND org_id = $2 FOR SHARE`,
		projectID, orgID,
	).Scan(&projectStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return errAPIKeyAuthorizationChanged
	}
	if err != nil {
		return fmt.Errorf("read API-key project authorization: %w", err)
	}
	if mustBeActive && projectStatus != "active" {
		return errAPIKeyAuthorizationChanged
	}
	if organizationRole == auth.RolePlatformAdmin || organizationRole == auth.RoleOrgAdmin {
		return nil
	}

	var membershipRole string
	err = tx.QueryRow(ctx,
		`SELECT pm.role
		 FROM project_members pm
		 WHERE pm.project_id = $1 AND pm.user_id = $2
		 FOR SHARE OF pm`,
		projectID, actorUserID,
	).Scan(&membershipRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return errAPIKeyAuthorizationChanged
	}
	if err != nil {
		return fmt.Errorf("read current API-key project membership: %w", err)
	}
	canonicalMembershipRole, ok := auth.NormalizeProjectMembershipRole(membershipRole)
	if !ok || canonicalMembershipRole == projectMemberViewer {
		return errAPIKeyAuthorizationChanged
	}
	if canonicalMembershipRole == projectMemberUser && (intent.keyType != "personal" || intent.ownerUserID != actorUserID) {
		return errAPIKeyAuthorizationChanged
	}
	return nil
}

func reauthorizeAPIKeyOwnerTx(ctx context.Context, tx pgx.Tx, orgID, projectID, ownerUserID int64, keyType string) error {
	if keyType != "personal" {
		if ownerUserID != 0 {
			return errAPIKeyAuthorizationChanged
		}
		return nil
	}
	if ownerUserID <= 0 {
		return errAPIKeyAuthorizationChanged
	}

	var ownerStatus string
	err := tx.QueryRow(ctx,
		`SELECT owner_user.status
		 FROM users owner_user
		 WHERE owner_user.id = $1 AND owner_user.org_id = $2
		 FOR SHARE OF owner_user`,
		ownerUserID, orgID,
	).Scan(&ownerStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return errAPIKeyAuthorizationChanged
	}
	if err != nil {
		return fmt.Errorf("read current API-key owner: %w", err)
	}
	if ownerStatus != "active" {
		return errAPIKeyAuthorizationChanged
	}
	if projectID <= 0 {
		return nil
	}

	var ownerMembershipRole string
	err = tx.QueryRow(ctx,
		`SELECT owner_membership.role
		 FROM project_members owner_membership
		 WHERE owner_membership.project_id = $1 AND owner_membership.user_id = $2
		 FOR SHARE OF owner_membership`,
		projectID, ownerUserID,
	).Scan(&ownerMembershipRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return errAPIKeyAuthorizationChanged
	}
	if err != nil {
		return fmt.Errorf("read current API-key owner membership: %w", err)
	}
	canonicalOwnerRole, ok := auth.NormalizeProjectMembershipRole(ownerMembershipRole)
	if !ok || canonicalOwnerRole == projectMemberViewer {
		return errAPIKeyAuthorizationChanged
	}
	return nil
}

func loadAPIKeyAuditStateTx(ctx context.Context, tx pgx.Tx, id int64, forUpdate bool) (apiKeyAuditState, error) {
	query := `SELECT org_id, COALESCE(project_id, 0), COALESCE(owner_user_id, 0),
	                COALESCE(NULLIF(key_type, ''), 'shared'), name, status, rpm_limit, qps_limit,
	                COALESCE(allowed_ips, ''), COALESCE(allowed_models, ''), COALESCE(policy_labels, ''),
	                COALESCE(purpose, ''), COALESCE(access_grant_id,0)
	         FROM api_keys WHERE id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	var state apiKeyAuditState
	err := tx.QueryRow(ctx, query, id).Scan(
		&state.OrgID, &state.ProjectID, &state.OwnerUserID, &state.KeyType,
		&state.Name, &state.Status, &state.RPMLimit, &state.QPSLimit,
		&state.AllowedIPs, &state.AllowedModels, &state.PolicyLabels, &state.Purpose, &state.AccessGrantID,
	)
	return state, err
}

func apiKeyChangedFieldProjection(state apiKeyAuditState, changedFields []string) map[string]interface{} {
	projection := make(map[string]interface{}, len(changedFields)+1)
	for _, field := range changedFields {
		switch field {
		case "name":
			projection[field] = state.Name
		case "allowed_ips":
			projection[field] = state.AllowedIPs
		case "allowed_models":
			projection[field] = state.AllowedModels
		case "policy_labels":
			labels := splitPolicyLabelCSV(state.PolicyLabels)
			sort.Strings(labels)
			projection[field] = labels
		case "status":
			projection[field] = state.Status
		case "project_id":
			projection[field] = state.ProjectID
		case "owner_user_id":
			projection[field] = nullableOwnerUserID(state.OwnerUserID)
		case "key_type":
			projection[field] = state.KeyType
		case "purpose":
			projection[field] = state.Purpose
		case "rpm_limit":
			projection[field] = state.RPMLimit
		case "rps_limit", "qps_limit":
			// Both names are API aliases for the same stored value. Recording both
			// makes the durable transition unambiguous without retaining secrets.
			projection["rps_limit"] = state.QPSLimit
			projection["qps_limit"] = state.QPSLimit
		}
	}
	return projection
}

func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)
	var conditions []string
	var args []interface{}
	argIdx := 1
	if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		if actorOrgID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("ak.org_id = $%d", argIdx))
		args = append(args, actorOrgID)
		argIdx++
	}
	if projectIDParam := r.URL.Query().Get("project_id"); projectIDParam != "" {
		projectID, err := strconv.ParseInt(projectIDParam, 10, 64)
		if err != nil || projectID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
			return
		}
		if !h.canReadProjectCredentialMetadata(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("ak.project_id = $%d", argIdx))
		args = append(args, projectID)
		argIdx++
	}
	organizationRole := actorOrganizationRole(r.Context())
	if organizationRole != auth.RolePlatformAdmin && organizationRole != auth.RoleOrgAdmin {
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM project_members pm
			JOIN projects scope_project ON scope_project.id = pm.project_id AND scope_project.org_id = ak.org_id
			JOIN users scope_user ON scope_user.id = pm.user_id AND scope_user.org_id = scope_project.org_id
			WHERE pm.project_id = ak.project_id AND pm.user_id = $%d
		)`, argIdx))
		args = append(args, middleware.GetUserIDFromContext(r.Context()))
		argIdx++
	}
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + joinStrings(conditions, " AND ")
	}

	var total int
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM api_keys ak %s`, whereClause)
	if err := h.db.QueryRow(r.Context(), countQuery, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		fmt.Sprintf(`SELECT ak.id, ak.org_id, COALESCE(ak.project_id, 0), ak.name, ak.key_prefix, ak.status, ak.rpm_limit, ak.qps_limit,
		        COALESCE(ak.budget_limit, 0), ak.allowed_ips, ak.allowed_models, ak.created_at,
		        o.name as org_name, COALESCE(p.name, '') as project_name,
		        COALESCE(ak.owner_user_id, 0), COALESCE(NULLIF(ak.key_type, ''), 'shared'), COALESCE(u.username, ''),
		        COALESCE(ak.policy_labels, ''), COALESCE(ak.purpose, ''),
		        COALESCE(ak.workload_application_id, 0), COALESCE(wa.application_ref, ''), COALESCE(wa.display_name, ''),
		        COALESCE(ak.access_grant_id, 0), COALESCE(ag.grant_ref, '')
		 FROM api_keys ak
		 JOIN orgs o ON o.id = ak.org_id
		 LEFT JOIN projects p ON p.id = ak.project_id AND p.org_id = ak.org_id
		 LEFT JOIN users u ON u.id = ak.owner_user_id AND u.org_id = ak.org_id
		 LEFT JOIN workload_applications wa ON wa.id = ak.workload_application_id AND wa.org_id = ak.org_id
		 LEFT JOIN access_grants ag ON ag.id = ak.access_grant_id AND ag.org_id = ak.org_id AND ag.project_id = ak.project_id
		 %s
		 ORDER BY ak.id ASC LIMIT $%d OFFSET $%d`, whereClause, argIdx, argIdx+1),
		append(args, pageSize, offset)...,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	keys := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgID, projectID, ownerUserID, workloadApplicationID, accessGrantID int64
		var name, keyPrefix, status, orgName, projectName, keyType, ownerUsername string
		var workloadApplicationRef, workloadApplicationName, grantRef string
		var rpmLimit, qpsLimit, budgetLimit int
		var allowedIPs, allowedModels, policyLabels, purpose string
		var createdAt time.Time
		if err := rows.Scan(&id, &orgID, &projectID, &name, &keyPrefix, &status, &rpmLimit, &qpsLimit, &budgetLimit, &allowedIPs, &allowedModels, &createdAt, &orgName, &projectName, &ownerUserID, &keyType, &ownerUsername, &policyLabels, &purpose, &workloadApplicationID, &workloadApplicationRef, &workloadApplicationName, &accessGrantID, &grantRef); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		var owner interface{}
		if ownerUserID > 0 {
			owner = ownerUserID
		}
		keys = append(keys, map[string]interface{}{
			"id": id, "org_id": orgID, "project_id": projectID, "name": name, "key_prefix": keyPrefix,
			"status": apiKeyDisplayStatus(status, purpose), "rpm_limit": rpmLimit, "qps_limit": qpsLimit, "rps_limit": qpsLimit,
			"budget_limit": budgetLimit, "allowed_ips": allowedIPs, "allowed_models": allowedModels,
			"policy_labels": splitPolicyLabelCSV(policyLabels),
			"created_at":    createdAt, "org_name": orgName, "project_name": projectName,
			"owner_user_id": owner, "owner_username": ownerUsername, "key_type": keyType,
			"purpose":                   purpose,
			"quota":                     apiKeyQuotaPayload(rpmLimit, qpsLimit, budgetLimit),
			"workload_application_id":   nullableWorkloadApplicationID(workloadApplicationID),
			"workload_application_ref":  workloadApplicationRef,
			"workload_application_name": workloadApplicationName,
			"access_grant_id":           nullableAccessGrantID(accessGrantID),
			"grant_ref":                 grantRef,
			"classification":            apiKeyClassification(accessGrantID, purpose),
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": keys, "total": total})
}

func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
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
	name, validationErr := normalizeAPIKeyName(req.Name)
	if validationErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": validationErr})
		return
	}
	req.Name = name
	policyLabels, labelErr := normalizePolicyLabels(req.PolicyLabels)
	if labelErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": labelErr})
		return
	}
	purpose, purposeErr := normalizeAPIKeyPurpose(req.Purpose)
	if purposeErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": purposeErr})
		return
	}
	if purpose != "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Purpose-specific feed credentials must be created from X-Insight Data Exchange control"})
		return
	}
	if policyLabels != "" && !auth.IsOrgAdmin(actorOrganizationRole(r.Context())) && !isUnauthenticatedUnitContext(r.Context()) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Only organization administrators can assign policy labels"})
		return
	}
	if req.ProjectID < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
		return
	}
	if req.RPMLimit == 0 {
		req.RPMLimit = 60
	}
	if req.RPMLimit < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "rpm_limit must be >= 0"})
		return
	}
	rpsLimit, err := createRPSLimit(req.RPSLimit, req.QPSLimit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	req.QPSLimit = rpsLimit

	if actorOrganizationRole(r.Context()) != auth.RolePlatformAdmin && actorOrganizationRole(r.Context()) != auth.RoleOrgAdmin && req.ProjectID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "project_id is required"})
		return
	}

	projectID, err := h.resolveAPIKeyProjectID(r.Context(), req.OrgID, req.ProjectID)
	if err != nil {
		if req.ProjectID > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to resolve default project"})
		}
		return
	}
	if !h.canCreateProjectKey(r.Context(), projectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	ownerUserID, keyType, err := h.resolveCreateAPIKeyOwnership(r.Context(), req.OrgID, projectID, req.OwnerUserID, req.KeyType)
	if err != nil {
		writeAPIKeyOwnershipError(w, err)
		return
	}
	if validationErr := validateCompletedFeedKeyShape(purpose, keyType, projectID, req.AllowedIPs, req.AllowedModels); validationErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": validationErr})
		return
	}
	workloadApplicationID, workloadApplicationRef, err := resolveWorkloadApplicationForCredential(
		r.Context(), h.db, req.OrgID, projectID, req.ApplicationRef, keyType, purpose,
	)
	if err != nil {
		writeWorkloadCredentialBindingError(w, err)
		return
	}
	if keyType == "shared" {
		writeLegacyKeyMigrationRequired(w, "shared credentials require the access workflow")
		return
	}
	if keyType == "service" && workloadApplicationID == 0 {
		writeLegacyKeyMigrationRequired(w, "service credentials require an exact v2 workload identity")
		return
	}
	if keyType == "personal" && workloadApplicationID != 0 {
		writeLegacyKeyMigrationRequired(w, "personal credentials cannot bind a workload identity")
		return
	}
	h.createProvisionedAPIKey(w, r, req, projectID, ownerUserID, keyType,
		workloadApplicationID, workloadApplicationRef, splitPolicyLabelCSV(policyLabels))
}

func writeLegacyKeyMigrationRequired(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusConflict, map[string]interface{}{
		"error": message, "reason": "migration_required",
	})
}

func (h *Handler) createProvisionedAPIKey(
	w http.ResponseWriter,
	r *http.Request,
	req createAPIKeyRequest,
	projectID, ownerUserID int64,
	keyType string,
	workloadApplicationID int64,
	workloadApplicationRef string,
	policyLabels []string,
) {
	actorUserID := middleware.GetUserIDFromContext(r.Context())
	if actorUserID <= 0 {
		writeAccessProvisioningError(w, accessprovisioning.ErrForbidden)
		return
	}

	provision := accessprovisioning.ProvisionInput{
		OrgID: req.OrgID, ProjectID: projectID,
		Envelope: accessprovisioning.Envelope{
			ProjectRole: auth.ProjectRoleUser,
			ScopeMode:   accessscope.ModeProjectDefault,
		},
		Environment:   strings.ToLower(strings.TrimSpace(req.Environment)),
		ProductionUse: req.ProductionUse,
	}

	switch keyType {
	case "personal":
		if provision.Environment != "" && provision.Environment != accessprovisioning.PersonalEnvironment {
			writeLegacyKeyMigrationRequired(w, "human personal access uses the canonical personal environment")
			return
		}
		if provision.ProductionUse {
			writeLegacyKeyMigrationRequired(w, "production or unattended access requires a v2 workload identity")
			return
		}
		provision.Environment = accessprovisioning.PersonalEnvironment
		provision.ProductionUse = false
		var userStatus, storedMembershipRole string
		if err := h.db.QueryRow(r.Context(), `
			SELECT u.status,pm.role
			FROM users u
			JOIN project_members pm ON pm.user_id=u.id AND pm.project_id=$3
			WHERE u.id=$1 AND u.org_id=$2`, ownerUserID, req.OrgID, projectID).Scan(
			&userStatus, &storedMembershipRole,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeLegacyKeyMigrationRequired(w, "personal credentials require an active calling membership in the exact project")
			} else {
				writeAccessProvisioningUnavailable(w)
			}
			return
		}
		projectRole, ok := auth.NormalizeProjectMembershipRole(storedMembershipRole)
		if userStatus != "active" || !ok || projectRole == auth.ProjectRoleViewer {
			writeLegacyKeyMigrationRequired(w, "personal credentials require an active calling membership in the exact project")
			return
		}
		provision.Subject = accessprovisioning.Subject{Type: accessprovisioning.SubjectUser, UserID: ownerUserID}
		provision.RequestKind = accessprovisioning.RequestKindHumanPersonal
		provision.Envelope.ProjectRole = projectRole
		switch {
		case ownerUserID == actorUserID:
			provision.SubmissionMode = accessprovisioning.SubmissionSelf
		case auth.IsOrgAdmin(actorOrganizationRole(r.Context())):
			provision.SubmissionMode = accessprovisioning.SubmissionDirectGrant
		default:
			provision.SubmissionMode = accessprovisioning.SubmissionDelegated
		}
	case "service":
		var contractVersion, identityVersion, environment, identityMode, workloadStatus string
		var productionUse bool
		var sponsorUserID int64
		if err := h.db.QueryRow(r.Context(), `
			SELECT contract_version,identity_contract_version,environment,production_use,identity_mode,status,created_by
			FROM workload_applications
			WHERE id=$1 AND org_id=$2 AND project_id=$3 AND application_ref=$4`,
			workloadApplicationID, req.OrgID, projectID, workloadApplicationRef,
		).Scan(&contractVersion, &identityVersion, &environment, &productionUse, &identityMode, &workloadStatus, &sponsorUserID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeLegacyKeyMigrationRequired(w, "service credentials require an exact v2 workload identity")
			} else {
				writeAccessProvisioningUnavailable(w)
			}
			return
		}
		if contractVersion != workloadapp.ContractVersion || identityVersion != workloadapp.IdentityContractVersion ||
			identityMode != workloadapp.IdentityServiceCredential || workloadStatus != workloadapp.StatusActive {
			writeLegacyKeyMigrationRequired(w, "service credentials require an active v2 workload identity")
			return
		}
		if requestedEnvironment := strings.ToLower(strings.TrimSpace(req.Environment)); requestedEnvironment != "" && requestedEnvironment != environment {
			writeLegacyKeyMigrationRequired(w, "service credential environment must match the immutable workload environment")
			return
		}
		if req.ProductionUse != productionUse {
			writeLegacyKeyMigrationRequired(w, "service credential production classification must match the immutable workload identity")
			return
		}
		provision.Environment = environment
		provision.ProductionUse = productionUse
		provision.Subject = accessprovisioning.Subject{
			Type: accessprovisioning.SubjectWorkloadApplication, WorkloadApplicationID: workloadApplicationID,
		}
		provision.RequestKind = accessprovisioning.RequestKindWorkloadService
		provision.SponsorUserID = sponsorUserID
		if auth.IsOrgAdmin(actorOrganizationRole(r.Context())) {
			provision.SubmissionMode = accessprovisioning.SubmissionDirectGrant
		} else {
			provision.SubmissionMode = accessprovisioning.SubmissionDelegated
		}
	default:
		writeLegacyKeyMigrationRequired(w, "ordinary credentials require an explicit personal or v2 workload identity")
		return
	}

	requestID := middleware.GetRequestIDFromContext(r.Context())
	clientRequestID := middleware.GetClientRequestIDFromContext(r.Context())
	if clientRequestID == "" {
		semantic := fmt.Sprintf("%s\x00%d\x00%d\x00%d\x00%s\x00%s\x00%s",
			requestID, actorUserID, req.OrgID, projectID, keyType, req.Name, workloadApplicationRef)
		digest := sha256.Sum256([]byte(semantic))
		clientRequestID = fmt.Sprintf("legacy-key-create-%x", digest[:12])
	}
	provision.ClientRequestID = clientRequestID
	provision.RequestID = requestID
	rpmLimit, rpsLimit := req.RPMLimit, req.QPSLimit
	if rpmLimit > accessscope.ProjectDefaultRPMLimit {
		provision.Envelope.RPMLimit = &rpmLimit
	}
	if rpsLimit > accessscope.ProjectDefaultRPSLimit {
		provision.Envelope.RPSLimit = &rpsLimit
	}
	credential := accessprovisioning.CredentialInput{
		Name: req.Name, AllowedIPs: req.AllowedIPs, AllowedModels: splitCSV(req.AllowedModels),
		AllowedEndpoints: accessscope.ProjectDefaultEndpoints(),
		RPMLimit:         &rpmLimit, RPSLimit: &rpsLimit,
		LegacyPolicyLabels: policyLabels, LegacyBudgetLimitCents: req.BudgetLimitCents,
		RequestID: requestID, ClientRequestID: clientRequestID,
	}
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	if keyType == "personal" && ownerUserID != actorUserID {
		result, err := service.Provision(r.Context(), provision)
		if err != nil {
			writeAccessProvisioningError(w, err)
			return
		}
		if result.Request.Status == accessprovisioning.RequestRejected {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"error": "Access request was rejected", "reason": "access_rejected",
				"request_ref": result.Request.RequestRef, "request_status": result.Request.Status,
			})
			return
		}
		response := map[string]interface{}{
			"status": "credential_self_service_required", "reason": "credential_self_service_required",
			"request_ref": result.Request.RequestRef, "request_status": result.Request.Status,
			"project_id": projectID, "owner_user_id": ownerUserID,
			"secret_available": false, "idempotent": result.Idempotent,
		}
		if result.Grant != nil {
			response["access_grant_id"] = result.Grant.ID
			response["grant_ref"] = result.Grant.GrantRef
		}
		writeJSON(w, http.StatusAccepted, response)
		return
	}
	result, err := service.ProvisionAndIssueCredential(r.Context(), accessActorScope(r.Context()), provision, credential)
	if err != nil {
		writeLegacyCredentialCreationError(w, err)
		return
	}
	if result.Credential == nil || result.Grant == nil {
		status := http.StatusConflict
		reason := "access_review_required"
		if result.Request.Status == accessprovisioning.RequestRejected {
			status = http.StatusForbidden
			reason = "access_rejected"
		}
		writeJSON(w, status, map[string]interface{}{
			"error": "Access approval is required before credential issuance", "reason": reason,
			"request_ref": result.Request.RequestRef, "request_status": result.Request.Status,
		})
		return
	}

	issued := result.Credential
	budgetLimit := 0
	if req.BudgetLimitCents != nil {
		budgetLimit = int(*req.BudgetLimitCents)
	}
	response := map[string]interface{}{
		"id": issued.ID, "key": issued.Key, "key_prefix": issued.KeyPrefix, "name": issued.Name,
		"credential_ref": issued.CredentialRef,
		"project_id":     projectID, "owner_user_id": nullableOwnerUserID(ownerUserID), "key_type": keyType,
		"status": issued.Status, "rpm_limit": issued.RPMLimit, "rps_limit": issued.RPSLimit, "qps_limit": issued.RPSLimit,
		"budget_limit": budgetLimit, "budget_limit_cents": req.BudgetLimitCents,
		"allowed_ips": issued.AllowedIPs, "allowed_models": strings.Join(issued.AllowedModels, ","),
		"allowed_endpoints": issued.AllowedEndpoints, "policy_labels": policyLabels, "purpose": "",
		"workload_application_id":  nullableWorkloadApplicationID(workloadApplicationID),
		"workload_application_ref": workloadApplicationRef,
		"quota":                    apiKeyQuotaPayload(issued.RPMLimit, issued.RPSLimit, budgetLimit),
		"access_grant_id":          result.Grant.ID, "grant_ref": result.Grant.GrantRef,
		"classification": issued.Classification, "secret_available": issued.SecretAvailable,
		"request_ref": result.Request.RequestRef,
	}
	if issued.RecoveryAction != "" {
		response["recovery_action"] = issued.RecoveryAction
	}
	if len(policyLabels) > 0 {
		response["runtime_snapshot_applied"] = h.refreshLimitPolicySnapshot(r.Context())
	}
	status := http.StatusCreated
	if result.Idempotent || issued.Idempotent {
		status = http.StatusOK
	}
	writeJSON(w, status, response)
}

// writeLegacyCredentialCreationError preserves the established /api/v1/keys
// response contract while applying the same ordered database error mapping as
// the canonical access-grant credential endpoints. Commercial License errors
// must be inspected before generic access provisioning classification because
// the provisioning service deliberately wraps the original PostgreSQL error.
func writeLegacyCredentialCreationError(w http.ResponseWriter, err error) {
	if writeCommercialLimitViolation(w, err) {
		return
	}
	if errors.Is(err, accessprovisioning.ErrCredentialNameConflict) {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "API key name already exists"})
		return
	}
	writeAccessProvisioningError(w, err)
}

func (h *Handler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}

	var req struct {
		Name          *string   `json:"name"`
		AllowedIPs    *string   `json:"allowed_ips"`
		AllowedModels *string   `json:"allowed_models"`
		Status        *string   `json:"status"`
		ProjectID     *int64    `json:"project_id"`
		OwnerUserID   *int64    `json:"owner_user_id"`
		KeyType       *string   `json:"key_type"`
		RPMLimit      *int      `json:"rpm_limit"`
		RPSLimit      *int      `json:"rps_limit"`
		QPSLimit      *int      `json:"qps_limit"`
		PolicyLabels  *[]string `json:"policy_labels"`
		Purpose       *string   `json:"purpose"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	if req.Purpose != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "purpose is immutable; create a dedicated service key"})
		return
	}

	var currentOrgID, currentProjectID, currentOwnerUserID, currentAccessGrantID int64
	var currentKeyType, currentPurpose, currentStatus, currentAllowedIPs, currentAllowedModels, currentGrantRef string
	if !isUnauthenticatedUnitContext(r.Context()) {
		lookupClause, lookupArgs, ok := actorScopedAPIKeyLookupClause(r.Context(), apiKeyObjectWrite, "ak", 2)
		if !ok {
			writeAPIKeyNotFound(w)
			return
		}
		queryArgs := append([]interface{}{id}, lookupArgs...)
		if err := h.db.QueryRow(r.Context(), fmt.Sprintf(
			`SELECT ak.org_id, COALESCE(ak.project_id, 0), COALESCE(ak.owner_user_id, 0),
			        COALESCE(NULLIF(ak.key_type, ''), 'shared'), COALESCE(ak.purpose, ''), ak.status,
			        COALESCE(ak.allowed_ips, ''), COALESCE(ak.allowed_models, ''),
			        COALESCE(ak.access_grant_id,0),COALESCE(ag.grant_ref,'')
			 FROM api_keys ak
			 LEFT JOIN access_grants ag ON ag.id=ak.access_grant_id AND ag.org_id=ak.org_id AND ag.project_id=ak.project_id
			 WHERE ak.id = $1%s`, lookupClause), queryArgs...,
		).Scan(
			&currentOrgID,
			&currentProjectID,
			&currentOwnerUserID,
			&currentKeyType,
			&currentPurpose,
			&currentStatus,
			&currentAllowedIPs,
			&currentAllowedModels,
			&currentAccessGrantID,
			&currentGrantRef,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeAPIKeyNotFound(w)
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read key"})
			}
			return
		}
		canAdministerKey := h.canWriteAPIKey(r.Context(), currentOrgID, currentProjectID)
		canManageOwnPersonalKey := h.canManageOwnPersonalAPIKey(
			r.Context(), currentOrgID, currentProjectID, currentOwnerUserID, currentKeyType,
		)
		if !canAdministerKey && !canManageOwnPersonalKey {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		if canManageOwnPersonalKey && !canAdministerKey &&
			(req.ProjectID != nil || req.OwnerUserID != nil || req.KeyType != nil || req.PolicyLabels != nil) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Personal credential ownership and project binding cannot be changed by its owner"})
			return
		}
	}
	if currentPurpose == middleware.APIKeyPurposeXInsightFeedRead || currentPurpose == middleware.APIKeyPurposeXInsightSourceRead {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Feed credentials are managed only from X-Insight Data Exchange control"})
		return
	}
	if currentAccessGrantID > 0 && req.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*req.Status))
		if status != "active" && status != "disabled" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid status"})
			return
		}
		if status == "active" {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error": "Revoked grant-bound credentials cannot be reactivated", "reason": "grant_credential_reactivation_forbidden",
				"grant_ref": currentGrantRef,
			})
			return
		}
		if req.Name != nil || req.AllowedIPs != nil || req.AllowedModels != nil || req.ProjectID != nil ||
			req.OwnerUserID != nil || req.KeyType != nil || req.RPMLimit != nil || req.RPSLimit != nil ||
			req.QPSLimit != nil || req.PolicyLabels != nil {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":  "Grant-bound credential revocation cannot be combined with other changes",
				"reason": "use_grant_credential_rotation", "grant_ref": currentGrantRef,
			})
			return
		}
		h.revokeGrantBoundAPIKeyCompatibility(w, r, id, currentAccessGrantID, currentGrantRef)
		return
	}
	if currentAccessGrantID > 0 && (req.Name != nil || req.AllowedIPs != nil || req.AllowedModels != nil ||
		req.ProjectID != nil || req.OwnerUserID != nil || req.KeyType != nil || req.RPMLimit != nil ||
		req.RPSLimit != nil || req.QPSLimit != nil || req.PolicyLabels != nil) {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":  "Grant-bound credential identity and scope are immutable; rotate or revoke the credential",
			"reason": "grant_credential_identity_immutable", "grant_ref": currentGrantRef,
		})
		return
	}

	var sets []string
	var args []interface{}
	argIdx := 1
	targetOrgID := currentOrgID
	targetProjectID := currentProjectID
	targetOwnerUserID := currentOwnerUserID
	targetKeyType := currentKeyType

	if req.Name != nil {
		name, validationErr := normalizeAPIKeyName(*req.Name)
		if validationErr != "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": validationErr})
			return
		}
		*req.Name = name
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.AllowedIPs != nil {
		sets = append(sets, fmt.Sprintf("allowed_ips = $%d", argIdx))
		args = append(args, *req.AllowedIPs)
		argIdx++
	}
	if req.AllowedModels != nil {
		sets = append(sets, fmt.Sprintf("allowed_models = $%d", argIdx))
		args = append(args, *req.AllowedModels)
		argIdx++
	}
	if req.PolicyLabels != nil {
		if !auth.IsOrgAdmin(actorOrganizationRole(r.Context())) && !isUnauthenticatedUnitContext(r.Context()) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Only organization administrators can assign policy labels"})
			return
		}
		policyLabels, validationErr := normalizePolicyLabels(*req.PolicyLabels)
		if validationErr != "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": validationErr})
			return
		}
		sets = append(sets, fmt.Sprintf("policy_labels = $%d", argIdx))
		args = append(args, policyLabels)
		argIdx++
	}
	if req.Status != nil {
		if *req.Status != "active" && *req.Status != "disabled" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid status"})
			return
		}
		if currentPurpose == middleware.APIKeyPurposeXInsightFeedRead && *req.Status == "active" {
			*req.Status = middleware.APIKeyStatusFeedActive
		}
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}
	if req.ProjectID != nil {
		if *req.ProjectID < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
			return
		}
		projectID, err := h.resolveAPIKeyProjectID(r.Context(), currentOrgID, *req.ProjectID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
			return
		}
		if !h.canWriteProject(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		targetProjectID = projectID
		sets = append(sets, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, projectID)
		argIdx++
	}
	if req.ProjectID != nil || req.OwnerUserID != nil || req.KeyType != nil {
		ownerUserID := currentOwnerUserID
		if req.OwnerUserID != nil {
			ownerUserID = *req.OwnerUserID
		}
		keyType := currentKeyType
		ownerWasExplicit := req.OwnerUserID != nil
		if req.KeyType != nil {
			keyType = *req.KeyType
		}
		ownerUserID, keyType, err = h.resolveUpdateAPIKeyOwnership(r.Context(), targetOrgID, targetProjectID, ownerUserID, keyType, ownerWasExplicit)
		if err != nil {
			writeAPIKeyOwnershipError(w, err)
			return
		}
		targetOwnerUserID = ownerUserID
		targetKeyType = keyType
		sets = append(sets, fmt.Sprintf("owner_user_id = $%d", argIdx))
		args = append(args, nullableOwnerUserID(ownerUserID))
		argIdx++
		sets = append(sets, fmt.Sprintf("key_type = $%d", argIdx))
		args = append(args, keyType)
		argIdx++
	}
	if req.RPMLimit != nil {
		if *req.RPMLimit <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "rpm_limit must be > 0"})
			return
		}
		sets = append(sets, fmt.Sprintf("rpm_limit = $%d", argIdx))
		args = append(args, *req.RPMLimit)
		argIdx++
	}
	rpsLimit, rpsProvided, err := updateRPSLimit(req.RPSLimit, req.QPSLimit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	if rpsProvided {
		sets = append(sets, fmt.Sprintf("qps_limit = $%d", argIdx))
		args = append(args, rpsLimit)
		argIdx++
	}

	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}

	sets = append(sets, "updated_at = NOW()")
	idArg := argIdx
	args = append(args, id)
	whereClause := fmt.Sprintf("id = $%d", idArg)
	if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		whereClause += fmt.Sprintf(" AND org_id = $%d", idArg+1)
		args = append(args, actorOrgID)
	}

	var tx pgx.Tx
	var beforeAuditState apiKeyAuditState
	execer := auditExecer(h.db)
	if !isUnauthenticatedUnitContext(r.Context()) {
		tx, err = h.db.Begin(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update key"})
			return
		}
		defer tx.Rollback(r.Context())
		execer = tx
		if err := h.reauthorizeAPIKeyMutationTx(r.Context(), tx, apiKeyAuthorizationIntent{
			operation:               apiKeyMutationUpdate,
			targetOrgID:             targetOrgID,
			sourceProjectID:         currentProjectID,
			targetProjectID:         targetProjectID,
			targetProjectMustActive: req.ProjectID != nil,
			ownerUserID:             targetOwnerUserID,
			keyType:                 targetKeyType,
			validateOwnership:       req.ProjectID != nil || req.OwnerUserID != nil || req.KeyType != nil,
		}); err != nil {
			if errors.Is(err, errAPIKeyAuthorizationChanged) {
				writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to reauthorize key update"})
			}
			return
		}
		beforeAuditState, err = loadAPIKeyAuditStateTx(r.Context(), tx, id, true)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeAPIKeyNotFound(w)
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read key"})
			}
			return
		}
		if beforeAuditState.Purpose == middleware.APIKeyPurposeXInsightFeedRead || beforeAuditState.Purpose == middleware.APIKeyPurposeXInsightSourceRead {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Feed credentials are managed only from X-Insight Data Exchange control"})
			return
		}
		if beforeAuditState.OrgID != currentOrgID || beforeAuditState.ProjectID != currentProjectID ||
			beforeAuditState.OwnerUserID != currentOwnerUserID || beforeAuditState.KeyType != currentKeyType ||
			beforeAuditState.Purpose != currentPurpose || beforeAuditState.AccessGrantID != currentAccessGrantID {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Key changed; retry the update"})
			return
		}
		if req.Name != nil && *req.Name != beforeAuditState.Name {
			lockIdentity := fmt.Sprintf("access-credential-name:%d:%d:%s", currentOrgID, currentProjectID, *req.Name)
			if _, err := tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockIdentity); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update key"})
				return
			}
			var duplicateName bool
			if err := tx.QueryRow(r.Context(), `SELECT EXISTS (
				SELECT 1 FROM api_keys WHERE org_id=$1 AND project_id=$2 AND name=$3 AND id<>$4
			)`, currentOrgID, currentProjectID, *req.Name, id).Scan(&duplicateName); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update key"})
				return
			}
			if duplicateName {
				writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "API key name already exists"})
				return
			}
		}
		// The organization is immutable, but constraining the write to the
		// authorized snapshot prevents a stale pre-read from mutating another row.
		if _, constrained := actorOrgConstraint(r.Context()); !constrained {
			whereClause += fmt.Sprintf(" AND org_id = $%d", len(args)+1)
			args = append(args, currentOrgID)
		}
	}

	query := fmt.Sprintf("UPDATE api_keys SET %s WHERE %s", joinStrings(sets, ", "), whereClause)
	tag, err := execer.Exec(r.Context(), query, args...)
	if err != nil {
		if writeCommercialLimitViolation(w, err) {
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update key"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeAPIKeyNotFound(w)
		return
	}

	changedFields := []string{}
	if req.Name != nil {
		changedFields = append(changedFields, "name")
	}
	if req.AllowedIPs != nil {
		changedFields = append(changedFields, "allowed_ips")
	}
	if req.AllowedModels != nil {
		changedFields = append(changedFields, "allowed_models")
	}
	if req.PolicyLabels != nil {
		changedFields = append(changedFields, "policy_labels")
	}
	if req.Status != nil {
		changedFields = append(changedFields, "status")
	}
	if req.ProjectID != nil {
		changedFields = append(changedFields, "project_id")
	}
	// Ownership resolution can change owner_user_id implicitly when key_type
	// changes (personal keys acquire an owner; shared/service keys clear it).
	// Preserve that effective persisted transition in the authoritative audit,
	// even when the caller did not send owner_user_id explicitly.
	if req.OwnerUserID != nil || targetOwnerUserID != currentOwnerUserID {
		changedFields = append(changedFields, "owner_user_id")
	}
	if req.KeyType != nil || targetKeyType != currentKeyType {
		changedFields = append(changedFields, "key_type")
	}
	if req.RPMLimit != nil {
		changedFields = append(changedFields, "rpm_limit")
	}
	if req.RPSLimit != nil {
		changedFields = append(changedFields, "rps_limit")
	}
	if req.QPSLimit != nil {
		changedFields = append(changedFields, "qps_limit")
	}
	auditDetail := map[string]interface{}{"fields": changedFields}
	if tx != nil {
		afterAuditState, stateErr := loadAPIKeyAuditStateTx(r.Context(), tx, id, false)
		if stateErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to capture updated key"})
			return
		}
		auditDetail["before"] = apiKeyChangedFieldProjection(beforeAuditState, changedFields)
		auditDetail["after"] = apiKeyChangedFieldProjection(afterAuditState, changedFields)
		if err := insertAuditEntry(r.Context(), tx, "apikey.update", "apikey", idStr, auditDetail,
			requestAuditMeta(r, afterAuditState.OrgID, afterAuditState.ProjectID)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update key"})
			return
		}
	} else {
		h.recordAudit(r.Context(), "apikey.update", "apikey", idStr, auditDetail, r.RemoteAddr)
	}

	response := map[string]interface{}{
		"message": "Key updated", "access_grant_id": nullableAccessGrantID(currentAccessGrantID),
		"grant_ref": currentGrantRef, "classification": apiKeyClassification(currentAccessGrantID, currentPurpose),
	}
	if req.PolicyLabels != nil {
		response["runtime_snapshot_applied"] = h.refreshLimitPolicySnapshot(r.Context())
	}
	writeJSON(w, http.StatusOK, response)
}

const apiKeyNameMaxChars = 255

func normalizeAPIKeyName(name string) (string, string) {
	normalized := strings.TrimSpace(name)
	if normalized == "" {
		return "", "name is required"
	}
	if utf8.RuneCountInString(normalized) > apiKeyNameMaxChars {
		return "", "name must be <= 255 characters"
	}
	if strings.Contains(normalized, "--") || strings.ContainsAny(normalized, "<>\"'`;\\") {
		return "", "name contains unsupported characters"
	}
	for _, r := range normalized {
		if unicode.IsControl(r) {
			return "", "name contains unsupported characters"
		}
	}
	return normalized, ""
}

func (h *Handler) DisableAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}

	var eventOrgID, eventProjectID, eventOwnerUserID, currentAccessGrantID int64
	var eventName, eventKeyPrefix, currentStatus, currentPurpose, currentKeyType, currentGrantRef string
	lookupClause, lookupArgs, ok := actorScopedAPIKeyLookupClause(r.Context(), apiKeyObjectWrite, "ak", 2)
	if !ok {
		writeAPIKeyNotFound(w)
		return
	}
	queryArgs := append([]interface{}{id}, lookupArgs...)
	if err := h.db.QueryRow(r.Context(), fmt.Sprintf(
		`SELECT ak.org_id, COALESCE(ak.project_id, 0), COALESCE(ak.owner_user_id, 0), ak.name, ak.key_prefix, ak.status,
		        COALESCE(ak.purpose, ''), COALESCE(NULLIF(ak.key_type, ''), 'shared'),
		        COALESCE(ak.access_grant_id,0),COALESCE(ag.grant_ref,'')
		 FROM api_keys ak
		 LEFT JOIN access_grants ag ON ag.id=ak.access_grant_id AND ag.org_id=ak.org_id AND ag.project_id=ak.project_id
		 WHERE ak.id = $1%s`, lookupClause),
		queryArgs...,
	).Scan(&eventOrgID, &eventProjectID, &eventOwnerUserID, &eventName, &eventKeyPrefix, &currentStatus, &currentPurpose, &currentKeyType, &currentAccessGrantID, &currentGrantRef); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAPIKeyNotFound(w)
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read key"})
		}
		return
	}
	if currentPurpose == middleware.APIKeyPurposeXInsightFeedRead || currentPurpose == middleware.APIKeyPurposeXInsightSourceRead {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Feed credentials are managed only from X-Insight Data Exchange control"})
		return
	}

	actorUserID := middleware.GetUserIDFromContext(r.Context())
	canAdministerKey := h.canWriteAPIKey(r.Context(), eventOrgID, eventProjectID)
	canManageOwnPersonalKey := h.canManageOwnPersonalAPIKey(
		r.Context(), eventOrgID, eventProjectID, eventOwnerUserID, currentKeyType,
	)
	if !canAdministerKey && !canManageOwnPersonalKey {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	if currentAccessGrantID > 0 {
		h.revokeGrantBoundAPIKeyCompatibility(w, r, id, currentAccessGrantID, currentGrantRef)
		return
	}

	query := `UPDATE api_keys SET status = 'disabled', updated_at = NOW() WHERE id = $1`
	args := []interface{}{id}
	if canManageOwnPersonalKey && !canAdministerKey {
		query += ` AND org_id = $2 AND project_id = $3 AND owner_user_id = $4`
		args = append(args, eventOrgID, eventProjectID, actorUserID)
	} else if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		query += ` AND org_id = $2`
		args = append(args, actorOrgID)
	}
	var tx pgx.Tx
	execer := auditExecer(h.db)
	if !isUnauthenticatedUnitContext(r.Context()) {
		tx, err = h.db.Begin(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to disable key"})
			return
		}
		defer tx.Rollback(r.Context())
		execer = tx
		if err := h.reauthorizeAPIKeyMutationTx(r.Context(), tx, apiKeyAuthorizationIntent{
			operation:       apiKeyMutationDisable,
			targetOrgID:     eventOrgID,
			sourceProjectID: eventProjectID,
			targetProjectID: eventProjectID,
			ownerUserID:     eventOwnerUserID,
			keyType:         currentKeyType,
		}); err != nil {
			if errors.Is(err, errAPIKeyAuthorizationChanged) {
				writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to reauthorize key disable"})
			}
			return
		}
		var lockedOrgID, lockedProjectID, lockedOwnerUserID int64
		var lockedName, lockedKeyPrefix, lockedStatus, lockedPurpose, lockedKeyType string
		if err := tx.QueryRow(r.Context(),
			`SELECT org_id, COALESCE(project_id, 0), COALESCE(owner_user_id, 0), name, key_prefix, status,
			        COALESCE(purpose, ''), COALESCE(NULLIF(key_type, ''), 'shared')
			 FROM api_keys WHERE id = $1 FOR UPDATE`, id,
		).Scan(&lockedOrgID, &lockedProjectID, &lockedOwnerUserID, &lockedName, &lockedKeyPrefix, &lockedStatus, &lockedPurpose, &lockedKeyType); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeAPIKeyNotFound(w)
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read key"})
			}
			return
		}
		if lockedPurpose == middleware.APIKeyPurposeXInsightFeedRead || lockedPurpose == middleware.APIKeyPurposeXInsightSourceRead {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Feed credentials are managed only from X-Insight Data Exchange control"})
			return
		}
		if lockedOrgID != eventOrgID || lockedProjectID != eventProjectID || lockedOwnerUserID != eventOwnerUserID ||
			lockedPurpose != currentPurpose || lockedKeyType != currentKeyType {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Key changed; retry the update"})
			return
		}
		eventName, eventKeyPrefix, currentStatus = lockedName, lockedKeyPrefix, lockedStatus
		if _, constrained := actorOrgConstraint(r.Context()); !constrained {
			query += fmt.Sprintf(" AND org_id = $%d", len(args)+1)
			args = append(args, eventOrgID)
		}
	}
	tag, err := execer.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to disable key"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeAPIKeyNotFound(w)
		return
	}

	auditDetail := map[string]interface{}{
		"before": map[string]interface{}{"status": currentStatus},
		"after":  map[string]interface{}{"status": "disabled"},
	}
	if tx != nil {
		if err := insertAuditEntry(r.Context(), tx, "apikey.disable", "apikey", idStr, auditDetail,
			requestAuditMeta(r, eventOrgID, eventProjectID)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to disable key"})
			return
		}
	} else {
		h.recordAudit(r.Context(), "apikey.disable", "apikey", idStr, auditDetail, r.RemoteAddr)
	}
	if currentStatus != "disabled" {
		h.emitAPIKeyDisabledEvent(r.Context(), eventOrgID, eventProjectID, id, eventName, eventKeyPrefix)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Key disabled"})
}

func (h *Handler) revokeGrantBoundAPIKeyCompatibility(
	w http.ResponseWriter,
	r *http.Request,
	keyID, accessGrantID int64,
	grantRef string,
) {
	requestID := middleware.GetRequestIDFromContext(r.Context())
	clientRequestID := middleware.GetClientRequestIDFromContext(r.Context())
	if clientRequestID == "" {
		actorUserID := middleware.GetUserIDFromContext(r.Context())
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", requestID, actorUserID, keyID)))
		clientRequestID = fmt.Sprintf("legacy-key-disable-%x", digest[:12])
	}
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	if err := service.RevokeCredential(r.Context(), accessActorScope(r.Context()), grantRef, keyID, requestID, clientRequestID); err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Key disabled", "classification": "grant_bound",
		"access_grant_id": accessGrantID, "grant_ref": grantRef,
	})
}

func (h *Handler) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}
	var k struct {
		ID, OrgID, ProjectID, OwnerUserID int64
		AccessGrantID                     int64
		Name, KeyPrefix                   string
		KeyType, OwnerUsername            string
		Status                            string
		RPMLimit, QPSLimit, BudgetLimit   int
		AllowedIPs, AllowedModels         string
		PolicyLabels                      string
		Purpose, GrantRef                 string
		OrgName, ProjectName              string
		CreatedAt                         time.Time
	}
	lookupClause, lookupArgs, ok := actorScopedAPIKeyLookupClause(r.Context(), apiKeyObjectRead, "ak", 2)
	if !ok {
		writeAPIKeyNotFound(w)
		return
	}
	queryArgs := append([]interface{}{id}, lookupArgs...)
	err = h.db.QueryRow(r.Context(), fmt.Sprintf(
		`SELECT ak.id, ak.org_id, COALESCE(ak.project_id, 0), ak.name, ak.key_prefix, ak.status, ak.rpm_limit, ak.qps_limit,
		        COALESCE(ak.budget_limit, 0), ak.allowed_ips, ak.allowed_models, ak.created_at, o.name, COALESCE(p.name, ''),
		        COALESCE(ak.owner_user_id, 0), COALESCE(NULLIF(ak.key_type, ''), 'shared'), COALESCE(u.username, ''),
		        COALESCE(ak.policy_labels, ''), COALESCE(ak.purpose, ''),
		        COALESCE(ak.access_grant_id,0),COALESCE(ag.grant_ref,'')
		 FROM api_keys ak JOIN orgs o ON o.id = ak.org_id
		 LEFT JOIN projects p ON p.id = ak.project_id AND p.org_id = ak.org_id
		 LEFT JOIN users u ON u.id = ak.owner_user_id AND u.org_id = ak.org_id
		 LEFT JOIN access_grants ag ON ag.id=ak.access_grant_id AND ag.org_id=ak.org_id AND ag.project_id=ak.project_id
		 WHERE ak.id = $1%s`, lookupClause), queryArgs...).Scan(
		&k.ID, &k.OrgID, &k.ProjectID, &k.Name, &k.KeyPrefix, &k.Status, &k.RPMLimit, &k.QPSLimit,
		&k.BudgetLimit, &k.AllowedIPs, &k.AllowedModels, &k.CreatedAt, &k.OrgName, &k.ProjectName, &k.OwnerUserID, &k.KeyType, &k.OwnerUsername, &k.PolicyLabels, &k.Purpose, &k.AccessGrantID, &k.GrantRef)
	if err != nil {
		writeAPIKeyNotFound(w)
		return
	}
	var owner interface{}
	if k.OwnerUserID > 0 {
		owner = k.OwnerUserID
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": k.ID, "org_id": k.OrgID, "project_id": k.ProjectID, "name": k.Name, "key_prefix": k.KeyPrefix,
		"status": apiKeyDisplayStatus(k.Status, k.Purpose), "rpm_limit": k.RPMLimit, "qps_limit": k.QPSLimit, "rps_limit": k.QPSLimit,
		"budget_limit": k.BudgetLimit, "allowed_ips": k.AllowedIPs, "allowed_models": k.AllowedModels,
		"policy_labels": splitPolicyLabelCSV(k.PolicyLabels),
		"purpose":       k.Purpose,
		"created_at":    k.CreatedAt, "org_name": k.OrgName, "project_name": k.ProjectName,
		"owner_user_id": owner, "owner_username": k.OwnerUsername, "key_type": k.KeyType,
		"quota":           apiKeyQuotaPayload(k.RPMLimit, k.QPSLimit, k.BudgetLimit),
		"access_grant_id": nullableAccessGrantID(k.AccessGrantID), "grant_ref": k.GrantRef,
		"classification": apiKeyClassification(k.AccessGrantID, k.Purpose),
	})
}

func normalizePolicyLabels(labels []string) (string, string) {
	if len(labels) > 16 {
		return "", "policy_labels supports at most 16 labels"
	}
	seen := make(map[string]struct{}, len(labels))
	normalized := make([]string, 0, len(labels))
	for _, raw := range labels {
		label := strings.TrimSpace(raw)
		if label == "" {
			continue
		}
		if len(label) > 64 || !validAPIKeyPolicyLabel(label) {
			return "", "policy_labels must use 1-64 letters, numbers, dot, underscore, colon, or dash"
		}
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		normalized = append(normalized, label)
	}
	sort.Strings(normalized)
	return strings.Join(normalized, ","), ""
}

func normalizeAPIKeyPurpose(raw string) (string, string) {
	purpose := strings.TrimSpace(raw)
	switch purpose {
	case "":
		return "", ""
	case middleware.APIKeyPurposeXInsightFeedRead:
		return purpose, ""
	default:
		return "", "Invalid purpose"
	}
}

func apiKeyDisplayStatus(status, purpose string) string {
	if purpose == middleware.APIKeyPurposeXInsightFeedRead {
		return xInsightFeedLifecycle(status)
	}
	return status
}

func validateCompletedFeedKeyShape(purpose, keyType string, projectID int64, allowedIPs, allowedModels string) string {
	if purpose == "" {
		return ""
	}
	if purpose != middleware.APIKeyPurposeXInsightFeedRead ||
		keyType != "service" ||
		projectID <= 0 ||
		strings.TrimSpace(allowedModels) != "" {
		return "xinsight.feed.read requires a project-bound service key with no model allowance"
	}
	if !validNetworkBinding(allowedIPs) {
		return "xinsight.feed.read requires a non-empty valid IP or CIDR binding"
	}
	return ""
}

func validNetworkBinding(raw string) bool {
	entries := strings.Split(raw, ",")
	if len(entries) == 0 || len(entries) > 32 {
		return false
	}
	valid := 0
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return false
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil || prefix.Addr().Zone() != "" || prefix.Bits() == 0 {
				return false
			}
		} else {
			address, err := netip.ParseAddr(entry)
			if err != nil || address.Zone() != "" {
				return false
			}
		}
		valid++
	}
	return valid > 0
}

func splitPolicyLabelCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	labels := strings.Split(value, ",")
	result := make([]string, 0, len(labels))
	for _, label := range labels {
		if trimmed := strings.TrimSpace(label); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func validAPIKeyPolicyLabel(label string) bool {
	for index, r := range label {
		if index == 0 {
			if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				continue
			}
			return false
		}
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			r == '.' || r == '_' || r == ':' || r == '-' {
			continue
		}
		return false
	}
	return label != ""
}

func createRPSLimit(preferredRPS *int, legacyQPS int) (int, error) {
	if preferredRPS != nil && legacyQPS > 0 && *preferredRPS != legacyQPS {
		return 0, fmt.Errorf("rps_limit conflicts with qps_limit")
	}
	limit := legacyQPS
	if preferredRPS != nil {
		limit = *preferredRPS
	}
	if limit == 0 {
		return 10, nil
	}
	if limit < 0 {
		return 0, fmt.Errorf("rps_limit must be >= 0")
	}
	return limit, nil
}

func updateRPSLimit(preferredRPS, legacyQPS *int) (int, bool, error) {
	if preferredRPS == nil && legacyQPS == nil {
		return 0, false, nil
	}
	if preferredRPS != nil && legacyQPS != nil && *preferredRPS != *legacyQPS {
		return 0, true, fmt.Errorf("rps_limit conflicts with qps_limit")
	}
	limit := 0
	if preferredRPS != nil {
		limit = *preferredRPS
	} else {
		limit = *legacyQPS
	}
	if limit <= 0 {
		return 0, true, fmt.Errorf("rps_limit must be > 0")
	}
	return limit, true, nil
}

func apiKeyQuotaPayload(rpmLimit, rpsLimit, budgetLimit int) map[string]interface{} {
	var budget interface{}
	if budgetLimit > 0 {
		budget = budgetLimit
	}
	return map[string]interface{}{
		"rpm_limit":          rpmLimit,
		"rps_limit":          rpsLimit,
		"qps_compat_limit":   rpsLimit,
		"tpm_limit":          nil,
		"tokens_per_second":  nil,
		"budget_limit_cents": budget,
		"tpm_source":         "not_configured",
		"compatibility_note": "qps_limit stores the same value as rps_limit for API compatibility",
	}
}

func nullableOwnerUserID(ownerUserID int64) interface{} {
	if ownerUserID > 0 {
		return ownerUserID
	}
	return nil
}

func nullableWorkloadApplicationID(applicationID int64) interface{} {
	if applicationID > 0 {
		return applicationID
	}
	return nil
}

func nullableAccessGrantID(grantID int64) interface{} {
	if grantID > 0 {
		return grantID
	}
	return nil
}

func apiKeyClassification(grantID int64, purpose string) string {
	if strings.TrimSpace(purpose) != "" {
		return "purpose_scoped"
	}
	if grantID > 0 {
		return "grant_bound"
	}
	return "legacy_unbound"
}

func resolveWorkloadApplicationForCredential(
	ctx context.Context,
	rower interface {
		QueryRow(context.Context, string, ...interface{}) pgx.Row
	},
	orgID, projectID int64,
	applicationRef, keyType, purpose string,
) (int64, string, error) {
	applicationRef = strings.ToLower(strings.TrimSpace(applicationRef))
	if applicationRef == "" {
		return 0, "", nil
	}
	if purpose != "" {
		return 0, "", fmt.Errorf("%w: purpose credentials cannot bind workload applications", workloadapp.ErrInvalid)
	}
	var applicationID int64
	var kind, identityMode, status string
	err := rower.QueryRow(ctx, `
		SELECT id, kind, identity_mode, status
		  FROM workload_applications
		 WHERE org_id=$1 AND project_id=$2 AND application_ref=$3`,
		orgID, projectID, applicationRef,
	).Scan(&applicationID, &kind, &identityMode, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", errors.New("workload application is outside credential scope")
	}
	if err != nil {
		return 0, "", fmt.Errorf("workload application store unavailable: %w", err)
	}
	if status != workloadapp.StatusActive {
		return 0, "", fmt.Errorf("%w: workload application is not active", workloadapp.ErrInvalid)
	}
	if !credentialTypeMatchesApplication(keyType, kind, identityMode) {
		return 0, "", fmt.Errorf("%w: credential identity type does not match application", workloadapp.ErrInvalid)
	}
	return applicationID, applicationRef, nil
}

func writeWorkloadCredentialBindingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workloadapp.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid application credential binding", "message": err.Error()})
	case strings.Contains(strings.ToLower(err.Error()), "outside"):
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Application credential scope forbidden"})
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
	}
}

func writeAPIKeyOwnershipError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if err.Error() == "Forbidden" {
		status = http.StatusForbidden
	}
	writeJSON(w, status, map[string]interface{}{"error": err.Error()})
}

func validAPIKeyType(keyType string) bool {
	switch keyType {
	case "shared", "personal", "service":
		return true
	default:
		return false
	}
}

func (h *Handler) resolveCreateAPIKeyOwnership(ctx context.Context, orgID, projectID int64, ownerReq *int64, keyTypeReq string) (int64, string, error) {
	actorID := middleware.GetUserIDFromContext(ctx)
	ownerUserID := int64(0)
	if ownerReq != nil {
		ownerUserID = *ownerReq
	}
	if ownerUserID < 0 {
		return 0, "", fmt.Errorf("Invalid owner_user_id")
	}

	keyType := strings.TrimSpace(keyTypeReq)
	if keyType == "" {
		keyType = "personal"
		if ownerReq == nil || ownerUserID == 0 {
			ownerUserID = actorID
		}
	}
	if keyType == "service" && !isUnauthenticatedUnitContext(ctx) &&
		actorOrganizationRole(ctx) != auth.RolePlatformAdmin && actorOrganizationRole(ctx) != auth.RoleOrgAdmin {
		if ownerUserID > 0 || !h.hasProjectMembership(ctx, projectID, projectMemberAdmin) {
			return 0, "", fmt.Errorf("Forbidden")
		}
		return 0, keyType, nil
	}
	return h.resolveAPIKeyOwnership(ctx, orgID, projectID, ownerUserID, keyType, ownerReq != nil)
}

func (h *Handler) resolveUpdateAPIKeyOwnership(ctx context.Context, orgID, projectID, ownerUserID int64, keyType string, ownerWasExplicit bool) (int64, string, error) {
	if ownerUserID < 0 {
		return 0, "", fmt.Errorf("Invalid owner_user_id")
	}
	return h.resolveAPIKeyOwnership(ctx, orgID, projectID, ownerUserID, keyType, ownerWasExplicit)
}

func (h *Handler) resolveAPIKeyOwnership(ctx context.Context, orgID, projectID, ownerUserID int64, keyType string, ownerWasExplicit bool) (int64, string, error) {
	keyType = strings.TrimSpace(keyType)
	if !validAPIKeyType(keyType) {
		return 0, "", fmt.Errorf("Invalid key_type")
	}
	actorID := middleware.GetUserIDFromContext(ctx)

	switch keyType {
	case "personal":
		if ownerUserID == 0 {
			if actorID > 0 {
				ownerUserID = actorID
			} else {
				return 0, "", fmt.Errorf("owner_user_id is required for personal keys")
			}
		}
	case "shared", "service":
		if ownerWasExplicit && ownerUserID > 0 {
			return 0, "", fmt.Errorf("owner_user_id is only valid for personal keys")
		}
		ownerUserID = 0
	}

	organizationRole := actorOrganizationRole(ctx)
	if !isUnauthenticatedUnitContext(ctx) && organizationRole != auth.RolePlatformAdmin && organizationRole != auth.RoleOrgAdmin {
		switch keyType {
		case "personal":
			if ownerUserID != actorID {
				return 0, "", fmt.Errorf("Forbidden")
			}
		case "shared":
			if !h.hasProjectMembership(ctx, projectID, projectMemberAdmin) {
				return 0, "", fmt.Errorf("Forbidden")
			}
		case "service":
			return 0, "", fmt.Errorf("Forbidden")
		}
	}
	if ownerUserID > 0 {
		if err := h.validateAPIKeyOwnerUser(ctx, orgID, projectID, ownerUserID); err != nil {
			return 0, "", err
		}
	}
	return ownerUserID, keyType, nil
}

func (h *Handler) validateAPIKeyOwnerUser(ctx context.Context, orgID, projectID, ownerUserID int64) error {
	var count int
	if err := h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE id = $1 AND org_id = $2 AND status = 'active'`,
		ownerUserID, orgID,
	).Scan(&count); err != nil || count == 0 {
		return fmt.Errorf("Invalid owner_user_id")
	}
	if projectID > 0 {
		if err := h.db.QueryRow(ctx,
			`SELECT COUNT(*)
			 FROM project_members pm
			 JOIN projects p ON p.id = pm.project_id AND p.org_id = $3
			 JOIN users u ON u.id = pm.user_id AND u.org_id = p.org_id
			 WHERE pm.project_id = $1 AND pm.user_id = $2
			   AND pm.role IN ('project_admin', 'project_user', 'project_developer', 'developer')`,
			projectID, ownerUserID, orgID,
		).Scan(&count); err != nil || count == 0 {
			return fmt.Errorf("owner_user_id is not a member of the project")
		}
	}
	return nil
}

func (h *Handler) resolveAPIKeyProjectID(ctx context.Context, orgID, requestedProjectID int64) (int64, error) {
	if orgID <= 0 {
		return 0, fmt.Errorf("invalid org_id")
	}
	if requestedProjectID > 0 {
		var projectID int64
		err := h.db.QueryRow(ctx,
			`SELECT id FROM projects WHERE id = $1 AND org_id = $2 AND status = 'active'`,
			requestedProjectID, orgID,
		).Scan(&projectID)
		return projectID, err
	}

	// PRD2 establishes one active default project as an organization invariant.
	// Key mutation paths must not silently create a control-plane resource as a
	// pre-transaction side effect: a later authorization or audit failure would
	// otherwise leave an unaudited project behind. Fail closed when the invariant
	// is broken and let the organization/project lifecycle repair it explicitly.
	var projectID int64
	err := h.db.QueryRow(ctx,
		`SELECT id FROM projects WHERE org_id = $1 AND slug = 'default' AND status = 'active'`,
		orgID,
	).Scan(&projectID)
	return projectID, err
}
