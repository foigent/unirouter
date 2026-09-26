package admin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/middleware"
)

type contentPolicyController interface {
	GetTx(context.Context, pgx.Tx, int64, int64, string) (contentcapture.PolicyView, error)
	PutTx(context.Context, pgx.Tx, int64, int64, contentcapture.PolicyPut, contentcapture.PolicyApplication) (contentcapture.PolicyView, error)
}
type contentPolicyReadiness func(context.Context) (contentcapture.PolicyApplication, error)

// The readiness callback must verify real runtime/storage readiness outside
// the database transaction. It cannot infer readiness from a configured path.
func (h *Handler) SetContentPolicyController(controller contentPolicyController, readiness func(context.Context) (contentcapture.PolicyApplication, error)) {
	h.contentPolicyController = controller
	h.contentPolicyReadiness = readiness
}

func (h *Handler) GetProjectPayloadPolicy(w http.ResponseWriter, r *http.Request) {
	h.projectPayloadPolicy(w, r, false)
}
func (h *Handler) PutProjectPayloadPolicy(w http.ResponseWriter, r *http.Request) {
	h.projectPayloadPolicy(w, r, true)
}

func (h *Handler) projectPayloadPolicy(w http.ResponseWriter, r *http.Request, update bool) {
	w.Header().Set("Cache-Control", "no-store")
	if h.contentPolicyController == nil || h.contentPolicyReadiness == nil {
		writeJSON(w, 404, map[string]any{"error": "Content policy is not configured"})
		return
	}
	if middleware.GetUserIDFromContext(r.Context()) <= 0 || middleware.GetUserRoleFromContext(r.Context()) == "" {
		writeContentPolicyError(w, errCommittingActorAuthorizationChanged, false)
		return
	}
	project, ok := parsePositivePathID(w, r, "id", "Invalid project ID")
	if !ok {
		return
	}
	var put contentcapture.PolicyPut
	if update {
		wire, err := io.ReadAll(http.MaxBytesReader(w, r.Body, contentcapture.MaxPolicyJSONBytes))
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": "Invalid content policy JSON"})
			return
		}
		put, err = contentcapture.DecodePolicyPut(wire)
		if err != nil {
			writeContentPolicyError(w, err, true)
			return
		}
	}
	// Filesystem checks are completed before acquiring authorization or policy locks.
	application, err := h.contentPolicyReadiness(r.Context())
	if err != nil {
		writeContentPolicyError(w, err, false)
		return
	}
	application.Now = time.Now().UTC()
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeContentPolicyError(w, err, false)
		return
	}
	defer tx.Rollback(r.Context())
	actor, err := h.reauthorizeCommittingActorTx(r.Context(), tx, committingActorRequirement{
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
		Project:                  &committingActorProjectRequirement{ProjectID: project, AllowedRoles: []string{projectMemberAdmin}, MustBeActive: update && put.Desired.Mode != contentcapture.ModeDisabled},
	})
	if err != nil {
		writeContentPolicyError(w, err, false)
		return
	}
	var view contentcapture.PolicyView
	if update {
		view, err = h.contentPolicyController.PutTx(r.Context(), tx, actor.TargetOrgID, project, put, application)
		if err == nil {
			err = insertAuditEntry(r.Context(), tx, "content.policy.update", "project", strconv.FormatInt(project, 10), map[string]any{"desired": view.Desired, "effective": view.Effective, "effective_at": view.EffectiveAt, "application_status": view.ApplicationStatus}, requestAuditMeta(r, actor.TargetOrgID, project))
		}
	} else {
		view, err = h.contentPolicyController.GetTx(r.Context(), tx, actor.TargetOrgID, project, application.StorageStatus)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeContentPolicyError(w, err, update)
		return
	}
	status := http.StatusOK
	if update && view.ApplicationStatus == "pending" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, view)
}

func writeContentPolicyError(w http.ResponseWriter, err error, input bool) {
	status, message := http.StatusServiceUnavailable, "Content policy is unavailable"
	switch {
	case errors.Is(err, errCommittingActorAuthorizationChanged):
		status, message = 403, "Forbidden"
	case errors.Is(err, errCommittingActorScopeNotFound), errors.Is(err, contentcapture.ErrPolicyProjectNotFound):
		status, message = 404, "Project not found"
	case errors.Is(err, contentcapture.ErrPolicyVersionConflict):
		status, message = 409, "Content policy version changed"
	case input && errors.Is(err, contentcapture.ErrInvalidPolicy):
		status, message = 400, "Invalid or unsupported content policy"
	case errors.Is(err, contentcapture.ErrPolicyInactive):
		message = "Content policy is stopped by deployment settings"
	}
	writeJSON(w, status, map[string]any{"error": message})
}
