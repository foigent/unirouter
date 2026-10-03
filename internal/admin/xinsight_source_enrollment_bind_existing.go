package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

type bindExistingInput struct {
	SchemaVersion       string `json:"schema_version"`
	Kind                string `json:"kind"`
	CredentialID        string `json:"credential_id"`
	ExpectedStateSHA256 string `json:"expected_state_sha256"`
	OperationID         string `json:"operation_id"`
}

type bindExistingKey struct {
	CredentialID     string   `json:"credential_id"`
	Status           string   `json:"status"`
	KeyPrefix        string   `json:"key_prefix"`
	AllowedIP        string   `json:"allowed_ip"`
	VerifierScheme   string   `json:"verifier_scheme"`
	RegistrationID   string   `json:"registration_operation_id"`
	RegistrationHash string   `json:"registration_sha256"`
	Profiles         []string `json:"profiles"`
	PolicyLabels     string   `json:"policy_labels"`
	CoverageSequence string   `json:"coverage_after_sequence,omitempty"`
	CoverageTime     string   `json:"coverage_time,omitempty"`
	AllowedPurposes  []string `json:"allowed_purposes,omitempty"`
}

type bindExistingCandidate struct {
	SchemaVersion        string            `json:"schema_version"`
	InstallationID       string            `json:"installation_id"`
	XDeploymentID        string            `json:"x_deployment_id"`
	SourceInstance       string            `json:"source_instance"`
	OrgID                string            `json:"org_id"`
	ProjectID            string            `json:"project_id"`
	EnrollmentID         string            `json:"enrollment_id"`
	EnrollmentRevision   string            `json:"enrollment_revision"`
	Kind                 string            `json:"kind"`
	ConnectionStatus     string            `json:"connection_status"`
	ScopeGeneration      string            `json:"scope_generation"`
	ContentSafetyVersion string            `json:"content_safety_version"`
	ConnectionProfiles   []string          `json:"connection_profiles"`
	ContentStatus        string            `json:"content_status,omitempty"`
	ContentStateVersion  string            `json:"content_state_version,omitempty"`
	ContentGeneration    string            `json:"content_access_generation,omitempty"`
	Keys                 []bindExistingKey `json:"keys"`
	StateSHA256          string            `json:"state_sha256"`
}

func decodeBindExistingInput(w http.ResponseWriter, r *http.Request) (bindExistingInput, bool) {
	var input bindExistingInput
	wire, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err == nil {
		if strictErr := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); strictErr != nil {
			err = strictErr
		}
	}
	if err == nil {
		var fields map[string]json.RawMessage
		if json.Unmarshal(wire, &fields) != nil || len(fields) != 5 {
			err = errors.New("invalid bind fields")
		} else {
			for _, field := range []string{"schema_version", "kind", "credential_id", "expected_state_sha256", "operation_id"} {
				if fields[field] == nil {
					err = errors.New("missing bind field")
				}
			}
		}
	}
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(wire))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&input)
		if err == nil && !errors.Is(decoder.Decode(new(any)), io.EOF) {
			err = errors.New("trailing bind JSON")
		}
	}
	credentialID, credentialOK := positiveIDOrZero(input.CredentialID)
	if err != nil || input.SchemaVersion != "unirouter.xinsight.bind-existing.v1" ||
		(input.Kind != "facts" && input.Kind != "content") || !credentialOK || credentialID == 0 ||
		!machineOperationIDPattern.MatchString(input.OperationID) ||
		len(input.ExpectedStateSHA256) != 71 || input.ExpectedStateSHA256[:7] != "sha256:" {
		writeJSON(w, 400, map[string]any{"error": "Invalid bind-existing request"})
		return input, false
	}
	for _, ch := range input.ExpectedStateSHA256[7:] {
		if ch < '0' || ch > '9' && ch < 'a' || ch > 'f' {
			writeJSON(w, 400, map[string]any{"error": "Invalid state digest"})
			return input, false
		}
	}
	return input, true
}

func bindDigest(value any) string {
	wire, _ := json.Marshal(value)
	digest := sha256.Sum256(wire)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (h *Handler) bindExistingAuthorityTx(ctx context.Context, tx pgx.Tx, orgID, projectID, enrollmentID, expectedGrantor int64) (bindExistingCandidate, []string, bool, error) {
	var candidate bindExistingCandidate
	var grantor, exactProject, anchor int64
	var coverage, installation string
	err := tx.QueryRow(ctx, `SELECT grantor_user_id,COALESCE(exact_project_id,0),coverage,installation_id,anchor_project_id
 FROM xinsight_source_enrollments WHERE id=$1 AND org_id=$2`, enrollmentID, orgID).
		Scan(&grantor, &exactProject, &coverage, &installation, &anchor)
	if err != nil {
		return candidate, nil, false, errCommittingActorScopeNotFound
	}
	// The locator grants nothing. The caller locks actor and grantor user lanes,
	// organization and target project before rereading this identity.
	var grantorOrg int64
	var grantorRole, grantorStatus string
	err = tx.QueryRow(ctx, `SELECT org_id,organization_role,status FROM users WHERE id=$1 FOR UPDATE`, grantor).
		Scan(&grantorOrg, &grantorRole, &grantorStatus)
	if err != nil || grantor != expectedGrantor || grantorStatus != "active" {
		return candidate, nil, false, errCommittingActorAuthorizationChanged
	}
	expectedInstallation := installation
	expectedAnchor := anchor
	var source, deployment, desired, observed, anchorStatus string
	var lockedAnchor, lockedOrg int64
	err = tx.QueryRow(ctx, `SELECT i.org_id,i.project_id,i.source_instance,i.x_deployment_id,i.desired_state,i.observed_state,p.status
	FROM xinsight_connector_installations i JOIN projects p ON p.id=i.project_id AND p.org_id=i.org_id
	WHERE i.installation_id=$1 FOR UPDATE OF i`, installation).
		Scan(&lockedOrg, &lockedAnchor, &source, &deployment, &desired, &observed, &anchorStatus)
	if err != nil || lockedOrg != orgID || lockedAnchor != anchor || desired != "active" || observed == "disconnected" ||
		anchorStatus != "active" ||
		h.sourceFactsController == nil || source != h.sourceFactsController.SourceInstance() {
		return candidate, nil, false, errCommittingActorAuthorizationChanged
	}
	var revision int64
	var state string
	var profiles []string
	var includeContent bool
	var expires *time.Time
	err = tx.QueryRow(ctx, `SELECT grantor_user_id,COALESCE(exact_project_id,0),coverage,installation_id,anchor_project_id,
 revision,state,profiles,include_content,expires_at FROM xinsight_source_enrollments
 WHERE id=$1 AND org_id=$2 FOR UPDATE`, enrollmentID, orgID).
		Scan(&grantor, &exactProject, &coverage, &installation, &anchor, &revision, &state, &profiles, &includeContent, &expires)
	if err != nil || grantor != expectedGrantor || installation != expectedInstallation || anchor != expectedAnchor || state != "active" || expires != nil && !expires.After(time.Now()) ||
		!(coverage == "all_current_future" || coverage == "exact" && exactProject == projectID) {
		return candidate, nil, false, errCommittingActorAuthorizationChanged
	}
	if grantorRole != auth.RolePlatformAdmin && (grantorRole != auth.RoleOrgAdmin || grantorOrg != orgID) {
		if coverage != "exact" || exactProject != projectID || grantorOrg != orgID {
			return candidate, nil, false, errCommittingActorAuthorizationChanged
		}
		var membership string
		err = tx.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2 FOR SHARE`, projectID, grantor).Scan(&membership)
		if err != nil || membership != projectMemberAdmin {
			return candidate, nil, false, errCommittingActorAuthorizationChanged
		}
	}
	var excluded bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xinsight_source_enrollment_exclusions
 WHERE enrollment_id=$1 AND project_id=$2 AND excluded)`, enrollmentID, projectID).Scan(&excluded)
	if err != nil || excluded {
		return candidate, nil, false, errCommittingActorAuthorizationChanged
	}
	candidate = bindExistingCandidate{SchemaVersion: "unirouter.xinsight.bind-existing-candidate.v1",
		InstallationID: installation, XDeploymentID: deployment, SourceInstance: source,
		OrgID: strconv.FormatInt(orgID, 10), ProjectID: strconv.FormatInt(projectID, 10),
		EnrollmentID: "xise_" + strconv.FormatInt(enrollmentID, 10), EnrollmentRevision: strconv.FormatInt(revision, 10)}
	return candidate, profiles, includeContent, nil
}

func readBindExistingCandidateTx(ctx context.Context, tx pgx.Tx, candidate bindExistingCandidate) (bindExistingCandidate, error) {
	orgID, _ := strconv.ParseInt(candidate.OrgID, 10, 64)
	projectID, _ := strconv.ParseInt(candidate.ProjectID, 10, 64)
	var connectionSource string
	var generation, safety int64
	err := tx.QueryRow(ctx, `SELECT source_instance,status,scope_generation,content_safety_version,profiles
 FROM execution_source_connections WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, orgID, projectID).
		Scan(&connectionSource, &candidate.ConnectionStatus, &generation, &safety, &candidate.ConnectionProfiles)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return candidate, errXInsightFeedConflict
		}
		return candidate, err
	}
	if connectionSource != candidate.SourceInstance || candidate.ConnectionStatus != "active" {
		return candidate, errXInsightFeedConflict
	}
	candidate.ScopeGeneration = strconv.FormatInt(generation, 10)
	candidate.ContentSafetyVersion = strconv.FormatInt(safety, 10)
	if candidate.Kind == "content" {
		var version, access int64
		err = tx.QueryRow(ctx, `SELECT status,state_version,access_generation FROM execution_source_content_scopes
 WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, orgID, projectID).
			Scan(&candidate.ContentStatus, &version, &access)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return candidate, errXInsightFeedConflict
			}
			return candidate, err
		}
		if candidate.ContentStatus != "active" {
			return candidate, errXInsightFeedConflict
		}
		candidate.ContentStateVersion = strconv.FormatInt(version, 10)
		candidate.ContentGeneration = strconv.FormatInt(access, 10)
	}
	rows, err := tx.Query(ctx, `SELECT ak.id,ak.status,ak.key_prefix,ak.allowed_ips,ak.verifier_scheme,
 ak.registration_operation_id,ak.policy_labels,sc.registration_sha256,sc.profiles,
 COALESCE(cb.coverage_after_sequence,0),cb.coverage_time,cb.allowed_purposes
 FROM api_keys ak JOIN execution_source_credentials sc ON sc.api_key_id=ak.id
 LEFT JOIN execution_source_content_bindings cb ON cb.api_key_id=ak.id
 WHERE ak.org_id=$1 AND ak.project_id=$2 AND ak.purpose=$3 AND ak.source_lifecycle_scope=$4
 AND ak.status IN ('feed_active','feed_staged') ORDER BY ak.id FOR UPDATE OF ak,sc`,
		orgID, projectID, sourcefacts.Purpose, map[bool]string{true: "content", false: "legacy"}[candidate.Kind == "content"])
	if err != nil {
		return candidate, err
	}
	candidate.Keys = []bindExistingKey{}
	for rows.Next() {
		var key bindExistingKey
		var id, after int64
		var at *time.Time
		if err = rows.Scan(&id, &key.Status, &key.KeyPrefix, &key.AllowedIP, &key.VerifierScheme,
			&key.RegistrationID, &key.PolicyLabels, &key.RegistrationHash, &key.Profiles,
			&after, &at, &key.AllowedPurposes); err != nil {
			break
		}
		key.CredentialID = strconv.FormatInt(id, 10)
		if candidate.Kind == "content" {
			if at == nil || !slices.Equal(key.AllowedPurposes, []string{"human_review"}) {
				err = errXInsightFeedConflict
				break
			}
			key.CoverageSequence = strconv.FormatInt(after, 10)
			key.CoverageTime = at.UTC().Format(time.RFC3339Nano)
		}
		candidate.Keys = append(candidate.Keys, key)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return candidate, err
	}
	candidate.StateSHA256 = bindDigest(struct {
		bindExistingCandidate
	}{candidate})
	return candidate, nil
}

func (h *Handler) bindExistingContext(w http.ResponseWriter, r *http.Request) (pgx.Tx, bindExistingCandidate, []string, bool, bool) {
	var zero bindExistingCandidate
	orgID, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
	if !ok {
		return nil, zero, nil, false, false
	}
	projectID, ok := parsePositivePathID(w, r, "project_id", "Invalid project ID")
	if !ok {
		return nil, zero, nil, false, false
	}
	enrollmentID, ok := sourceEnrollmentPathID(r.PathValue("enrollment_id"))
	if !ok {
		writeJSON(w, 400, map[string]any{"error": "Invalid enrollment ID"})
		return nil, zero, nil, false, false
	}
	var grantor int64
	err := h.db.QueryRow(r.Context(), `SELECT grantor_user_id FROM xinsight_source_enrollments WHERE id=$1 AND org_id=$2`, enrollmentID, orgID).Scan(&grantor)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, 404, map[string]any{"error": "Enrollment unavailable"})
		} else {
			writeJSON(w, 503, map[string]any{"error": "Enrollment read failed"})
		}
		return nil, zero, nil, false, false
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
		return nil, zero, nil, false, false
	}
	_, err = h.authorizePolicyDelegationAdminTx(r, tx, orgID, projectID, 0, grantor, "exact", "project_policy")
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Bind-existing denied"})
		tx.Rollback(r.Context())
		return nil, zero, nil, false, false
	}
	candidate, profiles, content, err := h.bindExistingAuthorityTx(r.Context(), tx, orgID, projectID, enrollmentID, grantor)
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "Enrollment or installation changed"})
		tx.Rollback(r.Context())
		return nil, zero, nil, false, false
	}
	return tx, candidate, profiles, content, true
}

func (h *Handler) GetXInsightBindExistingCandidates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	tx, candidate, profiles, content, ok := h.bindExistingContext(w, r)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	data := []bindExistingCandidate{}
	for _, kind := range []string{"facts", "content"} {
		if kind == "facts" && len(profiles) == 0 || kind == "content" && !content {
			continue
		}
		copy := candidate
		copy.Kind = kind
		candidateWithState, err := readBindExistingCandidateTx(r.Context(), tx, copy)
		if err != nil {
			if errors.Is(err, errXInsightFeedConflict) {
				continue
			}
			writeJSON(w, 503, map[string]any{"error": "Candidate read failed"})
			return
		}
		data = append(data, candidateWithState)
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, 503, map[string]any{"error": "Candidate read failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"data": data})
}

func (h *Handler) BindExistingXInsightSourceEnrollment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	input, ok := decodeBindExistingInput(w, r)
	if !ok {
		return
	}
	tx, candidate, profiles, content, ok := h.bindExistingContext(w, r)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	org, _ := strconv.ParseInt(candidate.OrgID, 10, 64)
	project, _ := strconv.ParseInt(candidate.ProjectID, 10, 64)
	replayed, err := bindExistingXInsightSourceEnrollmentTx(r.Context(), tx, candidate, profiles, content, input, middleware.GetUserIDFromContext(r.Context()), requestAuditMeta(r, org, project))
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		code := 503
		if errors.Is(err, errCommittingActorAuthorizationChanged) {
			code = 403
		}
		if errors.Is(err, errXInsightFeedConflict) {
			code = 409
		}
		writeJSON(w, code, map[string]any{"error": "Bind-existing qualification failed"})
		return
	}
	result := map[string]any{"enrollment_id": candidate.EnrollmentID, "project_id": candidate.ProjectID, "kind": input.Kind, "credential_id": input.CredentialID, "operation_id": input.OperationID, "status": "bound"}
	if replayed {
		result["replayed"] = true
	}
	writeJSON(w, 200, result)
}

// Caller owns original live principal, installation and enrollment authority.
// This function preserves the original precise CAS, replay, writer and audit.
func bindExistingXInsightSourceEnrollmentTx(ctx context.Context, tx pgx.Tx, candidate bindExistingCandidate, profiles []string, content bool, input bindExistingInput, actorID int64, meta auditMeta) (bool, error) {
	if input.Kind == "facts" && len(profiles) == 0 || input.Kind == "content" && !content {
		return false, errCommittingActorAuthorizationChanged
	}
	candidate.Kind = input.Kind
	requestDigest := bindDigest(struct {
		InstallationID string            `json:"installation_id"`
		EnrollmentID   string            `json:"enrollment_id"`
		ProjectID      string            `json:"project_id"`
		Request        bindExistingInput `json:"request"`
	}{candidate.InstallationID, candidate.EnrollmentID, candidate.ProjectID, input})
	enrollmentID, _ := sourceEnrollmentPathID(candidate.EnrollmentID)
	var existingKey int64
	var priorDigest string
	err := tx.QueryRow(ctx, `SELECT api_key_id,bind_request_sha256 FROM xinsight_source_enrollment_bindings WHERE enrollment_id=$1 AND bind_operation_id=$2 FOR UPDATE`, enrollmentID, input.OperationID).Scan(&existingKey, &priorDigest)
	if err == nil {
		if priorDigest != requestDigest || input.CredentialID != strconv.FormatInt(existingKey, 10) {
			return false, errXInsightFeedConflict
		}
		return true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	current, err := readBindExistingCandidateTx(ctx, tx, candidate)
	if err != nil {
		return false, err
	}
	if current.StateSHA256 != input.ExpectedStateSHA256 || input.Kind == "facts" && !slices.Equal(current.ConnectionProfiles, profiles) {
		return false, errXInsightFeedConflict
	}
	if len(current.Keys) != 1 || current.Keys[0].CredentialID != input.CredentialID || current.Keys[0].Status != "feed_active" || current.Keys[0].PolicyLabels != "" || current.Keys[0].VerifierScheme != auth.ReceiverFeedVerifierScheme || !exactHostNetworkBinding(current.Keys[0].AllowedIP) || input.Kind == "facts" && !slices.Equal(current.Keys[0].Profiles, profiles) || input.Kind == "content" && !slices.Equal(current.Keys[0].Profiles, []string{sourcefacts.ContentProfile}) {
		return false, errXInsightFeedConflict
	}
	keyID, _ := strconv.ParseInt(input.CredentialID, 10, 64)
	var bound bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xinsight_source_enrollment_bindings WHERE api_key_id=$1)`, keyID).Scan(&bound)
	if err != nil {
		return false, err
	}
	if bound {
		return false, errXInsightFeedConflict
	}
	_, err = tx.Exec(ctx, `UPDATE api_keys SET policy_labels=$2 WHERE id=$1 AND status='feed_active'`, keyID, sourcefacts.AutoEnrollmentPolicyLabel)
	if err == nil {
		org, _ := strconv.ParseInt(candidate.OrgID, 10, 64)
		project, _ := strconv.ParseInt(candidate.ProjectID, 10, 64)
		_, err = tx.Exec(ctx, `INSERT INTO xinsight_source_enrollment_bindings(enrollment_id,org_id,project_id,kind,api_key_id,source_instance,operation_id,bind_operation_id,bind_request_sha256,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'active')`, enrollmentID, org, project, input.Kind, keyID, candidate.SourceInstance, current.Keys[0].RegistrationID, input.OperationID, requestDigest)
		if err == nil {
			err = insertAuditEntryForActor(ctx, tx, actorID, "xinsight.source_enrollment.bind_existing", "xinsight_source_enrollment", strconv.FormatInt(enrollmentID, 10), map[string]any{"installation_id": candidate.InstallationID, "project_id": candidate.ProjectID, "kind": input.Kind, "credential_id": input.CredentialID, "operation_id": input.OperationID, "state_sha256": input.ExpectedStateSHA256}, meta)
		}
	}
	return false, err
}
