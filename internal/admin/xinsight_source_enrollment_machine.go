package admin

import (
	"bytes"
	"context"
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
	"github.com/uniroute/unirouter/internal/principallock"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

type machineEnrollmentRegistration struct {
	EnrollmentID string                   `json:"enrollment_id"`
	Kind         string                   `json:"kind"`
	Registration sourcefacts.Registration `json:"registration"`
}

func decodeMachineEnrollmentRegistration(w http.ResponseWriter, r *http.Request) (machineEnrollmentRegistration, bool) {
	var input machineEnrollmentRegistration
	wire, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err == nil {
		if strictErr := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); strictErr != nil {
			err = strictErr
		}
	}
	if err == nil {
		var fields map[string]json.RawMessage
		if json.Unmarshal(wire, &fields) != nil || fields == nil || len(fields) != 3 ||
			fields["enrollment_id"] == nil || fields["kind"] == nil || fields["registration"] == nil {
			err = errors.New("invalid enrollment registration fields")
		} else {
			for field := range fields {
				if field != "enrollment_id" && field != "kind" && field != "registration" {
					err = errors.New("unknown enrollment registration field")
				}
			}
			if !exactSourceControlFields([]byte(`{"registration":` + string(fields["registration"]) + `}`)) {
				err = errors.New("invalid source registration field")
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
	if err != nil || input.Kind != "facts" && input.Kind != "content" {
		writeJSON(w, 400, map[string]any{"error": "Invalid enrollment registration"})
		return input, false
	}
	return input, true
}

type machineEnrollmentAuthority struct {
	id, grantorID, projectID, anchorID, orgID int64
	coverage, installation, source            string
	profiles                                  []string
	content                                   bool
}

func (h *Handler) machineEnrollmentLocator(ctx context.Context, identity middleware.XInsightMachineIdentity, enrollmentID int64) (machineEnrollmentAuthority, error) {
	var a machineEnrollmentAuthority
	a.id = enrollmentID
	err := h.db.QueryRow(ctx, `SELECT grantor_user_id,COALESCE(exact_project_id,0),coverage FROM xinsight_source_enrollments
 WHERE id=$1 AND installation_id=$2 AND org_id=$3`, enrollmentID, identity.InstallationID, identity.OrgID).
		Scan(&a.grantorID, &a.projectID, &a.coverage)
	return a, err
}

// Call after a locator read. No locator value grants authority; every one is
// compared again under the locked transaction before writing project state.
func (h *Handler) authorizeMachineEnrollmentTx(ctx context.Context, tx pgx.Tx, identity middleware.XInsightMachineIdentity,
	projectID int64, a *machineEnrollmentAuthority) (string, error) {
	if identity.Purpose != sourceEnrollPurpose || a.grantorID <= 0 {
		return "", errCommittingActorAuthorizationChanged
	}
	if err := principallock.LockUsers(ctx, tx, a.grantorID); err != nil {
		return "", err
	}
	var grantorOrg int64
	var role, userStatus string
	err := tx.QueryRow(ctx, `SELECT org_id,organization_role,status FROM users WHERE id=$1 FOR UPDATE`, a.grantorID).
		Scan(&grantorOrg, &role, &userStatus)
	if err != nil || userStatus != "active" {
		return "", errCommittingActorAuthorizationChanged
	}
	var orgStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM orgs WHERE id=$1 FOR UPDATE`, identity.OrgID).Scan(&orgStatus)
	if err != nil || orgStatus != "active" {
		return "", errCommittingActorAuthorizationChanged
	}
	var projectOrg int64
	var projectStatus string
	err = tx.QueryRow(ctx, `SELECT org_id,status FROM projects WHERE id=$1 FOR UPDATE`, projectID).Scan(&projectOrg, &projectStatus)
	if err != nil || projectOrg != identity.OrgID || projectStatus != "active" {
		return "", errCommittingActorScopeNotFound
	}
	if role != auth.RolePlatformAdmin && (role != auth.RoleOrgAdmin || grantorOrg != identity.OrgID) {
		if a.coverage != "exact" || a.projectID != projectID || grantorOrg != identity.OrgID {
			return "", errCommittingActorAuthorizationChanged
		}
		var membership string
		err = tx.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2 FOR SHARE`,
			projectID, a.grantorID).Scan(&membership)
		if err != nil || membership != projectMemberAdmin {
			return "", errCommittingActorAuthorizationChanged
		}
	}
	var desired, observed, deployment string
	err = tx.QueryRow(ctx, `SELECT org_id,project_id,source_instance,x_deployment_id,desired_state,observed_state
 FROM xinsight_connector_installations WHERE installation_id=$1 FOR UPDATE`, identity.InstallationID).
		Scan(&a.orgID, &a.anchorID, &a.source, &deployment, &desired, &observed)
	if err != nil || a.orgID != identity.OrgID || a.anchorID != identity.AnchorProjectID ||
		a.source != identity.SourceInstance || deployment != identity.XDeploymentID ||
		desired != "active" || observed == "disconnected" || h.sourceFactsController == nil ||
		h.sourceFactsController.SourceInstance() != a.source {
		return "", errCommittingActorAuthorizationChanged
	}
	var keyStatus, purpose, allowedIP string
	err = tx.QueryRow(ctx, `SELECT status,purpose,allowed_ips FROM api_keys WHERE id=$1 AND org_id=$2 AND project_id=$3 FOR UPDATE`,
		identity.KeyID, identity.OrgID, identity.AnchorProjectID).Scan(&keyStatus, &purpose, &allowedIP)
	if err != nil || keyStatus != "feed_active" || purpose != sourceEnrollPurpose || !exactHostNetworkBinding(allowedIP) {
		return "", errCommittingActorAuthorizationChanged
	}
	var grantor, exactProject int64
	var coverage, state string
	var expiry *time.Time
	err = tx.QueryRow(ctx, `SELECT grantor_user_id,COALESCE(exact_project_id,0),coverage,profiles,include_content,state,expires_at
 FROM xinsight_source_enrollments WHERE id=$1 AND installation_id=$2 AND org_id=$3 FOR UPDATE`,
		a.id, identity.InstallationID, identity.OrgID).
		Scan(&grantor, &exactProject, &coverage, &a.profiles, &a.content, &state, &expiry)
	if err != nil || grantor != a.grantorID || exactProject != a.projectID || coverage != a.coverage ||
		state != "active" || expiry != nil && !expiry.After(time.Now()) ||
		!(coverage == "all_current_future" || coverage == "exact" && exactProject == projectID) {
		return "", errCommittingActorAuthorizationChanged
	}
	var excluded bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xinsight_source_enrollment_exclusions
 WHERE enrollment_id=$1 AND project_id=$2 AND excluded)`, a.id, projectID).Scan(&excluded)
	if err != nil || excluded {
		return "", errCommittingActorAuthorizationChanged
	}
	return allowedIP, nil
}

func (h *Handler) RegisterXInsightSourceEnrollmentProject(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identity, authenticated := middleware.XInsightMachineFromContext(r.Context())
	projectID, ok := parsePositivePathID(w, r, "project_id", "Invalid project ID")
	if !ok {
		return
	}
	input, ok := decodeMachineEnrollmentRegistration(w, r)
	if !ok {
		return
	}
	enrollmentID, ok := sourceEnrollmentPathID(input.EnrollmentID)
	if !authenticated || !ok || input.Registration.OrgID != strconv.FormatInt(identity.OrgID, 10) ||
		input.Registration.ProjectID != strconv.FormatInt(projectID, 10) ||
		input.Registration.SourceInstance != identity.SourceInstance || h.apiKeyHashSecret == "" {
		writeJSON(w, 400, map[string]any{"error": "Enrollment registration scope differs"})
		return
	}
	var digest string
	var err error
	if input.Kind == "facts" {
		digest, err = input.Registration.Hash()
	} else {
		digest, err = sourcefacts.HashContentRegistration(input.Registration)
	}
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": "Invalid source registration"})
		return
	}
	a, err := h.machineEnrollmentLocator(r.Context(), identity, enrollmentID)
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Enrollment unavailable"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	allowedIP, err := h.authorizeMachineEnrollmentTx(r.Context(), tx, identity, projectID, &a)
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Enrollment registration denied"})
		return
	}
	if input.Kind == "facts" {
		if !slices.Equal(input.Registration.Profiles, a.profiles) || !h.sourceFactsController.ProfilesEnabled(a.profiles) {
			writeJSON(w, 409, map[string]any{"error": "Enrollment profiles changed"})
			return
		}
	} else if !a.content {
		writeJSON(w, 403, map[string]any{"error": "Content enrollment not granted"})
		return
	}
	var oldKey int64
	var oldOperation, oldHash, oldStatus string
	err = tx.QueryRow(r.Context(), `SELECT b.api_key_id,b.operation_id,sc.registration_sha256,ak.status
 FROM xinsight_source_enrollment_bindings b JOIN api_keys ak ON ak.id=b.api_key_id
 JOIN execution_source_credentials sc ON sc.api_key_id=b.api_key_id
 WHERE b.enrollment_id=$1 AND b.org_id=$2 AND b.project_id=$3 AND b.kind=$4 FOR UPDATE OF b`,
		a.id, identity.OrgID, projectID, input.Kind).Scan(&oldKey, &oldOperation, &oldHash, &oldStatus)
	if err == nil {
		if oldOperation != input.Registration.OperationID || oldHash != digest || oldStatus != "feed_active" {
			writeJSON(w, 409, map[string]any{"error": "Enrollment registration changed"})
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeJSON(w, 503, map[string]any{"error": "Enrollment read failed"})
			return
		}
		writeJSON(w, 200, map[string]any{"enrollment_id": input.EnrollmentID, "kind": input.Kind,
			"credential_id": strconv.FormatInt(oldKey, 10), "status": "active",
			"registration_hash": oldHash, "replayed": true})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 503, map[string]any{"error": "Enrollment read failed"})
		return
	}
	var connectionCount int
	if err = tx.QueryRow(r.Context(), `SELECT COUNT(*) FROM execution_source_connections WHERE org_id=$1 AND project_id=$2`,
		identity.OrgID, projectID).Scan(&connectionCount); err != nil {
		writeJSON(w, 503, map[string]any{"error": "Connection read failed"})
		return
	}
	if input.Kind == "facts" && connectionCount != 0 || input.Kind == "content" && connectionCount != 1 {
		code := "source_connection_unavailable"
		if input.Kind == "facts" && connectionCount != 0 {
			code = "requires_migration"
		}
		writeJSON(w, 409, map[string]any{"error": code})
		return
	}
	if input.Kind == "content" {
		var existingContent int
		err = tx.QueryRow(r.Context(), `SELECT COUNT(*) FROM execution_source_content_scopes
 WHERE org_id=$1 AND project_id=$2`, identity.OrgID, projectID).Scan(&existingContent)
		if err != nil {
			writeJSON(w, 503, map[string]any{"error": "Content scope read failed"})
			return
		}
		if existingContent != 0 {
			writeJSON(w, 409, map[string]any{"error": "requires_migration"})
			return
		}
	}
	if input.Kind == "content" && len(a.profiles) > 0 {
		var factsActive int
		err = tx.QueryRow(r.Context(), `SELECT COUNT(*) FROM xinsight_source_enrollment_bindings b JOIN api_keys ak ON ak.id=b.api_key_id
 WHERE b.enrollment_id=$1 AND b.org_id=$2 AND b.project_id=$3 AND b.kind='facts' AND b.state='active'
 AND ak.status='feed_active'`, a.id, identity.OrgID, projectID).Scan(&factsActive)
		if err != nil || factsActive != 1 {
			writeJSON(w, 409, map[string]any{"error": "Facts enrollment must be active first"})
			return
		}
	}
	verifier, _ := auth.DecodeReceiverFeedVerifier(input.Registration.VerifierSHA256)
	hash := auth.HashReceiverFeedVerifier(verifier, h.apiKeyHashSecret)
	if input.Kind == "facts" {
		_, err = tx.Exec(r.Context(), `INSERT INTO execution_source_connections(org_id,project_id,source_instance,status,profiles)
 VALUES($1,$2,$3,'active',$4)`, identity.OrgID, projectID, identity.SourceInstance, a.profiles)
	} else {
		controller, ok := h.sourceFactsController.(interface{ ContentEnabled() bool })
		if !ok || !controller.ContentEnabled() {
			writeJSON(w, 503, map[string]any{"error": "Content capture unavailable"})
			return
		}
		var status, source string
		err = tx.QueryRow(r.Context(), `SELECT status,source_instance FROM execution_source_connections
 WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, identity.OrgID, projectID).Scan(&status, &source)
		if err == nil && (status != "active" || source != identity.SourceInstance) {
			err = errXInsightFeedConflict
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO execution_content_streams(org_id,project_id,source_instance)
 VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, identity.OrgID, projectID, identity.SourceInstance)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO execution_source_content_scopes(org_id,project_id,status,state_version)
 VALUES($1,$2,'active',1)`, identity.OrgID, projectID)
		}
	}
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "Project source connection conflict"})
		return
	}
	var keyID int64
	scope := "legacy"
	if input.Kind == "content" {
		scope = "content"
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,
 rpm_limit,qps_limit,budget_limit,allowed_ips,allowed_models,policy_labels,purpose,verifier_scheme,
 registration_operation_id,source_lifecycle_scope)
 VALUES($1,$2,'service','X-Insight auto-enrolled source',$3,$4,'feed_active',60,1,0,$5,'',$6,
 'xinsight.source.read',$7,$8,$9) RETURNING id`, identity.OrgID, projectID, hash, input.Registration.KeyPrefix,
		allowedIP, sourcefacts.AutoEnrollmentPolicyLabel, auth.ReceiverFeedVerifierScheme, input.Registration.OperationID, scope).Scan(&keyID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO execution_source_credentials
 (api_key_id,org_id,project_id,registration_sha256,profiles,source_lifecycle_scope)
 VALUES($1,$2,$3,$4,$5,$6)`, keyID, identity.OrgID, projectID, digest, input.Registration.Profiles, scope)
	}
	if err == nil && input.Kind == "content" {
		var after int64
		var at time.Time
		err = tx.QueryRow(r.Context(), `SELECT retained_after_sequence,retained_after_time FROM execution_content_streams
 WHERE org_id=$1 AND project_id=$2 FOR SHARE`, identity.OrgID, projectID).Scan(&after, &at)
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO execution_source_content_bindings
 (api_key_id,org_id,project_id,coverage_after_sequence,coverage_time,allowed_purposes)
 VALUES($1,$2,$3,$4,$5,ARRAY['human_review']::text[])`, keyID, identity.OrgID, projectID, after, at)
		}
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO xinsight_source_enrollment_bindings
 (enrollment_id,org_id,project_id,kind,api_key_id,source_instance,operation_id,state)
 VALUES($1,$2,$3,$4,$5,$6,$7,'active')`, a.id, identity.OrgID, projectID, input.Kind, keyID,
			identity.SourceInstance, input.Registration.OperationID)
	}
	if err == nil {
		detail, _ := json.Marshal(map[string]any{"enrollment_id": input.EnrollmentID, "kind": input.Kind,
			"credential_id": strconv.FormatInt(keyID, 10), "project_id": strconv.FormatInt(projectID, 10)})
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(org_id,project_id,user_id,actor_kind,actor_ref,
 action,resource,resource_id,detail,source,request_id)
 VALUES($1,$2,NULL,'service',$3,'data_exchange.xinsight.source_enrollment.register',
 'xinsight_source_enrollment',$4,$5::jsonb,'xinsight_integration',$6)`, identity.OrgID, projectID,
			"api_key:"+strconv.FormatInt(identity.KeyID, 10), input.EnrollmentID, detail, input.Registration.OperationID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Enrollment registration failed"})
		return
	}
	writeJSON(w, 201, map[string]any{"enrollment_id": input.EnrollmentID, "kind": input.Kind,
		"credential_id": strconv.FormatInt(keyID, 10), "status": "active", "registration_hash": digest})
}
