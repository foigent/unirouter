package models

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Organization represents the orgs table.
type Organization struct {
	ID        int64           `json:"id" db:"id"`
	Name      string          `json:"name" db:"name"`
	Status    string          `json:"status" db:"status"`
	Settings  json.RawMessage `json:"settings" db:"settings"`
	ParentID  *int64          `json:"parent_id" db:"parent_id"`
	OrgType   string          `json:"org_type" db:"org_type"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt time.Time       `json:"updated_at" db:"updated_at"`
}

// User represents the users table.
type User struct {
	ID           int64     `json:"id" db:"id"`
	OrgID        int64     `json:"org_id" db:"org_id"`
	Username     string    `json:"username" db:"username"`
	PasswordHash string    `json:"-" db:"password_hash"`
	Role         string    `json:"role" db:"role"`
	Status       string    `json:"status" db:"status"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// Project represents the projects table.
type Project struct {
	ID           int64     `json:"id" db:"id"`
	OrgID        int64     `json:"org_id" db:"org_id"`
	Name         string    `json:"name" db:"name"`
	Slug         string    `json:"slug" db:"slug"`
	Description  string    `json:"description" db:"description"`
	Status       string    `json:"status" db:"status"`
	BudgetCents  *int      `json:"budget_cents" db:"budget_cents"`
	BudgetPeriod string    `json:"budget_period" db:"budget_period"`
	CreatedBy    *int64    `json:"created_by" db:"created_by"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// ProjectMember represents a user's project-scoped membership.
type ProjectMember struct {
	ProjectID int64     `json:"project_id" db:"project_id"`
	UserID    int64     `json:"user_id" db:"user_id"`
	Role      string    `json:"role" db:"role"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// Channel represents the channels table.
type Channel struct {
	ID               int64           `json:"id" db:"id"`
	Name             string          `json:"name" db:"name"`
	Provider         string          `json:"provider" db:"provider"`
	BaseURL          string          `json:"base_url" db:"base_url"`
	Models           string          `json:"models" db:"models"`
	ModelMapping     json.RawMessage `json:"model_mapping" db:"model_mapping"`
	AdapterType      string          `json:"adapter_type" db:"adapter_type"`
	AdapterConfig    json.RawMessage `json:"adapter_config" db:"adapter_config"`
	MaxContext       int             `json:"max_context" db:"max_context"`
	DefaultMaxOutput int             `json:"default_max_output" db:"default_max_output"`
	Priority         int             `json:"priority" db:"priority"`
	RPMLimit         int             `json:"rpm_limit" db:"rpm_limit"`
	TPMLimit         int             `json:"tpm_limit" db:"tpm_limit"`
	Status           string          `json:"status" db:"status"`
	CircuitState     string          `json:"circuit_state" db:"circuit_state"`
	HalfOpenUntil    *time.Time      `json:"half_open_until" db:"half_open_until"`
	FailCount        int             `json:"fail_count" db:"fail_count"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at" db:"updated_at"`
}

// ChannelKey represents the channel_keys table.
type ChannelKey struct {
	ID            int64      `json:"id" db:"id"`
	ChannelID     int64      `json:"channel_id" db:"channel_id"`
	KeyEnc        string     `json:"-" db:"key_enc"`
	Status        string     `json:"status" db:"status"`
	FailCount     int        `json:"fail_count" db:"fail_count"`
	LastUsedAt    *time.Time `json:"last_used_at" db:"last_used_at"`
	CooldownUntil *time.Time `json:"cooldown_until" db:"cooldown_until"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}

// ApiKey represents the api_keys table.
type ApiKey struct {
	ID            int64     `json:"id" db:"id"`
	OrgID         int64     `json:"org_id" db:"org_id"`
	ProjectID     *int64    `json:"project_id" db:"project_id"`
	Name          string    `json:"name" db:"name"`
	KeyPrefix     string    `json:"key_prefix" db:"key_prefix"`
	KeyHash       string    `json:"-" db:"key_hash"`
	IPWhitelist   string    `json:"allowed_ips" db:"allowed_ips"`
	AllowedModels string    `json:"allowed_models" db:"allowed_models"`
	RPMLimit      int       `json:"rpm_limit" db:"rpm_limit"`
	QPSLimit      int       `json:"qps_limit" db:"qps_limit"`
	BudgetLimit   *int      `json:"budget_limit" db:"budget_limit"`
	Status        string    `json:"status" db:"status"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// ModelPrice represents the model_prices table.
type ModelPrice struct {
	ID               int64     `json:"id" db:"id"`
	ModelName        string    `json:"model_name" db:"model_name"`
	Provider         string    `json:"provider" db:"provider"`
	InputPriceCents  float64   `json:"input_price_cents" db:"input_price_cents"`
	OutputPriceCents float64   `json:"output_price_cents" db:"output_price_cents"`
	MaxContext       int       `json:"max_context" db:"max_context"`
	DefaultMaxOutput int       `json:"default_max_output" db:"default_max_output"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

// OrgModelPrice represents the org_model_prices table.
type OrgModelPrice struct {
	ID         int64     `json:"id" db:"id"`
	OrgID      int64     `json:"org_id" db:"org_id"`
	ModelName  string    `json:"model_name" db:"model_name"`
	Multiplier float64   `json:"multiplier" db:"multiplier"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

// ModelRegistryEntry represents an internal governed model.
type ModelRegistryEntry struct {
	ID                      int64     `json:"id" db:"id"`
	OrgID                   int64     `json:"org_id" db:"org_id"`
	ModelName               string    `json:"model_name" db:"model_name"`
	DisplayName             string    `json:"display_name" db:"display_name"`
	ProviderFamily          string    `json:"provider_family" db:"provider_family"`
	Lifecycle               string    `json:"lifecycle" db:"lifecycle"`
	ContextLength           int       `json:"context_length" db:"context_length"`
	OutputLimit             int       `json:"output_limit" db:"output_limit"`
	CapabilityTags          string    `json:"capability_tags" db:"capability_tags"`
	DataResidency           string    `json:"data_residency" db:"data_residency"`
	PricingModelName        string    `json:"pricing_model_name" db:"pricing_model_name"`
	ApprovedCatalog         bool      `json:"approved_catalog" db:"approved_catalog"`
	MigrationRecommendation string    `json:"migration_recommendation" db:"migration_recommendation"`
	CreatedAt               time.Time `json:"created_at" db:"created_at"`
	UpdatedAt               time.Time `json:"updated_at" db:"updated_at"`
}

// ModelAlias represents an enterprise-facing stable model alias.
type ModelAlias struct {
	ID          int64     `json:"id" db:"id"`
	OrgID       int64     `json:"org_id" db:"org_id"`
	Alias       string    `json:"alias" db:"alias"`
	TargetModel string    `json:"target_model" db:"target_model"`
	DisplayName string    `json:"display_name" db:"display_name"`
	Description string    `json:"description" db:"description"`
	Status      string    `json:"status" db:"status"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// ModelSet represents a curated group of models and aliases.
type ModelSet struct {
	ID          int64     `json:"id" db:"id"`
	OrgID       int64     `json:"org_id" db:"org_id"`
	Name        string    `json:"name" db:"name"`
	Slug        string    `json:"slug" db:"slug"`
	Description string    `json:"description" db:"description"`
	Status      string    `json:"status" db:"status"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// ModelSetItem represents a model or alias inside a model set.
type ModelSetItem struct {
	ModelSetID int64     `json:"model_set_id" db:"model_set_id"`
	ItemType   string    `json:"item_type" db:"item_type"`
	ItemName   string    `json:"item_name" db:"item_name"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// Wallet represents the wallets table.
type Wallet struct {
	OrgID          int64     `json:"org_id" db:"org_id"`
	Balance        int       `json:"balance" db:"balance"`
	GrantedBalance int       `json:"granted_balance" db:"granted_balance"`
	Frozen         int       `json:"frozen" db:"frozen"`
	TotalSpent     int       `json:"total_spent" db:"total_spent"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}

// Transaction represents the transactions table.
type Transaction struct {
	ID           int64     `json:"id" db:"id"`
	RequestID    string    `json:"request_id" db:"request_id"`
	OrgID        int64     `json:"org_id" db:"org_id"`
	ProjectID    *int64    `json:"project_id" db:"project_id"`
	Type         string    `json:"type" db:"type"`
	Status       string    `json:"status" db:"status"`
	Amount       int       `json:"amount" db:"amount"`
	BalanceAfter *int      `json:"balance_after" db:"balance_after"`
	Note         string    `json:"note" db:"note"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// CallLog represents the call_logs table.
type CallLog struct {
	ID                 int64          `json:"id" db:"id"`
	RequestID          string         `json:"request_id" db:"request_id"`
	OrgID              int64          `json:"org_id" db:"org_id"`
	ProjectID          *int64         `json:"project_id" db:"project_id"`
	ApiKeyID           *int64         `json:"api_key_id" db:"api_key_id"`
	Model              string         `json:"model" db:"model"`
	Provider           string         `json:"provider" db:"provider"`
	Channel            string         `json:"channel" db:"channel"`
	PromptTokens       int            `json:"prompt_tokens" db:"prompt_tokens"`
	CompletionTokens   int            `json:"completion_tokens" db:"completion_tokens"`
	TotalTokens        int            `json:"total_tokens" db:"total_tokens"`
	EstimatedTokens    int            `json:"estimated_tokens" db:"estimated_tokens"`
	CostCents          int            `json:"cost_cents" db:"cost_cents"`
	LatencyMs          int            `json:"latency_ms" db:"latency_ms"`
	TtftMs             int            `json:"ttft_ms" db:"ttft_ms"`
	StatusCode         int            `json:"status_code" db:"status_code"`
	UpstreamStatusCode *int           `json:"upstream_status_code" db:"upstream_status_code"`
	UpstreamError      sql.NullString `json:"upstream_error" db:"upstream_error"`
	UsageSource        string         `json:"usage_source" db:"usage_source"`
	UsageConfidence    float64        `json:"usage_confidence" db:"usage_confidence"`
	IsStream           bool           `json:"is_stream" db:"is_stream"`
	CreatedAt          time.Time      `json:"created_at" db:"created_at"`
}

// AuditLog represents the audit_logs table.
type AuditLog struct {
	ID         int64           `json:"id" db:"id"`
	OrgID      int64           `json:"org_id" db:"org_id"`
	ProjectID  *int64          `json:"project_id" db:"project_id"`
	UserID     int64           `json:"user_id" db:"user_id"`
	Action     string          `json:"action" db:"action"`
	Resource   string          `json:"resource" db:"resource"`
	ResourceID string          `json:"resource_id" db:"resource_id"`
	Detail     json.RawMessage `json:"detail" db:"detail"`
	Before     json.RawMessage `json:"before" db:"before_state"`
	After      json.RawMessage `json:"after" db:"after_state"`
	IP         string          `json:"ip" db:"ip"`
	UserAgent  string          `json:"user_agent" db:"user_agent"`
	RequestID  string          `json:"request_id" db:"request_id"`
	Source     string          `json:"source" db:"source"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
}

// Grant represents the grants table.
type Grant struct {
	ID        int64      `json:"id" db:"id"`
	OrgID     int64      `json:"org_id" db:"org_id"`
	Amount    int        `json:"amount" db:"amount"`
	Balance   int        `json:"balance" db:"balance"`
	ExpiresAt *time.Time `json:"expires_at" db:"expires_at"`
	Status    string     `json:"status" db:"status"`
	Reason    string     `json:"reason" db:"reason"`
	CreatedBy int64      `json:"created_by" db:"created_by"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" db:"updated_at"`
}

// FreezeIdempotency represents the freeze_idempotency table.
type FreezeIdempotency struct {
	RequestID string          `json:"request_id" db:"request_id"`
	Status    string          `json:"status" db:"status"`
	Response  json.RawMessage `json:"response" db:"response"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt time.Time       `json:"updated_at" db:"updated_at"`
}

// Setting represents the settings table.
type Setting struct {
	Key         string          `json:"key" db:"key"`
	Value       json.RawMessage `json:"value" db:"value"`
	Description string          `json:"description" db:"description"`
	UpdatedAt   time.Time       `json:"updated_at" db:"updated_at"`
}
