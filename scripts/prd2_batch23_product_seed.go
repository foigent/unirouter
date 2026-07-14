//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultBatch23DBDSN = "postgres://uniroute:uniroute_pass_2024@127.0.0.1:5432/uniroute?sslmode=disable"

	batch23OrgName             = "PRD2 Batch 23 Product Smoke Org"
	batch23AdminUsername       = "prd2-b23-admin"
	batch23AdminPassword       = "prd2B23Admin123!"
	batch23DeveloperUsername   = "prd2-b23-developer"
	batch23DeveloperPassword   = "prd2B23Developer123!"
	batch23ProjectName         = "PRD2 Batch 23 Product Project"
	batch23ProjectSlug         = "prd2-batch23-product"
	batch23ModelName           = "batch23-product-chat"
	batch23ModelAlias          = "batch23-company-chat"
	batch23ModelDisplay        = "Batch23 Product Chat"
	batch23ModelSetName        = "PRD2 Batch 23 Approved Models"
	batch23ModelSetSlug        = "prd2-batch23-approved"
	batch23ChannelName         = "prd2-batch23-openai-compatible-mock"
	batch23Provider            = "batch23-mock"
	batch23UpstreamPlaintext   = "upstream-batch23-product-key"
	batch23IncidentTitle       = "Batch 23 warning incident"
	batch23IncidentDedupeKey   = "prd2-batch23-product-warning"
	batch23SeededUpstreamLogID = "batch23-analytics-upstream"
	batch23SeededCounterLogID  = "batch23-analytics-counter"
	batch23SeededErrorLogID    = "batch23-analytics-error"
)

type batch23SeedOutput struct {
	AdminUsername       string `json:"admin_username"`
	AdminPassword       string `json:"admin_password"`
	DeveloperUsername   string `json:"developer_username"`
	DeveloperPassword   string `json:"developer_password"`
	OrgID               int64  `json:"org_id"`
	ProjectID           int64  `json:"project_id"`
	ProjectSlug         string `json:"project_slug"`
	ModelName           string `json:"model_name"`
	ModelAlias          string `json:"model_alias"`
	ChannelName         string `json:"channel_name"`
	Provider            string `json:"provider"`
	UpstreamURL         string `json:"upstream_url"`
	IncidentTitle       string `json:"incident_title"`
	SeededUpstreamLogID string `json:"seeded_upstream_log_id"`
	SeededCounterLogID  string `json:"seeded_counter_log_id"`
	SeededErrorLogID    string `json:"seeded_error_log_id"`
}

func main() {
	if err := runBatch23Seed(); err != nil {
		fmt.Fprintf(os.Stderr, "seed failed: %v\n", err)
		os.Exit(1)
	}
}

func runBatch23Seed() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbDSN := firstNonEmpty(os.Getenv("UNIROUTE_SMOKE_DB_DSN"), os.Getenv("DB_DSN"), defaultBatch23DBDSN)
	upstreamURL := strings.TrimRight(os.Getenv("UNIROUTE_SMOKE_UPSTREAM_URL"), "/")
	if upstreamURL == "" {
		return errors.New("UNIROUTE_SMOKE_UPSTREAM_URL is required")
	}
	encryptionKey := firstNonEmpty(os.Getenv("UNIROUTE_SMOKE_ENCRYPTION_KEY"), os.Getenv("ENCRYPTION_KEY"))
	if len([]byte(encryptionKey)) != 32 {
		return fmt.Errorf("ENCRYPTION_KEY/UNIROUTE_SMOKE_ENCRYPTION_KEY must be exactly 32 bytes, got %d", len([]byte(encryptionKey)))
	}

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

	orgID, err := upsertBatch23Org(ctx, tx)
	if err != nil {
		return err
	}
	adminID, err := upsertBatch23User(ctx, tx, orgID, batch23AdminUsername, batch23AdminPassword, "platform_admin")
	if err != nil {
		return err
	}
	developerID, err := upsertBatch23User(ctx, tx, orgID, batch23DeveloperUsername, batch23DeveloperPassword, "developer")
	if err != nil {
		return err
	}
	projectID, err := upsertBatch23Project(ctx, tx, orgID, adminID)
	if err != nil {
		return err
	}
	if err := upsertBatch23ProjectMember(ctx, tx, projectID, developerID); err != nil {
		return err
	}
	if err := upsertBatch23Wallet(ctx, tx, orgID); err != nil {
		return err
	}
	modelSetID, err := upsertBatch23Models(ctx, tx, orgID)
	if err != nil {
		return err
	}
	if err := assignBatch23ProjectModelSet(ctx, tx, projectID, modelSetID); err != nil {
		return err
	}
	if err := upsertBatch23MockChannel(ctx, tx, upstreamURL, []byte(encryptionKey)); err != nil {
		return err
	}
	if err := upsertBatch23AnalyticsLogs(ctx, tx, orgID, projectID); err != nil {
		return err
	}
	if err := upsertBatch23Incident(ctx, tx, orgID, projectID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	out := batch23SeedOutput{
		AdminUsername:       batch23AdminUsername,
		AdminPassword:       batch23AdminPassword,
		DeveloperUsername:   batch23DeveloperUsername,
		DeveloperPassword:   batch23DeveloperPassword,
		OrgID:               orgID,
		ProjectID:           projectID,
		ProjectSlug:         batch23ProjectSlug,
		ModelName:           batch23ModelName,
		ModelAlias:          batch23ModelAlias,
		ChannelName:         batch23ChannelName,
		Provider:            batch23Provider,
		UpstreamURL:         upstreamURL,
		IncidentTitle:       batch23IncidentTitle,
		SeededUpstreamLogID: batch23SeededUpstreamLogID,
		SeededCounterLogID:  batch23SeededCounterLogID,
		SeededErrorLogID:    batch23SeededErrorLogID,
	}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

func upsertBatch23Org(ctx context.Context, tx pgx.Tx) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO orgs (name, status, settings, org_type, created_at, updated_at)
		 VALUES ($1, 'active', '{}'::jsonb, 'customer', NOW(), NOW())
		 ON CONFLICT (name) DO UPDATE
		   SET status = 'active', updated_at = NOW()
		 RETURNING id`,
		batch23OrgName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert org: %w", err)
	}
	return id, nil
}

func upsertBatch23User(ctx context.Context, tx pgx.Tx, orgID int64, username, password, role string) (int64, error) {
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

func upsertBatch23Project(ctx context.Context, tx pgx.Tx, orgID, createdBy int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status, budget_cents, budget_period, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, 'Batch 23 product smoke fixture', 'active', 1000000, 'monthly', $4, NOW(), NOW())
		 ON CONFLICT (org_id, slug) DO UPDATE
		   SET name = EXCLUDED.name,
		       description = EXCLUDED.description,
		       status = 'active',
		       budget_cents = EXCLUDED.budget_cents,
		       budget_period = EXCLUDED.budget_period,
		       updated_at = NOW()
		 RETURNING id`,
		orgID, batch23ProjectName, batch23ProjectSlug, createdBy,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert project: %w", err)
	}
	return id, nil
}

func upsertBatch23ProjectMember(ctx context.Context, tx pgx.Tx, projectID, userID int64) error {
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

func upsertBatch23Wallet(ctx context.Context, tx pgx.Tx, orgID int64) error {
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

func upsertBatch23Models(ctx context.Context, tx pgx.Tx, orgID int64) (int64, error) {
	if _, err := tx.Exec(ctx,
		`INSERT INTO model_prices (model_name, provider, input_price_cents, output_price_cents, max_context, default_max_output, created_at, updated_at)
		 VALUES ($1, 'openai_compatible', 0.15, 0.60, 128000, 4096, NOW(), NOW())
		 ON CONFLICT (model_name) DO UPDATE
		   SET provider = EXCLUDED.provider,
		       input_price_cents = EXCLUDED.input_price_cents,
		       output_price_cents = EXCLUDED.output_price_cents,
		       max_context = EXCLUDED.max_context,
		       default_max_output = EXCLUDED.default_max_output,
		       updated_at = NOW()`,
		batch23ModelName,
	); err != nil {
		return 0, fmt.Errorf("upsert model price: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO model_registry (
		     org_id, model_name, display_name, provider_family, lifecycle,
		     context_length, output_limit, capability_tags, data_residency,
		     pricing_model_name, approved_catalog, migration_recommendation,
		     created_at, updated_at
		 )
		 VALUES ($1, $2, $3, 'openai_compatible', 'ga', 128000, 4096,
		         'chat,json,stream', 'global', $2, TRUE, '', NOW(), NOW())
		 ON CONFLICT (org_id, model_name) DO UPDATE
		   SET display_name = EXCLUDED.display_name,
		       provider_family = EXCLUDED.provider_family,
		       lifecycle = 'ga',
		       context_length = EXCLUDED.context_length,
		       output_limit = EXCLUDED.output_limit,
		       capability_tags = EXCLUDED.capability_tags,
		       data_residency = EXCLUDED.data_residency,
		       pricing_model_name = EXCLUDED.pricing_model_name,
		       approved_catalog = TRUE,
		       updated_at = NOW()`,
		orgID, batch23ModelName, batch23ModelDisplay,
	); err != nil {
		return 0, fmt.Errorf("upsert model registry: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO model_aliases (org_id, alias, target_model, display_name, description, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'Batch23 Company Chat', 'Batch 23 product smoke alias', 'active', NOW(), NOW())
		 ON CONFLICT (org_id, alias) DO UPDATE
		   SET target_model = EXCLUDED.target_model,
		       display_name = EXCLUDED.display_name,
		       description = EXCLUDED.description,
		       status = 'active',
		       updated_at = NOW()`,
		orgID, batch23ModelAlias, batch23ModelName,
	); err != nil {
		return 0, fmt.Errorf("upsert model alias: %w", err)
	}

	var modelSetID int64
	err := tx.QueryRow(ctx,
		`INSERT INTO model_sets (org_id, name, slug, description, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'Batch 23 approved model and alias', 'active', NOW(), NOW())
		 ON CONFLICT (org_id, slug) DO UPDATE
		   SET name = EXCLUDED.name,
		       description = EXCLUDED.description,
		       status = 'active',
		       updated_at = NOW()
		 RETURNING id`,
		orgID, batch23ModelSetName, batch23ModelSetSlug,
	).Scan(&modelSetID)
	if err != nil {
		return 0, fmt.Errorf("upsert model set: %w", err)
	}
	for _, item := range []struct {
		itemType string
		itemName string
	}{
		{"model", batch23ModelName},
		{"alias", batch23ModelAlias},
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

func assignBatch23ProjectModelSet(ctx context.Context, tx pgx.Tx, projectID, modelSetID int64) error {
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

func upsertBatch23MockChannel(ctx context.Context, tx pgx.Tx, upstreamURL string, encryptionKey []byte) error {
	encryptedKey, err := auth.EncryptKey([]byte(batch23UpstreamPlaintext), encryptionKey)
	if err != nil {
		return fmt.Errorf("encrypt mock channel key: %w", err)
	}

	var channelID int64
	err = tx.QueryRow(ctx,
		`SELECT id FROM channels WHERE name = $1 ORDER BY id ASC LIMIT 1`,
		batch23ChannelName,
	).Scan(&channelID)
	models := strings.Join([]string{batch23ModelName, batch23ModelAlias}, ",")
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx,
			`INSERT INTO channels (
			     name, provider, base_url, models, model_mapping, adapter_type, adapter_config,
			     max_context, default_max_output, priority, rpm_limit, tpm_limit, status,
			     circuit_state, fail_count, half_open_until, created_at, updated_at
			 )
			 VALUES ($1, $2, $3, $4, '{}'::jsonb, 'openai_compatible', '{}'::jsonb,
			         128000, 4096, 1, 600, 100000, 'active', 'healthy', 0, NULL, NOW(), NOW())
			 RETURNING id`,
			batch23ChannelName, batch23Provider, upstreamURL, models,
		).Scan(&channelID)
	}
	if err != nil {
		return fmt.Errorf("upsert mock channel: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE channels
		 SET provider = $2,
		     base_url = $3,
		     models = $4,
		     model_mapping = '{}'::jsonb,
		     adapter_type = 'openai_compatible',
		     adapter_config = '{}'::jsonb,
		     priority = 1,
		     status = 'active',
		     circuit_state = 'healthy',
		     fail_count = 0,
		     half_open_until = NULL,
		     updated_at = NOW()
		 WHERE id = $1`,
		channelID, batch23Provider, upstreamURL, models,
	); err != nil {
		return fmt.Errorf("update mock channel: %w", err)
	}

	var keyID int64
	err = tx.QueryRow(ctx,
		`SELECT id FROM channel_keys WHERE channel_id = $1 ORDER BY id ASC LIMIT 1`,
		channelID,
	).Scan(&keyID)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx,
			`INSERT INTO channel_keys (channel_id, key_enc, status, fail_count, cooldown_until, created_at, updated_at)
			 VALUES ($1, $2, 'active', 0, NULL, NOW(), NOW())`,
			channelID, encryptedKey,
		)
		if err != nil {
			return fmt.Errorf("insert mock channel key: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("query mock channel key: %w", err)
	}
	_, err = tx.Exec(ctx,
		`UPDATE channel_keys
		 SET key_enc = $1, status = 'active', fail_count = 0, cooldown_until = NULL, updated_at = NOW()
		 WHERE id = $2`,
		encryptedKey, keyID,
	)
	if err != nil {
		return fmt.Errorf("update mock channel key: %w", err)
	}
	return nil
}

func upsertBatch23AnalyticsLogs(ctx context.Context, tx pgx.Tx, orgID, projectID int64) error {
	rows := []struct {
		requestID        string
		totalTokens      int
		estimatedTokens  int
		costMicros       int64
		statusCode       int
		upstreamStatus   int
		upstreamError    string
		usageSource      string
		usageConfidence  float64
		promptTokens     int
		completionTokens int
	}{
		{batch23SeededUpstreamLogID, 40, 40, 40000, 200, 200, "", "upstream", 1.00, 24, 16},
		{batch23SeededCounterLogID, 20, 18, 20000, 200, 200, "", "counter_v2", 0.65, 12, 8},
		{batch23SeededErrorLogID, 0, 0, 0, 502, 502, "seeded upstream failure", "upstream_failed", 0.00, 0, 0},
	}
	for _, row := range rows {
		if _, err := tx.Exec(ctx,
			`INSERT INTO call_logs (
			     request_id, org_id, project_id, model, requested_model, provider, channel,
			     prompt_tokens, completion_tokens, total_tokens, cost_micros, latency_ms, ttft_ms,
			     status_code, upstream_status_code, upstream_error, usage_source, is_stream,
			     ip_address, estimated_tokens, usage_confidence, created_at
			 )
			 SELECT $1::varchar, $2::bigint, $3::bigint, $4::varchar, $5::varchar, $6::varchar, $7::varchar,
			        $8::integer, $9::integer, $10::integer, $11::bigint, 42, 0,
			        $12::integer, $13::integer, $14::text, $15::varchar, false,
			        '127.0.0.1', $16::integer, $17::double precision, NOW()
			 WHERE NOT EXISTS (
			     SELECT 1 FROM call_logs WHERE request_id = $1::varchar AND org_id = $2::bigint
			 )`,
			row.requestID, orgID, projectID, batch23ModelName, batch23ModelAlias, batch23Provider, batch23ChannelName,
			row.promptTokens, row.completionTokens, row.totalTokens, row.costMicros, row.statusCode, row.upstreamStatus,
			row.upstreamError, row.usageSource, row.estimatedTokens, row.usageConfidence,
		); err != nil {
			return fmt.Errorf("upsert analytics call log %s: %w", row.requestID, err)
		}
	}
	return nil
}

func upsertBatch23Incident(ctx context.Context, tx pgx.Tx, orgID, projectID int64) error {
	var eventID int64
	err := tx.QueryRow(ctx,
		`SELECT id FROM notification_events WHERE org_id = $1 AND dedupe_key = $2 LIMIT 1`,
		orgID, batch23IncidentDedupeKey,
	).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx,
			`INSERT INTO notification_events (
			     org_id, project_id, event_type, severity, title, message, resource,
			     resource_id, payload, status, dedupe_key, delivery_state, created_at, updated_at
			 )
			 VALUES ($1, $2, 'provider.channel.failure', 'warning', $3,
			         'Seeded warning incident for Batch 23 internal status smoke.',
			         'channel', $4, '{"batch":23}'::jsonb, 'open', $5, 'not_configured', NOW(), NOW())
			 RETURNING id`,
			orgID, projectID, batch23IncidentTitle, batch23ChannelName, batch23IncidentDedupeKey,
		).Scan(&eventID)
		if err != nil {
			return fmt.Errorf("insert incident: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("query incident: %w", err)
	}
	_, err = tx.Exec(ctx,
		`UPDATE notification_events
		 SET project_id = $2,
		     severity = 'warning',
		     title = $3,
		     message = 'Seeded warning incident for Batch 23 internal status smoke.',
		     resource = 'channel',
		     resource_id = $4,
		     payload = '{"batch":23}'::jsonb,
		     status = 'open',
		     delivery_state = 'not_configured',
		     updated_at = NOW()
		 WHERE id = $1`,
		eventID, projectID, batch23IncidentTitle, batch23ChannelName,
	)
	if err != nil {
		return fmt.Errorf("update incident: %w", err)
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
