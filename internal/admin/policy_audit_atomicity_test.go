package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPolicyMutationsRollBackWhenAuditFails(t *testing.T) {
	for _, testCase := range policyAuditMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &policyAuditTransactionState{
				operation: testCase.name,
				auditErr:  errors.New("forced audit persistence failure"),
			}
			h := newPolicyAuditAtomicityHandler(state)
			recorder := httptest.NewRecorder()
			testCase.invoke(h, recorder)

			if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "Audit failed") {
				t.Fatalf("audit failure must fail the mutation, got status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !state.began || !state.mutatedInTx || !state.auditAttempted {
				t.Fatalf("mutation and audit must share a transaction: %+v", state)
			}
			if state.poolMutation || state.commitAttempted || !state.rolledBack {
				t.Fatalf("audit failure must leave no committed or pool-level mutation: %+v", state)
			}
			if state.refreshCalls != 0 {
				t.Fatalf("runtime snapshot must not refresh after rollback, calls=%d", state.refreshCalls)
			}
		})
	}
}

func TestPolicyMutationsCommitAuditBeforeRefresh(t *testing.T) {
	for _, testCase := range policyAuditMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &policyAuditTransactionState{operation: testCase.name}
			h := newPolicyAuditAtomicityHandler(state)
			recorder := httptest.NewRecorder()
			testCase.invoke(h, recorder)

			if recorder.Code != testCase.successStatus {
				t.Fatalf("expected status=%d, got status=%d body=%s", testCase.successStatus, recorder.Code, recorder.Body.String())
			}
			if !state.began || !state.mutatedInTx || !state.auditAttempted || !state.committed || state.poolMutation {
				t.Fatalf("successful mutation and audit must commit together: %+v", state)
			}
			if state.auditAction != testCase.auditAction || state.auditResource != testCase.auditResource || state.auditResourceID != testCase.resourceID {
				t.Fatalf("unexpected target attribution action=%q resource=%q id=%q", state.auditAction, state.auditResource, state.auditResourceID)
			}
			if state.auditOrgID != 1 || state.auditProjectID != nil {
				t.Fatalf("global policy audit must retain the existing actor-organization attribution, org=%d project=%v", state.auditOrgID, state.auditProjectID)
			}
			if state.auditAfter == "{}" || (testCase.isUpdate && state.auditBefore == "{}") {
				t.Fatalf("audit must retain authoritative before/after state, before=%s after=%s", state.auditBefore, state.auditAfter)
			}
			if state.refreshCalls != 1 || !state.refreshObservedCommit {
				t.Fatalf("runtime snapshot must refresh once and only after commit: %+v", state)
			}
		})
	}
}

func TestPolicyMutationsDoNotRefreshWhenCommitFails(t *testing.T) {
	for _, testCase := range policyAuditMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &policyAuditTransactionState{
				operation: testCase.name,
				commitErr: errors.New("forced commit failure"),
			}
			h := newPolicyAuditAtomicityHandler(state)
			recorder := httptest.NewRecorder()
			testCase.invoke(h, recorder)

			if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "Commit failed") {
				t.Fatalf("commit failure must fail the mutation, got status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !state.auditAttempted || !state.commitAttempted || state.committed {
				t.Fatalf("failed commit must not be reported as committed: %+v", state)
			}
			if state.refreshCalls != 0 {
				t.Fatalf("runtime snapshot must not refresh after commit failure, calls=%d", state.refreshCalls)
			}
		})
	}
}

func TestPolicyUpdatesRollBackWhenTransactionalReadbackFails(t *testing.T) {
	for _, testCase := range policyAuditMutationCases() {
		if !testCase.isUpdate {
			continue
		}
		t.Run(testCase.name, func(t *testing.T) {
			state := &policyAuditTransactionState{operation: testCase.name, readbackErr: true}
			h := newPolicyAuditAtomicityHandler(state)
			recorder := httptest.NewRecorder()
			testCase.invoke(h, recorder)

			if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "Failed to read updated policy") {
				t.Fatalf("readback failure must fail the mutation, got status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !state.mutatedInTx || state.auditAttempted || state.commitAttempted || !state.rolledBack {
				t.Fatalf("readback failure must roll the mutation back before audit or commit: %+v", state)
			}
			if state.refreshCalls != 0 {
				t.Fatalf("runtime snapshot must not refresh after readback failure, calls=%d", state.refreshCalls)
			}
		})
	}
}

func TestPolicyUpdatesPreserveOptimisticConflictSemantics(t *testing.T) {
	for _, testCase := range policyAuditMutationCases() {
		if !testCase.isUpdate {
			continue
		}
		t.Run(testCase.name, func(t *testing.T) {
			state := &policyAuditTransactionState{operation: testCase.name, updateConflict: true}
			h := newPolicyAuditAtomicityHandler(state)
			recorder := httptest.NewRecorder()
			testCase.invoke(h, recorder)

			if recorder.Code != http.StatusConflict {
				t.Fatalf("stale version must remain a conflict, got status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if state.auditAttempted || state.commitAttempted || state.refreshCalls != 0 {
				t.Fatalf("conflict must not audit, commit, or refresh: %+v", state)
			}
		})
	}
}

func TestPolicyAuditMetaUsesTargetScope(t *testing.T) {
	orgID, projectID := int64(11), int64(22)
	req := authenticatedPolicyAuditRequest(http.MethodPost, "/", `{}`)
	for name, meta := range map[string]auditMeta{
		"guardrail": guardrailPolicyAuditMeta(req, guardrailPolicyRecord{OrgID: &orgID, ProjectID: &projectID}),
		"limit":     limitPolicyAuditMeta(req, limitPolicyRecord{OrgID: &orgID, ProjectID: &projectID}),
	} {
		t.Run(name, func(t *testing.T) {
			if meta.OrgID != orgID || meta.ProjectID != projectID || meta.RequestID != "policy-audit-atomicity-test" {
				t.Fatalf("target and request attribution changed: %+v", meta)
			}
		})
	}
}

type policyAuditMutationCase struct {
	name          string
	successStatus int
	auditAction   string
	auditResource string
	resourceID    string
	isUpdate      bool
	invoke        func(*Handler, *httptest.ResponseRecorder)
}

func policyAuditMutationCases() []policyAuditMutationCase {
	return []policyAuditMutationCase{
		{
			name:          "guardrail_create",
			successStatus: http.StatusCreated,
			auditAction:   "guardrail_policy.create",
			auditResource: "guardrail_policy",
			resourceID:    "41",
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedPolicyAuditRequest(http.MethodPost, "/api/v1/guardrail-policies", `{"name":"guardrail-create","stage":"input","detector_ref":"builtin:pii_secrets","action":"redact"}`)
				h.CreateGuardrailPolicy(recorder, req)
			},
		},
		{
			name:          "guardrail_update",
			successStatus: http.StatusOK,
			auditAction:   "guardrail_policy.update",
			auditResource: "guardrail_policy",
			resourceID:    "41",
			isUpdate:      true,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedPolicyAuditRequest(http.MethodPut, "/api/v1/guardrail-policies/41", `{"version":1,"mode":"enforce"}`)
				req.SetPathValue("id", "41")
				h.UpdateGuardrailPolicy(recorder, req)
			},
		},
		{
			name:          "limit_create",
			successStatus: http.StatusCreated,
			auditAction:   "limit_policy.create",
			auditResource: "distributed_limit_policy",
			resourceID:    "51",
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedPolicyAuditRequest(http.MethodPost, "/api/v1/limit-policies", `{"name":"limit-create","mode":"enforce","failure_mode":"fail_closed","rpm_limit":10}`)
				h.CreateLimitPolicy(recorder, req)
			},
		},
		{
			name:          "limit_update",
			successStatus: http.StatusOK,
			auditAction:   "limit_policy.update",
			auditResource: "distributed_limit_policy",
			resourceID:    "51",
			isUpdate:      true,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedPolicyAuditRequest(http.MethodPut, "/api/v1/limit-policies/51", `{"version":1,"mode":"enforce","rpm_limit":20}`)
				req.SetPathValue("id", "51")
				h.UpdateLimitPolicy(recorder, req)
			},
		},
	}
}

func authenticatedPolicyAuditRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RolePlatformAdmin)
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "policy-audit-atomicity-test")
	return req.WithContext(ctx)
}

type policyAuditTransactionState struct {
	operation             string
	auditErr              error
	commitErr             error
	readbackErr           bool
	updateConflict        bool
	began                 bool
	mutatedInTx           bool
	poolMutation          bool
	auditAttempted        bool
	commitAttempted       bool
	committed             bool
	rolledBack            bool
	refreshCalls          int
	refreshObservedCommit bool
	auditAction           string
	auditResource         string
	auditResourceID       string
	auditOrgID            int64
	auditProjectID        any
	auditBefore           string
	auditAfter            string
}

func newPolicyAuditAtomicityHandler(state *policyAuditTransactionState) *Handler {
	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, query string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(query, "INSERT INTO guardrail_policies"):
				state.mutatedInTx = true
				return policyMutationReturningRow(41)
			case strings.Contains(query, "INSERT INTO distributed_limit_policies"):
				state.mutatedInTx = true
				return policyMutationReturningRow(51)
			case strings.Contains(query, "FROM guardrail_policies"):
				if state.readbackErr {
					return &testutil.MockRow{ScanFn: func(...any) error { return errors.New("forced transactional readback failure") }}
				}
				return guardrailPolicyAtomicityRow(2)
			case strings.Contains(query, "FROM distributed_limit_policies"):
				if state.readbackErr {
					return &testutil.MockRow{ScanFn: func(...any) error { return errors.New("forced transactional readback failure") }}
				}
				version := int64(2)
				if state.updateConflict {
					version = 3
				}
				return limitPolicyAtomicityRow(version)
			default:
				return &testutil.MockRow{ScanFn: func(...any) error { return errors.New("unexpected transaction query") }}
			}
		},
		ExecFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(query, "unirouter.guardrail_org_active_policy_limit"):
				return pgconn.NewCommandTag("SELECT 1"), nil
			case strings.Contains(query, "INSERT INTO audit_logs"):
				capturePolicyAudit(state, args)
				if state.auditErr != nil {
					return pgconn.CommandTag{}, state.auditErr
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			case strings.Contains(query, "UPDATE guardrail_policies"), strings.Contains(query, "UPDATE distributed_limit_policies"):
				state.mutatedInTx = !state.updateConflict
				if state.updateConflict {
					return pgconn.NewCommandTag("UPDATE 0"), nil
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			default:
				return pgconn.CommandTag{}, errors.New("unexpected transaction statement")
			}
		},
		CommitFn: func(context.Context) error {
			state.commitAttempted = true
			if state.commitErr != nil {
				return state.commitErr
			}
			state.committed = true
			return nil
		},
		RollbackFn: func(context.Context) error {
			state.rolledBack = true
			return nil
		},
	}
	db := &testutil.MockDB{
		BeginFn: func(context.Context) (pgx.Tx, error) {
			state.began = true
			return tx, nil
		},
		QueryRowFn: func(_ context.Context, query string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(query, "INSERT INTO guardrail_policies"):
				state.poolMutation = true
				return policyMutationReturningRow(41)
			case strings.Contains(query, "INSERT INTO distributed_limit_policies"):
				state.poolMutation = true
				return policyMutationReturningRow(51)
			case strings.Contains(query, "FROM guardrail_policies"):
				version := int64(1)
				if state.poolMutation {
					version = 2
				}
				return guardrailPolicyAtomicityRow(version)
			case strings.Contains(query, "FROM distributed_limit_policies"):
				version := int64(1)
				if state.poolMutation {
					version = 2
				}
				return limitPolicyAtomicityRow(version)
			default:
				return &testutil.MockRow{ScanFn: func(...any) error { return errors.New("unexpected pool query") }}
			}
		},
		ExecFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(query, "INSERT INTO audit_logs"):
				capturePolicyAudit(state, args)
				if state.auditErr != nil {
					return pgconn.CommandTag{}, state.auditErr
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			case strings.Contains(query, "UPDATE guardrail_policies"), strings.Contains(query, "UPDATE distributed_limit_policies"):
				state.poolMutation = !state.updateConflict
				if state.updateConflict {
					return pgconn.NewCommandTag("UPDATE 0"), nil
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			default:
				return pgconn.CommandTag{}, errors.New("unexpected pool statement")
			}
		},
	}
	h := NewHandler(db, "test-secret", []byte("0123456789abcdef01234567"), nil)
	h.SetGuardrailController(policyAuditGuardrailController{state: state})
	h.SetLimitPolicyReloader(policyAuditLimitPolicyReloader{state: state})
	return h
}

func policyMutationReturningRow(id int64) pgx.Row {
	return &testutil.MockRow{ScanFn: func(dest ...any) error {
		*dest[0].(*int64) = id
		*dest[1].(*time.Time) = time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
		*dest[2].(*time.Time) = time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
		return nil
	}}
}

func guardrailPolicyAtomicityRow(version int64) pgx.Row {
	return &testutil.MockRow{ScanFn: func(dest ...any) error {
		*dest[0].(*int64) = 41
		*dest[1].(**int64) = nil
		*dest[2].(**int64) = nil
		*dest[3].(**int64) = nil
		*dest[4].(*string) = "guardrail-update"
		*dest[5].(*string) = ""
		*dest[6].(*string) = guardrail.StageInput
		*dest[7].(*string) = guardrail.DetectorPII
		*dest[8].(*string) = guardrail.ActionRedact
		if version == 1 {
			*dest[9].(*string) = guardrail.ModeShadow
		} else {
			*dest[9].(*string) = guardrail.ModeEnforce
		}
		*dest[10].(*string) = guardrail.FailureDegradedAllow
		*dest[11].(*bool) = false
		*dest[12].(*json.RawMessage) = json.RawMessage(`{}`)
		*dest[13].(*int64) = version
		*dest[14].(*time.Time) = time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
		*dest[15].(*time.Time) = time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
		*dest[16].(*string) = ""
		*dest[17].(*string) = ""
		*dest[18].(*string) = ""
		*dest[19].(*string) = ""
		return nil
	}}
}

func limitPolicyAtomicityRow(version int64) pgx.Row {
	return &testutil.MockRow{ScanFn: func(dest ...any) error {
		rpmLimit := int64(20)
		if version == 1 {
			rpmLimit = 10
		}
		*dest[0].(*int64) = 51
		*dest[1].(*sql.NullInt64) = sql.NullInt64{}
		*dest[2].(*sql.NullInt64) = sql.NullInt64{}
		*dest[3].(*sql.NullInt64) = sql.NullInt64{}
		*dest[4].(*string) = "limit-update"
		*dest[5].(*string) = ""
		*dest[6].(*string) = ""
		if version == 1 {
			*dest[7].(*string) = limitPolicyModeShadow
		} else {
			*dest[7].(*string) = limitPolicyModeEnforce
		}
		*dest[8].(*string) = limitPolicyFailClosed
		*dest[9].(*sql.NullInt64) = sql.NullInt64{}
		*dest[10].(*sql.NullInt64) = sql.NullInt64{Int64: rpmLimit, Valid: true}
		*dest[11].(*sql.NullInt64) = sql.NullInt64{}
		*dest[12].(*sql.NullInt64) = sql.NullInt64{}
		*dest[13].(*int64) = version
		*dest[14].(*time.Time) = time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
		*dest[15].(*time.Time) = time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
		*dest[16].(*string) = ""
		*dest[17].(*string) = ""
		*dest[18].(*string) = ""
		*dest[19].(*string) = ""
		return nil
	}}
}

func capturePolicyAudit(state *policyAuditTransactionState, args []any) {
	state.auditAttempted = true
	state.auditOrgID = args[0].(int64)
	state.auditAction = args[2].(string)
	state.auditResource = args[3].(string)
	state.auditResourceID = args[4].(string)
	state.auditProjectID = args[10]
	state.auditBefore = args[11].(string)
	state.auditAfter = args[12].(string)
}

type policyAuditGuardrailController struct{ state *policyAuditTransactionState }

func (policyAuditGuardrailController) ApprovedDetectors() []string {
	return []string{guardrail.DetectorPII}
}
func (policyAuditGuardrailController) DetectorStreamCapabilities() map[string]guardrail.StreamCapability {
	return map[string]guardrail.StreamCapability{}
}
func (policyAuditGuardrailController) DetectorInputLimits() (int, int, int)  { return 1, 1, 1 }
func (policyAuditGuardrailController) IsDetectorApproved(string) bool        { return true }
func (policyAuditGuardrailController) ValidatePolicy(guardrail.Policy) error { return nil }
func (c policyAuditGuardrailController) Refresh(context.Context) error {
	c.state.refreshCalls++
	c.state.refreshObservedCommit = c.state.committed
	return nil
}

type policyAuditLimitPolicyReloader struct{ state *policyAuditTransactionState }

func (r policyAuditLimitPolicyReloader) Refresh(context.Context) error {
	r.state.refreshCalls++
	r.state.refreshObservedCommit = r.state.committed
	return nil
}
