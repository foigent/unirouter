package responsecache

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"time"
)

type Service struct {
	backend   Backend
	indexKey  []byte
	keyring   EncryptionKeyring
	config    Config
	clock     func() time.Time
	random    io.Reader
	serviceID string
	waiters   chan struct{}
}

type encryptedEnvelope struct {
	ContractVersion string `json:"contract_version"`
	KeyVersion      string `json:"key_version"`
	Nonce           []byte `json:"nonce"`
	Ciphertext      []byte `json:"ciphertext"`
}

type encryptedRecord struct {
	ContractVersion string     `json:"contract_version"`
	IdentityDigest  string     `json:"identity_digest"`
	ScopeGeneration uint64     `json:"scope_generation"`
	CreatedAt       time.Time  `json:"created_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	SourceResponse  []byte     `json:"source_response"`
	Proof           UsageProof `json:"proof"`
}

func NewService(
	backend Backend,
	indexKey []byte,
	keyring EncryptionKeyring,
	config Config,
	options ...Option,
) (*Service, error) {
	if err := validateServiceDependency(backend, indexKey, keyring); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	runtime := runtimeOptions{clock: time.Now, random: rand.Reader}
	for _, option := range options {
		if option != nil {
			option(&runtime)
		}
	}
	if runtime.clock == nil || runtime.random == nil {
		return nil, ErrInvalidConfig
	}
	serviceIDBytes := make([]byte, 16)
	if _, err := io.ReadFull(runtime.random, serviceIDBytes); err != nil {
		return nil, ErrInvalidConfig
	}
	return &Service{
		backend: backend, indexKey: append([]byte(nil), indexKey...),
		keyring: keyring, config: config, clock: runtime.clock, random: runtime.random,
		serviceID: hex.EncodeToString(serviceIDBytes), waiters: make(chan struct{}, config.MaxWaiters),
	}, nil
}

func (s *Service) Begin(ctx context.Context, request Request) Decision {
	if s == nil || s.backend == nil {
		return bypassDecision(ReasonInvalidConfiguration)
	}
	if err := ctx.Err(); err != nil {
		return invalidDecision(ReasonRequestCancelled)
	}
	if err := request.Identity.Validate(); err != nil {
		return invalidDecision(ReasonInvalidConfiguration)
	}
	switch {
	case !request.OptIn:
		return bypassDecision(ReasonNotOptedIn)
	case !request.PersistenceAllowed:
		return bypassDecision(ReasonPersistenceDenied)
	case request.NoStore:
		return bypassDecision(ReasonNoStore)
	}
	if len(request.EffectiveBody) == 0 || len(request.EffectiveBody) > s.config.MaxRequestBytes {
		return bypassDecision(ReasonIneligibleShape)
	}
	if err := validateCacheableRequest(request.EffectiveBody); err != nil {
		return bypassDecision(ReasonIneligibleShape)
	}

	scopeKey := s.scopeKey(request.Identity.Scope)
	backendCtx, cancelBackend := s.dependencyContext(ctx)
	generation, err := s.backend.Generation(backendCtx, scopeKey)
	cancelBackend()
	if err != nil {
		return bypassDecision(ReasonBackendUnavailable)
	}
	entryKey := s.entryKey(request.Identity, request.EffectiveBody, generation)
	leaseKey := strings.Replace(entryKey, ":entry:", ":lease:", 1)
	identityDigest := strings.TrimPrefix(entryKey, "rc:v1:entry:")

	if hit, readErr := s.readHit(ctx, request.Identity, entryKey, identityDigest, generation); readErr == nil {
		return Decision{Kind: DecisionHit, Reason: ReasonHit, Hit: hit}
	} else if !errors.Is(readErr, ErrCacheMiss) {
		if errors.Is(readErr, ErrIntegrity) {
			backendCtx, cancelBackend := s.dependencyContext(ctx)
			err := s.backend.Invalidate(backendCtx, entryKey)
			cancelBackend()
			if err != nil {
				return bypassDecision(ReasonBackendUnavailable)
			}
			return s.acquire(ctx, request.Identity, entryKey, leaseKey, identityDigest, generation, ReasonIntegrityRecovery)
		}
		return bypassDecision(ReasonBackendUnavailable)
	}
	return s.acquire(ctx, request.Identity, entryKey, leaseKey, identityDigest, generation, ReasonMissOwner)
}

func (s *Service) acquire(
	ctx context.Context,
	identity Identity,
	entryKey, leaseKey, identityDigest string,
	generation uint64,
	ownerReason string,
) Decision {
	token, err := s.randomToken()
	if err != nil {
		return bypassDecision(ReasonInvalidConfiguration)
	}
	backendCtx, cancelBackend := s.dependencyContext(ctx)
	acquired, err := s.backend.Acquire(backendCtx, leaseKey, token, s.config.LeaseTTL)
	cancelBackend()
	if err != nil {
		return bypassDecision(ReasonBackendUnavailable)
	}
	if acquired {
		return Decision{
			Kind: DecisionOwner, Reason: ownerReason,
			Reservation: &Reservation{
				serviceID: s.serviceID, entryKey: entryKey, leaseKey: leaseKey, leaseToken: token,
				scopeGeneration: generation, identity: identity, identityDigest: identityDigest,
				acquiredAt: s.clock().UTC(),
			},
		}
	}
	return Decision{
		Kind: DecisionWait, Reason: ReasonMissWait,
		Wait: &Wait{
			serviceID: s.serviceID, entryKey: entryKey, leaseKey: leaseKey,
			scopeGeneration: generation, identity: identity, identityDigest: identityDigest,
		},
	}
}

// Await waits behind one miss owner. A normal wait timeout is retryable
// rejection, not provider bypass, so healthy-cache contention cannot amplify
// into an upstream stampede. Backend outage remains a safe bypass.
func (s *Service) Await(ctx context.Context, wait *Wait) Decision {
	if s == nil || wait == nil || wait.serviceID != s.serviceID {
		return invalidDecision(ReasonInvalidConfiguration)
	}
	select {
	case s.waiters <- struct{}{}:
		defer func() { <-s.waiters }()
	default:
		return invalidDecision(ReasonWaiterCapacity)
	}

	timeout := time.NewTimer(s.config.MaxWait)
	defer timeout.Stop()
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	for {
		if hit, err := s.readHit(
			ctx, wait.identity, wait.entryKey, wait.identityDigest, wait.scopeGeneration,
		); err == nil {
			return Decision{Kind: DecisionHit, Reason: ReasonHit, Hit: hit}
		} else if !errors.Is(err, ErrCacheMiss) {
			if errors.Is(err, ErrIntegrity) {
				backendCtx, cancelBackend := s.dependencyContext(ctx)
				invalidateErr := s.backend.Invalidate(backendCtx, wait.entryKey)
				cancelBackend()
				if invalidateErr != nil {
					return bypassDecision(ReasonBackendUnavailable)
				}
			} else {
				return bypassDecision(ReasonBackendUnavailable)
			}
		}

		token, err := s.randomToken()
		if err != nil {
			return bypassDecision(ReasonInvalidConfiguration)
		}
		backendCtx, cancelBackend := s.dependencyContext(ctx)
		acquired, err := s.backend.Acquire(backendCtx, wait.leaseKey, token, s.config.LeaseTTL)
		cancelBackend()
		if err != nil {
			return bypassDecision(ReasonBackendUnavailable)
		}
		if acquired {
			return Decision{
				Kind: DecisionOwner, Reason: ReasonMissOwner,
				Reservation: &Reservation{
					serviceID: s.serviceID, entryKey: wait.entryKey, leaseKey: wait.leaseKey,
					leaseToken: token, scopeGeneration: wait.scopeGeneration,
					identity: wait.identity, identityDigest: wait.identityDigest,
					acquiredAt: s.clock().UTC(),
				},
			}
		}

		select {
		case <-ctx.Done():
			return invalidDecision(ReasonRequestCancelled)
		case <-timeout.C:
			return invalidDecision(ReasonWaitTimeout)
		case <-ticker.C:
		}
	}
}

func (s *Service) Commit(ctx context.Context, reservation *Reservation, entry StoreEntry) error {
	if s == nil || reservation == nil || reservation.serviceID != s.serviceID {
		return ErrReservationMismatch
	}
	if err := reservation.identity.Validate(); err != nil ||
		reservation.entryKey == "" || reservation.leaseKey == "" ||
		reservation.leaseToken == "" || reservation.identityDigest == "" {
		return ErrReservationMismatch
	}
	if err := entry.Proof.Validate(reservation.identity); err != nil {
		return err
	}
	if len(entry.SourceResponse) == 0 || len(entry.SourceResponse) > s.config.MaxEntryBytes {
		return ErrInvalidResponse
	}
	if err := validateCacheableResponse(entry.SourceResponse); err != nil {
		return err
	}

	now := s.clock().UTC()
	record := encryptedRecord{
		ContractVersion: Version, IdentityDigest: reservation.identityDigest,
		ScopeGeneration: reservation.scopeGeneration,
		CreatedAt:       now, ExpiresAt: now.Add(s.config.TTL),
		SourceResponse: append([]byte(nil), entry.SourceResponse...), Proof: entry.Proof,
	}
	encrypted, err := s.seal(ctx, reservation.entryKey, record)
	if err != nil {
		return err
	}
	backendCtx, cancelBackend := s.dependencyContext(ctx)
	defer cancelBackend()
	return s.backend.Commit(
		backendCtx, reservation.entryKey, reservation.leaseKey, reservation.leaseToken,
		encrypted, s.config.TTL,
	)
}

func (s *Service) Release(ctx context.Context, reservation *Reservation) error {
	if s == nil || reservation == nil || reservation.serviceID != s.serviceID {
		return ErrReservationMismatch
	}
	backendCtx, cancelBackend := s.dependencyContext(ctx)
	defer cancelBackend()
	return s.backend.Release(backendCtx, reservation.leaseKey, reservation.leaseToken)
}

// PurgeScope writes the generation tombstone before old encrypted values age
// out. No later lookup derives an old-generation entry key.
func (s *Service) PurgeScope(ctx context.Context, scope Scope) (uint64, error) {
	if s == nil || s.backend == nil {
		return 0, ErrInvalidConfig
	}
	if err := scope.Validate(); err != nil {
		return 0, err
	}
	backendCtx, cancelBackend := s.dependencyContext(ctx)
	defer cancelBackend()
	return s.backend.Purge(backendCtx, s.scopeKey(scope))
}

func (s *Service) readHit(
	ctx context.Context,
	identity Identity,
	entryKey, identityDigest string,
	generation uint64,
) (*Hit, error) {
	backendCtx, cancelBackend := s.dependencyContext(ctx)
	encrypted, err := s.backend.Read(backendCtx, entryKey)
	cancelBackend()
	if err != nil {
		return nil, err
	}
	record, err := s.open(ctx, entryKey, encrypted)
	if err != nil {
		return nil, ErrIntegrity
	}
	now := s.clock().UTC()
	if record.ContractVersion != Version ||
		record.IdentityDigest != identityDigest ||
		record.ScopeGeneration != generation ||
		!now.Before(record.ExpiresAt) ||
		record.CreatedAt.After(now) ||
		record.ExpiresAt.Sub(record.CreatedAt) <= 0 ||
		record.ExpiresAt.Sub(record.CreatedAt) > s.config.TTL ||
		len(record.SourceResponse) == 0 ||
		len(record.SourceResponse) > s.config.MaxEntryBytes ||
		record.Proof.Validate(identity) != nil ||
		validateCacheableResponse(record.SourceResponse) != nil {
		return nil, ErrIntegrity
	}
	return &Hit{
		SourceResponse:          append([]byte(nil), record.SourceResponse...),
		SourceProof:             record.Proof,
		CurrentProviderRequests: 0, CurrentProviderCostMicros: CurrentProviderCostZero,
		CustomerChargeState: CustomerChargeUnpriced,
		CreatedAt:           record.CreatedAt, ExpiresAt: record.ExpiresAt,
	}, nil
}

func (s *Service) seal(ctx context.Context, entryKey string, record encryptedRecord) ([]byte, error) {
	keyCtx, cancelKey := s.dependencyContext(ctx)
	version, key, err := s.keyring.Current(keyCtx)
	cancelKey()
	if err != nil || !portableIdentity(version) || len(key) != 32 {
		wipeBytes(key)
		return nil, ErrEncryptionKey
	}
	defer wipeBytes(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrEncryptionKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrEncryptionKey
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(s.random, nonce); err != nil {
		return nil, ErrEncryptionKey
	}
	plaintext, err := json.Marshal(record)
	if err != nil {
		return nil, ErrInvalidResponse
	}
	defer wipeBytes(plaintext)
	ciphertext := aead.Seal(nil, nonce, plaintext, cacheAAD(entryKey))
	return json.Marshal(encryptedEnvelope{
		ContractVersion: Version, KeyVersion: version,
		Nonce: nonce, Ciphertext: ciphertext,
	})
}

func (s *Service) open(ctx context.Context, entryKey string, encoded []byte) (encryptedRecord, error) {
	if err := validateStrictJSON(encoded); err != nil {
		return encryptedRecord{}, ErrIntegrity
	}
	var envelope encryptedEnvelope
	if err := json.Unmarshal(encoded, &envelope); err != nil ||
		envelope.ContractVersion != Version ||
		!portableIdentity(envelope.KeyVersion) {
		return encryptedRecord{}, ErrIntegrity
	}
	keyCtx, cancelKey := s.dependencyContext(ctx)
	key, err := s.keyring.ByVersion(keyCtx, envelope.KeyVersion)
	cancelKey()
	if err != nil || len(key) != 32 {
		wipeBytes(key)
		return encryptedRecord{}, ErrEncryptionKey
	}
	defer wipeBytes(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return encryptedRecord{}, ErrEncryptionKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(envelope.Nonce) != aead.NonceSize() {
		return encryptedRecord{}, ErrIntegrity
	}
	plaintext, err := aead.Open(nil, envelope.Nonce, envelope.Ciphertext, cacheAAD(entryKey))
	if err != nil {
		return encryptedRecord{}, ErrIntegrity
	}
	defer wipeBytes(plaintext)
	if err := validateStrictJSON(plaintext); err != nil {
		return encryptedRecord{}, ErrIntegrity
	}
	var record encryptedRecord
	if err := json.Unmarshal(plaintext, &record); err != nil {
		return encryptedRecord{}, ErrIntegrity
	}
	record.SourceResponse = append([]byte(nil), record.SourceResponse...)
	return record, nil
}

func cacheAAD(entryKey string) []byte {
	return []byte(Version + "\x00" + entryKey)
}

func (s *Service) scopeKey(scope Scope) string {
	mac := hmac.New(sha256.New, s.indexKey)
	writeLP(mac, "scope")
	writeInt64(mac, scope.OrgID)
	writeInt64(mac, scope.ProjectID)
	writeLP(mac, scope.Region)
	writeLP(mac, scope.Residency)
	return "rc:v1:scope:" + hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) entryKey(identity Identity, requestBody []byte, generation uint64) string {
	mac := hmac.New(sha256.New, s.indexKey)
	writeLP(mac, Version)
	writeUint64(mac, generation)
	writeInt64(mac, identity.Scope.OrgID)
	writeInt64(mac, identity.Scope.ProjectID)
	writeInt64(mac, identity.APIKeyID)
	for _, value := range []string{
		identity.Scope.Region, identity.Scope.Residency,
		identity.AuthorizationEpoch,
		identity.Endpoint, identity.RequestedModel, identity.ResolvedModel,
		identity.AdapterProfileVersion, identity.RoutePolicyVersion,
		identity.GuardrailPolicyVersion, identity.CustomerPricingVersion,
		identity.ResponseCachePriceVersion, identity.RequestSchemaVersion,
		identity.ResponseSchemaVersion,
	} {
		writeLP(mac, value)
	}
	requestDigest := sha256.Sum256(requestBody)
	writeLPBytes(mac, requestDigest[:])
	writeUint64(mac, uint64(len(requestBody)))
	return "rc:v1:entry:" + hex.EncodeToString(mac.Sum(nil))
}

func writeInt64(w io.Writer, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = w.Write(encoded[:])
}

func writeUint64(w io.Writer, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = w.Write(encoded[:])
}

func writeLP(w io.Writer, value string) {
	writeLPBytes(w, []byte(value))
}

func writeLPBytes(w io.Writer, value []byte) {
	if len(value) > math.MaxUint32 {
		return
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = w.Write(length[:])
	_, _ = w.Write(value)
}

func (s *Service) randomToken() (string, error) {
	value := make([]byte, 24)
	if _, err := io.ReadFull(s.random, value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (s *Service) dependencyContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, s.config.DependencyTimeout)
}

func validateCacheableRequest(raw []byte) error {
	if err := validateStrictJSON(raw); err != nil {
		return ErrInvalidRequest
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wire); err != nil || wire == nil {
		return ErrInvalidRequest
	}
	if stream, exists := wire["stream"]; exists {
		var enabled bool
		if err := json.Unmarshal(stream, &enabled); err != nil || enabled {
			return ErrInvalidRequest
		}
	}
	for _, field := range []string{
		"tools", "tool_choice", "parallel_tool_calls", "functions", "function_call",
		"modalities", "audio", "image", "images", "video",
	} {
		if _, exists := wire[field]; exists {
			return ErrInvalidRequest
		}
	}
	if _, exists := wire["messages"]; !exists {
		return ErrInvalidRequest
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return ErrInvalidRequest
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		return ErrInvalidRequest
	}
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			return ErrInvalidRequest
		}
		role, ok := message["role"].(string)
		if !ok {
			return ErrInvalidRequest
		}
		switch role {
		case "system", "developer", "user", "assistant":
		default:
			return ErrInvalidRequest
		}
		for _, field := range []string{
			"tool_calls", "tool_call_id", "function_call", "audio", "image", "images", "video",
		} {
			if _, exists := message[field]; exists {
				return ErrInvalidRequest
			}
		}
		if content, exists := message["content"]; exists && content != nil {
			if _, ok := content.(string); !ok {
				// Structured content may contain media. B5 intentionally
				// admits text-only exact requests and fails closed here.
				return ErrInvalidRequest
			}
		}
	}
	return nil
}

func validateCacheableResponse(raw []byte) error {
	if err := validateStrictJSON(raw); err != nil {
		return ErrInvalidResponse
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return ErrInvalidResponse
	}
	if _, exists := payload["uniroute"]; exists {
		return ErrInvalidResponse
	}
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ErrInvalidResponse
	}
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]any)
		if !ok {
			return ErrInvalidResponse
		}
		if finish, _ := choice["finish_reason"].(string); finish == "tool_calls" || finish == "function_call" {
			return ErrInvalidResponse
		}
		message, ok := choice["message"].(map[string]any)
		if !ok {
			return ErrInvalidResponse
		}
		for _, field := range []string{"tool_calls", "function_call", "audio", "image", "images", "video"} {
			if _, exists := message[field]; exists {
				return ErrInvalidResponse
			}
		}
	}
	return nil
}

func validateStrictJSON(raw []byte) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return ErrInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := consumeStrictValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalidRequest
	}
	return nil
}

func consumeStrictValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidRequest
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return ErrInvalidRequest
			}
			key, ok := keyToken.(string)
			if !ok {
				return ErrInvalidRequest
			}
			if _, exists := seen[key]; exists {
				return ErrInvalidRequest
			}
			seen[key] = struct{}{}
			if err := consumeStrictValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalidRequest
		}
	case '[':
		for decoder.More() {
			if err := consumeStrictValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return ErrInvalidRequest
		}
	default:
		return ErrInvalidRequest
	}
	return nil
}

func wipeBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
