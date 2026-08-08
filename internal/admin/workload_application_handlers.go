package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/workloadapp"
)

type workloadApplicationRecord struct {
	ID                    int64      `json:"id"`
	OrgID                 int64      `json:"org_id"`
	ProjectID             int64      `json:"project_id"`
	ApplicationRef        string     `json:"application_ref"`
	DisplayName           string     `json:"display_name"`
	Kind                  string     `json:"kind"`
	Environment           string     `json:"environment"`
	IdentityMode          string     `json:"identity_mode"`
	ClientProfile         string     `json:"client_profile"`
	OwnerTeam             string     `json:"owner_team"`
	Description           string     `json:"description"`
	Status                string     `json:"status"`
	Revision              int64      `json:"revision"`
	CreatedBy             int64      `json:"created_by"`
	UpdatedBy             int64      `json:"updated_by"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	ArchivedAt            *time.Time `json:"archived_at,omitempty"`
	ProjectName           string     `json:"project_name"`
	ProjectStatus         string     `json:"project_status"`
	CredentialCount       int        `json:"credential_count"`
	ActiveCredentialCount int        `json:"active_credential_count"`
}

type workloadCredentialRecord struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	KeyPrefix     string    `json:"key_prefix"`
	Status        string    `json:"status"`
	KeyType       string    `json:"key_type"`
	AllowedModels string    `json:"allowed_models"`
	RPMLimit      int       `json:"rpm_limit"`
	RPSLimit      int       `json:"rps_limit"`
	CreatedAt     time.Time `json:"created_at"`
}

func (h *Handler) GetWorkloadApplicationContract(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"contract_version":      workloadapp.ContractVersion,
		"readiness_version":     workloadapp.ReadinessContractVersion,
		"client_profiles":       workloadapp.ClientProfiles(),
		"identity_authority":    "product_local_model_execution_only",
		"tool_or_mcp_authority": "none",
		"legacy_credentials":    "legacy_unbound_compatible",
		"runtime_mutation":      "none",
	})
}

func (h *Handler) ListWorkloadApplications(w http.ResponseWriter, r *http.Request) {
	requestedOrg, _ := strconv.ParseInt(r.URL.Query().Get("org_id"), 10, 64)
	orgID, err := resolveWorkloadApplicationOrg(r, requestedOrg)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != "" && status != workloadapp.StatusActive && status != workloadapp.StatusDisabled && status != workloadapp.StatusArchived {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid application status"})
		return
	}
	projectID, err := parseOptionalPositiveInt64(r.URL.Query().Get("project_id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
		return
	}
	rows, err := h.db.Query(r.Context(), `
SELECT a.id, a.org_id, a.project_id, a.application_ref, a.display_name, a.kind,
       a.environment, a.identity_mode, a.client_profile, a.owner_team,
       a.description, a.status, a.revision, a.created_by, a.updated_by,
       a.created_at, a.updated_at, a.archived_at, p.name, p.status,
       COUNT(k.id), COUNT(k.id) FILTER (WHERE k.status='active')
  FROM workload_applications a
  JOIN projects p ON p.id=a.project_id AND p.org_id=a.org_id
  LEFT JOIN api_keys k ON k.workload_application_id=a.id AND k.org_id=a.org_id
 WHERE a.org_id=$1 AND ($2='' OR a.status=$2) AND ($3=0 OR a.project_id=$3)
 GROUP BY a.id, p.name, p.status
 ORDER BY a.updated_at DESC, a.id DESC
 LIMIT 200`, orgID, status, projectID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	defer rows.Close()
	items := make([]workloadApplicationRecord, 0)
	for rows.Next() {
		item, scanErr := scanWorkloadApplication(rows)
		if scanErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": len(items)})
}

func (h *Handler) GetWorkloadApplication(w http.ResponseWriter, r *http.Request) {
	orgID, err := workloadApplicationRequestOrg(r)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	item, err := readWorkloadApplication(r.Context(), h.db, orgID, r.PathValue("ref"), false)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) CreateWorkloadApplication(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OrgID     int64 `json:"org_id"`
		ProjectID int64 `json:"project_id"`
		workloadapp.Draft
	}
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid application request"})
		return
	}
	orgID, err := resolveWorkloadApplicationOrg(r, request.OrgID)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	if request.ProjectID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "project_id is required"})
		return
	}
	draft, err := workloadapp.NormalizeDraft(request.Draft)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizeWorkloadApplicationMutationTx(r.Context(), tx, orgID); err != nil {
		writeWorkloadCommitAuthorizationError(w, err)
		return
	}
	if err := requireActiveWorkloadProject(r.Context(), tx, orgID, request.ProjectID); err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	var id int64
	err = tx.QueryRow(r.Context(), `
INSERT INTO workload_applications (
    contract_version, org_id, project_id, application_ref, display_name, kind,
    environment, identity_mode, client_profile, owner_team, description,
    status, revision, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,$13,$13)
RETURNING id`, workloadapp.ContractVersion, orgID, request.ProjectID,
		draft.ApplicationRef, draft.DisplayName, draft.Kind, draft.Environment,
		draft.IdentityMode, draft.ClientProfile, draft.OwnerTeam, draft.Description,
		draft.Status, actor).Scan(&id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Application reference already exists"})
		} else {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		}
		return
	}
	after := map[string]interface{}{
		"id": id, "org_id": orgID, "project_id": request.ProjectID,
		"application_ref": draft.ApplicationRef, "kind": draft.Kind,
		"environment": draft.Environment, "client_profile": draft.ClientProfile,
		"identity_mode": draft.IdentityMode, "status": draft.Status, "revision": 1,
	}
	if err := insertAuditEntry(r.Context(), tx, "workload_application.create", "workload_application", draft.ApplicationRef,
		map[string]interface{}{"after": after}, requestAuditMeta(r, orgID, request.ProjectID)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application audit unavailable"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	item, err := readWorkloadApplication(r.Context(), h.db, orgID, draft.ApplicationRef, false)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) UpdateWorkloadApplication(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OrgID            int64  `json:"org_id"`
		ExpectedRevision int64  `json:"expected_revision"`
		DisplayName      string `json:"display_name"`
		Environment      string `json:"environment"`
		ClientProfile    string `json:"client_profile"`
		OwnerTeam        string `json:"owner_team"`
		Description      string `json:"description"`
		Status           string `json:"status"`
	}
	if err := decodeStrictJSONBody(r, &request); err != nil || request.ExpectedRevision <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid application update"})
		return
	}
	orgID, err := resolveWorkloadApplicationOrg(r, request.OrgID)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizeWorkloadApplicationMutationTx(r.Context(), tx, orgID); err != nil {
		writeWorkloadCommitAuthorizationError(w, err)
		return
	}
	before, err := readWorkloadApplication(r.Context(), tx, orgID, r.PathValue("ref"), true)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	if before.Revision != request.ExpectedRevision || before.Status == workloadapp.StatusArchived {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Application changed or is archived"})
		return
	}
	normalized, err := workloadapp.NormalizeDraft(workloadapp.Draft{
		ApplicationRef: before.ApplicationRef, DisplayName: request.DisplayName,
		Kind: before.Kind, Environment: request.Environment,
		IdentityMode: before.IdentityMode, ClientProfile: request.ClientProfile,
		OwnerTeam: request.OwnerTeam, Description: request.Description, Status: request.Status,
	})
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	tag, err := tx.Exec(r.Context(), `
UPDATE workload_applications
   SET display_name=$1, environment=$2, client_profile=$3, owner_team=$4,
       description=$5, status=$6, revision=revision+1, updated_by=$7, updated_at=NOW()
 WHERE id=$8 AND org_id=$9 AND revision=$10 AND status<>'archived'`,
		normalized.DisplayName, normalized.Environment, normalized.ClientProfile,
		normalized.OwnerTeam, normalized.Description, normalized.Status, actor,
		before.ID, orgID, request.ExpectedRevision)
	if err != nil || tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Application changed since it was loaded"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "workload_application.update", "workload_application", before.ApplicationRef,
		map[string]interface{}{
			"before": workloadApplicationAuditProjection(before),
			"after": map[string]interface{}{
				"display_name": normalized.DisplayName, "environment": normalized.Environment,
				"client_profile": normalized.ClientProfile, "owner_team": normalized.OwnerTeam,
				"status": normalized.Status, "revision": before.Revision + 1,
			},
		}, requestAuditMeta(r, orgID, before.ProjectID)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application audit unavailable"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	item, err := readWorkloadApplication(r.Context(), h.db, orgID, before.ApplicationRef, false)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) ArchiveWorkloadApplication(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OrgID            int64 `json:"org_id"`
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if err := decodeStrictJSONBody(r, &request); err != nil || request.ExpectedRevision <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid application archive request"})
		return
	}
	orgID, err := resolveWorkloadApplicationOrg(r, request.OrgID)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizeWorkloadApplicationMutationTx(r.Context(), tx, orgID); err != nil {
		writeWorkloadCommitAuthorizationError(w, err)
		return
	}
	before, err := readWorkloadApplication(r.Context(), tx, orgID, r.PathValue("ref"), true)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	if before.Status == workloadapp.StatusArchived {
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": "duplicate_noop", "application": before})
		return
	}
	if before.Revision != request.ExpectedRevision {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Application changed since it was loaded"})
		return
	}
	credentialTag, err := tx.Exec(r.Context(), `
UPDATE api_keys SET status='disabled', updated_at=NOW()
 WHERE workload_application_id=$1 AND org_id=$2 AND status='active'`, before.ID, orgID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential state unavailable"})
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	tag, err := tx.Exec(r.Context(), `
UPDATE workload_applications
   SET status='archived', archived_at=NOW(), revision=revision+1,
       updated_by=$1, updated_at=NOW()
 WHERE id=$2 AND org_id=$3 AND revision=$4 AND status<>'archived'`,
		actor, before.ID, orgID, request.ExpectedRevision)
	if err != nil || tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Application changed since it was loaded"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "workload_application.archive", "workload_application", before.ApplicationRef,
		map[string]interface{}{
			"before": workloadApplicationAuditProjection(before),
			"after": map[string]interface{}{
				"status": workloadapp.StatusArchived, "revision": before.Revision + 1,
				"disabled_credentials": credentialTag.RowsAffected(), "historical_evidence_deleted": false,
			},
		}, requestAuditMeta(r, orgID, before.ProjectID)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application audit unavailable"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	item, err := readWorkloadApplication(r.Context(), h.db, orgID, before.ApplicationRef, false)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) ListWorkloadApplicationCredentials(w http.ResponseWriter, r *http.Request) {
	orgID, err := workloadApplicationRequestOrg(r)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	application, err := readWorkloadApplication(r.Context(), h.db, orgID, r.PathValue("ref"), false)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	rows, err := h.db.Query(r.Context(), `
SELECT id, name, key_prefix, status, COALESCE(NULLIF(key_type,''),'shared'),
       COALESCE(allowed_models,''), rpm_limit, qps_limit, created_at
  FROM api_keys
 WHERE org_id=$1 AND workload_application_id=$2 AND COALESCE(purpose,'')=''
 ORDER BY id DESC`, orgID, application.ID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
		return
	}
	defer rows.Close()
	items := make([]workloadCredentialRecord, 0)
	for rows.Next() {
		var item workloadCredentialRecord
		if err := rows.Scan(&item.ID, &item.Name, &item.KeyPrefix, &item.Status, &item.KeyType,
			&item.AllowedModels, &item.RPMLimit, &item.RPSLimit, &item.CreatedAt); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": len(items)})
}

func (h *Handler) BindWorkloadApplicationCredential(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OrgID            int64 `json:"org_id"`
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if err := decodeStrictJSONBody(r, &request); err != nil || request.ExpectedRevision <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid credential binding request"})
		return
	}
	orgID, err := resolveWorkloadApplicationOrg(r, request.OrgID)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	keyID, err := strconv.ParseInt(r.PathValue("key_id"), 10, 64)
	if err != nil || keyID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key_id"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizeWorkloadApplicationMutationTx(r.Context(), tx, orgID); err != nil {
		writeWorkloadCommitAuthorizationError(w, err)
		return
	}
	application, err := readWorkloadApplication(r.Context(), tx, orgID, r.PathValue("ref"), true)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	if application.Status == workloadapp.StatusArchived || application.Revision != request.ExpectedRevision {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Application changed or is archived"})
		return
	}
	var existingApplicationID int64
	var keyType, status string
	err = tx.QueryRow(r.Context(), `
	SELECT COALESCE(NULLIF(key_type,''),'shared'), status, COALESCE(workload_application_id,0)
	  FROM api_keys
	 WHERE id=$1 AND org_id=$2 AND project_id=$3 AND COALESCE(purpose,'')=''
	 FOR UPDATE`, keyID, orgID, application.ProjectID).Scan(&keyType, &status, &existingApplicationID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "API key not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
		return
	}
	if existingApplicationID == application.ID {
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": "duplicate_noop", "application": application})
		return
	}
	if existingApplicationID > 0 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Credential is already bound to another application"})
		return
	}
	if !credentialTypeMatchesApplication(keyType, application.Kind, application.IdentityMode) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Credential identity type does not match application identity mode"})
		return
	}
	if status != "active" && status != "disabled" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Credential status cannot be bound to an application"})
		return
	}
	tag, err := tx.Exec(r.Context(), `
	UPDATE api_keys SET workload_application_id=$1, updated_at=NOW()
	 WHERE id=$2 AND org_id=$3 AND project_id=$4
	   AND COALESCE(purpose,'')='' AND workload_application_id IS NULL`,
		application.ID, keyID, orgID, application.ProjectID)
	if err != nil || tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Credential binding changed since it was loaded"})
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	tag, err = tx.Exec(r.Context(), `
UPDATE workload_applications
   SET revision=revision+1, updated_by=$1, updated_at=NOW()
 WHERE id=$2 AND org_id=$3 AND revision=$4`, actor, application.ID, orgID, application.Revision)
	if err != nil || tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Application changed since it was loaded"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "workload_application.credential_bind", "workload_application", application.ApplicationRef,
		map[string]interface{}{
			"before": map[string]interface{}{"revision": application.Revision, "credential_application_id": nil},
			"after":  map[string]interface{}{"revision": application.Revision + 1, "credential_id": keyID, "credential_status": status},
		}, requestAuditMeta(r, orgID, application.ProjectID)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application audit unavailable"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	item, err := readWorkloadApplication(r.Context(), h.db, orgID, application.ApplicationRef, false)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"result": "bound", "application": item})
}

func (h *Handler) reauthorizeWorkloadApplicationMutationTx(ctx context.Context, tx pgx.Tx, orgID int64) error {
	_, err := h.reauthorizeCommittingActorTx(ctx, tx, committingActorRequirement{
		TargetOrgID:              orgID,
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
	})
	return err
}

func writeWorkloadCommitAuthorizationError(w http.ResponseWriter, err error) {
	if errors.Is(err, errCommittingActorAuthorizationChanged) || errors.Is(err, errCommittingActorScopeNotFound) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Application scope forbidden"})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application authorization unavailable"})
}

func (h *Handler) GetWorkloadApplicationReadiness(w http.ResponseWriter, r *http.Request) {
	orgID, err := workloadApplicationRequestOrg(r)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	application, err := readWorkloadApplication(r.Context(), h.db, orgID, r.PathValue("ref"), false)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	profile, _ := workloadapp.LookupClientProfile(application.ClientProfile)
	endpoint := strings.TrimSpace(r.URL.Query().Get("endpoint"))
	if endpoint == "" && len(profile.SupportedEndpoints) > 0 {
		endpoint = profile.SupportedEndpoints[0]
	}
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	if len(model) > 200 || len(endpoint) > 160 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Readiness context is too long"})
		return
	}

	credentials, err := readActiveWorkloadCredentials(r.Context(), h.db, orgID, application.ID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
		return
	}
	credentialAllows := model == ""
	eligibleCredentials := make([]workloadReadinessCredential, 0, len(credentials))
	for _, credential := range credentials {
		if model == "" || workloadCredentialAllowsModel(credential.AllowedModels, model, model) {
			credentialAllows = true
			eligibleCredentials = append(eligibleCredentials, credential)
		}
	}

	modelState := "ready"
	resolvedModel := model
	if model != "" {
		access, accessErr := modelgov.CheckAccess(r.Context(), h.db, orgID, application.ProjectID, model)
		if accessErr != nil {
			switch {
			case errors.Is(accessErr, modelgov.ErrModelDenied), errors.Is(accessErr, modelgov.ErrModelRetired), errors.Is(accessErr, modelgov.ErrModelRequired):
				modelState = access.Reason
			default:
				modelState = "model_governance_unavailable"
			}
		} else {
			resolvedModel = access.ResolvedModel
		}
	}

	authorizationState := "legacy_authority"
	if model != "" && len(eligibleCredentials) == 0 {
		authorizationState = "credential_required"
	} else if model != "" && h.modelAuthorizationEvaluator != nil {
		authorizationState = "model_authorization_denied"
		for _, credential := range eligibleCredentials {
			input := modelauthz.Input{
				OrgID: orgID, ProjectID: application.ProjectID, APIKeyID: credential.ID,
				OwnerUserID: credential.OwnerUserID, Endpoint: endpoint,
				RequestedModel: model, ResolvedModel: resolvedModel,
			}
			decision, evaluateErr := h.modelAuthorizationEvaluator.Evaluate(r.Context(), input)
			if evaluateErr != nil {
				authorizationState = "model_authorization_unavailable"
				break
			}
			if decision.Allowed {
				authorizationState = decision.Reason
				if authorizationState == "legacy_authority_unchanged" {
					authorizationState = "legacy_authority"
				}
				break
			}
			authorizationState = decision.Reason
		}
	}

	limitState := "builtin"
	var limitStoreAvailable bool
	if err := h.db.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM distributed_limit_policies WHERE org_id=$1 OR org_id IS NULL)`, orgID).Scan(&limitStoreAvailable); err != nil {
		limitState = "limit_policy_unavailable"
	} else if limitStoreAvailable {
		limitState = "ready"
	}

	routingAvailable := false
	routingCandidates := 0
	if model != "" && h.routingController != nil && modelState == "ready" {
		preview, previewErr := h.routingController.Preview(r.Context(), resolvedModel)
		if previewErr == nil {
			routingAvailable = true
			routingCandidates = len(preview.Candidates)
		}
	}
	result := workloadapp.ResolveReadiness(workloadapp.ReadinessInput{
		ApplicationStatus: application.Status, ClientProfile: profile,
		Endpoint: endpoint, Model: model, ProjectActive: application.ProjectStatus == "active",
		ActiveCredentialCount: len(credentials), CredentialAllowsModel: credentialAllows,
		AuthorizationState: authorizationState, LimitState: limitState,
		ModelGovernanceState: modelState, RoutingCandidateCount: routingCandidates,
		RoutingPreviewAvailable: routingAvailable,
	})
	writeJSON(w, http.StatusOK, result)
}

type workloadReadinessCredential struct {
	ID            int64
	OwnerUserID   int64
	AllowedModels string
}

func readActiveWorkloadCredentials(ctx context.Context, rower interface {
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
}, orgID, applicationID int64) ([]workloadReadinessCredential, error) {
	rows, err := rower.Query(ctx, `
SELECT id, COALESCE(owner_user_id,0), COALESCE(allowed_models,'')
  FROM api_keys
 WHERE org_id=$1 AND workload_application_id=$2 AND status='active' AND COALESCE(purpose,'')=''
 ORDER BY id`, orgID, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]workloadReadinessCredential, 0)
	for rows.Next() {
		var item workloadReadinessCredential
		if err := rows.Scan(&item.ID, &item.OwnerUserID, &item.AllowedModels); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func readWorkloadApplication(ctx context.Context, rower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, orgID int64, ref string, lock bool) (workloadApplicationRecord, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	query := `
SELECT a.id, a.org_id, a.project_id, a.application_ref, a.display_name, a.kind,
       a.environment, a.identity_mode, a.client_profile, a.owner_team,
       a.description, a.status, a.revision, a.created_by, a.updated_by,
       a.created_at, a.updated_at, a.archived_at, p.name, p.status,
       (SELECT COUNT(*) FROM api_keys k WHERE k.workload_application_id=a.id AND k.org_id=a.org_id),
       (SELECT COUNT(*) FROM api_keys k WHERE k.workload_application_id=a.id AND k.org_id=a.org_id AND k.status='active')
  FROM workload_applications a
  JOIN projects p ON p.id=a.project_id AND p.org_id=a.org_id
 WHERE a.org_id=$1 AND a.application_ref=$2`
	if lock {
		query += " FOR UPDATE OF a"
	}
	item, err := scanWorkloadApplication(rower.QueryRow(ctx, query, orgID, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return workloadApplicationRecord{}, errors.New("workload application not found")
	}
	return item, err
}

func scanWorkloadApplication(row interface{ Scan(...interface{}) error }) (workloadApplicationRecord, error) {
	var item workloadApplicationRecord
	err := row.Scan(
		&item.ID, &item.OrgID, &item.ProjectID, &item.ApplicationRef,
		&item.DisplayName, &item.Kind, &item.Environment, &item.IdentityMode,
		&item.ClientProfile, &item.OwnerTeam, &item.Description, &item.Status,
		&item.Revision, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt,
		&item.UpdatedAt, &item.ArchivedAt, &item.ProjectName, &item.ProjectStatus,
		&item.CredentialCount, &item.ActiveCredentialCount,
	)
	return item, err
}

func requireActiveWorkloadProject(ctx context.Context, rower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, orgID, projectID int64) error {
	var status string
	err := rower.QueryRow(ctx, `SELECT status FROM projects WHERE id=$1 AND org_id=$2 FOR SHARE`, projectID, orgID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || status != "active" {
		return errors.New("workload application project is outside organization or inactive")
	}
	if err != nil {
		return fmtWorkloadUnavailable(err)
	}
	return nil
}

func resolveWorkloadApplicationOrg(r *http.Request, requested int64) (int64, error) {
	role := actorOrganizationRole(r.Context())
	contextOrg := middleware.GetOrgIDFromContext(r.Context())
	if auth.IsPlatformAdmin(role) {
		if requested > 0 {
			return requested, nil
		}
		if contextOrg > 0 {
			return contextOrg, nil
		}
		return 0, fmtWorkloadInvalid("org_id required")
	}
	if contextOrg <= 0 || (requested > 0 && requested != contextOrg) {
		return 0, errors.New("workload application organization forbidden")
	}
	return contextOrg, nil
}

func workloadApplicationRequestOrg(r *http.Request) (int64, error) {
	requested, err := parseOptionalPositiveInt64(r.URL.Query().Get("org_id"))
	if err != nil {
		return 0, fmtWorkloadInvalid("org_id")
	}
	return resolveWorkloadApplicationOrg(r, requested)
}

func parseOptionalPositiveInt64(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, errors.New("invalid positive integer")
	}
	return parsed, nil
}

func credentialTypeMatchesApplication(keyType, kind, identityMode string) bool {
	if identityMode == workloadapp.IdentityPersonalDevelopment {
		return kind == workloadapp.KindInteractiveClient && keyType == "personal"
	}
	return keyType == "service" || keyType == "shared"
}

func workloadCredentialAllowsModel(allowedModels, requested, resolved string) bool {
	allowedModels = strings.TrimSpace(allowedModels)
	if allowedModels == "" {
		return true
	}
	for _, value := range strings.Split(allowedModels, ",") {
		value = strings.TrimSpace(value)
		if value == "*" || value == requested || value == resolved {
			return true
		}
	}
	return false
}

func workloadApplicationAuditProjection(item workloadApplicationRecord) map[string]interface{} {
	return map[string]interface{}{
		"application_ref": item.ApplicationRef, "display_name": item.DisplayName,
		"environment": item.Environment, "client_profile": item.ClientProfile,
		"owner_team": item.OwnerTeam, "status": item.Status, "revision": item.Revision,
	}
}

func fmtWorkloadInvalid(message string) error {
	return errors.Join(workloadapp.ErrInvalid, errors.New(message))
}

func fmtWorkloadUnavailable(err error) error {
	return errors.Join(errors.New("workload application store unavailable"), err)
}

func writeWorkloadApplicationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workloadapp.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid application", "message": err.Error()})
	case strings.Contains(strings.ToLower(err.Error()), "not found"):
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Application not found"})
	case strings.Contains(strings.ToLower(err.Error()), "outside"), strings.Contains(strings.ToLower(err.Error()), "forbidden"):
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Application scope forbidden"})
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
	}
}
