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
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestChannelControlMutationsRollBackWhenAuditFails(t *testing.T) {
	for _, testCase := range channelControlMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &channelControlTransactionState{operation: testCase.name, auditErr: errors.New("forced audit persistence failure")}
			h := newChannelControlAtomicityHandler(state)
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
			if h.routingController.(*channelControlRoutingInvalidator).calls != 0 {
				t.Fatal("routing snapshot must not be invalidated for a rolled-back mutation")
			}
		})
	}
}

func TestChannelControlMutationsCommitWithAudit(t *testing.T) {
	for _, testCase := range channelControlMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &channelControlTransactionState{operation: testCase.name}
			h := newChannelControlAtomicityHandler(state)
			recorder := httptest.NewRecorder()
			testCase.invoke(h, recorder)

			if recorder.Code != testCase.successStatus {
				t.Fatalf("expected status=%d, got status=%d body=%s", testCase.successStatus, recorder.Code, recorder.Body.String())
			}
			if !state.began || !state.mutatedInTx || !state.auditAttempted || !state.committed || state.poolMutation {
				t.Fatalf("successful mutation and audit must commit together: %+v", state)
			}
			if state.auditAction != testCase.auditAction {
				t.Fatalf("expected audit action %q, got %q", testCase.auditAction, state.auditAction)
			}
			testCase.assertAudit(t, state)
			invalidator := h.routingController.(*channelControlRoutingInvalidator)
			if invalidator.calls != 1 || !invalidator.observedCommit {
				t.Fatalf("routing snapshot must be invalidated exactly once after commit: %+v", invalidator)
			}
		})
	}
}

func TestChannelControlMutationsDoNotInvalidateWhenCommitFails(t *testing.T) {
	for _, testCase := range channelControlMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &channelControlTransactionState{operation: testCase.name, commitErr: errors.New("forced commit failure")}
			h := newChannelControlAtomicityHandler(state)
			recorder := httptest.NewRecorder()
			testCase.invoke(h, recorder)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("commit failure must fail the mutation, got status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !state.auditAttempted || !state.commitAttempted || state.committed {
				t.Fatalf("commit failure must not be reported as committed: %+v", state)
			}
			if h.routingController.(*channelControlRoutingInvalidator).calls != 0 {
				t.Fatal("routing snapshot must not be invalidated when commit fails")
			}
		})
	}
}

type channelControlMutationCase struct {
	name          string
	successStatus int
	auditAction   string
	invoke        func(*Handler, *httptest.ResponseRecorder)
	assertAudit   func(*testing.T, *channelControlTransactionState)
}

func channelControlMutationCases() []channelControlMutationCase {
	return []channelControlMutationCase{
		{
			name:          "create",
			successStatus: http.StatusCreated,
			auditAction:   "channel.create",
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedChannelControlRequest(http.MethodPost, "/api/v1/channels", `{"name":"primary","provider":"openai","base_url":"https://api.openai.com","models":"gpt-4o","priority":3}`)
				h.CreateChannel(recorder, req)
			},
			assertAudit: func(t *testing.T, state *channelControlTransactionState) {
				t.Helper()
				if !strings.Contains(state.auditAfter, `"name":"primary"`) {
					t.Fatalf("create audit must contain authoritative after state, got %s", state.auditAfter)
				}
			},
		},
		{
			name:          "update",
			successStatus: http.StatusOK,
			auditAction:   "channel.update",
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedChannelControlRequest(http.MethodPut, "/api/v1/channels/9", `{"name":"after","priority":2}`)
				req.SetPathValue("id", "9")
				h.UpdateChannel(recorder, req)
			},
			assertAudit: func(t *testing.T, state *channelControlTransactionState) {
				t.Helper()
				if !strings.Contains(state.auditBefore, `"name":"before"`) || !strings.Contains(state.auditAfter, `"name":"after"`) {
					t.Fatalf("update audit must contain transaction-consistent before/after state, before=%s after=%s", state.auditBefore, state.auditAfter)
				}
				if !state.lockingSnapshot {
					t.Fatal("update must lock the authoritative before snapshot in the transaction")
				}
			},
		},
		{
			name:          "delete",
			successStatus: http.StatusOK,
			auditAction:   "channel.delete",
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedChannelControlRequest(http.MethodDelete, "/api/v1/channels/9", "")
				req.SetPathValue("id", "9")
				h.DeleteChannel(recorder, req)
			},
			assertAudit: func(t *testing.T, state *channelControlTransactionState) {
				t.Helper()
				if !strings.Contains(state.auditBefore, `"name":"deleted"`) {
					t.Fatalf("delete audit must contain authoritative before state, got %s", state.auditBefore)
				}
				if !state.lockingSnapshot {
					t.Fatal("delete must lock the authoritative before snapshot in the transaction")
				}
			},
		},
	}
}

func authenticatedChannelControlRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RolePlatformAdmin)
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "channel-control-atomicity-test")
	return req.WithContext(ctx)
}

type channelControlTransactionState struct {
	operation       string
	auditErr        error
	commitErr       error
	began           bool
	mutatedInTx     bool
	auditAttempted  bool
	poolMutation    bool
	commitAttempted bool
	committed       bool
	rolledBack      bool
	lockingSnapshot bool
	snapshotCalls   int
	auditAction     string
	auditBefore     string
	auditAfter      string
}

func newChannelControlAtomicityHandler(state *channelControlTransactionState) *Handler {
	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "INSERT INTO channels"):
				state.mutatedInTx = true
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 42
					return nil
				}}
			case strings.Contains(sql, "FROM channels WHERE id = $1"):
				state.snapshotCalls++
				if strings.Contains(sql, "FOR UPDATE") {
					state.lockingSnapshot = true
				}
				return channelControlSnapshotRow(state)
			default:
				return &testutil.MockRow{ScanFn: func(...any) error { return errors.New("unexpected transaction query") }}
			}
		},
		ExecFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "INSERT INTO audit_logs"):
				state.auditAttempted = true
				state.auditAction = args[2].(string)
				state.auditBefore = args[11].(string)
				state.auditAfter = args[12].(string)
				if state.auditErr != nil {
					return pgconn.CommandTag{}, state.auditErr
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			case strings.Contains(sql, "UPDATE channels"):
				state.mutatedInTx = true
				return pgconn.NewCommandTag("UPDATE 1"), nil
			case strings.Contains(sql, "DELETE FROM channels"):
				state.mutatedInTx = true
				return pgconn.NewCommandTag("DELETE 1"), nil
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
			if strings.Contains(sql, "INSERT INTO channels") {
				state.poolMutation = true
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 42
					return nil
				}}
			}
			if strings.Contains(sql, "FROM channels WHERE id = $1") {
				state.snapshotCalls++
				return channelControlSnapshotRow(state)
			}
			return &testutil.MockRow{ScanFn: func(...any) error { return errors.New("unexpected pool query") }}
		},
		ExecFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "INSERT INTO audit_logs"):
				state.auditAttempted = true
				if state.auditErr != nil {
					return pgconn.CommandTag{}, state.auditErr
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			case strings.Contains(sql, "UPDATE channels"):
				state.poolMutation = true
				return pgconn.NewCommandTag("UPDATE 1"), nil
			case strings.Contains(sql, "DELETE FROM channels"):
				state.poolMutation = true
				return pgconn.NewCommandTag("DELETE 1"), nil
			default:
				return pgconn.CommandTag{}, errors.New("unexpected pool statement")
			}
		},
	}
	h := NewHandler(db, "test-secret", []byte("0123456789abcdef01234567"), nil)
	h.routingController = &channelControlRoutingInvalidator{state: state}
	return h
}

func channelControlSnapshotRow(state *channelControlTransactionState) pgx.Row {
	return &testutil.MockRow{ScanFn: func(dest ...any) error {
		name := "before"
		if state.operation == "delete" {
			name = "deleted"
		} else if state.mutatedInTx && state.snapshotCalls > 1 {
			name = "after"
		}
		*dest[0].(*string) = name
		*dest[1].(*string) = "openai"
		*dest[2].(*string) = "https://api.openai.com"
		*dest[3].(*string) = "gpt-4o"
		*dest[4].(*string) = "openai_compatible"
		*dest[5].(*int) = 1
		*dest[6].(*int) = 3
		*dest[7].(*string) = "active"
		*dest[8].(*string) = "healthy"
		*dest[9].(*int) = 0
		*dest[10].(*int) = 100
		*dest[11].(*int64) = int64(state.snapshotCalls)
		return nil
	}}
}

type channelControlRoutingInvalidator struct {
	state          *channelControlTransactionState
	calls          int
	observedCommit bool
}

func (c *channelControlRoutingInvalidator) Preview(context.Context, string) (routingpolicy.Preview, error) {
	return routingpolicy.Preview{}, nil
}

func (c *channelControlRoutingInvalidator) Invalidate() {
	c.calls++
	c.observedCommit = c.state.committed
}
