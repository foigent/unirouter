package admin

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/deploymentprofile"
	"github.com/uniroute/unirouter/internal/middleware"
)

type deploymentProfileRecord struct {
	ID              int64     `json:"id"`
	ProfileRef      string    `json:"profile_ref"`
	DisplayName     string    `json:"display_name"`
	Kind            string    `json:"kind"`
	Environment     string    `json:"environment"`
	IngressMode     string    `json:"ingress_mode"`
	DNSMode         string    `json:"dns_mode"`
	OutboundMode    string    `json:"outbound_mode"`
	Notes           string    `json:"notes"`
	Status          string    `json:"status"`
	DesiredRevision int64     `json:"desired_revision"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type gatewayEndpointRecord struct {
	ID                   int64                  `json:"id"`
	DeploymentProfileID  int64                  `json:"deployment_profile_id"`
	DeploymentProfileRef string                 `json:"deployment_profile_ref"`
	DeploymentName       string                 `json:"deployment_name"`
	EndpointRef          string                 `json:"endpoint_ref"`
	DisplayName          string                 `json:"display_name"`
	BaseURL              string                 `json:"base_url"`
	Visibility           string                 `json:"visibility"`
	TLSMode              string                 `json:"tls_mode"`
	CertificateRef       string                 `json:"certificate_ref"`
	EndpointProfile      string                 `json:"endpoint_profile"`
	Status               string                 `json:"status"`
	DesiredRevision      int64                  `json:"desired_revision"`
	ObservedStatus       string                 `json:"observed_status"`
	ObservedBaseURL      string                 `json:"observed_base_url"`
	ObservedEvidence     map[string]interface{} `json:"observed_evidence"`
	ObservedAt           *time.Time             `json:"observed_at,omitempty"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
}

func (h *Handler) GetDeploymentNetworkContract(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"contract_version":         deploymentprofile.ContractVersion,
		"endpoint_profiles":        deploymentprofile.EndpointProfiles(),
		"tls_private_key_storage":  "forbidden",
		"apply_authority":          "operator_cli_compose_or_helm",
		"runtime_mutation":         "none",
		"observed_state_authority": "operator_or_independent_probe_evidence",
	})
}

func (h *Handler) ListDeploymentProfiles(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), `
SELECT id, profile_ref, display_name, kind, environment, ingress_mode, dns_mode,
       outbound_mode, notes, status, desired_revision, created_at, updated_at
  FROM deployment_profiles ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Deployment profile store unavailable"})
		return
	}
	defer rows.Close()
	items := make([]deploymentProfileRecord, 0)
	for rows.Next() {
		var item deploymentProfileRecord
		if err := rows.Scan(&item.ID, &item.ProfileRef, &item.DisplayName, &item.Kind, &item.Environment,
			&item.IngressMode, &item.DNSMode, &item.OutboundMode, &item.Notes, &item.Status,
			&item.DesiredRevision, &item.CreatedAt, &item.UpdatedAt); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Deployment profile store unavailable"})
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Deployment profile store unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": len(items)})
}

func (h *Handler) CreateDeploymentProfile(w http.ResponseWriter, r *http.Request) {
	var request deploymentprofile.ProfileDraft
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid deployment profile request"})
		return
	}
	draft, err := deploymentprofile.NormalizeProfile(request)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Deployment profile store unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	actor := middleware.GetUserIDFromContext(r.Context())
	var id int64
	err = tx.QueryRow(r.Context(), `
INSERT INTO deployment_profiles (
    contract_version, profile_ref, display_name, kind, environment, ingress_mode,
    dns_mode, outbound_mode, notes, status, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11) RETURNING id`,
		deploymentprofile.ContractVersion, draft.ProfileRef, draft.DisplayName, draft.Kind,
		draft.Environment, draft.IngressMode, draft.DNSMode, draft.OutboundMode,
		draft.Notes, draft.Status, actor).Scan(&id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Deployment profile reference already exists"})
		} else {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Deployment profile store unavailable"})
		}
		return
	}
	after := map[string]interface{}{"id": id, "profile_ref": draft.ProfileRef, "kind": draft.Kind, "environment": draft.Environment, "status": draft.Status, "desired_revision": 1, "runtime_mutation": "none"}
	if err := insertAuditEntry(r.Context(), tx, "deployment_profile.create", "deployment_profile", draft.ProfileRef,
		map[string]interface{}{"after": after}, requestAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()), 0)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Deployment profile audit unavailable"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Deployment profile store unavailable"})
		return
	}
	writeJSON(w, http.StatusCreated, after)
}

func (h *Handler) UpdateDeploymentProfile(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ExpectedRevision int64 `json:"expected_revision"`
		deploymentprofile.ProfileDraft
	}
	if err := decodeStrictJSONBody(r, &request); err != nil || request.ExpectedRevision <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid deployment profile update"})
		return
	}
	request.ProfileRef = r.PathValue("ref")
	draft, err := deploymentprofile.NormalizeProfile(request.ProfileDraft)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Deployment profile store unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	var before deploymentProfileRecord
	err = tx.QueryRow(r.Context(), `
SELECT id, profile_ref, display_name, kind, environment, ingress_mode, dns_mode,
       outbound_mode, notes, status, desired_revision, created_at, updated_at
  FROM deployment_profiles WHERE profile_ref=$1 FOR UPDATE`, draft.ProfileRef).Scan(
		&before.ID, &before.ProfileRef, &before.DisplayName, &before.Kind, &before.Environment,
		&before.IngressMode, &before.DNSMode, &before.OutboundMode, &before.Notes,
		&before.Status, &before.DesiredRevision, &before.CreatedAt, &before.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Deployment profile not found"})
		return
	}
	if err != nil || before.DesiredRevision != request.ExpectedRevision {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Deployment profile changed since it was loaded"})
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	tag, err := tx.Exec(r.Context(), `
UPDATE deployment_profiles SET display_name=$1, kind=$2, environment=$3,
       ingress_mode=$4, dns_mode=$5, outbound_mode=$6, notes=$7, status=$8,
       desired_revision=desired_revision+1, updated_by=$9, updated_at=NOW()
 WHERE id=$10 AND desired_revision=$11`, draft.DisplayName, draft.Kind, draft.Environment,
		draft.IngressMode, draft.DNSMode, draft.OutboundMode, draft.Notes, draft.Status,
		actor, before.ID, request.ExpectedRevision)
	if err != nil || tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Deployment profile changed since it was loaded"})
		return
	}
	after := map[string]interface{}{"profile_ref": draft.ProfileRef, "display_name": draft.DisplayName, "kind": draft.Kind, "environment": draft.Environment, "ingress_mode": draft.IngressMode, "dns_mode": draft.DNSMode, "outbound_mode": draft.OutboundMode, "status": draft.Status, "desired_revision": before.DesiredRevision + 1, "runtime_mutation": "none"}
	if err := insertAuditEntry(r.Context(), tx, "deployment_profile.update", "deployment_profile", draft.ProfileRef,
		map[string]interface{}{"before": before, "after": after}, requestAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()), 0)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Deployment profile audit unavailable"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Deployment profile store unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, after)
}

func (h *Handler) ListGatewayEndpoints(w http.ResponseWriter, r *http.Request) {
	profileRef := strings.TrimSpace(r.URL.Query().Get("deployment_profile_ref"))
	includeOperatorEvidence := auth.IsPlatformAdmin(actorOrganizationRole(r.Context()))
	rows, err := h.db.Query(r.Context(), `
SELECT e.id, e.deployment_profile_id, p.profile_ref, p.display_name, e.endpoint_ref,
       e.display_name, e.base_url, e.visibility, e.tls_mode, e.certificate_ref,
       e.endpoint_profile, e.status, e.desired_revision, e.observed_status,
       e.observed_base_url, e.observed_evidence, e.observed_at, e.created_at, e.updated_at
  FROM gateway_endpoints e JOIN deployment_profiles p ON p.id=e.deployment_profile_id
 WHERE ($1='' OR p.profile_ref=$1) ORDER BY e.updated_at DESC, e.id DESC`, profileRef)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Gateway endpoint store unavailable"})
		return
	}
	defer rows.Close()
	items := make([]gatewayEndpointRecord, 0)
	for rows.Next() {
		var item gatewayEndpointRecord
		if err := rows.Scan(&item.ID, &item.DeploymentProfileID, &item.DeploymentProfileRef,
			&item.DeploymentName, &item.EndpointRef, &item.DisplayName, &item.BaseURL,
			&item.Visibility, &item.TLSMode, &item.CertificateRef, &item.EndpointProfile,
			&item.Status, &item.DesiredRevision, &item.ObservedStatus, &item.ObservedBaseURL,
			&item.ObservedEvidence, &item.ObservedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Gateway endpoint store unavailable"})
			return
		}
		if !includeOperatorEvidence {
			item.CertificateRef = ""
			item.ObservedEvidence = map[string]interface{}{}
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Gateway endpoint store unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": len(items)})
}

func (h *Handler) CreateGatewayEndpoint(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DeploymentProfileRef string `json:"deployment_profile_ref"`
		deploymentprofile.EndpointDraft
	}
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid Gateway endpoint request"})
		return
	}
	draft, err := deploymentprofile.NormalizeEndpoint(request.EndpointDraft)
	if err != nil || strings.TrimSpace(request.DeploymentProfileRef) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid Gateway endpoint request"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Gateway endpoint store unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	var deploymentID int64
	var deploymentStatus string
	err = tx.QueryRow(r.Context(), `SELECT id, status FROM deployment_profiles WHERE profile_ref=$1 FOR SHARE`, strings.TrimSpace(request.DeploymentProfileRef)).Scan(&deploymentID, &deploymentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Deployment profile not found"})
		return
	}
	if err != nil || deploymentStatus != "active" {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Deployment profile is not active"})
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	var id int64
	err = tx.QueryRow(r.Context(), `
INSERT INTO gateway_endpoints (
    deployment_profile_id, endpoint_ref, display_name, base_url, visibility,
    tls_mode, certificate_ref, endpoint_profile, status, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10) RETURNING id`, deploymentID,
		draft.EndpointRef, draft.DisplayName, draft.BaseURL, draft.Visibility, draft.TLSMode,
		draft.CertificateRef, draft.EndpointProfile, draft.Status, actor).Scan(&id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Gateway endpoint already exists"})
		} else {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Gateway endpoint store unavailable"})
		}
		return
	}
	after := map[string]interface{}{"id": id, "endpoint_ref": draft.EndpointRef, "deployment_profile_ref": request.DeploymentProfileRef, "base_url": draft.BaseURL, "endpoint_profile": draft.EndpointProfile, "status": draft.Status, "desired_revision": 1, "observed_status": "unverified", "runtime_mutation": "none"}
	if err := insertAuditEntry(r.Context(), tx, "gateway_endpoint.create", "gateway_endpoint", draft.EndpointRef,
		map[string]interface{}{"after": after}, requestAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()), 0)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Gateway endpoint audit unavailable"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Gateway endpoint store unavailable"})
		return
	}
	writeJSON(w, http.StatusCreated, after)
}

func (h *Handler) UpdateGatewayEndpoint(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ExpectedRevision int64 `json:"expected_revision"`
		deploymentprofile.EndpointDraft
	}
	if err := decodeStrictJSONBody(r, &request); err != nil || request.ExpectedRevision <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid Gateway endpoint update"})
		return
	}
	request.EndpointRef = r.PathValue("ref")
	draft, err := deploymentprofile.NormalizeEndpoint(request.EndpointDraft)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Gateway endpoint store unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	var id, revision int64
	var beforeBaseURL, beforeStatus, beforeProfile string
	err = tx.QueryRow(r.Context(), `SELECT id, desired_revision, base_url, status, endpoint_profile FROM gateway_endpoints WHERE endpoint_ref=$1 FOR UPDATE`, draft.EndpointRef).Scan(&id, &revision, &beforeBaseURL, &beforeStatus, &beforeProfile)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Gateway endpoint not found"})
		return
	}
	if err != nil || revision != request.ExpectedRevision {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Gateway endpoint changed since it was loaded"})
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	tag, err := tx.Exec(r.Context(), `
UPDATE gateway_endpoints SET display_name=$1, base_url=$2, visibility=$3, tls_mode=$4,
       certificate_ref=$5, endpoint_profile=$6, status=$7,
       desired_revision=desired_revision+1, updated_by=$8, updated_at=NOW()
 WHERE id=$9 AND desired_revision=$10`, draft.DisplayName, draft.BaseURL, draft.Visibility,
		draft.TLSMode, draft.CertificateRef, draft.EndpointProfile, draft.Status, actor, id, revision)
	if err != nil || tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Gateway endpoint changed since it was loaded"})
		return
	}
	after := map[string]interface{}{"endpoint_ref": draft.EndpointRef, "base_url": draft.BaseURL, "endpoint_profile": draft.EndpointProfile, "status": draft.Status, "desired_revision": revision + 1, "observed_state_changed": false, "runtime_mutation": "none"}
	if err := insertAuditEntry(r.Context(), tx, "gateway_endpoint.update", "gateway_endpoint", draft.EndpointRef,
		map[string]interface{}{"before": map[string]interface{}{"base_url": beforeBaseURL, "status": beforeStatus, "endpoint_profile": beforeProfile, "desired_revision": revision}, "after": after}, requestAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()), 0)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Gateway endpoint audit unavailable"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Gateway endpoint store unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, after)
}
