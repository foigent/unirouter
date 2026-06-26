package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// --- Mock DBQuerier ---

type mockRow struct {
	scanFn func(dest ...any) error
}

func (m *mockRow) Scan(dest ...any) error {
	if m.scanFn != nil {
		return m.scanFn(dest...)
	}
	return fmt.Errorf("no rows")
}

func (m *mockRow) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mockRow) Values() ([]any, error)                       { return nil, nil }
func (m *mockRow) RawValues() [][]byte                          { return nil }
func (m *mockRow) Err() error                                   { return nil }
func (m *mockRow) Close()                                       {}
func (m *mockRow) Next() bool                                   { return false }

type mockRows struct {
	rows   []func(dest ...any) error
	idx    int
	closed bool
}

func (m *mockRows) Close()                                       { m.closed = true }
func (m *mockRows) Err() error                                   { return nil }
func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mockRows) RawValues() [][]byte                          { return nil }
func (m *mockRows) Values() ([]any, error)                       { return nil, nil }
func (m *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (m *mockRows) Conn() *pgx.Conn                              { return nil }

func (m *mockRows) Next() bool {
	if m.idx < len(m.rows) {
		return true
	}
	return false
}

func (m *mockRows) Scan(dest ...any) error {
	if m.idx >= len(m.rows) {
		return fmt.Errorf("no more rows")
	}
	err := m.rows[m.idx](dest...)
	m.idx++
	return err
}

type mockDB struct {
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	beginFn    func(ctx context.Context) (pgx.Tx, error)
}

func (m *mockDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return &mockRow{}
}

func (m *mockDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFn != nil {
		return m.execFn(ctx, sql, args...)
	}
	return pgconn.CommandTag{}, nil
}

func (m *mockDB) Begin(ctx context.Context) (pgx.Tx, error) {
	if m.beginFn != nil {
		return m.beginFn(ctx)
	}
	return nil, fmt.Errorf("not implemented")
}

// --- NewRouter tests ---

func TestNewRouter_WithMockDB(t *testing.T) {
	db := &mockDB{}
	r := NewRouter(db)
	if r == nil {
		t.Fatal("NewRouter should not return nil")
	}
}

// --- Route tests ---

func TestRoute_Found(t *testing.T) {
	db := &mockDB{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				rows: []func(dest ...any) error{
					func(dest ...any) error {
						// ch fields: id, name, provider, base_url, models, model_mapping, max_context, default_max_output, priority, status, circuit_state, half_open_until, fail_count
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "openai"
						*dest[2].(*string) = "openai"
						*dest[3].(*string) = "https://api.openai.com"
						*dest[4].(*string) = "gpt-4o,gpt-3.5-turbo"
						*dest[5].(*json.RawMessage) = json.RawMessage(`{}`)
						*dest[6].(*int) = 128000
						*dest[7].(*int) = 4096
						*dest[8].(*int) = 100
						*dest[9].(*string) = "active"
						*dest[10].(*string) = "healthy"
						*dest[11].(**time.Time) = nil
						*dest[12].(*int) = 0
						// ck fields: id, key_enc, status, cooldown_until
						*dest[13].(*int64) = 1
						*dest[14].(*string) = "encrypted-key"
						*dest[15].(*string) = "active"
						*dest[16].(**time.Time) = nil
						return nil
					},
				},
			}, nil
		},
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	r := NewRouter(db)
	result, err := r.Route(context.Background(), "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Channel.ID != 1 {
		t.Fatalf("expected channel ID 1, got %d", result.Channel.ID)
	}
	if result.ChannelKey.ID != 1 {
		t.Fatalf("expected key ID 1, got %d", result.ChannelKey.ID)
	}
	if result.MappedModel != "gpt-4o" {
		t.Fatalf("expected gpt-4o, got %s", result.MappedModel)
	}
}

func TestRoute_NotFound(t *testing.T) {
	db := &mockDB{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{}, nil // empty rows
		},
	}
	r := NewRouter(db)
	_, err := r.Route(context.Background(), "nonexistent-model")
	if err == nil {
		t.Fatal("expected error for nonexistent model")
	}
}

func TestRoute_QueryError(t *testing.T) {
	db := &mockDB{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db connection failed")
		},
	}
	r := NewRouter(db)
	_, err := r.Route(context.Background(), "gpt-4o")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- RouteAll tests ---

func TestRouteAll_MultipleChannels(t *testing.T) {
	callCount := 0
	db := &mockDB{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "ch1"
						*dest[2].(*string) = "openai"
						*dest[3].(*string) = "https://api.openai.com"
						*dest[4].(*string) = "gpt-4o"
						*dest[5].(*json.RawMessage) = json.RawMessage(`{}`)
						*dest[6].(*int) = 128000
						*dest[7].(*int) = 4096
						*dest[8].(*int) = 100
						*dest[9].(*string) = "active"
						*dest[10].(*string) = "healthy"
						*dest[11].(**time.Time) = nil
						*dest[12].(*int) = 0
						*dest[13].(*int64) = 1
						*dest[14].(*string) = "key1"
						*dest[15].(*string) = "active"
						*dest[16].(**time.Time) = nil
						callCount++
						return nil
					},
					func(dest ...any) error {
						*dest[0].(*int64) = 2
						*dest[1].(*string) = "ch2"
						*dest[2].(*string) = "anthropic"
						*dest[3].(*string) = "https://api.anthropic.com"
						*dest[4].(*string) = "gpt-4o"
						*dest[5].(*json.RawMessage) = json.RawMessage(`{}`)
						*dest[6].(*int) = 128000
						*dest[7].(*int) = 4096
						*dest[8].(*int) = 200
						*dest[9].(*string) = "active"
						*dest[10].(*string) = "healthy"
						*dest[11].(**time.Time) = nil
						*dest[12].(*int) = 0
						*dest[13].(*int64) = 2
						*dest[14].(*string) = "key2"
						*dest[15].(*string) = "active"
						*dest[16].(**time.Time) = nil
						callCount++
						return nil
					},
				},
			}, nil
		},
	}
	r := NewRouter(db)
	results, err := r.RouteAll(context.Background(), "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestRouteAll_Empty(t *testing.T) {
	db := &mockDB{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{}, nil
		},
	}
	r := NewRouter(db)
	_, err := r.RouteAll(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for empty results")
	}
}

// --- MarkChannelSuccess tests ---

func TestMarkChannelSuccess_OK(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	r := NewRouter(db)
	err := r.MarkChannelSuccess(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMarkChannelSuccess_Error(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db error")
		},
	}
	r := NewRouter(db)
	err := r.MarkChannelSuccess(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- MarkChannelFail tests ---

func TestMarkChannelFail_BelowThreshold(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int) = 2 // fail_count = 2, threshold = 5
					return nil
				},
			}
		},
	}
	r := NewRouter(db)
	err := r.MarkChannelFail(context.Background(), 1, 5, 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMarkChannelFail_AtThreshold_TransitionsToHalfOpen(t *testing.T) {
	execCalled := false
	db := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int) = 5 // fail_count = 5, threshold = 5
					return nil
				},
			}
		},
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execCalled = true
			return pgconn.CommandTag{}, nil
		},
	}
	r := NewRouter(db)
	err := r.MarkChannelFail(context.Background(), 1, 5, 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !execCalled {
		t.Fatal("expected half_open transition exec")
	}
}

func TestMarkChannelFail_QueryError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFn: func(dest ...any) error {
					return fmt.Errorf("db error")
				},
			}
		},
	}
	r := NewRouter(db)
	err := r.MarkChannelFail(context.Background(), 1, 5, 30)
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- MarkKeyCooldown tests ---

func TestMarkKeyCooldown_OK(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	r := NewRouter(db)
	err := r.MarkKeyCooldown(context.Background(), 1, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- MarkKeyFail tests ---

func TestMarkKeyFail_BelowThreshold(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int) = 2 // fail_count = 2, threshold = 3
					return nil
				},
			}
		},
	}
	r := NewRouter(db)
	err := r.MarkKeyFail(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMarkKeyFail_AtThreshold_Invalidate(t *testing.T) {
	execCalled := false
	db := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int) = 3 // fail_count = 3
					return nil
				},
			}
		},
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execCalled = true
			return pgconn.CommandTag{}, nil
		},
	}
	r := NewRouter(db)
	err := r.MarkKeyFail(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !execCalled {
		t.Fatal("expected invalidation exec")
	}
}

// --- MarkKeySuccess tests ---

func TestMarkKeySuccess_OK(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	r := NewRouter(db)
	err := r.MarkKeySuccess(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- resolveModelMapping tests (补充) ---

func TestResolveModelMapping_WithMapping(t *testing.T) {
	mapping := json.RawMessage(`{"gpt-4": "gpt-4-0613"}`)
	result := resolveModelMapping("gpt-4", mapping)
	if result != "gpt-4-0613" {
		t.Fatalf("expected gpt-4-0613, got %s", result)
	}
}

func TestResolveModelMapping_UnmappedModel(t *testing.T) {
	mapping := json.RawMessage(`{"gpt-4": "gpt-4-0613"}`)
	result := resolveModelMapping("gpt-3.5-turbo", mapping)
	if result != "gpt-3.5-turbo" {
		t.Fatalf("expected gpt-3.5-turbo, got %s", result)
	}
}

var _ = pgtype.Int4{} // suppress unused import
var _ = time.Now()    // suppress unused import

// --- Route scan error path ---

func TestRoute_ScanError(t *testing.T) {
	db := &mockDB{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				rows: []func(dest ...any) error{
					func(dest ...any) error {
						return fmt.Errorf("scan error")
					},
				},
			}, nil
		},
	}
	r := NewRouter(db)
	_, err := r.Route(context.Background(), "gpt-4o")
	if err == nil {
		t.Fatal("expected error for scan failure")
	}
}

// --- RouteAll scan error path ---

func TestRouteAll_ScanError(t *testing.T) {
	db := &mockDB{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				rows: []func(dest ...any) error{
					func(dest ...any) error {
						return fmt.Errorf("scan error")
					},
				},
			}, nil
		},
	}
	r := NewRouter(db)
	_, err := r.RouteAll(context.Background(), "gpt-4o")
	if err == nil {
		t.Fatal("expected error for scan failure")
	}
}

// --- MarkKeyFail scan error path ---

func TestMarkKeyFail_ScanError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFn: func(dest ...any) error {
					return fmt.Errorf("scan error")
				},
			}
		},
	}
	r := NewRouter(db)
	err := r.MarkKeyFail(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error for scan failure")
	}
}
