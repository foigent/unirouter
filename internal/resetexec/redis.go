package resetexec

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/resetplan"
)

const redisScanBatch int64 = 256

func ObserveRedis(ctx context.Context, client *redis.Client) (resetplan.RedisObservation, error) {
	if client == nil {
		return resetplan.RedisObservation{}, fmt.Errorf("inspect reset Redis state: client is required")
	}
	observation := resetplan.RedisObservation{BoundToScope: false}
	for _, prefix := range resetplan.RedisKeyPrefixes() {
		count, err := countRedisPrefix(ctx, client, prefix)
		if err != nil {
			return resetplan.RedisObservation{}, err
		}
		observation.Prefixes = append(observation.Prefixes, resetplan.RedisPrefixObservation{Prefix: prefix, KeyCount: count})
	}
	return observation, nil
}

func PurgeRedis(ctx context.Context, client *redis.Client) (int64, error) {
	if client == nil {
		return 0, fmt.Errorf("purge reset Redis state: client is required")
	}
	var deleted int64
	for _, prefix := range resetplan.RedisKeyPrefixes() {
		prefixDeleted, err := purgeRedisPrefix(ctx, client, prefix)
		if err != nil {
			return deleted, err
		}
		deleted += prefixDeleted
	}
	observation, err := ObserveRedis(ctx, client)
	if err != nil {
		return deleted, err
	}
	for _, prefix := range observation.Prefixes {
		if prefix.KeyCount != 0 {
			return deleted, fmt.Errorf("purge reset Redis state: prefix %q retained %d key(s)", prefix.Prefix, prefix.KeyCount)
		}
	}
	return deleted, nil
}

func countRedisPrefix(ctx context.Context, client *redis.Client, prefix string) (int64, error) {
	var cursor uint64
	var count int64
	for {
		keys, next, err := client.Scan(ctx, cursor, prefix+"*", redisScanBatch).Result()
		if err != nil {
			return 0, fmt.Errorf("scan reset Redis prefix %q: %w", prefix, err)
		}
		count += int64(len(keys))
		cursor = next
		if cursor == 0 {
			return count, nil
		}
	}
}

func purgeRedisPrefix(ctx context.Context, client *redis.Client, prefix string) (int64, error) {
	var deleted int64
	for pass := 0; pass < 4; pass++ {
		var cursor uint64
		for {
			keys, next, err := client.Scan(ctx, cursor, prefix+"*", redisScanBatch).Result()
			if err != nil {
				return deleted, fmt.Errorf("scan reset Redis prefix %q: %w", prefix, err)
			}
			if len(keys) > 0 {
				removed, err := client.Unlink(ctx, keys...).Result()
				if err != nil {
					return deleted, fmt.Errorf("unlink reset Redis prefix %q: %w", prefix, err)
				}
				deleted += removed
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		remaining, err := countRedisPrefix(ctx, client, prefix)
		if err != nil {
			return deleted, err
		}
		if remaining == 0 {
			return deleted, nil
		}
	}
	return deleted, fmt.Errorf("purge reset Redis prefix %q did not converge while runtime should be quiesced", prefix)
}
