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
	"github.com/uniroute/unirouter/internal/testutil"
)

// --- NewRouter tests ---

func TestNewRouter_WithMockDB(t *testing.T) {
	db := &testutil.MockDB{}
	r := NewRouter(db)
	if r == nil {
		t.Fatal("NewRouter should not return nil")
	}
}

// --- Route tests ---

func TestRoute_Found(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						// ch fields: id, name, provider, base_url, models, model_mapping, adapter_type, adapter_config, max_context, default_max_output, priority, status, circuit_state, half_open_until, fail_count
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "openai"
						*dest[2].(*string) = "openai"
						*dest[3].(*string) = "https://api.openai.com"
						*dest[4].(*string) = "gpt-4o,gpt-3.5-turbo"
						*dest[5].(*json.RawMessage) = json.RawMessage(`{}`)
						*dest[6].(*string) = "openai_compatible"
						*dest[7].(*json.RawMessage) = json.RawMessage(`{}`)
						*dest[8].(*int) = 128000
						*dest[9].(*int) = 4096
						*dest[10].(*int) = 100
						*dest[11].(*string) = "active"
						*dest[12].(*string) = "healthy"
						*dest[13].(**time.Time) = nil
						*dest[14].(*int) = 0
						// ck fields: id, key_enc, status, cooldown_until
						*dest[15].(*int64) = 1
						*dest[16].(*string) = "encrypted-key"
						*dest[17].(*string) = "active"
						*dest[18].(**time.Time) = nil
						return nil
					},
				},
			}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
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
	if result.Channel.AdapterType != "openai_compatible" {
		t.Fatalf("expected adapter_type openai_compatible, got %q", result.Channel.AdapterType)
	}
}

func TestRoute_NotFound(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil // empty rows
		},
	}
	r := NewRouter(db)
	_, err := r.Route(context.Background(), "nonexistent-model")
	if err == nil {
		t.Fatal("expected error for nonexistent model")
	}
}

func TestRoute_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
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
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "ch1"
						*dest[2].(*string) = "openai"
						*dest[3].(*string) = "https://api.openai.com"
						*dest[4].(*string) = "gpt-4o"
						*dest[5].(*json.RawMessage) = json.RawMessage(`{}`)
						*dest[6].(*string) = "openai_compatible"
						*dest[7].(*json.RawMessage) = json.RawMessage(`{}`)
						*dest[8].(*int) = 128000
						*dest[9].(*int) = 4096
						*dest[10].(*int) = 100
						*dest[11].(*string) = "active"
						*dest[12].(*string) = "healthy"
						*dest[13].(**time.Time) = nil
						*dest[14].(*int) = 0
						*dest[15].(*int64) = 1
						*dest[16].(*string) = "key1"
						*dest[17].(*string) = "active"
						*dest[18].(**time.Time) = nil
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
						*dest[6].(*string) = "openai_compatible"
						*dest[7].(*json.RawMessage) = json.RawMessage(`{}`)
						*dest[8].(*int) = 128000
						*dest[9].(*int) = 4096
						*dest[10].(*int) = 200
						*dest[11].(*string) = "active"
						*dest[12].(*string) = "healthy"
						*dest[13].(**time.Time) = nil
						*dest[14].(*int) = 0
						*dest[15].(*int64) = 2
						*dest[16].(*string) = "key2"
						*dest[17].(*string) = "active"
						*dest[18].(**time.Time) = nil
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
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil
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
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
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
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
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
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
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
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 5 // fail_count = 5, threshold = 5
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
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
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
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
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
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
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
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
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 3 // fail_count = 3
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
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
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
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
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
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
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
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
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
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
