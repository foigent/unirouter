package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AlertService checks wallet balance and sends webhook notifications.
// Supports three-tier percentage-based alerts: 50%, 80%, 100% (or custom).
type AlertService struct {
	db     *pgxpool.Pool
	client *http.Client
}

// NewAlertService creates a new AlertService.
func NewAlertService(db *pgxpool.Pool) *AlertService {
	return &AlertService{
		db:     db,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// CheckAndNotify checks wallet balance against percentage thresholds and sends webhooks.
// budgetCents is the total budget for the organization. Thresholds are percentages (e.g. 50, 80, 100).
// For each tier, an alert is sent when usage >= tier%, rate-limited to once per hour per tier.
func (a *AlertService) CheckAndNotify(ctx context.Context, webhookURL string, budgetCents int) {
	if webhookURL == "" || budgetCents <= 0 {
		return
	}

	var balanceCents int
	err := a.db.QueryRow(ctx,
		`SELECT balance FROM wallets ORDER BY org_id ASC LIMIT 1`,
	).Scan(&balanceCents)
	if err != nil {
		log.Printf("Budget alert: failed to query wallet: %v", err)
		return
	}

	// usagePercent = (budget - balance) / budget * 100
	usagePercent := 0
	if budgetCents > 0 {
		usagePercent = (budgetCents - balanceCents) * 100 / budgetCents
	}

	// Three-tier thresholds: 50%, 80%, 100%
	thresholds := []int{50, 80, 100}

	for _, tier := range thresholds {
		if usagePercent < tier {
			continue
		}

		// Rate-limit: check if we already sent this tier alert within 1 hour
		alertKey := fmt.Sprintf("budget_alert_t%d", tier)
		var recentAlerts int
		a.db.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_logs 
			 WHERE action = $1 AND created_at > NOW() - INTERVAL '1 hour'`,
			alertKey,
		).Scan(&recentAlerts)
		if recentAlerts > 0 {
			continue
		}

		// Send webhook
		payload := map[string]interface{}{
			"event":          "budget_alert",
			"tier":           tier,
			"usage_percent":  usagePercent,
			"balance_cents":  balanceCents,
			"budget_cents":   budgetCents,
			"timestamp":      time.Now().UTC().Format(time.RFC3339),
		}

		body, err := json.Marshal(payload)
		if err != nil {
			log.Printf("Budget alert: failed to marshal payload: %v", err)
			continue
		}

		req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(body))
		if err != nil {
			log.Printf("Budget alert: failed to create request: %v", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := a.client.Do(req)
		if err != nil {
			log.Printf("Budget alert: webhook request failed: %v", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("Budget alert: tier %d%% triggered, usage=%d%%, balance=%d cents", tier, usagePercent, balanceCents)
			a.db.Exec(ctx,
				`INSERT INTO audit_logs (action, detail) VALUES ($1, $2)`,
				alertKey, string(body),
			)
		} else {
			log.Printf("Budget alert: webhook returned status %d for tier %d%%", resp.StatusCode, tier)
		}
	}
}

// CheckAndNotifyLegacy supports the old single-threshold (in cents) interface for backward compatibility.
func (a *AlertService) CheckAndNotifyLegacy(ctx context.Context, webhookURL string, thresholdCents int) {
	if webhookURL == "" || thresholdCents <= 0 {
		return
	}

	var balanceCents int
	err := a.db.QueryRow(ctx,
		`SELECT balance FROM wallets ORDER BY org_id ASC LIMIT 1`,
	).Scan(&balanceCents)
	if err != nil {
		log.Printf("Budget alert: failed to query wallet: %v", err)
		return
	}

	if balanceCents > thresholdCents {
		return
	}

	var recentAlerts int
	a.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs 
		 WHERE action = 'budget_alert' AND created_at > NOW() - INTERVAL '1 hour'`,
	).Scan(&recentAlerts)
	if recentAlerts > 0 {
		return
	}

	payload := map[string]interface{}{
		"event":          "low_balance",
		"balance_cents":  balanceCents,
		"threshold_cents": thresholdCents,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Budget alert: failed to marshal payload: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("Budget alert: failed to create request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		log.Printf("Budget alert: webhook request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("Budget alert: webhook sent successfully, balance=%d cents, threshold=%d cents", balanceCents, thresholdCents)
		a.db.Exec(ctx,
			`INSERT INTO audit_logs (action, detail) VALUES ('budget_alert', $1)`,
			string(body),
		)
	} else {
		log.Printf("Budget alert: webhook returned status %d", resp.StatusCode)
	}
}
