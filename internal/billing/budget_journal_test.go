package billing

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/outbox"
)

func TestSourceBudgetInspectsAllOriginalFinancialTypesWithoutCalculatedSpend(t *testing.T) {
	at := time.Date(2026, 9, 10, 0, 0, 0, 123456789, time.UTC)
	calculated, charged := int64(40000), int64(0)
	for _, tc := range []struct{ envelope, raw, kind string }{
		{outbox.EventBillingUsagePrepared, "usage_prepared", "usage_prepared"},
		{outbox.EventBillingSettled, "settled", "settled"},
		{outbox.EventBillingCancelled, "cancelled", "cancelled"},
		{outbox.EventBillingUsagePreparedV1, "usage_prepared", "usage_prepared"},
		{outbox.EventBillingSettledV1, "settled", "settled"},
		{outbox.EventBillingCancelledV1, "cancelled", "cancelled"},
		{outbox.EventBillingUsagePrepared, OutputHoldReasonGuardrailWithheld, "usage_prepared"},
	} {
		t.Run(tc.envelope+"/"+tc.raw, func(t *testing.T) {
			p := usageJournalPayload{EventType: tc.raw, RequestID: "synthetic-journal", OrgID: 1, ProjectID: 2, OccurredAt: at,
				MoneySchema: outbox.MoneyVersion, Currency: "CNY", AmountScale: 1000000, CalculatedCostMicros: "400000000", ChargedCostMicros: "0", CalculatedCostCents: &calculated, ChargedCostCents: &charged}
			makeEvent := func(p usageJournalPayload) outbox.Event {
				wire, err := json.Marshal(p)
				require.NoError(t, err)
				return outbox.Event{Key: billingEventKey(tc.raw, 1, p.RequestID), Type: tc.envelope, OrgID: 1, RequestID: p.RequestID, OccurredAt: at.Truncate(time.Microsecond), Payload: wire}
			}
			event := makeEvent(p)
			got, err := InspectBudgetJournalEvent(event)
			require.NoError(t, err)
			require.Equal(t, tc.kind, got.EventType)
			require.Zero(t, got.ChargedMicros)
			require.EqualValues(t, 2, got.ProjectID)
			for _, mutate := range []func(*outbox.Event){
				func(e *outbox.Event) { e.OrgID = 3 }, func(e *outbox.Event) { e.RequestID = "another" },
				func(e *outbox.Event) { e.Key = "billing:wrong" }, func(e *outbox.Event) { e.OccurredAt = e.OccurredAt.Add(time.Microsecond) },
				func(e *outbox.Event) { e.Type = "not-a-financial-event" }, func(e *outbox.Event) { e.Payload = json.RawMessage(`{"broken":`) },
			} {
				bad := event
				mutate(&bad)
				_, err = InspectBudgetJournalEvent(bad)
				require.Error(t, err)
			}
			if tc.envelope == outbox.EventBillingSettled {
				for _, mutate := range []func(*usageJournalPayload){
					func(p *usageJournalPayload) { p.Currency = "USD" }, func(p *usageJournalPayload) { p.AmountScale = 100 },
					func(p *usageJournalPayload) { p.MoneySchema = "unknown" }, func(p *usageJournalPayload) { p.ChargedCostMicros = "00" },
					func(p *usageJournalPayload) { p.EventType = "cancelled" }, func(p *usageJournalPayload) { p.ProjectID = -1 },
				} {
					bad := p
					mutate(&bad)
					_, err = InspectBudgetJournalEvent(makeEvent(bad))
					require.Error(t, err)
				}
			}
			if tc.envelope == outbox.EventBillingSettledV1 {
				bad := p
				bad.ChargedCostCents = nil
				_, err = InspectBudgetJournalEvent(makeEvent(bad))
				require.Error(t, err)
				overflow := int64(math.MaxInt64)
				bad = p
				bad.CalculatedCostCents = &overflow
				_, err = InspectBudgetJournalEvent(makeEvent(bad))
				require.Error(t, err)
			}
		})
	}
}
