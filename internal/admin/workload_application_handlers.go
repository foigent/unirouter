package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessgrantlifecycle"
	"github.com/uniroute/unirouter/internal/accessrequestlifecycle"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/workloadapp"
)

type workloadApplicationRecord struct {
	ID                      int64      `json:"id"`
	OrgID                   int64      `json:"org_id"`
	ProjectID               int64      `json:"project_id"`
	ApplicationRef          string     `json:"application_ref"`
	ContractVersion         string     `json:"contract_version"`
	IdentityContractVersion string     `json:"identity_contract_version"`
	LogicalRef              string     `json:"logical_ref,omitempty"`
	DisplayName             string     `json:"display_name"`
	Kind                    string     `json:"kind"`
	Environment             string     `json:"environment"`
	ProductionUse           bool       `json:"production_use"`
	IdentityMode            string     `json:"identity_mode"`
	ClientProfile           string     `json:"client_profile,omitempty"`
	ClientProfiles          []string   `json:"client_profiles"`
	OwnerTeam               string     `json:"owner_team"`
	Description             string     `json:"description"`
	Status                  string     `json:"status"`
	Revision                int64      `json:"revision"`
	CreatedBy               int64      `json:"created_by"`
	UpdatedBy               int64      `json:"updated_by"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	ArchivedAt              *time.Time `json:"archived_at,omitempty"`
	ProjectName             string     `json:"project_name"`
	ProjectStatus           string     `json:"project_status"`
	CredentialCount         int        `json:"credential_count"`
	ActiveCredentialCount   int        `json:"active_credential_count"`
	IdentityClassification  string     `json:"identity_classification"`
	CompatibilityEvidence   string     `json:"compatibility_evidence,omitempty"`
	CanManage               bool       `json:"can_manage"`
	CanRequestAccess        bool       `json:"can_request_access"`
	CanReadCredentials      bool       `json:"can_read_credentials"`
}

type workloadCredentialRecord struct {
	ID                int64                             `json:"id"`
	CredentialRef     string                            `json:"credential_ref,omitempty"`
	Name              string                            `json:"name"`
	KeyPrefix         string                            `json:"key_prefix,omitempty"`
	Status            string                            `json:"status"`
	EffectiveStatus   string                            `json:"effective_status"`
	KeyType           string                            `json:"key_type"`
	AllowedIPs        string                            `json:"allowed_ips"`
	AllowedModels     string                            `json:"allowed_models"`
	AllowedEndpoints  string                            `json:"allowed_endpoints"`
	PolicyLabels      string                            `json:"policy_labels,omitempty"`
	RPMLimit          int                               `json:"rpm_limit"`
	RPSLimit          int                               `json:"rps_limit"`
	ConcurrencyLimit  *int                              `json:"concurrency_limit,omitempty"`
	BudgetLimitCents  *int64                            `json:"budget_limit_cents,omitempty"`
	GrantRef          string                            `json:"grant_ref,omitempty"`
	Classification    string                            `json:"classification"`
	VersionCount      int                               `json:"version_count"`
	VersionsTruncated bool                              `json:"versions_truncated"`
	Versions          []workloadCredentialVersionRecord `json:"versions"`
	CreatedAt         time.Time                         `json:"created_at"`
}

const (
	maxWorkloadCredentialPage              = 1000
	maxWorkloadCredentialVersionsPerFamily = 20
	maxWorkloadReadinessCredentials        = 100
)

type workloadCredentialPageItem struct {
	Classification string
	ID             int64
}

type workloadCredentialVersionRecord struct {
	ID               int64     `json:"id"`
	KeyPrefix        string    `json:"key_prefix"`
	Status           string    `json:"status"`
	EffectiveStatus  string    `json:"effective_status"`
	CreatedAt        time.Time `json:"created_at"`
	RotatedFromKeyID *int64    `json:"rotated_from_key_id,omitempty"`
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
	rows, err := h.db.Query(r.Context(), fmt.Sprintf(`
SELECT a.id, a.org_id, a.project_id, a.application_ref,a.contract_version,
	   a.identity_contract_version,COALESCE(a.logical_ref,''),a.display_name, a.kind,
	   a.environment, a.production_use, a.identity_mode, COALESCE(a.client_profile,''),
	   ARRAY(SELECT cp.client_profile FROM workload_application_client_profiles cp
	         WHERE cp.workload_application_id=a.id ORDER BY cp.client_profile),a.owner_team,
       a.description, a.status, a.revision, a.created_by, a.updated_by,
       a.created_at, a.updated_at, a.archived_at, p.name, p.status,
       %s, %s
  FROM workload_applications a
  JOIN projects p ON p.id=a.project_id AND p.org_id=a.org_id
 WHERE a.org_id=$1 AND ($2='' OR a.status=$2) AND ($3=0 OR a.project_id=$3)
   AND (
       $4='platform_admin' OR $4 IN ('org_admin','org_viewer')
       OR EXISTS (
           SELECT 1 FROM project_members actor_pm
           WHERE actor_pm.project_id=a.project_id AND actor_pm.user_id=$5
             AND actor_pm.role IN ('project_admin','project_user','project_viewer','project_developer','developer','viewer')
       )
   )
 ORDER BY a.updated_at DESC, a.id DESC
 LIMIT 200`, workloadCredentialSeatCountSQL("a"), workloadActiveCredentialSeatCountSQL("a", "p")),
		orgID, status, projectID, actorOrganizationRole(r.Context()), middleware.GetUserIDFromContext(r.Context()))
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
		item, scanErr = h.decorateWorkloadApplicationCapabilities(r.Context(), item)
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
	item, err = h.decorateWorkloadApplicationCapabilities(r.Context(), item)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) CreateWorkloadApplication(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OrgID         int64  `json:"org_id"`
		ProjectID     int64  `json:"project_id"`
		IdentityMode  string `json:"identity_mode,omitempty"`
		ClientProfile string `json:"client_profile,omitempty"`
		workloadapp.V2Draft
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
	if !h.canWriteProject(r.Context(), request.ProjectID) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Workload application not found"})
		return
	}
	if request.IdentityMode != "" && request.IdentityMode != workloadapp.IdentityServiceCredential {
		writeWorkloadApplicationError(w, workloadapp.ErrInvalid)
		return
	}
	kind, kindAdapted, err := workloadapp.NormalizeCompatibleV2Kind(request.Kind)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	request.Kind = kind
	identityEvidence := "native_v2"
	if kindAdapted {
		identityEvidence = "legacy_endpoint_payload_adapted_to_v2"
	}
	generatedApplicationRef := false
	if strings.TrimSpace(request.ApplicationRef) == "" {
		request.ApplicationRef, err = newWorkloadApplicationRef()
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application identity generation unavailable"})
			return
		}
		generatedApplicationRef = true
		identityEvidence = "server_generated_v2_identity"
	}
	if strings.TrimSpace(request.LogicalRef) == "" {
		if request.IdentityMode != workloadapp.IdentityServiceCredential {
			writeWorkloadApplicationError(w, workloadapp.ErrInvalid)
			return
		}
		request.LogicalRef, err = workloadapp.DeriveV2LogicalRef(request.ApplicationRef)
		if err != nil {
			writeWorkloadApplicationError(w, err)
			return
		}
		if !generatedApplicationRef {
			identityEvidence = "legacy_endpoint_payload_adapted_to_v2"
		}
	}
	if len(request.ClientProfiles) == 0 && strings.TrimSpace(request.ClientProfile) != "" {
		request.ClientProfiles = []string{request.ClientProfile}
	}
	if request.PrimaryClientProfile == "" && strings.TrimSpace(request.ClientProfile) != "" {
		request.PrimaryClientProfile = request.ClientProfile
	}
	draft, err := workloadapp.NormalizeV2Draft(request.V2Draft)
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
	if err := h.reauthorizeWorkloadApplicationMutationTx(r.Context(), tx, orgID, request.ProjectID); err != nil {
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
	contract_version,identity_contract_version,org_id,project_id,application_ref,logical_ref,
	display_name,kind,environment,production_use,environment_locked_at,identity_mode,client_profile,
	owner_team,description,status,revision,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),$11,NULLIF($12,''),$13,$14,$15,1,$16,$16)
	RETURNING id`, workloadapp.ContractVersion, workloadapp.IdentityContractVersion, orgID, request.ProjectID,
		draft.ApplicationRef, draft.LogicalRef, draft.DisplayName, draft.Kind, draft.Environment, draft.ProductionUse,
		workloadapp.IdentityServiceCredential, draft.PrimaryClientProfile, draft.OwnerTeam, draft.Description,
		draft.Status, actor).Scan(&id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Application reference already exists"})
		} else {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		}
		return
	}
	for _, profile := range draft.ClientProfiles {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO workload_application_client_profiles (
			  workload_application_id,org_id,client_profile,is_primary
			) VALUES ($1,$2,$3,$4)
			ON CONFLICT (workload_application_id,client_profile)
			DO UPDATE SET org_id=EXCLUDED.org_id,is_primary=EXCLUDED.is_primary,updated_at=NOW()`,
			id, orgID, profile, profile == draft.PrimaryClientProfile); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application profile store unavailable"})
			return
		}
	}
	after := map[string]interface{}{
		"id": id, "org_id": orgID, "project_id": request.ProjectID,
		"application_ref": draft.ApplicationRef, "logical_ref": draft.LogicalRef, "kind": draft.Kind,
		"environment": draft.Environment, "production_use": draft.ProductionUse, "client_profiles": draft.ClientProfiles,
		"identity_contract_version": workloadapp.IdentityContractVersion,
		"compatibility_evidence":    identityEvidence,
		"identity_mode":             workloadapp.IdentityServiceCredential, "status": draft.Status, "revision": 1,
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
	if identityEvidence != "native_v2" {
		item.CompatibilityEvidence = identityEvidence
	}
	item, err = h.decorateWorkloadApplicationCapabilities(r.Context(), item)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func newWorkloadApplicationRef() (string, error) {
	var entropy [12]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return "app_generated_" + hex.EncodeToString(entropy[:]), nil
}

func (h *Handler) UpdateWorkloadApplication(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OrgID                int64    `json:"org_id"`
		ExpectedRevision     int64    `json:"expected_revision"`
		DisplayName          string   `json:"display_name"`
		Environment          string   `json:"environment,omitempty"`
		ClientProfile        string   `json:"client_profile,omitempty"`
		ClientProfiles       []string `json:"client_profiles"`
		PrimaryClientProfile string   `json:"primary_client_profile,omitempty"`
		OwnerTeam            string   `json:"owner_team"`
		Description          string   `json:"description"`
		Status               string   `json:"status"`
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
	visible, err := readWorkloadApplication(r.Context(), h.db, orgID, r.PathValue("ref"), false)
	if err != nil || !h.canWriteProject(r.Context(), visible.ProjectID) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Workload application not found"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizeWorkloadApplicationMutationTx(r.Context(), tx, orgID, visible.ProjectID, visible.ID); err != nil {
		writeWorkloadCommitAuthorizationError(w, err)
		return
	}
	before, err := readWorkloadApplicationForProject(r.Context(), tx, orgID, visible.ProjectID, r.PathValue("ref"), true)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	if before.Revision != request.ExpectedRevision || before.Status == workloadapp.StatusArchived {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Application changed or is archived"})
		return
	}
	if before.IdentityContractVersion != workloadapp.IdentityContractVersion {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":       "Legacy workload identity requires explicit migration review",
			"reason_code": "legacy_workload_review_required",
		})
		return
	}
	if request.Environment != "" && strings.ToLower(strings.TrimSpace(request.Environment)) != before.Environment {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Workload environment is immutable"})
		return
	}
	if len(request.ClientProfiles) == 0 && strings.TrimSpace(request.ClientProfile) != "" {
		request.ClientProfiles = []string{request.ClientProfile}
	}
	if request.PrimaryClientProfile == "" && strings.TrimSpace(request.ClientProfile) != "" {
		request.PrimaryClientProfile = request.ClientProfile
	}
	normalized, err := workloadapp.NormalizeV2Draft(workloadapp.V2Draft{
		ApplicationRef: before.ApplicationRef, LogicalRef: before.LogicalRef,
		DisplayName: request.DisplayName, Kind: before.Kind, Environment: before.Environment,
		ProductionUse:  before.ProductionUse,
		ClientProfiles: request.ClientProfiles, PrimaryClientProfile: request.PrimaryClientProfile,
		OwnerTeam: request.OwnerTeam, Description: request.Description, Status: request.Status,
	})
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	if _, err := tx.Exec(r.Context(), `DELETE FROM workload_application_client_profiles WHERE workload_application_id=$1`, before.ID); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application profile store unavailable"})
		return
	}
	tag, err := tx.Exec(r.Context(), `
UPDATE workload_applications
	SET display_name=$1,client_profile=NULLIF($2,''),owner_team=$3,
	    description=$4,status=$5,revision=revision+1,updated_by=$6,updated_at=NOW()
	WHERE id=$7 AND org_id=$8 AND project_id=$9 AND revision=$10 AND status<>'archived'`,
		normalized.DisplayName, normalized.PrimaryClientProfile,
		normalized.OwnerTeam, normalized.Description, normalized.Status, actor,
		before.ID, orgID, visible.ProjectID, request.ExpectedRevision)
	if err != nil || tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Application changed since it was loaded"})
		return
	}
	for _, profile := range normalized.ClientProfiles {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO workload_application_client_profiles (
			  workload_application_id,org_id,client_profile,is_primary
			) VALUES ($1,$2,$3,$4)
			ON CONFLICT (workload_application_id,client_profile)
			DO UPDATE SET org_id=EXCLUDED.org_id,is_primary=EXCLUDED.is_primary,updated_at=NOW()`,
			before.ID, orgID, profile, profile == normalized.PrimaryClientProfile); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application profile store unavailable"})
			return
		}
	}
	if err := insertAuditEntry(r.Context(), tx, "workload_application.update", "workload_application", before.ApplicationRef,
		map[string]interface{}{
			"before": workloadApplicationAuditProjection(before),
			"after": map[string]interface{}{
				"display_name": normalized.DisplayName, "environment": normalized.Environment,
				"client_profiles": normalized.ClientProfiles, "primary_client_profile": normalized.PrimaryClientProfile,
				"owner_team": normalized.OwnerTeam,
				"status":     normalized.Status, "revision": before.Revision + 1,
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
	item, err = h.decorateWorkloadApplicationCapabilities(r.Context(), item)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
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
	visible, err := readWorkloadApplication(r.Context(), h.db, orgID, r.PathValue("ref"), false)
	if err != nil || !h.canWriteProject(r.Context(), visible.ProjectID) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Workload application not found"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizeWorkloadApplicationMutationTx(r.Context(), tx, orgID, visible.ProjectID, visible.ID); err != nil {
		writeWorkloadCommitAuthorizationError(w, err)
		return
	}
	before, err := readWorkloadApplicationForProject(r.Context(), tx, orgID, visible.ProjectID, r.PathValue("ref"), true)
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
	actor := middleware.GetUserIDFromContext(r.Context())
	now := time.Now().UTC()
	requestLifecycle, err := accessrequestlifecycle.CancelForWorkload(
		r.Context(), tx, orgID, before.ProjectID, before.ID, actor,
		middleware.GetRequestIDFromContext(r.Context()), middleware.GetClientRequestIDFromContext(r.Context()), now,
	)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Pending access request closure unavailable"})
		return
	}
	credentialContainment, err := accessgrantlifecycle.RevokeWorkload(
		r.Context(), tx, orgID, before.ProjectID, before.ID,
		actor, accessgrantlifecycle.ReasonWorkloadArchive, now,
	)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential state unavailable"})
		return
	}
	tag, err := tx.Exec(r.Context(), `
UPDATE workload_applications
   SET status='archived', archived_at=NOW(), revision=revision+1,
       updated_by=$1, updated_at=NOW()
 WHERE id=$2 AND org_id=$3 AND project_id=$4 AND revision=$5 AND status<>'archived'`,
		actor, before.ID, orgID, visible.ProjectID, request.ExpectedRevision)
	if err != nil || tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Application changed since it was loaded"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "workload_application.archive", "workload_application", before.ApplicationRef,
		map[string]interface{}{
			"before": workloadApplicationAuditProjection(before),
			"after": map[string]interface{}{
				"status": workloadapp.StatusArchived, "revision": before.Revision + 1,
				"credential_families_revoked": credentialContainment.CredentialFamilies,
				"secret_versions_disabled":    credentialContainment.SecretVersions,
				"legacy_keys_disabled":        credentialContainment.LegacyKeys,
				"pending_requests_cancelled":  requestLifecycle.RequestsCancelled,
				"historical_evidence_deleted": false,
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
	item, err = h.decorateWorkloadApplicationCapabilities(r.Context(), item)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) ListWorkloadApplicationCredentials(w http.ResponseWriter, r *http.Request) {
	page, pageSize, err := parseBoundedWorkloadCredentialPagination(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid credential pagination"})
		return
	}
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
	canManage := h.canWriteProject(r.Context(), application.ProjectID)
	actorUserID := middleware.GetUserIDFromContext(r.Context())
	if !canManage {
		var sponsorMayRead bool
		if err := h.db.QueryRow(r.Context(), `SELECT EXISTS (
			SELECT 1
			FROM access_grants g
			JOIN users sponsor ON sponsor.id=g.sponsor_user_id AND sponsor.org_id=g.org_id
			WHERE g.org_id=$1 AND g.project_id=$2
			  AND g.subject_type='workload_application' AND g.subject_workload_application_id=$3
			  AND g.sponsor_user_id=$4 AND sponsor.status='active'
			  AND (
			    sponsor.organization_role IN ('platform_admin','org_admin')
			    OR EXISTS (
			      SELECT 1 FROM project_members sponsor_pm
			      WHERE sponsor_pm.project_id=g.project_id AND sponsor_pm.user_id=sponsor.id
			        AND sponsor_pm.role IN ('project_admin','project_user','project_developer','developer')
			    )
			  )
		)`, orgID, application.ProjectID, application.ID, actorUserID).Scan(&sponsorMayRead); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
			return
		}
		if !sponsorMayRead {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Workload application not found"})
			return
		}
	}
	selectorRows, err := h.db.Query(r.Context(), `
WITH credential_candidates AS (
	SELECT 'grant_bound'::text AS classification,ac.id,ac.created_at
	  FROM access_credentials ac
	  JOIN access_grants g
	    ON g.id=ac.access_grant_id AND g.org_id=ac.org_id AND g.project_id=ac.project_id
	   AND g.subject_type='workload_application' AND g.subject_workload_application_id=$2
	 WHERE ac.org_id=$1 AND ac.project_id=$3
	   AND ($4::boolean OR g.sponsor_user_id=$5)
	UNION ALL
	SELECT 'legacy_unbound'::text,k.id,k.created_at
	  FROM api_keys k
	 WHERE $4::boolean AND k.org_id=$1 AND k.project_id=$3 AND k.workload_application_id=$2
	   AND k.access_credential_id IS NULL AND COALESCE(k.purpose,'')=''
)
SELECT classification,id
  FROM credential_candidates
 ORDER BY created_at DESC,id DESC,classification
 LIMIT $6 OFFSET $7`, orgID, application.ID, application.ProjectID, canManage, actorUserID,
		pageSize+1, paginationOffset(page, pageSize))
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
		return
	}
	selectors := make([]workloadCredentialPageItem, 0, pageSize+1)
	for selectorRows.Next() {
		var selector workloadCredentialPageItem
		if err := selectorRows.Scan(&selector.Classification, &selector.ID); err != nil {
			selectorRows.Close()
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
			return
		}
		selectors = append(selectors, selector)
	}
	if err := selectorRows.Err(); err != nil {
		selectorRows.Close()
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
		return
	}
	selectorRows.Close()
	hasMore := len(selectors) > pageSize
	if hasMore {
		selectors = selectors[:pageSize]
	}
	familyIDs := make([]int64, 0, len(selectors))
	legacyIDs := make([]int64, 0, len(selectors))
	for _, selector := range selectors {
		if selector.Classification == "grant_bound" {
			familyIDs = append(familyIDs, selector.ID)
		} else {
			legacyIDs = append(legacyIDs, selector.ID)
		}
	}
	families := make(map[int64]workloadCredentialRecord, len(familyIDs))
	now := time.Now().UTC()
	if len(familyIDs) > 0 {
		rows, queryErr := h.db.Query(r.Context(), fmt.Sprintf(`
	SELECT ac.id,ac.credential_ref,ac.name,ac.status,ac.allowed_ips,ac.allowed_models,
	       ac.allowed_endpoints,ac.policy_labels,ac.rpm_limit,ac.rps_limit,
	       ac.concurrency_limit,ac.created_at,g.grant_ref,g.status,g.valid_from,g.valid_until,
	       EXISTS (
	         SELECT 1 FROM users live_sponsor
	         WHERE live_sponsor.id=g.sponsor_user_id AND live_sponsor.org_id=g.org_id
	           AND live_sponsor.status='active' AND (
	             live_sponsor.organization_role IN ('platform_admin','org_admin') OR EXISTS (
	               SELECT 1 FROM project_members live_sponsor_pm
	               WHERE live_sponsor_pm.project_id=g.project_id AND live_sponsor_pm.user_id=live_sponsor.id
	                 AND live_sponsor_pm.role IN ('project_admin','project_user','project_developer','developer')
	             )
	           )
	       ) AS sponsor_live,
	       %s AS runtime_effective,
	       EXISTS (
	         SELECT 1 FROM api_keys active_version
	         WHERE active_version.access_credential_id=ac.id AND active_version.credential_ref=ac.credential_ref
	           AND active_version.access_grant_id=ac.access_grant_id
	           AND active_version.org_id=ac.org_id AND active_version.project_id=ac.project_id
	           AND active_version.workload_application_id=a.id
	           AND COALESCE(active_version.purpose,'')='' AND active_version.status='active'
	       ) AS has_active_version,
	       k.id,k.key_prefix,k.status,k.created_at,k.rotated_from_key_id
	  FROM access_credentials ac
	  JOIN access_grants g
	    ON g.id=ac.access_grant_id AND g.org_id=ac.org_id AND g.project_id=ac.project_id
	   AND g.subject_type='workload_application' AND g.subject_workload_application_id=$2
	  JOIN workload_applications a
	    ON a.id=g.subject_workload_application_id AND a.org_id=g.org_id AND a.project_id=g.project_id
	  JOIN projects p ON p.id=a.project_id AND p.org_id=a.org_id
	  LEFT JOIN LATERAL (
	    SELECT version.id,version.key_prefix,version.status,version.created_at,version.rotated_from_key_id
	      FROM api_keys version
	     WHERE version.access_credential_id=ac.id AND version.credential_ref=ac.credential_ref
	       AND version.access_grant_id=ac.access_grant_id AND version.org_id=ac.org_id
	       AND version.project_id=ac.project_id AND version.workload_application_id=$2
	       AND COALESCE(version.purpose,'')=''
	     ORDER BY version.id DESC
	     LIMIT %d
	  ) k ON TRUE
	 WHERE ac.org_id=$1 AND ac.project_id=$3 AND ac.id=ANY($4::bigint[])
	 ORDER BY ac.id,k.id DESC`, workloadRuntimeEffectiveCredentialFamilySQL("a", "p", "ac", "g"),
			maxWorkloadCredentialVersionsPerFamily+1), orgID, application.ID, application.ProjectID, familyIDs)
		if queryErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
			return
		}
		for rows.Next() {
			var familyID int64
			var credentialRef, name, familyStatus, allowedIPs, allowedModels, allowedEndpoints, policyLabels string
			var rpmLimit, rpsLimit int
			var concurrencyLimit sql.NullInt64
			var familyCreatedAt, grantValidFrom time.Time
			var grantRef, grantStatus string
			var sponsorLive, runtimeEffective, hasActiveVersion bool
			var grantValidUntil sql.NullTime
			var keyID, rotatedFrom sql.NullInt64
			var keyPrefix, keyStatus sql.NullString
			var keyCreatedAt sql.NullTime
			if err := rows.Scan(&familyID, &credentialRef, &name, &familyStatus, &allowedIPs, &allowedModels,
				&allowedEndpoints, &policyLabels, &rpmLimit, &rpsLimit, &concurrencyLimit,
				&familyCreatedAt, &grantRef, &grantStatus, &grantValidFrom, &grantValidUntil, &sponsorLive,
				&runtimeEffective, &hasActiveVersion, &keyID, &keyPrefix, &keyStatus, &keyCreatedAt, &rotatedFrom); err != nil {
				rows.Close()
				writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
				return
			}
			item, exists := families[familyID]
			if !exists {
				item = workloadCredentialRecord{
					ID: familyID, CredentialRef: credentialRef, Name: name, Status: familyStatus,
					KeyType: "service", AllowedIPs: allowedIPs, AllowedModels: allowedModels,
					AllowedEndpoints: allowedEndpoints, PolicyLabels: policyLabels,
					RPMLimit: rpmLimit, RPSLimit: rpsLimit, GrantRef: grantRef,
					Classification: "grant_bound", Versions: make([]workloadCredentialVersionRecord, 0), CreatedAt: familyCreatedAt,
				}
				if concurrencyLimit.Valid {
					value := int(concurrencyLimit.Int64)
					item.ConcurrencyLimit = &value
				}
				item.EffectiveStatus = effectiveWorkloadFamilyStatus(application.Status, application.ProjectStatus,
					familyStatus, grantStatus, grantValidFrom, nullTimePointer(grantValidUntil), sponsorLive,
					runtimeEffective, hasActiveVersion, now)
			}
			if keyID.Valid {
				if len(item.Versions) >= maxWorkloadCredentialVersionsPerFamily {
					item.VersionsTruncated = true
				} else {
					version := workloadCredentialVersionRecord{ID: keyID.Int64, KeyPrefix: keyPrefix.String,
						Status: keyStatus.String, CreatedAt: keyCreatedAt.Time}
					if rotatedFrom.Valid {
						value := rotatedFrom.Int64
						version.RotatedFromKeyID = &value
					}
					version.EffectiveStatus = effectiveWorkloadCredentialVersionStatus(application.Status,
						application.ProjectStatus, familyStatus, grantStatus, grantValidFrom,
						nullTimePointer(grantValidUntil), sponsorLive, runtimeEffective, version.Status, now)
					item.Versions = append(item.Versions, version)
					item.VersionCount = len(item.Versions)
					if item.KeyPrefix == "" {
						item.KeyPrefix = version.KeyPrefix
					}
					if version.EffectiveStatus == "active" {
						item.EffectiveStatus = "active"
					}
				}
			}
			families[familyID] = item
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
			return
		}
		rows.Close()
	}
	legacyItems := make(map[int64]workloadCredentialRecord, len(legacyIDs))
	if len(legacyIDs) > 0 {
		legacyRows, queryErr := h.db.Query(r.Context(), `
	SELECT k.id,k.name,k.key_prefix,k.status,COALESCE(NULLIF(k.key_type,''),'shared'),
       COALESCE(k.allowed_ips,''),COALESCE(k.allowed_models,''),COALESCE(k.allowed_endpoints,''),
	       COALESCE(k.policy_labels,''),k.rpm_limit,k.qps_limit,k.concurrency_limit,k.budget_limit,
	       k.created_at,k.rotated_from_key_id
  FROM api_keys k
 WHERE k.org_id=$1 AND k.project_id=$2 AND k.workload_application_id=$3
	   AND k.access_credential_id IS NULL AND COALESCE(k.purpose,'')='' AND k.id=ANY($4::bigint[])
	 ORDER BY k.id DESC`, orgID, application.ProjectID, application.ID, legacyIDs)
		if queryErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
			return
		}
		for legacyRows.Next() {
			var item workloadCredentialRecord
			var concurrencyLimit, budgetLimit, rotatedFrom sql.NullInt64
			if err := legacyRows.Scan(&item.ID, &item.Name, &item.KeyPrefix, &item.Status, &item.KeyType,
				&item.AllowedIPs, &item.AllowedModels, &item.AllowedEndpoints, &item.PolicyLabels,
				&item.RPMLimit, &item.RPSLimit, &concurrencyLimit, &budgetLimit, &item.CreatedAt,
				&rotatedFrom); err != nil {
				legacyRows.Close()
				writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
				return
			}
			if concurrencyLimit.Valid {
				value := int(concurrencyLimit.Int64)
				item.ConcurrencyLimit = &value
			}
			if budgetLimit.Valid {
				value := budgetLimit.Int64
				item.BudgetLimitCents = &value
			}
			item.Classification = "legacy_unbound"
			item.EffectiveStatus = effectiveLegacyWorkloadCredentialStatus(application.Status, application.ProjectStatus, item.Status)
			version := workloadCredentialVersionRecord{ID: item.ID, KeyPrefix: item.KeyPrefix, Status: item.Status,
				EffectiveStatus: item.EffectiveStatus, CreatedAt: item.CreatedAt}
			if rotatedFrom.Valid {
				value := rotatedFrom.Int64
				version.RotatedFromKeyID = &value
			}
			item.VersionCount = 1
			item.Versions = []workloadCredentialVersionRecord{version}
			legacyItems[item.ID] = item
		}
		if err := legacyRows.Err(); err != nil {
			legacyRows.Close()
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application credential store unavailable"})
			return
		}
		legacyRows.Close()
	}
	items := make([]workloadCredentialRecord, 0, len(selectors))
	for _, selector := range selectors {
		var item workloadCredentialRecord
		var exists bool
		if selector.Classification == "grant_bound" {
			item, exists = families[selector.ID]
		} else {
			item, exists = legacyItems[selector.ID]
		}
		if exists {
			items = append(items, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": items, "total": len(items), "page": page, "page_size": pageSize, "has_more": hasMore,
	})
}

func parseBoundedWorkloadCredentialPagination(r *http.Request) (int, int, error) {
	page := 1
	pageSize := defaultPageSize
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maxWorkloadCredentialPage {
			return 0, 0, errors.New("invalid page")
		}
		page = value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maxPageSize {
			return 0, 0, errors.New("invalid page size")
		}
		pageSize = value
	}
	return page, pageSize, nil
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
	visible, err := readWorkloadApplication(r.Context(), h.db, orgID, r.PathValue("ref"), false)
	if err != nil || !h.canWriteProject(r.Context(), visible.ProjectID) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Workload application not found"})
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
	if err := h.reauthorizeWorkloadApplicationMutationTx(r.Context(), tx, orgID, visible.ProjectID, visible.ID); err != nil {
		writeWorkloadCommitAuthorizationError(w, err)
		return
	}
	application, err := readWorkloadApplicationForProject(r.Context(), tx, orgID, visible.ProjectID, r.PathValue("ref"), true)
	if err != nil {
		writeWorkloadApplicationError(w, err)
		return
	}
	if application.Status == workloadapp.StatusArchived || application.Revision != request.ExpectedRevision {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Application changed or is archived"})
		return
	}
	if application.IdentityContractVersion == workloadapp.IdentityContractVersion {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":       "V2 workload credentials must be issued from an active access grant",
			"reason_code": "workload_v2_grant_required",
		})
		return
	}
	var existingApplicationID int64
	var keyType, status string
	err = tx.QueryRow(r.Context(), `
	SELECT COALESCE(NULLIF(key_type,''),'shared'), status, COALESCE(workload_application_id,0)
	  FROM api_keys
	 WHERE id=$1 AND org_id=$2 AND project_id=$3 AND COALESCE(purpose,'')=''
	   AND access_grant_id IS NULL
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
	   AND COALESCE(purpose,'')='' AND access_grant_id IS NULL
	   AND workload_application_id IS NULL`,
		application.ID, keyID, orgID, application.ProjectID)
	if err != nil || tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Credential binding changed since it was loaded"})
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	tag, err = tx.Exec(r.Context(), `
UPDATE workload_applications
   SET revision=revision+1, updated_by=$1, updated_at=NOW()
 WHERE id=$2 AND org_id=$3 AND project_id=$4 AND revision=$5`, actor, application.ID, orgID, visible.ProjectID, application.Revision)
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
	item, err = h.decorateWorkloadApplicationCapabilities(r.Context(), item)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Application store unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"result": "bound", "application": item})
}

func (h *Handler) decorateWorkloadApplicationCapabilities(ctx context.Context, item workloadApplicationRecord) (workloadApplicationRecord, error) {
	item.CanManage = h.canWriteProject(ctx, item.ProjectID)
	item.CanRequestAccess = item.CanManage && item.Status == workloadapp.StatusActive && item.ProjectStatus == "active"
	if item.CanManage {
		item.CanReadCredentials = true
		return item, nil
	}
	actorUserID := middleware.GetUserIDFromContext(ctx)
	if actorUserID <= 0 {
		return item, nil
	}
	if err := h.db.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1
		FROM access_grants g
		JOIN users sponsor ON sponsor.id=g.sponsor_user_id AND sponsor.org_id=g.org_id
		WHERE g.org_id=$1 AND g.project_id=$2
		  AND g.subject_type='workload_application' AND g.subject_workload_application_id=$3
		  AND g.sponsor_user_id=$4 AND sponsor.status='active'
		  AND (
		    sponsor.organization_role IN ('platform_admin','org_admin')
		    OR EXISTS (
		      SELECT 1 FROM project_members sponsor_pm
		      WHERE sponsor_pm.project_id=g.project_id AND sponsor_pm.user_id=sponsor.id
		        AND sponsor_pm.role IN ('project_admin','project_user','project_developer','developer')
		    )
		  )
	)`, item.OrgID, item.ProjectID, item.ID, actorUserID).Scan(&item.CanReadCredentials); err != nil {
		return workloadApplicationRecord{}, err
	}
	return item, nil
}

func (h *Handler) reauthorizeWorkloadApplicationMutationTx(ctx context.Context, tx pgx.Tx, orgID, projectID int64, workloadIDs ...int64) error {
	_, err := h.reauthorizeCommittingActorTx(ctx, tx, committingActorRequirement{
		TargetOrgID:              orgID,
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
		AdditionalWorkloadIDs:    workloadIDs,
		Project: &committingActorProjectRequirement{
			ProjectID: projectID, AllowedRoles: []string{auth.RoleProjectAdmin}, MustBeActive: true,
		},
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
	} else if model != "" && len(credentials) > maxWorkloadReadinessCredentials {
		authorizationState = "model_authorization_unavailable"
	} else if model != "" && h.modelAuthorizationEvaluator != nil {
		inputs := make([]modelauthz.Input, 0, len(eligibleCredentials))
		for _, credential := range eligibleCredentials {
			inputs = append(inputs, modelauthz.Input{
				OrgID: orgID, ProjectID: application.ProjectID, APIKeyID: credential.ID,
				OwnerUserID: credential.OwnerUserID, Endpoint: endpoint,
				RequestedModel: model, ResolvedModel: resolvedModel,
				AccessCredentialID: credential.AccessCredentialID,
			})
		}
		decision, evaluateErr := h.modelAuthorizationEvaluator.EvaluateAny(r.Context(), inputs)
		if evaluateErr != nil {
			authorizationState = "model_authorization_unavailable"
		} else {
			authorizationState = decision.Reason
			if authorizationState == "legacy_authority_unchanged" {
				authorizationState = "legacy_authority"
			}
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
		ClientProfileOptional: application.IdentityContractVersion == workloadapp.IdentityContractVersion,
		ClientProfileSelectionRequired: application.IdentityContractVersion == workloadapp.IdentityContractVersion &&
			application.ClientProfile == "" && len(application.ClientProfiles) > 0,
		Endpoint: endpoint, Model: model, ProjectActive: application.ProjectStatus == "active",
		ActiveCredentialCount: len(credentials), CredentialAllowsModel: credentialAllows,
		AuthorizationState: authorizationState, LimitState: limitState,
		ModelGovernanceState: modelState, RoutingCandidateCount: routingCandidates,
		RoutingPreviewAvailable: routingAvailable,
	})
	writeJSON(w, http.StatusOK, result)
}

type workloadReadinessCredential struct {
	ID                 int64
	AccessCredentialID int64
	OwnerUserID        int64
	AllowedModels      string
}

func readActiveWorkloadCredentials(ctx context.Context, rower interface {
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
}, orgID, applicationID int64) ([]workloadReadinessCredential, error) {
	rows, err := rower.Query(ctx, fmt.Sprintf(`
(SELECT k.id,0,COALESCE(k.owner_user_id,0),COALESCE(k.allowed_models,'')
  FROM api_keys k
  JOIN workload_applications a
    ON a.id=k.workload_application_id AND a.org_id=k.org_id AND a.project_id=k.project_id
  JOIN projects p ON p.id=a.project_id AND p.org_id=a.org_id
	 WHERE k.org_id=$1 AND k.workload_application_id=$2
	   AND k.access_credential_id IS NULL AND COALESCE(k.purpose,'')=''
	   AND a.status='active' AND p.status='active'
	   AND k.status='active'
 ORDER BY k.id DESC
 LIMIT %d)
UNION ALL
(SELECT 0,ac.id,0,ac.allowed_models
  FROM access_credentials ac
  JOIN access_grants g
    ON g.id=ac.access_grant_id AND g.org_id=ac.org_id AND g.project_id=ac.project_id
   AND g.subject_type='workload_application' AND g.subject_workload_application_id=$2
  JOIN workload_applications a
    ON a.id=g.subject_workload_application_id AND a.org_id=g.org_id AND a.project_id=g.project_id
  JOIN projects p ON p.id=a.project_id AND p.org_id=a.org_id
 WHERE ac.org_id=$1
	AND %s
 ORDER BY ac.id DESC
 LIMIT %d)
LIMIT %d`, maxWorkloadReadinessCredentials+1,
		workloadRuntimeEffectiveCredentialFamilySQL("a", "p", "ac", "g"),
		maxWorkloadReadinessCredentials+1, maxWorkloadReadinessCredentials+1), orgID, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]workloadReadinessCredential, 0)
	for rows.Next() {
		var item workloadReadinessCredential
		if err := rows.Scan(&item.ID, &item.AccessCredentialID, &item.OwnerUserID, &item.AllowedModels); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func readWorkloadApplication(ctx context.Context, rower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, orgID int64, ref string, lock bool) (workloadApplicationRecord, error) {
	return readWorkloadApplicationForProject(ctx, rower, orgID, 0, ref, lock)
}

func readWorkloadApplicationForProject(ctx context.Context, rower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, orgID, expectedProjectID int64, ref string, lock bool) (workloadApplicationRecord, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	query := fmt.Sprintf(`
SELECT a.id, a.org_id, a.project_id, a.application_ref,a.contract_version,
	   a.identity_contract_version,COALESCE(a.logical_ref,''),a.display_name, a.kind,
	   a.environment, a.production_use, a.identity_mode, COALESCE(a.client_profile,''),
	   ARRAY(SELECT cp.client_profile FROM workload_application_client_profiles cp
	         WHERE cp.workload_application_id=a.id ORDER BY cp.client_profile),a.owner_team,
       a.description, a.status, a.revision, a.created_by, a.updated_by,
       a.created_at, a.updated_at, a.archived_at, p.name, p.status,
       %s, %s
  FROM workload_applications a
  JOIN projects p ON p.id=a.project_id AND p.org_id=a.org_id
 WHERE a.org_id=$1 AND a.application_ref=$2
   AND ($5::bigint=0 OR a.project_id=$5)`, workloadCredentialSeatCountSQL("a"), workloadActiveCredentialSeatCountSQL("a", "p"))
	query += ` AND (
		$3='platform_admin' OR $3 IN ('org_admin','org_viewer')
		OR EXISTS (
			SELECT 1 FROM project_members actor_pm
			WHERE actor_pm.project_id=a.project_id AND actor_pm.user_id=$4
			  AND actor_pm.role IN ('project_admin','project_user','project_viewer','project_developer','developer','viewer')
		)
	)`
	if lock {
		query += " FOR UPDATE OF a"
	}
	item, err := scanWorkloadApplication(rower.QueryRow(ctx, query, orgID, ref,
		actorOrganizationRole(ctx), middleware.GetUserIDFromContext(ctx), expectedProjectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return workloadApplicationRecord{}, errors.New("workload application not found")
	}
	return item, err
}

// workloadCredentialSeatCountSQL counts logical credentials rather than their
// physical secret versions. Existing legacy-unbound keys each remain one seat.
func workloadCredentialSeatCountSQL(applicationAlias string) string {
	return fmt.Sprintf(`
		(SELECT COUNT(*) FROM access_credentials ac
		 JOIN access_grants g
		   ON g.id=ac.access_grant_id AND g.org_id=ac.org_id AND g.project_id=ac.project_id
		  AND g.subject_type='workload_application'
		  AND g.subject_workload_application_id=%[1]s.id
		 WHERE ac.org_id=%[1]s.org_id AND ac.project_id=%[1]s.project_id)
		+
		(SELECT COUNT(*) FROM api_keys legacy_key
		 WHERE legacy_key.org_id=%[1]s.org_id AND legacy_key.project_id=%[1]s.project_id
		   AND legacy_key.workload_application_id=%[1]s.id
		   AND legacy_key.access_credential_id IS NULL
		   AND COALESCE(legacy_key.purpose,'')='')`, applicationAlias)
}

// workloadRuntimeEffectiveCredentialFamilySQL is the single SQL authority used
// by Workload list, detail, credential metadata and readiness reads. It mirrors
// the live access-grant boundary for a Workload family: exact v2 subject and
// environment/risk identity, a live sponsor and one active bound secret version.
func workloadRuntimeEffectiveCredentialFamilySQL(applicationAlias, projectAlias, credentialAlias, grantAlias string) string {
	return fmt.Sprintf(`(
		%[1]s.status='active'
		AND %[2]s.status='active'
		AND EXISTS (
		    SELECT 1 FROM orgs runtime_org
		    WHERE runtime_org.id=%[1]s.org_id AND runtime_org.status='active'
		)
		AND %[1]s.identity_contract_version='unirouter.workload-identity.v2'
		AND %[4]s.subject_type='workload_application'
		AND %[4]s.subject_workload_application_id=%[1]s.id
		AND %[4]s.org_id=%[1]s.org_id AND %[4]s.project_id=%[1]s.project_id
		AND %[4]s.environment=%[1]s.environment
		AND %[4]s.production_use=%[1]s.production_use
		AND %[4]s.project_role_snapshot IN ('project_admin','project_user','project_developer','developer')
		AND %[4]s.status='active'
		AND %[4]s.valid_from<=NOW() AND (%[4]s.valid_until IS NULL OR %[4]s.valid_until>NOW())
		AND %[3]s.access_grant_id=%[4]s.id
		AND %[3]s.org_id=%[4]s.org_id AND %[3]s.project_id=%[4]s.project_id
		AND %[3]s.status='active'
		AND EXISTS (
		    SELECT 1 FROM users runtime_sponsor
		    WHERE runtime_sponsor.id=%[4]s.sponsor_user_id
		      AND runtime_sponsor.org_id=%[4]s.org_id
		      AND runtime_sponsor.status='active'
		      AND (
		        runtime_sponsor.organization_role IN ('platform_admin','org_admin')
		        OR EXISTS (
		          SELECT 1 FROM project_members runtime_sponsor_pm
		          WHERE runtime_sponsor_pm.project_id=%[4]s.project_id
		            AND runtime_sponsor_pm.user_id=runtime_sponsor.id
		            AND runtime_sponsor_pm.role IN ('project_admin','project_user','project_developer','developer')
		        )
		      )
		)
		AND EXISTS (
		    SELECT 1 FROM api_keys runtime_version
		    WHERE runtime_version.access_credential_id=%[3]s.id
		      AND runtime_version.credential_ref=%[3]s.credential_ref
		      AND runtime_version.access_grant_id=%[3]s.access_grant_id
		      AND runtime_version.org_id=%[3]s.org_id AND runtime_version.project_id=%[3]s.project_id
		      AND runtime_version.workload_application_id=%[1]s.id
		      AND COALESCE(runtime_version.purpose,'')='' AND runtime_version.status='active'
		)
	)`, applicationAlias, projectAlias, credentialAlias, grantAlias)
}

// workloadActiveCredentialSeatCountSQL is shared by the Workload list and
// detail read models. Atomic replacement keeps exactly one active version per
// logical family.
func workloadActiveCredentialSeatCountSQL(applicationAlias, projectAlias string) string {
	return fmt.Sprintf(`
		CASE WHEN %[1]s.status<>'active' OR %[2]s.status<>'active' THEN 0 ELSE
		(SELECT COUNT(*) FROM access_credentials ac
		 JOIN access_grants g
		   ON g.id=ac.access_grant_id AND g.org_id=ac.org_id AND g.project_id=ac.project_id
		  AND g.subject_type='workload_application'
		  AND g.subject_workload_application_id=%[1]s.id
		 WHERE ac.org_id=%[1]s.org_id AND ac.project_id=%[1]s.project_id
		   AND %[3]s)
		+
		(SELECT COUNT(*) FROM api_keys legacy_key
		 WHERE legacy_key.org_id=%[1]s.org_id AND legacy_key.project_id=%[1]s.project_id
		   AND legacy_key.workload_application_id=%[1]s.id
		   AND legacy_key.access_credential_id IS NULL
		   AND COALESCE(legacy_key.purpose,'')='' AND legacy_key.status='active') END`,
		applicationAlias, projectAlias,
		workloadRuntimeEffectiveCredentialFamilySQL(applicationAlias, projectAlias, "ac", "g"))
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func effectiveWorkloadFamilyStatus(applicationStatus, projectStatus, familyStatus, grantStatus string, grantValidFrom time.Time, grantValidUntil *time.Time, sponsorLive, runtimeEffective, hasActiveVersion bool, now time.Time) string {
	if familyStatus != "active" {
		return "revoked"
	}
	if applicationStatus != workloadapp.StatusActive || !sponsorLive {
		return "subject_blocked"
	}
	if projectStatus != "active" || grantStatus != "active" || now.Before(grantValidFrom) || (grantValidUntil != nil && !grantValidUntil.After(now)) {
		return "disabled_by_grant"
	}
	if !runtimeEffective && hasActiveVersion {
		return "subject_blocked"
	}
	if !hasActiveVersion {
		return "no_active_version"
	}
	return "active"
}

func effectiveWorkloadCredentialVersionStatus(applicationStatus, projectStatus, familyStatus, grantStatus string, grantValidFrom time.Time, grantValidUntil *time.Time, sponsorLive, runtimeEffective bool, versionStatus string, now time.Time) string {
	if versionStatus != "active" {
		return versionStatus
	}
	familyEffective := effectiveWorkloadFamilyStatus(applicationStatus, projectStatus, familyStatus, grantStatus, grantValidFrom, grantValidUntil, sponsorLive, runtimeEffective, true, now)
	if familyEffective != "active" {
		return familyEffective
	}
	return "active"
}

func effectiveLegacyWorkloadCredentialStatus(applicationStatus, projectStatus, keyStatus string) string {
	if keyStatus != "active" {
		return keyStatus
	}
	if applicationStatus != workloadapp.StatusActive {
		return "subject_blocked"
	}
	if projectStatus != "active" {
		return "project_blocked"
	}
	return "active"
}

func scanWorkloadApplication(row interface{ Scan(...interface{}) error }) (workloadApplicationRecord, error) {
	var item workloadApplicationRecord
	err := row.Scan(
		&item.ID, &item.OrgID, &item.ProjectID, &item.ApplicationRef,
		&item.ContractVersion, &item.IdentityContractVersion, &item.LogicalRef,
		&item.DisplayName, &item.Kind, &item.Environment, &item.ProductionUse, &item.IdentityMode,
		&item.ClientProfile, &item.ClientProfiles, &item.OwnerTeam, &item.Description, &item.Status,
		&item.Revision, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt,
		&item.UpdatedAt, &item.ArchivedAt, &item.ProjectName, &item.ProjectStatus,
		&item.CredentialCount, &item.ActiveCredentialCount,
	)
	if err == nil {
		item.IdentityClassification = "legacy_v1"
		if item.IdentityContractVersion == workloadapp.IdentityContractVersion {
			item.IdentityClassification = "v2"
		}
	}
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
	if _, present := middleware.GetCustomerContextFromContext(r.Context()); present {
		orgID, err := resolveActorOrganization(r.Context(), requested)
		if err != nil {
			return 0, errors.New("workload application organization forbidden")
		}
		return orgID, nil
	}
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
		"environment": item.Environment, "production_use": item.ProductionUse, "client_profile": item.ClientProfile,
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
