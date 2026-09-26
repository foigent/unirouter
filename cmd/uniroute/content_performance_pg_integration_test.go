//go:build integration

package main

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/db"
)

// Opt-in fixture-only evidence. No SQL, arguments, error text, DSN, or connection
// config enters a record. Overflow is explicit, never interpreted as no waiting.
const contentPerformancePGLimit = 131072

type contentPerformancePGEvent struct {
	Kind       string `json:"kind"`
	Class      string `json:"class,omitempty"`
	Origin     string `json:"origin,omitempty"`
	Marker     string `json:"marker,omitempty"`
	Connection uint64 `json:"connection"`
	PID        uint32 `json:"pid"`
	Tx         uint64 `json:"tx,omitempty"`
	StartNS    int64  `json:"start_ns"`
	EndNS      int64  `json:"end_ns"`
	Error      string `json:"error_class"`
	TxStatus   string `json:"tx_status,omitempty"`
	// Batch callbacks report consumption, NOT independent SQL execution timings.
	BatchIndex          int    `json:"batch_index,omitempty"`
	TxStartNS           int64  `json:"tx_start_ns,omitempty"`
	FirstLockClass      string `json:"first_lock_class,omitempty"`
	FirstLockObservedNS int64  `json:"first_lock_observed_ns,omitempty"`
	Censored            bool   `json:"right_censored,omitempty"`
}
type contentPerformancePGConn struct {
	pid              uint32
	id, tx           uint64
	txStart, lockEnd int64
	lockClass        string
}
type contentPerformancePGCall struct {
	row  contentPerformancePGEvent
	next int
}
type contentPerformancePGKey struct{}
type contentPerformancePG struct {
	mu               sync.Mutex
	start            time.Time
	nextConn, nextTx uint64
	conns            map[*pgx.Conn]*contentPerformancePGConn
	active           map[*contentPerformancePGCall]bool
	events           []contentPerformancePGEvent
	dropped          uint64
}

func newContentPerformancePG() *contentPerformancePG {
	return &contentPerformancePG{start: time.Now(), conns: map[*pgx.Conn]*contentPerformancePGConn{}, active: map[*contentPerformancePGCall]bool{}}
}

// Classification is deliberately finite and advisory; server wait/lock evidence
// is still required. A mention of a relation does not prove a wait on that table.
func contentPerformanceSQLClass(sql string) string {
	s := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	switch s {
	case "begin", "begin isolation level read committed", "begin isolation level repeatable read", "begin isolation level serializable":
		return "begin"
	case "commit":
		return "commit"
	case "rollback":
		return "rollback"
	}
	if strings.HasPrefix(s, "savepoint ") || strings.HasPrefix(s, "release savepoint ") || strings.HasPrefix(s, "rollback to savepoint ") {
		return "savepoint"
	}
	for _, x := range []struct{ table, label string }{
		{"execution_source_streams", "ab_stream"}, {"execution_content_streams", "c_stream"},
		{"freeze_idempotency", "finance_freeze"}, {"wallets", "finance_wallet"},
		{"internal_outbox_events", "outbox"}, {"execution_source_facts", "ab_facts"},
		{"execution_content_manifests", "c_manifest"}, {"execution_content_objects", "c_object"},
		{"request_trace_events", "request_trace"}, {"limit_policy", "policy"},
	} {
		if strings.Contains(s, x.table) {
			if strings.Contains(s, "for update") || strings.Contains(s, "for no key update") || strings.Contains(s, "for share") {
				return x.label + "_lock"
			}
			if strings.HasPrefix(s, "insert ") {
				return x.label + "_insert"
			}
			if strings.HasPrefix(s, "update ") || strings.HasPrefix(s, "delete ") || strings.HasPrefix(s, "with ") {
				return x.label + "_write_or_cte"
			}
			return x.label + "_read"
		}
	}
	return "other"
}
func contentPerformancePGError(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "40001", "40P01", "55P03", "57014", "23505", "25P02", "08006":
			return "sqlstate_" + pgErr.Code
		}
	}
	return contentPerformanceErrorClass(err)
}

func contentPerformancePGOrigin() string {
	var pcs [28]uintptr
	n := runtime.Callers(3, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	names := []string{}
	for {
		f, more := frames.Next()
		const prefix = "github.com/uniroute/unirouter/internal/"
		if strings.HasPrefix(f.Function, prefix) {
			names = append(names, strings.TrimPrefix(f.Function, prefix))
			if len(names) == 3 {
				break
			}
		}
		if !more {
			break
		}
	}
	return strings.Join(names, " <- ")
}
func (d *contentPerformancePG) add(row contentPerformancePGEvent) {
	if len(d.events) < contentPerformancePGLimit {
		d.events = append(d.events, row)
	} else {
		d.dropped++
	}
}
func (d *contentPerformancePG) connection(conn *pgx.Conn) *contentPerformancePGConn {
	if conn == nil {
		return nil
	}
	c := d.conns[conn]
	if c == nil {
		d.nextConn++
		c = &contentPerformancePGConn{id: d.nextConn}
		d.conns[conn] = c
	}
	return c
}
func (d *contentPerformancePG) bind(row *contentPerformancePGEvent, conn *pgx.Conn) *contentPerformancePGConn {
	c := d.connection(conn)
	if c != nil {
		row.Connection = c.id
		row.PID = conn.PgConn().PID()
		c.pid = row.PID
		row.Tx = c.tx
		row.TxStartNS = c.txStart
		row.FirstLockClass = c.lockClass
		row.FirstLockObservedNS = c.lockEnd
		row.TxStatus = string(conn.PgConn().TxStatus())
	}
	return c
}
func (d *contentPerformancePG) begin(ctx context.Context, conn *pgx.Conn, kind, class string) context.Context {
	row := contentPerformancePGEvent{Kind: kind, Class: class, Origin: contentPerformancePGOrigin(), StartNS: time.Since(d.start).Nanoseconds()}
	if p := contentPerformancePhaseFrom(ctx); p != nil {
		row.Marker = contentPerformanceMarker(p.marker)
	}
	call := &contentPerformancePGCall{row: row}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bind(&call.row, conn)
	// Active calls are bounded independently of completed event storage.
	if len(d.active) < contentPerformancePGLimit {
		d.active[call] = true
	} else {
		d.dropped++
	}
	return context.WithValue(ctx, contentPerformancePGKey{}, call)
}
func (d *contentPerformancePG) finish(ctx context.Context, conn *pgx.Conn, err error) {
	call, _ := ctx.Value(contentPerformancePGKey{}).(*contentPerformancePGCall)
	if call == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.active, call)
	row := call.row
	row.EndNS = time.Since(d.start).Nanoseconds()
	row.Error = contentPerformancePGError(err)
	c := d.bind(&row, conn)
	d.complete(row, c, err)
}
func (d *contentPerformancePG) complete(row contentPerformancePGEvent, c *contentPerformancePGConn, err error) {
	if c != nil {
		if row.Class == "begin" && err == nil {
			d.nextTx++
			c.tx = d.nextTx
			c.txStart = row.StartNS
			c.lockEnd = 0
			c.lockClass = ""
		}
		if c.tx != 0 && strings.HasSuffix(row.Class, "_lock") && err == nil && c.lockEnd == 0 {
			c.lockEnd = row.EndNS
			c.lockClass = row.Class
		}
		row.Tx = c.tx
		row.TxStartNS = c.txStart
		row.FirstLockClass = c.lockClass
		row.FirstLockObservedNS = c.lockEnd
	}
	d.add(row)
	if c != nil && c.tx != 0 && (row.Class == "commit" || row.Class == "rollback") {
		tx := row
		tx.Kind = "transaction"
		tx.StartNS = c.txStart
		tx.Censored = err != nil
		d.add(tx)
		// A failed COMMIT without idle status is not evidence of release.
		if row.TxStatus == "I" {
			c.tx = 0
			c.txStart = 0
			c.lockEnd = 0
			c.lockClass = ""
		}
	}
}

func (d *contentPerformancePG) TraceQueryStart(ctx context.Context, c *pgx.Conn, v pgx.TraceQueryStartData) context.Context {
	return d.begin(ctx, c, "query", contentPerformanceSQLClass(v.SQL))
}
func (d *contentPerformancePG) TraceQueryEnd(ctx context.Context, c *pgx.Conn, v pgx.TraceQueryEndData) {
	d.finish(ctx, c, v.Err)
}
func (d *contentPerformancePG) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	return d.begin(ctx, nil, "acquire", "")
}
func (d *contentPerformancePG) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, v pgxpool.TraceAcquireEndData) {
	d.finish(ctx, v.Conn, v.Err)
}
func (d *contentPerformancePG) TraceRelease(_ *pgxpool.Pool, v pgxpool.TraceReleaseData) {
	d.mu.Lock()
	defer d.mu.Unlock()
	row := contentPerformancePGEvent{Kind: "release", EndNS: time.Since(d.start).Nanoseconds(), Error: "none"}
	c := d.bind(&row, v.Conn)
	if c != nil && c.tx != 0 {
		row.Kind = "transaction_unconfirmed_release"
		row.StartNS = c.txStart
		row.Censored = true
		c.tx = 0
		c.txStart = 0
		c.lockEnd = 0
		c.lockClass = ""
	}
	d.add(row)
}
func (d *contentPerformancePG) TraceBatchStart(ctx context.Context, c *pgx.Conn, v pgx.TraceBatchStartData) context.Context {
	ctx = d.begin(ctx, c, "batch", "")
	call := ctx.Value(contentPerformancePGKey{}).(*contentPerformancePGCall)
	_ = call
	return ctx
}
func (d *contentPerformancePG) TraceBatchQuery(ctx context.Context, c *pgx.Conn, v pgx.TraceBatchQueryData) {
	call, _ := ctx.Value(contentPerformancePGKey{}).(*contentPerformancePGCall)
	if call == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	call.next++
	row := call.row
	row.Kind = "batch_item_consumed"
	row.Class = contentPerformanceSQLClass(v.SQL)
	row.BatchIndex = call.next
	row.EndNS = time.Since(d.start).Nanoseconds()
	row.Error = contentPerformancePGError(v.Err)
	state := d.bind(&row, c)
	if state != nil && state.tx != 0 && strings.HasSuffix(row.Class, "_lock") && v.Err == nil && state.lockEnd == 0 {
		state.lockEnd = row.EndNS
		state.lockClass = row.Class
		d.bind(&row, c)
	}
	d.add(row)
}
func (d *contentPerformancePG) TraceBatchEnd(ctx context.Context, c *pgx.Conn, v pgx.TraceBatchEndData) {
	d.finish(ctx, c, v.Err)
}
func (d *contentPerformancePG) snapshot() map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	pending := []contentPerformancePGEvent{}
	for call := range d.active {
		row := call.row
		row.Censored = true
		row.EndNS = time.Since(d.start).Nanoseconds()
		pending = append(pending, row)
	}
	open := []contentPerformancePGEvent{}
	for _, c := range d.conns {
		if c.tx != 0 {
			open = append(open, contentPerformancePGEvent{Kind: "open_transaction", Connection: c.id, PID: c.pid, Tx: c.tx, StartNS: c.txStart, EndNS: time.Since(d.start).Nanoseconds(), FirstLockClass: c.lockClass, FirstLockObservedNS: c.lockEnd, Censored: true})
		}
	}

	return map[string]any{"clock": "monotonic_ns_since_start", "start_unix_ns": d.start.UnixNano(), "events": append([]contentPerformancePGEvent{}, d.events...), "pending_at_snapshot": pending, "open_transactions_at_snapshot": open, "dropped_events": d.dropped, "truncated": d.dropped != 0, "event_limit": contentPerformancePGLimit, "boundary": "client-observed acquire/statement/transaction intervals; batch timestamps are result consumption; first lock completion to commit is an observed suffix, not exact server lock lifetime; forced exit may lose this artifact"}
}

var _ pgx.QueryTracer = (*contentPerformancePG)(nil)
var _ pgx.BatchTracer = (*contentPerformancePG)(nil)
var _ pgxpool.AcquireTracer = (*contentPerformancePG)(nil)
var _ pgxpool.ReleaseTracer = (*contentPerformancePG)(nil)

// Resolve clone-local relation OIDs once before load, on the existing startup
// query budget. The external sampler stays on the base DB and cannot obstruct
// native clone cleanup. No payload or private row data is read.
type contentPerformancePGDatabase struct {
	contentPerformanceDatabaseBinding
	Relations          map[string]string `json:"relations"`
	RelationsTruncated bool              `json:"relations_truncated"`
}

func contentPerformanceReadPGDatabase(ctx context.Context, pool db.DBQuerier) (contentPerformancePGDatabase, error) {
	var result contentPerformancePGDatabase
	err := pool.QueryRow(ctx, `WITH relations AS MATERIALIZED (
 SELECT c.oid::text AS oid,n.nspname||'.'||c.relname AS name FROM pg_class c
 JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p','i','I')
 ), bounded AS (SELECT * FROM relations ORDER BY oid LIMIT 4096)
 SELECT current_database(),(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),
 COALESCE((SELECT jsonb_object_agg(oid,name) FROM bounded),'{}'::jsonb),
 (SELECT count(*)>4096 FROM relations)`).Scan(&result.Name, &result.OID, &result.Relations, &result.RelationsTruncated)
	if err == nil && (result.Name == "" || result.OID <= 0 || len(result.Relations) == 0) {
		err = errors.New("invalid diagnostic database identity or relation map")
	}
	return result, err
}
