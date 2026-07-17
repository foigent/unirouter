package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDataExchangeMutationsRollBackWhenAuditFails(t *testing.T) {
	for _, testCase := range dataExchangeAuditMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &dataExchangeAuditTransactionState{auditErr: errors.New("forced audit persistence failure")}
			h := newDataExchangeAuditAtomicityHandler(state)
			recorder := httptest.NewRecorder()

			testCase.invoke(h, recorder)

			if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "Audit failed") {
				t.Fatalf("audit failure must fail mutation: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !state.began || !state.mutatedInTx || !state.auditAttempted {
				t.Fatalf("mutation and audit must share one transaction: %+v", state)
			}
			if state.poolMutation || state.committed || !state.rollbackCalled {
				t.Fatalf("audit failure must leave no committed or pool-level mutation: %+v", state)
			}
			if testCase.name == "item_update" && !state.itemReadForUpdate {
				t.Fatal("item update must lock the before-state row in the mutation transaction")
			}
		})
	}
}

func TestDataExchangeMutationsCommitWithScopedAudit(t *testing.T) {
	for _, testCase := range dataExchangeAuditMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &dataExchangeAuditTransactionState{}
			h := newDataExchangeAuditAtomicityHandler(state)
			recorder := httptest.NewRecorder()

			testCase.invoke(h, recorder)

			if recorder.Code != testCase.successStatus {
				t.Fatalf("expected status=%d, got status=%d body=%s", testCase.successStatus, recorder.Code, recorder.Body.String())
			}
			if !state.began || !state.mutatedInTx || !state.auditAttempted || !state.committed || state.poolMutation {
				t.Fatalf("successful mutation and audit must commit together: %+v", state)
			}
			if state.auditAction != testCase.auditAction || state.auditResource != testCase.auditResource || state.auditResourceID != testCase.auditResourceID || state.auditOrgID != 3 || state.auditProjectID != nil {
				t.Fatalf("unexpected audit target/scope: action=%q resource=%s/%s org=%d project=%#v", state.auditAction, state.auditResource, state.auditResourceID, state.auditOrgID, state.auditProjectID)
			}
			if state.auditRequestID != "data-exchange-atomicity-test" || state.auditSource != "admin_console" {
				t.Fatalf("missing request-correlated audit metadata: request_id=%q source=%q", state.auditRequestID, state.auditSource)
			}
			if testCase.name == "item_update" {
				if !state.itemReadForUpdate || !strings.Contains(state.auditBefore, `"approval_status":"pending"`) || !strings.Contains(state.auditAfter, `"approval_status":"unavailable"`) {
					t.Fatalf("item update must audit its locked before/after state: before=%s after=%s", state.auditBefore, state.auditAfter)
				}
			}
		})
	}
}

func TestDataExchangeMutationsRejectCommitFailure(t *testing.T) {
	for _, testCase := range dataExchangeAuditMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			state := &dataExchangeAuditTransactionState{commitErr: errors.New("forced commit failure")}
			h := newDataExchangeAuditAtomicityHandler(state)
			recorder := httptest.NewRecorder()

			testCase.invoke(h, recorder)

			if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "Commit failed") {
				t.Fatalf("commit failure must fail mutation: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !state.began || !state.mutatedInTx || !state.auditAttempted || !state.commitAttempted {
				t.Fatalf("commit must follow the transactional mutation and audit: %+v", state)
			}
			if state.poolMutation || state.committed || !state.rollbackCalled {
				t.Fatalf("failed commit must not be reported as committed: %+v", state)
			}
		})
	}
}

func TestDataExchangeCreateConflictsRemainNonSuccessWithoutAudit(t *testing.T) {
	for _, testCase := range []struct {
		name             string
		mutationCase     dataExchangeAuditMutationCase
		queryErrContains string
	}{
		{name: "approval_item", mutationCase: dataExchangeAuditMutationCases()[0], queryErrContains: "INSERT INTO integration_exchange_items"},
		{name: "exchange", mutationCase: dataExchangeAuditMutationCases()[2], queryErrContains: "INSERT INTO integration_exchanges"},
		{name: "receipt", mutationCase: dataExchangeAuditMutationCases()[3], queryErrContains: "INSERT INTO integration_exchange_receipts"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			state := &dataExchangeAuditTransactionState{
				queryErrContains: testCase.queryErrContains,
				queryErr:         errors.New("forced uniqueness conflict"),
			}
			h := newDataExchangeAuditAtomicityHandler(state)
			recorder := httptest.NewRecorder()

			testCase.mutationCase.invoke(h, recorder)

			if recorder.Code != http.StatusConflict {
				t.Fatalf("expected conflict response, got status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if state.auditAttempted || state.commitAttempted || state.committed || state.poolMutation || !state.rollbackCalled {
				t.Fatalf("rejected duplicate must not audit or commit: %+v", state)
			}
		})
	}
}

type dataExchangeAuditMutationCase struct {
	name            string
	auditAction     string
	auditResource   string
	auditResourceID string
	successStatus   int
	invoke          func(*Handler, *httptest.ResponseRecorder)
}

func dataExchangeAuditMutationCases() []dataExchangeAuditMutationCase {
	return []dataExchangeAuditMutationCase{
		{
			name:            "item_create",
			auditAction:     "data_exchange.item.create",
			auditResource:   "integration_exchange_item",
			auditResourceID: "41",
			successStatus:   http.StatusCreated,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedDataExchangeRequest(http.MethodPost, "/api/v1/data-exchange/items", `{
					"approval_mode":"controlled_scenario",
					"scenario_label":"provider failure",
					"source_instance":"dev-a",
					"data_classes":["request_trace_events"]
				}`)
				h.CreateDataExchangeItem(recorder, req)
			},
		},
		{
			name:            "item_update",
			auditAction:     "data_exchange.item.update",
			auditResource:   "integration_exchange_item",
			auditResourceID: "41",
			successStatus:   http.StatusOK,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedDataExchangeRequest(http.MethodPut, "/api/v1/data-exchange/items/41", `{
					"approval_status":"unavailable",
					"reason":"source evidence unavailable"
				}`)
				req.SetPathValue("id", "41")
				h.UpdateDataExchangeItem(recorder, req)
			},
		},
		{
			name:            "exchange_create",
			auditAction:     "data_exchange.exchange.create",
			auditResource:   "integration_exchange",
			auditResourceID: "42",
			successStatus:   http.StatusCreated,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedDataExchangeRequest(http.MethodPost, "/api/v1/data-exchange/exchanges", `{
					"exchange_id":"uxchg_atomic_001",
					"source_instance":"dev-a",
					"schema_version":"unirouter.prd5.xinsight.snapshot.v1",
					"approval_manifest_sha256":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
					"artifact_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"artifact_ref":"evidence/snapshot.json",
					"item_ids":[41],
					"request_count":1,
					"input_event_count":1,
					"generated_at":"2026-07-10T08:00:00Z"
				}`)
				h.CreateDataExchange(recorder, req)
			},
		},
		{
			name:            "receipt_create",
			auditAction:     "data_exchange.receipt.record",
			auditResource:   "integration_exchange",
			auditResourceID: "42",
			successStatus:   http.StatusCreated,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedDataExchangeRequest(http.MethodPost, "/api/v1/data-exchange/exchanges/42/receipts", `{
					"exchange_id":"uxchg_atomic_001",
					"schema_version":"xinsight.import-receipt.v1",
					"source_system":"unirouter",
					"source_instance":"dev-a",
					"artifact_schema_version":"unirouter.prd5.xinsight.snapshot.v1",
					"artifact_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"approval_manifest_sha256":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
					"result_status":"persisted",
					"checked_at":"2026-07-10T08:00:00Z"
				}`)
				req.SetPathValue("id", "42")
				h.CreateDataExchangeReceipt(recorder, req)
			},
		},
		{
			name:            "delivery_handed_off",
			auditAction:     "data_exchange.handed_off",
			auditResource:   "integration_exchange",
			auditResourceID: "42",
			successStatus:   http.StatusCreated,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedDataExchangeRequest(http.MethodPost, "/api/v1/data-exchange/exchanges/42/deliveries", `{
					"action":"handed_off",
					"handoff_ref":"handoff/test-primary",
					"note":"test handoff"
				}`)
				req.SetPathValue("id", "42")
				h.CreateDataExchangeDelivery(recorder, req)
			},
		},
		{
			name:            "delivery_redelivered",
			auditAction:     "data_exchange.redelivered",
			auditResource:   "integration_exchange",
			auditResourceID: "42",
			successStatus:   http.StatusCreated,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedDataExchangeRequest(http.MethodPost, "/api/v1/data-exchange/exchanges/42/deliveries", `{
					"action":"redelivered",
					"handoff_ref":"handoff/test-redelivery",
					"note":"test redelivery"
				}`)
				req.SetPathValue("id", "42")
				h.CreateDataExchangeDelivery(recorder, req)
			},
		},
		{
			name:            "artifact_mark_deleted",
			auditAction:     "data_exchange.artifact.mark_deleted",
			auditResource:   "integration_exchange",
			auditResourceID: "42",
			successStatus:   http.StatusOK,
			invoke: func(h *Handler, recorder *httptest.ResponseRecorder) {
				req := authenticatedDataExchangeRequest(http.MethodPost, "/api/v1/data-exchange/exchanges/42/artifact-delete", "")
				req.SetPathValue("id", "42")
				h.DeleteDataExchangeArtifact(recorder, req)
			},
		},
	}
}

func authenticatedDataExchangeRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, "platform_admin")
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "data-exchange-atomicity-test")
	return req.WithContext(ctx)
}

type dataExchangeAuditTransactionState struct {
	auditErr          error
	commitErr         error
	queryErrContains  string
	queryErr          error
	began             bool
	mutatedInTx       bool
	poolMutation      bool
	auditAttempted    bool
	commitAttempted   bool
	committed         bool
	rollbackCalled    bool
	itemReadForUpdate bool
	auditAction       string
	auditResource     string
	auditResourceID   string
	auditOrgID        int64
	auditProjectID    any
	auditRequestID    string
	auditSource       string
	auditBefore       string
	auditAfter        string
}

func newDataExchangeAuditAtomicityHandler(state *dataExchangeAuditTransactionState) *Handler {
	queryRow := func(inTx bool, query string, args ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			if state.queryErr != nil && strings.Contains(query, state.queryErrContains) {
				return state.queryErr
			}
			switch {
			case strings.Contains(query, "SELECT approval_status, approval_mode, request_id"):
				state.itemReadForUpdate = inTx && strings.Contains(query, "FOR UPDATE")
				*dest[0].(*string) = "pending"
				*dest[1].(*string) = "controlled_scenario"
				*dest[2].(*sql.NullString) = sql.NullString{}
				return nil
			case strings.Contains(query, "SELECT COUNT(*) FROM integration_exchange_items"):
				*dest[0].(*int) = 1
				return nil
			case strings.Contains(query, "SELECT exchange_id, artifact_sha256"):
				*dest[0].(*string) = "uxchg_atomic_001"
				*dest[1].(*string) = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				*dest[2].(*string) = "dev-a"
				*dest[3].(*string) = "unirouter.prd5.xinsight.snapshot.v1"
				*dest[4].(*string) = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
				return nil
			case strings.Contains(query, "SELECT exchange_id FROM integration_exchanges"):
				*dest[0].(*string) = "uxchg_atomic_001"
				return nil
			case strings.Contains(query, "UPDATE integration_exchanges") && strings.Contains(query, "RETURNING exchange_id"):
				markDataExchangeMutation(state, inTx)
				*dest[0].(*string) = "uxchg_atomic_001"
				*dest[1].(*string) = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				*dest[2].(*string) = "evidence/snapshot.json"
				return nil
			case strings.Contains(query, "INSERT INTO integration_exchange_items"):
				markDataExchangeMutation(state, inTx)
				*dest[0].(*int64) = 41
				return nil
			case strings.Contains(query, "INSERT INTO integration_exchanges"):
				markDataExchangeMutation(state, inTx)
				*dest[0].(*int64) = 42
				return nil
			case strings.Contains(query, "INSERT INTO integration_exchange_receipts"):
				markDataExchangeMutation(state, inTx)
				*dest[0].(*int64) = 44
				return nil
			case strings.Contains(query, "INSERT INTO integration_exchange_deliveries"):
				markDataExchangeMutation(state, inTx)
				*dest[0].(*int64) = 43
				return nil
			default:
				return errors.New("unexpected data-exchange query")
			}
		}}
	}

	exec := func(inTx bool, query string, args ...any) (pgconn.CommandTag, error) {
		switch {
		case strings.Contains(query, "INSERT INTO audit_logs"):
			state.auditAttempted = true
			state.auditOrgID = args[0].(int64)
			state.auditAction = args[2].(string)
			state.auditResource = args[3].(string)
			state.auditResourceID = args[4].(string)
			state.auditRequestID = args[8].(string)
			state.auditSource = args[9].(string)
			state.auditProjectID = args[10]
			state.auditBefore = args[11].(string)
			state.auditAfter = args[12].(string)
			if state.auditErr != nil {
				return pgconn.CommandTag{}, state.auditErr
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		case strings.Contains(query, "UPDATE integration_exchange_items"):
			markDataExchangeMutation(state, inTx)
			return pgconn.NewCommandTag("UPDATE 1"), nil
		case strings.Contains(query, "INSERT INTO integration_exchange_members"):
			markDataExchangeMutation(state, inTx)
			return pgconn.NewCommandTag("INSERT 1"), nil
		case strings.Contains(query, "UPDATE integration_exchanges"):
			markDataExchangeMutation(state, inTx)
			return pgconn.NewCommandTag("UPDATE 1"), nil
		default:
			return pgconn.CommandTag{}, errors.New("unexpected data-exchange exec")
		}
	}

	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, query string, args ...any) pgx.Row {
			return queryRow(true, query, args...)
		},
		ExecFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			return exec(true, query, args...)
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
			state.rollbackCalled = true
			return nil
		},
	}
	db := &testutil.MockDB{
		BeginFn: func(context.Context) (pgx.Tx, error) {
			state.began = true
			return tx, nil
		},
		QueryRowFn: func(_ context.Context, query string, args ...any) pgx.Row {
			return queryRow(false, query, args...)
		},
		ExecFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			return exec(false, query, args...)
		},
	}
	return newTestHandler(db)
}

func markDataExchangeMutation(state *dataExchangeAuditTransactionState, inTx bool) {
	if inTx {
		state.mutatedInTx = true
		return
	}
	state.poolMutation = true
}
