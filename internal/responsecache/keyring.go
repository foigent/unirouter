package responsecache

import (
	"context"
	"sync"
)

// StaticKeyring is a fixture and local-deployment keyring. Production key
// ownership remains behind EncryptionKeyring and the B4 secret-reference
// boundary.
type StaticKeyring struct {
	mu      sync.RWMutex
	current string
	keys    map[string][]byte
}

func NewStaticKeyring(current string, keys map[string][]byte) (*StaticKeyring, error) {
	if !portableIdentity(current) || len(keys) == 0 {
		return nil, ErrEncryptionKey
	}
	ring := &StaticKeyring{current: current, keys: make(map[string][]byte, len(keys))}
	for version, key := range keys {
		if !portableIdentity(version) || len(key) != 32 {
			return nil, ErrEncryptionKey
		}
		ring.keys[version] = append([]byte(nil), key...)
	}
	if _, exists := ring.keys[current]; !exists {
		return nil, ErrEncryptionKey
	}
	return ring, nil
}

func (r *StaticKeyring) Current(ctx context.Context) (string, []byte, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if r == nil {
		return "", nil, ErrEncryptionKey
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := r.keys[r.current]
	if len(key) != 32 {
		return "", nil, ErrEncryptionKey
	}
	return r.current, append([]byte(nil), key...), nil
}

func (r *StaticKeyring) ByVersion(ctx context.Context, version string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || !portableIdentity(version) {
		return nil, ErrEncryptionKey
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := r.keys[version]
	if len(key) != 32 {
		return nil, ErrEncryptionKey
	}
	return append([]byte(nil), key...), nil
}
