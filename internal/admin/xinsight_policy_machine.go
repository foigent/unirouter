package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/principallock"
)

var machinePolicyOperationPattern = regexp.MustCompile(`^xipw_[0-9a-f]{32}$`)

type machinePolicyOperator struct {
	Kind         string `json:"kind"`
	SubjectRef   string `json:"subject_ref"`
	XIRequestRef string `json:"xi_request_ref"`
}
type machinePolicyWrite struct {
	SchemaVersion              string                `json:"schema_version"`
	OperationRef               string                `json:"operation_ref"`
	InstallationID             string                `json:"installation_id"`
	SourceInstance             string                `json:"source_instance"`
	DelegationID               string                `json:"delegation_id"`
	ExpectedDelegationRevision string                `json:"expected_delegation_revision"`
	XIOperator                 machinePolicyOperator `json:"xi_operator"`
	Policy                     json.RawMessage       `json:"policy"`
}
type machinePolicyTarget struct {
	orgID, projectID, userID int64
	kind, path               string
}

func exactMachinePolicyKeys(raw []byte) bool {
	var outer map[string]json.RawMessage
	if json.Unmarshal(raw, &outer) != nil || outer == nil {
		return false
	}
	allowed := map[string]bool{"schema_version": true, "operation_ref": true, "installation_id": true, "source_instance": true,
		"delegation_id": true, "expected_delegation_revision": true, "xi_operator": true, "policy": true}
	for key := range outer {
		if !allowed[key] {
			return false
		}
	}
	var operator map[string]json.RawMessage
	if json.Unmarshal(outer["xi_operator"], &operator) != nil || operator == nil {
		return false
	}
	for key := range operator {
		if key != "kind" && key != "subject_ref" && key != "xi_request_ref" {
			return false
		}
	}
	return true
}

func decodeMachinePolicyWrite(w http.ResponseWriter, r *http.Request, target machinePolicyTarget) (machinePolicyWrite, contentcapture.ScopedManagementPut, string, bool) {
	var envelope machinePolicyWrite
	var put contentcapture.ScopedManagementPut
	wire, err := io.ReadAll(http.MaxBytesReader(w, r.Body, contentcapture.MaxPolicyJSONBytes+4096))
	if err == nil {
		if strictErr := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); strictErr != nil {
			err = strictErr
		}
	}
	if err == nil && !exactMachinePolicyKeys(wire) {
		err = errors.New("unknown field")
	}
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(wire))
		dec.DisallowUnknownFields()
		err = dec.Decode(&envelope)
		if err == nil {
			err = dec.Decode(new(any))
			if errors.Is(err, io.EOF) {
				err = nil
			} else if err == nil {
				err = errors.New("trailing JSON")
			}
		}
	}
	if err == nil {
		put, err = contentcapture.DecodeScopedManagementPut(envelope.Policy)
	}
	revision, revisionOK := positiveIDOrZero(envelope.ExpectedDelegationRevision)
	_, grantOK := policyDelegationPathID(envelope.DelegationID)
	if err != nil || envelope.SchemaVersion != "unirouter.xinsight.policy-write.v1" ||
		!machinePolicyOperationPattern.MatchString(envelope.OperationRef) || !grantOK || !revisionOK || revision == 0 ||
		envelope.XIOperator.Kind != "human" || len(envelope.XIOperator.SubjectRef) < 1 || len(envelope.XIOperator.SubjectRef) > 128 ||
		len(envelope.XIOperator.XIRequestRef) < 1 || len(envelope.XIOperator.XIRequestRef) > 128 {
		writeJSON(w, 400, map[string]any{"error": "Invalid machine policy request"})
		return envelope, put, "", false
	}
	// Typed re-encoding normalizes JSON object key order while preserving the
	// exact CAS strings that P2a interprets. The method and target are part of
	// the operation identity, even if the envelope bytes are identical.
	envelope.Policy, _ = json.Marshal(put)
	canonical, _ := json.Marshal(struct {
		Method  string             `json:"method"`
		Target  string             `json:"target"`
		Request machinePolicyWrite `json:"request"`
	}{Method: http.MethodPut, Target: target.path, Request: envelope})
	digest := sha256.Sum256(canonical)
	return envelope, put, "sha256:" + hex.EncodeToString(digest[:]), true
}

func machinePolicyTargetFromPath(w http.ResponseWriter, r *http.Request, kind string) (machinePolicyTarget, bool) {
	var target machinePolicyTarget
	target.kind = kind
	switch kind {
	case contentcapture.PolicyScopeEnterprise:
		id, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
		if !ok {
			return target, false
		}
		target.orgID = id
		target.path = "/api/v1/integrations/xinsight/payload-policy/organizations/" + strconv.FormatInt(id, 10)
	case contentcapture.PolicyScopeProject, contentcapture.PolicyScopeUser:
		id, ok := parsePositivePathID(w, r, "project_id", "Invalid project ID")
		if !ok {
			return target, false
		}
		target.projectID = id
		target.path = "/api/v1/integrations/xinsight/payload-policy/projects/" + strconv.FormatInt(id, 10)
		if kind == contentcapture.PolicyScopeUser {
			id, ok = parsePositivePathID(w, r, "user_id", "Invalid user ID")
			if !ok {
				return target, false
			}
			target.userID = id
			target.path += "/users/" + strconv.FormatInt(id, 10)
		}
	default:
		writeJSON(w, 404, map[string]any{"error": "Unknown policy scope"})
		return target, false
	}
	return target, true
}

type machinePolicyAuthority struct {
	delegationID, revision, grantorID int64
	coverage, kind                    string
	read, write                       bool
	orgID                             int64
	locatorProjectID, locatorUserID   int64
}

// Read the grantor ID before any product lock. This is a locator only: the
// locked transaction compares all identity fields and the live grantor role.
func (h *Handler) machinePolicyGrantLocator(ctx context.Context, identity middleware.XInsightMachineIdentity, grantID int64) (machinePolicyAuthority, error) {
	var a machinePolicyAuthority
	a.delegationID = grantID
	err := h.db.QueryRow(ctx, `SELECT grantor_user_id,target_kind,coverage,COALESCE(exact_project_id,0),COALESCE(exact_user_id,0)
 FROM xinsight_policy_delegations WHERE id=$1 AND installation_id=$2 AND org_id=$3`,
		grantID, identity.InstallationID, identity.OrgID).Scan(&a.grantorID, &a.kind, &a.coverage, &a.locatorProjectID, &a.locatorUserID)
	return a, err
}

func (h *Handler) authorizeMachinePolicyTx(ctx context.Context, tx pgx.Tx, identity middleware.XInsightMachineIdentity, target machinePolicyTarget, a *machinePolicyAuthority, write bool) error {
	if a.grantorID <= 0 || identity.Purpose != policyManagePurpose {
		return errCommittingActorAuthorizationChanged
	}
	if err := principallock.LockUsers(ctx, tx, a.grantorID, target.userID); err != nil {
		return err
	}
	var grantorOrg int64
	var orgRole, userStatus string
	err := tx.QueryRow(ctx, `SELECT org_id,organization_role,status FROM users WHERE id=$1 FOR UPDATE`, a.grantorID).
		Scan(&grantorOrg, &orgRole, &userStatus)
	if err != nil || userStatus != "active" || grantorOrg <= 0 {
		return errCommittingActorAuthorizationChanged
	}
	orgLock := "FOR SHARE"
	if write {
		orgLock = "FOR UPDATE"
	}
	first, second := grantorOrg, identity.OrgID
	if second < first {
		first, second = second, first
	}
	for _, orgID := range []int64{first, second} {
		var status string
		err = tx.QueryRow(ctx, `SELECT status FROM orgs WHERE id=$1 `+orgLock, orgID).Scan(&status)
		if err != nil || status != "active" {
			return errCommittingActorAuthorizationChanged
		}
		if first == second {
			break
		}
	}
	if target.projectID > 0 {
		var projectStatus string
		err = tx.QueryRow(ctx, `SELECT org_id,status FROM projects WHERE id=$1 FOR UPDATE`, target.projectID).Scan(&target.orgID, &projectStatus)
		if err != nil || target.orgID != identity.OrgID || projectStatus != "active" {
			return errCommittingActorScopeNotFound
		}
	}
	if target.userID > 0 {
		var memberStatus, memberRole string
		err = tx.QueryRow(ctx, `SELECT u.status,pm.role FROM users u JOIN project_members pm ON pm.user_id=u.id
 WHERE u.id=$1 AND u.org_id=$2 AND pm.project_id=$3 FOR SHARE OF u,pm`, target.userID, identity.OrgID, target.projectID).Scan(&memberStatus, &memberRole)
		if err != nil || memberStatus != "active" || memberRole == "" {
			return errCommittingActorScopeNotFound
		}
	}
	orgAuthority := orgRole == auth.RolePlatformAdmin || grantorOrg == identity.OrgID && orgRole == auth.RoleOrgAdmin
	if !orgAuthority {
		if a.coverage != "exact" || target.projectID <= 0 {
			return errCommittingActorAuthorizationChanged
		}
		var memberRole string
		err = tx.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2 FOR SHARE`, target.projectID, a.grantorID).Scan(&memberRole)
		if err != nil || memberRole != projectMemberAdmin {
			return errCommittingActorAuthorizationChanged
		}
	}
	var installationOrg, anchor int64
	var deployment, instance, desired, observed string
	err = tx.QueryRow(ctx, `SELECT org_id,project_id,x_deployment_id,source_instance,desired_state,observed_state
 FROM xinsight_connector_installations WHERE installation_id=$1 FOR UPDATE`, identity.InstallationID).Scan(&installationOrg, &anchor, &deployment, &instance, &desired, &observed)
	if err != nil || installationOrg != identity.OrgID || anchor != identity.AnchorProjectID || deployment != identity.XDeploymentID ||
		instance != identity.SourceInstance || desired != "active" || observed == "disconnected" ||
		h.sourceFactsController == nil || h.sourceFactsController.SourceInstance() != instance {
		return errCommittingActorAuthorizationChanged
	}
	var keyStatus, keyPurpose string
	err = tx.QueryRow(ctx, `SELECT status,purpose FROM api_keys WHERE id=$1 AND org_id=$2 AND project_id=$3 FOR UPDATE`,
		identity.KeyID, identity.OrgID, anchor).Scan(&keyStatus, &keyPurpose)
	if err != nil || keyStatus != "feed_active" || keyPurpose != policyManagePurpose {
		return errCommittingActorAuthorizationChanged
	}
	var exactProject, exactUser int64
	var state string
	var expires *time.Time
	locatorGrantor, locatorKind, locatorCoverage, locatorProject, locatorUser := a.grantorID, a.kind, a.coverage, a.locatorProjectID, a.locatorUserID
	err = tx.QueryRow(ctx, `SELECT org_id,grantor_user_id,revision,target_kind,coverage,COALESCE(exact_project_id,0),
 COALESCE(exact_user_id,0),can_read,can_write,state,expires_at
 FROM xinsight_policy_delegations WHERE id=$1 AND installation_id=$2 FOR UPDATE`, a.delegationID, identity.InstallationID).
		Scan(&a.orgID, &a.grantorID, &a.revision, &a.kind, &a.coverage, &exactProject, &exactUser, &a.read, &a.write, &state, &expires)
	if err != nil || a.orgID != identity.OrgID || a.grantorID != locatorGrantor || a.kind != locatorKind ||
		a.coverage != locatorCoverage || exactProject != locatorProject || exactUser != locatorUser ||
		state != "active" || expires != nil && !expires.After(time.Now()) ||
		write && !a.write || !write && !a.read {
		return errCommittingActorAuthorizationChanged
	}
	wantKind := map[string]string{contentcapture.PolicyScopeEnterprise: "enterprise_default", contentcapture.PolicyScopeProject: "project_policy", contentcapture.PolicyScopeUser: "user_exception"}[target.kind]
	if a.kind != wantKind || a.coverage == "exact" && (exactProject != target.projectID || exactUser != target.userID) ||
		a.coverage == "all_current_future" && (target.kind == contentcapture.PolicyScopeEnterprise || exactProject != 0 || exactUser != 0) {
		return errCommittingActorScopeNotFound
	}
	return nil
}

func (h *Handler) machinePolicyReady(w http.ResponseWriter, r *http.Request) (scopedContentPolicyController, contentcapture.PolicyApplication, middleware.XInsightMachineIdentity, bool) {
	w.Header().Set("Cache-Control", "no-store")
	identity, ok := middleware.XInsightMachineFromContext(r.Context())
	controller, ready := h.contentPolicyController.(scopedContentPolicyController)
	if !ok || !ready || h.contentPolicyReadiness == nil {
		writeJSON(w, 503, map[string]any{"error": "Policy management unavailable"})
		return nil, contentcapture.PolicyApplication{}, identity, false
	}
	application, err := h.contentPolicyReadiness(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Policy readiness unavailable"})
		return nil, contentcapture.PolicyApplication{}, identity, false
	}
	application.Now = time.Now().UTC()
	return controller, application, identity, true
}

func (h *Handler) machinePolicyViewTx(ctx context.Context, tx pgx.Tx, controller scopedContentPolicyController, target machinePolicyTarget, application contentcapture.PolicyApplication) (contentcapture.ScopedManagementView, error) {
	if target.kind == contentcapture.PolicyScopeEnterprise {
		return controller.EnterpriseManagementTx(ctx, tx, target.orgID, application.StorageStatus)
	}
	return controller.ScopedManagementTx(ctx, tx, target.orgID, target.projectID, target.userID, application.StorageStatus)
}

func (h *Handler) GetXInsightMachinePolicy(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		controller, application, identity, ok := h.machinePolicyReady(w, r)
		if !ok {
			return
		}
		target, ok := machinePolicyTargetFromPath(w, r, kind)
		if !ok {
			return
		}
		if target.orgID > 0 && target.orgID != identity.OrgID {
			writeJSON(w, 404, map[string]any{"error": "Policy scope not found"})
			return
		}
		if target.orgID == 0 {
			target.orgID = identity.OrgID
		}
		grantID, ok := policyDelegationPathID(r.URL.Query().Get("delegation_id"))
		if !ok {
			writeJSON(w, 400, map[string]any{"error": "delegation_id required"})
			return
		}
		a, err := h.machinePolicyGrantLocator(r.Context(), identity, grantID)
		if err != nil {
			writeJSON(w, 403, map[string]any{"error": "Delegation unavailable"})
			return
		}
		tx, err := h.db.Begin(r.Context())
		if err != nil {
			writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
			return
		}
		defer tx.Rollback(r.Context())
		err = h.authorizeMachinePolicyTx(r.Context(), tx, identity, target, &a, false)
		if err != nil {
			writeJSON(w, 403, map[string]any{"error": "Policy read denied"})
			return
		}
		view, err := h.machinePolicyViewTx(r.Context(), tx, controller, target, application)
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			writeJSON(w, 503, map[string]any{"error": "Policy read failed"})
			return
		}
		writeJSON(w, 200, view)
	}
}

func machinePolicyLayer(view contentcapture.ScopedManagementView, kind string) contentcapture.ScopedPolicyLayer {
	switch kind {
	case contentcapture.PolicyScopeProject:
		if view.Project != nil {
			return *view.Project
		}
	case contentcapture.PolicyScopeUser:
		if view.User != nil {
			return *view.User
		}
	}
	return view.Enterprise
}

func (h *Handler) PutXInsightMachinePolicy(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		controller, application, identity, ok := h.machinePolicyReady(w, r)
		if !ok {
			return
		}
		target, ok := machinePolicyTargetFromPath(w, r, kind)
		if !ok {
			return
		}
		if target.orgID > 0 && target.orgID != identity.OrgID {
			writeJSON(w, 404, map[string]any{"error": "Policy scope not found"})
			return
		}
		if target.orgID == 0 {
			target.orgID = identity.OrgID
		}
		envelope, put, requestSHA, ok := decodeMachinePolicyWrite(w, r, target)
		if !ok {
			return
		}
		if envelope.InstallationID != identity.InstallationID || envelope.SourceInstance != identity.SourceInstance {
			writeJSON(w, 409, map[string]any{"error": "Binding changed"})
			return
		}
		grantID, _ := policyDelegationPathID(envelope.DelegationID)
		a, err := h.machinePolicyGrantLocator(r.Context(), identity, grantID)
		if err != nil {
			writeJSON(w, 403, map[string]any{"error": "Delegation unavailable"})
			return
		}
		tx, err := h.db.Begin(r.Context())
		if err != nil {
			writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
			return
		}
		defer tx.Rollback(r.Context())
		err = h.authorizeMachinePolicyTx(r.Context(), tx, identity, target, &a, true)
		if err != nil {
			writeJSON(w, 403, map[string]any{"error": "Policy write denied"})
			return
		}
		expectedRevision, _ := positiveIDOrZero(envelope.ExpectedDelegationRevision)
		if a.revision != expectedRevision {
			writeJSON(w, 409, map[string]any{"error": "delegation_changed"})
			return
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO xinsight_policy_operations
 (installation_id,operation_ref,request_sha256,delegation_id,org_id,project_id,user_id,policy_ref,state)
 VALUES($1,$2,$3,$4,$5,NULLIF($6,0),NULLIF($7,0),$8,'pending') ON CONFLICT DO NOTHING`,
			identity.InstallationID, envelope.OperationRef, requestSHA, grantID, target.orgID, target.projectID, target.userID, put.ExpectedPolicyRef)
		if err != nil {
			writeJSON(w, 503, map[string]any{"error": "Operation claim failed"})
			return
		}
		var storedHash, state string
		var result []byte
		var storedOrg, storedProject, storedUser int64
		err = tx.QueryRow(r.Context(), `SELECT request_sha256,state,result,org_id,COALESCE(project_id,0),COALESCE(user_id,0)
 FROM xinsight_policy_operations WHERE installation_id=$1 AND operation_ref=$2 FOR UPDATE`, identity.InstallationID, envelope.OperationRef).
			Scan(&storedHash, &state, &result, &storedOrg, &storedProject, &storedUser)
		if err != nil {
			writeJSON(w, 503, map[string]any{"error": "Operation read failed"})
			return
		}
		if storedHash != requestSHA || storedOrg != target.orgID || storedProject != target.projectID || storedUser != target.userID {
			writeJSON(w, 409, map[string]any{"error": "operation_body_changed"})
			return
		}
		if state == "committed" {
			if err = tx.Commit(r.Context()); err != nil {
				writeJSON(w, 503, map[string]any{"error": "Operation read failed"})
				return
			}
			writeJSON(w, 200, json.RawMessage(result))
			return
		}
		var view contentcapture.ScopedManagementView
		if kind == contentcapture.PolicyScopeEnterprise {
			view, err = controller.PutEnterpriseTx(r.Context(), tx, target.orgID, put, application)
		} else {
			view, err = controller.PutScopedTx(r.Context(), tx, kind, target.orgID, target.projectID, target.userID, put, application)
		}
		if errors.Is(err, contentcapture.ErrPolicyVersionConflict) {
			writeJSON(w, 409, map[string]any{"error": "version_conflict"})
			return
		}
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": "Policy update rejected"})
			return
		}
		layer := machinePolicyLayer(view, kind)
		var auditID int64
		detail, _ := json.Marshal(map[string]any{"installation_id": identity.InstallationID, "delegation_id": envelope.DelegationID,
			"policy_ref": layer.PolicyRef, "operation_ref": envelope.OperationRef,
			"xi_operator_assertion": map[string]any{"kind": envelope.XIOperator.Kind, "subject_ref": envelope.XIOperator.SubjectRef, "xi_request_ref": envelope.XIOperator.XIRequestRef}})
		err = tx.QueryRow(r.Context(), `INSERT INTO audit_logs(org_id,project_id,user_id,actor_kind,actor_ref,delegation_id,
 action,resource,resource_id,detail,source,request_id) VALUES($1,NULLIF($2,0),NULL,'service',$3,$4,
 'content.policy.machine.update','payload_policy',$5,$6::jsonb,'xinsight_integration',$7) RETURNING id`,
			target.orgID, target.projectID, fmt.Sprintf("api_key:%d", identity.KeyID), grantID, layer.PolicyRef, detail, envelope.OperationRef).Scan(&auditID)
		if err != nil {
			writeJSON(w, 503, map[string]any{"error": "Audit write failed"})
			return
		}
		version, effective, status := "0", "0", layer.ApplicationStatus
		if layer.Desired != nil {
			version = layer.Desired.PolicyVersion
		}
		if layer.Effective != nil {
			effective = layer.Effective.PolicyVersion
		}
		response := map[string]any{"schema_version": "unirouter.xinsight.policy-write-result.v1",
			"operation_ref": envelope.OperationRef, "result": "committed", "source_instance": identity.SourceInstance,
			"policy_ref": layer.PolicyRef, "desired_version": version, "effective_version": effective,
			"application_status": status, "audit_ref": strconv.FormatInt(auditID, 10), "readback_path": target.path}
		wire, _ := json.Marshal(response)
		_, err = tx.Exec(r.Context(), `UPDATE xinsight_policy_operations SET state='committed',result=$3::jsonb,audit_log_id=$4
 WHERE installation_id=$1 AND operation_ref=$2`, identity.InstallationID, envelope.OperationRef, wire, auditID)
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			writeJSON(w, 503, map[string]any{"error": "Commit unconfirmed"})
			return
		}
		code := 200
		if status == "pending" {
			code = 202
		}
		writeJSON(w, code, response)
	}
}

func (h *Handler) GetXInsightMachinePolicyOperation(w http.ResponseWriter, r *http.Request) {
	_, _, identity, ok := h.machinePolicyReady(w, r)
	if !ok {
		return
	}
	ref := r.PathValue("operation_ref")
	if !machinePolicyOperationPattern.MatchString(ref) {
		writeJSON(w, 400, map[string]any{"error": "Invalid operation_ref"})
		return
	}
	var grantID, orgID, projectID, userID int64
	var result []byte
	err := h.db.QueryRow(r.Context(), `SELECT delegation_id,org_id,COALESCE(project_id,0),COALESCE(user_id,0),result
 FROM xinsight_policy_operations WHERE installation_id=$1 AND operation_ref=$2 AND state='committed'`, identity.InstallationID, ref).
		Scan(&grantID, &orgID, &projectID, &userID, &result)
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "Operation not found"})
		return
	}
	kind := contentcapture.PolicyScopeEnterprise
	if projectID > 0 {
		kind = contentcapture.PolicyScopeProject
	}
	if userID > 0 {
		kind = contentcapture.PolicyScopeUser
	}
	target := machinePolicyTarget{orgID: orgID, projectID: projectID, userID: userID, kind: kind}
	a, err := h.machinePolicyGrantLocator(r.Context(), identity, grantID)
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Operation denied"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	if err = h.authorizeMachinePolicyTx(r.Context(), tx, identity, target, &a, false); err != nil {
		writeJSON(w, 403, map[string]any{"error": "Operation denied"})
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeJSON(w, 503, map[string]any{"error": "Operation read failed"})
		return
	}
	writeJSON(w, 200, json.RawMessage(result))
}
