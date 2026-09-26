package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
)

type contentControlTestRow struct{ supported bool }

func (r contentControlTestRow) Scan(dest ...any) error {
	if r.supported {
		*dest[0].(*bool) = true
	} else {
		*dest[0].(*int64) = 1
	}
	return nil
}

type contentControlTestTx struct {
	pgx.Tx
	rolledBack, committed bool
}

func (t *contentControlTestTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return contentControlTestRow{supported: true}
}
func (t *contentControlTestTx) Rollback(context.Context) error { t.rolledBack = true; return nil }
func (t *contentControlTestTx) Commit(context.Context) error   { t.committed = true; return nil }

type contentControlTestDB struct {
	db.DBQuerier
	tx *contentControlTestTx
}

func (d contentControlTestDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return contentControlTestRow{}
}
func (d contentControlTestDB) Begin(context.Context) (pgx.Tx, error) { return d.tx, nil }

func TestContentControlRejectsMalformedBeforeDatabase(t *testing.T) {
	h := &Handler{sourceFactsController: contentEnabledControllerFixture{}}
	for _, wire := range []string{`{}`, `{"operation_id":null}`, strings.Repeat("x", (16<<10)+1), `{"operation_id":"11111111111111111111111111111111","operation_id":"11111111111111111111111111111111","expected_state_version":"0"}`} {
		r := httptest.NewRequest("POST", "/synthetic", strings.NewReader(wire))
		w := httptest.NewRecorder()
		h.ContentControl("pause")(w, r)
		if w.Code != 400 {
			t.Fatalf("status %d", w.Code)
		}
	}
}

func TestContentControlActorRejectionRollsBack(t *testing.T) {
	tx := &contentControlTestTx{}
	h := &Handler{sourceFactsController: contentEnabledControllerFixture{}, db: contentControlTestDB{tx: tx}}
	r := httptest.NewRequest("POST", "/synthetic", strings.NewReader(`{"operation_id":"11111111111111111111111111111111","expected_state_version":"0"}`))
	r.SetPathValue("project_id", "2")
	w := httptest.NewRecorder()
	h.ContentControl("pause")(w, r)
	if w.Code != 403 || !tx.rolledBack || tx.committed {
		t.Fatalf("status=%d rollback=%t commit=%t: %s", w.Code, tx.rolledBack, tx.committed, w.Body.String())
	}
}
