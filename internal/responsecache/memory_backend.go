package responsecache

import (
	"context"
	"sync"
	"time"
)

type memoryValue struct {
	bytes     []byte
	expiresAt time.Time
}

type memoryLease struct {
	token     string
	expiresAt time.Time
}

// MemoryBackend is a bounded-semantics fixture. It is not a production
// multi-replica cache store.
type MemoryBackend struct {
	mu          sync.Mutex
	clock       func() time.Time
	available   bool
	values      map[string]memoryValue
	leases      map[string]memoryLease
	generations map[string]uint64
}

func NewMemoryBackend(clock func() time.Time) *MemoryBackend {
	if clock == nil {
		clock = time.Now
	}
	return &MemoryBackend{
		clock: clock, available: true,
		values: make(map[string]memoryValue), leases: make(map[string]memoryLease),
		generations: make(map[string]uint64),
	}
}

var _ Backend = (*MemoryBackend)(nil)

func (b *MemoryBackend) SetAvailable(available bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.available = available
}

func (b *MemoryBackend) Generation(ctx context.Context, scopeKey string) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.available {
		return 0, ErrBackendUnavailable
	}
	return b.generations[scopeKey], nil
}

func (b *MemoryBackend) Read(ctx context.Context, entryKey string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.available {
		return nil, ErrBackendUnavailable
	}
	value, exists := b.values[entryKey]
	if !exists {
		return nil, ErrCacheMiss
	}
	if !b.clock().Before(value.expiresAt) {
		delete(b.values, entryKey)
		return nil, ErrCacheMiss
	}
	return append([]byte(nil), value.bytes...), nil
}

func (b *MemoryBackend) Acquire(ctx context.Context, leaseKey, token string, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.available {
		return false, ErrBackendUnavailable
	}
	now := b.clock()
	if lease, exists := b.leases[leaseKey]; exists && now.Before(lease.expiresAt) {
		return false, nil
	}
	b.leases[leaseKey] = memoryLease{token: token, expiresAt: now.Add(ttl)}
	return true, nil
}

func (b *MemoryBackend) Commit(
	ctx context.Context,
	entryKey, leaseKey, token string,
	encrypted []byte,
	ttl time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.available {
		return ErrBackendUnavailable
	}
	now := b.clock()
	lease, exists := b.leases[leaseKey]
	if !exists || lease.token != token || !now.Before(lease.expiresAt) {
		return ErrLeaseLost
	}
	b.values[entryKey] = memoryValue{
		bytes: append([]byte(nil), encrypted...), expiresAt: now.Add(ttl),
	}
	delete(b.leases, leaseKey)
	return nil
}

func (b *MemoryBackend) Release(ctx context.Context, leaseKey, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.available {
		return ErrBackendUnavailable
	}
	if lease, exists := b.leases[leaseKey]; exists && lease.token == token {
		delete(b.leases, leaseKey)
	}
	return nil
}

func (b *MemoryBackend) Invalidate(ctx context.Context, entryKey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.available {
		return ErrBackendUnavailable
	}
	delete(b.values, entryKey)
	return nil
}

func (b *MemoryBackend) Purge(ctx context.Context, scopeKey string) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.available {
		return 0, ErrBackendUnavailable
	}
	b.generations[scopeKey]++
	return b.generations[scopeKey], nil
}
