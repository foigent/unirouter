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
	defaultSmokeDBDSN = "postgres://uniroute:uniroute_pass_2024@127.0.0.1:5432/uniroute?sslmode=disable"

	smokeOrgName           = "PRD2 Batch 16 Browser Smoke Org"
	smokeUsername          = "prd2-dev-smoke"
	smokePassword          = "prd2DevSmoke123!"
	smokeProjectName       = "PRD2 Browser Smoke Project"
	smokeProjectSlug       = "prd2-browser-smoke"
	smokeOtherProjectName  = "PRD2 Browser Smoke Other Project"
	smokeOtherProjectSlug  = "prd2-browser-smoke-other"
	smokeModelName         = "batch16-smoke-gpt-4o"
	smokeModelAlias        = "company-chat"
	smokeModelSetName      = "PRD2 Batch 16 Approved Models"
	smokeModelSetSlug      = "prd2-batch16-approved"
	smokeChannelName       = "prd2-batch16-openai-compatible-mock"
	smokeUpstreamPlaintext = "upstream-browser-smoke-key"
)

type seedOutput struct {
	Username         string `json:"username"`
	Password         string `json:"password"`
	OrgID            int64  `json:"org_id"`
	ProjectID        int64  `json:"project_id"`
	ProjectSlug      string `json:"project_slug"`
	OtherProjectID   int64  `json:"other_project_id"`
	OtherProjectSlug string `json:"other_project_slug"`
	ModelAlias       string `json:"model_alias"`
	ModelName        string `json:"model_name"`
	ChannelName      string `json:"channel_name"`
	UpstreamURL      string `json:"upstream_url"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "seed failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbDSN := firstNonEmpty(os.Getenv("UNIROUTE_SMOKE_DB_DSN"), os.Getenv("DB_DSN"), defaultSmokeDBDSN)
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

	orgID, err := upsertOrg(ctx, tx)
	if err != nil {
		return err
	}
	userID, err := upsertDeveloperUser(ctx, tx, orgID)
	if err != nil {
		return err
	}
	projectID, err := upsertProject(ctx, tx, orgID, userID, smokeProjectName, smokeProjectSlug)
	if err != nil {
		return err
	}
	otherProjectID, err := upsertProject(ctx, tx, orgID, userID, smokeOtherProjectName, smokeOtherProjectSlug)
	if err != nil {
		return err
	}
	if err := upsertWallet(ctx, tx, orgID); err != nil {
		return err
	}
	if err := upsertProjectMembership(ctx, tx, projectID, userID); err != nil {
		return err
	}
	modelSetID, err := upsertModelGovernance(ctx, tx, orgID)
	if err != nil {
		return err
	}
	if err := assignProjectModelSet(ctx, tx, projectID, modelSetID); err != nil {
		return err
	}
	if err := upsertMockChannel(ctx, tx, upstreamURL, []byte(encryptionKey)); err != nil {
		return err
	}
	if err := upsertOtherProjectHiddenLog(ctx, tx, orgID, otherProjectID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	out := seedOutput{
		Username:         smokeUsername,
		Password:         smokePassword,
		OrgID:            orgID,
		ProjectID:        projectID,
		ProjectSlug:      smokeProjectSlug,
		OtherProjectID:   otherProjectID,
		OtherProjectSlug: smokeOtherProjectSlug,
		ModelAlias:       smokeModelAlias,
		ModelName:        smokeModelName,
		ChannelName:      smokeChannelName,
		UpstreamURL:      upstreamURL,
	}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

func upsertOrg(ctx context.Context, tx pgx.Tx) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO orgs (name, status, settings, org_type, created_at, updated_at)
		 VALUES ($1, 'active', '{}'::jsonb, 'customer', NOW(), NOW())
		 ON CONFLICT (name) DO UPDATE
		   SET status = 'active', updated_at = NOW()
		 RETURNING id`,
		smokeOrgName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert org: %w", err)
	}
	return id, nil
}

func upsertDeveloperUser(ctx context.Context, tx pgx.Tx, orgID int64) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(smokePassword), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("hash smoke password: %w", err)
	}
	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'developer', 'active', NOW(), NOW())
		 ON CONFLICT (org_id, username) DO UPDATE
		   SET password_hash = EXCLUDED.password_hash,
		       role = 'developer',
		       status = 'active',
		       updated_at = NOW()
		 RETURNING id`,
		orgID, smokeUsername, string(hash),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert developer user: %w", err)
	}
	return id, nil
}

func upsertProject(ctx context.Context, tx pgx.Tx, orgID, userID int64, name, slug string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status, budget_cents, budget_period, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, 'Batch 16 browser smoke fixture', 'active', 1000000, 'monthly', $4, NOW(), NOW())
		 ON CONFLICT (org_id, slug) DO UPDATE
		   SET name = EXCLUDED.name,
		       description = EXCLUDED.description,
		       status = 'active',
		       budget_cents = EXCLUDED.budget_cents,
		       budget_period = EXCLUDED.budget_period,
		       updated_at = NOW()
		 RETURNING id`,
		orgID, name, slug, userID,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert project %s: %w", slug, err)
	}
	return id, nil
}

func upsertWallet(ctx context.Context, tx pgx.Tx, orgID int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO wallets (org_id, balance, granted_balance, frozen, total_spent, currency, auto_freeze, low_balance_threshold_cents, created_at, updated_at)
		 VALUES ($1, 1000000, 1000000, 0, 0, 'CNY', false, 0, NOW(), NOW())
		 ON CONFLICT (org_id) DO UPDATE
		   SET balance = GREATEST(wallets.balance, 1000000),
		       granted_balance = GREATEST(wallets.granted_balance, 1000000),
		       frozen = 0,
		       currency = 'CNY',
		       updated_at = NOW()`,
		orgID,
	)
	if err != nil {
		return fmt.Errorf("upsert wallet: %w", err)
	}
	return nil
}

func upsertProjectMembership(ctx context.Context, tx pgx.Tx, projectID, userID int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role, created_at, updated_at)
		 VALUES ($1, $2, 'project_developer', NOW(), NOW())
		 ON CONFLICT (project_id, user_id) DO UPDATE
		   SET role = 'project_developer', updated_at = NOW()`,
		projectID, userID,
	)
	if err != nil {
		return fmt.Errorf("upsert project membership: %w", err)
	}
	return nil
}

func upsertModelGovernance(ctx context.Context, tx pgx.Tx, orgID int64) (int64, error) {
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
		smokeModelName,
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
		 VALUES ($1, $2, 'GPT-4o', 'openai_compatible', 'ga', 128000, 4096,
		         'chat,tool-use,json', 'global', $2, TRUE, '', NOW(), NOW())
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
		orgID, smokeModelName,
	); err != nil {
		return 0, fmt.Errorf("upsert model registry: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO model_aliases (org_id, alias, target_model, display_name, description, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'Company Chat', 'Batch 16 approved project alias', 'active', NOW(), NOW())
		 ON CONFLICT (org_id, alias) DO UPDATE
		   SET target_model = EXCLUDED.target_model,
		       display_name = EXCLUDED.display_name,
		       description = EXCLUDED.description,
		       status = 'active',
		       updated_at = NOW()`,
		orgID, smokeModelAlias, smokeModelName,
	); err != nil {
		return 0, fmt.Errorf("upsert model alias: %w", err)
	}

	var modelSetID int64
	err := tx.QueryRow(ctx,
		`INSERT INTO model_sets (org_id, name, slug, description, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'Batch 16 Developer Console approved model set', 'active', NOW(), NOW())
		 ON CONFLICT (org_id, slug) DO UPDATE
		   SET name = EXCLUDED.name,
		       description = EXCLUDED.description,
		       status = 'active',
		       updated_at = NOW()
		 RETURNING id`,
		orgID, smokeModelSetName, smokeModelSetSlug,
	).Scan(&modelSetID)
	if err != nil {
		return 0, fmt.Errorf("upsert model set: %w", err)
	}
	for _, item := range []struct {
		itemType string
		itemName string
	}{
		{"model", smokeModelName},
		{"alias", smokeModelAlias},
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

func assignProjectModelSet(ctx context.Context, tx pgx.Tx, projectID, modelSetID int64) error {
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

func upsertMockChannel(ctx context.Context, tx pgx.Tx, upstreamURL string, encryptionKey []byte) error {
	encryptedKey, err := auth.EncryptKey([]byte(smokeUpstreamPlaintext), encryptionKey)
	if err != nil {
		return fmt.Errorf("encrypt mock channel key: %w", err)
	}

	var channelID int64
	err = tx.QueryRow(ctx,
		`SELECT id FROM channels WHERE name = $1 ORDER BY id ASC LIMIT 1`,
		smokeChannelName,
	).Scan(&channelID)
	models := strings.Join([]string{smokeModelName, smokeModelAlias}, ",")
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx,
			`INSERT INTO channels (
			     name, provider, base_url, models, model_mapping, adapter_type, adapter_config,
			     max_context, default_max_output, priority, rpm_limit, tpm_limit, status,
			     circuit_state, fail_count, half_open_until, created_at, updated_at
			 )
			 VALUES ($1, 'batch16-mock', $2, $3, '{}'::jsonb, 'openai_compatible', '{}'::jsonb,
			         128000, 4096, 1, 600, 100000, 'active', 'healthy', 0, NULL, NOW(), NOW())
			 RETURNING id`,
			smokeChannelName, upstreamURL, models,
		).Scan(&channelID)
	}
	if err != nil {
		return fmt.Errorf("upsert mock channel: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE channels
		 SET provider = 'batch16-mock',
		     base_url = $2,
		     models = $3,
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
		channelID, upstreamURL, models,
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

func upsertOtherProjectHiddenLog(ctx context.Context, tx pgx.Tx, orgID, otherProjectID int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO call_logs (
		     request_id, org_id, project_id, model, requested_model, provider, channel,
		     prompt_tokens, completion_tokens, total_tokens, cost_cents, latency_ms, ttft_ms,
		     status_code, upstream_status_code, upstream_error, usage_source, is_stream,
		     ip_address, estimated_tokens, usage_confidence, created_at
		 )
		 SELECT 'batch16-other-project-hidden', $1, $2, $3, $4, 'batch16-mock', $5,
		         1, 1, 2, 1, 1, 0, 200, 200, '', 'upstream', false,
		         '127.0.0.1', 2, 1.000, NOW()
		 WHERE NOT EXISTS (
		     SELECT 1 FROM call_logs WHERE request_id = 'batch16-other-project-hidden' AND project_id = $2
		 )`,
		orgID, otherProjectID, smokeModelName, smokeModelAlias, smokeChannelName,
	)
	if err != nil {
		return fmt.Errorf("upsert hidden other-project call log: %w", err)
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
