package redisx

import (
	"context"
	"os"
	"testing"
	"time"
)

func getTestRedisAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	return addr
}

func TestNewRedis_Success(t *testing.T) {
	addr := getTestRedisAddr(t)
	r, err := NewRedis(addr, "", 0, 10)
	if err != nil {
		t.Fatalf("NewRedis failed: %v", err)
	}
	defer r.Close()

	if r.Client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewRedis_InvalidAddr(t *testing.T) {
	_, err := NewRedis("192.0.2.1:9999", "", 0, 1)
	if err == nil {
		t.Fatal("expected error for invalid addr")
	}
}

func TestRedis_Health(t *testing.T) {
	addr := getTestRedisAddr(t)
	r, err := NewRedis(addr, "", 0, 5)
	if err != nil {
		t.Fatalf("NewRedis failed: %v", err)
	}
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := r.Health(ctx); err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
}

func TestRedis_Close(t *testing.T) {
	addr := getTestRedisAddr(t)
	r, err := NewRedis(addr, "", 0, 5)
	if err != nil {
		t.Fatalf("NewRedis failed: %v", err)
	}

	r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = r.Health(ctx)
	if err == nil {
		t.Fatal("expected error after Close")
	}
}

func TestRedis_SetAndGetBalance(t *testing.T) {
	addr := getTestRedisAddr(t)
	r, err := NewRedis(addr, "", 15, 5) // use DB 15 for testing
	if err != nil {
		t.Fatalf("NewRedis failed: %v", err)
	}
	defer r.Close()
	defer r.Client.FlushDB(context.Background())

	ctx := context.Background()
	orgID := int64(99999)

	// Set balance
	err = r.SetBalance(ctx, orgID, 10000, 500)
	if err != nil {
		t.Fatalf("SetBalance failed: %v", err)
	}

	// Get balance
	balance, frozen, err := r.GetBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}
	if balance != 10000 {
		t.Fatalf("expected balance=10000, got %d", balance)
	}
	if frozen != 500 {
		t.Fatalf("expected frozen=500, got %d", frozen)
	}
}

func TestRedis_GetBalance_NonExistent(t *testing.T) {
	addr := getTestRedisAddr(t)
	r, err := NewRedis(addr, "", 15, 5)
	if err != nil {
		t.Fatalf("NewRedis failed: %v", err)
	}
	defer r.Close()
	defer r.Client.FlushDB(context.Background())

	ctx := context.Background()

	// Get balance for non-existent org
	balance, frozen, err := r.GetBalance(ctx, 12345)
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}
	if balance != 0 {
		t.Fatalf("expected balance=0, got %d", balance)
	}
	if frozen != 0 {
		t.Fatalf("expected frozen=0, got %d", frozen)
	}
}

func TestRedis_InvalidateBalance(t *testing.T) {
	addr := getTestRedisAddr(t)
	r, err := NewRedis(addr, "", 15, 5)
	if err != nil {
		t.Fatalf("NewRedis failed: %v", err)
	}
	defer r.Close()
	defer r.Client.FlushDB(context.Background())

	ctx := context.Background()
	orgID := int64(88888)

	// Set then invalidate
	r.SetBalance(ctx, orgID, 5000, 100)
	err = r.InvalidateBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("InvalidateBalance failed: %v", err)
	}

	// Verify gone
	balance, frozen, err := r.GetBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}
	if balance != 0 || frozen != 0 {
		t.Fatalf("expected 0/0 after invalidate, got %d/%d", balance, frozen)
	}
}

func TestRedis_SetBalance_Overwrite(t *testing.T) {
	addr := getTestRedisAddr(t)
	r, err := NewRedis(addr, "", 15, 5)
	if err != nil {
		t.Fatalf("NewRedis failed: %v", err)
	}
	defer r.Close()
	defer r.Client.FlushDB(context.Background())

	ctx := context.Background()
	orgID := int64(77777)

	// Set twice
	r.SetBalance(ctx, orgID, 1000, 100)
	r.SetBalance(ctx, orgID, 2000, 200)

	balance, frozen, err := r.GetBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}
	if balance != 2000 {
		t.Fatalf("expected balance=2000, got %d", balance)
	}
	if frozen != 200 {
		t.Fatalf("expected frozen=200, got %d", frozen)
	}
}

func TestRedis_InvalidateBalance_NonExistent(t *testing.T) {
	addr := getTestRedisAddr(t)
	r, err := NewRedis(addr, "", 15, 5)
	if err != nil {
		t.Fatalf("NewRedis failed: %v", err)
	}
	defer r.Close()
	defer r.Client.FlushDB(context.Background())

	ctx := context.Background()

	// Invalidate non-existent key should not error
	err = r.InvalidateBalance(ctx, 12345)
	if err != nil {
		t.Fatalf("InvalidateBalance should not error for non-existent key: %v", err)
	}
}
