package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

type sourceEnrollmentInput struct {
	InstallationID   string   `json:"installation_id"`
	Coverage         string   `json:"coverage"`
	ProjectID        string   `json:"project_id,omitempty"`
	Profiles         []string `json:"profiles"`
	IncludeContent   bool     `json:"include_content"`
	ExpiresAt        string   `json:"expires_at,omitempty"`
	ExpectedRevision string   `json:"expected_revision,omitempty"`
}

func decodeSourceEnrollment(w http.ResponseWriter, r *http.Request) (sourceEnrollmentInput, bool) {
	var input sourceEnrollmentInput
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
			allowed := map[string]bool{"installation_id": true, "coverage": true, "project_id": true,
				"profiles": true, "include_content": true, "expires_at": true, "expected_revision": true}
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
		writeJSON(w, 400, map[string]any{"error": "Invalid source enrollment JSON"})
		return input, false
	}
	return input, true
}

func (input sourceEnrollmentInput) scope() (int64, *time.Time, bool) {
	projectID, ok := positiveIDOrZero(input.ProjectID)
	if !ok || input.InstallationID == "" ||
		!(input.Coverage == "exact" && projectID > 0 || input.Coverage == "all_current_future" && projectID == 0) ||
		len(input.Profiles) == 0 && !input.IncludeContent {
		return 0, nil, false
	}
	if len(input.Profiles) > 0 {
		if !sourcefacts.ValidateProfiles(input.Profiles) {
			return 0, nil, false
		}
		for _, profile := range input.Profiles {
			if !slices.Contains(sourcefacts.Profiles(), profile) {
				return 0, nil, false
			}
		}
	}
	var expiry *time.Time
	if input.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, input.ExpiresAt)
		if err != nil || !parsed.After(time.Now()) {
			return 0, nil, false
		}
		expiry = &parsed
	}
	return projectID, expiry, true
}

func (h *Handler) CreateXInsightSourceEnrollment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	orgID, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
	if !ok {
		return
	}
	input, ok := decodeSourceEnrollment(w, r)
	if !ok {
		return
	}
	projectID, expiry, ok := input.scope()
	if !ok || input.ExpectedRevision != "" {
		writeJSON(w, 400, map[string]any{"error": "Invalid source enrollment scope"})
		return
	}
	if h.sourceFactsController == nil || !h.sourceFactsController.Enabled() ||
		len(input.Profiles) > 0 && !h.sourceFactsController.ProfilesEnabled(input.Profiles) {
		writeJSON(w, 503, map[string]any{"error": "Source facts capture unavailable"})
		return
	}
	if input.IncludeContent {
		if enabled, ok := h.sourceFactsController.(interface{ ContentEnabled() bool }); !ok || !enabled.ContentEnabled() {
			writeJSON(w, 503, map[string]any{"error": "Content capture unavailable"})
			return
		}
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	actorID, err := h.authorizePolicyDelegationAdminTx(r, tx, orgID, projectID, 0, 0, input.Coverage, "project_policy")
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Source enrollment denied"})
		return
	}
	var anchor int64
	var source string
	err = tx.QueryRow(r.Context(), `SELECT project_id,source_instance FROM xinsight_connector_installations
 WHERE installation_id=$1 AND org_id=$2 AND desired_state='active' AND observed_state<>'disconnected' FOR UPDATE`,
		input.InstallationID, orgID).Scan(&anchor, &source)
	if err != nil || source != h.sourceFactsController.SourceInstance() {
		writeJSON(w, 409, map[string]any{"error": "Installation unavailable"})
		return
	}
	var id int64
	err = tx.QueryRow(r.Context(), `INSERT INTO xinsight_source_enrollments
 (installation_id,org_id,anchor_project_id,coverage,exact_project_id,profiles,include_content,grantor_user_id,expires_at)
 VALUES($1,$2,$3,$4,NULLIF($5,0),$6,$7,$8,$9) RETURNING id`, input.InstallationID, orgID, anchor,
		input.Coverage, projectID, input.Profiles, input.IncludeContent, actorID, expiry).Scan(&id)
	if err == nil {
		err = insertAuditEntry(r.Context(), tx, "data_exchange.xinsight.source_enrollment.create", "xinsight_source_enrollment",
			strconv.FormatInt(id, 10), map[string]any{"installation_id": input.InstallationID, "coverage": input.Coverage,
				"project_id": input.ProjectID, "profiles": input.Profiles, "include_content": input.IncludeContent}, requestAuditMeta(r, orgID, projectID))
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Source enrollment create failed"})
		return
	}
	writeJSON(w, 201, map[string]any{"enrollment_id": "xise_" + strconv.FormatInt(id, 10), "revision": "1", "state": "active"})
}

func (h *Handler) ListXInsightSourceEnrollments(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	orgID, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
	if !ok {
		return
	}
	installation := r.URL.Query().Get("installation_id")
	if installation == "" {
		writeJSON(w, 400, map[string]any{"error": "installation_id required"})
		return
	}
	projectFilter, projectOK := positiveIDOrZero(r.URL.Query().Get("project_id"))
	if !projectOK {
		writeJSON(w, 400, map[string]any{"error": "Invalid project_id"})
		return
	}
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
		writeJSON(w, 403, map[string]any{"error": "Source enrollment denied"})
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT id,coverage,COALESCE(exact_project_id,0),profiles,include_content,
 grantor_user_id,revision,state,expires_at FROM xinsight_source_enrollments
 WHERE org_id=$1 AND installation_id=$2 AND ($3::bigint=0 OR exact_project_id=$3) ORDER BY id`, orgID, installation, projectFilter)
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Source enrollment read failed"})
		return
	}
	data := []map[string]any{}
	for rows.Next() {
		var id, projectID, grantor, revision int64
		var coverage, state string
		var profiles []string
		var content bool
		var expiry *time.Time
		if err = rows.Scan(&id, &coverage, &projectID, &profiles, &content, &grantor, &revision, &state, &expiry); err != nil {
			break
		}
		data = append(data, map[string]any{"enrollment_id": "xise_" + strconv.FormatInt(id, 10), "coverage": coverage,
			"project_id": idOrEmpty(projectID), "profiles": profiles, "include_content": content,
			"grantor_user_id": strconv.FormatInt(grantor, 10), "revision": strconv.FormatInt(revision, 10), "state": state,
			"expires_at": expiry})
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Source enrollment read failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"data": data})
}

func sourceEnrollmentPathID(raw string) (int64, bool) {
	if !strings.HasPrefix(raw, "xise_") {
		return 0, false
	}
	id, ok := positiveIDOrZero(strings.TrimPrefix(raw, "xise_"))
	return id, ok && id > 0
}

func (h *Handler) RevokeXInsightSourceEnrollment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	orgID, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
	if !ok {
		return
	}
	id, ok := sourceEnrollmentPathID(r.PathValue("enrollment_id"))
	if !ok {
		writeJSON(w, 400, map[string]any{"error": "Invalid enrollment ID"})
		return
	}
	input, ok := decodeSourceEnrollment(w, r)
	if !ok {
		return
	}
	expected, ok := positiveIDOrZero(input.ExpectedRevision)
	if !ok || expected == 0 {
		writeJSON(w, 400, map[string]any{"error": "expected_revision required"})
		return
	}
	var grantor, projectID int64
	var coverage, installation string
	err := h.db.QueryRow(r.Context(), `SELECT grantor_user_id,COALESCE(exact_project_id,0),coverage,installation_id
 FROM xinsight_source_enrollments WHERE id=$1 AND org_id=$2`, id, orgID).Scan(&grantor, &projectID, &coverage, &installation)
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "Enrollment not found"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	_, err = h.authorizePolicyDelegationAdminTx(r, tx, orgID, projectID, 0, grantor, coverage, "project_policy")
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Enrollment revoke denied"})
		return
	}
	var revision int64
	var state string
	err = tx.QueryRow(r.Context(), `SELECT revision,state FROM xinsight_source_enrollments
 WHERE id=$1 AND org_id=$2 AND installation_id=$3 FOR UPDATE`, id, orgID, installation).Scan(&revision, &state)
	if err != nil || revision != expected || state != "active" {
		writeJSON(w, 409, map[string]any{"error": "Enrollment changed"})
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE xinsight_source_enrollments SET state='revoked',revision=revision+1,updated_at=NOW() WHERE id=$1`, id)
	if err == nil {
		err = insertAuditEntry(r.Context(), tx, "data_exchange.xinsight.source_enrollment.revoke", "xinsight_source_enrollment",
			strconv.FormatInt(id, 10), map[string]any{"installation_id": installation, "previous_revision": expected}, requestAuditMeta(r, orgID, projectID))
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Enrollment revoke failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"enrollment_id": "xise_" + strconv.FormatInt(id, 10), "revision": strconv.FormatInt(expected+1, 10), "state": "revoked"})
}

type sourceEnrollmentExclusionInput struct {
	Excluded         *bool  `json:"excluded"`
	ExpectedRevision string `json:"expected_revision"`
}

func (h *Handler) PutXInsightSourceEnrollmentExclusion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	orgID, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
	if !ok {
		return
	}
	enrollmentID, ok := sourceEnrollmentPathID(r.PathValue("enrollment_id"))
	if !ok {
		writeJSON(w, 400, map[string]any{"error": "Invalid enrollment ID"})
		return
	}
	projectID, ok := parsePositivePathID(w, r, "project_id", "Invalid project ID")
	if !ok {
		return
	}
	wire, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err == nil {
		if strictErr := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); strictErr != nil {
			err = strictErr
		}
	}
	var fields map[string]json.RawMessage
	if err == nil {
		if json.Unmarshal(wire, &fields) != nil || len(fields) != 2 || fields["excluded"] == nil || fields["expected_revision"] == nil {
			err = errors.New("invalid exclusion fields")
		}
	}
	var input sourceEnrollmentExclusionInput
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(wire))
		dec.DisallowUnknownFields()
		err = dec.Decode(&input)
	}
	expected, revisionOK := positiveIDOrZero(input.ExpectedRevision)
	if input.ExpectedRevision == "0" {
		expected, revisionOK = 0, true
	}
	if err != nil || !revisionOK || input.ExpectedRevision == "" || input.Excluded == nil {
		writeJSON(w, 400, map[string]any{"error": "Invalid exclusion request"})
		return
	}
	var grantor int64
	var coverage, installation string
	err = h.db.QueryRow(r.Context(), `SELECT grantor_user_id,coverage,installation_id FROM xinsight_source_enrollments
 WHERE id=$1 AND org_id=$2`, enrollmentID, orgID).Scan(&grantor, &coverage, &installation)
	if err != nil || coverage != "all_current_future" {
		writeJSON(w, 404, map[string]any{"error": "Enrollment not found"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	actorID, err := h.authorizePolicyDelegationAdminTx(r, tx, orgID, projectID, 0, grantor, "exact", "project_policy")
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Enrollment exclusion denied"})
		return
	}
	var currentGrantor int64
	var state string
	err = tx.QueryRow(r.Context(), `SELECT grantor_user_id,state FROM xinsight_source_enrollments
 WHERE id=$1 AND org_id=$2 AND installation_id=$3 AND coverage='all_current_future' FOR UPDATE`,
		enrollmentID, orgID, installation).Scan(&currentGrantor, &state)
	if err != nil || currentGrantor != grantor || state != "active" {
		writeJSON(w, 409, map[string]any{"error": "Enrollment changed"})
		return
	}
	var prior int64
	err = tx.QueryRow(r.Context(), `SELECT revision FROM xinsight_source_enrollment_exclusions
 WHERE enrollment_id=$1 AND project_id=$2 FOR UPDATE`, enrollmentID, projectID).Scan(&prior)
	if errors.Is(err, pgx.ErrNoRows) {
		prior = 0
	} else if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Exclusion read failed"})
		return
	}
	if prior != expected {
		writeJSON(w, 409, map[string]any{"error": "Exclusion changed"})
		return
	}
	if prior == 0 {
		_, err = tx.Exec(r.Context(), `INSERT INTO xinsight_source_enrollment_exclusions
 (enrollment_id,org_id,project_id,excluded,updated_by) VALUES($1,$2,$3,$4,$5)`,
			enrollmentID, orgID, projectID, *input.Excluded, actorID)
	} else {
		_, err = tx.Exec(r.Context(), `UPDATE xinsight_source_enrollment_exclusions
 SET excluded=$3,revision=revision+1,updated_by=$4,updated_at=NOW()
 WHERE enrollment_id=$1 AND project_id=$2`, enrollmentID, projectID, *input.Excluded, actorID)
	}
	if err == nil {
		err = insertAuditEntry(r.Context(), tx, "data_exchange.xinsight.source_enrollment.exclusion",
			"xinsight_source_enrollment", strconv.FormatInt(enrollmentID, 10),
			map[string]any{"project_id": strconv.FormatInt(projectID, 10), "excluded": *input.Excluded,
				"previous_revision": expected}, requestAuditMeta(r, orgID, projectID))
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Exclusion update failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"enrollment_id": "xise_" + strconv.FormatInt(enrollmentID, 10),
		"project_id": strconv.FormatInt(projectID, 10), "excluded": *input.Excluded,
		"revision": strconv.FormatInt(expected+1, 10)})
}
