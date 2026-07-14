package redisx

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis holds the Redis client.
type Redis struct {
	Client *redis.Client
}

// NewRedis creates a new Redis client.
func NewRedis(addr, password string, db, maxConns int) (*Redis, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     maxConns,
		MinIdleConns: 2,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		// Admission, authority and billing callers set tighter operation
		// deadlines. Respect them instead of allowing the socket defaults to
		// stretch a bounded failure path into seconds.
		ContextTimeoutEnabled: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &Redis{Client: client}, nil
}

// Close closes the Redis client.
func (r *Redis) Close() error {
	return r.Client.Close()
}

// Health checks the Redis connection.
func (r *Redis) Health(ctx context.Context) error {
	return r.Client.Ping(ctx).Err()
}

// GetBalance retrieves the v2 micro-CNY wallet cache for an org.
func (r *Redis) GetBalance(ctx context.Context, orgID int64) (int64, int64, error) {
	key := fmt.Sprintf("wallet:v2:%d", orgID)
	vals, err := r.Client.HMGet(ctx, key, "balance_micros", "frozen_micros").Result()
	if err != nil {
		return 0, 0, err
	}
	var balance, frozen int64
	if vals[0] != nil {
		balance, err = strconv.ParseInt(fmt.Sprint(vals[0]), 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse cached balance: %w", err)
		}
	}
	if vals[1] != nil {
		frozen, err = strconv.ParseInt(fmt.Sprint(vals[1]), 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse cached frozen balance: %w", err)
		}
	}
	return balance, frozen, nil
}

// SetBalance caches the balance for an org.
func (r *Redis) SetBalance(ctx context.Context, orgID int64, balanceMicros, frozenMicros int64) error {
	key := fmt.Sprintf("wallet:v2:%d", orgID)
	return r.Client.HSet(ctx, key, "balance_micros", balanceMicros, "frozen_micros", frozenMicros).Err()
}

// InvalidateBalance removes the cached balance for an org.
func (r *Redis) InvalidateBalance(ctx context.Context, orgID int64) error {
	key := fmt.Sprintf("wallet:v2:%d", orgID)
	return r.Client.Del(ctx, key).Err()
}
