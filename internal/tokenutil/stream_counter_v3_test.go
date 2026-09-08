package tokenutil

import (
	"github.com/uniroute/unirouter/internal/metering"
	"math/rand"
	"testing"
)

func TestCumulativeCounterIsInvariantToDecodedPartitions(t *testing.T) {
	for _, text := range []string{"", "hello", "你好世界", "hello世界 👩‍💻e\u0301\n", `{"value":"中英混合💡","count":42}`} {
		runes := []rune(text)
		for seed := int64(0); seed < 100; seed++ {
			c, err := NewStreamTokenCounterWithVersion(metering.GatewayCounterEstimateVersionV3)
			if err != nil {
				t.Fatal(err)
			}
			rng := rand.New(rand.NewSource(seed))
			for start := 0; start < len(runes); {
				end := start + 1 + rng.Intn(len(runes)-start)
				if err := c.AddOutputDelta(string(runes[start:end])); err != nil {
					t.Fatal(err)
				}
				start = end
			}
			if got, want := c.OutputTokens(), EstimateTokens(text); got != want {
				t.Fatalf("seed=%d text=%q got=%d want=%d", seed, text, got, want)
			}
		}
	}
	legacy := NewStreamTokenCounter()
	for _, s := range []string{"h", "e", "l", "l", "o"} {
		if err := legacy.AddOutputDelta(s); err != nil {
			t.Fatal(err)
		}
	}
	if legacy.OutputTokens() != 5 || legacy.Version() != metering.GatewayCounterEstimateVersionV2 {
		t.Fatal("legacy interpretation changed")
	}
}

func TestCumulativeCounterRejectsUnknownInvalidAndOverflowWithoutPartialAdvance(t *testing.T) {
	for _, version := range []string{"", "current", "unirouter.stream-counter.v4"} {
		if _, err := NewStreamTokenCounterWithVersion(version); err == nil {
			t.Fatal("unknown accepted", version)
		}
	}
	c, _ := NewStreamTokenCounterWithVersion(metering.GatewayCounterEstimateVersionV3)
	if err := c.AddOutputDelta("hello"); err != nil {
		t.Fatal(err)
	}
	if err := c.AddOutputDelta(string([]byte{0xe4})); err == nil || c.OutputTokens() != 2 {
		t.Fatal("invalid decoded text advanced")
	}
	c.outputUnits = uint64(metering.MaxPersistedTokenCount)*12 - 2
	before := c.outputUnits
	if err := c.AddOutputDelta("a"); err == nil || c.outputUnits != before {
		t.Fatal("overflow advanced")
	}
	c.SetEstimatedInputTokens(20)
	c.SetUpstreamUsage(30, 40)
	if c.InputTokens() != 30 || c.OutputTokens() != 40 || !c.UsedUpstreamUsage() {
		t.Fatal("provider usage not preserved")
	}
	if err := c.AddOutputDelta("a"); err == nil || c.OutputTokens() != 40 {
		t.Fatal("provider usage mixed with estimate")
	}
}
