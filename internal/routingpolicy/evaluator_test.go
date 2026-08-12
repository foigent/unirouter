package routingpolicy

import (
	"fmt"
	"testing"
	"time"
)

func TestEvaluateSelectionIsDeterministicAndPriorityBounded(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	snapshot := SelectionSnapshot{
		Model: "gpt-route-plan",
		DBNow: now,
		Channels: []SelectionChannel{
			{ID: 1, Name: "one", Provider: "openai", Priority: 10, Weight: 100, Status: "active", CircuitState: "healthy", Keys: []SelectionKey{{ID: 11, Status: "active"}}},
			{ID: 2, Name: "two", Provider: "azure", Priority: 10, Weight: 300, Status: "active", CircuitState: "healthy", Keys: []SelectionKey{{ID: 22, Status: "active"}}},
			{ID: 3, Name: "later", Provider: "anthropic", Priority: 20, Weight: 1000, Status: "active", CircuitState: "healthy", Keys: []SelectionKey{{ID: 33, Status: "active"}}},
		},
	}
	firstByChannel := map[int64]int{}
	for index := 0; index < 2000; index++ {
		key := fmt.Sprintf("server-request-%d", index)
		first := EvaluateSelection(snapshot, Default(), key)
		second := EvaluateSelection(snapshot, Default(), key)
		if len(first.Selected) != 3 || first.Selected[2].ChannelID != 3 {
			t.Fatalf("priority tier crossed: %#v", first.Selected)
		}
		if first.Selected[0].ChannelID != second.Selected[0].ChannelID ||
			first.Selected[1].ChannelID != second.Selected[1].ChannelID {
			t.Fatalf("same selection key drifted: %#v %#v", first.Selected, second.Selected)
		}
		firstByChannel[first.Selected[0].ChannelID]++
	}
	if firstByChannel[2] < 1350 || firstByChannel[2] > 1650 {
		t.Fatalf("300:100 distribution outside deterministic fixture tolerance: %#v", firstByChannel)
	}
}

func TestEvaluateSelectionExplainsCredentialFreeExclusions(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	cooldown := now.Add(time.Minute)
	result := EvaluateSelection(SelectionSnapshot{
		Model: "gpt-route-plan",
		DBNow: now,
		Channels: []SelectionChannel{
			{ID: 1, Name: "eligible", Provider: "openai", Priority: 10, Weight: 100, Status: "active", CircuitState: "healthy", Keys: []SelectionKey{{ID: 11, Status: "active"}}},
			{ID: 2, Name: "disabled", Provider: "azure", Priority: 10, Weight: 100, Status: "disabled", CircuitState: "healthy", Keys: []SelectionKey{{ID: 22, Status: "active"}}},
			{ID: 3, Name: "cooldown", Provider: "anthropic", Priority: 10, Weight: 100, Status: "active", CircuitState: "healthy", Keys: []SelectionKey{{ID: 33, Status: "active", CooldownUntil: &cooldown}}},
			{ID: 4, Name: "missing", Provider: "custom", Priority: 10, Weight: 100, Status: "active", CircuitState: "healthy"},
		},
	}, Default(), "preview:gpt-route-plan")
	if len(result.Selected) != 1 || len(result.Exclusions) != 3 || result.ExcludedCount != 3 {
		t.Fatalf("unexpected evaluation: %#v", result)
	}
	if result.ExclusionReasons != "channel_inactive=1,key_cooldown=1,no_key=1" {
		t.Fatalf("unexpected reason summary: %q", result.ExclusionReasons)
	}
}

func TestModelAllowedAcceptsHistoricalListSeparators(t *testing.T) {
	allowed := "kimi-k3;kimi-k2.7-code；kimi-k2.6\ncompany-chat"
	if !modelAllowed(allowed, "kimi-k2.7-code") {
		t.Fatal("historical semicolon-delimited credential scope was rejected")
	}
	if modelAllowed(allowed, "kimi-k2.7") {
		t.Fatal("partial model identifier matched credential scope")
	}
}
