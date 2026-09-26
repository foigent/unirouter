package admin

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/enterprisefinance"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
)

type sourceBudgetBindingRequest struct {
	OrgID            int64  `json:"org_id"`
	ProjectID        int64  `json:"project_id"`
	ScopeKind        string `json:"scope_kind"`
	ScopeRef         string `json:"scope_ref"`
	ExpectedRevision string `json:"expected_revision"`
	Status           string `json:"status"`
}

type sourceBudgetBinding struct {
	ScopeKind string `json:"scope_kind"`
	ScopeRef  string `json:"scope_ref"`
	ProjectID string `json:"project_id"`
	Revision  string `json:"revision"`
	Status    string `json:"status"`
	AuditRef  string `json:"audit_ref"`
	ChangedAt string `json:"changed_at"`
}

func (h *Handler) SetSourceBudgetBinding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if middleware.GetUserIDFromContext(r.Context()) <= 0 || !auth.RoleIn(actorOrganizationRole(r.Context()), auth.RolePlatformAdmin, auth.RoleBillingAdmin) {
		writeJSON(w, 403, map[string]any{"error": "Forbidden"})
		return
	}
	var request sourceBudgetBindingRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	expected, err := strconv.ParseInt(request.ExpectedRevision, 10, 64)
	if err != nil || expected < 0 || strconv.FormatInt(expected, 10) != request.ExpectedRevision || request.ProjectID <= 0 || request.ScopeKind != enterprisefinance.ScopeProject || request.ScopeRef == "" || len(request.ScopeRef) > 160 || strings.TrimSpace(request.ScopeRef) != request.ScopeRef || strings.ContainsAny(request.ScopeRef, "\x00\r\n") || (request.Status != "active" && request.Status != "revoked") || expected == 0 && request.Status != "active" {
		writeJSON(w, 400, map[string]any{"error": "Invalid explicit project budget binding"})
		return
	}
	org, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	if h.outbox == nil {
		writeJSON(w, 503, map[string]any{"error": "Audit outbox is unavailable"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Budget binding unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = h.reauthorizeCommittingActorTx(r.Context(), tx, committingActorRequirement{TargetOrgID: org, AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleBillingAdmin}, Project: &committingActorProjectRequirement{ProjectID: request.ProjectID, MustBeActive: true}}); err != nil {
		writeSourceBudgetAuthorizationError(w, err)
		return
	}
	var previousProject, revision int64
	var previousStatus string
	err = tx.QueryRow(r.Context(), `SELECT project_id,revision,status FROM execution_source_budget_bindings WHERE org_id=$1 AND enterprise_project_scope_ref=$2 FOR UPDATE`, org, request.ScopeRef).Scan(&previousProject, &revision, &previousStatus)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 503, map[string]any{"error": "Budget binding unavailable"})
		return
	}
	if revision != expected {
		writeJSON(w, 409, map[string]any{"error": "Budget binding revision changed"})
		return
	}
	next := revision + 1
	if next <= 0 {
		writeJSON(w, 409, map[string]any{"error": "Budget binding revision exhausted"})
		return
	}
	// The reference is the original audit event's idempotency key. Both it and
	// the visibility revision commit in this same transaction.
	identity := fmt.Sprintf("source-budget-binding:%d:%s:%d", org, request.ScopeRef, next)
	auditRef := fmt.Sprintf("audit:%x", sha256.Sum256([]byte(identity)))
	var changed time.Time
	if revision == 0 {
		err = tx.QueryRow(r.Context(), `INSERT INTO execution_source_budget_bindings(org_id,enterprise_project_scope_ref,project_id,revision,status,audit_ref)
 VALUES($1,$2,$3,1,$4,$5) ON CONFLICT DO NOTHING RETURNING changed_at`, org, request.ScopeRef, request.ProjectID, request.Status, auditRef).Scan(&changed)
	} else {
		err = tx.QueryRow(r.Context(), `UPDATE execution_source_budget_bindings SET project_id=$3,revision=$4,status=$5,audit_ref=$6,changed_at=clock_timestamp()
 WHERE org_id=$1 AND enterprise_project_scope_ref=$2 AND revision=$7 RETURNING changed_at`, org, request.ScopeRef, request.ProjectID, next, request.Status, auditRef, expected).Scan(&changed)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 409, map[string]any{"error": "Budget binding revision changed"})
		return
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Budget binding unavailable"})
		return
	}
	result := sourceBudgetBinding{ScopeKind: "project", ScopeRef: request.ScopeRef, ProjectID: strconv.FormatInt(request.ProjectID, 10), Revision: strconv.FormatInt(next, 10), Status: request.Status, AuditRef: auditRef, ChangedAt: changed.UTC().Format(time.RFC3339Nano)}
	before := map[string]any{"project_id": strconv.FormatInt(previousProject, 10), "revision": strconv.FormatInt(revision, 10), "status": previousStatus}
	afterJSON, _ := json.Marshal(result)
	beforeJSON, _ := json.Marshal(before)
	detailJSON, _ := json.Marshal(map[string]any{"before": before, "after": result})
	meta := requestAuditMeta(r, org, request.ProjectID)
	payload := auditOutboxPayload{OrgID: org, ProjectID: positiveAuditID(request.ProjectID), UserID: middleware.GetUserIDFromContext(r.Context()), Action: "enterprise_finance.source_budget_binding", Resource: "source_budget_binding", ResourceID: request.ScopeRef, Detail: detailJSON, BeforeState: beforeJSON, AfterState: afterJSON, IP: meta.IP, UserAgent: meta.UserAgent, RequestID: meta.RequestID, ClientRequestID: meta.ClientRequestID, Source: normalizeAuditSource(meta.Source), OccurredAt: changed}
	event, err := outbox.NewEvent(outbox.EventAudit, auditRef, outbox.PriorityAudit, org, meta.RequestID, payload, changed)
	if err == nil {
		_, _, err = outbox.EnqueueTx(r.Context(), tx, event)
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Budget binding audit unavailable"})
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeJSON(w, 503, map[string]any{"error": "Budget binding unavailable"})
		return
	}
	writeJSON(w, 200, result)
}

func (h *Handler) ListSourceBudgetBindings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if middleware.GetUserIDFromContext(r.Context()) <= 0 || !auth.RoleIn(actorOrganizationRole(r.Context()), auth.RolePlatformAdmin, auth.RoleBillingAdmin, auth.RoleOrgAdmin) {
		writeJSON(w, 403, map[string]any{"error": "Forbidden"})
		return
	}
	org, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	project, err := strconv.ParseInt(r.URL.Query().Get("project_id"), 10, 64)
	if err != nil || project <= 0 {
		writeJSON(w, 400, map[string]any{"error": "Exact project_id is required"})
		return
	}
	var valid bool
	if err = h.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM projects WHERE org_id=$1 AND id=$2)`, org, project).Scan(&valid); err != nil {
		writeJSON(w, 503, map[string]any{"error": "Budget binding unavailable"})
		return
	}
	if !valid {
		writeJSON(w, 404, map[string]any{"error": "Project not found"})
		return
	}
	rows, err := h.db.Query(r.Context(), `SELECT enterprise_project_scope_ref,revision,status,audit_ref,changed_at FROM execution_source_budget_bindings
 WHERE org_id=$1 AND project_id=$2 AND enterprise_project_scope_ref COLLATE "C">$3 COLLATE "C" ORDER BY enterprise_project_scope_ref COLLATE "C" LIMIT 51`, org, project, r.URL.Query().Get("after"))
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Budget binding unavailable"})
		return
	}
	defer rows.Close()
	items := []sourceBudgetBinding{}
	for rows.Next() {
		var item sourceBudgetBinding
		var revision int64
		var changed time.Time
		if err = rows.Scan(&item.ScopeRef, &revision, &item.Status, &item.AuditRef, &changed); err != nil {
			writeJSON(w, 503, map[string]any{"error": "Budget binding unavailable"})
			return
		}
		item.ScopeKind = "project"
		item.ProjectID = strconv.FormatInt(project, 10)
		item.Revision = strconv.FormatInt(revision, 10)
		item.ChangedAt = changed.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeJSON(w, 503, map[string]any{"error": "Budget binding unavailable"})
		return
	}
	more := len(items) > 50
	next := ""
	if more {
		items = items[:50]
		next = items[len(items)-1].ScopeRef
	}
	writeJSON(w, 200, map[string]any{"data": items, "has_more": more, "next_after": next, "org_id": strconv.FormatInt(org, 10), "project_id": strconv.FormatInt(project, 10)})
}

func writeSourceBudgetAuthorizationError(w http.ResponseWriter, err error) {
	if errors.Is(err, errCommittingActorScopeNotFound) {
		writeJSON(w, 404, map[string]any{"error": "Project not found"})
		return
	}
	if errors.Is(err, errCommittingActorAuthorizationChanged) {
		writeJSON(w, 403, map[string]any{"error": "Forbidden"})
		return
	}
	writeJSON(w, 503, map[string]any{"error": "Budget binding authorization unavailable"})
}
