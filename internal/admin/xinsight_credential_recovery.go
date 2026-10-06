package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

type xInsightRecoveryInput struct {
	RecoveryTicket string `json:"recovery_ticket"`
}

type xInsightRecoveryPreview struct {
	SchemaVersion       string                                  `json:"schema_version"`
	InstallationID      string                                  `json:"installation_id"`
	DeploymentID        string                                  `json:"deployment_id"`
	ProjectID           string                                  `json:"project_id"`
	FinalEventScope     []string                                `json:"final_event_scope"`
	SourceBindingSHA256 string                                  `json:"source_binding_sha256"`
	XInsightOrigin      string                                  `json:"xinsight_origin"`
	UniRouterOrigin     string                                  `json:"unirouter_origin"`
	RecoveryID          string                                  `json:"recovery_id"`
	ExpiresAt           string                                  `json:"expires_at"`
	Credential          xinsightconnector.CredentialLocalStatus `json:"credential"`
}

// The XI handoff carries no tenant selector. Resolve the installation's
// original organization locally, then reauthorize both the present and saved
// setup actors before revealing it. No ticket or XI request is involved.
func (h *Handler) GetXInsightCredentialRecoveryScope(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("installation_id")
	var org int64
	if err := h.db.QueryRow(r.Context(), `SELECT org_id FROM xinsight_connector_installations
WHERE installation_id=$1 AND setup_request_id IS NOT NULL`, id).Scan(&org); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "recovery_scope_unavailable"})
		return
	}
	if _, err := h.preflightXInsightCredentialRecovery(r.Context(), r, org, id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "recovery_scope_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schema_version":  "unirouter.xinsight.credential-recovery-scope.v1",
		"installation_id": id, "org_id": org,
	})
}

func (h *Handler) preflightXInsightCredentialRecovery(ctx context.Context, r *http.Request, org int64, id string) (int64, error) {
	journal := h.xInsightCredentialJournalSnapshot()
	if journal == nil || h.xInsightSetup == nil {
		return 0, xinsightconnector.ErrUnavailable
	}
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var project int64
	if err = tx.QueryRow(ctx, `SELECT project_id FROM xinsight_connector_installations WHERE installation_id=$1 AND org_id=$2 AND setup_request_id IS NOT NULL`, id, org).Scan(&project); err != nil {
		return 0, xinsightconnector.ErrConflict
	}
	if _, err = h.reauthorizeCommittingActorTx(ctx, tx, setupRequirement(org, project)); err != nil {
		return 0, xinsightconnector.ErrUnauthorized
	}
	saved, _, setupState, err := readXInsightSavedSetupTx(ctx, tx, id, false)
	if err != nil || setupState == "needs_action" || setupState == "failed" {
		return 0, xinsightconnector.ErrUnauthorized
	}
	if _, err = h.reauthorizeXInsightSetupActorTx(ctx, tx, saved.ActorID, saved.ActorEpoch, setupRequirement(org, project)); err != nil {
		return 0, xinsightconnector.ErrUnauthorized
	}
	if err = journal.ValidateCredentialOriginTx(ctx, tx, id, h.xInsightSetup.peer.XInsightOrigin); err != nil {
		return 0, err
	}
	if saved.XIConsent.UROrigin != h.xInsightSetup.peer.UROrigin || saved.XIConsent.DeploymentID != h.xInsightSetup.peer.XDeploymentID {
		return 0, xinsightconnector.ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return project, nil
}

func (h *Handler) resolveXInsightCredentialRecovery(ctx context.Context, ticket string) (xinsightconnector.CredentialRecoveryResolution, error) {
	var out xinsightconnector.CredentialRecoveryResolution
	if h.xInsightSetup == nil || h.xInsightSetup.client == nil {
		return out, xinsightconnector.ErrUnavailable
	}
	input, _ := json.Marshal(struct {
		Ticket string `json:"ticket"`
	}{ticket})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.xInsightSetup.peer.XInsightOrigin+"/api/v1/connector-control/credential-recoveries/resolve", bytes.NewReader(input))
	if err != nil {
		return out, xinsightconnector.ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := h.xInsightSetup.client.Do(req)
	if err != nil {
		return out, xinsightconnector.ErrUnavailable
	}
	defer res.Body.Close()
	mediaType, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if res.StatusCode != http.StatusOK || mediaType != "application/json" {
		return out, xinsightconnector.ErrConflict
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32769))
	if err != nil || len(raw) > 32768 || decodeXInsightSetupWire(raw, &out) != nil || out.Validate(time.Now().UTC()) != nil ||
		out.XInsightOrigin != h.xInsightSetup.peer.XInsightOrigin || out.UniRouterOrigin != h.xInsightSetup.peer.UROrigin ||
		out.DeploymentID != h.xInsightSetup.peer.XDeploymentID {
		return out, xinsightconnector.ErrConflict
	}
	return out, nil
}

func (h *Handler) xInsightRecoveryInput(w http.ResponseWriter, r *http.Request) (int64, xInsightRecoveryInput, bool) {
	org, ok := parsePositivePathID(w, r, "org_id", "Invalid organization")
	if !ok {
		return 0, xInsightRecoveryInput{}, false
	}
	var in xInsightRecoveryInput
	if decodeXInsightSetupJSON(w, r, &in) != nil || len(in.RecoveryTicket) != 48 ||
		!xinsightconnector.IsCredentialRecoveryTicket(in.RecoveryTicket) {
		writeJSON(w, 400, map[string]any{"error": "invalid_recovery_ticket"})
		return 0, in, false
	}
	return org, in, true
}

func (h *Handler) PreviewXInsightCredentialRecovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	org, in, ok := h.xInsightRecoveryInput(w, r)
	if !ok {
		return
	}
	id := r.PathValue("installation_id")
	if _, err := h.preflightXInsightCredentialRecovery(r.Context(), r, org, id); err != nil {
		writeSetupError(w, err)
		return
	}
	resolution, err := h.resolveXInsightCredentialRecovery(r.Context(), in.RecoveryTicket)
	if err != nil || resolution.InstallationID != r.PathValue("installation_id") {
		writeSetupError(w, xinsightconnector.ErrConflict)
		return
	}
	journal := h.xInsightCredentialJournalSnapshot()
	if journal == nil {
		writeSetupError(w, xinsightconnector.ErrUnavailable)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeSetupError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var project int64
	var deployment, eventScope, sourceBinding, controlProject string
	if tx.QueryRow(r.Context(), `SELECT i.project_id,i.x_deployment_id,i.event_scope,i.source_binding_sha256,b.control_project_id
FROM xinsight_connector_installations i JOIN xinsight_connector_control_bindings b USING(installation_id)
WHERE i.installation_id=$1 AND i.org_id=$2 AND i.setup_request_id IS NOT NULL`, resolution.InstallationID, org).
		Scan(&project, &deployment, &eventScope, &sourceBinding, &controlProject) != nil {
		writeJSON(w, 404, map[string]any{"error": "setup_not_found"})
		return
	}
	if resolution.DeploymentID != deployment || resolution.ProjectID != controlProject ||
		resolution.SourceBindingSHA256 != sourceBinding || len(resolution.FinalEventScope) != 1 ||
		resolution.FinalEventScope[0] != eventScope {
		writeSetupError(w, xinsightconnector.ErrConflict)
		return
	}
	if _, err := h.reauthorizeCommittingActorTx(r.Context(), tx, setupRequirement(org, project)); err != nil {
		writeJSON(w, 403, map[string]any{"error": "recovery_denied"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeSetupError(w, err)
		return
	}
	credential, err := journal.CredentialStatus(r.Context(), resolution.InstallationID)
	if err != nil || (credential.State != "expired_recovery_required" && credential.State != "rotation_pending" && credential.State != "committed_unpromoted") {
		writeSetupError(w, xinsightconnector.ErrConflict)
		return
	}
	writeJSON(w, 200, xInsightRecoveryPreview{SchemaVersion: "unirouter.xinsight.credential-recovery-preview.v1",
		InstallationID: resolution.InstallationID, DeploymentID: resolution.DeploymentID,
		ProjectID: resolution.ProjectID, FinalEventScope: resolution.FinalEventScope,
		SourceBindingSHA256: resolution.SourceBindingSHA256, XInsightOrigin: resolution.XInsightOrigin,
		UniRouterOrigin: resolution.UniRouterOrigin, RecoveryID: resolution.RecoveryID,
		ExpiresAt: resolution.ExpiresAt, Credential: credential})
}

func (h *Handler) ConfirmXInsightCredentialRecovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	org, in, ok := h.xInsightRecoveryInput(w, r)
	if !ok {
		return
	}
	id := r.PathValue("installation_id")
	if _, err := h.preflightXInsightCredentialRecovery(r.Context(), r, org, id); err != nil {
		writeSetupError(w, err)
		return
	}
	resolution, err := h.resolveXInsightCredentialRecovery(r.Context(), in.RecoveryTicket)
	if err != nil || resolution.InstallationID != r.PathValue("installation_id") {
		writeSetupError(w, xinsightconnector.ErrConflict)
		return
	}
	journal := h.xInsightCredentialJournalSnapshot()
	if journal == nil {
		writeSetupError(w, xinsightconnector.ErrUnavailable)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeSetupError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var project int64
	if err = tx.QueryRow(r.Context(), `SELECT project_id FROM xinsight_connector_installations WHERE installation_id=$1 AND org_id=$2 AND setup_request_id IS NOT NULL`, resolution.InstallationID, org).Scan(&project); err != nil {
		writeJSON(w, 404, map[string]any{"error": "setup_not_found"})
		return
	}
	actor, err := h.reauthorizeCommittingActorTx(r.Context(), tx, setupRequirement(org, project))
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "recovery_denied"})
		return
	}
	saved, _, setupState, err := readXInsightSavedSetupTx(r.Context(), tx, resolution.InstallationID, false)
	if err != nil || setupState == "needs_action" || setupState == "failed" {
		writeJSON(w, 403, map[string]any{"error": "recovery_denied"})
		return
	}
	if _, err = h.reauthorizeXInsightSetupActorTx(r.Context(), tx, saved.ActorID, saved.ActorEpoch, setupRequirement(org, project)); err != nil {
		writeJSON(w, 403, map[string]any{"error": "recovery_denied"})
		return
	}
	opID, err := journal.PrepareRecoveryTx(r.Context(), tx, resolution.InstallationID, in.RecoveryTicket, resolution)
	if err != nil {
		writeSetupError(w, err)
		return
	}
	if err = insertAuditEntry(r.Context(), tx, "data_exchange.xinsight.control_credential.recovery_confirm", "xinsight_connector_installation", resolution.InstallationID,
		map[string]any{"operation_id": opID, "recovery_id": resolution.RecoveryID, "actor_id": strconv.FormatInt(actor.UserID, 10)}, requestAuditMeta(r, org, project)); err != nil {
		writeSetupError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeSetupError(w, err)
		return
	}
	writeJSON(w, 202, map[string]any{"schema_version": "unirouter.xinsight.credential-recovery-accepted.v1", "installation_id": resolution.InstallationID, "operation_id": opID, "state": "pending"})
}
