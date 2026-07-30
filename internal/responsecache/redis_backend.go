package responsecache

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var redisPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_-]{0,95}$`)

type RedisBackend struct {
	client *redis.Client
	prefix string
}

func NewRedisBackend(client *redis.Client, prefix string) (*RedisBackend, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "uniroute:response-cache:v1"
	}
	if client == nil || !redisPrefixPattern.MatchString(prefix) {
		return nil, ErrInvalidConfig
	}
	return &RedisBackend{client: client, prefix: prefix}, nil
}

var _ Backend = (*RedisBackend)(nil)

func (b *RedisBackend) Generation(ctx context.Context, scopeKey string) (uint64, error) {
	value, err := b.client.Get(ctx, b.generationKey(scopeKey)).Uint64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, backendError("generation")
	}
	return value, nil
}

func (b *RedisBackend) Read(ctx context.Context, entryKey string) ([]byte, error) {
	value, err := b.client.Get(ctx, b.key(entryKey)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, backendError("read")
	}
	return append([]byte(nil), value...), nil
}

func (b *RedisBackend) Acquire(ctx context.Context, leaseKey, token string, ttl time.Duration) (bool, error) {
	acquired, err := b.client.SetNX(ctx, b.key(leaseKey), token, ttl).Result()
	if err != nil {
		return false, backendError("acquire")
	}
	return acquired, nil
}

var redisCommitScript = redis.NewScript(`
if redis.call("GET", KEYS[2]) ~= ARGV[1] then
  return 0
end
redis.call("SET", KEYS[1], ARGV[2], "PX", ARGV[3])
redis.call("DEL", KEYS[2])
return 1
`)

func (b *RedisBackend) Commit(
	ctx context.Context,
	entryKey, leaseKey, token string,
	encrypted []byte,
	ttl time.Duration,
) error {
	result, err := redisCommitScript.Run(
		ctx, b.client,
		[]string{b.key(entryKey), b.key(leaseKey)},
		token, encrypted, ttl.Milliseconds(),
	).Int()
	if err != nil {
		return backendError("commit")
	}
	if result != 1 {
		return ErrLeaseLost
	}
	return nil
}

var redisReleaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

func (b *RedisBackend) Release(ctx context.Context, leaseKey, token string) error {
	if _, err := redisReleaseScript.Run(
		ctx, b.client, []string{b.key(leaseKey)}, token,
	).Result(); err != nil {
		return backendError("release")
	}
	return nil
}

func (b *RedisBackend) Invalidate(ctx context.Context, entryKey string) error {
	if err := b.client.Del(ctx, b.key(entryKey)).Err(); err != nil {
		return backendError("invalidate")
	}
	return nil
}

func (b *RedisBackend) Purge(ctx context.Context, scopeKey string) (uint64, error) {
	value, err := b.client.Incr(ctx, b.generationKey(scopeKey)).Uint64()
	if err != nil {
		return 0, backendError("purge")
	}
	return value, nil
}

func (b *RedisBackend) key(opaque string) string {
	return b.prefix + ":" + opaque
}

func (b *RedisBackend) generationKey(scopeKey string) string {
	return b.prefix + ":generation:" + scopeKey
}

func backendError(operation string) error {
	return fmt.Errorf("%w: %s", ErrBackendUnavailable, operation)
}
