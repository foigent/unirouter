package completedfeed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompletedExecutionContractGoldenFixtures(t *testing.T) {
	fixtureDir := filepath.Join("..", "..", "contracts", "completed-execution", "v1", "fixtures")

	for _, name := range []string{"valid-settled.json", "valid-cancelled.json"} {
		payload, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := DecodeSnapshot(payload); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
	}

	redactionPayload, err := os.ReadFile(filepath.Join(fixtureDir, "redaction-rejected.json"))
	if err != nil {
		t.Fatalf("read redaction fixture: %v", err)
	}
	if _, err := DecodeSnapshot(redactionPayload); err == nil || !strings.Contains(err.Error(), "prohibited raw field") {
		t.Fatalf("expected prohibited raw field rejection, got %v", err)
	}

	errorPayload, err := os.ReadFile(filepath.Join(fixtureDir, "error-cursor-expired.json"))
	if err != nil {
		t.Fatalf("read error fixture: %v", err)
	}
	var response ErrorResponse
	if err := json.Unmarshal(errorPayload, &response); err != nil {
		t.Fatalf("decode error fixture: %v", err)
	}
	if response.Error.Code != "completed_execution_feed.cursor_expired" || response.Error.Retryable {
		t.Fatalf("unexpected error fixture: %#v", response)
	}
}

func TestBuildSnapshotIsDeterministicAndFinal(t *testing.T) {
	input := SnapshotInput{
		RequestID:            "req_contract_deterministic",
		OrgID:                11,
		ProjectID:            22,
		TerminalStatus:       TerminalSettled,
		TerminalAvailableAt:  time.Date(2026, 7, 25, 4, 5, 6, 0, time.UTC),
		Model:                "synthetic-model",
		PromptTokens:         8,
		UncachedPromptTokens: 5,
		CacheReadTokens:      2,
		CacheCreationTokens:  1,
		CompletionTokens:     3,
		UsageSource:          "provider",
		UsageAuthority:       "provider",
		UsagePurpose:         "billing",
		UsageFinality:        "final",
		UsageContractVersion: "usage.v1",
		CacheUsageStatus:     "reported",
		CalculatedCostMicros: 17,
		ChargedCostMicros:    17,
		PricingVersion:       "pricing-v1",
	}
	first, firstPayload, firstHash, err := BuildSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	_, secondPayload, secondHash, err := BuildSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstPayload) != string(secondPayload) || firstHash != secondHash {
		t.Fatal("snapshot encoding and hash must be deterministic")
	}
	if len(first.CaptureGaps) != 4 {
		t.Fatalf("expected fixed capture gaps, got %d", len(first.CaptureGaps))
	}
	if SnapshotRevision != "1" {
		t.Fatalf("unexpected immutable revision %q", SnapshotRevision)
	}
}

func TestCompletedExecutionContractRejectsInvalidMoneyAndTokenPartition(t *testing.T) {
	_, _, _, err := BuildSnapshot(SnapshotInput{
		RequestID:            "req_invalid",
		OrgID:                1,
		ProjectID:            2,
		TerminalStatus:       TerminalSettled,
		TerminalAvailableAt:  time.Now().UTC(),
		Model:                "synthetic-model",
		PromptTokens:         2,
		UncachedPromptTokens: 1,
		CalculatedCostMicros: -1,
	})
	if err == nil {
		t.Fatal("invalid token partition and money must fail closed")
	}
}
