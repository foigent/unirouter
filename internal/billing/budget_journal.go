package billing

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/uniroute/unirouter/internal/outbox"
)

// BudgetJournalObservation reuses the original financial envelope decoder. It
// exposes only scope, financial event time and original money; it never writes.
type BudgetJournalObservation struct {
	OrgID, ProjectID     int64
	RequestID, EventType string
	OccurredAt           time.Time
	ChargedMicros        int64
}

func InspectBudgetJournalEvent(event outbox.Event) (BudgetJournalObservation, error) {
	var result BudgetJournalObservation
	var p usageJournalPayload
	if err := json.Unmarshal(event.Payload, &p); err != nil {
		return result, err
	}
	if p.OrgID <= 0 || p.ProjectID < 0 || p.RequestID == "" || p.OccurredAt.IsZero() || p.OrgID != event.OrgID || p.RequestID != event.RequestID || event.Key != billingEventKey(p.EventType, p.OrgID, p.RequestID) || !p.OccurredAt.Truncate(time.Microsecond).Equal(event.OccurredAt.Truncate(time.Microsecond)) {
		return result, fmt.Errorf("financial outbox scope or time differs from original payload")
	}
	kind := normalizeUsageJournalEventType(event.Type, p.EventType)
	expected := ""
	switch event.Type {
	case outbox.EventBillingUsagePrepared, outbox.EventBillingUsagePreparedV1:
		expected = "usage_prepared"
	case outbox.EventBillingSettled, outbox.EventBillingSettledV1:
		expected = "settled"
	case outbox.EventBillingCancelled, outbox.EventBillingCancelledV1:
		expected = "cancelled"
	default:
		return result, fmt.Errorf("not a financial journal event")
	}
	if kind != expected {
		return result, fmt.Errorf("financial outbox event type differs from journal payload")
	}
	_, charged, err := decodeUsageJournalMoney(event.Type, p)
	if err != nil {
		return result, err
	}
	return BudgetJournalObservation{p.OrgID, p.ProjectID, p.RequestID, kind, p.OccurredAt.Truncate(time.Microsecond), charged}, nil
}
