package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

type policyDelegationInput struct {
	InstallationID   string `json:"installation_id"`
	TargetKind       string `json:"target_kind"`
	Coverage         string `json:"coverage"`
	ProjectID        string `json:"project_id,omitempty"`
	UserID           string `json:"user_id,omitempty"`
	Read             bool   `json:"read"`
	Write            bool   `json:"write"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	ExpectedRevision string `json:"expected_revision,omitempty"`
}

type policyDelegationRecord struct {
	ID             int64
	InstallationID string
	OrgID          int64
	AnchorProject  int64
	TargetKind     string
	Coverage       string
	ProjectID      int64
	UserID         int64
	Read           bool
	Write          bool
	GrantorID      int64
	Revision       int64
	State          string
	ExpiresAt      *time.Time
}

func decodePolicyDelegation(w http.ResponseWriter, r *http.Request) (policyDelegationInput, bool) {
	var input policyDelegationInput
	wire, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<10))
	if err == nil {
		if strictErr := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); strictErr != nil {
			err = strictErr
		}
	}
	if err == nil {
		var fields map[string]json.RawMessage
		if json.Unmarshal(wire, &fields) != nil || fields == nil {
			err = errors.New("invalid object")
		} else {
			allowed := map[string]bool{"installation_id": true, "target_kind": true, "coverage": true,
				"project_id": true, "user_id": true, "read": true, "write": true,
				"expires_at": true, "expected_revision": true}
			for field := range fields {
				if !allowed[field] {
					err = errors.New("unknown field")
				}
			}
		}
	}
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(wire))
		dec.DisallowUnknownFields()
		err = dec.Decode(&input)
		if err == nil {
			err = dec.Decode(new(any))
			if errors.Is(err, io.EOF) {
				err = nil
			} else if err == nil {
				err = errors.New("trailing JSON")
			}
		}
	}
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": "Invalid delegation JSON"})
		return input, false
	}
	return input, true
}

func positiveIDOrZero(raw string) (int64, bool) {
	if raw == "" {
		return 0, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == raw
}

func (input policyDelegationInput) scope() (int64, int64, *time.Time, bool) {
	projectID, ok := positiveIDOrZero(input.ProjectID)
	if !ok {
		return 0, 0, nil, false
	}
	userID, ok := positiveIDOrZero(input.UserID)
	if !ok || input.InstallationID == "" || (!input.Read && !input.Write) {
		return 0, 0, nil, false
	}
	switch input.TargetKind {
	case "enterprise_default":
		ok = input.Coverage == "exact" && projectID == 0 && userID == 0
	case "project_policy":
		ok = userID == 0 && (input.Coverage == "exact" && projectID > 0 || input.Coverage == "all_current_future" && projectID == 0)
	case "user_exception":
		ok = input.Coverage == "exact" && projectID > 0 && userID > 0 || input.Coverage == "all_current_future" && projectID == 0 && userID == 0
	default:
		ok = false
	}
	if !ok {
		return 0, 0, nil, false
	}
	var expires *time.Time
	if input.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, input.ExpiresAt)
		if err != nil || !parsed.After(time.Now()) {
			return 0, 0, nil, false
		}
		expires = &parsed
	}
	return projectID, userID, expires, true
}

func (h *Handler) authorizePolicyDelegationAdminTx(r *http.Request, tx pgx.Tx, orgID, projectID, userID, grantorID int64, coverage, targetKind string) (int64, error) {
	requirement := committingActorRequirement{TargetOrgID: orgID, OrganizationForUpdate: true,
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin}}
	if grantorID > 0 {
		requirement.AdditionalUserIDs = append(requirement.AdditionalUserIDs, grantorID)
	}
	if coverage == "exact" && targetKind != "enterprise_default" {
		requirement.Project = &committingActorProjectRequirement{ProjectID: projectID, AllowedRoles: []string{projectMemberAdmin}, MustBeActive: true}
		if userID > 0 {
			requirement.AdditionalUserIDs = append(requirement.AdditionalUserIDs, userID)
		}
	}
	actor, err := h.reauthorizeCommittingActorTx(r.Context(), tx, requirement)
	if err != nil || actor.TargetOrgID != orgID {
		return 0, errCommittingActorAuthorizationChanged
	}
	return actor.UserID, nil
}

func (h *Handler) CreateXInsightPolicyDelegation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	orgID, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
	if !ok {
		return
	}
	input, ok := decodePolicyDelegation(w, r)
	if !ok {
		return
	}
	projectID, userID, expires, ok := input.scope()
	if !ok || input.ExpectedRevision != "" {
		writeJSON(w, 400, map[string]any{"error": "Invalid delegation scope"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	actorID, err := h.authorizePolicyDelegationAdminTx(r, tx, orgID, projectID, userID, 0, input.Coverage, input.TargetKind)
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Delegation denied"})
		return
	}
	var anchor int64
	err = tx.QueryRow(r.Context(), `SELECT project_id FROM xinsight_connector_installations
 WHERE installation_id=$1 AND org_id=$2 AND desired_state='active' AND observed_state<>'disconnected' FOR UPDATE`, input.InstallationID, orgID).Scan(&anchor)
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "Installation unavailable"})
		return
	}
	var id int64
	err = tx.QueryRow(r.Context(), `INSERT INTO xinsight_policy_delegations
 (installation_id,org_id,anchor_project_id,target_kind,coverage,exact_project_id,exact_user_id,can_read,can_write,grantor_user_id,expires_at)
 VALUES($1,$2,$3,$4,$5,NULLIF($6,0),NULLIF($7,0),$8,$9,$10,$11) RETURNING id`, input.InstallationID, orgID, anchor, input.TargetKind, input.Coverage, projectID, userID, input.Read, input.Write, actorID, expires).Scan(&id)
	if err == nil {
		err = insertAuditEntry(r.Context(), tx, "content.policy.delegation.create", "xinsight_policy_delegation", strconv.FormatInt(id, 10), map[string]any{"installation_id": input.InstallationID, "target_kind": input.TargetKind, "coverage": input.Coverage, "project_id": input.ProjectID, "user_id": input.UserID, "read": input.Read, "write": input.Write}, requestAuditMeta(r, orgID, projectID))
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Delegation create failed"})
		return
	}
	writeJSON(w, 201, map[string]any{"delegation_id": "xipg_" + strconv.FormatInt(id, 10), "revision": "1", "state": "active"})
}

func (h *Handler) ListXInsightPolicyDelegations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	orgID, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
	if !ok {
		return
	}
	installationID := r.URL.Query().Get("installation_id")
	if installationID == "" {
		writeJSON(w, 400, map[string]any{"error": "installation_id required"})
		return
	}
	projectFilter, projectOK := positiveIDOrZero(r.URL.Query().Get("project_id"))
	if !projectOK {
		writeJSON(w, 400, map[string]any{"error": "Invalid project_id"})
		return
	}
	// This management view is restricted to live organization administrators.
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	if projectFilter > 0 {
		_, err = h.authorizePolicyDelegationAdminTx(r, tx, orgID, projectFilter, 0, 0, "exact", "project_policy")
	} else {
		_, err = h.reauthorizeCommittingActorTx(r.Context(), tx, committingActorRequirement{TargetOrgID: orgID,
			AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin}})
	}
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Delegation denied"})
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT id,target_kind,coverage,COALESCE(exact_project_id,0),COALESCE(exact_user_id,0),
 can_read,can_write,grantor_user_id,revision,state FROM xinsight_policy_delegations
 WHERE org_id=$1 AND installation_id=$2 AND ($3::bigint=0 OR exact_project_id=$3) ORDER BY id`, orgID, installationID, projectFilter)
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Delegation read failed"})
		return
	}
	defer rows.Close()
	data := []map[string]any{}
	for rows.Next() {
		var id, projectID, userID, grantor, revision int64
		var kind, coverage, state string
		var read, write bool
		if err = rows.Scan(&id, &kind, &coverage, &projectID, &userID, &read, &write, &grantor, &revision, &state); err != nil {
			break
		}
		data = append(data, map[string]any{"delegation_id": "xipg_" + strconv.FormatInt(id, 10), "target_kind": kind, "coverage": coverage,
			"project_id": strconv.FormatInt(projectID, 10), "user_id": strconv.FormatInt(userID, 10), "read": read, "write": write,
			"grantor_user_id": strconv.FormatInt(grantor, 10), "revision": strconv.FormatInt(revision, 10), "state": state})
	}
	if err == nil {
		err = rows.Err()
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Delegation read failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"data": data})
}

func policyDelegationPathID(raw string) (int64, bool) {
	if !strings.HasPrefix(raw, "xipg_") {
		return 0, false
	}
	id, ok := positiveIDOrZero(strings.TrimPrefix(raw, "xipg_"))
	return id, ok && id > 0
}

func (h *Handler) changeXInsightPolicyDelegation(w http.ResponseWriter, r *http.Request, revoke bool) {
	w.Header().Set("Cache-Control", "no-store")
	orgID, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
	if !ok {
		return
	}
	id, ok := policyDelegationPathID(r.PathValue("delegation_id"))
	if !ok {
		writeJSON(w, 400, map[string]any{"error": "Invalid delegation ID"})
		return
	}
	input, ok := decodePolicyDelegation(w, r)
	if !ok {
		return
	}
	expected, ok := positiveIDOrZero(input.ExpectedRevision)
	if !ok || expected == 0 {
		writeJSON(w, 400, map[string]any{"error": "expected_revision required"})
		return
	}
	var requestedProject, requestedUser int64
	var expires *time.Time
	if !revoke {
		requestedProject, requestedUser, expires, ok = input.scope()
		if !ok {
			writeJSON(w, 400, map[string]any{"error": "Invalid delegation scope"})
			return
		}
	}
	// Pre-read identifies the principal lane; it is never an authorization decision.
	var old policyDelegationRecord
	err := h.db.QueryRow(r.Context(), `SELECT installation_id,org_id,anchor_project_id,target_kind,coverage,
 COALESCE(exact_project_id,0),COALESCE(exact_user_id,0),grantor_user_id,revision,state
 FROM xinsight_policy_delegations WHERE id=$1 AND org_id=$2`, id, orgID).Scan(&old.InstallationID, &old.OrgID,
		&old.AnchorProject, &old.TargetKind, &old.Coverage, &old.ProjectID, &old.UserID, &old.GrantorID, &old.Revision, &old.State)
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "Delegation not found"})
		return
	}
	if !revoke && (input.InstallationID != old.InstallationID || input.TargetKind != old.TargetKind ||
		input.Coverage != old.Coverage || requestedProject != old.ProjectID || requestedUser != old.UserID) {
		writeJSON(w, 409, map[string]any{"error": "Scope identity cannot change; revoke and create a new delegation"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	actorID, err := h.authorizePolicyDelegationAdminTx(r, tx, orgID, old.ProjectID, old.UserID, old.GrantorID, old.Coverage, old.TargetKind)
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Delegation denied"})
		return
	}
	var anchor int64
	err = tx.QueryRow(r.Context(), `SELECT project_id FROM xinsight_connector_installations WHERE installation_id=$1 AND org_id=$2 FOR UPDATE`, old.InstallationID, orgID).Scan(&anchor)
	if err != nil || anchor != old.AnchorProject {
		writeJSON(w, 409, map[string]any{"error": "Installation changed"})
		return
	}
	err = tx.QueryRow(r.Context(), `SELECT revision,state,grantor_user_id FROM xinsight_policy_delegations WHERE id=$1 AND org_id=$2 FOR UPDATE`, id, orgID).Scan(&old.Revision, &old.State, &old.GrantorID)
	if err != nil || old.Revision != expected || old.State != "active" {
		writeJSON(w, 409, map[string]any{"error": "Delegation changed"})
		return
	}
	if revoke {
		_, err = tx.Exec(r.Context(), `UPDATE xinsight_policy_delegations SET state='revoked',revision=revision+1,updated_at=NOW() WHERE id=$1`, id)
	} else {
		_, err = tx.Exec(r.Context(), `UPDATE xinsight_policy_delegations SET can_read=$2,can_write=$3,expires_at=$4,
 revision=revision+1,updated_at=NOW() WHERE id=$1`, id, input.Read, input.Write, expires)
	}
	if err == nil {
		action := "content.policy.delegation.update"
		if revoke {
			action = "content.policy.delegation.revoke"
		}
		err = insertAuditEntry(r.Context(), tx, action, "xinsight_policy_delegation", strconv.FormatInt(id, 10),
			map[string]any{"actor_user_id": actorID, "previous_revision": expected, "revoke": revoke}, requestAuditMeta(r, orgID, old.ProjectID))
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Delegation change failed"})
		return
	}
	state := "active"
	if revoke {
		state = "revoked"
	}
	writeJSON(w, 200, map[string]any{"delegation_id": "xipg_" + strconv.FormatInt(id, 10), "revision": strconv.FormatInt(expected+1, 10), "state": state})
}

func (h *Handler) UpdateXInsightPolicyDelegation(w http.ResponseWriter, r *http.Request) {
	h.changeXInsightPolicyDelegation(w, r, false)
}

func (h *Handler) RevokeXInsightPolicyDelegation(w http.ResponseWriter, r *http.Request) {
	h.changeXInsightPolicyDelegation(w, r, true)
}
