//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultBatch17DBDSN = "postgres://uniroute:uniroute_pass_2024@127.0.0.1:5432/uniroute?sslmode=disable"

	batch17OrgName             = "PRD2 Batch 17 Model Governance Org"
	batch17AdminUsername       = "prd2-b17-admin"
	batch17AdminPassword       = "prd2B17Admin123!"
	batch17DeveloperUsername   = "prd2-b17-developer"
	batch17DeveloperPassword   = "prd2B17Developer123!"
	batch17ProjectName         = "PRD2 Batch 17 Project"
	batch17ProjectSlug         = "prd2-batch17-project"
	batch17ModelSetName        = "PRD2 Batch 17 Approved Set"
	batch17ModelSetSlug        = "prd2-batch17-approved"
	batch17GAModel             = "batch17-ga-chat"
	batch17GADisplay           = "Batch17 GA Chat"
	batch17GAAlias             = "batch17-company-chat"
	batch17DeprecatedModel     = "batch17-deprecated-chat"
	batch17DeprecatedDisplay   = "Batch17 Deprecated Chat"
	batch17DeprecatedAlias     = "batch17-legacy-chat"
	batch17MigrationAdvice     = "Migrate to batch17-ga-chat before the next renewal window"
	batch17UnapprovedModel     = "batch17-private-eval"
	batch17UnapprovedDisplay   = "Batch17 Private Eval"
	batch17RetiredModel        = "batch17-retired-chat"
	batch17RetiredDisplay      = "Batch17 Retired Chat"
	batch17ReconciliationModel = "batch17-recon-metering"
)

type batch17SeedOutput struct {
	AdminUsername       string `json:"admin_username"`
	AdminPassword       string `json:"admin_password"`
	DeveloperUsername   string `json:"developer_username"`
	DeveloperPassword   string `json:"developer_password"`
	OrgID               int64  `json:"org_id"`
	ProjectID           int64  `json:"project_id"`
	ModelSetID          int64  `json:"model_set_id"`
	ProjectSlug         string `json:"project_slug"`
	GAModel             string `json:"ga_model"`
	GADisplay           string `json:"ga_display"`
	GAAlias             string `json:"ga_alias"`
	DeprecatedModel     string `json:"deprecated_model"`
	DeprecatedDisplay   string `json:"deprecated_display"`
	DeprecatedAlias     string `json:"deprecated_alias"`
	MigrationAdvice     string `json:"migration_advice"`
	UnapprovedModel     string `json:"unapproved_model"`
	UnapprovedDisplay   string `json:"unapproved_display"`
	RetiredModel        string `json:"retired_model"`
	RetiredDisplay      string `json:"retired_display"`
	ReconciliationModel string `json:"reconciliation_model"`
}

func main() {
	if err := runBatch17Seed(); err != nil {
		fmt.Fprintf(os.Stderr, "seed failed: %v\n", err)
		os.Exit(1)
	}
}

func runBatch17Seed() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbDSN := firstNonEmpty(os.Getenv("UNIROUTE_SMOKE_DB_DSN"), os.Getenv("DB_DSN"), defaultBatch17DBDSN)
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

	orgID, err := upsertBatch17Org(ctx, tx)
	if err != nil {
		return err
	}
	adminID, err := upsertBatch17User(ctx, tx, orgID, batch17AdminUsername, batch17AdminPassword, "platform_admin")
	if err != nil {
		return err
	}
	developerID, err := upsertBatch17User(ctx, tx, orgID, batch17DeveloperUsername, batch17DeveloperPassword, "developer")
	if err != nil {
		return err
	}
	projectID, err := upsertBatch17Project(ctx, tx, orgID, adminID)
	if err != nil {
		return err
	}
	if err := upsertBatch17ProjectMember(ctx, tx, projectID, developerID); err != nil {
		return err
	}
	modelSetID, err := upsertBatch17Models(ctx, tx, orgID)
	if err != nil {
		return err
	}
	if err := upsertBatch17ProjectModelSet(ctx, tx, projectID, modelSetID); err != nil {
		return err
	}
	if err := upsertBatch17Wallet(ctx, tx, orgID); err != nil {
		return err
	}
	if err := upsertBatch17ReconciliationLogs(ctx, tx, orgID, projectID); err != nil {
		return err
	}
	if err := upsertBatch17Settings(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	out := batch17SeedOutput{
		AdminUsername:       batch17AdminUsername,
		AdminPassword:       batch17AdminPassword,
		DeveloperUsername:   batch17DeveloperUsername,
		DeveloperPassword:   batch17DeveloperPassword,
		OrgID:               orgID,
		ProjectID:           projectID,
		ModelSetID:          modelSetID,
		ProjectSlug:         batch17ProjectSlug,
		GAModel:             batch17GAModel,
		GADisplay:           batch17GADisplay,
		GAAlias:             batch17GAAlias,
		DeprecatedModel:     batch17DeprecatedModel,
		DeprecatedDisplay:   batch17DeprecatedDisplay,
		DeprecatedAlias:     batch17DeprecatedAlias,
		MigrationAdvice:     batch17MigrationAdvice,
		UnapprovedModel:     batch17UnapprovedModel,
		UnapprovedDisplay:   batch17UnapprovedDisplay,
		RetiredModel:        batch17RetiredModel,
		RetiredDisplay:      batch17RetiredDisplay,
		ReconciliationModel: batch17ReconciliationModel,
	}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

func upsertBatch17Org(ctx context.Context, tx pgx.Tx) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO orgs (name, status, settings, org_type, created_at, updated_at)
		 VALUES ($1, 'active', '{}'::jsonb, 'customer', NOW(), NOW())
		 ON CONFLICT (name) DO UPDATE
		   SET status = 'active', updated_at = NOW()
		 RETURNING id`,
		batch17OrgName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert org: %w", err)
	}
	return id, nil
}

func upsertBatch17User(ctx context.Context, tx pgx.Tx, orgID int64, username, password, role string) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("hash password for %s: %w", username, err)
	}
	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'active', NOW(), NOW())
		 ON CONFLICT (org_id, username) DO UPDATE
		   SET password_hash = EXCLUDED.password_hash,
		       role = EXCLUDED.role,
		       status = 'active',
		       updated_at = NOW()
		 RETURNING id`,
		orgID, username, string(hash), role,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert user %s: %w", username, err)
	}
	return id, nil
}

func upsertBatch17Project(ctx context.Context, tx pgx.Tx, orgID, createdBy int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status, budget_cents, budget_period, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, 'Batch 17 model-governance and metering fixture', 'active', 1000000, 'monthly', $4, NOW(), NOW())
		 ON CONFLICT (org_id, slug) DO UPDATE
		   SET name = EXCLUDED.name,
		       description = EXCLUDED.description,
		       status = 'active',
		       budget_cents = EXCLUDED.budget_cents,
		       budget_period = EXCLUDED.budget_period,
		       updated_at = NOW()
		 RETURNING id`,
		orgID, batch17ProjectName, batch17ProjectSlug, createdBy,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert project: %w", err)
	}
	return id, nil
}

func upsertBatch17ProjectMember(ctx context.Context, tx pgx.Tx, projectID, userID int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role, created_at, updated_at)
		 VALUES ($1, $2, 'project_developer', NOW(), NOW())
		 ON CONFLICT (project_id, user_id) DO UPDATE
		   SET role = 'project_developer', updated_at = NOW()`,
		projectID, userID,
	)
	if err != nil {
		return fmt.Errorf("upsert project member: %w", err)
	}
	return nil
}

func upsertBatch17Models(ctx context.Context, tx pgx.Tx, orgID int64) (int64, error) {
	models := []struct {
		name          string
		display       string
		lifecycle     string
		approved      bool
		migration     string
		pricingNeeded bool
	}{
		{batch17GAModel, batch17GADisplay, "ga", true, "", true},
		{batch17DeprecatedModel, batch17DeprecatedDisplay, "deprecated", true, batch17MigrationAdvice, true},
		{batch17UnapprovedModel, batch17UnapprovedDisplay, "ga", false, "", false},
		{batch17RetiredModel, batch17RetiredDisplay, "retired", true, "Do not use; retired by Batch 17 smoke", false},
	}
	for _, model := range models {
		if model.pricingNeeded {
			if err := upsertBatch17ModelPrice(ctx, tx, model.name); err != nil {
				return 0, err
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO model_registry (
			     org_id, model_name, display_name, provider_family, lifecycle,
			     context_length, output_limit, capability_tags, data_residency,
			     pricing_model_name, approved_catalog, migration_recommendation,
			     created_at, updated_at
			 )
			 VALUES ($1, $2, $3, 'openai_compatible', $4, 128000, 4096,
			         'chat,json', 'global', $2, $5, $6, NOW(), NOW())
			 ON CONFLICT (org_id, model_name) DO UPDATE
			   SET display_name = EXCLUDED.display_name,
			       provider_family = EXCLUDED.provider_family,
			       lifecycle = EXCLUDED.lifecycle,
			       context_length = EXCLUDED.context_length,
			       output_limit = EXCLUDED.output_limit,
			       capability_tags = EXCLUDED.capability_tags,
			       data_residency = EXCLUDED.data_residency,
			       pricing_model_name = EXCLUDED.pricing_model_name,
			       approved_catalog = EXCLUDED.approved_catalog,
			       migration_recommendation = EXCLUDED.migration_recommendation,
			       updated_at = NOW()`,
			orgID, model.name, model.display, model.lifecycle, model.approved, model.migration,
		); err != nil {
			return 0, fmt.Errorf("upsert model registry %s: %w", model.name, err)
		}
	}

	aliases := []struct {
		alias  string
		target string
		label  string
	}{
		{batch17GAAlias, batch17GAModel, "Batch17 Company Chat"},
		{batch17DeprecatedAlias, batch17DeprecatedModel, "Batch17 Legacy Chat"},
	}
	for _, alias := range aliases {
		if _, err := tx.Exec(ctx,
			`INSERT INTO model_aliases (org_id, alias, target_model, display_name, description, status, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, 'Batch 17 smoke alias', 'active', NOW(), NOW())
			 ON CONFLICT (org_id, alias) DO UPDATE
			   SET target_model = EXCLUDED.target_model,
			       display_name = EXCLUDED.display_name,
			       description = EXCLUDED.description,
			       status = 'active',
			       updated_at = NOW()`,
			orgID, alias.alias, alias.target, alias.label,
		); err != nil {
			return 0, fmt.Errorf("upsert alias %s: %w", alias.alias, err)
		}
	}

	var modelSetID int64
	err := tx.QueryRow(ctx,
		`INSERT INTO model_sets (org_id, name, slug, description, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'Batch 17 approved GA and deprecated models', 'active', NOW(), NOW())
		 ON CONFLICT (org_id, slug) DO UPDATE
		   SET name = EXCLUDED.name,
		       description = EXCLUDED.description,
		       status = 'active',
		       updated_at = NOW()
		 RETURNING id`,
		orgID, batch17ModelSetName, batch17ModelSetSlug,
	).Scan(&modelSetID)
	if err != nil {
		return 0, fmt.Errorf("upsert model set: %w", err)
	}
	for _, item := range []struct {
		itemType string
		itemName string
	}{
		{"model", batch17GAModel},
		{"alias", batch17GAAlias},
		{"model", batch17DeprecatedModel},
		{"alias", batch17DeprecatedAlias},
	} {
		if _, err := tx.Exec(ctx,
			`INSERT INTO model_set_items (model_set_id, item_type, item_name, created_at)
			 VALUES ($1, $2, $3, NOW())
			 ON CONFLICT (model_set_id, item_type, item_name) DO NOTHING`,
			modelSetID, item.itemType, item.itemName,
		); err != nil {
			return 0, fmt.Errorf("upsert model set item %s/%s: %w", item.itemType, item.itemName, err)
		}
	}
	return modelSetID, nil
}

func upsertBatch17ModelPrice(ctx context.Context, tx pgx.Tx, modelName string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO model_prices (model_name, provider, input_price_cents, output_price_cents, max_context, default_max_output, created_at, updated_at)
		 VALUES ($1, 'openai_compatible', 0.15, 0.60, 128000, 4096, NOW(), NOW())
		 ON CONFLICT (model_name) DO UPDATE
		   SET provider = EXCLUDED.provider,
		       input_price_cents = EXCLUDED.input_price_cents,
		       output_price_cents = EXCLUDED.output_price_cents,
		       max_context = EXCLUDED.max_context,
		       default_max_output = EXCLUDED.default_max_output,
		       updated_at = NOW()`,
		modelName,
	)
	if err != nil {
		return fmt.Errorf("upsert model price %s: %w", modelName, err)
	}
	return nil
}

func upsertBatch17ProjectModelSet(ctx context.Context, tx pgx.Tx, projectID, modelSetID int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO project_model_sets (project_id, model_set_id, created_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (project_id, model_set_id) DO NOTHING`,
		projectID, modelSetID,
	)
	if err != nil {
		return fmt.Errorf("assign project model set: %w", err)
	}
	return nil
}

func upsertBatch17Wallet(ctx context.Context, tx pgx.Tx, orgID int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros, currency, auto_freeze, low_balance_threshold_cents, created_at, updated_at)
		 VALUES ($1, 10000000000, 10000000000, 0, 0, 'CNY', false, 0, NOW(), NOW())
		 ON CONFLICT (org_id) DO UPDATE
		   SET balance_micros = GREATEST(wallets.balance_micros, 10000000000),
		       granted_balance_micros = GREATEST(wallets.granted_balance_micros, 10000000000),
		       frozen_micros = 0,
		       currency = 'CNY',
		       updated_at = NOW()`,
		orgID,
	)
	if err != nil {
		return fmt.Errorf("upsert wallet: %w", err)
	}
	return nil
}

func upsertBatch17ReconciliationLogs(ctx context.Context, tx pgx.Tx, orgID, projectID int64) error {
	rows := []struct {
		requestID       string
		totalTokens     int
		estimatedTokens int
		usageSource     string
		confidence      float64
	}{
		{"batch17-recon-upstream", 20, 20, "upstream", 1.00},
		{"batch17-recon-counter-v2-a", 50, 45, "counter_v2", 0.65},
		{"batch17-recon-counter-v2-b", 30, 25, "counter_v2", 0.65},
	}
	for _, row := range rows {
		if _, err := tx.Exec(ctx,
			`INSERT INTO call_logs (
			     request_id, org_id, project_id, model, requested_model, provider, channel,
			     prompt_tokens, completion_tokens, total_tokens, cost_micros, latency_ms, ttft_ms,
			     status_code, upstream_status_code, upstream_error, usage_source, is_stream,
			     ip_address, estimated_tokens, usage_confidence, created_at
			 )
			 SELECT $1::varchar, $2::bigint, $3::bigint, $4::varchar, $4::varchar, 'batch17-metering', 'batch17-seeded-log',
			        10, GREATEST($5::integer - 10, 0), $5::integer, 10000, 25, 0,
			        200, 200, '', $6::varchar, false,
			        '127.0.0.1', $7::integer, $8::double precision, NOW()
			 WHERE NOT EXISTS (
			     SELECT 1 FROM call_logs WHERE request_id = $1::varchar AND org_id = $2::bigint
			 )`,
			row.requestID, orgID, projectID, batch17ReconciliationModel, row.totalTokens, row.usageSource, row.estimatedTokens, row.confidence,
		); err != nil {
			return fmt.Errorf("upsert reconciliation call log %s: %w", row.requestID, err)
		}
	}
	return nil
}

func upsertBatch17Settings(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO settings (key, value, description, updated_at)
		 VALUES ('token_metering.estimated_ratio_alert_threshold', '0.2', 'Estimated-token ratio threshold for reconciliation alert signal', NOW())
		 ON CONFLICT (key) DO UPDATE
		   SET value = EXCLUDED.value,
		       description = EXCLUDED.description,
		       updated_at = NOW()`,
	)
	if err != nil {
		return fmt.Errorf("upsert estimated-ratio setting: %w", err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
