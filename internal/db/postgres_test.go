package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func getTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		dsn = os.Getenv("TEST_DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://uniroute:uniroute@localhost:5432/uniroute_test?sslmode=disable"
	}
	return dsn
}

func TestNewPostgres_Success(t *testing.T) {
	dsn := getTestDSN(t)
	db, err := NewPostgres(dsn, 10, 2)
	if err != nil {
		t.Fatalf("NewPostgres failed: %v", err)
	}
	defer db.Close()

	if db.Pool == nil {
		t.Fatal("expected non-nil pool")
	}
}

func TestNewPostgres_InvalidDSN(t *testing.T) {
	_, err := NewPostgres("invalid-dsn", 10, 2)
	if err == nil {
		t.Fatal("expected error for invalid DSN")
	}
}

func TestNewPostgres_UnreachableHost(t *testing.T) {
	_, err := NewPostgres("postgres://user:pass@192.0.2.1:5432/db?sslmode=disable&connect_timeout=2", 1, 0)
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

func TestPostgres_Health(t *testing.T) {
	dsn := getTestDSN(t)
	db, err := NewPostgres(dsn, 5, 1)
	if err != nil {
		t.Fatalf("NewPostgres failed: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.Health(ctx); err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
}

func TestPostgres_Close(t *testing.T) {
	dsn := getTestDSN(t)
	db, err := NewPostgres(dsn, 5, 1)
	if err != nil {
		t.Fatalf("NewPostgres failed: %v", err)
	}

	db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = db.Health(ctx)
	if err == nil {
		t.Fatal("expected error after Close")
	}
}

func TestPostgres_Tx_Commit(t *testing.T) {
	dsn := getTestDSN(t)
	// TEMP tables belong to one session; all fixture operations must share it.
	db, err := NewPostgres(dsn, 1, 1)
	if err != nil {
		t.Fatalf("NewPostgres failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Create a temp table for testing
	if _, err := db.Pool.Exec(ctx, "CREATE TEMP TABLE tx_test (id int, val text)"); err != nil {
		t.Fatalf("create temp table: %v", err)
	}

	committed := false
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO tx_test VALUES (1, 'hello')")
		if err != nil {
			return fmt.Errorf("insert: %w", err)
		}
		committed = true
		return nil
	})
	if err != nil {
		t.Fatalf("Tx commit failed: %v", err)
	}
	if !committed {
		t.Fatal("expected fn to be called")
	}

	// Verify data was committed
	var val string
	err = db.Pool.QueryRow(ctx, "SELECT val FROM tx_test WHERE id = 1").Scan(&val)
	if err != nil {
		t.Fatalf("query after commit failed: %v", err)
	}
	if val != "hello" {
		t.Fatalf("expected 'hello', got %q", val)
	}
}

func TestPostgres_Tx_Rollback(t *testing.T) {
	dsn := getTestDSN(t)
	// Keep setup, the real transaction and verification in the TEMP session.
	db, err := NewPostgres(dsn, 1, 1)
	if err != nil {
		t.Fatalf("NewPostgres failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Create temp table
	if _, err := db.Pool.Exec(ctx, "CREATE TEMP TABLE tx_rollback_test (id int)"); err != nil {
		t.Fatalf("create temp table: %v", err)
	}

	// Insert a row outside transaction
	if _, err := db.Pool.Exec(ctx, "INSERT INTO tx_rollback_test VALUES (99)"); err != nil {
		t.Fatalf("seed outside transaction: %v", err)
	}

	// Transaction that fails — should rollback
	rollbackErr := errors.New("simulated rollback")
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO tx_rollback_test VALUES (100)")
		if err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected callback rollback error, got %v", err)
	}

	// Verify the failed insert was rolled back (only 99 should exist)
	var count int
	err = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM tx_rollback_test WHERE id = 100").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows (rollback), got %d", count)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM tx_rollback_test WHERE id = 99").Scan(&count); err != nil {
		t.Fatalf("query retained seed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one unchanged outside-transaction row, got %d", count)
	}
}

func TestDBQuerier_InterfaceSatisfied(t *testing.T) {
	dsn := getTestDSN(t)
	db, err := NewPostgres(dsn, 5, 1)
	if err != nil {
		t.Fatalf("NewPostgres failed: %v", err)
	}
	defer db.Close()

	// Verify *pgxpool.Pool satisfies DBQuerier
	var _ DBQuerier = db.Pool
}

func TestNewPostgres_PoolConfig(t *testing.T) {
	dsn := getTestDSN(t)
	db, err := NewPostgres(dsn, 42, 7)
	if err != nil {
		t.Fatalf("NewPostgres failed: %v", err)
	}
	defer db.Close()

	stat := db.Pool.Stat()
	if stat.MaxConns() != 42 {
		t.Fatalf("expected MaxConns=42, got %d", stat.MaxConns())
	}
}

func TestPostgres_Tx_BeginError(t *testing.T) {
	dsn := getTestDSN(t)
	db, err := NewPostgres(dsn, 5, 1)
	if err != nil {
		t.Fatalf("NewPostgres failed: %v", err)
	}
	defer db.Close()

	// Close pool to cause Begin to fail
	db.Close()

	ctx := context.Background()
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error when pool is closed")
	}
}
