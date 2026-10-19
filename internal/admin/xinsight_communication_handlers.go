package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

type xInsightCommunicationOnce func(context.Context, *xinsightconnector.CommunicationPermit, string) (string, bool, error)

func (h *Handler) SetXInsightCommunication(store *xinsightconnector.CommunicationStore, once func(context.Context, *xinsightconnector.CommunicationPermit, string) (string, bool, error)) {
	h.xInsightCommunication = store
	h.xInsightCommunicationOnce = once
}

func writeCommunicationError(w http.ResponseWriter, e error) {
	code := http.StatusServiceUnavailable
	message := "Communication state is unavailable; no new operation was admitted"
	switch {
	case errors.Is(e, xinsightconnector.ErrInvalid):
		code = 400
		message = "Invalid communication request"
	case errors.Is(e, pgx.ErrNoRows):
		code = 404
		message = "Connection is not configured"
	case errors.Is(e, xinsightconnector.ErrConflict):
		code = 409
		message = "Connection or policy changed; refresh before retrying"
	case errors.Is(e, xinsightconnector.ErrCommunicationBusy):
		code = 409
		message = "A previous operation has not confirmed completion"
	case errors.Is(e, errAPIKeyAuthorizationChanged):
		code = 403
		message = "Communication control is not authorized"
	}
	writeJSON(w, code, map[string]any{"error": message})
}

func (h *Handler) GetXInsightCommunication(w http.ResponseWriter, r *http.Request) {
	project, e := xInsightFeedProjectID(r)
	if e != nil {
		writeCommunicationError(w, e)
		return
	}
	org, e := h.xInsightFeedExistingProjectOrg(r.Context(), project)
	if e != nil {
		writeXInsightFeedControlError(w, e)
		return
	}
	if h.xInsightCommunication == nil {
		writeCommunicationError(w, xinsightconnector.ErrUnavailable)
		return
	}
	tx, e := h.db.Begin(r.Context())
	if e != nil {
		writeCommunicationError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	if e = h.reauthorizeXInsightFeedControlTx(r.Context(), tx, org, project, apiKeyMutationUpdate, false); e != nil {
		writeCommunicationError(w, e)
		return
	}
	state, e := h.xInsightCommunication.ReadTx(r.Context(), tx, org, project)
	if e != nil {
		writeCommunicationError(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeCommunicationError(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, state)
}

type xInsightCommunicationUpdate struct {
	AutomaticEnabled *bool  `json:"automatic_enabled"`
	CadenceMS        int    `json:"cadence_ms"`
	ExpectedRevision int64  `json:"expected_revision"`
	InstallationID   string `json:"installation_id"`
}

func (h *Handler) UpdateXInsightCommunication(w http.ResponseWriter, r *http.Request) {
	var request xInsightCommunicationUpdate
	if !decodeXInsightFeedControlJSON(w, r, &request) {
		return
	}
	if request.AutomaticEnabled == nil {
		writeCommunicationError(w, xinsightconnector.ErrInvalid)
		return
	}
	project, e := xInsightFeedProjectID(r)
	if e != nil {
		writeCommunicationError(w, e)
		return
	}
	org, e := h.xInsightFeedExistingProjectOrg(r.Context(), project)
	if e != nil {
		writeXInsightFeedControlError(w, e)
		return
	}
	if h.xInsightCommunication == nil {
		writeCommunicationError(w, xinsightconnector.ErrUnavailable)
		return
	}
	tx, e := h.db.Begin(r.Context())
	if e != nil {
		writeCommunicationError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	if e = h.reauthorizeXInsightFeedControlTx(r.Context(), tx, org, project, apiKeyMutationUpdate, *request.AutomaticEnabled); e != nil {
		writeCommunicationError(w, e)
		return
	}
	before, e := h.xInsightCommunication.ReadTx(r.Context(), tx, org, project)
	if e != nil {
		writeCommunicationError(w, e)
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	if e = h.xInsightCommunication.UpdateTx(r.Context(), tx, org, project, actor, request.ExpectedRevision, request.InstallationID, *request.AutomaticEnabled, request.CadenceMS); e != nil {
		writeCommunicationError(w, e)
		return
	}
	if e = insertAuditEntry(r.Context(), tx, "data_exchange.xinsight.communication.update", "xinsight_communication", strconv.FormatInt(project, 10), map[string]any{"before": map[string]any{"automatic_enabled": before.AutomaticEnabled, "cadence_ms": before.CadenceMS, "revision": before.Revision}, "after": map[string]any{"automatic_enabled": *request.AutomaticEnabled, "cadence_ms": request.CadenceMS, "revision": request.ExpectedRevision + 1}}, requestAuditMeta(r, org, project)); e != nil {
		writeCommunicationError(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeCommunicationError(w, e)
		return
	}
	h.GetXInsightCommunication(w, r)
}

type xInsightCommunicationSync struct {
	IdempotencyKey   string `json:"idempotency_key"`
	ExpectedRevision int64  `json:"expected_revision"`
	InstallationID   string `json:"installation_id"`
}

func (h *Handler) SyncXInsightCommunicationOnce(w http.ResponseWriter, r *http.Request) {
	var request xInsightCommunicationSync
	if !decodeXInsightFeedControlJSON(w, r, &request) {
		return
	}
	project, e := xInsightFeedProjectID(r)
	if e != nil {
		writeCommunicationError(w, e)
		return
	}
	org, e := h.xInsightFeedExistingProjectOrg(r.Context(), project)
	if e != nil {
		writeXInsightFeedControlError(w, e)
		return
	}
	if h.xInsightCommunication == nil || h.xInsightCommunicationOnce == nil {
		writeCommunicationError(w, xinsightconnector.ErrUnavailable)
		return
	}
	tx, e := h.db.Begin(r.Context())
	if e != nil {
		writeCommunicationError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	if e = h.reauthorizeXInsightFeedControlTx(r.Context(), tx, org, project, apiKeyMutationUpdate, true); e != nil {
		writeCommunicationError(w, e)
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	permit, replay, e := h.xInsightCommunication.ClaimTx(r.Context(), tx, org, project, actor, request.ExpectedRevision, request.InstallationID, request.IdempotencyKey, "manual")
	if e != nil {
		writeCommunicationError(w, e)
		return
	}
	if replay == nil {
		e = insertAuditEntry(r.Context(), tx, "data_exchange.xinsight.communication.once", "xinsight_communication", strconv.FormatInt(project, 10), map[string]any{"installation_id": request.InstallationID, "revision": request.ExpectedRevision, "idempotency_key": request.IdempotencyKey}, requestAuditMeta(r, org, project))
		if e != nil {
			writeCommunicationError(w, e)
			return
		}
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeCommunicationError(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if replay != nil {
		writeJSON(w, 200, replay)
		return
	}
	code, success, _ := h.xInsightCommunicationOnce(r.Context(), permit, request.InstallationID)
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if e = permit.Finish(finishCtx, code, success); e != nil {
		writeCommunicationError(w, e)
		return
	}
	// Return the durable receipt even when the finite burst failed or was partial.
	state, e := h.xInsightCommunication.Read(finishCtx, org, project)
	if e != nil {
		writeCommunicationError(w, e)
		return
	}
	writeJSON(w, 200, state.LastRun)
}

// ObserveXInsightSetupPeer is only called inside an admitted control unit.
// Status-page GETs read this bounded latest observation; they never contact XI.
func (h *Handler) ObserveXInsightSetupPeer(ctx context.Context, id, deployment string) error {
	if h.xInsightSetup == nil || h.xInsightSetup.status == nil || h.xInsightCommunication == nil {
		return nil
	}
	var normal bool
	if e := h.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xinsight_connector_installations WHERE installation_id=$1 AND setup_request_id IS NOT NULL AND observed_state<>'disconnected')`, id).Scan(&normal); e != nil {
		return e
	}
	if !normal {
		return nil
	}
	raw, e := h.xInsightSetup.status(ctx, id, deployment)
	if e != nil {
		return e
	}
	var status xInsightSetupXIStatus
	if decodeXInsightSetupWire(raw, &status) != nil || status.SchemaVersion != "xinsight.connector-setup-status.v1" || status.InstallationID != id || status.DeploymentID != deployment {
		return xinsightconnector.ErrConflict
	}
	return h.xInsightCommunication.SavePeerObservation(ctx, id, raw)
}

// These local seams keep an explicit burst scoped to its saved installation.
func (h *Handler) AdvanceXInsightSetup(ctx context.Context, id string) error {
	var normal bool
	if e := h.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xinsight_connector_installations WHERE installation_id=$1 AND setup_request_id IS NOT NULL AND observed_state<>'disconnected')`, id).Scan(&normal); e != nil {
		return e
	}
	if !normal {
		return nil
	}
	return h.advanceXInsightSetup(ctx, id)
}
func (h *Handler) XInsightSetupInitializationPending(ctx context.Context, id string) (bool, error) {
	var normal bool
	if e := h.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xinsight_connector_installations WHERE installation_id=$1 AND setup_request_id IS NOT NULL)`, id).Scan(&normal); e != nil {
		return true, e
	}
	if !normal {
		return false, nil
	}
	raw, e := h.xInsightCommunication.PeerObservation(ctx, id)
	if e != nil {
		return true, nil
	}
	var xi xInsightSetupXIStatus
	if decodeXInsightSetupWire(raw, &xi) != nil {
		return true, nil
	}
	return !xi.Custody.Loaded || xi.Custody.Settings == nil || xi.Custody.EffectiveRevision < 1 || xi.Custody.DesiredRevision != xi.Custody.EffectiveRevision || (xi.State != "ready" && xi.State != "waiting_data"), nil
}
