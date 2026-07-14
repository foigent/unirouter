package billing

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestFreezeRequest_Fields(t *testing.T) {
	req := FreezeRequest{
		RequestID:       "req-001",
		OrgID:           1,
		Model:           "gpt-4o",
		EstimatedTokens: 1000,
		SafetyFactor:    "1.5",
		MinFreezeCents:  10,
	}
	if req.RequestID != "req-001" {
		t.Fatalf("RequestID mismatch: %s", req.RequestID)
	}
	if req.SafetyFactor != "1.5" {
		t.Fatalf("SafetyFactor mismatch: %s", req.SafetyFactor)
	}
}

func TestFreezeResult_Fields(t *testing.T) {
	result := FreezeResult{
		FreezeID:         42,
		AmountMicros:     100,
		NewBalanceMicros: 900,
		Success:          true,
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.AmountMicros != 100 {
		t.Fatalf("AmountMicros mismatch: %d", result.AmountMicros)
	}
}

func TestFreezeResult_Error(t *testing.T) {
	result := FreezeResult{
		Success:     false,
		ErrorCode:   "insufficient_balance",
		ErrorDetail: "余额不足",
	}
	if result.Success {
		t.Fatal("should not be success")
	}
	if result.ErrorCode != "insufficient_balance" {
		t.Fatalf("ErrorCode mismatch: %s", result.ErrorCode)
	}
}

func TestSettleRequest_Fields(t *testing.T) {
	req := SettleRequest{
		RequestID:            "req-002",
		OrgID:                1,
		PromptTokens:         500,
		UncachedPromptTokens: 500,
		CompletionTokens:     300,
		UsageSource:          "upstream",
	}
	if req.PromptTokens != 500 || req.UncachedPromptTokens != 500 {
		t.Fatalf("prompt token partition mismatch: total=%d uncached=%d", req.PromptTokens, req.UncachedPromptTokens)
	}
}

func TestNormalizeSettlementUsageRejectsPersistedIntegerOverflow(t *testing.T) {
	_, err := normalizeSettlementUsage(SettleRequest{
		PromptTokens:         maxPersistedTokenCount,
		UncachedPromptTokens: maxPersistedTokenCount,
		CompletionTokens:     1,
	})
	if err == nil {
		t.Fatal("expected prompt+completion integer overflow to be rejected")
	}
}

func TestSettleResult_Fields(t *testing.T) {
	result := SettleResult{
		SettleID:         43,
		RefundMicros:     15,
		NewBalanceMicros: 900,
		Success:          true,
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.RefundMicros != 15 {
		t.Fatalf("RefundMicros mismatch: %d", result.RefundMicros)
	}
}

func TestSettleResult_Error(t *testing.T) {
	result := SettleResult{
		Success:   false,
		ErrorCode: "freeze_not_found",
	}
	if result.Success {
		t.Fatal("should not be success")
	}
}

func TestBilling_Constructor_NilDB(t *testing.T) {
	// Constructor should work even with nil deps (won't panic until used)
	b := NewBilling(nil, nil)
	if b == nil {
		t.Fatal("NewBilling should not return nil")
	}
}

func TestIsUniqueViolation(t *testing.T) {
	if !isUniqueViolation(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("expected PostgreSQL 23505 to be treated as unique violation")
	}
	if isUniqueViolation(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("foreign-key violation must not be treated as unique violation")
	}
}
