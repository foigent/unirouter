package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/terminalhook"
)

type usageJournalPayload struct {
	EventType            string      `json:"event_type"`
	BillingMode          BillingMode `json:"billing_mode,omitempty"`
	RequestID            string      `json:"request_id"`
	OrgID                int64       `json:"org_id"`
	ProjectID            int64       `json:"project_id"`
	Model                string      `json:"model"`
	PromptTokens         int         `json:"prompt_tokens"`
	UncachedPromptTokens int         `json:"uncached_prompt_tokens"`
	CacheReadTokens      int         `json:"cache_read_tokens"`
	CacheCreationTokens  int         `json:"cache_creation_tokens"`
	CompletionTokens     int         `json:"completion_tokens"`
	UsageSource          string      `json:"usage_source"`
	UsageAuthority       string      `json:"usage_authority"`
	UsagePurpose         string      `json:"usage_purpose"`
	UsageFinality        string      `json:"usage_finality"`
	UsageContractVersion string      `json:"usage_contract_version"`
	UsageEstimateVersion string      `json:"usage_estimate_version,omitempty"`
	CacheUsageSchema     string      `json:"cache_usage_schema"`
	CacheUsageStatus     string      `json:"cache_usage_status"`
	MoneySchema          string      `json:"money_schema,omitempty"`
	Currency             string      `json:"currency,omitempty"`
	AmountScale          int64       `json:"amount_scale,omitempty"`
	CalculatedCostMicros string      `json:"calculated_cost_micros,omitempty"`
	ChargedCostMicros    string      `json:"charged_cost_micros,omitempty"`
	CalculatedCostCents  *int64      `json:"calculated_cost,omitempty"`
	ChargedCostCents     *int64      `json:"charged_cost,omitempty"`
	PricingVersion       string      `json:"pricing_version"`
	TerminalTransaction  int64       `json:"terminal_transaction_id,omitempty"`
	OccurredAt           time.Time   `json:"occurred_at"`
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

func enqueueUsageJournalTx(ctx context.Context, tx pgx.Tx, service *outbox.Service, outboxType, journalType string, record reservationRecord, req SettleRequest, calculatedCost, chargedCost int64, terminalTransactionID int64) error {
	if service == nil {
		return nil
	}
	payload := usageJournalPayload{
		EventType:            journalType,
		BillingMode:          record.BillingMode,
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
		UsageAuthority:       string(req.UsageAuthority),
		UsagePurpose:         string(req.UsagePurpose),
		UsageFinality:        string(req.UsageFinality),
		UsageContractVersion: req.UsageContractVersion,
		UsageEstimateVersion: req.UsageEstimateVersion,
		CacheUsageSchema:     req.CacheUsageSchema,
		CacheUsageStatus:     req.CacheUsageStatus,
		MoneySchema:          outbox.MoneyVersion,
		Currency:             "CNY",
		AmountScale:          money.MicrosPerCNY,
		CalculatedCostMicros: strconv.FormatInt(calculatedCost, 10),
		ChargedCostMicros:    strconv.FormatInt(chargedCost, 10),
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
	payload.EventType = normalizeUsageJournalEventType(event.Type, payload.EventType)
	if payload.UsageAuthority == "" {
		if payload.EventType == "cancelled" && payload.UsageSource == "" {
			payload.UsageSource = metering.UsageSourceMissing
		}
		authority, purpose, finality, estimateVersion := metering.LegacyUsageContract(payload.UsageSource, payload.EventType != "cancelled")
		payload.UsageAuthority = string(authority)
		payload.UsagePurpose = string(purpose)
		payload.UsageFinality = string(finality)
		payload.UsageContractVersion = metering.UsageContractVersion
		payload.UsageEstimateVersion = estimateVersion
	}
	if payload.OrgID <= 0 || payload.RequestID == "" || payload.EventType == "" {
		return fmt.Errorf("usage journal event identity is invalid")
	}
	if payload.BillingMode == "" {
		payload.BillingMode = BillingModePrepaidEnforced
	}
	if err := validateReservationBillingMode(payload.BillingMode); err != nil {
		return fmt.Errorf("usage journal: %w", err)
	}
	calculatedCostMicros, chargedCostMicros, err := decodeUsageJournalMoney(event.Type, payload)
	if err != nil {
		return err
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
			usage_authority, usage_purpose, usage_finality,
			usage_contract_version, usage_estimate_version,
			cache_usage_schema, cache_usage_status, calculated_cost_micros,
			charged_cost_micros, pricing_version, billing_mode,
			terminal_transaction_id, occurred_at
		 ) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17,
			$18, $19, $20, $21, $22, $23, $24, $25
		 ) ON CONFLICT (event_key) DO NOTHING`,
		event.Key, payload.EventType, payload.RequestID, payload.OrgID,
		payload.ProjectID, payload.Model, payload.PromptTokens,
		payload.UncachedPromptTokens, payload.CacheReadTokens,
		payload.CacheCreationTokens, payload.CompletionTokens,
		payload.UsageSource, payload.UsageAuthority, payload.UsagePurpose,
		payload.UsageFinality, payload.UsageContractVersion, payload.UsageEstimateVersion,
		payload.CacheUsageSchema, payload.CacheUsageStatus,
		calculatedCostMicros, chargedCostMicros, payload.PricingVersion, payload.BillingMode,
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
				calculatedCostMicros,
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
	if created && b.terminalMaterializer != nil &&
		(payload.EventType == "settled" || payload.EventType == "cancelled") {
		if err := b.terminalMaterializer.MaterializeTerminalTx(ctx, tx, TerminalRecord{
			EventKey:             event.Key,
			RequestID:            payload.RequestID,
			OrgID:                payload.OrgID,
			ProjectID:            payload.ProjectID,
			Status:               payload.EventType,
			Model:                payload.Model,
			PromptTokens:         payload.PromptTokens,
			UncachedPromptTokens: payload.UncachedPromptTokens,
			CacheReadTokens:      payload.CacheReadTokens,
			CacheCreationTokens:  payload.CacheCreationTokens,
			CompletionTokens:     payload.CompletionTokens,
			UsageSource:          payload.UsageSource,
			UsageAuthority:       payload.UsageAuthority,
			UsagePurpose:         payload.UsagePurpose,
			UsageFinality:        payload.UsageFinality,
			UsageContractVersion: payload.UsageContractVersion,
			UsageEstimateVersion: payload.UsageEstimateVersion,
			CacheUsageSchema:     payload.CacheUsageSchema,
			CacheUsageStatus:     payload.CacheUsageStatus,
			CalculatedCostMicros: calculatedCostMicros,
			ChargedCostMicros:    chargedCostMicros,
			PricingVersion:       payload.PricingVersion,
			OccurredAt:           payload.OccurredAt,
		}); err != nil {
			return fmt.Errorf("materialize completed-execution terminal: %w", err)
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
			org_id, project_id, budget_period, period_start, spent_micros, updated_at
		 ) VALUES ($1, $2, $3, $4, $5, NOW())
		 ON CONFLICT (org_id, project_id, budget_period, period_start)
		 DO UPDATE SET
			spent_micros = project_budget_usage.spent_micros + EXCLUDED.spent_micros,
			updated_at = NOW()`,
		payload.OrgID, payload.ProjectID, budgetPeriod, periodStart, chargedCostMicros,
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

func normalizeUsageJournalEventType(outboxType, journalType string) string {
	if outboxType != outbox.EventBillingUsagePrepared {
		return journalType
	}
	switch journalType {
	case OutputHoldReasonGuardrailWithheld,
		OutputHoldReasonResponseTooLarge,
		OutputHoldReasonProviderResponseUnusable,
		OutputHoldReasonProviderAttemptUnresolved:
		return "usage_prepared"
	default:
		return journalType
	}
}

func decodeUsageJournalMoney(eventType string, payload usageJournalPayload) (int64, int64, error) {
	switch eventType {
	case outbox.EventBillingUsagePrepared, outbox.EventBillingSettled, outbox.EventBillingCancelled:
		if payload.MoneySchema != outbox.MoneyVersion {
			return 0, 0, fmt.Errorf("usage journal money schema %q is unsupported", payload.MoneySchema)
		}
		if payload.Currency != "CNY" || payload.AmountScale != money.MicrosPerCNY {
			return 0, 0, fmt.Errorf("usage journal currency or amount scale is invalid")
		}
		calculated, err := strconv.ParseInt(payload.CalculatedCostMicros, 10, 64)
		if err != nil || calculated < 0 || strconv.FormatInt(calculated, 10) != payload.CalculatedCostMicros {
			return 0, 0, fmt.Errorf("usage journal calculated_cost_micros is invalid")
		}
		charged, err := strconv.ParseInt(payload.ChargedCostMicros, 10, 64)
		if err != nil || charged < 0 || strconv.FormatInt(charged, 10) != payload.ChargedCostMicros {
			return 0, 0, fmt.Errorf("usage journal charged_cost_micros is invalid")
		}
		return calculated, charged, nil
	case outbox.EventBillingUsagePreparedV1, outbox.EventBillingSettledV1, outbox.EventBillingCancelledV1:
		if payload.CalculatedCostCents == nil || payload.ChargedCostCents == nil ||
			*payload.CalculatedCostCents < 0 || *payload.ChargedCostCents < 0 {
			return 0, 0, fmt.Errorf("legacy usage journal cent amounts are invalid")
		}
		calculated, err := money.CentsToMicros(*payload.CalculatedCostCents)
		if err != nil {
			return 0, 0, fmt.Errorf("convert legacy calculated cost: %w", err)
		}
		charged, err := money.CentsToMicros(*payload.ChargedCostCents)
		if err != nil {
			return 0, 0, fmt.Errorf("convert legacy charged cost: %w", err)
		}
		return calculated, charged, nil
	default:
		return 0, 0, fmt.Errorf("usage journal event type %q is unsupported", eventType)
	}
}

func nullablePositiveInt64(value int64) interface{} {
	if value <= 0 {
		return nil
	}
	return value
}
