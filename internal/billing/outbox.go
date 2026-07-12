package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/terminalhook"
)

type usageJournalPayload struct {
	EventType            string    `json:"event_type"`
	RequestID            string    `json:"request_id"`
	OrgID                int64     `json:"org_id"`
	ProjectID            int64     `json:"project_id"`
	Model                string    `json:"model"`
	PromptTokens         int       `json:"prompt_tokens"`
	UncachedPromptTokens int       `json:"uncached_prompt_tokens"`
	CacheReadTokens      int       `json:"cache_read_tokens"`
	CacheCreationTokens  int       `json:"cache_creation_tokens"`
	CompletionTokens     int       `json:"completion_tokens"`
	UsageSource          string    `json:"usage_source"`
	CacheUsageSchema     string    `json:"cache_usage_schema"`
	CacheUsageStatus     string    `json:"cache_usage_status"`
	CalculatedCost       int       `json:"calculated_cost"`
	ChargedCost          int       `json:"charged_cost"`
	PricingVersion       string    `json:"pricing_version"`
	TerminalTransaction  int64     `json:"terminal_transaction_id,omitempty"`
	OccurredAt           time.Time `json:"occurred_at"`
}

type budgetEvaluationPayload struct {
	OrgID        int64     `json:"org_id"`
	ProjectID    int64     `json:"project_id"`
	BudgetPeriod string    `json:"budget_period"`
	PeriodStart  time.Time `json:"period_start"`
}

func billingEventKey(eventType string, orgID int64, requestID string) string {
	return fmt.Sprintf("billing:%s:%d:%s", eventType, orgID, requestID)
}

func enqueueUsageJournalTx(ctx context.Context, tx pgx.Tx, service *outbox.Service, outboxType, journalType string, record reservationRecord, req SettleRequest, calculatedCost, chargedCost int, terminalTransactionID int64) error {
	if service == nil {
		return nil
	}
	payload := usageJournalPayload{
		EventType:            journalType,
		RequestID:            req.RequestID,
		OrgID:                req.OrgID,
		ProjectID:            record.ProjectID,
		Model:                record.Model,
		PromptTokens:         req.PromptTokens,
		UncachedPromptTokens: req.UncachedPromptTokens,
		CacheReadTokens:      req.CacheReadTokens,
		CacheCreationTokens:  req.CacheCreationTokens,
		CompletionTokens:     req.CompletionTokens,
		UsageSource:          req.UsageSource,
		CacheUsageSchema:     req.CacheUsageSchema,
		CacheUsageStatus:     req.CacheUsageStatus,
		CalculatedCost:       calculatedCost,
		ChargedCost:          chargedCost,
		PricingVersion:       record.PricingVersion,
		TerminalTransaction:  terminalTransactionID,
		OccurredAt:           time.Now().UTC(),
	}
	event, err := outbox.NewEvent(
		outboxType,
		billingEventKey(journalType, req.OrgID, req.RequestID),
		outbox.PriorityBilling,
		req.OrgID,
		req.RequestID,
		payload,
		payload.OccurredAt,
	)
	if err != nil {
		return err
	}
	_, _, err = outbox.EnqueueTx(ctx, tx, event)
	return err
}

// ProcessUsageJournalEvent materializes the minimal financial envelope. The
// journal insert and incremental project spend update are idempotent; a replay
// after a worker crash cannot double count spend.
func (b *Billing) ProcessUsageJournalEvent(ctx context.Context, event outbox.Event) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("billing database is unavailable")
	}
	var payload usageJournalPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode usage journal event: %w", err)
	}
	if payload.OrgID <= 0 || payload.RequestID == "" || payload.EventType == "" {
		return fmt.Errorf("usage journal event identity is invalid")
	}

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin usage journal materialization: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx,
		`INSERT INTO usage_finalization_journal (
			event_key, event_type, request_id, org_id, project_id, model,
			prompt_tokens, uncached_prompt_tokens, cache_read_tokens,
			cache_creation_tokens, completion_tokens, usage_source,
			cache_usage_schema, cache_usage_status, calculated_cost,
			charged_cost, pricing_version, terminal_transaction_id, occurred_at
		 ) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17, $18, $19
		 ) ON CONFLICT (event_key) DO NOTHING`,
		event.Key, payload.EventType, payload.RequestID, payload.OrgID,
		payload.ProjectID, payload.Model, payload.PromptTokens,
		payload.UncachedPromptTokens, payload.CacheReadTokens,
		payload.CacheCreationTokens, payload.CompletionTokens,
		payload.UsageSource, payload.CacheUsageSchema, payload.CacheUsageStatus,
		payload.CalculatedCost, payload.ChargedCost, payload.PricingVersion,
		nullablePositiveInt64(payload.TerminalTransaction), payload.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf("insert usage finalization journal: %w", err)
	}
	created := result.RowsAffected() == 1
	if created {
		switch payload.EventType {
		case "settled":
			if err := terminalhook.MarkFromFinancialTx(
				ctx,
				tx,
				b.outbox,
				payload.RequestID,
				payload.OrgID,
				terminalhook.TerminalSettled,
				int64(payload.PromptTokens+payload.CompletionTokens),
				int64(payload.CalculatedCost),
			); err != nil {
				return fmt.Errorf("schedule settled runtime terminal hooks: %w", err)
			}
		case "cancelled":
			if err := terminalhook.MarkFromFinancialTx(
				ctx,
				tx,
				b.outbox,
				payload.RequestID,
				payload.OrgID,
				terminalhook.TerminalCancelled,
				0,
				0,
			); err != nil {
				return fmt.Errorf("schedule cancelled runtime terminal hooks: %w", err)
			}
		}
	}
	if !created || payload.EventType != "settled" || payload.ProjectID <= 0 {
		return tx.Commit(ctx)
	}

	var budgetCents int64
	var budgetPeriod string
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(budget_cents, 0), budget_period
		 FROM projects
		 WHERE id = $1 AND org_id = $2 AND status = 'active'`,
		payload.ProjectID, payload.OrgID,
	).Scan(&budgetCents, &budgetPeriod); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tx.Commit(ctx)
		}
		return fmt.Errorf("read incremental project budget: %w", err)
	}
	if budgetCents <= 0 || (budgetPeriod != "daily" && budgetPeriod != "monthly") {
		return tx.Commit(ctx)
	}
	periodStart := budgetPeriodStart(payload.OccurredAt, budgetPeriod)
	if _, err := tx.Exec(ctx,
		`INSERT INTO project_budget_usage (
			org_id, project_id, budget_period, period_start, spent_cents, updated_at
		 ) VALUES ($1, $2, $3, $4, $5, NOW())
		 ON CONFLICT (org_id, project_id, budget_period, period_start)
		 DO UPDATE SET
			spent_cents = project_budget_usage.spent_cents + EXCLUDED.spent_cents,
			updated_at = NOW()`,
		payload.OrgID, payload.ProjectID, budgetPeriod, periodStart, payload.ChargedCost,
	); err != nil {
		return fmt.Errorf("increment project budget usage: %w", err)
	}

	evaluation := budgetEvaluationPayload{
		OrgID:        payload.OrgID,
		ProjectID:    payload.ProjectID,
		BudgetPeriod: budgetPeriod,
		PeriodStart:  periodStart,
	}
	evaluationEvent, err := outbox.NewEvent(
		outbox.EventBudgetEvaluate,
		fmt.Sprintf("budget-evaluate:%s", event.Key),
		outbox.PriorityNotification,
		payload.OrgID,
		payload.RequestID,
		evaluation,
		payload.OccurredAt,
	)
	if err != nil {
		return err
	}
	if _, _, err := outbox.EnqueueTx(ctx, tx, evaluationEvent); err != nil {
		return fmt.Errorf("enqueue budget evaluation: %w", err)
	}
	return tx.Commit(ctx)
}

func nullablePositiveInt64(value int64) interface{} {
	if value <= 0 {
		return nil
	}
	return value
}
