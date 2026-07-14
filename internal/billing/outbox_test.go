package billing

import (
	"testing"

	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/outbox"
)

func TestDecodeUsageJournalMoneyRequiresCanonicalV2Envelope(t *testing.T) {
	valid := usageJournalPayload{
		MoneySchema:          outbox.MoneyVersion,
		Currency:             "CNY",
		AmountScale:          money.MicrosPerCNY,
		CalculatedCostMicros: "2",
		ChargedCostMicros:    "1",
	}
	calculated, charged, err := decodeUsageJournalMoney(outbox.EventBillingSettled, valid)
	if err != nil || calculated != 2 || charged != 1 {
		t.Fatalf("valid v2 envelope: calculated=%d charged=%d err=%v", calculated, charged, err)
	}

	invalid := []usageJournalPayload{
		{MoneySchema: outbox.MoneyVersion, Currency: "USD", AmountScale: money.MicrosPerCNY, CalculatedCostMicros: "2", ChargedCostMicros: "1"},
		{MoneySchema: outbox.MoneyVersion, Currency: "CNY", AmountScale: 100, CalculatedCostMicros: "2", ChargedCostMicros: "1"},
		{MoneySchema: outbox.MoneyVersion, Currency: "CNY", AmountScale: money.MicrosPerCNY, CalculatedCostMicros: "02", ChargedCostMicros: "1"},
		{MoneySchema: outbox.MoneyVersion, Currency: "CNY", AmountScale: money.MicrosPerCNY, CalculatedCostMicros: "2", ChargedCostMicros: "+1"},
	}
	for i, payload := range invalid {
		if _, _, err := decodeUsageJournalMoney(outbox.EventBillingSettled, payload); err == nil {
			t.Fatalf("invalid v2 envelope %d was accepted: %#v", i, payload)
		}
	}
}
