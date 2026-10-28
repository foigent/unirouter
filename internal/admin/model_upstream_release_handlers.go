package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/strictjson"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
)

func upstreamReleaseTarget(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("deploymentID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, upstreamrelease.ErrInvalid
	}
	return id, nil
}

func writeUpstreamReleaseError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "upstream_release_unavailable"
	var pe *pgconn.PgError
	switch {
	case errors.Is(err, upstreamrelease.ErrInvalid):
		status, code = 400, "upstream_release_invalid"
	case errors.Is(err, pgx.ErrNoRows):
		status, code = 404, "upstream_release_not_found"
	case errors.Is(err, errCommittingActorAuthorizationChanged), errors.Is(err, errCommittingActorScopeNotFound):
		status, code = 403, "upstream_release_forbidden"
	case errors.As(err, &pe) && pe.Code == "42501":
		status, code = 403, "upstream_release_forbidden"
	case errors.Is(err, upstreamrelease.ErrConflict):
		status, code = 409, "upstream_release_conflict"
	case errors.As(err, &pe) && (pe.Code == "55000" || pe.Code == "40001" || pe.Code == "23514"):
		status, code = 409, "upstream_release_conflict"
	}
	writeJSON(w, status, map[string]any{"error": code, "code": code})
}

func (h *Handler) GetModelUpstreamReleaseObservation(w http.ResponseWriter, r *http.Request) {
	id, err := upstreamReleaseTarget(r)
	if err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeUpstreamReleaseError(w, errCommittingActorScopeNotFound)
		return
	}
	state, _, err := upstreamrelease.Read(r.Context(), h.db, orgID, id, false)
	if err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	writeJSON(w, 200, state)
}

func (h *Handler) PutModelUpstreamReleaseObservation(w http.ResponseWriter, r *http.Request) {
	id, err := upstreamReleaseTarget(r)
	if err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<10))
	if err != nil || strictjson.Validate(body) != nil {
		writeUpstreamReleaseError(w, upstreamrelease.ErrInvalid)
		return
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(body, &members) != nil {
		writeUpstreamReleaseError(w, upstreamrelease.ErrInvalid)
		return
	}
	for _, key := range []string{"expected_revision", "expected_identity", "expected_connection_revision", "expected_deployment_revision", "release_label", "source_url"} {
		if _, ok := members[key]; !ok {
			writeUpstreamReleaseError(w, upstreamrelease.ErrInvalid)
			return
		}
	}
	for key := range members {
		switch key {
		case "expected_revision", "expected_identity", "expected_connection_revision", "expected_deployment_revision", "release_label", "announced_on", "source_url", "source_revision", "source_sha256":
		default:
			writeUpstreamReleaseError(w, upstreamrelease.ErrInvalid)
			return
		}
	}
	for _, value := range members {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			writeUpstreamReleaseError(w, upstreamrelease.ErrInvalid)
			return
		}
	}
	var input upstreamrelease.Input
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		writeUpstreamReleaseError(w, upstreamrelease.ErrInvalid)
		return
	}
	if input.ExpectedConnectionRevision <= 0 || input.ExpectedDeploymentRevision <= 0 || input.ExpectedRevision < 0 || !upstreamrelease.ValidIdentity(input.ExpectedIdentity) {
		writeUpstreamReleaseError(w, upstreamrelease.ErrInvalid)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeUpstreamReleaseError(w, errCommittingActorScopeNotFound)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if err := validateCanonicalOperationProbeCustomerOrg(r.Context(), tx, orgID); err != nil {
		writeUpstreamReleaseError(w, errCommittingActorScopeNotFound)
		return
	}
	actor, err := h.reauthorizeCommittingActorTx(r.Context(), tx, committingActorRequirement{
		TargetOrgID: orgID, AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
	})
	if err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	current, scope, err := upstreamrelease.Read(r.Context(), tx, orgID, id, true)
	if err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	var connectionRevision, deploymentRevision int64
	if err = tx.QueryRow(r.Context(), `SELECT c.config_revision,d.config_revision FROM model_deployments d JOIN provider_connections c ON c.id=d.connection_id WHERE d.id=$1`, id).Scan(&connectionRevision, &deploymentRevision); err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	if input.ExpectedConnectionRevision != connectionRevision || input.ExpectedDeploymentRevision != deploymentRevision {
		writeUpstreamReleaseError(w, upstreamrelease.ErrConflict)
		return
	}
	var retired bool
	if err = tx.QueryRow(r.Context(), `SELECT admin_state='retired' FROM model_deployments WHERE id=$1`, id).Scan(&retired); err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	if retired {
		writeUpstreamReleaseError(w, upstreamrelease.ErrConflict)
		return
	}
	next, err := upstreamrelease.Next(scope, current, input, actor.UserID, time.Now())
	if err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	raw, err := json.Marshal(next.Observation)
	if err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	tag, err := tx.Exec(r.Context(), `UPDATE model_deployments SET upstream_release_observation=$4,upstream_observation_revision=$5
 WHERE id=$1 AND customer_org_id=$2 AND upstream_observation_revision=$3
 AND COALESCE(upstream_release_observation->>'identity','')=$6`, id, orgID, input.ExpectedRevision, raw, next.Revision, input.ExpectedIdentity)
	if err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	if tag.RowsAffected() != 1 {
		writeUpstreamReleaseError(w, upstreamrelease.ErrConflict)
		return
	}
	if err = insertAuditEntry(r.Context(), tx, "model_deployment.upstream_release_observation", "model_deployment", strconv.FormatInt(id, 10),
		map[string]any{"before": current, "after": next}, requestAuditMeta(r, orgID, 0)); err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeUpstreamReleaseError(w, err)
		return
	}
	writeJSON(w, 200, next)
}
