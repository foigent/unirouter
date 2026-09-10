package sourcefacts

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/money"
)

type BudgetAlert struct {
	EventRef           string       `json:"event_ref"`
	TriggeredAt        string       `json:"triggered_at"`
	TriggerEvidence    string       `json:"trigger_evidence"`
	BudgetVersionState string       `json:"budget_version_state"`
	BudgetAtTrigger    *BudgetMoney `json:"budget_at_trigger,omitempty"`
	SpentAtTrigger     *BudgetMoney `json:"spent_at_trigger,omitempty"`
	PeriodKind         string       `json:"period_kind,omitempty"`
	PeriodStart        string       `json:"period_start,omitempty"`
	ThresholdPercent   string       `json:"threshold_percent,omitempty"`
	Status             string       `json:"status"`
	SourceUpdatedAt    string       `json:"source_updated_at"`
	StatusCutoff       string       `json:"status_cutoff"`
	DeliveryState      string       `json:"delivery_state"`
	LastDeliveryAt     string       `json:"last_delivery_at,omitempty"`
	DeliveryCutoff     string       `json:"delivery_cutoff"`
	HumanRead          string       `json:"human_read"`
}
type BudgetAlerts struct {
	Coverage   string        `json:"coverage"`
	Entries    []BudgetAlert `json:"entries"`
	HasMore    bool          `json:"has_more"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func readBudgetAlertsTx(ctx context.Context, tx pgx.Tx, a budgetAccess, record budgetRecord, after int64, limit int, cutoff time.Time) (BudgetAlerts, int64, error) {
	result := BudgetAlerts{Coverage: "project_budget_notifications", Entries: []BudgetAlert{}}
	if record.entry.Origin != "gateway_project" {
		result.Coverage = "not_modeled_by_original_authority"
		return result, 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT id,payload,status,delivery_state,created_at,updated_at,last_delivery_at
 FROM notification_events WHERE org_id=$1 AND project_id=$2 AND event_type=$3 AND id>$4 ORDER BY id LIMIT $5`, a.org, a.project, billing.EventProjectBudgetThreshold, after, limit+1)
	if err != nil {
		return result, 0, err
	}
	defer rows.Close()
	var last int64
	for rows.Next() {
		var id int64
		var payload []byte
		var item BudgetAlert
		var created, updated time.Time
		var delivery *time.Time
		if err = rows.Scan(&id, &payload, &item.Status, &item.DeliveryState, &created, &updated, &delivery); err != nil {
			return result, 0, err
		}
		if len(result.Entries) == limit {
			result.HasMore = true
			break
		}
		last = id
		item.EventRef = "notification_events:" + executioncontext.ID(id)
		item.TriggeredAt = executioncontext.Time(created)
		item.SourceUpdatedAt = executioncontext.Time(updated)
		item.StatusCutoff = executioncontext.Time(cutoff)
		item.DeliveryCutoff = executioncontext.Time(cutoff)
		item.HumanRead = "not_observed"
		item.BudgetVersionState = "version_unknown"
		item.TriggerEvidence = "unknown"
		if delivery != nil {
			item.LastDeliveryAt = executioncontext.Time(*delivery)
		}
		var values map[string]any
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()
		if decoder.Decode(&values) == nil {
			str := func(key string) string { v, _ := values[key].(string); return v }
			number := func(key string) string { v, _ := values[key].(json.Number); return string(v) }
			currency, scale := str("currency"), number("amount_scale")
			if currency == "CNY" && scale == "1000000" {
				budget := BudgetMoney{str("budget_micros"), currency, scale}
				spent := BudgetMoney{str("spent_micros"), currency, scale}
				if budget.valid(false) && spent.valid(false) {
					item.BudgetAtTrigger = &budget
					item.SpentAtTrigger = &spent
					item.TriggerEvidence = "original_event_amounts"
				}
			} else if number("budget_cents") != "" && number("spent_cents") != "" {
				b, e1 := strconv.ParseInt(number("budget_cents"), 10, 64)
				v, e2 := strconv.ParseInt(number("spent_cents"), 10, 64)
				bm, e3 := money.CentsToMicros(b)
				sm, e4 := money.CentsToMicros(v)
				if e1 == nil && e2 == nil && e3 == nil && e4 == nil && b >= 0 && v >= 0 {
					item.BudgetAtTrigger = &BudgetMoney{strconv.FormatInt(bm, 10), "CNY", "1000000"}
					item.SpentAtTrigger = &BudgetMoney{strconv.FormatInt(sm, 10), "CNY", "1000000"}
					item.TriggerEvidence = "original_legacy_event_cents"
				}
			}
			if oneOf(str("budget_period"), "daily", "monthly") {
				item.PeriodKind = str("budget_period")
			}
			if at, e := time.Parse(time.RFC3339Nano, str("period_start")); e == nil {
				item.PeriodStart = executioncontext.Time(at)
			}
			if threshold := number("threshold_percent"); positiveID(threshold) {
				item.ThresholdPercent = threshold
			}
		}
		result.Entries = append(result.Entries, item)
	}
	return result, last, rows.Err()
}
