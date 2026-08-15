package admin

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/middleware"
)

type channelModelProbeRecord struct {
	ChannelID            int64
	PublicModel          string
	UpstreamModel        string
	RoutingEpoch         int64
	CredentialID         int64
	CredentialStateEpoch int64
	Success              bool
	StatusCode           int
	LatencyMS            int64
	ErrorSummary         string
}

func (h *Handler) persistChannelModelProbe(r *http.Request, record channelModelProbeRecord) error {
	if isUnauthenticatedUnitContext(r.Context()) {
		return nil
	}
	var credentialID interface{}
	if record.CredentialID > 0 {
		credentialID = record.CredentialID
	}
	if record.CredentialStateEpoch <= 0 {
		record.CredentialStateEpoch = 1
	}
	_, err := h.db.Exec(r.Context(), `
INSERT INTO channel_model_probes (
    channel_id,public_model,upstream_model,routing_epoch,credential_id,
    credential_state_epoch,success,status_code,latency_ms,error_summary,tested_by,tested_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
ON CONFLICT (channel_id,public_model) DO UPDATE SET
    upstream_model=EXCLUDED.upstream_model,
    routing_epoch=EXCLUDED.routing_epoch,
    credential_id=EXCLUDED.credential_id,
    credential_state_epoch=EXCLUDED.credential_state_epoch,
    success=EXCLUDED.success,
    status_code=EXCLUDED.status_code,
    latency_ms=EXCLUDED.latency_ms,
    error_summary=EXCLUDED.error_summary,
    tested_by=EXCLUDED.tested_by,
    tested_at=EXCLUDED.tested_at`,
		record.ChannelID, record.PublicModel, record.UpstreamModel, record.RoutingEpoch, credentialID,
		record.CredentialStateEpoch, record.Success, record.StatusCode, record.LatencyMS,
		auditErrorSummary(record.ErrorSummary), middleware.GetUserIDFromContext(r.Context()))
	return err
}
