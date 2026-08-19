package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
)

func (h *Handler) GetModelAuthorizationContract(w http.ResponseWriter, _ *http.Request) {
	environment := ""
	if h.modelAuthorizationEvaluator != nil {
		environment = h.modelAuthorizationEvaluator.Environment()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"contract_version":      modelauthz.ContractVersion,
		"environment":           environment,
		"subjects":              []string{modelauthz.SubjectAPIKey, modelauthz.SubjectAccessCredential, modelauthz.SubjectProject, modelauthz.SubjectUser, modelauthz.SubjectAgent},
		"effects":               []string{modelauthz.EffectAllow, modelauthz.EffectDeny},
		"endpoints":             modelauthz.SupportedEndpoints(),
		"precedence":            "explicit_deny_then_allow_subject_intersection_then_legacy_authority",
		"authority":             "narrow_only",
		"tool_or_mcp_authority": "none",
		"runtime_mutation":      "none",
	})
}

func (h *Handler) ListModelAuthorizations(w http.ResponseWriter, r *http.Request) {
	requestedOrg, _ := strconv.ParseInt(r.URL.Query().Get("org_id"), 10, 64)
	orgID, err := resolveModelAuthorizationOrg(r, requestedOrg)
	if err != nil {
		writeModelAuthorizationError(w, err)
		return
	}
	status := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("status")))
	if status != "" && status != modelauthz.StatusDraft && status != modelauthz.StatusActive && status != modelauthz.StatusRevoked {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid authorization status"})
		return
	}
	rows, err := h.db.Query(r.Context(), `
SELECT id, org_id, authorization_ref, subject_type, subject_ref, environment,
       effect, allowed_models, allowed_endpoints, not_before, expires_at,
       status, revision, reason, created_by, activated_by, revoked_by,
       created_at, activated_at, revoked_at, updated_at
  FROM model_call_authorizations
 WHERE org_id=$1 AND ($2='' OR status=$2)
 ORDER BY updated_at DESC, id DESC LIMIT 200`, orgID, status)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization store unavailable"})
		return
	}
	defer rows.Close()
	items := make([]modelauthz.Policy, 0)
	for rows.Next() {
		item, scanErr := scanModelAuthorization(rows)
		if scanErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization store unavailable"})
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization store unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": len(items)})
}

func (h *Handler) CreateModelAuthorization(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OrgID int64 `json:"org_id"`
		modelauthz.Draft
	}
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid model authorization request"})
		return
	}
	orgID, err := resolveModelAuthorizationOrg(r, request.OrgID)
	if err != nil {
		writeModelAuthorizationError(w, err)
		return
	}
	if strings.TrimSpace(request.AuthorizationRef) == "" {
		request.AuthorizationRef, err = newModelAuthorizationRef()
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Authorization identity generation unavailable"})
			return
		}
	}
	draft, err := modelauthz.NormalizeDraft(request.Draft)
	if err != nil {
		writeModelAuthorizationError(w, err)
		return
	}
	draft.SubjectType, draft.SubjectRef, err = canonicalizeAuthorizationSubject(r.Context(), h.db, orgID, draft.SubjectType, draft.SubjectRef)
	if err != nil {
		writeModelAuthorizationError(w, err)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization store unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizePolicySelectorMutationTx(r.Context(), tx, policySelector{OrgID: int64Pointer(orgID)}); err != nil {
		writeModelAuthorizationCommitError(w, err)
		return
	}
	if err := lockAuthorizationSubjectTx(r.Context(), tx, orgID, draft.SubjectType, draft.SubjectRef); err != nil {
		writeModelAuthorizationError(w, err)
		return
	}
	modelsJSON, _ := json.Marshal(draft.AllowedModels)
	endpointsJSON, _ := json.Marshal(draft.AllowedEndpoints)
	actor := middleware.GetUserIDFromContext(r.Context())
	var id int64
	err = tx.QueryRow(r.Context(), `
INSERT INTO model_call_authorizations (
    org_id, authorization_ref, subject_type, subject_ref, environment, effect,
    allowed_models, allowed_endpoints, not_before, expires_at, status, revision,
    reason, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10,'draft',1,$11,$12)
RETURNING id`, orgID, draft.AuthorizationRef, draft.SubjectType, draft.SubjectRef,
		draft.Environment, draft.Effect, string(modelsJSON), string(endpointsJSON),
		draft.NotBefore, draft.ExpiresAt, draft.Reason, actor).Scan(&id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Authorization reference already exists"})
		} else {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization store unavailable"})
		}
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "model_authorization.create", "model_authorization", draft.AuthorizationRef,
		map[string]interface{}{"after": map[string]interface{}{"org_id": orgID, "status": modelauthz.StatusDraft, "subject_type": draft.SubjectType, "subject_ref": draft.SubjectRef, "effect": draft.Effect}},
		requestAuditMeta(r, orgID, 0)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization audit unavailable"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization store unavailable"})
		return
	}
	item, err := getModelAuthorization(r.Context(), h.db, orgID, draft.AuthorizationRef, false)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization store unavailable"})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func newModelAuthorizationRef() (string, error) {
	var entropy [12]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return "mca_" + hex.EncodeToString(entropy[:]), nil
}

func (h *Handler) ActivateModelAuthorization(w http.ResponseWriter, r *http.Request) {
	h.transitionModelAuthorization(w, r, modelauthz.StatusDraft, modelauthz.StatusActive)
}

func (h *Handler) RevokeModelAuthorization(w http.ResponseWriter, r *http.Request) {
	h.transitionModelAuthorization(w, r, modelauthz.StatusActive, modelauthz.StatusRevoked)
}

func (h *Handler) transitionModelAuthorization(w http.ResponseWriter, r *http.Request, from, to string) {
	var request struct {
		OrgID            int64 `json:"org_id"`
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if err := decodeStrictJSONBody(r, &request); err != nil || request.ExpectedRevision <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid model authorization transition"})
		return
	}
	orgID, err := resolveModelAuthorizationOrg(r, request.OrgID)
	if err != nil {
		writeModelAuthorizationError(w, err)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization store unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := h.reauthorizePolicySelectorMutationTx(r.Context(), tx, policySelector{OrgID: int64Pointer(orgID)}); err != nil {
		writeModelAuthorizationCommitError(w, err)
		return
	}
	ref := strings.TrimSpace(strings.ToLower(r.PathValue("ref")))
	before, err := getModelAuthorization(r.Context(), tx, orgID, ref, true)
	if err != nil {
		writeModelAuthorizationError(w, err)
		return
	}
	if before.Status != from || before.Revision != request.ExpectedRevision {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Authorization changed since it was loaded"})
		return
	}
	if to == modelauthz.StatusActive {
		if err := lockAuthorizationSubjectTx(r.Context(), tx, orgID, before.SubjectType, before.SubjectRef); err != nil {
			writeModelAuthorizationError(w, err)
			return
		}
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	var query string
	if to == modelauthz.StatusActive {
		query = `UPDATE model_call_authorizations SET status='active', revision=revision+1,
                 activated_by=$1, activated_at=NOW(), updated_at=NOW()
                 WHERE id=$2 AND status='draft' AND revision=$3`
	} else {
		query = `UPDATE model_call_authorizations SET status='revoked', revision=revision+1,
                 revoked_by=$1, revoked_at=NOW(), updated_at=NOW()
                 WHERE id=$2 AND status='active' AND revision=$3`
	}
	tag, err := tx.Exec(r.Context(), query, actor, before.ID, request.ExpectedRevision)
	if err != nil || tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Authorization changed since it was loaded"})
		return
	}
	action := "model_authorization." + to
	if err := insertAuditEntry(r.Context(), tx, action, "model_authorization", ref,
		map[string]interface{}{"before": map[string]interface{}{"status": from, "revision": before.Revision}, "after": map[string]interface{}{"status": to, "revision": before.Revision + 1}},
		requestAuditMeta(r, orgID, 0)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization audit unavailable"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization store unavailable"})
		return
	}
	item, err := getModelAuthorization(r.Context(), h.db, orgID, ref, false)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization store unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func lockAuthorizationSubjectTx(ctx context.Context, tx pgx.Tx, orgID int64, subjectType, subjectRef string) error {
	if subjectType == modelauthz.SubjectAgent {
		return nil
	}
	id, err := strconv.ParseInt(subjectRef, 10, 64)
	if err != nil || id <= 0 {
		return modelauthz.ErrInvalid
	}
	var found int64
	switch subjectType {
	case modelauthz.SubjectAPIKey:
		err = tx.QueryRow(ctx, `SELECT id FROM api_keys WHERE id=$1 AND org_id=$2 AND access_credential_id IS NULL FOR SHARE`, id, orgID).Scan(&found)
	case modelauthz.SubjectAccessCredential:
		err = tx.QueryRow(ctx, `
			SELECT credential.id
			FROM access_credentials credential
			JOIN projects project ON project.id=credential.project_id AND project.org_id=credential.org_id
			WHERE credential.id=$1 AND credential.org_id=$2
			  AND credential.status='active' AND project.status='active'
			FOR SHARE OF credential,project`, id, orgID).Scan(&found)
	case modelauthz.SubjectProject:
		err = tx.QueryRow(ctx, `SELECT id FROM projects WHERE id=$1 AND org_id=$2 FOR SHARE`, id, orgID).Scan(&found)
	case modelauthz.SubjectUser:
		err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 AND org_id=$2 FOR SHARE`, id, orgID).Scan(&found)
	default:
		return modelauthz.ErrInvalid
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("model authorization subject is outside organization")
	}
	if err != nil {
		return fmt.Errorf("%w: subject lookup", modelauthz.ErrUnavailable)
	}
	return nil
}

func resolveModelAuthorizationOrg(r *http.Request, requested int64) (int64, error) {
	if _, present := middleware.GetCustomerContextFromContext(r.Context()); present {
		orgID, err := resolveActorOrganization(r.Context(), requested)
		if err != nil {
			return 0, errors.New("model authorization organization forbidden")
		}
		return orgID, nil
	}
	role := actorOrganizationRole(r.Context())
	contextOrg := middleware.GetOrgIDFromContext(r.Context())
	if role == auth.RolePlatformAdmin {
		if requested <= 0 {
			return 0, fmt.Errorf("%w: org_id required", modelauthz.ErrInvalid)
		}
		return requested, nil
	}
	if contextOrg <= 0 || (requested > 0 && requested != contextOrg) {
		return 0, errors.New("model authorization organization forbidden")
	}
	return contextOrg, nil
}

func validateAuthorizationSubject(ctx context.Context, rower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, orgID int64, subjectType, subjectRef string) error {
	if subjectType == modelauthz.SubjectAgent {
		return nil
	}
	id, err := strconv.ParseInt(subjectRef, 10, 64)
	if err != nil || id <= 0 {
		return modelauthz.ErrInvalid
	}
	var exists bool
	switch subjectType {
	case modelauthz.SubjectAPIKey:
		err = rower.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM api_keys WHERE id=$1 AND org_id=$2)`, id, orgID).Scan(&exists)
	case modelauthz.SubjectAccessCredential:
		err = rower.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM access_credentials WHERE id=$1 AND org_id=$2)`, id, orgID).Scan(&exists)
	case modelauthz.SubjectProject:
		err = rower.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM projects WHERE id=$1 AND org_id=$2)`, id, orgID).Scan(&exists)
	case modelauthz.SubjectUser:
		err = rower.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id=$1 AND org_id=$2)`, id, orgID).Scan(&exists)
	default:
		return modelauthz.ErrInvalid
	}
	if err != nil {
		return fmt.Errorf("%w: subject lookup", modelauthz.ErrUnavailable)
	}
	if !exists {
		return errors.New("model authorization subject is outside organization")
	}
	return nil
}

func canonicalizeAuthorizationSubject(ctx context.Context, rower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, orgID int64, subjectType, subjectRef string) (string, string, error) {
	if subjectType != modelauthz.SubjectAPIKey {
		return subjectType, subjectRef, validateAuthorizationSubject(ctx, rower, orgID, subjectType, subjectRef)
	}
	keyID, err := strconv.ParseInt(subjectRef, 10, 64)
	if err != nil || keyID <= 0 {
		return "", "", modelauthz.ErrInvalid
	}
	var accessCredentialID *int64
	if err := rower.QueryRow(ctx, `SELECT access_credential_id FROM api_keys WHERE id=$1 AND org_id=$2`, keyID, orgID).Scan(&accessCredentialID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", errors.New("model authorization subject is outside organization")
		}
		return "", "", fmt.Errorf("%w: subject lookup", modelauthz.ErrUnavailable)
	}
	if accessCredentialID != nil && *accessCredentialID > 0 {
		return modelauthz.SubjectAccessCredential, strconv.FormatInt(*accessCredentialID, 10), nil
	}
	return modelauthz.SubjectAPIKey, subjectRef, nil
}

func getModelAuthorization(ctx context.Context, rower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, orgID int64, ref string, lock bool) (modelauthz.Policy, error) {
	query := `SELECT id, org_id, authorization_ref, subject_type, subject_ref, environment,
       effect, allowed_models, allowed_endpoints, not_before, expires_at,
       status, revision, reason, created_by, activated_by, revoked_by,
       created_at, activated_at, revoked_at, updated_at
  FROM model_call_authorizations WHERE org_id=$1 AND authorization_ref=$2`
	if lock {
		query += " FOR UPDATE"
	}
	item, err := scanModelAuthorization(rower.QueryRow(ctx, query, orgID, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return modelauthz.Policy{}, errors.New("model authorization not found")
	}
	return item, err
}

func scanModelAuthorization(row interface{ Scan(...interface{}) error }) (modelauthz.Policy, error) {
	var item modelauthz.Policy
	var modelsJSON, endpointsJSON []byte
	err := row.Scan(
		&item.ID, &item.OrgID, &item.AuthorizationRef, &item.SubjectType, &item.SubjectRef,
		&item.Environment, &item.Effect, &modelsJSON, &endpointsJSON,
		&item.NotBefore, &item.ExpiresAt, &item.Status, &item.Revision, &item.Reason,
		&item.CreatedBy, &item.ActivatedBy, &item.RevokedBy, &item.CreatedAt,
		&item.ActivatedAt, &item.RevokedAt, &item.UpdatedAt,
	)
	if err != nil {
		return modelauthz.Policy{}, err
	}
	if json.Unmarshal(modelsJSON, &item.AllowedModels) != nil || json.Unmarshal(endpointsJSON, &item.AllowedEndpoints) != nil {
		return modelauthz.Policy{}, modelauthz.ErrUnavailable
	}
	return item, nil
}

func writeModelAuthorizationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, modelauthz.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid model authorization", "message": err.Error()})
	case errors.Is(err, modelauthz.ErrUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization store unavailable"})
	case strings.Contains(strings.ToLower(err.Error()), "not found"):
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model authorization not found"})
	default:
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Model authorization scope forbidden"})
	}
}

func writeModelAuthorizationCommitError(w http.ResponseWriter, err error) {
	if errors.Is(err, errPolicySelectorNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model authorization scope not found"})
		return
	}
	if errors.Is(err, errLimitPolicyForbidden) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Model authorization scope forbidden"})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model authorization store unavailable"})
}
