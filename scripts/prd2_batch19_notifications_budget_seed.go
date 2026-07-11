//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/billing"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultBatch19DBDSN = "postgres://uniroute:uniroute_pass_2024@127.0.0.1:5432/uniroute?sslmode=disable"

	batch19OrgName       = "PRD2 Batch 19 Notification Budget Org"
	batch19AdminUsername = "prd2-b19-admin"
	batch19AdminPassword = "prd2B19Admin123!"
	batch19ProjectName   = "PRD2 Batch 19 Budget Project"
	batch19ProjectSlug   = "prd2-batch19-budget-project"
	batch19WebhookName   = "Batch19 Ops Webhook"
	batch19BudgetCents   = 1000
	batch19SpentCents    = 1200
)

type batch19SeedOutput struct {
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
	OrgID         int64  `json:"org_id"`
	ProjectID     int64  `json:"project_id"`
	WebhookID     int64  `json:"webhook_id"`
	EventID       int64  `json:"event_id"`
	BudgetCents   int    `json:"budget_cents"`
	SpentCents    int    `json:"spent_cents"`
	BudgetPeriod  string `json:"budget_period"`
	WebhookName   string `json:"webhook_name"`
}

func main() {
	if err := runBatch19Seed(); err != nil {
		fmt.Fprintf(os.Stderr, "seed failed: %v\n", err)
		os.Exit(1)
	}
}

func runBatch19Seed() error {
	webhookURL := os.Getenv("UNIROUTE_BATCH19_WEBHOOK_URL")
	if webhookURL == "" {
		return fmt.Errorf("UNIROUTE_BATCH19_WEBHOOK_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbDSN := firstNonEmpty(os.Getenv("UNIROUTE_SMOKE_DB_DSN"), os.Getenv("DB_DSN"), defaultBatch19DBDSN)
	pool, err := pgxpool.New(ctx, dbDSN)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	orgID, err := upsertBatch19Org(ctx, tx)
	if err != nil {
		return err
	}
	if err := cleanupBatch19Org(ctx, tx, orgID); err != nil {
		return err
	}
	adminID, err := createBatch19Admin(ctx, tx, orgID)
	if err != nil {
		return err
	}
	projectID, err := createBatch19Project(ctx, tx, orgID, adminID)
	if err != nil {
		return err
	}
	webhookID, err := createBatch19Webhook(ctx, tx, orgID, projectID, webhookURL)
	if err != nil {
		return err
	}
	if err := createBatch19BudgetLogs(ctx, tx, orgID, projectID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	svc := billing.NewEventService(pool)
	result, err := svc.CheckProjectBudget(ctx, orgID, projectID)
	if err != nil {
		return fmt.Errorf("check project budget: %w", err)
	}
	if result == nil || !result.Created {
		return fmt.Errorf("project budget event was not created: %+v", result)
	}

	out := batch19SeedOutput{
		AdminUsername: batch19AdminUsername,
		AdminPassword: batch19AdminPassword,
		OrgID:         orgID,
		ProjectID:     projectID,
		WebhookID:     webhookID,
		EventID:       result.EventID,
		BudgetCents:   batch19BudgetCents,
		SpentCents:    batch19SpentCents,
		BudgetPeriod:  "daily",
		WebhookName:   batch19WebhookName,
	}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

func upsertBatch19Org(ctx context.Context, q queryer) (int64, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO orgs (name, status, settings, org_type, created_at, updated_at)
		 VALUES ($1, 'active', '{}'::jsonb, 'customer', NOW(), NOW())
		 ON CONFLICT (name) DO UPDATE
		   SET status = 'active', updated_at = NOW()
		 RETURNING id`,
		batch19OrgName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert org: %w", err)
	}
	return id, nil
}

func cleanupBatch19Org(ctx context.Context, q execer, orgID int64) error {
	statements := []string{
		`DELETE FROM notification_deliveries WHERE event_id IN (SELECT id FROM notification_events WHERE org_id = $1)`,
		`DELETE FROM notification_events WHERE org_id = $1`,
		`DELETE FROM notification_webhooks WHERE org_id = $1`,
		`DELETE FROM call_logs WHERE org_id = $1`,
		`DELETE FROM project_members WHERE project_id IN (SELECT id FROM projects WHERE org_id = $1)`,
		`DELETE FROM projects WHERE org_id = $1`,
		`DELETE FROM users WHERE org_id = $1 AND username = '` + batch19AdminUsername + `'`,
	}
	for _, statement := range statements {
		if _, err := q.Exec(ctx, statement, orgID); err != nil {
			return fmt.Errorf("cleanup batch19 org: %w", err)
		}
	}
	return nil
}

func createBatch19Admin(ctx context.Context, q queryer, orgID int64) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(batch19AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("hash admin password: %w", err)
	}
	var id int64
	err = q.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'platform_admin', 'active', NOW(), NOW())
		 RETURNING id`,
		orgID, batch19AdminUsername, string(hash),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create admin user: %w", err)
	}
	return id, nil
}

func createBatch19Project(ctx context.Context, q queryer, orgID, adminID int64) (int64, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status, budget_cents, budget_period, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, 'Batch 19 project budget alert fixture', 'active', $4, 'daily', $5, NOW(), NOW())
		 RETURNING id`,
		orgID, batch19ProjectName, batch19ProjectSlug, batch19BudgetCents, adminID,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create project: %w", err)
	}
	return id, nil
}

func createBatch19Webhook(ctx context.Context, q queryer, orgID, projectID int64, webhookURL string) (int64, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO notification_webhooks (org_id, project_id, name, url, event_types, enabled, scope_verified, scope_write_generation, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, '["project_budget.threshold"]'::jsonb, TRUE, TRUE, 1, NOW(), NOW())
		 RETURNING id`,
		orgID, projectID, batch19WebhookName, webhookURL,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create webhook: %w", err)
	}
	return id, nil
}

func createBatch19BudgetLogs(ctx context.Context, q execer, orgID, projectID int64) error {
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	_, err := q.Exec(ctx,
		`INSERT INTO call_logs (request_id, org_id, project_id, model, provider, channel, total_tokens, cost_cents, status_code, created_at)
		 VALUES
		   ($1, $2, $3, 'batch19-budget-model', 'batch19', 'batch19-current', 10, $4, 200, $5),
		   ($6, $2, $3, 'batch19-budget-model', 'batch19', 'batch19-previous', 10, 5000, 200, $7)`,
		fmt.Sprintf("batch19-budget-current-%d", now.UnixNano()), orgID, projectID, batch19SpentCents, periodStart.Add(time.Hour),
		fmt.Sprintf("batch19-budget-previous-%d", now.UnixNano()), periodStart.Add(-time.Minute),
	)
	if err != nil {
		return fmt.Errorf("create budget call logs: %w", err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}
