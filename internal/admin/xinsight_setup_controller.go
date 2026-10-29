package admin

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

// CheckXInsightSetupControlAuthority gates each original runtime send for a
// normal setup. Legacy installations keep their original controller semantics.
func (h *Handler) CheckXInsightSetupControlAuthority(ctx context.Context, id string) error {
	var present bool
	if e := h.db.QueryRow(ctx, `SELECT COALESCE(to_jsonb(i)->>'setup_request_id','')<>'' FROM xinsight_connector_installations i WHERE installation_id=$1`, id).Scan(&present); e != nil {
		return e
	}
	if !present {
		return nil
	}
	tx, e := h.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	s, _, state, e := readXInsightSavedSetupTx(ctx, tx, id, false)
	if e != nil {
		return e
	}
	if state == "needs_action" || state == "failed" {
		return xinsightconnector.ErrUnauthorized
	}
	actor, e := h.reauthorizeXInsightSetupActorTx(ctx, tx, s.ActorID, s.ActorEpoch, setupRequirement(s.OrgID, s.ProjectID))
	if e != nil || !auth.RoleIn(actor.OrganizationRole, auth.RolePlatformAdmin, auth.RoleOrgAdmin) {
		return xinsightconnector.ErrUnauthorized
	}
	var fence int64
	var desired, observed string
	e = tx.QueryRow(ctx, `SELECT control_fence,desired_state,observed_state FROM xinsight_connector_installations WHERE installation_id=$1 FOR SHARE`, id).Scan(&fence, &desired, &observed)
	if e != nil || fence != s.ControlFence || desired != "active" || observed == "disconnected" {
		return xinsightconnector.ErrConflict
	}
	return tx.Commit(ctx)
}

// AdvanceXInsightSetups shares the existing same-process connector loop. Each
// installation advances one bounded transaction; no remote request or browser
// session participates in the lock, and every mutation uses fresh human scope.
func (h *Handler) AdvanceXInsightSetups(ctx context.Context, limit int) error {
	if h.xInsightSetup == nil {
		return nil
	}
	if limit < 1 || limit > 32 {
		return xinsightconnector.ErrInvalid
	}
	rows, e := h.db.Query(ctx, `SELECT installation_id FROM xinsight_connector_installations WHERE setup_request_id IS NOT NULL AND setup_state IN ('provisioning','waiting_data','ready') ORDER BY updated_at,installation_id LIMIT $1`, limit)
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			break
		}
		ids = append(ids, id)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return e
	}
	var failures []error
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e = h.advanceXInsightSetup(ctx, id); e != nil {
			failures = append(failures, e)
		}
	}
	return errors.Join(failures...)
}
func (h *Handler) advanceXInsightSetup(ctx context.Context, id string) error {
	tx, e := h.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Read immutable consent before principal locks; lock installation only
	// after the original principal/project lock order has been established.
	saved, _, _, e := readXInsightSavedSetupTx(ctx, tx, id, false)
	if e != nil {
		return e
	}
	requirement := setupRequirement(saved.OrgID, saved.ProjectID)
	for _, access := range saved.PolicyAccess {
		if access.TargetKind == "user_exception" && access.Coverage == "exact" {
			user, _ := strconv.ParseInt(access.UserID, 10, 64)
			requirement.AdditionalUserIDs = append(requirement.AdditionalUserIDs, user)
		}
	}
	actor, e := h.reauthorizeXInsightSetupActorTx(ctx, tx, saved.ActorID, saved.ActorEpoch, requirement)
	if e == nil && !auth.RoleIn(actor.OrganizationRole, auth.RolePlatformAdmin, auth.RoleOrgAdmin) {
		e = errCommittingActorAuthorizationChanged
	}
	if e != nil {
		tx.Rollback(ctx)
		return h.blockXInsightSetup(ctx, id, "human_authority_changed")
	}
	saved, progress, state, e := readXInsightSavedSetupTx(ctx, tx, id, true)
	if e != nil {
		return e
	}
	if state == "needs_action" || state == "failed" {
		return nil
	}
	var fence int64
	var desired, observed, source, origin string
	e = tx.QueryRow(ctx, `SELECT i.control_fence,i.desired_state,i.observed_state,i.source_instance,COALESCE(b.redeemed_origin,'') FROM xinsight_connector_installations i LEFT JOIN xinsight_connector_control_bindings b USING(installation_id) WHERE i.installation_id=$1`, id).Scan(&fence, &desired, &observed, &source, &origin)
	issue := ""
	if e != nil {
		return e
	}
	if fence != saved.ControlFence || desired != "active" || observed == "disconnected" || h.sourceFactsController == nil || source != h.sourceFactsController.SourceInstance() {
		issue = "installation_authority_changed"
	}
	if origin == "" && issue == "" {
		return tx.Commit(ctx)
	}
	if origin != h.xInsightSetup.peer.XInsightOrigin && issue == "" {
		issue = "redeem_origin_changed"
	}
	if issue == "" && !progress.PolicyEffective {
		e = h.applyXInsightSetupPolicyTx(ctx, tx, saved, &progress)
		if e != nil {
			issue = "ur_policy_changed"
		}
	}
	if issue == "" && (len(saved.PolicyAccess) > 0 && progress.PolicyManageKey == 0 || progress.SourceEnrollKey == 0) {
		var summary xInsightConnectorInstallationSummary
		summary.InstallationID = id
		summary.ControlFence = fence
		summary.SourceInstance = source
		e = tx.QueryRow(ctx, `SELECT source_binding_sha256 FROM xinsight_connector_installations WHERE installation_id=$1`, id).Scan(&summary.SourceBindingSHA256)
		if e != nil {
			return e
		}
		// Typed public journal verification is shared with the human status page.
		candidates, err := readConnectorV3MachineCandidates(ctx, tx, &summary, saved.OrgID, saved.ProjectID)
		if err != nil {
			issue = "machine_candidate_changed"
		} else {
			for _, candidate := range candidates {
				if candidate.Purpose != policyManagePurpose && candidate.Purpose != sourceEnrollPurpose {
					continue
				}
				if candidate.Purpose == policyManagePurpose && len(saved.PolicyAccess) == 0 {
					continue
				}
				key := &progress.PolicyManageKey
				if candidate.Purpose == sourceEnrollPurpose {
					key = &progress.SourceEnrollKey
				}
				if *key > 0 {
					continue
				}
				var acknowledged bool
				if e = tx.QueryRow(ctx, `SELECT acknowledged_at IS NOT NULL FROM xinsight_connector_phase_operations WHERE operation_id=$1 AND installation_id=$2 AND control_fence=$3`, candidate.ControlOperationID, id, fence).Scan(&acknowledged); e != nil {
					return e
				}
				if !acknowledged {
					continue
				}

				if candidate.Registration.AllowedIP != h.xInsightSetup.peer.AllowedIP {
					issue = "machine_network_changed"
					break
				}
				input := machineCredentialRegistration(candidate.Registration)
				meta := setupAuditMeta(saved)
				outcome, err := h.registerXInsightMachineCredentialTx(ctx, tx, saved.OrgID, input, saved.ActorID, meta)
				if err == nil {
					_, err = h.changeXInsightMachineCredentialTx(ctx, tx, saved.OrgID, saved.ProjectID, outcome.KeyID, id, candidate.Purpose, true, true, saved.ActorID, meta)
				}
				if err != nil {
					issue = "machine_activation_conflict"
					break
				}
				*key = outcome.KeyID
			}
		}
	}
	if issue == "" && progress.PolicyManageKey > 0 && len(progress.PolicyGrantIDs) < len(saved.PolicyAccess) {
		access := saved.PolicyAccess[len(progress.PolicyGrantIDs)]
		input := setupPolicyDelegation(id, strconv.FormatInt(saved.ProjectID, 10), saved.Coverage, access)
		project, user, expires, valid := input.scope()
		if !valid {
			issue = "policy_grant_invalid"
		} else {
			req := committingActorRequirement{TargetOrgID: saved.OrgID, OrganizationForUpdate: true, AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin}}
			if input.Coverage == "exact" && input.TargetKind != "enterprise_default" {
				req.Project = &committingActorProjectRequirement{ProjectID: project, AllowedRoles: []string{projectMemberAdmin}, MustBeActive: true}
				if user > 0 {
					req.AdditionalUserIDs = []int64{user}
				}
			}
			if _, e = h.reauthorizeXInsightSetupActorTx(ctx, tx, saved.ActorID, saved.ActorEpoch, req); e != nil {
				issue = "policy_grant_authority_changed"
			} else {
				var prior int64
				e = tx.QueryRow(ctx, `SELECT id FROM xinsight_policy_delegations WHERE installation_id=$1 AND state='active' AND target_kind=$2 AND coverage=$3 AND COALESCE(exact_project_id,0)=$4 AND COALESCE(exact_user_id,0)=$5 AND can_read=$6 AND can_write=$7 AND grantor_user_id=$8 AND expires_at IS NULL FOR UPDATE`, id, input.TargetKind, input.Coverage, project, user, input.Read, input.Write, saved.ActorID).Scan(&prior)
				if errors.Is(e, pgx.ErrNoRows) {
					prior, e = h.createXInsightPolicyDelegationTx(ctx, tx, saved.OrgID, input, project, user, expires, saved.ActorID, setupAuditMeta(saved))
				}
				if e != nil {
					issue = "policy_grant_conflict"
				} else {
					progress.PolicyGrantIDs = append(progress.PolicyGrantIDs, prior)
				}
			}
		}
	}
	if issue == "" && progress.SourceEnrollKey > 0 && progress.SourceGrantID == 0 {
		input := sourceEnrollmentInput{InstallationID: id, Coverage: saved.Coverage, Profiles: saved.Profiles, IncludeContent: saved.IncludeContent}
		if saved.Coverage == "exact" {
			input.ProjectID = strconv.FormatInt(saved.ProjectID, 10)
		}
		project, expiry, valid := input.scope()
		if !valid || !h.sourceFactsController.ProfilesEnabled(saved.Profiles) {
			issue = "source_qualification_changed"
		} else {
			var overlaps int
			e = tx.QueryRow(ctx, `SELECT count(*) FROM xinsight_source_enrollments WHERE org_id=$1 AND state='active' AND (coverage='all_current_future' OR $2='all_current_future' OR exact_project_id=$3)`, saved.OrgID, saved.Coverage, project).Scan(&overlaps)
			if e != nil {
				return e
			}
			if overlaps > 0 {
				issue = "source_enrollment_conflict"
			} else {
				progress.SourceGrantID, e = h.createXInsightSourceEnrollmentTx(ctx, tx, saved.OrgID, input, project, expiry, saved.ActorID, setupAuditMeta(saved))
				if e != nil {
					issue = "source_enrollment_conflict"
				}
				if e == nil {
					for _, raw := range saved.ExcludedProjectIDs {
						project, _ := strconv.ParseInt(raw, 10, 64)
						e = putXInsightSourceEnrollmentExclusionTx(ctx, tx, progress.SourceGrantID, saved.OrgID, project, 0, true, saved.ActorID, setupAuditMeta(saved))
						if e != nil {
							return e
						}
					}
				}
			}
		}
	}
	if issue == "" && progress.SourceGrantID > 0 && !progress.SourceBindingsDone {
		if e = h.bindXInsightSetupExistingSourcesTx(ctx, tx, id, saved, progress.SourceGrantID); e != nil {
			issue = "existing_source_changed"
		} else {
			progress.SourceBindingsDone = true
		}
	}
	if issue != "" { // Roll back all newly attempted writes before marking blocked.
		tx.Rollback(ctx)
		return h.blockXInsightSetup(ctx, id, issue)
	}
	// Existing durable results are checked against current actual state, never
	// assumed from a saved progress integer after revocation or replacement.
	if (progress.PolicyManageKey > 0 || len(saved.PolicyAccess) == 0) && progress.SourceEnrollKey > 0 {
		var active int
		e = tx.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE id=ANY($1::bigint[]) AND org_id=$2 AND project_id=$3 AND status='feed_active'`, []int64{progress.PolicyManageKey, progress.SourceEnrollKey}, saved.OrgID, saved.ProjectID).Scan(&active)
		if e != nil {
			return e
		}
		expected := 2
		if len(saved.PolicyAccess) == 0 {
			expected = 1
		}
		if active != expected {
			tx.Rollback(ctx)
			return h.blockXInsightSetup(ctx, id, "machine_authority_revoked")
		}
	}

	if len(progress.PolicyGrantIDs) > 0 {
		var active int
		e = tx.QueryRow(ctx, `SELECT count(*) FROM xinsight_policy_delegations WHERE id=ANY($1::bigint[]) AND installation_id=$2 AND state='active' AND (expires_at IS NULL OR expires_at>clock_timestamp())`, progress.PolicyGrantIDs, id).Scan(&active)
		if e != nil {
			return e
		}
		if active != len(progress.PolicyGrantIDs) {
			tx.Rollback(ctx)
			return h.blockXInsightSetup(ctx, id, "policy_grant_changed")
		}
	}
	if progress.SourceGrantID > 0 {
		var active bool
		e = tx.QueryRow(ctx, `SELECT state='active' AND (expires_at IS NULL OR expires_at>clock_timestamp()) FROM xinsight_source_enrollments WHERE id=$1 AND installation_id=$2`, progress.SourceGrantID, id).Scan(&active)
		if e != nil {
			return e
		}
		if !active {
			tx.Rollback(ctx)
			return h.blockXInsightSetup(ctx, id, "source_grant_changed")
		}
	}
	next := "provisioning"
	if progress.PolicyEffective && progress.SourceBindingsDone && progress.SourceGrantID > 0 && len(progress.PolicyGrantIDs) == len(saved.PolicyAccess) {
		next = "waiting_data"
	}
	raw, _ := json.Marshal(progress)
	_, e = tx.Exec(ctx, `UPDATE xinsight_connector_installations SET setup_state=$2,setup_progress=$3::jsonb,updated_at=clock_timestamp() WHERE installation_id=$1 AND (setup_state IS DISTINCT FROM $2 OR setup_progress IS DISTINCT FROM $3::jsonb)`, id, next, raw)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func setupAuditMeta(s xInsightSavedSetup) auditMeta {
	return auditMeta{OrgID: s.OrgID, ProjectID: s.ProjectID, RequestID: s.RequestID, ClientRequestID: s.RequestID, Source: "system"}
}
func (h *Handler) blockXInsightSetup(ctx context.Context, id, issue string) error {
	_, e := h.db.Exec(ctx, `UPDATE xinsight_connector_installations SET setup_state='needs_action',setup_progress=jsonb_set(setup_progress,'{issue}',to_jsonb($2::text)),updated_at=clock_timestamp() WHERE installation_id=$1 AND setup_request_id IS NOT NULL`, id, issue)
	return e
}
func (h *Handler) applyXInsightSetupPolicyTx(ctx context.Context, tx pgx.Tx, s xInsightSavedSetup, p *xInsightSetupProgress) error {
	controller, ok := h.contentPolicyController.(scopedContentPolicyController)
	if !ok || h.contentPolicyReadiness == nil {
		return xinsightconnector.ErrUnavailable
	}
	app, e := h.contentPolicyReadiness(ctx)
	if e != nil {
		return e
	}
	app.Now = time.Now().UTC()
	view, e := controller.ScopedManagementTx(ctx, tx, s.OrgID, s.ProjectID, 0, app.StorageStatus)
	if e != nil {
		return e
	}
	if view.VersionVector != s.PolicyVector {
		return contentcapture.ErrPolicyVersionConflict
	}
	if s.URPolicy != nil {
		view, e = h.putScopedPolicyForActorTx(ctx, tx, controller, s.URPolicy.Scope, s.OrgID, s.ProjectID, 0, s.URPolicy.Put, app, s.ActorID, setupAuditMeta(s))
		if e != nil {
			return e
		}
	}
	view, e = controller.ScopedManagementTx(ctx, tx, s.OrgID, s.ProjectID, 0, app.StorageStatus)
	if e != nil {
		return e
	}

	if s.IncludeContent {
		if view.Effective == nil || view.Effective.Effective.Config.Mode != "full_content" || view.Effective.ExecutionStorageMode != contentcapture.ExecutionStorageTemporary {
			return xinsightconnector.ErrConflict
		}
		if s.Coverage == "all_current_future" && (view.Enterprise.Effective == nil || view.Enterprise.Effective.Config.Mode != "full_content" || view.Enterprise.EffectiveExecutionMode != contentcapture.ExecutionStorageTemporary) {
			return xinsightconnector.ErrConflict
		}
	}
	p.PolicyEffective = true
	p.PolicyVector = view.VersionVector
	if view.Effective != nil {
		p.PolicyRef = view.Effective.PolicyRef
		p.PolicyVersion = view.Effective.Effective.PolicyVersion
	} else if view.Enterprise.Effective != nil {
		p.PolicyRef = view.Enterprise.PolicyRef
		p.PolicyVersion = view.Enterprise.Effective.PolicyVersion
	}
	return nil
}

func (h *Handler) bindXInsightSetupExistingSourcesTx(ctx context.Context, tx pgx.Tx, id string, s xInsightSavedSetup, grant int64) error {
	for _, prior := range s.ExistingSourceStates {
		if prior.Kind == "content" && !s.IncludeContent {
			continue
		}
		candidate, profiles, content, e := h.bindExistingAuthorityTx(ctx, tx, s.OrgID, s.ProjectID, grant, s.ActorID)
		if e != nil {
			return e
		}
		candidate.Kind = prior.Kind
		current, e := readBindExistingCandidateTx(ctx, tx, candidate)
		if e != nil {
			return e
		}
		observed := current
		observed.EnrollmentID = ""
		observed.EnrollmentRevision = ""
		observed.StateSHA256 = ""
		if bindDigest(struct{ bindExistingCandidate }{observed}) != prior.StateSHA256 || len(current.Keys) != 1 || current.Keys[0].AllowedIP != h.xInsightSetup.peer.AllowedIP {
			return xinsightconnector.ErrConflict
		}
		step := xinsightconnector.PublicCanonicalSHA256(struct {
			Request string
			Kind    string
		}{s.RequestID, prior.Kind})[7:39]
		input := bindExistingInput{SchemaVersion: "unirouter.xinsight.bind-existing.v1", Kind: prior.Kind, CredentialID: current.Keys[0].CredentialID, ExpectedStateSHA256: current.StateSHA256, OperationID: step}
		if _, e = bindExistingXInsightSourceEnrollmentTx(ctx, tx, candidate, profiles, content, input, s.ActorID, setupAuditMeta(s)); e != nil {
			return e
		}
	}
	return nil
}
