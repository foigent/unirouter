package testutil

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// MockDB implements db.DBQuerier for unit tests.
// Each field is an optional function override; nil fields use zero-value defaults.
type MockDB struct {
	QueryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	ExecFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	BeginFn    func(ctx context.Context) (pgx.Tx, error)
}

func (m *MockDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.QueryFn != nil {
		return m.QueryFn(ctx, sql, args...)
	}
	return &MockRows{}, nil
}

func (m *MockDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.QueryRowFn != nil {
		return m.QueryRowFn(ctx, sql, args...)
	}
	return &MockRow{}
}

func (m *MockDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.ExecFn != nil {
		return m.ExecFn(ctx, sql, args...)
	}
	return pgconn.CommandTag{}, nil
}

func (m *MockDB) Begin(ctx context.Context) (pgx.Tx, error) {
	if m.BeginFn != nil {
		return m.BeginFn(ctx)
	}
	return &MockTx{}, nil
}

// MockRow implements pgx.Row for unit tests.
type MockRow struct {
	ScanFn func(dest ...any) error
}

func (m *MockRow) Scan(dest ...any) error {
	if m.ScanFn != nil {
		return m.ScanFn(dest...)
	}
	return fmt.Errorf("no rows in result set")
}

func (m *MockRow) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *MockRow) Values() ([]any, error)                       { return nil, nil }
func (m *MockRow) RawValues() [][]byte                          { return nil }
func (m *MockRow) Err() error                                   { return nil }
func (m *MockRow) Close()                                       {}
func (m *MockRow) Next() bool                                   { return false }

// MockRows implements pgx.Rows for unit tests.
type MockRows struct {
	Rows   []func(dest ...any) error
	Idx    int
	Closed bool
}

func (m *MockRows) Close()                                       { m.Closed = true }
func (m *MockRows) Err() error                                   { return nil }
func (m *MockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *MockRows) RawValues() [][]byte                          { return nil }
func (m *MockRows) Values() ([]any, error)                       { return nil, nil }
func (m *MockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (m *MockRows) Conn() *pgx.Conn                              { return nil }

func (m *MockRows) Next() bool {
	if m.Idx < len(m.Rows) {
		m.Idx++
		return true
	}
	return false
}

func (m *MockRows) Scan(dest ...any) error {
	if m.Idx > 0 && m.Idx <= len(m.Rows) {
		return m.Rows[m.Idx-1](dest...)
	}
	return fmt.Errorf("no current row")
}

// MockTx implements pgx.Tx for unit tests.
type MockTx struct {
	ExecFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	CommitFn   func(ctx context.Context) error
	RollbackFn func(ctx context.Context) error
}

func (m *MockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, fmt.Errorf("nested begin not supported")
}

func (m *MockTx) Commit(ctx context.Context) error {
	if m.CommitFn != nil {
		return m.CommitFn(ctx)
	}
	return nil
}

func (m *MockTx) Rollback(ctx context.Context) error {
	if m.RollbackFn != nil {
		return m.RollbackFn(ctx)
	}
	return nil
}

func (m *MockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (m *MockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }
func (m *MockTx) LargeObjects() pgx.LargeObjects                               { return pgx.LargeObjects{} }

func (m *MockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (m *MockTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.ExecFn != nil {
		return m.ExecFn(ctx, sql, args...)
	}
	return pgconn.CommandTag{}, nil
}

func (m *MockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.QueryFn != nil {
		return m.QueryFn(ctx, sql, args...)
	}
	return &MockRows{}, nil
}

func (m *MockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.QueryRowFn != nil {
		return m.QueryRowFn(ctx, sql, args...)
	}
	return &MockRow{}
}

func (m *MockTx) Conn() *pgx.Conn { return nil }
