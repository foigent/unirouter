package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

var setupUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var setupDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func setupSHA(s string) bool { return setupDigest.MatchString(s) }

type xInsightSetupProgress struct {
	PolicyManageKey    int64   `json:"policy_manage_key"`
	SourceEnrollKey    int64   `json:"source_enroll_key"`
	PolicyGrantIDs     []int64 `json:"policy_grant_ids"`
	SourceGrantID      int64   `json:"source_grant_id"`
	SourceBindingsDone bool    `json:"source_bindings_done"`
	PolicyEffective    bool    `json:"policy_effective"`
	PolicyRef          string  `json:"policy_ref"`
	PolicyVersion      string  `json:"policy_version"`
	PolicyVector       string  `json:"policy_vector"`
	Issue              string  `json:"issue,omitempty"`
}

func (h *Handler) validateSetupSelection(in xInsightSetupConsentPut, preview xInsightSetupPreview) error {
	if in.SchemaVersion != setupConsentSchema || !setupUUID.MatchString(in.RequestID) || !setupSHA(in.ExpectedPreviewSHA256) || !setupSHA(in.ExpectedXIConsentSHA256) || in.ExpectedPreviewSHA256 != preview.PreviewSHA256 || in.ExpectedXIConsentSHA256 != preview.XIConsent.ConsentSHA256 || in.ReceiverMode != preview.ReceiverMode {
		return xinsightconnector.ErrConflict
	}
	if (in.ExistingBindingSHA256 == nil) != (preview.ExistingBindingSHA256 == nil) || in.ExistingBindingSHA256 != nil && *in.ExistingBindingSHA256 != *preview.ExistingBindingSHA256 {
		return xinsightconnector.ErrConflict
	}
	if !stringIn(in.Coverage, preview.AllowedCoverage) || !sourcefacts.ValidateProfiles(in.Profiles) || !h.sourceFactsController.ProfilesEnabled(in.Profiles) || in.IncludeContent && !preview.ContentAvailable {
		return xinsightconnector.ErrInvalid
	}
	if in.ExcludedProjectIDs == nil || len(in.ExcludedProjectIDs) > 256 || in.Coverage == "exact" && len(in.ExcludedProjectIDs) != 0 {
		return xinsightconnector.ErrInvalid
	}
	seen := map[string]bool{}
	for _, raw := range in.ExcludedProjectIDs {
		id, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || id <= 0 || strconv.FormatInt(id, 10) != raw || seen[raw] || raw == in.ProjectID {
			return xinsightconnector.ErrInvalid
		}
		seen[raw] = true
	}
	if in.PolicyAccess == nil || len(in.PolicyAccess) > 3 {
		return xinsightconnector.ErrInvalid
	}
	seen = map[string]bool{}
	lastKind := ""
	for _, access := range in.PolicyAccess {
		if access.TargetKind <= lastKind {
			return xinsightconnector.ErrInvalid
		}
		lastKind = access.TargetKind
		grant := setupPolicyDelegation(in.Invitation.InstallationID, in.ProjectID, in.Coverage, access)
		_, _, _, ok := grant.scope()
		allowed := false
		for _, capability := range preview.AllowedPolicyAccess {
			if capability.TargetKind == access.TargetKind && capability.Coverage == access.Coverage && (!access.Read || capability.Read) && (!access.Write || capability.Write) {
				allowed = true
			}
		}
		if !ok || !allowed || seen[access.TargetKind] {
			return xinsightconnector.ErrInvalid
		}
		seen[access.TargetKind] = true
	}
	if in.URPolicy != nil {
		if !stringIn(in.URPolicy.Scope, preview.AllowedPolicyScopes) {
			return xinsightconnector.ErrUnauthorized
		}
		raw, _ := json.Marshal(in.URPolicy.Put)
		if _, e := contentcapture.DecodeScopedManagementPut(raw); e != nil {
			return e
		}
		if in.IncludeContent && in.Coverage == "all_current_future" && in.URPolicy.Scope != "enterprise" {
			return xinsightconnector.ErrConflict
		}
	}
	for _, issue := range preview.Issues {
		if issue.RequiresChoice {
			return xinsightconnector.ErrConflict
		}
	}
	return nil
}
func stringIn(s string, values []string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
func setupPolicyDelegation(installation, project, coverage string, access xInsightSetupPolicyAccess) policyDelegationInput {
	input := policyDelegationInput{InstallationID: installation, TargetKind: access.TargetKind, Coverage: access.Coverage, Read: access.Read, Write: access.Write, UserID: access.UserID}
	if access.TargetKind != "enterprise_default" && access.Coverage == "exact" {
		input.ProjectID = project
	}
	return input
}

func (h *Handler) CreateXInsightSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	org, ok := parsePositivePathID(w, r, "org_id", "Invalid organization")
	if !ok {
		return
	}
	var in xInsightSetupConsentPut
	if decodeXInsightSetupJSON(w, r, &in) != nil || !setupUUID.MatchString(in.RequestID) {
		writeJSON(w, 400, map[string]any{"error": "invalid_setup_consent"})
		return
	}
	project, e := strconv.ParseInt(in.ProjectID, 10, 64)
	if e != nil || project <= 0 || strconv.FormatInt(project, 10) != in.ProjectID {
		writeJSON(w, 400, map[string]any{"error": "invalid_project"})
		return
	}
	requestSHA := xinsightconnector.PublicCanonicalSHA256(in)
	tx, e := h.db.Begin(r.Context())
	if e != nil {
		writeSetupError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	actor, e := h.reauthorizeCommittingActorTx(r.Context(), tx, setupRequirement(org, project))
	if e != nil {
		writeJSON(w, 403, map[string]any{"error": "setup_denied"})
		return
	}
	var existingID string
	var existingRaw []byte
	e = tx.QueryRow(r.Context(), `SELECT installation_id,setup_consent_canonical FROM xinsight_connector_installations WHERE setup_request_id=$1::uuid`, in.RequestID).Scan(&existingID, &existingRaw)
	if e == nil {
		var saved xInsightSavedSetup
		if decodeXInsightSetupWire(existingRaw, &saved) != nil || saved.OrgID != org || saved.ProjectID != project || saved.ActorID != actor.UserID || saved.RequestSHA256 != requestSHA {
			writeSetupError(w, xinsightconnector.ErrConflict)
			return
		}
		if tx.Commit(r.Context()) != nil {
			writeSetupError(w, xinsightconnector.ErrUnavailable)
			return
		}
		h.writeSetupStatus(w, r, org, existingID, 202)
		return
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		writeSetupError(w, e)
		return
	}
	// The resolve request carries no local credential and holds no SQL locks.
	if tx.Commit(r.Context()) != nil {
		writeSetupError(w, xinsightconnector.ErrUnavailable)
		return
	}
	xi, e := h.resolveXInsightSetupConsent(r.Context(), in.Invitation)
	if e != nil {
		writeSetupError(w, e)
		return
	}
	tx, e = h.db.Begin(r.Context())
	if e != nil {
		writeSetupError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	actor, e = h.reauthorizeCommittingActorTx(r.Context(), tx, setupRequirement(org, project))
	if e != nil {
		writeJSON(w, 403, map[string]any{"error": "setup_denied"})
		return
	}
	preview, e := h.setupPreviewTx(r.Context(), tx, org, project, actor, in.Invitation, xi)
	if e == nil {
		e = h.validateSetupSelection(in, preview)
	}
	if e != nil {
		writeSetupError(w, e)
		return
	}
	// Original machine management requires organization authority. A project
	// manager can inspect, but cannot silently acquire this higher scope.
	if !auth.RoleIn(actor.OrganizationRole, auth.RolePlatformAdmin, auth.RoleOrgAdmin) {
		writeJSON(w, 403, map[string]any{"error": "machine_registration_requires_org_admin"})
		return
	}
	for _, raw := range in.ExcludedProjectIDs {
		id, _ := strconv.ParseInt(raw, 10, 64)
		var present bool
		if tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND org_id=$2)`, id, org).Scan(&present) != nil || !present {
			writeSetupError(w, xinsightconnector.ErrInvalid)
			return
		}
	}
	for _, access := range in.PolicyAccess {
		input := setupPolicyDelegation(in.Invitation.InstallationID, in.ProjectID, in.Coverage, access)
		p, u, _, _ := input.scope()
		if input.TargetKind == "user_exception" && input.Coverage == "exact" {
			var member bool
			e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM project_members pm JOIN users u ON u.id=pm.user_id WHERE pm.project_id=$1 AND pm.user_id=$2 AND u.org_id=$3 AND u.status='active')`, p, u, org).Scan(&member)
			if e != nil || !member {
				writeSetupError(w, xinsightconnector.ErrInvalid)
				return
			}
		}

		if _, e = h.authorizePolicyDelegationAdminTx(r, tx, org, p, u, 0, input.Coverage, input.TargetKind); e != nil {
			writeJSON(w, 403, map[string]any{"error": "policy_delegation_denied"})
			return
		}
	}
	intake, ok := h.xInsightConnectorIntake.(interface {
		AcceptTransaction(context.Context, pgx.Tx, xinsightconnector.InvitationIntakeInput) (xinsightconnector.InvitationIntakeResult, error)
	})
	if !ok {
		writeSetupError(w, xinsightconnector.ErrUnavailable)
		return
	}
	existingSources, e := h.setupExistingSourceStatesTx(r.Context(), tx, org, project, in.Invitation)
	if e != nil {
		writeSetupError(w, xinsightconnector.ErrConflict)
		return
	}
	for _, state := range existingSources {
		if state.Kind == "facts" && !slices.Equal(state.ConnectionProfiles, in.Profiles) {
			writeSetupError(w, xinsightconnector.ErrConflict)
			return
		}
	}
	result, e := intake.AcceptTransaction(r.Context(), tx, xinsightconnector.InvitationIntakeInput{ReceiverMode: in.ReceiverMode, Invitation: in.Invitation, OrgID: org, ProjectID: project, ConsentedByUserID: actor.UserID})
	if e != nil {
		writeSetupError(w, e)
		return
	}
	saved := xInsightSavedSetup{SchemaVersion: setupConsentSchema, RequestID: in.RequestID, ActorID: actor.UserID, ActorEpoch: actor.AuthEpoch, OrgID: org, ProjectID: project, ControlFence: result.Installation.ControlFence, InvitationSHA256: result.Installation.InvitationSHA256, RequestSHA256: requestSHA, XIConsent: xi, Coverage: in.Coverage, ExcludedProjectIDs: in.ExcludedProjectIDs, Profiles: in.Profiles, IncludeContent: in.IncludeContent, PolicyAccess: in.PolicyAccess, URPolicy: in.URPolicy, PolicyVector: preview.URPolicy.VersionVector, ExistingSourceStates: existingSources}
	raw, _ := json.Marshal(saved)
	progress, _ := json.Marshal(xInsightSetupProgress{PolicyGrantIDs: []int64{}})
	tag, e := tx.Exec(r.Context(), `UPDATE xinsight_connector_installations SET setup_request_id=$2::uuid,setup_consent_canonical=$3,setup_consent_sha256=$4,setup_state='provisioning',setup_progress=$5::jsonb WHERE installation_id=$1 AND setup_request_id IS NULL`, result.Installation.InstallationID, in.RequestID, raw, xinsightconnector.PublicCanonicalSHA256(saved), progress)
	if e == nil && tag.RowsAffected() != 1 {
		e = xinsightconnector.ErrConflict
	}
	if e == nil {
		e = insertAuditEntry(r.Context(), tx, "data_exchange.xinsight.setup.consent", "xinsight_connector_installation", result.Installation.InstallationID, map[string]any{"coverage": in.Coverage, "profiles": in.Profiles, "include_content": in.IncludeContent, "policy_access": in.PolicyAccess, "xi_consent_sha256": xi.ConsentSHA256}, requestAuditMeta(r, org, project))
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		writeSetupError(w, e)
		return
	}
	h.writeSetupStatus(w, r, org, result.Installation.InstallationID, 202)
}

func (h *Handler) GetXInsightSetupStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	org, ok := parsePositivePathID(w, r, "org_id", "Invalid organization")
	if !ok {
		return
	}
	h.writeSetupStatus(w, r, org, r.PathValue("installation_id"), 200)
}
func (h *Handler) writeSetupStatus(w http.ResponseWriter, r *http.Request, org int64, id string, code int) {
	tx, e := h.db.Begin(r.Context())
	if e != nil {
		writeSetupError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	var project int64
	if tx.QueryRow(r.Context(), `SELECT project_id FROM xinsight_connector_installations WHERE installation_id=$1 AND org_id=$2 AND setup_request_id IS NOT NULL`, id, org).Scan(&project) != nil {
		writeJSON(w, 404, map[string]any{"error": "setup_not_found"})
		return
	}
	if _, e = h.reauthorizeCommittingActorTx(r.Context(), tx, setupRequirement(org, project)); e != nil {
		writeJSON(w, 403, map[string]any{"error": "setup_denied"})
		return
	}
	saved, progress, state, e := readXInsightSavedSetupTx(r.Context(), tx, id, false)
	if e != nil {
		writeSetupError(w, e)
		return
	}
	// Read the current policy, not the originally admitted setup snapshot. This
	// route cannot change policy or regrant revoked authority.
	if progress.PolicyEffective {
		controller, ok := h.contentPolicyController.(scopedContentPolicyController)
		if !ok || h.contentPolicyReadiness == nil {
			writeSetupError(w, xinsightconnector.ErrUnavailable)
			return
		}
		app, err := h.contentPolicyReadiness(r.Context())
		if err != nil {
			writeSetupError(w, err)
			return
		}
		view, err := controller.ScopedManagementTx(r.Context(), tx, org, project, 0, app.StorageStatus)
		if err != nil {
			writeSetupError(w, err)
			return
		}
		progress.PolicyVector = view.VersionVector
		if view.Effective != nil {
			progress.PolicyRef = view.Effective.PolicyRef
			progress.PolicyVersion = view.Effective.Effective.PolicyVersion
		}
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeSetupError(w, e)
		return
	}
	out := setupStatusProjection(id, saved, progress, state)
	if h.xInsightSetup != nil && h.xInsightSetup.status != nil {
		raw, err := h.xInsightSetup.status(r.Context(), id, saved.XIConsent.DeploymentID)
		var xi xInsightSetupXIStatus
		if err == nil && decodeXInsightSetupWire(raw, &xi) == nil && xi.SchemaVersion == "xinsight.connector-setup-status.v1" && xi.InstallationID == id && xi.DeploymentID == saved.XIConsent.DeploymentID && xi.ConsentRevision == saved.XIConsent.ConsentRevision {
			out.XI = &xi
			// Initial custody admission remains XI's CAS-controlled step. Once the
			// original local steps are complete, later human policy operations project
			// their current loaded effective revision rather than the initial snapshot.
			out.ConfigurationReady = out.ConfigurationReady && xi.Custody.Loaded && xi.Custody.EffectiveRevision == xi.Custody.DesiredRevision && xi.Custody.Settings != nil && xi.Custody.EffectiveRevision > 0

			if out.ConfigurationReady {
				out.State = "waiting_data"
				if xi.Receiving.LastSuccessAt != nil && !xi.Receiving.LastSuccessAt.IsZero() {
					out.State = "ready"
				}
			}
			if len(xi.Issues) > 0 {
				out.Issues = append(out.Issues, xi.Issues...)
			}
			if xi.State == "needs_action" || xi.State == "failed" {
				out.State = xi.State
				out.ConfigurationReady = false
			}
		} else {
			out.ConfigurationReady = false
			out.Issues = append(out.Issues, xInsightSetupIssue{"xi_status_unavailable", false})
		}
	} else {
		out.ConfigurationReady = false
		out.Issues = append(out.Issues, xInsightSetupIssue{"xi_status_unavailable", false})
	}
	if journal := h.xInsightCredentialJournalSnapshot(); journal != nil {
		credential, err := journal.CredentialStatus(r.Context(), id)
		if err == nil {
			out.Credential = &credential
			switch credential.State {
			case "expired_recovery_required":
				out.State, out.ConfigurationReady = "needs_action", false
				out.Issues = append(out.Issues, xInsightSetupIssue{"control_credential_expired", true})
			case "committed_unpromoted", "rotation_pending":
				out.State, out.ConfigurationReady = "needs_action", false
				out.Issues = append(out.Issues, xInsightSetupIssue{"control_credential_pending", false})
			case "revoked", "credential_unavailable":
				out.State, out.ConfigurationReady = "needs_action", false
				out.Issues = append(out.Issues, xInsightSetupIssue{"control_credential_unavailable", true})
			}
		}
	}
	writeJSON(w, code, out)
}
func readXInsightSavedSetupTx(ctx context.Context, tx pgx.Tx, id string, lock bool) (xInsightSavedSetup, xInsightSetupProgress, string, error) {
	var saved xInsightSavedSetup
	var progress xInsightSetupProgress
	var raw, p []byte
	var sha, state string
	sql := `SELECT setup_consent_canonical,setup_consent_sha256,setup_progress,setup_state FROM xinsight_connector_installations WHERE installation_id=$1 AND setup_request_id IS NOT NULL`
	if lock {
		sql += " FOR UPDATE"
	}
	e := tx.QueryRow(ctx, sql, id).Scan(&raw, &sha, &p, &state)
	if e == nil {
		e = decodeXInsightSetupWire(raw, &saved)
	}
	if e == nil {
		e = decodeXInsightSetupWire(p, &progress)
	}
	if e == nil && sha != xinsightconnector.PublicCanonicalSHA256(saved) {
		e = xinsightconnector.ErrConflict
	}
	return saved, progress, state, e
}
func setupStatusProjection(id string, s xInsightSavedSetup, p xInsightSetupProgress, state string) xInsightSetupStatus {
	out := xInsightSetupStatus{SchemaVersion: "unirouter.xinsight.setup-status.v1", InstallationID: id, ConsentRevision: 1, State: state, Issues: []xInsightSetupIssue{}}
	out.PurposeStatus.PolicyManage = "waiting_candidate"
	if len(s.PolicyAccess) == 0 {
		out.PurposeStatus.PolicyManage = "not_requested"
	}
	out.PurposeStatus.SourceEnroll = "waiting_candidate"
	out.URPolicyStatus.State = "pending"
	if p.PolicyManageKey > 0 && len(s.PolicyAccess) > 0 {
		out.PurposeStatus.PolicyManage = "active"
	}
	if p.SourceEnrollKey > 0 {
		out.PurposeStatus.SourceEnroll = "active"
	}
	if len(s.PolicyAccess) > 0 && len(p.PolicyGrantIDs) == len(s.PolicyAccess) {
		out.PurposeStatus.PolicyManage = "granted"
	}
	if p.SourceGrantID > 0 {
		out.PurposeStatus.SourceEnroll = "granted"
	}
	if p.PolicyEffective {
		out.URPolicyStatus.State = "effective"
	}
	out.URPolicyStatus.PolicyRef = p.PolicyRef
	out.URPolicyStatus.PolicyVersion = p.PolicyVersion
	out.URPolicyStatus.VersionVector = p.PolicyVector
	enterpriseGranted := false
	for _, access := range s.PolicyAccess {
		if access.TargetKind == "enterprise_default" && access.Write {
			enterpriseGranted = true
		}
	}
	if !enterpriseGranted {
		out.Issues = append(out.Issues, xInsightSetupIssue{"enterprise_joint_management_not_granted", false})
	}
	out.ConfigurationReady = p.PolicyEffective && p.SourceBindingsDone && p.SourceGrantID > 0 && len(p.PolicyGrantIDs) == len(s.PolicyAccess) && p.Issue == ""
	if p.Issue != "" {
		out.Issues = append(out.Issues, xInsightSetupIssue{p.Issue, true})
		out.URPolicyStatus.State = "blocked"
	}
	return out
}
