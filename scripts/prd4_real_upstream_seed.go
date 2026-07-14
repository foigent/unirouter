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
	prd4UpstreamDBDSN         = "postgres://uniroute:uniroute_pass_2024@127.0.0.1:5432/uniroute?sslmode=disable"
	prd4UpstreamAllowMutation = "approved-target-real-upstream"

	prd4UpstreamOrgName       = "PRD4 Real Upstream Evidence Org"
	prd4UpstreamAdminUsername = "prd4-real-upstream-admin"
	prd4UpstreamAdminPassword = "prd4RealUpstreamAdmin123!"
	prd4UpstreamProjectName   = "PRD4 Real Upstream Evidence Project"
	prd4UpstreamProjectSlug   = "prd4-real-upstream"
	prd4UpstreamModelSetName  = "PRD4 Real Upstream Evidence Models"
	prd4UpstreamModelSetSlug  = "prd4-real-upstream-models"
	prd4UpstreamAPIKeyName    = "prd4-real-upstream-evidence-key"
)

type upstreamProviderSeed struct {
	Key           string
	Model         string
	Provider      string
	ChannelName   string
	BaseURL       string
	AdapterType   string
	AdapterConfig string
	DisplayName   string
	Priority      int
}

type prd4UpstreamSeedOutput struct {
	AdminUsername string                         `json:"admin_username"`
	AdminPassword string                         `json:"admin_password"`
	OrgID         int64                          `json:"org_id"`
	ProjectID     int64                          `json:"project_id"`
	ProjectSlug   string                         `json:"project_slug"`
	APIKey        string                         `json:"api_key"`
	APIKeyPrefix  string                         `json:"api_key_prefix"`
	Providers     []prd4UpstreamProviderEvidence `json:"providers"`
}

type prd4UpstreamProviderEvidence struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	ChannelName string `json:"channel_name"`
	AdapterType string `json:"adapter_type"`
	BaseURL     string `json:"base_url"`
}

func main() {
	if err := runPRD4UpstreamSeed(); err != nil {
		fmt.Fprintf(os.Stderr, "seed failed: %v\n", err)
		os.Exit(1)
	}
}

func runPRD4UpstreamSeed() error {
	if os.Getenv("UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION") != prd4UpstreamAllowMutation {
		return fmt.Errorf("UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION=%s is required before seeding real-upstream evidence fixtures", prd4UpstreamAllowMutation)
	}

	providers, err := configuredProviders()
	if err != nil {
		return err
	}
	encryptionKey := firstNonEmpty(os.Getenv("UNIROUTE_SMOKE_ENCRYPTION_KEY"), os.Getenv("ENCRYPTION_KEY"))
	if len([]byte(encryptionKey)) != 32 {
		return fmt.Errorf("ENCRYPTION_KEY/UNIROUTE_SMOKE_ENCRYPTION_KEY must be exactly 32 bytes, got %d", len([]byte(encryptionKey)))
	}
	apiKeyHashSecret := firstNonEmpty(os.Getenv("UNIROUTE_SMOKE_API_KEY_HASH_SECRET"), os.Getenv("API_KEY_HASH_SECRET"), os.Getenv("JWT_SECRET"))
	if apiKeyHashSecret == "" {
		return errors.New("UNIROUTE_SMOKE_API_KEY_HASH_SECRET, API_KEY_HASH_SECRET, or JWT_SECRET is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	dbDSN := firstNonEmpty(os.Getenv("UNIROUTE_SMOKE_DB_DSN"), os.Getenv("DB_DSN"), prd4UpstreamDBDSN)
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

	orgID, err := upsertPRD4UpstreamOrg(ctx, tx)
	if err != nil {
		return err
	}
	adminID, err := upsertPRD4UpstreamAdmin(ctx, tx, orgID)
	if err != nil {
		return err
	}
	projectID, err := upsertPRD4UpstreamProject(ctx, tx, orgID, adminID)
	if err != nil {
		return err
	}
	if err := upsertPRD4UpstreamProjectMember(ctx, tx, projectID, adminID); err != nil {
		return err
	}
	if err := upsertPRD4UpstreamWallet(ctx, tx, orgID); err != nil {
		return err
	}
	modelSetID, err := upsertPRD4UpstreamModels(ctx, tx, orgID, providers)
	if err != nil {
		return err
	}
	if err := assignPRD4UpstreamProjectModelSet(ctx, tx, projectID, modelSetID); err != nil {
		return err
	}
	if err := upsertPRD4UpstreamChannels(ctx, tx, []byte(encryptionKey), providers); err != nil {
		return err
	}
	rawKey, keyPrefix, err := upsertPRD4UpstreamAPIKey(ctx, tx, orgID, projectID, adminID, apiKeyHashSecret, providers)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	out := prd4UpstreamSeedOutput{
		AdminUsername: prd4UpstreamAdminUsername,
		AdminPassword: prd4UpstreamAdminPassword,
		OrgID:         orgID,
		ProjectID:     projectID,
		ProjectSlug:   prd4UpstreamProjectSlug,
		APIKey:        rawKey,
		APIKeyPrefix:  keyPrefix,
	}
	for _, provider := range providers {
		out.Providers = append(out.Providers, prd4UpstreamProviderEvidence{
			Provider:    provider.Provider,
			Model:       provider.Model,
			ChannelName: provider.ChannelName,
			AdapterType: provider.AdapterType,
			BaseURL:     provider.BaseURL,
		})
	}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

func configuredProviders() ([]upstreamProviderSeed, error) {
	openRouterKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	openRouterModel := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
	anthropicKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	anthropicModel := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL"))
	if openRouterKey == "" || openRouterModel == "" || anthropicKey == "" || anthropicModel == "" {
		return nil, errors.New("OPENROUTER_API_KEY, OPENROUTER_MODEL, ANTHROPIC_API_KEY, and ANTHROPIC_MODEL are required")
	}
	return []upstreamProviderSeed{
		{
			Key:           openRouterKey,
			Model:         openRouterModel,
			Provider:      "openrouter",
			ChannelName:   "prd4-real-openrouter-compatible",
			BaseURL:       firstNonEmpty(os.Getenv("OPENROUTER_BASE_URL"), "https://openrouter.ai/api/v1"),
			AdapterType:   "openai_compatible",
			AdapterConfig: "{}",
			DisplayName:   "PRD4 Real OpenRouter Compatible",
			Priority:      10,
		},
		{
			Key:           anthropicKey,
			Model:         anthropicModel,
			Provider:      "anthropic",
			ChannelName:   "prd4-real-anthropic-native",
			BaseURL:       firstNonEmpty(os.Getenv("ANTHROPIC_BASE_URL"), "https://api.anthropic.com"),
			AdapterType:   "anthropic_native",
			AdapterConfig: `{"anthropic_version":"2023-06-01"}`,
			DisplayName:   "PRD4 Real Anthropic Native",
			Priority:      20,
		},
	}, nil
}

func upsertPRD4UpstreamOrg(ctx context.Context, tx pgx.Tx) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO orgs (name, status, settings, org_type, created_at, updated_at)
		 VALUES ($1, 'active', '{}'::jsonb, 'customer', NOW(), NOW())
		 ON CONFLICT (name) DO UPDATE
		   SET status = 'active', updated_at = NOW()
		 RETURNING id`,
		prd4UpstreamOrgName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert org: %w", err)
	}
	return id, nil
}

func upsertPRD4UpstreamAdmin(ctx context.Context, tx pgx.Tx, orgID int64) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(prd4UpstreamAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("hash admin password: %w", err)
	}
	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'platform_admin', 'active', NOW(), NOW())
		 ON CONFLICT (org_id, username) DO UPDATE
		   SET password_hash = EXCLUDED.password_hash,
		       role = 'platform_admin',
		       status = 'active',
		       updated_at = NOW()
		 RETURNING id`,
		orgID, prd4UpstreamAdminUsername, string(hash),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert admin: %w", err)
	}
	return id, nil
}

func upsertPRD4UpstreamProject(ctx context.Context, tx pgx.Tx, orgID, createdBy int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status, budget_cents, budget_period, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, 'PRD4 real upstream target-environment evidence fixture', 'active', 200000, 'monthly', $4, NOW(), NOW())
		 ON CONFLICT (org_id, slug) DO UPDATE
		   SET name = EXCLUDED.name,
		       description = EXCLUDED.description,
		       status = 'active',
		       budget_cents = EXCLUDED.budget_cents,
		       budget_period = EXCLUDED.budget_period,
		       updated_at = NOW()
		 RETURNING id`,
		orgID, prd4UpstreamProjectName, prd4UpstreamProjectSlug, createdBy,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert project: %w", err)
	}
	return id, nil
}

func upsertPRD4UpstreamProjectMember(ctx context.Context, tx pgx.Tx, projectID, userID int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role, created_at, updated_at)
		 VALUES ($1, $2, 'project_admin', NOW(), NOW())
		 ON CONFLICT (project_id, user_id) DO UPDATE
		   SET role = 'project_admin', updated_at = NOW()`,
		projectID, userID,
	)
	if err != nil {
		return fmt.Errorf("upsert project member: %w", err)
	}
	return nil
}

func upsertPRD4UpstreamWallet(ctx context.Context, tx pgx.Tx, orgID int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros, currency, auto_freeze, low_balance_threshold_cents, created_at, updated_at)
		 VALUES ($1, 2000000000, 2000000000, 0, 0, 'CNY', false, 0, NOW(), NOW())
		 ON CONFLICT (org_id) DO UPDATE
		   SET balance_micros = GREATEST(wallets.balance_micros, 2000000000),
		       granted_balance_micros = GREATEST(wallets.granted_balance_micros, 2000000000),
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

func upsertPRD4UpstreamModels(ctx context.Context, tx pgx.Tx, orgID int64, providers []upstreamProviderSeed) (int64, error) {
	for _, provider := range providers {
		if _, err := tx.Exec(ctx,
			`INSERT INTO model_prices (model_name, provider, input_price_cents, output_price_cents, max_context, default_max_output, created_at, updated_at)
			 VALUES ($1, $2, 1.0, 1.0, 200000, 64, NOW(), NOW())
			 ON CONFLICT (model_name) DO UPDATE
			   SET provider = EXCLUDED.provider,
			       input_price_cents = EXCLUDED.input_price_cents,
			       output_price_cents = EXCLUDED.output_price_cents,
			       max_context = EXCLUDED.max_context,
			       default_max_output = EXCLUDED.default_max_output,
			       updated_at = NOW()`,
			provider.Model, provider.Provider,
		); err != nil {
			return 0, fmt.Errorf("upsert model price %s: %w", provider.Model, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO model_registry (
			     org_id, model_name, display_name, provider_family, lifecycle,
			     context_length, output_limit, capability_tags, data_residency,
			     pricing_model_name, approved_catalog, migration_recommendation,
			     created_at, updated_at
			 )
			 VALUES ($1, $2, $3, $4, 'ga', 200000, 64,
			         'chat,stream,real-upstream', 'provider-managed', $2, TRUE, '', NOW(), NOW())
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
			orgID, provider.Model, provider.DisplayName, provider.Provider,
		); err != nil {
			return 0, fmt.Errorf("upsert model registry %s: %w", provider.Model, err)
		}
	}

	var modelSetID int64
	err := tx.QueryRow(ctx,
		`INSERT INTO model_sets (org_id, name, slug, description, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'PRD4 real upstream evidence models', 'active', NOW(), NOW())
		 ON CONFLICT (org_id, slug) DO UPDATE
		   SET name = EXCLUDED.name,
		       description = EXCLUDED.description,
		       status = 'active',
		       updated_at = NOW()
		 RETURNING id`,
		orgID, prd4UpstreamModelSetName, prd4UpstreamModelSetSlug,
	).Scan(&modelSetID)
	if err != nil {
		return 0, fmt.Errorf("upsert model set: %w", err)
	}
	for _, provider := range providers {
		if _, err := tx.Exec(ctx,
			`INSERT INTO model_set_items (model_set_id, item_type, item_name, created_at)
			 VALUES ($1, 'model', $2, NOW())
			 ON CONFLICT (model_set_id, item_type, item_name) DO NOTHING`,
			modelSetID, provider.Model,
		); err != nil {
			return 0, fmt.Errorf("upsert model set item %s: %w", provider.Model, err)
		}
	}
	return modelSetID, nil
}

func assignPRD4UpstreamProjectModelSet(ctx context.Context, tx pgx.Tx, projectID, modelSetID int64) error {
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

func upsertPRD4UpstreamChannels(ctx context.Context, tx pgx.Tx, encryptionKey []byte, providers []upstreamProviderSeed) error {
	for _, provider := range providers {
		encryptedKey, err := auth.EncryptKey([]byte(provider.Key), encryptionKey)
		if err != nil {
			return fmt.Errorf("encrypt %s channel key: %w", provider.Provider, err)
		}
		var channelID int64
		err = tx.QueryRow(ctx,
			`SELECT id FROM channels WHERE name = $1 ORDER BY id ASC LIMIT 1`,
			provider.ChannelName,
		).Scan(&channelID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx,
				`INSERT INTO channels (
				     name, provider, base_url, models, model_mapping, adapter_type, adapter_config,
				     max_context, default_max_output, priority, rpm_limit, tpm_limit, status,
				     circuit_state, fail_count, half_open_until, created_at, updated_at
				 )
				 VALUES ($1, $2, $3, $4, '{}'::jsonb, $5, $6::jsonb,
				         200000, 64, $7, 60, 100000, 'active', 'healthy', 0, NULL, NOW(), NOW())
				 RETURNING id`,
				provider.ChannelName, provider.Provider, provider.BaseURL, provider.Model, provider.AdapterType, provider.AdapterConfig, provider.Priority,
			).Scan(&channelID)
		}
		if err != nil {
			return fmt.Errorf("upsert channel %s: %w", provider.Provider, err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE channels
			 SET provider = $2,
			     base_url = $3,
			     models = $4,
			     model_mapping = '{}'::jsonb,
			     adapter_type = $5,
			     adapter_config = $6::jsonb,
			     priority = $7,
			     status = 'active',
			     circuit_state = 'healthy',
			     fail_count = 0,
			     half_open_until = NULL,
			     updated_at = NOW()
			 WHERE id = $1`,
			channelID, provider.Provider, provider.BaseURL, provider.Model, provider.AdapterType, provider.AdapterConfig, provider.Priority,
		); err != nil {
			return fmt.Errorf("update channel %s: %w", provider.Provider, err)
		}

		var keyID int64
		err = tx.QueryRow(ctx,
			`SELECT id FROM channel_keys WHERE channel_id = $1 ORDER BY id ASC LIMIT 1`,
			channelID,
		).Scan(&keyID)
		if errors.Is(err, pgx.ErrNoRows) {
			_, err = tx.Exec(ctx,
				`INSERT INTO channel_keys (channel_id, key_enc, allowed_models, status, fail_count, cooldown_until, created_at, updated_at)
				 VALUES ($1, $2, $3, 'active', 0, NULL, NOW(), NOW())`,
				channelID, encryptedKey, provider.Model,
			)
			if err != nil {
				return fmt.Errorf("insert channel key %s: %w", provider.Provider, err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("query channel key %s: %w", provider.Provider, err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE channel_keys
			 SET key_enc = $1, allowed_models = $2, status = 'active', fail_count = 0, cooldown_until = NULL, updated_at = NOW()
			 WHERE id = $3`,
			encryptedKey, provider.Model, keyID,
		); err != nil {
			return fmt.Errorf("update channel key %s: %w", provider.Provider, err)
		}
	}
	return nil
}

func upsertPRD4UpstreamAPIKey(ctx context.Context, tx pgx.Tx, orgID, projectID, ownerUserID int64, secret string, providers []upstreamProviderSeed) (string, string, error) {
	rawKey, keyPrefix, err := auth.GenerateAPIKey(orgID)
	if err != nil {
		return "", "", fmt.Errorf("generate api key: %w", err)
	}
	keyHash := auth.HashAPIKey(rawKey, secret)
	allowedModels := make([]string, 0, len(providers))
	for _, provider := range providers {
		allowedModels = append(allowedModels, provider.Model)
	}

	var existingID int64
	err = tx.QueryRow(ctx,
		`SELECT id FROM api_keys WHERE org_id = $1 AND project_id = $2 AND name = $3 ORDER BY id ASC LIMIT 1`,
		orgID, projectID, prd4UpstreamAPIKeyName,
	).Scan(&existingID)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx,
			`INSERT INTO api_keys (
			     org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix,
			     status, rpm_limit, qps_limit, allowed_ips, allowed_models, created_at, updated_at
			 )
			 VALUES ($1, $2, $3, 'service', $4, $5, $6, 'active', 20, 5, '', $7, NOW(), NOW())`,
			orgID, projectID, ownerUserID, prd4UpstreamAPIKeyName, keyHash, keyPrefix, strings.Join(allowedModels, ","),
		)
		if err != nil {
			return "", "", fmt.Errorf("insert api key: %w", err)
		}
		return rawKey, keyPrefix, nil
	}
	if err != nil {
		return "", "", fmt.Errorf("query api key: %w", err)
	}
	_, err = tx.Exec(ctx,
		`UPDATE api_keys
		 SET owner_user_id = $1,
		     key_type = 'service',
		     key_hash = $2,
		     key_prefix = $3,
		     status = 'active',
		     rpm_limit = 20,
		     qps_limit = 5,
		     allowed_ips = '',
		     allowed_models = $4,
		     updated_at = NOW()
		 WHERE id = $5`,
		ownerUserID, keyHash, keyPrefix, strings.Join(allowedModels, ","), existingID,
	)
	if err != nil {
		return "", "", fmt.Errorf("update api key: %w", err)
	}
	return rawKey, keyPrefix, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
