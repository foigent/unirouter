package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/models"
)

// Router selects the best channel and key for a given model.
type Router struct {
	db db.DBQuerier
}

// NewRouter creates a new Router.
func NewRouter(db db.DBQuerier) *Router {
	return &Router{db: db}
}

// RouteResult contains the selected channel, key, and mapped model name.
type RouteResult struct {
	Channel     models.Channel
	ChannelKey  models.ChannelKey
	MappedModel string
}

const channelQuery = `SELECT c.id, c.name, c.provider, c.base_url, c.models, c.model_mapping,
        COALESCE(c.adapter_type, 'openai_compatible'), COALESCE(c.adapter_config, '{}'::jsonb),
        c.max_context, c.default_max_output, c.priority, c.status,
        c.circuit_state, c.half_open_until, c.fail_count,
        ck.id, ck.key_enc, ck.status, ck.cooldown_until
 FROM channels c
 JOIN channel_keys ck ON ck.channel_id = c.id
 WHERE c.status = 'active'
   AND ck.status = 'active'
   AND (ck.cooldown_until IS NULL OR ck.cooldown_until < NOW())
   AND (c.circuit_state = 'healthy' OR (c.circuit_state = 'half_open' AND c.half_open_until <= NOW()))
   AND (',' || c.models || ',' LIKE '%,' || $1 || ',%')
 ORDER BY c.priority ASC, COALESCE(ck.last_used_at, '1970-01-01'::timestamptz) ASC, ck.id ASC`

// Route selects a channel and key for the requested model.
// Logic: find active channels that support the model, ordered by priority.
// Half-open channels become eligible after their recovery deadline and are
// filtered to allow only ~10% probe traffic.
func (r *Router) Route(ctx context.Context, modelName string) (*RouteResult, error) {
	rows, err := r.db.Query(ctx, channelQuery, modelName)
	if err != nil {
		return nil, fmt.Errorf("query channels: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ch models.Channel
		var ck models.ChannelKey
		err := rows.Scan(
			&ch.ID, &ch.Name, &ch.Provider, &ch.BaseURL, &ch.Models, &ch.ModelMapping,
			&ch.AdapterType, &ch.AdapterConfig, &ch.MaxContext, &ch.DefaultMaxOutput, &ch.Priority, &ch.Status,
			&ch.CircuitState, &ch.HalfOpenUntil, &ch.FailCount,
			&ck.ID, &ck.KeyEnc, &ck.Status, &ck.CooldownUntil,
		)
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		// Half-open channels: only allow 10% traffic through
		if ch.CircuitState == "half_open" && rand.Intn(100) >= 10 {
			continue // skip, try next candidate
		}

		mappedModel := resolveModelMapping(modelName, ch.ModelMapping)
		ch.AdapterType = adapters.NormalizeType(ch.AdapterType)
		// Update last_used_at for round-robin
		_, _ = r.db.Exec(ctx, `UPDATE channel_keys SET last_used_at = NOW() WHERE id = $1`, ck.ID)

		return &RouteResult{
			Channel:     ch,
			ChannelKey:  ck,
			MappedModel: mappedModel,
		}, nil
	}

	return nil, fmt.Errorf("no available channel for model %s", modelName)
}

// RouteAll returns all available routes for a model, ordered by priority.
// Used for failover: if the first channel fails, the relay can try the next.
// Half-open channels become eligible after their recovery deadline and are
// filtered to allow only ~10% probe traffic.
func (r *Router) RouteAll(ctx context.Context, modelName string) ([]RouteResult, error) {
	rows, err := r.db.Query(ctx, channelQuery, modelName)
	if err != nil {
		return nil, fmt.Errorf("query channels: %w", err)
	}
	defer rows.Close()

	var results []RouteResult
	for rows.Next() {
		var ch models.Channel
		var ck models.ChannelKey
		err := rows.Scan(
			&ch.ID, &ch.Name, &ch.Provider, &ch.BaseURL, &ch.Models, &ch.ModelMapping,
			&ch.AdapterType, &ch.AdapterConfig, &ch.MaxContext, &ch.DefaultMaxOutput, &ch.Priority, &ch.Status,
			&ch.CircuitState, &ch.HalfOpenUntil, &ch.FailCount,
			&ck.ID, &ck.KeyEnc, &ck.Status, &ck.CooldownUntil,
		)
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		// Half-open channels: only allow 10% traffic through
		if ch.CircuitState == "half_open" && rand.Intn(100) >= 10 {
			continue
		}

		mappedModel := resolveModelMapping(modelName, ch.ModelMapping)
		ch.AdapterType = adapters.NormalizeType(ch.AdapterType)
		results = append(results, RouteResult{
			Channel:     ch,
			ChannelKey:  ck,
			MappedModel: mappedModel,
		})
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no available channel for model %s", modelName)
	}
	return results, nil
}

// MarkChannelSuccess records a successful request for a channel.
// Resets circuit breaker state to healthy if it was half_open.
func (r *Router) MarkChannelSuccess(ctx context.Context, channelID int64) error {
	_, err := r.db.Exec(ctx,
		`UPDATE channels SET fail_count = 0, circuit_state = 'healthy', half_open_until = NULL
		 WHERE id = $1`,
		channelID,
	)
	return err
}

// MarkChannelFail records a failed request for a channel.
// If failure count exceeds threshold, transitions to half_open.
func (r *Router) MarkChannelFail(ctx context.Context, channelID int64, threshold int, recoverSecs int) error {
	var failCount int
	err := r.db.QueryRow(ctx,
		`UPDATE channels SET fail_count = fail_count + 1
		 WHERE id = $1
		 RETURNING fail_count`,
		channelID,
	).Scan(&failCount)
	if err != nil {
		return err
	}

	if failCount >= threshold {
		halfOpenUntil := time.Now().Add(time.Duration(recoverSecs) * time.Second)
		_, err = r.db.Exec(ctx,
			`UPDATE channels SET circuit_state = 'half_open', half_open_until = $1
			 WHERE id = $2`,
			halfOpenUntil, channelID,
		)
		return err
	}
	return nil
}

// MarkKeyCooldown puts a key on cooldown for the specified duration.
func (r *Router) MarkKeyCooldown(ctx context.Context, keyID int64, cooldownSecs int) error {
	cooldownUntil := time.Now().Add(time.Duration(cooldownSecs) * time.Second)
	_, err := r.db.Exec(ctx,
		`UPDATE channel_keys SET cooldown_until = $1 WHERE id = $2`,
		cooldownUntil, keyID,
	)
	return err
}

// resolveModelMapping applies model alias mapping.
// ModelMapping is a JSON string like: {"gpt-4": "gpt-4-0613"}
func resolveModelMapping(requestedModel string, modelMapping json.RawMessage) string {
	if len(modelMapping) == 0 {
		return requestedModel
	}
	mmStr := string(modelMapping)
	if mmStr == "{}" || mmStr == "null" {
		return requestedModel
	}
	var mapping map[string]string
	if err := json.Unmarshal(modelMapping, &mapping); err != nil {
		return requestedModel
	}
	if mapped, ok := mapping[requestedModel]; ok {
		return mapped
	}
	return requestedModel
}

// MarkKeyFail increments fail_count on a channel_key.
// If fail_count reaches 3, sets status='invalid'.
func (r *Router) MarkKeyFail(ctx context.Context, keyID int64) error {
	var failCount int
	err := r.db.QueryRow(ctx,
		`UPDATE channel_keys SET fail_count = fail_count + 1, updated_at = NOW()
		 WHERE id = $1
		 RETURNING fail_count`,
		keyID,
	).Scan(&failCount)
	if err != nil {
		return err
	}
	if failCount >= 3 {
		_, err = r.db.Exec(ctx,
			`UPDATE channel_keys SET status = 'invalid', updated_at = NOW() WHERE id = $1`,
			keyID,
		)
		log.Printf("Auto-invalidated channel_key %d after %d consecutive failures", keyID, failCount)
		return err
	}
	return nil
}

// MarkKeySuccess resets fail_count on a channel_key.
func (r *Router) MarkKeySuccess(ctx context.Context, keyID int64) error {
	_, err := r.db.Exec(ctx,
		`UPDATE channel_keys SET fail_count = 0, last_used_at = NOW(), updated_at = NOW()
		 WHERE id = $1`,
		keyID,
	)
	return err
}
