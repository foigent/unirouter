package responsecache

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newRedisFixture(t *testing.T) (*Service, *RedisBackend, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr:         server.Addr(),
		MaxRetries:   -1,
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	backend, err := NewRedisBackend(client, "uniroute:response-cache:test")
	require.NoError(t, err)
	ring := fixtureKeyring(t, "k1", map[string][]byte{
		"k1": bytes.Repeat([]byte{0x31}, 32),
	})
	service := newFixtureService(t, backend, time.Now, ring, fixtureConfig())
	return service, backend, server, client
}

func TestRedisBackendEndToEndUsesOpaqueKeysAndEncryptedValues(t *testing.T) {
	service, _, server, _ := newRedisFixture(t)
	request := fixtureRequest()
	hit := storeFixture(t, service, request)
	require.Equal(t, fixtureResponseBody, hit.Hit.SourceResponse)

	keys := server.Keys()
	require.Len(t, keys, 1)
	for _, key := range keys {
		require.True(t, strings.HasPrefix(key, "uniroute:response-cache:test:rc:v1:entry:"))
		require.NotContains(t, key, "exact private")
		require.NotContains(t, key, request.Identity.RequestedModel)
		require.NotContains(t, key, request.Identity.AuthorizationEpoch)
		value, err := server.Get(key)
		require.NoError(t, err)
		require.NotContains(t, value, "exact private prompt")
		require.NotContains(t, value, "exact private answer")
		require.NotContains(t, value, request.Identity.ResolvedModel)
		require.NotContains(t, value, "openai")
	}

	generation, err := service.PurgeScope(context.Background(), request.Identity.Scope)
	require.NoError(t, err)
	require.EqualValues(t, 1, generation)
	next := service.Begin(context.Background(), request)
	require.Equal(t, DecisionOwner, next.Kind)
	require.NoError(t, service.Release(context.Background(), next.Reservation))
	require.Len(t, server.Keys(), 2)
}

func TestRedisLeaseCompareAndSet(t *testing.T) {
	_, backend, _, _ := newRedisFixture(t)
	ctx := context.Background()
	const (
		entryKey = "rc:v1:entry:opaque"
		leaseKey = "rc:v1:lease:opaque"
	)
	acquired, err := backend.Acquire(ctx, leaseKey, "owner-a", time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = backend.Acquire(ctx, leaseKey, "owner-b", time.Second)
	require.NoError(t, err)
	require.False(t, acquired)

	require.NoError(t, backend.Release(ctx, leaseKey, "owner-b"))
	acquired, err = backend.Acquire(ctx, leaseKey, "owner-b", time.Second)
	require.NoError(t, err)
	require.False(t, acquired)
	require.ErrorIs(t, backend.Commit(ctx, entryKey, leaseKey, "owner-b", []byte("ciphertext"), time.Minute), ErrLeaseLost)
	require.NoError(t, backend.Commit(ctx, entryKey, leaseKey, "owner-a", []byte("ciphertext"), time.Minute))
	value, err := backend.Read(ctx, entryKey)
	require.NoError(t, err)
	require.Equal(t, []byte("ciphertext"), value)
}

func TestRedisOutageBecomesSafeBypass(t *testing.T) {
	service, _, server, _ := newRedisFixture(t)
	server.Close()

	start := time.Now()
	decision := service.Begin(context.Background(), fixtureRequest())
	require.Equal(t, DecisionBypass, decision.Kind)
	require.Equal(t, ReasonBackendUnavailable, decision.Reason)
	require.Less(t, time.Since(start), 250*time.Millisecond)
}

func TestRedisBackendRejectsInvalidDependencyAndSanitizesErrors(t *testing.T) {
	backend, err := NewRedisBackend(nil, "safe")
	require.Nil(t, backend)
	require.ErrorIs(t, err, ErrInvalidConfig)

	service, backend, server, _ := newRedisFixture(t)
	request := fixtureRequest()
	owner := service.Begin(context.Background(), request)
	require.Equal(t, DecisionOwner, owner.Kind)
	server.Close()
	err = backend.Commit(
		context.Background(),
		owner.Reservation.entryKey,
		owner.Reservation.leaseKey,
		owner.Reservation.leaseToken,
		[]byte("not-secret"),
		time.Minute,
	)
	require.ErrorIs(t, err, ErrBackendUnavailable)
	require.False(t, errors.Is(err, ErrLeaseLost))
	require.NotContains(t, err.Error(), owner.Reservation.entryKey)
	require.NotContains(t, err.Error(), owner.Reservation.leaseToken)
}
