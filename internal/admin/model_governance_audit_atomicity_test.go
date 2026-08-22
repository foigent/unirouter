package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelGovernanceMutationsRollBackWhenAuditFails(t *testing.T) {
	for _, testCase := range modelGovernanceAtomicMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &modelGovernanceAtomicityState{auditErr: errors.New("forced audit persistence failure")}
			h := newModelGovernanceAtomicityHandler(state)
			recorder := httptest.NewRecorder()
			testCase.invoke(h, recorder)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("audit failure must fail the mutation, got status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !state.began || !state.mutatedInTx || !state.auditAttempted {
				t.Fatalf("mutation and audit must share a transaction: %+v", state)
			}
			if state.poolMutation || state.commitAttempted || !state.rolledBack {
				t.Fatalf("audit failure must leave no committed or pool-level mutation: %+v", state)
			}
		})
	}
}

func TestModelGovernanceMutationsCommitWithAudit(t *testing.T) {
	for _, testCase := range modelGovernanceAtomicMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &modelGovernanceAtomicityState{}
			h := newModelGovernanceAtomicityHandler(state)
			recorder := httptest.NewRecorder()
			testCase.invoke(h, recorder)

			if recorder.Code != testCase.successStatus {
				t.Fatalf("expected status=%d, got status=%d body=%s", testCase.successStatus, recorder.Code, recorder.Body.String())
			}
			if !state.began || !state.mutatedInTx || !state.auditAttempted || !state.committed || state.poolMutation {
				t.Fatalf("successful mutation and audit must commit together: %+v", state)
			}
			if state.auditAction != testCase.auditAction {
				t.Fatalf("audit action=%q, want %q", state.auditAction, testCase.auditAction)
			}
			if state.auditOrgID != 17 || state.auditActorID != 7 {
				t.Fatalf("audit target/actor=%d/%d, want 17/7", state.auditOrgID, state.auditActorID)
			}
			if testCase.update && (!state.lockedBefore || state.auditBefore == "{}" || state.auditAfter == "{}") {
				t.Fatalf("update audit must use locked before/after evidence: %+v", state)
			}
		})
	}
}

func TestModelGovernanceMutationsDoNotReportSuccessWhenCommitFails(t *testing.T) {
	for _, testCase := range modelGovernanceAtomicMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &modelGovernanceAtomicityState{commitErr: errors.New("forced commit failure")}
			h := newModelGovernanceAtomicityHandler(state)
			recorder := httptest.NewRecorder()
			testCase.invoke(h, recorder)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("commit failure must not report success, got status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !state.auditAttempted || !state.commitAttempted || state.committed {
				t.Fatalf("commit failure state is inconsistent: %+v", state)
			}
		})
	}
}

type modelGovernanceAtomicMutationCase struct {
	name          string
	successStatus int
	auditAction   string
	update        bool
	invoke        func(*Handler, *httptest.ResponseRecorder)
}

func modelGovernanceAtomicMutationCases() []modelGovernanceAtomicMutationCase {
	return []modelGovernanceAtomicMutationCase{
		{
			name:          "registry_create",
			successStatus: http.StatusCreated,
			auditAction:   "model_registry.create",
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				h.CreateModelRegistry(recorder, authenticatedModelGovernanceMutationRequest(http.MethodPost,
					"/api/v1/model-registry", `{"model_name":"model-a","lifecycle":"ga","approved_catalog":true}`, ""))
			},
		},
		{
			name:          "registry_update",
			successStatus: http.StatusOK,
			auditAction:   "model_registry.update",
			update:        true,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				h.UpdateModelRegistry(recorder, authenticatedModelGovernanceMutationRequest(http.MethodPut,
					"/api/v1/model-registry/9", `{"lifecycle":"deprecated","approved_catalog":true}`, "9"))
			},
		},
		{
			name:          "alias_create",
			successStatus: http.StatusCreated,
			auditAction:   "model_alias.create",
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				h.CreateModelAlias(recorder, authenticatedModelGovernanceMutationRequest(http.MethodPost,
					"/api/v1/model-aliases", `{"alias":"stable","target_model":"model-a"}`, ""))
			},
		},
		{
			name:          "alias_update",
			successStatus: http.StatusOK,
			auditAction:   "model_alias.update",
			update:        true,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				h.UpdateModelAlias(recorder, authenticatedModelGovernanceMutationRequest(http.MethodPut,
					"/api/v1/model-aliases/9", `{"target_model":"model-b","status":"disabled"}`, "9"))
			},
		},
	}
}

func authenticatedModelGovernanceMutationRequest(method, target, body, id string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if id != "" {
		req.SetPathValue("id", id)
	}
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(17))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "model-governance-audit-atomicity")
	return req.WithContext(ctx)
}

type modelGovernanceAtomicityState struct {
	auditErr        error
	commitErr       error
	began           bool
	mutatedInTx     bool
	poolMutation    bool
	auditAttempted  bool
	commitAttempted bool
	committed       bool
	rolledBack      bool
	lockedBefore    bool
	auditAction     string
	auditBefore     string
	auditAfter      string
	auditOrgID      int64
	auditActorID    int64
}

func newModelGovernanceAtomicityHandler(state *modelGovernanceAtomicityState) *Handler {
	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			return modelGovernanceAtomicityRow(state, sql, true)
		},
		ExecFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "INSERT INTO audit_logs"):
				captureModelGovernanceAtomicityAudit(state, args)
				if state.auditErr != nil {
					return pgconn.CommandTag{}, state.auditErr
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			case strings.Contains(sql, "UPDATE model_registry"), strings.Contains(sql, "UPDATE model_aliases"):
				state.mutatedInTx = true
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
		QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			return modelGovernanceAtomicityRow(state, sql, false)
		},
		ExecFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "INSERT INTO audit_logs"):
				captureModelGovernanceAtomicityAudit(state, args)
				if state.auditErr != nil {
					return pgconn.CommandTag{}, state.auditErr
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			case strings.Contains(sql, "UPDATE model_registry"), strings.Contains(sql, "UPDATE model_aliases"):
				state.poolMutation = true
				return pgconn.NewCommandTag("UPDATE 1"), nil
			default:
				return pgconn.CommandTag{}, errors.New("unexpected pool statement")
			}
		},
	}
	return NewHandler(db, "test-secret", []byte("0123456789abcdef01234567"), nil)
}

func modelGovernanceAtomicityRow(state *modelGovernanceAtomicityState, sql string, inTx bool) pgx.Row {
	return &testutil.MockRow{ScanFn: func(dest ...any) error {
		switch {
		case strings.Contains(sql, "INSERT INTO model_registry"), strings.Contains(sql, "INSERT INTO model_aliases"):
			if inTx {
				state.mutatedInTx = true
			} else {
				state.poolMutation = true
			}
			*dest[0].(*int64) = 9
			return nil
		case strings.Contains(sql, "FROM model_registry"):
			if strings.Contains(sql, "FOR UPDATE") {
				state.lockedBefore = true
			}
			*dest[0].(*int64) = 17
			*dest[1].(*string) = "model-a"
			*dest[2].(*string) = "Model A"
			*dest[3].(*string) = "test-provider"
			*dest[4].(*string) = "ga"
			*dest[5].(*int) = 8192
			*dest[6].(*int) = 2048
			*dest[7].(*string) = "chat"
			*dest[8].(*string) = "regional"
			*dest[9].(*string) = "model-a"
			*dest[10].(*bool) = false
			*dest[11].(*string) = ""
			*dest[12].(*string) = "unverified.chat"
			*dest[13].(*string) = "1.0.0"
			if state.mutatedInTx {
				*dest[4].(*string) = "deprecated"
				*dest[10].(*bool) = true
			}
			return nil
		case strings.Contains(sql, "FROM model_aliases"):
			if strings.Contains(sql, "FOR UPDATE") {
				state.lockedBefore = true
			}
			*dest[0].(*int64) = 17
			*dest[1].(*string) = "stable"
			*dest[2].(*string) = "model-a"
			*dest[3].(*string) = "Stable"
			*dest[4].(*string) = ""
			*dest[5].(*string) = "active"
			return nil
		default:
			return errors.New("unexpected query")
		}
	}}
}

func captureModelGovernanceAtomicityAudit(state *modelGovernanceAtomicityState, args []any) {
	state.auditAttempted = true
	state.auditOrgID = args[0].(int64)
	state.auditActorID = args[1].(int64)
	state.auditAction = args[2].(string)
	state.auditBefore = args[11].(string)
	state.auditAfter = args[12].(string)
}
