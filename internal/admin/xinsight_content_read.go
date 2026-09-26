package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

type contentControlView struct {
	SchemaVersion        string                     `json:"schema_version"`
	SourceInstance       string                     `json:"source_instance"`
	OrgID                string                     `json:"org_id"`
	ProjectID            string                     `json:"project_id"`
	ConnectionStatus     string                     `json:"connection_status"`
	ContentStatus        string                     `json:"content_status"`
	StateVersion         string                     `json:"state_version"`
	ContentScopeRevision string                     `json:"content_scope_revision"`
	Active               *contentControlKeyRef      `json:"active,omitempty"`
	Staged               *contentControlKeyRef      `json:"staged,omitempty"`
	ContentAccess        *sourcefacts.ContentAccess `json:"content_access,omitempty"`
}

type contentControlKeyRef struct {
	CredentialID     string `json:"credential_id"`
	RegistrationHash string `json:"registration_hash"`
}

func contentKeyReference(key *contentControlKey) *contentControlKeyRef {
	if key == nil {
		return nil
	}
	return &contentControlKeyRef{CredentialID: key.CredentialID, RegistrationHash: key.RegistrationHash}
}

func contentView(instance string, org, project int64, s contentControlState) contentControlView {
	return contentControlView{SchemaVersion: "unirouter.source-content-control.v1", SourceInstance: instance,
		OrgID: strconv.FormatInt(org, 10), ProjectID: strconv.FormatInt(project, 10), ConnectionStatus: s.global, ContentStatus: s.local,
		StateVersion: strconv.FormatInt(s.version, 10), ContentScopeRevision: strconv.FormatInt(s.generation, 10),
		Active: contentKeyReference(s.active), Staged: contentKeyReference(s.staged), ContentAccess: contentAccessProjection(s)}
}

// Parent -> scope -> keys is shared with all C management transactions.
// The caller must first perform the original admin authorization in this tx.
func readContentStateTx(ctx context.Context, tx pgx.Tx, org, project int64, instance string, write bool) (contentControlState, error) {
	s := contentControlState{global: "not_configured", local: "not_configured"}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	var storedInstance string
	err := tx.QueryRow(ctx, `SELECT source_instance,status,content_safety_version FROM execution_source_connections WHERE org_id=$1 AND project_id=$2`+lock, org, project).Scan(&storedInstance, &s.global, &s.safetyVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if storedInstance != instance {
		return s, errXInsightFeedConflict
	}
	err = tx.QueryRow(ctx, `SELECT status,state_version,access_generation FROM execution_source_content_scopes WHERE org_id=$1 AND project_id=$2`+lock, org, project).Scan(&s.local, &s.version, &s.generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	rows, err := tx.Query(ctx, `SELECT ak.id,ak.status,ak.allowed_ips,sc.registration_sha256,b.coverage_after_sequence,b.coverage_time,b.allowed_purposes
	 FROM api_keys ak JOIN execution_source_credentials sc ON sc.api_key_id=ak.id AND sc.org_id=ak.org_id AND sc.project_id=ak.project_id AND sc.source_lifecycle_scope=ak.source_lifecycle_scope
	 JOIN execution_source_content_bindings b ON b.api_key_id=ak.id AND b.org_id=ak.org_id AND b.project_id=ak.project_id
	 WHERE ak.org_id=$1 AND ak.project_id=$2 AND ak.purpose='xinsight.source.read' AND ak.source_lifecycle_scope='content' AND ak.status IN ('feed_active','feed_staged') ORDER BY ak.id`+lock, org, project)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var key contentControlKey
		var id, after int64
		var state string
		var at time.Time
		if err = rows.Scan(&id, &state, &key.ips, &key.RegistrationHash, &after, &at, &key.AllowedPurposes); err != nil {
			return s, err
		}
		key.CredentialID = strconv.FormatInt(id, 10)
		key.CoverageFrom = sourcefacts.ContentBoundary{Time: executioncontext.Time(at), AfterSequence: strconv.FormatInt(after, 10)}
		if state == "feed_active" {
			if s.active != nil {
				return s, errXInsightFeedConflict
			}
			s.active = &key
		} else {
			if s.staged != nil {
				return s, errXInsightFeedConflict
			}
			s.staged = &key
		}
	}
	return s, rows.Err()
}

func (h *Handler) GetXInsightContentConnection(w http.ResponseWriter, r *http.Request) {
	if !h.sourceControlReady(w, false) {
		return
	}
	project, err := xInsightFeedProjectID(r)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	org, err := h.xInsightFeedExistingProjectOrg(r.Context(), project)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if err = h.reauthorizeXInsightFeedControlTx(r.Context(), tx, org, project, apiKeyMutationUpdate, false); err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	present, err := hasContentScopeTx(r.Context(), tx)
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	if !present {
		writeJSON(w, 404, map[string]any{"error": "Content lifecycle is not supported"})
		return
	}
	instance := h.sourceFactsController.SourceInstance()
	s, err := readContentStateTx(r.Context(), tx, org, project, instance, false)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeXInsightFeedControlError(w, err)
		return
	}
	writeJSON(w, 200, contentView(instance, org, project, s))
}
