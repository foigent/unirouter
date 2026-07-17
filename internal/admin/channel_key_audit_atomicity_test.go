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

func TestChannelKeyMutationsRollBackWhenAuditFails(t *testing.T) {
	for _, testCase := range channelKeyMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &channelKeyTransactionState{auditErr: errors.New("forced audit persistence failure")}
			h := newChannelKeyAtomicityHandler(state)
			recorder := httptest.NewRecorder()
			testCase.invoke(h, recorder)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("audit failure must fail the mutation, got status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !state.began || !state.mutatedInTx || !state.auditAttempted {
				t.Fatalf("mutation and audit must share a transaction: %+v", state)
			}
			if state.poolMutation || state.committed || !state.rolledBack {
				t.Fatalf("audit failure must leave no committed or pool-level mutation: %+v", state)
			}
			if h.routingController.(*channelKeyRoutingInvalidator).calls != 0 {
				t.Fatal("routing snapshot must not be invalidated for a rolled-back mutation")
			}
		})
	}
}

func TestChannelKeyMutationsCommitWithAudit(t *testing.T) {
	for _, testCase := range channelKeyMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &channelKeyTransactionState{}
			h := newChannelKeyAtomicityHandler(state)
			recorder := httptest.NewRecorder()
			testCase.invoke(h, recorder)

			if recorder.Code != testCase.successStatus {
				t.Fatalf("expected status=%d, got status=%d body=%s", testCase.successStatus, recorder.Code, recorder.Body.String())
			}
			if !state.began || !state.mutatedInTx || !state.auditAttempted || !state.committed || state.poolMutation {
				t.Fatalf("successful mutation and audit must commit together: %+v", state)
			}
			if h.routingController.(*channelKeyRoutingInvalidator).calls != 1 {
				t.Fatal("routing snapshot must be invalidated exactly once after commit")
			}
		})
	}
}

type channelKeyMutationCase struct {
	name          string
	successStatus int
	invoke        func(*Handler, *httptest.ResponseRecorder)
}

func channelKeyMutationCases() []channelKeyMutationCase {
	return []channelKeyMutationCase{
		{
			name:          "create",
			successStatus: http.StatusCreated,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedChannelKeyRequest(http.MethodPost, "/api/v1/channel-keys", `{"channel_id":1,"key":"provider-test-key","allowed_models":"gpt-4o"}`)
				h.CreateChannelKey(recorder, req)
			},
		},
		{
			name:          "update",
			successStatus: http.StatusOK,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedChannelKeyRequest(http.MethodPut, "/api/v1/channel-keys/9", `{"status":"disabled"}`)
				req.SetPathValue("id", "9")
				h.UpdateChannelKey(recorder, req)
			},
		},
		{
			name:          "delete",
			successStatus: http.StatusOK,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedChannelKeyRequest(http.MethodDelete, "/api/v1/channel-keys/9", "")
				req.SetPathValue("id", "9")
				h.DeleteChannelKey(recorder, req)
			},
		},
	}
}

func authenticatedChannelKeyRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RolePlatformAdmin)
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "channel-key-atomicity-test")
	return req.WithContext(ctx)
}

type channelKeyTransactionState struct {
	auditErr       error
	began          bool
	mutatedInTx    bool
	auditAttempted bool
	poolMutation   bool
	committed      bool
	rolledBack     bool
}

func newChannelKeyAtomicityHandler(state *channelKeyTransactionState) *Handler {
	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if strings.Contains(sql, "INSERT INTO channel_keys") {
					state.mutatedInTx = true
					*dest[0].(*int64) = 42
					return nil
				}
				return pgx.ErrNoRows
			}}
		},
		ExecFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "INSERT INTO audit_logs"):
				state.auditAttempted = true
				if state.auditErr != nil {
					return pgconn.CommandTag{}, state.auditErr
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			case strings.Contains(sql, "UPDATE channel_keys"):
				state.mutatedInTx = true
				return pgconn.NewCommandTag("UPDATE 1"), nil
			case strings.Contains(sql, "DELETE FROM channel_keys"):
				state.mutatedInTx = true
				return pgconn.NewCommandTag("DELETE 1"), nil
			default:
				return pgconn.CommandTag{}, errors.New("unexpected transaction statement")
			}
		},
		CommitFn: func(context.Context) error {
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
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if strings.Contains(sql, "INSERT INTO channel_keys") {
					state.poolMutation = true
					*dest[0].(*int64) = 42
					return nil
				}
				return pgx.ErrNoRows
			}}
		},
		ExecFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "INSERT INTO audit_logs"):
				state.auditAttempted = true
				if state.auditErr != nil {
					return pgconn.CommandTag{}, state.auditErr
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			case strings.Contains(sql, "UPDATE channel_keys"):
				state.poolMutation = true
				return pgconn.NewCommandTag("UPDATE 1"), nil
			case strings.Contains(sql, "DELETE FROM channel_keys"):
				state.poolMutation = true
				return pgconn.NewCommandTag("DELETE 1"), nil
			default:
				return pgconn.CommandTag{}, errors.New("unexpected pool statement")
			}
		},
	}
	h := NewHandler(db, "test-secret", []byte("0123456789abcdef01234567"), nil)
	h.routingController = &channelKeyRoutingInvalidator{}
	return h
}

type channelKeyRoutingInvalidator struct {
	calls int
}

func (c *channelKeyRoutingInvalidator) Preview(context.Context, string) (routingpolicy.Preview, error) {
	return routingpolicy.Preview{}, nil
}

func (c *channelKeyRoutingInvalidator) Invalidate() {
	c.calls++
}
