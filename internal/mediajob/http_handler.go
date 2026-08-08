package mediajob

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/middleware"
)

type HTTPHandler struct {
	service *Service
	db      db.DBQuerier
}

func NewHTTPHandler(service *Service, database db.DBQuerier) (*HTTPHandler, error) {
	if service == nil || database == nil {
		return nil, domainError(ErrUnavailable, "http_dependency_unavailable")
	}
	return &HTTPHandler{service: service, db: database}, nil
}

func (h *HTTPHandler) Submit(w http.ResponseWriter, r *http.Request) {
	apiKey := middleware.GetAPIKeyFromContext(r.Context())
	scope := Scope{
		OrgID:     middleware.GetOrgIDFromContext(r.Context()),
		ProjectID: middleware.GetProjectIDFromContext(r.Context()),
	}
	if apiKey == nil || !scope.Valid() {
		writeMediaError(w, domainError(ErrForbidden, "project_scoped_api_key_required"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxRequestBytes+1))
	if err != nil || len(body) > MaxRequestBytes {
		writeMediaError(w, domainError(ErrInvalid, "request_size_invalid"))
		return
	}
	result, err := h.service.Submit(r.Context(), SubmitInput{
		Scope: scope, APIKeyID: apiKey.ID,
		RequestID:       middleware.GetRequestIDFromContext(r.Context()),
		ClientRequestID: middleware.GetClientRequestIDFromContext(r.Context()),
		IdempotencyKey:  r.Header.Get("Idempotency-Key"),
		AllowedModels:   apiKey.AllowedModels,
		Body:            body,
	})
	if err != nil {
		writeMediaError(w, err)
		return
	}
	status := http.StatusAccepted
	if result.Reused {
		status = http.StatusOK
	}
	w.Header().Set("Location", "/v1/media/jobs/"+result.Job.JobID)
	writeMediaJSON(w, status, result)
}

func (h *HTTPHandler) Get(w http.ResponseWriter, r *http.Request) {
	scope, err := publicScope(r)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	job, err := h.service.Get(r.Context(), scope, r.PathValue("id"))
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeMediaJSON(w, http.StatusOK, job)
}

func (h *HTTPHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	scope, err := publicScope(r)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	result, err := h.service.Cancel(r.Context(), scope, r.PathValue("id"))
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeMediaJSON(w, http.StatusOK, result)
}

func (h *HTTPHandler) Content(w http.ResponseWriter, r *http.Request) {
	scope, err := publicScope(r)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	h.writeContent(w, r, scope, r.PathValue("id"))
}

func (h *HTTPHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	scope, err := h.resolveAdminScope(r, adminScopeRead)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			writeMediaError(w, domainError(ErrInvalid, "list_limit_invalid"))
			return
		}
	}
	before, beforeID, err := DecodeListCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeMediaError(w, err)
		return
	}
	result, err := h.service.List(r.Context(), ListFilter{
		Scope: scope, Limit: limit, Before: before, BeforeJobID: beforeID,
	})
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeMediaJSON(w, http.StatusOK, result)
}

func (h *HTTPHandler) AdminGet(w http.ResponseWriter, r *http.Request) {
	scope, err := h.resolveAdminScope(r, adminScopeRead)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	job, err := h.service.Get(r.Context(), scope, r.PathValue("id"))
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeMediaJSON(w, http.StatusOK, job)
}

func (h *HTTPHandler) AdminCancel(w http.ResponseWriter, r *http.Request) {
	scope, err := h.resolveAdminScope(r, adminScopeWrite)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	result, err := h.service.Cancel(r.Context(), scope, r.PathValue("id"))
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeMediaJSON(w, http.StatusOK, result)
}

func (h *HTTPHandler) AdminContent(w http.ResponseWriter, r *http.Request) {
	scope, err := h.resolveAdminScope(r, adminScopeRead)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	h.writeContent(w, r, scope, r.PathValue("id"))
}

func (h *HTTPHandler) Contract(w http.ResponseWriter, r *http.Request) {
	if _, err := h.resolveAdminScope(r, adminScopeRead); err != nil {
		writeMediaError(w, err)
		return
	}
	writeMediaJSON(
		w,
		http.StatusOK,
		Evidence(h.service.RuntimeEnabled(), h.service.AdmissionEnabled()),
	)
}

func (h *HTTPHandler) writeContent(
	w http.ResponseWriter,
	r *http.Request,
	scope Scope,
	jobID string,
) {
	output, err := h.service.ReadOutput(r.Context(), scope, jobID)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	extension := "bin"
	switch output.MediaType {
	case "image/png":
		extension = "png"
	case "image/jpeg":
		extension = "jpg"
	case "image/webp":
		extension = "webp"
	}
	w.Header().Set("Content-Type", output.MediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(output.Bytes)))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", `inline; filename="`+jobID+`.`+extension+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output.Bytes)
}

func publicScope(r *http.Request) (Scope, error) {
	scope := Scope{
		OrgID:     middleware.GetOrgIDFromContext(r.Context()),
		ProjectID: middleware.GetProjectIDFromContext(r.Context()),
	}
	if !scope.Valid() || middleware.GetAPIKeyFromContext(r.Context()) == nil {
		return Scope{}, domainError(ErrForbidden, "project_scoped_api_key_required")
	}
	return scope, nil
}

type adminScopeMode uint8

const (
	adminScopeRead adminScopeMode = iota
	adminScopeWrite
)

func (h *HTTPHandler) resolveAdminScope(r *http.Request, mode adminScopeMode) (Scope, error) {
	organizationRole := middleware.GetOrganizationRoleFromContext(r.Context())
	if organizationRole == "" {
		organizationRole, _ = auth.NormalizeOrganizationRole(middleware.GetUserRoleFromContext(r.Context()))
	}
	orgID := middleware.GetOrgIDFromContext(r.Context())
	if organizationRole == auth.RolePlatformAdmin {
		if raw := r.URL.Query().Get("org_id"); raw != "" {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || value <= 0 {
				return Scope{}, domainError(ErrInvalid, "organization_scope_invalid")
			}
			orgID = value
		}
	}
	projectID, err := strconv.ParseInt(r.URL.Query().Get("project_id"), 10, 64)
	if err != nil || orgID <= 0 || projectID <= 0 {
		return Scope{}, domainError(ErrInvalid, "project_scope_required")
	}
	userID := middleware.GetUserIDFromContext(r.Context())
	var allowed bool
	if organizationRole == auth.RolePlatformAdmin || organizationRole == auth.RoleOrgAdmin {
		err = h.db.QueryRow(r.Context(), `
			SELECT EXISTS (
			    SELECT 1 FROM projects
			     WHERE id=$1 AND org_id=$2 AND status='active'
			)`,
			projectID, orgID,
		).Scan(&allowed)
	} else {
		roles := []string{
			auth.ProjectRoleAdmin,
			auth.ProjectRoleUser,
			auth.ProjectRoleViewer,
			auth.LegacyProjectRoleDeveloper,
			auth.RoleDeveloper,
			auth.RoleViewer,
		}
		if mode == adminScopeWrite {
			roles = []string{
				auth.ProjectRoleAdmin,
				auth.ProjectRoleUser,
				auth.LegacyProjectRoleDeveloper,
				auth.RoleDeveloper,
			}
		}
		err = h.db.QueryRow(r.Context(), `
			SELECT EXISTS (
			    SELECT 1
			      FROM projects p
			      JOIN project_members pm ON pm.project_id=p.id
			      JOIN users u ON u.id=pm.user_id AND u.org_id=p.org_id
			     WHERE p.id=$1 AND p.org_id=$2 AND p.status='active'
			       AND pm.user_id=$3 AND u.status='active'
			       AND pm.role = ANY($4::text[])
			)`,
			projectID, orgID, userID, roles,
		).Scan(&allowed)
	}
	if err != nil {
		return Scope{}, domainError(ErrUnavailable, "project_scope_unavailable")
	}
	if !allowed {
		return Scope{}, ErrForbidden
	}
	return Scope{OrgID: orgID, ProjectID: projectID}, nil
}

func writeMediaError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	var contractErr *Error
	if errors.As(err, &contractErr) {
		code = contractErr.Code
	}
	switch {
	case errors.Is(err, ErrInvalid):
		status = http.StatusBadRequest
		if code == "internal_error" {
			code = "invalid_request"
		}
	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
		if code == "internal_error" {
			code = "forbidden"
		}
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		if code == "internal_error" {
			code = "job_not_found"
		}
	case errors.Is(err, ErrConflict):
		status = http.StatusConflict
		if code == "internal_error" {
			code = "state_conflict"
		}
	case errors.Is(err, ErrOutputExpired):
		status = http.StatusGone
		code = "output_expired"
	case errors.Is(err, ErrUnavailable):
		status = http.StatusServiceUnavailable
		if code == "internal_error" {
			code = "service_unavailable"
		}
	}
	writeMediaJSON(w, status, map[string]any{
		"error": map[string]any{
			"type": "media_job_error", "code": code,
			"message": mediaErrorMessage(status),
		},
	})
}

func mediaErrorMessage(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "The media job request is invalid"
	case http.StatusForbidden:
		return "The media job is not available to this principal"
	case http.StatusNotFound:
		return "The media job was not found"
	case http.StatusConflict:
		return "The media job state conflicts with this operation"
	case http.StatusGone:
		return "The media output has expired"
	case http.StatusServiceUnavailable:
		return "The media job service is temporarily unavailable"
	default:
		return "The media job operation failed"
	}
}

func writeMediaJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
